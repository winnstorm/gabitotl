package cli

import (
	"flag"
	"fmt"
	"os"

	"github.com/winnstorm/gabitotl/internal/budget"
	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/persona"
)

func runPersona(env Env, args []string) int {
	return subcommands(env, "persona", args, map[string]func(Env, []string) int{"print": runPersonaPrint, "mode": runPersonaMode})
}

func runPersonaPrint(env Env, args []string) int {
	fs := flag.NewFlagSet("persona print", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	stats := fs.Bool("stats", false, "also print the estimated token size to stderr")
	if err := fs.Parse(args); err != nil {
		return flagExit(err)
	}
	text := persona.DefaultText()
	fmt.Fprint(env.Stdout, text)
	if *stats {
		fmt.Fprintf(env.Stderr, "persona: %d characters, ~%d tokens (chars/4 estimate)\n", len([]rune(text)), budget.EstimateTokens(text))
	}
	return ExitOK
}

// runPersonaMode shows or sets the chat concision mode of the active person.
func runPersonaMode(env Env, args []string) int {
	fs := flag.NewFlagSet("persona mode", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	rootFlag := fs.String("root", "", "project root (default: search upward from the current directory)")
	if err := fs.Parse(args); err != nil {
		return flagExit(err)
	}
	if fs.NArg() > 1 {
		fmt.Fprintln(env.Stderr, "Usage: gabitotl persona mode [normal|lite|full] [--root DIR]")
		return ExitUsage
	}
	var want persona.Mode
	if fs.NArg() == 1 {
		m, err := persona.ParseMode(fs.Arg(0))
		if err != nil {
			fmt.Fprintf(env.Stderr, "gabitotl persona mode: %v\n", err)
			return ExitUsage
		}
		want = m
	}
	root, err := resolveRoot(env, *rootFlag)
	if err != nil {
		if *rootFlag != "" {
			fmt.Fprintf(env.Stderr, "gabitotl persona mode: %v\n", err)
			return ExitUsage
		}
		root = "" // outside a project: the home file applies
	}
	home, _ := os.UserHomeDir()
	user := gabitotl.ActiveUserID()
	if want == "" {
		m := gabitotl.LoadPersonaMode(gabitotl.NewLayout(root), user, home)
		fmt.Fprintf(env.Stdout, "mode: %s\n%s", m, persona.Rules(m))
		return ExitOK
	}
	path, err := gabitotl.SavePersonaMode(root, user, home, want)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl persona mode: %v\n", err)
		return ExitError
	}
	fmt.Fprintf(env.Stdout, "mode: %s\nsaved in %s\n", want, path)
	return ExitOK
}
