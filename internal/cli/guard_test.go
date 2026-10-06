package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
)

// Branding strings are built at runtime so this repository passes its own guard.
var (
	gLLM     = "Cla" + "ude"
	gTrailer = "Co-Authored-By: " + gLLM + " <noreply@" + "anthropic" + ".com>"
	gFooter  = "Generated " + "with " + gLLM + " Code"
)

func gitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")
	root := t.TempDir()
	if out, err := exec.Command("git", "-C", root, "init", "-q", "-b", "main").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	return root
}

func fakeBinary(t *testing.T) {
	t.Helper()
	old := executablePath
	executablePath = func() (string, error) { return "/fake/bin/gabitotl", nil }
	t.Cleanup(func() { executablePath = old })
}

func gwrite(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func initGit(t *testing.T, extra ...string) string {
	t.Helper()
	root := gitRepo(t)
	args := append([]string{"init", "--root", root, "--name", "Shop", "--kind", "new", "--user-id", "gabi", "--user-name", "Gabi", "--yes"}, extra...)
	if r := runIn(t, "", false, t.TempDir(), args...); r.code != 0 {
		t.Fatalf("init: %s", r.err)
	}
	return root
}

func TestInitInstallsHooksOnceAndRemembers(t *testing.T) {
	fakeBinary(t)
	root := initGit(t)
	for _, h := range []string{"commit-msg", "pre-commit"} {
		b, err := os.ReadFile(filepath.Join(root, ".git", "hooks", h))
		if err != nil || !strings.Contains(string(b), "gabitotl:guard:managed") {
			t.Fatalf("%s not installed: %v", h, err)
		}
	}
	wf, _ := gabitotl.LoadWorkflow(layoutOf(root))
	if wf.GuardHooks != gabitotl.HooksInstalled {
		t.Fatalf("guard_hooks = %q", wf.GuardHooks)
	}
	// Uninstall is remembered as declined and init does not bring them back.
	if r := runIn(t, "", false, root, "guard", "uninstall-hooks"); r.code != 0 {
		t.Fatal(r.err)
	}
	if r := runIn(t, "", false, t.TempDir(), "init", "--root", root, "--yes"); r.code != 0 {
		t.Fatal(r.err)
	}
	if _, err := os.Stat(filepath.Join(root, ".git", "hooks", "commit-msg")); err == nil {
		t.Fatal("a declined answer must be remembered")
	}
}

func TestInitNoHooksFlagAndNonGit(t *testing.T) {
	fakeBinary(t)
	root := initGit(t, "--no-hooks")
	if _, err := os.Stat(filepath.Join(root, ".git", "hooks", "commit-msg")); err == nil {
		t.Fatal("--no-hooks must not install")
	}
	if wf, _ := gabitotl.LoadWorkflow(layoutOf(root)); wf.GuardHooks != gabitotl.HooksDeclined {
		t.Fatalf("guard_hooks = %q", wf.GuardHooks)
	}
	plain := t.TempDir()
	if r := runIn(t, "", false, t.TempDir(), "init", "--root", plain, "--yes"); r.code != 0 {
		t.Fatal(r.err)
	}
	if wf, _ := gabitotl.LoadWorkflow(layoutOf(plain)); wf.GuardHooks != gabitotl.HooksAsk {
		t.Fatalf("a non-git project is not asked: %q", wf.GuardHooks)
	}
}

func TestInitAsksOnTTY(t *testing.T) {
	fakeBinary(t)
	root := gitRepo(t)
	r := runIn(t, "n\n", true, t.TempDir(), "init", "--root", root, "--name", "Shop", "--kind", "new", "--user-name", "Gabi", "--mode", "solo")
	if r.code != 0 || !strings.Contains(r.out, "no instalo los hooks") {
		t.Fatalf("%+v", r)
	}
	if wf, _ := gabitotl.LoadWorkflow(layoutOf(root)); wf.GuardHooks != gabitotl.HooksDeclined {
		t.Fatalf("guard_hooks = %q", wf.GuardHooks)
	}
}

func TestGuardScanAllStagedDiffJSON(t *testing.T) {
	fakeBinary(t)
	root := initGit(t, "--no-hooks")
	gwrite(t, root, "ok.md", "Supports "+gLLM+" Code as a runtime.\n")
	if r := runIn(t, "", false, root, "guard", "scan"); r.code != 0 || !strings.Contains(r.out, "no encontré") {
		t.Fatalf("clean scan: %+v", r)
	}
	gwrite(t, root, "bad.md", "# T\n"+gFooter+"\n")
	r := runIn(t, "", false, root, "guard", "scan", "--all")
	if r.code != 1 || !strings.Contains(r.out, "bad.md:2 [generated]") {
		t.Fatalf("scan: %+v", r)
	}
	r = runIn(t, "", false, root, "guard", "scan", "--json")
	var rep struct {
		Mode     string
		Findings []struct {
			File, Class, Action string
			Line                int
		}
	}
	if err := json.Unmarshal([]byte(r.out), &rep); err != nil || r.code != 1 || len(rep.Findings) != 1 || rep.Findings[0].File != "bad.md" || rep.Mode != "strip" {
		t.Fatalf("json: %v %+v", err, r)
	}
	if out, err := exec.Command("git", "-C", root, "add", "bad.md").CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if r := runIn(t, "", false, root, "guard", "scan", "--staged"); r.code != 1 {
		t.Fatalf("staged: %+v", r)
	}
	if r := runIn(t, "", false, root, "guard", "scan", "--staged", "--all"); r.code != 2 {
		t.Fatalf("flags are exclusive: %+v", r)
	}
	// Allowlisted paths are skipped.
	wf, _ := gabitotl.LoadWorkflow(layoutOf(root))
	wf.BrandingAllowPaths = []string{"bad.md"}
	if err := gabitotl.SaveWorkflow(layoutOf(root), wf); err != nil {
		t.Fatal(err)
	}
	if r := runIn(t, "", false, root, "guard", "scan"); r.code != 0 {
		t.Fatalf("allowlist: %+v", r)
	}
}

func TestGuardPreCommitHookModes(t *testing.T) {
	fakeBinary(t)
	root := initGit(t, "--no-hooks")
	gwrite(t, root, "bad.md", gFooter+"\n")
	if out, err := exec.Command("git", "-C", root, "add", "bad.md").CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	set := func(mode string) {
		wf, _ := gabitotl.LoadWorkflow(layoutOf(root))
		wf.BrandingGuard = mode
		if err := gabitotl.SaveWorkflow(layoutOf(root), wf); err != nil {
			t.Fatal(err)
		}
	}
	for mode, want := range map[string]int{"strip": 1, "block": 1, "warn": 0, "off": 0} {
		set(mode)
		r := runIn(t, "", false, root, "guard", "scan", "--staged", "--hook")
		if r.code != want {
			t.Errorf("%s: exit %d want %d (%s)", mode, r.code, want, r.err)
		}
		if mode == "block" && !strings.Contains(r.err, "Frené") {
			t.Errorf("block must explain in Gabito voice: %q", r.err)
		}
	}
}

func TestGuardCommitMsg(t *testing.T) {
	fakeBinary(t)
	root := initGit(t, "--no-hooks")
	msgFile := filepath.Join(t.TempDir(), "COMMIT_EDITMSG")
	msg := "feat: x\n\nBody.\n\n" + gFooter + "\n\n" + gTrailer + "\n"
	set := func(mode string) {
		wf, _ := gabitotl.LoadWorkflow(layoutOf(root))
		wf.BrandingGuard = mode
		if err := gabitotl.SaveWorkflow(layoutOf(root), wf); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		mode string
		code int
		body string
		said string
	}{
		{"strip", 0, "feat: x\n\nBody.\n", "Saqué 2"},
		{"block", 1, msg, "Frené"},
		{"warn", 0, msg, "modo warn"},
		{"off", 0, msg, ""},
	}
	for _, c := range cases {
		set(c.mode)
		if err := os.WriteFile(msgFile, []byte(msg), 0o600); err != nil {
			t.Fatal(err)
		}
		r := runIn(t, "", false, root, "guard", "commit-msg", msgFile)
		got, _ := os.ReadFile(msgFile)
		if r.code != c.code || string(got) != c.body || !strings.Contains(r.err, c.said) {
			t.Errorf("%s: code=%d body=%q err=%q", c.mode, r.code, got, r.err)
		}
	}
	// A clean message is left byte for byte as it was.
	set("strip")
	clean := "feat: y\n\nSupports " + gLLM + " Code.\n"
	_ = os.WriteFile(msgFile, []byte(clean), 0o600)
	if r := runIn(t, "", false, root, "guard", "commit-msg", msgFile); r.code != 0 {
		t.Fatal(r.err)
	}
	if got, _ := os.ReadFile(msgFile); string(got) != clean {
		t.Fatalf("clean message changed: %q", got)
	}
}

func TestGuardFixCLI(t *testing.T) {
	fakeBinary(t)
	root := initGit(t, "--no-hooks")
	gwrite(t, root, "README.md", "# App\n\n"+gFooter+"\n\nText.\n")
	gwrite(t, root, "tool.py", "__author__ = \""+gLLM+"\"\n")
	r := runIn(t, "", false, root, "guard", "fix", "--dry-run")
	if r.code != 1 || !strings.Contains(r.out, "README.md:3") || !strings.Contains(r.out, "tool.py:1") || !strings.Contains(r.out, "a mano") {
		t.Fatalf("dry-run: %+v", r)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "README.md")); !strings.Contains(string(b), gLLM) {
		t.Fatal("dry-run must not change anything")
	}
	if r := runIn(t, "", false, root, "guard", "fix"); r.code != 1 || !strings.Contains(r.out, "--yes") {
		t.Fatalf("without --yes and without a terminal nothing is applied: %+v", r)
	}
	r = runIn(t, "", false, root, "guard", "fix", "--yes")
	if r.code != 1 || !strings.Contains(r.out, "Backup en .gabitotl/.bak/gabi/") {
		t.Fatalf("fix: %+v", r)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "README.md")); string(b) != "# App\n\nText.\n" {
		t.Fatalf("README = %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "tool.py")); !strings.Contains(string(b), gLLM) {
		t.Fatal("manual cases must stay untouched")
	}
	baks, _ := filepath.Glob(filepath.Join(root, ".gabitotl", ".bak", "gabi", "*", "README.md"))
	if len(baks) != 1 {
		t.Fatalf("backup missing: %v", baks)
	}
}

func TestGuardStatusAndHistoryNote(t *testing.T) {
	fakeBinary(t)
	root := initGit(t)
	r := runIn(t, "", false, root, "guard", "status")
	if r.code != 0 || !strings.Contains(r.out, "Modo del guard: strip") || strings.Count(r.out, "instalado") < 2 {
		t.Fatalf("%+v", r)
	}
	r = runIn(t, "", false, root, "guard", "scan", "--history")
	if r.code != 0 {
		t.Fatalf("an empty repo has nothing to report: %+v", r)
	}
}
