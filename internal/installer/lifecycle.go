package installer

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/guard"
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
	// GuardHooks are the Git hooks of the authorship guard that GabitoTL installed
	// in the project (found by their ownership marker). Only whole-installation
	// uninstalls (no runtime filter) include them.
	GuardHooks []GuardHook
}

// GuardHook is one Git hook installed by GabitoTL. Chained means a hook of the
// user was moved aside when it was installed and goes back in place byte for
// byte; without it the slot is simply left empty.
type GuardHook struct {
	Name    string
	Path    string
	Chained bool
}

// Empty reports whether there is nothing to remove.
func (p UninstallPlan) Empty() bool { return len(p.Runtimes) == 0 && len(p.GuardHooks) == 0 }

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
	if len(only) == 0 {
		p.GuardHooks = guardHooksOf(env)
	}
	for _, only := range only {
		if _, ok := env.Runtimes.Get(only); !ok {
			return p, fmt.Errorf("%w %q (known: %s)", ErrUnknownRuntime, only, strings.Join(env.Runtimes.Names(), ", "))
		}
	}
	return p, nil
}

// guardHooksOf lists the guard hooks GabitoTL installed in the project's hooks
// directory. It only reads (one `git rev-parse` query); anything that is not ours
// is never listed.
func guardHooksOf(env Env) []GuardHook {
	if env.ProjectRoot == "" || env.Runner == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	dir, err := guard.HooksDir(ctx, gitRunner{env.Runner}, env.ProjectRoot)
	if err != nil {
		return nil
	}
	var out []GuardHook
	for _, h := range guard.HookStatus(dir) {
		if h.Installed {
			out = append(out, GuardHook{Name: h.Name, Path: h.Path, Chained: h.Chained})
		}
	}
	return out
}

// UninstallResult reports an uninstall.
type UninstallResult struct {
	// GuardRemoved are the guard hooks taken out; GuardRestored the user hooks that
	// had been chained and are back in place, untouched.
	GuardRemoved, GuardRestored []string
	Removed                     map[string][]string
	Manual                      []string
	Backup                      string
	Warnings                    []string
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
	if pl.Empty() {
		return res, nil
	}
	if len(pl.Runtimes) > 0 {
		if err := removeRuntimeAssets(env, only, pl, &res, obs); err != nil {
			return res, err
		}
	}
	return res, uninstallGuardHooks(env, pl, &res)
}

// uninstallGuardHooks removes the guard hooks GabitoTL installed, restoring any
// user hook that was chained (guard.UninstallHooks: marker-owned files only), and
// forgets the remembered "installed" answer so a later install offers them again.
func uninstallGuardHooks(env Env, pl UninstallPlan, res *UninstallResult) error {
	if len(pl.GuardHooks) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	dir, err := guard.HooksDir(ctx, gitRunner{env.Runner}, env.ProjectRoot)
	if err != nil {
		return fmt.Errorf("git hooks: %w", err)
	}
	if _, err := guard.UninstallHooks(dir); err != nil {
		return fmt.Errorf("git hooks: %w", err)
	}
	for _, h := range pl.GuardHooks {
		res.GuardRemoved = append(res.GuardRemoved, h.Path)
		if h.Chained {
			res.GuardRestored = append(res.GuardRestored, h.Path)
		}
	}
	l := gabitotl.NewLayout(env.ProjectRoot)
	if _, err := os.Stat(l.Workflow()); err != nil {
		return nil // no workflow.md: nothing remembered, and none is created here
	}
	if wf, err := gabitotl.LoadWorkflow(l); err == nil && wf.GuardHooks == gabitotl.HooksInstalled {
		wf.GuardHooks = gabitotl.HooksAsk
		if err := gabitotl.SaveWorkflow(l, wf); err != nil {
			res.Warnings = append(res.Warnings, "workflow.md: "+err.Error())
		}
	}
	return nil
}

// removeRuntimeAssets backs up and removes what GabitoTL wrote in the runtimes.
func removeRuntimeAssets(env Env, only []string, pl UninstallPlan, res *UninstallResult, obs Observer) error {
	if err := EnsureGlobalLayout(env); err != nil {
		return err
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
				return fmt.Errorf("backup before uninstall: %w", err) // nothing was removed yet
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
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return nil
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
