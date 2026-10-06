package cli

import (
	"fmt"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/identity"
	"github.com/winnstorm/gabitotl/internal/installer/ui"
	rt "github.com/winnstorm/gabitotl/internal/runtime"
)

// runHome is `gabitotl` with no arguments. It never starts an agent: without a
// .gabitotl it begins the onboarding (the installer questions); with one it
// prints a short status.
func runHome(env Env) int {
	m := ui.Default()
	root := ""
	if wd, err := env.Getwd(); err == nil {
		if r, err := gabitotl.FindRoot(wd); err == nil {
			root = r
		}
	}
	switch {
	case root == "":
		printHelp(env.Stdout)
		fmt.Fprintf(env.Stdout, "\n%s\n", m.Format("home.not_project"))
		return ExitOK
	case !gabitotl.Exists(root):
		if !env.IsTTY { // onboarding asks questions; it cannot without a terminal
			printHelp(env.Stdout)
			fmt.Fprintf(env.Stdout, "\n%s\n%s\n", m.Format("home.onboarding"), m.Format("refuse.no_tty"))
			return ExitOK
		}
		fmt.Fprintf(env.Stdout, "%s\n\n", m.Format("home.onboarding"))
		return execute(env, installFlags{root: root}, "install")
	}
	l := gabitotl.NewLayout(root)
	fmt.Fprintf(env.Stdout, "%s\n\n", m.Format("home.status.title"))
	if u, err := identity.Active(root); err == nil {
		fmt.Fprintf(env.Stdout, "%s %s\n", ui.SymOK, m.Format("home.status.identity", "detail", u.Name))
	} else {
		fmt.Fprintf(env.Stdout, "%s %s\n", ui.SymWarn, m.Format("home.status.identity.none"))
	}
	if mem, err := gabitotl.LoadMemory(l); err == nil {
		fmt.Fprintf(env.Stdout, "%s %s\n", ui.SymOK, m.Format("home.status.memory", "detail", fmt.Sprintf("%s (%s, %s)", mem.Provider, mem.Mode, mem.ProjectKey)))
	}
	if r, err := rt.ResolveRuntime(l); err == nil && r.Runtime != "" && r.Runtime != "unset" {
		d := r.Runtime
		if r.Model != "" {
			d += " / " + r.Model
		}
		fmt.Fprintf(env.Stdout, "%s %s\n", ui.SymOK, m.Format("home.status.runtime", "detail", d))
	}
	fmt.Fprintf(env.Stdout, "\n%s\n", m.Format("home.status.hint"))
	return ExitOK
}
