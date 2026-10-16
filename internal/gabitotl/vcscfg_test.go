package gabitotl

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func boolp(b bool) *bool { return &b }

// legacyVCS is a vcs.md written before the delivery settings existed, with an
// auth reference in the shared file.
const legacyVCS = `---
provider: github
branch_policy: task
commit_policy: milestone
pr_policy: task-complete
remote_actions: ask
auth: env:GH_TOKEN
permissions:
  branches: ask
  commits: ask
  push: ask
  pull_requests: ask
---

# Version control

My own notes.
`

func writeVCSFile(t *testing.T, l Layout, body string) {
	t.Helper()
	if err := WriteFile(l.VCS(), []byte(body)); err != nil {
		t.Fatal(err)
	}
}

func readText(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("%v", err)
	}
	return string(b)
}

func TestVCSDefaultsAreOptionalAndBackwardCompatible(t *testing.T) {
	l := NewLayout(t.TempDir())
	writeVCSFile(t, l, legacyVCS)
	c, err := LoadVCSFor(l, "")
	if err != nil {
		t.Fatal(err)
	}
	if c.EffectiveMergeMethod() != MergeSquash || !c.DeleteBranch || c.DraftPRs || c.Budget() != 400 || c.ChainStrategy() != ChainStacked || c.BaseBranch != "" {
		t.Fatalf("defaults for the new keys: %+v", c)
	}
	if c.Permissions.MergePermission() != PermAsk {
		t.Fatalf("a missing merge permission reads as ask: %+v", c.Permissions)
	}
	if c.IsConfigured() {
		t.Fatal("a plain defaults file is not configured")
	}
}

func TestVCSValidateNewFields(t *testing.T) {
	bad := map[string]func(*VCSConfig){
		"merge permission": func(c *VCSConfig) { c.Permissions.Merge = "maybe" },
		"merge method":     func(c *VCSConfig) { c.MergeMethod = "fast-forward" },
		"chain":            func(c *VCSConfig) { c.Chain = "tower" },
		"budget":           func(c *VCSConfig) { c.PRBudgetLines = -1 },
		"literal auth":     func(c *VCSConfig) { c.Auth = "ghp_secret" },
	}
	for name, mut := range bad {
		c := DefaultVCS("github")
		mut(&c)
		if err := c.Validate(); err == nil {
			t.Errorf("%s must be rejected", name)
		}
	}
	c := DefaultVCS("github")
	c.Permissions.Merge, c.MergeMethod, c.Chain, c.PRBudgetLines, c.BaseBranch = PermAllow, MergeRebase, ChainNone, 800, "develop"
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestConfiguredIsInferredFromLegacyFiles(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*VCSConfig)
		want bool
	}{
		{"pure defaults", func(*VCSConfig) {}, false},
		{"marker", func(c *VCSConfig) { c.Configured = true }, true},
		{"a permission is allow", func(c *VCSConfig) { c.Permissions.Push = PermAllow }, true},
		{"a permission is deny", func(c *VCSConfig) { c.Permissions.Commits = PermDeny }, true},
		{"remote actions automatic", func(c *VCSConfig) { c.RemoteActions = "automatic" }, true},
		{"merge allow", func(c *VCSConfig) { c.Permissions.Merge = PermAllow }, true},
	}
	for _, c := range cases {
		v := DefaultVCS("git")
		c.mut(&v)
		if v.IsConfigured() != c.want {
			t.Errorf("%s: IsConfigured = %v", c.name, v.IsConfigured())
		}
	}
}

func TestAutomationLevelNamesThePermissions(t *testing.T) {
	for level := range map[string]bool{AutomationAll: true, AutomationUntilPR: true, AutomationAsk: true} {
		p, _ := permissionsFor(level)
		c := DefaultVCS("git")
		c.Permissions = p
		if got := c.AutomationLevel(); got != level {
			t.Errorf("%s reads back as %s", level, got)
		}
	}
	c := DefaultVCS("git")
	c.Permissions.Push = PermDeny
	if c.AutomationLevel() != AutomationCustom {
		t.Fatalf("hand-edited permissions are custom: %s", c.AutomationLevel())
	}
}

