package ui

import (
	"bytes"
	"strings"
	"testing"

	"github.com/winnstorm/gabitotl/internal/installer"
)

func gkInventory() installer.GentleKiroInventory {
	f := func(rel string) installer.GentleKiroFile {
		return installer.GentleKiroFile{Path: "/home/u/.kiro/" + rel, Rel: rel, SHA256: "x"}
	}
	return installer.GentleKiroInventory{Bases: []installer.GentleKiroBase{{
		Root:     "/home/u/.kiro",
		Steering: &installer.GentleKiroFile{Path: "/home/u/.kiro/steering/gentle-ai.md", Rel: "steering/gentle-ai.md"},
		Skills:   []installer.GentleKiroSkill{{Name: "a", Files: []installer.GentleKiroFile{f("skills/a/SKILL.md")}}, {Name: "b", Files: []installer.GentleKiroFile{f("skills/b/SKILL.md")}}},
		Shared:   []installer.GentleKiroFile{f("skills/_shared/README.md")},
		Agents:   []installer.GentleKiroFile{f("agents/sdd-apply.md"), f("agents/jd-judge-a.md"), f("agents/review-risk.md")},
		Kept:     []string{"/home/u/.kiro/skills/a/mine.txt"},
	}}}
}

func TestGentleKiroFactsSayWhatWasFoundAndWhatStays(t *testing.T) {
	m := Default()
	got := strings.Join(GentleKiroFacts(m, gkInventory()), "\n")
	for _, want := range []string{"instrucciones siempre activas: sí", "skills: 2", "agentes: 3", "referencias compartidas: 1", "gabitotl restore", "MCP", "1 archivo(s) tuyos"} {
		if !strings.Contains(got, want) {
			t.Errorf("facts lack %q:\n%s", want, got)
		}
	}
	noSteering := gkInventory()
	noSteering.Bases[0].Steering, noSteering.Bases[0].Kept = nil, nil
	got = strings.Join(GentleKiroFacts(m, noSteering), "\n")
	if !strings.Contains(got, "siempre activas: no") || strings.Contains(got, "tuyos dentro") {
		t.Errorf("no steering and no leftovers:\n%s", got)
	}
}

func TestConfirmGentleKiroDefaultsToKeepingBoth(t *testing.T) {
	for name, tc := range map[string]struct {
		in   string
		want bool
	}{"yes": {"s\n", true}, "yes english": {"yes\n", true}, "no": {"n\n", false}, "enter": {"\n", false}, "garbage": {"quizás\n", false}, "closed input": {"", false}} {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			p := NewPrompter(strings.NewReader(tc.in), &out, Default())
			if got := p.ConfirmGentleKiro(gkInventory()); got != tc.want {
				t.Fatalf("answer %q = %v, want %v", tc.in, got, tc.want)
			}
			for _, want := range []string{"las dos están siempre activas", "skills: 2", "[s/N]"} {
				if !strings.Contains(out.String(), want) {
					t.Errorf("question lacks %q:\n%s", want, out.String())
				}
			}
		})
	}
	var out bytes.Buffer
	if NewPrompter(strings.NewReader("s\n"), &out, Default()).ConfirmGentleKiro(installer.GentleKiroInventory{}) || out.Len() != 0 {
		t.Fatal("nothing found: nothing is asked")
	}
}

func TestRenderGentleKiroResult(t *testing.T) {
	m := Default()
	var b bytes.Buffer
	g := &installer.GentleKiroResult{
		Snapshot: "20261005-210000", Removed: make([]string, 9),
		Skipped: []string{"/home/u/.kiro/agents/sdd-apply.md"},
		Kept:    []string{"/p/1", "/p/2", "/p/3", "/p/4", "/p/5", "/p/6", "/p/7"},
	}
	renderGentleKiro(&b, m, g, Paths{Home: "/home/u"})
	out := b.String()
	for _, want := range []string{"Pasé 9 archivo(s)", "gabitotl restore 20261005-210000", "No saqué 1 archivo(s)", "~/.kiro/agents/sdd-apply.md", "Dejé 7 archivo(s) tuyos", "/p/5", "…y 2 más"} {
		if !strings.Contains(out, want) {
			t.Errorf("result lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "/p/6") {
		t.Errorf("only the first %d leftovers are listed:\n%s", maxListed, out)
	}

	b.Reset()
	renderGentleKiro(&b, m, &installer.GentleKiroResult{Snapshot: "s1", Skipped: []string{"/x"}}, Paths{})
	if !strings.Contains(b.String(), "No saqué ningún archivo") {
		t.Errorf("nothing removed must say so:\n%s", b.String())
	}
	b.Reset()
	renderGentleKiro(&b, m, nil, Paths{})
	if b.Len() != 0 {
		t.Fatal("no retirement, no section")
	}
}

func TestResultMentionsRetirementOnlyWhenItRan(t *testing.T) {
	m := Default()
	res := installer.Result{GentleKiro: &installer.GentleKiroResult{Snapshot: "s1", Removed: []string{"/a"}}}
	var b bytes.Buffer
	RenderResult(&b, m, res, installer.Inventory{}, Paths{})
	if !strings.Contains(b.String(), "Gentle AI en Kiro") || !strings.Contains(b.String(), "gabitotl restore s1") {
		t.Fatalf("result:\n%s", b.String())
	}
	b.Reset()
	RenderResult(&b, m, installer.Result{}, installer.Inventory{}, Paths{})
	if strings.Contains(b.String(), "Gentle AI en Kiro") {
		t.Fatalf("no retirement, no section:\n%s", b.String())
	}
}
