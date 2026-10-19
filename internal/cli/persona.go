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
	lang := fs.String("lang", "", "persona language: es or en (default: the one chosen at install, else es)")
	if err := fs.Parse(args); err != nil {
		return flagExit(err)
	}
	pl := gabitotl.ActivePersonaLang("")
	if *lang != "" {
		var err error
		if pl, err = persona.ParseLang(*lang); err != nil {
			fmt.Fprintf(env.Stderr, "gabitotl persona print: %v\n", err)
			return ExitUsage
		}
	}
	text := persona.TextFor(pl)
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
	// Go stops flag parsing at the first positional: accept flags after the mode too.
	var pos []string
	for rest := fs.Args(); len(rest) > 0; rest = fs.Args() {
		pos = append(pos, rest[0])
		if err := fs.Parse(rest[1:]); err != nil {
			if *hook {
				return ExitOK
			}
			return flagExit(err)
		}
	}
	if *hook {
		return runPersonaModeHook(env, *rootFlag, *format)
	}
	if len(pos) > 1 {
		fmt.Fprintln(env.Stderr, "Usage: gabitotl persona mode [normal|lite|full] [--root DIR]")
		return ExitUsage
	}
	var want persona.Mode
	if len(pos) == 1 {
		m, err := persona.ParseMode(pos[0])
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
		fmt.Fprintf(env.Stdout, "mode: %s\n%s", m, persona.RulesFor(m, gabitotl.ActivePersonaLang(home)))
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
	emitHook(env, format, "SessionStart", persona.HookContextFor(m, gabitotl.ActivePersonaLang(home)))
	return ExitOK
}
