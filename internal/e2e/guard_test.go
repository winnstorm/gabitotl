package e2e

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Branding strings are built at runtime so this repository passes its own guard.
var (
	gTrailer  = "Co-Authored-By: " + cl + " <noreply@" + "anthropic" + ".com>"
	gFooter   = "🤖 " + "Generated " + "with [" + cl + " Code](https://" + "claude.com/claude-" + "code)"
	gChatGPT  = "Generated " + "by " + "Chat" + "GPT"
	gSettings = "{\n  // my settings\n  \"theme\": \"dark\",\n  \"permissions\": {\"allow\": [\"Bash(go test:*)\"]}\n}\n"
)

// gitTry runs git and returns its combined output and error instead of failing.
func (m *machine) gitTry(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = m.env()
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	return strings.TrimSpace(out.String()), err
}

func setWorkflow(t *testing.T, proj, key, value string) {
	t.Helper()
	p := filepath.Join(proj, ".gabitotl", "workflow.md")
	s := read(t, p)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, key+":") {
			lines[i] = key + ": " + value
		}
	}
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Hooks installed by init strip attribution from a commit message, block it in
// block mode and refuse staged additions that credit an AI tool.
func TestGuardHooksStripAndBlock(t *testing.T) {
	m := newMachine(t)
	proj := m.repo("hooked", map[string]string{"a.txt": "a\n"})
	r := m.ok(proj, "init", "--yes", "--user-name", "Gabi Dev")
	contains(t, "init", r.out, "Hooks del guard instalados")
	for _, h := range []string{"commit-msg", "pre-commit"} {
		contains(t, h, read(t, filepath.Join(proj, ".git/hooks", h)), "gabitotl:guard:managed")
	}
	contains(t, "workflow.md", read(t, filepath.Join(proj, ".gabitotl/workflow.md")), "guard_hooks: installed")

	// A Claude-style trailer and footer never reach the history.
	m.write(filepath.Join(proj, "b.txt"), "b\n")
	m.git(proj, "add", "b.txt")
	out, err := m.gitTry(proj, "commit", "-q", "-m", "feat: add b\n\nWhy.\n\n"+gFooter+"\n\n"+gTrailer)
	if err != nil {
		t.Fatalf("strip mode must let the commit through: %v\n%s", err, out)
	}
	contains(t, "git output", out, "Saqué 2")
	if got := m.git(proj, "log", "-1", "--format=%B"); got != "feat: add b\n\nWhy." {
		t.Fatalf("commit message = %q", got)
	}

	// A ChatGPT-style footer is stripped too.
	m.write(filepath.Join(proj, "c.txt"), "c\n")
	m.git(proj, "add", "c.txt")
	if out, err := m.gitTry(proj, "commit", "-q", "-m", "fix: c\n\n"+gChatGPT); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got := m.git(proj, "log", "-1", "--format=%B"); got != "fix: c" {
		t.Fatalf("commit message = %q", got)
	}

	// Staged additions that credit an AI tool are refused (files are never rewritten by a hook).
	m.write(filepath.Join(proj, "d.md"), "# D\n"+gFooter+"\n")
	m.git(proj, "add", "d.md")
	head := m.git(proj, "rev-parse", "HEAD")
	out, err = m.gitTry(proj, "commit", "-q", "-m", "docs: add d")
	if err == nil || !strings.Contains(out, "Frené") || !strings.Contains(out, "d.md:2") {
		t.Fatalf("pre-commit must refuse (err=%v):\n%s", err, out)
	}
	if m.git(proj, "rev-parse", "HEAD") != head {
		t.Fatal("a refused commit must not move HEAD")
	}
	m.git(proj, "reset", "-q", "d.md")

	// Block mode refuses the message instead of editing it.
	setWorkflow(t, proj, "branding_guard", "block")
	m.write(filepath.Join(proj, "e.txt"), "e\n")
	m.git(proj, "add", "e.txt")
	out, err = m.gitTry(proj, "commit", "-q", "-m", "feat: e\n\n"+gTrailer)
	if err == nil || !strings.Contains(out, "Frené") {
		t.Fatalf("block mode must refuse (err=%v):\n%s", err, out)
	}
	if m.git(proj, "rev-parse", "HEAD") != head {
		t.Fatal("a refused commit must not move HEAD")
	}
	// A plain technical mention is never blocked.
	if out, err := m.gitTry(proj, "commit", "-q", "-m", "feat(runtime): add "+cl+" Code adapter"); err != nil {
		t.Fatalf("plain mentions pass: %v\n%s", err, out)
	}
}

