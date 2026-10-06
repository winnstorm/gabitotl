package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/winnstorm/gabitotl/internal/memory"
	mcpclient "github.com/winnstorm/gabitotl/internal/memory/mcp"
	"github.com/winnstorm/gabitotl/internal/memory/store"
	"github.com/winnstorm/gabitotl/internal/memory/store/memstore"
)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

type env struct {
	t     *testing.T
	srv   *Server
	st    *memstore.Store
	ts    *httptest.Server
	clock *clock
	logs  *bytes.Buffer
	logMu sync.Mutex
}

type lockedBuf struct {
	mu sync.Mutex
	b  *bytes.Buffer
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func newEnv(t *testing.T, mut func(*Config)) *env {
	t.Helper()
	e := &env{t: t, clock: &clock{t: time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)}, logs: &bytes.Buffer{}}
	e.st = memstore.New()
	e.st.Now = e.clock.Now
	cfg := Config{Store: e.st, Now: e.clock.Now, UserRate: 1000, UserBurst: 1000, Logger: slog.New(slog.NewJSONHandler(&lockedBuf{b: e.logs}, &slog.HandlerOptions{Level: slog.LevelDebug}))}
	if mut != nil {
		mut(&cfg)
	}
	srv, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	e.srv = srv
	h, err := srv.Handler()
	if err != nil {
		t.Fatal(err)
	}
	e.ts = httptest.NewServer(h)
	t.Cleanup(e.ts.Close)
	return e
}

func (e *env) token(user string) string {
	e.t.Helper()
	plain, _, err := NewToken(context.Background(), e.st, user, e.clock.Now())
	if err != nil {
		e.t.Fatal(err)
	}
	return plain
}

type bearer struct{ token string }

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	if b.token != "" {
		r.Header.Set("Authorization", "Bearer "+b.token)
	}
	return http.DefaultTransport.RoundTrip(r)
}

func (e *env) client(token string) *mcp.ClientSession {
	e.t.Helper()
	c := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	cs, err := c.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint: e.ts.URL + EndpointPath, HTTPClient: &http.Client{Transport: bearer{token}}, DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		e.t.Fatalf("connect: %v", err)
	}
	e.t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// call invokes a tool. It returns the structured result and the error text
// when the tool reported one.
func call(t *testing.T, cs *mcp.ClientSession, tool string, args any) (out map[string]any, errText string) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		// Input-schema violations surface as protocol errors.
		return nil, err.Error()
	}
	if res.IsError {
		var b strings.Builder
		for _, c := range res.Content {
			if tc, ok := c.(*mcp.TextContent); ok {
				b.WriteString(tc.Text)
			}
		}
		return nil, b.String()
	}
	raw, _ := json.Marshal(res.StructuredContent)
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("result of %s is not an object: %s", tool, raw)
	}
	return out, ""
}

func entries(t *testing.T, out map[string]any) []map[string]any {
	t.Helper()
	var es []map[string]any
	raw, _ := json.Marshal(out["entries"])
	if err := json.Unmarshal(raw, &es); err != nil {
		t.Fatal(err)
	}
	return es
}

func save(t *testing.T, cs *mcp.ClientSession, title string, extra map[string]any) map[string]any {
	t.Helper()
	args := map[string]any{"type": "decision", "title": title, "content": "body of " + title, "project": "shop"}
	for k, v := range extra {
		args[k] = v
	}
	out, errText := call(t, cs, ToolSave, args)
	if errText != "" {
		t.Fatalf("save %q: %s", title, errText)
	}
	return out
}

func TestToolsMatchTheClientProviderMapping(t *testing.T) {
	e := newEnv(t, nil)
	cs := e.client(e.token("gabi"))
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]*mcp.Tool{}
	for _, tl := range res.Tools {
		got[tl.Name] = tl
		if tl.InputSchema == nil || tl.Description == "" {
			t.Errorf("%s needs a schema and a description", tl.Name)
		}
	}
	d := mcpclient.DefaultTools()
	for _, name := range []string{d.Search, d.Save, d.Update, d.SessionSummary, d.CurrentProject, d.Health, d.ActiveWork} {
		if got[name] == nil {
			t.Errorf("the client provider maps to %q, which the server does not offer", name)
		}
	}
	if len(got) != 7 {
		t.Errorf("tools: %d", len(got))
	}
	for _, ro := range []string{ToolSearch, ToolCurrentProject, ToolDoctor, ToolActiveWork} {
		if got[ro].Annotations == nil || !got[ro].Annotations.ReadOnlyHint {
			t.Errorf("%s must be annotated read-only", ro)
		}
	}
}

