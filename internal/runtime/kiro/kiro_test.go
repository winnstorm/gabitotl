package kiro

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	rt "github.com/winnstorm/gabitotl/internal/runtime"
)

const userMCP = "{\n  \"mcpServers\": {\n    \"github\": {\"command\": \"gh-mcp\"}\n  }\n}\n"

var opts = rt.Options{Scope: rt.ScopeGlobal, PersonaText: "Soy Gabito.", Memory: rt.MemoryMCP{Name: "engram", Command: "engram", Args: []string{"mcp"}}}

func setup(t *testing.T) (rt.Env, string) {
	t.Helper()
	home := t.TempDir()
	kh := filepath.Join(home, ".kiro")
	for _, d := range []string{"steering", "settings"} {
		if err := os.MkdirAll(filepath.Join(kh, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(kh, "steering", "product.md"), []byte("# Product\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(kh, "settings", "mcp.json"), []byte(userMCP), 0o644); err != nil {
		t.Fatal(err)
	}
	return rt.Env{Home: home, StateDir: filepath.Join(home, ".gabitotl"), GOOS: "linux",
		Getenv:   func(string) string { return "" },
		LookPath: func(n string) (string, error) { return "/fake/" + n, nil },
		Exists:   func(p string) bool { _, err := os.Stat(p); return err == nil || p == "/opt/Kiro" }}, kh
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestBothVariantsShareAssetsWithoutDuplicates(t *testing.T) {
	env, kh := setup(t)
	cli, ide := NewCLI(), NewIDE()
	planC, _ := cli.Plan(env, opts)
	planI, _ := ide.Plan(env, opts) // computed before CLI applies: still all CREATE/MODIFY
	if _, err := cli.Apply(env, planC); err != nil {
		t.Fatal(err)
	}
	if _, err := ide.Apply(env, planI); err != nil {
		t.Fatal(err)
	}
	mcp := read(t, filepath.Join(kh, "settings", "mcp.json"))
	if strings.Count(mcp, `"engram": {`) != 1 || !strings.Contains(mcp, "gh-mcp") {
		t.Fatalf("duplicated or damaged MCP:\n%s", mcp)
	}
	steer := read(t, filepath.Join(kh, "steering", "gabitotl.md"))
	if !strings.HasPrefix(steer, "---\ninclusion: always\n---\n") || strings.Count(steer, "gabitotl:start persona") != 1 {
		t.Fatalf("steering:\n%s", steer)
	}
	// Removing the CLI keeps shared assets for the IDE; removing the IDE cleans up.
	if _, err := cli.RemoveGabitoConfig(env); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(t, filepath.Join(kh, "settings", "mcp.json")), `"engram"`) {
		t.Fatal("shared MCP entry removed while the IDE still needs it")
	}
	if _, err := ide.RemoveGabitoConfig(env); err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(kh, "settings", "mcp.json")) != userMCP {
		t.Fatalf("mcp.json not restored:\n%s", read(t, filepath.Join(kh, "settings", "mcp.json")))
	}
	if _, err := os.Stat(filepath.Join(kh, "steering", "gabitotl.md")); err == nil {
		t.Fatal("created steering file should be gone")
	}
	if read(t, filepath.Join(kh, "steering", "product.md")) != "# Product\n" {
		t.Fatal("user steering touched")
	}
}

func TestPlanSecondVariantIsNoAction(t *testing.T) {
	env, _ := setup(t)
	cli, ide := NewCLI(), NewIDE()
	p, _ := cli.Plan(env, opts)
	if _, err := cli.Apply(env, p); err != nil {
		t.Fatal(err)
	}
	p, _ = ide.Plan(env, opts)
	for _, a := range p {
		if a.Kind != rt.NoAction {
			t.Fatalf("second variant must not rewrite: %s", a)
		}
	}
}

func TestKiroHomeAndProjectScope(t *testing.T) {
	env, _ := setup(t)
	custom := t.TempDir()
	env.Getenv = func(k string) string {
		if k == "KIRO_HOME" {
			return custom
		}
		return ""
	}
	p, _ := NewCLI().Plan(env, opts)
	if !strings.HasPrefix(p[0].Path, custom) {
		t.Fatalf("ignored KIRO_HOME: %s", p[0])
	}
	env.ProjectRoot = t.TempDir()
	o := opts
	o.Scope = rt.ScopeProject
	p, _ = NewIDE().Plan(env, o)
	if p[0].Path != filepath.Join(env.ProjectRoot, ".kiro", "steering", "gabitotl.md") {
		t.Fatalf("%s", p[0])
	}
}

func TestDetectDistinguishesCLIIDEAndWorkspace(t *testing.T) {
	env, _ := setup(t)
	env.ProjectRoot = t.TempDir()
	env.LookPath = func(n string) (string, error) {
		if n == "kiro-cli" {
			return "/fake/kiro-cli", nil
		}
		return "", os.ErrNotExist
	}
	env.Exists = func(p string) bool { _, err := os.Stat(p); return err == nil }
	if d := NewCLI().Detect(env); d.State != rt.StateInstalled {
		t.Fatalf("cli: %+v", d)
	}
	if d := NewIDE().Detect(env); d.State != rt.StateDetectedHistory {
		t.Fatalf("ide with only ~/.kiro: %+v", d)
	}
	if err := os.MkdirAll(filepath.Join(env.ProjectRoot, ".kiro"), 0o755); err != nil {
		t.Fatal(err)
	}
	d := NewIDE().Detect(env)
	ws := false
	for _, s := range d.Signals {
		ws = ws || (s.Name == "workspace .kiro" && s.Found)
	}
	if !ws {
		t.Fatalf("workspace not reported: %+v", d)
	}
	env.Exists = func(p string) bool { return p == "/opt/Kiro" }
	if d := NewIDE().Detect(env); d.State != rt.StateInstalled {
		t.Fatalf("ide app present: %+v", d)
	}
}
