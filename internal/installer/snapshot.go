package installer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/identity"
	rt "github.com/winnstorm/gabitotl/internal/runtime"
)

// Errors of the snapshot phase. Any of them stops the run before a single
// mutation (invariant a).
var (
	ErrMemoryUnhealthy = errors.New("engram doctor reports errors; refusing to start")
	ErrExportFailed    = errors.New("engram export failed; refusing to start without a safety backup")
)

// ManifestVersion is the schema version of manifest.json.
const ManifestVersion = 1

// ManifestFile records one file the run may touch: whether it existed and where
// its pre-change copy lives inside the snapshot.
type ManifestFile struct {
	Path    string `json:"path"`
	Runtime string `json:"runtime"`
	Existed bool   `json:"existed"`
	Copy    string `json:"copy,omitempty"` // relative to the snapshot dir; empty when the file did not exist
	SHA256  string `json:"sha256,omitempty"`
}

// EngramBackup records the safety export.
type EngramBackup struct {
	Done         bool   `json:"done"`
	File         string `json:"file,omitempty"` // relative to the snapshot dir
	Project      string `json:"project,omitempty"`
	Sessions     int    `json:"sessions"`
	Observations int    `json:"observations"`
	Prompts      int    `json:"prompts"`
	Reason       string `json:"reason,omitempty"` // why no export was made
}

// Manifest is manifest.json, the machine-readable half of migration.md.
type Manifest struct {
	Version        int            `json:"version"`
	ID             string         `json:"id"`
	Source         string         `json:"source"`
	CreatedAt      string         `json:"created_at"`
	Status         string         `json:"status"` // in-progress | complete | rolled-back | failed
	Project        string         `json:"project,omitempty"`
	Files          []ManifestFile `json:"files"`
	OwnershipState ManifestFile   `json:"ownership_state"`
	GentleState    bool           `json:"gentle_state"`
	Engram         EngramBackup   `json:"engram"`
}

// Snapshot is the global migration snapshot (spec 26).
type Snapshot struct {
	ID       string
	Dir      string
	Manifest Manifest
}

// ProjectRuntime labels manifest entries that belong to the project's
// .gabitotl/ rather than to a runtime.
const ProjectRuntime = "project"

// projectPaths lists the .gabitotl files a run may create or modify.
func projectPaths(env Env, pl Plan) []string {
	if env.ProjectRoot == "" {
		return nil
	}
	l := gabitotl.NewLayout(env.ProjectRoot)
	out := projectFiles(l)
	out = append(out, filepath.Join(l.Work(), ".gitkeep"), l.LocalRuntime())
	if pl.Options.UserName != "" {
		if u, err := identity.New(pl.Options.User, pl.Options.UserName, ""); err == nil {
			if f, err := l.UserFile(u.ID); err == nil {
				out = append(out, f)
			}
		}
	}
	// Active creates the profile of the OS user when it is missing.
	if f, err := l.UserFile(identity.OSUserID()); err == nil {
		out = append(out, f)
	}
	return out
}

// GlobalDirs are the subdirectories of ~/.gabitotl (spec 39).
var GlobalDirs = []string{"installations", "migrations", "backups", "legacy", "logs"}

// MigrationsDir is ~/.gabitotl/migrations.
func MigrationsDir(env Env) string { return filepath.Join(env.StateDir(), "migrations") }

// EnsureGlobalLayout creates ~/.gabitotl and its subdirectories. It is a
// mutation of GabitoTL's own directory only, never called by dry runs.
func EnsureGlobalLayout(env Env) error {
	for _, d := range GlobalDirs {
		if err := os.MkdirAll(filepath.Join(env.StateDir(), d), 0o755); err != nil {
			return err
		}
	}
	return nil
}

func sha(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func copyFile(src, dst string) (hash string, existed bool, err error) {
	b, err := os.ReadFile(src)
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return "", true, err
	}
	return sha(b), true, os.WriteFile(dst, b, 0o600)
}

