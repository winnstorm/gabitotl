package cli

import (
	"flag"
	"fmt"

	"github.com/winnstorm/gabitotl/internal/coordination"
	"github.com/winnstorm/gabitotl/internal/gabitotl"
)

// runSession lets any agent session register its presence, whatever memory
// provider the project uses: `session start` from a SessionStart hook or a
// shell, `session touch` to refresh it, `session end` to leave. All three are
// advisory: outside a GabitoTL project they do nothing, and a directory that
// cannot be written is a warning. They never fail the caller.
func runSession(env Env, args []string) int {
	return subcommands(env, "session", args, map[string]func(Env, []string) int{
		"start": runSessionStart,
		"touch": runSessionTouch,
		"end":   runSessionEnd,
	})
}

// sessionTarget resolves what a session command acts on. ok is false outside an
// initialized project or without an identity: the command then succeeds silently.
type sessionTarget struct {
	layout gabitotl.Layout
	user   string
	pid    int
}

func resolveSession(env Env, root string, anchor int) (t sessionTarget, ok bool) {
	dir, err := resolveRoot(env, root)
	if err != nil || !gabitotl.Exists(dir) {
		return t, false
	}
	user := activeID(dir)
	if user == "" {
		return t, false
	}
	if anchor <= 0 {
		anchor = anchorPIDFn(false)
	}
	return sessionTarget{layout: layoutOf(dir), user: user, pid: anchor}, true
}

func sessionFlags(name string, env Env) (*flag.FlagSet, *string, *int) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	root := fs.String("root", "", "project root")
	anchor := fs.Int("anchor-pid", 0, "PID whose life ends this session (default: the parent process)")
	return fs, root, anchor
}

func runSessionStart(env Env, args []string) int {
	fs, root, anchor := sessionFlags("session start", env)
	task := fs.String("task", "", "task id")
	branch := fs.String("branch", "", "task branch")
	areas := fs.String("areas", "", "comma-separated areas (modules or paths) you touch")
	rt := fs.String("runtime", "", "agent runtime name (informational)")
	if err := fs.Parse(args); err != nil {
		return flagExit(err)
	}
	t, ok := resolveSession(env, *root, *anchor)
	if !ok {
		return ExitOK
	}
	now := nowFn()
	s := coordination.Session{User: t.user, PID: t.pid, Task: *task, Branch: *branch, Areas: splitList(*areas), Runtime: *rt}
	if err := coordination.Register(t.layout, s, now); err != nil {
		fmt.Fprintf(env.Stderr, "warning: could not record this session in %s (%v); others will not see you here\n", t.layout.Local(), err)
	}
	// stdout, so a hook can hand it to the agent as context; nothing else is printed.
	if sessions, err := coordination.Sessions(t.layout, t.user, now); err == nil {
		for _, line := range coordination.Notices(sessions, now) {
			fmt.Fprintf(env.Stdout, "note: %s\n", line)
		}
	}
	return ExitOK
}

func runSessionTouch(env Env, args []string) int {
	fs, root, anchor := sessionFlags("session touch", env)
	if err := fs.Parse(args); err != nil {
		return flagExit(err)
	}
	if t, ok := resolveSession(env, *root, *anchor); ok {
		// A session that was never started (or already ended) is not created here.
		if _, err := coordination.Touch(t.layout, t.user, t.pid, nowFn()); err != nil {
			fmt.Fprintf(env.Stderr, "warning: could not refresh this session: %v\n", err)
		}
	}
	return ExitOK
}

func runSessionEnd(env Env, args []string) int {
	fs, root, anchor := sessionFlags("session end", env)
	if err := fs.Parse(args); err != nil {
		return flagExit(err)
	}
	if t, ok := resolveSession(env, *root, *anchor); ok {
		if err := coordination.Unregister(t.layout, t.user, t.pid); err != nil {
			fmt.Fprintf(env.Stderr, "warning: could not remove this session: %v\n", err)
		}
	}
	return ExitOK
}
