package installer

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	rt "github.com/winnstorm/gabitotl/internal/runtime"
	"github.com/winnstorm/gabitotl/internal/runtime/all"
)

// wrapAdapter decorates a real adapter with fault injection and probes.
type wrapAdapter struct {
	rt.Adapter
	failApply   bool
	failVerify  bool
	beforeApply func()
	applied     *int
}

func (w wrapAdapter) Apply(env rt.Env, plan []rt.Action) (rt.Result, error) {
	if w.beforeApply != nil {
		w.beforeApply()
	}
	if w.applied != nil {
		*w.applied++
	}
	if w.failApply {
		return rt.Result{}, errors.New("injected apply failure")
	}
	return w.Adapter.Apply(env, plan)
}

func (w wrapAdapter) Verify(env rt.Env, o rt.Options) []rt.Check {
	if w.failVerify {
		return []rt.Check{{Name: "injected", OK: false, Detail: "injected verify failure"}}
	}
	return w.Adapter.Verify(env, o)
}

// withAdapters replaces the registry with the real adapters, each optionally wrapped.
func withAdapters(f *fixture, wrap map[string]func(rt.Adapter) rt.Adapter) {
	reg := &rt.Registry{}
	for _, a := range all.Registry().All() {
		if w, ok := wrap[a.Name()]; ok {
			a = w(a)
		}
		reg.Register(a)
	}
	f.env.Runtimes = reg
}

func runPlan(t *testing.T, f *fixture, o Options) (Result, error) {
	t.Helper()
	inv, pl := buildPlan(t, f, o)
	return Run(context.Background(), f.env, inv, pl, nil, Hooks{})
}

func index(ps []Phase, p Phase) int {
	for i, x := range ps {
		if x == p {
			return i
		}
	}
	return -1
}

func TestRunInstallConfiguresAdoptsEngramAndKeepsUserContent(t *testing.T) {
	f := newFixture(t)
	engramBefore := treeHash(t, f.home+"/.engram")
	gentleBackups := treeHash(t, f.home+"/.gentle-ai/backups")
	res, err := runPlan(t, f, Options{User: "gabi", UserName: "Gabi"})
	if err != nil {
		t.Fatal(err)
	}
	want := []Phase{PhasePlan, PhaseSnapshot, PhaseMigrate, PhaseConfigure, PhaseVerifyMemory, PhaseVerifyProject}
	for i := 1; i < len(want); i++ {
		if index(res.Phases, want[i-1]) < 0 || index(res.Phases, want[i-1]) >= index(res.Phases, want[i]) {
			t.Fatalf("phases out of order: %v", res.Phases)
		}
	}
	md := slurp(t, f.home+"/.claude/CLAUDE.md")
	if !strings.HasPrefix(md, fxUserMD) || !strings.Contains(md, "gabitotl:start persona") || !strings.Contains(md, "gentle-ai:persona") {
		t.Fatalf("user content and the (unconsented) Gentle-AI block must survive:\n%s", md)
	}
	if !strings.Contains(slurp(t, f.home+"/.config/opencode/opencode.json"), `"anthropic"`) {
		t.Fatal("OpenCode user config lost")
	}
	l := gabitotl.NewLayout(f.root)
	if m, err := gabitotl.LoadMemory(l); err != nil || m.Provider != "engram" {
		t.Fatalf("%+v %v", m, err)
	}
	if rc, _ := gabitotl.LoadRuntime(l); rc.Runtime == "unset" {
		t.Fatal("runtime.md must name the first configured runtime")
	}
	if len(f.mem.saved) == 0 {
		t.Fatal("knowledge must be saved through the memory provider")
	}
	// Invariants c, d, e.
	if treeHash(t, f.home+"/.engram") != engramBefore {
		t.Fatal("c: Engram data dir changed")
	}
	if treeHash(t, f.home+"/.gentle-ai/backups") != gentleBackups {
		t.Fatal("e: Gentle-AI backups changed")
	}
	for _, c := range f.runner.Log() {
		if strings.HasPrefix(c, "engram ") {
			if sub := strings.Fields(c)[1]; sub != "doctor" && sub != "projects" && sub != "export" {
				t.Fatalf("c: unexpected engram subcommand %q", c)
			}
		}
		if strings.HasPrefix(c, "gentle-ai") {
			t.Fatalf("a plain install must not run gentle-ai: %q", c)
		}
	}
	if f.runner.Count("engram doctor") < 3 { // detection, snapshot (before), verify (after)
		t.Fatalf("engram doctor must bracket the run: %v", f.runner.Log())
	}
}

