// Package guard is the authorship guard around Git: the commit-msg and
// pre-commit hooks, the scan sources (staged diff, diff against a ref, whole
// tree, history), and the fixer that removes attribution lines after a backup.
// Detection itself lives in package branding.
package guard

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/winnstorm/gabitotl/internal/vcs"
)

// Hook names the guard manages.
const (
	HookCommitMsg = "commit-msg"
	HookPreCommit = "pre-commit"
)

// Hooks lists the managed hooks.
var Hooks = []string{HookCommitMsg, HookPreCommit}

// Marker identifies a hook file written by GabitoTL. A hook without it is the
// user's and is never overwritten: it is moved aside and chained instead.
const Marker = "# gabitotl:guard:managed"

// OrigSuffix is appended to the name of a user hook that was moved aside.
const OrigSuffix = ".gabitotl-orig"

// ErrNotRepo is returned when dir is not inside a Git work tree.
var ErrNotRepo = errors.New("guard: not a git repository")

// HooksDir returns the directory Git runs hooks from for the work tree at dir.
// It honors core.hooksPath and resolves linked worktrees to the shared hooks
// directory (git rev-parse --git-path hooks).
func HooksDir(ctx context.Context, run vcs.Runner, dir string) (string, error) {
	out, err := run.Run(ctx, dir, "git", "rev-parse", "--git-path", "hooks")
	if err != nil || strings.TrimSpace(out) == "" {
		return "", ErrNotRepo
	}
	p := strings.TrimSpace(out)
	if !filepath.IsAbs(p) {
		p = filepath.Join(dir, p)
	}
	// Resolve symlinks (macOS /var -> /private/var) so the main checkout and its
	// linked worktrees report the same hooks directory.
	if real, err := filepath.EvalSymlinks(p); err == nil {
		return real, nil
	}
	return filepath.Clean(p), nil
}

// HookState is the read-only state of one managed hook.
type HookState struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	Installed bool   `json:"installed"` // our wrapper is in place
	Chained   bool   `json:"chained"`   // a user hook was preserved and is called after ours
	Foreign   bool   `json:"foreign"`   // a hook that is not ours occupies the slot
}

// script renders the wrapper. bin is the absolute path of the gabitotl binary
// at install time; the wrapper falls back to PATH, and a missing binary never
// blocks a commit (it only warns), so removing GabitoTL cannot brick Git.
func script(name, bin string) string {
	var run string
	switch name {
	case HookCommitMsg:
		run = `"$GABITOTL_BIN" guard commit-msg "$1" || exit $?`
	default:
		run = `"$GABITOTL_BIN" guard scan --staged --hook || exit $?`
	}
	return fmt.Sprintf(`#!/bin/sh
%s v1 - managed by GabitoTL, remove with: gabitotl guard uninstall-hooks
GABITOTL_BIN=%s
[ -x "$GABITOTL_BIN" ] || GABITOTL_BIN=$(command -v gabitotl 2>/dev/null)
if [ -n "$GABITOTL_BIN" ] && [ -x "$GABITOTL_BIN" ]; then
  %s
else
  echo "gabitotl: binary not found, authorship guard skipped" >&2
fi
HOOK_DIR=$(dirname "$0")
if [ -x "$HOOK_DIR/%s%s" ]; then
  exec "$HOOK_DIR/%s%s" "$@"
fi
exit 0
`, Marker, shQuote(bin), run, name, OrigSuffix, name, OrigSuffix)
}

func shQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func isManaged(path string) bool {
	b, err := os.ReadFile(path)
	return err == nil && strings.Contains(string(b), Marker)
}

// InstallHooks installs (or refreshes) the managed hooks in hooksDir. A hook
// that already exists and is not ours is renamed to <name>.gabitotl-orig and
// chained after the guard, never overwritten. Running it twice changes nothing.
func InstallHooks(hooksDir, bin string) ([]HookState, error) {
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return nil, err
	}
	for _, name := range Hooks {
		path := filepath.Join(hooksDir, name)
		if _, err := os.Lstat(path); err == nil && !isManaged(path) {
			orig := path + OrigSuffix
			if _, err := os.Lstat(orig); err == nil {
				return nil, fmt.Errorf("guard: %s and %s both exist; resolve them by hand before installing", name, name+OrigSuffix)
			}
			if err := os.Rename(path, orig); err != nil {
				return nil, err
			}
		}
		if err := writeExec(path, []byte(script(name, bin))); err != nil {
			return nil, err
		}
	}
	return HookStatus(hooksDir), nil
}

func writeExec(path string, data []byte) error {
	if old, err := os.ReadFile(path); err == nil && string(old) == string(data) {
		return os.Chmod(path, 0o755)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".gabitotl-hook-*")
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
	if err := os.Chmod(tmp.Name(), 0o755); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// UninstallHooks removes the managed wrappers and restores any user hook that
// was moved aside, byte for byte. Hooks that are not ours are left alone.
func UninstallHooks(hooksDir string) ([]HookState, error) {
	for _, name := range Hooks {
		path := filepath.Join(hooksDir, name)
		if !isManaged(path) {
			continue
		}
		if err := os.Remove(path); err != nil {
			return nil, err
		}
		if orig := path + OrigSuffix; fileExists(orig) {
			if err := os.Rename(orig, path); err != nil {
				return nil, err
			}
		}
	}
	return HookStatus(hooksDir), nil
}

func fileExists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

// HookStatus reports the state of every managed hook in hooksDir.
func HookStatus(hooksDir string) []HookState {
	var out []HookState
	for _, name := range Hooks {
		path := filepath.Join(hooksDir, name)
		st := HookState{Name: name, Path: path}
		switch {
		case isManaged(path):
			st.Installed = true
			st.Chained = fileExists(path + OrigSuffix)
		case fileExists(path):
			st.Foreign = true
		}
		out = append(out, st)
	}
	return out
}

// AllInstalled reports whether every managed hook is in place.
func AllInstalled(states []HookState) bool {
	for _, s := range states {
		if !s.Installed {
			return false
		}
	}
	return len(states) > 0
}
