package desktop

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// Proc is one running copy of an installed executable.
type Proc struct {
	PID       uint32
	Path      string    // full image path
	Started   time.Time // zero when unknown
	HasWindow bool      // owns a visible top-level window
}

// Processes is the process port. Only same-user processes can be inspected or
// stopped; others are skipped, never escalated.
type Processes interface {
	// Running lists the processes (other than this one) whose image is one of
	// exes, all of which live inside dir. Nothing outside dir is ever listed.
	Running(dir string, exes []string) ([]Proc, error)
	// Stop closes p gracefully, waits up to grace, then terminates it and waits
	// (bounded) for it to be gone.
	Stop(p Proc, grace time.Duration) error
}

// Grace is how long a copy gets to close on its own before it is terminated.
const Grace = 5 * time.Second

// hungAfter is how old a window-less copy must be to count as hung.
const hungAfter = 60 * time.Second

func closeTitle(n int) string {
	if n == 1 {
		return "Close 1 running copy of GabitoTL (unsaved work in it is lost)"
	}
	return fmt.Sprintf("Close %d running copies of GabitoTL (unsaved work in them is lost)", n)
}

func (in *Installer) logf(format string, a ...any) {
	if in.Log != nil {
		in.Log(format, a...)
	}
}

// runningCopies lists the copies of the owned executables in dir, ignoring
// lookup errors (they only mean "nothing known to close").
func (in *Installer) runningCopies(dir string) []Proc {
	if in.Procs == nil || dir == "" {
		return nil
	}
	ps, err := in.Procs.Running(dir, ownedFiles(dir))
	if err != nil {
		in.logf("process lookup failed: %v", err)
	}
	return ps
}

// RunningCopies lists the running copies of the app installed for o.
func (in *Installer) RunningCopies(o Options) []Proc { return in.runningCopies(in.dir(o)) }

// HungCopies are the copies with no visible window that have lived over a
// minute: a startup that never produced a window.
func HungCopies(ps []Proc, now time.Time) []Proc {
	var out []Proc
	for _, p := range ps {
		if !p.HasWindow && !p.Started.IsZero() && now.Sub(p.Started) > hungAfter {
			out = append(out, p)
		}
	}
	return out
}

// stopAll stops every copy. Failures are joined; with strict the error also
// tells the person how to proceed, as the install must not go on half-way.
func (in *Installer) stopAll(ps []Proc, strict bool) error {
	var errs []error
	for _, p := range ps {
		in.logf("closing pid=%d path=%s", p.PID, p.Path)
		if err := in.Procs.Stop(p, Grace); err != nil {
			in.logf("could not close pid=%d: %v", p.PID, err)
			errs = append(errs, fmt.Errorf("process %d (%s): %w", p.PID, filepath.Base(p.Path), err))
		}
	}
	err := errors.Join(errs...)
	if err != nil && strict {
		return fmt.Errorf("%d running copy(ies) of GabitoTL could not be closed, so nothing was copied; close them in Task Manager and try again: %w", len(errs), err)
	}
	return err
}

// stripExtended drops the Win32 extended-length prefix (\\?\C:\x, or
// \\?\UNC\srv\share\x) so such paths compare equal to their plain form.
func stripExtended(p string) string {
	switch {
	case len(p) >= 8 && strings.EqualFold(p[:8], `\\?\UNC\`):
		return `\\` + p[8:]
	case strings.HasPrefix(p, `\\?\`) || strings.HasPrefix(p, `\\.\`):
		return p[4:]
	}
	return p
}

func normPath(p string) string { return strings.ToLower(filepath.Clean(stripExtended(p))) }

// Owned reports whether path is one of exes (case-insensitive, cleaned, with a
// \\?\ prefix normalized away) and inside dir. Adapters use it to filter
// candidates.
func Owned(path, dir string, exes []string) bool {
	c := normPath(path)
	if !sameOrInside(c, normPath(dir)) {
		return false
	}
	for _, e := range exes {
		if c == normPath(e) {
			return true
		}
	}
	return false
}

// procHandle is one open handle on a process, used for the whole stop
// sequence: while it is open the PID cannot be reused, so everything done
// through it targets the process whose identity was verified.
type procHandle interface {
	// Identity returns the live image path and start time of the process.
	Identity() (image string, started time.Time, err error)
	// CloseWindows asks the process's windows to close and reports whether it
	// owns a visible one.
	CloseWindows() (visible bool)
	// Wait reports whether the process exited within d.
	Wait(d time.Duration) bool
	Terminate() error
}

// sameProcess is the identity decision: act only when the live process has the
// same image and the same start time as the one that was listed. An unknown
// start time cannot be verified, so it counts as a different process.
func sameProcess(p Proc, image string, started time.Time) bool {
	if p.Started.IsZero() || !started.Equal(p.Started) {
		return false
	}
	return Owned(image, filepath.Dir(stripExtended(p.Path)), []string{p.Path})
}

// stopProcess closes p gracefully, then terminates it, through h. A process
// that is no longer p (exited, or its PID reused) is "already gone": nil.
func stopProcess(p Proc, grace time.Duration, h procHandle) error {
	image, started, err := h.Identity()
	if err != nil {
		if h.Wait(0) {
			return nil
		}
		return fmt.Errorf("verify identity: %w", err)
	}
	if !sameProcess(p, image, started) {
		return nil // already gone: this PID is not the listed process any more
	}
	visible := h.CloseWindows()
	if p.HasWindow || visible {
		if h.Wait(grace) {
			return nil
		}
	}
	if err := h.Terminate(); err != nil {
		return fmt.Errorf("terminate: %w", err)
	}
	if !h.Wait(5 * time.Second) {
		return errors.New("still running after terminate")
	}
	return nil
}
