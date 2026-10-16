package vcs

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
)

// scripted is a Runner that answers from a function and records every call.
type scripted struct {
	calls  [][]string
	handle func(name string, args []string) (string, error)
}

func (s *scripted) Run(_ context.Context, _ string, name string, args ...string) (string, error) {
	s.calls = append(s.calls, append([]string{name}, args...))
	return s.handle(name, args)
}

func (s *scripted) last() string { return strings.Join(s.calls[len(s.calls)-1], " ") }

func (s *scripted) joined() []string {
	var out []string
	for _, c := range s.calls {
		out = append(out, strings.Join(c, " "))
	}
	return out
}

func notFound() error { return &CommandError{Cmd: "x", Err: exec.ErrNotFound} }

func has(t *testing.T, got string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("%q lacks %q", got, w)
		}
	}
}

func TestGitHubCreateReturnsAnExistingOpenPR(t *testing.T) {
	s := &scripted{handle: func(_ string, args []string) (string, error) {
		return `[{"number":7,"url":"https://github.com/acme/app/pull/7"}]`, nil
	}}
	pr, err := GitHub{Runner: s}.CreatePR(context.Background(), "", PRRequest{Title: "feat: x", Head: "feature/x"})
	if err != nil || pr.URL != "https://github.com/acme/app/pull/7" || pr.Number != 7 || pr.ID != "7" {
		t.Fatalf("%+v %v", pr, err)
	}
	if len(s.calls) != 1 || s.last() != "gh pr list --head feature/x --state open --json number,url --limit 1" {
		t.Fatalf("calls = %v", s.joined())
	}
	// On the current branch the lookup is `pr view`; a closed PR does not count.
	s2 := &scripted{handle: func(_ string, args []string) (string, error) {
		if args[1] == "view" {
			return `{"number":3,"url":"https://github.com/acme/app/pull/3","state":"CLOSED"}`, nil
		}
		return "https://github.com/acme/app/pull/9", nil
	}}
	pr, err = GitHub{Runner: s2}.CreatePR(context.Background(), "", PRRequest{Title: "feat: x"})
	if err != nil || pr.Number != 9 {
		t.Fatalf("%+v %v", pr, err)
	}
	if got := s2.joined(); got[0] != "gh pr view --json number,url,state" || !strings.HasPrefix(got[1], "gh pr create") {
		t.Fatalf("calls = %v", got)
	}
}

func TestGitHubCreateArgumentVectorAndGuards(t *testing.T) {
	s := &scripted{handle: func(_ string, args []string) (string, error) {
		if args[1] == "list" {
			return "[]", nil
		}
		return "Creating...\nhttps://github.com/acme/app/pull/12\n", nil
	}}
	pr, err := GitHub{Runner: s}.CreatePR(context.Background(), "", PRRequest{Title: "feat: x", Body: "b", Base: "main", Head: "feature/x", Draft: true})
	if err != nil || pr.Number != 12 {
		t.Fatalf("%+v %v", pr, err)
	}
	if want := "gh pr create --title feat: x --body b --base main --head feature/x --draft"; s.last() != want {
		t.Fatalf("got %q want %q", s.last(), want)
	}
	n := len(s.calls)
	if _, err := (GitHub{Runner: s}).CreatePR(context.Background(), "", PRRequest{Title: " "}); err == nil {
		t.Fatal("an empty title must fail")
	}
	if _, err := (GitHub{Runner: s}).CreatePR(context.Background(), "", PRRequest{Title: "x", Body: "Co-Authored-By: A <a@b.c>"}); !errors.Is(err, ErrAttribution) {
		t.Fatalf("attribution must be refused: %v", err)
	}
	if len(s.calls) != n {
		t.Fatal("a refused request must not reach gh")
	}
}

