package vcs

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
)

// newOrigin is newRepo plus a bare repository on disk as origin, with main pushed.
func newOrigin(t *testing.T) (r *Repo, bare *Repo) {
	t.Helper()
	r = newRepo(t)
	ctx := context.Background()
	bare = &Repo{Dir: t.TempDir(), Runner: ExecRunner{Env: testEnv}, Config: allowAll()}
	if _, err := bare.Git(ctx, "init", "-q", "--bare", "-b", "main"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Git(ctx, "remote", "add", "origin", bare.Dir); err != nil {
		t.Fatal(err)
	}
	if err := r.Push(ctx, "main"); err != nil {
		t.Fatal(err)
	}
	return r, bare
}

func switchNew(t *testing.T, r *Repo, branch string) {
	t.Helper()
	if _, err := r.Git(context.Background(), "switch", "-c", branch); err != nil {
		t.Fatal(err)
	}
}

func hasBranch(r *Repo, branch string) bool {
	_, err := r.Git(context.Background(), "rev-parse", "--verify", "-q", "refs/heads/"+branch)
	return err == nil
}

func TestCommitWorkStagesEverythingExceptTheLocalDirAndIgnoredFiles(t *testing.T) {
	r := newRepo(t)
	ctx := context.Background()
	switchNew(t, r, "feature/x")
	write(t, r.Dir, ".gitignore", "secret.txt\n")
	write(t, r.Dir, "secret.txt", "nope")
	write(t, r.Dir, ".gabitotl/local/users/me/vcs.md", "personal")
	write(t, r.Dir, "src/new.go", "package src\n")
	write(t, r.Dir, "a.txt", "two\n")
	hash, err := r.CommitWork(ctx, "feat(x): add things", nil)
	if err != nil || hash == "" {
		t.Fatalf("hash=%q err=%v", hash, err)
	}
	files, _ := r.Git(ctx, "show", "--name-only", "--format=", "HEAD")
	for _, want := range []string{".gitignore", "src/new.go", "a.txt"} {
		if !strings.Contains(files, want) {
			t.Errorf("%s not committed: %q", want, files)
		}
	}
	for _, not := range []string{"secret.txt", ".gabitotl/local"} {
		if strings.Contains(files, not) {
			t.Errorf("%s must never be committed: %q", not, files)
		}
	}
}

func TestCommitWorkWithPathsCommitsOnlyThose(t *testing.T) {
	r := newRepo(t)
	ctx := context.Background()
	switchNew(t, r, "feature/x")
	write(t, r.Dir, "a.txt", "two\n")
	write(t, r.Dir, "b.txt", "other\n")
	if _, err := r.CommitWork(ctx, "fix: only a", []string{"a.txt"}); err != nil {
		t.Fatal(err)
	}
	files, _ := r.Git(ctx, "show", "--name-only", "--format=", "HEAD")
	if files != "a.txt" {
		t.Fatalf("files = %q", files)
	}
	if _, err := r.CommitWork(ctx, "fix: local", []string{".gabitotl/local/users/me/vcs.md"}); !errors.Is(err, ErrLocalPath) {
		t.Fatalf("want ErrLocalPath, got %v", err)
	}
	if _, err := r.CommitWork(ctx, "fix: dots", []string{"./.gabitotl/../.gabitotl/local"}); !errors.Is(err, ErrLocalPath) {
		t.Fatalf("a roundabout path must be refused too, got %v", err)
	}
}

func TestCommitWorkRefusesBadMessagesAndTheBaseBranch(t *testing.T) {
	r := newRepo(t)
	ctx := context.Background()
	write(t, r.Dir, "a.txt", "two\n")
	if _, err := r.CommitWork(ctx, "fix: on main", nil); !errors.Is(err, ErrOnBaseBranch) {
		t.Fatalf("a commit on the base branch must be refused, got %v", err)
	}
	switchNew(t, r, "feature/x")
	if _, err := r.CommitWork(ctx, "  ", nil); !errors.Is(err, ErrInvalidCommitMessage) {
		t.Fatalf("empty message: %v", err)
	}
	if _, err := r.CommitWork(ctx, "fix: x\n\nCo-Authored-By: A Bot <bot@example.com>", nil); !errors.Is(err, ErrAttribution) {
		t.Fatalf("attribution: %v", err)
	}
	if out, _ := r.Git(ctx, "diff", "--cached", "--name-only"); out != "" {
		t.Fatalf("a refused message must stage nothing, staged %q", out)
	}
	if _, err := r.CommitWork(ctx, "fix: nothing", []string{"a.txt"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.CommitWork(ctx, "fix: again", nil); !errors.Is(err, ErrNothingToCommit) {
		t.Fatalf("clean tree: %v", err)
	}
}

func TestCommitWorkAsksOnceWhenThePermissionIsAsk(t *testing.T) {
	r := newRepo(t)
	switchNew(t, r, "feature/x")
	r.Config.Permissions.Commits = gabitotl.PermAsk
	write(t, r.Dir, "a.txt", "two\n")
	if _, err := r.CommitWork(context.Background(), "fix: ask", nil); !errors.Is(err, ErrNeedsConfirmation) {
		t.Fatalf("no confirm must not proceed, got %v", err)
	}
	asked := 0
	r.Confirm = func(op, detail string) bool { asked++; return true }
	if _, err := r.CommitWork(context.Background(), "fix: ask", nil); err != nil || asked != 1 {
		t.Fatalf("asked=%d err=%v", asked, err)
	}
}

// recording wraps a runner and remembers every git command.
type recording struct {
	Runner
	calls []string
}

func (c *recording) Run(ctx context.Context, dir, name string, args ...string) (string, error) {
	c.calls = append(c.calls, name+" "+strings.Join(args, " "))
	return c.Runner.Run(ctx, dir, name, args...)
}

func TestPushTaskPublishesTheBranchNeverForcedAndNeverTheBase(t *testing.T) {
	r, bare := newOrigin(t)
	rec := &recording{Runner: r.Runner}
	r.Runner = rec
	ctx := context.Background()
	if _, err := r.PushTask(ctx, ""); !errors.Is(err, ErrOnBaseBranch) {
		t.Fatalf("pushing the base branch must be refused, got %v", err)
	}
	if _, err := r.PushTask(ctx, "main"); !errors.Is(err, ErrOnBaseBranch) {
		t.Fatalf("pushing main by name must be refused, got %v", err)
	}
	switchNew(t, r, "feature/x")
	write(t, r.Dir, "a.txt", "two\n")
	if _, err := r.CommitWork(ctx, "fix: change", nil); err != nil {
		t.Fatal(err)
	}
	got, err := r.PushTask(ctx, "")
	if err != nil || got != "feature/x" {
		t.Fatalf("branch=%q err=%v", got, err)
	}
	if !hasBranch(bare, "feature/x") {
		t.Fatal("origin does not have the branch")
	}
	for _, c := range rec.calls {
		if strings.Contains(c, "push") && (strings.Contains(c, "force") || strings.Contains(c, " -f")) {
			t.Errorf("forced push: %s", c)
		}
	}
	r.Config.Permissions.Push = gabitotl.PermDeny
	if _, err := r.PushTask(ctx, "feature/x"); !errors.Is(err, ErrDenied) {
		t.Fatalf("deny: %v", err)
	}
}

func TestBaseBranchPrecedence(t *testing.T) {
	r, _ := newOrigin(t)
	ctx := context.Background()
	if b, err := r.BaseBranch(ctx); err != nil || b != "main" {
		t.Fatalf("fallback to main: %q %v", b, err)
	}
	if _, err := r.Git(ctx, "branch", "-m", "main", "trunk"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Git(ctx, "remote", "set-head", "origin", "main"); err != nil {
		t.Fatal(err)
	}
	if b, _ := r.BaseBranch(ctx); b != "main" {
		t.Fatalf("origin/HEAD must win over guessing, got %q", b)
	}
	r.Config.BaseBranch = "develop"
	if b, _ := r.BaseBranch(ctx); b != "develop" {
		t.Fatalf("base_branch must win, got %q", b)
	}
	bare := newRepo(t)
	if _, err := bare.Git(ctx, "branch", "-m", "main", "odd"); err != nil {
		t.Fatal(err)
	}
	if _, err := bare.BaseBranch(ctx); !errors.Is(err, ErrNoBase) {
		t.Fatalf("want ErrNoBase, got %v", err)
	}
}

func TestPRForSettingsPrecedence(t *testing.T) {
	r, _ := newOrigin(t)
	ctx := context.Background()
	if _, err := r.PRFor(ctx, "t", "", "", nil); !errors.Is(err, ErrOnBaseBranch) {
		t.Fatalf("no PR from the base branch: %v", err)
	}
	switchNew(t, r, "feature/x")
	req, err := r.PRFor(ctx, "t", "b", "", nil)
	if err != nil || req.Base != "main" || req.Head != "feature/x" || req.Draft {
		t.Fatalf("defaults: %+v %v", req, err)
	}
	r.Config.BaseBranch, r.Config.DraftPRs = "release", true
	req, _ = r.PRFor(ctx, "t", "b", "", nil)
	if req.Base != "release" || !req.Draft {
		t.Fatalf("settings: %+v", req)
	}
	no := false
	req, _ = r.PRFor(ctx, "t", "b", "hotfix", &no)
	if req.Base != "hotfix" || req.Draft {
		t.Fatalf("flags win: %+v", req)
	}
}

// fakeHost scripts a platform: checks come from a sequence (the last one repeats).
type fakeHost struct {
	auth       AuthState
	checks     []Checks
	checkCalls int
	merged     []PR
	opts       []MergeOptions
	retargeted map[string]string
}

func (f *fakeHost) Name() string { return "fake" }
func (f *fakeHost) CreatePR(_ context.Context, _ string, req PRRequest) (PR, error) {
	return PR{URL: "https://fake.example/pr/1", ID: "1", Number: 1}, nil
}
func (f *fakeHost) MergePR(_ context.Context, _ string, pr PR, o MergeOptions) error {
	f.merged, f.opts = append(f.merged, pr), append(f.opts, o)
	return nil
}
func (f *fakeHost) SetBase(_ context.Context, _ string, pr PR, base string) error {
	if f.retargeted == nil {
		f.retargeted = map[string]string{}
	}
	f.retargeted[pr.URL] = base
	return nil
}
func (f *fakeHost) ChecksState(context.Context, string, PR) (Checks, error) {
	i := min(f.checkCalls, len(f.checks)-1)
	f.checkCalls++
	return f.checks[i], nil
}
func (f *fakeHost) AuthStatus(context.Context, string) (AuthState, error) { return f.auth, nil }

// clock is a fake clock whose Sleep only moves time forward.
type clock struct {
	now    time.Time
	sleeps []time.Duration
}

func (c *clock) waiter() Waiter {
	return Waiter{
		Now: func() time.Time { return c.now },
		Sleep: func(_ context.Context, d time.Duration) error {
			c.sleeps = append(c.sleeps, d)
			c.now = c.now.Add(d)
			return nil
		},
		Every: 10 * time.Second,
	}
}

func TestWaitForChecksStates(t *testing.T) {
	ctx := context.Background()
	pr := PR{URL: "u"}
	cases := []struct {
		name   string
		checks []Checks
		wait   time.Duration
		err    error
		state  string
		sleeps int
		calls  int
	}{
		{"passing", []Checks{{State: ChecksPassing}}, time.Minute, nil, ChecksPassing, 0, 1},
		{"none", []Checks{{State: ChecksNone}}, time.Minute, nil, ChecksNone, 0, 1},
		{"pending then passing", []Checks{{State: ChecksPending}, {State: ChecksPending}, {State: ChecksPassing}}, time.Minute, nil, ChecksPassing, 2, 3},
		{"failing stops at once", []Checks{{State: ChecksPending}, {State: ChecksFailing, Detail: "unit"}}, time.Minute, ErrChecksFailing, ChecksFailing, 1, 2},
		{"pending forever", []Checks{{State: ChecksPending}}, 25 * time.Second, ErrChecksPending, ChecksPending, 3, 4},
		{"zero wait reads once", []Checks{{State: ChecksPending}}, 0, ErrChecksPending, ChecksPending, 0, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := &fakeHost{checks: c.checks}
			clk := &clock{now: time.Unix(1000, 0)}
			got, err := clk.waiter().WaitForChecks(ctx, h, "", pr, c.wait)
			if !errors.Is(err, c.err) && !(c.err == nil && err == nil) {
				t.Fatalf("err = %v, want %v", err, c.err)
			}
			if got.State != c.state || len(clk.sleeps) != c.sleeps || h.checkCalls != c.calls {
				t.Fatalf("state=%s sleeps=%v calls=%d", got.State, clk.sleeps, h.checkCalls)
			}
		})
	}
}

func TestWaitForChecksNeverSleepsPastTheDeadline(t *testing.T) {
	h := &fakeHost{checks: []Checks{{State: ChecksPending}}}
	clk := &clock{now: time.Unix(0, 0)}
	_, err := clk.waiter().WaitForChecks(context.Background(), h, "", PR{}, 25*time.Second)
	var ce *ChecksError
	if !errors.As(err, &ce) || ce.State != ChecksPending {
		t.Fatalf("err = %v", err)
	}
	want := []time.Duration{10 * time.Second, 10 * time.Second, 5 * time.Second}
	if len(clk.sleeps) != len(want) {
		t.Fatalf("sleeps = %v", clk.sleeps)
	}
	for i := range want {
		if clk.sleeps[i] != want[i] {
			t.Fatalf("sleeps = %v, want %v", clk.sleeps, want)
		}
	}
}

func TestMergeWhenGreenUsesTheSavedMethodAndWaitsOnlyWhenAllowed(t *testing.T) {
	r, _ := newOrigin(t)
	r.Config.Permissions.Merge = gabitotl.PermAllow
	r.Config.MergeMethod, r.Config.DeleteBranch = gabitotl.MergeRebase, false
	h := &fakeHost{checks: []Checks{{State: ChecksPending}, {State: ChecksPassing}}}
	clk := &clock{now: time.Unix(0, 0)}
	if _, err := r.MergeWhenGreen(context.Background(), h, clk.waiter(), PR{URL: "u", ID: "7"}, time.Minute); err != nil {
		t.Fatal(err)
	}
	if len(h.merged) != 1 || h.opts[0].Method != "rebase" || h.opts[0].DeleteBranch {
		t.Fatalf("merged=%v opts=%+v", h.merged, h.opts)
	}

	// A failing check never merges.
	h = &fakeHost{checks: []Checks{{State: ChecksFailing}}}
	if _, err := r.MergeWhenGreen(context.Background(), h, clk.waiter(), PR{URL: "u"}, time.Minute); !errors.Is(err, ErrChecksFailing) || len(h.merged) != 0 {
		t.Fatalf("err=%v merged=%v", err, h.merged)
	}

	// deny fails before reading a single check.
	r.Config.Permissions.Merge = gabitotl.PermDeny
	h = &fakeHost{checks: []Checks{{State: ChecksPassing}}}
	if _, err := r.MergeWhenGreen(context.Background(), h, clk.waiter(), PR{URL: "u"}, time.Minute); !errors.Is(err, ErrDenied) || h.checkCalls != 0 {
		t.Fatalf("err=%v calls=%d", err, h.checkCalls)
	}

	// ask is answered once, before the wait, not again at the merge.
	r.Config.Permissions.Merge = gabitotl.PermAsk
	asked := 0
	r.Confirm = func(string, string) bool { asked++; return true }
	h = &fakeHost{checks: []Checks{{State: ChecksPassing}}}
	if _, err := r.MergeWhenGreen(context.Background(), h, clk.waiter(), PR{URL: "u"}, time.Minute); err != nil || asked != 1 || len(h.merged) != 1 {
		t.Fatalf("err=%v asked=%d merged=%v", err, asked, h.merged)
	}
}

func TestEnsureLoginReturnsTheExactFix(t *testing.T) {
	h := &fakeHost{auth: AuthState{Provider: "github", Status: AuthNotLoggedIn, Command: "gh auth login", Fix: "Log in with: gh auth login"}}
	err := EnsureLogin(context.Background(), h, "")
	var le *LoginError
	if !errors.Is(err, ErrNotLoggedIn) || !errors.As(err, &le) || le.State.Command != "gh auth login" {
		t.Fatalf("err = %v", err)
	}
	h.auth = AuthState{Status: AuthLoggedIn}
	if err := EnsureLogin(context.Background(), h, ""); err != nil {
		t.Fatal(err)
	}
}

func TestAfterMergeReturnsToTheBaseAndDeletesAPublishedBranch(t *testing.T) {
	r, _ := newOrigin(t)
	ctx := context.Background()
	switchNew(t, r, "feature/x")
	write(t, r.Dir, "a.txt", "two\n")
	if _, err := r.CommitWork(ctx, "fix: change", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := r.PushTask(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if !r.IsPublished(ctx, "feature/x") {
		t.Fatal("the pushed branch must read as published")
	}
	published := r.IsPublished(ctx, "feature/x")
	notes := r.AfterMerge(ctx, "feature/x", published)
	if cur, _ := r.CurrentBranch(ctx); cur != "main" || hasBranch(r, "feature/x") {
		t.Fatalf("cur=%q notes=%v", cur, notes)
	}
}

func TestAfterMergeKeepsWhatIsNotSafeToDelete(t *testing.T) {
	r, _ := newOrigin(t)
	ctx := context.Background()
	switchNew(t, r, "feature/x")
	write(t, r.Dir, "a.txt", "two\n")
	if _, err := r.CommitWork(ctx, "fix: one", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := r.PushTask(ctx, ""); err != nil {
		t.Fatal(err)
	}
	write(t, r.Dir, "a.txt", "three\n")
	if _, err := r.CommitWork(ctx, "fix: two, never pushed", nil); err != nil {
		t.Fatal(err)
	}
	if r.IsPublished(ctx, "feature/x") {
		t.Fatal("a branch with an unpushed commit is not published")
	}
	notes := r.AfterMerge(ctx, "feature/x", false)
	if !hasBranch(r, "feature/x") || !hasNote(notes, "kept-unpublished") {
		t.Fatalf("the branch must stay: %v", notes)
	}

	// delete_branch=false keeps even a published branch.
	r2, _ := newOrigin(t)
	r2.Config.DeleteBranch = false
	switchNew(t, r2, "feature/y")
	write(t, r2.Dir, "a.txt", "two\n")
	if _, err := r2.CommitWork(ctx, "fix: y", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := r2.PushTask(ctx, ""); err != nil {
		t.Fatal(err)
	}
	r2.AfterMerge(ctx, "feature/y", true)
	if !hasBranch(r2, "feature/y") {
		t.Fatal("delete_branch=false must keep the branch")
	}
}

func hasNote(notes []Note, code string) bool {
	for _, n := range notes {
		if n.Code == code {
			return true
		}
	}
	return false
}
