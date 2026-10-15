package installer

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
)

func configureDelivery(t *testing.T, f *fixture, provider string) {
	t.Helper()
	a := gabitotl.DeliveryAnswers{Provider: provider, Automation: gabitotl.AutomationAsk}
	if _, err := gabitotl.SaveDelivery(gabitotl.NewLayout(f.root), gabitotl.ActiveUserID(), a); err != nil {
		t.Fatal(err)
	}
}

func authLinesOf(t *testing.T, f *fixture) []Line {
	t.Helper()
	d := Doctor(context.Background(), f.env)
	if d.Errors() != 0 {
		t.Fatalf("a login finding is never a failure: %+v", d)
	}
	var out []Line
	for _, s := range d.Sections {
		if s.Name != "git" {
			continue
		}
		for _, l := range s.Lines {
			if strings.HasPrefix(l.Code, "doctor.git.auth") {
				out = append(out, l)
			}
		}
	}
	return out
}

func TestDoctorWarnsAboutTheLoginWithTheExactFix(t *testing.T) {
	f := newFixture(t) // origin is on GitHub
	installed(t, f, Options{User: "gabi", UserName: "Gabi"})
	if got := authLinesOf(t, f); len(got) != 0 {
		t.Fatalf("no login check until delivery is configured: %+v", got)
	}
	configureDelivery(t, f, "github")

	f.runner.Handlers["gh auth"] = func([]string) (string, error) { return "Logged in", nil }
	if got := authLinesOf(t, f); len(got) != 1 || got[0].Status != LineOK || got[0].Code != DocGitAuth {
		t.Fatalf("logged in: %+v", got)
	}
	f.runner.Handlers["gh auth"] = func([]string) (string, error) { return "", errors.New("exit 1") }
	if got := authLinesOf(t, f); len(got) != 1 || got[0].Status != LineWarn || got[0].Code != DocGitAuthTodo || got[0].Detail != "gh auth login" {
		t.Fatalf("logged out is a warning with the command: %+v", got)
	}
	f.runner.Handlers["gh auth"] = func([]string) (string, error) { return "", exec.ErrNotFound }
	if got := authLinesOf(t, f); len(got) != 1 || got[0].Code != DocGitAuthTool || !strings.HasPrefix(got[0].Detail, "https://cli.github.com") {
		t.Fatalf("gh missing: %+v", got)
	}
}

func TestDoctorChecksBitbucketOfflineByLookingForTheToken(t *testing.T) {
	f := newFixture(t)
	installed(t, f, Options{User: "gabi", UserName: "Gabi"})
	configureDelivery(t, f, "bitbucket")
	before := len(f.runner.Log())
	got := authLinesOf(t, f)
	if len(got) != 1 || got[0].Code != DocGitAuthTodo || got[0].Detail != "gabitotl vcs login" {
		t.Fatalf("no token: %+v", got)
	}
	f.vars["BITBUCKET_TOKEN"] = "tok"
	if got := authLinesOf(t, f); len(got) != 1 || got[0].Status != LineOK {
		t.Fatalf("token present: %+v", got)
	}
	for _, c := range f.runner.Log()[before:] {
		if strings.HasPrefix(c, "gh ") || strings.HasPrefix(c, "glab ") {
			t.Fatalf("Bitbucket must not call a CLI: %q", c)
		}
	}
}