func TestGitHubMergeMethodsDeleteBranchAndRetarget(t *testing.T) {
	pr := PR{URL: "https://github.com/acme/app/pull/7", ID: "7", Number: 7}
	for method, flag := range map[string]string{"squash": "--squash", "merge": "--merge", "rebase": "--rebase"} {
		s := &scripted{handle: func(string, []string) (string, error) { return "", nil }}
		if err := (GitHub{Runner: s}).MergePR(context.Background(), "", pr, MergeOptions{Method: method}); err != nil {
			t.Fatal(err)
		}
		if want := "gh pr merge " + pr.URL + " " + flag; s.last() != want {
			t.Errorf("got %q want %q", s.last(), want)
		}
	}
	s := &scripted{handle: func(string, []string) (string, error) { return "", nil }}
	if err := (GitHub{Runner: s}).MergePR(context.Background(), "", pr, MergeOptions{Method: "squash", DeleteBranch: true}); err != nil || s.last() != "gh pr merge "+pr.URL+" --squash --delete-branch" {
		t.Fatalf("%q %v", s.last(), err)
	}
	if err := (GitHub{Runner: s}).MergePR(context.Background(), "", pr, MergeOptions{Method: "fast"}); err == nil {
		t.Fatal("an unknown method must be refused")
	}
	if err := (GitHub{Runner: s}).SetBase(context.Background(), "", pr, "main"); err != nil || s.last() != "gh pr edit "+pr.URL+" --base main" {
		t.Fatalf("%q %v", s.last(), err)
	}
}

func TestGitHubChecksStates(t *testing.T) {
	pr := PR{URL: "https://github.com/acme/app/pull/7"}
	cases := []struct {
		name, out string
		err       error
		want      string
	}{
		{"passing", `[{"name":"a","bucket":"pass"},{"name":"b","bucket":"skipping"}]`, nil, ChecksPassing},
		{"failing wins over pending", `[{"name":"a","bucket":"fail"},{"name":"b","bucket":"pending"}]`, &CommandError{Cmd: "gh", Err: errors.New("exit 1")}, ChecksFailing},
		{"pending", `[{"name":"a","bucket":"pending"}]`, &CommandError{Cmd: "gh", Err: errors.New("exit 8")}, ChecksPending},
		{"cancelled fails", `[{"name":"a","bucket":"cancel"}]`, nil, ChecksFailing},
		{"empty list", `[]`, nil, ChecksNone},
		{"no checks reported", "", &CommandError{Cmd: "gh", Err: errors.New("exit 1"), Stderr: "no checks reported on the 'x' branch"}, ChecksNone},
	}
	for _, c := range cases {
		s := &scripted{handle: func(string, []string) (string, error) { return c.out, c.err }}
		got, err := GitHub{Runner: s}.ChecksState(context.Background(), "", pr)
		if err != nil || got.State != c.want {
			t.Errorf("%s: %+v %v", c.name, got, err)
		}
		if s.last() != "gh pr checks "+pr.URL+" --json bucket,name,state" {
			t.Errorf("%s: %q", c.name, s.last())
		}
	}
	s := &scripted{handle: func(string, []string) (string, error) { return "", &CommandError{Cmd: "gh", Err: errors.New("boom")} }}
	if _, err := (GitHub{Runner: s}).ChecksState(context.Background(), "", pr); err == nil {
		t.Fatal("a real failure must surface")
	}
}

