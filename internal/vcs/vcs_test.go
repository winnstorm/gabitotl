package vcs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
)

var testEnv = []string{
	"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=t@example.com",
	"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=t@example.com",
	"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
}

func allowAll() gabitotl.VCSConfig {
	c := gabitotl.DefaultVCS("git")
	c.Permissions = gabitotl.VCSPermissions{
		Branches: gabitotl.PermAllow, Commits: gabitotl.PermAllow,
		Push: gabitotl.PermAllow, PullRequests: gabitotl.PermAllow,
	}
	return c
}

// newRepo creates a temp git repo with one initial commit.
func newRepo(t *testing.T) *Repo {
	t.Helper()
	dir := t.TempDir()
	r := &Repo{Dir: dir, Runner: ExecRunner{Env: testEnv}, Config: allowAll()}
	ctx := context.Background()
	if _, err := r.Git(ctx, "init", "-q", "-b", "main"); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "a.txt", "one\n")
	if _, err := r.Commit(ctx, "chore: initial commit", "a.txt"); err != nil {
		t.Fatal(err)
	}
	return r
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestInferProvider(t *testing.T) {
	cases := map[string]string{
		"":                                "local",
		"git@github.com:acme/app.git":     "github",
		"https://github.com/acme/app.git": "github",
		"https://user@bitbucket.org/acme/app.git":  "bitbucket",
		"git@gitlab.com:acme/app.git":              "gitlab",
		"https://gitlab.acme.io/acme/app.git":      "gitlab",
		"https://dev.azure.com/acme/p/_git/app":    "azure-devops",
		"git@ssh.dev.azure.com:v3/acme/p/app":      "azure-devops",
		"https://acme.visualstudio.com/p/_git/app": "azure-devops",
		"ssh://git@git.example.org/app.git":        "git",
		"https://notgithub.com/x/y.git":            "git",
	}
	for in, want := range cases {
		if got := InferProvider(in); got != want {
			t.Errorf("InferProvider(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStatus(t *testing.T) {
	r := newRepo(t)
	ctx := context.Background()
	st, err := r.Status(ctx)
	if err != nil || !st.IsRepo || st.Branch != "main" || st.Head == "" || st.Provider != "local" || st.Dirty {
		t.Fatalf("status = %+v, err %v", st, err)
	}
	if _, err := r.Git(ctx, "remote", "add", "origin", "git@github.com:acme/app.git"); err != nil {
		t.Fatal(err)
	}
	write(t, r.Dir, "b.txt", "x")
	st, _ = r.Status(ctx)
	if st.Provider != "github" || !st.Dirty || st.Remote == "" {
		t.Fatalf("status = %+v", st)
	}
	out := &Repo{Dir: t.TempDir(), Runner: ExecRunner{Env: testEnv}}
	st, err = out.Status(ctx)
	if err != nil || st.IsRepo {
		t.Fatalf("non-repo status = %+v, err %v", st, err)
	}
}

func TestCreateTaskBranch(t *testing.T) {
	r := newRepo(t)
	ctx := context.Background()
	name, err := r.CreateTaskBranch(ctx, "auth-login")
	if err != nil || name != "feature/auth-login" {
		t.Fatalf("got %q, %v", name, err)
	}
	st, _ := r.Status(ctx)
	if st.Branch != "feature/auth-login" {
		t.Fatalf("branch = %q", st.Branch)
	}
	if _, err := r.CreateTaskBranch(ctx, "auth-login"); !errors.Is(err, ErrBranchExists) {
		t.Fatalf("want ErrBranchExists, got %v", err)
	}
	if _, err := r.CreateTaskBranch(ctx, "../evil"); !errors.Is(err, gabitotl.ErrInvalidID) {
		t.Fatalf("want ErrInvalidID, got %v", err)
	}
}

func TestPermissionGating(t *testing.T) {
	r := newRepo(t)
	ctx := context.Background()
	r.Config.Permissions.Branches = gabitotl.PermDeny
	if _, err := r.CreateTaskBranch(ctx, "x"); !errors.Is(err, ErrDenied) {
		t.Fatalf("want ErrDenied, got %v", err)
	}
	r.Config.Permissions.Branches = gabitotl.PermAsk
	if _, err := r.CreateTaskBranch(ctx, "x"); !errors.Is(err, ErrNeedsConfirmation) {
		t.Fatalf("want ErrNeedsConfirmation, got %v", err)
	}
	var asked string
	r.Confirm = func(op, detail string) bool { asked = op; return true }
	if _, err := r.CreateTaskBranch(ctx, "x"); err != nil || asked != OpBranch {
		t.Fatalf("confirmed branch: err %v asked %q", err, asked)
	}
	r.Config.Permissions.Commits = gabitotl.PermDeny
	write(t, r.Dir, "c.txt", "c")
	if _, err := r.Commit(ctx, "feat: add c", "c.txt"); !errors.Is(err, ErrDenied) {
		t.Fatalf("want ErrDenied, got %v", err)
	}
	r.Config.Permissions.Push = gabitotl.PermDeny
	_, _ = r.Git(ctx, "remote", "add", "origin", "/nonexistent/remote.git")
	if err := r.Push(ctx, "main"); !errors.Is(err, ErrDenied) {
		t.Fatalf("want ErrDenied for push, got %v", err)
	}
}

func TestCommitRules(t *testing.T) {
	r := newRepo(t)
	ctx := context.Background()
	write(t, r.Dir, "b.txt", "b")
	for _, bad := range []string{"added stuff", "Feat: x", "feat:nospace"} {
		if _, err := r.Commit(ctx, bad, "b.txt"); !errors.Is(err, ErrInvalidCommitMessage) {
			t.Errorf("%q: want ErrInvalidCommitMessage, got %v", bad, err)
		}
	}
	if _, err := r.Commit(ctx, "feat: b\n\nCo-Authored-By: Bot <bot@example.com>", "b.txt"); !errors.Is(err, ErrAttribution) {
		t.Fatalf("want ErrAttribution, got %v", err)
	}
	sha, err := r.Commit(ctx, "feat(b): add b file", "b.txt")
	if err != nil || sha == "" {
		t.Fatalf("commit: %q %v", sha, err)
	}
	body, _ := r.Git(ctx, "log", "-1", "--format=%B")
	if strings.Contains(strings.ToLower(body), "co-authored") || strings.TrimSpace(body) != "feat(b): add b file" {
		t.Fatalf("commit body = %q", body)
	}
	if _, err := r.Commit(ctx, "fix: nothing"); !errors.Is(err, ErrNothingToCommit) {
		t.Fatalf("want ErrNothingToCommit, got %v", err)
	}
}

func TestHistoryRewriteRejected(t *testing.T) {
	r := newRepo(t)
	ctx := context.Background()
	bad := [][]string{
		{"push", "--force", "origin", "main"},
		{"push", "-f"},
		{"push", "--force-with-lease"},
		{"push", "origin", "+main"},
		{"push", "origin", ":main"},
		{"reset", "--hard", "HEAD~1"},
		{"commit", "--amend", "-m", "x"},
		{"rebase", "main"},
		{"-C", ".", "filter-branch"},
		{"-c", "a=b", "reset", "--hard"},
	}
	for _, args := range bad {
		if _, err := r.Git(ctx, args...); !errors.Is(err, ErrHistoryRewrite) {
			t.Errorf("%v: want ErrHistoryRewrite, got %v", args, err)
		}
	}
	for _, ok := range [][]string{{"push", "origin", "main"}, {"reset", "--soft", "HEAD"}, {"status"}} {
		if err := CheckHistorySafe(ok); err != nil {
			t.Errorf("%v rejected: %v", ok, err)
		}
	}
}

// remotePair returns a repo whose origin is a local bare repo (never a real remote).
func remotePair(t *testing.T) *Repo {
	t.Helper()
	r := newRepo(t)
	bare := filepath.Join(t.TempDir(), "origin.git")
	if _, err := r.Git(context.Background(), "init", "-q", "--bare", bare); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Git(context.Background(), "remote", "add", "origin", bare); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestPushAndRevertShared(t *testing.T) {
	r := remotePair(t)
	ctx := context.Background()
	write(t, r.Dir, "s.txt", "shared")
	sha, err := r.Commit(ctx, "feat: shared change", "s.txt")
	if err != nil {
		t.Fatal(err)
	}
	if shared, _ := r.IsShared(ctx, sha); shared {
		t.Fatal("commit should not be shared before push")
	}
	if err := r.Push(ctx, "main"); err != nil {
		t.Fatal(err)
	}
	if shared, _ := r.IsShared(ctx, sha); !shared {
		t.Fatal("commit should be shared after push")
	}
	res, err := r.Revert(ctx, sha)
	if err != nil || !res.Shared || res.Commit == "" || res.Commit == sha {
		t.Fatalf("revert = %+v, %v", res, err)
	}
	if _, err := os.Stat(filepath.Join(r.Dir, "s.txt")); !os.IsNotExist(err) {
		t.Fatalf("s.txt should be gone after revert, stat err %v", err)
	}
	subj, _ := r.Git(ctx, "log", "-1", "--format=%s")
	if !strings.HasPrefix(subj, "revert: ") {
		t.Fatalf("revert subject = %q", subj)
	}
	// History kept: the original commit is still an ancestor.
	if _, err := r.Git(ctx, "merge-base", "--is-ancestor", sha, "HEAD"); err != nil {
		t.Fatalf("original commit lost: %v", err)
	}
	if _, err := r.Revert(ctx, "--hard"); err == nil {
		t.Fatal("option-looking commit must be rejected")
	}
}

func TestRevertConflictAborts(t *testing.T) {
	r := newRepo(t)
	ctx := context.Background()
	write(t, r.Dir, "a.txt", "two\n")
	first, _ := r.Commit(ctx, "fix: second", "a.txt")
	write(t, r.Dir, "a.txt", "three\n")
	if _, err := r.Commit(ctx, "fix: third", "a.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Revert(ctx, first); err == nil {
		t.Fatal("expected conflict")
	}
	if out, _ := r.Git(ctx, "status", "--porcelain"); out != "" {
		t.Fatalf("work tree must be clean after aborted revert, got %q", out)
	}
}

type fakeRunner struct {
	calls [][]string
	out   string
	err   error
}

func (f *fakeRunner) Run(_ context.Context, dir, name string, args ...string) (string, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	return f.out, f.err
}

func TestGitHubPR(t *testing.T) {
	fr := &fakeRunner{out: "Creating pull request...\nhttps://github.com/acme/app/pull/7"}
	r := &Repo{Dir: t.TempDir(), Config: allowAll()}
	prov, err := ProviderFor("github", fr)
	if err != nil {
		t.Fatal(err)
	}
	pr, err := r.OpenPR(context.Background(), prov, PRRequest{Title: "feat: login", Body: "body", Base: "main", Head: "feature/login", Draft: true})
	if err != nil || pr.URL != "https://github.com/acme/app/pull/7" {
		t.Fatalf("pr = %+v, %v", pr, err)
	}
	got := strings.Join(fr.calls[0], " ")
	for _, want := range []string{"gh pr create", "--title feat: login", "--base main", "--head feature/login", "--draft"} {
		if !strings.Contains(got, want) {
			t.Errorf("gh call %q missing %q", got, want)
		}
	}
	if _, err := prov.CreatePR(context.Background(), "", PRRequest{Title: "x", Body: "Generated " + "with " + "Cla" + "ude Code"}); !errors.Is(err, ErrAttribution) {
		t.Fatalf("want ErrAttribution, got %v", err)
	}
	r.Config.Permissions.PullRequests = gabitotl.PermDeny
	if _, err := r.OpenPR(context.Background(), prov, PRRequest{Title: "x"}); !errors.Is(err, ErrDenied) {
		t.Fatalf("want ErrDenied, got %v", err)
	}
}

func TestBitbucketStubAndUnsupported(t *testing.T) {
	prov, err := ProviderFor("bitbucket", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := prov.CreatePR(context.Background(), "", PRRequest{Title: "x"}); !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("want ErrNotImplemented, got %v", err)
	}
	if _, err := ProviderFor("gitlab", nil); !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("want ErrNotImplemented, got %v", err)
	}
}
