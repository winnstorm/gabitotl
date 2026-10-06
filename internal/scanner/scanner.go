// Package scanner builds a bounded, layered ProjectMap of an existing
// repository (spec section 5): structure, README/docs, manifests, recent Git
// history and main modules. It only reads: it never modifies the repository.
package scanner

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Defaults for Options.
const (
	DefaultMaxFiles    = 5000
	DefaultGitCommits  = 15
	DefaultDocLines    = 12
	maxManifestBytes   = 256 << 10
	manifestMaxDepth   = 2
	maxExtensionsShown = 12
)

// Options bound the scan. Zero values use the defaults.
type Options struct {
	Root       string
	MaxFiles   int // hard cap on files visited
	GitCommits int // recent commits to read
	DocLines   int // lines of README/docs excerpt
	// GitLog overrides the git invocation (tests). nil runs `git log`.
	GitLog func(ctx context.Context, root string, n int) ([]Commit, error)
}

func (o Options) maxFiles() int {
	if o.MaxFiles > 0 {
		return o.MaxFiles
	}
	return DefaultMaxFiles
}

func (o Options) gitCommits() int {
	if o.GitCommits > 0 {
		return o.GitCommits
	}
	return DefaultGitCommits
}

func (o Options) docLines() int {
	if o.DocLines > 0 {
		return o.DocLines
	}
	return DefaultDocLines
}

// skipDirs are never descended into (vendored, generated, VCS or tool state).
var skipDirs = map[string]bool{
	".git": true, ".gabitotl": true, "node_modules": true, "vendor": true,
	"dist": true, "build": true, "target": true, "out": true, ".next": true,
	".nuxt": true, ".cache": true, "coverage": true, ".venv": true, "venv": true,
	"__pycache__": true, ".idea": true, ".gradle": true, ".tox": true, ".mypy_cache": true,
}

// sourceExts marks files that count as source code for module sizing.
var sourceExts = map[string]bool{
	".go": true, ".py": true, ".js": true, ".jsx": true, ".ts": true, ".tsx": true, ".java": true,
	".kt": true, ".rs": true, ".rb": true, ".php": true, ".cs": true, ".c": true, ".cpp": true,
	".h": true, ".swift": true, ".scala": true, ".sh": true, ".ex": true, ".vue": true, ".svelte": true,
}

// ExtCount is a file-type tally.
type ExtCount struct {
	Ext   string `json:"ext"`
	Files int    `json:"files"`
}

// DirInfo describes a top-level directory.
type DirInfo struct {
	Name  string `json:"name"`
	Files int    `json:"files"`
}

// Doc is a documentation file with its first lines.
type Doc struct {
	Path    string `json:"path"`
	Excerpt string `json:"excerpt,omitempty"`
}

// Command is a known build/test/run command and where it was found.
type Command struct {
	Command string `json:"command"`
	Source  string `json:"source"`
}

// Module is a main source module with a size heuristic.
type Module struct {
	Path  string `json:"path"`
	Files int    `json:"source_files"`
	Bytes int64  `json:"bytes"`
	Size  string `json:"size"` // small | medium | large
}

// Commit is a recent Git commit (author names are intentionally not read).
type Commit struct {
	Hash    string `json:"hash"`
	Date    string `json:"date"`
	Subject string `json:"subject"`
}

// GitInfo is the recent-history layer.
type GitInfo struct {
	IsRepo  bool     `json:"is_repo"`
	Commits []Commit `json:"commits,omitempty"`
}

// ProjectMap is the deterministic result of a scan.
type ProjectMap struct {
	Root          string     `json:"root"`
	Name          string     `json:"name"`
	Purpose       string     `json:"purpose,omitempty"`
	FilesVisited  int        `json:"files_visited"`
	Truncated     bool       `json:"truncated"`
	TopDirs       []DirInfo  `json:"top_dirs"`
	FileTypes     []ExtCount `json:"file_types"`
	Docs          []Doc      `json:"docs"`
	Manifests     []string   `json:"manifests"`
	Stack         []string   `json:"stack"`
	Commands      []Command  `json:"commands"`
	Modules       []Module   `json:"modules"`
	Git           GitInfo    `json:"git"`
	Architecture  []string   `json:"architecture"`
	Conventions   []string   `json:"conventions"`
	Integrations  []string   `json:"integrations"`
	Risks         []string   `json:"risks"`
	OpenQuestions []string   `json:"open_questions"`
}

