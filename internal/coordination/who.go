package coordination

import (
	"context"
	"path/filepath"
	"sort"
	"time"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/memory"
)

// Scopes and sources of a Presence row. The scope says how close the person is
// to this checkout, the source says where the information came from.
const (
	ScopeDirectory = "directory" // a live session in this working directory
	ScopeWorktree  = "worktree"  // a live session in another worktree of this machine
	ScopeRemote    = "remote"    // pushed state: active-work files, shared memory

	SourceSession = "session"
	SourceWork    = "work-file"
	SourceMemory  = "memory"
)

// Presence is one row of `gabitotl who`: one person doing one thing somewhere.
type Presence struct {
	User     string    `json:"user"`
	Own      bool      `json:"own"`
	Scope    string    `json:"scope"`
	Source   string    `json:"source"`
	Task     string    `json:"task,omitempty"`
	Branch   string    `json:"branch,omitempty"`
	Worktree string    `json:"worktree,omitempty"`
	Areas    []string  `json:"areas,omitempty"`
	Runtime  string    `json:"runtime,omitempty"`
	Started  time.Time `json:"started,omitzero"`
	LastSeen time.Time `json:"last_seen,omitzero"`
}

// WhoReport is the advisory answer. Warnings name the sources that could not be
// read; a missing source never makes the report fail.
type WhoReport struct {
	People   []Presence `json:"people"`
	Warnings []string   `json:"warnings,omitempty"`
}

// WhoSources are the optional readers beyond this directory. A nil reader skips
// its source silently.
type WhoSources struct {
	// Worktrees lists the git worktrees of the repository (this one included).
	Worktrees func(ctx context.Context) ([]Worktree, error)
	// Remote lists recently updated shared-memory entries (who works on what).
	Remote func(ctx context.Context) ([]memory.Entry, error)
}

// Who merges the three scopes into one list: live sessions of this directory,
// live sessions of the other worktrees, and pushed state (active-work files and
// shared memory). The same person and task seen through several sources is one
// row; the most specific source wins and the other rows only fill what it
// lacks (branch, areas). Everything is best effort.
func Who(ctx context.Context, layout gabitotl.Layout, self string, now time.Time, src WhoSources) WhoReport {
	var rep WhoReport
	var rows []Presence

	var wts []Worktree
	if src.Worktrees != nil {
		var err error
		if wts, err = src.Worktrees(ctx); err != nil {
			rep.Warnings = append(rep.Warnings, "worktrees unavailable: "+err.Error())
		}
	}
	here := canonical(layout.Root)
	hereBranch := ""
	for _, wt := range wts {
		if canonical(wt.Path) == here {
			hereBranch = wt.Branch
		}
	}

	if ss, err := Sessions(layout, self, now); err != nil {
		rep.Warnings = append(rep.Warnings, "sessions unreadable: "+err.Error())
	} else {
		for _, s := range ss {
			rows = append(rows, fromSession(s, ScopeDirectory, layout.Root, hereBranch))
		}
	}

	for _, wt := range wts {
		if canonical(wt.Path) == here {
			continue
		}
		// An unreadable or missing directory of another worktree is just empty.
		ss, err := Sessions(gabitotl.NewLayout(wt.Path), self, now)
		if err != nil {
			rep.Warnings = append(rep.Warnings, "sessions of "+wt.Path+" unreadable: "+err.Error())
			continue
		}
		for _, s := range ss {
			rows = append(rows, fromSession(s, ScopeWorktree, wt.Path, wt.Branch))
		}
	}

	tasks, err := ListTasks(layout, "")
	if err != nil {
		rep.Warnings = append(rep.Warnings, "active work unreadable: "+err.Error())
	}
	for _, t := range tasks {
		if !t.Active() {
			continue
		}
		rows = append(rows, Presence{
			User: t.Owner, Own: t.Owner == self, Scope: ScopeRemote, Source: SourceWork,
			Task: t.ID, Branch: t.Branch, Areas: append(append([]string{}, t.Areas...), t.Files...),
		})
	}

	if src.Remote != nil {
		entries, err := src.Remote(ctx)
		if err != nil {
			rep.Warnings = append(rep.Warnings, "shared memory unavailable: "+err.Error())
		}
		for _, e := range entries {
			if e.Actor == "" || e.Actor == memory.ActorLegacy {
				continue
			}
			rows = append(rows, Presence{
				User: e.Actor, Own: e.Actor == self, Scope: ScopeRemote, Source: SourceMemory,
				Task: e.Task, Branch: e.Branch, Areas: e.Files, LastSeen: e.Timestamp,
			})
		}
	}

	rep.People = mergePresence(rows)
	return rep
}

func fromSession(s Session, scope, dir, wtBranch string) Presence {
	p := Presence{
		User: s.User, Own: s.Own, Scope: scope, Source: SourceSession, Task: s.Task, Branch: s.Branch,
		Areas: s.Areas, Runtime: s.Runtime, Started: s.Started, LastSeen: s.LastSeen,
	}
	if p.Branch == "" {
		p.Branch = wtBranch
	}
	if scope == ScopeWorktree {
		p.Worktree = dir
	}
	return p
}

// rank orders scopes and sources from most to least specific.
func rank(p Presence) int {
	switch {
	case p.Scope == ScopeDirectory:
		return 0
	case p.Scope == ScopeWorktree:
		return 1
	case p.Source == SourceWork:
		return 2
	default:
		return 3
	}
}

// sameWork reports whether two rows are the same person doing the same thing:
// the same task, or no task on one side and the same branch, or nothing but a
// shell in the same place on both.
func sameWork(a, b Presence) bool {
	if a.User != b.User {
		return false
	}
	switch {
	case a.Task != "" && b.Task != "":
		return a.Task == b.Task
	case a.Branch != "" && a.Branch == b.Branch:
		return true
	case a.Task == "" && b.Task == "" && a.Branch == "" && b.Branch == "":
		return a.Scope == b.Scope && a.Worktree == b.Worktree
	}
	return false
}

// mergePresence collapses rows of the same person and work, keeping the most
// specific source and filling its empty fields from the others. The result is
// ordered by user, then scope, then task.
func mergePresence(rows []Presence) []Presence {
	sort.SliceStable(rows, func(i, j int) bool { return rank(rows[i]) < rank(rows[j]) })
	var out []Presence
next:
	for _, r := range rows {
		for i := range out {
			if !sameWork(out[i], r) {
				continue
			}
			m := &out[i]
			if m.Task == "" {
				m.Task = r.Task
			}
			if m.Branch == "" {
				m.Branch = r.Branch
			}
			if len(m.Areas) == 0 {
				m.Areas = r.Areas
			}
			if m.Runtime == "" {
				m.Runtime = r.Runtime
			}
			if m.Started.IsZero() || (!r.Started.IsZero() && r.Started.Before(m.Started)) {
				m.Started = r.Started
			}
			if r.LastSeen.After(m.LastSeen) {
				m.LastSeen = r.LastSeen
			}
			continue next
		}
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.User != b.User {
			return a.User < b.User
		}
		if rank(a) != rank(b) {
			return rank(a) < rank(b)
		}
		return a.Task < b.Task
	})
	return out
}

func canonical(p string) string {
	if real, err := filepath.EvalSymlinks(p); err == nil {
		p = real
	}
	return filepath.Clean(p)
}
