// Package desktop is the OS-integration port of the GabitoTL Manager: a
// per-user (no admin) install of the app onto a Windows machine. It copies the
// executables into an install directory, adds that directory to the user PATH,
// creates Start menu / desktop shortcuts and registers an entry in Windows
// "Apps & features".
//
// The core in this file is platform-neutral: it plans and orchestrates through
// small ports (FileSystem, PathEnv, Shortcuts, Registry). The real adapters
// live in desktop_windows.go; every other OS gets adapters that fail with
// ErrUnsupported (desktop_other.go).
package desktop

import (
	"errors"
	"fmt"
	"path/filepath"
	"time"
)

// File names and display strings used by the install.
const (
	ManagerExe   = "gabitotl-manager.exe"
	CLIExe       = "gabitotl.exe"
	AppName      = "GabitoTL Manager"
	Publisher    = "GabitoTL"
	startMenuDir = "GabitoTL"
)

// ErrUnsupported is returned by the adapters on operating systems where the
// desktop integration does not exist.
var ErrUnsupported = errors.New("desktop integration is only supported on Windows")

// Options is what the user chose to install.
type Options struct {
	InstallDir      string // empty means Env.DefaultInstallDir
	Version         string // shown in Apps & features
	AddToPath       bool
	StartMenu       bool
	DesktopShortcut bool
}

// Step is one human-readable action of a plan; the GUI lists them on the
// review screen and ticks them off as Apply progresses.
type Step struct {
	ID    string
	Title string
}

// Step IDs.
const (
	StepClose     = "close"
	StepCopy      = "copy"
	StepPath      = "path"
	StepStartMenu = "startmenu"
	StepDesktop   = "desktop"
	StepRegister  = "register"
	StepUnlink    = "remove-shortcuts"
	StepUnpath    = "remove-path"
	StepUnreg     = "unregister"
	StepRemove    = "remove-files"
)

// Status reports what is currently installed.
type Status struct {
	InstallDir  string
	DirExists   bool
	Registered  bool
	Version     string // from the uninstall registration
	OnPath      bool
	StartMenu   bool
	DesktopLink bool
}

// Installed is true when the app is registered or its directory exists.
func (s Status) Installed() bool { return s.Registered || s.DirExists }

// Shortcut describes a .lnk file.
type Shortcut struct {
	Target      string
	WorkDir     string
	Icon        string
	Description string
}

// Registration is the Apps & features entry.
type Registration struct {
	DisplayName     string
	DisplayVersion  string
	Publisher       string
	DisplayIcon     string
	InstallLocation string
	UninstallString string
	ModifyPath      string
	EstimatedSizeKB uint32
	InstallDate     string // yyyymmdd
}

// FileSystem is the file port.
type FileSystem interface {
	// CopyFile copies src to dst atomically (temp file + rename) and returns
	// the number of bytes written.
	CopyFile(src, dst string) (int64, error)
	// WriteFile writes data to dst atomically, like CopyFile.
	WriteFile(dst string, data []byte) (int64, error)
	Exists(path string) bool
	RemoveAll(path string) error
	// ScheduleRemoval cleans up after this process has exited (a running
	// executable cannot delete itself on Windows); see Removal.
	ScheduleRemoval(r Removal) error
}

// PathEnv is the per-user PATH port.
type PathEnv interface {
	Get() (string, error)
	// Set stores the value (keeping its registry type) and tells running
	// programs that the environment changed.
	Set(value string) error
}

// Shortcuts is the .lnk port.
type Shortcuts interface {
	Create(lnkPath string, s Shortcut) error
	Remove(lnkPath string) error
}

// Registry is the Apps & features port.
type Registry interface {
	Register(r Registration) error
	Unregister() error
	// Registered returns the registered version.
	Registered() (version string, ok bool)
}

