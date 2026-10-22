package installer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sysEnv(t *testing.T, goos string, bins map[string]string) Env {
	t.Helper()
	return Env{Home: t.TempDir(), GOOS: goos, Getenv: func(string) string { return "" },
		LookPath: func(n string) (string, error) {
			if p, ok := bins[n]; ok {
				return p, nil
			}
			return "", os.ErrNotExist
		}}
}

func put(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func sysByID(t *testing.T, l []SystemStatus, id string) SystemStatus {
	t.Helper()
	for _, s := range l {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("system %s not listed", id)
	return SystemStatus{}
}

func TestGlobalSystemsAreAlwaysListedWhenNoneIsFound(t *testing.T) {
	l := DetectSystems(sysEnv(t, "linux", nil))
	for _, id := range []string{"gentle-ai", "superclaude", "spec-kit", "openspec", "task-master", "ruflo", "omo"} {
		s := sysByID(t, l, id)
		if s.Found || s.Possible || len(s.Signals) == 0 {
			t.Fatalf("%s: %+v", id, s)
		}
		for _, sg := range s.Signals {
			if sg.Path == "" && sg.Kind != SigBinary {
				t.Fatalf("%s must say where it looked: %+v", id, sg)
			}
		}
	}
	for _, s := range l {
		if s.ID == "bmad" || s.ID == "ccpm" {
			t.Fatalf("%s is project-only", s.ID)
		}
	}
}

func TestGlobalBinaryDetectors(t *testing.T) {
	for id, bin := range map[string]string{"superclaude": "superclaude", "spec-kit": "specify", "openspec": "openspec", "task-master": "task-master", "ruflo": "claude-flow", "omo": "omo"} {
		l := DetectSystems(sysEnv(t, "linux", map[string]string{bin: "/usr/bin/" + bin}))
		if s := sysByID(t, l, id); !s.Found || s.Possible {
			t.Fatalf("%s via %s: %+v", id, bin, s)
		}
		for _, o := range l {
			if o.ID != id && o.Found {
				t.Fatalf("%s must not light up %s", bin, o.ID)
			}
		}
	}
}

func TestWindowsCmdShimIsFound(t *testing.T) {
	env := sysEnv(t, "windows", map[string]string{"task-master.cmd": `C:\Users\u\AppData\Roaming\npm\task-master.cmd`})
	s := sysByID(t, DetectSystems(env), "task-master")
	if !s.Found || !strings.HasSuffix(s.Signals[0].Path, "task-master.cmd") {
		t.Fatalf("%+v", s)
	}
	if s := sysByID(t, DetectSystems(sysEnv(t, "linux", map[string]string{"task-master.cmd": "x"})), "task-master"); s.Found {
		t.Fatal("the .cmd retry is Windows only")
	}
}

func TestGlobalDirFileAndMarkerDetectors(t *testing.T) {
	env := sysEnv(t, "linux", nil)
	put(t, filepath.Join(env.Home, ".claude/commands/sc/analyze.md"), "x")
	put(t, filepath.Join(env.Home, ".omo/omo.jsonc"), "{}")
	put(t, filepath.Join(env.Home, ".claude/CLAUDE.md"), "# mine\n"+openspecMarker+"\nrules\n<!-- OPENSPEC:END -->\n")
	l := DetectSystems(env)
	for _, id := range []string{"superclaude", "omo", "openspec"} {
		if !sysByID(t, l, id).Found {
			t.Fatalf("%s not found: %+v", id, sysByID(t, l, id))
		}
	}
	if sysByID(t, l, "superclaude").Conflict != ConflictMedium || sysByID(t, l, "superclaude").Action != ActionReview {
		t.Fatal("SuperClaude adds commands under ~/.claude: review")
	}
	// The OpenCode global AGENTS.md follows XDG_CONFIG_HOME.
	env2 := sysEnv(t, "linux", nil)
	xdg := t.TempDir()
	env2.Getenv = func(k string) string {
		if k == "XDG_CONFIG_HOME" {
			return xdg
		}
		return ""
	}
	put(t, filepath.Join(xdg, "opencode/AGENTS.md"), openspecMarker)
	if !sysByID(t, DetectSystems(env2), "openspec").Found {
		t.Fatal("marker in the OpenCode AGENTS.md")
	}
}

func TestMarkerScanIsBounded(t *testing.T) {
	env := sysEnv(t, "linux", nil)
	put(t, filepath.Join(env.Home, ".claude/CLAUDE.md"), strings.Repeat("a", markerScanLimit)+openspecMarker)
	if sysByID(t, DetectSystems(env), "openspec").Found {
		t.Fatal("a marker past the read limit must not be found")
	}
}

func TestGentleAIListedLikeTheOldDetector(t *testing.T) {
	f := newFixture(t)
	o := DetectOtherHarness(f.env)
	if g := sysByID(t, o.Systems, "gentle-ai"); g.Found != o.GentleAI.Found || !g.Found || g.Action != ActionDecide {
		t.Fatalf("%+v", g)
	}
}

func TestProjectSystems(t *testing.T) {
	env := sysEnv(t, "linux", nil)
	root := t.TempDir()
	if got := DetectProjectSystems(env, ""); got != nil {
		t.Fatal("no project, no list")
	}
	l := DetectProjectSystems(env, root)
	for _, s := range l {
		if s.Found || s.Possible {
			t.Fatalf("empty project: %+v", s)
		}
	}
	put(t, filepath.Join(root, ".specify/scripts/x.sh"), "")
	put(t, filepath.Join(root, "openspec/specs/a.md"), "")
	put(t, filepath.Join(root, "_bmad/core.md"), "")
	put(t, filepath.Join(root, ".taskmaster/tasks.json"), "{}")
	put(t, filepath.Join(root, ".claude-flow/c.json"), "{}")
	put(t, filepath.Join(root, ".omo/omo.jsonc"), "{}")
	put(t, filepath.Join(root, ".claude/prds/a.md"), "")
	l = DetectProjectSystems(env, root)
	for _, id := range []string{"spec-kit", "openspec", "bmad", "task-master", "ruflo", "omo"} {
		if !sysByID(t, l, id).Found {
			t.Fatalf("%s: %+v", id, sysByID(t, l, id))
		}
	}
	if s := sysByID(t, l, "ccpm"); s.Found || s.Possible {
		t.Fatalf("prds alone is not ccpm: %+v", s)
	}
	put(t, filepath.Join(root, ".claude/epics/e.md"), "")
	if s := sysByID(t, DetectProjectSystems(env, root), "ccpm"); s.Found || !s.Possible {
		t.Fatalf("prds + epics is a possible ccpm: %+v", s)
	}
}

func TestProjectWeakSignalsArePossible(t *testing.T) {
	env := sysEnv(t, "linux", nil)
	root := t.TempDir()
	put(t, filepath.Join(root, ".claude/skills/speckit-plan/SKILL.md"), "")
	put(t, filepath.Join(root, ".claude/commands/opsx/a.md"), "")
	put(t, filepath.Join(root, ".bmad-core/x.md"), "")
	put(t, filepath.Join(root, "CLAUDE.md"), "x")
	l := DetectProjectSystems(env, root)
	for _, id := range []string{"spec-kit", "openspec", "bmad"} {
		if s := sysByID(t, l, id); s.Found || !s.Possible {
			t.Fatalf("%s: %+v", id, s)
		}
	}
	put(t, filepath.Join(root, "AGENTS.md"), "<!-- OPENSPEC:START -->")
	if s := sysByID(t, DetectProjectSystems(env, root), "openspec"); !s.Found || s.Possible {
		t.Fatalf("marker is strong: %+v", s)
	}
}
