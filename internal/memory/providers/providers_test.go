package providers

import (
	"errors"
	"slices"
	"testing"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/memory/mcp"
)

func TestFactoryRegistersShippedProviders(t *testing.T) {
	f := NewFactory()
	if got := f.Names(); !slices.Equal(got, []string{"engram", "memory-mcp"}) {
		t.Fatalf("names: %v", got)
	}
	_, err := f.New(gabitotl.MemoryConfig{Provider: gabitotl.MemoryMemoryMCP, Mode: gabitotl.ModeTeam, ProjectKey: "p"})
	if !errors.Is(err, mcp.ErrNoTransport) {
		t.Fatalf("memory-mcp without a selected server must be ErrNoTransport, got %v", err)
	}
}