func TestRunNothingMutatesBeforeTheSnapshotExists(t *testing.T) {
	f := newFixture(t)
	probe := func() {
		snaps, _ := filepath.Glob(f.home + "/.gabitotl/migrations/*/manifest.json")
		exp, _ := filepath.Glob(f.home + "/.gabitotl/migrations/*/memory/engram-export.json")
		if len(snaps) != 1 || len(exp) != 1 {
			t.Errorf("a: a runtime mutation started before the snapshot (manifest=%v export=%v)", snaps, exp)
		}
		if gabitotl.Exists(f.root) == false {
			t.Error("the project must exist before runtimes are configured (knowledge is migrated first)")
		}
	}
	withAdapters(f, map[string]func(rt.Adapter) rt.Adapter{
		"claude-code": func(a rt.Adapter) rt.Adapter { return wrapAdapter{Adapter: a, beforeApply: probe} },
	})
	if _, err := runPlan(t, f, Options{Runtimes: []string{"claude-code"}}); err != nil {
		t.Fatal(err)
	}
}

func TestRunFailedSnapshotLeavesTheMachineUntouched(t *testing.T) {
	f := newFixture(t)
	f.runner.Handlers["engram export"] = func([]string) (string, error) { return "", errors.New("disk full") }
	calls := 0
	withAdapters(f, map[string]func(rt.Adapter) rt.Adapter{
		"claude-code": func(a rt.Adapter) rt.Adapter { return wrapAdapter{Adapter: a, applied: &calls} },
	})
	before, rootBefore := treeHashExcluding(t, f.home, ".gabitotl"), treeHash(t, f.root)
	_, err := runPlan(t, f, Options{})
	if !errors.Is(err, ErrExportFailed) || calls != 0 {
		t.Fatalf("err=%v applyCalls=%d", err, calls)
	}
	if treeHashExcluding(t, f.home, ".gabitotl") != before || treeHash(t, f.root) != rootBefore {
		t.Fatal("a: nothing may change when the safety backup cannot be made")
	}
}

func TestRunRollbackWhenOpenCodeFailsRevertsClaudeAndLeavesLegacyIntact(t *testing.T) {
	f := newFixture(t)
	withAdapters(f, map[string]func(rt.Adapter) rt.Adapter{
		"opencode": func(a rt.Adapter) rt.Adapter { return wrapAdapter{Adapter: a, failApply: true} },
	})
	claudeMD, ocJSON := slurp(t, f.home+"/.claude/CLAUDE.md"), slurp(t, f.home+"/.config/opencode/opencode.json")
	gentle := treeHash(t, f.home+"/.gentle-ai")
	engramTree := treeHash(t, f.home+"/.engram")
	rootBefore := treeHash(t, f.root)

	res, err := runPlan(t, f, Options{Runtimes: []string{"claude-code", "opencode"}, User: "gabi", UserName: "Gabi", MigratePersona: true})
	if err == nil || !strings.Contains(err.Error(), "injected apply failure") {
		t.Fatalf("err = %v", err)
	}
	if !res.RolledBack {
		t.Fatal("the run must report the rollback")
	}
	if got := slurp(t, f.home+"/.claude/CLAUDE.md"); got != claudeMD {
		t.Fatalf("Claude changes were not reverted:\n%s", got)
	}
	if slurp(t, f.home+"/.config/opencode/opencode.json") != ocJSON {
		t.Fatal("OpenCode config changed")
	}
	if _, err := os.Stat(f.home + "/.gabitotl/state.json"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("ownership state must be reverted to its pre-run absence")
	}
	if treeHash(t, f.home+"/.gentle-ai") != gentle || treeHash(t, f.home+"/.engram") != engramTree {
		t.Fatal("Gentle-AI and Engram must be intact")
	}
	if treeHash(t, f.root) != rootBefore {
		t.Fatal("the .gabitotl folder created by the failed run must be gone")
	}
	if f.runner.Count("gentle-ai") != 0 {
		t.Fatal("legacy must not be retired by a failed run")
	}
	if res.Snapshot.Manifest.Status != "rolled-back" {
		t.Fatalf("status = %s", res.Snapshot.Manifest.Status)
	}
}

func TestRunFailedVerificationRollsBack(t *testing.T) {
	f := newFixture(t)
	withAdapters(f, map[string]func(rt.Adapter) rt.Adapter{
		"opencode": func(a rt.Adapter) rt.Adapter { return wrapAdapter{Adapter: a, failVerify: true} },
	})
	claudeMD := slurp(t, f.home+"/.claude/CLAUDE.md")
	res, err := runPlan(t, f, Options{Runtimes: []string{"claude-code", "opencode"}})
	if !errors.Is(err, ErrVerifyFailed) || !res.RolledBack || slurp(t, f.home+"/.claude/CLAUDE.md") != claudeMD {
		t.Fatalf("err=%v rolledBack=%v", err, res.RolledBack)
	}
}

