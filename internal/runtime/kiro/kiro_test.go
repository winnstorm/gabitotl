package kiro

import (
	"encoding/json"
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

var skillOpts = func() rt.Options {
	o := opts
	o.Skills = []rt.Asset{{Name: "gabito-terminal", Content: "---\nname: gabito-terminal\n---\nbody\n"}}
	return o
}()

func TestSkillsAreSharedByBothVariants(t *testing.T) {
	env, kh := setup(t)
	skill := filepath.Join(kh, "skills", "gabito-terminal", "SKILL.md")
	cli, ide := NewCLI(), NewIDE()
	planC, _ := cli.Plan(env, skillOpts)
	planI, _ := ide.Plan(env, skillOpts) // both computed before anything is applied
	if _, err := cli.Apply(env, planC); err != nil {
		t.Fatal(err)
	}
	if _, err := ide.Apply(env, planI); err != nil {
		t.Fatal(err)
	}
	if read(t, skill) != skillOpts.Skills[0].Content {
		t.Fatal("skill not written")
	}
	if p, _ := ide.Plan(env, skillOpts); len(p) == 0 {
		t.Fatal("empty plan")
	} else {
		for _, a := range p {
			if a.Kind != rt.NoAction {
				t.Fatalf("a second variant must not rewrite anything: %s", a)
			}
		}
	}
	if _, err := cli.RemoveGabitoConfig(env); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(skill); err != nil {
		t.Fatal("the skill is shared: removing the CLI must keep it for the IDE")
	}
	if _, err := ide.RemoveGabitoConfig(env); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(kh, "skills")); err == nil {
		t.Fatal("the skills directory GabitoTL created must be gone")
	}
	if read(t, filepath.Join(kh, "steering", "product.md")) != "# Product\n" {
		t.Fatal("user files touched")
	}
}

func TestSkillsFollowKiroHomeAndProjectScope(t *testing.T) {
	env, _ := setup(t)
	custom := t.TempDir()
	env.Getenv = func(k string) string {
		if k == "KIRO_HOME" {
			return custom
		}
		return ""
	}
	has := func(plan []rt.Action, want string) bool {
		for _, a := range plan {
			if a.Path == want {
				return true
			}
		}
		return false
	}
	p, _ := NewCLI().Plan(env, skillOpts)
	if !has(p, filepath.Join(custom, "skills", "gabito-terminal", "SKILL.md")) {
		t.Fatalf("KIRO_HOME ignored: %v", p)
	}
	env.ProjectRoot = t.TempDir()
	o := skillOpts
	o.Scope = rt.ScopeProject
	p, _ = NewIDE().Plan(env, o)
	if !has(p, filepath.Join(env.ProjectRoot, ".kiro", "skills", "gabito-terminal", "SKILL.md")) {
		t.Fatalf("workspace skills dir: %v", p)
	}
}

func TestHooksFileIsPerPersonAtKiroHome(t *testing.T) {
	env, _ := setup(t)
	custom := t.TempDir()
	env.Getenv = func(k string) string {
		if k == "KIRO_HOME" {
			return custom
		}
		return ""
	}
	env.ProjectRoot = t.TempDir()
	want := filepath.Join(custom, "hooks", "gabitotl.json")
	count := func(plan []rt.Action) (n int) {
		for _, a := range plan {
			if a.Path == want {
				n++
			}
			if strings.HasPrefix(a.Path, filepath.Join(env.ProjectRoot, ".kiro", "hooks")) {
				t.Fatalf("hooks must never go in the shared project .kiro: %s", a.Path)
			}
		}
		return
	}
	for _, scope := range []rt.Scope{rt.ScopeGlobal, rt.ScopeProject} {
		o := opts
		o.Scope = scope
		p, _ := NewCLI().Plan(env, o)
		if count(p) != 1 {
			t.Fatalf("scope %s: hook file not planned once at %s", scope, want)
		}
	}
}

