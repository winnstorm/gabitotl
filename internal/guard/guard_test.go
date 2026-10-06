package guard

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/winnstorm/gabitotl/internal/branding"
	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/vcs"
)

// Branding strings are built at runtime so this repository passes its own guard.
var (
	llm     = "Cla" + "ude"
	trailer = "Co-Authored-By: " + llm + " <noreply@" + "anthropic" + ".com>"
	footer  = "Generated " + "with " + llm + " Code"
)

var gitEnv = []string{
	"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=t@example.com",
	"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=t@example.com",
	"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
}

func runner() vcs.Runner { return vcs.ExecRunner{Env: gitEnv} }

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := runner().Run(context.Background(), dir, "git", args...)
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return out
}

func newRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	return dir
}

func put(t *testing.T, dir, rel, body string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestHooksInstallChainUninstall(t *testing.T) {
	dir := newRepo(t)
	ctx := context.Background()
	hd, err := HooksDir(ctx, runner(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(hd, 0o755); err != nil {
		t.Fatal(err)
	}
	userHook := "#!/bin/sh\necho user-hook-ran \"$@\" > \"$(dirname \"$0\")/../user-ran\"\n"
	put(t, hd, "commit-msg", userHook)
	if err := os.Chmod(filepath.Join(hd, "commit-msg"), 0o755); err != nil {
		t.Fatal(err)
	}

	st, err := InstallHooks(hd, "/opt/gabitotl/bin")
	if err != nil || !AllInstalled(st) {
		t.Fatalf("install: %+v %v", st, err)
	}
	if !st[0].Chained || st[1].Chained {
		t.Fatalf("only commit-msg had a user hook: %+v", st)
	}
	if read(t, filepath.Join(hd, "commit-msg"+OrigSuffix)) != userHook {
		t.Fatal("the user hook must be preserved byte for byte")
	}
	first := read(t, filepath.Join(hd, "commit-msg"))
	if !strings.Contains(first, Marker) {
		t.Fatal("ownership marker missing")
	}
	// Idempotent: a second install changes nothing and never re-chains our own wrapper.
	if _, err := InstallHooks(hd, "/opt/gabitotl/bin"); err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(hd, "commit-msg")) != first || read(t, filepath.Join(hd, "commit-msg"+OrigSuffix)) != userHook {
		t.Fatal("install must be idempotent")
	}

	// The wrapper runs the guard first, then chains to the user hook.
	fake := filepath.Join(t.TempDir(), "gabitotl")
	put(t, filepath.Dir(fake), "gabitotl", "#!/bin/sh\necho \"$@\" > \"$(dirname \"$0\")/guard-ran\"\nexit ${FAKE_EXIT:-0}\n")
	if err := os.Chmod(fake, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallHooks(hd, fake); err != nil {
		t.Fatal(err)
	}
	msg := filepath.Join(dir, "MSG")
	put(t, dir, "MSG", "feat: x\n")
	cmd := exec.Command(filepath.Join(hd, "commit-msg"), msg)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("hook: %v %s", err, out)
	}
	if !strings.Contains(read(t, filepath.Join(filepath.Dir(fake), "guard-ran")), "guard commit-msg "+msg) {
		t.Fatal("the guard must run")
	}
	if !strings.Contains(read(t, filepath.Join(filepath.Dir(hd), "user-ran")), "user-hook-ran "+msg) {
		t.Fatal("the user hook must be chained with the original arguments")
	}
	// A failing guard blocks and the user hook does not run.
	_ = os.Remove(filepath.Join(filepath.Dir(hd), "user-ran"))
	cmd = exec.Command(filepath.Join(hd, "commit-msg"), msg)
	cmd.Env = append(os.Environ(), "FAKE_EXIT=1")
	if err := cmd.Run(); err == nil {
		t.Fatal("a failing guard must block the commit")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(hd), "user-ran")); err == nil {
		t.Fatal("user hook must not run after a refusal")
	}

	st, err = UninstallHooks(hd)
	if err != nil {
		t.Fatal(err)
	}
	if st[0].Installed || !st[0].Foreign || read(t, filepath.Join(hd, "commit-msg")) != userHook {
		t.Fatalf("uninstall must restore the user hook: %+v", st)
	}
	if _, err := os.Stat(filepath.Join(hd, "pre-commit")); err == nil {
		t.Fatal("pre-commit wrapper must be gone")
	}
	if _, err := UninstallHooks(hd); err != nil {
		t.Fatal("uninstall is idempotent")
	}
}

func TestMissingBinaryNeverBlocks(t *testing.T) {
	dir := newRepo(t)
	hd, _ := HooksDir(context.Background(), runner(), dir)
	if _, err := InstallHooks(hd, "/nonexistent/gabitotl"); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(filepath.Join(hd, "pre-commit"))
	cmd.Env = []string{"PATH=/usr/bin:/bin"}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("a missing binary must not block: %v %s", err, out)
	}
}

