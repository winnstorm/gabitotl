package cli

import (
	"os"
	"strings"
	"testing"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
)

// engramProjectFix is a fake machine with an initialized solo project on Engram
// and no Gentle-AI, so the interactive flow only asks the memory questions.
func engramProjectFix(t *testing.T) *cfix {
	t.Helper()
	f := newCfix(t)
	noGentle(t, f)
	if r := f.run("", false, "init", "--yes"); r.code != 0 {
		t.Fatalf("init: %+v", r)
	}
	return f
}

func noGentle(t *testing.T, f *cfix) {
	t.Helper()
	_ = os.RemoveAll(f.home + "/.gentle-ai")
	f.bins["gentle-ai"] = false
	cwrite(t, f.home+"/.claude/CLAUDE.md", cUser+"\n")
}

func (f *cfix) memoryMd() string { return slurpT(f.t, f.root+"/.gabitotl/memory.md") }

func (f *cfix) provider() string {
	cfg, err := gabitotl.LoadMemory(gabitotl.NewLayout(f.root))
	if err != nil {
		f.t.Fatal(err)
	}
	return cfg.Provider
}

func TestMemoryUseToTheSameProviderIsANoop(t *testing.T) {
	f := engramProjectFix(t)
	before := f.memoryMd()
	r := f.run("", false, "memory", "use", "engram")
	if r.code != 0 || !strings.Contains(r.out, "ya usa engram") || f.memoryMd() != before {
		t.Fatalf("%+v", r)
	}
}

func TestMemoryUseNeverMovesTeamMemoryBackToEngram(t *testing.T) {
	f := newCfix(t)
	if r := f.run("", false, "init", "--yes", "--mode", "team"); r.code != 0 {
		t.Fatalf("%+v", r)
	}
	before := f.memoryMd()
	r := f.run("", false, "memory", "use", "engram", "--yes")
	if r.code != ExitError || !strings.Contains(r.err, "no está soportado") || f.memoryMd() != before {
		t.Fatalf("the project must stay on memory-mcp: %+v", r)
	}
}

func TestMemoryUseRefusesWhatItCannotDoSafely(t *testing.T) {
	f := engramProjectFix(t)
	before := f.memoryMd()
	for name, args := range map[string][]string{
		"no role and no terminal": {"memory", "use", "memory-mcp", "--yes"},
		"migrate from a client":   {"memory", "use", "memory-mcp", "--yes", "--role", "client", "--url", "https://memoria.example.com/mcp", "--migrate"},
		"unknown provider":        {"memory", "use", "postgres"},
		"no provider":             {"memory", "use"},
	} {
		if r := f.run("", false, args...); r.code == 0 {
			t.Errorf("%s: want a refusal, got %+v", name, r)
		}
	}
	if f.memoryMd() != before {
		t.Fatal("a refusal changes nothing")
	}
}

func TestMemoryUseNeverBlocksWithoutATerminal(t *testing.T) {
	f := engramProjectFix(t)
	// No terminal and no --yes: it refuses instead of waiting for an answer.
	r := f.run("", false, "memory", "use", "memory-mcp", "--role", "client", "--url", "https://memoria.example.com/mcp")
	if r.code == 0 || f.provider() != "engram" {
		t.Fatalf("%+v", r)
	}
}

func TestMemoryUseDryRunChangesNothing(t *testing.T) {
	f := engramProjectFix(t)
	before := chash(t, f.home, f.root)
	r := f.run("", false, "memory", "use", "memory-mcp", "--role", "client", "--url", "https://memoria.example.com/mcp", "--dry-run")
	if r.code != 0 || !strings.Contains(r.out, ".gabitotl/memory.md") {
		t.Fatalf("%+v", r)
	}
	if chash(t, f.home, f.root) != before || f.provider() != "engram" {
		t.Fatal("a dry run changes nothing")
	}
}

