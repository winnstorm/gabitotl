package all

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	rt "github.com/winnstorm/gabitotl/internal/runtime"
)

// fixture machines: one config file per runtime that already holds a user entry.
type machine struct {
	runtime string
	file    func(home string) string
	initial string
}

var machines = []machine{
	{"claude-code", func(h string) string { return filepath.Join(h, ".claude.json") },
		"{\n  \"mcpServers\": {\n    \"other\": {\"command\": \"x\"}\n  }\n}\n"},
	{"opencode", func(h string) string { return filepath.Join(h, ".config", "opencode", "opencode.jsonc") },
		"{\n  // mine\n  \"instructions\": [\"CONTRIBUTING.md\"],\n  \"mcp\": {\n    \"other\": {\"type\": \"local\", \"command\": [\"x\"]}\n  }\n}\n"},
	{"kiro-cli", func(h string) string { return filepath.Join(h, ".kiro", "settings", "mcp.json") },
		"{\n  \"mcpServers\": {\n    \"other\": {\"command\": \"x\"}\n  }\n}\n"},
}

func setup(t *testing.T, m machine) (rt.Env, string) {
	t.Helper()
	home := t.TempDir()
	f := m.file(home)
	if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f, []byte(m.initial), 0o644); err != nil {
		t.Fatal(err)
	}
	return rt.Env{Home: home, StateDir: filepath.Join(home, ".gabitotl"), GOOS: "linux",
		Getenv:   func(string) string { return "" },
		LookPath: func(n string) (string, error) { return "/fake/" + n, nil },
		Exists:   func(p string) bool { _, err := os.Stat(p); return err == nil }}, f
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func optsFor(provider string, engramPresent bool) rt.Options {
	mem, extra, retire := rt.MemoryFor(provider, engramPresent)
	return rt.Options{Scope: rt.ScopeGlobal, PersonaText: "Soy Gabito.", Memory: mem, MemoryExtra: extra, RetireMemory: retire}
}

