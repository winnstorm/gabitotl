package branding

import (
	"bytes"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// MaxFileSize is the largest file the guard scans; bigger files are skipped.
const MaxFileSize = 2 << 20

// Skipped reports whether rel (slash-separated, relative to the project root)
// is never scanned or rewritten: Git internals and the local backups.
func Skipped(rel string) bool {
	rel = strings.TrimPrefix(path.Clean(filepath.ToSlash(rel)), "./")
	for _, p := range []string{".git", ".gabitotl/.bak"} {
		if rel == p || strings.HasPrefix(rel, p+"/") {
			return true
		}
	}
	// A nested checkout or worktree keeps its own .git entry (file or directory).
	return strings.Contains("/"+rel+"/", "/.git/")
}

// IsBinary reports whether data looks binary (a NUL byte in the first 8 KiB).
func IsBinary(data []byte) bool {
	if len(data) > 8192 {
		data = data[:8192]
	}
	return bytes.IndexByte(data, 0) >= 0
}

var proseExt = map[string]bool{
	".md": true, ".markdown": true, ".txt": true, ".rst": true, ".adoc": true, ".html": true,
	".htm": true, ".tex": true, ".org": true, "": true,
}

// ContextFor picks the scan context of a file from its name.
func ContextFor(file string) Context {
	if proseExt[strings.ToLower(filepath.Ext(file))] {
		return ContextProse
	}
	return ContextCode
}

// ScanFile scans one file's content. Binary content yields no findings.
func (d *Detector) ScanFile(file string, data []byte) []Finding {
	if IsBinary(data) {
		return nil
	}
	return d.Scan(file, string(data), ContextFor(file))
}

var (
	globMu    sync.Mutex
	globCache = map[string]*regexp.Regexp{}
)

func globRegexp(pattern string) *regexp.Regexp {
	globMu.Lock()
	defer globMu.Unlock()
	if re, ok := globCache[pattern]; ok {
		return re
	}
	p := strings.TrimPrefix(filepath.ToSlash(strings.TrimSpace(pattern)), "./")
	if strings.HasSuffix(p, "/") {
		p += "**"
	}
	if !strings.Contains(strings.TrimSuffix(p, "/**"), "/") {
		p = "**/" + p
	}
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(p); i++ {
		switch c := p[i]; {
		case c == '*' && i+1 < len(p) && p[i+1] == '*':
			i++
			if i+1 < len(p) && p[i+1] == '/' {
				i++
				b.WriteString("(?:.*/)?")
			} else {
				b.WriteString(".*")
			}
		case c == '*':
			b.WriteString("[^/]*")
		case c == '?':
			b.WriteString("[^/]")
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	re, err := regexp.Compile(b.String())
	if err != nil {
		re = regexp.MustCompile(`$^`) // matches nothing
	}
	globCache[pattern] = re
	return re
}

// Allowed reports whether rel matches one of the allowlist globs. Globs follow
// gitignore-like rules: "*" stays inside a segment, "**" crosses segments, a
// pattern without "/" matches at any depth and a trailing "/" means a whole
// directory.
func Allowed(rel string, globs []string) bool {
	rel = strings.TrimPrefix(path.Clean(filepath.ToSlash(rel)), "./")
	for _, g := range globs {
		if strings.TrimSpace(g) != "" && globRegexp(g).MatchString(rel) {
			return true
		}
	}
	return false
}

// ScanPaths reads and scans the files rels (relative to root). Skipped paths,
// allowlisted paths, directories, oversized and binary files are left out.
// Unreadable files are ignored: the guard never fails a scan on them.
func (d *Detector) ScanPaths(root string, rels []string, allow []string) []Finding {
	var out []Finding
	for _, rel := range rels {
		rel = filepath.ToSlash(rel)
		if Skipped(rel) || Allowed(rel, allow) {
			continue
		}
		abs := filepath.Join(root, filepath.FromSlash(rel))
		st, err := os.Lstat(abs)
		if err != nil || !st.Mode().IsRegular() || st.Size() > MaxFileSize {
			continue
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			continue
		}
		out = append(out, d.ScanFile(rel, data)...)
	}
	return out
}

// WalkFiles lists every regular file under root (slash-separated, relative),
// skipping .git, .gabitotl/.bak and dependency folders. It is the fallback when
// the project is not a Git work tree.
func WalkFiles(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(p string, de os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if de.IsDir() {
			if Skipped(rel) || de.Name() == "node_modules" || de.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !Skipped(rel) && de.Type().IsRegular() {
			out = append(out, rel)
		}
		return nil
	})
	return out, err
}
