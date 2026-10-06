package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func contains(t *testing.T, what, s string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(s, w) {
			t.Errorf("%s lacks %q:\n%s", what, w, s)
		}
	}
}

func absent(t *testing.T, what, s string, bad ...string) {
	t.Helper()
	for _, w := range bad {
		if strings.Contains(s, w) {
			t.Errorf("%s must not contain %q:\n%s", what, w, s)
		}
	}
}

// (a) A new project is onboarded entirely through flags, with no terminal.
func TestNewProjectOnboardingByFlags(t *testing.T) {
	m := newMachine(t)
	proj := m.repo("fresh", nil) // empty repository
	m.write(filepath.Join(proj, "README.md"), "# fresh\n")

	// Without a terminal the bare command refuses to onboard and changes nothing.
	r := m.run(proj)
	contains(t, "bare gabitotl without a TTY", r.out+r.err, "Usage: gabitotl", "Sin una terminal")
	if exists(filepath.Join(proj, ".gabitotl")) {
		t.Fatal("no .gabitotl may be created without a terminal")
	}

	r = m.ok(proj, "init", "--yes", "--user-name", "Gabi Dev", "--mode", "solo")
	contains(t, "init output", r.out, "identity: gabi-dev")
	for _, f := range []string{"project.md", "memory.md", "runtime.md", "vcs.md", "workflow.md", "imports.md", ".gitignore", "local/user.md"} {
		if !exists(filepath.Join(proj, ".gabitotl", f)) {
			t.Errorf(".gabitotl/%s was not created", f)
		}
	}
	// A README alone is not enough to call a project "existing".
	contains(t, "project.md", read(t, filepath.Join(proj, ".gabitotl/project.md")), "kind: new")
	contains(t, ".gabitotl/.gitignore", read(t, filepath.Join(proj, ".gabitotl/.gitignore")), "local/", ".bak/")
	contains(t, "memory.md", read(t, filepath.Join(proj, ".gabitotl/memory.md")), "mode: solo", "provider: engram")

	// Idempotent: a second init creates nothing and keeps the identity.
	before := hash(t, nil, proj)
	r = m.ok(proj, "init", "--yes")
	contains(t, "second init", r.out, "created: 0", "identity: unchanged")
	if hash(t, nil, proj) != before {
		t.Fatal("a second init changed files")
	}

	// With a project present, the bare command prints a short status.
	r = m.ok(proj)
	contains(t, "status", r.out, "GabitoTL en este proyecto", "Identidad: Gabi Dev")
}

// (b, CLI half) An existing project is detected, scanned, initialized and its
// memory is healthy. Saving the project map is covered in TestProjectMapSavedThroughEngram.
func TestExistingProjectScanInitMemoryDoctor(t *testing.T) {
	m := newMachine(t)
	proj := m.repo("shop", map[string]string{
		"go.mod":                 "module shop\n\ngo 1.22\n",
		"main.go":                "package main\n\nfunc main() {}\n",
		"internal/cart/cart.go":  "package cart\n",
		"README.md":              "# shop\n\nA small shop.\n",
		"docs/ARCHITECTURE.md":   "# Architecture\n\nLayered.\n",
		".env":                   "API_TOKEN=do-not-read-me-0123456789\n",
		"internal/cart/notes.md": "notes\n",
	})
	m.git(proj, "remote", "add", "origin", "https://github.com/acme/shop.git")

	before := hash(t, nil, proj)
	r := m.ok(proj, "scan")
	contains(t, "scan", r.out, "Go")
	absent(t, "scan", r.out, "do-not-read-me")
	j := m.ok(proj, "scan", "--json")
	contains(t, "scan --json", j.out, `"stack"`)
	absent(t, "scan --json", j.out, "do-not-read-me")
	if hash(t, nil, proj) != before {
		t.Fatal("scan must be read-only")
	}

	m.ok(proj, "init", "--yes", "--user-name", "Ana", "--vcs", "github")
	contains(t, "project.md", read(t, filepath.Join(proj, ".gabitotl/project.md")), "kind: existing")

	r = m.ok(proj, "memory", "doctor")
	contains(t, "memory doctor", r.out, "provider: engram", "[ok] engram", "[ok] db: fine")
	if m.count("engram doctor --json") == 0 {
		t.Fatalf("memory doctor must go through the engram CLI: %v", m.calls())
	}

	r = m.ok(proj, "vcs", "status")
	contains(t, "vcs status", r.out, "provider: github", "branch: main")
}

