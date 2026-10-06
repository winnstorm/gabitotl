package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/identity"
)

type result struct {
	code     int
	out, err string
}

func runIn(t *testing.T, stdin string, tty bool, wd string, args ...string) result {
	t.Helper()
	var out, errb bytes.Buffer
	code := Run(args, Env{
		Stdin: strings.NewReader(stdin), Stdout: &out, Stderr: &errb,
		Getwd: func() (string, error) { return wd, nil }, IsTTY: tty,
	})
	return result{code, out.String(), errb.String()}
}

func TestInitEndToEnd(t *testing.T) {
	root := t.TempDir()
	args := []string{"init", "--root", root, "--name", "Shop", "--kind", "existing", "--user-id", "gabi",
		"--user-name", "Gabriela", "--mode", "team", "--provider", "memory-mcp", "--vcs", "github", "--yes"}
	r := runIn(t, "", false, t.TempDir(), args...)
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.err)
	}
	l := gabitotl.NewLayout(root)
	proj, err := gabitotl.LoadProject(l)
	if err != nil || proj.Name != "Shop" || proj.ProjectKind != "existing" {
		t.Fatalf("project = %+v, %v", proj, err)
	}
	mem, err := gabitotl.LoadMemory(l)
	if err != nil || mem.Mode != "team" || mem.Provider != "memory-mcp" {
		t.Fatalf("memory = %+v, %v", mem, err)
	}
	if v, err := gabitotl.LoadVCS(l); err != nil || v.Provider != "github" {
		t.Fatalf("vcs = %+v, %v", v, err)
	}
	if u, err := identity.Active(root); err != nil || u.ID != "gabi" || u.Name != "Gabriela" {
		t.Fatalf("active = %+v, %v", u, err)
	}
	if _, err := identity.Load(root, "gabi"); err != nil {
		t.Fatal(err)
	}
	gi, _ := os.ReadFile(l.Gitignore())
	if string(gi) != "local/\n.bak/\ntmp/\n" {
		t.Errorf("gitignore = %q", gi)
	}
	if !strings.Contains(r.out, "identity: gabi") {
		t.Errorf("output = %q", r.out)
	}

	// Second run: idempotent, nothing created, files untouched.
	before, _ := os.ReadFile(l.Memory())
	r2 := runIn(t, "", false, t.TempDir(), args...)
	if r2.code != 0 || !strings.Contains(r2.out, "created: 0") {
		t.Fatalf("second run: %+v", r2)
	}
	if after, _ := os.ReadFile(l.Memory()); !bytes.Equal(before, after) {
		t.Error("memory.md changed on second init")
	}
}

func TestInitDefaultsUseCwdRootAndNoIdentity(t *testing.T) {
	root := t.TempDir()
	r := runIn(t, "", false, root, "init", "--yes")
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.err)
	}
	if !gabitotl.Exists(root) {
		t.Fatal(".gabitotl not created in cwd")
	}
	if _, err := identity.Active(root); err == nil {
		t.Error("identity must not be set without --user-name")
	}
	if !strings.Contains(r.out, "identity: not set") {
		t.Errorf("output = %q", r.out)
	}
	if mem, _ := gabitotl.LoadMemory(gabitotl.NewLayout(root)); mem.Mode != "solo" || mem.Provider != "engram" {
		t.Errorf("memory = %+v", mem)
	}
}

func TestInitPrompts(t *testing.T) {
	root := t.TempDir()
	r := runIn(t, "team\nGabi Pérez\n", true, t.TempDir(), "init", "--root", root)
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.err)
	}
	if !strings.Contains(r.out, "Memory mode") || !strings.Contains(r.out, "Your name") {
		t.Errorf("missing prompts: %q", r.out)
	}
	if mem, _ := gabitotl.LoadMemory(gabitotl.NewLayout(root)); mem.Mode != "team" {
		t.Errorf("mode = %q", mem.Mode)
	}
	if u, err := identity.Active(root); err != nil || u.ID != "gabi-perez" {
		t.Fatalf("active = %+v, %v", u, err)
	}

	// With an active identity and flags set, a TTY run asks nothing about the user.
	r = runIn(t, "", true, t.TempDir(), "init", "--root", root, "--mode", "team")
	if r.code != 0 || strings.Contains(r.out, "Your name") || !strings.Contains(r.out, "identity: unchanged") {
		t.Fatalf("rerun: %+v", r)
	}

	// --yes never prompts even on a TTY; EOF on stdin falls back to defaults.
	root2 := t.TempDir()
	r = runIn(t, "", true, t.TempDir(), "init", "--root", root2, "--yes")
	if r.code != 0 || strings.Contains(r.out, "Memory mode") {
		t.Fatalf("--yes prompted: %+v", r)
	}
}

func TestInitErrors(t *testing.T) {
	home, _ := os.UserHomeDir()
	tests := []struct {
		name string
		args []string
		code int
		err  string
	}{
		{"bad mode", []string{"--mode", "duo"}, 1, "mode"},
		{"bad kind", []string{"--kind", "maybe"}, 1, "project_kind"},
		{"bad provider", []string{"--provider", "redis"}, 1, "provider"},
		{"user-id without name", []string{"--user-id", "gabi"}, 1, "--user-name"},
		{"unknown flag", []string{"--nope"}, 2, "flag provided but not defined"},
		{"positional", []string{"extra"}, 2, "unexpected argument"},
		{"home refused", []string{"--root", home}, 1, "refusing"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			args := append([]string{"init", "--yes"}, tc.args...)
			if !strings.Contains(strings.Join(tc.args, " "), "--root") {
				args = append(args, "--root", root)
			}
			r := runIn(t, "", false, root, args...)
			if r.code != tc.code || !strings.Contains(r.err, tc.err) {
				t.Fatalf("code=%d err=%q", r.code, r.err)
			}
			if tc.code == 1 && gabitotl.Exists(root) && tc.name != "user-id without name" {
				t.Error("failed init must not leave .gabitotl behind")
			}
		})
	}
}

func TestInitRefusesSecretsInFlags(t *testing.T) {
	root := t.TempDir()
	r := runIn(t, "", false, root, "init", "--yes", "--root", root, "--name", "ghp_abcdefghijklmnopqrstuvwxyz0123456789")
	if r.code == 0 {
		t.Fatal("secret-looking name must be rejected")
	}
	if strings.Contains(r.err, "ghp_abcdef") {
		t.Errorf("error leaks secret: %q", r.err)
	}
}
