package guard

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/winnstorm/gabitotl/internal/backup"
	"github.com/winnstorm/gabitotl/internal/branding"
	"github.com/winnstorm/gabitotl/internal/gabitotl"
)

// FileFix is the planned rewrite of one file: only the lines that are pure
// attribution are removed, nothing else is touched.
type FileFix struct {
	Path    string
	Removed []branding.Finding
	Content []byte // the new content, kept in memory until applied
}

// Plan is the result of PlanFix.
type Plan struct {
	Fixes  []FileFix
	Manual []branding.Finding // matches that share a line with code or prose: a human decides
}

// RemoveLines deletes the given 1-based lines from data, keeping the line
// endings of every other line, and collapses the blank line a removal leaves
// behind when it would double up.
func RemoveLines(data []byte, lines []int) []byte {
	drop := map[int]bool{}
	for _, n := range lines {
		drop[n] = true
	}
	parts := strings.SplitAfter(string(data), "\n")
	var out []string
	removedPrev := false
	blank := func(s string) bool { return strings.TrimSpace(s) == "" }
	for i, p := range parts {
		if p == "" {
			continue
		}
		if drop[i+1] {
			removedPrev = true
			continue
		}
		if removedPrev && blank(p) && (len(out) == 0 || blank(out[len(out)-1])) {
			continue
		}
		removedPrev = false
		out = append(out, p)
	}
	return []byte(strings.Join(out, ""))
}

// PlanFix scans rels (relative to root) and plans the rewrites. It reads only.
func PlanFix(root string, rels []string, s Settings) (Plan, error) {
	d := s.Detector()
	var plan Plan
	for _, rel := range rels {
		if branding.Skipped(rel) || branding.Allowed(rel, s.AllowPaths) {
			continue
		}
		abs := filepath.Join(root, filepath.FromSlash(rel))
		st, err := os.Lstat(abs)
		if err != nil || !st.Mode().IsRegular() || st.Size() > branding.MaxFileSize {
			continue
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			return plan, err
		}
		var rm []branding.Finding
		var lines []int
		for _, f := range d.ScanFile(rel, data) {
			if f.Removable() {
				rm = append(rm, f)
				lines = append(lines, f.Line)
			} else {
				plan.Manual = append(plan.Manual, f)
			}
		}
		if len(rm) > 0 {
			plan.Fixes = append(plan.Fixes, FileFix{Path: rel, Removed: rm, Content: RemoveLines(data, lines)})
		}
	}
	return plan, nil
}

// Applied is the outcome of ApplyFix.
type Applied struct {
	Snapshot string
	Written  []string
	Skipped  []backup.Skip // files that could not be backed up are never rewritten
}

// ApplyFix backs up every file to rewrite (.gabitotl/.bak) and only then
// rewrites it. A file the backup refuses (for example a secret-looking one) is
// left alone and reported.
func ApplyFix(root, user string, policy gabitotl.BackupPolicy, plan Plan) (Applied, error) {
	var res Applied
	if len(plan.Fixes) == 0 {
		return res, nil
	}
	paths := make([]string, len(plan.Fixes))
	for i, f := range plan.Fixes {
		paths[i] = f.Path
	}
	m := backup.New(root, policy)
	b, err := m.Create(backup.Meta{User: user, Task: "guard-fix", Reason: "remove AI attribution lines"}, paths)
	res.Skipped = b.Skipped
	if err != nil && !errors.Is(err, backup.ErrNothingToBackup) {
		return res, fmt.Errorf("guard: backup failed, nothing was changed: %w", err)
	}
	res.Snapshot = b.Snapshot.ID
	for _, f := range plan.Fixes {
		if !slices.Contains(b.Snapshot.Manifest.Files, f.Path) {
			continue
		}
		abs := filepath.Join(root, filepath.FromSlash(f.Path))
		mode := os.FileMode(0o644)
		if st, err := os.Stat(abs); err == nil {
			mode = st.Mode().Perm()
		}
		if err := writeKeepingMode(abs, f.Content, mode); err != nil {
			return res, err
		}
		res.Written = append(res.Written, f.Path)
	}
	return res, nil
}

func writeKeepingMode(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".gabitotl-fix-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
