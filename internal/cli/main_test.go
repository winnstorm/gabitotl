package cli

import (
	"os"
	"testing"
)

// The active identity comes from the OS user; pin it so tests do not depend on
// who runs them. Individual tests override it with t.Setenv.
func TestMain(m *testing.M) {
	os.Setenv("GABITOTL_USER", "gabi")
	os.Exit(m.Run())
}
