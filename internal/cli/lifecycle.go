package cli

import (
	"context"
	"flag"
	"fmt"
	"strconv"

	"github.com/winnstorm/gabitotl/internal/installer"
	"github.com/winnstorm/gabitotl/internal/installer/ui"
)

// lifecycleEnv resolves the installer Env for commands that work on the global
// state (a project root is optional).
func lifecycleEnv(env Env, flagRoot string) (installer.Env, ui.Paths, error) {
	root := projectRoot(env, flagRoot)
	ienv, err := installerEnvFn(root)
	return ienv, ui.Paths{Home: ienv.Home, Root: root}, err
}

// confirmed returns true when the user agreed: --yes, or an explicit yes on a
// terminal. Without a terminal and without --yes it refuses and says so.
func confirmed(env Env, m ui.Messages, yes bool, question string) (ok, refused bool) {
	if yes {
		return true, false
	}
	if !env.IsTTY {
		fmt.Fprintf(env.Stderr, "%s %s\n", ui.SymError, m.Format("refuse.no_tty"))
		return false, true
	}
	return ui.NewPrompter(env.Stdin, env.Stdout, m).Confirm(question), false
}

func runDoctor(env Env, args []string) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	root := fs.String("root", "", "project root (default: Git top-level of the current directory)")
	if err := fs.Parse(args); err != nil {
		return flagExit(err)
	}
	ienv, _, err := lifecycleEnv(env, *root)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl doctor: %v\n", err)
		return ExitError
	}
	ctx, cancel := context.WithTimeout(context.Background(), installTimeout)
	defer cancel()
	d := installer.Doctor(ctx, ienv) // read-only
	ui.RenderDoctor(env.Stdout, ui.Default(), d)
	if d.Errors() > 0 {
		return ExitError
	}
	return ExitOK
}

func runRestore(env Env, args []string) int {
	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	yes := fs.Bool("yes", false, "do not ask")
	dry := fs.Bool("dry-run", false, "show what would change and change nothing")
	if len(args) > 0 && args[0] != "" && args[0][0] != '-' { // snapshot id first: restore <id> --yes
		id := args[0]
		if err := fs.Parse(args[1:]); err != nil {
			return flagExit(err)
		}
		return doRestore(env, id, *yes, *dry)
	}
	if err := fs.Parse(args); err != nil {
		return flagExit(err)
	}
	if fs.NArg() > 0 {
		return doRestore(env, fs.Arg(0), *yes, *dry)
	}
	ienv, _, err := lifecycleEnv(env, "")
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl restore: %v\n", err)
		return ExitError
	}
	snaps, err := installer.ListSnapshots(ienv)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl restore: %v\n", err)
		return ExitError
	}
	ui.RenderSnapshots(env.Stdout, ui.Default(), snaps)
	return ExitOK
}

func doRestore(env Env, id string, yes, dry bool) int {
	m := ui.Default()
	ienv, paths, err := lifecycleEnv(env, "")
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl restore: %v\n", err)
		return ExitError
	}
	_, changes, err := installer.PlanRestore(ienv, id)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl restore: %v\n", err)
		return ExitError
	}
	ui.RenderRestorePlan(env.Stdout, m, changes, paths)
	if dry || len(changes) == 0 {
		return ExitOK
	}
	if ok, refused := confirmed(env, m, yes, m.Format("restore.confirm")); refused {
		return ExitError
	} else if !ok {
		return ExitOK
	}
	changed, backup, err := installer.Restore(ienv, id, nil)
	if err != nil {
		fmt.Fprintf(env.Stderr, "%s %s\n", ui.SymError, m.Format("result.failed", "detail", err.Error()))
		return ExitError
	}
	fmt.Fprintf(env.Stdout, "%s %s\n", ui.SymOK, m.Format("restore.done", "count", strconv.Itoa(len(changed)), "path", paths.Short(backup)))
	return ExitOK
}

func runUninstall(env Env, args []string) int {
	fs := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	yes := fs.Bool("yes", false, "do not ask")
	dry := fs.Bool("dry-run", false, "show what would be removed and change nothing")
	var only listFlag
	fs.Var(&only, "runtime", "only this runtime (repeatable)")
	if err := fs.Parse(args); err != nil {
		return flagExit(err)
	}
	m := ui.Default()
	ienv, paths, err := lifecycleEnv(env, "")
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl uninstall: %v\n", err)
		return ExitError
	}
	pl, err := installer.PlanUninstall(ienv, only)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl uninstall: %v\n", err)
		return ExitUsage
	}
	ui.RenderUninstallPlan(env.Stdout, m, pl, paths)
	if *dry || pl.Empty() {
		return ExitOK
	}
	if ok, refused := confirmed(env, m, *yes, m.Format("uninstall.confirm")); refused {
		return ExitError
	} else if !ok {
		return ExitOK
	}
	res, err := installer.Uninstall(ienv, only, nil)
	for _, x := range res.Manual {
		fmt.Fprintf(env.Stdout, "%s %s\n", ui.SymWarn, m.Format("uninstall.manual", "detail", x))
	}
	if err != nil {
		fmt.Fprintf(env.Stderr, "%s %s\n", ui.SymError, m.Format("result.failed", "detail", err.Error()))
		return ExitError
	}
	ui.RenderUninstallResult(env.Stdout, m, res, paths)
	if res.Backup == "" {
		fmt.Fprintf(env.Stdout, "%s %s\n", ui.SymOK, m.Format("uninstall.done.nobackup"))
		return ExitOK
	}
	fmt.Fprintf(env.Stdout, "%s %s\n", ui.SymOK, m.Format("uninstall.done", "path", paths.Short(res.Backup)))
	return ExitOK
}

func runCleanup(env Env, args []string) int {
	fs := flag.NewFlagSet("cleanup", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	yes := fs.Bool("yes", false, "do not ask")
	dry := fs.Bool("dry-run", false, "list what would be removed and change nothing")
	days := fs.Int("days", installer.DefaultRetentionDays, "retention in days for legacy snapshots")
	if err := fs.Parse(args); err != nil {
		return flagExit(err)
	}
	m := ui.Default()
	ienv, paths, err := lifecycleEnv(env, "")
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl cleanup: %v\n", err)
		return ExitError
	}
	items, err := installer.PlanCleanup(ienv, *days)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl cleanup: %v\n", err)
		return ExitError
	}
	ui.RenderCleanup(env.Stdout, m, items, *days, paths)
	if *dry || len(items) == 0 {
		return ExitOK
	}
	if ok, refused := confirmed(env, m, *yes, m.Format("cleanup.confirm")); refused {
		return ExitError
	} else if !ok {
		return ExitOK
	}
	removed, err := installer.ApplyCleanup(ienv, items)
	if err != nil {
		fmt.Fprintf(env.Stderr, "%s %s\n", ui.SymError, m.Format("result.failed", "detail", err.Error()))
		return ExitError
	}
	fmt.Fprintf(env.Stdout, "%s %s\n", ui.SymOK, m.Format("cleanup.done", "count", strconv.Itoa(len(removed))))
	return ExitOK
}
