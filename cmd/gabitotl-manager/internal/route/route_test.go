package route

import "testing"

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
