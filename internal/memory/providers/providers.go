// Package providers is the composition root for memory providers: the only
// place that imports concrete providers. Workflow code receives a
// memory.Provider and never imports this package's dependencies.
package providers

import (
	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/memory"
	"github.com/winnstorm/gabitotl/internal/memory/engram"
	"github.com/winnstorm/gabitotl/internal/memory/mcp"
)

// NewFactory returns a factory with every shipped provider registered.
func NewFactory() *memory.Factory {
	f := memory.NewFactory()
	f.Register(gabitotl.MemoryEngram, engram.FromConfig)
	f.Register(gabitotl.MemoryMemoryMCP, mcp.FromConfig)
	return f
}
