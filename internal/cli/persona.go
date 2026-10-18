package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"slices"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/persona"
	"github.com/winnstorm/gabitotl/internal/tokens"
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
		fmt.Fprintf(env.Stderr, "persona: %d characters, ~%d tokens (chars/4 estimate)\n", len([]rune(text)), tokens.EstimateLegacy(text))
	}
	return ExitOK
}

// runPersonaMode shows or sets the chat concision mode of the active person.
func runPersonaMode(env Env, args []string) int {
	fs := flag.NewFlagSet("persona mode", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	if slices.Contains(args, "--hook") || slices.Contains(args, "-hook") {
		fs.SetOutput(io.Discard) // a hook never writes to stderr
	}
	rootFlag := fs.String("root", "", "project root (default: search upward from the current directory)")
	hook := fs.Bool("hook", false, "print the active mode as SessionStart hook output (never fails)")
	format := fs.String("format", formatClaude, "with --hook: claude (additionalContext JSON) or text")
	if err := fs.Parse(args); err != nil {
		if *hook {
			return ExitOK // a hook never fails the host
		}
		return flagExit(err)
	}
	if *hook {
		return runPersonaModeHook(env, *rootFlag, *format)
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

// runPersonaModeHook backs the Claude Code SessionStart hook: it injects the
// active mode so the agent needs no tool call. Like every hook it exits 0.
func runPersonaModeHook(env Env, rootFlag, format string) int {
	root, err := resolveRoot(env, rootFlag)
	if err != nil {
		root = ""
	}
	home, _ := os.UserHomeDir()
	m := gabitotl.LoadPersonaMode(gabitotl.NewLayout(root), gabitotl.ActiveUserID(), home)
	emitHook(env, format, "SessionStart", persona.HookContext(m))
	return ExitOK
}