func TestHooksPathAndWorktree(t *testing.T) {
	dir := newRepo(t)
	ctx := context.Background()
	put(t, dir, "a.txt", "a\n")
	git(t, dir, "add", "a.txt")
	git(t, dir, "commit", "-q", "-m", "chore: init")
	wt := filepath.Join(t.TempDir(), "wt")
	git(t, dir, "worktree", "add", "-q", "-b", "other", wt)
	main, _ := HooksDir(ctx, runner(), dir)
	linked, _ := HooksDir(ctx, runner(), wt)
	if main != linked {
		t.Fatalf("a worktree shares the hooks of the main repo: %s vs %s", main, linked)
	}
	custom := filepath.Join(t.TempDir(), "myhooks")
	git(t, dir, "config", "core.hooksPath", custom)
	got, _ := HooksDir(ctx, runner(), dir)
	if got != custom {
		t.Fatalf("core.hooksPath must be honored: %s vs %s", got, custom)
	}
	if _, err := HooksDir(ctx, runner(), t.TempDir()); err != ErrNotRepo {
		t.Fatalf("outside a repo: %v", err)
	}
}

func TestParseAddedAndScanStaged(t *testing.T) {
	diff := "diff --git a/x.md b/x.md\n--- a/x.md\n+++ b/x.md\n@@ -1,0 +5,2 @@\n+fine\n+" + footer + "\n@@ -9 +20 @@\n-old\n+" + trailer + "\n--- a/gone\n+++ /dev/null\n@@ -1 +0,0 @@\n-removed " + footer + "\n"
	added := ParseAdded(diff)
	if len(added) != 3 || added[1].Line != 6 || added[2].Line != 20 || added[2].File != "x.md" {
		t.Fatalf("added = %+v", added)
	}
	fs := ScanAdded(branding.Default(), nil, added)
	if len(fs) != 2 || fs[0].Line != 6 || fs[1].Line != 20 {
		t.Fatalf("findings = %+v", fs)
	}
	if got := ScanAdded(branding.Default(), []string{"x.md"}, added); len(got) != 0 {
		t.Fatalf("allowlist must apply: %+v", got)
	}

	dir := newRepo(t)
	put(t, dir, "old.md", footer+"\n")
	git(t, dir, "add", "old.md")
	git(t, dir, "commit", "-q", "-m", "chore: old")
	put(t, dir, "new.md", "ok\n"+footer+"\n")
	put(t, dir, "plain.md", "Supports "+llm+" Code.\n")
	git(t, dir, "add", "new.md", "plain.md")
	s := Settings{Mode: gabitotl.GuardStrip}
	got, err := ScanStaged(context.Background(), runner(), dir, s)
	if err != nil || len(got) != 1 || got[0].File != "new.md" || got[0].Line != 2 {
		t.Fatalf("staged = %+v %v (only additions count, old content is ignored)", got, err)
	}
	all, err := ScanAll(context.Background(), runner(), dir, s)
	if err != nil || len(all) != 2 {
		t.Fatalf("all = %+v %v", all, err)
	}
	diffs, err := ScanDiff(context.Background(), runner(), dir, "HEAD", s)
	if err != nil || len(diffs) != 1 {
		t.Fatalf("diff HEAD = %+v %v", diffs, err)
	}
	if _, err := ScanDiff(context.Background(), runner(), dir, "--output=x", s); err == nil {
		t.Fatal("option-looking refs must be refused")
	}
}

