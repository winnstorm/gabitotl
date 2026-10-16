package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/winnstorm/gabitotl/internal/coordination"
	"github.com/winnstorm/gabitotl/internal/gabitotl"
)

// Hooks for tests.
var (
	anchorPIDFn = coordination.AnchorPID
	nowFn       = time.Now
)

// presence describes the session an entry point registers.
type presence struct {
	// longLived entry points (the stdio memory servers an agent runtime spawns)
	// stay up for the whole agent session, so their own PID is the anchor and
	// they refresh it and unregister on exit. Short-lived ones are anchored to
	// their parent, see coordination.AnchorPID.
	longLived bool
	task      string
	branch    string
	areas     []string
	runtime   string
}

// joinSession makes this invocation visible to the other people using the same
// working directory and prints one advisory line per person who is already
// here to stderr (never stdout: that belongs to command output and to the MCP
// protocol). Presence is advisory: every failure is at most a warning and the
// command goes on. The returned func ends the session; call it when done.
func joinSession(env Env, root string, p presence) func() {
	noop := func() {}
	if !gabitotl.Exists(root) {
		return noop
	}
	user := activeID(root)
	if user == "" {
		return noop
	}
	layout := layoutOf(root)
	pid := anchorPIDFn(p.longLived)
	now := nowFn()
	s := coordination.Session{User: user, PID: pid, Task: p.task, Branch: p.branch, Areas: p.areas, Runtime: p.runtime}
	if err := coordination.Register(layout, s, now); err != nil {
		fmt.Fprintf(env.Stderr, "warning: could not record this session in %s (%v); others will not see you here\n", layout.Local(), err)
	}
	printNotices(env, layout, user, now)
	if !p.longLived {
		return noop // the anchor outlives this process; TTL and PID liveness retire it
	}
	ctx, cancel := context.WithCancel(context.Background())
	go coordination.Heartbeat(ctx, layout, user, pid, coordination.HeartbeatInterval)
	return func() {
		cancel()
		_ = coordination.Unregister(layout, user, pid)
	}
}

// printNotices writes the advisory lines about other people's live sessions.
func printNotices(env Env, layout gabitotl.Layout, user string, now time.Time) {
	sessions, err := coordination.Sessions(layout, user, now)
	if err != nil {
		return
	}
	for _, line := range coordination.Notices(sessions, now) {
		fmt.Fprintf(env.Stderr, "note: %s\n", line)
	}
}
