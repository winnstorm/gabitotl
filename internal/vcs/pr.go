package vcs

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// PRRequest describes a pull request to open.
type PRRequest struct {
	Title string
	Body  string
	Base  string // target branch; empty means the repository default
	Head  string // source branch; empty means the current branch
	Draft bool
}

// PR is a created pull request.
type PR struct {
	URL string `json:"url"`
}

// PRProvider opens pull requests (or merge requests) on a hosting provider.
type PRProvider interface {
	Name() string
	CreatePR(ctx context.Context, dir string, req PRRequest) (PR, error)
}

// GitHub opens pull requests with the `gh` CLI, which owns authentication
// ("native" auth: GabitoTL never sees a token).
type GitHub struct {
	Runner Runner
}

// Name implements PRProvider.
func (GitHub) Name() string { return "github" }

// CreatePR implements PRProvider.
func (g GitHub) CreatePR(ctx context.Context, dir string, req PRRequest) (PR, error) {
	if strings.TrimSpace(req.Title) == "" {
		return PR{}, errors.New("vcs: pull request title is required")
	}
	if err := ValidateNoAttribution(req.Title + "\n" + req.Body); err != nil {
		return PR{}, err
	}
	run := g.Runner
	if run == nil {
		run = ExecRunner{}
	}
	args := []string{"pr", "create", "--title", req.Title, "--body", req.Body}
	if req.Base != "" {
		args = append(args, "--base", req.Base)
	}
	if req.Head != "" {
		args = append(args, "--head", req.Head)
	}
	if req.Draft {
		args = append(args, "--draft")
	}
	out, err := run.Run(ctx, dir, "gh", args...)
	if err != nil {
		return PR{}, err
	}
	// gh prints the PR URL as its last line.
	lines := strings.Split(strings.TrimSpace(out), "\n")
	return PR{URL: strings.TrimSpace(lines[len(lines)-1])}, nil
}

// Bitbucket is a documented stub. The spec lists Bitbucket as a first-wave
// provider; the REST/CLI wiring is deferred until the auth reference model for
// it is decided. It always returns ErrNotImplemented.
type Bitbucket struct{}

// Name implements PRProvider.
func (Bitbucket) Name() string { return "bitbucket" }

// CreatePR implements PRProvider.
func (Bitbucket) CreatePR(context.Context, string, PRRequest) (PR, error) {
	return PR{}, fmt.Errorf("%w: bitbucket pull requests are not wired yet", ErrNotImplemented)
}

// ProviderFor returns the PR provider for a vcs.md provider value.
// GitLab, Azure DevOps, generic git and local-only have no PR support in V1.
func ProviderFor(name string, run Runner) (PRProvider, error) {
	switch name {
	case "github":
		return GitHub{Runner: run}, nil
	case "bitbucket":
		return Bitbucket{}, nil
	}
	return nil, fmt.Errorf("%w: no pull request support for provider %q", ErrNotImplemented, name)
}

// OpenPR opens a pull request through prov. Gated by permissions.pull_requests.
func (r *Repo) OpenPR(ctx context.Context, prov PRProvider, req PRRequest) (PR, error) {
	if err := Gate(r.Config.Permissions, OpPullRequest, "open pull request: "+req.Title, r.Confirm); err != nil {
		return PR{}, err
	}
	return prov.CreatePR(ctx, r.Dir, req)
}
