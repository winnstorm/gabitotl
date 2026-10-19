package desktop

import (
	"fmt"
	"path/filepath"
	"strings"
)

// UnsafeDirError means an install directory was refused.
type UnsafeDirError struct {
	Dir    string
	Reason string
}

func (e *UnsafeDirError) Error() string {
	return fmt.Sprintf("unsafe install directory %q: %s", e.Dir, e.Reason)
}

// shellMeta are characters that would escape the quoting of the removal
// command line (or expand inside it).
const shellMeta = `"%&|<>^`

// safeInstallDir guards both install and uninstall: the directory must be
// absolute, not a drive or share root, free of cmd metacharacters, and neither
// equal to nor an ancestor of any protected directory.
func safeInstallDir(dir string, protected []string) error {
	bad := func(reason string) error { return &UnsafeDirError{Dir: dir, Reason: reason} }
	if strings.TrimSpace(dir) == "" {
		return bad("empty")
	}
	if i := strings.IndexAny(dir, shellMeta); i >= 0 {
		return bad(fmt.Sprintf("contains %q", dir[i]))
	}
	if !filepath.IsAbs(dir) {
		return bad("not absolute")
	}
	clean := filepath.Clean(dir)
	if rest := strings.TrimPrefix(clean, filepath.VolumeName(clean)); strings.Trim(rest, `\/`) == "" {
		return bad("is a drive or share root")
	}
	for _, p := range protected {
		if p == "" {
			continue
		}
		if sameOrInside(filepath.Clean(p), clean) {
			return bad("is, or contains, the protected folder " + p)
		}
	}
	return nil
}

// sameOrInside reports whether inner equals outer or lies below it
// (case-insensitively, as Windows paths are).
func sameOrInside(inner, outer string) bool {
	i, o := strings.ToLower(inner), strings.ToLower(outer)
	sep := string(filepath.Separator)
	return i == o || strings.HasPrefix(i, strings.TrimRight(o, sep)+sep)
}

// RemovalCommandLine builds the complete command line of the detached cleanup
// process (no shell parsing by Go: it goes to SysProcAttr.CmdLine verbatim):
//
//	cmd /d /s /c "<script>"
//
// With /s cmd strips exactly the outer pair of quotes, so inner quotes around
// paths with spaces survive. The script runs five rounds of "wait ~1s, delete
// the owned files" (the first rounds cover the parent still holding its exe
// open), then rmdir without /s, which fails harmlessly on a non-empty dir.
// Paths must have passed safeInstallDir: no quote or %, & | < > ^.
func RemovalCommandLine(dir string, files []string) string {
	var q []string
	for _, f := range files {
		q = append(q, `"`+f+`"`)
	}
	script := fmt.Sprintf(`for /l %%i in (1,1,5) do (ping -n 2 127.0.0.1 >nul & del /f /q %s >nul 2>&1) & rmdir "%s" >nul 2>&1`,
		strings.Join(q, " "), dir)
	return `cmd /d /s /c "` + script + `"`
}