// Env carries the machine-specific locations, so the core never reads the
// process environment itself.
type Env struct {
	ExePath           string // the running executable
	DefaultInstallDir string // %LOCALAPPDATA%\Programs\GabitoTL
	StartMenuPrograms string // %APPDATA%\Microsoft\Windows\Start Menu\Programs
	DesktopDir        string
	// Protected lists directories an install dir may never be, nor contain
	// (home, Documents, %LOCALAPPDATA%, Program Files, ...).
	Protected []string
	// DataDir is %LOCALAPPDATA%\GabitoTL, where the Manager keeps its WebView2
	// profiles and logs. Empty means nothing there is cleaned up.
	DataDir string
	Today   func() string // yyyymmdd; nil means the empty string
}

// Installer orchestrates the install and uninstall through the ports.
type Installer struct {
	Env   Env
	FS    FileSystem
	Path  PathEnv
	Links Shortcuts
	Reg   Registry
	// EmbeddedCLI is the gabitotl.exe bundled inside the setup executable. It
	// is written to the install dir when no gabitotl.exe sits next to the
	// running manager. Nil means nothing is bundled.
	EmbeddedCLI []byte
	// Procs finds and stops running copies of the installed executables. Nil
	// means none are ever found.
	Procs Processes
	// Sleep pauses (nil means time.Sleep); tests replace it.
	Sleep func(time.Duration)
	// Log receives one line per process step (PIDs and paths, never secrets).
	Log func(format string, a ...any)
}

// validate refuses to run with unresolved locations: an empty
// %LOCALAPPDATA% would otherwise yield relative paths under the working dir.
func (e Env) validate() error {
	for name, p := range map[string]string{
		"executable": e.ExePath, "default install dir": e.DefaultInstallDir,
		"Start menu folder": e.StartMenuPrograms, "desktop folder": e.DesktopDir,
	} {
		if !filepath.IsAbs(p) {
			return fmt.Errorf("cannot locate the %s (got %q)", name, p)
		}
	}
	return nil
}

func (in *Installer) dir(o Options) string {
	if o.InstallDir != "" {
		return o.InstallDir
	}
	return in.Env.DefaultInstallDir
}

func (in *Installer) startMenuFolder() string {
	return filepath.Join(in.Env.StartMenuPrograms, startMenuDir)
}

func (in *Installer) startMenuLink() string {
	return filepath.Join(in.startMenuFolder(), AppName+".lnk")
}

func (in *Installer) desktopLink() string {
	return filepath.Join(in.Env.DesktopDir, AppName+".lnk")
}

// ownedFiles are the only files the installer writes into the install dir.
func ownedFiles(dir string) []string {
	return []string{filepath.Join(dir, ManagerExe), filepath.Join(dir, CLIExe)}
}

// UninstallString is the command Windows runs from Apps & features. The path
// is always quoted because the install dir may contain spaces.
func UninstallString(dir string) string {
	return `"` + filepath.Join(dir, ManagerExe) + `" --uninstall`
}

// ModifyString is the command behind the "Modify" button.
func ModifyString(dir string) string {
	return `"` + filepath.Join(dir, ManagerExe) + `" --manage`
}

// action is a plan step bound to its do/undo closures.
type action struct {
	Step
	do   func() error
	undo func()
	// commit runs once every step succeeded (drops the upgrade backups).
	commit func()
}

// Plan lists, in order, what Apply will do.
func (in *Installer) Plan(o Options) ([]Step, error) {
	if err := in.check(o); err != nil {
		return nil, err
	}
	return steps(in.installActions(o)), nil
}

// check validates the environment and the chosen install directory.
func (in *Installer) check(o Options) error {
	if err := in.Env.validate(); err != nil {
		return err
	}
	return safeInstallDir(in.dir(o), in.Env.Protected)
}

func steps(as []action) []Step {
	out := make([]Step, len(as))
	for i, a := range as {
		out[i] = a.Step
	}
	return out
}