func TestHooksFileContentSharedAndRemoved(t *testing.T) {
	env, kh := setup(t)
	cli, ide := NewCLI(), NewIDE()
	planC, _ := cli.Plan(env, opts)
	planI, _ := ide.Plan(env, opts) // computed before the CLI applies, like the installer
	if _, err := cli.Apply(env, planC); err != nil {
		t.Fatal(err)
	}
	if _, err := ide.Apply(env, planI); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(kh, "hooks", "gabitotl.json")
	var f struct {
		Version string `json:"version"`
		Hooks   []struct {
			Name    string `json:"name"`
			Trigger string `json:"trigger"`
			Action  struct {
				Type    string `json:"type"`
				Command string `json:"command"`
			} `json:"action"`
			Timeout int `json:"timeout"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal([]byte(read(t, path)), &f); err != nil || f.Version != "v1" || len(f.Hooks) != 3 {
		t.Fatalf("invalid hook file: %v %+v", err, f)
	}
	wantCmd := map[string]bool{
		"gabitotl persona mode --hook --format text":                       true,
		"gabitotl context hook --format text --event session --dump":       true,
		"gabitotl context hook --format text --source kiro --event prompt": true,
	}
	for _, h := range f.Hooks {
		okTrigger := h.Trigger == "SessionStart" && h.Timeout == 10 || h.Trigger == "PromptSubmit" && h.Timeout == 5
		if !okTrigger || h.Action.Type != "command" || !wantCmd[h.Action.Command] || h.Name == "" {
			t.Fatalf("unexpected hook: %+v", h)
		}
		delete(wantCmd, h.Action.Command)
	}
	if len(wantCmd) != 0 {
		t.Fatalf("missing hooks: %v", wantCmd)
	}
	// A fresh plan finds the file in place for either variant.
	again, _ := ide.Plan(env, opts)
	for _, a := range again {
		if a.Path == path && a.Kind != rt.NoAction {
			t.Fatalf("IDE must not duplicate the hook file: %v", a)
		}
	}
	if _, err := cli.RemoveGabitoConfig(env); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("shared hook file removed while the IDE needs it: %v", err)
	}
	if _, err := ide.RemoveGabitoConfig(env); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("hook file should be gone after the last owner is removed")
	}
}

func TestHooksFileUserEditIsKept(t *testing.T) {
	env, kh := setup(t)
	path := filepath.Join(kh, "hooks", "gabitotl.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"version":"v1","hooks":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	p, _ := NewCLI().Plan(env, opts)
	for _, a := range p {
		if a.Path == path && a.Kind != rt.Preserve {
			t.Fatalf("a user file must be preserved: %v", a)
		}
	}
}

func TestHooksUseAbsoluteBinaryPathAndFollowMoves(t *testing.T) {
	env, kh := setup(t)
	path := filepath.Join(kh, "hooks", "gabitotl.json")
	cli := NewCLI()
	env.Exe = `/opt/my tools/gabitotl`
	p, _ := cli.Plan(env, opts)
	if _, err := cli.Apply(env, p); err != nil {
		t.Fatal(err)
	}
	if got := read(t, path); !strings.Contains(got, `\"/opt/my tools/gabitotl\" persona mode --hook --format text`) {
		t.Fatalf("path with spaces must be quoted:\n%s", got)
	}
	env.Exe = "/usr/local/bin/gabitotl" // the binary moved: re-running install updates our file
	p, _ = cli.Plan(env, opts)
	if _, err := cli.Apply(env, p); err != nil {
		t.Fatal(err)
	}
	got := read(t, path)
	if strings.Contains(got, "my tools") || !strings.Contains(got, "/usr/local/bin/gabitotl context hook --format text --event session --dump") {
		t.Fatalf("hook file not updated:\n%s", got)
	}
	again, _ := cli.Plan(env, opts)
	for _, a := range again {
		if a.Path == path && a.Kind != rt.NoAction {
			t.Fatalf("not idempotent: %v", a)
		}
	}
}

func TestPromptHookUsesAbsolutePath(t *testing.T) {
	env, _ := setup(t)
	env.Exe = "/opt/bin/gabitotl"
	if got := hooksJSON(env); !strings.Contains(got, `"command": "/opt/bin/gabitotl context hook --format text --source kiro --event prompt"`) {
		t.Fatalf("per-message hook must use the absolute path:\n%s", got)
	}
}
