package cli

import (
	"context"
	"os"
	"os/exec"
	"testing"

	"github.com/winnstorm/gabitotl/internal/vcs"
)

// noTools stands for a machine without gh or glab: tests never run the real ones.
type noTools struct{}

func (noTools) Run(context.Context, string, string, ...string) (string, error) {
	return "", &vcs.CommandError{Cmd: "tool", Err: exec.ErrNotFound}
}

func (noTools) RunTTY(context.Context, string, string, ...string) error { return exec.ErrNotFound }

// The active identity comes from the OS user; pin it so tests do not depend on
// who runs them. Individual tests override it with t.Setenv.
func TestMain(m *testing.M) {
	os.Setenv("GABITOTL_USER", "gabi")
	vcsRunnerFn = func() vcs.Runner { return noTools{} }
	vcsTTYFn = func() vcs.TTYRunner { return noTools{} }
	os.Exit(m.Run())
}
