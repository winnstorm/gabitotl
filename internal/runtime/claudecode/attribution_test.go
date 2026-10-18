package claudecode

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	rt "github.com/winnstorm/gabitotl/internal/runtime"
	"github.com/winnstorm/gabitotl/internal/runtime/engine"
)

const userSettings = "{\n  // my settings\n  \"theme\": \"dark\",\n  \"permissions\": {\"allow\": [\"Bash(go test:*)\"]}\n}\n"

func TestAttributionKeysAreManagedAndRemovable(t *testing.T) {
	env, home := fixture(t, true)
	settings := filepath.Join(home, ".claude", "settings.json")
	must(t, os.WriteFile(settings, []byte(userSettings), 0o644))
	a := New()
	opts := opts
	opts.MigrateGentleman = true

	plan, err := a.Plan(env, opts)
	must(t, err)
	if read(t, settings) != userSettings {
		t.Fatal("Plan must be pure")
	}
	res, err := a.Apply(env, plan)
	must(t, err)
	got := read(t, settings)
	for _, want := range []string{`"commit": ""`, `"pr": ""`, `"sessionUrl": false`, "// my settings", `"theme": "dark"`, `"Bash(go test:*)"`} {
		if !strings.Contains(got, want) {
			t.Errorf("settings.json is missing %s:\n%s", want, got)
		}
	}
	for _, m := range res.Manual {
		if strings.HasSuffix(m.Path, "settings.json") {
			t.Fatalf("unexpected manual action on settings.json: %+v", m)
		}
	}

	// Idempotent: nothing left to do and the file is unchanged.
	again, _ := a.Plan(env, opts)
	for _, ac := range again {
		if ac.Kind != rt.NoAction {
			t.Fatalf("not idempotent: %s", ac)
		}
	}
	if _, err := a.Apply(env, again); err != nil || read(t, settings) != got {
		t.Fatalf("second apply changed the file: %v", err)
	}
	if bad := a.Verify(env, opts); len(bad) > 0 {
		for _, c := range bad {
			if !c.OK {
				t.Fatalf("verify: %+v", c)
			}
		}
	}

	// Removal restores the user's file byte for byte.
	if _, err := a.RemoveGabitoConfig(env); err != nil {
		t.Fatal(err)
	}
	if read(t, settings) != userSettings {
		t.Fatalf("removal must restore the original:\n%s", read(t, settings))
	}
}

func TestAttributionCreatesAndRemovesTheFile(t *testing.T) {
	env, home := fixture(t, true)
	settings := filepath.Join(home, ".claude", "settings.json")
	a := New()
	plan, _ := a.Plan(env, opts)
	if _, err := a.Apply(env, plan); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(t, settings), `"attribution"`) {
		t.Fatal("settings.json must be created with the managed keys")
	}
	if _, err := a.RemoveGabitoConfig(env); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(settings); err == nil {
		t.Fatal("a file GabitoTL created and nobody else touched is removed")
	}
}

func TestAttributionKeepsAUsersOwnChoice(t *testing.T) {
	env, home := fixture(t, true)
	settings := filepath.Join(home, ".claude", "settings.json")
	mine := "{\n  \"attribution\": {\"commit\": \"Mine\"}\n}\n"
	must(t, os.WriteFile(settings, []byte(mine), 0o644))
	a := New()
	plan, _ := a.Plan(env, opts)
	var kept bool
	for _, ac := range plan {
		if strings.HasSuffix(ac.Path, "settings.json") && ac.Kind == rt.Preserve && strings.Contains(ac.Target, "attribution.commit") {
			kept = true
		}
	}
	if !kept {
		t.Fatalf("a different existing value is kept and reported: %v", plan)
	}
	if _, err := a.Apply(env, plan); err != nil {
		t.Fatal(err)
	}
	got := read(t, settings)
	if !strings.Contains(got, `"commit": "Mine"`) || !strings.Contains(got, `"pr": ""`) {
		t.Fatalf("settings = %s", got)
	}
	if _, err := a.RemoveGabitoConfig(env); err != nil {
		t.Fatal(err)
	}
	if read(t, settings) != mine {
		t.Fatalf("removal must leave the user's value: %s", read(t, settings))
	}
}

func TestAttributionProjectScope(t *testing.T) {
	env, _ := fixture(t, true)
	env.ProjectRoot = t.TempDir()
	o := opts
	o.Scope = rt.ScopeProject
	plan, _ := New().Plan(env, o)
	shared := filepath.Join(env.ProjectRoot, ".claude", "settings.json")
	local := filepath.Join(env.ProjectRoot, ".claude", "settings.local.json")
	var nShared, nLocal int
	for _, ac := range plan {
		switch {
		case ac.Path == shared && strings.HasPrefix(ac.Target, "key attribution"):
			nShared++
		case ac.Path == shared:
			t.Fatalf("shared settings.json must not get %s", ac)
		case ac.Path == local && strings.HasPrefix(ac.Target, "key hooks."):
			nLocal++
		}
	}
	if nShared != 3 || nLocal != 3 { // attribution is shared policy; hooks are per person
		t.Fatalf("shared=%d local=%d: %v", nShared, nLocal, plan)
	}
}

