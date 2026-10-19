package runtime

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// stubAdapter embeds the interface so only the methods under test exist.
type stubAdapter struct {
	Adapter
	backupErr error
	applied   bool
}

func (s *stubAdapter) Name() string { return "stub" }
func (s *stubAdapter) Backup(_ Env, snap *Snapshot, _ []Action) error {
	snap.Files = append(snap.Files, "a")
	return s.backupErr
}
func (s *stubAdapter) Apply(Env, []Action) (Result, error) {
	s.applied = true
	return Result{}, nil
}

func TestBackupAndApplyBacksUpFirst(t *testing.T) {
	env := Env{StateDir: t.TempDir(), Now: func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) }}
	a := &stubAdapter{}
	snap, _, err := BackupAndApply(env, a, nil)
	if err != nil || !a.applied {
		t.Fatalf("err=%v applied=%v", err, a.applied)
	}
	if want := filepath.Join(env.StateDir, "backups", "20260102-030405", "stub"); snap.Dir != want {
		t.Fatalf("snapshot dir %q, want %q", snap.Dir, want)
	}
}

func TestBackupAndApplyStopsWhenTheBackupFails(t *testing.T) {
	a := &stubAdapter{backupErr: errors.New("disk full")}
	_, _, err := BackupAndApply(Env{StateDir: t.TempDir()}, a, nil)
	var be *BackupError
	if !errors.As(err, &be) || a.applied {
		t.Fatalf("want a BackupError and no apply, got err=%v applied=%v", err, a.applied)
	}
}