// Apply installs, reporting each step before it runs. If a step fails, the
// steps already applied are rolled back (best effort) and the error is
// returned wrapped with the failing step.
func (in *Installer) Apply(o Options, progress func(Step)) error {
	if err := in.check(o); err != nil {
		return err
	}
	in.cancelPendingRemoval()
	as := in.installActions(o)
	for i, a := range as {
		if progress != nil {
			progress(a.Step)
		}
		if err := a.do(); err != nil {
			for j := i - 1; j >= 0; j-- {
				if as[j].undo != nil {
					as[j].undo()
				}
			}
			return fmt.Errorf("%s: %w", a.Title, err)
		}
	}
	if missing := in.missingAfterCopy(o); missing != "" {
		for j := len(as) - 1; j >= 0; j-- {
			if as[j].undo != nil {
				as[j].undo()
			}
		}
		in.logf("install verification failed: %s is missing", missing)
		return fmt.Errorf("the install is incomplete: %s is missing after copying (a cleanup from an earlier uninstall may have removed it); close GabitoTL and run the setup again", missing)
	}
	for _, a := range as {
		if a.commit != nil {
			a.commit()
		}
	}
	return nil
}

// settleWait is how long an install waits for a cleanup round that was already
// running when the pending-uninstall marker was removed (one round is ~1 s).
const settleWait = 3 * time.Second

// pendingMarker is the file that lets an uninstall cleanup delete; "" when
// there is no data dir.
func (in *Installer) pendingMarker() string {
	if d := in.Env.DataDir; d != "" && safeDataDir(d, d) == nil {
		return filepath.Join(d, PendingMarkerName)
	}
	return ""
}

// cancelPendingRemoval removes the pending-uninstall marker so a cleanup left
// over from an earlier uninstall stops deleting, then waits out its in-flight
// round. Without this the cleanup would delete the files about to be copied.
func (in *Installer) cancelPendingRemoval() {
	m := in.pendingMarker()
	if m == "" || !in.FS.Exists(m) {
		return
	}
	in.logf("cancelling a pending uninstall cleanup: removing %s", m)
	if err := in.FS.RemoveAll(m); err != nil {
		in.logf("could not remove %s: %v", m, err)
	}
	sleep := in.Sleep
	if sleep == nil {
		sleep = time.Sleep
	}
	sleep(settleWait)
	in.logf("pending uninstall cleanup cancelled")
}

// missingAfterCopy returns the first executable that should have been
// installed but is not on disk, or "".
func (in *Installer) missingAfterCopy(o Options) string {
	want := []string{filepath.Join(in.dir(o), ManagerExe)}
	if cli := in.CLITarget(o); cli != "" {
		want = append(want, cli)
	}
	for _, p := range want {
		if !in.FS.Exists(p) {
			return p
		}
	}
	return ""
}

func (in *Installer) installActions(o Options) []action {
	dir := in.dir(o)
	manager := filepath.Join(dir, ManagerExe)
	var size int64
	var as []action

	if procs := in.runningCopies(dir); len(procs) > 0 {
		as = append(as, action{
			Step: Step{StepClose, closeTitle(len(procs))},
			do:   func() error { return in.stopAll(procs, true) },
		})
	}

	var existed bool
	var b backups
	as = append(as, action{
		Step: Step{StepCopy, "Copy the app to " + dir},
		do: func() error {
			existed = in.FS.Exists(dir)
			b = backups{fs: in.FS, saved: map[string]string{}}
			if err := in.copyApp(dir, manager, &size, &b); err != nil {
				b.restore() // a failed step is not undone by Apply
				b.discard()
				if !existed {
					_ = in.FS.RemoveAll(dir)
				}
				return err
			}
			return nil
		},
		undo: func() {
			if existed {
				b.restore()
			} else {
				_ = in.FS.RemoveAll(dir)
			}
			b.discard()
		},
		commit: func() { b.discard() },
	})

	if o.AddToPath {
		var before string
		var changed bool
		as = append(as, action{
			Step: Step{StepPath, "Add " + dir + " to your PATH"},
			do: func() error {
				cur, err := in.Path.Get()
				if err != nil {
					return err
				}
				before = cur
				next, ch := AddPathEntry(cur, dir)
				if !ch {
					return nil
				}
				changed = true
				return in.Path.Set(next)
			},
			undo: func() {
				if changed {
					_ = in.Path.Set(before)
				}
			},
		})
	}

	link := func(id, title, lnk string) action {
		return action{
			Step: Step{id, title},
			do: func() error {
				return in.Links.Create(lnk, Shortcut{
					Target: manager, WorkDir: dir, Icon: manager, Description: AppName,
				})
			},
			undo: func() { _ = in.Links.Remove(lnk) },
		}
	}
	if o.StartMenu {
		a := link(StepStartMenu, "Create the Start menu entry", in.startMenuLink())
		a.undo = func() { _ = in.FS.RemoveAll(in.startMenuFolder()) }
		as = append(as, a)
	}
	if o.DesktopShortcut {
		as = append(as, link(StepDesktop, "Create a desktop shortcut", in.desktopLink()))
	}

	as = append(as, action{
		Step: Step{StepRegister, "Register in Windows Apps & features"},
		do: func() error {
			date := ""
			if in.Env.Today != nil {
				date = in.Env.Today()
			}
			kb := (size + 1023) / 1024
			return in.Reg.Register(Registration{
				DisplayName:     AppName,
				DisplayVersion:  o.Version,
				Publisher:       Publisher,
				DisplayIcon:     manager,
				InstallLocation: dir,
				UninstallString: UninstallString(dir),
				ModifyPath:      ModifyString(dir),
				EstimatedSizeKB: uint32(kb),
				InstallDate:     date,
			})
		},
		undo: func() { _ = in.Reg.Unregister() },
	})
	return as
}

