package e2e

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/winnstorm/gabitotl/internal/memory/mcp"
	"github.com/winnstorm/gabitotl/internal/memory/migrate"
	"github.com/winnstorm/gabitotl/internal/memory/migrate/engrammcp"
	"github.com/winnstorm/gabitotl/internal/memory/server"
	"github.com/winnstorm/gabitotl/internal/memory/store"
	"github.com/winnstorm/gabitotl/internal/memory/store/memstore"
)

// teamServer is the real Memory MCP server over an in-memory store, listening
// on 127.0.0.1 (no Docker, no PostgreSQL). Everything else in these tests is
// the real binary.
type teamServer struct {
	st  *memstore.Store
	url string
}

func startTeamServer(t *testing.T) *teamServer {
	t.Helper()
	st := memstore.New()
	srv, err := server.New(server.Config{Store: st, UserRate: 1000, UserBurst: 1000})
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("cannot listen on loopback: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = srv.Serve(ctx, ln, server.HTTPConfig{Addr: ln.Addr().String()}); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	return &teamServer{st: st, url: "http://" + ln.Addr().String() + server.EndpointPath}
}

func (s *teamServer) token(t *testing.T, user string, projects ...string) string {
	t.Helper()
	p, _, err := server.NewScopedToken(context.Background(), s.st, user, projects, len(projects) == 0, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// bridge starts `gabitotl memory mcp` as a child process and connects an MCP
// client to its stdio, exactly as a runtime would.
func (m *machine) bridge(t *testing.T, dir string, extra map[string]string, args ...string) *sdk.ClientSession {
	t.Helper()
	cmd := exec.Command(binary, append([]string{"memory", "mcp"}, args...)...)
	cmd.Dir = dir
	cmd.Env = m.env()
	for k, v := range extra {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "runtime", Version: "0"}, nil).Connect(ctx, &sdk.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("starting the bridge: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func callTool(t *testing.T, cs *sdk.ClientSession, tool string, args map[string]any) (map[string]any, string) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		return nil, err.Error()
	}
	if res.IsError {
		var b strings.Builder
		for _, c := range res.Content {
			if tc, ok := c.(*sdk.TextContent); ok {
				b.WriteString(tc.Text)
			}
		}
		return nil, b.String()
	}
	raw, _ := json.Marshal(res.StructuredContent)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return out, ""
}

func entriesOf(out map[string]any) []map[string]any {
	raw, _ := json.Marshal(out["entries"])
	var es []map[string]any
	_ = json.Unmarshal(raw, &es)
	return es
}

const e2eExport = `{"version":"0.2.0","sessions":[{"id":"s1","project":"shop","started_at":"2026-01-01 09:00:00","summary":"Moved checkout."}],
"observations":[
 {"id":1,"sync_id":"a","session_id":"s1","type":"decision","title":"Legacy decision","content":"Use PostgreSQL for shared memory.","project":"shop","scope":"project","topic_key":"arch/db","created_at":"2026-01-01 09:10:00","updated_at":"2026-01-02 09:10:00"},
 {"id":2,"sync_id":"b","session_id":"s1","type":"pattern","title":"Repository pattern","content":"Keep SQL in repositories.","project":"shop","scope":"project","created_at":"2026-01-03 09:10:00","updated_at":"2026-01-03 09:10:00"},
 {"id":3,"sync_id":"c","session_id":"s1","type":"decision","title":"Private note","content":"not for the team","project":"shop","scope":"personal","created_at":"2026-01-04 09:10:00","updated_at":"2026-01-04 09:10:00"}]}`

// The whole team-memory flow, hermetically: a localhost server with two users and
// scoped tokens, the client-role install (dry run, then apply) through the real
// binary, the stdio bridge round trip, the Engram import and the doctor.
func TestTeamMemoryFlow(t *testing.T) {
	m := newMachine(t)
	srv := startTeamServer(t)
	gabi := srv.token(t, "gabi", "shop")
	gonza := srv.token(t, "gonza", "kraken")
	proj := m.repo("shop", map[string]string{"go.mod": "module shop\n", "main.go": "package main\n"})
	// Engram stays as it is: a fixture data directory whose bytes must never change.
	m.write(filepath.Join(m.home, ".engram", "engram.db"), "ENGRAM-DB-BYTES")
	engramBefore := hash(t, nil, filepath.Join(m.home, ".engram"))
	tokenEnv := map[string]string{"GBT_E2E_TOKEN": gabi}

	args := []string{"install", "--memory", "memory-mcp", "--memory-role", "client", "--memory-url", srv.url,
		"--memory-token-env", "GBT_E2E_TOKEN", "--memory-store-token", "--user", "gabi", "--name", "Gabi", "--runtime", "claude-code", "--no-tui"}

	// 1. Dry run: every step shown, nothing changed, no engram export.
	before := hash(t, []string{m.dir}, m.home, proj)
	r := m.runEnv(tokenEnv, proj, append(args, "--dry-run")...)
	if r.code != 0 {
		t.Fatalf("dry run exited %d\n%s\n%s", r.code, r.out, r.err)
	}
	contains(t, "dry run", r.out, "memory.md", "gabitotl-memory")
	if hash(t, []string{m.dir}, m.home, proj) != before || m.count("engram export") != 0 {
		t.Fatal("a dry run must change nothing")
	}

	// 2. Apply: memory.md holds references only, the token is in the 0600 file,
	// the runtime has ONE managed bridge entry, and the doctor passes.
	r = m.runEnv(tokenEnv, proj, append(args, "--yes")...)
	if r.code != 0 {
		t.Fatalf("install exited %d\n%s\n%s", r.code, r.out, r.err)
	}
	mem := read(t, filepath.Join(proj, ".gabitotl", "memory.md"))
	contains(t, "memory.md", mem, "mode: team", "provider: memory-mcp", "role: client", "url: "+srv.url, "token: env:GBT_E2E_TOKEN")
	absent(t, "memory.md", mem, gabi, strings.SplitN(gabi, ".", 2)[1])
	secrets := mcp.ClientSecretsPath(m.home)
	if st, err := os.Stat(secrets); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("secrets file: %v %v", st, err)
	}
	claude := read(t, filepath.Join(m.home, ".claude.json"))
	contains(t, ".claude.json", claude, "gabitotl-memory", `"memory"`, `"mcp"`)
	absent(t, ".claude.json", claude, gabi)
	// No secret anywhere a log, report or snapshot could carry it.
	for _, dir := range []string{proj, filepath.Join(m.home, ".gabitotl", "migrations"), filepath.Join(m.home, ".gabitotl", "logs"), filepath.Join(m.home, ".gabitotl", "installations")} {
		_ = filepath.WalkDir(dir, func(p string, e os.DirEntry, err error) error {
			if err == nil && !e.IsDir() && !strings.Contains(p, ".git"+string(filepath.Separator)) {
				absent(t, p, read(t, p), strings.SplitN(gabi, ".", 2)[1])
			}
			return nil
		})
	}
	// The doctor works WITHOUT the variable in the environment: the secrets file serves it.
	r = m.ok(proj, "memory", "doctor")
	contains(t, "memory doctor", r.out, "[ok] server", "[ok] authentication", "[ok] project")
	r = m.ok(proj, "doctor")
	contains(t, "doctor", r.out+r.err, "Memoria compartida")

	// 3. The bridge, as a runtime starts it: no URL, no token on the command line.
	bg := m.bridge(t, proj, nil)
	out, msg := callTool(t, bg, "memory_save", map[string]any{"type": "decision", "title": "From the bridge", "content": "saved by gabi through stdio"})
	if msg != "" {
		t.Fatalf("save through the bridge: %s", msg)
	}
	if out["created"] != true {
		t.Fatalf("%v", out)
	}
	found, msg := callTool(t, bg, "memory_search", map[string]any{"text": "bridge"})
	es := entriesOf(found)
	if msg != "" || len(es) != 1 || es[0]["actor"] != "gabi" || es[0]["project"] != "shop" {
		t.Fatalf("search: %v %s", found, msg)
	}
	// Scoped token: kraken is not gabi's project; a forged actor is rejected.
	if _, msg := callTool(t, bg, "memory_search", map[string]any{"text": "x", "project": "kraken"}); !strings.Contains(msg, "not allowed to use project") {
		t.Fatalf("out-of-scope read must be rejected: %q", msg)
	}
	if _, msg := callTool(t, bg, "memory_save", map[string]any{"type": "decision", "title": "f", "content": "c", "project": "kraken"}); !strings.Contains(msg, "not allowed to use project") {
		t.Fatalf("out-of-scope write must be rejected: %q", msg)
	}
	if _, msg := callTool(t, bg, "memory_save", map[string]any{"type": "decision", "title": "f", "content": "c", "actor": "gonza"}); !strings.Contains(msg, "actor mismatch") {
		t.Fatalf("a forged actor must be rejected: %q", msg)
	}

	// 4. A second user on the same server through the bridge with explicit flags.
	kraken := m.repo("kraken", map[string]string{"go.mod": "module kraken\n"})
	bk := m.bridge(t, kraken, map[string]string{"GBT_GONZA": gonza}, "--url", srv.url, "--token-env", "GBT_GONZA", "--project", "kraken")
	if _, msg := callTool(t, bk, "memory_save", map[string]any{"type": "bugfix", "title": "Kraken fix", "content": "gonza's work"}); msg != "" {
		t.Fatalf("gonza saves in kraken: %s", msg)
	}
	if got, _ := callTool(t, bk, "memory_search", map[string]any{"text": "bridge"}); len(entriesOf(got)) != 0 {
		t.Fatalf("gonza must not see gabi's project: %v", got)
	}
	if _, msg := callTool(t, bk, "memory_search", map[string]any{"text": "bridge", "project": "shop"}); !strings.Contains(msg, "not allowed") {
		t.Fatalf("gonza cannot read shop: %q", msg)
	}
	act, _ := callTool(t, bg, "memory_active_work", map[string]any{"project": "shop"})
	for _, e := range entriesOf(act) {
		if e["actor"] != "gabi" {
			t.Fatalf("shop only holds gabi's work: %v", e)
		}
	}

	// 5. Engram -> team memory with the tested migrator, on the host side (store level).
	export := filepath.Join(m.root, "engram-export.json")
	if err := os.WriteFile(export, []byte(e2eExport), 0o600); err != nil {
		t.Fatal(err)
	}
	exportBefore := hash(t, nil, export)
	r = m.ok(proj, "memory", "migrate", "engram", "--project", "shop", "--export", export, "--dry-run")
	contains(t, "migrate dry run", r.out, "2 se importarían", "personal-scope 1", "no importé nada")
	mig := engrammcp.New(func(context.Context, string) (store.Importer, func(), error) { return srv.st, func() {}, nil })
	reg := migrate.NewRegistry()
	if err := reg.Register(mig); err != nil {
		t.Fatal(err)
	}
	mm, err := reg.Lookup("engram", "memory-mcp")
	if err != nil {
		t.Fatal(err)
	}
	req := migrate.Request{ExportPath: export, BackupPath: export, ReportPath: filepath.Join(m.root, "import-report.json"), Project: "shop"}
	res, err := mm.Migrate(context.Background(), req)
	if err != nil || res.Report.Imported != 2 || !res.Report.Verified || res.Report.Status != "complete" {
		t.Fatalf("import: %+v %v", res.Report, err)
	}
	res, err = mm.Migrate(context.Background(), req)
	if err != nil || res.Report.Imported != 0 || res.Report.AlreadyPresent != 2 || !res.Report.Verified {
		t.Fatalf("re-run must not duplicate: %+v %v", res.Report, err)
	}
	if hash(t, nil, export) != exportBefore {
		t.Fatal("the export must be untouched")
	}
	// The imported history is visible to gabi through the bridge, attributed to legacy.
	hist, msg := callTool(t, bg, "memory_search", map[string]any{"text": "legacy"})
	he := entriesOf(hist)
	if msg != "" || len(he) != 1 || he[0]["actor"] != "legacy" || he[0]["source"] != "engram/gentle-ai" || he[0]["topic_key"] != "arch/db" {
		t.Fatalf("history through the bridge: %v %s", hist, msg)
	}
	if _, err := srv.st.BySourceRef(context.Background(), "engram:c"); err == nil {
		t.Fatal("the personal observation must not be imported")
	}

	// 6. Engram itself was never modified, and the harness never wrote to it.
	if hash(t, nil, filepath.Join(m.home, ".engram")) != engramBefore {
		t.Fatal("the Engram data directory must be byte-identical")
	}
	if m.count("engram save") != 0 {
		t.Fatal("no engram save may run")
	}

	// 7. A revoked token stops working at once; the doctor says so.
	ts, _ := srv.st.ListTokens(context.Background(), "gabi")
	if err := srv.st.RevokeToken(context.Background(), "gabi", ts[0].ID); err != nil {
		t.Fatal(err)
	}
	r = m.run(proj, "memory", "doctor")
	if r.code == 0 || !strings.Contains(r.out, "authentication") {
		t.Fatalf("a revoked token must fail the doctor: %+v", r)
	}
}

func TestTeamInstallRefusesInsecureAndLiteralTokens(t *testing.T) {
	m := newMachine(t)
	proj := m.repo("shop", map[string]string{"go.mod": "module shop\n"})
	before := hash(t, []string{m.dir}, m.home, proj)
	for name, args := range map[string][]string{
		"plain http to a remote": {"--memory-url", "http://memoria.example.com/mcp", "--memory-token-env", "T"},
		"literal token":          {"--memory-url", "https://memoria.example.com/mcp", "--memory-token-env", "gbt_0123456789abcdef.secretsecretsecretsecretsecret"},
		"client import":          {"--memory-url", "https://memoria.example.com/mcp", "--import-engram"},
		"no container engine":    {"--memory-role", "host", "--user", "gabi", "--name", "Gabi"},
	} {
		r := m.run(proj, append([]string{"install", "--memory", "memory-mcp", "--runtime", "claude-code", "--no-tui", "--yes"}, args...)...)
		if r.code == 0 {
			t.Errorf("%s must be refused: %+v", name, r)
		}
		if strings.Contains(r.out+r.err, "secretsecret") {
			t.Errorf("%s: a token value was echoed", name)
		}
	}
	if hash(t, []string{m.dir}, m.home, proj) != before {
		t.Fatal("refused installs must change nothing (and never install a container engine)")
	}
}
