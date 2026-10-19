// Package kiro implements the Kiro CLI and Kiro IDE adapters on one shared
// core, because both read the same ~/.kiro tree (installer spec sections 10, 11
// and 35). Assets are keyed by file, not by variant: selecting both runtimes
// never duplicates the steering file or the MCP entry, and removing one variant
// keeps the shared assets until the last owner is removed.
//
// Verified (2026-10):
//   - Steering: markdown files in .kiro/steering (workspace) and ~/.kiro/steering
//     (global); front matter selects the inclusion mode (always, fileMatch,
//     manual, auto); workspace wins on conflict. https://kiro.dev/docs/steering/
//   - MCP: JSON with a top-level "mcpServers" object; local servers need
//     "command" (plus "args"/"env"), remote ones "url"; "disabled", "autoApprove"
//     and "disabledTools" are optional. Files: workspace .kiro/settings/mcp.json
//     and user ~/.kiro/settings/mcp.json, workspace wins.
//     https://kiro.dev/docs/mcp/configuration/
//   - Scopes and KIRO_HOME (redirects the global ~/.kiro directory); steering,
//     skills and hooks merge across scopes, MCP servers and agents resolve to the
//     highest-priority scope. https://kiro.dev/docs/configuration/
//   - CLI settings live at ~/.kiro/settings/cli.json and the binary is kiro-cli
//     (redirect stub of https://kiro.dev/docs/cli/chat/configuration/).
//   - Skills: <base>/skills/<name>/SKILL.md under the same base as steering (global
//     KIRO_HOME or ~/.kiro, workspace .kiro). The file is keyed by path, so the CLI
//     and the IDE share one copy and the first variant removed leaves it for the other.
//
// ASSUMPTIONS: the "inclusion: always" front matter key spelling; the Kiro IDE
// install locations per platform (macOS /Applications/Kiro.app, Windows
// %LOCALAPPDATA%\Programs\Kiro, Linux a "kiro" launcher or /opt/Kiro); the name
// of the model key inside cli.json, so the adapter never writes a model.
//
// Attribution: no setting that controls commit or pull request attribution was
// found in the Kiro documentation pages fetched (2026-10, https://kiro.dev/docs/cli/),
// so none is written. The authorship guard relies on the Git hooks and the persona
// rule instead (docs/GUARD.md).
package kiro

import (
	"encoding/json"
	"path/filepath"
	"slices"

	rt "github.com/winnstorm/gabitotl/internal/runtime"
	"github.com/winnstorm/gabitotl/internal/runtime/engine"
)

// Runtime ids used in runtime.md.
const (
	NameCLI = "kiro-cli"
	NameIDE = "kiro-ide"
)

const steeringHeader = "---\ninclusion: always\n---\n\n"

// Core holds everything the two variants share.
type Core struct{ name, label string }

func home(env rt.Env) string {
	if h := env.Var("KIRO_HOME"); h != "" {
		return h
	}
	return filepath.Join(env.Home, ".kiro")
}

func (Core) base(env rt.Env, s rt.Scope) string {
	if s == rt.ScopeProject && env.ProjectRoot != "" {
		return filepath.Join(env.ProjectRoot, ".kiro")
	}
	return home(env)
}

// ForeignAssetRoots lists the Kiro directories that exist and can hold assets of
// another harness: the global tree (KIRO_HOME or ~/.kiro) and the workspace
// .kiro. The installer looks for Gentle AI's files only inside them.
func (Core) ForeignAssetRoots(env rt.Env) []string {
	var out []string
	for _, s := range []rt.Scope{rt.ScopeGlobal, rt.ScopeProject} {
		b := Core{}.base(env, s)
		if s == rt.ScopeProject && env.ProjectRoot == "" {
			continue
		}
		if env.Has(b) && !slices.Contains(out, b) {
			out = append(out, b)
		}
	}
	return out
}

func (c Core) steeringPath(env rt.Env, s rt.Scope) string {
	return filepath.Join(c.base(env, s), "steering", "gabitotl.md")
}
func (c Core) skillsDir(env rt.Env, s rt.Scope) string {
	return filepath.Join(c.base(env, s), "skills")
}

// hooksPath is the one owned hook file. It is per-person: it always lives under
// the user's Kiro home, never in the shared project .kiro, because teammates
// without gabitotl would get an error on every prompt.
func (Core) hooksPath(env rt.Env) string {
	return filepath.Join(home(env), "hooks", "gabitotl.json")
}

// hooksJSON is the Kiro IDE 1.0 / CLI V3 unified hook file (kiro.dev/docs/hooks).
// Command hooks add STDOUT to the agent context on exit 0, so both commands
// print nothing unless they have something to say.
func hooksJSON(env rt.Env) string {
	type action struct {
		Type    string `json:"type"`
		Command string `json:"command"`
	}
	type hook struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Trigger     string `json:"trigger"`
		Action      action `json:"action"`
		Timeout     int    `json:"timeout"`
	}
	b, _ := json.MarshalIndent(map[string]any{"version": "v1", "hooks": []hook{
		{"gabitotl-persona-mode", "Tell the agent the active GabitoTL chat mode", "SessionStart",
			action{"command", env.Command("persona mode --hook --format text")}, 10},
		{"gabitotl-capture-hook-input", "Record the hook payload Kiro sends (silent) so context hooks can be enabled", "SessionStart",
			action{"command", env.Command("context hook --format text --event session --dump")}, 10},
	}}, "", "  ")
	return string(b) + "\n"
}

func (c Core) mcpPath(env rt.Env, s rt.Scope) string {
	return filepath.Join(c.base(env, s), "settings", "mcp.json")
}

func (c Core) Name() string  { return c.name }
func (c Core) Label() string { return c.label }

