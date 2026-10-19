package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/winnstorm/gabitotl/internal/persona"
)

func isolatedHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

func TestPersonaModeCommand(t *testing.T) {
	home := isolatedHome(t)
	root := t.TempDir()
	t.Setenv("GABITOTL_USER", "ana")
	if r := runIn(t, "", false, root, "init", "--root", root, "--name", "Shop", "--kind", "existing", "--user-id", "ana", "--user-name", "Ana", "--mode", "solo", "--provider", "engram", "--vcs", "git", "--yes"); r.code != 0 {
		t.Fatalf("init: %s", r.err)
	}
	r := runIn(t, "", false, root, "persona", "mode")
	if r.code != 0 || !strings.HasPrefix(r.out, "mode: normal\n") || !strings.Contains(r.out, "ONLY to what you say in chat") {
		t.Fatalf("default: %+v", r)
	}
	if _, err := os.Stat(filepath.Join(root, ".gabitotl", "local", "users", "ana", "persona.md")); !os.IsNotExist(err) {
		t.Fatal("reading wrote persona.md")
	}
	r = runIn(t, "", false, root, "persona", "mode", "lite")
	want := filepath.Join(root, ".gabitotl", "local", "users", "ana", "persona.md")
	if r.code != 0 || !strings.Contains(r.out, "mode: lite") || !strings.Contains(r.out, want) {
		t.Fatalf("set: %+v", r)
	}
	lite := runIn(t, "", false, root, "persona", "mode")
	if !strings.HasPrefix(lite.out, "mode: lite\n") || lite.out == r.out {
		t.Fatalf("get: %+v", lite)
	}
	runIn(t, "", false, root, "persona", "mode", "full")
	if full := runIn(t, "", false, root, "persona", "mode"); !strings.HasPrefix(full.out, "mode: full\n") || full.out == lite.out {
		t.Fatalf("full: %+v", full)
	}
	t.Setenv("GABITOTL_USER", "beto")
	if r := runIn(t, "", false, root, "persona", "mode"); !strings.HasPrefix(r.out, "mode: normal\n") {
		t.Fatalf("another person must be unaffected: %+v", r)
	}
	if entries, _ := os.ReadDir(home); len(entries) != 0 {
		t.Fatalf("project save wrote in home: %v", entries)
	}
}

func TestPersonaModeInvalidWritesNothing(t *testing.T) {
	home := isolatedHome(t)
	root := t.TempDir()
	r := runIn(t, "", false, root, "persona", "mode", "bogus")
	if r.code != ExitUsage || !strings.Contains(r.err, "bogus") {
		t.Fatalf("got %+v", r)
	}
	if r := runIn(t, "", false, root, "persona", "mode", "lite", "full"); r.code != ExitUsage {
		t.Fatalf("two args: %+v", r)
	}
	if entries, _ := os.ReadDir(home); len(entries) != 0 {
		t.Fatalf("home touched: %v", entries)
	}
	if entries, _ := os.ReadDir(root); len(entries) != 0 {
		t.Fatalf("root touched: %v", entries)
	}
}

func TestPersonaModeOutsideProjectUsesHome(t *testing.T) {
	home := isolatedHome(t)
	t.Setenv("GABITOTL_USER", "ana")
	dir := t.TempDir()
	r := runIn(t, "", false, dir, "persona", "mode", "full")
	if r.code != 0 || !strings.Contains(r.out, filepath.Join(home, ".gabitotl", "persona.md")) {
		t.Fatalf("set: %+v", r)
	}
	if r := runIn(t, "", false, dir, "persona", "mode"); !strings.HasPrefix(r.out, "mode: full\n") || !strings.Contains(r.out, string(persona.ModeFull)) {
		t.Fatalf("get: %+v", r)
	}
}
