package installer

import (
	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/persona"
	rt "github.com/winnstorm/gabitotl/internal/runtime"
)

// RuntimeOptions builds the adapter options of a run outside the install flow
// (runtime plan, apply and verify, and the desktop manager): the persona in the
// language of the person, the skills and the memory MCP entries of the chosen
// provider. An empty memory choice reads the provider from the project's
// memory.md; the provider only validates the choice, because both providers are
// served by the same bridge entry. Engram's own entry is added when its binary
// is on PATH.
func RuntimeOptions(renv rt.Env, root string, scope rt.Scope, memory string, migrate bool) rt.Options {
	_, engramPresent := renv.Look("engram")
	if memory == "" && root != "" {
		if m, err := gabitotl.LoadMemory(gabitotl.NewLayout(root)); err == nil {
			memory = m.Provider
		}
	}
	mem, extra, retire := rt.MemoryFor(memory, engramPresent)
	return rt.Options{
		Scope: scope, PersonaText: persona.TextFor(gabitotl.ActivePersonaLang(renv.Home)), Skills: SkillAssets(),
		Memory: mem, MemoryExtra: extra, Tools: CodeGraphTools(renv, false), RetireMemory: retire, MigrateGentleman: migrate,
	}
}
