// Package route decides which screen the manager opens first. It is plain Go so
// the rule is tested on every platform.
package route

import "path/filepath"

// Screens the page can open.
const (
	Wizard    = "wizard"
	Manage    = "manage"
	Uninstall = "uninstall"
)

// Resolve maps the command line and the install state to a screen. An explicit
// --uninstall or --manage wins (Apps & features and the Start menu pass them);
// otherwise an installed app opens the Manager and a fresh machine the wizard.
func Resolve(args []string, installed bool) string {
	for _, a := range args {
		if a == "--uninstall" {
			return Uninstall
		}
	}
	for _, a := range args {
		if a == "--manage" {
			return Manage
		}
	}
	if installed {
		return Manage
	}
	return Wizard
}

// RelaunchTarget is the manager executable inside the install folder, the copy
// the wizard opens when it finishes. It is empty when there is no install folder.
func RelaunchTarget(installDir, managerExe string) string {
	if installDir == "" {
		return ""
	}
	return filepath.Join(installDir, managerExe)
}

// WebViewDataDir is the WebView2 user-data folder of a run. The setup and the
// installed app each get their own: two processes sharing one folder share one
// WebView2 browser process, and a freshly launched installed copy then depends
// on the setup that is still shutting down. Other routes (the installed app) use
// the plain "webview" folder.
func WebViewDataDir(localAppData, screen string) string {
	if localAppData == "" {
		return ""
	}
	name := "webview"
	if screen == Wizard {
		name = "webview-setup"
	}
	return filepath.Join(localAppData, "GabitoTL", name)
}
