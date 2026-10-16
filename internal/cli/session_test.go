package cli

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/winnstorm/gabitotl/internal/coordination"
)

func mySession(t *testing.T, root string) (coordination.Session, bool) {
	t.Helper()
	got, err := coordination.Sessions(layoutOf(root), "bob", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range got {
		if s.User == "gabi" {
			return s, true
		}
	}
	return coordination.Session{}, false
}

func TestSessionLifecycle(t *testing.T) {
	root, layout := presenceProject(t)
	r := runIn(t, "", false, root, "session", "start", "--task", "auth", "--branch", "feature/auth", "--areas", "api,web", "--runtime", "claude-code")
	if r.code != 0 || r.err != "" {
		t.Fatalf("%+v", r)
	}
	if r.out != "note: bob is also active in this directory since 12 min ago (task pay)\n" {
		t.Fatalf("stdout must carry only the advisory notice: %q", r.out)
	}
	s, ok := mySession(t, root)
	if !ok || s.PID != os.Getppid() || s.Task != "auth" || s.Branch != "feature/auth" || strings.Join(s.Areas, ",") != "api,web" || s.Runtime != "claude-code" {
		t.Fatalf("session = %+v ok=%v", s, ok)
	}

	// Idempotent: the start time is kept and a bare start keeps the details.
	first := s.Started
	nowFn = func() time.Time { return first.Add(30 * time.Minute) }
	t.Cleanup(func() { nowFn = time.Now })
	if r := runIn(t, "", false, root, "session", "start"); r.code != 0 {
		t.Fatalf("%+v", r)
	}
	s2, _ := mySession(t, root)
	if !s2.Started.Equal(first) || s2.Task != "auth" || !s2.LastSeen.After(s.LastSeen) {
		t.Fatalf("restart = %+v (was %+v)", s2, s)
	}

	// Touch refreshes silently.
	nowFn = func() time.Time { return first.Add(time.Hour) }
	r = runIn(t, "", false, root, "session", "touch")
	s3, _ := mySession(t, root)
	if r.code != 0 || r.out != "" || r.err != "" || !s3.LastSeen.Equal(first.Add(time.Hour).UTC()) {
		t.Fatalf("touch: %+v %+v", r, s3)
	}

	// End removes it (and only it).
	if r := runIn(t, "", false, root, "session", "end"); r.code != 0 || r.out != "" || r.err != "" {
		t.Fatalf("%+v", r)
	}
	if _, ok := mySession(t, root); ok {
		t.Fatal("session must be gone")
	}
	if got, _ := coordination.Sessions(layout, "gabi", time.Now()); len(got) != 1 || got[0].User != "bob" {
		t.Fatalf("bob must remain: %+v", got)
	}

	// Touch never creates; ending twice is fine.
	for _, sub := range []string{"touch", "end"} {
		if r := runIn(t, "", false, root, "session", sub); r.code != 0 || r.err != "" {
			t.Fatalf("%s: %+v", sub, r)
		}
	}
	if _, ok := mySession(t, root); ok {
		t.Fatal("touch must not create a session")
	}
}

func TestSessionAnchorPIDFlag(t *testing.T) {
	root, layout := presenceProject(t)
	if r := runIn(t, "", false, root, "session", "start", "--anchor-pid", strconv.Itoa(os.Getpid())); r.code != 0 {
		t.Fatalf("%+v", r)
	}
	if _, err := os.Stat(filepath.Join(layout.Local(), coordination.SessionsDir, "gabi-"+strconv.Itoa(os.Getpid())+".yaml")); err != nil {
		t.Fatal(err)
	}
}

func TestSessionOutsideProjectIsSilentNoop(t *testing.T) {
	t.Setenv("GABITOTL_USER", "gabi")
	dir := t.TempDir()
	for _, sub := range []string{"start", "touch", "end"} {
		r := runIn(t, "", false, dir, "session", sub)
		if r.code != 0 || r.out != "" || r.err != "" {
			t.Fatalf("%s: %+v", sub, r)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ".gabitotl")); !os.IsNotExist(err) {
		t.Fatalf("nothing may be created outside a project: %v", err)
	}
}

func TestSessionStartUnwritableDirWarns(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	root, layout := presenceProject(t)
	dir := filepath.Join(layout.Local(), coordination.SessionsDir)
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	r := runIn(t, "", false, root, "session", "start")
	if r.code != 0 || !strings.Contains(r.err, "warning: could not record this session") || !strings.Contains(r.out, "bob is also active") {
		t.Fatalf("never block: %+v", r)
	}
	for _, sub := range []string{"touch", "end"} {
		if r := runIn(t, "", false, root, "session", sub); r.code != 0 {
			t.Fatalf("%s: %+v", sub, r)
		}
	}
}

func TestSessionUsageWithoutSubcommand(t *testing.T) {
	if r := runIn(t, "", false, t.TempDir(), "session"); r.code != ExitUsage || !strings.Contains(r.err, "end|start|touch") {
		t.Fatalf("%+v", r)
	}
}