// copyApp writes the manager and the CLI into dir. The bundled CLI wins over
// a gabitotl.exe next to the setup, which may be stale.
func (in *Installer) copyApp(dir, manager string, size *int64, b *backups) error {
	cliDst := filepath.Join(dir, CLIExe)
	if err := b.save(manager, in.Env.ExePath); err != nil {
		return err
	}
	if err := in.copyOne(in.Env.ExePath, manager, size); err != nil {
		return err
	}
	if len(in.EmbeddedCLI) > 0 {
		if err := b.save(cliDst, ""); err != nil {
			return err
		}
		n, err := in.FS.WriteFile(cliDst, in.EmbeddedCLI)
		if err != nil {
			return err
		}
		*size += n
		return nil
	}
	if cli := filepath.Join(filepath.Dir(in.Env.ExePath), CLIExe); in.FS.Exists(cli) {
		if err := b.save(cliDst, cli); err != nil {
			return err
		}
		return in.copyOne(cli, cliDst, size)
	}
	return nil
}

// backups keeps the previous version of files an upgrade overwrites, so a
// failure leaves the earlier install working.
type backups struct {
	fs    FileSystem
	saved map[string]string // dst -> backup path ("" = did not exist before)
}

// save remembers dst before it is overwritten. src is the file about to be
// copied there (so running from the install dir needs no backup).
func (b *backups) save(dst, src string) error {
	if _, done := b.saved[dst]; done {
		return nil
	}
	if src != "" && filepath.Clean(src) == filepath.Clean(dst) {
		return nil
	}
	if !b.fs.Exists(dst) {
		b.saved[dst] = ""
		return nil
	}
	bak := dst + ".bak"
	if _, err := b.fs.CopyFile(dst, bak); err != nil {
		return fmt.Errorf("back up %s: %w", filepath.Base(dst), err)
	}
	b.saved[dst] = bak
	return nil
}

// restore puts the previous files back (and removes new ones), best effort.
func (b *backups) restore() {
	for dst, bak := range b.saved {
		if bak == "" {
			_ = b.fs.RemoveAll(dst)
			continue
		}
		_, _ = b.fs.CopyFile(bak, dst)
	}
}

// discard deletes the backup copies.
func (b *backups) discard() {
	for _, bak := range b.saved {
		if bak != "" {
			_ = b.fs.RemoveAll(bak)
		}
	}
	b.saved = map[string]string{}
}

// CLITarget is where Apply puts gabitotl.exe for o, or "" when it will not be
// installed (no sibling next to the manager and nothing bundled).
func (in *Installer) CLITarget(o Options) string {
	if len(in.EmbeddedCLI) == 0 && !in.FS.Exists(filepath.Join(filepath.Dir(in.Env.ExePath), CLIExe)) {
		return ""
	}
	return filepath.Join(in.dir(o), CLIExe)
}