// Scan reads the repository under opt.Root and returns its ProjectMap.
func Scan(ctx context.Context, opt Options) (*ProjectMap, error) {
	root, err := filepath.Abs(opt.Root)
	if err != nil {
		return nil, err
	}
	if st, err := os.Stat(root); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("scan: %s is not a directory", opt.Root)
	}
	pm := &ProjectMap{Root: root, Name: filepath.Base(root)}
	w := newWalk(root, opt.maxFiles())
	if err := w.run(ctx); err != nil {
		return nil, err
	}
	pm.FilesVisited, pm.Truncated = w.visited, w.truncated
	pm.TopDirs = w.topDirs()
	pm.FileTypes = w.fileTypes()
	pm.Modules = w.modules()

	// (b) README/docs
	pm.Docs = readDocs(root, w.docFiles, opt.docLines())
	// (c) manifests -> stack and commands
	analyzeManifests(root, w.manifests, pm)
	// (d) recent git history
	logFn := opt.GitLog
	if logFn == nil {
		logFn = gitLog
	}
	if cs, err := logFn(ctx, root, opt.gitCommits()); err == nil {
		pm.Git = GitInfo{IsRepo: true, Commits: cs}
	}
	derive(pm, w)
	return pm, nil
}

// walk accumulates the structure layer in one bounded pass.
type walk struct {
	root      string
	max       int
	visited   int
	truncated bool
	ext       map[string]int
	top       map[string]int // top-level dir -> files
	topFiles  int            // files directly in root
	docFiles  []string       // repo-relative README/docs candidates
	manifests []string       // repo-relative manifest paths
	flags     map[string]bool
	modStats  map[string]*Module
	hasTests  bool
}

func newWalk(root string, max int) *walk {
	return &walk{root: root, max: max, ext: map[string]int{}, top: map[string]int{}, flags: map[string]bool{}, modStats: map[string]*Module{}}
}

var manifestNames = map[string]bool{
	"go.mod": true, "package.json": true, "pyproject.toml": true, "requirements.txt": true, "setup.py": true,
	"pom.xml": true, "build.gradle": true, "build.gradle.kts": true, "Cargo.toml": true, "composer.json": true,
	"Gemfile": true, "mix.exs": true, "Makefile": true, "Dockerfile": true, "docker-compose.yml": true,
	"docker-compose.yaml": true, "compose.yaml": true,
}

// containerDirs hold several modules one level down.
var containerDirs = map[string]bool{"internal": true, "cmd": true, "pkg": true, "src": true, "packages": true, "apps": true, "services": true, "lib": true}

func (w *walk) run(ctx context.Context) error {
	err := filepath.WalkDir(w.root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil // unreadable entries are skipped, never fatal
		}
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		rel, _ := filepath.Rel(w.root, p)
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return fs.SkipDir
			}
			if d.Name() == "workflows" && rel == ".github/workflows" {
				w.flags["github-actions"] = true
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if w.visited >= w.max {
			w.truncated = true
			return fs.SkipAll
		}
		w.visited++
		w.record(rel, d)
		return nil
	})
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return err
}

func (w *walk) record(rel string, d fs.DirEntry) {
	name := d.Name()
	parts := strings.Split(rel, "/")
	ext := strings.ToLower(path.Ext(name))
	if ext == "" {
		ext = "(none)"
	}
	w.ext[ext]++
	if len(parts) == 1 {
		w.topFiles++
	} else {
		w.top[parts[0]]++
	}
	depth := len(parts) - 1
	if manifestNames[name] && depth <= manifestMaxDepth {
		w.manifests = append(w.manifests, rel)
	}
	if depth == 0 {
		low := strings.ToLower(name)
		if strings.HasPrefix(low, "readme") || low == "contributing.md" || low == "architecture.md" {
			w.docFiles = append(w.docFiles, rel)
		}
		switch low {
		case ".editorconfig", ".golangci.yml", ".golangci.yaml", ".prettierrc", ".eslintrc", ".eslintrc.json", ".eslintrc.js", "ruff.toml", ".pre-commit-config.yaml", "eslint.config.js", "eslint.config.mjs", "tsconfig.json", ".env.example", ".env", ".gitlab-ci.yml", "jenkinsfile", "bitbucket-pipelines.yml", "contributing.md", "claude.md", "agents.md":
			w.flags[low] = true
		}
	}
	if len(parts) >= 2 && parts[0] == "docs" && depth == 1 && strings.HasSuffix(strings.ToLower(name), ".md") {
		w.docFiles = append(w.docFiles, rel)
	}
	if isTestFile(rel, name) {
		w.hasTests = true
	}
	if sourceExts[ext] {
		w.countModule(parts, d)
	}
	if strings.HasSuffix(ext, "tf") {
		w.flags["terraform"] = true
	}
}

