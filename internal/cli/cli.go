// Package cli implements a stdlib-only subcommand dispatcher.
package cli

import (
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/winnstorm/gabitotl/internal/version"
)

// Exit codes.
const (
	ExitOK             = 0
	ExitError          = 1
	ExitUsage          = 2
	ExitNotImplemented = 2
)

// Env carries process-level dependencies so commands are testable in-process.
type Env struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	// Getwd returns the current working directory.
	Getwd func() (string, error)
	// IsTTY reports whether Stdin is an interactive terminal.
	IsTTY bool
}

// DefaultEnv builds an Env bound to the real process streams.
func DefaultEnv() Env {
	return Env{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Getwd:  os.Getwd,
		IsTTY:  isTerminal(os.Stdin),
	}
}

func isTerminal(f *os.File) bool {
	st, err := f.Stat()
	if err != nil {
		return false
	}
	return st.Mode()&os.ModeCharDevice != 0
}

// Command is a registered subcommand.
type Command struct {
	Name    string
	Summary string
	Run     func(env Env, args []string) int
}

func notImplemented(name string) func(Env, []string) int {
	return func(env Env, _ []string) int {
		fmt.Fprintf(env.Stderr, "gabitotl %s: not implemented yet\n", name)
		return ExitNotImplemented
	}
}

func commands() map[string]Command {
	list := []Command{
		{"install", "Install GabitoTL into a runtime", notImplemented("install")},
		{"doctor", "Diagnose installation and project state", notImplemented("doctor")},
		{"migrate", "Migrate configuration between versions", notImplemented("migrate")},
		{"restore", "Restore files from a local backup", notImplemented("restore")},
		{"uninstall", "Remove GabitoTL from a runtime", notImplemented("uninstall")},
		{"init", "Create .gabitotl/ in a project", runInit},
		{"memory", "Memory provider tools (doctor, search)", runMemory},
		{"scan", "Scan the project and print its map", runScan},
		{"version", "Print the version", runVersion},
	}
	m := make(map[string]Command, len(list)+1)
	for _, c := range list {
		m[c.Name] = c
	}
	m["help"] = Command{Name: "help", Summary: "Show help", Run: runHelp}
	return m
}

// Run dispatches args (without the program name) and returns the exit code.
func Run(args []string, env Env) int {
	if len(args) == 0 {
		printHelp(env.Stdout)
		return ExitOK
	}
	switch args[0] {
	case "-h", "--help":
		printHelp(env.Stdout)
		return ExitOK
	case "-v", "--version":
		return runVersion(env, nil)
	}
	cmd, ok := commands()[args[0]]
	if !ok {
		fmt.Fprintf(env.Stderr, "gabitotl: unknown command %q\n\n", args[0])
		printHelp(env.Stderr)
		return ExitUsage
	}
	return cmd.Run(env, args[1:])
}

func runVersion(env Env, _ []string) int {
	fmt.Fprintln(env.Stdout, version.String())
	return ExitOK
}

func runHelp(env Env, _ []string) int {
	printHelp(env.Stdout)
	return ExitOK
}

func printHelp(w io.Writer) {
	cmds := commands()
	names := make([]string, 0, len(cmds))
	for n := range cmds {
		names = append(names, n)
	}
	sort.Strings(names)
	fmt.Fprintln(w, "Usage: gabitotl <command> [flags]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands:")
	for _, n := range names {
		fmt.Fprintf(w, "  %-10s %s\n", n, cmds[n].Summary)
	}
}