func TestDryRunChangesNothing(t *testing.T) {
	f := newFixture(t)
	before := treeHash(t, f.home, f.root)
	res, err := runPlan(t, f, Options{DryRun: true, Source: SourceGentleAI, RemoveGentleAI: true, MigratePersona: true})
	if err != nil || !res.DryRun {
		t.Fatalf("%v %+v", err, res)
	}
	if treeHash(t, f.home, f.root) != before {
		t.Fatal("dry-run changed the tree")
	}
	if _, err := os.Stat(f.home + "/.gabitotl"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("dry-run must not even create ~/.gabitotl")
	}
	if f.runner.Count("engram export") != 0 || f.runner.Count("engram save") != 0 || f.runner.Count("gentle-ai") != 0 {
		t.Fatalf("dry-run may only run read-only commands: %v", f.runner.Log())
	}
}

func TestRunIsIdempotent(t *testing.T) {
	f := newFixture(t)
	if _, err := runPlan(t, f, Options{}); err != nil {
		t.Fatal(err)
	}
	after := treeHash(t, f.home, f.root)
	res, err := runPlan(t, f, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Skipped {
		t.Fatalf("a second run must find nothing to do: %+v", res.Phases)
	}
	if treeHash(t, f.home, f.root) != after {
		t.Fatal("re-run changed the tree (no new snapshot, no duplicated config)")
	}
	md := slurp(t, f.home+"/.claude/CLAUDE.md")
	if strings.Count(md, "gabitotl:start persona") != 1 {
		t.Fatalf("duplicated persona section:\n%s", md)
	}
}

func TestRunNeverLeaksSecretsIntoGabitoTLFiles(t *testing.T) {
	f := newFixture(t)
	f.vars["ENGRAM_CLOUD_TOKEN"] = "SECRETVALUE-123"
	f.remote = "https://user:SECRETVALUE-123@github.com/acme/shop.git"
	if _, err := runPlan(t, f, Options{}); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{f.home + "/.gabitotl", f.root + "/.gabitotl"} {
		_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() && !strings.Contains(p, "/.bak/") {
				if b, _ := os.ReadFile(p); strings.Contains(string(b), "SECRETVALUE-123") {
					t.Errorf("f: %s leaks a secret", p)
				}
			}
			return nil
		})
	}
	if logs, _ := filepath.Glob(f.home + "/.gabitotl/logs/*.log"); len(logs) != 1 {
		t.Fatalf("expected one log file, got %v", logs)
	}
}

// cancelAt returns an observer that cancels the run when the given phase starts.
func cancelAt(phase Phase, cancel context.CancelFunc) Observer {
	return func(e Event) {
		if e.Phase == phase && e.Status == "start" {
			cancel()
		}
	}
}

func TestCancellationBetweenPhasesRollsBack(t *testing.T) {
	for _, phase := range []Phase{PhaseMigrate, PhaseConfigure, PhaseVerifyProject} {
		t.Run(string(phase), func(t *testing.T) {
			f := newFixture(t)
			claudeMD := slurp(t, f.home+"/.claude/CLAUDE.md")
			rootBefore := treeHash(t, f.root)
			inv, pl := buildPlan(t, f, Options{Runtimes: []string{"claude-code", "opencode"}, User: "gabi", UserName: "Gabi", MigratePersona: true})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			res, err := Run(ctx, f.env, inv, pl, cancelAt(phase, cancel), Hooks{})
			if !errors.Is(err, context.Canceled) || !res.RolledBack {
				t.Fatalf("err=%v rolledBack=%v", err, res.RolledBack)
			}
			if slurp(t, f.home+"/.claude/CLAUDE.md") != claudeMD || treeHash(t, f.root) != rootBefore {
				t.Fatal("a cancelled run must leave the machine as it was")
			}
			if f.runner.Count("gentle-ai") != 0 {
				t.Fatal("legacy must not be retired by a cancelled run")
			}
		})
	}
}

func TestCancellationDuringRetirementDoesNotKillTheUninstall(t *testing.T) {
	f := newFixture(t)
	inv, pl := buildPlan(t, f, Options{Source: SourceGentleAI, Runtimes: []string{"claude-code", "opencode", "kiro-ide"}, User: "gabi", UserName: "Gabi", RemoveGentleAI: true, MigratePersona: true})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	res, err := Run(ctx, f.env, inv, pl, cancelAt(PhaseRetire, cancel), Hooks{})
	if err != nil || res.RolledBack {
		t.Fatalf("retirement must finish: err=%v rolledBack=%v", err, res.RolledBack)
	}
	if len(res.Retired.Personas) == 0 {
		t.Fatal("the persona block was not retired")
	}
}
