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

// runMemoryMCP is `gabitotl memory mcp`: a stdio MCP server that forwards every
// call to the team memory server over HTTP. It is what runtimes register as the
// memory MCP entry in team mode, so they only ever need a stdio command.
func runMemoryMCP(env Env, args []string) int {
	fs := flag.NewFlagSet("memory mcp", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	url := fs.String("url", "", "team server endpoint (default: url of .gabitotl/memory.md)")
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
	defProject := *project
	if *url == "" || tokenRef == "" || defProject == "" {
		r, err := resolveRoot(env, *root)
		if err != nil {
			if *url == "" {
				return fail("no --url and no GabitoTL project here (%v); run it inside a project or pass --url and --token-env", err)
			}
		} else if cfg, err := gabitotl.LoadMemory(gabitotl.NewLayout(r)); err == nil {
			if cfg.Provider != gabitotl.MemoryMemoryMCP && *url == "" {
				return fail("this project uses %q memory, not memory-mcp; nothing to bridge", cfg.Provider)
			}
			if *url == "" {
				*url = cfg.URL
			}
			if tokenRef == "" {
				tokenRef = cfg.Token
			}
			if defProject == "" {
				defProject = cfg.ProjectKey
			}
		} else if *url == "" {
			return fail("cannot read .gabitotl/memory.md: %v", err)
		}
	}
	if *url == "" {
		return fail(".gabitotl/memory.md has no url; pass --url")
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
	// stdout belongs to the protocol: logs go to stderr and never carry tokens.
	logger := slog.New(slog.NewTextHandler(env.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	srv, err := server.NewBridge(server.BridgeConfig{Forward: tr.Call, DefaultProject: defProject, Logger: logger})
	if err != nil {
		return fail("%v", err)
	}
	ctx, stop := signalCtxFn()
	defer stop()
	if err := server.ServeBridge(ctx, srv, env.Stdin, env.Stdout); err != nil && !errors.Is(err, context.Canceled) {
		return fail("%v", err)
	}
	return ExitOK
}
