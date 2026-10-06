package mcp_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/memory"
	"github.com/winnstorm/gabitotl/internal/memory/mcp"
	"github.com/winnstorm/gabitotl/internal/memory/server"
	"github.com/winnstorm/gabitotl/internal/memory/store"
	"github.com/winnstorm/gabitotl/internal/memory/store/memstore"
	"github.com/winnstorm/gabitotl/internal/memory/store/pgstore"
	"github.com/winnstorm/gabitotl/internal/pgmanaged/pgtest"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Main(m)) }

type rig struct {
	ts    *httptest.Server
	st    store.Store
	hits  atomic.Int32
	token string
}

func newRig(t *testing.T) *rig { return newRigStore(t, memstore.New()) }

func newRigStore(t *testing.T, st store.Store) *rig {
	t.Helper()
	r := &rig{st: st}
	srv, err := server.New(server.Config{Store: r.st, UserRate: 1000, UserBurst: 1000, DefaultProject: "kraken"})
	if err != nil {
		t.Fatal(err)
	}
	h, err := srv.Handler()
	if err != nil {
		t.Fatal(err)
	}
	r.ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		r.hits.Add(1)
		h.ServeHTTP(w, q)
	}))
	t.Cleanup(r.ts.Close)
	r.token = r.newToken(t, "gabi")
	return r
}

