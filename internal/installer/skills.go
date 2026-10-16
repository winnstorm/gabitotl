package installer

import (
	rt "github.com/winnstorm/gabitotl/internal/runtime"
	"github.com/winnstorm/gabitotl/internal/skills"
)

// SkillAssets returns the embedded GabitoTL skills in the form adapters plan
// them (package runtime must not import package skills, so the installer converts). Every install, plan, verify and doctor path uses it, so
// they all agree on the same files.
func SkillAssets() []rt.Asset {
	all := skills.All()
	out := make([]rt.Asset, len(all))
	for i, s := range all {
		out[i] = rt.Asset{Name: s.Name, Content: s.Content}
	}
	return out
}
