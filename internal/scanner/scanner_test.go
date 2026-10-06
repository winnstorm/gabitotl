package scanner

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/winnstorm/gabitotl/internal/memory"
)

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func noGit(context.Context, string, int) ([]Commit, error) { return nil, errors.New("not a repo") }

func goFixture(t *testing.T) string {
	root := t.TempDir()
	write(t, root, "go.mod", "module example.com/demo\n\ngo 1.22\n")
	write(t, root, "README.md", "# Demo\n\n[![ci](x)](y)\n\nDemo is a tiny service that greets people.\n\nMore text.\n")
	write(t, root, "Makefile", ".PHONY: build test\nVAR := 1\nbuild:\n\tgo build ./...\ntest:\n\tgo test ./...\n")
	write(t, root, "cmd/demo/main.go", "package main\n")
	write(t, root, "internal/greet/greet.go", "package greet\n")
	write(t, root, "internal/greet/greet_test.go", "package greet\n")
	write(t, root, "internal/store/store.go", "package store\n")
	write(t, root, "docs/ARCH.md", "# arch\n")
	write(t, root, ".github/workflows/ci.yml", "name: ci\n")
	write(t, root, ".editorconfig", "root = true\n")
	write(t, root, "Dockerfile", "FROM scratch\n")
	// must be skipped
	write(t, root, "node_modules/x/index.js", "x")
	write(t, root, "vendor/v/v.go", "package v")
	write(t, root, ".git/HEAD", "ref")
	write(t, root, ".gabitotl/project.md", "x")
	write(t, root, "dist/out.js", "x")
	return root
}

func TestScanGoFixture(t *testing.T) {
	root := goFixture(t)
	pm, err := Scan(context.Background(), Options{Root: root, GitLog: noGit})
	if err != nil {
		t.Fatal(err)
	}
	if pm.Truncated || pm.Git.IsRepo {
		t.Fatalf("unexpected truncated/git: %+v", pm.Git)
	}
	// skipped directories never counted
	for _, d := range pm.TopDirs {
		if skipDirs[d.Name] {
			t.Fatalf("skipped dir %s counted", d.Name)
		}
	}
	if !slices.Contains(pm.Stack, "Go 1.22") || !slices.Contains(pm.Stack, "Go module example.com/demo") {
		t.Fatalf("stack: %v", pm.Stack)
	}
	wantCmds := []string{"go build ./...", "go test ./...", "make build", "make test"}
	var got []string
	for _, c := range pm.Commands {
		got = append(got, c.Command)
	}
	for _, w := range wantCmds {
		if !slices.Contains(got, w) {
			t.Errorf("missing command %q in %v", w, got)
		}
	}
	for _, c := range got {
		if strings.Contains(c, "make VAR") || strings.Contains(c, "make .PHONY") {
			t.Errorf("bogus make target %q", c)
		}
	}
	if pm.Purpose != "Demo is a tiny service that greets people." {
		t.Fatalf("purpose: %q", pm.Purpose)
	}
	mods := map[string]Module{}
	for _, m := range pm.Modules {
		mods[m.Path] = m
	}
	if mods["internal/greet"].Files != 2 || mods["cmd/demo"].Files != 1 || mods["internal/store"].Files != 1 {
		t.Fatalf("modules: %+v", pm.Modules)
	}
	for _, want := range []string{"GitHub Actions", "Docker"} {
		if !slices.Contains(pm.Integrations, want) {
			t.Errorf("integration %q missing: %v", want, pm.Integrations)
		}
	}
	if !slices.Contains(pm.Conventions, "EditorConfig") {
		t.Errorf("conventions: %v", pm.Conventions)
	}
	if slices.Contains(pm.Risks, "No test files detected") {
		t.Errorf("tests exist, risk wrongly reported: %v", pm.Risks)
	}
	if !slices.ContainsFunc(pm.OpenQuestions, func(s string) bool { return strings.Contains(s, "Not a Git repository") }) {
		t.Errorf("non-git must be an open question: %v", pm.OpenQuestions)
	}
}

func TestScanIsDeterministicAndReadOnly(t *testing.T) {
	root := goFixture(t)
	before := snapshot(t, root)
	a, err := Scan(context.Background(), Options{Root: root, GitLog: noGit})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Scan(context.Background(), Options{Root: root, GitLog: noGit})
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	if string(ja) != string(jb) {
		t.Fatal("scan output is not deterministic")
	}
	if after := snapshot(t, root); !slices.Equal(before, after) {
		t.Fatal("scan modified the repository")
	}
}