func TestLegacyAuthKeepsWorkingAndMovesToTheLocalFileOnTheNextWrite(t *testing.T) {
	l := NewLayout(t.TempDir())
	writeVCSFile(t, l, legacyVCS)
	c, err := LoadVCSFor(l, "ana")
	if err != nil || c.Auth != "env:GH_TOKEN" {
		t.Fatalf("legacy auth must still load: %+v %v", c, err)
	}
	if !InspectVCS(l, "ana").LegacyAuth {
		t.Fatal("the legacy auth must be reported")
	}
	if err := SaveVCSFor(l, "ana", c); err != nil {
		t.Fatal(err)
	}
	shared := readText(t, l.VCS())
	if strings.Contains(shared, "auth:") || !strings.Contains(shared, "My own notes.") {
		t.Fatalf("auth leaves the shared file and the body stays:\n%s", shared)
	}
	lp, _ := l.LocalVCS("ana")
	if !strings.Contains(readText(t, lp), "auth: env:GH_TOKEN") {
		t.Fatalf("auth moves to the person's file:\n%s", readText(t, lp))
	}
	again, err := LoadVCSFor(l, "ana")
	if err != nil || again.Auth != "env:GH_TOKEN" {
		t.Fatalf("merged view after the split: %+v %v", again, err)
	}
	other, _ := LoadVCSFor(l, "bruno")
	if other.Auth != "" {
		t.Fatalf("a teammate does not inherit someone else's auth: %+v", other)
	}
}

func TestSaveVCSWithoutAUserKeepsEverythingShared(t *testing.T) {
	l := NewLayout(t.TempDir())
	c := DefaultVCS("github")
	c.Auth = "native"
	if err := SaveVCSFor(l, "", c); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readText(t, l.VCS()), "auth: native") {
		t.Fatal("without an id the auth stays in the shared file, as before the split")
	}
}

func TestLocalOverrideWinsAndSavingKeepsTheTeamValue(t *testing.T) {
	l := NewLayout(t.TempDir())
	team := DefaultVCS("github")
	team.Permissions = VCSPermissions{PermAllow, PermAllow, PermAllow, PermAllow, PermAllow}
	team.Configured = true
	if err := SaveVCSFor(l, "", team); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveDelivery(l, "ana", DeliveryAnswers{Automation: AutomationAsk}); err != nil {
		t.Fatal(err)
	}
	merged, err := LoadVCSFor(l, "ana")
	if err != nil || merged.AutomationLevel() != AutomationAsk {
		t.Fatalf("ana is more conservative than the team: %+v %v", merged, err)
	}
	if bruno, _ := LoadVCSFor(l, "bruno"); bruno.AutomationLevel() != AutomationAll {
		t.Fatalf("the team keeps its value for everyone else: %+v", bruno.Permissions)
	}
	merged.DraftPRs = true
	if err := SaveVCSFor(l, "ana", merged); err != nil {
		t.Fatal(err)
	}
	if sh, _ := LoadVCSFor(l, ""); sh.AutomationLevel() != AutomationAll || !sh.DraftPRs {
		t.Fatalf("saving the merged view must not bake ana's override into the team file: %+v", sh)
	}
	// A deliberate change of an overridden permission is the new team value.
	merged, _ = LoadVCSFor(l, "ana")
	merged.Permissions.Merge = PermDeny
	if err := SaveVCSFor(l, "ana", merged); err != nil {
		t.Fatal(err)
	}
	sh, _ := LoadVCSFor(l, "")
	if sh.Permissions.Merge != PermDeny {
		t.Fatalf("changed on purpose: %+v", sh.Permissions)
	}
	if o := InspectVCS(l, "ana"); slicesContains(o.Overrides, "merge") {
		t.Fatalf("the override for a changed permission is dropped: %+v", o.Overrides)
	}
}

