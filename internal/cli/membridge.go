package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"regexp"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/memory/mcp"
	"github.com/winnstorm/gabitotl/internal/memory/server"
)

var envNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// runMemoryMCP is `gabitotl memory mcp`: a stdio MCP server with the team
// memory tools (memory_search, memory_save, ...). It is the ONE memory entry
// runtimes register, so it routes per project:
//
//   - without --url it reads .gabitotl/memory.md of the project it runs in on
//     every call: memory-mcp projects are forwarded to the team server over
//     HTTP, Engram projects are served through the engram CLI, and a directory
//     without a usable memory.md gets a clear "no memory provider configured
//     for this project" tool error (the server still starts);
//   - with --url it is a plain bridge to that server, whatever the project says.
func runMemoryMCP(env Env, args []string) int {
	fs := flag.NewFlagSet("memory mcp", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	url := fs.String("url", "", "bridge to this team server only (default: route by the provider of .gabitotl/memory.md)")
	tokenEnv := fs.String("token-env", "", "NAME of the environment variable holding your token (default: token of .gabitotl/memory.md)")
	root := fs.String("root", "", "project root (default: the Git top-level of the current directory)")
	insecure := fs.Bool("allow-insecure", false, "allow plain http:// to a non-loopback server (only through a tunnel you trust)")
	project := fs.String("project", "", "default project key (default: project_key of .gabitotl/memory.md)")
	if err := fs.Parse(args); err != nil {
		return flagExit(err)
	}
	fail := func(format string, a ...any) int {
		fmt.Fprintf(env.Stderr, "gabitotl memory mcp: "+format+"\n", a...)
		return ExitError
	}
	tokenRef := ""
	if *tokenEnv != "" {
		if !envNameRe.MatchString(*tokenEnv) {
			return fail("--token-env wants the NAME of a variable, never the token itself")
		}
		tokenRef = "env:" + *tokenEnv
	}
	if *url == "" {
		router := newMemoryRouter(env, *root, tokenRef, *project, *insecure)
		defer router.Close()
		return serveBridge(env, *root, server.BridgeConfig{Forward: router.Forward}, "memory-bridge", fail)
	}
	defProject := *project
	if tokenRef == "" || defProject == "" {
		if r, err := resolveRoot(env, *root); err == nil {
			if cfg, err := gabitotl.LoadMemory(gabitotl.NewLayout(r)); err == nil {
				if tokenRef == "" {
					tokenRef = cfg.Token
				}
				if defProject == "" {
					defProject = cfg.ProjectKey
				}
			}
		}
	}
	if tokenRef == "" {
		return fail("no token reference: pass --token-env NAME or set token: env:NAME in .gabitotl/memory.md")
	}
	renv, err := runtimeEnvFn("")
	if err != nil {
		return fail("%v", err)
	}
	tr, err := mcp.NewHTTPTransport(mcp.HTTPOptions{URL: *url, TokenRef: tokenRef, AllowInsecure: *insecure,
		Getenv: mcp.Resolver{Getenv: renv.Getenv, Home: renv.Home}.Lookup})
	if err != nil {
		return fail("%v", err)
	}
	defer tr.Close()
	return serveBridge(env, *root, server.BridgeConfig{Forward: tr.Call, DefaultProject: defProject}, "memory-bridge", fail)
}

// serveBridge runs the stdio bridge until the client goes away.
func serveBridge(env Env, root string, cfg server.BridgeConfig, runtimeName string, fail func(string, ...any) int) int {
	// stdout belongs to the protocol: logs go to stderr and never carry tokens.
	cfg.Logger = slog.New(slog.NewTextHandler(env.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	srv, err := server.NewBridge(cfg)
	if err != nil {
		return fail("%v", err)
	}
	ctx, stop := signalCtxFn()
	defer stop()
	// Like `memory serve --stdio`, this process lives as long as the agent
	// session. Notices go to stderr: stdout belongs to the protocol.
	if r, err := resolveRoot(env, root); err == nil {
		defer joinSession(env, r, presence{longLived: true, runtime: runtimeName})()
	}
	if err := server.ServeBridge(ctx, srv, env.Stdin, env.Stdout); err != nil && !errors.Is(err, context.Canceled) {
		return fail("%v", err)
	}
	return ExitOK
}
