package gui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
)

func TestMessagesAndLanguage(t *testing.T) {
	r := newRig(t)
	es, err := r.svc.Messages("es")
	if err != nil {
		t.Fatal(err)
	}
	en, _ := r.svc.Messages("en")
	if es["result.done"] != "Listo." || en["result.done"] == es["result.done"] || len(en) != len(es) {
		t.Fatalf("catalogues look wrong: %q vs %q (%d/%d keys)", es["result.done"], en["result.done"], len(es), len(en))
	}
	if _, err := r.svc.Messages("fr"); err == nil {
		t.Fatal("an unknown language must be refused")
	}
	if lang, _ := r.svc.Language(); lang != "es" {
		t.Fatalf("default language %q, want es", lang)
	}
	if err := r.svc.SetLanguage("en"); err != nil {
		t.Fatal(err)
	}
	if lang, _ := r.svc.Language(); lang != "en" {
		t.Fatalf("saved language %q, want en", lang)
	}
	if r.svc.SetLanguage("xx") == nil {
		t.Fatal("an unknown language must be refused")
	}
}

func TestDetectBuildsTheInventory(t *testing.T) {
	r := newRig(t)
	d, err := r.svc.Detect(context.Background(), r.root)
	if err != nil {
		t.Fatal(err)
	}
	if !d.Git.Found || d.Git.Provider != "github" || d.UserName != "Ada Lovelace" || d.ProjectReady || d.Gentle.Found {
		t.Fatalf("unexpected detection: %+v", d)
	}
	names := map[string]bool{}
	for _, rt := range d.Runtimes {
		names[rt.Name] = rt.Usable
	}
	if !names["claude-code"] || !names["opencode"] || names["kiro-cli"] {
		t.Fatalf("runtimes: %+v", d.Runtimes)
	}
	if len(d.Sections) == 0 || len(d.CodeIndex) != 2 || d.PersonaMode != "normal" || d.Desktop.Supported {
		t.Fatalf("sections/index/mode/desktop: %+v", d)
	}
}

func TestDetectFindsGentleAI(t *testing.T) {
	r := newRig(t)
	write(t, filepath.Join(r.home, ".gentle-ai", "state.json"), `{"version":"4.2.0","installed_agents":["claude-code"],"components":["persona"]}`)
	d, err := r.svc.Detect(context.Background(), r.root)
	if err != nil {
		t.Fatal(err)
	}
	if !d.Gentle.Found || d.Gentle.Action != ActionReview || d.Gentle.Version != "4.2.0" {
		t.Fatalf("gentle: %+v", d.Gentle)
	}
	a := r.answers()
	a.GentleAction = ActionReview
	if _, err := r.svc.PlanInstall(context.Background(), a); !errors.Is(err, ErrDecisionPending) {
		t.Fatalf("review must stay a pending decision, got %v", err)
	}
}

func TestInstallIsPlanFirstAndApplies(t *testing.T) {
	r := newRig(t)
	a := r.answers()
	ctx := context.Background()
	pl, err := r.svc.PlanInstall(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	if !pl.Mutates || len(pl.Groups) == 0 || pl.ID == "" {
		t.Fatalf("plan: %+v", pl)
	}
	if _, err := os.Stat(filepath.Join(r.root, gabitotl.DirName)); err == nil {
		t.Fatal("planning must write nothing")
	}
	if _, err := r.svc.Install(ctx, a, "", nil); !errors.Is(err, ErrPlanRequired) {
		t.Fatalf("no plan id: %v", err)
	}
	if _, err := r.svc.Install(ctx, a, "stale", nil); !errors.Is(err, ErrPlanChanged) {
		t.Fatalf("wrong plan id: %v", err)
	}
	var events []Event
	res, err := r.svc.Install(ctx, a, pl.ID, func(e Event) { events = append(events, e) })
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Applied) != 1 || res.Applied[0] != "claude-code" || res.Snapshot == "" || res.RolledBack {
		t.Fatalf("result: %+v", res)
	}
	if _, err := os.Stat(res.Snapshot); err != nil {
		t.Fatalf("the backup directory must exist: %v", err)
	}
	if _, err := os.Stat(gabitotl.NewLayout(r.root).Project()); err != nil {
		t.Fatalf("project.md: %v", err)
	}
	if lang, _ := r.svc.Language(); lang != "en" {
		t.Fatalf("the wizard language must be remembered, got %q", lang)
	}
	steps := map[string]string{}
	for _, s := range res.Steps {
		steps[s.ID] = s.Status
	}
	if steps["persona-mode"] != StatusOK {
		t.Fatalf("steps: %+v", res.Steps)
	}
	if set, _ := r.svc.Settings(ctx, r.root); set.PersonaMode != "lite" || set.Delivery.Automation != "until-pr" || set.Memory.Provider != "engram" {
		t.Fatalf("settings after install: %+v", set)
	}
	last := -1
	for _, e := range events {
		if e.Percent < last {
			t.Fatalf("progress went backwards: %+v", events)
		}
		last = e.Percent
	}
	if len(events) == 0 || events[len(events)-1].Percent != 100 {
		t.Fatalf("progress must end at 100: %+v", events)
	}
}

