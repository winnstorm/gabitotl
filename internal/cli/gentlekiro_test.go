package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/winnstorm/gabitotl/internal/tui"
)

// gentleKiroFiles is a small copy of what Gentle AI leaves in a Kiro tree, plus
// files that are the user's and must never move.
var gentleKiroFiles = map[string]string{
	"steering/gentle-ai.md":     "---\ninclusion: always\n---\n\nPersona.\n",
	"skills/branch-pr/SKILL.md": "---\nname: branch-pr\nmetadata:\n  author: gentleman-programming\n---\n\nBody.\n",
	"skills/_shared/README.md":  "shared\n",
	"agents/sdd-apply.md":       "agent\n",
}

var userKiroFiles = map[string]string{
	"skills/frontend-design/SKILL.md": "---\nname: frontend-design\n---\n\nMine.\n",
	"agents/my-agent.md":              "mine\n",
	"steering/product.md":             "# Product\n",
}

// gentleKiroFix is a fake machine whose Kiro tree carries Gentle AI's files and
// whose project is already initialized, so the only open question is Kiro's.
func gentleKiroFix(t *testing.T) *cfix {
	t.Helper()
	f := newCfix(t)
	for rel, body := range gentleKiroFiles {
		cwrite(t, filepath.Join(f.home, ".kiro", filepath.FromSlash(rel)), body)
	}
	for rel, body := range userKiroFiles {
		cwrite(t, filepath.Join(f.home, ".kiro", filepath.FromSlash(rel)), body)
	}
	r := runIn(t, "", false, t.TempDir(), "init", "--root", f.root, "--name", "Shop", "--kind", "new",
		"--user-id", "gabi", "--user-name", "Gabi", "--yes")
	if r.code != 0 {
		t.Fatalf("init: %s", r.err)
	}
	return f
}

var kiroInstall = []string{"install", "--no-tui", "--runtime", "kiro-cli", "--memory", "engram", "--user", "gabi", "--name", "Gabi"}

func (f *cfix) gentleStays(t *testing.T) {
	t.Helper()
	for rel, body := range gentleKiroFiles {
		if b, err := os.ReadFile(filepath.Join(f.home, ".kiro", filepath.FromSlash(rel))); err != nil || string(b) != body {
			t.Errorf("Gentle AI's file must stay in place: %s (%v)", rel, err)
		}
	}
}

func (f *cfix) gentleGone(t *testing.T) {
	t.Helper()
	for rel := range gentleKiroFiles {
		if _, err := os.Stat(filepath.Join(f.home, ".kiro", filepath.FromSlash(rel))); err == nil {
			t.Errorf("Gentle AI's file must have moved to the backup: %s", rel)
		}
	}
	for rel, body := range userKiroFiles {
		if b, err := os.ReadFile(filepath.Join(f.home, ".kiro", filepath.FromSlash(rel))); err != nil || string(b) != body {
			t.Errorf("the user's file must stay untouched: %s (%v)", rel, err)
		}
	}
}

// The fake machine also has Gentle-AI itself, which opens the "what do you want
// to do" menu first: option 2 installs without touching it.
// acceptDelivery takes the default of the three delivery questions that an
// unconfigured project is asked once: platform, automation (up to the PR) and draft PRs.
const acceptDelivery = "\n\n\n"

const keepGentleAI = "2\n" + acceptDelivery

const kiroQuestion = "las dos están siempre activas"

func TestInstallAsksAndMovesGentleKiroFilesOnYes(t *testing.T) {
	f := gentleKiroFix(t)
	// Answers: keep Gentle-AI itself, yes to the Kiro question, yes to applying the replanned plan.
	r := f.run(keepGentleAI+"s\ns\n", true, kiroInstall...)
	if r.code != 0 {
		t.Fatalf("%+v", r)
	}
	for _, want := range []string{kiroQuestion, "skills: 1", "agentes: 1", "REMOVE AFTER VERIFY", "Pasé 4 archivo(s)", "gabitotl restore "} {
		if !strings.Contains(r.out, want) {
			t.Errorf("output lacks %q:\n%s", want, r.out)
		}
	}
	if strings.Contains(r.out, "corré gabitotl install --retire-gentle-kiro") {
		t.Errorf("after saying yes the note that explains the flag must not appear:\n%s", r.out)
	}
	// The replanned plan was printed before the apply question and lists what moves.
	if strings.Index(r.out, "REMOVE AFTER VERIFY") > strings.Index(r.out, "¿Aplico el plan?") {
		t.Errorf("the plan must list the removal before asking to apply:\n%s", r.out)
	}
	f.gentleGone(t)
}

