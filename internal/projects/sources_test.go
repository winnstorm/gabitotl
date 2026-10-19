package projects

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func put(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func paths(c []Candidate) []string {
	var out []string
	for _, x := range c {
		out = append(out, x.Path)
	}
	sort.Strings(out)
	return out
}

func env(home string, kv map[string]string) Env {
	return Env{Home: home, Getenv: func(k string) string { return kv[k] }}
}

func TestClaudeCodeReadsProjectKeys(t *testing.T) {
	home := t.TempDir()
	put(t, filepath.Join(home, ".claude.json"), `{"theme":"x","projects":{"/w/a":{"allowedTools":[]},"/w/b":{}}}`)
	got, _ := ClaudeCode{env(home, nil)}.Discover(context.Background())
	if g := paths(got); len(g) != 2 || g[0] != "/w/a" || g[1] != "/w/b" {
		t.Fatalf("got %v", g)
	}
	// Absent and corrupt files are silent.
	put(t, filepath.Join(home, ".claude.json"), `{not json`)
	if got, err := (ClaudeCode{env(home, nil)}).Discover(context.Background()); err != nil || len(got) != 0 {
		t.Fatalf("corrupt: %v %v", got, err)
	}
	if got, _ := (ClaudeCode{env(t.TempDir(), nil)}).Discover(context.Background()); len(got) != 0 {
		t.Fatal("absent")
	}
}

func TestOpenCodeLocationsAndLegacy(t *testing.T) {
	home, custom := t.TempDir(), t.TempDir()
	e := env(home, map[string]string{"OPENCODE_DATA": custom, "LOCALAPPDATA": filepath.Join(home, "local"), "APPDATA": filepath.Join(home, "roam")})
	dirs := OpenCodeDataDirs(e)
	want := []string{custom, filepath.Join(home, ".local", "share", "opencode"), filepath.Join(home, "local", "opencode"), filepath.Join(home, "roam", "opencode")}
	if len(dirs) != len(want) {
		t.Fatalf("dirs %v", dirs)
	}
	for i := range want {
		if dirs[i] != want[i] {
			t.Fatalf("dirs %v want %v", dirs, want)
		}
	}
	put(t, filepath.Join(custom, "opencode.db"), "x")
	if p := OpenCodeDBPaths(e); len(p) != 1 || p[0] != filepath.Join(custom, "opencode.db") {
		t.Fatalf("db paths %v", p)
	}
	put(t, filepath.Join(custom, "storage", "project", "1.json"), `{"worktree":"/w/old","time":{"updated":1700000000000}}`)
	put(t, filepath.Join(custom, "storage", "project", "2.json"), `{"worktree":"/"}`)
	put(t, filepath.Join(custom, "storage", "project", "3.json"), `garbage`)
	got, _ := OpenCodeLegacy{e}.Discover(context.Background())
	if len(got) != 1 || got[0].Path != "/w/old" || got[0].LastSeen.IsZero() {
		t.Fatalf("legacy %+v", got)
	}
}

func TestKiroWorkspaceAndGlobalStorage(t *testing.T) {
	appdata := t.TempDir()
	u := filepath.Join(appdata, "Kiro", "User")
	put(t, filepath.Join(u, "workspaceStorage", "h1", "workspace.json"), `{"folder":"file:///c%3A/Users/x/repo"}`)
	put(t, filepath.Join(u, "workspaceStorage", "h2", "workspace.json"), `{"folder":"vscode-remote://ssh/x"}`)
	put(t, filepath.Join(u, "globalStorage", "storage.json"), `{"backupWorkspaces":{"folders":[{"folderUri":"file:///d%3A/Work/app"}]},"profileAssociations":{"workspaces":{"file:///e%3A/Other":"p"}}}`)
	got, _ := Kiro{env("", map[string]string{"APPDATA": appdata})}.Discover(context.Background())
	g := paths(got)
	if len(g) != 3 || g[0] != "c:/Users/x/repo" || g[1] != "d:/Work/app" || g[2] != "e:/Other" {
		t.Fatalf("kiro %v", g)
	}
}

func TestCommonRootsFindsGabitotlFoldersBounded(t *testing.T) {
	home := t.TempDir()
	mk := func(p string) { _ = os.MkdirAll(filepath.Join(home, p), 0o755) }
	mk("projects/app/.gabitotl")
	mk("projects/group/deep/.gabitotl")
	mk("projects/node_modules/pkg/.gabitotl") // skipped
	mk("projects/a/b/c/d/.gabitotl")          // too deep
	mk("projects/plain/.git")                 // not a gabitotl folder
	got, _ := CommonRoots{Env: env(home, nil)}.Discover(context.Background())
	g := paths(got)
	if len(g) != 2 || g[0] != filepath.Join(home, "projects/app") || g[1] != filepath.Join(home, "projects/group/deep") {
		t.Fatalf("roots %v", g)
	}
}
