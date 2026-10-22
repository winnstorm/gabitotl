package cli

import (
	"slices"
	"strings"
	"testing"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
)

func TestSkillsCommandValidatesTheName(t *testing.T) {
	for _, args := range [][]string{{"skills", "enable"}, {"skills", "enable", "nope"}, {"skills", "disable", "a", "b"}, {"skills"}} {
		if r := runIn(t, "", false, t.TempDir(), args...); r.code != ExitUsage {
			t.Errorf("%v: exit %d, want usage; stderr %q", args, r.code, r.err)
		}
	}
}

// With no managed agent the choice is still remembered, and --dry-run never is.
func TestSkillsEnableDisableRemembersTheChoice(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	wd := t.TempDir()
	if r := runIn(t, "", false, wd, "skills", "enable", "gabito-design", "--dry-run"); r.code != ExitOK || len(gabitotl.LoadOptionalSkills(home)) != 0 {
		t.Fatalf("dry run: %+v saved %v", r, gabitotl.LoadOptionalSkills(home))
	}
	if r := runIn(t, "", false, wd, "skills", "enable", "gabito-design"); r.code != ExitOK || !strings.Contains(r.out, "enabled") {
		t.Fatalf("enable: %+v", r)
	}
	if got := gabitotl.LoadOptionalSkills(home); !slices.Equal(got, []string{"gabito-design"}) {
		t.Fatalf("saved %v", got)
	}
	if r := runIn(t, "", false, wd, "skills", "list"); r.code != ExitOK || !strings.Contains(r.out, "gabito-design: on") {
		t.Fatalf("list: %+v", r)
	}
	if r := runIn(t, "", false, wd, "skills", "disable", "gabito-design"); r.code != ExitOK {
		t.Fatalf("disable: %+v", r)
	}
	if got := gabitotl.LoadOptionalSkills(home); len(got) != 0 {
		t.Fatalf("saved after disable: %v", got)
	}
}

func TestInstallRefusesAnUnknownSkill(t *testing.T) {
	r := runIn(t, "", false, t.TempDir(), "install", "--yes", "--dry-run", "--skill", "nope")
	if r.code != ExitUsage || !strings.Contains(r.err, "not an optional skill") {
		t.Fatalf("%+v", r)
	}
}
