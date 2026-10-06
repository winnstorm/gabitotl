package installer

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/winnstorm/gabitotl/internal/guard"
)

func codesOf(d DoctorReport, section string) map[string]Line {
	out := map[string]Line{}
	for _, s := range d.Sections {
		if s.Name == section {
			for _, l := range s.Lines {
				out[l.Code] = l
			}
		}
	}
	return out
}

func TestDoctorAuthorshipSection(t *testing.T) {
	f := newFixture(t)
	installed(t, f, Options{User: "gabi", UserName: "Gabi"})

	hooks := filepath.Join(t.TempDir(), "hooks")
	prevGit := f.runner.Handlers["git"]
	f.runner.Handlers["git"] = func(a []string) (string, error) {
		switch {
		case len(a) > 1 && a[0] == "rev-parse" && a[1] == "--git-path":
			return hooks + "\n", nil
		case len(a) > 0 && a[0] == "-c":
			return "main.go\x00BAD.md\x00", nil
		}
		return prevGit(a)
	}
	writeFile(t, filepath.Join(f.root, "BAD.md"), "Generated "+"with "+"Cla"+"ude Code\n")

	before := treeHash(t, f.home, f.root, hooks)
	d := Doctor(context.Background(), f.env)
	if treeHash(t, f.home, f.root, hooks) != before {
		t.Fatal("doctor must stay read-only")
	}
	c := codesOf(d, "authorship")
	if c[DocGuardMode].Detail != "strip" {
		t.Fatalf("mode line missing: %+v", c)
	}
	if c[DocGuardHooksMissing].Code == "" {
		t.Fatalf("hooks are not installed yet: %+v", c)
	}
	if l := c[DocGuardScanFound]; l.Detail != "1" || l.Status != LineWarn {
		t.Fatalf("scan summary: %+v", c)
	}
	if c[DocGuardRuntimeOK].Code == "" || !strings.Contains(c[DocGuardRuntimeOK].Detail, "Claude") {
		t.Fatalf("the managed settings keys were installed with the persona: %+v", c)
	}
	if c[DocGuardRuntimeNone].Code == "" {
		t.Fatalf("runtimes without a documented setting say so: %+v", c)
	}

	if _, err := guard.InstallHooks(hooks, "/x/gabitotl"); err != nil {
		t.Fatal(err)
	}
	c = codesOf(Doctor(context.Background(), f.env), "authorship")
	if c[DocGuardHooksOK].Code == "" || c[DocGuardHooksMissing].Code != "" {
		t.Fatalf("hooks installed: %+v", c)
	}
}
