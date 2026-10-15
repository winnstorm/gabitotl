package installer

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	rt "github.com/winnstorm/gabitotl/internal/runtime"
)

const (
	fxSkillGentle = "---\nname: %s\ndescription: x\nmetadata:\n  author: gentleman-programming\n  version: \"2.0\"\n---\n\nBody.\n"
	fxSkillMine   = "---\nname: frontend-design\ndescription: mine\n---\n\nBody.\n"
	fxMCP         = "{\n  \"mcpServers\": {\"engram\": {\"command\": \"/opt/engram\", \"args\": [\"mcp\"]}}\n}\n"
)

// gentleKiroTree writes a synthetic copy of Gentle AI's footprint under base and
// returns every file path with its content (the files that must survive too).
func gentleKiroTree(t *testing.T, base string) (gentle, mine map[string]string) {
	t.Helper()
	gentle, mine = map[string]string{}, map[string]string{}
	put := func(m map[string]string, rel, body string) {
		p := filepath.Join(base, filepath.FromSlash(rel))
		writeFile(t, p, body)
		m[p] = body
	}
	put(gentle, "steering/gentle-ai.md", "---\ninclusion: always\n---\n\nPersona.\n<!-- gentle-ai:orchestrator -->\nx\n<!-- /gentle-ai:orchestrator -->\n")
	put(gentle, "skills/branch-pr/SKILL.md", strings.Replace(fxSkillGentle, "%s", "branch-pr", 1))
	put(gentle, "skills/go-testing/SKILL.md", strings.Replace(fxSkillGentle, "%s", "go-testing", 1))
	put(gentle, "skills/go-testing/references/examples.md", "examples\n")
	// CRLF front matter with a BOM and a quoted author.
	put(gentle, "skills/sdd-apply/SKILL.md", "\xef\xbb\xbf---\r\nname: sdd-apply\r\nmetadata:\r\n  author: 'Gentleman-Programming'\r\n---\r\nBody\r\n")
	put(gentle, "skills/sdd-apply/strict-tdd.md", "tdd\n")
	put(gentle, "skills/_shared/README.md", "shared\n")
	put(gentle, "skills/_shared/skill-resolver.md", "resolver\n")
	put(gentle, "agents/sdd-apply.md", "agent\n")
	put(gentle, "agents/jd-judge-a.md", "judge\n")
	put(gentle, "agents/custom-gentle.md", "only in the manifest\n")
	put(gentle, "agents/.gentle-ai-native-agent-ownership.json", `{"version":1,"files":{"custom-gentle.md":"abc","../escape.md":"x"}}`)

	put(mine, "skills/frontend-design/SKILL.md", fxSkillMine)
	put(mine, "skills/branch-pr/my-notes.txt", "user file inside a Gentle skill\n")
	put(mine, "skills/_shared/my-shared.md", "user file in _shared\n")
	put(mine, "agents/my-agent.md", "my agent\n")
	put(mine, "settings/mcp.json", fxMCP)
	put(mine, "settings/permissions.yaml", "allow: []\n")
	put(mine, "steering/product.md", "# Product\n")
	return gentle, mine
}

func kiroBase(f *fixture) string { return filepath.Join(f.home, ".kiro") }

func gentleFixture(t *testing.T) (*fixture, map[string]string, map[string]string) {
	t.Helper()
	f := newFixture(t)
	g, m := gentleKiroTree(t, kiroBase(f))
	return f, g, m
}

func exists(p string) bool { _, err := os.Lstat(p); return err == nil }

func TestDetectGentleKiroInventory(t *testing.T) {
	f, gentle, mine := gentleFixture(t)
	inv := DetectGentleKiro([]string{kiroBase(f), filepath.Join(f.home, "missing")})
	if len(inv.Bases) != 1 {
		t.Fatalf("bases = %d", len(inv.Bases))
	}
	got := map[string]bool{}
	for _, x := range inv.Files() {
		got[x.Path] = true
		if x.SHA256 == "" || x.Rel == "" {
			t.Fatalf("incomplete entry %+v", x)
		}
	}
	for p := range gentle {
		if !got[p] {
			t.Errorf("Gentle file not inventoried: %s", p)
		}
	}
	for p := range mine {
		if got[p] {
			t.Errorf("user file inventoried: %s", p)
		}
	}
	if got[filepath.Join(kiroBase(f), "agents", "..", "escape.md")] {
		t.Error("a manifest key with a separator must be ignored")
	}
	c := inv.Counts()
	if c.Steering != 1 || c.Skills != 3 || c.Shared != 2 || c.Agents != 4 {
		t.Fatalf("counts = %+v", c)
	}
	kept := strings.Join(inv.KeptFiles(), "\n")
	for _, want := range []string{"my-notes.txt", "my-shared.md"} {
		if !strings.Contains(kept, want) {
			t.Errorf("leftover %s must be reported as kept:\n%s", want, kept)
		}
	}
}

