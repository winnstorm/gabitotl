// Package opencode is the OpenCode runtime adapter.
//
// Verified against https://opencode.ai/docs/config/ (fetched 2026-10):
//   - Config is JSON or JSONC (comments allowed) and is merged, not replaced.
//     Order: global ~/.config/opencode/opencode.json, then OPENCODE_CONFIG (a
//     custom file), then project opencode.json, then .opencode directories.
//   - OPENCODE_CONFIG_DIR is a custom directory for agents, commands, modes and
//     plugins; XDG_CONFIG_HOME relocates the global directory.
//   - "instructions" is an array of paths and glob patterns of instruction files;
//     "model" is "provider/model"; "mcp" maps a name to a server whose local
//     form has "type": "local", a "command" array and "enabled".
//
// ASSUMPTIONS (not verifiable from the page we fetched): relative entries of
// "instructions" in a project opencode.json resolve against the project root;
// the global config is searched as opencode.jsonc as well as opencode.json.
//
// ASSUMPTION (not confirmed by official docs): skills are read from
// <config dir>/skills/<name>/SKILL.md, where the config dir is OPENCODE_CONFIG_DIR,
// else XDG_CONFIG_HOME/opencode, else ~/.config/opencode, and from
// <project>/.opencode/skills for project scope. The only source for this path is
// Gentle AI's steering text, which names ~/.config/opencode/skills; the page
// https://opencode.ai/docs/config/ does not list a skills directory.
//
// Only managed keys are spliced into the file: the persona lives in its own
// file (gabitotl-persona.md) referenced from "instructions", the memory server
// is one "mcp" entry, and "model" is written only when the user chose it in
// runtime.md. Providers and every other key are never touched.
//
// Attribution: no option that controls commit or pull request attribution is
// documented on https://opencode.ai/docs/config/ (fetched 2026-10), so none is
// written. The authorship guard relies on the Git hooks and the persona rule
// instead (docs/GUARD.md).
package opencode

import (
	"encoding/json"
	"path/filepath"

	rt "github.com/winnstorm/gabitotl/internal/runtime"
	"github.com/winnstorm/gabitotl/internal/runtime/engine"
)

// Name is the runtime id used in runtime.md.
const Name = "opencode"

const personaFile = "gabitotl-persona.md"

// Adapter implements runtime.Adapter for OpenCode.
type Adapter struct{}

// New returns the adapter.
func New() *Adapter { return &Adapter{} }

var _ rt.Adapter = (*Adapter)(nil)

func (*Adapter) Name() string  { return Name }
func (*Adapter) Label() string { return "OpenCode" }

func globalDir(env rt.Env) string {
	if x := env.Var("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "opencode")
	}
	return filepath.Join(env.Home, ".config", "opencode")
}

// firstExisting returns the first existing candidate, else the first one.
func firstExisting(env rt.Env, c ...string) string {
	for _, p := range c {
		if env.Has(p) {
			return p
		}
	}
	return c[0]
}

// configFile is the opencode.json(c) that receives managed keys.
func configFile(env rt.Env, s rt.Scope) string {
	if s == rt.ScopeProject && env.ProjectRoot != "" {
		return firstExisting(env, filepath.Join(env.ProjectRoot, "opencode.jsonc"), filepath.Join(env.ProjectRoot, "opencode.json"))
	}
	if p := env.Var("OPENCODE_CONFIG"); p != "" {
		return p
	}
	d := globalDir(env)
	return firstExisting(env, filepath.Join(d, "opencode.jsonc"), filepath.Join(d, "opencode.json"))
}

// personaPath is the file we own entirely; instructionRef is how the config points to it.
func personaPath(env rt.Env, s rt.Scope) (path, ref string) {
	if s == rt.ScopeProject && env.ProjectRoot != "" {
		return filepath.Join(env.ProjectRoot, ".opencode", personaFile), ".opencode/" + personaFile
	}
	base := globalDir(env)
	if d := env.Var("OPENCODE_CONFIG_DIR"); d != "" {
		base = d
	}
	p := filepath.Join(base, personaFile)
	return p, filepath.ToSlash(p)
}

// skillsDir is where OpenCode skills live (see the ASSUMPTION in the package doc).
func skillsDir(env rt.Env, s rt.Scope) string {
	if s == rt.ScopeProject && env.ProjectRoot != "" {
		return filepath.Join(env.ProjectRoot, ".opencode", "skills")
	}
	base := globalDir(env)
	if d := env.Var("OPENCODE_CONFIG_DIR"); d != "" {
		base = d
	}
	return filepath.Join(base, "skills")
}

