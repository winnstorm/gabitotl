package installer

import (
	"maps"
	"path/filepath"
	"testing"

	"github.com/winnstorm/gabitotl/internal/skills"
)

func TestSkillAssetsMirrorTheEmbeddedSkills(t *testing.T) {
	got, want := SkillAssets(), skills.Required()
	if len(got) == 0 || len(got) != len(want) {
		t.Fatalf("assets %d, skills %d", len(got), len(want))
	}
	for i := range got {
		if got[i].Name != want[i].Name || got[i].Content != want[i].Content || !maps.Equal(got[i].Files, want[i].Files) {
			t.Errorf("asset %d differs from skill %s", i, want[i].Name)
		}
	}
}

// GabitoTL's own skills carry another author, so the Gentle AI retirement never
// selects them, even when they sit in the same Kiro skills directory.
func TestGentleRetirementNeverSelectsGabitoSkills(t *testing.T) {
	f, gentle, mine := gentleFixture(t)
	for _, s := range skills.All() {
		if a := skillAuthor([]byte(s.Content)); a != skills.Author || a == gentleKiroAuthor {
			t.Fatalf("%s: author %q must be %q", s.Name, a, skills.Author)
		}
		writeFile(t, filepath.Join(kiroBase(f), "skills", s.Name, "SKILL.md"), s.Content)
	}
	inv := DetectGentleKiro([]string{kiroBase(f)})
	chosen := map[string]bool{}
	for _, x := range inv.Files() {
		chosen[x.Path] = true
	}
	for _, s := range skills.All() {
		p := filepath.Join(kiroBase(f), "skills", s.Name, "SKILL.md")
		if chosen[p] {
			t.Errorf("a GabitoTL skill would be retired: %s", p)
		}
	}
	for p := range gentle {
		if !chosen[p] {
			t.Errorf("Gentle file no longer inventoried: %s", p)
		}
	}
	for p := range mine {
		if chosen[p] {
			t.Errorf("user file inventoried: %s", p)
		}
	}
}
