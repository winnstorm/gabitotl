package vcs

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
)

// Errors of the delivery commands (commit, push, pr, merge). Callers match them
// with errors.Is and word their own messages.
var (
	// ErrOnBaseBranch means the operation would commit or push the base branch.
	ErrOnBaseBranch = errors.New("vcs: this is the base branch; work happens on a task branch")
	// ErrNoBase means the base branch could not be told.
	ErrNoBase = errors.New("vcs: cannot tell the base branch; set base_branch in vcs.md")
	// ErrDetached means HEAD is not on a branch.
	ErrDetached = errors.New("vcs: HEAD is detached; switch to a branch first")
	// ErrNoPR means no open pull request exists for a branch.
	ErrNoPR = errors.New("vcs: no open pull request for the branch")
	// ErrChecksFailing and ErrChecksPending stop a merge; ChecksError carries them.
	ErrChecksFailing = errors.New("vcs: the checks of the pull request are failing")
	ErrChecksPending = errors.New("vcs: the checks of the pull request did not finish in time")
	// ErrLocalPath means a path inside .gabitotl/local, which is never committed.
	ErrLocalPath = errors.New("vcs: .gabitotl/local is personal and is never committed")
)

// LocalDir is the per-person directory that is never staged.
const LocalDir = ".gabitotl/local"

// ChecksError is returned when the checks forbid the merge. It matches
// ErrChecksFailing or ErrChecksPending with errors.Is.
type ChecksError struct {
	State  string
	Detail string
	Waited time.Duration
}

func (e *ChecksError) Error() string {
	msg := fmt.Sprintf("checks are %s", e.State)
	if e.Detail != "" {
		msg += " (" + e.Detail + ")"
	}
	return "vcs: " + msg + "; the pull request was not merged"
}

// Is lets errors.Is match the sentinel of the state.
func (e *ChecksError) Is(target error) bool {
	return (target == ErrChecksFailing && e.State == ChecksFailing) ||
		(target == ErrChecksPending && e.State == ChecksPending)
}

// LoginError means the person is not logged in to the platform; State carries
// the exact fix. It matches ErrNotLoggedIn.
type LoginError struct{ State AuthState }

func (e *LoginError) Error() string {
	if e.State.Fix != "" {
		return "vcs: not logged in to " + e.State.Provider + ". " + e.State.Fix
	}
	return "vcs: not logged in to " + e.State.Provider
}

// Is makes errors.Is(err, ErrNotLoggedIn) true.
func (e *LoginError) Is(target error) bool { return target == ErrNotLoggedIn }

// PRFinder is implemented by the hosts that can look up the open pull request of
// a branch without creating one.
type PRFinder interface {
	FindPR(ctx context.Context, dir, head string) (PR, bool, error)
}

// EnsureLogin returns a *LoginError (with the exact fix) unless the person is
// logged in. It never prompts and never changes anything.
func EnsureLogin(ctx context.Context, h Host, dir string) error {
	st, err := h.AuthStatus(ctx, dir)
	if err != nil {
		return err
	}
	if !st.OK() {
		return &LoginError{State: st}
	}
	return nil
}

// CurrentBranch is the checked-out branch.
func (r *Repo) CurrentBranch(ctx context.Context) (string, error) {
	b, err := r.Git(ctx, "symbolic-ref", "--short", "-q", "HEAD")
	if err != nil || b == "" {
		return "", ErrDetached
	}
	return b, nil
}

// BaseBranch is the branch work is delivered to: base_branch from vcs.md, else the
// repository default (what origin/HEAD points at), else main or master when one
// exists.
func (r *Repo) BaseBranch(ctx context.Context) (string, error) {
	if b := strings.TrimSpace(r.Config.BaseBranch); b != "" {
		return b, nil
	}
	if ref, err := r.Git(ctx, "symbolic-ref", "--short", "-q", "refs/remotes/origin/HEAD"); err == nil && ref != "" {
		return strings.TrimPrefix(ref, "origin/"), nil
	}
	for _, b := range []string{"main", "master"} {
		for _, ref := range []string{"refs/heads/" + b, "refs/remotes/origin/" + b} {
			if _, err := r.Git(ctx, "rev-parse", "--verify", "-q", ref); err == nil {
				return b, nil
			}
		}
	}
	return "", ErrNoBase
}

// notBase returns the current branch, or ErrOnBaseBranch when it is the base.
func (r *Repo) notBase(ctx context.Context) (string, error) {
	cur, err := r.CurrentBranch(ctx)
	if err != nil {
		return "", err
	}
	base, err := r.BaseBranch(ctx)
	if err != nil {
		return "", err
	}
	if cur == base {
		return "", fmt.Errorf("%w (%s)", ErrOnBaseBranch, base)
	}
	return cur, nil
}

