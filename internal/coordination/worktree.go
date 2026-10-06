// Package coordination supports teams working on the same repository without
// stepping on each other (spec section 8): git worktrees, detection of two
// sessions in one working directory, and advisory soft locks.
package coordination

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/vcs"
)

// Worktree errors.
var (
	ErrTempLocation   = errors.New("coordination: worktrees must not live under a temporary directory")
	ErrWorktreeExists = errors.New("coordination: worktree directory already exists")
	ErrMainWorktree   = errors.New("coordination: refusing to remove the main worktree")
	ErrNoWorktree     = errors.New("coordination: worktree not found")
)

// Worktree is one entry of `git worktree list`.
type Worktree struct {
	Path   string `json:"path"`
	Branch string `json:"branch,omitempty"`
	Head   string `json:"head,omitempty"`
	Main   bool   `json:"main"`
}

// Worktrees manages git worktrees placed next to the repository.
type Worktrees struct {
	Repo *vcs.Repo
	// AllowTemp lets worktrees live under a temporary directory. It exists for
	// tests only: real worktrees belong under the user's home, next to the repo.
	AllowTemp bool
}

// NewWorktrees returns a manager for repo.
func NewWorktrees(repo *vcs.Repo) *Worktrees { return &Worktrees{Repo: repo} }

func (w *Worktrees) topLevel(ctx context.Context) (string, error) {
	top, err := w.Repo.Git(ctx, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", vcs.ErrNotRepo
	}
	return filepath.EvalSymlinks(top)
}

// Dir returns the sibling worktree directory
// <repo-parent>/<repo-name>-worktrees/<task-or-user>. The task names the
// directory when given, otherwise the user.
func (w *Worktrees) Dir(ctx context.Context, user, task string) (string, error) {
	name := task
	if name == "" {
		name = user
	}
	if !gabitotl.ValidID(name) {
		return "", gabitotl.ErrInvalidID
	}
	top, err := w.topLevel(ctx)
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(top), filepath.Base(top)+"-worktrees", name), nil
}

func isTemp(p string) bool {
	for _, t := range []string{os.TempDir(), "/tmp", "/var/tmp"} {
		if real, err := filepath.EvalSymlinks(t); err == nil {
			t = real
		}
		if rel, err := filepath.Rel(t, p); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// Add creates a worktree for user/task on branch, creating the branch when it
// does not exist (gated by permissions.branches). Each worktree gets its own
// .gabitotl/local/, so identity and backups stay per working directory.
func (w *Worktrees) Add(ctx context.Context, user, task, branch string) (Worktree, error) {
	if strings.TrimSpace(branch) == "" || strings.HasPrefix(branch, "-") {
		return Worktree{}, errors.New("coordination: a valid branch name is required")
	}
	dir, err := w.Dir(ctx, user, task)
	if err != nil {
		return Worktree{}, err
	}
	if !w.AllowTemp && isTemp(dir) {
		return Worktree{}, fmt.Errorf("%w: %s", ErrTempLocation, dir)
	}
	if _, err := os.Stat(dir); err == nil {
		return Worktree{}, fmt.Errorf("%w: %s", ErrWorktreeExists, dir)
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return Worktree{}, err
	}
	_, existsErr := w.Repo.Git(ctx, "rev-parse", "--verify", "-q", "refs/heads/"+branch)
	if existsErr == nil {
		_, err = w.Repo.Git(ctx, "worktree", "add", dir, branch)
	} else {
		if gerr := vcs.Gate(w.Repo.Config.Permissions, vcs.OpBranch, "create branch "+branch, w.Repo.Confirm); gerr != nil {
			return Worktree{}, gerr
		}
		_, err = w.Repo.Git(ctx, "worktree", "add", "-b", branch, dir)
	}
	if err != nil {
		return Worktree{}, err
	}
	head, _ := vcs.New(dir, w.Repo.Config).Git(ctx, "rev-parse", "HEAD")
	return Worktree{Path: dir, Branch: branch, Head: head}, nil
}

// List returns every worktree of the repository; the first one is the main one.
func (w *Worktrees) List(ctx context.Context) ([]Worktree, error) {
	out, err := w.Repo.Git(ctx, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	return parseWorktrees(out), nil
}

func parseWorktrees(out string) []Worktree {
	var list []Worktree
	var cur *Worktree
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "worktree "):
			list = append(list, Worktree{Path: strings.TrimPrefix(line, "worktree "), Main: len(list) == 0})
			cur = &list[len(list)-1]
		case cur == nil:
		case strings.HasPrefix(line, "HEAD "):
			cur.Head = strings.TrimPrefix(line, "HEAD ")
		case strings.HasPrefix(line, "branch "):
			cur.Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		}
	}
	return list
}

// Remove deletes a linked worktree identified by its directory name (the
// task-or-user slug) or full path. It never removes the main worktree and
// never forces: a dirty worktree is refused by git.
func (w *Worktrees) Remove(ctx context.Context, nameOrPath string) error {
	list, err := w.List(ctx)
	if err != nil {
		return err
	}
	want := nameOrPath
	if !filepath.IsAbs(want) && gabitotl.ValidID(want) {
		want = ""
		for _, wt := range list {
			if filepath.Base(wt.Path) == nameOrPath && !wt.Main {
				want = wt.Path
			}
		}
	}
	for _, wt := range list {
		if want == "" || filepath.Clean(wt.Path) != filepath.Clean(want) {
			continue
		}
		if wt.Main {
			return ErrMainWorktree
		}
		_, err := w.Repo.Git(ctx, "worktree", "remove", wt.Path)
		return err
	}
	return fmt.Errorf("%w: %s", ErrNoWorktree, nameOrPath)
}
