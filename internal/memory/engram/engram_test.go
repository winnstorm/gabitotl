package engram

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/winnstorm/gabitotl/internal/memory"
)

type call struct{ args []string }

type fakeRunner struct {
	calls []call
	out   string
	err   error
}

func (f *fakeRunner) Run(_ context.Context, args ...string) ([]byte, []byte, error) {
	f.calls = append(f.calls, call{args})
	return []byte(f.out), nil, f.err
}

func TestSaveBuildsArgsAndParsesID(t *testing.T) {
	fr := &fakeRunner{out: "Memory saved: #42 \"T\" (decision)\n"}
	p := New(Options{Runner: fr, Project: "proj"})
	ts := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	id, err := p.Save(context.Background(), memory.Entry{
		Type: memory.TypeDecision, Title: "T", Content: "body", TopicKey: "a/b",
		Actor: "gabi", Branch: "feat/x", Files: []string{"a.go", "b.go"}, Timestamp: ts, Source: "gabitotl",
	})
	if err != nil || id != "42" {
		t.Fatalf("id=%q err=%v", id, err)
	}
	a := fr.calls[0].args
	if a[0] != "save" || a[1] != "T" || !slices.Contains(a, "--project") || !slices.Contains(a, "--topic") {
		t.Fatalf("bad args %q", a)
	}
	if !strings.Contains(a[2], "**Actor**: gabi") || !strings.Contains(a[2], "**Files**: a.go, b.go") || !strings.Contains(a[2], "2026-01-02T03:04:05Z") {
		t.Fatalf("trailer missing: %q", a[2])
	}
}

func TestSaveUnexpectedOutput(t *testing.T) {
	p := New(Options{Runner: &fakeRunner{out: "nope"}})
	if _, err := p.Save(context.Background(), memory.Entry{Type: memory.TypeRisk, Title: "t", Content: "c"}); err == nil {
		t.Fatal("expected error")
	}
}

const searchOut = `Found 2 memories:

[1] #630 (decision) — listing: breadcrumb (with parens) (2026-10-05)
    **What**: first line
second line
**Actor**: gabi
**Files**: a.go, b.go
    2026-10-05 16:52:16 | project: remategarage | scope: project

[2] #1 (decision) — T title
    changed
    2026-10-06 00:20:01 | project: demo | scope: project
`

func TestSearchParsing(t *testing.T) {
	fr := &fakeRunner{out: searchOut}
	p := New(Options{Runner: fr, Project: "demo"})
	got, err := p.Search(context.Background(), memory.Query{Text: "-breadcrumb", Type: memory.TypeDecision, Limit: 5, MatchAny: true})
	if err != nil || len(got) != 2 {
		t.Fatalf("got=%d err=%v", len(got), err)
	}
	a := fr.calls[0].args
	if a[1] != "breadcrumb" || !slices.Contains(a, "any") || !slices.Contains(a, "demo") {
		t.Fatalf("bad args %q", a)
	}
	e := got[0]
	if e.ID != "630" || e.Type != memory.TypeDecision || e.Title != "listing: breadcrumb (with parens)" || e.Project != "remategarage" {
		t.Fatalf("entry %+v", e)
	}
	if e.Actor != "gabi" || !slices.Equal(e.Files, []string{"a.go", "b.go"}) || e.Content != "**What**: first line\nsecond line" {
		t.Fatalf("trailer/body parse: %+v", e)
	}
	if got[1].Title != "T title" || got[1].Content != "changed" {
		t.Fatalf("second: %+v", got[1])
	}
}

func TestSearchNoResultsAndErrors(t *testing.T) {
	p := New(Options{Runner: &fakeRunner{out: "No memories found for: \"x\"\n"}})
	if got, err := p.Search(context.Background(), memory.Query{Text: "x"}); err != nil || len(got) != 0 {
		t.Fatalf("got=%v err=%v", got, err)
	}
	if _, err := p.Search(context.Background(), memory.Query{Text: "  "}); err == nil {
		t.Fatal("empty text must error")
	}
	boom := errors.New("boom")
	if _, err := New(Options{Runner: &fakeRunner{err: boom}}).Search(context.Background(), memory.Query{Text: "x"}); !errors.Is(err, boom) {
		t.Fatalf("want boom, got %v", err)
	}
}

func TestUnsupported(t *testing.T) {
	p := New(Options{Runner: &fakeRunner{}})
	if err := p.Update(context.Background(), "1", memory.EntryPatch{}); !errors.Is(err, memory.ErrUnsupported) {
		t.Fatal("Update must be ErrUnsupported")
	}
	if err := p.SessionSummary(context.Background(), memory.Summary{}); !errors.Is(err, memory.ErrUnsupported) {
		t.Fatal("SessionSummary must be ErrUnsupported")
	}
	if pr, err := p.CurrentProject(context.Background()); !errors.Is(err, memory.ErrUnsupported) || pr != "" {
		t.Fatal("CurrentProject without key must be ErrUnsupported")
	}
	if pr, _ := New(Options{Runner: &fakeRunner{}, Project: "k"}).CurrentProject(context.Background()); pr != "k" {
		t.Fatal("CurrentProject must return configured key")
	}
}

func TestDoctor(t *testing.T) {
	out := `{"status":"error","project":"demo","checks":[
		{"check_id":"a","result":"ok","message":"fine"},
		{"check_id":"b","result":"warning","message":"hmm"},
		{"check_id":"c","result":"blocked","message":"bad"}]}`
	fr := &fakeRunner{out: out, err: errors.New("exit status 1")}
	rep := New(Options{Runner: fr, Project: "demo"}).Doctor(context.Background())
	if !slices.Equal(fr.calls[0].args, []string{"doctor", "--json", "--project", "demo"}) {
		t.Fatalf("args %q", fr.calls[0].args)
	}
	if len(rep.Checks) != 4 || rep.Checks[2].Status != memory.StatusWarn || rep.Checks[3].Status != memory.StatusError || rep.OK() {
		t.Fatalf("report %+v", rep)
	}
	bad := New(Options{Runner: &fakeRunner{out: "garbage", err: errors.New("x")}}).Doctor(context.Background())
	if bad.OK() {
		t.Fatal("unreadable doctor output must fail")
	}
}

func TestProjectsAndExport(t *testing.T) {
	fr := &fakeRunner{out: "Projects (2):\n  gztech                          269 obs     2 sessions    39 prompts\n  demo                              1 obs     1 session      0 prompts\n"}
	p := New(Options{Runner: fr})
	ps, err := p.Projects(context.Background())
	if err != nil || len(ps) != 2 || ps[0].Name != "gztech" || ps[0].Observations != 269 || ps[1].Sessions != 1 {
		t.Fatalf("projects %+v err=%v", ps, err)
	}
	fr.out = "Exported to /x/e.json\n  Sessions:     2\n  Observations: 7\n  Prompts:      0\n"
	res, err := p.Export(context.Background(), "/x/e.json", "kraken")
	if err != nil || res.Sessions != 2 || res.Observations != 7 {
		t.Fatalf("export %+v err=%v", res, err)
	}
	if !slices.Equal(fr.calls[1].args, []string{"export", "/x/e.json", "--project", "kraken"}) {
		t.Fatalf("args %q", fr.calls[1].args)
	}
	if _, err := p.Export(context.Background(), "--help", ""); err == nil {
		t.Fatal("flag-like path must be rejected")
	}
}
