package budget

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/memory"
	"github.com/winnstorm/gabitotl/internal/persona"
	"github.com/winnstorm/gabitotl/internal/tokens"
)

func TestConfigValidate(t *testing.T) {
	for _, c := range []Config{{0, 0.75}, {100, 0}, {100, 1.5}} {
		if c.Validate() == nil {
			t.Errorf("%+v should be invalid", c)
		}
	}
	if err := DefaultConfig().Validate(); err != nil {
		t.Fatal(err)
	}
	if _, err := New(gabitotl.NewLayout(t.TempDir()), "ana", Config{}); err == nil {
		t.Fatal("New must validate the config")
	}
}

func newMon(t *testing.T, limit int) (*Monitor, gabitotl.Layout) {
	t.Helper()
	l := gabitotl.NewLayout(t.TempDir())
	m, err := New(l, "ana", Config{Limit: limit, Threshold: 0.75})
	if err != nil {
		t.Fatal(err)
	}
	m.Now = func() time.Time { return time.Date(2026, 10, 5, 20, 0, 0, 0, time.UTC) }
	return m, l
}

func TestCheckFiresOncePerSession(t *testing.T) {
	m, l := newMon(t, 1000) // 750 tokens = 3000 chars
	if err := m.AddText(strings.Repeat("x", 1800)); err != nil {
		t.Fatal(err)
	}
	d, err := m.Check()
	if err != nil || d.ShouldSave || d.Source != SourceEstimated {
		t.Fatalf("below threshold: %+v %v", d, err)
	}
	_ = m.AddText(strings.Repeat("x", 1080))
	d, _ = m.Check()
	if !d.ShouldSave || !strings.HasPrefix(d.Message, "**TIP:** Este chat ya viene bastante largo") || d.Percent < 75 {
		t.Fatalf("at threshold: %+v", d)
	}
	if d, _ = m.Check(); d.ShouldSave {
		t.Fatal("must fire only once per session")
	}
	// State survives a restart (new Monitor, same session.md).
	m2, err := New(l, "ana", Config{Limit: 1000, Threshold: 0.75})
	if err != nil {
		t.Fatal(err)
	}
	if d, _ = m2.Check(); d.ShouldSave {
		t.Fatal("notified flag must persist in session.md")
	}
	if u, _ := m2.Used(); u != 800 {
		t.Fatalf("used = %d", u)
	}
	// A new session re-arms it.
	if err := m2.NewSession(); err != nil {
		t.Fatal(err)
	}
	if u, _ := m2.Used(); u != 0 {
		t.Fatalf("used after reset = %d", u)
	}
	_ = m2.ReportTokens(900)
	if d, _ = m2.Check(); !d.ShouldSave {
		t.Fatalf("new session should fire again: %+v", d)
	}
}

