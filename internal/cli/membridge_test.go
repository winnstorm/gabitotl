package cli

import (
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/memory"
	"github.com/winnstorm/gabitotl/internal/memory/server"
)

// startBridge runs `gabitotl memory mcp args...` in-process and returns an MCP
// client session wired to its stdin/stdout, plus the stderr buffer.
func startBridge(t *testing.T, f *sfix, args ...string) (*mcp.ClientSession, *safeBuf, func() int) {
	t.Helper()
	return startBridgeAt(t, f, f.t.TempDir(), args...)
}

// startBridgeAt is startBridge with the working directory of the process set to cwd.
func startBridgeAt(t *testing.T, f *sfix, cwd string, args ...string) (*mcp.ClientSession, *safeBuf, func() int) {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	signalCtxFn = func() (context.Context, context.CancelFunc) { return ctx, func() {} }
	var errb safeBuf
	done := make(chan int, 1)
	go func() {
		done <- Run(append([]string{"memory", "mcp"}, args...), Env{Stdin: inR, Stdout: outW, Stderr: &errb,
			Getwd: func() (string, error) { return cwd, nil }})
		outW.Close()
	}()
	c := mcp.NewClient(&mcp.Implementation{Name: "runtime", Version: "0"}, nil)
	cs, err := c.Connect(ctx, &mcp.IOTransport{Reader: outR, Writer: inW}, nil)
	if err != nil {
		t.Fatalf("connect to the bridge: %v (stderr: %s)", err, errb.String())
	}
	stop := func() int {
		_ = cs.Close()
		cancel()
		select {
		case c := <-done:
			return c
		case <-time.After(5 * time.Second):
			t.Fatal("the bridge did not stop")
			return -1
		}
	}
	return cs, &errb, stop
}

func TestBridgeForwardsToolsOverStdio(t *testing.T) {
	f := newSfix(t)
	srv, err := server.New(server.Config{Store: f.st, UserRate: 1000, UserBurst: 1000})
	if err != nil {
		t.Fatal(err)
	}
	h, _ := srv.Handler()
	ts := httptest.NewServer(h)
	defer ts.Close()
	tok, _, err := server.NewScopedToken(context.Background(), f.st, "gabi", []string{"shop"}, false, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	f.envv["GBT_BRIDGE_TOKEN"] = tok

	cs, errb, stop := startBridge(t, f, "--url", ts.URL+"/mcp", "--token-env", "GBT_BRIDGE_TOKEN", "--project", "shop")

	tools, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tl := range tools.Tools {
		names = append(names, tl.Name)
		if tl.InputSchema == nil {
			t.Errorf("%s must carry the server's input schema", tl.Name)
		}
	}
	for _, want := range []string{"memory_search", "memory_save", "memory_update", "memory_session_summary", "memory_current_project", "memory_doctor", "memory_active_work"} {
		found := false
		for _, n := range names {
			found = found || n == want
		}
		if !found {
			t.Errorf("tool %s missing from the bridge: %v", want, names)
		}
	}

	// The project is filled from --project; the actor comes from the token.
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "memory_save",
		Arguments: map[string]any{"type": "decision", "title": "Via bridge", "content": "forwarded to the server"}})
	if err != nil || res.IsError {
		t.Fatalf("save: %v %+v", err, res)
	}
	res, err = cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "memory_search", Arguments: map[string]any{"text": "bridge"}})
	if err != nil || res.IsError || !strings.Contains(toJSON(res.StructuredContent), `"actor":"gabi"`) || !strings.Contains(toJSON(res.StructuredContent), `"project":"shop"`) {
		t.Fatalf("search: %v %+v", err, res)
	}
	// Server-side rules still apply through the bridge: scope and actor.
	res, _ = cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "memory_search", Arguments: map[string]any{"text": "x", "project": "kraken"}})
	if res == nil || !res.IsError || !strings.Contains(textOf(res), "not allowed to use project") {
		t.Fatalf("out-of-scope search must be rejected: %+v", res)
	}
	res, _ = cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "memory_save",
		Arguments: map[string]any{"type": "decision", "title": "f", "content": "c", "actor": "gonza"}})
	if res == nil || !res.IsError || !strings.Contains(textOf(res), "actor mismatch") {
		t.Fatalf("a forged actor must be rejected: %+v", res)
	}
	if code := stop(); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if strings.Contains(errb.String(), tok) {
		t.Fatal("the token must never reach the logs")
	}
}

