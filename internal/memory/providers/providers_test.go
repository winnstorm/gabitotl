package providers

import (
	"slices"
	"strings"
	"testing"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
)

func TestFactoryRegistersShippedProviders(t *testing.T) {
	f := NewFactory()
	if got := f.Names(); !slices.Equal(got, []string{"engram", "memory-mcp"}) {
		t.Fatalf("names: %v", got)
	}
	_, err := f.New(gabitotl.MemoryConfig{Provider: gabitotl.MemoryMemoryMCP, Mode: gabitotl.ModeTeam, ProjectKey: "p"})
	if err == nil || !strings.Contains(err.Error(), "no url") {
		t.Fatalf("memory-mcp without a url must say so, got %v", err)
	}
	p, err := f.New(gabitotl.MemoryConfig{Provider: gabitotl.MemoryMemoryMCP, Mode: gabitotl.ModeTeam, ProjectKey: "p",
		URL: "https://memoria.example.com/mcp", Token: "env:GABITOTL_MEMORY_TOKEN"})
	if err != nil || p == nil {
		t.Fatalf("a configured memory-mcp must build a provider: %v", err)
	}
}
