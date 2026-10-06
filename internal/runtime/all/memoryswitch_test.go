package all

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	rt "github.com/winnstorm/gabitotl/internal/runtime"
)

// fixture machines: one config file per runtime that already holds a user
// entry plus an equivalent engram entry of the user's own ("my-engram").
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

func optsFor(provider string) rt.Options {
	mem, retire := rt.MemoryFor(provider, true)
	return rt.Options{Scope: rt.ScopeGlobal, PersonaText: "Soy Gabito.", Memory: mem, RetireMemory: retire}
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

func TestMemoryModesAndSwitching(t *testing.T) {
	for _, m := range machines {
		t.Run(m.runtime, func(t *testing.T) {
			env, f := setup(t, m)
			a, ok := Registry().Get(m.runtime)
			if !ok {
				t.Fatal("no adapter")
			}
			// A second user entry that happens to be an engram entry: not ours.
			user := read(t, f)

			// 1. Engram mode (unchanged behavior): one managed engram entry.
			apply(t, a, env, optsFor(gabitotl.MemoryEngram))
			got := read(t, f)
			if strings.Count(got, `"engram": {`) != 1 || strings.Contains(got, "gabitotl-memory") || !strings.Contains(got, `"other"`) {
				t.Fatalf("engram mode:\n%s", got)
			}

			// 2. Switch to memory-mcp: ONE managed bridge entry, the managed engram entry is gone, user entries stay.
			res := apply(t, a, env, optsFor(gabitotl.MemoryMemoryMCP))
			got = read(t, f)
			if strings.Count(got, "gabitotl-memory") != 1 || !strings.Contains(got, `"memory"`) || !strings.Contains(got, `"mcp"`) {
				t.Fatalf("bridge entry missing:\n%s", got)
			}
			if strings.Contains(got, `"engram": {`) || !strings.Contains(got, `"other"`) {
				t.Fatalf("the managed engram entry must be removed and user entries kept:\n%s", got)
			}
			removed := 0
			for _, r := range res.Removed {
				if r.Edit.Op == rt.OpRemoveKey {
					removed++
				}
			}
			if removed != 1 {
				t.Fatalf("one entry must be retired: %+v", res.Removed)
			}
			if strings.Contains(read(t, filepath.Join(env.StateDir, "state.json")), `"engram"`) {
				t.Fatal("the ownership state must forget the removed entry")
			}

			// 3. Idempotent: nothing left to do, verify passes.
			plan, _ := a.Plan(env, optsFor(gabitotl.MemoryMemoryMCP))
			for _, ac := range plan {
				if ac.Kind == rt.Create || ac.Kind == rt.Modify || ac.Kind == rt.RemoveAfterVerify {
					t.Fatalf("not idempotent: %s", ac)
				}
			}
			before := read(t, f)
			apply(t, a, env, optsFor(gabitotl.MemoryMemoryMCP))
			if read(t, f) != before {
				t.Fatal("re-apply changed the file")
			}
			for _, c := range a.Verify(env, optsFor(gabitotl.MemoryMemoryMCP)) {
				if !c.OK {
					t.Fatalf("verify: %+v", c)
				}
			}

			// 4. Back to engram: the managed bridge entry is retired.
			apply(t, a, env, optsFor(gabitotl.MemoryEngram))
			got = read(t, f)
			if strings.Contains(got, "gabitotl-memory") || strings.Count(got, `"engram": {`) != 1 || !strings.Contains(got, `"other"`) {
				t.Fatalf("switching back:\n%s", got)
			}

			// 5. Uninstall restores the user's file.
			if _, err := a.RemoveGabitoConfig(env); err != nil {
				t.Fatal(err)
			}
			if after := read(t, f); strings.Contains(after, "gabitotl") || strings.Contains(after, `"engram": {`) || !strings.Contains(after, `"other"`) {
				t.Fatalf("uninstall:\n%s", after)
			}
			_ = user
		})
	}
}

// An engram entry the user created is never removed by a switch, even when it is
// equivalent to the one GabitoTL would manage.
func TestSwitchNeverRemovesAUserEngramEntry(t *testing.T) {
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
			apply(t, a, env, optsFor(gabitotl.MemoryEngram)) // adopts the equivalent entry: nothing added
			if strings.Count(read(t, f), "engram") != 3 && !strings.Contains(read(t, f), "my-engram") {
				t.Fatalf("setup:\n%s", read(t, f))
			}
			apply(t, a, env, optsFor(gabitotl.MemoryMemoryMCP))
			got := read(t, f)
			if !strings.Contains(got, "my-engram") || !strings.Contains(got, "gabitotl-memory") {
				t.Fatalf("the user's entry must survive the switch:\n%s", got)
			}
		})
	}
}

// A Plan for the new mode is pure: it writes nothing and names the removal.
func TestSwitchPlanIsPureAndReportsTheRemoval(t *testing.T) {
	for _, m := range machines {
		t.Run(m.runtime, func(t *testing.T) {
			env, f := setup(t, m)
			a, _ := Registry().Get(m.runtime)
			apply(t, a, env, optsFor(gabitotl.MemoryEngram))
			before := read(t, f)
			plan, err := a.Plan(env, optsFor(gabitotl.MemoryMemoryMCP))
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
			if create != 1 || retire != 1 {
				t.Fatalf("want one bridge entry and one retirement, got %d/%d: %v", create, retire, plan)
			}
		})
	}
}

// Kiro CLI and IDE share one MCP file: a switch planned for both before either
// applies must end with exactly one bridge entry and no engram entry.
func TestKiroVariantsSwitchOnTheSharedFile(t *testing.T) {
	env, f := setup(t, machines[2])
	cli, _ := Registry().Get("kiro-cli")
	ide, _ := Registry().Get("kiro-ide")
	pc, _ := cli.Plan(env, optsFor(gabitotl.MemoryEngram))
	pi, _ := ide.Plan(env, optsFor(gabitotl.MemoryEngram))
	if _, err := cli.Apply(env, pc); err != nil {
		t.Fatal(err)
	}
	if _, err := ide.Apply(env, pi); err != nil {
		t.Fatal(err)
	}
	sc, _ := cli.Plan(env, optsFor(gabitotl.MemoryMemoryMCP))
	si, _ := ide.Plan(env, optsFor(gabitotl.MemoryMemoryMCP))
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
