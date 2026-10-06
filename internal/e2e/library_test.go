package e2e

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/winnstorm/gabitotl/internal/budget"
	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/memory"
	"github.com/winnstorm/gabitotl/internal/memory/engram"
	"github.com/winnstorm/gabitotl/internal/scanner"
	"github.com/winnstorm/gabitotl/internal/vcs"
)

// engramProvider returns the real Engram adapter over the fake engram script.
func engramProvider(m *machine, project string) *engram.Provider {
	return engram.New(engram.Options{Runner: &engram.ExecRunner{Binary: filepath.Join(m.bin, "engram")}, Project: project})
}

// (b, memory half) The scanned project map is saved through the real Engram
// adapter, which runs `engram save`; the fake records what it was given.
func TestProjectMapSavedThroughEngram(t *testing.T) {
	m := newMachine(t)
	proj := m.repo("shop", map[string]string{"go.mod": "module shop\n\ngo 1.22\n", "main.go": "package main\n", "README.md": "# shop\n\nA small shop.\n"})
	ctx := context.Background()
	pm, err := scanner.Scan(ctx, scanner.Options{Root: proj})
	if err != nil {
		t.Fatal(err)
	}
	entry := pm.ToEntry("shop")
	if entry.Type != memory.TypeProjectMap {
		t.Fatalf("type = %s", entry.Type)
	}
	id, err := engramProvider(m, "shop").Save(ctx, entry)
	if err != nil || id == "" {
		t.Fatalf("save: %v %q", err, id)
	}
	saves := read(t, filepath.Join(m.dir, "engram.saves"))
	contains(t, "engram save", saves, "Go 1.22")
	if i := index(m.calls(), "engram save", 0); i < 0 || !strings.Contains(m.calls()[i], "--type project-map") || !strings.Contains(m.calls()[i], "--project shop") {
		t.Fatalf("save must carry the type and project: %v", m.calls())
	}
}

// (c) Two developers, two worktrees, each with their own identity. Work files
// travel through Git; when they overlap the second developer gets an OJO and
// is never blocked.
func TestTwoUsersTwoWorktreesOverlapOjo(t *testing.T) {
	base := outsideTemp(t)
	if isUnderTemp(base) {
		t.Skip("worktrees refuse temporary directories and the package sources live under one")
	}
	m := newMachineIn(t, base)
	main := m.repo("team", map[string]string{"go.mod": "module team\n", "main.go": "package main\n", "src/auth/login.go": "package auth\n"})
	m.ok(main, "init", "--yes", "--mode", "team", "--user-name", "Owner")
	m.allowVCS(main)
	m.git(main, "add", ".gabitotl")
	m.git(main, "commit", "-q", "-m", "chore: add gabitotl project files")

	r := m.ok(main, "worktree", "add", "--user", "ana", "--task", "auth")
	wtAna := strings.TrimSpace(strings.TrimPrefix(strings.Split(r.out, "\n")[0], "worktree: "))
	r = m.ok(main, "worktree", "add", "--user", "bob", "--task", "billing")
	wtBob := strings.TrimSpace(strings.TrimPrefix(strings.Split(r.out, "\n")[0], "worktree: "))
	if want := filepath.Join(base, "team-worktrees"); filepath.Dir(wtAna) != want || filepath.Dir(wtBob) != want {
		t.Fatalf("worktrees must be siblings of the repo: %s %s (want under %s)", wtAna, wtBob, want)
	}
	list := m.ok(main, "worktree", "list").out
	contains(t, "worktree list", list, "feature/auth", "feature/billing")

	// Each worktree has its own identity; the main worktree keeps its own.
	m.ok(wtAna, "init", "--yes", "--user-name", "Ana")
	m.ok(wtBob, "init", "--yes", "--user-name", "Bob")
	contains(t, "ana identity", read(t, filepath.Join(wtAna, ".gabitotl/local/user.md")), "ana")
	contains(t, "bob identity", read(t, filepath.Join(wtBob, ".gabitotl/local/user.md")), "bob")
	contains(t, "main identity", read(t, filepath.Join(main, ".gabitotl/local/user.md")), "owner")

	// Ana starts on src/auth and shares her work file through her branch.
	m.ok(wtAna, "work", "start", "--task", "auth", "--areas", "src/auth", "--branch", "feature/auth")
	m.git(wtAna, "add", ".gabitotl/work")
	m.git(wtAna, "commit", "-q", "-m", "chore: share work state")
	// Bob picks up the shared work state from Ana's branch (what a pull does).
	m.git(wtBob, "checkout", "feature/auth", "--", ".gabitotl/work")

	r = m.ok(wtBob, "work", "start", "--task", "billing", "--areas", "src/billing")
	absent(t, "non-overlapping start", r.out, "overlap with")

	r = m.ok(wtBob, "work", "update", "--task", "billing", "--areas", "src/auth/login.go")
	contains(t, "overlap advisory", r.out, "overlap with ana", "Ojo", "Conviene coordinar")
	r = m.ok(wtBob, "work", "overlap", "--no-memory", "--task", "billing", "--areas", "src/auth")
	contains(t, "work overlap", r.out, "overlap with ana")
	r = m.ok(wtBob, "work", "status")
	contains(t, "work status", r.out, "ana/auth", "bob/billing")

	// A worktree with uncommitted work is never removed by force.
	if r := m.run(main, "worktree", "remove", "billing"); r.code == 0 || !exists(wtBob) {
		t.Fatalf("remove must refuse a dirty worktree: %+v", r)
	}
	m.git(wtBob, "add", "-A")
	m.git(wtBob, "commit", "-q", "-m", "chore: bob work state")
	// Removing a worktree never touches the main one.
	m.ok(main, "worktree", "remove", "billing")
	if !exists(main) || exists(wtBob) {
		t.Fatal("remove must delete only the named worktree")
	}
}

