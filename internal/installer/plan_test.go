package installer

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	rt "github.com/winnstorm/gabitotl/internal/runtime"
)

func buildPlan(t *testing.T, f *fixture, o Options) (Inventory, Plan) {
	t.Helper()
	inv := BuildInventory(context.Background(), f.env, nil)
	pl, err := BuildPlan(context.Background(), f.env, inv, o)
	if err != nil {
		t.Fatal(err)
	}
	return inv, pl
}

func kinds(pl Plan, k rt.Kind) []string {
	var out []string
	for _, it := range pl.Items {
		if it.Kind == k {
			out = append(out, it.Target)
		}
	}
	return out
}

func TestBuildPlanIsPureAndSplitsRetirement(t *testing.T) {
	f := newFixture(t)
	before := treeHash(t, f.home, f.root)
	_, pl := buildPlan(t, f, Options{Source: SourceGentleAI, MigratePersona: true})
	if treeHash(t, f.home, f.root) != before {
		t.Fatal("BuildPlan must write nothing")
	}
	creates := strings.Join(kinds(pl, rt.Create), "\n")
	for _, want := range []string{"/migrations/2026-10-05_2100_gentle-ai/", ".gabitotl/project.md", ".gabitotl/imports.md"} {
		if !strings.Contains(creates, want) {
			t.Errorf("CREATE lacks %s:\n%s", want, creates)
		}
	}
	if len(kinds(pl, rt.Modify)) == 0 {
		t.Error("expected MODIFY for the Claude persona section")
	}
	if got := kinds(pl, rt.Preserve); !slices.ContainsFunc(got, func(s string) bool { return strings.HasSuffix(s, "engram.db") }) ||
		!slices.ContainsFunc(got, func(s string) bool { return strings.HasSuffix(s, ".gentle-ai/backups") }) {
		t.Errorf("PRESERVE must list engram.db and the Gentle-AI backups: %v", got)
	}
	if len(kinds(pl, rt.NoAction)) == 0 {
		t.Error("user assets must be listed as NO ACTION")
	}
	// Invariant (b) shape: retirement actions are separate from install actions.
	if len(pl.Retire["claude-code"]) != 1 || pl.Retire["claude-code"][0].Kind != rt.RemoveAfterVerify {
		t.Fatalf("persona retirement: %+v", pl.Retire)
	}
	for name, acts := range pl.Install {
		for _, a := range acts {
			if a.Kind == rt.RemoveAfterVerify {
				t.Fatalf("%s: install phase carries a removal: %+v", name, a)
			}
		}
	}
}

func TestBuildPlanPersonaConflictNeedsExplicitConsent(t *testing.T) {
	f := newFixture(t)
	_, pl := buildPlan(t, f, Options{})
	if len(pl.Retire) != 0 || !pl.HasNote(NotePersonaConflict) {
		t.Fatalf("without consent the block is only reported: retire=%v notes=%v", pl.Retire, pl.Notes)
	}
}

func TestBuildPlanRejectsUnknownRuntimeAndHandlesMissingEngram(t *testing.T) {
	f := newFixture(t)
	inv := BuildInventory(context.Background(), f.env, nil)
	if _, err := BuildPlan(context.Background(), f.env, inv, Options{Runtimes: []string{"vscode"}}); !errors.Is(err, ErrUnknownRuntime) {
		t.Fatalf("err = %v", err)
	}
	f.bins["engram"] = false
	if err := removeAll(f.home + "/.engram"); err != nil {
		t.Fatal(err)
	}
	_, pl := buildPlan(t, f, Options{})
	if pl.Memory.Command != "" || !pl.HasNote(NoteEngramMissing) {
		t.Fatalf("a missing Engram is never installed or wired: %+v", pl)
	}
}

func TestBuildPlanTeamMemoryWithoutMigratorIsHonest(t *testing.T) {
	f := newFixture(t)
	_, pl := buildPlan(t, f, Options{Mode: "team"})
	if pl.MemoryChoice != "memory-mcp" || !pl.HasNote(NoteNoMigrator) {
		t.Fatalf("%+v", pl)
	}
	if pl.Memory.Command != "" {
		t.Fatalf("no concrete memory MCP exists yet, none may be configured: %+v", pl.Memory)
	}
}

func TestGentleUninstallArgsPrefersNarrowFormWhenAgentsAreNotAllCovered(t *testing.T) {
	g := GentleAI{Agents: []AgentRef{{"claude-code", "claude-code"}, {"opencode", "opencode"}}}
	if a, covered := GentleUninstallArgs(g, []string{"claude-code", "opencode"}); !covered || strings.Join(a, " ") != "uninstall --all" {
		t.Fatalf("%v %v", a, covered)
	}
	g.Agents = append(g.Agents, AgentRef{ID: "windsurf"}) // no V1 adapter: GabitoTL does not replace it
	a, covered := GentleUninstallArgs(g, []string{"claude-code", "opencode"})
	if covered || strings.Join(a, " ") != "uninstall --agent claude-code --agent opencode" {
		t.Fatalf("an agent GabitoTL does not cover must keep its Gentle-AI setup: %v %v", a, covered)
	}
	if a, _ := GentleUninstallArgs(g, []string{"kiro-cli"}); a != nil {
		t.Fatalf("no overlap means nothing to retire: %v", a)
	}
}