// A hook the user already had is kept, chained after the guard, restored on
// uninstall and never duplicated by a second install.
func TestGuardChainsAndPreservesUserHooks(t *testing.T) {
	m := newMachine(t)
	proj := m.repo("chained", map[string]string{"a.txt": "a\n"})
	userHook := "#!/bin/sh\necho \"user hook ran on $(basename \"$1\")\" >> \"$(git rev-parse --git-dir)/chain.log\"\n"
	hookPath := filepath.Join(proj, ".git/hooks/commit-msg")
	m.write(hookPath, userHook)
	if err := os.Chmod(hookPath, 0o755); err != nil {
		t.Fatal(err)
	}

	r := m.ok(proj, "guard", "install-hooks")
	contains(t, "install-hooks", r.out, "Conservé tu hook commit-msg")
	if read(t, hookPath+".gabitotl-orig") != userHook {
		t.Fatal("the user hook must be preserved byte for byte")
	}
	first := read(t, hookPath)
	m.ok(proj, "guard", "install-hooks")
	if read(t, hookPath) != first || read(t, hookPath+".gabitotl-orig") != userHook {
		t.Fatal("install-hooks must be idempotent")
	}
	r = m.ok(proj, "guard", "status")
	contains(t, "status", r.out, "instalado (con tu hook encadenado)")

	m.write(filepath.Join(proj, "b.txt"), "b\n")
	m.git(proj, "add", "b.txt")
	if out, err := m.gitTry(proj, "commit", "-q", "-m", "feat: b\n\n"+gTrailer); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got := m.git(proj, "log", "-1", "--format=%B"); got != "feat: b" {
		t.Fatalf("the guard still strips: %q", got)
	}
	contains(t, "chain.log", read(t, filepath.Join(proj, ".git/chain.log")), "user hook ran on COMMIT_EDITMSG")

	m.ok(proj, "guard", "uninstall-hooks")
	if read(t, hookPath) != userHook || exists(hookPath+".gabitotl-orig") || exists(filepath.Join(proj, ".git/hooks/pre-commit")) {
		t.Fatal("uninstall must restore the user's hook and remove the guard's")
	}
	if info, err := os.Stat(hookPath); err != nil || info.Mode().Perm()&0o100 == 0 {
		t.Fatalf("the restored hook must stay executable: %v", err)
	}
}

// scan reports, fix removes only attribution lines after a backup, history is
// reported and never rewritten.
func TestGuardScanFixWithBackup(t *testing.T) {
	m := newMachine(t)
	proj := m.repo("fixme", map[string]string{"a.txt": "a\n"})
	m.ok(proj, "init", "--yes", "--user-name", "Gabi Dev", "--no-hooks")
	readme := "# App\n\n" + gFooter + "\n\nSupports " + cl + " Code as a runtime.\n"
	m.write(filepath.Join(proj, "README.md"), readme)
	m.write(filepath.Join(proj, "util.go"), "package main\n\n// @author "+cl+"\nfunc main() {}\n")

	r := m.run(proj, "guard", "scan", "--all")
	if r.code != 1 {
		t.Fatalf("scan must exit 1 on findings: %+v", r)
	}
	contains(t, "scan", r.out, "README.md:3 [generated]", "util.go:3 [header]")
	absent(t, "scan", r.out, "README.md:5") // the plain mention is not a finding
	r = m.run(proj, "guard", "scan", "--json")
	contains(t, "scan --json", r.out, `"file": "README.md"`, `"class": "generated"`)

	before := hash(t, []string{m.dir}, proj)
	r = m.ok(proj, "guard", "fix", "--dry-run")
	contains(t, "fix --dry-run", r.out, "README.md:3", "util.go:3")
	if hash(t, []string{m.dir}, proj) != before {
		t.Fatal("--dry-run changed files")
	}

	r = m.ok(proj, "guard", "fix", "--yes")
	contains(t, "fix", r.out, "Listo. Saqué 2 línea(s) en 2 archivo(s)", "Backup en .gabitotl/.bak/gabi-dev/")
	if got := read(t, filepath.Join(proj, "README.md")); got != "# App\n\nSupports "+cl+" Code as a runtime.\n" {
		t.Fatalf("README = %q", got)
	}
	if got := read(t, filepath.Join(proj, "util.go")); got != "package main\n\nfunc main() {}\n" {
		t.Fatalf("util.go = %q", got)
	}
	baks, _ := filepath.Glob(filepath.Join(proj, ".gabitotl/.bak/gabi-dev/*/README.md"))
	if len(baks) != 1 || read(t, baks[0]) != readme {
		t.Fatalf("the backup must hold the original README: %v", baks)
	}
	m.ok(proj, "guard", "scan", "--all")

	// History: a commit that already carries a trailer is reported, never rewritten.
	m.git(proj, "add", "-A")
	m.git(proj, "commit", "-q", "-m", "feat: add files\n\n"+gTrailer, "--no-verify")
	head := m.git(proj, "rev-parse", "HEAD")
	r = m.run(proj, "guard", "scan", "--history")
	if r.code != 1 || !strings.Contains(r.out, "history "+head[:8]) || !strings.Contains(r.out, "No reescribo la historia") {
		t.Fatalf("history audit: %+v", r)
	}
	if m.git(proj, "rev-parse", "HEAD") != head {
		t.Fatal("the guard must never rewrite history")
	}
}

