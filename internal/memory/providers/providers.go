// Package providers is the composition root for memory providers: the only
// place that imports concrete providers. Workflow code receives a
// memory.Provider and never imports this package's dependencies.
package providers

import (
	"context"
	"os"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/memory"
	"github.com/winnstorm/gabitotl/internal/memory/engram"
	"github.com/winnstorm/gabitotl/internal/memory/mcp"
)

// NewFactory returns a factory with every shipped provider registered.
func NewFactory() *memory.Factory {
	home, _ := os.UserHomeDir()
	return NewFactoryEnv(mcp.Resolver{Home: home})
}

// NewFactoryEnv is NewFactory with an explicit environment for the memory-mcp
// client (variables and the home that holds its secrets file).
func NewFactoryEnv(r mcp.Resolver) *memory.Factory {
	f := memory.NewFactory()
	f.Register(gabitotl.MemoryEngram, engram.FromConfig)
	f.Register(gabitotl.MemoryMemoryMCP, func(cfg gabitotl.MemoryConfig) (memory.Provider, error) {
		return mcp.NewFromConfig(cfg, r)
	})
	return f
}

// EngramProject is one row of `engram projects list`.
type EngramProject struct {
	Name         string
	Observations int
}

// EngramProjects lists Engram projects through the real CLI (read-only). It
// fails when the engram binary is not installed.
func EngramProjects(ctx context.Context) ([]EngramProject, error) {
	r, err := engram.NewExecRunner("")
	if err != nil {
		return nil, err
	}
	ps, err := engram.New(engram.Options{Runner: r}).Projects(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]EngramProject, len(ps))
	for i, p := range ps {
		out[i] = EngramProject{p.Name, p.Observations}
	}
	return out, nil
}
