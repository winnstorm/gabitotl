package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/winnstorm/gabitotl/internal/coordination"
	"github.com/winnstorm/gabitotl/internal/memory"
	"github.com/winnstorm/gabitotl/internal/memory/mcp"
)

// whoWindow is how far back shared-memory entries still count as active work.
const whoWindow = 24 * time.Hour

// whoJSON is the stable shape of `who --json`.
type whoJSON struct {
	Self     string                  `json:"self,omitempty"`
	People   []coordination.Presence `json:"people"`
	Warnings []string                `json:"warnings,omitempty"`
}

// activeLister is implemented by the memory-mcp provider only; Engram has no
// shared active-work query, so it is skipped silently.
type activeLister interface {
	ActiveWork(ctx context.Context, q mcp.ActiveQuery) ([]memory.Entry, error)
}

func runWho(env Env, args []string) int {
	fs := flag.NewFlagSet("who", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	root := fs.String("root", "", "project root")
	noMemory := fs.Bool("no-memory", false, "skip shared memory")
	noWorktrees := fs.Bool("no-worktrees", false, "skip other worktrees")
	asJSON := fs.Bool("json", false, "print as JSON")
	if err := fs.Parse(args); err != nil {
		return flagExit(err)
	}
	dir, err := resolveRoot(env, *root)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl who: %v\n", err)
		return ExitError
	}
	self := activeID(dir)
	now := nowFn()

	var src coordination.WhoSources
	var setup []string
	if !*noWorktrees {
		if repo, err := openRepo(env, dir); err != nil {
			setup = append(setup, "worktrees unavailable: "+err.Error())
		} else {
			wt := coordination.NewWorktrees(repo)
			src.Worktrees = wt.List
		}
	}
	if !*noMemory {
		if p, cfg, err := openMemory(env, dir); err != nil {
			setup = append(setup, "memory unavailable: "+err.Error())
		} else if l, ok := p.(activeLister); ok {
			src.Remote = func(ctx context.Context) ([]memory.Entry, error) {
				return l.ActiveWork(ctx, mcp.ActiveQuery{Project: cfg.ProjectKey, SinceMinutes: int(whoWindow / time.Minute), Limit: 20})
			}
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), memoryTimeout)
	defer cancel()
	rep := coordination.Who(ctx, layoutOf(dir), self, now, src)
	rep.Warnings = append(setup, rep.Warnings...)

	if *asJSON {
		people := rep.People
		if people == nil {
			people = []coordination.Presence{}
		}
		enc := json.NewEncoder(env.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(whoJSON{Self: self, People: people, Warnings: rep.Warnings})
		return ExitOK
	}
	printWho(env, rep.People, now)
	for _, w := range rep.Warnings {
		fmt.Fprintf(env.Stderr, "warning: %s\n", w)
	}
	return ExitOK // advisory: never fails because a source is unavailable
}

func printWho(env Env, people []coordination.Presence, now time.Time) {
	if len(people) == 0 {
		fmt.Fprintln(env.Stdout, "no active sessions or work found")
		return
	}
	tw := tabwriter.NewWriter(env.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "USER\tWHERE\tSOURCE\tTASK\tBRANCH\tSINCE\tAREAS")
	for _, p := range people {
		user := p.User
		if p.Own {
			user += " (you)"
		}
		where := p.Scope
		if p.Worktree != "" {
			where += " " + p.Worktree
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", user, where, p.Source, dash(p.Task), dash(p.Branch),
			whoSince(p, now), dash(strings.Join(p.Areas, ",")))
	}
	_ = tw.Flush()
}

// whoSince renders when the person started, or was last seen when no start is
// known (shared memory), or "-".
func whoSince(p coordination.Presence, now time.Time) string {
	switch {
	case !p.Started.IsZero():
		return coordination.Ago(now.Sub(p.Started))
	case !p.LastSeen.IsZero():
		return "seen " + coordination.Ago(now.Sub(p.LastSeen))
	}
	return "-"
}