func (r *rig) newToken(t *testing.T, user string) string {
	t.Helper()
	plain, _, err := server.NewToken(context.Background(), r.st, user, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return plain
}

func (r *rig) provider(t *testing.T, token string, project string) (*mcp.Provider, *mcp.SDKTransport) {
	t.Helper()
	tr, err := mcp.NewHTTPTransport(mcp.HTTPOptions{
		URL: r.ts.URL + server.EndpointPath, TokenRef: "env:GBT_TEST_TOKEN", AllowInsecure: true,
		Getenv: func(k string) string {
			if k == "GBT_TEST_TOKEN" {
				return token
			}
			return ""
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tr.Close() })
	p, err := mcp.New(mcp.Options{Transport: tr, ConnectionRef: "env:GBT_TEST_TOKEN", Project: project, Getenv: func(string) string { return "x" }})
	if err != nil {
		t.Fatal(err)
	}
	return p, tr
}

func TestHTTPRoundTripThroughTheProvider(t *testing.T) { roundTrip(t, newRig(t)) }

// TestHTTPRoundTripAgainstPostgreSQL runs the same flow over the real store
// (opt-in: GABITOTL_TEST_DOCKER=1 or GABITOTL_TEST_PG_DSN).
func TestHTTPRoundTripAgainstPostgreSQL(t *testing.T) {
	st, err := pgstore.Open(context.Background(), pgtest.Database(t), pgstore.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	roundTrip(t, newRigStore(t, st))
}

func roundTrip(t *testing.T, r *rig) {
	p, _ := r.provider(t, r.token, "kraken")
	ctx := context.Background()
	id, err := p.Save(ctx, memory.Entry{Type: memory.TypeDecision, Title: "Use PostgreSQL", Content: "shared memory for the team", TopicKey: "arch/db"})
	if err != nil || id == "" {
		t.Fatalf("save: %v", err)
	}
	got, err := p.Search(ctx, memory.Query{Text: "postgresql"})
	if err != nil || len(got) != 1 {
		t.Fatalf("search: %v %v", got, err)
	}
	if got[0].Actor != "gabi" || got[0].Project != "kraken" {
		t.Fatalf("actor must come from the token: %+v", got[0])
	}
	title := "Use PostgreSQL 17"
	if err := p.Update(ctx, id, memory.EntryPatch{Title: &title}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if err := p.SessionSummary(ctx, memory.Summary{Goal: "g", Accomplished: "a", Actor: "gabi"}); err != nil {
		t.Fatalf("summary: %v", err)
	}
	if proj, err := p.CurrentProject(ctx); err != nil || proj != "kraken" {
		t.Fatalf("current project: %q %v", proj, err)
	}
	if act, err := p.ActiveWork(ctx, mcp.ActiveQuery{Actor: "gabi"}); err != nil || len(act) == 0 {
		t.Fatalf("active work: %v %v", act, err)
	}
	rep := p.Doctor(ctx)
	if !rep.OK() {
		t.Fatalf("doctor: %+v", rep)
	}
}

func TestActorSpoofIsAToolErrorNotRetried(t *testing.T) {
	r := newRig(t)
	p, _ := r.provider(t, r.token, "kraken")
	_, err := p.Save(context.Background(), memory.Entry{Type: memory.TypeDecision, Title: "t", Content: "c", Actor: "gonza"})
	var te *mcp.ToolError
	if !errors.As(err, &te) || !strings.Contains(te.Message, "actor mismatch") {
		t.Fatalf("want a tool error about the actor, got %v", err)
	}
}

func TestBadAndMissingTokensMapToErrAuth(t *testing.T) {
	r := newRig(t)
	p, _ := r.provider(t, "gbt_0000000000000000.aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "kraken")
	if _, err := p.Search(context.Background(), memory.Query{Text: "x"}); !errors.Is(err, mcp.ErrAuth) {
		t.Fatalf("a rejected token must be ErrAuth, got %v", err)
	}
	p2, _ := r.provider(t, "", "kraken")
	before := r.hits.Load()
	_, err := p2.Search(context.Background(), memory.Query{Text: "x"})
	if !errors.Is(err, mcp.ErrAuth) {
		t.Fatalf("an unset token must be ErrAuth, got %v", err)
	}
	if r.hits.Load() != before {
		t.Fatal("with no token no request should reach the server")
	}
}

func TestTokenIsReadAtCallTime(t *testing.T) {
	r := newRig(t)
	cur := ""
	tr, err := mcp.NewHTTPTransport(mcp.HTTPOptions{URL: r.ts.URL + "/mcp", TokenRef: "env:T", AllowInsecure: true,
		Getenv: func(string) string { return cur }})
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()
	p, _ := mcp.New(mcp.Options{Transport: tr, Project: "kraken"})
	if _, err := p.Search(context.Background(), memory.Query{Text: "x"}); !errors.Is(err, mcp.ErrAuth) {
		t.Fatalf("before the token exists: %v", err)
	}
	cur = r.token
	if _, err := p.Search(context.Background(), memory.Query{Text: "x"}); err != nil {
		t.Fatalf("after the token is exported it must work without a restart: %v", err)
	}
}

func TestUnreachableServerAndRetryOnlyForReads(t *testing.T) {
	r := newRig(t)
	url := r.ts.URL + "/mcp"
	r.ts.Close() // nothing listens any more
	tr, err := mcp.NewHTTPTransport(mcp.HTTPOptions{URL: url, TokenRef: "env:T", AllowInsecure: true, Timeout: 3 * time.Second,
		Getenv: func(string) string { return r.token }})
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()
	p, _ := mcp.New(mcp.Options{Transport: tr, Project: "kraken"})
	if _, err := p.Search(context.Background(), memory.Query{Text: "x"}); !errors.Is(err, mcp.ErrUnreachable) {
		t.Fatalf("want ErrUnreachable, got %v", err)
	}
	if _, err := p.Save(context.Background(), memory.Entry{Type: memory.TypeDecision, Title: "t", Content: "c"}); !errors.Is(err, mcp.ErrUnreachable) {
		t.Fatalf("want ErrUnreachable for a write, got %v", err)
	}
}

func TestReadsRetryAfterAServerRestartWritesDoNot(t *testing.T) {
	st := memstore.New()
	var fail atomic.Int32
	srv, _ := server.New(server.Config{Store: st, UserRate: 1000, UserBurst: 1000, DefaultProject: "kraken"})
	h, _ := srv.Handler()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		if fail.Load() > 0 && q.Method == http.MethodPost {
			fail.Add(-1)
			if hj, ok := w.(http.Hijacker); ok {
				c, _, _ := hj.Hijack()
				_ = c.Close() // drop the connection mid request
				return
			}
		}
		h.ServeHTTP(w, q)
	}))
	defer ts.Close()
	plain, _, _ := server.NewToken(context.Background(), st, "gabi", time.Now())
	tr, _ := mcp.NewHTTPTransport(mcp.HTTPOptions{URL: ts.URL + "/mcp", TokenRef: "env:T", AllowInsecure: true, Timeout: 5 * time.Second,
		Getenv: func(string) string { return plain }})
	defer tr.Close()
	p, _ := mcp.New(mcp.Options{Transport: tr, Project: "kraken"})
	if _, err := p.Search(context.Background(), memory.Query{Text: "x"}); err != nil { // opens the session
		t.Fatal(err)
	}
	fail.Store(1)
	if _, err := p.Search(context.Background(), memory.Query{Text: "x"}); err != nil {
		t.Fatalf("a read must be retried once: %v", err)
	}
	fail.Store(1)
	if _, err := p.Save(context.Background(), memory.Entry{Type: memory.TypeDecision, Title: "t", Content: "c"}); err == nil {
		t.Fatal("a write must not be retried silently")
	}
}

func TestRateLimitMapsToErrRateLimited(t *testing.T) {
	st := memstore.New()
	srv, _ := server.New(server.Config{Store: st, UserRate: 0.0001, UserBurst: 1, DefaultProject: "kraken"})
	h, _ := srv.Handler()
	ts := httptest.NewServer(h)
	defer ts.Close()
	plain, _, _ := server.NewToken(context.Background(), st, "gabi", time.Now())
	tr, _ := mcp.NewHTTPTransport(mcp.HTTPOptions{URL: ts.URL + "/mcp", TokenRef: "env:T", AllowInsecure: true, Timeout: 5 * time.Second,
		Getenv: func(string) string { return plain }})
	defer tr.Close()
	p, _ := mcp.New(mcp.Options{Transport: tr, Project: "kraken"})
	var last error
	for i := 0; i < 6 && last == nil; i++ {
		_, last = p.Save(context.Background(), memory.Entry{Type: memory.TypeDecision, Title: "t", Content: "c"})
	}
	if !errors.Is(last, mcp.ErrRateLimited) {
		t.Fatalf("want ErrRateLimited, got %v", last)
	}
}

func TestWrongEndpointMapsToSchemaMismatch(t *testing.T) {
	r := newRig(t)
	tr, _ := mcp.NewHTTPTransport(mcp.HTTPOptions{URL: r.ts.URL + "/nope", TokenRef: "env:T", AllowInsecure: true, Timeout: 3 * time.Second,
		Getenv: func(string) string { return r.token }})
	defer tr.Close()
	p, _ := mcp.New(mcp.Options{Transport: tr, Project: "kraken"})
	if _, err := p.Search(context.Background(), memory.Query{Text: "x"}); !errors.Is(err, mcp.ErrSchema) {
		t.Fatalf("a 404 means the wrong endpoint, want ErrSchema, got %v", err)
	}
}

func TestUnknownToolMapsToSchemaMismatch(t *testing.T) {
	r := newRig(t)
	p, tr := r.provider(t, r.token, "kraken")
	_ = p
	if _, err := tr.Call(context.Background(), "memory_does_not_exist", map[string]any{}); !errors.Is(err, mcp.ErrSchema) {
		t.Fatalf("want ErrSchema, got %v", err)
	}
}

func TestURLChecks(t *testing.T) {
	for _, tc := range []struct {
		url   string
		allow bool
		ok    bool
	}{
		{"https://memoria.example.com/mcp", false, true},
		{"http://127.0.0.1:8765/mcp", false, true},
		{"http://localhost:8765/mcp", false, true},
		{"http://memoria.example.com/mcp", false, false},
		{"http://memoria.example.com/mcp", true, true},
		{"https://user:pw@memoria.example.com/mcp", false, false},
		{"ftp://x/mcp", false, false},
		{"memoria", false, false},
	} {
		if err := mcp.CheckURL(tc.url, tc.allow); (err == nil) != tc.ok {
			t.Errorf("CheckURL(%q, %v) = %v", tc.url, tc.allow, err)
		}
	}
	if _, err := mcp.NewHTTPTransport(mcp.HTTPOptions{URL: "https://x/mcp", TokenRef: "gbt_literal.secret"}); err == nil {
		t.Fatal("a literal token must be refused")
	}
}

func TestFromConfigUsesTheSecretsFileWhenTheEnvironmentLacksTheToken(t *testing.T) {
	r := newRig(t)
	home := t.TempDir()
	if err := mcp.WriteClientSecret(home, "GABITOTL_MEMORY_TOKEN", r.token); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(mcp.ClientSecretsPath(home))
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("secrets file must be 0600: %v %v", st, err)
	}
	cfg := gabitotl.MemoryConfig{Mode: gabitotl.ModeTeam, Provider: gabitotl.MemoryMemoryMCP, ProjectKey: "kraken",
		URL: r.ts.URL + "/mcp", Token: "env:GABITOTL_MEMORY_TOKEN"}
	// http://127.0.0.1 is loopback, so no insecure flag is needed.
	p, err := mcp.NewFromConfig(cfg, mcp.Resolver{Getenv: func(string) string { return "" }, Home: home})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Save(context.Background(), memory.Entry{Type: memory.TypeDecision, Title: "t", Content: "c", Actor: "gabi"}); err != nil {
		t.Fatalf("save through the provider built from memory.md: %v", err)
	}
	// Writing another variable keeps the first one.
	if err := mcp.WriteClientSecret(home, "OTHER", "v"); err != nil {
		t.Fatal(err)
	}
	if (mcp.Resolver{Home: home, Getenv: func(string) string { return "" }}).Lookup("GABITOTL_MEMORY_TOKEN") != r.token {
		t.Fatal("the first secret was lost")
	}
	if b, _ := os.ReadFile(filepath.Join(home, ".gabitotl", "secrets", "memory-client.env")); !strings.Contains(string(b), "OTHER=v") {
		t.Fatal("second secret missing")
	}
}

func TestFromConfigRefusals(t *testing.T) {
	base := gabitotl.MemoryConfig{Mode: gabitotl.ModeTeam, Provider: gabitotl.MemoryMemoryMCP, ProjectKey: "p"}
	for name, cfg := range map[string]gabitotl.MemoryConfig{
		"no url":   base,
		"no token": {Mode: base.Mode, Provider: base.Provider, ProjectKey: "p", URL: "https://x/mcp"},
	} {
		if _, err := mcp.NewFromConfig(cfg, mcp.Resolver{}); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestStdioTransportAgainstAHelperProcess(t *testing.T) {
	if os.Getenv("GBT_HELPER_STDIO") == "1" {
		return // never reached: the helper is TestHelperStdioServer
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	tr, err := mcp.NewStdioTransport(mcp.StdioOptions{Command: exe, Args: []string{"-test.run=TestHelperStdioServer"},
		Env: append(os.Environ(), "GBT_HELPER_STDIO=1"), Timeout: 20 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()
	p, _ := mcp.New(mcp.Options{Transport: tr, Project: "kraken"})
	id, err := p.Save(context.Background(), memory.Entry{Type: memory.TypeDecision, Title: "stdio", Content: "works"})
	if err != nil || id == "" {
		t.Fatalf("save over stdio: %v", err)
	}
	got, err := p.Search(context.Background(), memory.Query{Text: "stdio"})
	if err != nil || len(got) != 1 || got[0].Actor != "local-dev" {
		t.Fatalf("search over stdio: %+v %v", got, err)
	}
}

// TestHelperStdioServer is the child process of the stdio test: a real
// Memory MCP server over stdio with a fixed actor.
func TestHelperStdioServer(t *testing.T) {
	if os.Getenv("GBT_HELPER_STDIO") != "1" {
		t.Skip("helper process")
	}
	srv, err := server.New(server.Config{Store: memstore.New(), FixedActor: "local-dev", DefaultProject: "kraken"})
	if err != nil {
		t.Fatal(err)
	}
	_ = srv.ServeStdio(context.Background())
	os.Exit(0)
}

func checkByName(rep memory.Report, name string) (memory.Check, bool) {
	for _, c := range rep.Checks {
		if c.Name == name {
			return c, true
		}
	}
	return memory.Check{}, false
}

func TestDoctorReportsAuthSchemaProjectAndReachability(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()

	ok, _ := r.provider(t, r.token, "kraken")
	rep := ok.Doctor(ctx)
	for _, name := range []string{"connection", "server", "authentication", "project"} {
		if c, found := checkByName(rep, name); !found || c.Status != memory.StatusOK {
			t.Fatalf("%s must be ok: %+v", name, rep)
		}
	}
	if c, found := checkByName(rep, "server schema"); found && c.Status == memory.StatusError {
		t.Fatalf("schema: %+v", c)
	}

	// A token for another project: valid, but the project is not allowed.
	scoped := mustScoped(t, r, "gonza", "shop")
	p, _ := r.provider(t, scoped, "kraken")
	if c, _ := checkByName(p.Doctor(ctx), "project"); c.Status != memory.StatusError || !strings.Contains(c.Message, "not allowed") {
		t.Fatalf("an out-of-scope project must be an error: %+v", c)
	}
	p, _ = r.provider(t, scoped, "shop")
	if c, _ := checkByName(p.Doctor(ctx), "project"); c.Status != memory.StatusOK {
		t.Fatalf("an allowed project is ok: %+v", c)
	}

	// No project key in memory.md.
	none, _ := r.provider(t, r.token, "")
	if c, _ := checkByName(none.Doctor(ctx), "project"); c.Status != memory.StatusWarn {
		t.Fatalf("a missing project key warns: %+v", c)
	}

	// A rejected token is an authentication error and nothing else is probed.
	bad, _ := r.provider(t, "gbt_0000000000000000.aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "kraken")
	rep = bad.Doctor(ctx)
	if c, found := checkByName(rep, "authentication"); !found || c.Status != memory.StatusError || rep.OK() {
		t.Fatalf("%+v", rep)
	}

	// An unreachable server is a server error.
	url := r.ts.URL + "/mcp"
	r.ts.Close()
	tr, _ := mcp.NewHTTPTransport(mcp.HTTPOptions{URL: url, TokenRef: "env:T", AllowInsecure: true, Timeout: 3 * time.Second, Getenv: func(string) string { return r.token }})
	defer tr.Close()
	dead, _ := mcp.New(mcp.Options{Transport: tr, ConnectionRef: "env:T", Project: "kraken", Getenv: func(string) string { return "x" }})
	if c, found := checkByName(dead.Doctor(ctx), "server"); !found || c.Status != memory.StatusError || !strings.Contains(c.Message, "unreachable") {
		t.Fatalf("%+v", c)
	}
}

func mustScoped(t *testing.T, r *rig, user string, projects ...string) string {
	t.Helper()
	p, _, err := server.NewScopedToken(context.Background(), r.st, user, projects, false, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return p
}
