package gui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	// The wizard is global: a root passed in is ignored, so no project remote.
	if !d.Git.Found || d.Git.Provider != "" || d.Root != "" || d.UserName != "Ada Lovelace" || d.ProjectReady || d.Gentle.Found {
		t.Fatalf("unexpected detection: %+v", d)
	}
	names := map[string]bool{}
	for _, rt := range d.Runtimes {
		names[rt.Name] = rt.Usable
	}
	if !names["claude-code"] || !names["opencode"] || names["kiro-cli"] {
		t.Fatalf("runtimes: %+v", d.Runtimes)
	}
	if len(d.Sections) == 0 || len(d.CodeIndex) != 0 || d.PersonaMode != "normal" || d.Desktop.Supported {
		t.Fatalf("sections/index/mode/desktop: %+v", d)
	}
	for _, sec := range d.Sections {
		for _, row := range sec.Rows {
			if strings.Contains(row, "proyecto") {
				t.Fatalf("the global wizard shows a project row: %q", row)
			}
		}
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

// The snapshot name carries the minute: a plan reviewed at 10:00 and applied
// at 10:02 is still the same plan.
func TestInstallAcceptsAPlanReviewedMinutesBefore(t *testing.T) {
	r := newRig(t)
	a := r.answers()
	ctx := context.Background()
	pl, err := r.svc.PlanInstall(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	r.now = r.now.Add(2 * time.Minute)
	if _, err := r.svc.Install(ctx, a, pl.ID, nil); err != nil {
		t.Fatalf("install after two minutes: %v", err)
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
	if _, err := os.Stat(filepath.Join(r.root, gabitotl.DirName)); err == nil {
		t.Fatal("the wizard is global: it must not create a project")
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
	if g, _ := r.svc.GlobalDelivery(); !g.Saved || g.Automation != "until-pr" {
		t.Fatalf("global delivery after install: %+v", g)
	}
	if steps["delivery-global"] != StatusOK {
		t.Fatalf("steps: %+v", res.Steps)
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
	a.Delivery.Automation = "sometimes"
	if _, err := r.svc.PlanInstall(context.Background(), a); err == nil {
		t.Fatal("an unknown automation level must be refused")
	}
}

func TestWindowsStepsAreUnsupportedOffWindows(t *testing.T) {
	r := newRig(t)
	a := r.answers()
	a.Windows.StartMenu, a.Windows.DesktopShortcut = true, true
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
	if got["desktop"] != StatusUnsupported {
		t.Fatalf("steps %+v", got)
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

func stepStatus(res InstallResult, id string) string {
	for _, s := range res.Steps {
		if s.ID == id {
			return s.Status
		}
	}
	return ""
}

func TestWizardIsGlobalAndIgnoresProjectOnlyAnswers(t *testing.T) {
	for _, root := range []string{"", "project"} {
		r := newRig(t)
		r.withDesktop()
		r.svc.d.Desktop.EmbeddedCLI = []byte("cli")
		in, _ := r.withCodeGraph(false)
		a := r.answers()
		if root == "" {
			a.Root = ""
		}
		// Project-only answers a stale client might still send.
		a.Windows.StartMenu, a.Windows.GuardHooks, a.Windows.CodeIndex = true, true, "codegraph"
		a.Delivery = DeliveryAnswers{Provider: "bitbucket", Automation: "ask", TokenEnv: "MY_BB_TOKEN"}
		pl, err := r.svc.PlanInstall(context.Background(), a)
		if err != nil {
			t.Fatal(err)
		}
		ids := map[string]bool{}
		for _, s := range pl.Steps {
			ids[s.ID] = true
		}
		if !ids["codegraph"] || !ids["delivery-global"] || ids["guard-hooks"] || ids["code-index"] || ids["delivery-auth"] {
			t.Fatalf("root %q: plan steps %+v", root, pl.Steps)
		}
		res := r.install(a)
		if r.hooksOn != nil || len(r.index.setups) != 0 || stepStatus(res, "guard-hooks") != "" || stepStatus(res, "code-index") != "" {
			t.Fatalf("root %q: project-only steps ran: hooks=%v setups=%v steps=%+v", root, r.hooksOn, r.index.setups, res.Steps)
		}
		if stepStatus(res, "codegraph") != StatusOK {
			t.Fatalf("root %q: codegraph step: %+v", root, res.Steps)
		}
		if _, err := os.Stat(in.Launcher()); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(r.root, gabitotl.DirName)); err == nil {
			t.Fatalf("root %q: a project folder was written", root)
		}
		g, err := r.svc.GlobalDelivery()
		if err != nil || !g.Saved || g.Automation != "ask" {
			t.Fatalf("root %q: global delivery %+v %v", root, g, err)
		}
		if b, _ := os.ReadFile(gabitotl.GlobalLayout(r.home).VCS()); strings.Contains(string(b), "MY_BB_TOKEN") || strings.Contains(string(b), "bitbucket") {
			t.Fatalf("the global file must hold settings only:\n%s", b)
		}
	}
}

func TestDetectListsOtherSystemsAlways(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	d, err := r.svc.Detect(ctx, r.root)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Systems) < 7 {
		t.Fatalf("every system is listed even when none is found: %+v", d.Systems)
	}
	for _, s := range d.Systems {
		if s.Found || s.Possible || len(s.Signals) == 0 || s.Name == "" || s.Signals == nil {
			t.Fatalf("nothing is installed in the rig: %+v", s)
		}
	}
	write(t, filepath.Join(r.home, ".claude", "commands", "sc", "a.md"), "x")
	write(t, filepath.Join(r.home, ".claude", "CLAUDE.md"), "<!-- OPENSPEC:START -->\n")
	d, _ = r.svc.Detect(ctx, r.root)
	found := map[string]bool{}
	for _, s := range d.Systems {
		if s.Found {
			found[s.ID] = true
		}
	}
	if !found["superclaude"] || !found["openspec"] || found["gentle-ai"] || d.Gentle.Found {
		t.Fatalf("found: %v", found)
	}
	list, err := r.svc.OtherSystems(ctx)
	if err != nil || len(list) != len(d.Systems) {
		t.Fatalf("manager list: %v %d", err, len(list))
	}
}

func TestProjectDetailListsProjectSystems(t *testing.T) {
	c := newRig(t)
	write(t, filepath.Join(c.root, ".taskmaster", "tasks.json"), "{}")
	d, err := c.svc.Project(context.Background(), c.root)
	if err != nil {
		t.Fatal(err)
	}
	var tm bool
	for _, s := range d.Systems {
		tm = tm || (s.ID == "task-master" && s.Found)
	}
	if !tm || len(d.Systems) < 5 {
		t.Fatalf("%+v", d.Systems)
	}
}
