package installer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func installed(t *testing.T, f *fixture, o Options) Result {
	t.Helper()
	res, err := runPlan(t, f, o)
	if err != nil {
		t.Fatalf("%v %+v", err, Failed(res.Checks))
	}
	return res
}

func TestDoctorIsReadOnlyAndFollowsSpec41Sections(t *testing.T) {
	f := newFixture(t)
	installed(t, f, Options{User: "gabi", UserName: "Gabi"})
	before := treeHash(t, f.home, f.root)
	f.runner.Calls = nil
	d := Doctor(context.Background(), f.env)
	if treeHash(t, f.home, f.root) != before {
		t.Fatal("doctor must not write anything")
	}
	var names []string
	for _, s := range d.Sections {
		names = append(names, s.Name)
	}
	if strings.Join(names, ",") != strings.Join(DoctorSections, ",") {
		t.Fatalf("sections = %v", names)
	}
	if d.Errors() != 0 {
		t.Fatalf("a healthy install has no errors: %+v", d)
	}
	for _, c := range f.runner.Log() {
		if strings.HasPrefix(c, "engram export") || strings.HasPrefix(c, "engram save") || strings.HasPrefix(c, "gentle-ai") {
			t.Fatalf("doctor ran %q", c)
		}
	}
	codes := map[string]bool{}
	for _, s := range d.Sections {
		for _, l := range s.Lines {
			codes[l.Code] = true
		}
	}
	for _, want := range []string{DocProjectOK, DocMemDoctorOK, DocMemMCP, DocRuntimeOK, DocGitIdentity, DocLegacyGentle, DocLegacySnapshots} {
		if !codes[want] {
			t.Errorf("missing %s in %v", want, codes)
		}
	}
}

func TestDoctorReportsErrorsAndDrift(t *testing.T) {
	f := newFixture(t)
	installed(t, f, Options{})
	writeFile(t, f.home+"/.claude/CLAUDE.md", fxUserMD) // someone removed the persona section
	f.runner.Handlers["engram doctor"] = func([]string) (string, error) {
		return `{"status":"error","checks":[{"check_id":"db","result":"error","message":"corrupt"}]}`, nil
	}
	d := Doctor(context.Background(), f.env)
	if d.Errors() < 2 {
		t.Fatalf("expected the engram error and the runtime drift: %+v", d)
	}
}

func TestRestoreRevertsConfigsButNeverProjectOrEngram(t *testing.T) {
	f := newFixture(t)
	res := installed(t, f, landing)
	engramTree := treeHash(t, f.home+"/.engram")
	projectFiles := treeHash(t, f.root+"/.gabitotl")
	_, plan, err := PlanRestore(f.env, res.Snapshot.ID)
	if err != nil || len(plan) == 0 {
		t.Fatalf("%v %v", err, plan)
	}
	for _, c := range plan {
		if strings.Contains(c.Path, "/.gabitotl/") && strings.HasPrefix(c.Path, f.root) {
			t.Fatalf("restore must never touch the project .gabitotl: %v", c)
		}
	}
	changed, backup, err := Restore(f.env, res.Snapshot.ID, nil)
	if err != nil || len(changed) == 0 {
		t.Fatalf("%v %v", err, changed)
	}
	md := slurp(t, f.home+"/.claude/CLAUDE.md")
	if md != fxUserMD+"\n"+fxGentle {
		t.Fatalf("Claude config not restored to its pre-migration bytes:\n%s", md)
	}
	if treeHash(t, f.home+"/.engram") != engramTree || treeHash(t, f.root+"/.gabitotl") != projectFiles {
		t.Fatal("restore touched Engram or the project folder")
	}
	if _, err := os.Stat(backup); err != nil {
		t.Fatal("restore must save the current files first")
	}
	if _, plan, _ := PlanRestore(f.env, res.Snapshot.ID); len(plan) != 0 {
		t.Fatalf("restore must converge: %v", plan)
	}
}

