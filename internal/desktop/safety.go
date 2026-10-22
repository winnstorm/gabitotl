package desktop

import (
	"fmt"
	"path"
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
	// Marker is the pending-uninstall file. When set, every delete of the
	// cleanup happens only while it exists, so a reinstall cancels the cleanup
	// by deleting it; the cleanup removes it at the end.
	Marker string
}

// PendingMarkerName is the file in Env.DataDir that says an uninstall cleanup
// is allowed to delete.
const PendingMarkerName = "uninstall.pending"

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
func RemovalCommandLine(r Removal) (string, error) {
	clean := func(ss []string) ([]string, error) {
		out := make([]string, len(ss))
		for i, s := range ss {
			c, err := cleanWinPath(s)
			if err != nil {
				return nil, err
			}
			out[i] = c
		}
		return out, nil
	}
	quote := func(ss []string) []string {
		out := make([]string, len(ss))
		for i, s := range ss {
			out[i] = `"` + s + `"`
		}
		return out
	}
	rawFiles, err := clean(r.Files)
	if err != nil {
		return "", err
	}
	rawFolders, err := clean(r.Folders)
	if err != nil {
		return "", err
	}
	var dir, data string
	if r.Dir != "" {
		if dir, err = cleanWinPath(r.Dir); err != nil {
			return "", err
		}
	}
	if r.DataDir != "" {
		if data, err = cleanWinPath(r.DataDir); err != nil {
			return "", err
		}
	}
	var marker string
	if r.Marker != "" {
		if marker, err = cleanWinPath(r.Marker); err != nil {
			return "", err
		}
		marker = `"` + marker + `"`
	}
	// guard makes a delete conditional on the marker (when there is one).
	guard := func(s string) string {
		if marker == "" {
			return s
		}
		return `if exist ` + marker + ` ` + s
	}
	files, folders := quote(rawFiles), quote(rawFolders)
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
		conds = append(conds, guard(`if exist `+t+` (`+strings.Join(act, " & ")+`)`))
	}
	if len(conds) > 0 {
		steps = append(steps, fmt.Sprintf(`for /l %%i in (1,1,%d) do (%s)`, cleanupRounds, strings.Join(conds, " else ")))
	}
	if dir != "" {
		steps = append(steps, guard(`rmdir "`+dir+`" >nul 2>&1`))
	}
	// The log lists leftovers; the data dir is judged after the marker is gone.
	left := quote(append(append(append([]string{}, rawFiles...), rawFolders...), keepSet(dir)...))
	steps = append(steps, guard(`(for %p in (`+strings.Join(left, " ")+`) do @if exist %p echo left behind: %p>>"`+cleanupLog+`")`))
	switch {
	case marker != "" && data != "":
		steps = append(steps, `if exist `+marker+` (del /f /q `+marker+` >nul 2>&1 & rmdir "`+data+`" >nul 2>&1)`)
	case marker != "":
		steps = append(steps, `del /f /q `+marker+` >nul 2>&1`)
	case data != "":
		steps = append(steps, `rmdir "`+data+`" >nul 2>&1`)
	}
	return `cmd /d /s /c "` + strings.Join(steps, " & ") + `"`, nil
}

// cleanWinPath validates and normalizes one path for the removal command line,
// independent of the host OS: it must be a non-empty absolute Windows path
// (drive or UNC), free of cmd metacharacters and control characters, and not a
// drive or share root once cleaned (so "..", "." and doubled separators are
// resolved before anything is emitted).
func cleanWinPath(p string) (string, error) {
	bad := func(reason string) (string, error) { return "", &UnsafeDirError{Dir: p, Reason: reason} }
	if strings.TrimSpace(p) == "" {
		return bad("empty")
	}
	if i := strings.IndexAny(p, shellMeta); i >= 0 {
		return bad(fmt.Sprintf("contains %q", p[i]))
	}
	if i := strings.IndexFunc(p, func(r rune) bool { return r < 0x20 }); i >= 0 {
		return bad("contains a control character")
	}
	q := strings.ReplaceAll(stripExtended(p), `\`, "/")
	unc := strings.HasPrefix(q, "//")
	drive := len(q) >= 3 && q[1] == ':' && q[2] == '/' &&
		(q[0] >= 'a' && q[0] <= 'z' || q[0] >= 'A' && q[0] <= 'Z')
	switch {
	case unc:
		parts := strings.Split(strings.Trim(path.Clean("/"+q), "/"), "/")
		if len(parts) < 3 || parts[0] == "" { // host, share, and something below
			return bad("is a drive or share root")
		}
		return `\\` + strings.Join(parts, `\`), nil
	case drive:
		rest := path.Clean("/" + q[3:])
		if rest == "/" {
			return bad("is a drive or share root")
		}
		return q[:2] + strings.ReplaceAll(rest, "/", `\`), nil
	}
	return bad("not absolute")
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
