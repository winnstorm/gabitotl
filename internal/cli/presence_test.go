package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/winnstorm/gabitotl/internal/coordination"
	"github.com/winnstorm/gabitotl/internal/gabitotl"
)

// presenceProject is a project where gabi is the active identity and bob has a
// live session in the same directory (his anchor is this test process).
func presenceProject(t *testing.T) (root string, layout gabitotl.Layout) {
	t.Helper()
	t.Setenv("GABITOTL_USER", "gabi")
	root = initProject(t)
	layout = gabitotl.NewLayout(root)
	bob := coordination.Session{User: "bob", PID: os.Getpid(), Task: "pay"}
	if err := coordination.Register(layout, bob, time.Now().Add(-12*time.Minute)); err != nil {
		t.Fatal(err)
	}
	old := anchorPIDFn
	anchorPIDFn = func(bool) int { return os.Getppid() }
	t.Cleanup(func() { anchorPIDFn = old })
	return root, layout
}

func TestWorkStatusShowsOthersWithoutBlocking(t *testing.T) {
	root, _ := presenceProject(t)
	r := runIn(t, "", false, root, "work", "status")
	if r.code != 0 || !strings.Contains(r.out, "bob is also active in this directory") || !strings.Contains(r.out, "(task pay)") {
		t.Fatalf("%+v", r)
	}
	if !strings.Contains(r.out, "worktree add") || !strings.Contains(r.out, "optional") {
		t.Fatalf("worktree is an optional tip: %+v", r)
	}
	if _, err := os.Stat(filepath.Join(root, ".gabitotl", "local", "workdir.lock")); !os.IsNotExist(err) {
		t.Fatalf("the exclusive lock is gone: %v", err)
	}
}

func TestWorkStartRegistersAndNotifiesOnStderr(t *testing.T) {
	root, layout := presenceProject(t)
	r := runIn(t, "", false, root, "work", "start", "--task", "auth", "--areas", "api", "--branch", "feature/auth")
	if r.code != 0 {
		t.Fatalf("a second person must never be blocked: %+v", r)
	}
	if !strings.Contains(r.err, "note: bob is also active in this directory") || strings.Contains(r.out, "also active") {
		t.Fatalf("notice belongs on stderr only: %+v", r)
	}
	got, err := coordination.Sessions(layout, "bob", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var mine *coordination.Session
	for i := range got {
		if got[i].User == "gabi" {
			mine = &got[i]
		}
	}
	if mine == nil || mine.Task != "auth" || mine.Branch != "feature/auth" || mine.PID != os.Getppid() {
		t.Fatalf("gabi's session = %+v (all: %+v)", mine, got)
	}
}

func TestWorkStartUnwritableSessionsDegradesToWarning(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	root, layout := presenceProject(t)
	dir := filepath.Join(layout.Local(), coordination.SessionsDir)
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	r := runIn(t, "", false, root, "work", "start", "--task", "auth", "--areas", "api")
	if r.code != 0 || !strings.Contains(r.out, "gabi/auth [in-progress]") {
		t.Fatalf("work must go on: %+v", r)
	}
	if !strings.Contains(r.err, "warning: could not record this session") {
		t.Fatalf("want a warning: %+v", r)
	}
}

func TestJoinSessionLongLivedUnregistersOnExit(t *testing.T) {
	root, layout := presenceProject(t)
	anchorPIDFn = func(bool) int { return os.Getpid() }
	var out, errb strings.Builder
	env := Env{Stdout: &out, Stderr: &errb}
	stop := joinSession(env, root, presence{longLived: true, runtime: "memory-stdio"})
	if out.Len() != 0 || !strings.Contains(errb.String(), "note: bob is also active") {
		t.Fatalf("stdout must stay clean (it is the MCP protocol): out=%q err=%q", out.String(), errb.String())
	}
	if got, _ := coordination.Sessions(layout, "gabi", time.Now()); len(got) != 2 {
		t.Fatalf("sessions = %+v", got)
	}
	stop()
	if got, _ := coordination.Sessions(layout, "gabi", time.Now()); len(got) != 1 || got[0].User != "bob" {
		t.Fatalf("own session must be gone: %+v", got)
	}
	// Outside a project nothing happens.
	joinSession(env, t.TempDir(), presence{longLived: true})()
}