func TestUninstallRemovesOnlyManagedAssets(t *testing.T) {
	f := newFixture(t)
	installed(t, f, Options{User: "gabi", UserName: "Gabi"})
	engramTree, project := treeHash(t, f.home+"/.engram"), treeHash(t, f.root+"/.gabitotl")
	snapshots := treeHash(t, f.home+"/.gabitotl/migrations")
	steering := slurp(t, f.home+"/.kiro/steering/product.md")

	pl, err := PlanUninstall(f.env, nil)
	if err != nil || len(pl.Runtimes) == 0 || len(pl.Preserved) == 0 {
		t.Fatalf("%v %+v", err, pl)
	}
	res, err := Uninstall(f.env, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	md := slurp(t, f.home+"/.claude/CLAUDE.md")
	if strings.Contains(md, "gabitotl") || !strings.HasPrefix(md, fxUserMD) || !strings.Contains(md, "gentle-ai:persona") {
		t.Fatalf("only GabitoTL's section may go; Gentle-AI and user content stay:\n%s", md)
	}
	if slurp(t, f.home+"/.config/opencode/opencode.json") != fxOC || slurp(t, f.home+"/.kiro/steering/product.md") != steering {
		t.Fatal("user configuration changed")
	}
	if treeHash(t, f.home+"/.engram") != engramTree || treeHash(t, f.root+"/.gabitotl") != project || treeHash(t, f.home+"/.gabitotl/migrations") != snapshots {
		t.Fatal("uninstall must never delete memory, the project folder or backups")
	}
	if _, err := os.Stat(res.Backup); err != nil {
		t.Fatal("touched files must be backed up first")
	}
	if left, _ := PlanUninstall(f.env, nil); len(left.Runtimes) != 0 {
		t.Fatalf("still owned: %+v", left)
	}
	if _, err := PlanUninstall(f.env, []string{"vscode"}); err == nil {
		t.Fatal("unknown runtime must be an error")
	}
}

func TestCleanupOffersOnlyExpiredLegacyInsideGabitoTLDir(t *testing.T) {
	f := newFixture(t)
	res := installed(t, f, landing)
	old := f.home + "/.gabitotl/backups/old"
	writeFile(t, old+"/x", "x")
	writeFile(t, f.home+"/.gabitotl/legacy/gentle-ai/state.json", "{}")
	oldT := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for _, p := range []string{old, f.home + "/.gabitotl/legacy/gentle-ai"} {
		if err := os.Chtimes(p, oldT, oldT); err != nil {
			t.Fatal(err)
		}
	}
	f.env.Now = func() time.Time { return time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC) } // snapshot is 15 days old
	items, err := PlanCleanup(f.env, 30)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, it := range items {
		kinds = append(kinds, it.Kind)
		if strings.Contains(it.Path, ".gentle-ai") || strings.Contains(it.Path, res.Snapshot.ID) {
			t.Fatalf("%v must not be offered yet", it)
		}
	}
	if strings.Join(kinds, ",") != "backup,legacy" {
		t.Fatalf("items = %+v", items)
	}
	f.env.Now = func() time.Time { return time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC) }
	items, _ = PlanCleanup(f.env, 30)
	if len(items) != 3 {
		t.Fatalf("the completed snapshot expires after 30 days: %+v", items)
	}
	gentleBackups := treeHash(t, f.home+"/.gentle-ai/backups")
	if _, err := ApplyCleanup(f.env, items); err != nil {
		t.Fatal(err)
	}
	if treeHash(t, f.home+"/.gentle-ai/backups") != gentleBackups {
		t.Fatal("e: ~/.gentle-ai/backups is never deleted")
	}
	if _, err := ApplyCleanup(f.env, []Expired{{Kind: "legacy", Path: filepath.Join(f.home, ".gentle-ai", "backups")}}); err == nil {
		t.Fatal("paths outside the GabitoTL folders must be refused")
	}
	if _, err := ApplyCleanup(f.env, []Expired{{Kind: "legacy", Path: f.home + "/.gabitotl/legacy/../../.engram"}}); err == nil {
		t.Fatal("traversal must be refused")
	}
}