// (d) A backup is taken before modifying files and restores them exactly.
func TestBackupBeforeModifyAndRestore(t *testing.T) {
	m := newMachine(t)
	proj := m.repo("app", map[string]string{"src/app.txt": "original\n", "README.md": "# app\n"})
	m.ok(proj, "init", "--yes", "--user-name", "Ana")
	m.write(filepath.Join(proj, ".env"), "DB_PASSWORD=hunter2hunter2\n")
	m.write(filepath.Join(proj, "src/app.txt"), "original\n")

	r := m.ok(proj, "backup", "create", "--task", "t1", "--reason", "refactor", "src/app.txt", ".env")
	contains(t, "backup create", r.out, "snapshot: ana/", "src/app.txt")
	absent(t, "backup create", r.out, ".env,", ", .env")
	contains(t, "backup create skips secrets", r.err, "skipped .env")

	// The snapshot never contains the secret-looking file.
	bak := filepath.Join(proj, ".gabitotl/.bak")
	var found []string
	_ = filepath.WalkDir(bak, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			found = append(found, p)
		}
		return nil
	})
	for _, p := range found {
		if strings.HasSuffix(p, ".env") {
			t.Fatalf("a secret file was backed up: %s", p)
		}
	}

	// Modify, then restore.
	m.write(filepath.Join(proj, "src/app.txt"), "broken by an agent\n")
	m.ok(proj, "backup", "list")
	r = m.ok(proj, "backup", "restore", "latest")
	contains(t, "restore", r.out, "restored from", "src/app.txt")
	if got := read(t, filepath.Join(proj, "src/app.txt")); got != "original\n" {
		t.Fatalf("restore did not bring the original back: %q", got)
	}

	// Paths outside the project root are refused.
	outside := filepath.Join(m.root, "outside.txt")
	m.write(outside, "x")
	if r := m.run(proj, "backup", "create", "--task", "t2", outside); r.code == 0 {
		t.Fatalf("a path outside the root must be refused: %+v", r)
	}
}

