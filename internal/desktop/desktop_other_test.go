//go:build !windows

package desktop

import (
	"errors"
	"testing"
)

func TestNewIsUnsupportedOffWindows(t *testing.T) {
	in := New()
	if err := in.Apply(Options{InstallDir: "x"}, nil); err == nil {
		t.Fatal("an unresolved environment must be refused")
	}
	in.Env = newInstaller(newFake()).Env
	if err := in.Apply(Options{InstallDir: abs("x")}, nil); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("got %v, want ErrUnsupported", err)
	}
	if s := in.Status(Options{InstallDir: abs("x")}); s.Installed() {
		t.Fatal("must report not installed")
	}
}
