package opencodedb

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/winnstorm/gabitotl/internal/projects"
)

func TestDiscoverReadsWorktreesReadOnly(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".local", "share", "opencode")
	db := filepath.Join(dir, "opencode.db")
	mk(t, db, `CREATE TABLE project (id TEXT, worktree TEXT, time_updated INTEGER);
INSERT INTO project VALUES ('g','/',1),('a','/work/app',1791649612139),('b','',5);`)

	env := projects.Env{Home: home, Getenv: func(string) string { return "" }}
	got, err := Source{env}.Discover(context.Background())
	if err != nil || len(got) != 1 || got[0].Path != "/work/app" || got[0].LastSeen.IsZero() {
		t.Fatalf("got %+v err %v", got, err)
	}
	// A write through the same DSN must be refused: the connection is read-only.
	h, err := sql.Open("sqlite", dsn(db))
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if _, err := h.Exec(`INSERT INTO project VALUES ('x','/y',1)`); err == nil {
		t.Fatal("the DSN must be read-only")
	}
}

func TestDiscoverSkipsForeignAndMissingDBs(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".local", "share", "opencode")
	mk(t, filepath.Join(dir, "opencode.db"), `CREATE TABLE other (a TEXT);`)
	env := projects.Env{Home: home, Getenv: func(string) string { return "" }}
	if got, err := (Source{env}).Discover(context.Background()); err != nil || len(got) != 0 {
		t.Fatalf("got %+v err %v", got, err)
	}
	if got, _ := (Source{projects.Env{Home: t.TempDir()}}).Discover(context.Background()); len(got) != 0 {
		t.Fatal("no db means no projects")
	}
}

func TestDSNWindowsPath(t *testing.T) {
	if got := dsn("C:/Users/x/opencode.db"); got[:21] != "file:///C:/Users/x/op" {
		t.Fatalf("dsn %q", got)
	}
}

func mk(t *testing.T, path, script string) {
	t.Helper()
	if err := mkdir(filepath.Dir(path)); err != nil {
		t.Fatal(err)
	}
	h, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path))
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if _, err := h.Exec(script); err != nil {
		t.Fatal(err)
	}
}

func mkdir(p string) error { return os.MkdirAll(p, 0o755) }
