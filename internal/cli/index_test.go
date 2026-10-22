package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func indexFixture(t *testing.T, tools ...string) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "proj")
	bin := t.TempDir()
	os.MkdirAll(root, 0o755)
	for _, n := range tools {
		// The --version probe runs in the test's working directory (the
		// package dir), so it must not write ran.txt there.
		script := "#!/bin/sh\n[ \"$1\" = --version ] && exit 0\necho \"" + n + " ran $@\" > \"$PWD/ran.txt\"\n"
		if err := os.WriteFile(filepath.Join(bin, n), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	return root
}

func TestIndexStatusText(t *testing.T) {
	root := indexFixture(t, "codegraph")
	os.Mkdir(filepath.Join(root, ".codegraph"), 0o755)
	r := runIn(t, "", false, root, "index", "status", "--root", root)
	for _, want := range []string{"codegraph: installed, indexed", "code-review-graph: not installed", "next: ready:"} {
		if r.code != 0 || !strings.Contains(r.out, want) {
			t.Fatalf("missing %q: %+v", want, r)
		}
	}
}

func TestIndexStatusJSON(t *testing.T) {
	root := indexFixture(t)
	r := runIn(t, "", false, root, "index", "status", "--root", root, "--json")
	var got struct {
		Backends map[string]struct{ Installed, Indexed bool }
		Next     string
	}
	if err := json.Unmarshal([]byte(r.out), &got); err != nil || len(got.Backends) != 2 || got.Backends["codegraph"].Installed || got.Next == "" {
		t.Fatalf("json = %q (%v)", r.out, err)
	}
}

func TestIndexSetupDryRunPrintsArgv(t *testing.T) {
	root := indexFixture(t, "codegraph")
	r := runIn(t, "", false, root, "index", "setup", "--root", root)
	if r.code != 0 || !strings.Contains(r.out, "codegraph init --yes "+root) {
		t.Fatalf("%+v", r)
	}
	if _, err := os.Stat(filepath.Join(root, "ran.txt")); err == nil {
		t.Fatal("dry run executed the tool")
	}
}

func TestIndexSetupYesRunsTheTool(t *testing.T) {
	root := indexFixture(t, "code-review-graph")
	r := runIn(t, "", false, root, "index", "setup", "--root", root, "--yes")
	if r.code != 0 {
		t.Fatalf("%+v", r)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "ran.txt")); !strings.Contains(string(b), "code-review-graph ran build") {
		t.Fatalf("ran.txt = %q", b)
	}
}

func TestIndexSetupMissingToolExitsOne(t *testing.T) {
	root := indexFixture(t)
	for _, args := range [][]string{{"index", "setup", "--root", root, "--yes"}, {"index", "setup", "--root", root, "--tool", "codegraph", "--yes"}} {
		r := runIn(t, "", false, root, args...)
		if r.code != ExitError || !strings.Contains(r.err, "install") {
			t.Fatalf("%v: %+v", args, r)
		}
	}
}
