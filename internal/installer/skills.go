package installer

import (
	"slices"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	rt "github.com/winnstorm/gabitotl/internal/runtime"
	"github.com/winnstorm/gabitotl/internal/skills"
)

func asset(s skills.Skill) rt.Asset {
	return rt.Asset{Name: s.Name, Content: s.Content, Files: s.Files}
}

// SkillAssets returns the embedded GabitoTL skills every install gets (the
// optional ones are chosen, see SkillSet), in the form adapters plan them
// (package runtime must not import package skills, so the installer converts).
func SkillAssets() []rt.Asset {
	all := skills.Required()
	out := make([]rt.Asset, len(all))
	for i, s := range all {
		out[i] = asset(s)
	}
	return out
}

// OptionalSkills lists the names of the optional skills GabitoTL can install.
func OptionalSkills() []string { return skills.OptionalNames() }

// SelectedSkills keeps the names of selected that are real optional skills.
func SelectedSkills(selected []string) []string {
	var out []string
	for _, n := range skills.OptionalNames() {
		if slices.Contains(selected, n) {
			out = append(out, n)
		}
	}
	return out
}

// SkillSet returns what adapters install (the required skills plus the selected
// optional ones) and the optional skills to retire (the unselected ones).
func SkillSet(selected []string) (install []rt.Asset, retire []string) {
	for _, s := range skills.All() {
		if !s.Optional || slices.Contains(selected, s.Name) {
			install = append(install, asset(s))
		} else {
			retire = append(retire, s.Name)
		}
	}
	return install, retire
}

// SavedSkillSet is SkillSet for the selection saved in home.
func SavedSkillSet(home string) ([]rt.Asset, []string) {
	return SkillSet(gabitotl.LoadOptionalSkills(home))
}