func TestAuthStatusOfTheCLIHosts(t *testing.T) {
	cases := []struct {
		host   func(Runner) Host
		tool   string
		url    string
		fixCmd string
	}{
		{func(r Runner) Host { return GitHub{Runner: r} }, "gh", "https://cli.github.com/", "gh auth login"},
		{func(r Runner) Host { return GitLab{Runner: r} }, "glab", "https://gitlab.com/gitlab-org/cli", "glab auth login"},
	}
	for _, c := range cases {
		ok := &scripted{handle: func(string, []string) (string, error) { return "", nil }}
		st, err := c.host(ok).AuthStatus(context.Background(), "")
		if err != nil || !st.OK() || ok.last() != c.tool+" auth status" || st.Command != "" {
			t.Errorf("%s logged in: %+v %v %q", c.tool, st, err, ok.last())
		}
		out := &scripted{handle: func(string, []string) (string, error) {
			return "", &CommandError{Cmd: c.tool, Err: errors.New("exit 1")}
		}}
		st, _ = c.host(out).AuthStatus(context.Background(), "")
		if st.Status != AuthNotLoggedIn || st.Command != c.fixCmd {
			t.Errorf("%s logged out: %+v", c.tool, st)
		}
		has(t, st.Fix, c.fixCmd)
		miss := &scripted{handle: func(string, []string) (string, error) { return "", notFound() }}
		st, _ = c.host(miss).AuthStatus(context.Background(), "")
		if st.Status != AuthToolMissing || !strings.HasPrefix(st.InstallURL, c.url) {
			t.Errorf("%s missing: %+v", c.tool, st)
		}
		has(t, st.Fix, c.url, c.fixCmd)
	}
}

const mrJSON = `{"iid":5,"web_url":"https://gitlab.com/acme/app/-/merge_requests/5","state":"opened","head_pipeline":{"status":"running"}}`

func TestGitLabCreateIsIdempotentAndBuildsTheVector(t *testing.T) {
	s := &scripted{handle: func(string, []string) (string, error) { return mrJSON, nil }}
	pr, err := GitLab{Runner: s}.CreatePR(context.Background(), "", PRRequest{Title: "feat: x", Head: "feature/x"})
	if err != nil || pr.Number != 5 || pr.ID != "5" || len(s.calls) != 1 || s.last() != "glab mr view feature/x --output json" {
		t.Fatalf("%+v %v %v", pr, err, s.joined())
	}
	s = &scripted{handle: func(_ string, args []string) (string, error) {
		if args[1] == "view" {
			return "", &CommandError{Cmd: "glab", Err: errors.New("exit 1"), Stderr: "no merge request found"}
		}
		return "Creating merge request for feature/x into main in acme/app\n\n!8 feat: x\nhttps://gitlab.com/acme/app/-/merge_requests/8\n", nil
	}}
	pr, err = GitLab{Runner: s}.CreatePR(context.Background(), "", PRRequest{Title: "feat: x", Body: "b", Base: "main", Head: "feature/x", Draft: true})
	if err != nil || pr.Number != 8 || pr.URL != "https://gitlab.com/acme/app/-/merge_requests/8" {
		t.Fatalf("%+v %v", pr, err)
	}
	if want := "glab mr create --title feat: x --description b --yes --target-branch main --source-branch feature/x --draft"; s.last() != want {
		t.Fatalf("got %q want %q", s.last(), want)
	}
	if _, err := (GitLab{Runner: s}).CreatePR(context.Background(), "", PRRequest{Title: "x", Body: "Generated " + "with Claude Code"}); !errors.Is(err, ErrAttribution) {
		t.Fatalf("%v", err)
	}
	gone := &scripted{handle: func(string, []string) (string, error) { return "", notFound() }}
	if _, err := (GitLab{Runner: gone}).CreatePR(context.Background(), "", PRRequest{Title: "feat: x"}); !toolMissing(err) {
		t.Fatalf("a missing glab must surface: %v", err)
	}
}

