//go:build windows

// Command gabitotl-manager is the GabitoTL desktop app for Windows: the setup
// wizard on a fresh machine and the Manager once installed. It is a Wails v2
// shell (WebView2) around the headless backend in internal/gui and embeds a
// plain HTML/CSS/JS frontend; there is no npm step and no wails CLI needed.
//
// Build (from the repository root, on Windows or cross-compiling):
//
//	go build -tags desktop,production -ldflags "-H windowsgui" -o gabitotl-manager.exe ./cmd/gabitotl-manager
//
// The icon, version info and manifest are linked from the committed
// rsrc_windows_amd64.syso; regenerate it with `go generate` in
// internal/icongen. Flags: --manage opens the Manager, --uninstall the
// uninstall screen; with no flag an installed app opens the Manager and a
// fresh machine the wizard.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"

	"github.com/winnstorm/gabitotl/cmd/gabitotl-manager/frontend"
	"github.com/winnstorm/gabitotl/cmd/gabitotl-manager/internal/route"
	"github.com/winnstorm/gabitotl/internal/applog"
	"github.com/winnstorm/gabitotl/internal/cli"
	"github.com/winnstorm/gabitotl/internal/desktop"
	"github.com/winnstorm/gabitotl/internal/gui"
	"github.com/winnstorm/gabitotl/internal/projects"
	"github.com/winnstorm/gabitotl/internal/projects/opencodedb"
	"github.com/winnstorm/gabitotl/internal/version"
)

var appVersion = version.Version

func main() {
	lg := openLog()
	defer lg.Close()
	// A panic before or around wails.Run would otherwise vanish: the GUI exe has
	// no console. Log it with its stack and tell the person where the log is.
	defer func() {
		if r := recover(); r != nil {
			lg.Panic("main", r)
			fatal(lg, fmt.Sprintf("GabitoTL stopped unexpectedly: %v", r))
			os.Exit(1)
		}
	}()
	if err := run(lg); err != nil {
		lg.Errorf("run: %v", err)
		fatal(lg, "GabitoTL could not start: "+err.Error())
		os.Exit(1)
	}
}

// fatal shows a native message box that names the log file.
func fatal(lg *applog.Logger, msg string) {
	if p := lg.Path(); p != "" {
		msg += "\n\nDetails: " + p
	}
	messageBox("GabitoTL", msg)
}

func run(lg *applog.Logger) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	wv := webView2Version()
	lg.Infof("start: version=%s exe=%s args=%s go=%s webview2=%q", appVersion, exe, applog.SafeArgs(os.Args[1:]), goVersion(), wv)
	if wv == "" {
		lg.Errorf("WebView2 runtime not found in the registry (Wails may offer to install it)")
	}
	sibling := filepath.Join(filepath.Dir(exe), "gabitotl.exe")
	if _, err := os.Stat(sibling); err != nil {
		sibling = ""
	}
	inst := desktop.New()
	inst.EmbeddedCLI = bundledCLI
	// Order: CLI next to this exe, else the default install dir when the setup
	// bundles one. The Service overrides it with the path it installed.
	cliPath := sibling
	if cliPath == "" && len(bundledCLI) > 0 {
		cliPath = filepath.Join(inst.Env.DefaultInstallDir, desktop.CLIExe)
	}
	deps := cli.GUIDeps(func() string { return cliPath })
	deps.Desktop = inst
	// Projects: the CLI's dependency-free sources + OpenCode's SQLite (linked only
	// here) + the folders this Manager remembers.
	env := projects.OSEnv()
	srcs := append(projects.Defaults(env), opencodedb.Source{Env: env},
		projects.Func{Label: projects.SrcGabitoTL, List: func() []string { return route.LoadProjects(statePath()) }})
	deps.Projects = func(ctx context.Context) ([]projects.Project, error) {
		return projects.Scan(ctx, srcs, projects.DefaultOptions(env.Home)), nil
	}
	deps.RememberProject = func(p string) error { return route.AddProject(statePath(), p) }
	deps.ForgetProject = func(p string) error { return route.ForgetProject(statePath(), p) }
	svc := gui.New(deps)

	app := &App{svc: svc, exe: exe, cliPath: cliPath, log: lg}
	st := svc.DesktopStatus()
	app.route = route.Resolve(os.Args[1:], st.Installed)
	lg.Infof("route=%s installed=%v installDir=%s", app.route, st.Installed, st.InstallDir)

	data := route.WebViewDataDir(localDir(), app.route)
	lg.Infof("webview data dir: %s", data)
	lg.Infof("wails.Run")
	err = wails.Run(&options.App{
		Title: "GabitoTL", Width: 960, Height: 640, MinWidth: 960, MinHeight: 640,
		AssetServer:    &assetserver.Options{Assets: frontend.Assets},
		OnStartup:      app.startup,
		OnDomReady:     func(context.Context) { app.domReady(); lg.Infof("dom ready") },
		OnShutdown:     func(context.Context) { lg.Infof("shutdown") },
		Bind:           []any{app},
		Logger:         wailsLog{lg},
		ErrorFormatter: func(err error) any { lg.Errorf("bound call: %v", err); return err.Error() },
		Windows:        &windows.Options{WebviewUserDataPath: data, Theme: windows.SystemDefault},
	})
	lg.Infof("wails.Run returned: %v", err)
	return err
}
