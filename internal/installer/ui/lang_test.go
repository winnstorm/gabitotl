package ui

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/installer"
	"github.com/winnstorm/gabitotl/internal/persona"
)

var placeholderRe = regexp.MustCompile(`\{[a-z_]+\}`)

func placeholders(s string) []string {
	p := placeholderRe.FindAllString(s, -1)
	slices.Sort(p)
	return p
}

func TestEnglishCatalogueHasTheSameKeysAndPlaceholdersAsSpanish(t *testing.T) {
	es, en := For(persona.LangES), For(persona.LangEN)
	for k, v := range es {
		w, ok := en[k]
		if !ok || w == "" {
			t.Errorf("messages_en.txt is missing %s", k)
			continue
		}
		if !slices.Equal(placeholders(v), placeholders(w)) {
			t.Errorf("%s: placeholders differ: es %v, en %v", k, placeholders(v), placeholders(w))
		}
	}
	for k := range en {
		if _, ok := es[k]; !ok {
			t.Errorf("messages_en.txt has %s, which es-AR does not", k)
		}
	}
}

func TestForUnknownLanguageFallsBackToSpanish(t *testing.T) {
	if For("fr").Format("result.done") != "Listo." || Default().Format("result.done") != "Listo." {
		t.Fatal("es-AR must stay the default")
	}
	if got := For(persona.LangEN).Format("result.done"); got != "Done." {
		t.Fatalf("en = %q", got)
	}
}

func TestActiveFollowsTheSavedPersonaLanguage(t *testing.T) {
	home := t.TempDir()
	if got := Active(home).Format("result.done"); got != "Listo." {
		t.Fatalf("nothing saved must read es, got %q", got)
	}
	if _, err := gabitotl.SavePersonaLang(home, persona.LangEN); err != nil {
		t.Fatal(err)
	}
	if got := Active(home).Format("result.done"); got != "Done." {
		t.Fatalf("lang: en must read English, got %q", got)
	}
	if b, _ := os.ReadFile(filepath.Join(home, ".gabitotl", "persona.md")); !strings.Contains(string(b), "lang: en") {
		t.Fatalf("persona.md = %s", b)
	}
}

func TestDoctorRendersInEnglish(t *testing.T) {
	var out bytes.Buffer
	RenderDoctor(&out, For(persona.LangEN), installer.DoctorReport{Sections: []installer.Section{
		{Name: "core", Lines: []installer.Line{{Code: "doctor.core.git", Status: installer.LineOK}}},
		{Name: "git", Lines: []installer.Line{{Code: "doctor.git.delivery-todo", Status: installer.LineWarn}}},
	}})
	for _, want := range []string{"GabitoTL doctor", "Git installed", "you have not chosen how delivery works yet"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "Proyecto") || strings.Contains(out.String(), "instalado") {
		t.Errorf("Spanish leaked into the English screen:\n%s", out.String())
	}
}
