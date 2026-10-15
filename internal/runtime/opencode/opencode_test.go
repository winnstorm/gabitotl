package opencode

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	rt "github.com/winnstorm/gabitotl/internal/runtime"
)

const userCfg = `{
  // my setup
  "$schema": "https://opencode.ai/config.json",
  "model": "anthropic/claude-sonnet-4-5",
  "provider": {
    "anthropic": { "options": { "apiKey": "{env:ANTHROPIC_API_KEY}" } }
  },
  "instructions": ["CONTRIBUTING.md"],
}
`

func setup(t *testing.T, vars map[string]string) (rt.Env, string) {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, ".config", "opencode")
	if x := vars["XDG_CONFIG_HOME"]; x != "" {
		dir = filepath.Join(x, "opencode")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(dir, "opencode.jsonc")
	if err := os.WriteFile(cfg, []byte(userCfg), 0o644); err != nil {
		t.Fatal(err)
	}
	return rt.Env{Home: home, StateDir: filepath.Join(home, ".gabitotl"), GOOS: "linux",
		Getenv:   func(k string) string { return vars[k] },
		LookPath: func(string) (string, error) { return "/fake/opencode", nil }}, cfg
}

var opts = rt.Options{Scope: rt.ScopeGlobal, PersonaText: "Soy Gabito.", Memory: rt.MemoryMCP{Name: "engram", Command: "engram", Args: []string{"mcp"}}}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestManagedKeysOnlyAndRestoreJSONC(t *testing.T) {
	env, cfg := setup(t, nil)
	a := New()
	plan, err := a.Plan(env, opts)
	if err != nil {
		t.Fatal(err)
	}
	for _, ac := range plan {
		if strings.Contains(ac.Target, "model") {
			t.Fatalf("model must not be planned when runtime.md has none: %s", ac)
		}
	}
	if _, err := a.Apply(env, plan); err != nil {
		t.Fatal(err)
	}
	got := read(t, cfg)
	for _, must := range []string{"// my setup", `"anthropic"`, `"model": "anthropic/claude-sonnet-4-5"`, "CONTRIBUTING.md", `"engram"`, "gabitotl-persona.md"} {
		if !strings.Contains(got, must) {
			t.Fatalf("missing %q in\n%s", must, got)
		}
	}
	// idempotent
	plan, _ = a.Plan(env, opts)
	for _, ac := range plan {
		if ac.Kind != rt.NoAction {
			t.Fatalf("not idempotent: %s", ac)
		}
	}
	if _, err := a.RemoveGabitoConfig(env); err != nil {
		t.Fatal(err)
	}
	if read(t, cfg) != userCfg {
		t.Fatalf("not restored:\n%s", read(t, cfg))
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(cfg), "gabitotl-persona.md")); err == nil {
		t.Fatal("persona file should be removed")
	}
}

func TestModelAppliedOnlyWhenChosenAndRestored(t *testing.T) {
	env, cfg := setup(t, nil)
	o := opts
	o.Model = "openai/gpt-5"
	a := New()
	plan, _ := a.Plan(env, o)
	if _, err := a.Apply(env, plan); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(t, cfg), `"model": "openai/gpt-5"`) {
		t.Fatalf("model not applied:\n%s", read(t, cfg))
	}
	if _, err := a.RemoveGabitoConfig(env); err != nil {
		t.Fatal(err)
	}
	if read(t, cfg) != userCfg {
		t.Fatalf("previous model not restored:\n%s", read(t, cfg))
	}
}

func TestXDGAndExplicitConfigAreHonored(t *testing.T) {
	xdg := t.TempDir()
	env, cfg := setup(t, map[string]string{"XDG_CONFIG_HOME": xdg})
	if !strings.HasPrefix(cfg, xdg) {
		t.Fatal("fixture should live under XDG")
	}
	plan, _ := New().Plan(env, opts)
	for _, ac := range plan {
		if !strings.HasPrefix(ac.Path, xdg) {
			t.Fatalf("ignored XDG_CONFIG_HOME: %s", ac)
		}
	}
	custom := filepath.Join(t.TempDir(), "custom.json")
	env.Getenv = func(k string) string {
		if k == "OPENCODE_CONFIG" {
			return custom
		}
		return ""
	}
	plan, _ = New().Plan(env, opts)
	found := false
	for _, ac := range plan {
		found = found || ac.Path == custom
	}
	if !found {
		t.Fatal("ignored OPENCODE_CONFIG")
	}
	env.Getenv = func(k string) string {
		if k == "OPENCODE_CONFIG_DIR" {
			return "/custom/dir"
		}
		return ""
	}
	plan, _ = New().Plan(env, opts)
	if plan[0].Path != "/custom/dir/gabitotl-persona.md" {
		t.Fatalf("ignored OPENCODE_CONFIG_DIR: %s", plan[0])
	}
}

