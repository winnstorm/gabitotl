package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/vcs"
)

// commitLines adds a file of n lines and commits it on the current branch.
func commitLines(t *testing.T, root, name string, n int, msg string) {
	t.Helper()
	writeIn(t, root, name, strings.Repeat("line\n", n))
	gitOut(t, root, "add", name)
	gitOut(t, root, "commit", "-q", "-m", msg)
}

func shipArgs(extra ...string) []string {
	return append([]string{"vcs", "ship", "--title", "feat: login"}, extra...)
}

func withBudget(n int) func(*gabitotl.VCSConfig) {
	return func(c *gabitotl.VCSConfig) { c.PRBudgetLines = n }
}

func TestShipHappyPathCommitsPushesOpensAndMerges(t *testing.T) {
	root, bare, h, _ := deliveryProject(t, nil)
	onFeature(t, root)
	r := runIn(t, "", false, root, shipArgs("-m", "feat(login): add the form")...)
	if r.code != 0 || len(h.created) != 1 || len(h.merged) != 1 {
		t.Fatalf("%+v created=%d merged=%d", r, len(h.created), len(h.merged))
	}
	for _, want := range []string{"Commit ", "Subí feature/login", "PR de feature/login hacia main", "Mergeé"} {
		if !strings.Contains(r.out, want) {
			t.Errorf("missing %q in:\n%s", want, r.out)
		}
	}
	if gitOut(t, root, "symbolic-ref", "--short", "HEAD") != "main" {
		t.Fatal("must end on the base branch")
	}
	if gitOut(t, bare, "rev-parse", "--verify", "refs/heads/feature/login") == "" {
		t.Fatal("the branch was not pushed")
	}
}

func TestShipStopsAfterThePRWhenMergeIsNotAllowed(t *testing.T) {
	cases := []struct {
		name, perm, stdin string
		tty               bool
		merged            bool
		want              string
	}{
		{"deny", "deny", "", false, false, "no deja mergear"},
		{"deny with a terminal", "deny", "s\n", true, false, "no deja mergear"},
		{"ask without a terminal", "ask", "", false, false, "gabitotl vcs setup"},
		{"ask declined", "ask", "n\n", true, false, "dijiste que no"},
		{"ask accepted", "ask", "s\n", true, true, "Mergeé"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root, _, h, _ := deliveryProject(t, func(cfg *gabitotl.VCSConfig) { cfg.Permissions.Merge = c.perm })
			onFeature(t, root)
			r := runIn(t, c.stdin, c.tty, root, shipArgs("-m", "feat(login): add the form")...)
			if r.code != 0 || len(h.created) != 1 || (len(h.merged) == 1) != c.merged || !strings.Contains(r.out, c.want) {
				t.Fatalf("%+v created=%d merged=%d", r, len(h.created), len(h.merged))
			}
		})
	}
}

func TestShipFromTheBaseBranchMovesToATaskBranchAndLeavesTheBaseAlone(t *testing.T) {
	root, bare, h, _ := deliveryProject(t, nil)
	before := gitOut(t, root, "rev-parse", "main")
	writeIn(t, root, "app.txt", "changed on main")
	r := runIn(t, "", false, root, shipArgs("-m", "feat(login): add the form")...)
	if r.code != 0 || !strings.Contains(r.out, "creé la rama feature/feat-login") || !strings.Contains(r.out, "quedó como estaba") {
		t.Fatalf("%+v", r)
	}
	if got := gitOut(t, root, "rev-parse", "main"); got != before {
		t.Fatalf("the local base moved: %s -> %s", before, got)
	}
	if gitOut(t, bare, "rev-parse", "refs/heads/main") != before {
		t.Fatal("the base branch must never be pushed")
	}
	if len(h.created) != 1 || h.created[0].Head != "feature/feat-login" {
		t.Fatalf("created = %+v", h.created)
	}
}

func TestShipFromTheBaseBranchWithACommitAheadAndACleanTree(t *testing.T) {
	root, bare, h, _ := deliveryProject(t, nil)
	before := gitOut(t, bare, "rev-parse", "refs/heads/main")
	commitLines(t, root, "n.txt", 2, "feat: local only")
	ahead := gitOut(t, root, "rev-parse", "main")
	r := runIn(t, "", false, root, shipArgs()...)
	if r.code != 0 || len(h.created) != 1 {
		t.Fatalf("%+v", r)
	}
	if gitOut(t, root, "rev-parse", "main") != ahead || gitOut(t, bare, "rev-parse", "refs/heads/main") != before {
		t.Fatal("neither the local nor the remote base may change")
	}
}

