package vcs

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/winnstorm/gabitotl/internal/branding"
	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/vcshosts"
)

// Repo is a Git work tree plus the permissions from vcs.md.
type Repo struct {
	Dir    string
	Runner Runner
	Config gabitotl.VCSConfig
	// Confirm answers "ask" permissions; nil means "ask" never proceeds.
	Confirm Confirm
	// BranchPrefix is prepended to task branches (default "feature").
	BranchPrefix string
	// Branding is the authorship guard policy (workflow.md); the zero value
	// refuses attribution (block).
	Branding BrandingPolicy
	// Servers is the registry of self-hosted servers used to infer the provider; may be nil.
	Servers vcshosts.Registry
	// OnBranding is told what the guard removed (strip) or only reported (warn),
	// so nothing is ever changed silently. May be nil.
	OnBranding func(op string, removed, warned []branding.Finding)
}

// New returns a Repo using the real git/gh executables.
func New(dir string, cfg gabitotl.VCSConfig) *Repo {
	return &Repo{Dir: dir, Runner: ExecRunner{}, Config: cfg}
}

func (r *Repo) servers() vcshosts.Registry {
	if r.Servers != nil {
		return r.Servers
	}
	return defaultServers()
}

func (r *Repo) runner() Runner {
	if r.Runner == nil {
		return ExecRunner{}
	}
	return r.Runner
}

// Git runs a git command in the repo. Commands that would rewrite history are
// rejected with ErrHistoryRewrite before they reach the runner.
func (r *Repo) Git(ctx context.Context, args ...string) (string, error) {
	if err := CheckHistorySafe(args); err != nil {
		return "", err
	}
	return r.runner().Run(ctx, r.Dir, "git", args...)
}

// Status is a read-only snapshot of the repository.
type Status struct {
	IsRepo   bool   `json:"is_repo"`
	Root     string `json:"root,omitempty"`
	Branch   string `json:"branch,omitempty"` // empty when detached or no commits
	Head     string `json:"head,omitempty"`   // empty when there are no commits
	Remote   string `json:"remote,omitempty"` // origin URL
	Provider string `json:"provider"`
	Dirty    bool   `json:"dirty"`
}

// Status inspects the repository. A directory outside a work tree yields
// IsRepo=false with a nil error.
func (r *Repo) Status(ctx context.Context) (Status, error) {
	top, err := r.Git(ctx, "rev-parse", "--show-toplevel")
	if err != nil {
		var ce *CommandError
		if errors.As(err, &ce) {
			return Status{Provider: "local"}, nil
		}
		return Status{}, err
	}
	st := Status{IsRepo: true, Root: top}
	st.Branch, _ = r.Git(ctx, "symbolic-ref", "--short", "-q", "HEAD")
	st.Head, _ = r.Git(ctx, "rev-parse", "--verify", "-q", "HEAD")
	st.Remote, _ = r.Git(ctx, "remote", "get-url", "origin")
	st.Provider = InferProviderWith(st.Remote, r.servers())
	porcelain, _ := r.Git(ctx, "status", "--porcelain")
	st.Dirty = porcelain != ""
	return st, nil
}

// InferProvider maps a remote URL to a vcs.md provider value. No remote means
// "local"; an unknown host means generic "git".
func InferProvider(remote string) string {
	remote = strings.TrimSpace(remote)
	if remote == "" {
		return "local"
	}
	host := remoteHost(remote)
	switch {
	case host == "github.com" || strings.HasSuffix(host, ".github.com"):
		return "github"
	case host == "bitbucket.org" || strings.HasSuffix(host, ".bitbucket.org"):
		return "bitbucket"
	case host == "gitlab.com" || strings.HasPrefix(host, "gitlab."):
		return "gitlab"
	case host == "dev.azure.com" || host == "ssh.dev.azure.com" || strings.HasSuffix(host, ".visualstudio.com"):
		return "azure-devops"
	}
	return "git"
}

// InferProviderWith is InferProvider that first consults the registry of
// self-hosted servers: a remote on a registered host takes the registered platform.
func InferProviderWith(remote string, reg vcshosts.Registry) string {
	if e, ok := vcshosts.Lookup(reg, remoteHost(strings.TrimSpace(remote))); ok {
		return e.Platform
	}
	return InferProvider(remote)
}

func remoteHost(remote string) string {
	if strings.Contains(remote, "://") {
		if u, err := url.Parse(remote); err == nil {
			return strings.ToLower(u.Hostname())
		}
	}
	// scp-like: user@host:path
	rest := remote
	if i := strings.IndexByte(rest, '@'); i >= 0 {
		rest = rest[i+1:]
	}
	if i := strings.IndexByte(rest, ':'); i >= 0 {
		rest = rest[:i]
	}
	return strings.ToLower(rest)
}