func TestAuthentication(t *testing.T) {
	e := newEnv(t, nil)
	good := e.token("gabi")
	post := func(authz string) *http.Response {
		req, _ := http.NewRequest(http.MethodPost, e.ts.URL+EndpointPath, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		if authz != "" {
			req.Header.Set("Authorization", authz)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { resp.Body.Close() })
		return resp
	}
	id, secret, _ := strings.Cut(strings.TrimPrefix(good, TokenPrefix), ".")
	for name, authz := range map[string]string{
		"no header":      "",
		"basic":          "Basic Z2FiaTpwdw==",
		"empty bearer":   "Bearer ",
		"garbage":        "Bearer nonsense",
		"wrong secret":   "Bearer " + TokenPrefix + id + "." + strings.Repeat("A", len(secret)),
		"unknown id":     "Bearer " + TokenPrefix + "0000000000000000." + secret,
		"id as password": "Bearer " + TokenPrefix + id,
	} {
		if resp := post(authz); resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s: want 401, got %d", name, resp.StatusCode)
		}
	}
	if resp := post("Bearer " + good); resp.StatusCode != http.StatusOK {
		t.Fatalf("a valid token must work, got %d", resp.StatusCode)
	}
	if resp := post("bearer " + good); resp.StatusCode != http.StatusOK {
		t.Fatalf("the scheme is case-insensitive, got %d", resp.StatusCode)
	}
	// Revocation is immediate.
	if err := e.st.RevokeToken(context.Background(), "gabi", id); err != nil {
		t.Fatal(err)
	}
	if resp := post("Bearer " + good); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("a revoked token must stop working at once, got %d", resp.StatusCode)
	}
	// The endpoint is the only protected thing; health is public and shallow.
	resp, err := http.Get(e.ts.URL + "/healthz")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("healthz: %v", err)
	}
	resp.Body.Close()
	if resp, _ := http.Get(e.ts.URL + "/other"); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown path: %d", resp.StatusCode)
	}
}

func TestSaveSearchRoundTripOverHTTP(t *testing.T) {
	e := newEnv(t, nil)
	cs := e.client(e.token("gabi"))
	out := save(t, cs, "Use JWT for sessions", map[string]any{"topic_key": "arch/auth", "task": "auth-login", "files": []string{"auth.go"}})
	if out["id"] == "" || out["created"] != true || out["revision"].(float64) != 1 {
		t.Fatalf("save: %+v", out)
	}
	res, errText := call(t, cs, ToolSearch, map[string]any{"text": "jwt", "project": "shop"})
	if errText != "" {
		t.Fatal(errText)
	}
	es := entries(t, res)
	if len(es) != 1 || es[0]["title"] != "Use JWT for sessions" || es[0]["actor"] != "gabi" || es[0]["project"] != "shop" || es[0]["task"] != "auth-login" {
		t.Fatalf("search: %+v", es)
	}
	// Another project does not see it.
	res, _ = call(t, cs, ToolSearch, map[string]any{"text": "jwt", "project": "other"})
	if len(entries(t, res)) != 0 {
		t.Fatal("projects must be isolated")
	}
	// Re-saving the topic key revises it.
	again := save(t, cs, "Use JWT, rotated", map[string]any{"topic_key": "arch/auth"})
	if again["created"] != false || again["revision"].(float64) != 2 || again["id"] != out["id"] {
		t.Fatalf("upsert: %+v", again)
	}
}

func TestActorComesFromTheTokenAndCannotBeSpoofed(t *testing.T) {
	e := newEnv(t, nil)
	gabi, gonza := e.client(e.token("gabi")), e.client(e.token("gonza"))
	out := save(t, gabi, "No actor given", nil)
	rec, _ := e.st.Get(context.Background(), "shop", memory.ID(out["id"].(string)))
	if rec.Actor != "gabi" || rec.CreatedBy != "gabi" {
		t.Fatalf("the actor must be derived from the token: %+v", rec.Entry)
	}
	save(t, gabi, "Own actor named", map[string]any{"actor": "gabi"})
	for _, spoof := range []string{"gonza", "legacy", "admin", " gonza "} {
		_, errText := call(t, gabi, ToolSave, map[string]any{"type": "decision", "title": "forged", "content": "x", "project": "shop", "actor": spoof})
		if !strings.Contains(errText, "actor mismatch") {
			t.Errorf("spoofing %q must be rejected, got %q", spoof, errText)
		}
	}
	if _, errText := call(t, gabi, ToolSessionSummary, map[string]any{"project": "shop", "actor": "gonza", "goal": "g"}); !strings.Contains(errText, "actor mismatch") {
		t.Errorf("summary spoof: %q", errText)
	}
	rs, _ := e.st.Search(context.Background(), memory.Query{Text: "forged", Project: "shop"})
	if len(rs) != 0 {
		t.Fatal("a rejected spoof must not store anything")
	}
	// An update is attributed to the authenticated user, whoever wrote the entry.
	title := "Edited by gonza"
	if _, errText := call(t, gonza, ToolUpdate, map[string]any{"id": out["id"], "project": "shop", "patch": map[string]any{"title": title}}); errText != "" {
		t.Fatal(errText)
	}
	rec, _ = e.st.Get(context.Background(), "shop", memory.ID(out["id"].(string)))
	if rec.Actor != "gonza" || rec.CreatedBy != "gabi" || rec.Title != title {
		t.Fatalf("update attribution: %+v", rec)
	}
	if !strings.Contains(e.logs.String(), "actor spoof rejected") {
		t.Fatal("a spoof attempt must be logged")
	}
	// Tokens can never be created for the reserved or malformed users.
	for _, u := range []string{"legacy", "", "Gabi Pérez", "../x"} {
		if _, _, err := NewToken(context.Background(), e.st, u, time.Now()); err == nil {
			t.Errorf("token for %q must be refused", u)
		}
	}
}

