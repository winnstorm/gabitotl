package ui

import (
	"bytes"
	"strings"
	"testing"

	"github.com/winnstorm/gabitotl/internal/installer"
	rt "github.com/winnstorm/gabitotl/internal/runtime"
)

func TestMessagesCoverEveryCode(t *testing.T) {
	m := Default()
	for _, c := range installer.WhyCodes {
		if _, ok := m["why."+c]; !ok {
			t.Errorf("missing why.%s", c)
		}
	}
	for _, c := range installer.NoteCodes {
		if _, ok := m["note."+c]; !ok {
			t.Errorf("missing note.%s", c)
		}
	}
	for _, c := range installer.WarningCodes {
		if _, ok := m["warn."+c]; !ok {
			t.Errorf("missing warn.%s", c)
		}
	}
}

func TestParseMessagesRejectsGaps(t *testing.T) {
	if _, err := ParseMessages("plan.title = x\n"); err == nil || !strings.Contains(err.Error(), "missing keys") {
		t.Fatalf("err = %v", err)
	}
	if _, err := ParseMessages("not a pair\n"); err == nil {
		t.Fatal("malformed line must fail")
	}
	if got := Default().Format("nope"); got != "nope" {
		t.Fatalf("unknown key must be visible, got %q", got)
	}
}

