package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeKiroSession writes a synthetic Kiro session under home.
func fakeKiroSession(t *testing.T, home, id, workspace, modified string, lines ...string) {
	t.Helper()
	dir := filepath.Join(home, "sessions", "0123456789abcdef", "sess_"+id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	meta := `{"id":"` + id + `","workspacePaths":["` + workspace + `"],"rootPaths":[],"lastModifiedAt":"` + modified + `"}`
	if err := os.WriteFile(filepath.Join(dir, "session.json"), []byte(meta), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "messages.jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func usageLine(pct string) string {
	return `{"id":"x","timestamp":"t","payload":{"type":"session_metadata","key":"contextUsage","value":{"usagePercentage":` + pct + `}}}`
}

func TestKiroUsagePicksNewestSessionOfThisWorkspace(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	fakeKiroSession(t, home, "old", root, "2026-10-01T10:00:00Z", usageLine("10"))
	fakeKiroSession(t, home, "new", root, "2026-10-02T10:00:00Z", usageLine("20"), `{broken`, usageLine("42.5"), `{"payload":{"type":"x"}}`, `{"trunc`)
	fakeKiroSession(t, home, "other", t.TempDir(), "2026-10-09T10:00:00Z", usageLine("99"))
	pct, ok := KiroUsage(home, root)
	if !ok || pct != 42.5 {
		t.Fatalf("got %v %v, want 42.5 (last line of the newest session of this workspace)", pct, ok)
	}
}

func TestKiroUsageNoSessionOrFigure(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	if _, ok := KiroUsage(home, root); ok {
		t.Fatal("no session must report nothing")
	}
	fakeKiroSession(t, home, "a", root, "2026-10-01T10:00:00Z", `{"payload":{"type":"tombstone","kind":"summarization"}}`)
	if _, ok := KiroUsage(home, root); ok {
		t.Fatal("a session without contextUsage must report nothing")
	}
}

func TestKiroUsageFallsBackToFullScanForLargeFile(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	filler := `{"payload":{"type":"message","text":"` + strings.Repeat("a", 1000) + `"}}`
	lines := []string{usageLine("61")}
	for i := 0; i < 400; i++ { // > 256 KB after the only figure
		lines = append(lines, filler)
	}
	fakeKiroSession(t, home, "big", root, "2026-10-01T10:00:00Z", lines...)
	if pct, ok := KiroUsage(home, root); !ok || pct != 61 {
		t.Fatalf("got %v %v", pct, ok)
	}
}

func TestKiroHookWarnsOnceAtThresholdAndStaysSilentOtherwise(t *testing.T) {
	root, home := initProject(t), t.TempDir()
	t.Setenv("KIRO_HOME", home)
	hook := func() result {
		return runIn(t, `{"cwd":"`+root+`"}`, false, root, "context", "hook", "--format", "text", "--source", "kiro", "--event", "prompt")
	}
	if r := hook(); r.code != 0 || r.out != "" || r.err != "" {
		t.Fatalf("no session must be silent: %+v", r)
	}
	fakeKiroSession(t, home, "s", root, "2026-10-01T10:00:00Z", usageLine("50"))
	if r := hook(); r.code != 0 || r.out != "" {
		t.Fatalf("below threshold must print nothing: %+v", r)
	}
	fakeKiroSession(t, home, "s", root, "2026-10-01T10:00:00Z", usageLine("50"), usageLine("78"))
	r := hook()
	if r.code != 0 || r.err != "" || !strings.Contains(r.out, "78%") || !strings.Contains(r.out, "gabitotl context checkpoint") {
		t.Fatalf("over threshold must instruct the agent: %+v", r)
	}
	if r := hook(); r.out != "" {
		t.Fatalf("the warning must not repeat: %+v", r)
	}
	// Kiro compacted: usage drops, the warning is re-armed for the next crossing.
	fakeKiroSession(t, home, "s", root, "2026-10-01T10:00:00Z", usageLine("12"))
	hook()
	fakeKiroSession(t, home, "s", root, "2026-10-01T10:00:00Z", usageLine("80"))
	if r := hook(); !strings.Contains(r.out, "80%") {
		t.Fatalf("must warn again after a compaction: %+v", r)
	}
}

func TestKiroHookNeverFails(t *testing.T) {
	root := initProject(t)
	t.Setenv("KIRO_HOME", filepath.Join(t.TempDir(), "missing"))
	for _, in := range []string{"", "not json", "[]", `{"cwd":"/nonexistent/x"}`} {
		r := runIn(t, in, false, root, "context", "hook", "--format", "text", "--source", "kiro")
		if r.code != 0 || r.out != "" || r.err != "" {
			t.Fatalf("input %q must be silent with exit 0: %+v", in, r)
		}
	}
	if r := runIn(t, "", false, root, "context", "hook", "--source", "nope"); r.code != 0 || r.out != "" || r.err != "" {
		t.Fatalf("unknown source must stay silent: %+v", r)
	}
}

func TestKiroCheckAndReportShowPercent(t *testing.T) {
	root, home := initProject(t), t.TempDir()
	t.Setenv("KIRO_HOME", home)
	fakeKiroSession(t, home, "s", root, "2026-10-01T10:00:00Z", usageLine("81"))
	for _, sub := range []string{"check", "report"} {
		r := runIn(t, "", false, root, "context", sub, "--source", "kiro")
		if r.code != 0 || !strings.Contains(r.out, "81%") || !strings.Contains(r.out, "save-now") {
			t.Fatalf("%s: %+v", sub, r)
		}
	}
	if r := runIn(t, "", false, root, "context", "check", "--source", "kiro", "--json"); !strings.Contains(r.out, `"source":"percent"`) {
		t.Fatalf("%+v", r)
	}
}
