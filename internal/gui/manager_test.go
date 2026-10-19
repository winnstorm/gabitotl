package gui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
)

func findLine(rep DoctorReport, code string) (DoctorLine, bool) {
	for _, s := range rep.Sections {
		for _, l := range s.Lines {
			if l.Code == code {
				return l, true
			}
		}
	}
	return DoctorLine{}, false
}

func TestDoctorFixConnectsAnUnconfiguredRuntime(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	rep, err := r.svc.Doctor(ctx, r.root)
	if err != nil {
		t.Fatal(err)
	}
	line, ok := findLine(rep, "doctor.runtime.todo")
	if !ok || line.Status != "todo" || line.Fix == nil || line.Fix.ID != FixRuntimeConnect || line.Message == "" || line.Message == line.Code {
		t.Fatalf("a detected, unconfigured runtime needs a connect fix: %+v", line)
	}
	req := FixRequest{Root: r.root, ID: line.Fix.ID, Args: line.Fix.Args}
	if _, err := r.svc.RunFix(ctx, req, "", nil); !errors.Is(err, ErrPlanRequired) {
		t.Fatalf("a mutating fix needs its plan: %v", err)
	}
	pl, err := r.svc.PlanFix(ctx, req)
	if err != nil || !pl.Mutates {
		t.Fatalf("%+v %v", pl, err)
	}
	if _, err := r.svc.RunFix(ctx, req, pl.ID, nil); err != nil {
		t.Fatal(err)
	}
	rep, _ = r.svc.Doctor(ctx, r.root)
	if l, _ := findLine(rep, "doctor.runtime.ok"); l.Status != "ok" {
		t.Fatalf("after the fix the runtime must be ok: %+v", rep.Sections)
	}
	if _, err := os.Stat(filepath.Join(r.home, ".gabitotl", "backups")); err != nil {
		t.Fatalf("applying must back up first: %v", err)
	}
}

func TestDoctorSpeaksTheActiveLanguage(t *testing.T) {
	r := newRig(t)
	_ = r.svc.SetLanguage("en")
	rep, _ := r.svc.Doctor(context.Background(), r.root)
	if rep.Sections[1].Title != "Project" {
		t.Fatalf("english section title expected, got %q", rep.Sections[1].Title)
	}
	_ = r.svc.SetLanguage("es")
	rep, _ = r.svc.Doctor(context.Background(), r.root)
	if rep.Sections[1].Title != "Proyecto" || rep.Summary == "" {
		t.Fatalf("spanish expected: %+v", rep)
	}
}

func TestReadOnlyFixesRunThroughThePorts(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	res, err := r.svc.RunFix(ctx, FixRequest{Root: r.root, ID: FixVCSLogin}, "", nil)
	if err != nil || res.SignIn == nil || !res.OK {
		t.Fatalf("%+v %v", res, err)
	}
	res, err = r.svc.RunFix(ctx, FixRequest{Root: r.root, ID: FixGuardHistory}, "", nil)
	if err != nil || res.Guard == nil || len(res.Guard.History) != 1 {
		t.Fatalf("%+v %v", res, err)
	}
	pl, _ := r.svc.PlanFix(ctx, FixRequest{Root: r.root, ID: FixGuardHooks})
	if _, err := r.svc.RunFix(ctx, FixRequest{Root: r.root, ID: FixGuardHooks}, pl.ID, nil); err != nil || r.hooksOn == nil || !*r.hooksOn {
		t.Fatalf("hooks: %v %v", err, r.hooksOn)
	}
	if _, err := r.svc.RunFix(ctx, FixRequest{ID: "nope"}, "", nil); err == nil {
		t.Fatal("an unknown fix must be refused")
	}
}

func TestDesktopFixAppearsOnlyWhereSupported(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	if rep, _ := r.svc.Doctor(ctx, r.root); len(rep.Sections) > 0 && rep.Sections[len(rep.Sections)-1].ID == "desktop" {
		t.Fatal("no desktop section off Windows")
	}
	if _, err := r.svc.RunFix(ctx, FixRequest{ID: FixDesktop}, "", nil); !errors.Is(err, ErrUnsupportedOS) {
		t.Fatalf("typed unsupported result expected, got %v", err)
	}
	m := r.withDesktop()
	rep, _ := r.svc.Doctor(ctx, r.root)
	last := rep.Sections[len(rep.Sections)-1]
	if last.ID != "desktop" || last.Lines[0].Fix == nil || last.Lines[0].Status != "todo" {
		t.Fatalf("the missing app integration needs a repair fix: %+v", last)
	}
	req := FixRequest{Root: r.root, ID: FixDesktop}
	pl, err := r.svc.PlanFix(ctx, req)
	if err != nil || !pl.Mutates || len(pl.Steps) == 0 {
		t.Fatalf("%+v %v", pl, err)
	}
	if _, err := r.svc.RunFix(ctx, req, pl.ID, nil); err != nil || m.reg == nil {
		t.Fatalf("%v %v", err, m.reg)
	}
}