func TestSessionIsPerPerson(t *testing.T) {
	l := gabitotl.NewLayout(t.TempDir())
	cfg := Config{Limit: 1000, Threshold: 0.75}
	ana, err := New(l, "ana", cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := ana.ReportTokens(400); err != nil {
		t.Fatal(err)
	}
	bob, err := New(l, "bob", cfg)
	if err != nil {
		t.Fatal(err)
	}
	if u, _ := bob.Used(); u != 0 {
		t.Fatalf("bob sees ana's session: %d", u)
	}
	if p, _ := l.LocalSession("ana"); !strings.HasSuffix(p, "local/users/ana/session.md") {
		t.Fatalf("path = %s", p)
	}
	if _, err := New(l, "../x", cfg); err == nil {
		t.Fatal("an invalid user must be rejected")
	}
}

func TestReportedTokensWinOverEstimates(t *testing.T) {
	m, _ := newMon(t, 1000)
	_ = m.AddText(strings.Repeat("x", 3600)) // 1000 estimated
	if err := m.ReportTokens(100); err != nil {
		t.Fatal(err)
	}
	_ = m.AddText(strings.Repeat("x", 3600)) // ignored now
	d, _ := m.Check()
	if d.ShouldSave || d.Used != 100 || d.Source != SourceReported {
		t.Fatalf("decision = %+v", d)
	}
	if err := m.ReportTokens(-1); err == nil {
		t.Fatal("negative count must be rejected")
	}
}

type fakeProvider struct {
	saved    []memory.Entry
	sessions []memory.Summary
	failSave error
	noNative bool
}

func (f *fakeProvider) Search(context.Context, memory.Query) ([]memory.Entry, error) { return nil, nil }
func (f *fakeProvider) Save(_ context.Context, e memory.Entry) (memory.ID, error) {
	if f.failSave != nil {
		return "", f.failSave
	}
	f.saved = append(f.saved, e)
	return "1", nil
}
func (f *fakeProvider) Update(context.Context, memory.ID, memory.EntryPatch) error { return nil }
func (f *fakeProvider) SessionSummary(_ context.Context, s memory.Summary) error {
	if f.noNative {
		return memory.ErrUnsupported
	}
	f.sessions = append(f.sessions, s)
	return nil
}
func (f *fakeProvider) CurrentProject(context.Context) (string, error) { return "p", nil }
func (f *fakeProvider) Doctor(context.Context) memory.Report           { return memory.Report{} }

func TestCheckpointSavesAndReturnsTip(t *testing.T) {
	m, _ := newMon(t, 1000)
	_ = m.ReportTokens(800)
	m.Check()
	p := &fakeProvider{}
	rec := memory.Recorder{Provider: p, Enabled: true}
	res, err := m.Checkpoint(context.Background(), rec, State{
		Project: "proj", Actor: "gabi", Task: "auth-login", Branch: "feature/auth-login", Goal: "login",
		Progress: "Login listo", NextStep: "Implementar logout", Decisions: []string{"JWT corto", "refresh opaco"},
	})
	if err != nil || !res.Recorded || len(res.Warnings) != 0 {
		t.Fatalf("res = %+v err %v", res, err)
	}
	if !strings.Contains(res.Message, "Dejé el estado y lo pendiente guardados en memoria") {
		t.Fatalf("message = %q", res.Message)
	}
	if len(p.saved) != 1 || p.saved[0].NextStep != "Implementar logout" || !strings.Contains(p.saved[0].Content, "JWT corto; refresh opaco") || p.saved[0].Actor != "gabi" {
		t.Fatalf("saved = %+v", p.saved)
	}
	if len(p.sessions) != 1 || p.sessions[0].NextSteps != "Implementar logout" {
		t.Fatalf("sessions = %+v", p.sessions)
	}
}

func TestCheckpointFallbackWarningsAndDisabled(t *testing.T) {
	m, _ := newMon(t, 1000)
	p := &fakeProvider{noNative: true}
	res, err := m.Checkpoint(context.Background(), memory.Recorder{Provider: p, Enabled: true}, State{Progress: "x"})
	if err != nil || !res.Recorded || len(res.Warnings) != 1 {
		t.Fatalf("res = %+v err %v", res, err)
	}
	if len(p.saved) != 2 { // change + session fallback entry
		t.Fatalf("saved = %d", len(p.saved))
	}
	// Disabled memory: nothing recorded, no false claim to the user.
	p2 := &fakeProvider{}
	res, err = m.Checkpoint(context.Background(), memory.Recorder{Provider: p2, Enabled: false}, State{NextStep: "n"})
	if err != nil || res.Recorded || res.Message != "" || len(p2.saved) != 0 {
		t.Fatalf("disabled: %+v %v", res, err)
	}
}

func TestCheckpointFailureRearms(t *testing.T) {
	m, _ := newMon(t, 1000)
	_ = m.ReportTokens(900)
	if d, _ := m.Check(); !d.ShouldSave {
		t.Fatal("expected trigger")
	}
	p := &fakeProvider{failSave: errors.New("down")}
	res, err := m.Checkpoint(context.Background(), memory.Recorder{Provider: p, Enabled: true}, State{NextStep: "n"})
	if err == nil || res.Message != "" {
		t.Fatalf("res = %+v err %v", res, err)
	}
	if d, _ := m.Check(); !d.ShouldSave {
		t.Fatal("a failed checkpoint must be retried on the next check")
	}
}

func TestPersonaFitsTokenBudget(t *testing.T) {
	if n := tokens.EstimateLegacy(persona.DefaultText()); n > 1500 {
		t.Fatalf("persona is ~%d tokens, budget is 1500", n)
	}
}
