package cli

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
)

func TestVCSSetupAsksOnceSavesToTheTeamFileAndStatusShowsIt(t *testing.T) {
	t.Setenv("GABITOTL_USER", "ana")
	root := project(t)
	l := gabitotl.NewLayout(root)
	if v, _ := gabitotl.LoadVCS(l); v.IsConfigured() {
		t.Fatal("a new project starts unconfigured")
	}
	if r := runIn(t, "", false, root, "vcs", "status"); !strings.Contains(r.out, "delivery: not configured yet; run gabitotl vcs setup") {
		t.Fatalf("%+v", r)
	}
	// platform GitHub | everything automatic | squash | delete the branch | normal PRs
	r := runIn(t, "1\n1\n1\n1\n1\n", true, root, "vcs", "setup")
	if r.code != 0 || !strings.Contains(r.out, "se comparte con el equipo") || !strings.Contains(r.out, "todo automático") {
		t.Fatalf("%+v", r)
	}
	v, err := gabitotl.LoadVCS(l)
	if err != nil || !v.Configured || v.Provider != "github" || v.AutomationLevel() != gabitotl.AutomationAll || v.EffectiveMergeMethod() != "squash" || !v.DeleteBranch {
		t.Fatalf("%+v %v", v, err)
	}
	st := runIn(t, "", false, root, "vcs", "status")
	for _, want := range []string{"delivery: configured (all)", "merge=allow", "merge_method: squash", "delete_branch: true", "draft_prs: false", "chain: stacked", "pr_budget_lines: 400", "files: shared="} {
		if !strings.Contains(st.out, want) {
			t.Errorf("status lacks %q:\n%s", want, st.out)
		}
	}
	if strings.Contains(st.out, "(yours)") {
		t.Fatalf("nothing is overridden yet:\n%s", st.out)
	}
}

func TestVCSSetupOnAConfiguredProjectAsksOnlyForYourAutomationAndKeepsItYours(t *testing.T) {
	t.Setenv("GABITOTL_USER", "ana")
	root := project(t)
	if r := runIn(t, "", false, root, "vcs", "setup", "--vcs-automation=all"); r.code != 0 {
		t.Fatalf("%+v", r)
	}
	team, _ := os.ReadFile(gabitotl.NewLayout(root).VCS())
	// Only one question: how much to automate. 3 = ask me each time.
	r := runIn(t, "3\n", true, root, "vcs", "setup")
	if r.code != 0 || !strings.Contains(r.out, "solo tuya") || strings.Contains(r.out, "se comparte con el equipo") {
		t.Fatalf("%+v", r)
	}
	if strings.Count(r.out, "Elegí una opción") != 1 {
		t.Fatalf("one question:\n%s", r.out)
	}
	after, _ := os.ReadFile(gabitotl.NewLayout(root).VCS())
	if string(after) != string(team) {
		t.Fatalf("the team file must not change:\n%s", after)
	}
	st := runIn(t, "", false, root, "vcs", "status")
	if !strings.Contains(st.out, "delivery: configured (ask)") || !strings.Contains(st.out, "push=ask (yours)") || !strings.Contains(st.out, "overridden by you:") {
		t.Fatalf("status:\n%s", st.out)
	}
	if !strings.Contains(st.out, "yours="+root) {
		t.Fatalf("status names your file:\n%s", st.out)
	}
	// A teammate keeps the team's choice.
	t.Setenv("GABITOTL_USER", "bruno")
	if st := runIn(t, "", false, root, "vcs", "status"); !strings.Contains(st.out, "delivery: configured (all)") || strings.Contains(st.out, "(yours)") {
		t.Fatalf("teammate:\n%s", st.out)
	}
}

