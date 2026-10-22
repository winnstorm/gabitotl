package projects

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Env is what the sources read from the machine; tests fill it with a temp tree.
type Env struct {
	Home    string
	Getenv  func(string) string
	Windows bool
}

// OSEnv is the running machine.
func OSEnv() Env {
	home, _ := os.UserHomeDir()
	return Env{Home: home, Getenv: os.Getenv, Windows: os.PathSeparator == '\\'}
}

func (e Env) get(k string) string {
	if e.Getenv == nil {
		return ""
	}
	return e.Getenv(k)
}

// readJSON decodes a bounded JSON file; any problem is "absent".
func readJSON(p string, v any) bool {
	f, err := os.Open(p)
	if err != nil {
		return false
	}
	defer f.Close()
	dec := json.NewDecoder(&limitReader{f: f, left: 32 << 20})
	return dec.Decode(v) == nil
}

type limitReader struct {
	f    *os.File
	left int64
}

func (l *limitReader) Read(b []byte) (int, error) {
	if l.left <= 0 {
		return 0, fs.ErrInvalid
	}
	if int64(len(b)) > l.left {
		b = b[:l.left]
	}
	n, err := l.f.Read(b)
	l.left -= int64(n)
	return n, err
}

// ClaudeCode reads the top-level `projects` keys of ~/.claude.json.
type ClaudeCode struct{ Env Env }

func (ClaudeCode) Name() string { return SrcClaudeCode }

func (c ClaudeCode) Discover(context.Context) ([]Candidate, error) {
	var doc struct {
		Projects map[string]json.RawMessage `json:"projects"`
	}
	if !readJSON(filepath.Join(c.Env.Home, ".claude.json"), &doc) {
		return nil, nil
	}
	var out []Candidate
	for p := range doc.Projects {
		out = append(out, Candidate{Path: p, Source: SrcClaudeCode})
	}
	return out, nil
}

// OpenCodeDataDirs lists, in probe order, the folders that may hold OpenCode's
// data: OPENCODE_DATA, the dir of OPENCODE_DB, ~/.local/share/opencode (Linux
// and, per the official docs, Windows), then %LOCALAPPDATA% and %APPDATA%.
func OpenCodeDataDirs(e Env) []string {
	var dirs []string
	if d := e.get("OPENCODE_DATA"); d != "" {
		dirs = append(dirs, d)
	}
	if db := e.get("OPENCODE_DB"); db != "" && db != ":memory:" {
		dirs = append(dirs, filepath.Dir(db))
	}
	if e.Home != "" {
		dirs = append(dirs, filepath.Join(e.Home, ".local", "share", "opencode"))
	}
	if x := e.get("XDG_DATA_HOME"); x != "" {
		dirs = append(dirs, filepath.Join(x, "opencode"))
	}
	for _, k := range []string{"LOCALAPPDATA", "APPDATA"} {
		if v := e.get(k); v != "" {
			dirs = append(dirs, filepath.Join(v, "opencode"))
		}
	}
	return dirs
}

// OpenCodeDBPaths lists the existing opencode.db files to read.
func OpenCodeDBPaths(e Env) []string {
	var out []string
	if db := e.get("OPENCODE_DB"); db != "" && db != ":memory:" {
		out = append(out, db)
	}
	for _, d := range OpenCodeDataDirs(e) {
		out = append(out, filepath.Join(d, "opencode.db"))
	}
	var found []string
	seen := map[string]bool{}
	for _, p := range out {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() && !seen[p] {
			seen[p] = true
			found = append(found, p)
		}
	}
	return found
}

// OpenCodeLegacy reads the pre-SQLite storage/project/*.json files.
type OpenCodeLegacy struct{ Env Env }

func (OpenCodeLegacy) Name() string { return SrcOpenCode }

func (o OpenCodeLegacy) Discover(ctx context.Context) ([]Candidate, error) {
	var out []Candidate
	for _, d := range OpenCodeDataDirs(o.Env) {
		files, _ := filepath.Glob(filepath.Join(d, "storage", "project", "*.json"))
		for _, f := range files {
			if ctx.Err() != nil {
				return out, nil
			}
			var doc struct {
				Worktree string `json:"worktree"`
				Time     struct {
					Updated int64 `json:"updated"`
				} `json:"time"`
			}
			if !readJSON(f, &doc) || doc.Worktree == "" || doc.Worktree == "/" {
				continue
			}
			out = append(out, Candidate{Path: doc.Worktree, Source: SrcOpenCode, LastSeen: UnixMilli(doc.Time.Updated)})
		}
	}
	return out, nil
}

// UnixMilli converts a millisecond epoch; zero for non-positive values.
func UnixMilli(ms int64) time.Time {
	if ms <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms)
}

