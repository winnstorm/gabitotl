package codeindex

import (
	"os"
	"path/filepath"
	"testing"
)

// stubPath puts executables named names (each printing a version) alone on PATH.
func stubPath(t *testing.T, names ...string) {
	t.Helper()
	dir := t.TempDir()
	for _, n := range names {
		script := "#!/bin/sh\necho " + n + " 1.2.3\n"
		if err := os.WriteFile(filepath.Join(dir, n), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
}

func TestDetect(t *testing.T) {
	root := t.TempDir()
	stubPath(t, "codegraph")
	if s := CodeGraph().Detect(root); !s.Installed || s.Indexed || s.Version != "codegraph 1.2.3" {
		t.Fatalf("installed, not indexed: %+v", s)
	}
	if err := os.Mkdir(filepath.Join(root, ".codegraph"), 0o755); err != nil {
		t.Fatal(err)
	}
	if s := CodeGraph().Detect(root); !s.Installed || !s.Indexed {
		t.Fatalf("indexed: %+v", s)
	}
	if s := CodeReviewGraph().Detect(root); s.Installed || s.Indexed {
		t.Fatalf("absent tool: %+v", s)
	}
	// An index directory without the binary is not usable.
	os.MkdirAll(filepath.Join(root, ".code-review-graph"), 0o755)
	if s := CodeReviewGraph().Detect(root); s.Installed || s.Indexed {
		t.Fatalf("dir without binary: %+v", s)
	}
}

func TestDefaultAndNextStep(t *testing.T) {
	root := t.TempDir()
	stubPath(t)
	if Default(root) != nil {
		t.Fatal("nothing installed must give no default")
	}
	stubPath(t, "code-review-graph")
	if d := Default(root); d == nil || d.Name() != "code-review-graph" {
		t.Fatalf("default = %v", d)
	}
	stubPath(t, "code-review-graph", "codegraph")
	if d := Default(root); d.Name() != "codegraph" {
		t.Fatalf("codegraph must win, got %s", d.Name())
	}
}

func TestSetupCommands(t *testing.T) {
	if got := CodeGraph().SetupCommand("/p"); got[0] != "codegraph" || got[1] != "init" || got[len(got)-1] != "/p" {
		t.Fatalf("codegraph argv = %v", got)
	}
	if got := CodeReviewGraph().SetupCommand("/p"); len(got) != 2 || got[1] != "build" {
		t.Fatalf("code-review-graph argv = %v", got)
	}
}
