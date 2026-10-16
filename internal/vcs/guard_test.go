package vcs

import (
	"errors"
	"testing"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
)

func TestGateMergeFollowsItsPermission(t *testing.T) {
	asks := 0
	confirm := func(op, detail string) bool {
		asks++
		return op == OpMerge && detail == "PR 7"
	}
	cases := []struct {
		name    string
		merge   string
		confirm Confirm
		want    error
		asked   int
	}{
		{"allow proceeds without asking", gabitotl.PermAllow, confirm, nil, 0},
		{"ask consults confirm and proceeds", gabitotl.PermAsk, confirm, nil, 1},
		{"ask without confirm needs confirmation", gabitotl.PermAsk, nil, ErrNeedsConfirmation, 0},
		{"missing key reads as ask", "", nil, ErrNeedsConfirmation, 0},
		{"deny is final", gabitotl.PermDeny, confirm, ErrDenied, 0},
	}
	for _, c := range cases {
		asks = 0
		p := gabitotl.VCSPermissions{Branches: "allow", Commits: "allow", Push: "allow", PullRequests: "allow", Merge: c.merge}
		err := Gate(p, OpMerge, "PR 7", c.confirm)
		if !errors.Is(err, c.want) && !(c.want == nil && err == nil) {
			t.Errorf("%s: err = %v, want %v", c.name, err, c.want)
		}
		if asks != c.asked {
			t.Errorf("%s: asked %d times, want %d", c.name, asks, c.asked)
		}
	}
}

func TestGateMergeIsIndependentFromThePullRequestPermission(t *testing.T) {
	p := gabitotl.VCSPermissions{Branches: "allow", Commits: "allow", Push: "allow", PullRequests: "allow", Merge: "ask"}
	if err := Gate(p, OpPullRequest, "", nil); err != nil {
		t.Fatalf("opening the PR is allowed: %v", err)
	}
	if err := Gate(p, OpMerge, "", nil); !errors.Is(err, ErrNeedsConfirmation) {
		t.Fatalf("merging still asks: %v", err)
	}
}
