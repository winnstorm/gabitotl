package coordination

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
)

// fakeAlive makes only the listed PIDs alive for the duration of the test.
func fakeAlive(t *testing.T, pids ...int) {
	t.Helper()
	saved := pidAlive
	t.Cleanup(func() { pidAlive = saved })
	pidAlive = func(pid int) bool {
		for _, p := range pids {
			if p == pid {
				return true
			}
		}
		return false
	}
}

func TestRegisterIsIdempotentAndKeepsStart(t *testing.T) {
	layout := gabitotl.NewLayout(t.TempDir())
	fakeAlive(t, 100)
	t0 := time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC)
	if err := Register(layout, Session{User: "ana", PID: 100, Task: "auth"}, t0); err != nil {
		t.Fatal(err)
	}
	// A later touch without optional fields keeps them and the start time.
	if err := Register(layout, Session{User: "ana", PID: 100}, t0.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	got, err := Sessions(layout, "ana", t0.Add(6*time.Minute))
	if err != nil || len(got) != 1 {
		t.Fatalf("sessions = %+v, %v", got, err)
	}
	s := got[0]
	if !s.Started.Equal(t0) || !s.LastSeen.Equal(t0.Add(5*time.Minute)) || s.Task != "auth" || !s.Own {
		t.Fatalf("session = %+v", s)
	}
	if _, err := os.Stat(filepath.Join(layout.Local(), SessionsDir, "ana-100.yaml")); err != nil {
		t.Fatal(err)
	}
}

func TestSessionsTwoUsersSameDirectory(t *testing.T) {
	layout := gabitotl.NewLayout(t.TempDir())
	fakeAlive(t, 100, 200)
	now := time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC)
	var wg sync.WaitGroup
	for _, s := range []Session{
		{User: "ana", PID: 100, Task: "auth-refactor", Started: now},
		{User: "bob", PID: 200, Branch: "feature/pay"},
	} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 20; i++ { // concurrent writers, readers and refreshes
				if err := Register(layout, s, now.Add(time.Duration(i)*time.Second)); err != nil {
					t.Error(err)
				}
				if _, err := Sessions(layout, s.User, now); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
	got, err := Sessions(layout, "ana", now.Add(time.Minute))
	if err != nil || len(got) != 2 {
		t.Fatalf("sessions = %+v, %v", got, err)
	}
	others := Others(got)
	if len(others) != 1 || others[0].User != "bob" || others[0].Own {
		t.Fatalf("others = %+v", others)
	}
	lines := Notices(got, now.Add(12*time.Minute))
	if len(lines) != 1 || !strings.Contains(lines[0], "bob is also active in this directory since") || !strings.Contains(lines[0], "branch feature/pay") {
		t.Fatalf("notices = %q", lines)
	}
}

func TestNoticeFormat(t *testing.T) {
	now := time.Date(2026, 10, 10, 9, 12, 0, 0, time.UTC)
	s := Session{User: "ana", Started: now.Add(-12 * time.Minute), Task: "auth-refactor"}
	want := "ana is also active in this directory since 12 min ago (task auth-refactor)"
	if got := Notice(s, now); got != want {
		t.Fatalf("notice = %q, want %q", got, want)
	}
	if got := Notice(Session{User: "bob", Started: now.Add(-3 * time.Hour)}, now); got != "bob is also active in this directory since 3 h ago" {
		t.Fatalf("notice = %q", got)
	}
	if lines := Notices(nil, now); len(lines) != 0 {
		t.Fatalf("no sessions, no notice: %q", lines)
	}
	// One line per person, even with two sessions; own sessions never notify.
	list := []Session{
		{User: "ana", Started: now.Add(-time.Hour), Own: true},
		{User: "bob", Started: now.Add(-time.Hour), Task: "a"},
		{User: "bob", Started: now.Add(-time.Minute)},
	}
	if lines := Notices(list, now); len(lines) != 1 || !strings.Contains(lines[0], "task a") {
		t.Fatalf("notices = %q", lines)
	}
}