// touchedPaths lists the files a runtime's plan may change (never manual or
// no-op actions) plus the files its adapter says it cares about.
func touchedPaths(plans ...[]rt.Action) []string {
	seen := map[string]bool{}
	var out []string
	for _, pl := range plans {
		for _, a := range pl {
			if a.Edit.Op == rt.OpManual || a.Kind == rt.NoAction || a.Kind == rt.Preserve || seen[a.Path] {
				continue
			}
			seen[a.Path] = true
			out = append(out, a.Path)
		}
	}
	sort.Strings(out)
	return out
}

// CreateSnapshot is phase "snapshot". It runs BEFORE any mutation:
//
//  1. engram doctor (read-only); errors abort the run;
//  2. official engram export into the snapshot (a safety backup, never an import);
//  3. a copy of ~/.gentle-ai/state.json;
//  4. per runtime, a copy of only the files the plan touches and the files the
//     adapter inspects (so Gentle-AI's own uninstall is reversible too);
//  5. a copy of the ownership state, inventory.json, manifest.json, migration.md.
//
// Anything that fails here leaves the machine exactly as it was.
func CreateSnapshot(ctx context.Context, env Env, inv Inventory, pl Plan, obs Observer) (*Snapshot, error) {
	if err := EnsureGlobalLayout(env); err != nil {
		return nil, err
	}
	id := pl.SnapshotID
	dir := filepath.Join(MigrationsDir(env), id)
	for n := 2; ; n++ { // never reuse a snapshot directory
		if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
			break
		}
		id = fmt.Sprintf("%s-%d", pl.SnapshotID, n)
		dir = filepath.Join(MigrationsDir(env), id)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	snap := &Snapshot{ID: id, Dir: dir, Manifest: Manifest{
		Version: ManifestVersion, ID: id, Source: pl.Options.Source, Status: "in-progress",
		CreatedAt: env.Clock().Format("2006-01-02T15:04:05Z07:00"), Project: inv.Project.Key,
	}}
	fail := func(err error) (*Snapshot, error) {
		snap.Manifest.Status = "failed"
		_ = snap.Save(env)
		return snap, err
	}
	obs.emit(PhaseSnapshot, "info", "snapshot", id)

	if b, err := inv.JSON(); err != nil {
		return fail(err)
	} else if err := os.WriteFile(filepath.Join(dir, "inventory.json"), b, 0o600); err != nil {
		return fail(err)
	}

	// Engram: doctor before, then the official export. Neither modifies Engram.
	if p, _, ok := env.Engram(inv.Project.Key); ok {
		rep := p.Doctor(ctx)
		obs.emit(PhaseSnapshot, "ok", "engram doctor (before)", "")
		if !rep.OK() {
			snap.Manifest.Engram.Reason = "doctor reported errors"
			return fail(ErrMemoryUnhealthy)
		}
		out := filepath.Join(dir, "memory", "engram-export.json")
		if err := os.MkdirAll(filepath.Dir(out), 0o700); err != nil {
			return fail(err)
		}
		res, err := p.Export(ctx, out, inv.Memory.ProjectMatch)
		if err != nil {
			return fail(fmt.Errorf("%w: %v", ErrExportFailed, err))
		}
		snap.Manifest.Engram = EngramBackup{Done: true, File: filepath.Join("memory", "engram-export.json"), Project: inv.Memory.ProjectMatch,
			Sessions: res.Sessions, Observations: res.Observations, Prompts: res.Prompts}
		obs.emit(PhaseSnapshot, "ok", "engram export", fmt.Sprintf("%d observations", res.Observations))
	} else {
		snap.Manifest.Engram.Reason = "engram binary not found"
	}

	// Gentle-AI state.
	if inv.Other.GentleAI.HasState {
		_, ok, err := copyFile(GentleStatePath(env), filepath.Join(dir, "gentle-ai", "state.json"))
		if err != nil {
			return fail(err)
		}
		snap.Manifest.GentleState = ok
	}

	// Runtime files: plan paths plus adapter-inspected files.
	renv := env.RT()
	seenRT := map[string]bool{}
	for _, name := range pl.RuntimeOrder {
		a, _ := env.Runtimes.Get(name)
		acts := append(append([]rt.Action(nil), pl.Install[name]...), pl.Retire[name]...)
		paths := touchedPaths(acts)
		if in, err := a.Inspect(renv, rt.Options{Scope: rt.ScopeGlobal}); err == nil {
			for _, f := range in.Files {
				if f.Exists {
					paths = append(paths, f.Path)
				}
			}
		}
		rsnap := rt.Snapshot{Dir: filepath.Join(dir, name)}
		if err := a.Backup(renv, &rsnap, acts); err != nil { // adapter contract: plan files
			return fail(fmt.Errorf("%s backup: %w", name, err))
		}
		sort.Strings(paths)
		for _, p := range paths {
			if seenRT[name+"\x00"+p] {
				continue
			}
			seenRT[name+"\x00"+p] = true
			dst := rsnap.CopyPath(p)
			h, existed, err := copyFile(p, dst)
			if err != nil {
				return fail(err)
			}
			mf := ManifestFile{Path: p, Runtime: name, Existed: existed, SHA256: h}
			if existed {
				mf.Copy, _ = filepath.Rel(dir, dst)
			}
			snap.Manifest.Files = append(snap.Manifest.Files, mf)
		}
		obs.emit(PhaseSnapshot, "ok", "snapshot "+name, "")
	}

	// Project files the run may create or modify: recording "did not exist"
	// lets rollback and restore remove exactly what a run created.
	for _, p := range projectPaths(env, pl) {
		dst := filepath.Join(dir, "project", filepath.Base(filepath.Dir(p))+"__"+filepath.Base(p))
		h, existed, err := copyFile(p, dst)
		if err != nil {
			return fail(err)
		}
		mf := ManifestFile{Path: p, Runtime: ProjectRuntime, Existed: existed, SHA256: h}
		if existed {
			mf.Copy, _ = filepath.Rel(dir, dst)
		}
		snap.Manifest.Files = append(snap.Manifest.Files, mf)
	}

	// Ownership state (so rollback and restore can revert it too).
	statePath := filepath.Join(env.StateDir(), "state.json")
	h, existed, err := copyFile(statePath, filepath.Join(dir, "gabitotl-state.json"))
	if err != nil {
		return fail(err)
	}
	snap.Manifest.OwnershipState = ManifestFile{Path: statePath, Runtime: "gabitotl", Existed: existed, SHA256: h}
	if existed {
		snap.Manifest.OwnershipState.Copy = "gabitotl-state.json"
	}
	if err := snap.Save(env); err != nil {
		return fail(err)
	}
	return snap, nil
}

