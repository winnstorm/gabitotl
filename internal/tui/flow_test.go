package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/winnstorm/gabitotl/internal/installer"
	"github.com/winnstorm/gabitotl/internal/installer/ui"
)

var msgs = ui.Default()

func lbl(key string) string { return msgs.Format(key) }

func opts() installer.Options { return installer.Options{User: "gabi", UserName: "Gabi"} }

// toPlan walks the migrate path up to the plan screen, keeping the persona block.
func toPlan(t *testing.T, d *driver, persona string) {
	t.Helper()
	d.pick(lbl("ask.action.migrate"))
	d.require(lbl("tui.memory.title"))
	d.pick(lbl("tui.memory.use"))
	d.require(lbl("tui.runtimes.title"))
	d.key("enter") // detected runtimes come preselected
	d.until("persona question", func() bool { return d.m.screen == scrPersona })
	d.pick(lbl(persona))
	d.until("plan", func() bool { return d.m.screen == scrPlan })
}

func TestDetectionScreenShowsTheFourStatesAndTheMenu(t *testing.T) {
	d := newDriver(t, newMachine(t).config(opts()))
	for _, want := range []string{
		"✓", "!", "Claude Code", "OpenCode", "Engram encontrado", "7 recuerdos existentes",
		"Gentle-AI encontrado", lbl("ask.action.migrate"), lbl("ask.action.keep"), lbl("ask.action.inventory"), lbl("ask.action.exit"),
	} {
		d.require(want)
	}
	if !strings.Contains(d.view(), "❯ "+lbl("ask.action.migrate")) {
		t.Fatalf("the first option should hold the cursor:\n%s", d.view())
	}
}

func TestMenuAdaptsToWhatWasDetected(t *testing.T) {
	labels := func(d *driver) []string {
		var out []string
		for _, it := range d.m.items() {
			out = append(out, it.label)
		}
		return out
	}
	with := newDriver(t, newMachine(t).config(opts()))
	if got := labels(with); len(got) != 4 || got[0] != lbl("ask.action.migrate") || got[1] != lbl("ask.action.keep") {
		t.Fatalf("with Gentle-AI: %v", got)
	}

	mc := newMachine(t)
	mc.bins["gentle-ai"] = false
	for _, p := range []string{".gentle-ai"} {
		if err := os.RemoveAll(filepath.Join(mc.home, p)); err != nil {
			t.Fatal(err)
		}
	}
	without := newDriver(t, mc.config(opts()))
	got := labels(without)
	if len(got) != 3 || got[0] != lbl("tui.action.install") {
		t.Fatalf("without Gentle-AI: %v", got)
	}
	for _, l := range got {
		if strings.Contains(l, "Gentle") {
			t.Fatalf("a Gentle-AI option is offered without Gentle-AI: %v", got)
		}
	}
	without.require(lbl("inv.other.none"))
}

func TestReviewDetailsAndBack(t *testing.T) {
	d := newDriver(t, newMachine(t).config(opts()))
	d.send(sizeMsg(100, 50))
	d.pick(lbl("ask.action.inventory"))
	d.require(lbl("tui.details.title"))
	d.require("Componentes de Gentle-AI:")
	d.key("esc")
	d.require(lbl("ask.action.migrate"))
}

func TestCancelBeforeApplyChangesNothing(t *testing.T) {
	for name, quit := range map[string][]string{"q": {"q"}, "ctrl+c": {"ctrl+c"}, "exit item": nil} {
		t.Run(name, func(t *testing.T) {
			mc := newMachine(t)
			before := mc.state()
			d := newDriver(t, mc.config(opts()))
			toPlan(t, d, "tui.persona.keep")
			if quit == nil {
				d.pick(lbl("tui.plan.exit"))
			} else {
				d.key(quit...)
			}
			if !d.m.quitting {
				t.Fatal("the program should be quitting")
			}
			if d.m.Outcome().Ran {
				t.Fatal("nothing must have run")
			}
			if mc.state() != before {
				t.Fatal("cancelling before apply changed the machine")
			}
			if mc.runner.Count("gentle-ai") != 0 || mc.runner.Count("engram export") != 0 {
				t.Fatalf("no mutating command may run: %v", mc.runner.Calls)
			}
		})
	}
}

func TestDryRunAppliesNothing(t *testing.T) {
	mc := newMachine(t)
	before := mc.state()
	o := opts()
	o.DryRun = true
	d := newDriver(t, mc.config(o))
	toPlan(t, d, "tui.persona.do")
	d.require("REMOVE AFTER VERIFY")
	d.pick(lbl("tui.plan.dry"))
	d.until("result", func() bool { return d.m.screen == scrResult })
	out := d.m.Outcome()
	if !out.Ran || !out.Result.DryRun || out.Err != nil {
		t.Fatalf("outcome = %+v", out)
	}
	if mc.state() != before {
		t.Fatal("a dry run changed the machine")
	}
}

