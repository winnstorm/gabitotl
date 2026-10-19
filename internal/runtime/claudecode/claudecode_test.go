package claudecode

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	rt "github.com/winnstorm/gabitotl/internal/runtime"
)

const userMD = "# My rules\n\nBe kind.\n\n"
const gentleman = "<!-- gentle-ai:persona -->\nYou are Gentleman.\n<!-- /gentle-ai:persona -->\n"
const userAfter = "\n## Mine too\ntext without trailing newline"

const claudeJSON = "{\n  \"numStartups\": 3,\n  \"mcpServers\": {\n    \"other\": {\"command\": \"x\"}\n  }\n}\n"

func fixture(t *testing.T, bin bool) (rt.Env, string) {
	t.Helper()
	home := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(home, ".claude"), 0o755))
	must(t, os.WriteFile(filepath.Join(home, ".claude", "CLAUDE.md"), []byte(userMD+gentleman+userAfter), 0o644))
	must(t, os.WriteFile(filepath.Join(home, ".claude.json"), []byte(claudeJSON), 0o600))
	env := rt.Env{Home: home, StateDir: filepath.Join(home, ".gabitotl"), GOOS: "linux",
		Getenv: func(string) string { return "" },
		LookPath: func(n string) (string, error) {
			if bin && n == "claude" {
				return "/fake/claude", nil
			}
			return "", os.ErrNotExist
		}}
	return env, home
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	must(t, err)
	return string(b)
}

var opts = rt.Options{Scope: rt.ScopeGlobal, PersonaText: "Soy Gabito.", Memory: rt.MemoryMCP{Name: "engram", Command: "engram", Args: []string{"mcp"}}}

func TestDetectStates(t *testing.T) {
	env, _ := fixture(t, true)
	if d := New().Detect(env); d.State != rt.StateInstalled {
		t.Fatalf("%+v", d)
	}
	env2, _ := fixture(t, false)
	if d := New().Detect(env2); d.State != rt.StateDetectedHistory {
		t.Fatalf("%+v", d)
	}
	if d := New().Detect(rt.Env{Home: t.TempDir(), StateDir: t.TempDir()}); d.State != rt.StateNone {
		t.Fatalf("%+v", d)
	}
}

func TestPlanIsPureAndReportsPersonaConflict(t *testing.T) {
	env, home := fixture(t, true)
	md := filepath.Join(home, ".claude", "CLAUDE.md")
	before := read(t, md)
	plan, err := New().Plan(env, opts)
	must(t, err)
	if read(t, md) != before || read(t, filepath.Join(home, ".claude.json")) != claudeJSON {
		t.Fatal("Plan wrote to disk")
	}
	if _, err := os.Stat(filepath.Join(home, ".gabitotl")); err == nil {
		t.Fatal("Plan created state")
	}
	kinds := map[rt.Kind]int{}
	for _, a := range plan {
		kinds[a.Kind]++
	}
	if kinds[rt.Modify] != 2 || kinds[rt.Preserve] != 1 || kinds[rt.RemoveAfterVerify] != 0 {
		t.Fatalf("without --migrate the gentleman block is only preserved: %v", kinds)
	}
	opts2 := opts
	opts2.MigrateGentleman = true
	plan, _ = New().Plan(env, opts2)
	var rm int
	for _, a := range plan {
		if a.Kind == rt.RemoveAfterVerify {
			rm++
		}
	}
	if rm != 1 {
		t.Fatalf("want one REMOVE-AFTER-VERIFY, got %d", rm)
	}
}

