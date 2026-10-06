// Package backup keeps local, never-committed copies of the files GabitoTL is
// about to modify, under .gabitotl/.bak/<user>/<timestamp>_<task>/ (spec 14).
package backup

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
)

// Errors.
var (
	ErrOutsideRoot     = errors.New("backup: path is outside the project root")
	ErrNothingToBackup = errors.New("backup: no existing files to back up")
	ErrNotInSnapshot   = errors.New("backup: file is not part of the snapshot")
	ErrNotFound        = errors.New("backup: snapshot not found")
)

// TimeLayout is the timestamp prefix of a snapshot directory (spec 14 example).
const TimeLayout = "2006-01-02_1504"

// ManifestName is the manifest file inside every snapshot.
const ManifestName = "manifest.md"

// Meta is the context recorded in a snapshot's manifest.
type Meta struct {
	User       string
	Task       string
	Branch     string
	BaseCommit string
	Reason     string
}

// Manifest is the front matter of manifest.md.
type Manifest struct {
	User       string   `yaml:"user"`
	Task       string   `yaml:"task"`
	Date       string   `yaml:"date"` // RFC 3339
	Branch     string   `yaml:"branch,omitempty"`
	BaseCommit string   `yaml:"base_commit,omitempty"`
	Reason     string   `yaml:"reason,omitempty"`
	Files      []string `yaml:"files"`
}

// Snapshot is one backup directory.
type Snapshot struct {
	ID       string // directory name: <timestamp>_<task>
	Dir      string
	Manifest Manifest
}

// Time parses the manifest date.
func (s Snapshot) Time() time.Time {
	t, _ := time.Parse(time.RFC3339, s.Manifest.Date)
	return t
}

// Skip explains why a requested file was not copied.
type Skip struct {
	Path   string
	Reason string
}

// Result is the outcome of Create.
type Result struct {
	Snapshot Snapshot
	Skipped  []Skip
}

// Manager creates, lists, restores and prunes snapshots of one project.
type Manager struct {
	Layout gabitotl.Layout
	Policy gabitotl.BackupPolicy
	// Now is the clock; nil means time.Now.
	Now func() time.Time
}

// New returns a Manager for the project root using the workflow retention policy.
func New(root string, policy gabitotl.BackupPolicy) *Manager {
	return &Manager{Layout: gabitotl.NewLayout(root), Policy: policy}
}

func (m *Manager) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

// realRoot is the symlink-free project root.
func (m *Manager) realRoot() (string, error) {
	root, err := filepath.Abs(m.Layout.Root)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(root)
}