func (c Core) signals(env rt.Env) (sig []rt.Signal, hasKiroDir, workspace bool) {
	h := home(env)
	hasKiroDir = env.Has(h)
	if env.ProjectRoot != "" {
		workspace = env.Has(filepath.Join(env.ProjectRoot, ".kiro"))
	}
	sig = []rt.Signal{
		{Name: "shared config " + h, Found: hasKiroDir},
		{Name: "workspace .kiro", Found: workspace},
	}
	return
}

func (c Core) finish(env rt.Env, d rt.Detection, present, history bool) rt.Detection {
	st, _ := env.Store().Load()
	managed := len(st.ForRuntime(c.name)) > 0
	d.Signals = append(d.Signals, rt.Signal{Name: "GabitoTL assets", Found: managed})
	switch {
	case present && managed:
		d.State = rt.StateConfigured
	case present:
		d.State = rt.StateInstalled
	case history || managed:
		d.State = rt.StateDetectedHistory
	default:
		d.State = rt.StateNone
	}
	return d
}

func (c Core) Inspect(env rt.Env, opts rt.Options) (rt.Inspection, error) {
	return engine.Inspect(env, c.name, c.steeringPath(env, opts.Scope), c.mcpPath(env, opts.Scope)), nil
}

func (c Core) Backup(env rt.Env, snap *rt.Snapshot, plan []rt.Action) error {
	return engine.Backup(env, snap, plan)
}

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

// Plan is identical for both variants apart from the owner name, so the second
// selected variant finds every asset already in place (NO-ACTION).
func (c Core) Plan(env rt.Env, opts rt.Options) ([]rt.Action, error) {
	if opts.Scope == "" {
		opts.Scope = rt.ScopeGlobal
	}
	plan := []rt.Action{engine.PlanSection(c.name, c.steeringPath(env, opts.Scope), "persona", opts.PersonaText, steeringHeader)}
	var kept []string
	for _, m := range opts.MemoryServers() {
		name := m.Name
		if name == "" {
			name = "engram"
		}
		kept = append(kept, name)
		val, _ := json.Marshal(map[string]any{"command": m.Command, "args": m.Args})
		plan = append(plan, engine.PlanServer(c.name, c.mcpPath(env, opts.Scope), []string{"mcpServers"}, name, val, sameServer(m)))
	}
	plan = append(plan, engine.PlanRetireServers(env, c.name, c.mcpPath(env, opts.Scope), []string{"mcpServers"}, kept, opts.RetireMemory)...)
	plan = append(plan, engine.PlanSkills(env, c.name, c.skillsDir(env, opts.Scope), opts.Skills)...)
	plan = append(plan, engine.PlanFile(env, c.name, c.hooksPath(env), hooksJSON(env)))
	return plan, nil
}

func (c Core) Apply(env rt.Env, plan []rt.Action) (rt.Result, error) {
	return engine.Apply(env, c.name, "", plan)
}

func (c Core) Verify(env rt.Env, opts rt.Options) []rt.Check {
	plan, err := c.Plan(env, opts)
	if err != nil {
		return []rt.Check{{Name: "plan", Detail: err.Error()}}
	}
	return engine.Pending(plan)
}

func (c Core) RemoveGabitoConfig(env rt.Env) (rt.Result, error) { return engine.Remove(env, c.name) }

// ListModels is unsupported: no verified model catalog source.
func (Core) ListModels(rt.Env) ([]string, error) { return nil, rt.ErrUnsupported }

// CLI is the Kiro CLI adapter.
type CLI struct{ Core }

// IDE is the Kiro IDE adapter.
type IDE struct{ Core }

// NewCLI and NewIDE return the adapters.
func NewCLI() *CLI { return &CLI{Core{NameCLI, "Kiro CLI"}} }
func NewIDE() *IDE { return &IDE{Core{NameIDE, "Kiro IDE"}} }

var (
	_ rt.Adapter = (*CLI)(nil)
	_ rt.Adapter = (*IDE)(nil)
)

func (c *CLI) Detect(env rt.Env) rt.Detection {
	bin, hasBin := env.Look("kiro-cli")
	cfg := filepath.Join(home(env), "settings", "cli.json")
	hasCfg := env.Has(cfg)
	sig, _, _ := c.signals(env)
	d := rt.Detection{Runtime: NameCLI, Signals: append([]rt.Signal{
		{Name: "binary kiro-cli", Found: hasBin, Detail: bin},
		{Name: "CLI settings " + cfg, Found: hasCfg},
	}, sig...)}
	return c.finish(env, d, hasBin, hasCfg)
}

// idePaths are the application locations probed per platform.
func idePaths(env rt.Env) []string {
	switch env.GOOS {
	case "darwin":
		return []string{"/Applications/Kiro.app", filepath.Join(env.Home, "Applications", "Kiro.app")}
	case "windows":
		if l := env.Var("LOCALAPPDATA"); l != "" {
			return []string{filepath.Join(l, "Programs", "Kiro", "Kiro.exe")}
		}
		return nil
	}
	return []string{"/usr/share/kiro", "/opt/Kiro", "/usr/bin/kiro"}
}

func (c *IDE) Detect(env rt.Env) rt.Detection {
	app, found := "", false
	for _, p := range idePaths(env) {
		if env.Has(p) {
			app, found = p, true
			break
		}
	}
	if bin, ok := env.Look("kiro"); ok && !found {
		app, found = bin, true
	}
	sig, hasDir, _ := c.signals(env)
	d := rt.Detection{Runtime: NameIDE, Signals: append([]rt.Signal{{Name: "Kiro IDE application", Found: found, Detail: app}}, sig...)}
	return c.finish(env, d, found, hasDir)
}