func TestGitLabMergeRetargetAndChecks(t *testing.T) {
	pr := PR{ID: "5", Number: 5, URL: "https://gitlab.com/acme/app/-/merge_requests/5"}
	want := map[string]string{
		"squash": "glab mr merge 5 --squash --yes",
		"merge":  "glab mr merge 5 --yes",
		"rebase": "glab mr merge 5 --rebase --yes",
	}
	for method, w := range want {
		s := &scripted{handle: func(string, []string) (string, error) { return "", nil }}
		if err := (GitLab{Runner: s}).MergePR(context.Background(), "", pr, MergeOptions{Method: method}); err != nil || s.last() != w {
			t.Errorf("%s: %q %v", method, s.last(), err)
		}
	}
	s := &scripted{handle: func(string, []string) (string, error) { return "", nil }}
	if err := (GitLab{Runner: s}).MergePR(context.Background(), "", pr, MergeOptions{Method: "squash", DeleteBranch: true}); err != nil || s.last() != "glab mr merge 5 --squash --remove-source-branch --yes" {
		t.Fatalf("%q %v", s.last(), err)
	}
	if err := (GitLab{Runner: s}).MergePR(context.Background(), "", pr, MergeOptions{Method: "x"}); err == nil {
		t.Fatal("an unknown method must be refused")
	}
	if err := (GitLab{Runner: s}).SetBase(context.Background(), "", pr, "main"); err != nil || s.last() != "glab mr update 5 --target-branch main" {
		t.Fatalf("%q %v", s.last(), err)
	}
	for status, state := range map[string]string{"success": ChecksPassing, "failed": ChecksFailing, "canceled": ChecksFailing, "running": ChecksPending, "pending": ChecksPending} {
		r := &scripted{handle: func(string, []string) (string, error) {
			return `{"iid":5,"web_url":"u","state":"opened","head_pipeline":{"status":"` + status + `"}}`, nil
		}}
		got, err := GitLab{Runner: r}.ChecksState(context.Background(), "", pr)
		if err != nil || got.State != state || r.last() != "glab mr view 5 --output json" {
			t.Errorf("%s: %+v %v %q", status, got, err, r.last())
		}
	}
	r := &scripted{handle: func(string, []string) (string, error) { return `{"iid":5,"web_url":"u","state":"opened"}`, nil }}
	if got, _ := (GitLab{Runner: r}).ChecksState(context.Background(), "", pr); got.State != ChecksNone {
		t.Fatalf("no pipeline is none: %+v", got)
	}
}

func TestRepoMergeIsGatedValidatedAndDefaultsFromTheConfig(t *testing.T) {
	pr := PR{URL: "https://github.com/acme/app/pull/7"}
	s := &scripted{handle: func(string, []string) (string, error) { return "", nil }}
	r := &Repo{Dir: t.TempDir(), Config: allowAll()}
	r.Config.MergeMethod, r.Config.DeleteBranch = gabitotl.MergeRebase, true
	r.Config.Permissions.Merge = gabitotl.PermAllow
	if err := r.MergePR(context.Background(), GitHub{Runner: s}, pr, MergeOptions{}); err != nil || s.last() != "gh pr merge "+pr.URL+" --rebase" {
		t.Fatalf("method from the config: %q %v", s.last(), err)
	}
	if err := r.MergePR(context.Background(), GitHub{Runner: s}, pr, r.MergeDefaults()); err != nil || !strings.HasSuffix(s.last(), "--rebase --delete-branch") {
		t.Fatalf("%q %v", s.last(), err)
	}
	n := len(s.calls)
	if err := r.MergePR(context.Background(), GitHub{Runner: s}, pr, MergeOptions{Method: "yolo"}); err == nil {
		t.Fatal("unknown method")
	}
	r.Config.Permissions.Merge = gabitotl.PermDeny
	if err := r.MergePR(context.Background(), GitHub{Runner: s}, pr, MergeOptions{Method: "squash"}); !errors.Is(err, ErrDenied) {
		t.Fatalf("merge denied: %v", err)
	}
	r.Config.Permissions.Merge = gabitotl.PermAsk
	if err := r.MergePR(context.Background(), GitHub{Runner: s}, pr, MergeOptions{Method: "squash"}); !errors.Is(err, ErrNeedsConfirmation) {
		t.Fatalf("merge ask: %v", err)
	}
	if len(s.calls) != n {
		t.Fatal("a gated merge must not reach the platform")
	}
	r.Config.Permissions.PullRequests = gabitotl.PermDeny
	if err := r.RetargetPR(context.Background(), GitHub{Runner: s}, pr, "main"); !errors.Is(err, ErrDenied) {
		t.Fatalf("retarget is gated by pull_requests: %v", err)
	}
}
