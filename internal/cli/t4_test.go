package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/winnstorm/gabitotl/internal/coordination"
	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/memory/providers"
	rt "github.com/winnstorm/gabitotl/internal/runtime"
)

const (
	fxUser   = "# My rules\n\nBe kind.\n"
	fxGentle = "<!-- gentle-ai:persona -->\nGentleman.\n<!-- /gentle-ai:persona -->\n"
	fxOC     = "{\n  \"provider\": {\"anthropic\": {}},\n  \"model\": \"anthropic/claude-sonnet-4-5\"\n}\n"
)

// fakeHome builds a home with a Claude config (user content plus a Gentleman
// block), an OpenCode config and Kiro steering, and points every hook at it.
func fakeHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	wr := func(rel, s string) {
		p := filepath.Join(home, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	wr(".claude/CLAUDE.md", fxUser+"\n"+fxGentle)
	wr(".config/opencode/opencode.json", fxOC)
	wr(".kiro/steering/product.md", "# Product\n")
	oldEnv, oldProj := runtimeEnvFn, engramProjectsFn
	runtimeEnvFn = func(root string) (rt.Env, error) {
		return rt.Env{Home: home, ProjectRoot: root, StateDir: filepath.Join(home, ".gabitotl"), GOOS: "linux",
			Getenv:   func(string) string { return "" },
			LookPath: func(n string) (string, error) { return "/fake/" + n, nil },
			Exists:   func(p string) bool { _, err := os.Stat(p); return err == nil },
			Now:      func() time.Time { return time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC) }}, nil
	}
	engramProjectsFn = func(context.Context) ([]providers.EngramProject, error) {
		return []providers.EngramProject{{Name: "Shop", Observations: 7}}, nil
	}
	t.Cleanup(func() { runtimeEnvFn, engramProjectsFn = oldEnv, oldProj })
	return home
}