// checkedMessage applies the authorship guard, the Conventional Commits rule and
// the commits permission, in that order, and returns the message to commit.
func (r *Repo) checkedMessage(message string) (string, error) {
	message, err := r.enforce(OpCommit, message)
	if err != nil {
		return "", err
	}
	if err := ValidateCommitMessage(message); err != nil {
		return "", err
	}
	if err := Gate(r.Config.Permissions, OpCommit, "commit: "+firstLine(message), r.Confirm); err != nil {
		return "", err
	}
	return message, nil
}

// CommitWork commits the given paths, or every change when none is given. It
// never stages .gabitotl/local and never stages what git ignores, and it refuses
// to commit on the base branch. Gated by permissions.commits. It returns the
// full hash of the new commit.
func (r *Repo) CommitWork(ctx context.Context, message string, paths []string) (string, error) {
	if strings.TrimSpace(message) == "" {
		return "", ErrInvalidCommitMessage
	}
	if _, err := r.notBase(ctx); err != nil {
		return "", err
	}
	for _, p := range paths {
		if inLocalDir(p) {
			return "", fmt.Errorf("%w: %s", ErrLocalPath, p)
		}
	}
	message, err := r.checkedMessage(message)
	if err != nil {
		return "", err
	}
	args := []string{"add", "-A", "--", "."}
	if len(paths) > 0 {
		args = append([]string{"add", "--"}, paths...)
	}
	if _, err := r.Git(ctx, args...); err != nil {
		return "", err
	}
	// Naming an ignored directory in an exclude pathspec makes git complain, so
	// the personal directory is unstaged after the fact instead (a no-op when git
	// already ignores it, which is the normal case).
	if _, err := r.Git(ctx, "reset", "-q", "--", LocalDir); err != nil {
		return "", err
	}
	staged, err := r.Git(ctx, "diff", "--cached", "--name-only")
	if err != nil {
		return "", err
	}
	if staged == "" {
		return "", ErrNothingToCommit
	}
	if _, err := r.Git(ctx, "commit", "-m", strings.TrimSpace(message)); err != nil {
		return "", err
	}
	return r.Git(ctx, "rev-parse", "HEAD")
}

func inLocalDir(p string) bool {
	c := path.Clean(strings.ReplaceAll(p, "\\", "/"))
	return c == LocalDir || strings.HasPrefix(c, LocalDir+"/")
}

// PushTask publishes branch (the current one when empty) to origin, never forced
// and never the base branch. Gated by permissions.push. It returns the branch.
func (r *Repo) PushTask(ctx context.Context, branch string) (string, error) {
	base, err := r.BaseBranch(ctx)
	if err != nil {
		return "", err
	}
	if branch == "" {
		if branch, err = r.CurrentBranch(ctx); err != nil {
			return "", err
		}
	}
	if branch == base {
		return "", fmt.Errorf("%w (%s)", ErrOnBaseBranch, base)
	}
	return branch, r.Push(ctx, branch)
}

// PRFor builds the pull request of the current branch from the settings: the base
// is the flag, else base_branch, else the repository default (empty lets the
// platform choose); draft is the flag, else draft_prs.
func (r *Repo) PRFor(ctx context.Context, title, body, base string, draft *bool) (PRRequest, error) {
	head, err := r.notBase(ctx)
	if err != nil {
		return PRRequest{}, err
	}
	if strings.TrimSpace(base) == "" {
		base, _ = r.BaseBranch(ctx)
	}
	d := r.Config.DraftPRs
	if draft != nil {
		d = *draft
	}
	return PRRequest{Title: title, Body: body, Base: base, Head: head, Draft: d}, nil
}

// Waiter polls the checks of a pull request. The zero value uses the real clock,
// a 15 second interval and context-aware sleeping; tests inject their own.
type Waiter struct {
	Now   func() time.Time
	Sleep func(ctx context.Context, d time.Duration) error
	Every time.Duration
}

func (w Waiter) now() time.Time {
	if w.Now == nil {
		return time.Now()
	}
	return w.Now()
}