func TestVCSSetupFlagsSettleItWithoutATerminal(t *testing.T) {
	t.Setenv("GABITOTL_USER", "ana")
	root := project(t)
	r := runIn(t, "", false, root, "vcs", "setup", "--vcs-provider", "gitlab", "--vcs-automation=until-pr",
		"--vcs-merge-method=rebase", "--vcs-delete-branch=false", "--vcs-draft")
	if r.code != 0 {
		t.Fatalf("%+v", r)
	}
	v, _ := gabitotl.LoadVCS(gabitotl.NewLayout(root))
	if !v.Configured || v.Provider != "gitlab" || v.AutomationLevel() != gabitotl.AutomationUntilPR ||
		v.MergeMethod != "rebase" || v.DeleteBranch || !v.DraftPRs {
		t.Fatalf("%+v", v)
	}
	st := runIn(t, "", false, root, "vcs", "status", "--json")
	if !strings.Contains(st.out, `"delivery_configured": true`) || !strings.Contains(st.out, `"automation": "until-pr"`) {
		t.Fatalf("%s", st.out)
	}
}

func TestVCSSetupWithYesOrWithoutATerminalAsksNothingAndMarksNothing(t *testing.T) {
	t.Setenv("GABITOTL_USER", "ana")
	root := project(t)
	l := gabitotl.NewLayout(root)
	before, _ := os.ReadFile(l.VCS())
	r := runIn(t, "1\n1\n", true, root, "vcs", "setup", "--yes")
	if r.code != 0 || strings.Contains(r.out, "Elegí una opción") {
		t.Fatalf("--yes asks nothing: %+v", r)
	}
	r = runIn(t, "", false, root, "vcs", "setup")
	if r.code != ExitError || !strings.Contains(r.err, "Sin terminal") {
		t.Fatalf("no terminal and no flags is reported, not guessed: %+v", r)
	}
	if after, _ := os.ReadFile(l.VCS()); string(after) != string(before) {
		t.Fatalf("nothing is written:\n%s", after)
	}
	if v, _ := gabitotl.LoadVCS(l); v.IsConfigured() {
		t.Fatal("nothing is marked configured")
	}
}

func TestVCSSetupRejectsUnknownValues(t *testing.T) {
	root := project(t)
	for _, args := range [][]string{{"--vcs-automation=everything"}, {"--vcs-merge-method=ff"}, {"--vcs-provider=svn"}} {
		r := runIn(t, "", false, root, append([]string{"vcs", "setup"}, args...)...)
		if r.code != ExitUsage {
			t.Errorf("%v: %+v", args, r)
		}
	}
}

func TestVCSSetupSharedFlagChangesTheTeamFile(t *testing.T) {
	t.Setenv("GABITOTL_USER", "ana")
	root := project(t)
	runIn(t, "", false, root, "vcs", "setup", "--vcs-automation=all")
	runIn(t, "3\n", true, root, "vcs", "setup") // ana: ask each time (hers)
	r := runIn(t, "", false, root, "vcs", "setup", "--shared", "--vcs-automation=until-pr")
	if r.code != 0 || !strings.Contains(r.out, "se comparte con el equipo") {
		t.Fatalf("%+v", r)
	}
	st := runIn(t, "", false, root, "vcs", "status")
	if !strings.Contains(st.out, "delivery: configured (until-pr)") || strings.Contains(st.out, "(yours)") {
		t.Fatalf("the team file wins and ana's override is dropped:\n%s", st.out)
	}
}

func TestInstallAsksTheDeliveryOnceAndNeverAgain(t *testing.T) {
	f := engramProjectFix(t)
	l := gabitotl.NewLayout(f.root)
	if v, _ := gabitotl.LoadVCS(l); v.IsConfigured() {
		t.Fatal("init alone leaves the delivery unconfigured")
	}
	// keep local memory | platform, automation (everything), merge method, delete, draft | apply
	r := f.run("1\n\n1\n1\n1\n1\ns\n", true, "install", "--no-tui")
	if r.code != 0 || !strings.Contains(r.out, "¿Querés que Gabito haga solo commit, push, PR y merge?") {
		t.Fatalf("%+v", r)
	}
	v, err := gabitotl.LoadVCS(l)
	if err != nil || !v.Configured || v.AutomationLevel() != gabitotl.AutomationAll || v.Provider != "github" {
		t.Fatalf("%+v %v", v, err)
	}
	r = f.run("1\ns\n", true, "install", "--no-tui")
	if r.code != 0 || strings.Contains(r.out, "commit, push, PR y merge?") || strings.Contains(r.out, "¿Abro los PR como borrador?") {
		t.Fatalf("the second run asks nothing about delivery:\n%s", r.out)
	}
}

