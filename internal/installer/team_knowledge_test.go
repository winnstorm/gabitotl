package installer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/importer"
	"github.com/winnstorm/gabitotl/internal/memory"
	"github.com/winnstorm/gabitotl/internal/memory/mcp"
	"github.com/winnstorm/gabitotl/internal/memory/server"
	"github.com/winnstorm/gabitotl/internal/memory/store"
	"github.com/winnstorm/gabitotl/internal/memory/store/memstore"
)

const agentsMD = "# Decisions\n\nWe chose PostgreSQL for the shared memory.\n\n# Commands\n\nRun the tests with make test.\n"

// liveTeam is the real Memory MCP server over HTTP on a loopback port, backed by
// an in-memory store: it authenticates tokens and derives the actor from them,
// exactly like the real one. Nothing outside the process is touched.
type liveTeam struct {
	ts *httptest.Server
	st store.Store
}

func newLiveTeam(t *testing.T, st store.Store) *liveTeam {
	t.Helper()
	srv, err := server.New(server.Config{Store: st, UserRate: 1000, UserBurst: 1000, DefaultProject: "unused"})
	if err != nil {
		t.Fatal(err)
	}
	h, err := srv.Handler()
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(http.HandlerFunc(h.ServeHTTP))
	t.Cleanup(ts.Close)
	return &liveTeam{ts: ts, st: st}
}

func (l *liveTeam) url() string { return l.ts.URL + server.EndpointPath }

// provider is the authenticated client of the server, reading the token from the
// fixture's 0600 secrets file like every real client.
func (l *liveTeam) provider(home string, cfg gabitotl.MemoryConfig) (memory.Provider, error) {
	return mcp.NewFromConfig(cfg, mcp.Resolver{Getenv: func(string) string { return "" }, Home: home})
}

