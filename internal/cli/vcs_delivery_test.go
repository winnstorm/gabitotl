package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/installer/ui"
	"github.com/winnstorm/gabitotl/internal/vcs"
)

// deliveryHost scripts a platform for the delivery commands; nothing here talks
// to a network or to a real gh/glab.
type deliveryHost struct {
	mu         sync.Mutex
	auth       vcs.AuthState
	open       map[string]vcs.PR // head branch -> PR
	created    []vcs.PRRequest
	merged     []vcs.PR
	checks     []vcs.Checks // the last one repeats
	checkCalls int
}

func newDeliveryHost() *deliveryHost {
	return &deliveryHost{auth: vcs.AuthState{Provider: "github", Status: vcs.AuthLoggedIn},
		open: map[string]vcs.PR{}, checks: []vcs.Checks{{State: vcs.ChecksPassing}}}
}

func (h *deliveryHost) Name() string { return "github" }
func (h *deliveryHost) CreatePR(_ context.Context, _ string, req vcs.PRRequest) (vcs.PR, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if pr, ok := h.open[req.Head]; ok {
		return pr, nil
	}
	h.created = append(h.created, req)
	pr := vcs.PR{URL: "https://github.com/acme/app/pull/" + string(rune('0'+len(h.created))), ID: string(rune('0' + len(h.created)))}
	h.open[req.Head] = pr
	return pr, nil
}
func (h *deliveryHost) FindPR(_ context.Context, _, head string) (vcs.PR, bool, error) {
	pr, ok := h.open[head]
	return pr, ok, nil
}
func (h *deliveryHost) MergePR(_ context.Context, _ string, pr vcs.PR, _ vcs.MergeOptions) error {
	h.merged = append(h.merged, pr)
	return nil
}
func (h *deliveryHost) SetBase(context.Context, string, vcs.PR, string) error { return nil }
func (h *deliveryHost) ChecksState(context.Context, string, vcs.PR) (vcs.Checks, error) {
	i := min(h.checkCalls, len(h.checks)-1)
	h.checkCalls++
	return h.checks[i], nil
}
func (h *deliveryHost) AuthStatus(context.Context, string) (vcs.AuthState, error) { return h.auth, nil }

// fakeClock replaces the real clock of `vcs merge`.
type fakeClock struct {
	now    time.Time
	sleeps int
}

