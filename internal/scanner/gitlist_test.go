package scanner

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/winnstorm/gabitotl/internal/tokens"
)

func gitIn(t *testing.T, root string, args ...string) {
	t.Helper()
	full := append([]string{"-C", root, "-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false"}, args...)
	if out, err := exec.Command("git", full...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func gitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	gitIn(t, root, "init", "-q")
	write(t, root, ".gitignore", "secret/\n*.log\n")
	write(t, root, "go.mod", "module x\n")
	write(t, root, "cmd/app/main.go", "package main\n")
	write(t, root, "secret/keys.go", "package secret\n")
	write(t, root, "debug.log", "x")
	gitIn(t, root, "add", "-A")
	gitIn(t, root, "commit", "-q", "-m", "init")
	write(t, root, "internal/new/new.go", "package new\n") // untracked, not ignored
	return root
}

func TestScanUsesGitListing(t *testing.T) {
	root := gitRepo(t)
	pm, err := Scan(context.Background(), Options{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if pm.Source != "git" {
		t.Fatalf("source = %q", pm.Source)
	}
	var paths []string
	for _, m := range pm.Modules {
		paths = append(paths, m.Path)
	}
	joined := strings.Join(paths, ",")
	if strings.Contains(joined, "secret") || !strings.Contains(joined, "internal/new") || !strings.Contains(joined, "cmd/app") {
		t.Fatalf("modules = %v (ignored must be out, untracked in)", paths)
	}
	for _, e := range pm.FileTypes {
		if e.Ext == ".log" {
			t.Fatal("ignored *.log counted")
		}
	}
}

func TestScanWithoutGitFallsBackToWalk(t *testing.T) {
	root := goFixture(t) // fake .git/HEAD: not a real repository
	pm, err := Scan(context.Background(), Options{Root: root, GitLog: noGit})
	if err != nil {
		t.Fatal(err)
	}
	if pm.Source != "walk" || pm.FilesVisited == 0 {
		t.Fatalf("source=%q visited=%d", pm.Source, pm.FilesVisited)
	}
}

func TestRefreshReusesMapWhileHeadAndUntrackedAreUnchanged(t *testing.T) {
	root := gitRepo(t)
	cachePath := filepath.Join(t.TempDir(), "scan-cache.json")
	ctx := context.Background()
	opt := Options{Root: root}

	pm, rebuilt, err := Refresh(ctx, opt, cachePath, false)
	if err != nil || !rebuilt {
		t.Fatalf("first refresh: rebuilt=%v err=%v", rebuilt, err)
	}
	before := pm.FilesVisited
	if _, rebuilt, _ = Refresh(ctx, opt, cachePath, false); rebuilt {
		t.Fatal("unchanged HEAD and untracked set must not rebuild")
	}
	if _, rebuilt, _ = Refresh(ctx, opt, cachePath, true); !rebuilt {
		t.Fatal("force must rebuild")
	}

	write(t, root, "internal/new/more.go", "package new\n") // untracked set changed
	pm, rebuilt, _ = Refresh(ctx, opt, cachePath, false)
	if !rebuilt || pm.FilesVisited != before+1 {
		t.Fatalf("untracked change: rebuilt=%v visited=%d want %d", rebuilt, pm.FilesVisited, before+1)
	}

	gitIn(t, root, "add", "-A")
	gitIn(t, root, "commit", "-q", "-m", "more")
	write(t, root, "pkg/p/p.go", "package p\n")
	gitIn(t, root, "add", "-A")
	gitIn(t, root, "commit", "-q", "-m", "p")
	pm, rebuilt, _ = Refresh(ctx, opt, cachePath, false)
	if !rebuilt || !hasModule(pm, "pkg/p") {
		t.Fatalf("HEAD moved: rebuilt=%v modules=%+v", rebuilt, pm.Modules)
	}
	if _, rebuilt, _ = Refresh(ctx, opt, cachePath, false); rebuilt {
		t.Fatal("second call after the rebuild must reuse the cache")
	}
}

func TestRefreshOutsideGitNeverCaches(t *testing.T) {
	root := goFixture(t)
	cachePath := filepath.Join(t.TempDir(), "scan-cache.json")
	for i := 0; i < 2; i++ {
		if _, rebuilt, err := Refresh(context.Background(), Options{Root: root, GitLog: noGit}, cachePath, false); err != nil || !rebuilt {
			t.Fatalf("run %d: rebuilt=%v err=%v", i, rebuilt, err)
		}
	}
}

func hasModule(pm *ProjectMap, p string) bool {
	for _, m := range pm.Modules {
		if m.Path == p {
			return true
		}
	}
	return false
}

func TestHugeListingDegradesToTruncatedSummary(t *testing.T) {
	root := t.TempDir()
	w := newWalk(root, 10)
	var files []string
	for i := 0; i < 10; i++ { // real files fill the detail budget
		rel := fmt.Sprintf("aaa/f%d.go", i)
		write(t, root, rel, "package a\n")
		files = append(files, rel)
	}
	for i := 0; i < 500; i++ { // the rest is only counted, never statted
		files = append(files, fmt.Sprintf("big/sub/f%d.go", i), fmt.Sprintf("other/f%d.py", i))
	}
	w.runList(files)
	if !w.truncated || w.visited != 1010 || w.detailed != 10 {
		t.Fatalf("truncated=%v visited=%d detailed=%d", w.truncated, w.visited, w.detailed)
	}
	top := w.topDirs()
	if len(top) != 3 || top[0].Files != 500 {
		t.Fatalf("top = %+v", top)
	}
}

func TestMapEntryRespectsTokenBudgetAndSaysWhatWasOmitted(t *testing.T) {
	pm := &ProjectMap{Name: "huge", FilesVisited: 90000, DetailFiles: 5000, Truncated: true}
	for i := 0; i < 60; i++ {
		pm.Modules = append(pm.Modules, Module{Path: fmt.Sprintf("services/module-number-%d", i), Files: 100 - i, Size: "large"})
		pm.Commands = append(pm.Commands, Command{Command: fmt.Sprintf("make target-%d --with-long-flags", i), Source: "Makefile"})
		pm.TopDirs = append(pm.TopDirs, DirInfo{Name: fmt.Sprintf("dir%d", i), Files: 1000 - i})
		pm.Risks = append(pm.Risks, strings.Repeat("risk text ", 12))
		pm.Conventions = append(pm.Conventions, strings.Repeat("convention ", 12))
	}
	e := pm.ToEntry("")
	if got := tokens.Estimate(e.Content); got > MapTokenBudget {
		t.Fatalf("map is %d tokens, budget %d", got, MapTokenBudget)
	}
	for _, want := range []string{"truncated", "lists shortened"} {
		if !strings.Contains(e.Content, want) {
			t.Fatalf("missing %q in:\n%s", want, e.Content)
		}
	}
}
