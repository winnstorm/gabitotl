package installer

import (
	"os"
	"path/filepath"
	"testing"

	rt "github.com/winnstorm/gabitotl/internal/runtime"
)

func TestCodeGraphTools(t *testing.T) {
	local := t.TempDir()
	launcher := filepath.Join(local, "codegraph", "current", "bin", "codegraph.cmd")
	env := func(goos string, onPath bool) rt.Env {
		return rt.Env{GOOS: goos,
			Getenv: func(k string) string {
				if k == "LOCALAPPDATA" {
					return local
				}
				return ""
			},
			LookPath: func(n string) (string, error) {
				if onPath && n == "codegraph" {
					return "/x/codegraph", nil
				}
				return "", os.ErrNotExist
			}}
	}
	if got := CodeGraphTools(env("windows", false), false); len(got) != 0 {
		t.Errorf("not installed: %v", got)
	}
	if got := CodeGraphTools(env("linux", true), false); len(got) != 0 {
		t.Errorf("linux must stay untouched: %v", got)
	}
	got := CodeGraphTools(env("windows", false), true)
	if len(got) != 1 || got[0].Name != rt.EntryCodeGraph || got[0].Command != "cmd" {
		t.Errorf("forced: %v", got)
	}
	if got := CodeGraphTools(env("windows", true), false); len(got) != 1 {
		t.Errorf("on PATH: %v", got)
	}
	os.MkdirAll(filepath.Dir(launcher), 0o755)
	os.WriteFile(launcher, []byte("x"), 0o755)
	if got := CodeGraphTools(env("windows", false), false); len(got) != 1 {
		t.Errorf("managed install: %v", got)
	}
}