// deliveryProject is a project with a bare repository on disk as origin, the
// delivery settings answered ("all" unless changed) and the platform faked.
func deliveryProject(t *testing.T, change func(*gabitotl.VCSConfig)) (root string, bare string, h *deliveryHost, clk *fakeClock) {
	t.Helper()
	root = project(t)
	bare = t.TempDir()
	ctx := context.Background()
	if _, err := vcs.New(bare, gabitotl.VCSConfig{}).Git(ctx, "init", "-q", "--bare", "-b", "main"); err != nil {
		t.Fatal(err)
	}
	setOrigin(t, root, bare)
	// init installed the authorship-guard hooks, which call the gabitotl binary;
	// under test that binary is the test binary itself, so the hooks stay off.
	gitOut(t, root, "config", "core.hooksPath", t.TempDir())
	if _, err := vcs.New(root, gabitotl.VCSConfig{}).Git(ctx, "push", "-q", "-u", "origin", "main"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GABITOTL_USER", "tester")
	layout := gabitotl.NewLayout(root)
	cfg := gabitotl.DefaultVCS("github")
	cfg.Permissions = gabitotl.VCSPermissions{Branches: "allow", Commits: "allow", Push: "allow", PullRequests: "allow", Merge: "allow"}
	cfg.Configured = true
	if change != nil {
		change(&cfg)
	}
	if err := gabitotl.SaveVCSFor(layout, "tester", cfg); err != nil {
		t.Fatal(err)
	}
	h, clk = newDeliveryHost(), &fakeClock{now: time.Unix(0, 0)}
	oldHost, oldWaiter := deliveryHostFn, deliveryWaiterFn
	deliveryHostFn = func(string, string, gabitotl.VCSConfig) (vcs.Host, error) { return h, nil }
	deliveryWaiterFn = func() vcs.Waiter {
		return vcs.Waiter{
			Now:   func() time.Time { return clk.now },
			Sleep: func(_ context.Context, d time.Duration) error { clk.sleeps++; clk.now = clk.now.Add(d); return nil },
		}
	}
	t.Cleanup(func() { deliveryHostFn, deliveryWaiterFn = oldHost, oldWaiter })
	return root, bare, h, clk
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := vcs.New(dir, gabitotl.VCSConfig{}).Git(context.Background(), args...)
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return out
}

func writeIn(t *testing.T, root, name, content string) {
	t.Helper()
	p := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// onFeature switches to a task branch with one uncommitted change.
func onFeature(t *testing.T, root string) {
	t.Helper()
	gitOut(t, root, "switch", "-c", "feature/login")
	writeIn(t, root, "app.txt", "v2")
}

func TestCommitStagesEverythingExceptPersonalFilesAndPrintsTheHash(t *testing.T) {
	root, _, _, _ := deliveryProject(t, nil)
	onFeature(t, root)
	writeIn(t, root, ".gabitotl/local/users/tester/notes.md", "mine")
	r := runIn(t, "", false, root, "vcs", "commit", "-m", "feat(login): add the form")
	if r.code != 0 || !strings.Contains(r.out, "Commit listo: ") {
		t.Fatalf("%+v", r)
	}
	files := gitOut(t, root, "show", "--name-only", "--format=", "HEAD")
	if !strings.Contains(files, "app.txt") || strings.Contains(files, ".gabitotl/local") {
		t.Fatalf("files = %q", files)
	}
	if !strings.Contains(r.out, gitOut(t, root, "rev-parse", "--short=8", "HEAD")) {
		t.Fatalf("the short hash must be printed: %q", r.out)
	}
}

func TestCommitRefusesAnEmptyMessageAttributionAndTheBaseBranch(t *testing.T) {
	root, _, _, _ := deliveryProject(t, nil)
	writeIn(t, root, "app.txt", "v2")
	if r := runIn(t, "", false, root, "vcs", "commit", "-m", "fix: on main"); r.code == 0 || !strings.Contains(r.err, "rama base") {
		t.Fatalf("base branch: %+v", r)
	}
	onFeature(t, root)
	if r := runIn(t, "", false, root, "vcs", "commit", "-m", " "); r.code != ExitUsage || !strings.Contains(r.err, "Falta el mensaje") {
		t.Fatalf("empty: %+v", r)
	}
	if r := runIn(t, "", false, root, "vcs", "commit", "-m", "fix: x\n\nCo-Authored-By: Bot <b@example.com>"); r.code == 0 || !strings.Contains(r.err, "autoría") {
		t.Fatalf("attribution: %+v", r)
	}
	if r := runIn(t, "", false, root, "vcs", "commit", "-m", "just words"); r.code == 0 || !strings.Contains(r.err, "Conventional Commits") {
		t.Fatalf("format: %+v", r)
	}
	if r := runIn(t, "", false, root, "vcs", "commit", "-m", "fix: x", "--paths", ".gabitotl/local/x"); r.code == 0 || !strings.Contains(r.err, ".gabitotl/local") {
		t.Fatalf("local path: %+v", r)
	}
}

func TestPushPublishesTheBranchAndRefusesTheBase(t *testing.T) {
	root, bare, _, _ := deliveryProject(t, nil)
	if r := runIn(t, "", false, root, "vcs", "push"); r.code == 0 || !strings.Contains(r.err, "rama base") {
		t.Fatalf("%+v", r)
	}
	onFeature(t, root)
	runIn(t, "", false, root, "vcs", "commit", "-m", "feat(login): add the form")
	r := runIn(t, "", false, root, "vcs", "push")
	if r.code != 0 || !strings.Contains(r.out, "feature/login") {
		t.Fatalf("%+v", r)
	}
	if gitOut(t, bare, "rev-parse", "--verify", "refs/heads/feature/login") == "" {
		t.Fatal("origin has no branch")
	}
}

func TestPushWithAskAndNoTerminalStopsWithTheSetting(t *testing.T) {
	root, _, _, _ := deliveryProject(t, func(c *gabitotl.VCSConfig) { c.Permissions.Push = "ask" })
	onFeature(t, root)
	runIn(t, "", false, root, "vcs", "commit", "-m", "feat(login): add the form")
	r := runIn(t, "", false, root, "vcs", "push")
	if r.code == 0 || !strings.Contains(r.err, "gabitotl vcs setup") || strings.Contains(r.out+r.err, "?") {
		t.Fatalf("%+v", r)
	}
}

func TestPRFollowsTheSettingsAndIsIdempotent(t *testing.T) {
	root, _, h, _ := deliveryProject(t, func(c *gabitotl.VCSConfig) { c.BaseBranch, c.DraftPRs = "release", true })
	onFeature(t, root)
	runIn(t, "", false, root, "vcs", "commit", "-m", "feat(login): add the form")
	r := runIn(t, "", false, root, "vcs", "pr", "--title", "feat: login")
	if r.code != 0 || !strings.Contains(r.out, "PR listo: https://github.com/acme/app/pull/1") {
		t.Fatalf("%+v", r)
	}
	if got := h.created[0]; got.Base != "release" || !got.Draft || got.Head != "feature/login" {
		t.Fatalf("settings not applied: %+v", got)
	}
	again := runIn(t, "", false, root, "vcs", "pr", "--title", "feat: login")
	if again.code != 0 || !strings.Contains(again.out, "pull/1") || len(h.created) != 1 {
		t.Fatalf("not idempotent: %+v created=%d", again, len(h.created))
	}
}

func TestPRFlagsBeatTheSettings(t *testing.T) {
	root, _, h, _ := deliveryProject(t, func(c *gabitotl.VCSConfig) { c.BaseBranch, c.DraftPRs = "release", true })
	onFeature(t, root)
	runIn(t, "", false, root, "vcs", "commit", "-m", "feat(login): add the form")
	r := runIn(t, "", false, root, "vcs", "pr", "--title", "feat: login", "--base", "hotfix", "--draft=false")
	if r.code != 0 || h.created[0].Base != "hotfix" || h.created[0].Draft {
		t.Fatalf("%+v %+v", r, h.created)
	}
}

func TestPRWithoutLoginPrintsTheFixAndNeverPrompts(t *testing.T) {
	root, _, h, _ := deliveryProject(t, nil)
	h.auth = vcs.AuthState{Provider: "github", Status: vcs.AuthNotLoggedIn, Command: "gh auth login"}
	onFeature(t, root)
	r := runIn(t, "", true, root, "vcs", "pr", "--title", "feat: login")
	if r.code == 0 || !strings.Contains(r.err, "gh auth login") || len(h.created) != 0 {
		t.Fatalf("%+v", r)
	}
	if strings.Contains(r.out+r.err, "(s/n)") {
		t.Fatalf("a missing login must not prompt: %+v", r)
	}
	h.auth.Status, h.auth.Command, h.auth.InstallURL = vcs.AuthToolMissing, "", "https://cli.github.com"
	if r := runIn(t, "", false, root, "vcs", "pr", "--title", "feat: login"); r.code == 0 || !strings.Contains(r.err, "https://cli.github.com") {
		t.Fatalf("tool missing: %+v", r)
	}
}

func TestPRRefusesTheBaseBranchAndAMissingTitle(t *testing.T) {
	root, _, _, _ := deliveryProject(t, nil)
	if r := runIn(t, "", false, root, "vcs", "pr", "--title", "x: y"); r.code == 0 || !strings.Contains(r.err, "rama base") {
		t.Fatalf("%+v", r)
	}
	if r := runIn(t, "", false, root, "vcs", "pr"); r.code != ExitUsage || !strings.Contains(r.err, "--title") {
		t.Fatalf("%+v", r)
	}
}

// shipped puts the feature branch on origin with an open PR.
func shipped(t *testing.T, root string, h *deliveryHost) {
	t.Helper()
	onFeature(t, root)
	for _, args := range [][]string{
		{"vcs", "commit", "-m", "feat(login): add the form"},
		{"vcs", "push"},
		{"vcs", "pr", "--title", "feat: login"},
	} {
		if r := runIn(t, "", false, root, args...); r.code != 0 {
			t.Fatalf("%v: %+v", args, r)
		}
	}
}

func TestMergeMergesPassingChecksThenCleansUpLocally(t *testing.T) {
	root, _, h, _ := deliveryProject(t, nil)
	shipped(t, root, h)
	r := runIn(t, "", false, root, "vcs", "merge")
	if r.code != 0 || !strings.Contains(r.out, "Mergeé") || len(h.merged) != 1 {
		t.Fatalf("%+v merged=%v", r, h.merged)
	}
	if cur := gitOut(t, root, "symbolic-ref", "--short", "HEAD"); cur != "main" {
		t.Fatalf("must return to the base, on %q", cur)
	}
	if gitOut(t, root, "branch", "--list", "feature/login") != "" || !strings.Contains(r.out, "Borré la rama local") {
		t.Fatalf("local branch must be deleted: %+v", r)
	}
}

func TestMergeWithNoChecksMergesAndSaysSo(t *testing.T) {
	root, _, h, _ := deliveryProject(t, nil)
	shipped(t, root, h)
	h.checks = []vcs.Checks{{State: vcs.ChecksNone}}
	r := runIn(t, "", false, root, "vcs", "merge")
	if r.code != 0 || !strings.Contains(r.out, "no tiene checks") || len(h.merged) != 1 {
		t.Fatalf("%+v", r)
	}
}

func TestMergeKeepsTheLocalBranchWhenDeleteBranchIsOff(t *testing.T) {
	root, _, h, _ := deliveryProject(t, func(c *gabitotl.VCSConfig) { c.DeleteBranch = false })
	shipped(t, root, h)
	if r := runIn(t, "", false, root, "vcs", "merge"); r.code != 0 {
		t.Fatalf("%+v", r)
	}
	if gitOut(t, root, "branch", "--list", "feature/login") == "" {
		t.Fatal("delete_branch=false must keep the local branch")
	}
}

func TestMergeRefusesFailingChecksAndNamesThem(t *testing.T) {
	root, _, h, clk := deliveryProject(t, nil)
	shipped(t, root, h)
	h.checks = []vcs.Checks{{State: vcs.ChecksPending}, {State: vcs.ChecksFailing, Detail: "unit-tests"}}
	r := runIn(t, "", false, root, "vcs", "merge")
	if r.code == 0 || !strings.Contains(r.err, "fallando") || !strings.Contains(r.err, "unit-tests") || len(h.merged) != 0 || clk.sleeps != 1 {
		t.Fatalf("%+v sleeps=%d", r, clk.sleeps)
	}
}

func TestMergeGivesUpOnPendingChecksAfterTheWait(t *testing.T) {
	root, _, h, clk := deliveryProject(t, nil)
	shipped(t, root, h)
	h.checks = []vcs.Checks{{State: vcs.ChecksPending}}
	r := runIn(t, "", false, root, "vcs", "merge", "--wait", "1m")
	if r.code == 0 || !strings.Contains(r.err, "siguen corriendo") || len(h.merged) != 0 {
		t.Fatalf("%+v", r)
	}
	if clk.now.Sub(time.Unix(0, 0)) != time.Minute {
		t.Fatalf("it must wait exactly the asked time, waited %v", clk.now.Sub(time.Unix(0, 0)))
	}
}

func TestMergeHonorsThePermissionAndTheExplicitPR(t *testing.T) {
	root, _, h, _ := deliveryProject(t, func(c *gabitotl.VCSConfig) { c.Permissions.Merge = "deny" })
	shipped(t, root, h)
	if r := runIn(t, "", false, root, "vcs", "merge"); r.code == 0 || !strings.Contains(r.err, "no permite mergear") || h.checkCalls != 0 {
		t.Fatalf("deny: %+v", r)
	}

	root2, _, h2, _ := deliveryProject(t, func(c *gabitotl.VCSConfig) { c.Permissions.Merge = "ask" })
	shipped(t, root2, h2)
	if r := runIn(t, "", false, root2, "vcs", "merge"); r.code == 0 || !strings.Contains(r.err, "gabitotl vcs setup") || len(h2.merged) != 0 {
		t.Fatalf("ask without a terminal: %+v", r)
	}
	r := runIn(t, "s\n", true, root2, "vcs", "merge", "--pr", "42")
	if r.code != 0 || len(h2.merged) != 1 || h2.merged[0].ID != "42" {
		t.Fatalf("ask with a terminal and --pr: %+v merged=%v", r, h2.merged)
	}
	if !strings.Contains(r.err, "¿Mergeo?") {
		t.Fatalf("the question is in Spanish: %q", r.err)
	}
}

func TestMergeWithoutAnOpenPRSaysHowToOpenIt(t *testing.T) {
	root, _, _, _ := deliveryProject(t, nil)
	onFeature(t, root)
	r := runIn(t, "", false, root, "vcs", "merge")
	if r.code == 0 || !strings.Contains(r.err, "gabitotl vcs pr") {
		t.Fatalf("%+v", r)
	}
}

func TestParsePRRef(t *testing.T) {
	if pr := parsePRRef("12"); pr.ID != "12" || pr.Number != 12 || pr.URL != "" {
		t.Fatalf("%+v", pr)
	}
	if pr := parsePRRef("https://x/pull/3"); pr.URL != "https://x/pull/3" || pr.ID != "" {
		t.Fatalf("%+v", pr)
	}
}

func TestDeliveryMessagesNeverAskAboutPRSizeOrExceptions(t *testing.T) {
	for k, v := range ui.Default() {
		if !strings.HasPrefix(k, "deliver.") {
			continue
		}
		low := strings.ToLower(v)
		for _, w := range []string{"exce", "tamaño", "tamano", "presupuesto", "400"} {
			if strings.Contains(low, w) {
				t.Errorf("%s mentions %q: %s", k, w, v)
			}
		}
		if strings.HasPrefix(k, "deliver.confirm.") && !strings.Contains(v, "{detail}") {
			t.Errorf("%s must say what it asks about", k)
		}
	}
}