func TestStalePIDIsPruned(t *testing.T) {
	layout := gabitotl.NewLayout(t.TempDir())
	now := time.Now()
	fakeAlive(t, 100, 200)
	for _, s := range []Session{{User: "ana", PID: 100}, {User: "bob", PID: 200}} {
		if err := Register(layout, s, now); err != nil {
			t.Fatal(err)
		}
	}
	fakeAlive(t, 100) // bob's anchor died
	got, err := Sessions(layout, "ana", now)
	if err != nil || len(got) != 1 || got[0].User != "ana" {
		t.Fatalf("sessions = %+v, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(layout.Local(), SessionsDir, "bob-200.yaml")); !os.IsNotExist(err) {
		t.Fatalf("stale session file should be pruned: %v", err)
	}
}

func TestExpiredTTLIsPrunedEvenIfPIDLives(t *testing.T) {
	layout := gabitotl.NewLayout(t.TempDir())
	fakeAlive(t, 100)
	t0 := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	if err := Register(layout, Session{User: "bob", PID: 100}, t0); err != nil {
		t.Fatal(err)
	}
	if got, _ := Sessions(layout, "ana", t0.Add(SessionTTL-time.Minute)); len(got) != 1 {
		t.Fatalf("within the TTL the session is live: %+v", got)
	}
	if got, _ := Sessions(layout, "ana", t0.Add(SessionTTL+time.Minute)); len(got) != 0 {
		t.Fatalf("past the TTL the session is stale: %+v", got)
	}
	// Touch keeps a session alive; a missing one is not created.
	if err := Register(layout, Session{User: "bob", PID: 100}, t0); err != nil {
		t.Fatal(err)
	}
	if ok, err := Touch(layout, "bob", 100, t0.Add(SessionTTL)); err != nil || !ok {
		t.Fatalf("touch = %v, %v", ok, err)
	}
	if got, _ := Sessions(layout, "ana", t0.Add(SessionTTL+time.Minute)); len(got) != 1 {
		t.Fatalf("a refreshed session stays live: %+v", got)
	}
	if ok, err := Touch(layout, "bob", 999, t0); err != nil || ok {
		t.Fatalf("touch of a missing session = %v, %v", ok, err)
	}
}

// A hook-style invocation exits right away; the session is anchored to its
// parent, which keeps it alive.
func TestShortLivedRegistrationUsesParentAnchor(t *testing.T) {
	if pp := AnchorPID(false); pp != os.Getppid() && pp != os.Getpid() {
		t.Fatalf("short-lived anchor = %d, want the parent (%d) or self", pp, os.Getppid())
	}
	if AnchorPID(true) != os.Getpid() {
		t.Fatal("long-lived anchor must be the process itself")
	}
	layout := gabitotl.NewLayout(t.TempDir())
	const parent = 4242 // alive shell; the registering process is long gone
	fakeAlive(t, parent)
	now := time.Now()
	if err := Register(layout, Session{User: "ana", PID: parent, Task: "x"}, now); err != nil {
		t.Fatal(err)
	}
	if got, _ := Sessions(layout, "bob", now); len(got) != 1 || got[0].Own {
		t.Fatalf("sessions = %+v", got)
	}
	fakeAlive(t) // the shell exits
	if got, _ := Sessions(layout, "bob", now); len(got) != 0 {
		t.Fatalf("session must end with its anchor: %+v", got)
	}
}

func TestUnregisterAndInvalidInput(t *testing.T) {
	layout := gabitotl.NewLayout(t.TempDir())
	now := time.Now()
	if err := Register(layout, Session{User: "../x", PID: 1}, now); err == nil {
		t.Fatal("invalid user must be rejected")
	}
	if err := Register(layout, Session{User: "ana"}, now); err == nil {
		t.Fatal("missing PID must be rejected")
	}
	if err := Register(layout, Session{User: "ana", PID: 100}, now); err != nil {
		t.Fatal(err)
	}
	if err := Unregister(layout, "ana", 100); err != nil {
		t.Fatal(err)
	}
	if err := Unregister(layout, "ana", 100); err != nil {
		t.Fatalf("unregister twice: %v", err)
	}
	if got, err := Sessions(layout, "ana", now); err != nil || len(got) != 0 {
		t.Fatalf("sessions = %+v, %v", got, err)
	}
	// Garbage files are ignored, not fatal.
	dir := filepath.Join(layout.Local(), SessionsDir)
	_ = os.WriteFile(filepath.Join(dir, "junk-1.yaml"), []byte("not yaml: ["), 0o664)
	if got, err := Sessions(layout, "ana", now); err != nil || len(got) != 0 {
		t.Fatalf("sessions with junk = %+v, %v", got, err)
	}
}

func TestUnwritableDirectoryIsAnErrorNotAPanic(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	root := t.TempDir()
	layout := gabitotl.NewLayout(root)
	local := layout.Local()
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(local, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(local, 0o755) })
	if err := Register(layout, Session{User: "ana", PID: 100}, time.Now()); err == nil {
		t.Fatal("registering in a read-only directory must report an error for the caller to downgrade")
	}
	if got, err := Sessions(layout, "ana", time.Now()); err != nil || len(got) != 0 {
		t.Fatalf("reading still works: %+v, %v", got, err)
	}
}
