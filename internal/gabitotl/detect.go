package gabitotl

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/winnstorm/gabitotl/internal/procattr"
)

// Signals are the facts DetectKind looks at (spec section 11).
type Signals struct {
	CodeFiles  bool // source files outside ignored dirs
	GitHistory bool // at least one commit at the root
	Manifest   bool // go.mod, package.json, pyproject.toml, ...
	Readme     bool // README present (weak signal, see DetectKind)
}

var manifests = map[string]bool{
	"go.mod": true, "package.json": true, "pyproject.toml": true, "requirements.txt": true,
	"setup.py": true, "pom.xml": true, "build.gradle": true, "build.gradle.kts": true,
	"Cargo.toml": true, "Gemfile": true, "composer.json": true, "mix.exs": true,
	"pubspec.yaml": true, "CMakeLists.txt": true,
}

var codeExts = map[string]bool{
	".go": true, ".py": true, ".js": true, ".jsx": true, ".ts": true, ".tsx": true,
	".java": true, ".kt": true, ".rs": true, ".c": true, ".h": true, ".cc": true,
	".cpp": true, ".cs": true, ".rb": true, ".php": true, ".swift": true, ".scala": true,
	".ex": true, ".exs": true, ".dart": true, ".lua": true,
}

var skipDirs = map[string]bool{
	DirName: true, ".git": true, ".hg": true, ".svn": true, "node_modules": true,
	"vendor": true, "target": true, "dist": true, "build": true, "__pycache__": true,
	".venv": true, "venv": true,
}

const maxWalkEntries = 5000

// DetectSignals inspects root without reading file contents.
func DetectSignals(root string) (Signals, error) {
	var s Signals
	if st, err := os.Stat(root); err != nil || !st.IsDir() {
		return s, fmt.Errorf("%s is not a directory", root)
	}
	if files, ok := gitListing(root); ok {
		for _, rel := range files {
			if observe(&s, rel) {
				break
			}
		}
	} else if err := walkSignals(root, &s); err != nil {
		return s, err
	}
	s.GitHistory = hasGitHistory(root)
	return s, nil
}

// gitListing lists tracked plus untracked-not-ignored files (honouring
// .gitignore without walking directories); ok is false outside a repository.
func gitListing(root string) ([]string, bool) {
	out, err := procattr.NoWindow(exec.Command("git", "-C", root, "ls-files", "-z", "--cached", "--others", "--exclude-standard")).Output()
	if err != nil {
		return nil, false
	}
	var files []string
	for _, p := range strings.Split(string(out), "\x00") {
		if p != "" {
			files = append(files, p)
		}
	}
	return files, true
}

// observe records what the repo-relative file rel says about the project and
// reports whether both a manifest and code were found (nothing more to learn).
// Files under skipped or hidden directories are ignored.
func observe(s *Signals, rel string) bool {
	parts := strings.Split(rel, "/")
	for _, d := range parts[:len(parts)-1] {
		if skipDirs[d] || strings.HasPrefix(d, ".") {
			return false
		}
	}
	name := parts[len(parts)-1]
	if len(parts) == 1 {
		if manifests[name] {
			s.Manifest = true
		}
		if strings.HasPrefix(strings.ToLower(name), "readme") {
			s.Readme = true
		}
	}
	if codeExts[strings.ToLower(filepath.Ext(name))] {
		s.CodeFiles = true
	}
	return s.CodeFiles && s.Manifest
}

// walkSignals is the fallback without Git: a bounded directory walk.
func walkSignals(root string, s *Signals) error {
	seen := 0
	errStop := errors.New("stop")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable entries are ignored
		}
		if d.IsDir() {
			if path != root && (skipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if seen++; seen > maxWalkEntries {
			return errStop
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return nil
		}
		if observe(s, filepath.ToSlash(rel)) {
			return errStop
		}
		return nil
	})
	if err != nil && !errors.Is(err, errStop) {
		return err
	}
	return nil
}

func hasGitHistory(root string) bool {
	if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
		return false
	}
	cmd := procattr.NoWindow(exec.Command("git", "-C", root, "rev-list", "-n", "1", "HEAD"))
	return cmd.Run() == nil
}

// DetectKind classifies root as KindExisting or KindNew. Existing means there
// is real material to investigate: code files, git history or a manifest.
// A README alone is a weak signal (fresh repositories often start with one)
// and does not make a project existing.
func DetectKind(root string) (string, error) {
	s, err := DetectSignals(root)
	if err != nil {
		return "", err
	}
	if s.CodeFiles || s.GitHistory || s.Manifest {
		return KindExisting, nil
	}
	return KindNew, nil
}

// FindRoot resolves the project root for start: the Git top-level when start
// is inside a repository, otherwise start itself. It refuses unsafe roots.
func FindRoot(start string) (string, error) {
	abs, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	root := abs
	cmd := procattr.NoWindow(exec.Command("git", "-C", abs, "rev-parse", "--show-toplevel"))
	if out, err := cmd.Output(); err == nil {
		if top := strings.TrimSpace(string(out)); top != "" {
			root = top
		}
	}
	if err := ValidateRoot(root); err != nil {
		return "", err
	}
	return root, nil
}

// ValidateRoot refuses directories that are never legitimate project roots:
// the filesystem root, $HOME and the system temporary directories themselves.
func ValidateRoot(root string) error {
	if IsFilesystemRoot(root) {
		return fmt.Errorf("refusing to use %s as a project root", root)
	}
	clean := resolve(root)
	unsafe := []string{"/", os.TempDir(), "/tmp", "/var/tmp"}
	if home, err := os.UserHomeDir(); err == nil {
		unsafe = append(unsafe, home)
	}
	for _, u := range unsafe {
		if clean == resolve(u) {
			return fmt.Errorf("refusing to use %s as a project root", root)
		}
	}
	return nil
}

func resolve(p string) string {
	p = filepath.Clean(p)
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

// IsFilesystemRoot reports a path that is a filesystem root on any OS: "/", a
// Windows drive root (C:, C:\, C:/) or a UNC share root (\\host\share). It is a
// pure string check, so it behaves the same on every platform.
func IsFilesystemRoot(p string) bool {
	q := strings.ReplaceAll(strings.TrimSpace(p), "\\", "/")
	if q == "" {
		return false
	}
	if strings.HasPrefix(q, "//") {
		parts := strings.FieldsFunc(q, func(r rune) bool { return r == '/' })
		return len(parts) <= 2
	}
	if len(q) >= 2 && q[1] == ':' && ((q[0] >= 'a' && q[0] <= 'z') || (q[0] >= 'A' && q[0] <= 'Z')) {
		return strings.Trim(q[2:], "/") == ""
	}
	return strings.Trim(q, "/") == "" && strings.HasPrefix(q, "/")
}
