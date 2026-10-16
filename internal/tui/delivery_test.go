package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/installer"
	"github.com/winnstorm/gabitotl/internal/installer/ui"
)

// deliveryMachine is a machine without Engram or Gentle AI, so the first screen
// after choosing "install" is the first question that applies.
func deliveryMachine(t *testing.T, need ui.Need) *driver {
	t.Helper()
	mc := newMachine(t)
	mc.bins["gentle-ai"], mc.bins["engram"] = false, false
	for _, d := range []string{".gentle-ai", ".engram"} {
		if err := os.RemoveAll(filepath.Join(mc.home, d)); err != nil {
			t.Fatal(err)
		}
	}
	cfg := mc.config(installer.Options{})
	cfg.Need = func(installer.Inventory, installer.Options) ui.Need { return need }
	return newDriver(t, cfg)
}

func TestDeliveryQuestionsAreAskedOneAtATimeAndRecorded(t *testing.T) {
	d := deliveryMachine(t, ui.Need{Delivery: true, DeliveryFull: true, Inferred: "github"})
	d.pick(lbl("tui.action.install"))
	d.require("Tu repositorio está en GitHub") // platform, with the inferred one preselected
	d.key("enter")
	d.require(lbl("ask.vcs.auto"))
	if d.has(lbl("ask.vcs.merge")) {
		t.Fatal("one question at a time")
	}
	d.key("up", "enter") // everything automatic
	d.require(lbl("ask.vcs.merge"))
	d.key("down", "enter") // merge commit
	d.require(lbl("ask.vcs.delete"))
	d.key("down", "enter") // keep the branch
	d.require(lbl("ask.vcs.draft"))
	d.key("down", "enter") // draft PRs
	d.require(lbl("tui.runtimes.title"))
	a := d.m.opts.Delivery
	if a.Provider != "github" || a.Automation != gabitotl.AutomationAll || a.MergeMethod != gabitotl.MergeMerge {
		t.Fatalf("%+v", a)
	}
	if a.DeleteBranch == nil || *a.DeleteBranch || a.Draft == nil || !*a.Draft {
		t.Fatalf("%+v", a)
	}
}

func TestDeliveryDefaultsStopAtTheMiddleOption(t *testing.T) {
	d := deliveryMachine(t, ui.Need{Delivery: true, DeliveryFull: true})
	d.pick(lbl("tui.action.install"))
	d.require("¿En qué plataforma vive el repositorio?")
	d.key("enter")
	d.key("enter") // automation: up to the PR
	d.require(lbl("ask.vcs.draft"))
	d.key("enter")
	d.require(lbl("tui.runtimes.title"))
	a := d.m.opts.Delivery
	if a.Provider != "" || a.Automation != gabitotl.AutomationUntilPR || a.MergeMethod != "" || a.Draft == nil || *a.Draft {
		t.Fatalf("%+v", a)
	}
}

func TestDeliveryIsNeverAskedOnceItWasAnswered(t *testing.T) {
	d := deliveryMachine(t, ui.Need{})
	d.pick(lbl("tui.action.install"))
	d.require(lbl("tui.runtimes.title"))
	for _, k := range []string{"ask.vcs.auto", "ask.vcs.platform", "ask.vcs.draft"} {
		if d.has(lbl(k)) {
			t.Fatalf("%s must not be asked again", k)
		}
	}
}

func TestDeliveryScreensNeverMentionPRSizeOrExceptions(t *testing.T) {
	d := deliveryMachine(t, ui.Need{Delivery: true, DeliveryFull: true})
	d.pick(lbl("tui.action.install"))
	seen := d.view()
	d.key("enter")
	seen += d.view()
	d.key("up", "enter")
	seen += d.view()
	low := strings.ToLower(seen)
	for _, w := range []string{"excepci", "tamaño", "presupuesto", "encaden", "apilad"} {
		if strings.Contains(low, w) {
			t.Errorf("the screens mention %q:\n%s", w, seen)
		}
	}
}
