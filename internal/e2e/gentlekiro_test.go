package e2e

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var e2eGentleKiro = map[string]string{
	"steering/gentle-ai.md":                         "---\ninclusion: always\n---\n\nPersona.\n<!-- gentle-ai:orchestrator -->\nx\n<!-- /gentle-ai:orchestrator -->\n",
	"skills/branch-pr/SKILL.md":                     "---\nname: branch-pr\nmetadata:\n  author: gentleman-programming\n---\n\nBody.\n",
	"skills/branch-pr/references/checklist.md":      "check\n",
	"skills/_shared/persistence-contract.md":        "shared\n",
	"agents/sdd-apply.md":                           "agent\n",
	"agents/.gentle-ai-native-agent-ownership.json": `{"version":1,"files":{"jd-judge-a.md":"abc"}}`,
	"agents/jd-judge-a.md":                          "judge\n",
}

var e2eUserKiro = map[string]string{
	"skills/frontend-design/SKILL.md": "---\nname: frontend-design\n---\n\nMine.\n",
	"agents/my-agent.md":              "mine\n",
	"steering/product.md":             "# Product\n",
	"settings/mcp.json":               "{\n  \"mcpServers\": {\"engram\": {\"command\": \"/opt/engram\", \"args\": [\"mcp\"]}}\n}\n",
}

// Kiro with Gentle AI's files: nothing moves without consent, the flag moves them
// into a snapshot after verification, doctor warns only while both coexist, and
// `gabitotl restore <id>` brings every file back byte for byte.
func TestKiroGentleAIFilesMoveToABackupAndComeBackByteForByte(t *testing.T) {
	m := newMachine(t)
	proj := m.repo("shop", map[string]string{"go.mod": "module shop\n", "main.go": "package main\n"})
	kiro := filepath.Join(m.home, ".kiro")
	for rel, body := range e2eGentleKiro {
		m.write(filepath.Join(kiro, filepath.FromSlash(rel)), body)
	}
	for rel, body := range e2eUserKiro {
		m.write(filepath.Join(kiro, filepath.FromSlash(rel)), body)
	}
	install := []string{"install", "--yes", "--user", "gabi", "--runtime", "kiro-cli"}

	// --yes alone keeps both and says how to retire Gentle AI's files.
	r := m.ok(proj, install...)
	contains(t, "install without the flag", r.out, "--retire-gentle-kiro")
	for rel, body := range e2eGentleKiro {
		if read(t, filepath.Join(kiro, filepath.FromSlash(rel))) != body {
			t.Fatalf("%s must stay without the flag", rel)
		}
	}
	// Doctor warns (not fails) about the two always-on instruction sets, with the fix.
	d := m.run(proj, "doctor")
	contains(t, "doctor with both", d.out, "gabitotl install --runtime kiro-cli --retire-gentle-kiro")

	userBefore := map[string]string{}
	for rel := range e2eUserKiro {
		if rel != "settings/mcp.json" { // GabitoTL adds its own entry there; the user's stays
			userBefore[rel] = read(t, filepath.Join(kiro, filepath.FromSlash(rel)))
		}
	}

	// The flag moves them after verification and names the snapshot.
	r = m.ok(proj, append(install, "--retire-gentle-kiro")...)
	contains(t, "install with the flag", r.out, "Pasé 7 archivo(s)", "gabitotl restore ")
	for rel := range e2eGentleKiro {
		if exists(filepath.Join(kiro, filepath.FromSlash(rel))) {
			t.Errorf("%s should have moved to the backup", rel)
		}
	}
	for rel, body := range userBefore {
		if read(t, filepath.Join(kiro, filepath.FromSlash(rel))) != body {
			t.Errorf("the user's %s changed", rel)
		}
	}
	if !strings.Contains(read(t, filepath.Join(kiro, "settings/mcp.json")), `"/opt/engram"`) {
		t.Fatal("the user's MCP entry must stay")
	}
	id := regexp.MustCompile(`gabitotl restore (\S+)`).FindStringSubmatch(r.out)
	if id == nil {
		t.Fatalf("the result must name the snapshot:\n%s", r.out)
	}
	if d := m.run(proj, "doctor"); strings.Contains(d.out, "--retire-gentle-kiro") {
		t.Fatalf("nothing coexists anymore, doctor must say nothing about it:\n%s", d.out)
	}

	// Re-running asks nothing and creates no second snapshot.
	snaps := m.ok(proj, "restore").out
	r = m.ok(proj, install...)
	if strings.Contains(r.out, "Encontré") || m.ok(proj, "restore").out != snaps {
		t.Fatalf("a re-run after the retirement must be silent and snapshot-free:\n%s", r.out)
	}

	// Restore puts Gentle AI's files back, byte for byte.
	m.ok(proj, "restore", id[1], "--yes")
	for rel, body := range e2eGentleKiro {
		if !exists(filepath.Join(kiro, filepath.FromSlash(rel))) || read(t, filepath.Join(kiro, filepath.FromSlash(rel))) != body {
			t.Errorf("%s was not restored byte for byte", rel)
		}
	}
}
