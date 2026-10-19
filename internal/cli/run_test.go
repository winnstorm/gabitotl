package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runEnv(dir string) (Env, *bytes.Buffer, *bytes.Buffer) {
	var out, errb bytes.Buffer
	return Env{Stdout: &out, Stderr: &errb, Getwd: func() (string, error) { return dir, nil }}, &out, &errb
}

func TestRunDigestExitAndSavings(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".gabitotl"), 0o755); err != nil {
		t.Fatal(err)
	}
	env, out, _ := runEnv(root)
	code := Run([]string{"run", "--root", root, "--", "sh", "-c", "i=0; while [ $i -lt 120 ]; do echo \"line $i of padding output\"; i=$((i+1)); done; echo 'error: boom' >&2; exit 3"}, env)
	if code != 3 {
		t.Fatalf("exit = %d, want 3", code)
	}
	s := out.String()
	if !strings.HasPrefix(s, "run: sh -c") || !strings.Contains(s, "exit 3") || !strings.Contains(s, "error: boom") {
		t.Fatalf("digest = %q", s)
	}
	logs, _ := filepath.Glob(filepath.Join(root, ".gabitotl", "tmp", "run-*.log"))
	if len(logs) != 1 {
		t.Fatalf("logs = %v", logs)
	}
	sav, err := os.ReadFile(filepath.Join(root, ".gabitotl", "local", "users", "gabi", "savings.log"))
	if err != nil || !strings.Contains(string(sav), " run ") {
		t.Fatalf("savings = %q, %v", sav, err)
	}
}

func TestRunNotFoundAndUsage(t *testing.T) {
	env, _, errb := runEnv(t.TempDir())
	if code := Run([]string{"run", "--", "definitely-not-a-command-xyz"}, env); code != 127 {
		t.Fatalf("exit = %d, want 127 (%s)", code, errb)
	}
	env, _, _ = runEnv(t.TempDir())
	if code := Run([]string{"run"}, env); code != ExitUsage {
		t.Fatalf("exit = %d", code)
	}
}

const bigOut = "i=0; while [ $i -lt 120 ]; do echo \"line $i of padding output\"; i=$((i+1)); done"

func TestRunKeepsNewestLogsAndShortPath(t *testing.T) {
	root := t.TempDir()
	tmp := filepath.Join(root, ".gabitotl", "tmp")
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"run-20200101-000000.001.log", "run-20200102-000000.001.log", "run-20200103-000000.001.log", "notes.log"} {
		if err := os.WriteFile(filepath.Join(tmp, n), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	env, out, _ := runEnv(root)
	if code := Run([]string{"run", "--root", root, "--keep", "2", "--", "sh", "-c", bigOut}, env); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	logs, _ := filepath.Glob(filepath.Join(tmp, "run-*.log"))
	if len(logs) != 2 {
		t.Fatalf("logs = %v, want 2 newest", logs)
	}
	if _, err := os.Stat(filepath.Join(tmp, "notes.log")); err != nil {
		t.Fatal("non-matching file must survive")
	}
	if !strings.Contains(out.String(), "(log: .gabitotl/tmp/run-") {
		t.Fatalf("path not relative: %q", out.String())
	}
	env, _, _ = runEnv(root)
	Run([]string{"run", "--root", root, "--keep", "0", "--", "sh", "-c", bigOut}, env)
	if logs, _ = filepath.Glob(filepath.Join(tmp, "run-*.log")); len(logs) != 3 {
		t.Fatalf("keep 0 must keep all: %v", logs)
	}
}

func TestRunSmallOutputPassesThrough(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".gabitotl"), 0o755); err != nil {
		t.Fatal(err)
	}
	env, out, _ := runEnv(root)
	if code := Run([]string{"run", "--root", root, "--", "sh", "-c", "echo hello; exit 2"}, env); code != 2 {
		t.Fatalf("exit = %d", code)
	}
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 2 || lines[0] != "hello" || !strings.HasPrefix(lines[1], "run: exit 2 in ") || strings.Contains(out.String(), "--- tail") {
		t.Fatalf("output = %q", out.String())
	}
	if logs, _ := filepath.Glob(filepath.Join(root, ".gabitotl", "tmp", "run-*.log")); len(logs) != 0 {
		t.Fatalf("no log expected: %v", logs)
	}
	if _, err := os.Stat(filepath.Join(root, ".gabitotl", "local", "users", "gabi", "savings.log")); err == nil {
		t.Fatal("no savings expected for pass-through")
	}
}
