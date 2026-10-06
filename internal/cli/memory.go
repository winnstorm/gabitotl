package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/memory"
	"github.com/winnstorm/gabitotl/internal/memory/mcp"
	"github.com/winnstorm/gabitotl/internal/memory/providers"
)

// newProvider builds the configured memory provider. It is a variable so tests
// can inject a fake; the CLI is the composition root that may import providers.
var newProvider = func(cfg gabitotl.MemoryConfig) (memory.Provider, error) {
	var r mcp.Resolver
	if renv, err := runtimeEnvFn(""); err == nil {
		r = mcp.Resolver{Getenv: renv.Getenv, Home: renv.Home}
	}
	return providers.NewFactoryEnv(r).New(cfg)
}

const memoryTimeout = 30 * time.Second

func runMemory(env Env, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(env.Stderr, "Usage: gabitotl memory <doctor|search|server|serve|token|mcp|migrate> [flags]")
		return ExitUsage
	}
	switch args[0] {
	case "doctor":
		return runMemoryDoctor(env, args[1:])
	case "search":
		return runMemorySearch(env, args[1:])
	case "server":
		return runMemoryServer(env, args[1:])
	case "serve":
		return runMemoryServe(env, args[1:])
	case "token":
		return runMemoryToken(env, args[1:])
	case "mcp":
		return runMemoryMCP(env, args[1:])
	}
	fmt.Fprintf(env.Stderr, "gabitotl memory: unknown subcommand %q (want doctor, search, server, serve, token, mcp or migrate)\n", args[0])
	return ExitUsage
}

// openMemory resolves the project root, loads memory.md and builds the provider.
func openMemory(env Env, flagRoot string) (memory.Provider, gabitotl.MemoryConfig, error) {
	root, err := resolveRoot(env, flagRoot)
	if err != nil {
		return nil, gabitotl.MemoryConfig{}, err
	}
	cfg, err := gabitotl.LoadMemory(gabitotl.NewLayout(root))
	if err != nil {
		return nil, cfg, fmt.Errorf("cannot load .gabitotl/memory.md (run `gabitotl init`): %w", err)
	}
	p, err := newProvider(cfg)
	return p, cfg, err
}

func runMemoryDoctor(env Env, args []string) int {
	fs := flag.NewFlagSet("memory doctor", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	root := fs.String("root", "", "project root (default: Git top-level of the current directory)")
	if err := fs.Parse(args); err != nil {
		return flagExit(err)
	}
	p, cfg, err := openMemory(env, *root)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl memory doctor: %v\n", err)
		return ExitError
	}
	ctx, cancel := context.WithTimeout(context.Background(), memoryTimeout)
	defer cancel()
	rep := p.Doctor(ctx)
	fmt.Fprintf(env.Stdout, "provider: %s (mode: %s, project: %s)\n", rep.Provider, cfg.Mode, cfg.ProjectKey)
	for _, c := range rep.Checks {
		fmt.Fprintf(env.Stdout, "  [%s] %s: %s\n", c.Status, c.Name, c.Message)
	}
	if !rep.OK() {
		return ExitError
	}
	return ExitOK
}

func runMemorySearch(env Env, args []string) int {
	fs := flag.NewFlagSet("memory search", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	root := fs.String("root", "", "project root (default: Git top-level of the current directory)")
	typ := fs.String("type", "", "restrict to an entry type")
	limit := fs.Int("limit", memory.DefaultPerQuery, "maximum results")
	if err := fs.Parse(args); err != nil {
		return flagExit(err)
	}
	text := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if text == "" {
		fmt.Fprintln(env.Stderr, "Usage: gabitotl memory search [--root DIR] [--type T] [--limit N] <terms>")
		return ExitUsage
	}
	if *limit < 1 || *limit > memory.DefaultTotal {
		*limit = memory.DefaultPerQuery
	}
	p, cfg, err := openMemory(env, *root)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl memory search: %v\n", err)
		return ExitError
	}
	ctx, cancel := context.WithTimeout(context.Background(), memoryTimeout)
	defer cancel()
	entries, err := p.Search(ctx, memory.Query{Text: text, Project: cfg.ProjectKey, Type: memory.EntryType(*typ), Limit: *limit, MatchAny: true})
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl memory search: %v\n", err)
		return ExitError
	}
	if len(entries) == 0 {
		fmt.Fprintln(env.Stdout, "no matching memories")
		return ExitOK
	}
	for _, e := range entries {
		fmt.Fprintf(env.Stdout, "#%s [%s] %s\n", e.ID, e.Type, e.Title)
		if e.Actor != "" {
			fmt.Fprintf(env.Stdout, "    actor: %s\n", e.Actor)
		}
		if c := preview(e.Content, 160); c != "" {
			fmt.Fprintf(env.Stdout, "    %s\n", c)
		}
	}
	return ExitOK
}

func preview(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "..."
	}
	return s
}

func flagExit(err error) int {
	if errors.Is(err, flag.ErrHelp) {
		return ExitOK
	}
	return ExitUsage
}
