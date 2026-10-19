package persona_test

import (
	"strings"
	"testing"

	"github.com/winnstorm/gabitotl/internal/persona"
)

func TestParseMode(t *testing.T) {
	for in, want := range map[string]persona.Mode{
		"normal": persona.ModeNormal, " LITE ": persona.ModeLite, "Full": persona.ModeFull,
	} {
		if got, err := persona.ParseMode(in); err != nil || got != want {
			t.Errorf("ParseMode(%q) = %q, %v", in, got, err)
		}
	}
	for _, in := range []string{"", "caveman", "ultra", "lite full"} {
		if _, err := persona.ParseMode(in); err == nil {
			t.Errorf("ParseMode(%q) must fail", in)
		}
	}
}

func TestRulesKeepTheGuarantees(t *testing.T) {
	seen := map[string]bool{}
	for _, m := range persona.Modes {
		r := persona.Rules(m)
		for _, must := range []string{"ONLY to what you say in chat", "security warnings", "Quote error messages verbatim", "Qué hice"} {
			if !strings.Contains(r, must) {
				t.Errorf("%s rules miss %q", m, must)
			}
		}
		if seen[r] {
			t.Errorf("%s rules duplicate another mode", m)
		}
		seen[r] = true
	}
	if !strings.Contains(persona.Rules(persona.ModeFull), "telegraphic") || !strings.Contains(persona.Rules(persona.ModeLite), "filler") {
		t.Error("lite/full rules lost their wording")
	}
	if persona.Rules("bogus") != persona.Rules(persona.ModeNormal) {
		t.Error("unknown mode must read as normal")
	}
}
