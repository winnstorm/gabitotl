package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/winnstorm/gabitotl/internal/installer"
	"github.com/winnstorm/gabitotl/internal/installer/ui"
	"github.com/winnstorm/gabitotl/internal/tui"
)

// tuiRunFn runs the full-screen interface; tests replace it.
var tuiRunFn = tui.Run

// executeTUI is the interactive install on a real terminal. It only wires the
// flags and the injected Env into the TUI; detection, questions-to-ask, plan and
// apply all come from internal/installer.
func executeTUI(env Env, f installFlags, ienv installer.Env, root string, paths ui.Paths) int {
	m := ui.Default()
	ctx := context.Background()
	before := projectProvider(root)
	opts := installer.Options{
		Runtimes: f.runtimes, Memory: f.memory, User: f.user, UserName: f.name,
		MigratePersona: f.migratePersona, RetireGentleKiro: f.retireGentleKiro, RemoveGentleAI: f.removeGentle, PreserveEngram: f.preserveE, NoGuardHooks: f.noGuardHooks, PersonaLang: f.personaLang,
	}
	if opts.UserName == "" {
		opts.UserName = f.user
	}
	opts.Delivery, _ = f.delivery.answers() // already validated by execute
	cfg := tui.Config{
		Backend: tui.NewBackend(ienv), Messages: m, Paths: paths, Options: opts,
		Need: func(inv installer.Inventory, o installer.Options) ui.Need {
			return openQuestions(root, ienv.Home, inv, o)
		},
		SuggestedName: suggestName(ctx, ienv, root),
		NoColor:       tui.NoColor(os.Getenv),
		Timeout:       installTimeout,
	}
	out, err := tuiRunFn(cfg, env.Stdin, env.Stdout)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl: %v\n", err)
		return ExitError
	}
	if !out.Ran {
		fmt.Fprintf(env.Stdout, "%s %s\n", ui.SymOK, m.Format("tui.quit.nothing"))
		return ExitOK
	}
	// The screen is gone now: print the same closing report the plain flow prints.
	if out.Cancelled {
		fmt.Fprintf(env.Stdout, "%s %s\n", ui.SymOK, m.Format("tui.apply.cancelled"))
		return ExitError
	}
	ui.RenderOutcome(env.Stdout, env.Stderr, m, out.Result, out.Inv, paths, out.Err)
	if out.Err != nil {
		return ExitError
	}
	if after := projectProvider(root); before != "" && after != before {
		fmt.Fprintf(env.Stdout, "%s %s\n", ui.SymWarn, m.Format("memory.changed.advisory"))
	}
	return ExitOK
}