func TestInstallYesAsksNothingAndFlagsConfigureWithoutATerminal(t *testing.T) {
	f := engramProjectFix(t)
	l := gabitotl.NewLayout(f.root)
	r := f.run("", false, "install", "--yes")
	if r.code != 0 || strings.Contains(r.out, "Elegí una opción") {
		t.Fatalf("%+v", r)
	}
	if v, _ := gabitotl.LoadVCS(l); v.IsConfigured() {
		t.Fatal("--yes alone marks nothing as configured")
	}
	r = f.run("", false, "install", "--yes", "--vcs-automation=until-pr", "--vcs-draft")
	if r.code != 0 {
		t.Fatalf("%+v", r)
	}
	v, _ := gabitotl.LoadVCS(l)
	if !v.Configured || v.AutomationLevel() != gabitotl.AutomationUntilPR || !v.DraftPRs {
		t.Fatalf("%+v", v)
	}
	if r := f.run("", false, "install", "--yes", "--vcs-automation=sometimes"); r.code != ExitUsage {
		t.Fatalf("bad flag value: %+v", r)
	}
}

func TestLegacyAutomatedFileCountsAsConfiguredAndIsNotAsked(t *testing.T) {
	f := engramProjectFix(t)
	l := gabitotl.NewLayout(f.root)
	v, _ := gabitotl.LoadVCS(l)
	v.RemoteActions = "automatic"
	v.Permissions = gabitotl.VCSPermissions{Branches: "allow", Commits: "allow", Push: "allow", PullRequests: "allow"}
	if err := gabitotl.SaveVCS(l, v); err != nil {
		t.Fatal(err)
	}
	r := f.run("1\nn\n", true, "install", "--no-tui")
	if r.code != 0 || strings.Contains(r.out, "commit, push, PR y merge?") {
		t.Fatalf("a file that was automated before the marker is not asked again:\n%s", r.out)
	}
	got, _ := gabitotl.LoadVCS(l)
	if got.Permissions.MergePermission() != "ask" {
		t.Fatalf("merge stays ask for such a file: %+v", got.Permissions)
	}
}

func TestVCSSetupQuestionsPrintWhatToAskAndChangeNothing(t *testing.T) {
	t.Setenv("GABITOTL_USER", "ana")
	root := project(t)
	l := gabitotl.NewLayout(root)
	before, _ := os.ReadFile(l.VCS())

	type q struct {
		ID, Flag, Default string
		Options           []struct{ Value string }
		OnlyIfAutomation  []string `json:"only_if_automation"`
	}
	type out struct {
		Configured bool
		Scope      string
		Questions  []q
	}
	read := func(args ...string) out {
		t.Helper()
		r := runIn(t, "", false, root, append([]string{"vcs", "setup", "--questions"}, args...)...)
		var o out
		if r.code != 0 || json.Unmarshal([]byte(r.out), &o) != nil {
			t.Fatalf("%+v", r)
		}
		return o
	}
	ids := func(o out) string {
		var s []string
		for _, x := range o.Questions {
			s = append(s, x.ID)
		}
		return strings.Join(s, ",")
	}

	o := read()
	if o.Configured || o.Scope != "team" || ids(o) != "platform,automation,merge,delete,draft" {
		t.Fatalf("fresh project asks everything: %+v", o)
	}
	for _, x := range o.Questions {
		if !strings.HasPrefix(x.Flag, "--vcs-") || x.Default == "" || len(x.Options) < 2 {
			t.Errorf("incomplete question: %+v", x)
		}
		if strings.Contains(strings.ToLower(x.ID), "size") || strings.Contains(strings.ToLower(x.ID), "budget") {
			t.Errorf("never ask about PR size: %+v", x)
		}
	}
	if after, _ := os.ReadFile(l.VCS()); string(after) != string(before) {
		t.Fatal("--questions writes nothing")
	}

	runIn(t, "", false, root, "vcs", "setup", "--vcs-automation=all")
	if o := read(); !o.Configured || o.Scope != "personal" || ids(o) != "automation" {
		t.Fatalf("a configured team asks only your automation: %+v", o)
	}
	if o := read("--shared"); o.Scope != "team" || len(o.Questions) != 5 {
		t.Fatalf("--shared asks the team questions again: %+v", o)
	}
}