func TestProjectHooksMigrateOutOfSharedSettings(t *testing.T) {
	env, _ := fixture(t, true)
	env.ProjectRoot = t.TempDir()
	o := opts
	o.Scope = rt.ScopeProject
	shared := filepath.Join(env.ProjectRoot, ".claude", "settings.json")
	local := filepath.Join(env.ProjectRoot, ".claude", "settings.local.json")
	a := New()
	// An earlier version wrote the hooks into the shared file: replay it.
	must(t, os.MkdirAll(filepath.Dir(shared), 0o755))
	must(t, os.WriteFile(shared, []byte("{\n  \"theme\": \"dark\"\n}\n"), 0o644))
	var old []rt.Action
	for _, k := range HookKeys() {
		old = append(old, engine.PlanKey(Name, shared, k.Path, k.Value, false))
	}
	_, err := a.Apply(env, old)
	must(t, err)
	if !strings.Contains(read(t, shared), "gabitotl context hook") {
		t.Fatal("fixture did not write the old hooks")
	}
	plan, err := a.Plan(env, o)
	must(t, err)
	_, err = a.Apply(env, plan)
	must(t, err)
	if got := read(t, shared); strings.Contains(got, "gabitotl") || strings.Contains(got, "PreCompact") || !strings.Contains(got, `"theme": "dark"`) {
		t.Fatalf("owned hooks must leave the shared file and the user's keys stay:\n%s", got)
	}
	got := read(t, local)
	for _, want := range []string{"gabitotl context checkpoint", "gabitotl context hook", "gabitotl persona mode --hook", `"SessionStart"`} {
		if !strings.Contains(got, want) {
			t.Errorf("settings.local.json is missing %s:\n%s", want, got)
		}
	}
	if _, err := a.RemoveGabitoConfig(env); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(local); err == nil && strings.Contains(string(b), "gabitotl") {
		t.Fatalf("uninstall must remove the local hooks:\n%s", b)
	}
}

func TestContextHooksAreManagedAndRemovable(t *testing.T) {
	env, home := fixture(t, true)
	settings := filepath.Join(home, ".claude", "settings.json")
	must(t, os.WriteFile(settings, []byte(userSettings), 0o644))
	a := New()
	plan, err := a.Plan(env, opts)
	must(t, err)
	_, err = a.Apply(env, plan)
	must(t, err)
	got := read(t, settings)
	for _, want := range []string{`"PreCompact"`, "gabitotl context checkpoint", `"UserPromptSubmit"`, "gabitotl context hook", `"SessionStart"`, "gabitotl persona mode --hook", "// my settings"} {
		if !strings.Contains(got, want) {
			t.Errorf("settings.json is missing %s:\n%s", want, got)
		}
	}
	_, err = a.RemoveGabitoConfig(env)
	must(t, err)
	got = read(t, settings)
	if strings.Contains(got, "gabitotl context") || strings.Contains(got, "PreCompact") {
		t.Fatalf("hooks must be removed on uninstall:\n%s", got)
	}
	if !strings.Contains(got, `"theme": "dark"`) {
		t.Fatalf("user settings lost:\n%s", got)
	}
}

func TestContextHooksKeepAUsersOwnHook(t *testing.T) {
	env, home := fixture(t, true)
	settings := filepath.Join(home, ".claude", "settings.json")
	own := `{"hooks":{"PreCompact":[{"hooks":[{"type":"command","command":"mine"}]}]}}`
	must(t, os.WriteFile(settings, []byte(own), 0o644))
	a := New()
	plan, err := a.Plan(env, opts)
	must(t, err)
	_, err = a.Apply(env, plan)
	must(t, err)
	got := read(t, settings)
	if !strings.Contains(got, `"mine"`) || strings.Contains(got, "gabitotl context checkpoint") {
		t.Fatalf("a user's own PreCompact hook must be kept untouched:\n%s", got)
	}
}

func TestPersonaHookKeepsAUsersOwnSessionStart(t *testing.T) {
	env, home := fixture(t, true)
	settings := filepath.Join(home, ".claude", "settings.json")
	must(t, os.WriteFile(settings, []byte(`{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"mine"}]}]}}`), 0o644))
	a := New()
	plan, err := a.Plan(env, opts)
	must(t, err)
	_, err = a.Apply(env, plan)
	must(t, err)
	if got := read(t, settings); !strings.Contains(got, `"mine"`) || strings.Contains(got, "persona mode --hook") {
		t.Fatalf("a user's own SessionStart hook must be kept untouched:\n%s", got)
	}
}
