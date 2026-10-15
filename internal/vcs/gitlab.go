package vcs

import (
	"context"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

// GitLab drives GitLab merge requests with the `glab` CLI, which owns
// authentication. Self-managed instances work because glab reads the host from
// the repository's remote.
type GitLab struct {
	Runner Runner
}

// Name implements PRProvider.
func (GitLab) Name() string { return "gitlab" }

func (g GitLab) run(ctx context.Context, dir string, args ...string) (string, error) {
	return runnerOr(g.Runner).Run(ctx, dir, "glab", args...)
}

var mrURL = regexp.MustCompile(`https?://\S+/merge_requests/\d+`)

// mrView is the part of `glab mr view --output json` GabitoTL reads.
type mrView struct {
	IID          int    `json:"iid"`
	WebURL       string `json:"web_url"`
	State        string `json:"state"`
	HeadPipeline *struct {
		Status string `json:"status"`
	} `json:"head_pipeline"`
	Pipeline *struct {
		Status string `json:"status"`
	} `json:"pipeline"`
}

func (v mrView) pr() PR {
	pr := newPR(v.WebURL)
	if v.IID > 0 {
		pr.Number, pr.ID = v.IID, strconv.Itoa(v.IID)
	}
	return pr
}

func (g GitLab) view(ctx context.Context, dir, ref string) (mrView, error) {
	args := []string{"mr", "view"}
	if ref != "" {
		args = append(args, ref)
	}
	out, err := g.run(ctx, dir, append(args, "--output", "json")...)
	if err != nil {
		return mrView{}, err
	}
	var v mrView
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		return mrView{}, errBadOutput("glab mr view")
	}
	return v, nil
}

// CreatePR implements PRProvider. It is idempotent: an open merge request of the
// source branch is returned instead of opening a second one.
func (g GitLab) CreatePR(ctx context.Context, dir string, req PRRequest) (PR, error) {
	if err := checkRequest(req); err != nil {
		return PR{}, err
	}
	switch v, err := g.view(ctx, dir, req.Head); {
	case err != nil && toolMissing(err):
		return PR{}, err
	case err == nil && v.State == "opened" && v.WebURL != "":
		return v.pr(), nil
	}
	args := []string{"mr", "create", "--title", req.Title, "--description", req.Body, "--yes"}
	if req.Base != "" {
		args = append(args, "--target-branch", req.Base)
	}
	if req.Head != "" {
		args = append(args, "--source-branch", req.Head)
	}
	if req.Draft {
		args = append(args, "--draft")
	}
	out, err := g.run(ctx, dir, args...)
	if err != nil {
		return PR{}, err
	}
	if u := mrURL.FindString(out); u != "" {
		return newPR(u), nil
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	return newPR(strings.TrimSpace(lines[len(lines)-1])), nil
}

// MergePR implements Host. glab has no flag for a plain merge, so "merge" passes
// none.
func (g GitLab) MergePR(ctx context.Context, dir string, pr PR, opts MergeOptions) error {
	if err := checkMergeMethod(opts.Method); err != nil {
		return err
	}
	id, err := prID(pr)
	if err != nil {
		return err
	}
	args := []string{"mr", "merge", id}
	switch opts.Method {
	case "squash":
		args = append(args, "--squash")
	case "rebase":
		args = append(args, "--rebase")
	}
	if opts.DeleteBranch {
		args = append(args, "--remove-source-branch")
	}
	_, err = g.run(ctx, dir, append(args, "--yes")...)
	return err
}

// SetBase implements Host.
func (g GitLab) SetBase(ctx context.Context, dir string, pr PR, newBase string) error {
	id, err := prID(pr)
	if err != nil {
		return err
	}
	_, err = g.run(ctx, dir, "mr", "update", id, "--target-branch", newBase)
	return err
}

// ChecksState implements Host from the pipeline of the merge request.
func (g GitLab) ChecksState(ctx context.Context, dir string, pr PR) (Checks, error) {
	id, err := prID(pr)
	if err != nil {
		return Checks{}, err
	}
	v, err := g.view(ctx, dir, id)
	if err != nil {
		return Checks{}, err
	}
	p := v.HeadPipeline
	if p == nil {
		p = v.Pipeline
	}
	if p == nil || p.Status == "" {
		return Checks{State: ChecksNone}, nil
	}
	switch p.Status {
	case "success", "skipped":
		return Checks{State: ChecksPassing, Detail: p.Status}, nil
	case "failed", "canceled", "canceling":
		return Checks{State: ChecksFailing, Detail: p.Status}, nil
	}
	return Checks{State: ChecksPending, Detail: p.Status}, nil
}

// AuthStatus implements Host.
func (g GitLab) AuthStatus(ctx context.Context, dir string) (AuthState, error) {
	return cliAuth(ctx, g.Runner, dir, "gitlab", "glab", "https://gitlab.com/gitlab-org/cli#installation")
}
