package installer

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/winnstorm/gabitotl/internal/engraminstall"
	"github.com/winnstorm/gabitotl/internal/gabitotl"
)

type noDL struct{}

func (noDL) Download(context.Context, string, io.Writer, int64) error { return errors.New("offline") }

func engramSetup(t *testing.T, found bool) *engraminstall.Installer {
	return &engraminstall.Installer{
		Ports: engraminstall.Ports{
			Download: noDL{},
			Look: func(string) (string, error) {
				if found {
					return "/usr/bin/engram", nil
				}
				return "", errors.New("missing")
			},
			Run:    func(context.Context, string, ...string) (string, error) { return "engram 1.0.0", nil },
			Getenv: func(string) string { return "" },
		},
		GOOS: "linux", GOARCH: "amd64", Dir: t.TempDir(),
	}
}

func TestEngramNeededOnlyForEngramProjectsWhenMissing(t *testing.T) {
	ctx := context.Background()
	engram := Plan{MemoryChoice: gabitotl.MemoryEngram}
	team := Plan{MemoryChoice: gabitotl.MemoryMemoryMCP}
	if _, ok := EngramNeeded(ctx, Env{}, engram); ok {
		t.Fatal("no installer wired: nothing to do")
	}
	if _, ok := EngramNeeded(ctx, Env{EngramSetup: engramSetup(t, false)}, engram); !ok {
		t.Fatal("engram project without the binary must install it")
	}
	if _, ok := EngramNeeded(ctx, Env{EngramSetup: engramSetup(t, false)}, team); ok {
		t.Fatal("team memory does not use Engram")
	}
	if _, ok := EngramNeeded(ctx, Env{EngramSetup: engramSetup(t, true)}, engram); ok {
		t.Fatal("an Engram on PATH is never replaced")
	}
}

func TestEnsureEngramFailureKeepsInventoryAndPlan(t *testing.T) {
	env := Env{EngramSetup: engramSetup(t, false)}
	inv, pl := Inventory{Version: 7}, Plan{MemoryChoice: gabitotl.MemoryEngram}
	inv2, pl2, out, err := EnsureEngram(context.Background(), env, inv, pl, nil)
	if err == nil || out.Installed || inv2.Version != 7 || pl2.MemoryChoice != gabitotl.MemoryEngram {
		t.Fatalf("err=%v out=%+v", err, out)
	}
}
