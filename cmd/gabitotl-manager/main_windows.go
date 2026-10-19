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
	"log"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"

	"github.com/winnstorm/gabitotl/cmd/gabitotl-manager/frontend"
	"github.com/winnstorm/gabitotl/cmd/gabitotl-manager/internal/route"
	"github.com/winnstorm/gabitotl/internal/cli"
	"github.com/winnstorm/gabitotl/internal/desktop"
	"github.com/winnstorm/gabitotl/internal/gui"
	"github.com/winnstorm/gabitotl/internal/version"
)

var appVersion = version.Version

func main() {
	exe, err := os.Executable()
	if err != nil {
		log.Fatal(err)
	}
	// The guard hooks run the gabitotl CLI; only offer them when it ships next
	// to this executable.
	cliPath := filepath.Join(filepath.Dir(exe), "gabitotl.exe")
	if _, err := os.Stat(cliPath); err != nil {
		cliPath = ""
	}
	inst := desktop.New()
	inst.EmbeddedCLI = bundledCLI
	if cliPath == "" && len(bundledCLI) > 0 {
		// The setup installs the bundled CLI here, so hooks may point at it.
		cliPath = filepath.Join(inst.Env.DefaultInstallDir, desktop.CLIExe)
	}
	deps := cli.GUIDeps(cliPath)
	deps.Desktop = inst
	svc := gui.New(deps)

	app := &App{svc: svc, exe: exe, cliPath: cliPath}
	app.route = route.Resolve(os.Args[1:], svc.DesktopStatus().Installed)

	data := ""
	if cache, err := os.UserCacheDir(); err == nil {
		data = filepath.Join(cache, "GabitoTL", "WebView2")
	}
	err = wails.Run(&options.App{
		Title: "GabitoTL", Width: 960, Height: 640, MinWidth: 960, MinHeight: 640,
		AssetServer: &assetserver.Options{Assets: frontend.Assets},
		OnStartup:   app.startup,
		Bind:        []any{app},
		Windows:     &windows.Options{WebviewUserDataPath: data, Theme: windows.SystemDefault},
	})
	if err != nil {
		log.Fatal(err)
	}
}