// (e) install --dry-run is a no-op; install configures all four runtimes
// idempotently; uninstall gives the user their content back.
func TestInstallDryRunIdempotenceAndUninstall(t *testing.T) {
	m := newMachine(t)
	proj := m.repo("shop", map[string]string{"go.mod": "module shop\n", "main.go": "package main\n"})
	const userMD = "# My rules\n\nBe kind.\n"
	const userOC = "{\n  // keep me\n  \"provider\": {\"anthropic\": {}}\n}\n"
	m.write(filepath.Join(m.home, ".claude/CLAUDE.md"), userMD)
	m.write(filepath.Join(m.home, ".config/opencode/opencode.json"), userOC)
	m.write(filepath.Join(m.home, ".kiro/steering/product.md"), "# Product\n")
	m.write(filepath.Join(m.home, ".engram/engram.db"), "DB")
	runtimes := []string{"--runtime", "claude-code", "--runtime", "opencode", "--runtime", "kiro-cli", "--runtime", "kiro-ide"}
	skipLog := []string{m.dir}
	original := hash(t, skipLog, m.home, proj)

	// Dry run: plan printed, nothing changed, no mutating command executed.
	args := append([]string{"install", "--dry-run", "--user", "gabi"}, runtimes...)
	r := m.ok(proj, args...)
	contains(t, "dry-run", r.out, "Plan:", "~/.claude/CLAUDE.md", "Esto fue un ensayo")
	if hash(t, skipLog, m.home, proj) != original {
		t.Fatal("install --dry-run changed the tree")
	}
	for _, c := range m.calls() {
		if strings.HasPrefix(c, "engram export") || strings.HasPrefix(c, "engram save") || strings.HasPrefix(c, "gentle-ai") {
			t.Fatalf("dry-run ran a mutating command: %q", c)
		}
	}

	// Without --yes and without a terminal install refuses.
	if r := m.run(proj, append([]string{"install", "--user", "gabi"}, runtimes...)...); r.code == 0 || !strings.Contains(r.err, "Sin una terminal") {
		t.Fatalf("install without TTY or --yes must refuse: %+v", r)
	}

	args = append([]string{"install", "--yes", "--user", "gabi"}, runtimes...)
	r = m.ok(proj, args...)
	contains(t, "install", r.out, "Listo.")

	md := read(t, filepath.Join(m.home, ".claude/CLAUDE.md"))
	if !strings.HasPrefix(md, userMD) || !strings.Contains(md, "gabitotl:start persona") {
		t.Fatalf("claude persona must be a marked section after the user's content:\n%s", md)
	}
	oc := read(t, filepath.Join(m.home, ".config/opencode/opencode.json"))
	contains(t, "opencode.json", oc, "// keep me", `"anthropic"`, "gabitotl-persona.md", `"engram"`)
	for _, f := range []string{".claude.json", ".kiro/steering/gabitotl.md", ".kiro/settings/mcp.json", ".config/opencode/gabitotl-persona.md"} {
		if !exists(filepath.Join(m.home, f)) {
			t.Errorf("%s was not created", f)
		}
	}
	if !exists(filepath.Join(proj, ".gabitotl/local/user.md")) {
		t.Error("identity not saved")
	}

	// Idempotent: second run changes nothing.
	after := hash(t, skipLog, m.home, proj)
	r = m.ok(proj, args...)
	contains(t, "second install", r.out, "Ya estaba todo configurado")
	if hash(t, skipLog, m.home, proj) != after {
		t.Fatal("a second install changed files")
	}

	// doctor is read-only and healthy.
	r = m.ok(proj, "doctor")
	contains(t, "doctor", r.out, "Todo en orden")
	if hash(t, skipLog, m.home, proj) != after {
		t.Fatal("doctor changed files")
	}

	// uninstall removes only what GabitoTL installed.
	m.ok(proj, "uninstall", "--dry-run")
	if hash(t, skipLog, m.home, proj) != after {
		t.Fatal("uninstall --dry-run changed files")
	}
	if r := m.run(proj, "uninstall"); r.code == 0 {
		t.Fatalf("uninstall without TTY or --yes must refuse: %+v", r)
	}
	m.ok(proj, "uninstall", "--yes")
	if got := read(t, filepath.Join(m.home, ".claude/CLAUDE.md")); strings.Contains(got, "gabitotl") || !strings.HasPrefix(got, userMD) {
		t.Fatalf("uninstall must leave the user's CLAUDE.md as it was:\n%s", got)
	}
	if got := read(t, filepath.Join(m.home, ".config/opencode/opencode.json")); strings.Contains(got, "gabitotl") || !strings.Contains(got, "// keep me") {
		t.Fatalf("opencode.json must lose only GabitoTL keys:\n%s", got)
	}
	for _, f := range []string{".kiro/steering/gabitotl.md", ".config/opencode/gabitotl-persona.md"} {
		if exists(filepath.Join(m.home, f)) {
			t.Errorf("%s created by GabitoTL must be removed", f)
		}
	}
	if !exists(filepath.Join(m.home, ".kiro/steering/product.md")) || read(t, filepath.Join(m.home, ".engram/engram.db")) != "DB" {
		t.Fatal("user files and Engram data must be kept")
	}
	if !exists(filepath.Join(proj, ".gabitotl/project.md")) {
		t.Fatal("uninstall must keep the project's .gabitotl")
	}
}

const gentleBlock = "<!-- gentle-ai:persona -->\nGentleman persona.\n<!-- /gentle-ai:persona -->\n"

// gentleMachine builds a machine with a Gentle-AI install managing agents.
func gentleMachine(t *testing.T, agents string) (*machine, string) {
	m := newMachine(t)
	proj := m.repo("shop", map[string]string{"go.mod": "module shop\n", "main.go": "package main\n"})
	m.write(filepath.Join(m.home, ".claude/CLAUDE.md"), "# My rules\n\nBe kind.\n\n"+gentleBlock)
	m.write(filepath.Join(m.home, ".config/opencode/opencode.json"), "{\n  \"provider\": {}\n}\n")
	m.write(filepath.Join(m.home, ".gentle-ai/state.json"), `{"version":"4.2.0","installed_agents":[`+agents+`],"components":["persona"]}`)
	m.write(filepath.Join(m.home, ".gentle-ai/backups/old.tgz"), "backup")
	m.write(filepath.Join(m.home, ".engram/engram.db"), "DB")
	return m, proj
}

