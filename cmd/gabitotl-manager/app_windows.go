//go:build windows

package main

import (
	"context"
	"os"
	"os/exec"
	goruntime "runtime"
	"syscall"

	wr "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/winnstorm/gabitotl/cmd/gabitotl-manager/internal/route"
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

func (a *App) startup(ctx context.Context) { a.ctx = ctx }

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

// CheckSignIn checks the Git platform login of the project.
func (a *App) CheckSignIn(root string) (gui.SignIn, error) {
	return a.svc.CheckSignIn(a.ctx, root)
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
// window. When this already is the installed copy it only closes.
func (a *App) Finish(installDir string) error {
	target := route.RelaunchTarget(installDir, desktop.ManagerExe)
	if target != "" && !sameFile(target, a.exe) {
		cmd := exec.Command(target, "--manage")
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000008 | 0x00000200} // DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP
		if err := cmd.Start(); err != nil {
			return err
		}
		_ = cmd.Process.Release()
	}
	wr.Quit(a.ctx)
	return nil
}

// Quit closes the window.
func (a *App) Quit() { wr.Quit(a.ctx) }

func sameFile(a, b string) bool {
	x, errA := os.Stat(a)
	y, errB := os.Stat(b)
	return errA == nil && errB == nil && os.SameFile(x, y)
}