func TestPersonaRetirementShowsExactBlockAndAsks(t *testing.T) {
	mc := newMachine(t)
	d := newDriver(t, mc.config(opts()))
	d.pick(lbl("ask.action.migrate"))
	d.pick(lbl("tui.memory.use"))
	d.key("enter")
	d.until("persona question", func() bool { return d.m.screen == scrPersona })
	d.require(lbl("ask.persona.show"))
	d.require("~/.claude/CLAUDE.md")
	d.require(lbl("ask.persona.confirm"))

	d.pick(lbl("tui.persona.keep"))
	d.until("plan", func() bool { return d.m.screen == scrPlan })
	if strings.Contains(d.view(), "REMOVE AFTER VERIFY") || d.m.plan.Options.MigratePersona {
		t.Fatalf("declining must keep the block:\n%s", d.view())
	}
}

func TestFullInstallAsksOnlyWhatCannotBeDetectedOneAtATime(t *testing.T) {
	mc := newMachine(t)
	mc.bins["gentle-ai"] = false
	if err := os.RemoveAll(filepath.Join(mc.home, ".gentle-ai")); err != nil {
		t.Fatal(err)
	}
	cfg := mc.config(installer.Options{})
	cfg.Need = func(installer.Inventory, installer.Options) ui.Need {
		return ui.Need{Name: true, Mode: true, Delivery: true, DeliveryFull: true}
	}
	d := newDriver(t, cfg)
	d.pick(lbl("tui.action.install"))
	d.require(lbl("tui.memory.title")) // Engram exists: reuse or shared first
	d.pick(lbl("tui.memory.use"))      // answers solo/team, so no separate mode question
	d.require(lbl("ask.name"))
	if d.has(lbl("ask.vcs.auto")) || d.has(lbl("tui.runtimes.title")) {
		t.Fatal("questions must come one at a time")
	}
	d.require("Test Dev") // suggested name, prefilled
	d.key("ctrl+a", "ctrl+k")
	d.typeText("Gabi")
	d.key("enter")
	d.require("¿En qué plataforma vive el repositorio?")
	d.key("enter") // default: none yet
	d.require(lbl("ask.vcs.auto"))
	d.key("enter") // default: automatic up to the PR
	d.require(lbl("ask.vcs.draft"))
	d.key("enter") // default: normal PRs
	d.require(lbl("tui.runtimes.title"))
	d.require("Claude Code")
	d.key("enter")
	d.until("persona question", func() bool { return d.m.screen == scrPersona })
	d.pick(lbl("tui.persona.keep"))
	d.until("plan", func() bool { return d.m.screen == scrPlan })
	d.require("CREATE")
	d.pick(lbl("tui.plan.apply"))
	d.until("result", func() bool { return d.m.screen == scrResult })
	out := d.m.Outcome()
	if out.Err != nil || out.Result.RolledBack || len(out.Result.Applied) == 0 {
		t.Fatalf("outcome = %+v", out)
	}
	if _, err := os.Stat(filepath.Join(mc.root, ".gabitotl", "project.md")); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(mc.home, ".claude/CLAUDE.md")); !strings.Contains(string(b), "gabitotl") {
		t.Fatalf("Claude Code was not configured:\n%s", b)
	}
	d.require(lbl("result.done"))
	d.key("enter")
	if !d.m.quitting {
		t.Fatal("enter on the result screen quits")
	}
}

// gate blocks the pipeline when a phase starts so a test can act mid-apply.
type gate struct {
	Backend
	phase   installer.Phase
	reached chan struct{}
	proceed chan struct{}
	once    sync.Once
}

func newGate(b Backend, p installer.Phase) *gate {
	return &gate{Backend: b, phase: p, reached: make(chan struct{}), proceed: make(chan struct{})}
}

func (g *gate) Run(ctx context.Context, inv installer.Inventory, pl installer.Plan, obs installer.Observer, h installer.Hooks) (installer.Result, error) {
	return g.Backend.Run(ctx, inv, pl, func(e installer.Event) {
		if e.Phase == g.phase && e.Status == "start" {
			g.once.Do(func() { close(g.reached); <-g.proceed })
		}
		obs(e)
	}, h)
}