func TestApplyIdempotentAndRemoveRestoresUserContent(t *testing.T) {
	env, home := fixture(t, true)
	md := filepath.Join(home, ".claude", "CLAUDE.md")
	cj := filepath.Join(home, ".claude.json")
	a := New()
	o := opts
	o.MigrateGentleman = true
	plan, _ := a.Plan(env, o)
	res, err := a.Apply(env, plan)
	must(t, err)
	if len(res.Applied) != 8 || len(res.Removed) != 1 { // persona, memory MCP and the three attribution keys and three hooks
		t.Fatalf("%+v", res)
	}
	got := read(t, md)
	if strings.Contains(got, "gentle-ai") || !strings.HasPrefix(got, userMD) || !strings.Contains(got, userAfter) {
		t.Fatalf("user content not preserved:\n%s", got)
	}
	if !strings.Contains(got, "<!-- gabitotl:start persona -->") || !strings.Contains(read(t, cj), `"engram"`) || !strings.Contains(read(t, cj), `"other"`) {
		t.Fatal("assets missing")
	}
	// Second run is a no-op.
	snap1, snap2 := got, read(t, cj)
	plan, _ = a.Plan(env, o)
	for _, ac := range plan {
		if ac.Kind != rt.NoAction {
			t.Fatalf("not idempotent: %s", ac)
		}
	}
	res, err = a.Apply(env, plan)
	must(t, err)
	if len(res.Applied) != 0 || read(t, md) != snap1 || read(t, cj) != snap2 {
		t.Fatal("re-apply changed files")
	}
	for _, c := range a.Verify(env, o) {
		if !c.OK {
			t.Fatalf("verify: %+v", c)
		}
	}
	// Uninstall removes only ours; the Gentleman block was retired (a documented, backed-up migration step).
	_, err = a.RemoveGabitoConfig(env)
	must(t, err)
	if after := read(t, md); strings.Contains(after, "gabitotl") || !strings.HasPrefix(after, userMD) || !strings.HasSuffix(after, userAfter+"\n") {
		t.Fatalf("remove damaged user content:\n%q", after)
	}
	if read(t, cj) != claudeJSON {
		t.Fatalf("json not restored:\n%s", read(t, cj))
	}
}

func TestExistingEquivalentMCPNotDuplicated(t *testing.T) {
	env, home := fixture(t, true)
	cj := filepath.Join(home, ".claude.json")
	must(t, os.WriteFile(cj, []byte(`{"mcpServers":{"gentle-engram":{"command":"engram","args":["mcp"]}}}`), 0o644))
	plan, _ := New().Plan(env, opts)
	for _, a := range plan {
		if strings.HasPrefix(a.Target, "server") && a.Kind != rt.NoAction {
			t.Fatalf("duplicate planned: %s", a)
		}
	}
	_, err := New().Apply(env, plan)
	must(t, err)
	if strings.Count(read(t, cj), "engram") != 2 { // key name + command, no second entry
		t.Fatalf("duplicated: %s", read(t, cj))
	}
	// Removing GabitoTL must not remove an entry it did not create.
	_, err = New().RemoveGabitoConfig(env)
	must(t, err)
	if !strings.Contains(read(t, cj), "gentle-engram") {
		t.Fatal("removed a foreign entry")
	}
}

func TestUnsafeJSONCProposesManualAction(t *testing.T) {
	env, home := fixture(t, true)
	cj := filepath.Join(home, ".claude.json")
	must(t, os.WriteFile(cj, []byte(`{"mcpServers": {broken`), 0o644))
	plan, _ := New().Plan(env, opts)
	var manual bool
	for _, a := range plan {
		if strings.HasPrefix(a.Target, "key") && a.Kind == rt.Preserve && strings.Contains(a.Detail, "by hand") {
			manual = true
		}
	}
	if !manual {
		t.Fatal("expected a manual action")
	}
	res, err := New().Apply(env, plan)
	must(t, err)
	if read(t, cj) != `{"mcpServers": {broken` || len(res.Manual) == 0 {
		t.Fatal("unsafe file was touched or no manual action reported")
	}
}

