package backup

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
)

func setup(t *testing.T) (*Manager, string) {
	t.Helper()
	root := t.TempDir()
	m := New(root, gabitotl.BackupPolicy{MaxSnapshots: 2, MaxAgeDays: 7})
	m.Now = func() time.Time { return time.Date(2026, 10, 5, 20, 50, 0, 0, time.UTC) }
	return m, root
}

func put(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestCreateAndManifest(t *testing.T) {
	m, root := setup(t)
	put(t, root, "src/auth/service.ts", "old service")
	put(t, root, "src/auth/controller.ts", "old controller")
	put(t, root, "src/untouched.ts", "untouched")
	res, err := m.Create(Meta{User: "gabi", Task: "auth-login", Branch: "feature/auth-login", BaseCommit: "abc123", Reason: "rework login"},
		[]string{"src/auth/service.ts", "src/auth/controller.ts", "src/new.ts"})
	if err != nil {
		t.Fatal(err)
	}
	wantDir := filepath.Join(root, ".gabitotl", ".bak", "gabi", "2026-10-05_2050_auth-login")
	if res.Snapshot.Dir != wantDir {
		t.Fatalf("dir = %s, want %s", res.Snapshot.Dir, wantDir)
	}
	if got := read(t, filepath.Join(wantDir, "src/auth/service.ts")); got != "old service" {
		t.Fatalf("copied content = %q", got)
	}
	if _, err := os.Stat(filepath.Join(wantDir, "src/untouched.ts")); err == nil {
		t.Fatal("untouched file must not be copied")
	}
	if len(res.Skipped) != 1 || res.Skipped[0].Path != "src/new.ts" {
		t.Fatalf("skipped = %+v", res.Skipped)
	}
	man := read(t, filepath.Join(wantDir, ManifestName))
	for _, want := range []string{"user: gabi", "task: auth-login", "branch: feature/auth-login", "base_commit: abc123", "reason: rework login", "src/auth/service.ts", "src/auth/controller.ts"} {
		if !strings.Contains(man, want) {
			t.Errorf("manifest missing %q:\n%s", want, man)
		}
	}
	snaps, err := m.List("gabi")
	if err != nil || len(snaps) != 1 || snaps[0].Manifest.BaseCommit != "abc123" || len(snaps[0].Manifest.Files) != 2 {
		t.Fatalf("list = %+v, %v", snaps, err)
	}
}

func TestCreateRefusals(t *testing.T) {
	m, root := setup(t)
	outside := t.TempDir()
	put(t, outside, "x.txt", "x")
	put(t, root, "ok.txt", "ok")
	for _, bad := range []string{"../x.txt", filepath.Join(outside, "x.txt"), "a/../../x.txt", "/etc/passwd"} {
		if _, err := m.Create(Meta{User: "gabi", Task: "t"}, []string{"ok.txt", bad}); !errors.Is(err, ErrOutsideRoot) {
			t.Errorf("%q: want ErrOutsideRoot, got %v", bad, err)
		}
	}
	if snaps, _ := m.List("gabi"); len(snaps) != 0 {
		t.Fatal("a refused request must not create a snapshot")
	}
	// Symlink escaping the root.
	if err := os.Symlink(filepath.Join(outside, "x.txt"), filepath.Join(root, "link.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Create(Meta{User: "gabi", Task: "t"}, []string{"link.txt"}); !errors.Is(err, ErrOutsideRoot) {
		t.Errorf("symlink escape: want ErrOutsideRoot, got %v", err)
	}
	// Invalid ids.
	if _, err := m.Create(Meta{User: "../evil", Task: "t"}, []string{"ok.txt"}); !errors.Is(err, gabitotl.ErrInvalidID) {
		t.Errorf("want ErrInvalidID, got %v", err)
	}
	if _, err := m.Create(Meta{User: "gabi", Task: "a/b"}, []string{"ok.txt"}); !errors.Is(err, gabitotl.ErrInvalidID) {
		t.Errorf("want ErrInvalidID, got %v", err)
	}
}

func TestSecretsAndBakNeverBackedUp(t *testing.T) {
	m, root := setup(t)
	put(t, root, ".env", "TOKEN=abc")
	put(t, root, "config/.env.local", "X=1")
	put(t, root, "keys/server.pem", "pem")
	put(t, root, ".gabitotl/.bak/gabi/old/file.txt", "bak")
	put(t, root, "real.txt", "real")
	res, err := m.Create(Meta{User: "gabi", Task: "t"}, []string{".env", "config/.env.local", "keys/server.pem", ".gabitotl/.bak/gabi/old/file.txt", "real.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Snapshot.Manifest.Files; len(got) != 1 || got[0] != "real.txt" {
		t.Fatalf("files = %v", got)
	}
	if len(res.Skipped) != 4 {
		t.Fatalf("skipped = %+v", res.Skipped)
	}
	if _, err := m.Create(Meta{User: "gabi", Task: "t2"}, []string{".env"}); !errors.Is(err, ErrNothingToBackup) {
		t.Fatalf("want ErrNothingToBackup, got %v", err)
	}
}

func TestRestoreAndSafetySnapshot(t *testing.T) {
	m, root := setup(t)
	put(t, root, "a.txt", "A0")
	put(t, root, "b.txt", "B0")
	res, err := m.Create(Meta{User: "gabi", Task: "t"}, []string{"a.txt", "b.txt"})
	if err != nil {
		t.Fatal(err)
	}
	put(t, root, "a.txt", "A1")
	put(t, root, "b.txt", "B1")
	got, err := m.Restore(res.Snapshot, "a.txt")
	if err != nil || len(got) != 1 {
		t.Fatalf("restore: %v %v", got, err)
	}
	if read(t, filepath.Join(root, "a.txt")) != "A0" || read(t, filepath.Join(root, "b.txt")) != "B1" {
		t.Fatal("only a.txt should be restored")
	}
	if _, err := m.Restore(res.Snapshot, "zzz.txt"); !errors.Is(err, ErrNotInSnapshot) {
		t.Fatalf("want ErrNotInSnapshot, got %v", err)
	}
	if _, err := m.Restore(res.Snapshot, "../a.txt"); !errors.Is(err, ErrOutsideRoot) {
		t.Fatalf("want ErrOutsideRoot, got %v", err)
	}
	// A restore saved the pre-restore state (A1) as its own snapshot.
	snaps, _ := m.List("gabi")
	var safety *Snapshot
	for i := range snaps {
		if snaps[i].Manifest.Task == "pre-restore" {
			safety = &snaps[i]
		}
	}
	if safety == nil || read(t, filepath.Join(safety.Dir, "a.txt")) != "A1" {
		t.Fatalf("pre-restore snapshot missing: %+v", snaps)
	}
	// Restore everything.
	if _, err := m.Restore(res.Snapshot); err != nil || read(t, filepath.Join(root, "b.txt")) != "B0" {
		t.Fatalf("full restore failed: %v", err)
	}
	if latest, err := m.Find("gabi", "latest"); err != nil || latest.ID == "" {
		t.Fatalf("latest: %v", err)
	}
	if _, err := m.Find("gabi", "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestSameMinuteDoesNotCollide(t *testing.T) {
	m, root := setup(t)
	put(t, root, "a.txt", "A")
	a, _ := m.Create(Meta{User: "gabi", Task: "t"}, []string{"a.txt"})
	b, err := m.Create(Meta{User: "gabi", Task: "t"}, []string{"a.txt"})
	if err != nil || a.Snapshot.ID == b.Snapshot.ID {
		t.Fatalf("ids %q %q err %v", a.Snapshot.ID, b.Snapshot.ID, err)
	}
}

func TestPrune(t *testing.T) {
	m, root := setup(t) // keep newest 2 OR newer than 7 days
	put(t, root, "a.txt", "A")
	base := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	make1 := func(task string, age time.Duration) string {
		m.Now = func() time.Time { return base.Add(-age) }
		r, err := m.Create(Meta{User: "gabi", Task: task}, []string{"a.txt"})
		if err != nil {
			t.Fatal(err)
		}
		return r.Snapshot.ID
	}
	day := 24 * time.Hour
	old1 := make1("old-1", 30*day)
	old2 := make1("old-2", 20*day)
	mid := make1("mid", 10*day)
	recent := make1("recent", 2*day)
	newest := make1("newest", 0)
	m.Now = func() time.Time { return base }
	removed, err := m.Prune("gabi")
	if err != nil {
		t.Fatal(err)
	}
	// Newest two (newest, recent) kept by count; recent also by age; the rest are
	// older than 7 days and beyond the count: removed.
	want := map[string]bool{old1: true, old2: true, mid: true}
	if len(removed) != 3 {
		t.Fatalf("removed = %v", removed)
	}
	for _, id := range removed {
		if !want[id] {
			t.Errorf("unexpected removal %s (recent %s newest %s)", id, recent, newest)
		}
	}
	left, _ := m.List("gabi")
	if len(left) != 2 {
		t.Fatalf("left = %d", len(left))
	}
	// Age keeps even beyond the count.
	m.Policy.MaxSnapshots = 1
	if removed, _ := m.Prune("gabi"); len(removed) != 0 {
		t.Fatalf("age window must keep recent snapshots, removed %v", removed)
	}
}
