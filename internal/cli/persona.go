package cli

import (
	"flag"
	"fmt"

	"github.com/winnstorm/gabitotl/internal/budget"
	"github.com/winnstorm/gabitotl/internal/persona"
)

func runPersona(env Env, args []string) int {
	return subcommands(env, "persona", args, map[string]func(Env, []string) int{"print": runPersonaPrint})
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
