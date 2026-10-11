package installer

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
)

// engramProject makes the fixture project an initialized solo project on Engram
// whose Engram memory holds two observations, and returns its project key.
func engramProject(t *testing.T, r *teamRig) string {
	t.Helper()
	if _, err := gabitotl.Init(r.root, gabitotl.InitOptions{Name: "shop"}); err != nil {
		t.Fatal(err)
	}
	key := BuildInventory(context.Background(), r.env, nil).Project.Key
	r.runner.Handlers["engram projects"] = func([]string) (string, error) {
		return "Projects:\n  " + key + "  2 obs  1 sessions  0 prompts\n", nil
	}
	r.runner.Handlers["engram export"] = func(a []string) (string, error) {
		writeFile(t, a[1], engramExport(t, key))
		return "Exported to " + a[1] + "\n  Sessions: 1\n  Observations: 2\n  Prompts: 0\n", nil
	}
	return key
}

func memoryCfg(t *testing.T, r *teamRig) gabitotl.MemoryConfig {
	t.Helper()
	cfg, err := gabitotl.LoadMemory(gabitotl.NewLayout(r.root))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestChangingAnEngramProjectToTeamMemoryMigratesIt(t *testing.T) {
	r := newTeamRig(t)
	engramProject(t, r)
	if c := memoryCfg(t, r); c.Provider != "engram" || c.Mode != "solo" {
		t.Fatalf("precondition: %+v", c)
	}
	engramBefore := treeHash(t, filepath.Join(r.home, ".engram"))
	o := hostOpts()
	o.ImportEngram = true
	inv, pl, err := r.plan(o)
	if err != nil {
		t.Fatal(err)
	}
	if pl.Team.Import == nil || pl.Team.Import.Observations == 0 {
		t.Fatalf("the plan must show how many entries move: %+v", pl.Team.Import)
	}
	res, err := Run(context.Background(), r.env, inv, pl, nil, Hooks{})
	if err != nil {
		t.Fatal(err)
	}
	if c := memoryCfg(t, r); c.Provider != "memory-mcp" || c.Mode != "team" || c.Role != "host" {
		t.Fatalf("memory.md after the change: %+v", c)
	}
	if res.Import == nil || res.Import.Imported != 2 || !res.Import.Verified {
		t.Fatalf("%+v", res.Import)
	}
	// The previous memory.md is kept with the snapshot, and Engram is untouched.
	if !walkContains(t, "provider: engram", res.Snapshot.Dir) {
		t.Fatal("the snapshot must hold the memory.md of the previous provider")
	}
	if treeHash(t, filepath.Join(r.home, ".engram")) != engramBefore || r.runner.Count("engram save") != 0 {
		t.Fatal("the source memory is never modified")
	}
	// Re-running the change is idempotent: no duplicates.
	inv2, pl2, err := r.plan(o)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), r.env, inv2, pl2, nil, Hooks{}); err != nil {
		t.Fatal(err)
	}
	if st, _ := r.st.Stats(context.Background()); st.Observations != 2 {
		t.Fatalf("no duplicates: %+v", st)
	}
}

func TestAFailedChangeLeavesMemoryMdOnTheOldProvider(t *testing.T) {
	r := newTeamRig(t)
	engramProject(t, r)
	before := slurp(t, filepath.Join(r.root, ".gabitotl", "memory.md"))
	r.failStore = 2 // the import works, the verification of the team database fails
	o := hostOpts()
	o.ImportEngram = true
	inv, pl, err := r.plan(o)
	if err != nil {
		t.Fatal(err)
	}
	res, err := Run(context.Background(), r.env, inv, pl, nil, Hooks{})
	if err == nil || !res.RolledBack {
		t.Fatalf("want a rolled back run: %v", err)
	}
	if got := slurp(t, filepath.Join(r.root, ".gabitotl", "memory.md")); got != before {
		t.Fatalf("memory.md must be byte for byte the old one:\n%s", got)
	}
	// The report says what was migrated before the failure; nothing is deleted.
	if res.Import == nil || res.Import.Imported != 2 {
		t.Fatalf("what was migrated must be reported: %+v", res.Import)
	}
	if st, _ := r.st.Stats(context.Background()); st.Observations != 2 {
		t.Fatalf("migrated entries are kept: %+v", st)
	}
	// Retrying after the cause is gone finishes the change without duplicating.
	r.failStore = 0
	inv2, pl2, _ := r.plan(o)
	if _, err := Run(context.Background(), r.env, inv2, pl2, nil, Hooks{}); err != nil {
		t.Fatal(err)
	}
	if st, _ := r.st.Stats(context.Background()); st.Observations != 2 || memoryCfg(t, r).Provider != "memory-mcp" {
		t.Fatalf("retry: %+v %+v", st, memoryCfg(t, r))
	}
}