func TestSkillAuthorParsing(t *testing.T) {
	for name, tc := range map[string]struct{ in, want string }{
		"plain":      {"---\nmetadata:\n  author: gentleman-programming\n---\n", "gentleman-programming"},
		"crlf":       {"---\r\nmetadata:\r\n  author: gentleman-programming\r\n---\r\n", "gentleman-programming"},
		"bom":        {"\xef\xbb\xbf---\nmetadata:\n  author: x\n---\n", "x"},
		"quoted":     {"---\nmetadata:\n  author: \"Gentleman-Programming\"\n---\n", "gentleman-programming"},
		"comment":    {"---\nmetadata:\n  author: gentleman-programming # who\n---\n", "gentleman-programming"},
		"no author":  {"---\nname: a\n---\n", ""},
		"top author": {"---\nauthor: gentleman-programming\n---\n", ""},
		"no front":   {"metadata:\n  author: gentleman-programming\n", ""},
		"unclosed":   {"---\nmetadata:\n  author: gentleman-programming\n", "gentleman-programming"},
		"after body": {"---\nname: a\n---\nmetadata:\n  author: gentleman-programming\n", ""},
	} {
		if got := skillAuthor([]byte(tc.in)); got != tc.want {
			t.Errorf("%s: got %q want %q", name, got, tc.want)
		}
	}
}

func TestPlanGentleKiroWithAndWithoutOption(t *testing.T) {
	f, _, _ := gentleFixture(t)
	_, pl := buildPlan(t, f, Options{Runtimes: []string{"kiro-cli"}})
	if pl.GentleKiro == nil || !pl.HasNote(NoteGentleKiroPresent) {
		t.Fatalf("without the option the inventory and one note are expected: %+v", pl.Notes)
	}
	if got := kinds(pl, rt.RemoveAfterVerify); len(got) != 0 {
		t.Fatalf("nothing may be planned for removal without the option: %v", got)
	}
	_, pl = buildPlan(t, f, Options{Runtimes: []string{"kiro-cli"}, RetireGentleKiro: true})
	if !pl.retireGentleKiro() || !pl.Mutates() {
		t.Fatal("the option must plan the retirement")
	}
	if pl.HasNote(NoteGentleKiroPresent) {
		t.Fatal("the informational note is for the declined case")
	}
	var why []string
	for _, it := range pl.Items {
		if it.Kind == rt.RemoveAfterVerify {
			why = append(why, it.Why)
		}
	}
	sort.Strings(why)
	want := []string{WhyGentleKiroAgents, WhyGentleKiroShared, WhyGentleKiroSkills, WhyGentleKiroSteering}
	sort.Strings(want)
	if strings.Join(why, ",") != strings.Join(want, ",") {
		t.Fatalf("retire items = %v", why)
	}
}

func TestPlanGentleKiroNothingFoundAndNoKiroRuntime(t *testing.T) {
	f := newFixture(t)
	_, pl := buildPlan(t, f, Options{Runtimes: []string{"kiro-cli"}, RetireGentleKiro: true})
	if pl.GentleKiro != nil || !pl.HasNote(NoteGentleKiroNone) || pl.retireGentleKiro() {
		t.Fatalf("option set with nothing found: %+v", pl.Notes)
	}
	f, _, _ = gentleFixture(t)
	_, pl = buildPlan(t, f, Options{Runtimes: []string{"claude-code"}, RetireGentleKiro: true})
	if pl.GentleKiro != nil || pl.HasNote(NoteGentleKiroNone) || pl.HasNote(NoteGentleKiroPresent) || pl.retireGentleKiro() {
		t.Fatalf("without a Kiro runtime the option does nothing: %+v", pl.Notes)
	}
	for _, it := range pl.Items {
		if it.Kind == rt.RemoveAfterVerify {
			t.Fatalf("unexpected retire item %+v", it)
		}
	}
}

