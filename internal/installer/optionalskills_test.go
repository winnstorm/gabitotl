package installer

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	rt "github.com/winnstorm/gabitotl/internal/runtime"
	"github.com/winnstorm/gabitotl/internal/skills"
)

const optName = "gabito-design"

// designFiles lists where the optional skill lives for the three agents.
func designFiles(home string) []string {
	var out []string
	for _, base := range []string{filepath.Join(home, ".claude", "skills"), filepath.Join(home, ".config", "opencode", "skills"), filepath.Join(home, ".kiro", "skills")} {
		out = append(out, filepath.Join(base, optName, "SKILL.md"), filepath.Join(base, optName, "references", "commands.md"))
	}
	return out
}

func present(t *testing.T, paths []string) (n int) {
	t.Helper()
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			n++
		}
	}
	return n
}

func TestOptionalSkillsAreOffByDefault(t *testing.T) {
	if !slices.Contains(OptionalSkills(), optName) {
		t.Fatalf("%s must be an optional skill", optName)
	}
	for _, a := range SkillAssets() {
		if a.Name == optName {
			t.Fatal("an optional skill must not be part of the default assets")
		}
	}
	inst, retire := SkillSet(nil)
	if slices.ContainsFunc(inst, func(a rt.Asset) bool { return a.Name == optName }) || !slices.Contains(retire, optName) {
		t.Fatal("with no selection the optional skill is retired, not installed")
	}
	inst, retire = SkillSet([]string{optName, "not-a-skill"})
	if !slices.ContainsFunc(inst, func(a rt.Asset) bool { return a.Name == optName && len(a.Files) > 0 }) || len(retire) != 0 {
		t.Fatal("a selected optional skill is installed with its references")
	}
	if len(inst) != len(skills.All()) {
		t.Fatalf("installed %d, embedded %d", len(inst), len(skills.All()))
	}
	if got := SelectedSkills([]string{"nope", optName}); !slices.Equal(got, []string{optName}) {
		t.Fatalf("SelectedSkills = %v", got)
	}
}

func TestOptionalSkillSelectInstallsDeselectRemovesAndDoctorAgrees(t *testing.T) {
	f := newFixture(t)
	if _, err := runPlan(t, f, Options{}); err != nil {
		t.Fatal(err)
	}
	if n := present(t, designFiles(f.home)); n != 0 {
		t.Fatalf("%d GabitoDesign files installed by default", n)
	}
	// Select: planned, applied, verified, remembered.
	_, pl := buildPlan(t, f, Options{SetOptionalSkills: true, OptionalSkills: []string{optName}})
	creates := 0
	for _, it := range pl.Items {
		if it.Kind == rt.Create && strings.Contains(it.Target, optName) {
			creates++
		}
	}
	if creates == 0 {
		t.Fatal("selecting the skill must plan its files")
	}
	if _, err := runPlan(t, f, Options{SetOptionalSkills: true, OptionalSkills: []string{optName}}); err != nil {
		t.Fatal(err)
	}
	if n := present(t, designFiles(f.home)); n != len(designFiles(f.home)) {
		t.Fatal("selected skill was not installed")
	}
	if got := gabitotl.LoadOptionalSkills(f.home); !slices.Equal(got, []string{optName}) {
		t.Fatalf("selection not remembered: %v", got)
	}
	// Without an explicit choice the saved selection stays: nothing to do, nothing removed.
	if res, err := runPlan(t, f, Options{}); err != nil || !res.Skipped {
		t.Fatalf("re-run with the saved selection: skipped=%v err=%v", res.Skipped, err)
	}
	// A user file next to ours survives deselection.
	mine := filepath.Join(f.home, ".claude", "skills", optName, "references", "mine.md")
	if err := os.WriteFile(mine, []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Deselect: removed, and the saved selection is empty.
	if _, err := runPlan(t, f, Options{SetOptionalSkills: true}); err != nil {
		t.Fatal(err)
	}
	for _, p := range designFiles(f.home) {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("%s survived deselection", p)
		}
	}
	if _, err := os.Stat(mine); err != nil {
		t.Error("the user file did not survive deselection")
	}
	if got := gabitotl.LoadOptionalSkills(f.home); len(got) != 0 {
		t.Fatalf("selection not cleared: %v", got)
	}
	// Deselected and gone is the settled state: the next run finds nothing to do.
	if res, err := runPlan(t, f, Options{SetOptionalSkills: true}); err != nil || !res.Skipped {
		t.Fatalf("settled state: skipped=%v err=%v", res.Skipped, err)
	}
}

func TestSkillSelectionPlansAppliesAndReports(t *testing.T) {
	f := newFixture(t)
	if _, err := runPlan(t, f, Options{}); err != nil {
		t.Fatal(err)
	}
	state := func() OptionalSkillState {
		t.Helper()
		st, err := OptionalSkillStates(f.env)
		if err != nil || len(st) != 1 || st[0].Name != optName {
			t.Fatalf("states %+v %v", st, err)
		}
		return st[0]
	}
	if s := state(); s.Enabled || len(s.Agents) == 0 {
		t.Fatalf("default state: %+v", s)
	} else {
		for _, a := range s.Agents {
			if a.Installed {
				t.Errorf("%s reports it installed by default", a.Runtime)
			}
		}
	}
	changes, err := PlanSkillSelection(f.env, []string{optName})
	if err != nil || len(changes) == 0 || !changes[0].Pending() {
		t.Fatalf("enable plan: %+v %v", changes, err)
	}
	if n := present(t, designFiles(f.home)); n != 0 {
		t.Fatal("planning must not write")
	}
	if _, _, err := ApplySkillSelection(f.env, []string{optName}); err != nil {
		t.Fatal(err)
	}
	if s := state(); !s.Enabled {
		t.Fatalf("enabled state: %+v", s)
	} else {
		for _, a := range s.Agents {
			if !a.Installed {
				t.Errorf("%s: not installed after enabling", a.Runtime)
			}
		}
	}
	if changes, _ = PlanSkillSelection(f.env, []string{optName}); changes[0].Pending() {
		t.Error("a second enable must have nothing to do")
	}
	if _, removed, err := ApplySkillSelection(f.env, nil); err != nil || len(removed) == 0 {
		t.Fatalf("disable: removed %v err %v", removed, err)
	}
	if n := present(t, designFiles(f.home)); n != 0 {
		t.Errorf("%d files left after disabling", n)
	}
	if _, err := PlanSkillSelection(f.env, []string{"nope"}); err == nil {
		t.Error("an unknown skill must be refused")
	}
}

func TestOptionalSkillFileMatchesOnlyTheAgentSkillsDir(t *testing.T) {
	dirs := []string{filepath.Join("home", "u", ".claude", "skills")}
	in := filepath.Join(dirs[0], optName, "SKILL.md")
	if !isOptionalSkillFile(in, dirs) {
		t.Fatalf("%s must match", in)
	}
	trap := filepath.Join("work", "skills", optName, "app", ".claude", "other", "SKILL.md")
	if isOptionalSkillFile(trap, dirs) {
		t.Fatalf("%s lies outside the agent skills dir and must not match", trap)
	}
	if isOptionalSkillFile(filepath.Join(dirs[0], optName+"-x", "SKILL.md"), dirs) {
		t.Fatal("a sibling directory with the same prefix must not match")
	}
}