func snapshot(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, _ error) error {
		info, _ := d.Info()
		out = append(out, p+"|"+info.ModTime().String()+"|"+string(rune(info.Size())))
		return nil
	})
	return out
}

func TestScanFileCapTruncates(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 30; i++ {
		write(t, root, "src/f"+string(rune('a'+i%26))+string(rune('a'+i/26))+".go", "package x")
	}
	pm, err := Scan(context.Background(), Options{Root: root, MaxFiles: 10, GitLog: noGit})
	if err != nil {
		t.Fatal(err)
	}
	if pm.FilesVisited != 10 || !pm.Truncated {
		t.Fatalf("visited=%d truncated=%v", pm.FilesVisited, pm.Truncated)
	}
	if !slices.ContainsFunc(pm.Risks, func(s string) bool { return strings.Contains(s, "truncated") }) {
		t.Fatalf("risks: %v", pm.Risks)
	}
}

func TestScanNodeAndPython(t *testing.T) {
	root := t.TempDir()
	write(t, root, "package.json", `{"name":"web","description":"A web app","scripts":{"test":"jest","build":"vite build"},"dependencies":{"react":"1"},"devDependencies":{"typescript":"5"}}`)
	write(t, root, "api/requirements.txt", "fastapi\npytest\n")
	write(t, root, "src/app.tsx", "x")
	pm, err := Scan(context.Background(), Options{Root: root, GitLog: noGit})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"Node.js", "React", "TypeScript", "Python", "fastapi"} {
		if !slices.Contains(pm.Stack, s) {
			t.Errorf("stack missing %s: %v", s, pm.Stack)
		}
	}
	var cmds []string
	for _, c := range pm.Commands {
		cmds = append(cmds, c.Command)
	}
	for _, w := range []string{"npm run test", "npm run build", "cd api && pytest"} {
		if !slices.Contains(cmds, w) {
			t.Errorf("missing %q in %v", w, cmds)
		}
	}
	if pm.Purpose != "A web app" {
		t.Errorf("purpose %q", pm.Purpose)
	}
}

func TestScanEmptyDirListsOpenQuestions(t *testing.T) {
	pm, err := Scan(context.Background(), Options{Root: t.TempDir(), GitLog: noGit})
	if err != nil {
		t.Fatal(err)
	}
	if len(pm.Stack) != 0 || len(pm.OpenQuestions) < 4 {
		t.Fatalf("stack=%v questions=%v", pm.Stack, pm.OpenQuestions)
	}
	b, _ := json.Marshal(pm)
	if strings.Contains(string(b), "null") {
		t.Fatalf("JSON must not contain null: %s", b)
	}
}

func TestScanRejectsMissingRoot(t *testing.T) {
	if _, err := Scan(context.Background(), Options{Root: filepath.Join(t.TempDir(), "nope")}); err == nil {
		t.Fatal("expected error")
	}
}

func TestScanRealGitHistory(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := goFixture(t)
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", root, "-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	_ = os.RemoveAll(filepath.Join(root, ".git")) // drop the fake .git of the fixture
	run("init", "-q")
	run("add", "go.mod", "README.md")
	run("commit", "-q", "-m", "feat: first")
	pm, err := Scan(context.Background(), Options{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if !pm.Git.IsRepo || len(pm.Git.Commits) != 1 || pm.Git.Commits[0].Subject != "feat: first" {
		t.Fatalf("git: %+v", pm.Git)
	}
	if !slices.Contains(pm.Conventions, "Conventional Commits (recent history)") {
		t.Fatalf("conventions %v", pm.Conventions)
	}
}

func TestToEntry(t *testing.T) {
	root := goFixture(t)
	pm, _ := Scan(context.Background(), Options{Root: root, GitLog: noGit})
	e := pm.ToEntry("demo")
	if e.Type != memory.TypeProjectMap || e.Project != "demo" || e.TopicKey != ProjectMapTopicKey || !strings.HasPrefix(e.Title, "Project map:") {
		t.Fatalf("entry %+v", e)
	}
	for _, s := range []string{"**Purpose (guess)**", "**Stack**", "**Known commands**", "**Open questions**", "internal/greet"} {
		if !strings.Contains(e.Content, s) {
			t.Errorf("content missing %q", s)
		}
	}
	if err := e.Validate("solo"); err != nil {
		t.Fatal(err)
	}
}