func TestCtrlCDuringApplyRollsBackAndNeverQuitsHalfWay(t *testing.T) {
	mc := newMachine(t)
	before := hash(t, []string{filepath.Join(mc.home, ".gabitotl")}, mc.home, mc.root)
	cfg := mc.config(opts())
	g := newGate(cfg.Backend, installer.PhaseConfigure)
	cfg.Backend = g
	d := newDriver(t, cfg)
	toPlan(t, d, "tui.persona.do")
	d.pick(lbl("tui.plan.apply"))
	<-g.reached
	d.until("apply started", func() bool { return d.m.screen == scrApply })

	d.key("ctrl+c")
	if d.m.quitting {
		t.Fatal("ctrl+c while applying must wait for the rollback, not quit")
	}
	d.require(lbl("tui.apply.cancelling"))
	d.key("q") // ignored while applying
	if d.m.quitting {
		t.Fatal("q must not quit while applying")
	}
	close(g.proceed)
	d.until("result", func() bool { return d.m.screen == scrResult })

	out := d.m.Outcome()
	if !out.Cancelled || !out.Result.RolledBack {
		t.Fatalf("outcome = %+v", out)
	}
	d.require(lbl("tui.apply.cancelled"))
	after := hash(t, []string{filepath.Join(mc.home, ".gabitotl")}, mc.home, mc.root)
	if after != before {
		t.Fatal("the machine is not as it was before the cancelled apply")
	}
	if mc.runner.Count("gentle-ai") != 0 {
		t.Fatal("Gentle-AI must not be touched by a cancelled run")
	}
}

func retireMachine(t *testing.T) *machine {
	mc := newMachine(t)
	// Every agent Gentle-AI manages is one GabitoTL replaces, so its own uninstall is offered.
	write(t, filepath.Join(mc.home, ".gentle-ai/state.json"), fxStateAll)
	return mc
}

func TestRetireQuestionIsAskedOnlyAfterVerification(t *testing.T) {
	for name, tc := range map[string]struct {
		answer string
		runs   int
	}{"retire": {"ask.retire.do", 1}, "keep": {"ask.retire.keep", 0}} {
		t.Run(name, func(t *testing.T) {
			mc := retireMachine(t)
			d := newDriver(t, mc.config(opts()))
			toPlan(t, d, "tui.persona.keep")
			d.pick(lbl("tui.plan.apply"))
			d.until("retire question", func() bool { return d.m.screen == scrRetire })
			if mc.runner.Count("gentle-ai") != 0 {
				t.Fatal("Gentle-AI was touched before the question was answered")
			}
			d.require(lbl("ask.retire.title"))
			d.pick(lbl(tc.answer))
			d.until("result", func() bool { return d.m.screen == scrResult })
			if got := mc.runner.Count("gentle-ai uninstall --all"); got != tc.runs {
				t.Fatalf("gentle-ai uninstall --all ran %d times, want %d", got, tc.runs)
			}
			if out := d.m.Outcome(); out.Err != nil || out.Result.RolledBack {
				t.Fatalf("outcome = %+v", out)
			}
		})
	}
}

func TestCtrlCOnTheRetireQuestionKeepsGentleAI(t *testing.T) {
	mc := retireMachine(t)
	d := newDriver(t, mc.config(opts()))
	toPlan(t, d, "tui.persona.keep")
	d.pick(lbl("tui.plan.apply"))
	d.until("retire question", func() bool { return d.m.screen == scrRetire })
	d.key("ctrl+c")
	if d.m.quitting {
		t.Fatal("must not quit mid-apply")
	}
	d.until("result", func() bool { return d.m.screen == scrResult })
	if mc.runner.Count("gentle-ai") != 0 {
		t.Fatal("Gentle-AI must be kept")
	}
}

func TestNarrowTerminalsStayInsideTheScreen(t *testing.T) {
	d := newDriver(t, newMachine(t).config(opts()))
	for _, sz := range [][2]int{{80, 24}, {40, 14}, {30, 10}, {100, 40}} {
		d.send(sizeMsg(sz[0], sz[1]))
		view := d.view()
		lines := strings.Split(view, "\n")
		if len(lines) > sz[1] {
			t.Fatalf("%dx%d: %d lines:\n%s", sz[0], sz[1], len(lines), view)
		}
		for _, l := range lines {
			if lipgloss.Width(l) > sz[0] {
				t.Fatalf("%dx%d: line wider than the terminal (%d): %q", sz[0], sz[1], lipgloss.Width(l), l)
			}
		}
	}
	d.send(sizeMsg(30, 10))
	d.require("GabitoTL")
	d.send(sizeMsg(20, 5))
	d.require("muy chica")
	d.send(sizeMsg(80, 24)) // growing again brings the screen back
	d.require(lbl("ask.action.migrate"))
}

