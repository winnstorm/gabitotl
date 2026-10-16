package cli

import (
	"flag"
	"fmt"
	"strings"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/installer"
	"github.com/winnstorm/gabitotl/internal/installer/ui"
)

// runMemoryUse is the non-interactive equivalent of the question "do you now work
// with other people?": it changes the memory provider of the project.
//
// It does not have a flow of its own. Moving to team memory is a re-run of the
// install flow with the provider fixed, so it shares everything the interactive
// install has: the team setup (host or client), the plan with the entry count of
// the Engram import, a snapshot of memory.md before any change, the tested
// Engram migrator (safety export, no duplicates, Engram untouched, verified) and
// a rollback that leaves memory.md on the old provider when anything fails.
func runMemoryUse(env Env, args []string) int {
	const usage = "Usage: gabitotl memory use <engram|memory-mcp> [--migrate] [--yes] [--dry-run] [--role host|client] [--url URL] [--token-env NAME] [--instance NAME] [--root DIR]"
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		fmt.Fprintln(env.Stderr, usage)
		return ExitUsage
	}
	target := args[0]
	fs := flag.NewFlagSet("memory use", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	root := fs.String("root", "", "project root (default: Git top-level of the current directory)")
	migrateMem := fs.Bool("migrate", false, "also import the existing Engram memory into the team memory (host only; Engram stays intact)")
	yes := fs.Bool("yes", false, "do not ask; required when there is no terminal")
	dry := fs.Bool("dry-run", false, "show the plan, with the number of entries to import, and change nothing")
	role := fs.String("role", "", "team memory role: host (this machine runs the server) or client (connect to one)")
	url := fs.String("url", "", "team memory server endpoint, for example https://memoria.example.com/mcp (client)")
	tokenEnv := fs.String("token-env", "", "NAME of the environment variable that holds your token (default "+installer.DefaultTokenEnv+"); never the token itself")
	instance := fs.String("instance", "", "host: named instance of the managed server")
	if err := fs.Parse(args[1:]); err != nil {
		return flagExit(err)
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(env.Stderr, "gabitotl memory use: unexpected argument %q\n", fs.Arg(0))
		return ExitUsage
	}
	if target != gabitotl.MemoryEngram && target != gabitotl.MemoryMemoryMCP {
		fmt.Fprintf(env.Stderr, "gabitotl memory use: the provider must be engram or memory-mcp, got %q\n%s\n", target, usage)
		return ExitUsage
	}

	m := ui.Default()
	rootDir, err := resolveRoot(env, *root)
	if err != nil {
		fmt.Fprintf(env.Stderr, "%s %s (%v)\n", ui.SymError, m.Format("refuse.no_project"), err)
		return ExitError
	}
	cfg, err := gabitotl.LoadMemory(gabitotl.NewLayout(rootDir))
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl memory use: cannot load .gabitotl/memory.md (run `gabitotl init`): %v\n", err)
		return ExitError
	}
	switch {
	case cfg.Provider == target:
		fmt.Fprintf(env.Stdout, "%s %s\n", ui.SymOK, m.Format("memory.use.same", "provider", target))
		return ExitOK
	case target == gabitotl.MemoryEngram:
		fmt.Fprintf(env.Stderr, "%s %s\n", ui.SymError, m.Format("memory.use.reverse"))
		return ExitError
	}
	interactive := env.IsTTY && !*yes && !*dry
	if *role == "" && *url == "" && !interactive {
		fmt.Fprintf(env.Stderr, "%s %s\n", ui.SymError, m.Format("memory.use.needs_role"))
		return ExitUsage
	}
	if *migrateMem && *role == gabitotl.RoleClient {
		fmt.Fprintf(env.Stderr, "%s %s\n", ui.SymError, m.Format("memory.use.migrate_needs_host"))
		return ExitUsage
	}
	return execute(env, installFlags{
		root: rootDir, memory: target, dryRun: *dry, yes: *yes, noTUI: true,
		memoryRole: *role, memoryURL: *url, memoryTokenEnv: *tokenEnv, memoryEngine: "auto", memoryInstance: *instance,
		importEngram: *migrateMem,
	}, installer.SourceInstall)
}
