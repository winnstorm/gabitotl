package vcs

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

// GitHub drives GitHub with the `gh` CLI, which owns authentication ("native"
// auth: GabitoTL never sees a token).
type GitHub struct {
	Runner Runner
}

// Name implements PRProvider.
func (GitHub) Name() string { return "github" }

func (g GitHub) run(ctx context.Context, dir string, args ...string) (string, error) {
	return runnerOr(g.Runner).Run(ctx, dir, "gh", args...)
}

// existing returns the open PR of the head branch (the current branch when head
// is empty). Any failure other than a missing gh reads as "none": creating then
// reports the real problem.
func (g GitHub) existing(ctx context.Context, dir, head string) (PR, bool, error) {
	var out string
	var err error
	if head == "" {
		out, err = g.run(ctx, dir, "pr", "view", "--json", "number,url,state")
		if err != nil {
			if toolMissing(err) {
				return PR{}, false, err
			}
			return PR{}, false, nil
		}
		var v struct {
			URL   string `json:"url"`
			State string `json:"state"`
		}
		if json.Unmarshal([]byte(out), &v) != nil || v.URL == "" || !strings.EqualFold(v.State, "OPEN") {
			return PR{}, false, nil
		}
		return newPR(v.URL), true, nil
	}
	out, err = g.run(ctx, dir, "pr", "list", "--head", head, "--state", "open", "--json", "number,url", "--limit", "1")
	if err != nil {
		if toolMissing(err) {
			return PR{}, false, err
		}
		return PR{}, false, nil
	}
	var list []struct {
		URL string `json:"url"`
	}
	if json.Unmarshal([]byte(out), &list) != nil || len(list) == 0 || list[0].URL == "" {
		return PR{}, false, nil
	}
	return newPR(list[0].URL), true, nil
}

// CreatePR implements PRProvider. It is idempotent: an open PR of the head
// branch is returned instead of opening a second one.
func (g GitHub) CreatePR(ctx context.Context, dir string, req PRRequest) (PR, error) {
	if err := checkRequest(req); err != nil {
		return PR{}, err
	}
	if pr, ok, err := g.existing(ctx, dir, req.Head); err != nil || ok {
		return pr, err
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
	out, err := g.run(ctx, dir, args...)
	if err != nil {
		return PR{}, err
	}
	// gh prints the PR URL as its last line.
	lines := strings.Split(strings.TrimSpace(out), "\n")
	return newPR(strings.TrimSpace(lines[len(lines)-1])), nil
}

// MergePR implements Host.
func (g GitHub) MergePR(ctx context.Context, dir string, pr PR, opts MergeOptions) error {
	if err := checkMergeMethod(opts.Method); err != nil {
		return err
	}
	ref, err := prRef(pr)
	if err != nil {
		return err
	}
	args := []string{"pr", "merge", ref, "--" + opts.Method}
	if opts.DeleteBranch {
		args = append(args, "--delete-branch")
	}
	_, err = g.run(ctx, dir, args...)
	return err
}

// SetBase implements Host.
func (g GitHub) SetBase(ctx context.Context, dir string, pr PR, newBase string) error {
	ref, err := prRef(pr)
	if err != nil {
		return err
	}
	_, err = g.run(ctx, dir, "pr", "edit", ref, "--base", newBase)
	return err
}

// ChecksState implements Host. `gh pr checks` exits non-zero while checks are
// failing or pending but still prints the JSON, so the output is read first.
func (g GitHub) ChecksState(ctx context.Context, dir string, pr PR) (Checks, error) {
	ref, err := prRef(pr)
	if err != nil {
		return Checks{}, err
	}
	out, runErr := g.run(ctx, dir, "pr", "checks", ref, "--json", "bucket,name,state")
	var list []struct {
		Name   string `json:"name"`
		Bucket string `json:"bucket"`
	}
	if json.Unmarshal([]byte(out), &list) != nil {
		var ce *CommandError
		if errors.As(runErr, &ce) && strings.Contains(strings.ToLower(ce.Stderr), "no checks reported") {
			return Checks{State: ChecksNone}, nil
		}
		if runErr != nil {
			return Checks{}, runErr
		}
		return Checks{}, errBadOutput("gh pr checks")
	}
	var failing, pending []string
	for _, c := range list {
		switch strings.ToLower(c.Bucket) {
		case "fail", "cancel":
			failing = append(failing, c.Name)
		case "pending":
			pending = append(pending, c.Name)
		}
	}
	return combineChecks(len(list), failing, pending), nil
}

// AuthStatus implements Host.
func (g GitHub) AuthStatus(ctx context.Context, dir string) (AuthState, error) {
	return cliAuth(ctx, g.Runner, dir, "github", "gh", "https://cli.github.com/")
}