func isTestFile(rel, name string) bool {
	low := strings.ToLower(name)
	switch {
	case strings.HasSuffix(low, "_test.go"), strings.HasPrefix(low, "test_") && strings.HasSuffix(low, ".py"),
		strings.HasSuffix(low, "_test.py"), strings.Contains(low, ".test."), strings.Contains(low, ".spec."):
		return true
	}
	for _, seg := range strings.Split(path.Dir(rel), "/") {
		if seg == "tests" || seg == "test" || seg == "__tests__" || seg == "spec" {
			return true
		}
	}
	return false
}

func (w *walk) countModule(parts []string, d fs.DirEntry) {
	if len(parts) < 2 {
		return
	}
	key := parts[0]
	if containerDirs[parts[0]] && len(parts) >= 3 {
		key = parts[0] + "/" + parts[1]
	}
	m := w.modStats[key]
	if m == nil {
		m = &Module{Path: key}
		w.modStats[key] = m
	}
	m.Files++
	if info, err := d.Info(); err == nil {
		m.Bytes += info.Size()
	}
}

func sizeClass(m Module) string {
	switch {
	case m.Files >= 100 || m.Bytes >= 1<<20:
		return "large"
	case m.Files >= 20 || m.Bytes >= 100<<10:
		return "medium"
	}
	return "small"
}

func (w *walk) topDirs() []DirInfo {
	out := make([]DirInfo, 0, len(w.top))
	for n, c := range w.top {
		out = append(out, DirInfo{Name: n, Files: c})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Files != out[j].Files {
			return out[i].Files > out[j].Files
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func (w *walk) fileTypes() []ExtCount {
	out := make([]ExtCount, 0, len(w.ext))
	for e, c := range w.ext {
		out = append(out, ExtCount{Ext: e, Files: c})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Files != out[j].Files {
			return out[i].Files > out[j].Files
		}
		return out[i].Ext < out[j].Ext
	})
	if len(out) > maxExtensionsShown {
		out = out[:maxExtensionsShown]
	}
	return out
}

func (w *walk) modules() []Module {
	out := make([]Module, 0, len(w.modStats))
	for _, m := range w.modStats {
		m.Size = sizeClass(*m)
		out = append(out, *m)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Files != out[j].Files {
			return out[i].Files > out[j].Files
		}
		return out[i].Path < out[j].Path
	})
	return out
}

// readDocs returns README/docs files in sorted order with a short excerpt.
func readDocs(root string, files []string, lines int) []Doc {
	sort.Strings(files)
	var docs []Doc
	for _, f := range files {
		d := Doc{Path: f}
		if f == "README.md" || strings.HasPrefix(strings.ToLower(f), "readme") {
			d.Excerpt = firstLines(filepath.Join(root, f), lines)
		}
		docs = append(docs, d)
	}
	return docs
}

func firstLines(p string, n int) string {
	f, err := os.Open(p)
	if err != nil {
		return ""
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 256<<10)
	for sc.Scan() && len(out) < n {
		if ln := strings.TrimRight(sc.Text(), " \t\r"); strings.TrimSpace(ln) != "" || len(out) > 0 {
			out = append(out, ln)
		}
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// gitLog reads the last n commits; it fails (callers tolerate it) outside a repository.
func gitLog(ctx context.Context, root string, n int) ([]Commit, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", root, "log", "-n", strconv.Itoa(n), "--date=short", "--pretty=format:%h\x1f%ad\x1f%s")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var cs []Commit
	for _, ln := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		p := strings.SplitN(ln, "\x1f", 3)
		if len(p) == 3 {
			cs = append(cs, Commit{Hash: p[0], Date: p[1], Subject: p[2]})
		}
	}
	return cs, nil
}