// copyOne copies src to dst unless it already is dst (running from the
// install dir), adding the bytes written to size.
func (in *Installer) copyOne(src, dst string, size *int64) error {
	if filepath.Clean(src) == filepath.Clean(dst) {
		return nil
	}
	n, err := in.FS.CopyFile(src, dst)
	if err != nil {
		return err
	}
	*size += n
	return nil
}

// UninstallPlan lists what Uninstall will do.
func (in *Installer) UninstallPlan(o Options) ([]Step, error) {
	if err := in.check(o); err != nil {
		return nil, err
	}
	return in.uninstallSteps(o), nil
}

func (in *Installer) uninstallSteps(o Options) []Step {
	var ss []Step
	if n := len(in.runningCopies(in.dir(o))); n > 0 {
		ss = append(ss, Step{StepClose, closeTitle(n)})
	}
	return append(ss,
		Step{StepUnlink, "Remove the shortcuts"},
		Step{StepUnpath, "Remove " + in.dir(o) + " from your PATH"},
		Step{StepUnreg, "Remove the Apps & features entry"},
		Step{StepRemove, "Delete the GabitoTL files in " + in.dir(o) + " once this window closes"},
	)
}

// Uninstall removes everything Apply created. It is best effort: every step
// runs, and the failures are joined. Running copies are closed first. Only the
// files GabitoTL installed are deleted, by a detached process after this one
// exits (the running executable lives there); the directory goes only if that
// leaves it empty.
func (in *Installer) Uninstall(o Options, progress func(Step)) error {
	if err := in.check(o); err != nil {
		return err
	}
	dir := in.dir(o)
	var errs []error
	run := func(s Step, f func() error) {
		if progress != nil {
			progress(s)
		}
		if err := f(); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", s.Title, err))
		}
	}
	for _, s := range in.uninstallSteps(o) {
		switch s.ID {
		case StepClose:
			run(s, func() error { return in.stopAll(in.runningCopies(dir), false) })
		case StepUnlink:
			run(s, func() error {
				return errors.Join(in.FS.RemoveAll(in.startMenuFolder()), in.Links.Remove(in.desktopLink()))
			})
		case StepUnpath:
			run(s, func() error {
				cur, err := in.Path.Get()
				if err != nil {
					return err
				}
				next, changed := RemovePathEntry(cur, dir)
				if !changed {
					return nil
				}
				return in.Path.Set(next)
			})
		case StepUnreg:
			run(s, in.Reg.Unregister)
		case StepRemove:
			run(s, func() error {
				r := in.removal(dir)
				if m := in.pendingMarker(); m != "" {
					if _, err := in.FS.WriteFile(m, []byte("pending\n")); err != nil {
						// No marker, no guard: an unguarded cleanup could delete a
						// reinstall, so nothing is scheduled.
						in.logf("could not write %s: %v (no cleanup scheduled)", m, err)
						return fmt.Errorf("could not write the uninstall marker %s: %w", m, err)
					}
					r.Marker = m
				}
				return in.FS.ScheduleRemoval(r)
			})
		}
	}
	return errors.Join(errs...)
}

// removal is what the detached cleanup deletes: the owned files, the Manager's
// own WebView2 profiles and logs, then the (then empty) install and data dirs.
func (in *Installer) removal(dir string) Removal {
	r := Removal{Dir: dir, Files: ownedFiles(dir)}
	if d := in.Env.DataDir; d != "" && safeDataDir(d, d) == nil {
		r.DataDir = d
		for _, n := range dataFolders {
			if p := filepath.Join(d, n); safeDataDir(p, d) == nil {
				r.Folders = append(r.Folders, p)
			}
		}
	}
	return r
}

// Status reports what is installed in the directory chosen by o.
func (in *Installer) Status(o Options) Status {
	dir := in.dir(o)
	s := Status{InstallDir: dir}
	s.DirExists = dir != "" && in.FS.Exists(filepath.Join(dir, ManagerExe))
	s.Version, s.Registered = in.Reg.Registered()
	if cur, err := in.Path.Get(); err == nil {
		s.OnPath = HasPathEntry(cur, dir)
	}
	s.StartMenu = in.FS.Exists(in.startMenuLink())
	s.DesktopLink = in.FS.Exists(in.desktopLink())
	return s
}