func TestADryRunChangeShowsTheCountAndChangesNothing(t *testing.T) {
	r := newTeamRig(t)
	engramProject(t, r)
	before := treeHash(t, r.home, r.root)
	o := hostOpts()
	o.ImportEngram, o.DryRun = true, true
	inv, pl, err := r.plan(o)
	if err != nil {
		t.Fatal(err)
	}
	var counted bool
	for _, it := range pl.Items {
		if it.Kind == KindImport && it.Count > 0 {
			counted = true
		}
	}
	if !counted {
		t.Fatalf("the plan must list the import with its entry count: %+v", pl.Items)
	}
	if res, err := Run(context.Background(), r.env, inv, pl, nil, Hooks{}); err != nil || !res.DryRun {
		t.Fatalf("%+v %v", res, err)
	}
	if treeHash(t, r.home, r.root) != before || r.runner.Count("engram export") != 0 || len(r.eng.Mutations) != 0 {
		t.Fatal("a dry run changes nothing")
	}
}

func TestTeamMemoryIsNeverMovedBackToEngram(t *testing.T) {
	r := newTeamRig(t)
	if _, err := gabitotl.Init(r.root, gabitotl.InitOptions{Name: "shop", Mode: gabitotl.ModeTeam}); err != nil {
		t.Fatal(err)
	}
	before := treeHash(t, r.home, r.root)
	_, _, err := r.plan(Options{Memory: "engram", Runtimes: []string{"claude-code"}})
	if !errors.Is(err, ErrNoReverseMigration) {
		t.Fatalf("want ErrNoReverseMigration, got %v", err)
	}
	if treeHash(t, r.home, r.root) != before {
		t.Fatal("a refused change leaves everything as it was")
	}
	// Not naming a provider keeps the project's own.
	if _, pl, err := r.plan(Options{Runtimes: []string{"claude-code"}}); err != nil || pl.MemoryChoice != "memory-mcp" {
		t.Fatalf("%v %v", err, pl.MemoryChoice)
	}
}

func TestDoctorReportsTheChosenProviderAndFlagsAMismatch(t *testing.T) {
	r := newTeamRig(t)
	engramProject(t, r)
	lines := func() []Line {
		for _, s := range Doctor(context.Background(), r.env).Sections {
			if s.Name == "memory" {
				return s.Lines
			}
		}
		return nil
	}
	find := func(code string) (Line, bool) {
		for _, l := range lines() {
			if l.Code == code {
				return l, true
			}
		}
		return Line{}, false
	}
	if l, ok := find(DocMemChosen); !ok || l.Status != LineOK || !strings.Contains(l.Detail, "engram (solo)") {
		t.Fatalf("chosen provider: %+v %v", l, ok)
	}
	if _, ok := find(DocMemMismatch); ok {
		t.Fatal("a consistent project has no mismatch")
	}
	// memory.md says memory-mcp but its mode is solo, and no server is configured.
	l := gabitotl.NewLayout(r.root)
	cfg := memoryCfg(t, r)
	cfg.Provider = gabitotl.MemoryMemoryMCP
	if err := gabitotl.SaveMemory(l, cfg); err != nil {
		t.Fatal(err)
	}
	if m, ok := find(DocMemMismatch); !ok || m.Status != LineWarn || !strings.Contains(m.Detail, "solo") {
		t.Fatalf("mismatch: %+v %v", m, ok)
	}
	if _, ok := find(DocMemMCPPending); !ok {
		t.Fatal("team memory without a server is reported as pending")
	}
	// mode team with Engram.
	cfg.Provider, cfg.Mode = gabitotl.MemoryEngram, gabitotl.ModeTeam
	if err := gabitotl.SaveMemory(l, cfg); err != nil {
		t.Fatal(err)
	}
	if m, ok := find(DocMemMismatch); !ok || !strings.Contains(m.Detail, "memory use memory-mcp") {
		t.Fatalf("mismatch: %+v %v", m, ok)
	}
}