// Save writes manifest.json and migration.md.
func (s *Snapshot) Save(env Env) error {
	b, err := json.MarshalIndent(s.Manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(s.Dir, "manifest.json"), append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.Dir, "migration.md"), []byte(s.markdown(env.Home)), 0o600)
}

func tilde(home, p string) string {
	if home != "" && (p == home || strings.HasPrefix(p, home+string(filepath.Separator))) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}

// markdown renders migration.md. It lists paths and counts only: no file
// content, environment values or remotes (invariant f).
func (s *Snapshot) markdown(home string) string {
	m := s.Manifest
	var b strings.Builder
	fmt.Fprintf(&b, "---\nid: %s\nsource: %s\ncreated_at: %s\nstatus: %s\n---\n\n# Migration snapshot %s\n\n", m.ID, m.Source, m.CreatedAt, m.Status, m.ID)
	b.WriteString("## Memory\n\n")
	if m.Engram.Done {
		fmt.Fprintf(&b, "Engram was adopted (never copied or modified). Safety export: `%s` (%d sessions, %d observations, %d prompts).\n\n", m.Engram.File, m.Engram.Sessions, m.Engram.Observations, m.Engram.Prompts)
	} else {
		fmt.Fprintf(&b, "No Engram export was made: %s.\n\n", orStr(m.Engram.Reason, "not requested"))
	}
	b.WriteString("## Files saved before any change\n\n")
	if len(m.Files) == 0 {
		b.WriteString("None.\n")
	}
	for _, f := range m.Files {
		state := "did not exist"
		if f.Existed {
			state = "copy in `" + f.Copy + "`"
		}
		fmt.Fprintf(&b, "- [%s] `%s`: %s\n", f.Runtime, tilde(home, f.Path), state)
	}
	if m.GentleState {
		b.WriteString("- `~/.gentle-ai/state.json`: copy in `gentle-ai/state.json`\n")
	}
	b.WriteString("\n## Restore\n\n`gabitotl restore " + m.ID + "` puts the files above back. Engram is never part of a restore.\n")
	return b.String()
}

