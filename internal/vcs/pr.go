package vcs

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
)

// PRRequest describes a pull request to open.
type PRRequest struct {
	Title string
	Body  string
	Base  string // target branch; empty means the repository default
	Head  string // source branch; empty means the current branch
	Draft bool
}

// PR is a created (or already open) pull request or merge request.
type PR struct {
	URL string `json:"url"`
	// ID is the platform's identifier: the number on GitHub, the iid on GitLab,
	// the id on Bitbucket. Number is the same value as an integer when numeric.
	ID     string `json:"id,omitempty"`
	Number int    `json:"number,omitempty"`
}

// PRProvider opens pull requests (or merge requests) on a hosting provider.
type PRProvider interface {
	Name() string
	CreatePR(ctx context.Context, dir string, req PRRequest) (PR, error)
}

// ProviderFor returns the PR provider for a vcs.md provider value. It builds the
// same hosts as HostFor; Azure DevOps, generic git and local-only have no PR
// support and fail with ErrUnsupported.
func ProviderFor(name string, run Runner) (PRProvider, error) {
	h, err := HostFor(name, HostOptions{Runner: run})
	if err != nil {
		return nil, err
	}
	return h, nil
}

// checkRequest is the guard every host applies before it talks to a platform.
func checkRequest(req PRRequest) error {
	if strings.TrimSpace(req.Title) == "" {
		return errors.New("vcs: pull request title is required")
	}
	return ValidateNoAttribution(req.Title + "\n" + req.Body)
}

// OpenPR opens a pull request through prov after the authorship guard has
// stripped or refused LLM attribution in the title and body. Gated by
// permissions.pull_requests.
func (r *Repo) OpenPR(ctx context.Context, prov PRProvider, req PRRequest) (PR, error) {
	if err := Gate(r.Config.Permissions, OpPullRequest, "open pull request: "+req.Title, r.Confirm); err != nil {
		return PR{}, err
	}
	var err error
	if req.Title, err = r.enforce(OpPullRequest, req.Title); err != nil {
		return PR{}, err
	}
	if req.Body, err = r.enforce(OpPullRequest, req.Body); err != nil {
		return PR{}, err
	}
	return prov.CreatePR(ctx, r.Dir, req)
}

// MergeDefaults are the merge options vcs.md asks for.
func (r *Repo) MergeDefaults() MergeOptions {
	return MergeOptions{Method: r.Config.EffectiveMergeMethod(), DeleteBranch: r.Config.DeleteBranch}
}

// MergePR merges pr through h. It is the only way the Repo merges and it is
// gated by permissions.merge; an unknown method is refused before any call.
func (r *Repo) MergePR(ctx context.Context, h Host, pr PR, opts MergeOptions) error {
	if opts.Method == "" {
		opts.Method = r.MergeDefaults().Method
	}
	if err := checkMergeMethod(opts.Method); err != nil {
		return err
	}
	if err := Gate(r.Config.Permissions, OpMerge, "merge pull request "+pr.URL, r.Confirm); err != nil {
		return err
	}
	return h.MergePR(ctx, r.Dir, pr, opts)
}

// RetargetPR points a stacked pull request at a new base branch (for example
// after its parent was merged). Gated by permissions.pull_requests.
func (r *Repo) RetargetPR(ctx context.Context, h Host, pr PR, newBase string) error {
	if strings.TrimSpace(newBase) == "" {
		return errors.New("vcs: the new base branch is required")
	}
	if err := Gate(r.Config.Permissions, OpPullRequest, "retarget pull request "+pr.URL+" to "+newBase, r.Confirm); err != nil {
		return err
	}
	return h.SetBase(ctx, r.Dir, pr, newBase)
}

func checkMergeMethod(m string) error {
	for _, ok := range gabitotl.MergeMethods {
		if m == ok {
			return nil
		}
	}
	return fmt.Errorf("vcs: unknown merge method %q (use squash, merge or rebase)", m)
}