func TestIntegrationsListApplyAndRemove(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	list, err := r.svc.Integrations(ctx, r.root)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Integration{}
	for _, it := range list {
		got[it.Name] = it
	}
	if !got["claude-code"].Usable || got["claude-code"].Managed || got["kiro-cli"].Usable {
		t.Fatalf("%+v", list)
	}
	req := IntegrationRequest{Root: r.root, Runtime: "opencode", Op: OpApply}
	pl, err := r.svc.PlanIntegration(ctx, req)
	if err != nil || !pl.Mutates {
		t.Fatalf("%+v %v", pl, err)
	}
	if _, err := r.svc.ApplyIntegration(ctx, req, "", nil); !errors.Is(err, ErrPlanRequired) {
		t.Fatalf("%v", err)
	}
	res, err := r.svc.ApplyIntegration(ctx, req, pl.ID, nil)
	if err != nil || len(res.Applied) == 0 || res.Backup == "" {
		t.Fatalf("%+v %v", res, err)
	}
	list, _ = r.svc.Integrations(ctx, r.root)
	for _, it := range list {
		if it.Name == "opencode" && (!it.Managed || !it.Healthy) {
			t.Fatalf("opencode must be managed and healthy: %+v", it)
		}
	}
	rm := IntegrationRequest{Root: r.root, Runtime: "opencode", Op: OpRemove}
	rp, err := r.svc.PlanIntegration(ctx, rm)
	if err != nil || !rp.Mutates || len(rp.Groups) == 0 {
		t.Fatalf("%+v %v", rp, err)
	}
	if _, err := r.svc.ApplyIntegration(ctx, rm, rp.ID, nil); err != nil {
		t.Fatal(err)
	}
	list, _ = r.svc.Integrations(ctx, r.root)
	for _, it := range list {
		if it.Name == "opencode" && it.Managed {
			t.Fatalf("opencode must be removed: %+v", it)
		}
	}
	if _, err := r.svc.PlanIntegration(ctx, IntegrationRequest{Root: r.root, Runtime: "nope"}); err == nil {
		t.Fatal("unknown runtime")
	}
	if _, err := r.svc.PlanIntegration(ctx, IntegrationRequest{Root: t.TempDir(), Runtime: "opencode", Scope: "project"}); err == nil {
		t.Fatal("project scope needs a GabitoTL project")
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	r.install(r.answers())
	if _, err := r.svc.SetDelivery(r.root, DeliveryAnswers{Automation: "all", MergeMethod: "rebase"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.svc.SetDelivery(r.root, DeliveryAnswers{MergeMethod: "nope"}); err == nil {
		t.Fatal("invalid merge method")
	}
	res, err := r.svc.SetPersona(r.root, "es", "full")
	if err != nil || len(res.Files) != 2 || len(res.Notes) != 1 {
		t.Fatalf("%+v %v", res, err)
	}
	if _, err := r.svc.SetPersona(r.root, "", "loud"); err == nil {
		t.Fatal("invalid mode")
	}
	set, err := r.svc.Settings(ctx, r.root)
	if err != nil {
		t.Fatal(err)
	}
	if set.Delivery.Automation != "all" || set.Delivery.MergeMethod != "rebase" || set.Language != "es" || set.PersonaMode != "full" {
		t.Fatalf("%+v", set)
	}
	if set.Guard.Mode == "" || len(set.CodeIndex) != 2 {
		t.Fatalf("%+v", set)
	}
	if err := r.svc.SetCodeIndex(ctx, r.root, "codegraph"); err != nil || len(r.index.setups) != 1 {
		t.Fatalf("%v %v", err, r.index.setups)
	}
	if r.svc.SetCodeIndex(ctx, r.root, "x") == nil {
		t.Fatal("unknown tool")
	}
	if err := r.svc.SetGuardHooks(ctx, r.root, false); err != nil || r.hooksOn == nil || *r.hooksOn {
		t.Fatalf("%v %v", err, r.hooksOn)
	}
	if _, err := r.svc.SetDelivery(t.TempDir(), DeliveryAnswers{Automation: "all"}); err == nil {
		t.Fatal("no project")
	}
}

func TestMemorySettingsGoThroughThePlan(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	r.install(r.answers())
	team := MemoryAnswers{Mode: "team", Role: "client", URL: "https://memoria.example.com/mcp"}
	pl, err := r.svc.PlanMemory(ctx, r.root, team)
	if err != nil || !pl.Mutates {
		t.Fatalf("%+v %v", pl, err)
	}
	if _, err := r.svc.ApplyMemory(ctx, r.root, team, "", nil); !errors.Is(err, ErrPlanRequired) {
		t.Fatalf("%v", err)
	}
	res, err := r.svc.ApplyMemory(ctx, r.root, team, pl.ID, nil)
	if err != nil || res.Snapshot == "" {
		t.Fatalf("%+v %v", res, err)
	}
	set, _ := r.svc.Settings(ctx, r.root)
	if set.Memory.Provider != "memory-mcp" || set.Memory.Role != "client" || set.Memory.URL != team.URL {
		t.Fatalf("%+v", set.Memory)
	}
	// Going back to solo memory is refused by the installer, not re-implemented here.
	if _, err := r.svc.PlanMemory(ctx, r.root, MemoryAnswers{Mode: "solo"}); err == nil {
		t.Fatal("team to solo has no migrator and must be refused")
	}
	if _, err := r.svc.PlanMemory(ctx, r.root, MemoryAnswers{}); err == nil {
		t.Fatal("a mode is required")
	}
}

func TestBackupsRestoreAndCleanup(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	claude := filepath.Join(r.home, ".claude", "CLAUDE.md")
	before, _ := os.ReadFile(claude)
	res := r.install(r.answers())
	snaps, err := r.svc.Snapshots(r.root)
	if err != nil || len(snaps) != 1 || snaps[0].Status != "complete" || snaps[0].Dir != res.Snapshot {
		t.Fatalf("%+v %v", snaps, err)
	}
	pl, err := r.svc.PlanRestore(r.root, snaps[0].ID)
	if err != nil || !pl.Mutates || len(pl.Groups) == 0 {
		t.Fatalf("%+v %v", pl, err)
	}
	if _, err := r.svc.Restore(r.root, snaps[0].ID, "", nil); !errors.Is(err, ErrPlanRequired) {
		t.Fatalf("%v", err)
	}
	rr, err := r.svc.Restore(r.root, snaps[0].ID, pl.ID, nil)
	if err != nil || len(rr.Changed) == 0 || rr.Backup == "" {
		t.Fatalf("%+v %v", rr, err)
	}
	after, _ := os.ReadFile(claude)
	if string(after) != string(before) {
		t.Fatalf("restore must put the original back:\n%s", after)
	}
	if _, err := r.svc.PlanRestore(r.root, "../escape"); err == nil {
		t.Fatal("a path-like snapshot id must be refused")
	}
	// Nothing is old enough to clean yet.
	cp, err := r.svc.PlanCleanup(ctx, r.root, 0)
	if err != nil || cp.Mutates {
		t.Fatalf("%+v %v", cp, err)
	}
	r.now = r.now.AddDate(0, 0, 60)
	cp, _ = r.svc.PlanCleanup(ctx, r.root, 30)
	if !cp.Mutates {
		t.Fatalf("aged snapshots must be listed: %+v", cp)
	}
	if _, err := r.svc.ApplyCleanup(ctx, r.root, 30, ""); !errors.Is(err, ErrPlanRequired) {
		t.Fatalf("%v", err)
	}
	removed, err := r.svc.ApplyCleanup(ctx, r.root, 30, cp.ID)
	if err != nil || len(removed) == 0 {
		t.Fatalf("%v %v", removed, err)
	}
}

func TestUninstallPlanAndRun(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	r.install(r.answers())
	m := r.withDesktop()
	o := UninstallOptions{Root: r.root, Desktop: true}
	pl, err := r.svc.PlanUninstall(ctx, o)
	if err != nil || !pl.Mutates || len(pl.Runtimes) != 1 || len(pl.Groups) == 0 || len(pl.Steps) == 0 || len(pl.Notes) == 0 {
		t.Fatalf("%+v %v", pl, err)
	}
	if _, err := r.svc.Uninstall(ctx, o, "", nil); !errors.Is(err, ErrPlanRequired) {
		t.Fatalf("%v", err)
	}
	res, err := r.svc.Uninstall(ctx, o, pl.ID, nil)
	if err != nil || res.Backup == "" || len(res.Removed) == 0 {
		t.Fatalf("%+v %v", res, err)
	}
	if len(res.Steps) != 1 || res.Steps[0].ID != "desktop" || res.Steps[0].Status != StatusOK || len(m.calls) == 0 {
		t.Fatalf("desktop step: %+v %v", res.Steps, m.calls)
	}
	if _, err := os.Stat(gabitotl.NewLayout(r.root).Project()); err != nil {
		t.Fatal("uninstall must keep the project's .gabitotl")
	}
	again, _ := r.svc.PlanUninstall(ctx, UninstallOptions{Root: r.root})
	if again.Mutates {
		t.Fatalf("nothing left to remove: %+v", again)
	}
}

func TestUninstallDesktopIsUnsupportedOffWindows(t *testing.T) {
	r := newRig(t)
	pl, err := r.svc.PlanUninstall(context.Background(), UninstallOptions{Root: r.root, Desktop: true})
	if err != nil || len(pl.Unsupports) != 1 {
		t.Fatalf("%+v %v", pl, err)
	}
}