// Kiro reads the VS Code style workspace storage of Kiro (best effort).
type Kiro struct{ Env Env }

func (Kiro) Name() string { return SrcKiro }

// userDirs are the possible `Kiro/User` folders.
func (k Kiro) userDirs() []string {
	var out []string
	if v := k.Env.get("APPDATA"); v != "" {
		out = append(out, filepath.Join(v, "Kiro", "User"))
	}
	if k.Env.Home != "" {
		out = append(out, filepath.Join(k.Env.Home, ".config", "Kiro", "User"))
	}
	return out
}

func (k Kiro) Discover(ctx context.Context) ([]Candidate, error) {
	var out []Candidate
	add := func(uri string, t time.Time) {
		if p, ok := FileURIPath(uri); ok {
			out = append(out, Candidate{Path: p, Source: SrcKiro, LastSeen: t})
		}
	}
	for _, u := range k.userDirs() {
		files, _ := filepath.Glob(filepath.Join(u, "workspaceStorage", "*", "workspace.json"))
		for _, f := range files {
			if ctx.Err() != nil {
				return out, nil
			}
			var doc struct {
				Folder string `json:"folder"`
			}
			if readJSON(f, &doc) && doc.Folder != "" {
				var t time.Time
				if fi, err := os.Stat(f); err == nil {
					t = fi.ModTime()
				}
				add(doc.Folder, t)
			}
		}
		var st struct {
			Backup struct {
				Folders []struct {
					FolderURI string `json:"folderUri"`
				} `json:"folders"`
			} `json:"backupWorkspaces"`
			Profiles struct {
				Workspaces map[string]json.RawMessage `json:"workspaces"`
			} `json:"profileAssociations"`
		}
		if readJSON(filepath.Join(u, "globalStorage", "storage.json"), &st) {
			for _, f := range st.Backup.Folders {
				add(f.FolderURI, time.Time{})
			}
			for uri := range st.Profiles.Workspaces {
				add(uri, time.Time{})
			}
		}
	}
	return out, nil
}

// FileURIPath turns file:///c%3A/Users/x/repo into C:/Users/x/repo and
// file:///home/x/repo into /home/x/repo. Other schemes (remote workspaces) are
// not local folders and report ok=false.
func FileURIPath(uri string) (string, bool) {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" || u.Host != "" && u.Host != "localhost" {
		return "", false
	}
	p := u.Path // already percent-decoded
	if len(p) >= 3 && p[0] == '/' && p[2] == ':' && isLetter(p[1]) {
		p = p[1:]
	}
	if p == "" {
		return "", false
	}
	return p, true
}

// Func adapts a function (e.g. the folders the Manager remembers) to a Source.
type Func struct {
	Label string
	List  func() []string
}

func (f Func) Name() string { return f.Label }
func (f Func) Discover(context.Context) ([]Candidate, error) {
	var out []Candidate
	for _, p := range f.List() {
		out = append(out, Candidate{Path: p, Source: f.Label})
	}
	return out, nil
}

// skipDirs are never entered by the bounded scan.
var skipDirs = map[string]bool{"node_modules": true, ".git": true, "vendor": true, "target": true, "dist": true, "venv": true, ".venv": true}

// CommonRoots scans a few usual project parents for folders that hold
// `.gabitotl/`, to depth 3, skipping heavy folders, until ctx ends.
type CommonRoots struct {
	Env   Env
	Roots []string // relative to Home; default below
}

func (CommonRoots) Name() string { return SrcGabitoTL }

func (c CommonRoots) Discover(ctx context.Context) ([]Candidate, error) {
	roots := c.Roots
	if roots == nil {
		roots = []string{"source/repos", "projects", "dev", "code", "src", "repos", "workspace", "PY_SCRIPTS"}
	}
	var out []Candidate
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		if ctx.Err() != nil || depth > 3 {
			return
		}
		if isDir(filepath.Join(dir, ".gabitotl")) {
			out = append(out, Candidate{Path: dir, Source: SrcGabitoTL})
			return
		}
		ents, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range ents {
			if e.IsDir() && !skipDirs[e.Name()] && !strings.HasPrefix(e.Name(), ".") {
				walk(filepath.Join(dir, e.Name()), depth+1)
			}
		}
	}
	for _, r := range roots {
		walk(filepath.Join(c.Env.Home, r), 1)
	}
	return out, nil
}

// Defaults are the dependency-free sources; the Manager adds the SQLite one.
func Defaults(e Env) []Source {
	return []Source{ClaudeCode{e}, OpenCodeLegacy{e}, Kiro{e}, CommonRoots{Env: e}}
}
