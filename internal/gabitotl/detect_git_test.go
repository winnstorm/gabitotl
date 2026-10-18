package gabitotl

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestDetectSignalsHonoursGitignore(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	if out, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	put := func(rel, body string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	put(".gitignore", "generated/\n")
	put("generated/gen.go", "package gen\n")
	s, err := DetectSignals(root)
	if err != nil {
		t.Fatal(err)
	}
	if s.CodeFiles {
		t.Fatal("ignored code must not make the project existing")
	}
	put("src/app.go", "package app\n") // untracked, not ignored
	if s, _ = DetectSignals(root); !s.CodeFiles {
		t.Fatal("untracked, not ignored code must count")
	}
}