// The Claude Code adapter writes managed attribution keys into settings.json
// and removal gives the user's file back byte for byte.
func TestGuardClaudeSettingsManagedKeys(t *testing.T) {
	m := newMachine(t)
	proj := m.repo("claude", map[string]string{"go.mod": "module x\n"})
	settings := filepath.Join(m.home, ".claude/settings.json")
	m.write(settings, gSettings)
	m.write(filepath.Join(m.home, ".claude/CLAUDE.md"), "# My rules\n")

	m.ok(proj, "install", "--yes", "--user", "gabi", "--runtime", "claude-code")
	got := read(t, settings)
	contains(t, "settings.json", got, `"commit": ""`, `"pr": ""`, `"sessionUrl": false`, "// my settings", `"theme": "dark"`)
	r := m.ok(proj, "doctor")
	contains(t, "doctor", r.out, "Autoría/branding", "no agrega su firma a commits ni PRs")

	// Idempotent.
	m.ok(proj, "install", "--yes", "--user", "gabi", "--runtime", "claude-code")
	if read(t, settings) != got {
		t.Fatal("a second install changed settings.json")
	}

	m.ok(proj, "uninstall", "--yes")
	if read(t, settings) != gSettings {
		t.Fatalf("uninstall must restore settings.json byte for byte:\n%s", read(t, settings))
	}
}

// uninstall takes out the guard hooks GabitoTL installed (found by their
// ownership marker), gives a chained user hook back byte for byte and reports
// both. A dry run and a runtime-only uninstall leave them alone.
func TestUninstallRemovesGuardHooksAndRestoresUserHooks(t *testing.T) {
	m := newMachine(t)
	proj := m.repo("hooked", map[string]string{"go.mod": "module x\n"})
	preCommit := filepath.Join(proj, ".git/hooks/pre-commit")
	commitMsg := filepath.Join(proj, ".git/hooks/commit-msg")
	userHook := "#!/bin/sh\n# the user's own hook, with odd bytes: \t trailing  \necho mine\n"
	m.write(preCommit, userHook)
	if err := os.Chmod(preCommit, 0o755); err != nil {
		t.Fatal(err)
	}
	m.write(filepath.Join(m.home, ".claude/CLAUDE.md"), "# My rules\n")

	m.ok(proj, "install", "--yes", "--user", "gabi", "--runtime", "claude-code")
	if !strings.Contains(read(t, commitMsg), "gabitotl:guard:managed") || !strings.Contains(read(t, preCommit), "gabitotl:guard:managed") || read(t, preCommit+".gabitotl-orig") != userHook {
		t.Fatal("install must have wrapped the hooks and chained the user's")
	}
	managed := read(t, commitMsg)

	r := m.ok(proj, "uninstall", "--dry-run")
	contains(t, "plan", r.out, "git:", "tu hook de Git original")
	m.ok(proj, "uninstall", "--yes", "--runtime", "claude-code")
	if read(t, commitMsg) != managed || !exists(preCommit+".gabitotl-orig") {
		t.Fatal("a runtime-only uninstall and a dry run must not touch the hooks")
	}
	m.ok(proj, "install", "--yes", "--user", "gabi", "--runtime", "claude-code")

	r = m.ok(proj, "uninstall", "--yes")
	contains(t, "uninstall", r.out, "Saqué el hook de Git de GabitoTL", "Volvió tu hook original, byte por byte")
	if exists(commitMsg) || exists(preCommit+".gabitotl-orig") {
		t.Fatal("the guard's hooks and the aside copy must be gone")
	}
	if read(t, preCommit) != userHook {
		t.Fatal("the user's hook must come back byte for byte")
	}
	if info, err := os.Stat(preCommit); err != nil || info.Mode().Perm()&0o100 == 0 {
		t.Fatalf("the restored hook must stay executable: %v", err)
	}
	if !strings.Contains(read(t, filepath.Join(proj, ".gabitotl", "workflow.md")), "guard_hooks: ask") {
		t.Fatal("the remembered answer must be reset so a later install offers the hooks again")
	}
	// Idempotent: nothing left to do.
	r = m.ok(proj, "uninstall", "--yes")
	if strings.Contains(r.out, "hook") {
		t.Fatalf("second uninstall: %s", r.out)
	}
	if read(t, preCommit) != userHook {
		t.Fatal("the user hook must survive a second uninstall")
	}
}

// Only hooks with the ownership marker are ever removed: a hook the user wrote
// in place of ours after the install is not ours to delete.
func TestUninstallLeavesForeignHooksAlone(t *testing.T) {
	m := newMachine(t)
	proj := m.repo("foreign", map[string]string{"go.mod": "module x\n"})
	m.write(filepath.Join(m.home, ".claude/CLAUDE.md"), "# My rules\n")
	m.ok(proj, "install", "--yes", "--user", "gabi", "--runtime", "claude-code")
	preCommit := filepath.Join(proj, ".git/hooks/pre-commit")
	mine := "#!/bin/sh\necho replaced by the user\n"
	m.write(preCommit, mine)
	if err := os.Chmod(preCommit, 0o755); err != nil {
		t.Fatal(err)
	}
	m.ok(proj, "uninstall", "--yes")
	if read(t, preCommit) != mine {
		t.Fatal("a hook that is not ours must be left alone")
	}
	if exists(filepath.Join(proj, ".git/hooks/commit-msg")) {
		t.Fatal("the hook that is ours must be removed")
	}
}