func isUnderTemp(p string) bool {
	for _, t := range []string{os.TempDir(), "/tmp", "/var/tmp"} {
		if strings.HasPrefix(p, t+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// (g) The VCS layer against real Git, a local bare repository as origin and a
// fake gh: task branch, Conventional Commits, no attribution, revert of a pushed
// commit, no history rewriting, PR through gh.
func TestVCSBranchCommitRevertAndNoHistoryRewrite(t *testing.T) {
	m := newMachine(t)
	proj := m.repo("shop", map[string]string{"main.go": "package main\n"})
	origin := filepath.Join(m.base, "origin.git")
	m.git(m.base, "init", "-q", "--bare", origin)
	m.git(proj, "remote", "add", "origin", origin)
	m.git(proj, "push", "-q", "-u", "origin", "main")
	m.ok(proj, "init", "--yes", "--user-name", "Ana", "--vcs", "git")
	m.allowVCS(proj)

	// The CLI creates the task branch.
	r := m.ok(proj, "vcs", "branch", "--task", "login")
	contains(t, "vcs branch", r.out, "feature/login")
	if got := m.git(proj, "branch", "--show-current"); got != "feature/login" {
		t.Fatalf("branch = %s", got)
	}

	allow := gabitotl.VCSConfig{Provider: "github", Permissions: gabitotl.VCSPermissions{Branches: "allow", Commits: "allow", Push: "allow", PullRequests: "allow"}}
	repo := vcs.New(proj, allow)
	ctx := context.Background()
	head := func() string { return m.git(proj, "rev-parse", "HEAD") }

	// Conventional commit accepted; no trailer is ever added.
	m.write(filepath.Join(proj, "login.go"), "package main\n\nfunc login() {}\n")
	sha, err := repo.Commit(ctx, "feat(auth): add login", "login.go")
	if err != nil || sha != head() {
		t.Fatalf("conventional commit: %v", err)
	}
	if body := m.git(proj, "log", "-1", "--format=%B"); strings.Contains(strings.ToLower(body), "co-authored-by") || strings.Contains(strings.ToLower(body), "generated") {
		t.Fatalf("GabitoTL must never add attribution: %q", body)
	}

	// Attribution and non-conventional messages are rejected before git runs.
	m.write(filepath.Join(proj, "other.go"), "package main\n")
	m.git(proj, "add", "other.go")
	tip := head()
	for _, bad := range []struct {
		msg  string
		want error
	}{
		{"feat: add other\n\nCo-Authored-By: Claude <noreply@anthropic.com>", vcs.ErrAttribution},
		{"feat: add other\n\n🤖 Generated with Claude Code", vcs.ErrAttribution},
		{"added some stuff", vcs.ErrInvalidCommitMessage},
	} {
		if _, err := repo.Commit(ctx, bad.msg); !errors.Is(err, bad.want) {
			t.Errorf("%q: err = %v, want %v", bad.msg, err, bad.want)
		}
	}
	if head() != tip {
		t.Fatal("a rejected commit must not create a commit")
	}
	m.git(proj, "reset", "-q") // unstage the probe file

	// Push, then revert the pushed commit: a new commit, history intact.
	if err := repo.Push(ctx, "feature/login"); err != nil {
		t.Fatal(err)
	}
	count := func() int { n, _ := strconv.Atoi(m.git(proj, "rev-list", "--count", "HEAD")); return n }
	before := count()
	res, err := repo.Revert(ctx, sha)
	if err != nil || !res.Shared {
		t.Fatalf("revert of a pushed commit: %+v %v", res, err)
	}
	if count() != before+1 || exists(filepath.Join(proj, "login.go")) {
		t.Fatalf("revert must add one commit and undo the change (commits %d -> %d)", before, count())
	}
	contains(t, "revert subject", m.git(proj, "log", "-1", "--format=%s"), "revert:")
	m.git(proj, "merge-base", "--is-ancestor", sha, "HEAD") // the reverted commit is still in history

	// History rewriting never reaches git.
	remoteTip := m.git(origin, "rev-parse", "feature/login")
	for _, args := range [][]string{
		{"push", "--force", "origin", "feature/login"},
		{"push", "--force-with-lease", "origin", "feature/login"},
		{"push", "origin", "+feature/login"},
		{"commit", "--amend", "-m", "feat: rewrite"},
		{"reset", "--hard", "HEAD~1"},
		{"rebase", "main"},
	} {
		if _, err := repo.Git(ctx, args...); !errors.Is(err, vcs.ErrHistoryRewrite) {
			t.Errorf("git %v: err = %v, want ErrHistoryRewrite", args, err)
		}
	}
	if m.git(origin, "rev-parse", "feature/login") != remoteTip || count() != before+1 {
		t.Fatal("the remote and the local history must be untouched")
	}

	// Permissions: "ask" without confirmation never proceeds.
	ask := vcs.New(proj, gabitotl.DefaultVCS("github"))
	m.write(filepath.Join(proj, "more.go"), "package main\n")
	if _, err := ask.Commit(ctx, "feat: more", "more.go"); !errors.Is(err, vcs.ErrNeedsConfirmation) {
		t.Fatalf("ask permission: %v", err)
	}

	// Pull request through the fake gh (the only external service involved).
	pr, err := repo.OpenPR(ctx, vcs.GitHub{}, vcs.PRRequest{Title: "feat(auth): add login", Body: "Adds login.", Base: "main", Head: "feature/login"})
	if err != nil || pr.URL != "https://github.com/acme/shop/pull/7" {
		t.Fatalf("pr: %+v %v", pr, err)
	}
	if i := index(m.calls(), "gh pr create", 0); i < 0 || !strings.Contains(m.calls()[i], "--head feature/login") {
		t.Fatalf("gh was not called as expected: %v", m.calls())
	}
	if _, err := repo.OpenPR(ctx, vcs.GitHub{}, vcs.PRRequest{Title: "feat: x", Body: "Co-Authored-By: Claude <noreply@anthropic.com>"}); !errors.Is(err, vcs.ErrAttribution) {
		t.Fatalf("a PR body with attribution must be rejected: %v", err)
	}
	if _, err := repo.OpenPR(ctx, vcs.Bitbucket{}, vcs.PRRequest{Title: "feat: x"}); !errors.Is(err, vcs.ErrNotImplemented) {
		t.Fatalf("bitbucket is a documented stub: %v", err)
	}
}

// (h) The secret guard refuses a connection string with a password anywhere
// under .gabitotl and never writes the file.
func TestSecretGuardRejectsConnectionStringWithPassword(t *testing.T) {
	m := newMachine(t)
	proj := m.repo("shop", map[string]string{"main.go": "package main\n"})
	m.ok(proj, "init", "--yes", "--user-name", "Ana")
	l := gabitotl.NewLayout(proj)

	target := filepath.Join(l.Dir(), "notes.md")
	err := gabitotl.WriteFile(target, []byte("db: postgres://app:hunter2@db.internal:5432/shop\n"))
	var se *gabitotl.SecretError
	if !errors.As(err, &se) || strings.Contains(err.Error(), "hunter2") {
		t.Fatalf("want a SecretError that never echoes the value, got %v", err)
	}
	if exists(target) {
		t.Fatal("a rejected write must not leave a file")
	}

	// A typed config refuses it too; references are fine.
	cfg, err := gabitotl.LoadMemory(l)
	if err != nil {
		t.Fatal(err)
	}
	cfg.PostgresConnection = "postgres://app:hunter2@db.internal/shop"
	if err := gabitotl.SaveMemory(l, cfg); err == nil {
		t.Fatal("memory.md must refuse a connection string with a password")
	}
	cfg.PostgresConnection = "env:GABITOTL_DB_URL"
	if err := gabitotl.SaveMemory(l, cfg); err != nil {
		t.Fatalf("an env: reference must be accepted: %v", err)
	}
	for _, f := range []string{"memory.md", "project.md", "vcs.md"} {
		if strings.Contains(read(t, filepath.Join(l.Dir(), f)), "hunter2") {
			t.Fatalf("%s holds the secret", f)
		}
	}
}

// (i) The context-budget checkpoint fires once per session, saves state through
// Engram and survives a process restart (state lives in .gabitotl/local).
func TestContextBudgetCheckpointFiresOnce(t *testing.T) {
	m := newMachine(t)
	proj := m.repo("shop", map[string]string{"main.go": "package main\n"})
	m.ok(proj, "init", "--yes", "--user-name", "Ana")
	l := gabitotl.NewLayout(proj)
	wf, err := gabitotl.LoadWorkflow(l)
	if err != nil {
		t.Fatal(err)
	}
	cfg := budget.Config{Limit: 1000, Threshold: 0.75}
	mon, err := budget.New(l, cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if err := mon.ReportTokens(500); err != nil {
		t.Fatal(err)
	}
	if d, _ := mon.Check(); d.ShouldSave {
		t.Fatalf("500/1000 is under the 75%% threshold: %+v", d)
	}
	if err := mon.ReportTokens(800); err != nil {
		t.Fatal(err)
	}
	d, err := mon.Check()
	if err != nil || !d.ShouldSave || d.Message == "" {
		t.Fatalf("800/1000 must fire the checkpoint with a TIP: %+v %v", d, err)
	}
	rec := memory.NewRecorder(engramProvider(m, "shop"), wf)
	res, err := mon.Checkpoint(ctx, rec, budget.State{Project: "shop", Actor: "ana", Task: "login", Branch: "feature/login",
		Goal: "ship login", Progress: "login form done", NextStep: "wire the API", Decisions: []string{"use sessions"}})
	if err != nil || !res.Recorded || res.Message == "" {
		t.Fatalf("checkpoint: %+v %v", res, err)
	}
	saves := m.count("engram save")
	if saves < 1 {
		t.Fatalf("the checkpoint must save through engram: %v", m.calls())
	}
	contains(t, "engram saves", read(t, filepath.Join(m.dir, "engram.saves")), "wire the API", "use sessions")

	// Once: not again in this session, even after a restart or more tokens.
	if d, _ := mon.Check(); d.ShouldSave {
		t.Fatal("the checkpoint fired twice in one session")
	}
	mon2, err := budget.New(l, cfg)
	if err != nil {
		t.Fatal(err)
	}
	_ = mon2.ReportTokens(950)
	if d, _ := mon2.Check(); d.ShouldSave {
		t.Fatal("a restarted process must remember that the checkpoint already fired")
	}
	if m.count("engram save") != saves {
		t.Fatal("no further saves expected")
	}

	// A new session re-arms it.
	if err := mon2.NewSession(); err != nil {
		t.Fatal(err)
	}
	_ = mon2.ReportTokens(900)
	if d, _ := mon2.Check(); !d.ShouldSave {
		t.Fatal("a new session must re-arm the checkpoint")
	}
}