func TestSearchIsCappedAndScoped(t *testing.T) {
	e := newEnv(t, nil)
	cs := e.client(e.token("gabi"))
	for i := range 30 {
		save(t, cs, fmt.Sprintf("Convention %d", i), map[string]any{"type": "convention"})
	}
	count := func(args map[string]any) int {
		res, errText := call(t, cs, ToolSearch, args)
		if errText != "" {
			t.Fatalf("%v: %s", args, errText)
		}
		return len(entries(t, res))
	}
	if n := count(map[string]any{"text": "convention", "project": "shop"}); n != memory.DefaultPerQuery {
		t.Fatalf("default cap: %d", n)
	}
	if n := count(map[string]any{"text": "convention", "project": "shop", "limit": 1000}); n != MaxSearchLimit {
		t.Fatalf("the maximum cap must hold: %d", n)
	}
	if n := count(map[string]any{"text": "convention", "project": "shop", "limit": 3}); n != 3 {
		t.Fatalf("explicit limit: %d", n)
	}
	if n := count(map[string]any{"text": "convention", "project": "shop", "limit": -5}); n != memory.DefaultPerQuery {
		t.Fatalf("negative limit: %d", n)
	}
	if _, errText := call(t, cs, ToolSearch, map[string]any{"text": "convention"}); !strings.Contains(errText, "project is required") {
		t.Fatalf("a search must name a project: %q", errText)
	}
	if _, errText := call(t, cs, ToolSearch, map[string]any{"text": "x", "project": "shop", "type": "nonsense"}); !strings.Contains(errText, "invalid entry type") {
		t.Fatalf("type validation: %q", errText)
	}
	if _, errText := call(t, cs, ToolSearch, map[string]any{"text": strings.Repeat("a", MaxQueryBytes+1), "project": "shop"}); !strings.Contains(errText, "longer than") {
		t.Fatalf("query length: %q", errText)
	}
	// Relevance: a title match outranks a body mention.
	save(t, cs, "Logging format", map[string]any{"content": "mentions rotation once"})
	save(t, cs, "Rotation policy", map[string]any{"content": "keys rotate monthly"})
	res, errText := call(t, cs, ToolSearch, map[string]any{"text": "rotation", "project": "shop"})
	if errText != "" {
		t.Fatal(errText)
	}
	if es := entries(t, res); len(es) != 2 || es[0]["title"] != "Rotation policy" {
		t.Fatalf("relevance order: %+v", es)
	}
}

func TestDefaultProjectFillsAMissingOne(t *testing.T) {
	e := newEnv(t, func(c *Config) { c.DefaultProject = "shop" })
	cs := e.client(e.token("gabi"))
	out, errText := call(t, cs, ToolSave, map[string]any{"type": "decision", "title": "No project sent", "content": "x"})
	if errText != "" {
		t.Fatal(errText)
	}
	rec, _ := e.st.Get(context.Background(), "shop", memory.ID(out["id"].(string)))
	if rec.Project != "shop" {
		t.Fatalf("%+v", rec)
	}
	cur, _ := call(t, cs, ToolCurrentProject, map[string]any{})
	if cur["project"] != "shop" || cur["actor"] != "gabi" {
		t.Fatalf("current project: %+v", cur)
	}
}