func TestScanHistory(t *testing.T) {
	dir := newRepo(t)
	put(t, dir, "a.txt", "a\n")
	git(t, dir, "add", "a.txt")
	git(t, dir, "commit", "-q", "-m", "feat: a\n\n"+trailer)
	put(t, dir, "b.txt", "b\n")
	git(t, dir, "add", "b.txt")
	ai := vcs.ExecRunner{Env: append(append([]string(nil), gitEnv...),
		"GIT_AUTHOR_NAME="+llm, "GIT_AUTHOR_EMAIL=noreply@"+"anthropic"+".com",
		"GIT_COMMITTER_NAME="+llm, "GIT_COMMITTER_EMAIL=noreply@"+"anthropic"+".com")}
	if _, err := ai.Run(context.Background(), dir, "git", "commit", "-q", "-m", "feat: b"); err != nil {
		t.Fatal(err)
	}
	put(t, dir, "c.txt", "c\n")
	git(t, dir, "add", "c.txt")
	git(t, dir, "commit", "-q", "-m", "feat: clean")
	head := git(t, dir, "rev-parse", "HEAD")
	got, err := ScanHistory(context.Background(), runner(), dir, Settings{})
	if err != nil {
		t.Fatal(err)
	}
	where := map[string]int{}
	for _, f := range got {
		where[f.Where]++
		if f.Commit == head {
			t.Fatalf("clean commit flagged: %+v", f)
		}
	}
	if where["message"] != 1 || where["author"] != 1 || where["committer"] != 1 {
		t.Fatalf("history = %+v", got)
	}
	if after := git(t, dir, "rev-parse", "HEAD"); after != head {
		t.Fatal("history scan must not move HEAD")
	}
}

func TestRemoveLines(t *testing.T) {
	in := "title\r\n\r\n" + footer + "\r\n\r\nbody\r\n"
	if got := string(RemoveLines([]byte(in), []int{3})); got != "title\r\n\r\nbody\r\n" {
		t.Fatalf("got %q", got)
	}
	if got := string(RemoveLines([]byte("a\n"+footer+"\n"), []int{2})); got != "a\n" {
		t.Fatalf("got %q", got)
	}
	if got := string(RemoveLines([]byte("a\nb"), nil)); got != "a\nb" {
		t.Fatalf("untouched content must be identical: %q", got)
	}
}

func TestPlanAndApplyFix(t *testing.T) {
	root := t.TempDir()
	put(t, root, "README.md", "# App\n\n"+footer+"\n\nText.\n")
	put(t, root, "main.go", "package main\n\n// @author "+llm+"\nvar __author__ = \"x\"\nvar a = \"Thanks, "+footer+"\"\n")
	put(t, root, "tool.py", "__author__ = \""+llm+"\"\n")
	put(t, root, "docs/keep.md", footer+"\n")
	put(t, root, ".env", "# Written by "+llm+"\n")
	put(t, root, ".gabitotl/.bak/u/x.md", footer+"\n")
	s := Settings{AllowPaths: []string{"docs/"}}
	rels := []string{"README.md", "main.go", "tool.py", "docs/keep.md", ".env", ".gabitotl/.bak/u/x.md"}
	plan, err := PlanFix(root, rels, s)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Fixes) != 3 {
		t.Fatalf("fixes = %+v", plan.Fixes)
	}
	var manual []string
	for _, m := range plan.Manual {
		manual = append(manual, m.File)
	}
	if len(plan.Manual) != 2 || !(slicesHas(manual, "tool.py") && slicesHas(manual, "main.go")) {
		t.Fatalf("code that may be logic goes to manual review: %+v", plan.Manual)
	}
	before := read(t, filepath.Join(root, "README.md"))
	res, err := ApplyFix(root, "ana", gabitotl.DefaultWorkflow().Backup, plan)
	if err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(root, "README.md")); got != "# App\n\nText.\n" {
		t.Fatalf("README = %q", got)
	}
	if got := read(t, filepath.Join(root, "main.go")); strings.Contains(got, "@author") || !strings.Contains(got, "var __author__") {
		t.Fatalf("main.go = %q", got)
	}
	if read(t, filepath.Join(root, "tool.py")) == "" || read(t, filepath.Join(root, "docs/keep.md")) != footer+"\n" {
		t.Fatal("manual and allowlisted files must not change")
	}
	if read(t, filepath.Join(root, ".env")) != "# Written by "+llm+"\n" {
		t.Fatal("a secret-looking file is never backed up, so it is never rewritten")
	}
	if len(res.Written) != 2 || res.Snapshot == "" || len(res.Skipped) == 0 {
		t.Fatalf("applied = %+v", res)
	}
	bak := filepath.Join(root, ".gabitotl", ".bak", "ana", res.Snapshot, "README.md")
	if read(t, bak) != before {
		t.Fatal("the backup must hold the original")
	}
}

func slicesHas(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}