// TaskBranchName returns the branch for a task slug, e.g. feature/auth-login.
func (r *Repo) TaskBranchName(task string) (string, error) {
	if !gabitotl.ValidID(task) {
		return "", gabitotl.ErrInvalidID
	}
	prefix := r.BranchPrefix
	if prefix == "" {
		prefix = "feature"
	}
	return prefix + "/" + task, nil
}

// CreateTaskBranch creates and switches to the task branch. Gated by
// permissions.branches.
func (r *Repo) CreateTaskBranch(ctx context.Context, task string) (string, error) {
	name, err := r.TaskBranchName(task)
	if err != nil {
		return "", err
	}
	if err := Gate(r.Config.Permissions, OpBranch, "create branch "+name, r.Confirm); err != nil {
		return "", err
	}
	if _, err := r.Git(ctx, "rev-parse", "--verify", "-q", "refs/heads/"+name); err == nil {
		return "", fmt.Errorf("%w: %s", ErrBranchExists, name)
	}
	if _, err := r.Git(ctx, "switch", "-c", name); err != nil {
		return "", err
	}
	return name, nil
}

// Commit stages paths (when given) and commits staged work as one logical
// unit. The message must be a Conventional Commit without attribution
// trailers; none is ever added. The authorship guard (Branding) strips or
// refuses LLM attribution first. Gated by permissions.commits.
func (r *Repo) Commit(ctx context.Context, message string, paths ...string) (string, error) {
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
	if len(paths) > 0 {
		args := append([]string{"add", "--"}, paths...)
		if _, err := r.Git(ctx, args...); err != nil {
			return "", err
		}
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

// Push publishes branch to origin (never forced). Gated by permissions.push.
func (r *Repo) Push(ctx context.Context, branch string) error {
	if strings.TrimSpace(branch) == "" {
		return errors.New("vcs: branch is required")
	}
	if remote, _ := r.Git(ctx, "remote", "get-url", "origin"); remote == "" {
		return ErrNoRemote
	}
	if err := Gate(r.Config.Permissions, OpPush, "push "+branch+" to origin", r.Confirm); err != nil {
		return err
	}
	_, err := r.Git(ctx, "push", "-u", "origin", "--", branch)
	return err
}

// IsShared reports whether commit is already reachable from a remote branch.
func (r *Repo) IsShared(ctx context.Context, commit string) (bool, error) {
	out, err := r.Git(ctx, "branch", "-r", "--contains", commit)
	if err != nil {
		return false, err
	}
	return out != "", nil
}

// RevertResult describes a rollback.
type RevertResult struct {
	Commit   string // the new revert commit
	Reverted string // the commit that was reverted
	Shared   bool   // the reverted commit was already on a remote
}

// Revert rolls a commit back by creating a new commit that undoes it. This is
// the only rollback GabitoTL performs, and it is the right one for shared
// changes: history is never rewritten. On conflict the revert is aborted and
// the work tree is left as it was. Gated by permissions.commits.
func (r *Repo) Revert(ctx context.Context, commit string) (RevertResult, error) {
	if strings.TrimSpace(commit) == "" || strings.HasPrefix(commit, "-") {
		return RevertResult{}, errors.New("vcs: a commit id is required")
	}
	if err := Gate(r.Config.Permissions, OpCommit, "revert "+commit, r.Confirm); err != nil {
		return RevertResult{}, err
	}
	full, err := r.Git(ctx, "rev-parse", "--verify", "-q", commit+"^{commit}")
	if err != nil {
		return RevertResult{}, fmt.Errorf("vcs: unknown commit %q: %w", commit, err)
	}
	shared, err := r.IsShared(ctx, full)
	if err != nil {
		return RevertResult{}, err
	}
	subject, _ := r.Git(ctx, "log", "-1", "--format=%s", full)
	if _, err := r.Git(ctx, "revert", "--no-commit", full); err != nil {
		_, _ = r.Git(ctx, "revert", "--abort")
		return RevertResult{}, fmt.Errorf("vcs: revert of %s failed and was aborted: %w", short(full), err)
	}
	msg := fmt.Sprintf("revert: %s\n\nThis reverts commit %s.", subject, full)
	if _, err := r.Git(ctx, "commit", "-m", msg); err != nil {
		_, _ = r.Git(ctx, "revert", "--abort")
		return RevertResult{}, err
	}
	head, err := r.Git(ctx, "rev-parse", "HEAD")
	return RevertResult{Commit: head, Reverted: full, Shared: shared}, err
}

func short(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	return s
}

// RemoteHost is the lower-case host of a Git remote URL (https or scp-like),
// without credentials or port; "" when it cannot be read.
func RemoteHost(remote string) string { return remoteHost(strings.TrimSpace(remote)) }
