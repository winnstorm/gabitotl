package installer

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
)

// DefaultRetentionDays is how long legacy snapshots are kept (spec 25).
const DefaultRetentionDays = 30

// RestoreChange is one file a restore would change.
type RestoreChange struct {
	Path   string
	Action string // "restore" (write the saved copy) or "remove" (it did not exist before)
}

// PlanRestore lists what restoring snapshot id would change. It writes nothing.
// Project (.gabitotl) files are never part of a restore: they are committed
// work, and Engram is never part of it either.
func PlanRestore(env Env, id string) (*Snapshot, []RestoreChange, error) {
	s, err := LoadSnapshot(env, id)
	if err != nil {
		return nil, nil, err
	}
	var out []RestoreChange
	seen := map[string]bool{}
	files := append(append([]ManifestFile(nil), s.Manifest.Files...), s.Manifest.OwnershipState)
	for _, mf := range files {
		if seen[mf.Path] || mf.Runtime == ProjectRuntime {
			continue
		}
		seen[mf.Path] = true
		cur, err := os.ReadFile(mf.Path)
		exists := err == nil
		switch {
		case !mf.Existed && exists:
			out = append(out, RestoreChange{mf.Path, "remove"})
		case mf.Existed:
			want, werr := s.copyOf(mf)
			if werr == nil && (!exists || string(cur) != string(want)) {
				out = append(out, RestoreChange{mf.Path, "restore"})
			}
		}
	}
	return s, out, nil
}

