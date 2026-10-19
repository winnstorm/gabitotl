package runtime

import "path/filepath"

// BackupError means the pre-change backup failed, so nothing was changed.
type BackupError struct{ Err error }

func (e *BackupError) Error() string { return e.Err.Error() }
func (e *BackupError) Unwrap() error { return e.Err }

// BackupAndApply copies the files plan touches into a timestamped snapshot under
// <state>/backups/<time>/<runtime> and only then applies plan. A failed backup
// returns a *BackupError before anything is written. The snapshot is returned in
// every case so callers can report where the copies live.
func BackupAndApply(env Env, a Adapter, plan []Action) (Snapshot, Result, error) {
	snap := Snapshot{Dir: filepath.Join(env.StateDir, "backups", env.Clock().Format("20060102-150405"), a.Name())}
	if err := a.Backup(env, &snap, plan); err != nil {
		return snap, Result{}, &BackupError{Err: err}
	}
	res, err := a.Apply(env, plan)
	return snap, res, err
}
