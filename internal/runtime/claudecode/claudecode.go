// Package claudecode is the Claude Code runtime adapter.
//
// Formats (verified 2026-10 against official docs unless marked ASSUMPTION):
//
//   - Memory files: CLAUDE.md, user scope ~/.claude/CLAUDE.md (Windows
//     %USERPROFILE%\.claude\CLAUDE.md) and project scope ./CLAUDE.md.
//     https://code.claude.com/docs/en/memory (page fetched; the scope table
//     was truncated in our fetch, so the exact user path is partly ASSUMPTION
//     from long-standing Claude Code behavior).
//
//   - MCP servers: project scope is .mcp.json at the project root with a
//     top-level "mcpServers" object; stdio entries use "type": "stdio",
//     "command", "args", "env". https://code.claude.com/docs/en/mcp (verified).
//     User/local scope lives in ~/.claude.json; the docs fetch mixed the user
//     and per-project ("projects.<path>.mcpServers") shapes, so we ASSUME a
//     top-level "mcpServers" object in ~/.claude.json is the user scope.
//
//   - Gentle-AI persona marker format "<!-- gentle-ai:NAME -->" ...
//     "<!-- /gentle-ai:NAME -->" is an ASSUMPTION (engine.foreign); only blocks
//     whose name contains "persona" or "gentleman" are ever retired.
//
//   - Attribution: settings.json (user ~/.claude/settings.json, project
//     .claude/settings.json) accepts an "attribution" object with "commit" and
//     "pr" strings (the text Claude Code adds to commits and pull requests) and
//     a boolean "sessionUrl"; "includeCoAuthoredBy" is deprecated in favor of it.
//     https://code.claude.com/docs/en/settings-reference (fetched 2026-10).
//     ASSUMPTION: an empty string hides the commit trailer and the PR line (the
//     long-standing documented way; one fetch of the page listed `false` for
//     the same purpose and another could not confirm the exact value), and
//     sessionUrl=false omits the claude.ai session link, as in the docs example.
//
// The adapter never rewrites a whole file: persona text is a marked section,
// and the memory MCP entry is a managed key spliced into the JSON.
package claudecode

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/winnstorm/gabitotl/internal/ownership"
	rt "github.com/winnstorm/gabitotl/internal/runtime"
	"github.com/winnstorm/gabitotl/internal/runtime/engine"
)

// Name is the runtime id used in runtime.md.
const Name = "claude-code"

// SectionPersona is the managed Markdown section name.
const SectionPersona = "persona"

// Adapter implements runtime.Adapter for Claude Code.
type Adapter struct{}

// New returns the adapter.
func New() *Adapter { return &Adapter{} }

var _ rt.Adapter = (*Adapter)(nil)

func (*Adapter) Name() string  { return Name }
func (*Adapter) Label() string { return "Claude Code" }

func (*Adapter) dir(env rt.Env) string { return filepath.Join(env.Home, ".claude") }

func (a *Adapter) personaPath(env rt.Env, s rt.Scope) string {
	if s == rt.ScopeProject && env.ProjectRoot != "" {
		return filepath.Join(env.ProjectRoot, "CLAUDE.md")
	}
	return filepath.Join(a.dir(env), "CLAUDE.md")
}

func (*Adapter) mcpPath(env rt.Env, s rt.Scope) string {
	if s == rt.ScopeProject && env.ProjectRoot != "" {
		return filepath.Join(env.ProjectRoot, ".mcp.json")
	}
	return filepath.Join(env.Home, ".claude.json")
}

func (a *Adapter) settingsPath(env rt.Env, s rt.Scope) string {
	if s == rt.ScopeProject && env.ProjectRoot != "" {
		return filepath.Join(env.ProjectRoot, ".claude", "settings.json")
	}
	return filepath.Join(a.dir(env), "settings.json")
}

