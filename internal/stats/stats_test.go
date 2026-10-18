package stats

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/winnstorm/gabitotl/internal/budget"
	"github.com/winnstorm/gabitotl/internal/gabitotl"
)

var now = time.Unix(1_800_000_000, 0)

func writeLog(t *testing.T, l gabitotl.Layout, user, body string) {
	t.Helper()
	dir, err := l.LocalUserDir(user)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, budget.SavingsFile), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestEmptyStateIsZeros(t *testing.T) {
	l := gabitotl.NewLayout(t.TempDir())
	r := Collect(l, "ana", 0, now)
	if len(r.Savings) != 0 || r.Context.Used != 0 || r.Context.Checkpoints != 0 || r.Context.Limit != budget.DefaultLimit {
		t.Fatalf("unexpected empty report: %+v", r)
	}
	if r.AlwaysOn.Persona <= 0 || r.AlwaysOn.Skills <= 0 || r.AlwaysOn.Total != r.AlwaysOn.Persona+r.AlwaysOn.Skills {
		t.Fatalf("always-on cost not computed: %+v", r.AlwaysOn)
	}
	if r.Index == "" {
		t.Fatal("code index line is empty")
	}
}

func TestSavingsAggregateAndSince(t *testing.T) {
	l := gabitotl.NewLayout(t.TempDir())
	old := now.Add(-10 * 24 * time.Hour).Unix()
	recent := now.Add(-time.Hour).Unix()
	writeLog(t, l, "ana", fmt.Sprintf("%d run 36000 360\n%d run 3600 360\n%d memory 1800 180\nbroken line\n%d run x 1\n", old, recent, recent, recent))

	all := Collect(l, "ana", 0, now)
	if s := all.Savings["run"]; s.Runs != 2 || s.FullBytes != 39600 || s.PrintedByte != 720 {
		t.Fatalf("run totals: %+v", s)
	}
	if all.Savings["run"].SavedTokens != (38880*5+17)/18 {
		t.Fatalf("saved tokens: %+v", all.Savings["run"])
	}
	week := Collect(l, "ana", ParseSince("7d"), now)
	if s := week.Savings["run"]; s.Runs != 1 || s.FullBytes != 3600 {
		t.Fatalf("since filter: %+v", s)
	}
	if week.Savings["memory"].Runs != 1 || week.Since == "" {
		t.Fatalf("memory/since: %+v", week)
	}
	if got := week.Kinds(); len(got) != 2 || got[0] != "memory" || got[1] != "run" {
		t.Fatalf("kinds: %v", got)
	}
}

func TestSessionStateAndCheckpoints(t *testing.T) {
	l := gabitotl.NewLayout(t.TempDir())
	m, err := budget.New(l, "ana", budget.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if err := m.ReportTokens(50_000); err != nil {
		t.Fatal(err)
	}
	r := Collect(l, "ana", 0, now)
	if r.Context.Used != 50_000 || r.Context.Source != budget.SourceReported || r.Context.Percent != 25 {
		t.Fatalf("context: %+v", r.Context)
	}
}

func TestParseSince(t *testing.T) {
	cases := map[string]time.Duration{
		"": 0, "7d": 7 * 24 * time.Hour, "2w": 14 * 24 * time.Hour, "12h": 12 * time.Hour,
		"junk": 0, "-3d": 0, "0d": 0, "d": 0,
	}
	for in, want := range cases {
		if got := ParseSince(in); got != want {
			t.Errorf("ParseSince(%q) = %v, want %v", in, got, want)
		}
	}
}