// within reports whether p is root or below it (both must be clean/absolute).
func within(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// resolve turns a user path into a slash-style relative path inside the real
// project root, rejecting traversal and symlink escapes. The file itself may
// be missing (the caller decides); only its existing ancestors are resolved.
func (m *Manager) resolve(realRoot, file string) (rel string, abs string, err error) {
	if strings.TrimSpace(file) == "" {
		return "", "", fmt.Errorf("%w: empty path", ErrOutsideRoot)
	}
	p := file
	if !filepath.IsAbs(p) {
		p = filepath.Join(realRoot, p)
	}
	p = filepath.Clean(p)
	// Resolve the deepest existing ancestor to defeat symlink escapes.
	probe := p
	for {
		if _, lerr := os.Lstat(probe); lerr == nil {
			break
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			break
		}
		probe = parent
	}
	realProbe, rerr := filepath.EvalSymlinks(probe)
	if rerr != nil {
		return "", "", rerr
	}
	resolved := filepath.Join(realProbe, strings.TrimPrefix(p, probe))
	if !within(realRoot, resolved) || resolved == realRoot {
		return "", "", fmt.Errorf("%w: %s", ErrOutsideRoot, file)
	}
	rel, err = filepath.Rel(realRoot, resolved)
	if err != nil {
		return "", "", err
	}
	return filepath.ToSlash(rel), resolved, nil
}

// secretNames are file names never copied into backups.
func secretLooking(rel string) bool {
	base := strings.ToLower(filepath.Base(rel))
	switch {
	case base == ".env" || strings.HasPrefix(base, ".env."):
		return true
	case base == ".npmrc" || base == ".pypirc" || base == ".netrc" || base == "credentials":
		return true
	case strings.HasPrefix(base, "id_rsa") || strings.HasPrefix(base, "id_ed25519") || strings.HasPrefix(base, "id_ecdsa"):
		return true
	}
	for _, ext := range []string{".pem", ".key", ".p12", ".pfx", ".kdbx"} {
		if strings.HasSuffix(base, ext) {
			return true
		}
	}
	return false
}

// Create copies the existing files in files into a new snapshot and writes its
// manifest. Files that do not exist (they will be created, nothing to lose),
// directories, secret-looking files and anything under .gabitotl/.bak are
// skipped and reported. A path outside the project root is an error and nothing
// is written.
func (m *Manager) Create(meta Meta, files []string) (Result, error) {
	if !gabitotl.ValidID(meta.User) || !gabitotl.ValidID(meta.Task) {
		return Result{}, gabitotl.ErrInvalidID
	}
	userDir, err := m.Layout.BakUser(meta.User)
	if err != nil {
		return Result{}, err
	}
	root, err := m.realRoot()
	if err != nil {
		return Result{}, err
	}
	bakRoot := filepath.Join(root, gabitotl.DirName, ".bak")

	type item struct{ rel, abs string }
	var items []item
	var skipped []Skip
	seen := map[string]bool{}
	for _, f := range files {
		rel, abs, err := m.resolve(root, f)
		if err != nil {
			return Result{}, err
		}
		if seen[rel] {
			continue
		}
		seen[rel] = true
		switch {
		case within(bakRoot, abs):
			skipped = append(skipped, Skip{rel, "inside .gabitotl/.bak"})
		case secretLooking(rel):
			skipped = append(skipped, Skip{rel, "looks like a secret file"})
		default:
			st, serr := os.Stat(abs)
			switch {
			case errors.Is(serr, fs.ErrNotExist):
				skipped = append(skipped, Skip{rel, "does not exist yet"})
			case serr != nil:
				return Result{}, serr
			case !st.Mode().IsRegular():
				skipped = append(skipped, Skip{rel, "not a regular file"})
			default:
				items = append(items, item{rel, abs})
			}
		}
	}
	if len(items) == 0 {
		return Result{Skipped: skipped}, ErrNothingToBackup
	}

	ts := m.now().Format(TimeLayout)
	id := ts + "_" + meta.Task
	dir := filepath.Join(userDir, id)
	for n := 2; ; n++ {
		if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
			break
		}
		id = fmt.Sprintf("%s_%s-%d", ts, meta.Task, n)
		dir = filepath.Join(userDir, id)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Result{}, err
	}
	man := Manifest{
		User: meta.User, Task: meta.Task, Date: m.now().UTC().Format(time.RFC3339),
		Branch: meta.Branch, BaseCommit: meta.BaseCommit, Reason: meta.Reason,
	}
	for _, it := range items {
		if err := copyFile(it.abs, filepath.Join(dir, filepath.FromSlash(it.rel))); err != nil {
			_ = os.RemoveAll(dir)
			return Result{}, err
		}
		man.Files = append(man.Files, it.rel)
	}
	sort.Strings(man.Files)
	if err := gabitotl.WriteDoc(filepath.Join(dir, ManifestName), man, manifestBody(man)); err != nil {
		_ = os.RemoveAll(dir)
		return Result{}, err
	}
	return Result{Snapshot: Snapshot{ID: id, Dir: dir, Manifest: man}, Skipped: skipped}, nil
}

func manifestBody(m Manifest) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n# Backup %s\n\n", m.Task)
	fmt.Fprintf(&b, "- user: %s\n- task: %s\n- date: %s\n", m.User, m.Task, m.Date)
	if m.Branch != "" {
		fmt.Fprintf(&b, "- branch: %s\n", m.Branch)
	}
	if m.BaseCommit != "" {
		fmt.Fprintf(&b, "- base commit: %s\n", m.BaseCommit)
	}
	if m.Reason != "" {
		fmt.Fprintf(&b, "- reason: %s\n", m.Reason)
	}
	b.WriteString("\n## Files\n\n")
	for _, f := range m.Files {
		fmt.Fprintf(&b, "- %s\n", f)
	}
	return b.String()
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	st, err := in.Stat()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, st.Mode().Perm()|0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// readSnapshot loads a snapshot directory; ok is false when it has no valid manifest.
func readSnapshot(dir string) (Snapshot, bool) {
	d, err := gabitotl.ReadDoc(filepath.Join(dir, ManifestName))
	if err != nil {
		return Snapshot{}, false
	}
	var man Manifest
	if d.Decode(&man) != nil {
		return Snapshot{}, false
	}
	return Snapshot{ID: filepath.Base(dir), Dir: dir, Manifest: man}, true
}