func TestShipRefusesWhenThereIsNothingOrNoMessage(t *testing.T) {
	root, _, h, _ := deliveryProject(t, nil)
	if r := runIn(t, "", false, root, shipArgs()...); r.code == 0 || !strings.Contains(r.err, "No hay nada para entregar") {
		t.Fatalf("%+v", r)
	}
	onFeature(t, root)
	if r := runIn(t, "", false, root, shipArgs()...); r.code == 0 || !strings.Contains(r.err, "Falta el mensaje") && !strings.Contains(r.err, "Conventional") {
		t.Fatalf("dirty without -m: %+v", r)
	}
	if len(h.created) != 0 {
		t.Fatal("nothing may be opened")
	}
}

func TestShipDryRunChangesNothing(t *testing.T) {
	for _, onBase := range []bool{false, true} {
		root, bare, h, _ := deliveryProject(t, nil)
		if onBase {
			writeIn(t, root, "app.txt", "changed")
		} else {
			onFeature(t, root)
		}
		head := gitOut(t, root, "rev-parse", "HEAD")
		branch := gitOut(t, root, "symbolic-ref", "--short", "HEAD")
		r := runIn(t, "", false, root, shipArgs("-m", "feat(login): add the form", "--dry-run")...)
		if r.code != 0 || !strings.Contains(r.out, "Plan (no hice ningún cambio)") || !strings.Contains(r.out, "Abriría el PR") {
			t.Fatalf("%+v", r)
		}
		if gitOut(t, root, "rev-parse", "HEAD") != head || gitOut(t, root, "symbolic-ref", "--short", "HEAD") != branch ||
			gitOut(t, root, "status", "--porcelain") == "" || len(h.created) != 0 || h.checkCalls != 0 ||
			gitOut(t, bare, "branch", "--list", "feature/*") != "" {
			t.Fatalf("a dry run left traces (onBase=%v)", onBase)
		}
	}
}

func TestShipJSONIsMachineReadable(t *testing.T) {
	root, _, _, _ := deliveryProject(t, nil)
	onFeature(t, root)
	r := runIn(t, "", false, root, shipArgs("-m", "feat(login): add the form", "--json")...)
	var res vcs.ShipResult
	if err := json.Unmarshal([]byte(r.out), &res); err != nil || r.code != 0 {
		t.Fatalf("%v\n%+v", err, r)
	}
	if !res.Merged || res.Branch != "feature/login" || res.Base != "main" || len(res.PRs) != 1 || res.PRs[0].URL == "" || len(res.Steps) == 0 {
		t.Fatalf("%+v", res)
	}
	root2, _, _, _ := deliveryProject(t, func(c *gabitotl.VCSConfig) { c.Permissions.Merge = "deny" })
	onFeature(t, root2)
	r = runIn(t, "", false, root2, shipArgs("-m", "feat(login): add the form", "--json")...)
	res = vcs.ShipResult{}
	if err := json.Unmarshal([]byte(r.out), &res); err != nil || res.Merged || res.Reason != vcs.StopMergeDenied {
		t.Fatalf("%v %+v", err, res)
	}
}

func TestShipBelowTheBudgetOpensOnePR(t *testing.T) {
	root, _, h, _ := deliveryProject(t, withBudget(50))
	gitOut(t, root, "switch", "-c", "feature/login")
	commitLines(t, root, "a.txt", 10, "feat: a")
	commitLines(t, root, "b.txt", 10, "feat: b")
	r := runIn(t, "", false, root, shipArgs()...)
	if r.code != 0 || len(h.created) != 1 || strings.Contains(r.out, "encadenados") {
		t.Fatalf("%+v", r)
	}
}

