// Package route decides which screen the manager opens first. It is plain Go so
// the rule is tested on every platform.
package route

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
