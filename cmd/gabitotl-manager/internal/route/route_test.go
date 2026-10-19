package route

import (
	"path/filepath"
	"testing"
)

func TestResolve(t *testing.T) {
	cases := []struct {
		name      string
		args      []string
		installed bool
		want      string
	}{
		{"fresh machine", nil, false, Wizard},
		{"installed", nil, true, Manage},
		{"manage flag on a fresh machine", []string{"--manage"}, false, Manage},
		{"uninstall flag", []string{"--uninstall"}, true, Uninstall},
		{"uninstall beats manage", []string{"--manage", "--uninstall"}, true, Uninstall},
		{"unknown flags are ignored", []string{"--foo"}, false, Wizard},
	}
	for _, c := range cases {
		if got := Resolve(c.args, c.installed); got != c.want {
			t.Errorf("%s: Resolve(%v, %v) = %q, want %q", c.name, c.args, c.installed, got, c.want)
		}
	}
}

func TestRelaunchTarget(t *testing.T) {
	if got := RelaunchTarget("", "m.exe"); got != "" {
		t.Errorf("empty dir gave %q", got)
	}
	if got, want := RelaunchTarget("dir", "m.exe"), filepath.Join("dir", "m.exe"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRootIsRemembered(t *testing.T) {
	f := filepath.Join(t.TempDir(), "sub", "manager.json")
	if got := LoadRoot(f); got != "" {
		t.Fatalf("no file yet, got %q", got)
	}
	if err := SaveRoot(f, "/work/app"); err != nil {
		t.Fatal(err)
	}
	if got := LoadRoot(f); got != "/work/app" {
		t.Fatalf("got %q", got)
	}
}
