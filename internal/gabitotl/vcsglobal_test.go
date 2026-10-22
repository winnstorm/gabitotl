package gabitotl

import (
	"os"
	"strings"
	"testing"
)

func TestVCSLayeringBuiltInGlobalProjectPerson(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	l := NewLayout(root)

	// Built-in defaults when nothing exists.
	g, ok, err := LoadGlobalVCS(home)
	if err != nil || ok || g.MergeMethod != MergeSquash {
		t.Fatalf("built-in: %+v %v %v", g, ok, err)
	}

	// Global layer alone: a project with no vcs.md reports not-exist, the caller uses the base.
	f := false
	if _, err := SaveGlobalDelivery(home, DeliveryAnswers{Automation: AutomationUntilPR, MergeMethod: MergeRebase, DeleteBranch: &f}); err != nil {
		t.Fatal(err)
	}
	g, ok, err = LoadGlobalVCS(home)
	if err != nil || !ok || g.MergeMethod != MergeRebase || g.DeleteBranch || g.AutomationLevel() != AutomationUntilPR || g.Configured || g.Provider != "git" {
		t.Fatalf("global: %+v %v %v", g, ok, err)
	}
	if _, err := LoadVCSLayered(l, "", g); !os.IsNotExist(err) {
		t.Fatalf("a project without vcs.md is reported as missing: %v", err)
	}

	// A project file that sets only some keys inherits the rest from the global layer.
	write := func(path, body string) {
		t.Helper()
		if err := os.MkdirAll(l.Dir(), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(l.VCS(), "---\nprovider: github\nmerge_method: merge\n---\n")
	c, err := LoadVCSLayered(l, "", g)
	if err != nil || c.MergeMethod != MergeMerge || c.Provider != "github" || c.DeleteBranch || c.AutomationLevel() != AutomationUntilPR {
		t.Fatalf("project over global: %+v %v", c, err)
	}

	// The per-person file wins over both.
	if p, err := l.LocalVCS("ana"); err == nil {
		if err := os.MkdirAll(strings.TrimSuffix(p, "/vcs.md"), 0o755); err != nil {
			t.Fatal(err)
		}
		write(p, "---\npermissions:\n  merge: allow\n---\n")
	}
	c, err = LoadVCSLayered(l, "ana", g)
	if err != nil || c.Permissions.Merge != PermAllow || c.MergeMethod != MergeMerge {
		t.Fatalf("person over project: %+v %v", c, err)
	}

	// The old entry point still ignores the global layer.
	if c, _ := LoadVCSFor(l, ""); c.DeleteBranch != true {
		t.Fatalf("LoadVCSFor keeps the built-in base: %+v", c)
	}
}

func TestGlobalFileNeverCarriesAuthOrConfigured(t *testing.T) {
	home := t.TempDir()
	c := DefaultVCS("github")
	c.Auth, c.Configured = "env:TOKEN", true
	if err := SaveGlobalVCS(home, c); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(GlobalLayout(home).VCS())
	if strings.Contains(string(b), "TOKEN") || strings.Contains(string(b), "configured") || !strings.Contains(string(b), "provider: git") {
		t.Fatalf("%s", b)
	}
}

func TestMalformedGlobalFileErrorNamesThePath(t *testing.T) {
	home := t.TempDir()
	p := GlobalLayout(home).VCS()
	if err := os.MkdirAll(GlobalLayout(home).Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{"---\nprovider: [unclosed\n---\n", "---\nmerge_method: nope\n---\n"} {
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, _, err := LoadGlobalVCS(home); err == nil || !strings.Contains(err.Error(), p) {
			t.Fatalf("the error must name %s: %v", p, err)
		}
	}
}

func TestResetDeliveryToGlobalDropsProjectAndPersonalOverrides(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	l := NewLayout(root)
	if _, err := Init(root, InitOptions{VCSProvider: "github"}); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveGlobalDelivery(home, DeliveryAnswers{Automation: AutomationAsk, MergeMethod: MergeRebase}); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveDelivery(l, "ada", DeliveryAnswers{Automation: AutomationAll, MergeMethod: MergeMerge}); err != nil {
		t.Fatal(err)
	}
	if c, _ := LoadVCSFor(l, "ada"); !c.Configured || c.MergeMethod != MergeMerge {
		t.Fatalf("override not saved: %+v", c)
	}
	if err := ResetDeliveryToGlobal(l, "ada", home); err != nil {
		t.Fatal(err)
	}
	g, _, _ := LoadGlobalVCS(home)
	c, err := LoadVCSLayered(l, "ada", g)
	if err != nil || c.Configured || c.MergeMethod != MergeRebase || c.AutomationLevel() != AutomationAsk || c.Provider != "github" {
		t.Fatalf("after reset: %+v %v", c, err)
	}
}