func TestCursorStaysVisibleInASmallWindow(t *testing.T) {
	d := newDriver(t, newMachine(t).config(opts()))
	d.send(sizeMsg(60, 9))
	d.key("down", "down", "down")
	if !strings.Contains(d.view(), "❯ "+lbl("ask.action.exit")) {
		t.Fatalf("the cursor line scrolled out of view:\n%s", d.view())
	}
}

func TestNoColorEmitsNoEscapeSequences(t *testing.T) {
	mc := newMachine(t)
	plain := newDriver(t, mc.config(opts()))
	if strings.Contains(plain.view(), "\x1b") {
		t.Fatalf("NO_COLOR output has escapes: %q", plain.view())
	}
	for _, sym := range []string{"✓", "!", "❯"} {
		plain.require(sym) // meaning is carried by symbols
	}
	cfg := mc.config(opts())
	cfg.NoColor = false
	colored := newDriver(t, cfg)
	if !strings.Contains(colored.view(), "\x1b[") {
		t.Fatal("colors should be on by default")
	}
	if NoColor(func(k string) string { return map[string]string{"NO_COLOR": "1"}[k] }) != true || NoColor(func(string) string { return "" }) {
		t.Fatal("NoColor must follow the NO_COLOR variable")
	}
}

// killed stands in for a pipeline whose external command dies when the context
// is cancelled: the error does not wrap context.Canceled.
type killed struct{ Backend }

func (killed) Run(ctx context.Context, _ installer.Inventory, _ installer.Plan, obs installer.Observer, _ installer.Hooks) (installer.Result, error) {
	obs(installer.Event{Phase: installer.PhaseSnapshot, Status: "start"})
	<-ctx.Done()
	return installer.Result{}, errors.New("snapshot: engram export failed: signal: killed")
}

func TestCancelWhileTheSnapshotIsRunningSaysCancelledNotDone(t *testing.T) {
	mc := newMachine(t)
	cfg := mc.config(opts())
	cfg.Backend = killed{cfg.Backend}
	d := newDriver(t, cfg)
	toPlan(t, d, "tui.persona.keep")
	d.pick(lbl("tui.plan.apply"))
	d.until("snapshot step", func() bool { return d.has(lbl("phase.snapshot")) })
	d.key("ctrl+c")
	d.until("result", func() bool { return d.m.screen == scrResult })
	d.require(lbl("tui.apply.cancelled"))
	if d.has(lbl("result.done")) || !d.m.Outcome().Cancelled {
		t.Fatalf("a cancelled run must not say it is done:\n%s", d.view())
	}
}

func TestFailedRunBeforeAnythingChangedShowsOnlyTheFailure(t *testing.T) {
	var b strings.Builder
	ui.RenderOutcome(&b, &b, msgs, installer.Result{}, installer.Inventory{}, ui.Paths{}, errors.New("snapshot: boom"))
	if strings.Contains(b.String(), lbl("result.done")) || !strings.Contains(b.String(), "snapshot: boom") {
		t.Fatalf("outcome = %q", b.String())
	}
}

func TestContinuationLinesStayAlignedUnderTheText(t *testing.T) {
	lines, _ := wrapped([]string{"  → una línea larguísima que se tiene que cortar en varias partes para entrar"}, -1, 30)
	if len(lines) < 3 {
		t.Fatalf("expected a wrapped line: %q", lines)
	}
	for _, l := range lines[1:] {
		if !strings.HasPrefix(l, "    ") {
			t.Fatalf("continuation not indented: %q", lines)
		}
	}
}

func TestExistingEngramProjectIsOfferedTheChangeToTeamMemory(t *testing.T) {
	mc := newMachine(t)
	mc.bins["gentle-ai"], mc.bins["engram"] = false, false
	for _, d := range []string{".gentle-ai", ".engram"} {
		if err := os.RemoveAll(filepath.Join(mc.home, d)); err != nil {
			t.Fatal(err)
		}
	}
	cfg := mc.config(installer.Options{})
	cfg.Need = func(installer.Inventory, installer.Options) ui.Need {
		return ui.Need{Mode: true, Current: "engram"}
	}
	d := newDriver(t, cfg)
	d.pick(lbl("tui.action.install"))
	d.require("Este proyecto usa memoria local (Engram).") // the long title wraps
	d.require(lbl("ask.change.no"))
	if d.has(lbl("ask.mode")) {
		t.Fatal("an existing project is asked about the change, not the first choice")
	}
	d.key("down", "enter") // yes: the second option
	d.until("team screen", func() bool { return d.m.screen == scrTeam })
	d.require(lbl("tui.team.title"))
}