func TestKnowledgeSinkSendsNoActorToTheAuthenticatedServer(t *testing.T) {
	ctx := context.Background()
	st := memstore.New()
	live := newLiveTeam(t, st)
	tok, _, err := server.NewScopedToken(ctx, st, "gabi", []string{"shop"}, false, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	cfg := gabitotl.MemoryConfig{Mode: gabitotl.ModeTeam, Provider: gabitotl.MemoryMemoryMCP, ProjectKey: "shop", URL: live.url(), Token: "env:T"}
	p, err := mcp.NewFromConfig(cfg, mcp.Resolver{Getenv: func(k string) string {
		if k == "T" {
			return tok
		}
		return ""
	}})
	if err != nil {
		t.Fatal(err)
	}
	k := importer.Knowledge{Kind: importer.KindDecision, Title: "Use PostgreSQL", Body: "Shared memory.", Origin: "instructions", Path: "AGENTS.md"}

	// The server's contract, as the previous implementation broke it: a claimed
	// "legacy" actor never authenticates.
	if _, err := p.Save(ctx, memory.Entry{Type: memory.TypeDecision, Title: "x", Content: "y", Project: "shop", Actor: memory.ActorLegacy}); err == nil {
		t.Fatal("the server must reject a claimed legacy actor")
	}

	if err := KnowledgeSink(p, cfg)(ctx, k); err != nil {
		t.Fatalf("the import must work through the authenticated client: %v", err)
	}
	rs, err := st.Search(ctx, memory.Query{Text: "postgresql", Project: "shop"})
	if err != nil || len(rs) != 1 {
		t.Fatalf("%v %+v", err, rs)
	}
	if rs[0].Actor != "gabi" || rs[0].Source != "gabitotl-import" {
		t.Fatalf("the actor comes from the token and the provenance stays in source: %+v", rs[0])
	}
}

func TestKnowledgeSinkKeepsLegacyForProvidersWithoutTokens(t *testing.T) {
	rec := &recMemory{}
	cfg := gabitotl.MemoryConfig{Mode: gabitotl.ModeTeam, Provider: gabitotl.MemoryEngram, ProjectKey: "shop"}
	k := importer.Knowledge{Kind: importer.KindTask, Title: "Pending", Body: "b", Origin: "kiro", Path: "x.md"}
	if err := KnowledgeSink(rec, cfg)(context.Background(), k); err != nil {
		t.Fatal(err)
	}
	if len(rec.saved) != 1 || rec.saved[0].Actor != memory.ActorLegacy {
		t.Fatalf("%+v", rec.saved)
	}
}

// teamKnowledgeRig is a team rig whose memory provider is the real client of a
// live server, so the whole install goes through authentication.
func teamKnowledgeRig(t *testing.T, st *memstore.Store) (*teamRig, *liveTeam) {
	t.Helper()
	r := newTeamRig(t)
	r.st = st
	live := newLiveTeam(t, st)
	r.env.OpenMemory = func(cfg gabitotl.MemoryConfig) (memory.Provider, error) { return live.provider(r.home, cfg) }
	writeFile(t, r.root+"/AGENTS.md", agentsMD)
	return r, live
}

func hostKnowledgeOpts(live *liveTeam) Options {
	o := hostOpts()
	o.MemoryURL = live.url()
	return o
}

func TestHostInstallImportsKnowledgeThroughTheAuthenticatedClient(t *testing.T) {
	st := memstore.New()
	r, live := teamKnowledgeRig(t, st)
	o := hostKnowledgeOpts(live)
	inv, pl, err := r.plan(o)
	if err != nil {
		t.Fatal(err)
	}
	res, err := Run(context.Background(), r.env, inv, pl, nil, Hooks{})
	if err != nil {
		t.Fatalf("%v (%+v)", err, res.Checks)
	}
	for _, w := range res.Warnings {
		if strings.Contains(w, "import") {
			t.Fatalf("the import must not warn: %v", res.Warnings)
		}
	}
	var imported int
	for _, o := range res.Imports {
		if o.Status == importer.StatusImported {
			imported += len(o.Items)
		}
	}
	if imported == 0 {
		t.Fatalf("expected imported knowledge: %+v", res.Imports)
	}
	rs, _ := st.Search(context.Background(), memory.Query{Text: "postgresql", Project: inv.Project.Key})
	if len(rs) == 0 || rs[0].Actor != "gabi" {
		t.Fatalf("saved as the token's owner: %+v", rs)
	}

	// Re-running changes nothing and takes no new snapshot.
	snaps, _ := ListSnapshots(r.env)
	inv2, pl2, err := r.plan(o)
	if err != nil {
		t.Fatal(err)
	}
	if pl2.Mutates() {
		t.Fatalf("a re-run with nothing to do must not mutate: %+v", pl2.Items)
	}
	res2, err := Run(context.Background(), r.env, inv2, pl2, nil, Hooks{})
	if err != nil || !res2.Skipped {
		t.Fatalf("%v %+v", err, res2)
	}
	if after, _ := ListSnapshots(r.env); len(after) != len(snaps) {
		t.Fatalf("no new snapshot: %d -> %d", len(snaps), len(after))
	}
}

func TestUnreachableTeamMemoryDefersTheImportWithoutMutatingOnRerun(t *testing.T) {
	st := memstore.New()
	r, live := teamKnowledgeRig(t, st)
	o := hostKnowledgeOpts(live)
	live.ts.Close() // the server process is not running (the host starts it by hand)

	inv, pl, err := r.plan(o)
	if err != nil {
		t.Fatal(err)
	}
	res, err := Run(context.Background(), r.env, inv, pl, nil, Hooks{})
	if err != nil {
		t.Fatalf("an unreachable server must not fail the install: %v", err)
	}
	if !warned(res.Warnings, "unreachable") {
		t.Fatalf("the warning must say the server is unreachable: %v", res.Warnings)
	}
	if len(res.Imports) != 0 {
		t.Fatalf("nothing may be recorded as imported: %+v", res.Imports)
	}
	if reg, _ := importer.LoadRegistry(gabitotl.NewLayout(r.root)); len(reg.Imports) != 0 {
		t.Fatalf("the registry must stay empty so the next run retries: %+v", reg)
	}

	// The re-run reports it and changes nothing: no new snapshot, every time.
	for i := 0; i < 2; i++ {
		snaps, _ := ListSnapshots(r.env)
		inv2, pl2, err := r.plan(o)
		if err != nil {
			t.Fatal(err)
		}
		if !pl2.HasNote(NoteTeamNotReady) || pl2.Mutates() {
			t.Fatalf("run %d: want the not-ready note and no mutation (notes %v, items %+v)", i, pl2.Notes, pl2.Items)
		}
		if res2, err := Run(context.Background(), r.env, inv2, pl2, nil, Hooks{}); err != nil || !res2.Skipped {
			t.Fatalf("%v %+v", err, res2)
		}
		if after, _ := ListSnapshots(r.env); len(after) != len(snaps) {
			t.Fatalf("a snapshot was taken on a no-op re-run: %d -> %d", len(snaps), len(after))
		}
	}
}

func warned(ws []string, sub string) bool {
	for _, w := range ws {
		if strings.Contains(w, sub) {
			return true
		}
	}
	return false
}
