package cli

import (
	"context"
	"fmt"

	"github.com/winnstorm/gabitotl/internal/installer"
	"github.com/winnstorm/gabitotl/internal/installer/ui"
)

// ensureEngramCLI installs Engram before the apply when the plan uses it and
// it is missing, then returns the refreshed inventory and plan. A failure is
// shown as a warning and the install goes on: only the Engram server entry and
// the project's local memory wait for the binary.
func ensureEngramCLI(ctx context.Context, env Env, m ui.Messages, ienv installer.Env, inv installer.Inventory, plan installer.Plan) (installer.Inventory, installer.Plan) {
	ep, need := installer.EngramNeeded(ctx, ienv, plan)
	if !need {
		return inv, plan
	}
	key := "gui.step.engram-install"
	if ep.Action != "install" {
		key = "gui.step.engram-path"
	}
	fmt.Fprintf(env.Stdout, "%s %s\n", ui.SymOK, m.Format(key, "version", ep.Pinned))
	inv2, plan2, out, err := installer.EnsureEngram(ctx, ienv, inv, plan, func(id string) {
		fmt.Fprintf(env.Stdout, "  %s\n", m.Format("gui.engram."+id))
	})
	if err != nil {
		fmt.Fprintf(env.Stderr, "%s %v\n", ui.SymWarn, err)
		return inv, plan
	}
	if out.Installed {
		fmt.Fprintf(env.Stdout, "%s %s\n", ui.SymOK, m.Format("gui.step.engram-done", "version", out.Version))
	}
	if out.Installed {
		fmt.Fprintf(env.Stdout, "%s %s\n", ui.SymWarn, m.Format("gui.engram.restart"))
	}
	if out.NotOnPath {
		fmt.Fprintf(env.Stdout, "%s %s\n", ui.SymWarn, m.Format("gui.engram.notonpath", "dir", out.Dir))
	}
	return inv2, plan2
}
