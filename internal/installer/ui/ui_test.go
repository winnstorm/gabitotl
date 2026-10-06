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