func TestMemoryUseSwitchesToTeamMemoryAndWarnsAboutTeammates(t *testing.T) {
	f := engramProjectFix(t)
	r := f.run("", false, "memory", "use", "memory-mcp", "--yes", "--role", "client", "--url", "https://memoria.example.com/mcp")
	if r.code != 0 {
		t.Fatalf("%+v", r)
	}
	cfg, _ := gabitotl.LoadMemory(gabitotl.NewLayout(f.root))
	if cfg.Provider != "memory-mcp" || cfg.Mode != "team" || cfg.Role != "client" || cfg.URL != "https://memoria.example.com/mcp" {
		t.Fatalf("%+v", cfg)
	}
	if strings.Count(r.out, "tus compañeros la van a usar cuando hagan pull") != 1 {
		t.Fatalf("one advisory line is expected:\n%s", r.out)
	}
}

func TestInteractiveInstallOffersTheChangeOnceAndKeepsEngramByDefault(t *testing.T) {
	f := engramProjectFix(t)
	before := f.provider()
	// Answer: keep local memory, then do not apply.
	r := f.run("1\nn\n", true, "install", "--no-tui")
	if r.code != 0 {
		t.Fatalf("%+v", r)
	}
	if strings.Count(r.out, "Este proyecto usa memoria local (Engram). ¿Ahora trabajás con otras personas?") != 1 || strings.Contains(r.out, "¿Cómo te conectás") {
		t.Fatalf("one change question, no team questions:\n%s", r.out)
	}
	if f.provider() != before {
		t.Fatal("declining changes nothing")
	}
	// Non-interactively the same project is never asked.
	r = f.run("", false, "install", "--yes")
	if strings.Contains(r.out, "otras personas") || f.provider() != "engram" {
		t.Fatalf("--yes must not ask or change the provider: %+v", r)
	}
}

func TestInteractiveChangeAsksTheTeamSetupAndStaysWhenDeclined(t *testing.T) {
	f := engramProjectFix(t)
	// yes, I work with others -> not yet (stay with Engram) -> apply no.
	r := f.run("2\n3\nn\n", true, "install", "--no-tui")
	if r.code != 0 || !strings.Contains(r.out, "¿Cómo te conectás a la memoria compartida?") || f.provider() != "engram" {
		t.Fatalf("%+v", r)
	}
	// yes -> connect to an existing server -> URL -> default token variable -> apply yes.
	r = f.run("2\n1\nhttps://memoria.example.com/mcp\n\ns\n", true, "install", "--no-tui")
	if r.code != 0 || f.provider() != "memory-mcp" {
		t.Fatalf("%+v", r)
	}
	if !strings.Contains(r.out, "se comparte por Git") {
		t.Fatalf("missing advisory:\n%s", r.out)
	}
}

func TestInitAsksAloneOrWithOthersAndChainsTheTeamSetup(t *testing.T) {
	f := newCfix(t)
	noGentle(t, f)
	// with other people -> connect to an existing server -> URL -> default variable -> apply yes.
	r := f.run("2\n1\nhttps://memoria.example.com/mcp\n\ns\n", true, "init")
	if r.code != 0 {
		t.Fatalf("%+v", r)
	}
	cfg, _ := gabitotl.LoadMemory(gabitotl.NewLayout(f.root))
	if cfg.Provider != "memory-mcp" || cfg.Role != "client" || cfg.URL != "https://memoria.example.com/mcp" {
		t.Fatalf("%+v\n%s", cfg, r.out)
	}
	// Re-running init in the (now team) project does not offer the change.
	if r := f.run("", true, "init"); strings.Contains(r.out, "Este proyecto usa memoria local") {
		t.Fatalf("a team project is not offered the change: %s", r.out)
	}
}

func TestInitRerunOffersTheChangeForAnEngramProject(t *testing.T) {
	f := engramProjectFix(t)
	r := f.run("1\n", true, "init")
	if r.code != 0 || strings.Count(r.out, "Este proyecto usa memoria local (Engram)") != 1 || f.provider() != "engram" {
		t.Fatalf("%+v", r)
	}
}
