package memory

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
)

type fakeProvider struct {
	saved     []Entry
	summaries []Summary
	sumErr    error
	searchFn  func(Query) ([]Entry, error)
	queries   []Query
}

func (f *fakeProvider) Search(_ context.Context, q Query) ([]Entry, error) {
	f.queries = append(f.queries, q)
	if f.searchFn != nil {
		return f.searchFn(q)
	}
	return nil, nil
}
func (f *fakeProvider) Save(_ context.Context, e Entry) (ID, error) {
	f.saved = append(f.saved, e)
	return ID("id"), nil
}
func (f *fakeProvider) Update(context.Context, ID, EntryPatch) error { return ErrUnsupported }
func (f *fakeProvider) SessionSummary(_ context.Context, s Summary) error {
	f.summaries = append(f.summaries, s)
	return f.sumErr
}
func (f *fakeProvider) CurrentProject(context.Context) (string, error) { return "p", nil }
func (f *fakeProvider) Doctor(context.Context) Report                  { return Report{Provider: "fake"} }

func TestGuardTeamRequiresActor(t *testing.T) {
	fp := &fakeProvider{}
	g := Guard{Provider: fp, Mode: gabitotl.ModeTeam}
	e := Entry{Type: TypeDecision, Title: "t", Content: "c"}
	if _, err := g.Save(context.Background(), e); !errors.Is(err, ErrActorRequired) {
		t.Fatalf("want ErrActorRequired, got %v", err)
	}
	e.Actor = ActorLegacy
	if _, err := g.Save(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	if fp.saved[0].Timestamp.IsZero() || fp.saved[0].Source != DefaultSource {
		t.Fatalf("entry not stamped: %+v", fp.saved[0])
	}
	if err := g.SessionSummary(context.Background(), Summary{}); !errors.Is(err, ErrActorRequired) {
		t.Fatalf("summary: want ErrActorRequired, got %v", err)
	}
}

func TestGuardSoloAllowsMissingActor(t *testing.T) {
	g := Guard{Provider: &fakeProvider{}, Mode: gabitotl.ModeSolo}
	if _, err := g.Save(context.Background(), Entry{Type: TypeBugfix, Title: "t", Content: "c"}); err != nil {
		t.Fatal(err)
	}
}

func TestEntryValidate(t *testing.T) {
	for _, e := range []Entry{
		{Type: "nope", Title: "t", Content: "c"},
		{Type: TypeRisk, Content: "c"},
		{Type: TypeRisk, Title: "t"},
	} {
		if e.Validate(gabitotl.ModeSolo) == nil {
			t.Errorf("expected error for %+v", e)
		}
	}
}

func TestFactory(t *testing.T) {
	f := NewFactory()
	f.Register("engram", func(gabitotl.MemoryConfig) (Provider, error) { return &fakeProvider{}, nil })
	p, err := f.New(gabitotl.MemoryConfig{Provider: "engram", Mode: gabitotl.ModeTeam})
	if err != nil {
		t.Fatal(err)
	}
	if g, ok := p.(Guard); !ok || g.Mode != gabitotl.ModeTeam {
		t.Fatalf("expected team guard, got %#v", p)
	}
	if _, err := f.New(gabitotl.MemoryConfig{Provider: "memory-mcp"}); err == nil {
		t.Fatal("expected error for unregistered provider")
	}
}

func TestExtractTerms(t *testing.T) {
	got := ExtractTerms("Sigamos con la recuperación de contraseña, fix the AuthService.refresh_token flow")
	want := []string{"recuperación", "contraseña", "fix", "authservice.refresh_token", "flow"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	if n := len(ExtractTerms("alpha1 beta22 gamma33 delta44 epsilon5 zeta666 eta7777 theta88")); n != maxTerms {
		t.Fatalf("terms not capped: %d", n)
	}
	if len(ExtractTerms("con el de la")) != 0 {
		t.Fatal("stopword-only request must yield no terms")
	}
}

func TestBuildQueriesAlwaysIncludesStanding(t *testing.T) {
	qs := BuildQueries("login bug", "proj", QueryOptions{})
	if len(qs) != 1+len(standing) || qs[0].Text != "login bug" {
		t.Fatalf("unexpected queries: %+v", qs)
	}
	qs = BuildQueries("de la", "proj", QueryOptions{})
	if len(qs) != len(standing) {
		t.Fatalf("standing queries missing: %+v", qs)
	}
	types := map[EntryType]bool{}
	for _, q := range qs {
		types[q.Type] = true
		if q.Project != "proj" || q.Limit != DefaultPerQuery {
			t.Fatalf("bad query %+v", q)
		}
	}
	for _, want := range []EntryType{TypeProjectMap, TypeProgress, TypePending, TypeConvention, TypeBugfix} {
		if !types[want] {
			t.Errorf("missing standing type %s", want)
		}
	}
}

func TestRetrieveCapsAndDedupes(t *testing.T) {
	n := 0
	fp := &fakeProvider{searchFn: func(q Query) ([]Entry, error) {
		var es []Entry
		for i := 0; i < 15; i++ { // provider ignores the limit: Retrieve must still cap
			n++
			es = append(es, Entry{ID: ID(string(rune('a'+n%26)) + "x"), Title: "t", Type: TypeDecision})
		}
		return es, nil
	}}
	got, err := Retrieve(context.Background(), fp, BuildQueries("auth login", "p", QueryOptions{}), QueryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) > DefaultTotal {
		t.Fatalf("total cap exceeded: %d", len(got))
	}
	seen := map[ID]bool{}
	for _, e := range got {
		if seen[e.ID] {
			t.Fatalf("duplicate %s", e.ID)
		}
		seen[e.ID] = true
	}
}

func TestRetrieveAllFailing(t *testing.T) {
	boom := errors.New("boom")
	fp := &fakeProvider{searchFn: func(Query) ([]Entry, error) { return nil, boom }}
	if _, err := Retrieve(context.Background(), fp, BuildQueries("x1y", "p", QueryOptions{}), QueryOptions{}); !errors.Is(err, boom) {
		t.Fatalf("want boom, got %v", err)
	}
}

func TestChangeEntry(t *testing.T) {
	e := ChangeEntry(Change{Actor: "gabi", What: "Fixed refresh\nmore", Why: "token reuse", Files: []string{"a.go"}, Validated: "tests OK", Pending: "logout", Ojo: "x"})
	if e.Type != TypeProgress || e.Title != "Fixed refresh" || e.Actor != "gabi" || e.NextStep != "logout" {
		t.Fatalf("unexpected entry %+v", e)
	}
	if !strings.Contains(e.Content, "**Why**: token reuse") || strings.Contains(e.Content, "Impact") {
		t.Fatalf("content: %q", e.Content)
	}
}

func TestRecorder(t *testing.T) {
	fp := &fakeProvider{}
	off := NewRecorder(fp, gabitotl.WorkflowConfig{MemoryAfterWork: false})
	if _, rec, err := off.RecordChange(context.Background(), Change{What: "x"}); rec || err != nil || len(fp.saved) != 0 {
		t.Fatalf("disabled recorder must not save: rec=%v err=%v", rec, err)
	}
	on := NewRecorder(fp, gabitotl.WorkflowConfig{MemoryAfterWork: true})
	if _, rec, err := on.RecordChange(context.Background(), Change{What: "did x", Actor: "a"}); !rec || err != nil || len(fp.saved) != 1 {
		t.Fatalf("enabled recorder must save: rec=%v err=%v", rec, err)
	}
	fp.sumErr = ErrUnsupported
	if rec, err := on.RecordSession(context.Background(), Summary{Task: "t", Accomplished: "a"}); !rec || err != nil {
		t.Fatalf("fallback failed: %v", err)
	}
	if len(fp.saved) != 2 || fp.saved[1].TopicKey != "session/summary" {
		t.Fatalf("expected fallback progress entry, got %+v", fp.saved)
	}
}