// List returns one user's snapshots, newest first.
func (m *Manager) List(user string) ([]Snapshot, error) {
	userDir, err := m.Layout.BakUser(user)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(userDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Snapshot
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if s, ok := readSnapshot(filepath.Join(userDir, e.Name())); ok {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if ti, tj := out[i].Time(), out[j].Time(); !ti.Equal(tj) {
			return ti.After(tj)
		}
		return out[i].ID > out[j].ID
	})
	return out, nil
}

// Find returns one snapshot by id. The id "latest" selects the newest.
func (m *Manager) Find(user, id string) (Snapshot, error) {
	snaps, err := m.List(user)
	if err != nil {
		return Snapshot{}, err
	}
	for _, s := range snaps {
		if id == "latest" || s.ID == id {
			return s, nil
		}
	}
	return Snapshot{}, fmt.Errorf("%w: %s/%s", ErrNotFound, user, id)
}

// Restore copies files from the snapshot back into the project (all of them
// when files is empty) and returns the restored paths. Only files listed in
// the manifest can be restored, and destinations must stay inside the project.
// Files that currently exist are first saved in a "pre-restore" snapshot, so a
// restore is itself recoverable.
func (m *Manager) Restore(snap Snapshot, files ...string) ([]string, error) {
	root, err := m.realRoot()
	if err != nil {
		return nil, err
	}
	want := snap.Manifest.Files
	if len(files) > 0 {
		want = nil
		inManifest := map[string]bool{}
		for _, f := range snap.Manifest.Files {
			inManifest[f] = true
		}
		for _, f := range files {
			rel, _, err := m.resolve(root, f)
			if err != nil {
				return nil, err
			}
			if !inManifest[rel] {
				return nil, fmt.Errorf("%w: %s", ErrNotInSnapshot, rel)
			}
			want = append(want, rel)
		}
	}
	type pair struct{ src, dst, rel string }
	var plan []pair
	for _, rel := range want {
		_, dst, err := m.resolve(root, rel)
		if err != nil {
			return nil, err
		}
		src := filepath.Join(snap.Dir, filepath.FromSlash(rel))
		if !within(snap.Dir, src) {
			return nil, fmt.Errorf("%w: %s", ErrOutsideRoot, rel)
		}
		plan = append(plan, pair{src, dst, rel})
	}
	var existing []string
	for _, p := range plan {
		if _, err := os.Stat(p.dst); err == nil {
			existing = append(existing, p.rel)
		}
	}
	if len(existing) > 0 {
		meta := Meta{User: snap.Manifest.User, Task: "pre-restore", Reason: "state before restoring " + snap.ID}
		if _, err := m.Create(meta, existing); err != nil {
			return nil, fmt.Errorf("backup: cannot save current files before restoring: %w", err)
		}
	}
	var restored []string
	for _, p := range plan {
		if err := copyFile(p.src, p.dst); err != nil {
			return restored, err
		}
		restored = append(restored, p.rel)
	}
	return restored, nil
}

// Prune applies the retention policy to one user's snapshots and returns the
// removed snapshot ids. A snapshot is kept when it is among the newest
// MaxSnapshots OR newer than MaxAgeDays; only snapshots failing both tests are
// deleted. Directories without a valid manifest are never touched.
func (m *Manager) Prune(user string) ([]string, error) {
	snaps, err := m.List(user)
	if err != nil {
		return nil, err
	}
	cutoff := m.now().AddDate(0, 0, -m.Policy.MaxAgeDays)
	var removed []string
	for i, s := range snaps { // newest first
		keepByCount := i < m.Policy.MaxSnapshots
		keepByAge := s.Time().After(cutoff)
		if keepByCount || keepByAge {
			continue
		}
		if err := os.RemoveAll(s.Dir); err != nil {
			return removed, err
		}
		removed = append(removed, s.ID)
	}
	return removed, nil
}
