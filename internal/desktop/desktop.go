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
	Exists(path string) bool
	RemoveAll(path string) error
	// ScheduleRemoval deletes files after this process has exited (a running
	// executable cannot delete itself on Windows).
	// Only the listed files are deleted, then dir itself if it is empty;
	// anything else the user keeps there survives.
	ScheduleRemoval(dir string, files []string) error
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
	Today     func() string // yyyymmdd; nil means the empty string
}

// Installer orchestrates the install and uninstall through the ports.
type Installer struct {
	Env   Env
	FS    FileSystem
	Path  PathEnv
	Links Shortcuts
	Reg   Registry
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
	return nil
}

func (in *Installer) installActions(o Options) []action {
	dir := in.dir(o)
	manager := filepath.Join(dir, ManagerExe)
	var size int64
	var as []action

	var existed bool
	as = append(as, action{
		Step: Step{StepCopy, "Copy the app to " + dir},
		do: func() error {
			existed = in.FS.Exists(dir)
			srcs := []string{in.Env.ExePath}
			if cli := filepath.Join(filepath.Dir(in.Env.ExePath), CLIExe); in.FS.Exists(cli) {
				srcs = append(srcs, cli)
			}
			for i, src := range srcs {
				dst := manager
				if i == 1 {
					dst = filepath.Join(dir, CLIExe)
				}
				if filepath.Clean(src) == filepath.Clean(dst) {
					continue // already running from the install dir
				}
				n, err := in.FS.CopyFile(src, dst)
				if err != nil {
					return err
				}
				size += n
			}
			return nil
		},
		undo: func() {
			if !existed {
				_ = in.FS.RemoveAll(dir)
			}
		},
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

// UninstallPlan lists what Uninstall will do.
func (in *Installer) UninstallPlan(o Options) ([]Step, error) {
	if err := in.check(o); err != nil {
		return nil, err
	}
	return in.uninstallSteps(o), nil
}

func (in *Installer) uninstallSteps(o Options) []Step {
	return []Step{
		{StepUnlink, "Remove the shortcuts"},
		{StepUnpath, "Remove " + in.dir(o) + " from your PATH"},
		{StepUnreg, "Remove the Apps & features entry"},
		{StepRemove, "Delete the GabitoTL files in " + in.dir(o) + " once this window closes"},
	}
}

// Uninstall removes everything Apply created. It is best effort: every step
// runs, and the failures are joined. Only the files GabitoTL installed are
// deleted, by a detached process after this one exits (the running executable
// lives there); the directory goes only if that leaves it empty.
func (in *Installer) Uninstall(o Options, progress func(Step)) error {
	if err := in.check(o); err != nil {
		return err
	}
	dir := in.dir(o)
	ss := in.uninstallSteps(o)
	var errs []error
	run := func(i int, f func() error) {
		if progress != nil {
			progress(ss[i])
		}
		if err := f(); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", ss[i].Title, err))
		}
	}
	run(0, func() error {
		return errors.Join(in.FS.RemoveAll(in.startMenuFolder()), in.Links.Remove(in.desktopLink()))
	})
	run(1, func() error {
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
	run(2, in.Reg.Unregister)
	run(3, func() error { return in.FS.ScheduleRemoval(dir, ownedFiles(dir)) })
	return errors.Join(errs...)
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
