package cli

import (
	"flag"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/installer"
	rt "github.com/winnstorm/gabitotl/internal/runtime"
)

func runSkills(env Env, args []string) int {
	return subcommands(env, "skills", args, map[string]func(Env, []string) int{
		"list":    runSkillsList,
		"enable":  func(e Env, a []string) int { return runSkillsSet(e, a, true) },
		"disable": func(e Env, a []string) int { return runSkillsSet(e, a, false) },
	})
}

// runSkillsList shows each optional skill, whether it is on and where it is installed.
func runSkillsList(env Env, args []string) int {
	fs := flag.NewFlagSet("skills list", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	if err := fs.Parse(args); err != nil {
		return flagExit(err)
	}
	ienv, _, err := lifecycleEnv(env, "")
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl skills list: %v\n", err)
		return ExitError
	}
	states, err := installer.OptionalSkillStates(ienv)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl skills list: %v\n", err)
		return ExitError
	}
	for _, s := range states {
		state := "off"
		if s.Enabled {
			state = "on"
		}
		fmt.Fprintf(env.Stdout, "%s: %s\n", s.Name, state)
		for _, a := range s.Agents {
			fmt.Fprintf(env.Stdout, "  %s: %s\n", a.Label, map[bool]string{true: "installed", false: "not installed"}[a.Installed])
		}
		if len(s.Agents) == 0 {
			fmt.Fprintln(env.Stdout, "  no agent is managed by GabitoTL yet: run gabitotl install first")
		}
	}
	return ExitOK
}

// runSkillsSet turns one optional skill on or off in every managed agent. It
// shows the plan first; --dry-run stops there.
func runSkillsSet(env Env, args []string, on bool) int {
	verb := map[bool]string{true: "enable", false: "disable"}[on]
	fs := flag.NewFlagSet("skills "+verb, flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	dry := fs.Bool("dry-run", false, "show the plan and change nothing")
	// Go stops flag parsing at the first positional: accept flags after the name too.
	var pos []string
	for rest := args; ; {
		if err := fs.Parse(rest); err != nil {
			return flagExit(err)
		}
		if fs.NArg() == 0 {
			break
		}
		pos = append(pos, fs.Arg(0))
		rest = fs.Args()[1:]
	}
	if len(pos) != 1 {
		fmt.Fprintf(env.Stderr, "Usage: gabitotl skills %s <name> [--dry-run] (optional skills: %s)\n", verb, strings.Join(installer.OptionalSkills(), ", "))
		return ExitUsage
	}
	name := pos[0]
	if !slices.Contains(installer.OptionalSkills(), name) {
		fmt.Fprintf(env.Stderr, "gabitotl skills %s: %q is not an optional skill (known: %s)\n", verb, name, strings.Join(installer.OptionalSkills(), ", "))
		return ExitUsage
	}
	ienv, _, err := lifecycleEnv(env, "")
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl skills %s: %v\n", verb, err)
		return ExitError
	}
	sel := gabitotl.LoadOptionalSkills(ienv.Home)
	if on && !slices.Contains(sel, name) {
		sel = append(sel, name)
	}
	if !on {
		sel = slices.DeleteFunc(slices.Clone(sel), func(s string) bool { return s == name })
	}
	changes, err := installer.PlanSkillSelection(ienv, sel)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl skills %s: %v\n", verb, err)
		return ExitError
	}
	pending := 0
	for _, c := range changes {
		for _, a := range c.Actions {
			if a.Kind == rt.NoAction {
				continue
			}
			pending++
			fmt.Fprintf(env.Stdout, "%s %s\n", a.Kind, filepath.ToSlash(a.Path))
		}
	}
	if len(changes) == 0 {
		fmt.Fprintln(env.Stderr, "gabitotl skills: no agent is managed by GabitoTL yet; run gabitotl install first (the choice is kept for then)")
	}
	if *dry {
		fmt.Fprintf(env.Stdout, "dry run: %d change(s), nothing written\n", pending)
		return ExitOK
	}
	written, removed, err := installer.ApplySkillSelection(ienv, sel)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl skills %s: %v\n", verb, err)
		return ExitError
	}
	fmt.Fprintf(env.Stdout, "%s %s: %d file(s) written, %d removed\n", name, map[bool]string{true: "enabled", false: "disabled"}[on], len(written), len(removed))
	return ExitOK
}

// withSkills adds the --skill choices to the optional skills already on and marks
// the selection as set. Without --skill the saved selection is kept untouched.
func withSkills(o *installer.Options, home string, names []string) error {
	if len(names) == 0 {
		return nil
	}
	for _, n := range names {
		if !slices.Contains(installer.OptionalSkills(), n) {
			return fmt.Errorf("--skill %q: not an optional skill (known: %s)", n, strings.Join(installer.OptionalSkills(), ", "))
		}
	}
	sel := gabitotl.LoadOptionalSkills(home)
	for _, n := range names {
		if !slices.Contains(sel, n) {
			sel = append(sel, n)
		}
	}
	o.OptionalSkills, o.SetOptionalSkills = sel, true
	return nil
}
