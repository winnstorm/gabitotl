package memory

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/winnstorm/gabitotl/internal/branding"
	"github.com/winnstorm/gabitotl/internal/gabitotl"
)

// Branding strings are built at runtime so this repository passes its own guard.
var (
	llm     = "Cla" + "ude"
	trailer = "Co-Authored-By: " + llm + " <noreply@" + "anthropic" + ".com>"
	footer  = "Generated " + "with " + llm + " Code"
)

func TestGuardStripsAttributionBeforeSaving(t *testing.T) {
	fp := &fakeProvider{}
	var removed []branding.Finding
	g := Guard{Provider: fp, Mode: gabitotl.ModeSolo, OnBranding: func(r, _ []branding.Finding) { removed = r }}
	e := Entry{Type: TypeDecision, Title: "Use JWT", Content: "We chose JWT.\n\n" + footer + "\n" + trailer, Ojo: trailer}
	if _, err := g.Save(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	got := fp.saved[0]
	if got.Content != "We chose JWT." || got.Ojo != "" || strings.Contains(got.Content+got.Title, llm) {
		t.Fatalf("saved = %+v", got)
	}
	if len(removed) != 3 {
		t.Fatalf("what was removed must be reported: %+v", removed)
	}
	// A plain technical mention is memory worth keeping.
	if _, err := g.Save(context.Background(), Entry{Type: TypeConfig, Title: "Runtime", Content: "The project uses " + llm + " Code."}); err != nil {
		t.Fatal(err)
	}
	if fp.saved[1].Content != "The project uses "+llm+" Code." {
		t.Fatalf("technical mention changed: %q", fp.saved[1].Content)
	}
}

func TestGuardRefusesWhatItCannotStrip(t *testing.T) {
	fp := &fakeProvider{}
	g := Guard{Provider: fp, Mode: gabitotl.ModeSolo}
	_, err := g.Save(context.Background(), Entry{Type: TypeBugfix, Title: "t", Content: "Fixed, " + strings.ToLower(footer)})
	if !errors.Is(err, ErrBranding) || len(fp.saved) != 0 {
		t.Fatalf("want ErrBranding and nothing saved, got %v / %+v", err, fp.saved)
	}
}

func TestGuardSummaryAndUpdate(t *testing.T) {
	fp := &fakeProvider{}
	g := Guard{Provider: fp, Mode: gabitotl.ModeSolo}
	if err := g.SessionSummary(context.Background(), Summary{Task: "t", Accomplished: "Done.\n" + footer}); err != nil {
		t.Fatal(err)
	}
	if fp.summaries[0].Accomplished != "Done." {
		t.Fatalf("summary = %+v", fp.summaries[0])
	}
	c := "Body\n" + trailer
	patch, rem, _, err := Policy{}.SanitizePatch(EntryPatch{Content: &c})
	if err != nil || *patch.Content != "Body" || len(rem) != 1 {
		t.Fatalf("patch = %+v %v", patch, err)
	}
	if err := g.Update(context.Background(), "id", EntryPatch{Content: &c}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("fake provider says unsupported, got %v", err)
	}
}

func TestRecorderObeysWorkflowPolicy(t *testing.T) {
	fp := &fakeProvider{}
	wf := gabitotl.DefaultWorkflow()
	rec := NewRecorder(fp, wf)
	var seen int
	rec.OnBranding = func(r, _ []branding.Finding) { seen = len(r) }
	if _, ok, err := rec.RecordChange(context.Background(), Change{What: "did x", Actor: "a", Why: "ok\n" + trailer}); !ok || err != nil {
		t.Fatal(err)
	}
	if strings.Contains(fp.saved[0].Content, llm) || seen != 1 {
		t.Fatalf("recorder must strip: %+v seen=%d", fp.saved[0], seen)
	}

	wf.BrandingGuard = gabitotl.GuardBlock
	if _, ok, err := NewRecorder(fp, wf).RecordChange(context.Background(), Change{What: "did y", Why: trailer}); ok || !errors.Is(err, ErrBranding) {
		t.Fatalf("block mode must refuse: %v", err)
	}
	wf.BrandingGuard = gabitotl.GuardWarn
	var warned int
	w := NewRecorder(fp, wf)
	w.OnBranding = func(_, wr []branding.Finding) { warned = len(wr) }
	if _, ok, err := w.RecordChange(context.Background(), Change{What: "did z", Why: trailer}); !ok || err != nil || warned != 1 {
		t.Fatalf("warn mode saves and reports: %v %d", err, warned)
	}
	wf.BrandingGuard = gabitotl.GuardOff
	if _, ok, err := NewRecorder(fp, wf).RecordChange(context.Background(), Change{What: "did w", Why: trailer}); !ok || err != nil {
		t.Fatalf("off mode saves untouched: %v", err)
	}
	if _, err := NewRecorder(fp, gabitotl.WorkflowConfig{MemoryAfterWork: true, BrandingExtraTerms: []string{"acmellm"}}).RecordSession(context.Background(), Summary{Accomplished: "Done\nCreated with AcmeLLM"}); err != nil {
		t.Fatal(err)
	}
	if fp.summaries[len(fp.summaries)-1].Accomplished != "Done" {
		t.Fatalf("extra terms apply: %+v", fp.summaries)
	}
}