func apply(t *testing.T, a rt.Adapter, env rt.Env, o rt.Options) rt.Result {
	t.Helper()
	plan, err := a.Plan(env, o)
	if err != nil {
		t.Fatal(err)
	}
	res, err := a.Apply(env, plan)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func removals(res rt.Result) int {
	n := 0
	for _, r := range res.Removed {
		if r.Edit.Op == rt.OpRemoveKey {
			n++
		}
	}
	return n
}

// The memory provider is chosen per project (memory.md), never per machine: the
// runtime gets the project-routing bridge plus Engram's own entry whichever
// provider is asked for, and nothing is retired while Engram is available.
func TestBothProvidersAreRegisteredWhateverTheChoice(t *testing.T) {
	for _, m := range machines {
		t.Run(m.runtime, func(t *testing.T) {
			env, f := setup(t, m)
			a, ok := Registry().Get(m.runtime)
			if !ok {
				t.Fatal("no adapter")
			}

			apply(t, a, env, optsFor(gabitotl.MemoryEngram, true))
			got := read(t, f)
			if strings.Count(got, "gabitotl-memory") != 1 || strings.Count(got, `"engram": {`) != 1 || !strings.Contains(got, `"other"`) {
				t.Fatalf("bridge and engram entries expected once each:\n%s", got)
			}

			// A project that switches to team memory changes nothing in the runtime.
			before := got
			if res := apply(t, a, env, optsFor(gabitotl.MemoryMemoryMCP, true)); removals(res) != 0 {
				t.Fatalf("no entry may be retired while Engram is available: %+v", res.Removed)
			}
			if read(t, f) != before {
				t.Fatalf("the project's provider must not change the runtime config:\n%s", read(t, f))
			}
			plan, _ := a.Plan(env, optsFor(gabitotl.MemoryMemoryMCP, true))
			for _, ac := range plan {
				if ac.Kind == rt.Create || ac.Kind == rt.Modify || ac.Kind == rt.RemoveAfterVerify {
					t.Fatalf("not idempotent: %s", ac)
				}
			}
			for _, c := range a.Verify(env, optsFor(gabitotl.MemoryMemoryMCP, true)) {
				if !c.OK {
					t.Fatalf("verify: %+v", c)
				}
			}

			// Uninstall restores the user's file.
			if _, err := a.RemoveGabitoConfig(env); err != nil {
				t.Fatal(err)
			}
			if after := read(t, f); strings.Contains(after, "gabitotl") || strings.Contains(after, `"engram": {`) || !strings.Contains(after, `"other"`) {
				t.Fatalf("uninstall:\n%s", after)
			}
		})
	}
}

// Without the engram binary the team provider keeps working: the bridge alone.
func TestBridgeAloneWhenEngramIsMissing(t *testing.T) {
	for _, m := range machines {
		t.Run(m.runtime, func(t *testing.T) {
			env, f := setup(t, m)
			a, _ := Registry().Get(m.runtime)
			apply(t, a, env, optsFor(gabitotl.MemoryMemoryMCP, false))
			got := read(t, f)
			if strings.Count(got, "gabitotl-memory") != 1 || strings.Contains(got, `"engram": {`) || !strings.Contains(got, `"other"`) {
				t.Fatalf("only the bridge expected:\n%s", got)
			}
		})
	}
}

// The only obsolete entry is Engram's own once its binary is gone, and only the
// one GabitoTL created; the bridge and user entries stay. The Plan is pure.
func TestObsoleteEngramEntryIsRetiredOnlyWhenOwned(t *testing.T) {
	for _, m := range machines {
		t.Run(m.runtime, func(t *testing.T) {
			env, f := setup(t, m)
			a, _ := Registry().Get(m.runtime)
			apply(t, a, env, optsFor(gabitotl.MemoryEngram, true))
			before := read(t, f)
			plan, err := a.Plan(env, optsFor(gabitotl.MemoryEngram, false))
			if err != nil {
				t.Fatal(err)
			}
			if read(t, f) != before {
				t.Fatal("Plan wrote to disk")
			}
			var create, retire int
			for _, ac := range plan {
				switch {
				case ac.Kind == rt.Modify && strings.Contains(ac.Target, "gabitotl-memory"):
					create++
				case ac.Kind == rt.RemoveAfterVerify && ac.Edit.Op == rt.OpRemoveKey:
					retire++
				}
			}
			if create != 0 || retire != 1 {
				t.Fatalf("want one retirement and no new bridge entry, got %d/%d: %v", create, retire, plan)
			}
			if res := apply(t, a, env, optsFor(gabitotl.MemoryEngram, false)); removals(res) != 1 {
				t.Fatalf("one entry must be retired: %+v", res.Removed)
			}
			got := read(t, f)
			if strings.Count(got, "gabitotl-memory") != 1 || strings.Contains(got, `"engram": {`) || !strings.Contains(got, `"other"`) {
				t.Fatalf("after retiring:\n%s", got)
			}
			if strings.Contains(read(t, filepath.Join(env.StateDir, "state.json")), `"engram"`) {
				t.Fatal("the ownership state must forget the removed entry")
			}
		})
	}
}

// An engram entry the user created is never removed, even when it is equivalent
// to the one GabitoTL would manage and the binary later disappears.
func TestNeverRemovesAUserEngramEntry(t *testing.T) {
	cases := map[string]string{
		"claude-code": `{"mcpServers":{"my-engram":{"command":"engram","args":["mcp"]}}}`,
		"opencode":    `{"mcp":{"my-engram":{"type":"local","command":["engram","mcp"],"enabled":true}}}`,
		"kiro-cli":    `{"mcpServers":{"my-engram":{"command":"engram","args":["mcp"]}}}`,
	}
	for _, m := range machines {
		t.Run(m.runtime, func(t *testing.T) {
			m.initial = cases[m.runtime]
			env, f := setup(t, m)
			a, _ := Registry().Get(m.runtime)
			apply(t, a, env, optsFor(gabitotl.MemoryEngram, true)) // adopts the equivalent entry: no second engram entry
			apply(t, a, env, optsFor(gabitotl.MemoryEngram, false))
			got := read(t, f)
			if !strings.Contains(got, "my-engram") || !strings.Contains(got, "gabitotl-memory") || strings.Count(got, `"engram": {`) != 0 {
				t.Fatalf("the user's entry must survive:\n%s", got)
			}
		})
	}
}

// Kiro CLI and IDE share one MCP file: retiring Engram's entry planned for both
// before either applies must end with exactly one bridge entry and no engram entry.
func TestKiroVariantsRetireOnTheSharedFile(t *testing.T) {
	env, f := setup(t, machines[2])
	cli, _ := Registry().Get("kiro-cli")
	ide, _ := Registry().Get("kiro-ide")
	pc, _ := cli.Plan(env, optsFor(gabitotl.MemoryEngram, true))
	pi, _ := ide.Plan(env, optsFor(gabitotl.MemoryEngram, true))
	if _, err := cli.Apply(env, pc); err != nil {
		t.Fatal(err)
	}
	if _, err := ide.Apply(env, pi); err != nil {
		t.Fatal(err)
	}
	sc, _ := cli.Plan(env, optsFor(gabitotl.MemoryMemoryMCP, false))
	si, _ := ide.Plan(env, optsFor(gabitotl.MemoryMemoryMCP, false))
	if _, err := cli.Apply(env, sc); err != nil {
		t.Fatal(err)
	}
	if _, err := ide.Apply(env, si); err != nil {
		t.Fatalf("the second variant must tolerate the entry the first one already retired: %v", err)
	}
	got := read(t, f)
	if strings.Count(got, "gabitotl-memory") != 1 || strings.Contains(got, `"engram": {`) || !strings.Contains(got, `"other"`) {
		t.Fatalf("shared file:\n%s", got)
	}
}