func TestRenderPlanGroupsLikeSpec42(t *testing.T) {
	pl := installer.Plan{Items: []installer.PlanItem{
		{Kind: rt.NoAction, Target: "user skills", Why: installer.WhyUserAssets},
		{Kind: rt.RemoveAfterVerify, Target: "gentle-ai uninstall --all", Why: installer.WhyUninstallAll},
		{Kind: installer.KindImport, Target: "gentle-ai", Count: 3},
		{Kind: rt.Preserve, Target: "/home/u/.engram/engram.db", Why: installer.WhyEngramAdopted},
		{Kind: rt.Modify, Target: "/home/u/.claude/CLAUDE.md [section persona]", Detail: "append managed section"},
		{Kind: rt.Create, Target: "/proj/.gabitotl/project.md"},
	}}
	var b bytes.Buffer
	RenderPlan(&b, Default(), pl, Paths{Home: "/home/u", Root: "/proj"})
	out := b.String()
	idx := -1
	for _, h := range []string{"CREATE", "MODIFY", "PRESERVE", "IMPORT", "REMOVE AFTER VERIFY", "NO ACTION"} {
		i := strings.Index(out, "\n"+h+"\n")
		if i < idx || i < 0 {
			t.Fatalf("group %s out of order or missing:\n%s", h, out)
		}
		idx = i
	}
	for _, want := range []string{"  .gabitotl/project.md", "~/.claude/CLAUDE.md [section persona]", "~/.engram/engram.db", "3 elemento(s)"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestMessagesCoverDoctorAndReport(t *testing.T) {
	m := Default()
	for _, c := range installer.DoctorCodes {
		if _, ok := m[c]; !ok {
			t.Errorf("missing %s", c)
		}
	}
	for _, s := range installer.DoctorSections {
		if _, ok := m["doctor.section."+s]; !ok {
			t.Errorf("missing section %s", s)
		}
	}
	for _, p := range installer.PhaseOrder {
		if _, ok := m["phase."+string(p)]; !ok && p != installer.PhaseInventory && p != installer.PhasePlatform && p != installer.PhaseProject && p != installer.PhaseRuntimes && p != installer.PhaseMemory && p != installer.PhaseOtherHarness {
			t.Errorf("missing phase.%s", p)
		}
	}
}

func TestReportRendererFollowsSpec22(t *testing.T) {
	out := ReportRenderer(Default())(installer.Report{
		Source: "gentle-ai", ImportedAt: "2026-10-05T21:00:00-03:00", Status: "complete",
		Engram: true, Project: "ecommerce", Observation: 1248, Backup: "~/.gabitotl/migrations/x/memory/engram-export.json",
		Imported: []string{"decision: Auth"}, NotImported: []string{"persona", "components: skills"},
		Retired: []string{"persona: claude-code", "gentle-ai: uninstall --all"}, Preserved: []string{"engram", "user-files", "unmanaged-config"},
	})
	for _, want := range []string{"# Gentle-AI", "## Memoria", "Engram detectado y reutilizado.", "ecommerce", "1248", "## Configuración importada",
		"## No importado", "Persona Gentleman", "## Retirado", "## Preservado", "Archivos del usuario", "source: gentle-ai", "status: complete"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestPrompterNeverAnswersForTheUser(t *testing.T) {
	m := Default()
	var out bytes.Buffer
	if NewPrompter(strings.NewReader(""), &out, m).Confirm("¿Seguro?") {
		t.Fatal("end of input must decline")
	}
	p := NewPrompter(strings.NewReader("9\nx\n2\n"), &out, m)
	if i, ok := p.Choose("q", []string{"a", "b"}, 0); !ok || i != 1 {
		t.Fatalf("invalid answers are re-asked: %d %v", i, ok)
	}
	if _, ok := NewPrompter(strings.NewReader(""), &out, m).Choose("q", []string{"a"}, 0); ok {
		t.Fatal("ended input is not a choice")
	}
	if got := NewPrompter(strings.NewReader("\n"), &out, m).Ask("nombre", "Gabi"); got != "Gabi" {
		t.Fatalf("default = %q", got)
	}
	if !NewPrompter(strings.NewReader("sí\n"), &out, m).Confirm("¿?") {
		t.Fatal("sí is a yes")
	}
}

func TestPathsTildeShortensEmbeddedHome(t *testing.T) {
	p := Paths{Home: "/home/u", Root: "/proj"}
	if got := p.Tilde("item x[/home/u/.config/a.md]"); got != "item x[~/.config/a.md]" {
		t.Fatal(got)
	}
}

func TestTeamHintsFollowTheNamedInstance(t *testing.T) {
	render := func(instance string) string {
		var b bytes.Buffer
		renderTeam(&b, Default(), installer.Result{
			Team:   &installer.TeamResult{Role: "host", URL: "http://127.0.0.1:8765/mcp", TokenEnv: "GABITOTL_MEMORY_TOKEN", Project: "shop", Instance: instance},
			Server: &installer.ServerResult{Port: 54329, Created: true},
		}, Paths{})
		return b.String()
	}
	if out := render("trial"); strings.Count(out, "--instance trial") != 2 {
		t.Errorf("both the serve and the token hints must name the instance:\n%s", out)
	}
	if out := render(""); strings.Contains(out, "--instance") {
		t.Errorf("the default instance keeps the plain hints:\n%s", out)
	}
}

func TestOnboardQuestionPicksTheProvider(t *testing.T) {
	m := Default()
	ask := func(in string, o installer.Options, n Need) (installer.Options, string) {
		var out bytes.Buffer
		NewPrompter(strings.NewReader(in), &out, m).Onboard(&o, n, "")
		return o, out.String()
	}
	// Alone: nothing about the provider is set, the default (Engram) stays.
	o, out := ask("1\n", installer.Options{}, Need{Mode: true, Team: true})
	if o.Memory != "" || o.Mode != "" || !strings.Contains(out, "¿Trabajás solo o con otras personas en este proyecto?") {
		t.Fatalf("alone: %+v\n%s", o, out)
	}
	// With other people: team memory, then only what the team setup needs.
	o, out = ask("2\n1\nhttps://memoria.example.com/mcp\n\n", installer.Options{}, Need{Mode: true, Team: true})
	if o.Memory != "memory-mcp" || o.Mode != "team" || o.MemoryRole != "client" || o.MemoryURL != "https://memoria.example.com/mcp" || o.MemoryTokenEnv != installer.DefaultTokenEnv {
		t.Fatalf("with others (client): %+v\n%s", o, out)
	}
	// A bad URL is explained and asked again; it never ends the question.
	o, out = ask("2\n1\nnot a url\nhttps://memoria.example.com/mcp\nMI_TOKEN\n", installer.Options{}, Need{Mode: true, Team: true})
	if o.MemoryURL != "https://memoria.example.com/mcp" || o.MemoryTokenEnv != "MI_TOKEN" || !strings.Contains(out, "La URL no sirve") {
		t.Fatalf("bad url: %+v\n%s", o, out)
	}
	// Host with Engram memory to import asks the import question.
	o, _ = ask("2\n2\ns\n", installer.Options{}, Need{Mode: true, Team: true, CanImport: true})
	if o.MemoryRole != "host" || !o.ImportEngram {
		t.Fatalf("host: %+v", o)
	}
	// "Not yet" goes back to the local default.
	o, _ = ask("2\n3\n", installer.Options{}, Need{Mode: true, Team: true})
	if o.Memory != "" || o.Mode != "" || o.MemoryRole != "" {
		t.Fatalf("stay: %+v", o)
	}
	// Flags answer the questions: a given role and URL are never asked again.
	o, out = ask("2\n", installer.Options{MemoryRole: "client", MemoryURL: "https://memoria.example.com/mcp", MemoryTokenEnv: "X"}, Need{Mode: true, Team: true})
	if strings.Contains(out, "¿Cómo te conectás") || o.MemoryURL != "https://memoria.example.com/mcp" {
		t.Fatalf("flags: %+v\n%s", o, out)
	}
	// Ended input never answers for the user.
	o, _ = ask("", installer.Options{}, Need{Mode: true, Team: true})
	if o.Memory != "" || o.Mode != "" {
		t.Fatalf("eof: %+v", o)
	}
}

func TestOnboardOffersToChangeTheProviderOfAnExistingProject(t *testing.T) {
	m := Default()
	var out bytes.Buffer
	o := installer.Options{}
	NewPrompter(strings.NewReader("1\n"), &out, m).Onboard(&o, Need{Mode: true, Current: "engram", Team: true}, "")
	if !strings.Contains(out.String(), "Este proyecto usa memoria local (Engram)") || o.Memory != "" {
		t.Fatalf("declining keeps the provider: %+v\n%s", o, out.String())
	}
	out.Reset()
	o = installer.Options{}
	NewPrompter(strings.NewReader("2\n1\nhttps://memoria.example.com/mcp\n\n"), &out, m).Onboard(&o, Need{Mode: true, Current: "engram", Team: true}, "")
	if o.Memory != "memory-mcp" || o.MemoryRole != "client" {
		t.Fatalf("accepting moves to team memory: %+v", o)
	}
	// A team project whose server was never configured is asked how to connect.
	out.Reset()
	o = installer.Options{}
	NewPrompter(strings.NewReader("1\nhttps://memoria.example.com/mcp\n\n"), &out, m).Onboard(&o, Need{Pending: true, Team: true}, "")
	if o.Memory != "memory-mcp" || o.MemoryURL == "" {
		t.Fatalf("pending: %+v", o)
	}
}