func TestProjectScopeUsesProjectFiles(t *testing.T) {
	env, _ := fixture(t, true)
	env.ProjectRoot = t.TempDir()
	o := opts
	o.Scope = rt.ScopeProject
	plan, _ := New().Plan(env, o)
	for _, a := range plan {
		if !strings.HasPrefix(a.Path, env.ProjectRoot) {
			t.Fatalf("project scope wrote outside the project: %s", a)
		}
	}
	_, err := New().Apply(env, plan)
	must(t, err)
	if !strings.Contains(read(t, filepath.Join(env.ProjectRoot, ".mcp.json")), "mcpServers") {
		t.Fatal(".mcp.json missing")
	}
	_, err = New().RemoveGabitoConfig(env)
	must(t, err)
	for _, f := range []string{"CLAUDE.md", ".mcp.json"} {
		if _, err := os.Stat(filepath.Join(env.ProjectRoot, f)); err == nil {
			t.Fatalf("created file %s should be deleted on removal", f)
		}
	}
}

var skillOpts = func() rt.Options {
	o := opts
	o.Skills = []rt.Asset{{Name: "gabito-terminal", Content: "---\nname: gabito-terminal\n---\nbody\n"}}
	return o
}()

func TestSkillsRoundTrip(t *testing.T) {
	env, home := fixture(t, true)
	a := New()
	want := filepath.Join(home, ".claude", "skills", "gabito-terminal", "SKILL.md")
	plan, _ := a.Plan(env, skillOpts)
	var found bool
	for _, act := range plan {
		if act.Path == want {
			found = true
			if act.Kind != rt.Create || act.Edit.Op != rt.OpWriteFile {
				t.Fatalf("skill action: %s", act)
			}
		}
	}
	if !found {
		t.Fatalf("no skill action for %s in %v", want, plan)
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
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills")); err == nil {
		t.Fatal("skills directory created by GabitoTL should be gone")
	}
	if _, err := os.Stat(filepath.Join(home, ".claude")); err != nil {
		t.Fatal("the user's .claude directory must stay")
	}
}

func TestSkillsProjectScopeAndEditedFile(t *testing.T) {
	env, _ := fixture(t, true)
	env.ProjectRoot = t.TempDir()
	o := skillOpts
	o.Scope = rt.ScopeProject
	a := New()
	plan, _ := a.Plan(env, o)
	want := filepath.Join(env.ProjectRoot, ".claude", "skills", "gabito-terminal", "SKILL.md")
	if _, err := a.Apply(env, plan); err != nil {
		t.Fatal(err)
	}
	if read(t, want) != o.Skills[0].Content {
		t.Fatal("project skill not written")
	}
	must(t, os.WriteFile(want, []byte("edited\n"), 0o644))
	res, err := a.RemoveGabitoConfig(env)
	must(t, err)
	if read(t, want) != "edited\n" || len(res.Manual) == 0 {
		t.Fatalf("an edited skill must be kept and reported: %+v", res)
	}
}

func TestToolServerRegisteredOnceAndIdempotent(t *testing.T) {
	env, home := fixture(t, true)
	o := opts
	o.Tools = []rt.MemoryMCP{{Name: rt.EntryCodeGraph, Command: "cmd", Args: []string{"/c", "codegraph", "serve", "--mcp"}}}
	a := New()
	for i := 0; i < 2; i++ {
		plan, err := a.Plan(env, o)
		must(t, err)
		_, _, err = rt.BackupAndApply(env, a, plan)
		must(t, err)
	}
	cfg := read(t, filepath.Join(home, ".claude.json"))
	if strings.Count(cfg, `"codegraph": {`) != 1 || !strings.Contains(cfg, `"serve"`) || !strings.Contains(cfg, `"other"`) {
		t.Fatalf("config:\n%s", cfg)
	}
	plan, _ := a.Plan(env, o)
	for _, ac := range plan {
		if strings.Contains(ac.Target, "codegraph") && ac.Kind != rt.NoAction {
			t.Errorf("second plan not idempotent: %s", ac)
		}
	}
}
