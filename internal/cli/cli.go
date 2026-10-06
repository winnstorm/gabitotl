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

func commands() map[string]Command {
	list := []Command{
		{"install", "Install GabitoTL: detect, back up, migrate, configure (--dry-run, --yes)", runInstall},
		{"migrate", "Migrate from another harness (migrate gentle-ai)", runMigrate},
		{"doctor", "Check the installation and the project (read-only)", runDoctor},
		{"restore", "Restore configs from a migration snapshot (lists snapshots)", runRestore},
		{"uninstall", "Remove only what GabitoTL installed", runUninstall},
		{"cleanup", "Offer to delete expired legacy snapshots (30 days)", runCleanup},
		{"init", "Create .gabitotl/ in a project", runInit},
		{"memory", "Memory provider tools (doctor, search)", runMemory},
		{"scan", "Scan the project and print its map", runScan},
		{"vcs", "Git status and provider (status)", runVCS},
		{"backup", "Local file backups (create, list, restore, prune)", runBackup},
		{"worktree", "Git worktrees for teams (add, list, remove)", runWorktree},
		{"work", "Active work and overlap checks (start, update, status, overlap)", runWork},
		{"runtime", "Runtimes and models (list, detect, plan, apply, verify, remove, set)", runRuntime},
		{"import", "Import knowledge from other harnesses (detect, run)", runImport},
		{"persona", "Show the installed persona (print)", runPersona},
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
		return runHome(env)
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