// Restore puts the snapshot's files back. Before overwriting anything it saves
// the current version of every file it is about to change into
// ~/.gabitotl/backups/<timestamp>_pre-restore/, so a restore is itself
// reversible.
func Restore(env Env, id string, obs Observer) (changed []string, backup string, err error) {
	s, plan, err := PlanRestore(env, id)
	if err != nil {
		return nil, "", err
	}
	if len(plan) == 0 {
		return nil, "", nil
	}
	if err := EnsureGlobalLayout(env); err != nil {
		return nil, "", err
	}
	backup = filepath.Join(env.StateDir(), "backups", env.Clock().Format("2006-01-02_1504")+"_pre-restore")
	for n := 2; ; n++ {
		if _, err := os.Stat(backup); errors.Is(err, fs.ErrNotExist) {
			break
		}
		backup = fmt.Sprintf("%s-%d", filepath.Join(env.StateDir(), "backups", env.Clock().Format("2006-01-02_1504")+"_pre-restore"), n)
	}
	for _, c := range plan {
		if _, _, err := copyFile(c.Path, filepath.Join(backup, "files", strings.TrimLeft(strings.NewReplacer(":", "_", `\`, "/").Replace(c.Path), "/"))); err != nil {
			return nil, backup, err
		}
	}
	obs.emit(PhaseReport, "ok", "pre-restore backup", backup)
	changed, err = s.RestoreFiles(false)
	return changed, backup, err
}

// UninstallPlan says what `gabitotl uninstall` removes and what it keeps.
type UninstallPlan struct {
	Runtimes  []string // runtimes with GabitoTL assets in the ownership state
	Files     map[string][]string
	Preserved []string // reason codes, see UninstallPreserved
}

// UninstallPreserved are the things uninstall never deletes (spec 46).
var UninstallPreserved = []string{"engram", "memory", "code", "project", "backups"}

// PlanUninstall reads the ownership state; it writes nothing. only restricts
// the runtimes.
func PlanUninstall(env Env, only []string) (UninstallPlan, error) {
	st, err := env.RT().Store().Load()
	if err != nil {
		return UninstallPlan{}, err
	}
	p := UninstallPlan{Files: map[string][]string{}, Preserved: UninstallPreserved}
	for _, a := range env.Runtimes.All() {
		if len(only) > 0 && !slices.Contains(only, a.Name()) {
			continue
		}
		for _, e := range st.ForRuntime(a.Name()) {
			p.Files[a.Name()] = append(p.Files[a.Name()], e.Path)
		}
		if len(p.Files[a.Name()]) > 0 {
			p.Runtimes = append(p.Runtimes, a.Name())
		}
	}
	for _, only := range only {
		if _, ok := env.Runtimes.Get(only); !ok {
			return p, fmt.Errorf("%w %q (known: %s)", ErrUnknownRuntime, only, strings.Join(env.Runtimes.Names(), ", "))
		}
	}
	return p, nil
}

// UninstallResult reports an uninstall.
type UninstallResult struct {
	Removed  map[string][]string
	Manual   []string
	Backup   string
	Warnings []string
}

// Uninstall removes only what GabitoTL wrote, driven by the ownership state
// (adapters' RemoveGabitoConfig). The touched files are copied to
// ~/.gabitotl/backups/<timestamp>_uninstall/ first. It never deletes memory, the
// project's .gabitotl folder, backups or any content the user wrote.
func Uninstall(env Env, only []string, obs Observer) (UninstallResult, error) {
	res := UninstallResult{Removed: map[string][]string{}}
	pl, err := PlanUninstall(env, only)
	if err != nil {
		return res, err
	}
	if len(pl.Runtimes) == 0 {
		return res, nil
	}
	if err := EnsureGlobalLayout(env); err != nil {
		return res, err
	}
	res.Backup = filepath.Join(env.StateDir(), "backups", env.Clock().Format("2006-01-02_1504")+"_uninstall")
	for n := 2; ; n++ {
		if _, err := os.Stat(res.Backup); errors.Is(err, fs.ErrNotExist) {
			break
		}
		res.Backup = fmt.Sprintf("%s-%d", filepath.Join(env.StateDir(), "backups", env.Clock().Format("2006-01-02_1504")+"_uninstall"), n)
	}
	for _, name := range pl.Runtimes {
		for _, p := range pl.Files[name] {
			dst := filepath.Join(res.Backup, name, "files", strings.TrimLeft(strings.NewReplacer(":", "_", `\`, "/").Replace(p), "/"))
			if _, _, err := copyFile(p, dst); err != nil {
				return res, fmt.Errorf("backup before uninstall: %w", err) // nothing was removed yet
			}
		}
	}
	obs.emit(PhaseReport, "ok", "uninstall backup", res.Backup)
	for _, name := range pl.Runtimes {
		a, _ := env.Runtimes.Get(name)
		r, err := a.RemoveGabitoConfig(env.RT())
		for _, x := range r.Removed {
			res.Removed[name] = append(res.Removed[name], x.Path)
		}
		for _, x := range r.Manual {
			res.Manual = append(res.Manual, name+": "+x.Path)
		}
		if err != nil {
			return res, fmt.Errorf("%s: %w", name, err)
		}
	}
	return res, nil
}

// Expired is a cleanup candidate.
type Expired struct {
	Kind    string // snapshot | backup | legacy
	Path    string
	AgeDays int
}

// PlanCleanup lists legacy material older than days: completed migration
// snapshots, global backups and ~/.gabitotl/legacy entries. It never lists
// ~/.gentle-ai/backups (Gentle-AI's own, never deleted here), rolled-back or
// partial snapshots (still useful), Engram, or anything outside ~/.gabitotl.
func PlanCleanup(env Env, days int) ([]Expired, error) {
	if days < 1 {
		days = DefaultRetentionDays
	}
	now := env.Clock()
	age := func(t time.Time) int { return int(now.Sub(t).Hours() / 24) }
	var out []Expired
	snaps, err := ListSnapshots(env)
	if err != nil {
		return nil, err
	}
	for _, s := range snaps {
		t, err := time.Parse("2006-01-02T15:04:05Z07:00", s.Manifest.CreatedAt)
		if err != nil || s.Manifest.Status != "complete" {
			continue
		}
		if a := age(t); a >= days {
			out = append(out, Expired{"snapshot", s.Dir, a})
		}
	}
	for _, kind := range []struct{ dir, name string }{{"backups", "backup"}, {"legacy", "legacy"}} {
		es, err := os.ReadDir(filepath.Join(env.StateDir(), kind.dir))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, e := range es {
			info, err := e.Info()
			if err != nil {
				continue
			}
			if a := age(info.ModTime()); a >= days {
				out = append(out, Expired{kind.name, filepath.Join(env.StateDir(), kind.dir, e.Name()), a})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// ApplyCleanup deletes the given candidates after the caller confirmed. Every
// path must sit directly inside ~/.gabitotl/{migrations,backups,legacy}; anything
// else is refused.
func ApplyCleanup(env Env, items []Expired) (removed []string, err error) {
	for _, it := range items {
		dir := filepath.Dir(it.Path)
		ok := false
		for _, sub := range []string{"migrations", "backups", "legacy"} {
			ok = ok || dir == filepath.Join(env.StateDir(), sub)
		}
		if !ok || filepath.Base(it.Path) == "." || filepath.Base(it.Path) == ".." {
			return removed, fmt.Errorf("refusing to delete %s: outside the GabitoTL legacy folders", it.Path)
		}
		if err := os.RemoveAll(it.Path); err != nil {
			return removed, err
		}
		removed = append(removed, it.Path)
	}
	return removed, nil
}
