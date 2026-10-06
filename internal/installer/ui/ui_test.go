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
