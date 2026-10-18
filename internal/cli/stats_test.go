package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func statsFixture(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "proj")
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GABITOTL_USER", "ana")
	t.Setenv("PATH", t.TempDir())
	return root
}

func TestStatsEmptyState(t *testing.T) {
	root := statsFixture(t)
	r := runIn(t, "", false, root, "stats", "--root", root)
	if r.code != 0 {
		t.Fatalf("stats must not fail on an empty project: %+v", r)
	}
	for _, want := range []string{"stats for ana", "context: 0/200k", "savings: none recorded", "always-on: ~", "code index: none installed"} {
		if !strings.Contains(r.out, want) {
			t.Fatalf("missing %q in:\n%s", want, r.out)
		}
	}
}

func TestStatsJSONAndSavings(t *testing.T) {
	root := statsFixture(t)
	env := Env{}
	recordSavings(env, root, "run", 7200, 360)
	recordSavings(env, root, "memory", 1800, 180)
	r := runIn(t, "", false, root, "stats", "--root", root, "--json", "--since", "7d")
	var got struct {
		User    string `json:"user"`
		Savings map[string]struct {
			Runs        int `json:"runs"`
			SavedTokens int `json:"saved_tokens_estimated"`
		} `json:"savings"`
		AlwaysOn struct {
			Total int `json:"total_tokens"`
		} `json:"always_on"`
	}
	if err := json.Unmarshal([]byte(r.out), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, r.out)
	}
	if got.User != "ana" || got.Savings["run"].Runs != 1 || got.Savings["run"].SavedTokens == 0 || got.Savings["memory"].Runs != 1 || got.AlwaysOn.Total == 0 {
		t.Fatalf("unexpected report: %+v", got)
	}
}