func (w Waiter) sleep(ctx context.Context, d time.Duration) error {
	if w.Sleep != nil {
		return w.Sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// DefaultPollEvery is how often the checks are read while waiting.
const DefaultPollEvery = 15 * time.Second

// WaitForChecks returns the checks once they pass (or there are none), a
// *ChecksError when they fail, or a *ChecksError (pending) when they are still
// running after wait. A wait of zero reads them once.
func (w Waiter) WaitForChecks(ctx context.Context, h Host, dir string, pr PR, wait time.Duration) (Checks, error) {
	every := w.Every
	if every <= 0 {
		every = DefaultPollEvery
	}
	start := w.now()
	for {
		c, err := h.ChecksState(ctx, dir, pr)
		if err != nil {
			return c, err
		}
		switch c.State {
		case ChecksPassing, ChecksNone:
			return c, nil
		case ChecksFailing:
			return c, &ChecksError{State: ChecksFailing, Detail: c.Detail, Waited: w.now().Sub(start)}
		}
		left := wait - w.now().Sub(start)
		if left <= 0 {
			return c, &ChecksError{State: ChecksPending, Detail: c.Detail, Waited: wait}
		}
		if err := w.sleep(ctx, min(every, left)); err != nil {
			return c, err
		}
	}
}

// MergeWhenGreen merges pr with the merge method and branch cleanup from vcs.md,
// but only once the checks pass (or there are none). The merge permission is
// consulted first, so a denied merge never waits and an "ask" is answered once.
func (r *Repo) MergeWhenGreen(ctx context.Context, h Host, w Waiter, pr PR, wait time.Duration) (Checks, error) {
	opts := r.MergeDefaults()
	if err := checkMergeMethod(opts.Method); err != nil {
		return Checks{}, err
	}
	if err := Gate(r.Config.Permissions, OpMerge, "merge pull request "+pr.URL, r.Confirm); err != nil {
		return Checks{}, err
	}
	c, err := w.WaitForChecks(ctx, h, r.Dir, pr, wait)
	if err != nil {
		return c, err
	}
	granted := *r // the question was answered above: do not ask a second time
	granted.Config.Permissions.Merge = gabitotl.PermAllow
	return c, granted.MergePR(ctx, h, pr, opts)
}

// FindOpenPR returns the open pull request of head, or ErrNoPR.
func (r *Repo) FindOpenPR(ctx context.Context, h Host, head string) (PR, error) {
	f, ok := h.(PRFinder)
	if !ok {
		return PR{}, fmt.Errorf("%w: %s cannot look up pull requests; pass --pr", ErrUnsupported, h.Name())
	}
	pr, found, err := f.FindPR(ctx, r.Dir, head)
	if err != nil {
		return PR{}, err
	}
	if !found {
		return PR{}, fmt.Errorf("%w %s", ErrNoPR, head)
	}
	return pr, nil
}

// IsPublished reports whether the local tip of branch is exactly what origin has,
// so deleting the local branch after a merge cannot lose work.
func (r *Repo) IsPublished(ctx context.Context, branch string) bool {
	local, err := r.Git(ctx, "rev-parse", "--verify", "-q", "refs/heads/"+branch)
	if err != nil {
		return false
	}
	remote, err := r.Git(ctx, "rev-parse", "--verify", "-q", "refs/remotes/origin/"+branch)
	return err == nil && local == remote
}

// Note is something worth telling after a step that is not an error; Code is a
// stable identifier the presenter words.
type Note struct {
	Code string
	Args map[string]string
}

// AfterMerge brings the local clone up to date once branch was merged: fetch,
// then (when HEAD is on the merged branch) back to the base branch with a
// fast-forward pull, then delete the local branch if vcs.md says so and it was
// fully published before the merge. Anything that cannot be done safely is
// skipped and reported as a note; it never fails the merge that already happened.
func (r *Repo) AfterMerge(ctx context.Context, branch string, published bool) []Note {
	var notes []Note
	if _, err := r.Git(ctx, "fetch", "origin", "--prune"); err != nil {
		notes = append(notes, Note{"fetch-failed", nil})
	}
	base, err := r.BaseBranch(ctx)
	if err != nil {
		return append(notes, Note{"no-base", nil})
	}
	cur, _ := r.CurrentBranch(ctx)
	if branch != "" && cur == branch {
		if _, err := r.Git(ctx, "switch", base); err != nil {
			return append(notes, Note{"switch-failed", map[string]string{"base": base}})
		}
		if _, err := r.Git(ctx, "pull", "--ff-only", "origin", base); err != nil {
			notes = append(notes, Note{"pull-failed", map[string]string{"base": base}})
		} else {
			notes = append(notes, Note{"pulled", map[string]string{"base": base}})
		}
	}
	if branch == "" || branch == base || !r.Config.DeleteBranch {
		return notes
	}
	if !published {
		return append(notes, Note{"kept-unpublished", map[string]string{"branch": branch}})
	}
	// The host merged the published tip, so the local copy holds nothing else;
	// -D is needed because a squash leaves the branch looking unmerged.
	if _, err := r.Git(ctx, "branch", "-D", branch); err != nil {
		return append(notes, Note{"kept", map[string]string{"branch": branch}})
	}
	return append(notes, Note{"deleted", map[string]string{"branch": branch}})
}
