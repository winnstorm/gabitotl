//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	goruntime "runtime"
	"sync"
	"syscall"
	"time"

	wr "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/winnstorm/gabitotl/cmd/gabitotl-manager/internal/route"
	"github.com/winnstorm/gabitotl/internal/applog"
	"github.com/winnstorm/gabitotl/internal/desktop"
	"github.com/winnstorm/gabitotl/internal/gui"
)

// App is bound to the page: every exported method is callable from JavaScript
// as window.go.main.App.<Method>. The methods are thin pass-throughs to the
// headless gui.Service; they hold no install logic.
type App struct {
	ctx     context.Context
	svc     *gui.Service
	route   string
	exe     string
	cliPath string // gabitotl.exe next to this executable, "" when absent
	log     *applog.Logger
	shown   chan struct{} // closed when the page finished loading

	mu     sync.Mutex
	opID   uint64
	cancel context.CancelFunc // the running cancellable operation, if any
}

// Info is what the page needs before it draws the first screen.
type Info struct {
	Route      string `json:"route"`
	Version    string `json:"version"`
	OS         string `json:"os"`
	Arch       string `json:"arch"`
	Cwd        string `json:"cwd"`
	InstallDir string `json:"installDir"`
	Installed  bool   `json:"installed"`
	HasCLI     bool   `json:"hasCli"`
}

func (a *App) startup(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			a.log.Panic("startup", r)
		}
	}()
	a.ctx = ctx
	a.shown = make(chan struct{})
	a.log.Infof("startup: window created")
	// If the page never loads (WebView2 stuck, blocked profile folder), say so
	// instead of leaving an invisible process behind.
	go func() {
		select {
		case <-a.shown:
		case <-time.After(30 * time.Second):
			a.log.Errorf("the page did not finish loading within 30s")
			fatal(a.log, "GabitoTL is taking too long to open. If no window appears, close it from Task Manager and send the log.")
		}
	}()
}

func (a *App) domReady() {
	select {
	case <-a.shown:
	default:
		close(a.shown)
	}
}

// LogPath is the diagnostics log file shown in the Manager.
func (a *App) LogPath() string { return a.log.Path() }

// OpenLogFolder opens the folder with the diagnostics log in Explorer.
func (a *App) OpenLogFolder() error {
	dir := a.log.Dir()
	if dir == "" {
		return errors.New("no log folder")
	}
	a.log.Infof("open log folder: %s", dir)
	return exec.Command("explorer.exe", dir).Start()
}

// Info describes the start state: the screen to open and the machine.
func (a *App) Info() Info {
	st := a.svc.DesktopStatus()
	cwd, _ := os.Getwd()
	return Info{
		Route: a.route, Version: appVersion, OS: goruntime.GOOS, Arch: goruntime.GOARCH, Cwd: cwd,
		InstallDir: st.InstallDir, Installed: st.Installed, HasCLI: a.cliPath != "",
	}
}

// Messages is the text catalogue of lang ("es" or "en").
func (a *App) Messages(lang string) (map[string]string, error) { return a.svc.Messages(lang) }

// Language is the saved language.
func (a *App) Language() (string, error) { return a.svc.Language() }

// SetLanguage saves the language.
func (a *App) SetLanguage(lang string) error { return a.svc.SetLanguage(lang) }

// Detect inventories the machine; root "" means no project.
func (a *App) Detect(root string) (gui.DetectResult, error) {
	return a.svc.Detect(a.ctx, root)
}

// PlanInstall shows what the wizard would do without changing anything.
func (a *App) PlanInstall(ans gui.InstallAnswers) (gui.Plan, error) {
	return a.svc.PlanInstall(a.ctx, ans)
}

// Install runs the wizard and streams progress to the page as "install:progress"
// events (gui.Event).
func (a *App) Install(ans gui.InstallAnswers, planID string) (gui.InstallResult, error) {
	res, err := a.svc.Install(a.ctx, ans, planID, func(e gui.Event) {
		wr.EventsEmit(a.ctx, "install:progress", e)
	})
	if err != nil {
		// Return the result with the error inside so the page can show the
		// rollback and the failed steps instead of a bare message.
		res.Error = err.Error()
		a.log.Errorf("install failed: %v", err)
	} else {
		a.log.Infof("install finished")
	}
	return res, nil
}

const licenseURL = "https://github.com/winnstorm/gabitotl/blob/main/LICENSE"

// OpenLicense opens the license in the default browser.
func (a *App) OpenLicense() { wr.BrowserOpenURL(a.ctx, licenseURL) }

// ChooseFolder opens the native folder picker; "" means the person cancelled.
func (a *App) ChooseFolder(title, start string) (string, error) {
	return wr.OpenDirectoryDialog(a.ctx, wr.OpenDialogOptions{Title: title, DefaultDirectory: start})
}

// Finish opens the installed copy of the app on the Manager and closes this
// window. When this already is the installed copy it only closes. When the
// installed copy cannot be started (or dies at once) the error is returned and
// the window stays open so the page can tell the person; Quit closes it anyway.
func (a *App) Finish(installDir string) error {
	target := route.RelaunchTarget(installDir, desktop.ManagerExe)
	a.log.Infof("finish: installDir=%s target=%s self=%s", installDir, target, a.exe)
	if target != "" && !sameFile(target, a.exe) {
		if err := a.launch(target, installDir); err != nil {
			a.log.Errorf("finish: %v", err)
			return err
		}
	}
	a.quitSoon()
	return nil
}

// launch starts the installed Manager detached and watches it for a moment: a
// process that exits right away is a failure the person must hear about.
func (a *App) launch(target, dir string) error {
	if _, err := os.Stat(target); err != nil {
		return fmt.Errorf("%s: %w", target, err)
	}
	cmd := exec.Command(target, "--manage")
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000008 | 0x00000200} // DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("%s: %w", target, err)
	}
	a.log.Infof("finish: started pid=%d", cmd.Process.Pid)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		a.log.Errorf("finish: the installed app exited at once: %v", err)
		return fmt.Errorf("%s closed right after starting (%v)", target, err)
	case <-time.After(2 * time.Second):
		return nil
	}
}

// quitSoon closes the window after the binding's reply has reached the page; a
// Quit inside the bound call can drop that reply.
func (a *App) quitSoon() {
	go func() {
		time.Sleep(300 * time.Millisecond)
		wr.Quit(a.ctx)
	}()
}

// Quit closes the window.
func (a *App) Quit() { a.log.Infof("quit"); a.quitSoon() }

func sameFile(a, b string) bool {
	x, errA := os.Stat(a)
	y, errB := os.Stat(b)
	return errA == nil && errB == nil && os.SameFile(x, y)
}
