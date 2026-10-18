package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/memory"
)

type stubProvider struct {
	q    memory.Query
	rep  memory.Report
	hits []memory.Entry
}

func (s *stubProvider) Search(_ context.Context, q memory.Query) ([]memory.Entry, error) {
	s.q = q
	return s.hits, nil
}
func (s *stubProvider) Save(context.Context, memory.Entry) (memory.ID, error) { return "", nil }
func (s *stubProvider) Update(context.Context, memory.ID, memory.EntryPatch) error {
	return memory.ErrUnsupported
}
func (s *stubProvider) SessionSummary(context.Context, memory.Summary) error {
	return memory.ErrUnsupported
}
func (s *stubProvider) CurrentProject(context.Context) (string, error) { return "p", nil }
func (s *stubProvider) Doctor(context.Context) memory.Report           { return s.rep }

func initProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	r := runIn(t, "", false, t.TempDir(), "init", "--root", root, "--name", "Shop", "--kind", "new",
		"--user-id", "gabi", "--user-name", "Gabi", "--yes")
	if r.code != 0 {
		t.Fatalf("init: %s", r.err)
	}
	return root
}

func withProvider(t *testing.T, p memory.Provider) {
	t.Helper()
	old := newProvider
	newProvider = func(gabitotl.MemoryConfig) (memory.Provider, error) { return p, nil }
	t.Cleanup(func() { newProvider = old })
}

func TestMemoryDoctor(t *testing.T) {
	root := initProject(t)
	sp := &stubProvider{rep: memory.Report{Provider: "engram", Checks: []memory.Check{{Name: "engram", Status: memory.StatusOK, Message: "CLI reachable"}}}}
	withProvider(t, sp)
	r := runIn(t, "", false, root, "memory", "doctor")
	if r.code != 0 || !strings.Contains(r.out, "provider: engram") || !strings.Contains(r.out, "[ok] engram: CLI reachable") {
		t.Fatalf("%+v", r)
	}
	sp.rep.Checks = append(sp.rep.Checks, memory.Check{Name: "x", Status: memory.StatusError, Message: "bad"})
	if r := runIn(t, "", false, root, "memory", "doctor"); r.code != ExitError {
		t.Fatalf("failing report must exit 1, got %d", r.code)
	}
}

func TestMemorySearch(t *testing.T) {
	root := initProject(t)
	sp := &stubProvider{hits: []memory.Entry{{ID: "7", Type: memory.TypeDecision, Title: "Use JWT", Content: "tokens\nfor auth", Actor: "gabi"}}}
	withProvider(t, sp)
	r := runIn(t, "", false, root, "memory", "search", "--limit", "3", "jwt", "auth")
	if r.code != 0 || !strings.Contains(r.out, "#7 [decision] Use JWT") || !strings.Contains(r.out, "actor: gabi") {
		t.Fatalf("%+v", r)
	}
	if sp.q.Text != "jwt auth" || sp.q.Limit != 3 || !sp.q.MatchAny || sp.q.Project != "shop" {
		t.Fatalf("query %+v", sp.q)
	}
	if r := runIn(t, "", false, root, "memory", "search"); r.code != ExitUsage {
		t.Fatalf("no terms must be usage error, got %d", r.code)
	}
	if r := runIn(t, "", false, root, "memory", "bogus"); r.code != ExitUsage {
		t.Fatalf("unknown sub must be usage error, got %d", r.code)
	}
}

func TestMemoryWithoutInit(t *testing.T) {
	r := runIn(t, "", false, t.TempDir(), "memory", "doctor")
	if r.code != ExitError {
		t.Fatalf("uninitialized project must fail, got %+v", r)
	}
}

func TestMemorySearchJSONAndLimit(t *testing.T) {
	root := initProject(t)
	withProvider(t, &stubProvider{hits: []memory.Entry{{ID: "7", Type: memory.TypeDecision, Title: "Use JWT", Content: "x"}}})
	r := runIn(t, "", false, root, "memory", "search", "--json", "jwt")
	var got []memory.Entry
	if r.code != 0 || json.Unmarshal([]byte(r.out), &got) != nil || len(got) != 1 || got[0].ID != "7" {
		t.Fatalf("%+v", r)
	}
	for _, l := range []string{"0", "21", "-1"} {
		if r := runIn(t, "", false, root, "memory", "search", "--limit", l, "jwt"); r.code != ExitUsage {
			t.Fatalf("--limit %s must be usage error, got %d", l, r.code)
		}
	}
}

func TestMemoryContext(t *testing.T) {
	root := initProject(t)
	long := strings.Repeat("A fairly long sentence about tokens. ", 100)
	sp := &stubProvider{hits: []memory.Entry{
		{ID: "7", Type: memory.TypeDecision, Title: "Use JWT", Content: long},
		{ID: "8", Type: memory.TypeBugfix, Title: "Fix race", Content: "short"},
	}}
	withProvider(t, sp)
	r := runIn(t, "", false, root, "memory", "context", "--task", "auth jwt tokens", "--per-entry-tokens", "60")
	if r.code != 0 || !strings.Contains(r.out, "decision · Use JWT · #7") || !strings.Contains(r.out, "memory: 2 entries, ~") {
		t.Fatalf("%+v", r)
	}
	if len(r.out) > 2000 {
		t.Fatalf("bundle not capped: %d bytes", len(r.out))
	}
	r = runIn(t, "", false, root, "memory", "context", "--task", "auth", "--max-tokens", "40", "--json")
	var b memory.Bundle
	if r.code != 0 || json.Unmarshal([]byte(r.out), &b) != nil || b.Tokens > 40 || b.Omitted == 0 {
		t.Fatalf("%+v %+v", r, b)
	}
	if r := runIn(t, "", false, root, "memory", "context"); r.code != ExitUsage {
		t.Fatalf("missing --task must be usage error, got %d", r.code)
	}
}

func TestMemoryContextNoProvider(t *testing.T) {
	r := runIn(t, "", false, t.TempDir(), "memory", "context", "--task", "x")
	if r.code != 0 || !strings.Contains(r.out, "no provider") {
		t.Fatalf("%+v", r)
	}
}