// AttributionKeys are the managed settings that stop Claude Code from adding
// its own authorship line to commits and pull requests: authorship belongs to
// the person who asked for the work.
func AttributionKeys() []Key {
	return []Key{
		{[]string{"attribution", "commit"}, json.RawMessage(`""`)},
		{[]string{"attribution", "pr"}, json.RawMessage(`""`)},
		{[]string{"attribution", "sessionUrl"}, json.RawMessage(`false`)},
	}
}

// Attribution implements runtime.AttributionReporter: it reads settings.json
// (read-only) and reports whether every managed key has the expected value.
func (a *Adapter) Attribution(env rt.Env, scope rt.Scope) rt.AttributionState {
	path := a.settingsPath(env, scope)
	st := rt.AttributionState{Supported: true, Detail: path}
	data, err := os.ReadFile(path)
	if err != nil {
		return st
	}
	st.Hidden = true
	for _, k := range AttributionKeys() {
		cur, ok, err := ownership.Get(data, k.Path)
		if err != nil || !ok {
			st.Hidden = false
			continue
		}
		if !bytes.Equal(bytes.TrimSpace(cur), k.Value) {
			st.Hidden, st.Custom = false, true
		}
	}
	return st
}

// Key is one managed settings key and the value GabitoTL sets.
type Key struct {
	Path  []string
	Value json.RawMessage
}

func (a *Adapter) Detect(env rt.Env) rt.Detection {
	bin, hasBin := env.Look("claude")
	cfg := a.dir(env)
	hasCfg := env.Has(cfg)
	st, _ := env.Store().Load()
	managed := len(st.ForRuntime(Name)) > 0
	d := rt.Detection{Runtime: Name, Signals: []rt.Signal{
		{Name: "binary claude", Found: hasBin, Detail: bin},
		{Name: "config " + cfg, Found: hasCfg},
		{Name: "GabitoTL assets", Found: managed},
	}}
	switch {
	case hasBin && managed:
		d.State = rt.StateConfigured
	case hasBin:
		d.State = rt.StateInstalled
	case hasCfg || managed:
		d.State = rt.StateDetectedHistory
	default:
		d.State = rt.StateNone
	}
	return d
}

func (a *Adapter) Inspect(env rt.Env, opts rt.Options) (rt.Inspection, error) {
	return engine.Inspect(env, Name, a.personaPath(env, opts.Scope), a.mcpPath(env, opts.Scope), a.settingsPath(env, opts.Scope)), nil
}

func (a *Adapter) Backup(env rt.Env, snap *rt.Snapshot, plan []rt.Action) error {
	return engine.Backup(env, snap, plan)
}

// sameServer reports whether a server entry already runs the requested command.
func sameServer(m rt.MemoryMCP) func(json.RawMessage) bool {
	return func(raw json.RawMessage) bool {
		var s struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		}
		if json.Unmarshal(raw, &s) != nil || s.Command != m.Command || len(s.Args) != len(m.Args) {
			return false
		}
		for i := range s.Args {
			if s.Args[i] != m.Args[i] {
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
	pp := a.personaPath(env, opts.Scope)
	plan := []rt.Action{engine.PlanSection(Name, pp, SectionPersona, opts.PersonaText, "")}
	plan = append(plan, engine.PlanForeignPersona(Name, pp, "gentle-ai", opts.MigrateGentleman)...)
	if m := opts.Memory; m.Command != "" {
		name := m.Name
		if name == "" {
			name = "engram"
		}
		val, _ := json.Marshal(map[string]any{"type": "stdio", "command": m.Command, "args": m.Args})
		plan = append(plan, engine.PlanServer(Name, a.mcpPath(env, opts.Scope), []string{"mcpServers"}, name, val, sameServer(m)))
	}
	for _, k := range AttributionKeys() {
		plan = append(plan, engine.PlanKey(Name, a.settingsPath(env, opts.Scope), k.Path, k.Value, false))
	}
	plan = append(plan, engine.PlanRetireServers(env, Name, a.mcpPath(env, opts.Scope), []string{"mcpServers"}, opts.Memory.Name, opts.RetireMemory)...)
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

func (*Adapter) ListModels(rt.Env) ([]string, error) { return nil, rt.ErrUnsupported }
