package installer

import (
	"strings"
	"testing"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/persona"
)

func TestPersonaLangInstallPersistsAndKeepsChoice(t *testing.T) {
	f := newFixture(t)
	claude := f.home + "/.claude/CLAUDE.md"

	// Default: Spanish, and nothing saved until a run succeeds.
	if _, err := runPlan(t, f, Options{User: "gabi", UserName: "Gabi"}); err != nil {
		t.Fatal(err)
	}
	if md := slurp(t, claude); !strings.Contains(md, "Argentine Spanish") || strings.Contains(md, "**What I did:**") {
		t.Fatalf("default persona must be Spanish:\n%s", md)
	}
	if l, ok := gabitotl.LoadPersonaLang(f.home); !ok || l != persona.LangES {
		t.Fatalf("saved = %q %v", l, ok)
	}

	// Explicit English replaces the owned persona section and is saved.
	if _, err := runPlan(t, f, Options{User: "gabi", UserName: "Gabi", PersonaLang: "en"}); err != nil {
		t.Fatal(err)
	}
	md := slurp(t, claude)
	if !strings.Contains(md, "**What I did:**") || !strings.Contains(md, "Reply in natural English") || strings.Contains(md, "Argentine Spanish") {
		t.Fatalf("English persona expected, stale Spanish copy found:\n%s", md)
	}
	if strings.Count(md, "gabitotl:start persona") != 1 {
		t.Fatalf("persona section must be replaced, not duplicated:\n%s", md)
	}
	if l, ok := gabitotl.LoadPersonaLang(f.home); !ok || l != persona.LangEN {
		t.Fatalf("saved = %q %v", l, ok)
	}

	// A reinstall without the flag keeps English.
	if _, err := runPlan(t, f, Options{User: "gabi", UserName: "Gabi"}); err != nil {
		t.Fatal(err)
	}
	if md := slurp(t, claude); !strings.Contains(md, "**What I did:**") {
		t.Fatalf("reinstall must keep the saved language:\n%s", md)
	}

	// Doctor reports the language and no drift.
	rep := Doctor(t.Context(), f.env)
	found := false
	for _, s := range rep.Sections {
		for _, l := range s.Lines {
			if l.Code == DocRuntimePersonaLang && l.Detail == "en" {
				found = true
			}
			if l.Code == DocRuntimeDrift {
				t.Fatalf("unexpected drift: %+v", l)
			}
		}
	}
	if !found {
		t.Fatal("doctor must show the persona language")
	}
}

func TestPersonaLangRejectsUnknown(t *testing.T) {
	if _, err := ResolvePersonaLang(t.TempDir(), "fr"); err == nil {
		t.Fatal("an unknown language must be rejected")
	}
	if l, err := ResolvePersonaLang(t.TempDir(), ""); err != nil || l != persona.LangES {
		t.Fatalf("default = %q %v", l, err)
	}
}
