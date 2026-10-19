package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/winnstorm/gabitotl/internal/budget"
)

func TestContextCommands(t *testing.T) {
	root := initProject(t)
	r := runIn(t, "", false, root, "context", "check")
	if r.code != 0 || strings.Count(strings.TrimSpace(r.out), "\n") != 0 || !strings.Contains(r.out, "(0%) ok") {
		t.Fatalf("%+v", r)
	}
	r = runIn(t, strings.Repeat("a", 3600), false, root, "context", "add")
	if r.code != 0 || !strings.Contains(r.out, "context: ~1k/200k") {
		t.Fatalf("%+v", r)
	}
	r = runIn(t, "", false, root, "context", "report", "--tokens", "160000")
	if r.code != 0 || !strings.Contains(r.out, "~160k/200k (80%) save-now") {
		t.Fatalf("%+v", r)
	}
	r = runIn(t, "", false, root, "context", "check", "--json")
	if r.code != 0 || !strings.Contains(r.out, `"should_save":true`) || !strings.Contains(r.out, `"source":"reported"`) {
		t.Fatalf("%+v", r)
	}
	if r := runIn(t, "", false, root, "context", "report"); r.code != ExitUsage {
		t.Fatalf("report without --tokens must be a usage error: %+v", r)
	}
}

func TestContextCheckpointWithoutProviderExitsZero(t *testing.T) {
	root := initProject(t)
	withProvider(t, &stubProvider{})
	r := runIn(t, "", false, root, "context", "checkpoint", "--next", "finish E2")
	if r.code != 0 || !strings.HasPrefix(r.out, "context: checkpoint") || strings.Count(r.out, "\n") != 1 {
		t.Fatalf("%+v", r)
	}
}

func TestTranscriptTokens(t *testing.T) {
	dir := t.TempDir()
	usage := filepath.Join(dir, "u.jsonl")
	body := `{"type":"user","message":{"content":"hola"}}` + "\n" +
		`{"type":"assistant","message":{"usage":{"input_tokens":10,"cache_read_input_tokens":100,"cache_creation_input_tokens":5,"output_tokens":9}}}` + "\n" +
		`{"type":"assistant","message":{"usage":{"input_tokens":20,"cache_read_input_tokens":200,"cache_creation_input_tokens":30}}}` + "\n"
	if err := os.WriteFile(usage, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if n, src, err := TranscriptTokens(usage); err != nil || n != 250 || src != budget.SourceReported {
		t.Fatalf("got %d %s %v", n, src, err)
	}
	plain := filepath.Join(dir, "p.jsonl")
	if err := os.WriteFile(plain, []byte(strings.Repeat("x", 400)), 0o644); err != nil {
		t.Fatal(err)
	}
	if n, src, _ := TranscriptTokens(plain); n != 100 || src != budget.SourceEstimated {
		t.Fatalf("got %d %s", n, src)
	}
	if _, _, err := TranscriptTokens(filepath.Join(dir, "none")); err == nil {
		t.Fatal("missing transcript must error")
	}
}

func TestContextHookFiresOnceAndRearms(t *testing.T) {
	root := initProject(t)
	tr := filepath.Join(t.TempDir(), "t.jsonl")
	write := func(tokens string) {
		line := `{"message":{"usage":{"input_tokens":` + tokens + `}}}` + "\n"
		if err := os.WriteFile(tr, []byte(line), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	payload := `{"transcript_path":"` + tr + `","cwd":"` + root + `"}`
	write("170000")
	r := runIn(t, payload, false, root, "context", "hook")
	if r.code != 0 || !strings.Contains(r.out, `"additionalContext"`) || !strings.Contains(r.out, "gabitotl context checkpoint") || len(r.out) > 700 {
		t.Fatalf("%+v", r)
	}
	if r := runIn(t, payload, false, root, "context", "hook"); r.out != "" {
		t.Fatalf("second call must stay silent: %+v", r)
	}
	write("1000") // compaction happened: re-armed
	runIn(t, payload, false, root, "context", "hook")
	write("170000")
	if r := runIn(t, payload, false, root, "context", "hook"); !strings.Contains(r.out, "additionalContext") {
		t.Fatalf("hook must fire again after a re-arm: %+v", r)
	}
	for _, bad := range []string{"", "not json", `{"transcript_path":"/nope"}`} {
		if r := runIn(t, bad, false, root, "context", "hook"); r.code != 0 || r.out != "" {
			t.Fatalf("bad input %q must be silent: %+v", bad, r)
		}
	}
}

func TestContextHookTextFormatAndUnknownInput(t *testing.T) {
	root := initProject(t)
	for _, bad := range []string{"", "not json", "[]", `{"hook_event_name":"promptSubmit","prompt":"hi"}`, `{"usage":"x"}`} {
		r := runIn(t, bad, false, root, "context", "hook", "--format", "text")
		if r.code != 0 || r.out != "" || r.err != "" {
			t.Fatalf("input %q must be silent: %+v", bad, r)
		}
	}
	if r := runIn(t, "", false, root, "context", "hook", "--bogus"); r.code != 0 || r.out != "" || r.err != "" {
		t.Fatalf("bad flag must be silent: %+v", r)
	}
	// Kiro-like payload with an obvious numeric usage field.
	r := runIn(t, `{"cwd":"`+root+`","usage":{"input_tokens":170000}}`, false, root, "context", "hook", "--format", "text")
	if r.code != 0 || r.err != "" || strings.Contains(r.out, "additionalContext") || !strings.Contains(r.out, "gabitotl context checkpoint") {
		t.Fatalf("text output expected: %+v", r)
	}
}

func TestContextHookDumpWritesAndPrunes(t *testing.T) {
	root := initProject(t)
	for i := 0; i < 8; i++ {
		r := runIn(t, `{"cwd":"`+root+`","n":`+strings.Repeat("1", i+1)+`}`, false, root, "context", "hook", "--dump", "--event", "prompt/x")
		if r.code != 0 || r.out != "" || r.err != "" {
			t.Fatalf("%+v", r)
		}
	}
	files, _ := filepath.Glob(filepath.Join(root, ".gabitotl", "tmp", "hook-stdin-prompt-x-*.json"))
	if len(files) != 5 {
		t.Fatalf("want newest 5 dumps, got %v", files)
	}
	b, _ := os.ReadFile(files[len(files)-1])
	if !strings.Contains(string(b), `"n":11111111`) {
		t.Fatalf("newest dump missing: %s", b)
	}
}
