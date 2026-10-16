package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/installer"
	"github.com/winnstorm/gabitotl/internal/memory"
	rt "github.com/winnstorm/gabitotl/internal/runtime"
)

const (
	cUser   = "# My rules\n\nBe kind.\n"
	cGentle = "<!-- gentle-ai:persona -->\nGentleman.\n<!-- /gentle-ai:persona -->\n"
	cOC     = "{\n  \"provider\": {\"anthropic\": {}}\n}\n"
	cState  = `{"version":"4.2.0","installed_agents":["claude-code","opencode"],"components":["persona"]}`
	cDoctor = `{"status":"ok","checks":[{"check_id":"db","result":"ok","message":"fine"}]}`
)

type cfix struct {
	t      *testing.T
	home   string
	root   string
	runner *installer.FakeRunner
	bins   map[string]bool
}

func cwrite(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func chash(t *testing.T, dirs ...string) string {
	t.Helper()
	h := sha256.New()
	for _, d := range dirs {
		var ps []string
		_ = filepath.WalkDir(d, func(p string, e fs.DirEntry, err error) error {
			if err == nil && !e.IsDir() {
				ps = append(ps, p)
			}
			return nil
		})
		sort.Strings(ps)
		for _, p := range ps {
			b, _ := os.ReadFile(p)
			h.Write([]byte(p + "\x00"))
			h.Write(b)
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

// newCfix builds a fake machine (home, runtime configs, Gentle-AI, Engram) and
// points every composition-root hook at it. Nothing real is read or run.
func newCfix(t *testing.T) *cfix {
	t.Helper()
	f := &cfix{t: t, home: t.TempDir(), root: t.TempDir(),
		bins: map[string]bool{"claude": true, "opencode": true, "kiro-cli": true, "git": true, "engram": true, "gentle-ai": true}}
	cwrite(t, f.home+"/.claude/CLAUDE.md", cUser+"\n"+cGentle)
	cwrite(t, f.home+"/.config/opencode/opencode.json", cOC)
	cwrite(t, f.home+"/.kiro/steering/product.md", "# Product\n")
	cwrite(t, f.home+"/.gentle-ai/state.json", cState)
	cwrite(t, f.home+"/.gentle-ai/backups/old.tgz", "backup")
	cwrite(t, f.home+"/.engram/engram.db", "DB")
	cwrite(t, f.root+"/go.mod", "module shop\n")
	cwrite(t, f.root+"/main.go", "package main\n")
	f.runner = &installer.FakeRunner{Handlers: map[string]func([]string) (string, error){
		"git": func(a []string) (string, error) {
			if len(a) > 0 && a[0] == "rev-parse" {
				return "true\n", nil
			}
			return "https://github.com/acme/shop.git\n", nil
		},
		"engram doctor":   func([]string) (string, error) { return cDoctor, nil },
		"engram projects": func([]string) (string, error) { return "  shop  7 obs  1 sessions  0 prompts\n", nil },
		"engram export": func(a []string) (string, error) {
			cwrite(t, a[1], `{}`)
			return "Exported to " + a[1] + "\n  Sessions: 1\n  Observations: 7\n  Prompts: 0\n", nil
		},
		"gentle-ai uninstall": func([]string) (string, error) {
			cwrite(t, f.home+"/.gentle-ai/uninstalled", "yes")
			return "ok\n", nil
		},
	}}
	oldEnv, oldRun, oldProv := runtimeEnvFn, installerRunnerFn, newProvider
	runtimeEnvFn = func(root string) (rt.Env, error) {
		return rt.Env{Home: f.home, ProjectRoot: root, StateDir: filepath.Join(f.home, ".gabitotl"), GOOS: "linux",
			Getenv: func(string) string { return "" },
			LookPath: func(n string) (string, error) {
				if f.bins[n] {
					return "/fake/bin/" + n, nil
				}
				return "", os.ErrNotExist
			},
			Exists: func(p string) bool { _, err := os.Stat(p); return err == nil },
			Now:    func() time.Time { return time.Date(2026, 10, 5, 21, 0, 0, 0, time.UTC) }}, nil
	}
	installerRunnerFn = func() installer.Runner { return f.runner }
	newProvider = func(gabitotl.MemoryConfig) (memory.Provider, error) { return &stubProvider{}, nil }
	t.Cleanup(func() { runtimeEnvFn, installerRunnerFn, newProvider = oldEnv, oldRun, oldProv })
	return f
}

func (f *cfix) run(stdin string, tty bool, args ...string) result {
	f.t.Helper()
	var out, errb bytes.Buffer
	code := Run(args, Env{Stdin: strings.NewReader(stdin), Stdout: &out, Stderr: &errb, Getwd: func() (string, error) { return f.root, nil }, IsTTY: tty})
	return result{code, out.String(), errb.String()}
}

func (f *cfix) calls(prefix string) int { return f.runner.Count(prefix) }

func TestInstallRefusesToMutateWithoutTTYOrYes(t *testing.T) {
	f := newCfix(t)
	before := chash(t, f.home, f.root)
	for _, cmd := range [][]string{{"install"}, {"migrate", "gentle-ai"}} {
		r := f.run("", false, cmd...)
		if r.code != ExitError || !strings.Contains(r.err, "Sin una terminal") {
			t.Fatalf("%v: %+v", cmd, r)
		}
	}
	if chash(t, f.home, f.root) != before || len(f.runner.Log()) != 0 {
		t.Fatal("a refused run must not even run commands")
	}
}

func TestInstallDryRunPrintsPlanAndChangesNothing(t *testing.T) {
	f := newCfix(t)
	before := chash(t, f.home, f.root)
	r := f.run("", false, "migrate", "gentle-ai", "--dry-run", "--remove-gentle-ai", "--migrate-persona", "--runtime", "claude-code", "--runtime", "opencode")
	if r.code != 0 {
		t.Fatalf("%+v", r)
	}
	last := -1
	for _, h := range []string{"CREATE", "MODIFY", "PRESERVE", "IMPORT", "REMOVE AFTER VERIFY", "NO ACTION"} {
		i := strings.Index(r.out, "\n"+h+"\n")
		if i < 0 || i < last {
			t.Fatalf("section %s missing or out of order:\n%s", h, r.out)
		}
		last = i
	}
	for _, want := range []string{"Plan:", ".gabitotl/project.md", "~/.claude/CLAUDE.md", "~/.engram/engram.db", "gentle-ai uninstall", "Esto fue un ensayo"} {
		if !strings.Contains(r.out, want) {
			t.Errorf("missing %q:\n%s", want, r.out)
		}
	}
	if chash(t, f.home, f.root) != before {
		t.Fatal("dry-run changed the tree")
	}
	for _, c := range f.runner.Log() {
		if strings.HasPrefix(c, "gentle-ai") || strings.HasPrefix(c, "engram export") || strings.HasPrefix(c, "engram save") {
			t.Fatalf("dry-run ran %q", c)
		}
	}
}

func TestInstallYesIsIdempotent(t *testing.T) {
	f := newCfix(t)
	r := f.run("", false, "install", "--yes", "--runtime", "claude-code", "--runtime", "opencode", "--user", "gabi")
	if r.code != 0 || !strings.Contains(r.out, "Listo.") {
		t.Fatalf("%+v", r)
	}
	md, _ := os.ReadFile(f.home + "/.claude/CLAUDE.md")
	if !strings.Contains(string(md), "gabitotl:start persona") || !strings.HasPrefix(string(md), cUser) {
		t.Fatalf("%s", md)
	}
	if u, err := os.ReadFile(f.root + "/.gabitotl/users/gabi.md"); err != nil || !strings.Contains(string(u), "gabi") {
		t.Fatalf("profile not saved: %v", err)
	}
	if _, err := os.Stat(f.root + "/.gabitotl/local/user.md"); err == nil {
		t.Fatal("install must not write a local identity file")
	}
	after := chash(t, f.home, f.root)
	r = f.run("", false, "install", "--yes", "--runtime", "claude-code", "--runtime", "opencode", "--user", "gabi")
	if r.code != 0 || !strings.Contains(r.out, "Ya estaba todo configurado") || chash(t, f.home, f.root) != after {
		t.Fatalf("second run must change nothing: %+v", r)
	}
}

func TestMigrateGentleAIEndToEndOrderDoctorRestoreUninstall(t *testing.T) {
	f := newCfix(t)
	engram := chash(t, f.home+"/.engram")
	backups := chash(t, f.home+"/.gentle-ai/backups")

	// 1. Migrate without removal: Gentle-AI stays.
	r := f.run("", false, "migrate", "gentle-ai", "--yes", "--preserve-engram", "--user", "gabi")
	if r.code != 0 || f.calls("gentle-ai") != 0 || !strings.Contains(r.out, "sigue instalado") {
		t.Fatalf("%+v log=%v", r, f.runner.Log())
	}
	// 2. Now remove Gentle-AI with its own mechanism (only after verification).
	f.runner.Calls = nil
	r = f.run("", false, "migrate", "gentle-ai", "--yes", "--remove-gentle-ai", "--migrate-persona", "--user", "gabi")
	if r.code != 0 {
		t.Fatalf("%+v", r)
	}
	log := f.runner.Log()
	doc := slices.IndexFunc(log, func(c string) bool { return strings.HasPrefix(c, "engram doctor") })
	exp := slices.IndexFunc(log, func(c string) bool { return strings.HasPrefix(c, "engram export") })
	un := slices.IndexFunc(log, func(c string) bool { return strings.HasPrefix(c, "gentle-ai uninstall") })
	if !(doc >= 0 && doc < exp && exp < un) || !strings.HasPrefix(log[len(log)-1], "engram doctor") {
		t.Fatalf("order must be doctor < export < uninstall, then a fresh doctor: %v", log)
	}
	for _, c := range log[un+1:] {
		if strings.HasPrefix(c, "gentle-ai") {
			t.Fatalf("uninstall must be Gentle-AI's last call: %v", log)
		}
	}
	if strings.Contains(slurpT(t, f.home+"/.claude/CLAUDE.md"), "gentle-ai") {
		t.Fatal("persona block must be retired")
	}
	if chash(t, f.home+"/.engram") != engram || chash(t, f.home+"/.gentle-ai/backups") != backups {
		t.Fatal("Engram and Gentle-AI backups must be preserved")
	}
	if !strings.Contains(slurpT(t, f.root+"/.gabitotl/imports.md"), "## Retirado") {
		t.Fatalf("imports.md lacks the Spanish report:\n%s", slurpT(t, f.root+"/.gabitotl/imports.md"))
	}

	// 3. doctor is read-only and healthy.
	snap := chash(t, f.home, f.root)
	r = f.run("", false, "doctor")
	if r.code != 0 || !strings.Contains(r.out, "Core") || !strings.Contains(r.out, "Legacy") || !strings.Contains(r.out, "✓") {
		t.Fatalf("%+v", r)
	}
	if chash(t, f.home, f.root) != snap {
		t.Fatal("doctor changed something")
	}

	// 4. restore lists, then restores only with --yes.
	r = f.run("", false, "restore")
	if r.code != 0 || !strings.Contains(r.out, "gentle-ai") {
		t.Fatalf("%+v", r)
	}
	ids := snapshotIDs(t, f.home)
	if len(ids) != 2 {
		t.Fatalf("ids = %v", ids)
	}
	if r := f.run("", false, "restore", ids[1]); r.code != ExitError || !strings.Contains(r.err, "Sin una terminal") {
		t.Fatalf("restore needs --yes without a terminal: %+v", r)
	}
	if r := f.run("", false, "restore", ids[1], "--dry-run"); r.code != 0 || !strings.Contains(r.out, "restaurar") {
		t.Fatalf("%+v", r)
	}
	if r := f.run("", false, "restore", ids[1], "--yes"); r.code != 0 || !strings.Contains(r.out, "Listo.") {
		t.Fatalf("%+v", r)
	}
	if !strings.Contains(slurpT(t, f.home+"/.claude/CLAUDE.md"), "gabitotl:start persona") {
		t.Fatal("restoring the snapshot taken right before the removal brings the migrated config back")
	}

	// 5. uninstall removes only GabitoTL's assets and keeps project, memory, backups.
	proj := chash(t, f.root+"/.gabitotl")
	r = f.run("", false, "uninstall", "--dry-run")
	if r.code != 0 || !strings.Contains(r.out, "Se eliminará:") || !strings.Contains(r.out, "Se conservará:") {
		t.Fatalf("%+v", r)
	}
	if r := f.run("", false, "uninstall"); r.code != ExitError {
		t.Fatalf("uninstall needs --yes without a terminal: %+v", r)
	}
	if r := f.run("", false, "uninstall", "--yes"); r.code != 0 {
		t.Fatalf("%+v", r)
	}
	if strings.Contains(slurpT(t, f.home+"/.claude/CLAUDE.md"), "gabitotl") || chash(t, f.root+"/.gabitotl") != proj || chash(t, f.home+"/.engram") != engram {
		t.Fatal("uninstall must remove only GabitoTL assets")
	}
}

func snapshotIDs(t *testing.T, home string) []string {
	t.Helper()
	es, err := os.ReadDir(home + "/.gabitotl/migrations")
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, e := range es {
		ids = append(ids, e.Name())
	}
	sort.Strings(ids)
	return ids
}

func slurpT(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestInteractiveFlowAsksOnlyWhatCannotBeDetected(t *testing.T) {
	f := newCfix(t)
	// 1 migrate | mode (default solo) | git (default no) | persona yes | apply yes | retire: do it
	in := "1\n\n\ns\ns\n1\n"
	r := f.run(in, true, "install")
	if r.code != 0 {
		t.Fatalf("%+v", r)
	}
	for _, forbidden := range []string{"¿Qué runtime", "¿Usás Engram", "¿Git está instalado", "proveedor Git"} {
		if strings.Contains(r.out, forbidden) {
			t.Errorf("asked something detectable: %q", forbidden)
		}
	}
	if strings.Contains(r.out, "¿Cómo querés que te llame?") || strings.Count(r.out, "¿Trabajás solo o con otras personas en este proyecto?") != 1 {
		t.Fatalf("each question exactly once:\n%s", r.out)
	}
	for _, want := range []string{"Esto es exactamente lo que voy a quitar", "[gentle-ai block persona]", "¿Aplico el plan?", "Gentle-AI ya no es necesario"} {
		if !strings.Contains(r.out, want) {
			t.Errorf("missing %q:\n%s", want, r.out)
		}
	}
	// The persona block and Gentle-AI are retired only after the plan was applied and verified.
	if f.calls("gentle-ai uninstall") != 1 || strings.Contains(slurpT(t, f.home+"/.claude/CLAUDE.md"), "gentle-ai:persona") {
		t.Fatalf("log=%v", f.runner.Log())
	}
	if !strings.Contains(slurpT(t, f.root+"/.gabitotl/users/gabi.md"), "gabi") {
		t.Fatal("OS-derived profile not created")
	}

	// Declining the persona question keeps the block; declining retirement keeps Gentle-AI.
	g := newCfix(t)
	r = g.run("1\n\n\nn\ns\n2\n", true, "install")
	if r.code != 0 || g.calls("gentle-ai") != 0 || !strings.Contains(slurpT(t, g.home+"/.claude/CLAUDE.md"), "gentle-ai:persona") {
		t.Fatalf("%+v log=%v", r, g.runner.Log())
	}
}

func TestInteractiveEndOfInputNeverChoosesForTheUser(t *testing.T) {
	f := newCfix(t)
	before := chash(t, f.home, f.root)
	r := f.run("", true, "install") // TTY but stdin ends immediately
	if r.code != 0 {
		t.Fatalf("%+v", r)
	}
	if chash(t, f.home, f.root) != before || f.calls("gentle-ai") != 0 {
		t.Fatal("a closed stdin must not apply or retire anything")
	}
}

func TestDoctorExitsNonZeroOnErrors(t *testing.T) {
	f := newCfix(t)
	if r := f.run("", false, "install", "--yes"); r.code != 0 {
		t.Fatalf("%+v", r)
	}
	if r := f.run("", false, "doctor"); r.code != 0 {
		t.Fatalf("healthy doctor must exit 0: %+v", r)
	}
	f.runner.Handlers["engram doctor"] = func([]string) (string, error) {
		return `{"status":"error","checks":[{"check_id":"db","result":"error","message":"corrupt"}]}`, nil
	}
	r := f.run("", false, "doctor")
	if r.code != ExitError || !strings.Contains(r.out, "×") {
		t.Fatalf("%+v", r)
	}
}

func TestCleanupRequiresConfirmationAndRespectsRetention(t *testing.T) {
	f := newCfix(t)
	if r := f.run("", false, "install", "--yes"); r.code != 0 {
		t.Fatalf("%+v", r)
	}
	if r := f.run("", false, "cleanup"); r.code != 0 || !strings.Contains(r.out, "nada vencido") {
		t.Fatalf("fresh snapshots are not expired: %+v", r)
	}
	old := f.home + "/.gabitotl/legacy/gentle-ai"
	cwrite(t, old+"/state.json", "{}")
	past := time.Now().AddDate(0, 0, -60)
	_ = os.Chtimes(old, past, past)
	if r := f.run("", false, "cleanup", "--days", "30"); r.code != ExitError || !strings.Contains(r.err, "Sin una terminal") {
		t.Fatalf("deleting needs confirmation: %+v", r)
	}
	if _, err := os.Stat(old); err != nil {
		t.Fatal("nothing may be deleted without confirmation")
	}
	if r := f.run("n\n", true, "cleanup", "--days", "30"); r.code != 0 {
		t.Fatalf("%+v", r)
	}
	if _, err := os.Stat(old); err != nil {
		t.Fatal("answering no must keep it")
	}
	if r := f.run("s\n", true, "cleanup", "--days", "30"); r.code != 0 || !strings.Contains(r.out, "Borré 1") {
		t.Fatalf("%+v", r)
	}
	if _, err := os.Stat(f.home + "/.gentle-ai/backups/old.tgz"); err != nil {
		t.Fatal("~/.gentle-ai/backups is never touched")
	}
}

func TestHomeWithoutArgsOnboardsOrShowsStatus(t *testing.T) {
	f := newCfix(t)
	r := f.run("", false)
	if r.code != 0 || !strings.Contains(r.out, "Usage: gabitotl") || !strings.Contains(r.out, "Sin una terminal") {
		t.Fatalf("no TTY: help plus the refusal, nothing changes: %+v", r)
	}
	if gabitotl.Exists(f.root) {
		t.Fatal("no .gabitotl may be created without a terminal")
	}
	if r := f.run("", false, "install", "--yes", "--user", "gabi"); r.code != 0 {
		t.Fatalf("%+v", r)
	}
	r = f.run("", false)
	if r.code != 0 || !strings.Contains(r.out, "GabitoTL en este proyecto") || !strings.Contains(r.out, "Identidad: gabi") || !strings.Contains(r.out, "engram") {
		t.Fatalf("%+v", r)
	}
}

func TestWorkUpdatePrintsSoftLockAdvisory(t *testing.T) {
	root := initProject(t)
	for _, args := range [][]string{
		{"work", "start", "--root", root, "--user", "ana", "--task", "t1", "--areas", "src/auth"},
		{"work", "start", "--root", root, "--user", "bob", "--task", "t2"},
	} {
		if r := runIn(t, "", false, root, args...); r.code != 0 {
			t.Fatalf("%v: %+v", args, r)
		}
	}
	r := runIn(t, "", false, root, "work", "update", "--root", root, "--user", "bob", "--task", "t2", "--areas", "src/auth")
	if r.code != 0 || !strings.Contains(r.out, "overlap with ana") || !strings.Contains(r.out, "Ojo") || !strings.Contains(r.out, "Conviene coordinar") {
		t.Fatalf("the advisory must be printed and must not block: %+v", r)
	}
	r = runIn(t, "", false, root, "work", "update", "--root", root, "--user", "bob", "--task", "t2", "--areas", "docs")
	if r.code != 0 || strings.Contains(r.out, "overlap with") {
		t.Fatalf("no overlap, no advisory: %+v", r)
	}
}

func TestDevNullIsNotATerminal(t *testing.T) {
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if isTerminal(f) {
		t.Fatal("/dev/null is a character device but not a terminal: `< /dev/null` must count as no TTY")
	}
}