func slurp(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestRuntimePlanApplyRemoveFlow(t *testing.T) {
	home := fakeHome(t)
	root := initProject(t)
	md := filepath.Join(home, ".claude", "CLAUDE.md")
	oc := filepath.Join(home, ".config", "opencode", "opencode.json")

	r := runIn(t, "", false, root, "runtime", "detect", "--runtime", "claude-code,kiro-ide")
	if r.code != 0 || !strings.Contains(r.out, "Claude Code: installed") {
		t.Fatalf("%+v", r)
	}
	r = runIn(t, "", false, root, "runtime", "plan", "--root", root)
	if r.code != 0 || !strings.Contains(r.out, "MODIFY") || !strings.Contains(r.out, "PRESERVE") {
		t.Fatalf("%+v", r)
	}
	if slurp(t, md) != fxUser+"\n"+fxGentle || slurp(t, oc) != fxOC {
		t.Fatal("plan must not write")
	}
	if r := runIn(t, "", false, root, "runtime", "apply", "--root", root); r.code != ExitUsage {
		t.Fatalf("apply without --runtime must be refused: %+v", r)
	}
	args := []string{"runtime", "apply", "--root", root, "--runtime", "claude-code,opencode,kiro-cli,kiro-ide", "--migrate-persona"}
	r = runIn(t, "", false, root, args...)
	if r.code != 0 || !strings.Contains(r.out, "backup:") {
		t.Fatalf("%+v", r)
	}
	got := slurp(t, md)
	if strings.Contains(got, "gentle-ai") || !strings.HasPrefix(got, fxUser) || !strings.Contains(got, "gabitotl:start persona") {
		t.Fatalf("claude md:\n%s", got)
	}
	if !strings.Contains(slurp(t, oc), `"anthropic"`) || !strings.Contains(slurp(t, oc), "gabitotl-persona.md") {
		t.Fatalf("opencode:\n%s", slurp(t, oc))
	}
	if _, err := os.Stat(filepath.Join(home, ".gabitotl", "state.json")); err != nil {
		t.Fatal("ownership state missing")
	}
	// Idempotent re-apply and verify.
	r = runIn(t, "", false, root, args...)
	if r.code != 0 || strings.Contains(r.out, "applied") || slurp(t, md) != got {
		t.Fatalf("re-apply: %+v", r)
	}
	if r := runIn(t, "", false, root, "runtime", "verify", "--root", root, "--runtime", "claude-code,opencode,kiro-cli,kiro-ide"); r.code != 0 || strings.Contains(r.out, "FAIL") {
		t.Fatalf("verify: %+v", r)
	}
	r = runIn(t, "", false, root, "runtime", "remove", "--root", root, "--runtime", "claude-code,opencode,kiro-cli,kiro-ide")
	if r.code != 0 {
		t.Fatalf("%+v", r)
	}
	if after := slurp(t, md); strings.Contains(after, "gabitotl") || !strings.HasPrefix(after, fxUser) {
		t.Fatalf("remove damaged user content:\n%s", after)
	}
	if slurp(t, oc) != fxOC {
		t.Fatalf("opencode not restored:\n%s", slurp(t, oc))
	}
	if slurp(t, filepath.Join(home, ".kiro", "steering", "product.md")) != "# Product\n" {
		t.Fatal("kiro user steering touched")
	}
}

func TestRuntimeSetSeparatesRuntimeFromModel(t *testing.T) {
	fakeHome(t)
	root := initProject(t)
	before := slurp(t, filepath.Join(root, ".gabitotl", "workflow.md"))
	if r := runIn(t, "", false, root, "runtime", "set", "--runtime", "opencode", "--model", "anthropic/claude-sonnet-4-5"); r.code != 0 || !strings.Contains(r.out, "opencode (project)") {
		t.Fatalf("%+v", r)
	}
	if r := runIn(t, "", false, root, "runtime", "set", "--model", "openai/gpt-5", "--local"); r.code != 0 || !strings.Contains(r.out, "model: openai/gpt-5 (local)") {
		t.Fatalf("%+v", r)
	}
	if r := runIn(t, "", false, root, "runtime", "list"); !strings.Contains(r.out, "* opencode") {
		t.Fatalf("%+v", r)
	}
	if r := runIn(t, "", false, root, "runtime", "set", "--runtime", "nope"); r.code != ExitUsage {
		t.Fatalf("%+v", r)
	}
	if slurp(t, filepath.Join(root, ".gabitotl", "workflow.md")) != before {
		t.Fatal("workflow changed")
	}
	// The chosen (local) model reaches only the selected runtime's plan.
	r := runIn(t, "", false, root, "runtime", "plan", "--root", root, "--runtime", "opencode,claude-code")
	if !strings.Contains(r.out, "key model") {
		t.Fatalf("model not planned for opencode: %s", r.out)
	}
	if strings.Count(r.out, "key model") != 1 {
		t.Fatalf("model leaked to another runtime: %s", r.out)
	}
}

func TestImportDryRunThenRunThenUnchanged(t *testing.T) {
	fakeHome(t)
	root := initProject(t)
	withProvider(t, &stubProvider{})
	if err := os.MkdirAll(filepath.Join(root, ".kiro", "steering"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".kiro", "steering", "tech.md"), []byte("# Tech\nGo, stdlib first.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	imports := filepath.Join(root, ".gabitotl", "imports.md")
	before := slurp(t, imports)
	r := runIn(t, "", false, root, "import", "run", "--source", "kiro", "--dry-run")
	if r.code != 0 || !strings.Contains(r.out, "kiro: would-import") || !strings.Contains(r.out, "steering: tech") || slurp(t, imports) != before {
		t.Fatalf("%+v", r)
	}
	r = runIn(t, "", false, root, "import", "run", "--source", "kiro")
	if r.code != 0 || !strings.Contains(r.out, "kiro: imported") || !strings.Contains(slurp(t, imports), "origin: kiro") {
		t.Fatalf("%+v", r)
	}
	if r = runIn(t, "", false, root, "import", "run", "--source", "kiro"); !strings.Contains(r.out, "kiro: unchanged") {
		t.Fatalf("%+v", r)
	}
	if r = runIn(t, "", false, root, "import", "detect"); r.code != 0 || !strings.Contains(r.out, "engram:") {
		t.Fatalf("%+v", r)
	}
	if r = runIn(t, "", false, root, "import", "run", "--source", "nope"); r.code != ExitUsage {
		t.Fatalf("%+v", r)
	}
}

func TestWorkStartUpdateFeedsSoftLocks(t *testing.T) {
	root := initProject(t)
	r := runIn(t, "", false, root, "work", "start", "--task", "auth", "--areas", "internal/auth, api", "--branch", "feature/auth")
	if r.code != 0 || !strings.Contains(r.out, "gabi/auth [in-progress]") {
		t.Fatalf("%+v", r)
	}
	if r = runIn(t, "", false, root, "work", "update", "--task", "auth", "--status", "blocked", "--next", "wait for review"); r.code != 0 || !strings.Contains(r.out, "[blocked]") {
		t.Fatalf("%+v", r)
	}
	tk, err := coordination.ReadTask(gabitotl.NewLayout(root), "gabi", "auth")
	if err != nil || tk.Branch != "feature/auth" || len(tk.Areas) != 2 || tk.State.Next != "wait for review" {
		t.Fatalf("%+v %v", tk, err)
	}
	// Another developer's overlap check sees it.
	r = runIn(t, "", false, root, "work", "overlap", "--user", "other", "--no-memory", "--areas", "api")
	if !strings.Contains(r.out, "overlap with") {
		t.Fatalf("soft lock did not see the task: %+v", r)
	}
	if r = runIn(t, "", false, root, "work", "update", "--task", "missing"); r.code == 0 {
		t.Fatal("update of a missing task must fail")
	}
	if r = runIn(t, "", false, root, "work", "start", "--task", "x", "--status", "weird"); r.code == 0 {
		t.Fatal("bad status must fail")
	}
}
