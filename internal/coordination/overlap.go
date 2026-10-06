package coordination

import (
	"context"
	"path"
	"sort"
	"strings"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/memory"
)

// Probe describes the work about to start.
type Probe struct {
	User    string
	Task    string
	Project string // memory project key
	Areas   []string
	Files   []string
}

// Finding is one advisory overlap with someone else's work.
type Finding struct {
	Who         string   `json:"who"`
	Task        string   `json:"task,omitempty"`
	Branch      string   `json:"branch,omitempty"`
	Source      string   `json:"source"` // "work" (active-work file) or "memory"
	SharedAreas []string `json:"shared_areas"`
}

// Report is the result of CheckOverlap. It is advisory: it never blocks and
// never fails because memory is unavailable (that becomes a warning).
type Report struct {
	Findings []Finding `json:"findings"`
	Warnings []string  `json:"warnings,omitempty"`
}

func norm(p string) string {
	p = strings.ToLower(strings.TrimSpace(strings.ReplaceAll(p, "\\", "/")))
	p = path.Clean("/" + p)
	return strings.Trim(p, "/")
}

// areasOverlap reports whether two areas are equal or one contains the other
// at a path-segment boundary ("src/auth" contains "src/auth/login.ts").
func areasOverlap(a, b string) bool {
	a, b = norm(a), norm(b)
	if a == "" || b == "" {
		return false
	}
	return a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/")
}

func shared(mine, theirs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range mine {
		for _, t := range theirs {
			if areasOverlap(m, t) && !seen[norm(m)] {
				seen[norm(m)] = true
				out = append(out, m)
			}
		}
	}
	return out
}

// CheckOverlap looks for other developers working on the same areas, in two
// places: the active-work files under .gabitotl/work and, when mem is not nil,
// shared memory. It is a soft lock: findings are advice, Git remains the final
// authority on real conflicts.
func CheckOverlap(ctx context.Context, layout gabitotl.Layout, mem memory.Provider, p Probe) Report {
	var rep Report
	mine := append(append([]string{}, p.Areas...), p.Files...)
	if len(mine) == 0 {
		return rep
	}
	type key struct{ who, task string }
	idx := map[key]int{}
	add := func(f Finding) {
		if f.Task == "" {
			for _, ex := range rep.Findings {
				if ex.Who == f.Who {
					return // already reported for this person
				}
			}
		}
		k := key{f.Who, f.Task}
		if i, ok := idx[k]; ok {
			// Prefer the active-work finding, keep its branch.
			if rep.Findings[i].Branch == "" {
				rep.Findings[i].Branch = f.Branch
			}
			return
		}
		idx[k] = len(rep.Findings)
		rep.Findings = append(rep.Findings, f)
	}

	tasks, err := ListTasks(layout, "")
	if err != nil {
		rep.Warnings = append(rep.Warnings, "active work unreadable: "+err.Error())
	}
	for _, t := range tasks {
		if t.Owner == p.User || !t.Active() {
			continue
		}
		if sh := shared(mine, append(append([]string{}, t.Areas...), t.Files...)); len(sh) > 0 {
			add(Finding{Who: t.Owner, Task: t.ID, Branch: t.Branch, Source: "work", SharedAreas: sh})
		}
	}

	if mem != nil {
		entries, err := mem.Search(ctx, memory.Query{
			Text: strings.Join(p.Areas, " "), Project: p.Project, Limit: 10, MatchAny: true,
		})
		if err != nil {
			rep.Warnings = append(rep.Warnings, "memory search failed: "+err.Error())
		}
		for _, e := range entries {
			if e.Actor == "" || e.Actor == memory.ActorLegacy || e.Actor == p.User {
				continue
			}
			if e.Type != memory.TypeProgress && e.Type != memory.TypePending {
				continue // only ongoing-work records signal concurrent work
			}
			theirs := append([]string{}, e.Files...)
			sh := shared(mine, theirs)
			if len(sh) == 0 {
				text := strings.ToLower(e.Title + " " + e.Content)
				for _, a := range p.Areas {
					if n := norm(a); n != "" && strings.Contains(text, n) {
						sh = append(sh, a)
					}
				}
			}
			if len(sh) > 0 {
				add(Finding{Who: e.Actor, Task: e.Task, Branch: e.Branch, Source: "memory", SharedAreas: sh})
			}
		}
	}
	sort.SliceStable(rep.Findings, func(i, j int) bool {
		a, b := rep.Findings[i], rep.Findings[j]
		if a.Who != b.Who {
			return a.Who < b.Who
		}
		return a.Task < b.Task
	})
	return rep
}