func TestGentleKiroUsesKiroHomeAndWorkspace(t *testing.T) {
	f := newFixture(t)
	alt := filepath.Join(f.home, "alt-kiro")
	f.vars["KIRO_HOME"] = alt
	g1, _ := gentleKiroTree(t, alt)
	g2, _ := gentleKiroTree(t, filepath.Join(f.root, ".kiro"))
	_, pl := buildPlan(t, f, Options{Runtimes: []string{"kiro-cli", "kiro-ide"}, RetireGentleKiro: true})
	if pl.GentleKiro == nil || len(pl.GentleKiro.Bases) != 2 {
		t.Fatalf("both bases expected: %+v", pl.GentleKiro)
	}
	got := map[string]bool{}
	for _, x := range pl.GentleKiro.Files() {
		got[x.Path] = true
	}
	if len(got) != len(g1)+len(g2) {
		t.Fatalf("files = %d, want %d", len(got), len(g1)+len(g2))
	}
}

func TestRunRetiresGentleKiroIntoSnapshotAndRestoresByteForByte(t *testing.T) {
	f, gentle, mine := gentleFixture(t)
	res, err := runPlan(t, f, Options{User: "gabi", UserName: "Gabi", Runtimes: []string{"kiro-cli"}, RetireGentleKiro: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.GentleKiro == nil || len(res.GentleKiro.Removed) != len(gentle) || len(res.GentleKiro.Skipped) != 0 {
		t.Fatalf("result = %+v", res.GentleKiro)
	}
	for p := range gentle {
		if exists(p) {
			t.Errorf("Gentle file still in place: %s", p)
		}
	}
	for p, body := range mine {
		if strings.HasSuffix(p, "mcp.json") { // GabitoTL adds its own entry; the user's stays
			continue
		}
		if slurp(t, p) != body {
			t.Errorf("user file changed: %s", p)
		}
	}
	for _, d := range []string{"skills/sdd-apply", "skills/go-testing", "agents/.gentle-ai-native-agent-ownership.json"} {
		if exists(filepath.Join(kiroBase(f), filepath.FromSlash(d))) {
			t.Errorf("%s should be gone", d)
		}
	}
	if !exists(filepath.Join(kiroBase(f), "skills", "branch-pr", "my-notes.txt")) || !exists(filepath.Join(kiroBase(f), "skills", "_shared", "my-shared.md")) {
		t.Fatal("user files inside Gentle directories must stay with their directories")
	}
	if !strings.Contains(slurp(t, filepath.Join(kiroBase(f), "settings", "mcp.json")), `"/opt/engram"`) {
		t.Fatal("the user's MCP entry changed")
	}
	// The copies live in the snapshot, preserving relative paths.
	copyDir := filepath.Join(res.Snapshot.Dir, "gentle-kiro", "0")
	if slurp(t, filepath.Join(copyDir, "steering", "gentle-ai.md")) != gentle[filepath.Join(kiroBase(f), "steering", "gentle-ai.md")] {
		t.Fatal("the steering copy is not in the snapshot")
	}

	// Restore brings every file back at its original path, byte for byte.
	if _, _, err := Restore(f.env, res.Snapshot.ID, nil); err != nil {
		t.Fatal(err)
	}
	for p, body := range gentle {
		if !exists(p) || slurp(t, p) != body {
			t.Errorf("not restored byte for byte: %s", p)
		}
	}
}

func TestGentleKiroRetireIsIdempotent(t *testing.T) {
	f, _, _ := gentleFixture(t)
	o := Options{User: "gabi", UserName: "Gabi", Runtimes: []string{"kiro-cli"}, RetireGentleKiro: true}
	if _, err := runPlan(t, f, o); err != nil {
		t.Fatal(err)
	}
	snaps, _ := ListSnapshots(f.env)
	res, err := runPlan(t, f, o)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Skipped {
		t.Fatalf("a re-run must find nothing to do: %+v", res.Phases)
	}
	if after, _ := ListSnapshots(f.env); len(after) != len(snaps) {
		t.Fatal("a re-run must not create a snapshot")
	}
}

func TestGentleKiroSkipsFilesChangedSincePlanning(t *testing.T) {
	f, gentle, _ := gentleFixture(t)
	inv, pl := buildPlan(t, f, Options{User: "gabi", UserName: "Gabi", Runtimes: []string{"kiro-cli"}, RetireGentleKiro: true})
	edited := filepath.Join(kiroBase(f), "agents", "sdd-apply.md")
	writeFile(t, edited, "the user edited this after the plan\n")
	res, err := Run(context.Background(), f.env, inv, pl, nil, Hooks{})
	if err != nil {
		t.Fatal(err)
	}
	if slurp(t, edited) != "the user edited this after the plan\n" {
		t.Fatal("a changed file must stay as the user left it")
	}
	if len(res.GentleKiro.Skipped) != 1 || res.GentleKiro.Skipped[0] != edited || len(res.GentleKiro.Removed) != len(gentle)-1 {
		t.Fatalf("result = %+v", res.GentleKiro)
	}
}

// rootsWrap keeps the Kiro adapter's asset roots visible through a fault wrapper.
type rootsWrap struct{ wrapAdapter }

func (w rootsWrap) ForeignAssetRoots(env rt.Env) []string {
	return w.Adapter.(foreignAssetRoots).ForeignAssetRoots(env)
}

func TestFailedVerificationRemovesNothingFromKiro(t *testing.T) {
	f, gentle, _ := gentleFixture(t)
	withAdapters(f, map[string]func(rt.Adapter) rt.Adapter{
		"kiro-cli": func(a rt.Adapter) rt.Adapter { return rootsWrap{wrapAdapter{Adapter: a, failVerify: true}} },
	})
	res, err := runPlan(t, f, Options{User: "gabi", UserName: "Gabi", Runtimes: []string{"kiro-cli"}, RetireGentleKiro: true})
	if !errors.Is(err, ErrVerifyFailed) || !res.RolledBack {
		t.Fatalf("err = %v rolledBack = %v", err, res.RolledBack)
	}
	for p, body := range gentle {
		if !exists(p) || slurp(t, p) != body {
			t.Errorf("a failed verification must leave %s untouched", p)
		}
	}
}

func TestBackupFailureAbortsBeforeRemoval(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "kiro", "steering", "gentle-ai.md")
	writeFile(t, src, "x")
	// A path that cannot be read as a file stands for a copy that fails.
	bad := filepath.Join(dir, "kiro", "agents", "sdd-apply.md")
	if err := os.MkdirAll(bad, 0o755); err != nil {
		t.Fatal(err)
	}
	inv := &GentleKiroInventory{Bases: []GentleKiroBase{{Root: filepath.Join(dir, "kiro"),
		Steering: &GentleKiroFile{Path: src, Rel: filepath.Join("steering", "gentle-ai.md")},
		Agents:   []GentleKiroFile{{Path: bad, Rel: filepath.Join("agents", "sdd-apply.md")}}}}}
	if _, err := backupGentleKiro(filepath.Join(dir, "snap"), inv); !errors.Is(err, ErrGentleKiroBackup) {
		t.Fatalf("err = %v", err)
	}
	if !exists(src) {
		t.Fatal("nothing may be removed when the backup fails")
	}
}

func TestCorruptSavedCopyAbortsRetirement(t *testing.T) {
	f, gentle, _ := gentleFixture(t)
	inv, pl := buildPlan(t, f, Options{User: "gabi", UserName: "Gabi", Runtimes: []string{"kiro-cli"}, RetireGentleKiro: true})
	withAdapters(f, map[string]func(rt.Adapter) rt.Adapter{
		"kiro-cli": func(a rt.Adapter) rt.Adapter {
			return rootsWrap{wrapAdapter{Adapter: a, beforeApply: func() {
				// Corrupt a saved copy after the snapshot, before retirement.
				_ = filepath.WalkDir(filepath.Join(f.env.StateDir(), "migrations"), func(p string, d fs.DirEntry, err error) error {
					if err == nil && !d.IsDir() && strings.Contains(p, "gentle-kiro") && strings.HasSuffix(p, "gentle-ai.md") {
						_ = os.WriteFile(p, []byte("corrupt"), 0o600)
					}
					return nil
				})
			}}}
		},
	})
	res, err := Run(context.Background(), f.env, inv, pl, nil, Hooks{})
	if !errors.Is(err, ErrGentleKiroBackup) || !res.RolledBack {
		t.Fatalf("err = %v rolledBack = %v", err, res.RolledBack)
	}
	for p, body := range gentle {
		if !exists(p) || slurp(t, p) != body {
			t.Errorf("%s must still be in place", p)
		}
	}
}