func TestInstallDryRunChangesNothing(t *testing.T) {
	r := newRig(t)
	a := r.answers()
	a.DryRun = true
	res := r.install(a)
	if !res.DryRun || len(res.Steps) != 0 {
		t.Fatalf("dry run: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(r.root, gabitotl.DirName)); err == nil {
		t.Fatal("a dry run must not create the project")
	}
}

func TestInstallRefusesWhatHasNoBackingCode(t *testing.T) {
	r := newRig(t)
	a := r.answers()
	a.Scope = "project"
	if _, err := r.svc.PlanInstall(context.Background(), a); !errors.Is(err, ErrNotAvailable) {
		t.Fatalf("project scope in the wizard: %v", err)
	}
	a = r.answers()
	a.Memory = MemoryAnswers{Mode: "other"}
	if _, err := r.svc.PlanInstall(context.Background(), a); err == nil {
		t.Fatal("an unknown memory mode must be refused")
	}
	a = r.answers()
	a.Delivery.TokenEnv = "not valid!"
	if _, err := r.svc.PlanInstall(context.Background(), a); err == nil {
		t.Fatal("a bad token variable name must be refused")
	}
	a = r.answers()
	a.Windows.CodeIndex = "nope"
	if _, err := r.svc.PlanInstall(context.Background(), a); err == nil {
		t.Fatal("an unknown code index must be refused")
	}
}

func TestWindowsStepsAreUnsupportedOffWindows(t *testing.T) {
	r := newRig(t)
	a := r.answers()
	a.Windows.StartMenu, a.Windows.DesktopShortcut, a.Windows.CodeIndex = true, true, "codegraph"
	pl, err := r.svc.PlanInstall(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	if len(pl.Unsupports) != 1 || pl.Unsupports[0] != "desktop" {
		t.Fatalf("the plan must say the Windows step is skipped: %+v", pl)
	}
	res, err := r.svc.Install(context.Background(), a, pl.ID, nil)
	if err != nil {
		t.Fatalf("an unsupported Windows step must not fail the install: %v", err)
	}
	got := map[string]string{}
	for _, s := range res.Steps {
		got[s.ID] = s.Status
	}
	if got["desktop"] != StatusUnsupported || got["code-index"] != StatusOK || len(r.index.setups) != 1 || r.index.setups[0] != "codegraph" {
		t.Fatalf("steps %+v, index setups %v", got, r.index.setups)
	}
}

func TestWindowsStepsRunThroughTheDesktopPort(t *testing.T) {
	r := newRig(t)
	m := r.withDesktop()
	a := r.answers()
	a.Windows.StartMenu, a.Windows.AddToPath, a.Windows.DesktopShortcut = true, true, true
	pl, err := r.svc.PlanInstall(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	if len(pl.Unsupports) != 0 {
		t.Fatalf("supported: %+v", pl)
	}
	var seen []string
	res, err := r.svc.Install(context.Background(), a, pl.ID, func(e Event) { seen = append(seen, e.Step) })
	if err != nil {
		t.Fatal(err)
	}
	if m.reg == nil || m.path == "" || len(m.links) != 2 {
		t.Fatalf("the desktop port must have been applied: reg=%v path=%q links=%v", m.reg, m.path, m.links)
	}
	if st := r.svc.DesktopStatus(); !st.Supported || !st.Installed || !st.OnPath {
		t.Fatalf("status: %+v", st)
	}
	var desk string
	for _, s := range res.Steps {
		if s.ID == "desktop" {
			desk = s.Status
		}
	}
	if desk != StatusOK {
		t.Fatalf("desktop step: %+v", res.Steps)
	}
}

func TestUnsafeInstallDirFailsThePlan(t *testing.T) {
	r := newRig(t)
	r.withDesktop()
	a := r.answers()
	a.Windows.StartMenu, a.Windows.InstallDir = true, filepath.Dir(r.svc.d.Desktop.Env.Protected[0])
	if _, err := r.svc.PlanInstall(context.Background(), a); err == nil {
		t.Fatal("an install directory that contains a protected folder must be refused at plan time")
	}
}

func TestCheckSignIn(t *testing.T) {
	r := newRig(t)
	si, err := r.svc.CheckSignIn(context.Background(), r.root)
	if err != nil || !si.OK || si.Status != "logged-in" {
		t.Fatalf("%+v %v", si, err)
	}
	r.svc.d.VCSAuth = nil
	if _, err := r.svc.CheckSignIn(context.Background(), r.root); !errors.Is(err, ErrNotAvailable) {
		t.Fatalf("without the port: %v", err)
	}
}

func TestBitbucketTokenReferenceIsSavedNeverTheToken(t *testing.T) {
	r := newRig(t)
	a := r.answers()
	a.Delivery = DeliveryAnswers{Provider: "bitbucket", Automation: "ask", TokenEnv: "MY_BB_TOKEN"}
	r.install(a)
	set, err := r.svc.Settings(context.Background(), r.root)
	if err != nil || set.Delivery.Auth != "env:MY_BB_TOKEN" || set.Delivery.Provider != "bitbucket" {
		t.Fatalf("%+v %v", set.Delivery, err)
	}
}
