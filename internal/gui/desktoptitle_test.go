package gui

import (
	"testing"

	"github.com/winnstorm/gabitotl/internal/desktop"
	"github.com/winnstorm/gabitotl/internal/installer/ui"
	"github.com/winnstorm/gabitotl/internal/persona"
)

func TestDesktopTitleIsLocalized(t *testing.T) {
	es, en := ui.For(persona.LangES), ui.For(persona.LangEN)
	copyStep := desktop.Step{ID: desktop.StepCopy, Title: "Copy the app to C:\\X", Dir: `C:\X`}
	if got := desktopTitle(es, copyStep); got != `Copio la app a C:\X` {
		t.Fatalf("es copy: %q", got)
	}
	if got := desktopTitle(en, copyStep); got != `Copy the app to C:\X` {
		t.Fatalf("en copy: %q", got)
	}
	if got, want := desktopTitle(es, desktop.Step{ID: desktop.StepClose, Count: 3}), es.Format("gui.step.close-copies.many", "count", "3"); got != want {
		t.Fatalf("es close: %q, want %q", got, want)
	}
	// Every step ID the desktop package emits has an entry in both catalogues.
	for _, id := range []string{desktop.StepCopy, desktop.StepPath, desktop.StepStartMenu, desktop.StepDesktop,
		desktop.StepRegister, desktop.StepUnlink, desktop.StepUnpath, desktop.StepUnreg, desktop.StepRemove} {
		for name, m := range map[string]ui.Messages{"es": es, "en": en} {
			if _, ok := m["gui.desktop.step."+id]; !ok {
				t.Errorf("%s catalogue has no gui.desktop.step.%s", name, id)
			}
		}
	}
	if got := desktopTitle(es, desktop.Step{ID: "unknown", Title: "Fallback"}); got != "Fallback" {
		t.Fatalf("fallback: %q", got)
	}
}