func orStr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// LoadSnapshot reads a snapshot by id.
func LoadSnapshot(env Env, id string) (*Snapshot, error) {
	if id == "" || id != filepath.Base(id) || strings.ContainsAny(id, `/\`) || id == ".." {
		return nil, fmt.Errorf("invalid snapshot id %q", id)
	}
	dir := filepath.Join(MigrationsDir(env), id)
	b, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return nil, fmt.Errorf("snapshot %s: %w", id, err)
	}
	s := &Snapshot{ID: id, Dir: dir}
	if err := json.Unmarshal(b, &s.Manifest); err != nil {
		return nil, fmt.Errorf("snapshot %s: manifest is not valid JSON", id)
	}
	return s, nil
}

// ListSnapshots returns every readable snapshot, oldest first.
func ListSnapshots(env Env) ([]*Snapshot, error) {
	es, err := os.ReadDir(MigrationsDir(env))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []*Snapshot
	for _, e := range es {
		if !e.IsDir() {
			continue
		}
		if s, err := LoadSnapshot(env, e.Name()); err == nil {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// copyOf returns the saved bytes of mf.
func (s *Snapshot) copyOf(mf ManifestFile) ([]byte, error) {
	return os.ReadFile(filepath.Join(s.Dir, filepath.FromSlash(mf.Copy)))
}

// RestoreFiles puts every file of the manifest back exactly as it was: saved
// copies are written back, files that did not exist are removed. The ownership
// state is restored the same way. It never touches Engram or anything outside
// the manifest. Project (.gabitotl) files are only reverted when includeProject
// is set: a rollback of a run that just created them does, a later user-driven
// restore never does (the folder is committed work). It returns the paths it changed.
func (s *Snapshot) RestoreFiles(includeProject bool) ([]string, error) {
	var changed []string
	done := map[string]bool{}
	apply := func(mf ManifestFile) error {
		if done[mf.Path] || (mf.Runtime == ProjectRuntime && !includeProject) {
			return nil
		}
		done[mf.Path] = true
		cur, err := os.ReadFile(mf.Path)
		curExists := err == nil
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		if !mf.Existed {
			if curExists {
				if err := os.Remove(mf.Path); err != nil {
					return err
				}
				changed = append(changed, mf.Path)
			}
			if mf.Runtime == ProjectRuntime {
				pruneEmptyDirs(filepath.Dir(mf.Path))
			}
			return nil
		}
		want, err := s.copyOf(mf)
		if err != nil {
			return fmt.Errorf("snapshot copy of %s is missing: %w", mf.Path, err)
		}
		if curExists && string(cur) == string(want) {
			return nil
		}
		mode := os.FileMode(0o644)
		if st, err := os.Stat(mf.Path); err == nil {
			mode = st.Mode().Perm()
		}
		if err := os.MkdirAll(filepath.Dir(mf.Path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(mf.Path, want, mode); err != nil {
			return err
		}
		changed = append(changed, mf.Path)
		return nil
	}
	var firstErr error
	for _, mf := range s.Manifest.Files {
		if err := apply(mf); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if err := apply(s.Manifest.OwnershipState); err != nil && firstErr == nil {
		firstErr = err
	}
	return changed, firstErr
}

// pruneEmptyDirs removes empty directories upward from dir while they are inside
// a .gabitotl folder. os.Remove refuses non-empty directories, so nothing that
// holds content can be lost.
func pruneEmptyDirs(dir string) {
	for strings.Contains(dir+string(filepath.Separator), string(filepath.Separator)+gabitotl.DirName+string(filepath.Separator)) {
		if os.Remove(dir) != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
}
