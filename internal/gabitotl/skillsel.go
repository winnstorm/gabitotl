package gabitotl

import (
	"os"
	"path/filepath"
	"slices"
)

// The optional skills a person turned on (docs/DECISIONS.md D62) are personal and
// global, like the persona language: <home>/.gabitotl/skills.md with the front
// matter key `optional`. A missing, unreadable or invalid file reads as none.

type skillsFront struct {
	Optional []string `yaml:"optional,omitempty"`
}

const bodySkills = `
# Optional skills

The optional skills installed in your agents (for example gabito-design). Personal
setting, never committed. Change it with ` + "`gabitotl skills enable|disable <name>`" + `.
`

// HomeSkillsPath is the file that stores the selected optional skills.
func HomeSkillsPath(home string) string {
	return filepath.Join(home, ".gabitotl", "skills.md")
}

// LoadOptionalSkills returns the optional skills saved in the home directory
// (os.UserHomeDir when home is empty), sorted. It never fails.
func LoadOptionalSkills(home string) []string {
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	if home == "" {
		return nil
	}
	d, err := ReadDoc(HomeSkillsPath(home))
	if err != nil || !d.HasFront {
		return nil
	}
	var f skillsFront
	if d.Decode(&f) != nil {
		return nil
	}
	out := slices.Clone(f.Optional)
	slices.Sort(out)
	return slices.Compact(out)
}

// SaveOptionalSkills stores the selection (sorted, without duplicates) and returns
// the file written.
func SaveOptionalSkills(home string, names []string) (string, error) {
	if home == "" {
		var err error
		if home, err = os.UserHomeDir(); err != nil {
			return "", err
		}
	}
	out := slices.Clone(names)
	slices.Sort(out)
	p := HomeSkillsPath(home)
	return p, WriteDoc(p, skillsFront{Optional: slices.Compact(out)}, bodySkills)
}
