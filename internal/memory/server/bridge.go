package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/winnstorm/gabitotl/internal/version"
)

// Forwarder sends one tool call to the real memory server and returns its
// JSON result. The stdio bridge has no store and no identity of its own: the
// remote server authenticates the token and decides the actor.
type Forwarder func(ctx context.Context, tool string, args any) (json.RawMessage, error)

// BridgeConfig configures NewBridge.
type BridgeConfig struct {
	Forward Forwarder
	// DefaultProject fills "project" on calls that omit it (the project_key of
	// memory.md), so an agent does not have to know it.
	DefaultProject string
	Logger         *slog.Logger
}

// NewBridge returns an MCP server that exposes exactly the tools of the memory
// server (same names, descriptions and input schemas) and forwards every call.
// Runtimes only ever need a stdio command entry for it.
func NewBridge(cfg BridgeConfig) (*mcp.Server, error) {
	if cfg.Forward == nil {
		return nil, errors.New("bridge: a forwarder is required")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	srv := mcp.NewServer(&mcp.Implementation{Name: "gabitotl-memory-bridge", Title: "GabitoTL Memory (bridge)", Version: version.Version},
		&mcp.ServerOptions{Instructions: instructions})
	b := bridge{cfg}
	addForward[SearchIn](srv, ToolSearch, b, true)
	addForward[SaveIn](srv, ToolSave, b, true)
	addForward[UpdateIn](srv, ToolUpdate, b, true)
	addForward[SummaryIn](srv, ToolSessionSummary, b, true)
	addForward[CurrentIn](srv, ToolCurrentProject, b, false)
	addForward[CurrentIn](srv, ToolDoctor, b, false)
	addForward[ActiveIn](srv, ToolActiveWork, b, true)
	return srv, nil
}

type bridge struct{ cfg BridgeConfig }

func addForward[In any](srv *mcp.Server, tool string, b bridge, withProject bool) {
	mcp.AddTool(srv, descriptor(tool), func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, map[string]any, error) {
		var args any = in
		if withProject && b.cfg.DefaultProject != "" {
			raw, err := json.Marshal(in)
			if err != nil {
				return nil, nil, err
			}
			m := map[string]any{}
			if err := json.Unmarshal(raw, &m); err != nil {
				return nil, nil, err
			}
			if p, _ := m["project"].(string); p == "" {
				m["project"] = b.cfg.DefaultProject
			}
			args = m
		}
		raw, err := b.cfg.Forward(ctx, tool, args)
		if err != nil {
			b.cfg.Logger.Warn("forward failed", "tool", tool, "error", err.Error())
			return nil, nil, err
		}
		out := map[string]any{}
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &out); err != nil {
				return nil, nil, err
			}
		}
		if tool == ToolCurrentProject && b.cfg.DefaultProject != "" {
			if p, _ := out["project"].(string); p == "" {
				out["project"] = b.cfg.DefaultProject
			}
		}
		return nil, out, nil
	})
}

type nopReadCloser struct{ io.Reader }

func (nopReadCloser) Close() error { return nil }

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

// ServeBridge runs the bridge over in/out (stdin/stdout of the process) until
// ctx ends or the client disconnects. Nothing but the protocol is written to out.
func ServeBridge(ctx context.Context, srv *mcp.Server, in io.Reader, out io.Writer) error {
	return srv.Run(ctx, &mcp.IOTransport{Reader: nopReadCloser{in}, Writer: nopWriteCloser{out}})
}