func TestInputLimits(t *testing.T) {
	e := newEnv(t, nil)
	cs := e.client(e.token("gabi"))
	base := func(k string, v any) map[string]any {
		a := map[string]any{"type": "decision", "title": "t", "content": "c", "project": "shop"}
		a[k] = v
		return a
	}
	for name, args := range map[string]map[string]any{
		"title":      base("title", strings.Repeat("t", MaxTitleBytes+1)),
		"content":    base("content", strings.Repeat("c", MaxContentBytes+1)),
		"topic_key":  base("topic_key", strings.Repeat("k", MaxTopicBytes+1)),
		"task":       base("task", strings.Repeat("k", MaxFieldBytes+1)),
		"ojo":        base("ojo", strings.Repeat("k", MaxFieldBytes+1)),
		"project":    base("project", strings.Repeat("p", MaxProjectBytes+1)),
		"many files": base("files", make([]string, MaxFiles+1)),
		"long file":  base("files", []string{strings.Repeat("f", MaxFileBytes+1)}),
		"blank":      base("title", "   "),
		"bad type":   base("type", "nonsense"),
		"NUL":        base("content", "a\x00b"),
	} {
		if _, errText := call(t, cs, ToolSave, args); errText == "" {
			t.Errorf("%s must be rejected", name)
		}
	}
	// Exactly at the limit is fine.
	if _, errText := call(t, cs, ToolSave, base("content", strings.Repeat("c", MaxContentBytes))); errText != "" {
		t.Fatalf("content at the limit: %s", errText)
	}
	// Missing required fields are caught by the schema.
	if _, errText := call(t, cs, ToolSave, map[string]any{"project": "shop"}); errText == "" {
		t.Error("missing title/content/type must be rejected")
	}
}

