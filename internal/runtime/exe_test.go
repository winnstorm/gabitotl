package runtime

import "testing"

func TestEnvCommandAndResolveExe(t *testing.T) {
	if got := (Env{}).Command("a b"); got != "gabitotl a b" {
		t.Fatal(got)
	}
	if got := (Env{Exe: "/usr/bin/gabitotl"}).Command("x"); got != "/usr/bin/gabitotl x" {
		t.Fatal(got)
	}
	if got := (Env{Exe: "/a b/gabitotl"}).Command("x"); got != `"/a b/gabitotl" x` {
		t.Fatal(got)
	}
	// A test binary lives in a temp or build directory: never recorded in hooks.
	if got := ResolveExe(); got != "" {
		t.Fatalf("temp/build binary must fall back to plain gabitotl, got %q", got)
	}
}
