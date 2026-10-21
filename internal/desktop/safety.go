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

// dataFolders are the only folders of Env.DataDir the cleanup deletes
// recursively: the WebView2 profiles and the Manager's log.
var dataFolders = []string{"webview", "webview-setup", "logs"}

// Removal is the work of the detached cleanup.
type Removal struct {
	Dir     string   // install dir, removed last if empty
	Files   []string // owned files, deleted
	DataDir string   // %LOCALAPPDATA%\GabitoTL, removed if empty at the end
	Folders []string // folders under DataDir deleted recursively
	WaitPID int      // the cleanup waits for this process to exit first (0: none)
}

// safeDataDir applies the install-dir quoting rules to p and requires it to lie
// inside root (the data dir itself is allowed when p == root).
func safeDataDir(p, root string) error {
	bad := func(reason string) error { return &UnsafeDirError{Dir: p, Reason: reason} }
	if i := strings.IndexAny(p, shellMeta); i >= 0 {
		return bad(fmt.Sprintf("contains %q", p[i]))
	}
	if !filepath.IsAbs(p) || !sameOrInside(filepath.Clean(p), filepath.Clean(root)) {
		return bad("not inside the GabitoTL data folder")
	}
	if rest := strings.TrimPrefix(filepath.Clean(p), filepath.VolumeName(p)); strings.Trim(rest, `\/`) == "" {
		return bad("is a drive or share root")
	}
	return nil
}

// cleanupRounds bounds the wait for the owner process and for locked files:
// one round is about a second.
const cleanupRounds = 60

// cleanupLog is where leftovers are listed.
const cleanupLog = `%TEMP%\gabitotl-uninstall.log`

// RemovalCommandLine builds the complete command line of the detached cleanup
// process (no shell parsing by Go: it goes to SysProcAttr.CmdLine verbatim):
//
//	cmd /d /s /c "<script>"
//
// With /s cmd strips exactly the outer pair of quotes, so inner quotes around
// paths with spaces survive. The script (1) waits up to ~60 s for WaitPID to
// exit, (2) retries up to ~60 s, one second apart, until the owned files and
// the listed folders are gone, (3) removes the install and data dirs with
// rmdir (no /s: they only go when empty), and (4) appends whatever is left to
// the cleanup log. Paths must have passed safeInstallDir / safeDataDir: no
// quote or %, & | < > ^.
func RemovalCommandLine(r Removal) string {
	q := func(ss []string) []string {
		out := make([]string, len(ss))
		for i, s := range ss {
			out[i] = `"` + s + `"`
		}
		return out
	}
	files, folders := q(r.Files), q(r.Folders)
	var steps []string
	if r.WaitPID > 0 {
		pid := fmt.Sprint(r.WaitPID)
		steps = append(steps, fmt.Sprintf(`for /l %%i in (1,1,%d) do (tasklist /fi "PID eq %s" /nh 2>nul | find " %s " >nul && ping -n 2 127.0.0.1 >nul)`, cleanupRounds, pid, pid))
	}
	act := []string{`ping -n 2 127.0.0.1 >nul`}
	if len(files) > 0 {
		act = append(act, `del /f /q `+strings.Join(files, " ")+` >nul 2>&1`)
	}
	for _, f := range folders {
		act = append(act, `rmdir /s /q `+f+` >nul 2>&1`)
	}
	var conds []string
	for _, t := range append(append([]string{}, files...), folders...) {
		conds = append(conds, `if exist `+t+` (`+strings.Join(act, " & ")+`)`)
	}
	if len(conds) > 0 {
		steps = append(steps, fmt.Sprintf(`for /l %%i in (1,1,%d) do (%s)`, cleanupRounds, strings.Join(conds, " else ")))
	}
	if r.Dir != "" {
		steps = append(steps, `rmdir "`+r.Dir+`" >nul 2>&1`)
	}
	if r.DataDir != "" {
		steps = append(steps, `rmdir "`+r.DataDir+`" >nul 2>&1`)
	}
	left := q(append(append(append([]string{}, r.Files...), r.Folders...), keepSet(r.Dir, r.DataDir)...))
	steps = append(steps, `(for %p in (`+strings.Join(left, " ")+`) do @if exist %p echo left behind: %p>>"`+cleanupLog+`")`)
	return `cmd /d /s /c "` + strings.Join(steps, " & ") + `"`
}

func keepSet(ss ...string) []string {
	var out []string
	for _, s := range ss {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}
