package all

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	rt "github.com/winnstorm/gabitotl/internal/runtime"
)

var multiSkill = rt.Asset{
	Name: "demo-opt", Content: "---\nname: demo-opt\n---\nbody\n",
	Files: map[string]string{"references/a.md": "A\n", "references/deep/b.md": "B\n"},
}

// skillDirs are where each adapter keeps its skills under a fresh home.
var skillDirs = map[string]func(h string) string{
	"claude-code": func(h string) string { return filepath.Join(h, ".claude", "skills") },
	"opencode":    func(h string) string { return filepath.Join(h, ".config", "opencode", "skills") },
	"kiro-cli":    func(h string) string { return filepath.Join(h, ".kiro", "skills") },
}

func skillFiles(dir string) []string {
	return []string{
		filepath.Join(dir, "demo-opt", "SKILL.md"),
		filepath.Join(dir, "demo-opt", "references", "a.md"),
		filepath.Join(dir, "demo-opt", "references", "deep", "b.md"),
	}
}

func TestMultiFileSkillInstallAndRetire(t *testing.T) {
	for _, m := range machines {
		t.Run(m.runtime, func(t *testing.T) {
			env, _ := setup(t, m)
			a, _ := Registry().Get(m.runtime)
			dir := skillDirs[m.runtime](env.Home)
			on := rt.Options{Scope: rt.ScopeGlobal, PersonaText: "x", Skills: []rt.Asset{multiSkill}}
			off := rt.Options{Scope: rt.ScopeGlobal, PersonaText: "x", RetireSkills: []string{"demo-opt"}}

			apply(t, a, env, on)
			want := skillFiles(dir)
			for i, p := range want {
				if got := read(t, p); got != []string{multiSkill.Content, "A\n", "B\n"}[i] {
					t.Errorf("%s = %q", p, got)
				}
			}
			// A file of the user inside the skill directory survives the removal.
			mine := filepath.Join(dir, "demo-opt", "references", "mine.md")
			if err := os.WriteFile(mine, []byte("mine\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			// Idempotent: planning again changes nothing, with plan IDs stable.
			p1, _ := a.Plan(env, on)
			p2, _ := a.Plan(env, on)
			for i := range p1 {
				if p1[i].String() != p2[i].String() {
					t.Errorf("plan not stable: %q vs %q", p1[i], p2[i])
				}
			}
			for _, c := range a.Verify(env, on) {
				if !c.OK {
					t.Errorf("verify: %+v", c)
				}
			}
			// Deselected: the plan retires exactly the files we wrote; verify still passes.
			plan, _ := a.Plan(env, off)
			var rm []string
			for _, ac := range plan {
				if ac.Kind == rt.RemoveAfterVerify && ac.Edit.Op == rt.OpRemoveFile {
					rm = append(rm, ac.Path)
				}
			}
			if !slices.Equal(rm, slices.Sorted(slices.Values(want))) {
				t.Errorf("retire plan = %v, want %v", rm, want)
			}
			apply(t, a, env, off)
			for _, p := range want {
				if _, err := os.Stat(p); err == nil {
					t.Errorf("%s survived deselection", p)
				}
			}
			if read(t, mine) != "mine\n" {
				t.Error("the user file did not survive")
			}
			// Nothing left to retire once they are gone.
			plan, _ = a.Plan(env, off)
			for _, ac := range plan {
				if ac.Edit.Op == rt.OpRemoveFile {
					t.Errorf("second retire plan not empty: %v", ac)
				}
			}
		})
	}
}

func TestRetireRemovesDirectoriesWeCreatedAndOffByDefault(t *testing.T) {
	m := machines[0]
	env, _ := setup(t, m)
	a, _ := Registry().Get(m.runtime)
	dir := skillDirs[m.runtime](env.Home)
	apply(t, a, env, rt.Options{Scope: rt.ScopeGlobal, PersonaText: "x", Skills: []rt.Asset{multiSkill}})
	apply(t, a, env, rt.Options{Scope: rt.ScopeGlobal, PersonaText: "x", RetireSkills: []string{"demo-opt"}})
	if _, err := os.Stat(filepath.Join(dir, "demo-opt")); err == nil {
		t.Error("the empty skill directory was left behind")
	}
	// Without Skills nothing is installed.
	if plan, _ := a.Plan(env, rt.Options{Scope: rt.ScopeGlobal, PersonaText: "x"}); len(plan) > 0 {
		for _, ac := range plan {
			if filepath.Base(ac.Path) == "SKILL.md" {
				t.Errorf("unexpected skill action %v", ac)
			}
		}
	}
}

func TestUninstallRemovesEveryFileOfASkill(t *testing.T) {
	m := machines[1]
	env, _ := setup(t, m)
	a, _ := Registry().Get(m.runtime)
	dir := skillDirs[m.runtime](env.Home)
	apply(t, a, env, rt.Options{Scope: rt.ScopeGlobal, PersonaText: "x", Skills: []rt.Asset{multiSkill}})
	if _, err := a.RemoveGabitoConfig(env); err != nil {
		t.Fatal(err)
	}
	for _, p := range skillFiles(dir) {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("%s survived uninstall", p)
		}
	}
}
