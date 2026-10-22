package cli

import (
	"context"
	"github.com/winnstorm/gabitotl/internal/engraminstall"
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
	// Isolate the home directory too: a saved persona language (lang: en) in the
	// real home must not change the messages the tests assert.
	if home, err := os.MkdirTemp("", "gabitotl-home-"); err == nil {
		os.Setenv("HOME", home)
		os.Setenv("USERPROFILE", home)
		defer os.RemoveAll(home)
	}
	// No test downloads Engram: this installer has no pinned release.
	engramInstallerFn = func() *engraminstall.Installer { return &engraminstall.Installer{GOOS: "plan9", GOARCH: "386"} }
	vcsRunnerFn = func() vcs.Runner { return noTools{} }
	vcsTTYFn = func() vcs.TTYRunner { return noTools{} }
	code := m.Run()
	os.Exit(code)
}