// (f) Migration from Gentle-AI: ordered invocations, persona retirement,
// everything user-owned preserved.
func TestMigrateFromGentleAIOrderAndPersonaRetirement(t *testing.T) {
	m, proj := gentleMachine(t, `"claude-code","opencode"`)
	engram := hash(t, nil, filepath.Join(m.home, ".engram"))
	backups := hash(t, nil, filepath.Join(m.home, ".gentle-ai/backups"))
	runtimes := []string{"--runtime", "claude-code", "--runtime", "opencode"}

	// Dry run first: nothing runs, nothing changes.
	before := hash(t, []string{m.dir}, m.home, proj)
	args := append([]string{"migrate", "gentle-ai", "--dry-run", "--remove-gentle-ai", "--migrate-persona"}, runtimes...)
	r := m.ok(proj, args...)
	contains(t, "migrate dry-run", r.out, "gentle-ai uninstall --all", "REMOVE AFTER VERIFY")
	if hash(t, []string{m.dir}, m.home, proj) != before || m.count("gentle-ai") != 0 {
		t.Fatal("dry-run must change nothing and never call gentle-ai")
	}

	// Without --migrate-persona the Gentleman block stays (the user decides).
	args = append([]string{"migrate", "gentle-ai", "--yes", "--user", "gabi"}, runtimes...)
	m.ok(proj, args...)
	if !strings.Contains(read(t, filepath.Join(m.home, ".claude/CLAUDE.md")), "gentle-ai:persona") {
		t.Fatal("the foreign persona block must stay without --migrate-persona")
	}
	if m.count("gentle-ai") != 0 {
		t.Fatalf("Gentle-AI must not be touched without --remove-gentle-ai: %v", m.calls())
	}

	// Full soft landing.
	m.ok(proj, append([]string{"migrate", "gentle-ai", "--yes", "--remove-gentle-ai", "--migrate-persona", "--user", "gabi"}, runtimes...)...)
	calls := m.calls()
	doc := index(calls, "engram doctor", 0)
	exp := index(calls, "engram export", 0)
	un := index(calls, "gentle-ai uninstall --all", 0)
	if !(doc >= 0 && doc < exp && exp < un) {
		t.Fatalf("order must be engram doctor < engram export < gentle-ai uninstall --all: %v", calls)
	}
	if lastIndex(calls, "gentle-ai") != un || lastIndex(calls, "engram doctor") < un {
		t.Fatalf("gentle-ai uninstall must be the last gentle-ai call and be followed by a fresh engram doctor: %v", calls)
	}
	if index(calls, "gentle-ai uninstall --agent", 0) >= 0 {
		t.Fatalf("the unverified per-agent form must never run: %v", calls)
	}

	md := read(t, filepath.Join(m.home, ".claude/CLAUDE.md"))
	if strings.Contains(md, "gentle-ai") || !strings.HasPrefix(md, "# My rules\n\nBe kind.\n") || !strings.Contains(md, "gabitotl:start persona") {
		t.Fatalf("only the Gentleman block may be replaced:\n%s", md)
	}
	if hash(t, nil, filepath.Join(m.home, ".engram")) != engram || hash(t, nil, filepath.Join(m.home, ".gentle-ai/backups")) != backups {
		t.Fatal("Engram data and ~/.gentle-ai/backups must be preserved")
	}
	imports := read(t, filepath.Join(proj, ".gabitotl/imports.md"))
	contains(t, "imports.md", imports, "gentle-ai")
	// The snapshot taken before any change lets the user go back.
	r = m.ok(proj, "restore")
	contains(t, "restore listing", r.out, "gentle-ai")
}

// (f) When Gentle-AI also manages an agent GabitoTL does not replace, its
// uninstall is not run: the user gets an OJO and does it manually.
func TestMigrateDoesNotUninstallGentleAIWhenAnAgentIsNotReplaced(t *testing.T) {
	m, proj := gentleMachine(t, `"claude-code","opencode","windsurf"`)
	r := m.ok(proj, "migrate", "gentle-ai", "--yes", "--remove-gentle-ai", "--migrate-persona", "--user", "gabi",
		"--runtime", "claude-code", "--runtime", "opencode")
	if m.count("gentle-ai") != 0 {
		t.Fatalf("gentle-ai uninstall must not run when --all would remove an unreplaced agent: %v", m.calls())
	}
	contains(t, "migrate output", r.out+r.err, "Gentle-AI", "desinstalador propio")
	// The persona block is still retired through GabitoTL's own adapter.
	if strings.Contains(read(t, filepath.Join(m.home, ".claude/CLAUDE.md")), "gentle-ai:persona") {
		t.Fatal("the persona block should have been retired")
	}
}