func TestBridgeSurfacesAuthAndUnreachableErrors(t *testing.T) {
	f := newSfix(t)
	f.envv["GBT_BRIDGE_TOKEN"] = "gbt_0000000000000000.wrongwrongwrongwrongwrong"
	srv, _ := server.New(server.Config{Store: f.st})
	h, _ := srv.Handler()
	ts := httptest.NewServer(h)
	cs, _, stop := startBridge(t, f, "--url", ts.URL+"/mcp", "--token-env", "GBT_BRIDGE_TOKEN", "--project", "shop")
	res, _ := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "memory_doctor", Arguments: map[string]any{}})
	if res == nil || !res.IsError || !strings.Contains(textOf(res), "authentication failed") {
		t.Fatalf("a bad token must be reported clearly: %+v", res)
	}
	ts.Close()
	res, _ = cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "memory_doctor", Arguments: map[string]any{}})
	if res == nil || !res.IsError || !strings.Contains(textOf(res), "unreachable") {
		t.Fatalf("a dead server must be reported clearly: %+v", res)
	}
	stop()
}

func TestBridgeArgumentChecks(t *testing.T) {
	f := newSfix(t)
	for name, args := range map[string][]string{
		"literal token":   {"--url", "https://x/mcp", "--token-env", "gbt_abc.def"},
		"plain remote":    {"--url", "http://memoria.example.com/mcp", "--token-env", "T"},
		"no token source": {"--url", "https://x/mcp"},
	} {
		if r := f.run("", append([]string{"memory", "mcp"}, args...)...); r.code == 0 || r.out != "" {
			t.Errorf("%s must fail without writing to stdout: %+v", name, r)
		}
	}
}

func toJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func textOf(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

// recProvider records what an Engram-backed project receives through the bridge.
type recProvider struct {
	stubProvider
	saved []memory.Entry
}

func (r *recProvider) Save(_ context.Context, e memory.Entry) (memory.ID, error) {
	r.saved = append(r.saved, e)
	return "42", nil
}

// teamProject turns an initialized project into a memory-mcp client of url.
func teamProject(t *testing.T, root, url string) {
	t.Helper()
	l := gabitotl.NewLayout(root)
	cfg, err := gabitotl.LoadMemory(l)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Mode, cfg.Provider, cfg.URL, cfg.Token, cfg.Role = gabitotl.ModeTeam, gabitotl.MemoryMemoryMCP, url, "env:GBT_BRIDGE_TOKEN", gabitotl.RoleClient
	if err := gabitotl.SaveMemory(l, cfg); err != nil {
		t.Fatal(err)
	}
}

func callTool(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return res
}

// One registration, two projects with different providers: each bridge process
// routes by the memory.md of the project it runs in.
func TestBridgeRoutesByProjectProvider(t *testing.T) {
	f := newSfix(t)
	srv, _ := server.New(server.Config{Store: f.st, UserRate: 1000, UserBurst: 1000})
	h, _ := srv.Handler()
	ts := httptest.NewServer(h)
	defer ts.Close()
	tok, _, err := server.NewScopedToken(context.Background(), f.st, "gabi", []string{"shop"}, false, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	f.envv["GBT_BRIDGE_TOKEN"] = tok

	solo := initProject(t) // engram by default
	team := initProject(t)
	teamProject(t, team, ts.URL+"/mcp")
	rec := &recProvider{}
	withProvider(t, rec)

	save := map[string]any{"type": "decision", "title": "Routed", "content": "per project"}

	cs, errb, stop := startBridgeAt(t, f, team)
	if res := callTool(t, cs, "memory_save", save); res.IsError {
		t.Fatalf("team save: %s", textOf(res))
	}
	if len(rec.saved) != 0 {
		t.Fatalf("a team project must not reach Engram: %+v", rec.saved)
	}
	res := callTool(t, cs, "memory_search", map[string]any{"text": "routed"})
	if res.IsError || !strings.Contains(toJSON(res.StructuredContent), `"actor":"gabi"`) {
		t.Fatalf("team search: %+v", res)
	}
	if code := stop(); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}

	inTeam := func() int {
		recs, _ := f.st.Search(context.Background(), memory.Query{Text: "routed", Project: "shop"})
		return len(recs)
	}
	if inTeam() != 1 {
		t.Fatalf("the team save must reach the server once, got %d", inTeam())
	}

	cs, errb, stop = startBridgeAt(t, f, solo)
	if res := callTool(t, cs, "memory_save", save); res.IsError || !strings.Contains(toJSON(res.StructuredContent), `"id":"42"`) {
		t.Fatalf("engram save: %+v", res)
	}
	if len(rec.saved) != 1 || rec.saved[0].Title != "Routed" || rec.saved[0].Project == "" {
		t.Fatalf("an Engram project must reach Engram with its project key: %+v", rec.saved)
	}
	if inTeam() != 1 {
		t.Fatalf("an Engram project must not reach the team server, got %d entries", inTeam())
	}
	// What Engram cannot do is said, not faked.
	if res := callTool(t, cs, "memory_update", map[string]any{"id": "42", "patch": map[string]any{"title": "x"}}); !res.IsError {
		t.Fatalf("update is unsupported over the engram CLI: %+v", res)
	}
	if res := callTool(t, cs, "memory_active_work", map[string]any{}); !res.IsError || !strings.Contains(textOf(res), "only available with team memory") {
		t.Fatalf("active work is team-only: %+v", res)
	}
	// A summary falls back to a progress entry, like memory.Recorder.
	if res := callTool(t, cs, "memory_session_summary", map[string]any{"goal": "g", "accomplished": "a"}); res.IsError {
		t.Fatalf("summary: %s", textOf(res))
	}
	if n := len(rec.saved); n != 2 || rec.saved[1].Type != memory.TypeProgress {
		t.Fatalf("the summary must be saved as a progress entry: %+v", rec.saved)
	}
	if res := callTool(t, cs, "memory_current_project", map[string]any{}); res.IsError || !strings.Contains(toJSON(res.StructuredContent), rec.saved[0].Project) {
		t.Fatalf("current project: %+v", res)
	}

	// memory.md is read on every call: switching the project to the team server
	// takes effect without restarting the bridge.
	teamProject(t, solo, ts.URL+"/mcp")
	rec.saved = nil
	if res := callTool(t, cs, "memory_save", save); res.IsError || len(rec.saved) != 0 {
		t.Fatalf("after the switch the call must go to the team server: %+v %+v", res, rec.saved)
	}
	if inTeam() != 2 {
		t.Fatalf("the switched project must reach the server, got %d entries", inTeam())
	}
	stop()
}

func TestBridgeOutsideAProjectReportsNoProvider(t *testing.T) {
	f := newSfix(t)
	cs, errb, stop := startBridgeAt(t, f, t.TempDir()) // starts fine: the runtime may launch it anywhere
	res := callTool(t, cs, "memory_search", map[string]any{"text": "x"})
	if !res.IsError || !strings.Contains(textOf(res), "no memory provider configured for this project") {
		t.Fatalf("want a clear tool error: %+v", res)
	}
	if code := stop(); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
}
