package gabitotl

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Branding strings are built at runtime so this repository passes its own guard.
var (
	llm     = "Cla" + "ude"
	trailer = "Co-Authored-By: " + llm + " <noreply@" + "anthropic" + ".com>"
)

func TestWriteFileRejectsBranding(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.md")
	err := WriteFile(p, []byte("# Notes\n\n"+trailer+"\n"))
	var be *BrandingError
	if !errors.As(err, &be) || be.Line != 3 {
		t.Fatalf("want BrandingError at line 3, got %v", err)
	}
	if strings.Contains(err.Error(), llm) {
		t.Fatalf("the error must not echo the offending text: %v", err)
	}
	if _, serr := os.Stat(p); serr == nil {
		t.Fatal("nothing must be written")
	}
	if created, err := WriteFileIfMissing(p, []byte(trailer)); err == nil || created {
		t.Fatalf("WriteFileIfMissing must refuse too: %v %v", created, err)
	}
	if err := WriteFile(p, []byte("Supports "+llm+" Code as a runtime.\n")); err != nil {
		t.Fatalf("a plain technical mention must be written: %v", err)
	}
}

func TestWorkflowBrandingConfig(t *testing.T) {
	w := DefaultWorkflow()
	if w.BrandingGuard != GuardStrip || w.GuardHooks != HooksAsk || len(w.BrandingAllowPaths) != 0 {
		t.Fatalf("defaults = %+v", w)
	}
	w.BrandingGuard = "loud"
	if w.Validate() == nil {
		t.Fatal("unknown branding_guard must be invalid")
	}
	w = DefaultWorkflow()
	w.GuardHooks = "maybe"
	if w.Validate() == nil {
		t.Fatal("unknown guard_hooks must be invalid")
	}

	// Files written before the keys existed keep working with the defaults.
	l := NewLayout(t.TempDir())
	old := "---\noutput: compact\nlanguage: es-AR\npersona: gabito\nbackup_before_write: true\nmemory_before_work: true\nmemory_after_work: true\nbackup:\n  max_snapshots: 20\n  max_age_days: 7\n---\n\n# Workflow\n"
	if err := WriteFile(l.Workflow(), []byte(old)); err != nil {
		t.Fatal(err)
	}
	got, err := LoadWorkflow(l)
	if err != nil || !reflect.DeepEqual(got, DefaultWorkflow()) {
		t.Fatalf("old workflow.md = %+v, %v", got, err)
	}

	got.BrandingGuard = GuardBlock
	got.BrandingExtraTerms = []string{"AcmeLLM"}
	got.BrandingAllowPaths = []string{"docs/runtimes/**"}
	if err := SaveWorkflow(l, got); err != nil {
		t.Fatal(err)
	}
	back, err := LoadWorkflow(l)
	if err != nil || !reflect.DeepEqual(back, got) {
		t.Fatalf("round trip = %+v, %v", back, err)
	}
}