func TestInstallKeepsBothOnNoAndOnEnter(t *testing.T) {
	for name, answer := range map[string]string{"no": "n\n", "enter": "\n"} {
		t.Run(name, func(t *testing.T) {
			f := gentleKiroFix(t)
			r := f.run(keepGentleAI+answer+"s\n", true, kiroInstall...)
			if r.code != 0 || !strings.Contains(r.out, kiroQuestion) {
				t.Fatalf("%+v", r)
			}
			if strings.Contains(r.out, "REMOVE AFTER VERIFY") || strings.Contains(r.out, "Pasé") {
				t.Errorf("declining must plan and move nothing:\n%s", r.out)
			}
			if !strings.Contains(r.out, "--retire-gentle-kiro") {
				t.Errorf("the note must tell how to do it later:\n%s", r.out)
			}
			f.gentleStays(t)
		})
	}
}

func TestInstallWithYesAloneNeverRetiresAndNeverAsks(t *testing.T) {
	f := gentleKiroFix(t)
	r := f.run("", false, append(kiroInstall, "--yes")...)
	if r.code != 0 {
		t.Fatalf("%+v", r)
	}
	if strings.Contains(r.out, kiroQuestion) || strings.Contains(r.out, "REMOVE AFTER VERIFY") {
		t.Fatalf("--yes must neither ask nor retire:\n%s", r.out)
	}
	if !strings.Contains(r.out, "corré gabitotl install --retire-gentle-kiro") {
		t.Fatalf("the note must name the flag:\n%s", r.out)
	}
	f.gentleStays(t)
}

func TestInstallWithTheFlagRetiresWithoutAsking(t *testing.T) {
	f := gentleKiroFix(t)
	r := f.run("", false, append(kiroInstall, "--yes", "--retire-gentle-kiro")...)
	if r.code != 0 {
		t.Fatalf("%+v", r)
	}
	if strings.Contains(r.out, kiroQuestion) || !strings.Contains(r.out, "Pasé 4 archivo(s)") {
		t.Fatalf("the flag answers the question:\n%s", r.out)
	}
	f.gentleGone(t)

	// A second run finds nothing: no question, no new snapshot, no change.
	before := chash(t, f.home, f.root)
	r = f.run("", false, append(kiroInstall, "--yes")...)
	if r.code != 0 || strings.Contains(r.out, kiroQuestion) || strings.Contains(r.out, "Encontré") {
		t.Fatalf("%+v", r)
	}
	if chash(t, f.home, f.root) != before {
		t.Fatal("re-running after the retirement must change nothing")
	}
}

func TestInteractiveRunAfterRetirementAsksNothing(t *testing.T) {
	f := gentleKiroFix(t)
	if r := f.run("", false, append(kiroInstall, "--yes", "--retire-gentle-kiro")...); r.code != 0 {
		t.Fatalf("%+v", r)
	}
	r := f.run(keepGentleAI+"s\n", true, kiroInstall...)
	if strings.Contains(r.out, kiroQuestion) {
		t.Fatalf("nothing is left to ask about:\n%s", r.out)
	}
}

func TestDryRunNeverAsksAndShowsThePlanTheFlagWouldMake(t *testing.T) {
	f := gentleKiroFix(t)
	before := chash(t, f.home, f.root)

	r := f.run("s\n", true, append(kiroInstall, "--dry-run")...)
	if r.code != 0 || strings.Contains(r.out, kiroQuestion) || strings.Contains(r.out, "REMOVE AFTER VERIFY") {
		t.Fatalf("dry run without the flag: no question, no removal:\n%+v", r)
	}
	if !strings.Contains(r.out, "--retire-gentle-kiro") {
		t.Fatalf("the note must name the flag:\n%s", r.out)
	}

	r = f.run("s\n", true, append(kiroInstall, "--dry-run", "--retire-gentle-kiro")...)
	if r.code != 0 || strings.Contains(r.out, kiroQuestion) || !strings.Contains(r.out, "REMOVE AFTER VERIFY") {
		t.Fatalf("dry run with the flag lists the removal without asking:\n%+v", r)
	}
	if chash(t, f.home, f.root) != before {
		t.Fatal("a dry run changes nothing")
	}
}

func TestTUIConfigCarriesTheRetireFlag(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want bool
	}{{[]string{"install"}, false}, {[]string{"install", "--retire-gentle-kiro"}, true}} {
		f := newCfix(t)
		old := tuiRunFn
		var got tui.Config
		tuiRunFn = func(cfg tui.Config, _ io.Reader, _ io.Writer) (tui.Outcome, error) {
			got = cfg
			return tui.Outcome{}, nil
		}
		t.Cleanup(func() { tuiRunFn = old })
		var out, errb bytes.Buffer
		Run(tc.args, Env{Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errb, Getwd: func() (string, error) { return f.root, nil }, IsTTY: true, IsOutTTY: true})
		if got.Options.RetireGentleKiro != tc.want {
			t.Fatalf("%v: RetireGentleKiro = %v, want %v", tc.args, got.Options.RetireGentleKiro, tc.want)
		}
	}
}
