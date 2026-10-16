package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/winnstorm/gabitotl/internal/installer"
)

var (
	tuiGentleFiles = map[string]string{
		"steering/gentle-ai.md":     "---\ninclusion: always\n---\n\nPersona.\n",
		"skills/branch-pr/SKILL.md": "---\nname: branch-pr\nmetadata:\n  author: gentleman-programming\n---\n\nBody.\n",
		"skills/_shared/README.md":  "shared\n",
		"agents/sdd-apply.md":       "agent\n",
	}
	tuiUserFiles = map[string]string{
		"skills/frontend-design/SKILL.md": "---\nname: frontend-design\n---\n\nMine.\n",
		"steering/product.md":             "# Product\n",
	}
)

// kiroMachine is the fixture machine with Kiro installed and Gentle AI's files in
// its tree next to the user's own.
func kiroMachine(t *testing.T) *machine {
	t.Helper()
	mc := newMachine(t)
	mc.bins["kiro-cli"] = true
	for rel, body := range tuiGentleFiles {
		write(t, filepath.Join(mc.home, ".kiro", filepath.FromSlash(rel)), body)
	}
	for rel, body := range tuiUserFiles {
		write(t, filepath.Join(mc.home, ".kiro", filepath.FromSlash(rel)), body)
	}
	return mc
}

func kiroOpts() installer.Options {
	o := opts()
	o.Runtimes, o.Memory = []string{"kiro-cli"}, "engram"
	return o
}

func toKiroQuestion(t *testing.T, d *driver) {
	t.Helper()
	d.pick(lbl("ask.action.keep")) // install without touching Gentle-AI itself
	d.until("the Kiro question", func() bool { return d.m.screen == scrGentleKiro })
}

func fileIs(t *testing.T, p, body string) bool {
	t.Helper()
	b, err := os.ReadFile(p)
	return err == nil && string(b) == body
}

func TestKiroQuestionShowsTheFactsAndDefaultsToKeepingBoth(t *testing.T) {
	mc := kiroMachine(t)
	d := newDriver(t, mc.config(kiroOpts()))
	toKiroQuestion(t, d)
	d.send(sizeMsg(80, 24))
	view := d.view()
	if lines := strings.Split(view, "\n"); len(lines) > 24 {
		t.Fatalf("80x24: %d lines:\n%s", len(lines), view)
	}
	for _, l := range strings.Split(view, "\n") {
		if lipgloss.Width(l) > 80 {
			t.Fatalf("line wider than 80 columns: %q", l)
		}
	}
	for _, want := range []string{lbl("ask.gentle-kiro.title"), "skills: 1", "agentes: 1", "gabitotl restore", lbl("ask.gentle-kiro.confirm"), lbl("tui.gentle-kiro.keep"), lbl("tui.gentle-kiro.do")} {
		d.require(want)
	}
	if first := d.m.items()[d.m.cur]; first.id != "kiro-keep" {
		t.Fatalf("the cursor must start on the safe choice, got %q", first.id)
	}
}

func TestKiroQuestionYesReplansListsTheMoveAndRestoresInTheResult(t *testing.T) {
	mc := kiroMachine(t)
	d := newDriver(t, mc.config(kiroOpts()))
	before := mc.state()
	toKiroQuestion(t, d)
	if mc.state() != before {
		t.Fatal("asking changes nothing")
	}
	d.pick(lbl("tui.gentle-kiro.do"))
	d.until("the replanned plan", func() bool { return d.m.screen == scrPlan })
	if !d.m.plan.Options.RetireGentleKiro {
		t.Fatal("yes must replan with the option on")
	}
	d.require("REMOVE AFTER VERIFY (4)")
	steering := filepath.Join(mc.home, ".kiro", "steering", "gentle-ai.md")
	listed := false
	for _, it := range d.m.plan.Items {
		listed = listed || it.Target == steering
	}
	if !listed {
		t.Fatalf("the replanned plan must list exactly what moves: %+v", d.m.plan.Items)
	}
	d.pick(lbl("tui.plan.apply"))
	finishApply(d)
	out := d.m.Outcome()
	if out.Err != nil || out.Result.GentleKiro == nil || len(out.Result.GentleKiro.Removed) != len(tuiGentleFiles) {
		t.Fatalf("outcome: %+v err=%v", out.Result.GentleKiro, out.Err)
	}
	d.require("Pasé 4 archivo(s)")
	d.require("gabitotl restore " + out.Result.GentleKiro.Snapshot)
	for rel := range tuiGentleFiles {
		if _, err := os.Stat(filepath.Join(mc.home, ".kiro", filepath.FromSlash(rel))); err == nil {
			t.Errorf("not moved: %s", rel)
		}
	}
	for rel, body := range tuiUserFiles {
		if !fileIs(t, filepath.Join(mc.home, ".kiro", filepath.FromSlash(rel)), body) {
			t.Errorf("the user's file changed: %s", rel)
		}
	}
}

