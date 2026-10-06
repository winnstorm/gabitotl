package all

import (
	"slices"
	"testing"
)

func TestRegistryHasV1Runtimes(t *testing.T) {
	want := []string{"claude-code", "opencode", "kiro-cli", "kiro-ide"}
	if got := Registry().Names(); !slices.Equal(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}
