package vcs

import "context"

// FindPR implements PRFinder: the open PR of the head branch, if any.
func (g GitHub) FindPR(ctx context.Context, dir, head string) (PR, bool, error) {
	return g.existing(ctx, dir, head)
}

// FindPR implements PRFinder: the open merge request of the source branch, if any.
func (g GitLab) FindPR(ctx context.Context, dir, head string) (PR, bool, error) {
	v, err := g.view(ctx, dir, head)
	switch {
	case err != nil && toolMissing(err):
		return PR{}, false, err
	case err != nil || v.State != "opened" || v.WebURL == "":
		return PR{}, false, nil
	}
	return v.pr(), true, nil
}