func TestKiroQuestionNoKeepsBothAndStopsNaggingInThePlan(t *testing.T) {
	mc := kiroMachine(t)
	d := newDriver(t, mc.config(kiroOpts()))
	toKiroQuestion(t, d)
	d.pick(lbl("tui.gentle-kiro.keep"))
	d.until("plan", func() bool { return d.m.screen == scrPlan })
	if d.m.plan.Options.RetireGentleKiro || d.m.opts.RetireGentleKiro {
		t.Fatal("no must keep both")
	}
	if d.has("REMOVE AFTER VERIFY") || d.has("--retire-gentle-kiro") {
		t.Fatalf("answered already: no removal and no note about the flag:\n%s", d.view())
	}
	d.pick(lbl("tui.plan.apply"))
	finishApply(d)
	if res := d.m.Outcome().Result; res.GentleKiro != nil {
		t.Fatalf("nothing may be retired: %+v", res.GentleKiro)
	}
	for rel, body := range tuiGentleFiles {
		if !fileIs(t, filepath.Join(mc.home, ".kiro", filepath.FromSlash(rel)), body) {
			t.Errorf("Gentle AI's file must stay: %s", rel)
		}
	}
}

func TestQuittingOnTheKiroQuestionChangesNothing(t *testing.T) {
	for name, quit := range map[string]string{"q": "q", "ctrl+c": "ctrl+c"} {
		t.Run(name, func(t *testing.T) {
			mc := kiroMachine(t)
			before := mc.state()
			d := newDriver(t, mc.config(kiroOpts()))
			toKiroQuestion(t, d)
			d.key(quit)
			if !d.m.quitting || d.m.Outcome().Ran {
				t.Fatal("the program should quit without running")
			}
			if mc.state() != before {
				t.Fatal("quitting at the question changed the machine")
			}
		})
	}
}

func TestEscOnTheKiroQuestionGoesHomeAndAsksAgain(t *testing.T) {
	mc := kiroMachine(t)
	d := newDriver(t, mc.config(kiroOpts()))
	toKiroQuestion(t, d)
	d.key("esc")
	d.require(lbl("ask.action.keep"))
	toKiroQuestion(t, d) // the earlier attempt left no answer behind
}

func TestKiroQuestionIsSkippedWhenTheFlagAnsweredIt(t *testing.T) {
	mc := kiroMachine(t)
	o := kiroOpts()
	o.RetireGentleKiro = true
	d := newDriver(t, mc.config(o))
	d.pick(lbl("ask.action.keep"))
	d.until("plan", func() bool { return d.m.screen == scrPlan || d.m.screen == scrGentleKiro })
	if d.m.screen != scrPlan || !d.m.plan.Options.RetireGentleKiro {
		t.Fatalf("the flag already retires: no question (screen %v)", d.m.screen)
	}
}

func TestKiroQuestionIsSkippedWhenThereIsNothingFromGentleAI(t *testing.T) {
	mc := newMachine(t)
	mc.bins["kiro-cli"] = true
	d := newDriver(t, mc.config(kiroOpts()))
	d.pick(lbl("ask.action.keep"))
	d.until("plan", func() bool { return d.m.screen == scrPlan || d.m.screen == scrGentleKiro })
	if d.m.screen != scrPlan {
		t.Fatalf("nothing to ask about (screen %v)", d.m.screen)
	}
}