func (a *Adapter) Detect(env rt.Env) rt.Detection {
	bin, hasBin := env.Look("opencode")
	gd := globalDir(env)
	gcfg := firstExisting(env, filepath.Join(gd, "opencode.jsonc"), filepath.Join(gd, "opencode.json"))
	hasG := env.Has(gcfg) || env.Has(gd)
	sig := []rt.Signal{
		{Name: "binary opencode", Found: hasBin, Detail: bin},
		{Name: "global config " + gd, Found: hasG},
	}
	hasP := false
	if env.ProjectRoot != "" {
		for _, p := range []string{"opencode.json", "opencode.jsonc", ".opencode"} {
			if env.Has(filepath.Join(env.ProjectRoot, p)) {
				hasP = true
			}
		}
		sig = append(sig, rt.Signal{Name: "project config", Found: hasP})
	}
	for _, v := range []string{"OPENCODE_CONFIG", "OPENCODE_CONFIG_DIR", "XDG_CONFIG_HOME"} {
		if val := env.Var(v); val != "" {
			sig = append(sig, rt.Signal{Name: "env " + v, Found: true, Detail: val})
		}
	}
	st, _ := env.Store().Load()
	managed := len(st.ForRuntime(Name)) > 0
	sig = append(sig, rt.Signal{Name: "GabitoTL assets", Found: managed})
	d := rt.Detection{Runtime: Name, Signals: sig}
	switch {
	case hasBin && managed:
		d.State = rt.StateConfigured
	case hasBin:
		d.State = rt.StateInstalled
	case hasG || hasP || managed:
		d.State = rt.StateDetectedHistory
	default:
		d.State = rt.StateNone
	}
	return d
}

func (a *Adapter) Inspect(env rt.Env, opts rt.Options) (rt.Inspection, error) {
	pp, _ := personaPath(env, opts.Scope)
	return engine.Inspect(env, Name, configFile(env, opts.Scope), pp), nil
}

func (a *Adapter) Backup(env rt.Env, snap *rt.Snapshot, plan []rt.Action) error {
	return engine.Backup(env, snap, plan)
}

func sameServer(m rt.MemoryMCP) func(json.RawMessage) bool {
	want := append([]string{m.Command}, m.Args...)
	return func(raw json.RawMessage) bool {
		var s struct {
			Command []string `json:"command"`
		}
		if json.Unmarshal(raw, &s) != nil || len(s.Command) != len(want) {
			return false
		}
		for i := range want {
			if s.Command[i] != want[i] {
				return false
			}
		}
		return true
	}
}

func (a *Adapter) Plan(env rt.Env, opts rt.Options) ([]rt.Action, error) {
	if opts.Scope == "" {
		opts.Scope = rt.ScopeGlobal
	}
	cfg := configFile(env, opts.Scope)
	pp, ref := personaPath(env, opts.Scope)
	plan := []rt.Action{
		engine.PlanSection(Name, pp, "persona", opts.PersonaText, ""),
		engine.PlanItem(Name, cfg, []string{"instructions"}, ref),
	}
	var kept []string
	for _, m := range opts.MemoryServers() {
		name := m.Name
		if name == "" {
			name = "engram"
		}
		kept = append(kept, name)
		val, _ := json.Marshal(map[string]any{"type": "local", "command": append([]string{m.Command}, m.Args...), "enabled": true})
		plan = append(plan, engine.PlanServer(Name, cfg, []string{"mcp"}, name, val, sameServer(m)))
	}
	plan = append(plan, engine.PlanRetireServers(env, Name, cfg, []string{"mcp"}, kept, opts.RetireMemory)...)
	plan = append(plan, engine.PlanSkills(env, Name, skillsDir(env, opts.Scope), opts.Skills)...)
	if opts.Model != "" { // the user chose it in runtime.md: replace the existing selection
		val, _ := json.Marshal(opts.Model)
		plan = append(plan, engine.PlanKey(Name, cfg, []string{"model"}, val, true))
	}
	return plan, nil
}

func (a *Adapter) Apply(env rt.Env, plan []rt.Action) (rt.Result, error) {
	return engine.Apply(env, Name, "", plan)
}

func (a *Adapter) Verify(env rt.Env, opts rt.Options) []rt.Check {
	plan, err := a.Plan(env, opts)
	if err != nil {
		return []rt.Check{{Name: "plan", Detail: err.Error()}}
	}
	return engine.Pending(plan)
}

func (a *Adapter) RemoveGabitoConfig(env rt.Env) (rt.Result, error) { return engine.Remove(env, Name) }

// ListModels is unsupported: the model catalog needs `opencode models`, which
// adapters do not run (no subprocess in adapters).
func (*Adapter) ListModels(rt.Env) ([]string, error) { return nil, rt.ErrUnsupported }
