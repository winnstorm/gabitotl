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

	"github.com/winnstorm/gabitotl/internal/memory/server"
)

// startBridge runs `gabitotl memory mcp args...` in-process and returns an MCP
// client session wired to its stdin/stdout, plus the stderr buffer.
func startBridge(t *testing.T, f *sfix, args ...string) (*mcp.ClientSession, *safeBuf, func() int) {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	signalCtxFn = func() (context.Context, context.CancelFunc) { return ctx, func() {} }
	var errb safeBuf
	done := make(chan int, 1)
	go func() {
		done <- Run(append([]string{"memory", "mcp"}, args...), Env{Stdin: inR, Stdout: outW, Stderr: &errb,
			Getwd: func() (string, error) { return f.t.TempDir(), nil }})
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
		"no url no proj":  {},
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