func slicesContains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func TestSaveDeliveryChoosesTheFileByTheRule(t *testing.T) {
	l := NewLayout(t.TempDir())

	// New file: the team file.
	r, err := SaveDelivery(l, "ana", DeliveryAnswers{Provider: "gitlab", Automation: AutomationUntilPR, DeleteBranch: boolp(false)})
	if err != nil || r.Target != TargetShared || r.Path != l.VCS() {
		t.Fatalf("%+v %v", r, err)
	}
	c := r.Config
	if !c.Configured || c.Provider != "gitlab" || c.AutomationLevel() != AutomationUntilPR || c.DeleteBranch || c.RemoteActions != "automatic" {
		t.Fatalf("%+v", c)
	}
	if lp, _ := l.LocalVCS("ana"); fileExists(lp) {
		t.Fatal("no personal file is created for nothing")
	}

	// Configured team: only the automation, so it is the person's own file.
	r, err = SaveDelivery(l, "ana", DeliveryAnswers{Automation: AutomationAll})
	lp, _ := l.LocalVCS("ana")
	if err != nil || r.Target != TargetYours || r.Path != lp || r.Config.AutomationLevel() != AutomationAll {
		t.Fatalf("%+v %v", r, err)
	}
	if sh, _ := LoadVCSFor(l, ""); sh.AutomationLevel() != AutomationUntilPR {
		t.Fatalf("the team value must not move: %+v", sh.Permissions)
	}
	if got := readText(t, lp); !strings.Contains(got, "merge: allow") || strings.Contains(got, "push") {
		t.Fatalf("only what differs from the team is overridden:\n%s", got)
	}

	// Same as the team: the override disappears.
	r, err = SaveDelivery(l, "ana", DeliveryAnswers{Automation: AutomationUntilPR})
	if err != nil || len(InspectVCS(l, "ana").Overrides) != 0 || r.Config.AutomationLevel() != AutomationUntilPR {
		t.Fatalf("%+v %v", r, err)
	}

	// Team-wide settings always go to the team file.
	r, err = SaveDelivery(l, "ana", DeliveryAnswers{MergeMethod: MergeRebase, Draft: boolp(true)})
	if err != nil || r.Target != TargetShared || r.Config.MergeMethod != MergeRebase || !r.Config.DraftPRs {
		t.Fatalf("%+v %v", r, err)
	}

	// --shared forces the team file and drops the person's own overrides.
	if _, err := SaveDelivery(l, "ana", DeliveryAnswers{Automation: AutomationAsk}); err != nil {
		t.Fatal(err)
	}
	r, err = SaveDelivery(l, "ana", DeliveryAnswers{Automation: AutomationAll, Shared: true})
	if err != nil || r.Target != TargetShared || r.Config.AutomationLevel() != AutomationAll || len(InspectVCS(l, "ana").Overrides) != 0 {
		t.Fatalf("%+v %v", r, err)
	}
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func TestSaveDeliveryOnAnUnconfiguredLegacyFileKeepsNotesAndAnswersOnce(t *testing.T) {
	l := NewLayout(t.TempDir())
	writeVCSFile(t, l, legacyVCS)
	r, err := SaveDelivery(l, "ana", DeliveryAnswers{Automation: AutomationAsk})
	if err != nil || r.Target != TargetShared {
		t.Fatalf("%+v %v", r, err)
	}
	if !r.Config.IsConfigured() || r.Config.AutomationLevel() != AutomationAsk {
		t.Fatalf("choosing ask is an answer too, so it is never asked again: %+v", r.Config)
	}
	if !strings.Contains(readText(t, l.VCS()), "My own notes.") || r.Config.Auth != "env:GH_TOKEN" {
		t.Fatal("the body and the auth reference survive")
	}
}

func TestSaveDeliveryRejectsUnknownValuesAndSecrets(t *testing.T) {
	l := NewLayout(t.TempDir())
	for _, a := range []DeliveryAnswers{{Automation: "everything"}, {MergeMethod: "ff"}, {Provider: "svn"}} {
		if _, err := SaveDelivery(l, "ana", a); err == nil {
			t.Errorf("%+v must be rejected", a)
		}
	}
	if fileExists(l.VCS()) {
		t.Fatal("a rejected answer writes nothing")
	}
}

func TestLocalVCSPathLivesUnderTheIgnoredLocalDirectory(t *testing.T) {
	l := NewLayout("/p")
	p, err := l.LocalVCS("ana")
	if err != nil || filepath.ToSlash(p) != "/p/.gabitotl/local/users/ana/vcs.md" {
		t.Fatalf("%q %v", p, err)
	}
	if _, err := l.LocalVCS("../x"); err == nil {
		t.Fatal("an unsafe id must be rejected")
	}
}

func TestDeliveryAnswersAreOnlyEmptyWhenNothingWasGiven(t *testing.T) {
	if !(DeliveryAnswers{}).Empty() {
		t.Fatal("zero value is empty")
	}
	for _, a := range []DeliveryAnswers{{Provider: "github"}, {Automation: "ask"}, {MergeMethod: "squash"}, {DeleteBranch: boolp(false)}, {Draft: boolp(false)}} {
		if a.Empty() {
			t.Errorf("%+v is an answer", a)
		}
	}
	if reflect.DeepEqual(DefaultVCS("git"), VCSConfig{}) {
		t.Fatal("defaults carry the new settings")
	}
}