func TestShipAboveTheBudgetSlicesAtCommitBoundariesAndMergesInOrder(t *testing.T) {
	root, bare, h, _ := deliveryProject(t, withBudget(10))
	gitOut(t, root, "switch", "-c", "feature/login")
	for _, n := range []string{"a", "b", "c", "d"} {
		commitLines(t, root, n+".txt", 4, "feat: "+n)
	}
	original := gitOut(t, root, "rev-parse", "HEAD")
	r := runIn(t, "", false, root, shipArgs()...)
	if r.code != 0 || len(h.created) != 2 {
		t.Fatalf("%+v created=%d", r, len(h.created))
	}
	if h.created[0].Base != "main" || h.created[0].Head != "feature/login-1" || h.created[1].Base != "feature/login-1" || h.created[1].Head != "feature/login-2" {
		t.Fatalf("stack = %+v", h.created)
	}
	if !strings.Contains(h.created[1].Title, "(2/2)") {
		t.Fatalf("title = %q", h.created[1].Title)
	}
	if gitOut(t, bare, "rev-parse", "refs/heads/feature/login-1") != gitOut(t, root, "rev-parse", original+"~2") {
		t.Fatal("slice 1 must end at the second commit")
	}
	want := []string{"retarget:https://github.com/acme/app/pull/2:main", "merge:https://github.com/acme/app/pull/1", "merge:https://github.com/acme/app/pull/2"}
	if strings.Join(h.events, " ") != strings.Join(want, " ") {
		t.Fatalf("order = %v, want %v", h.events, want)
	}
	for _, m := range h.methods {
		if m != "merge" {
			t.Fatalf("a chain must merge with the plain method, got %v", h.methods)
		}
	}
	if strings.Count(r.out, "no con squash") != 1 {
		t.Fatalf("the merge-method note must print once:\n%s", r.out)
	}
	if gitOut(t, root, "rev-parse", "feature/login") != original {
		t.Fatal("the original branch must be untouched")
	}
	if gitOut(t, root, "branch", "--list", "feature/login-*") != "" {
		t.Fatal("slice branches are deleted locally after the merge")
	}
}

func TestShipChainNoteIsSkippedWhenTheMethodIsAlreadyMerge(t *testing.T) {
	root, _, _, _ := deliveryProject(t, func(c *gabitotl.VCSConfig) { c.PRBudgetLines, c.MergeMethod = 10, "merge" })
	gitOut(t, root, "switch", "-c", "feature/login")
	commitLines(t, root, "a.txt", 8, "feat: a")
	commitLines(t, root, "b.txt", 8, "feat: b")
	r := runIn(t, "", false, root, shipArgs()...)
	if r.code != 0 || strings.Contains(r.out, "no con merge") || !strings.Contains(r.out, "2 PRs encadenados") {
		t.Fatalf("%+v", r)
	}
}

func TestShipPutsAnOversizedCommitInItsOwnSliceAndSaysSo(t *testing.T) {
	root, _, h, _ := deliveryProject(t, withBudget(10))
	gitOut(t, root, "switch", "-c", "feature/login")
	commitLines(t, root, "a.txt", 3, "feat: a")
	commitLines(t, root, "big.txt", 25, "feat: big")
	commitLines(t, root, "c.txt", 3, "feat: c")
	r := runIn(t, "", false, root, shipArgs()...)
	if r.code != 0 || len(h.created) != 3 || !strings.Contains(r.out, "tiene 25 líneas y va solo") {
		t.Fatalf("%+v created=%d", r, len(h.created))
	}
}

func TestShipWithChainNoneOpensOnePRAndStillReportsTheSize(t *testing.T) {
	root, _, h, _ := deliveryProject(t, func(c *gabitotl.VCSConfig) { c.PRBudgetLines, c.Chain = 10, "none" })
	gitOut(t, root, "switch", "-c", "feature/login")
	commitLines(t, root, "a.txt", 8, "feat: a")
	commitLines(t, root, "b.txt", 8, "feat: b")
	r := runIn(t, "", false, root, shipArgs()...)
	if r.code != 0 || len(h.created) != 1 || h.created[0].Head != "feature/login" || !strings.Contains(r.out, "16 líneas") {
		t.Fatalf("%+v created=%+v", r, h.created)
	}
	if len(h.methods) != 1 || h.methods[0] != "squash" {
		t.Fatalf("a single PR keeps the configured method: %v", h.methods)
	}
}

func TestShipDoesNotCountGeneratedFiles(t *testing.T) {
	root, _, h, _ := deliveryProject(t, withBudget(10))
	gitOut(t, root, "switch", "-c", "feature/login")
	writeIn(t, root, "go.sum", strings.Repeat("sum\n", 500))
	writeIn(t, root, "web/package-lock.json", strings.Repeat("lock\n", 500))
	writeIn(t, root, "vendor/x/x.go", strings.Repeat("v\n", 500))
	writeIn(t, root, "a.txt", "1\n2\n3\n")
	gitOut(t, root, "add", "-A")
	gitOut(t, root, "commit", "-q", "-m", "feat: lots of generated files")
	r := runIn(t, "", false, root, shipArgs("--json")...)
	var res vcs.ShipResult
	if err := json.Unmarshal([]byte(r.out), &res); err != nil || res.Lines != 3 || len(h.created) != 1 {
		t.Fatalf("%v lines=%d created=%d\n%s", err, res.Lines, len(h.created), r.out)
	}
}
