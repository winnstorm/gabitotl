package installer

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	rt "github.com/winnstorm/gabitotl/internal/runtime"
)

var landing = Options{
	Source: SourceGentleAI, Runtimes: []string{"claude-code", "opencode", "kiro-ide"},
	RemoveGentleAI: true, MigratePersona: true, User: "gabi", UserName: "Gabi",
}

func logIndex(log []string, prefix string) int {
	return slices.IndexFunc(log, func(c string) bool { return strings.HasPrefix(c, prefix) })
}

func lastIndex(log []string, prefix string) int {
	for i := len(log) - 1; i >= 0; i-- {
		if strings.HasPrefix(log[i], prefix) {
			return i
		}
	}
	return -1
}

// coverAllAgents rewrites the fake Gentle-AI state so every agent it manages is
// one GabitoTL replaces, which makes `gentle-ai uninstall --all` safe to run.
func coverAllAgents(t *testing.T, f *fixture) {
	t.Helper()
	state := `{"version":"4.2.0","installed_agents":["claude-code","opencode","kiro-ide"],"components":["persona","engram"]}`
	if err := os.WriteFile(f.home+"/.gentle-ai/state.json", []byte(state), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSoftLandingOrderAndPreservation(t *testing.T) {
	f := newFixture(t)
	coverAllAgents(t, f)
	engramTree := treeHash(t, f.home+"/.engram")
	backups := treeHash(t, f.home+"/.gentle-ai/backups")
	steering := slurp(t, f.home+"/.kiro/steering/product.md")
	f.runner.Calls = nil

	res, err := runPlan(t, f, landing)
	if err != nil {
		t.Fatalf("%v (checks %+v)", err, Failed(res.Checks))
	}
	log := f.runner.Log()
	un := logIndex(log, "gentle-ai uninstall")
	if un < 0 {
		t.Fatalf("Gentle-AI's own uninstall never ran: %v", log)
	}
	if strings.Join(res.Retired.GentleArgs, " ") != "uninstall --all" {
		t.Fatalf("only the documented --all form may run: %v", res.Retired.GentleArgs)
	}
	// engram doctor -> export -> ... -> gentle-ai uninstall LAST -> doctor again.
	doc, exp := logIndex(log, "engram doctor"), logIndex(log, "engram export")
	if !(doc >= 0 && doc < exp && exp < un) {
		t.Fatalf("order must be doctor < export < uninstall: %v", log)
	}
	if un != lastIndexOf(log, "gentle-ai") || lastIndex(log, "engram doctor") < un {
		t.Fatalf("uninstall must be the last gentle-ai call and be followed by a fresh doctor: %v", log)
	}
	if index(res.Phases, PhaseRetire) < index(res.Phases, PhaseVerifyProject) || index(res.Phases, PhaseReport) < index(res.Phases, PhaseRetire) {
		t.Fatalf("phases: %v", res.Phases)
	}

	md := slurp(t, f.home+"/.claude/CLAUDE.md")
	if strings.Contains(md, "gentle-ai") || !strings.HasPrefix(md, fxUserMD) || !strings.Contains(md, "gabitotl:start persona") {
		t.Fatalf("only the managed Gentleman block may be retired:\n%s", md)
	}
	if len(res.Retired.Personas) != 1 {
		t.Fatalf("%+v", res.Retired)
	}
	if treeHash(t, f.home+"/.engram") != engramTree {
		t.Fatal("c: Engram must be untouched")
	}
	if treeHash(t, f.home+"/.gentle-ai/backups") != backups || slurp(t, f.home+"/.kiro/steering/product.md") != steering {
		t.Fatal("e: backups and user files must be preserved")
	}
	imp := slurp(t, f.root+"/.gabitotl/imports.md")
	for _, want := range []string{"source: gentle-ai", "status: complete", "## Memory", "## Retired", "persona: claude-code", "gentle-ai: uninstall", "gentle-ai-backups"} {
		if !strings.Contains(imp, want) {
			t.Errorf("imports.md lacks %q:\n%s", want, imp)
		}
	}
	if !strings.Contains(imp, "origin: gentle-ai") && !strings.Contains(imp, "gentle-ai") {
		t.Error("the registry front matter must survive")
	}
	if res.Snapshot.Manifest.Status != "complete" {
		t.Fatalf("status %s", res.Snapshot.Manifest.Status)
	}
	if _, err := os.Stat(f.home + "/.gabitotl/installations/" + res.Snapshot.ID + ".json"); err != nil {
		t.Fatal("installation record missing")
	}
}

func lastIndexOf(log []string, prefix string) int { return lastIndex(log, prefix) }

func TestMigrateWithoutRemovalLeavesGentleAIAlone(t *testing.T) {
	f := newFixture(t)
	o := landing
	o.RemoveGentleAI, o.MigratePersona = false, false
	res, err := runPlan(t, f, o)
	if err != nil {
		t.Fatal(err)
	}
	if f.runner.Count("gentle-ai") != 0 || len(res.Retired.Personas) != 0 {
		t.Fatalf("nothing may be retired without consent: %v %+v", f.runner.Log(), res.Retired)
	}
	if !strings.Contains(slurp(t, f.home+"/.claude/CLAUDE.md"), "gentle-ai:persona") {
		t.Fatal("the persona block stays until the user consents")
	}
	if index(res.Phases, PhaseRetire) >= 0 {
		t.Fatalf("no retire phase without anything to retire: %v", res.Phases)
	}
}

func TestUnreplacedGentleAgentMeansNoUninstallAllButPersonaIsRetired(t *testing.T) {
	f := newFixture(t) // Gentle-AI also manages windsurf, which GabitoTL does not replace
	inv, pl := buildPlan(t, f, landing)
	if !pl.HasNote(NoteGentleManual) || pl.GentleArgs != nil {
		t.Fatalf("the plan must tell the user to uninstall manually: %+v", pl)
	}
	f.runner.Calls = nil
	res, err := Run(context.Background(), f.env, inv, pl, nil, Hooks{ConfirmRetire: func(RetirePreview) bool {
		t.Error("the user must not be asked to run an uninstall that will not run")
		return true
	}})
	if err != nil {
		t.Fatal(err)
	}
	if f.runner.Count("gentle-ai") != 0 || len(res.Retired.GentleArgs) != 0 {
		t.Fatalf("gentle-ai uninstall must not run when --all would remove an agent GabitoTL keeps out: %v", f.runner.Log())
	}
	if len(res.Retired.Personas) != 1 || !slices.ContainsFunc(res.Warnings, func(w string) bool { return strings.Contains(w, "manually") }) {
		t.Fatalf("persona retirement still applies and the manual step is reported: %+v", res)
	}
}

func TestInteractiveRetirementIsAskedOnlyAfterVerification(t *testing.T) {
	f := newFixture(t)
	coverAllAgents(t, f)
	o := landing
	o.RemoveGentleAI = false
	inv, pl := buildPlan(t, f, o)
	asked := 0
	hooks := Hooks{ConfirmRetire: func(p RetirePreview) bool {
		asked++
		if f.runner.Count("gentle-ai") != 0 {
			t.Error("asked after the uninstall already ran")
		}
		if f.runner.Count("engram export") != 1 || !strings.Contains(slurp(t, f.home+"/.claude/CLAUDE.md"), "gabitotl:start persona") {
			t.Error("must be asked only after the snapshot and the GabitoTL configuration exist")
		}
		return true
	}}
	res, err := Run(context.Background(), f.env, inv, pl, nil, hooks)
	if err != nil || asked != 1 || f.runner.Count("gentle-ai uninstall") != 1 || len(res.Retired.GentleArgs) == 0 {
		t.Fatalf("err=%v asked=%d log=%v", err, asked, f.runner.Log())
	}

	// Declined: GabitoTL stays, Gentle-AI is not touched.
	g := newFixture(t)
	coverAllAgents(t, g)
	ginv, gpl := buildPlan(t, g, o)
	res, err = Run(context.Background(), g.env, ginv, gpl, nil, Hooks{ConfirmRetire: func(RetirePreview) bool { return false }})
	if err != nil || g.runner.Count("gentle-ai") != 0 || len(res.Retired.GentleArgs) != 0 {
		t.Fatalf("declined retirement must not run anything: %v %v", err, g.runner.Log())
	}
}

func TestRetirementNeverRunsWithoutCleanVerification(t *testing.T) {
	// The gate in code: failing critical checks refuse retirement outright.
	f := newFixture(t)
	inv, pl := buildPlan(t, f, landing)
	r := &run{env: f.env, inv: inv, pl: pl, res: &Result{Checks: []Check{{Group: "runtime", Name: "x", OK: false}}}}
	if err := r.retire(context.Background()); !errors.Is(err, ErrNotVerified) {
		t.Fatalf("err = %v", err)
	}
	r.res.Checks = nil
	if err := r.retire(context.Background()); !errors.Is(err, ErrNotVerified) {
		t.Fatalf("no verification at all is not verification: %v", err)
	}
	if f.runner.Count("gentle-ai") != 0 {
		t.Fatal("b: gentle-ai ran")
	}

	// End to end: a verification failure rolls back and Gentle-AI is untouched.
	g := newFixture(t)
	withAdapters(g, map[string]func(rt.Adapter) rt.Adapter{
		"opencode": func(a rt.Adapter) rt.Adapter { return wrapAdapter{Adapter: a, failVerify: true} },
	})
	claudeMD := slurp(t, g.home+"/.claude/CLAUDE.md")
	res, err := runPlan(t, g, landing)
	if !errors.Is(err, ErrVerifyFailed) || !res.RolledBack || g.runner.Count("gentle-ai") != 0 {
		t.Fatalf("err=%v log=%v", err, g.runner.Log())
	}
	if slurp(t, g.home+"/.claude/CLAUDE.md") != claudeMD {
		t.Fatal("the persona block was retired although verification failed")
	}
}

func TestFailedGentleUninstallKeepsVerifiedGabitoTLAndPointsToSnapshot(t *testing.T) {
	f := newFixture(t)
	coverAllAgents(t, f)
	f.runner.Handlers["gentle-ai uninstall"] = func([]string) (string, error) { return "", errors.New("exit status 2") }
	res, err := runPlan(t, f, landing)
	var pe *PartialError
	if !errors.As(err, &pe) || !errors.Is(err, ErrRetireFailed) {
		t.Fatalf("err = %v", err)
	}
	if res.RolledBack || !strings.Contains(slurp(t, f.home+"/.claude/CLAUDE.md"), "gabitotl:start persona") {
		t.Fatal("GabitoTL was verified; it must not be reverted because Gentle-AI's uninstall failed")
	}
	if res.Snapshot.Manifest.Status != "partial" || !strings.Contains(strings.Join(res.Warnings, " "), "gabitotl restore "+res.Snapshot.ID) {
		t.Fatalf("status=%s warnings=%v", res.Snapshot.Manifest.Status, res.Warnings)
	}
}

func TestReconnectsEntriesGentleUninstallRemoved(t *testing.T) {
	f := newFixture(t)
	coverAllAgents(t, f)
	f.runner.Handlers["gentle-ai uninstall"] = func([]string) (string, error) {
		writeFile(t, f.home+"/.claude.json", "{}\n") // Gentle-AI removed the shared engram MCP entry
		return "ok", nil
	}
	res, err := runPlan(t, f, landing)
	if err != nil {
		t.Fatalf("%v %+v", err, Failed(res.Checks))
	}
	if !strings.Contains(slurp(t, f.home+"/.claude.json"), `"engram"`) {
		t.Fatal("GabitoTL must reconnect its own memory entry and verify again")
	}
}

func TestNoGentleAIFoundNeverRunsItsUninstall(t *testing.T) {
	f := newFixture(t)
	f.bins["gentle-ai"] = false
	if err := os.RemoveAll(f.home + "/.gentle-ai"); err != nil {
		t.Fatal(err)
	}
	if _, err := runPlan(t, f, landing); err != nil {
		t.Fatal(err)
	}
	if f.runner.Count("gentle-ai") != 0 {
		t.Fatal("nothing to retire")
	}
}
