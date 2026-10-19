package installer

import (
	"testing"

	rt "github.com/winnstorm/gabitotl/internal/runtime"
)

func TestRuntimeOptionsConfiguresTheMemoryBridge(t *testing.T) {
	f := newFixture(t)
	o := RuntimeOptions(f.env.RT(), f.root, rt.ScopeProject, "engram", true)
	if o.Scope != rt.ScopeProject || !o.MigrateGentleman || o.PersonaText == "" || len(o.Skills) == 0 {
		t.Fatalf("unexpected options: %+v", o)
	}
	if o.Memory.Command == "" {
		t.Fatal("the memory bridge must be configured for a chosen provider")
	}
}