func TestProjectScopePrefersExistingJSONC(t *testing.T) {
	env, _ := setup(t, nil)
	env.ProjectRoot = t.TempDir()
	if err := os.WriteFile(filepath.Join(env.ProjectRoot, "opencode.jsonc"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	o := opts
	o.Scope = rt.ScopeProject
	plan, _ := New().Plan(env, o)
	if plan[1].Path != filepath.Join(env.ProjectRoot, "opencode.jsonc") || plan[1].Edit.Item != ".opencode/gabitotl-persona.md" {
		t.Fatalf("%s", plan[1])
	}
}

func TestDetect(t *testing.T) {
	env, _ := setup(t, nil)
	if d := New().Detect(env); d.State != rt.StateInstalled {
		t.Fatalf("%+v", d)
	}
	env.LookPath = func(string) (string, error) { return "", os.ErrNotExist }
	if d := New().Detect(env); d.State != rt.StateDetectedHistory {
		t.Fatalf("%+v", d)
	}
}

var skillOpts = func() rt.Options {
	o := opts
	o.Skills = []rt.Asset{{Name: "gabito-terminal", Content: "---\nname: gabito-terminal\n---\nbody\n"}}
	return o
}()

func skillAction(t *testing.T, plan []rt.Action) rt.Action {
	t.Helper()
	for _, a := range plan {
		if a.Edit.Op == rt.OpWriteFile {
			return a
		}
	}
	t.Fatalf("no skill action in plan: %v", plan)
	return rt.Action{}
}

func TestSkillsRoundTrip(t *testing.T) {
	env, cfg := setup(t, nil)
	a := New()
	plan, _ := a.Plan(env, skillOpts)
	sa := skillAction(t, plan)
	want := filepath.Join(filepath.Dir(cfg), "skills", "gabito-terminal", "SKILL.md")
	if sa.Path != want || sa.Kind != rt.Create {
		t.Fatalf("skill action %s, want path %s", sa, want)
	}
	if _, err := a.Apply(env, plan); err != nil {
		t.Fatal(err)
	}
	if read(t, want) != skillOpts.Skills[0].Content {
		t.Fatal("skill not written")
	}
	for _, c := range a.Verify(env, skillOpts) {
		if !c.OK {
			t.Fatalf("verify: %+v", c)
		}
	}
	if _, err := a.RemoveGabitoConfig(env); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(cfg), "skills")); err == nil {
		t.Fatal("skills directory created by GabitoTL should be gone")
	}
	if read(t, cfg) != userCfg {
		t.Fatalf("user config not restored:\n%s", read(t, cfg))
	}
}

func TestSkillsDirFollowsEnvironmentAndScope(t *testing.T) {
	xdg := t.TempDir()
	env, _ := setup(t, map[string]string{"XDG_CONFIG_HOME": xdg})
	plan, _ := New().Plan(env, skillOpts)
	if got := skillAction(t, plan).Path; got != filepath.Join(xdg, "opencode", "skills", "gabito-terminal", "SKILL.md") {
		t.Fatalf("XDG_CONFIG_HOME ignored: %s", got)
	}
	cfgDir := t.TempDir()
	env, _ = setup(t, map[string]string{"OPENCODE_CONFIG_DIR": cfgDir})
	plan, _ = New().Plan(env, skillOpts)
	if got := skillAction(t, plan).Path; got != filepath.Join(cfgDir, "skills", "gabito-terminal", "SKILL.md") {
		t.Fatalf("OPENCODE_CONFIG_DIR ignored: %s", got)
	}
	env.ProjectRoot = t.TempDir()
	o := skillOpts
	o.Scope = rt.ScopeProject
	plan, _ = New().Plan(env, o)
	if got := skillAction(t, plan).Path; got != filepath.Join(env.ProjectRoot, ".opencode", "skills", "gabito-terminal", "SKILL.md") {
		t.Fatalf("project scope: %s", got)
	}
}

func TestSkillUserFileIsKept(t *testing.T) {
	env, cfg := setup(t, nil)
	p := filepath.Join(filepath.Dir(cfg), "skills", "gabito-terminal", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("my own skill\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := New()
	plan, _ := a.Plan(env, skillOpts)
	var found bool
	for _, act := range plan {
		if act.Path == p {
			found = true
			if act.Kind != rt.Preserve || act.Edit.Op != rt.OpManual {
				t.Fatalf("a user file must be preserved: %s", act)
			}
		}
	}
	if !found {
		t.Fatalf("skill path missing from the plan: %v", plan)
	}
	if _, err := a.Apply(env, plan); err != nil {
		t.Fatal(err)
	}
	if _, err := a.RemoveGabitoConfig(env); err != nil {
		t.Fatal(err)
	}
	if read(t, p) != "my own skill\n" {
		t.Fatal("the user's skill file was changed")
	}
}