func TestRequestBodyLimit(t *testing.T) {
	e := newEnv(t, func(c *Config) { c.MaxBodyBytes = 2048 })
	tok := e.token("gabi")
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"memory_save","arguments":{"type":"decision","title":"t","project":"p","content":"` + strings.Repeat("x", 8192) + `"}}}`
	req, _ := http.NewRequest(http.MethodPost, e.ts.URL+EndpointPath, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("oversized body: want 413, got %d %s", resp.StatusCode, b)
	}
}

func TestUpdateOptimisticLockAndNotFound(t *testing.T) {
	e := newEnv(t, nil)
	cs := e.client(e.token("gabi"))
	id := save(t, cs, "Original", nil)["id"]
	upd := func(extra map[string]any) (map[string]any, string) {
		a := map[string]any{"id": id, "project": "shop", "patch": map[string]any{"content": "new body"}}
		for k, v := range extra {
			a[k] = v
		}
		return call(t, cs, ToolUpdate, a)
	}
	out, errText := upd(map[string]any{"expected_revision": 1})
	if errText != "" || out["revision"].(float64) != 2 {
		t.Fatalf("%+v %s", out, errText)
	}
	if _, errText := upd(map[string]any{"expected_revision": 1}); !strings.Contains(errText, "revision conflict") {
		t.Fatalf("stale revision: %q", errText)
	}
	if _, errText := call(t, cs, ToolUpdate, map[string]any{"id": "00000000-0000-4000-8000-000000000000", "project": "shop", "patch": map[string]any{}}); !strings.Contains(errText, "not found") {
		t.Fatalf("unknown id: %q", errText)
	}
	if _, errText := call(t, cs, ToolUpdate, map[string]any{"id": id, "project": "other", "patch": map[string]any{"title": "x"}}); !strings.Contains(errText, "not found") {
		t.Fatalf("another project's entry must look absent: %q", errText)
	}
	if _, errText := call(t, cs, ToolUpdate, map[string]any{"id": id, "project": "shop", "patch": map[string]any{"title": " "}}); errText == "" {
		t.Fatal("blanking the title must be rejected")
	}
}

func TestSessionSummaryIsStoredAndSearchable(t *testing.T) {
	e := newEnv(t, nil)
	cs := e.client(e.token("gabi"))
	for _, acc := range []string{"first pass", "second pass"} {
		if out, errText := call(t, cs, ToolSessionSummary, map[string]any{"project": "shop", "task": "auth", "goal": "login", "accomplished": acc, "next_steps": "logout"}); errText != "" || out["ok"] != true {
			t.Fatalf("%+v %s", out, errText)
		}
	}
	sums, _ := e.st.Summaries(context.Background(), "shop", 10)
	if len(sums) != 2 || sums[0].Actor != "gabi" || sums[0].Accomplished != "second pass" {
		t.Fatalf("summaries: %+v", sums)
	}
	res, _ := call(t, cs, ToolSearch, map[string]any{"text": "session", "project": "shop"})
	es := entries(t, res)
	if len(es) != 1 || es[0]["topic_key"] != "session/summary/gabi" || es[0]["revision"].(float64) != 2 {
		t.Fatalf("the latest summary must be one revised, searchable entry: %+v", es)
	}
}

func TestActiveWork(t *testing.T) {
	e := newEnv(t, nil)
	gabi, gonza := e.client(e.token("gabi")), e.client(e.token("gonza"))
	save(t, gabi, "Login form", map[string]any{"type": "progress", "task": "auth-login"})
	e.clock.Advance(2 * time.Hour)
	save(t, gonza, "Validation rules", map[string]any{"type": "progress", "task": "user-validation"})
	list := func(args map[string]any) []map[string]any {
		args["project"] = "shop"
		res, errText := call(t, gabi, ToolActiveWork, args)
		if errText != "" {
			t.Fatal(errText)
		}
		return entries(t, res)
	}
	if es := list(map[string]any{}); len(es) != 2 || es[0]["actor"] != "gonza" {
		t.Fatalf("newest first: %+v", es)
	}
	if es := list(map[string]any{"actor": "gabi"}); len(es) != 1 || es[0]["task"] != "auth-login" {
		t.Fatalf("actor filter: %+v", es)
	}
	if es := list(map[string]any{"task": "user-validation"}); len(es) != 1 {
		t.Fatalf("task filter: %+v", es)
	}
	if es := list(map[string]any{"since_minutes": 60}); len(es) != 1 || es[0]["actor"] != "gonza" {
		t.Fatalf("since filter: %+v", es)
	}
}

func TestDoctorAndCurrentProject(t *testing.T) {
	e := newEnv(t, nil)
	cs := e.client(e.token("gabi"))
	out, errText := call(t, cs, ToolDoctor, map[string]any{})
	if errText != "" || out["ok"] != true {
		t.Fatalf("%+v %s", out, errText)
	}
	raw, _ := json.Marshal(out)
	if strings.Contains(string(raw), "://") {
		t.Fatalf("doctor leaks connection details: %s", raw)
	}
	if cur, _ := call(t, cs, ToolCurrentProject, map[string]any{}); cur["actor"] != "gabi" {
		t.Fatalf("%+v", cur)
	}
}

func TestRateLimitPerUser(t *testing.T) {
	e := newEnv(t, func(c *Config) { c.UserRate, c.UserBurst = 0.001, 3 })
	tok, other := e.token("gabi"), e.token("gonza")
	post := func(tok string) *http.Response {
		req, _ := http.NewRequest(http.MethodPost, e.ts.URL+EndpointPath, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp
	}
	for i := range 3 {
		if resp := post(tok); resp.StatusCode != http.StatusOK {
			t.Fatalf("request %d: %d", i, resp.StatusCode)
		}
	}
	resp := post(tok)
	if resp.StatusCode != http.StatusTooManyRequests || resp.Header.Get("Retry-After") == "" {
		t.Fatalf("burst exhausted must give 429 with Retry-After, got %d", resp.StatusCode)
	}
	if resp := post(other); resp.StatusCode != http.StatusOK {
		t.Fatalf("limits are per user: %d", resp.StatusCode)
	}
}

func TestFailedAuthenticationThrottlesTheClientAddress(t *testing.T) {
	e := newEnv(t, func(c *Config) { c.AuthFailPerMinute = 3 })
	good := e.token("gabi")
	post := func(tok string) int {
		req, _ := http.NewRequest(http.MethodPost, e.ts.URL+EndpointPath, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	for i := range 3 {
		if c := post("bad"); c != http.StatusUnauthorized {
			t.Fatalf("attempt %d: %d", i, c)
		}
	}
	if c := post("bad"); c != http.StatusTooManyRequests {
		t.Fatalf("guessing must be throttled: %d", c)
	}
	if c := post(good); c != http.StatusTooManyRequests {
		t.Fatalf("a throttled address is blocked until the bucket refills: %d", c)
	}
	e.clock.Advance(2 * time.Minute)
	if c := post(good); c != http.StatusOK {
		t.Fatalf("after the window the address works again: %d", c)
	}
}

func TestStdioStyleServerUsesTheFixedActor(t *testing.T) {
	st := memstore.New()
	srv, err := New(Config{Store: st, FixedActor: "gabi", DefaultProject: "shop"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.Handler(); err == nil {
		t.Fatal("HTTP must refuse a fixed actor: the token decides there")
	}
	ctx := context.Background()
	t1, t2 := mcp.NewInMemoryTransports()
	ss, err := srv.MCP().Connect(ctx, t1, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "0"}, nil).Connect(ctx, t2, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	out := save(t, cs, "Local entry", map[string]any{"project": "shop"})
	rec, _ := st.Get(ctx, "shop", memory.ID(out["id"].(string)))
	if rec.Actor != "gabi" {
		t.Fatalf("%+v", rec)
	}
	if _, errText := call(t, cs, ToolSave, map[string]any{"type": "decision", "title": "t", "content": "c", "actor": "gonza"}); !strings.Contains(errText, "actor mismatch") {
		t.Fatalf("stdio spoof: %q", errText)
	}
	// Without any actor (misconfigured stdio) nothing can be written.
	bare, _ := New(Config{Store: memstore.New(), DefaultProject: "shop"})
	u1, u2 := mcp.NewInMemoryTransports()
	bs, _ := bare.MCP().Connect(ctx, u1, nil)
	defer bs.Close()
	bc, _ := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "0"}, nil).Connect(ctx, u2, nil)
	defer bc.Close()
	if _, errText := call(t, bc, ToolSave, map[string]any{"type": "decision", "title": "t", "content": "c"}); !strings.Contains(errText, "unauthenticated") {
		t.Fatalf("no actor: %q", errText)
	}
	if err := bare.ServeStdio(ctx); err == nil {
		t.Fatal("ServeStdio without an actor must refuse")
	}
	if _, err := New(Config{Store: st, FixedActor: "legacy"}); err == nil {
		t.Fatal("legacy cannot be an authenticated actor")
	}
}

func TestLogsNeverContainContentOrTokens(t *testing.T) {
	e := newEnv(t, nil)
	tok := e.token("gabi")
	_, secret, _ := strings.Cut(tok, ".")
	cs := e.client(tok)
	save(t, cs, "Sensitive title ZZTITLE", map[string]any{"content": "very secret ZZCONTENT body"})
	call(t, cs, ToolSearch, map[string]any{"text": "ZZQUERY", "project": "shop"})
	for _, bad := range []string{"Bearer wrong-token-ZZBAD"} {
		req, _ := http.NewRequest(http.MethodPost, e.ts.URL+EndpointPath, strings.NewReader("{}"))
		req.Header.Set("Authorization", bad)
		resp, _ := http.DefaultClient.Do(req)
		resp.Body.Close()
	}
	logs := e.logs.String()
	for _, leak := range []string{"ZZTITLE", "ZZCONTENT", "ZZQUERY", "ZZBAD", secret, tok, "Authorization"} {
		if strings.Contains(logs, leak) {
			t.Errorf("logs contain %q:\n%s", leak, logs)
		}
	}
	for _, want := range []string{`"tool":"memory_save"`, `"actor":"gabi"`, `"msg":"http request"`, `"status":401`} {
		if !strings.Contains(logs, want) {
			t.Errorf("logs lack %s", want)
		}
	}
}

func TestConcurrentClientsAreAttributedCorrectly(t *testing.T) {
	e := newEnv(t, nil)
	users := []string{"ana", "ben", "cat", "dan"}
	var wg sync.WaitGroup
	for _, u := range users {
		cs := e.client(e.token(u))
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 5 {
				if _, errText := call(t, cs, ToolSave, map[string]any{"type": "decision", "title": fmt.Sprintf("%s entry %d", u, i), "content": "x", "project": "shop"}); errText != "" {
					t.Errorf("%s: %s", u, errText)
				}
			}
		}()
	}
	wg.Wait()
	rs, _ := e.st.Active(context.Background(), store.ActiveQuery{Project: "shop", Limit: 50})
	if len(rs) != 20 {
		t.Fatalf("entries: %d", len(rs))
	}
	for _, r := range rs {
		if !strings.HasPrefix(r.Title, r.Actor+" ") {
			t.Fatalf("entry %q attributed to %q", r.Title, r.Actor)
		}
	}
}

func TestBindCheck(t *testing.T) {
	cases := []struct {
		hc HTTPConfig
		ok bool
	}{
		{HTTPConfig{Addr: "127.0.0.1:8080"}, true},
		{HTTPConfig{Addr: "localhost:8080"}, true},
		{HTTPConfig{Addr: "[::1]:8080"}, true},
		{HTTPConfig{Addr: "0.0.0.0:8080"}, false},
		{HTTPConfig{Addr: ":8080"}, false},
		{HTTPConfig{Addr: "10.0.0.5:8080"}, false},
		{HTTPConfig{Addr: "0.0.0.0:8080", AllowInsecureRemote: true}, true},
		{HTTPConfig{Addr: "0.0.0.0:8443", TLSCert: "c.pem", TLSKey: "k.pem"}, true},
		{HTTPConfig{Addr: "127.0.0.1:8443", TLSCert: "c.pem"}, false},
		{HTTPConfig{Addr: "nonsense"}, false},
	}
	for _, c := range cases {
		if err := c.hc.CheckBind(); (err == nil) != c.ok {
			t.Errorf("%+v: ok=%v err=%v", c.hc, c.ok, err)
		}
	}
}

func TestServeAndShutdown(t *testing.T) {
	st := memstore.New()
	srv, _ := New(Config{Store: st})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.ListenAndServe(ctx, HTTPConfig{Addr: "127.0.0.1:0"}) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil && err != http.ErrServerClosed {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown must be prompt")
	}
	if err := srv.ListenAndServe(context.Background(), HTTPConfig{Addr: "0.0.0.0:0"}); err == nil {
		t.Fatal("plain HTTP on all interfaces must be refused")
	}
}

func TestTokenStorageAndVerification(t *testing.T) {
	st := memstore.New()
	ctx := context.Background()
	plain, rec, err := NewToken(ctx, st, "gabi", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(plain, TokenPrefix) || len(plain) < 60 {
		t.Fatalf("token shape: %q", plain)
	}
	_, secret, _ := strings.Cut(plain, ".")
	stored, _ := st.TokenByID(ctx, rec.ID)
	if len(stored.Hash) != 32 || bytes.Contains(stored.Hash, []byte(secret)) || stored.User != "gabi" {
		t.Fatalf("only a hash of the secret may be stored: %+v", stored)
	}
	if u, err := VerifyToken(ctx, st, plain); err != nil || u != "gabi" {
		t.Fatalf("verify: %q %v", u, err)
	}
	other, _, _ := NewToken(ctx, st, "gabi", time.Now())
	if plain == other {
		t.Fatal("tokens must be unique")
	}
	for _, bad := range []string{"", "gbt_", plain + "x", plain[:len(plain)-1], strings.Replace(plain, ".", ":", 1), "Bearer " + plain} {
		if _, err := VerifyToken(ctx, st, bad); err != ErrBadToken {
			t.Errorf("%q: %v", bad, err)
		}
	}
	list, _ := st.ListTokens(ctx, "gabi")
	if len(list) != 2 || len(list[0].Hash) != 0 {
		t.Fatalf("list: %+v", list)
	}
}

// scopedToken creates a token limited to projects.
func (e *env) scopedToken(user string, projects ...string) string {
	e.t.Helper()
	plain, _, err := NewScopedToken(context.Background(), e.st, user, projects, false, e.clock.Now())
	if err != nil {
		e.t.Fatal(err)
	}
	return plain
}

func TestScopedTokenIsConfinedToItsProjects(t *testing.T) {
	e := newEnv(t, func(c *Config) { c.DefaultProject = "shop" })
	admin := e.client(e.token("root")) // all-project token (pre-scope behavior)
	other := save(t, admin, "Secret of kraken", map[string]any{"project": "kraken"})
	save(t, admin, "Visible in shop", nil)

	cs := e.client(e.scopedToken("gonza", "shop"))
	if _, msg := call(t, cs, ToolSave, map[string]any{"type": "decision", "title": "ok", "content": "c", "project": "shop"}); msg != "" {
		t.Fatalf("in-scope write: %s", msg)
	}
	if out, msg := call(t, cs, ToolSearch, map[string]any{"text": "visible", "project": "shop"}); msg != "" || len(entries(t, out)) != 1 {
		t.Fatalf("in-scope read: %v %s", out, msg)
	}
	denied := func(what string, tool string, args map[string]any) {
		t.Helper()
		out, msg := call(t, cs, tool, args)
		if !strings.Contains(msg, "not allowed to use project") || out != nil {
			t.Errorf("%s must be rejected as out of scope: %v %q", what, out, msg)
		}
	}
	denied("search", ToolSearch, map[string]any{"text": "secret", "project": "kraken"})
	denied("save", ToolSave, map[string]any{"type": "decision", "title": "x", "content": "c", "project": "kraken"})
	denied("update", ToolUpdate, map[string]any{"id": other["id"], "project": "kraken", "patch": map[string]any{"title": "hijack"}})
	denied("summary", ToolSessionSummary, map[string]any{"project": "kraken", "goal": "g"})
	denied("active work", ToolActiveWork, map[string]any{"project": "kraken"})
	// An update that names no project would match any project in the store.
	if _, msg := call(t, cs, ToolUpdate, map[string]any{"id": other["id"], "patch": map[string]any{"title": "hijack"}}); msg == "" {
		t.Error("an update without a project must be refused for a scoped token... unless the default project is in scope and the entry is not found there")
	}
	rs, _ := e.st.Search(context.Background(), memory.Query{Text: "secret", Project: "kraken"})
	if len(rs) != 1 || rs[0].Title != "Secret of kraken" || rs[0].Actor != "root" {
		t.Fatalf("the out-of-scope entry must be untouched: %+v", rs)
	}
}

func TestScopedTokenWithoutADefaultProjectMustNameOneForUpdate(t *testing.T) {
	e := newEnv(t, nil) // no default project
	admin := e.client(e.token("root"))
	id := save(t, admin, "Entry", map[string]any{"project": "kraken"})["id"]
	cs := e.client(e.scopedToken("gonza", "shop"))
	_, msg := call(t, cs, ToolUpdate, map[string]any{"id": id, "patch": map[string]any{"title": "x"}})
	if !strings.Contains(msg, "project is required") {
		t.Fatalf("got %q", msg)
	}
}

func TestScopedTokenHidesWhatItCannotUse(t *testing.T) {
	e := newEnv(t, func(c *Config) { c.DefaultProject = "kraken" })
	save(t, e.client(e.token("root")), "x", map[string]any{"project": "kraken"})
	cs := e.client(e.scopedToken("gonza", "shop"))
	out, msg := call(t, cs, ToolCurrentProject, map[string]any{})
	if msg != "" || out["project"] != "" || out["actor"] != "gonza" {
		t.Fatalf("current project must not reveal an out-of-scope default: %v %q", out, msg)
	}
	d, msg := call(t, cs, ToolDoctor, map[string]any{})
	if msg != "" {
		t.Fatal(msg)
	}
	if strings.Contains(fmt.Sprint(d), "entries") {
		t.Fatalf("doctor must not leak aggregate counts to a scoped token: %v", d)
	}
}

func TestNewScopedTokenValidation(t *testing.T) {
	e := newEnv(t, nil)
	ctx := context.Background()
	for name, c := range map[string]struct {
		projects []string
		all      bool
	}{
		"neither":    {nil, false},
		"both":       {[]string{"a"}, true},
		"comma":      {[]string{"a,b"}, false},
		"blank":      {[]string{" "}, false},
		"whitespace": {[]string{"a b"}, false},
	} {
		if _, _, err := NewScopedToken(ctx, e.st, "gabi", c.projects, c.all, time.Now()); err == nil {
			t.Errorf("%s must be refused", name)
		}
	}
	_, rec, err := NewScopedToken(ctx, e.st, "gabi", []string{"a", "b", "a"}, false, time.Now())
	if err != nil || len(rec.Projects) != 2 || rec.AllProjects {
		t.Fatalf("dedupe: %+v %v", rec, err)
	}
}

func TestTrustedProxyHeaderIsOptIn(t *testing.T) {
	post := func(e *env, xff string) int {
		req, _ := http.NewRequest(http.MethodPost, e.ts.URL+EndpointPath, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
		req.Header.Set("Authorization", "Bearer bad")
		req.Header.Set("X-Forwarded-For", xff)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	// Off by default: the header is ignored, so rotating it does not escape the throttle.
	off := newEnv(t, func(c *Config) { c.AuthFailPerMinute = 2 })
	post(off, "1.1.1.1")
	post(off, "2.2.2.2")
	if c := post(off, "3.3.3.3"); c != http.StatusTooManyRequests {
		t.Fatalf("without the opt-in the peer address is throttled whatever the header says: %d", c)
	}
	// On: each proxied client address has its own bucket; the last entry wins.
	on := newEnv(t, func(c *Config) { c.AuthFailPerMinute = 2; c.TrustedProxyHeader = "X-Forwarded-For" })
	post(on, "spoofed, 10.0.0.1")
	post(on, "spoofed2, 10.0.0.1")
	if c := post(on, "spoofed3, 10.0.0.1"); c != http.StatusTooManyRequests {
		t.Fatalf("the same proxied client must be throttled, the forged leading entries ignored: %d", c)
	}
	if c := post(on, "10.0.0.2"); c != http.StatusUnauthorized {
		t.Fatalf("another client behind the proxy is not throttled: %d", c)
	}
	if c := post(on, "not-an-ip"); c != http.StatusUnauthorized && c != http.StatusTooManyRequests {
		t.Fatalf("garbage falls back to the peer address: %d", c)
	}
	if _, err := New(Config{Store: memstore.New(), TrustedProxyHeader: "bad header"}); err == nil {
		t.Fatal("an invalid header name must be refused")
	}
}

func TestServerStripsAttributionBeforeStoring(t *testing.T) {
	e := newEnv(t, nil)
	cs := e.client(e.token("gabi"))
	llm := "Cla" + "ude"
	body := "We picked JWT.\n\nCo-Authored-By: " + llm + " <noreply@" + "anthropic" + ".com>"
	out := save(t, cs, "Use JWT", map[string]any{"content": body})
	rec, _ := e.st.Get(context.Background(), "shop", memory.ID(out["id"].(string)))
	if rec.Content != "We picked JWT." {
		t.Fatalf("the server must strip attribution before storing: %q", rec.Content)
	}
	_, errText := call(t, cs, ToolSave, map[string]any{"type": "decision", "title": "t", "project": "shop",
		"content": "Fixed, generated " + "with " + llm})
	if !strings.Contains(errText, "authorship") {
		t.Fatalf("a line it cannot strip is refused: %q", errText)
	}
}
