package cli

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/memory"
	"github.com/winnstorm/gabitotl/internal/memory/store"
	"github.com/winnstorm/gabitotl/internal/memory/store/memstore"
	"github.com/winnstorm/gabitotl/internal/pgmanaged"
	rt "github.com/winnstorm/gabitotl/internal/runtime"
)

type safeBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *safeBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *safeBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

type sfix struct {
	t    *testing.T
	home string
	eng  *pgmanaged.FakeEngine
	st   *memstore.Store
	envv map[string]string
}

// newSfix points every hook of the memory-server commands at fakes: a fake
// container engine, an in-memory store and a temporary home. Nothing real runs.
func newSfix(t *testing.T) *sfix {
	t.Helper()
	f := &sfix{t: t, home: t.TempDir(), eng: pgmanaged.NewFakeEngine(), st: memstore.New(), envv: map[string]string{}}
	oldEnv, oldPG, oldStore, oldSig := runtimeEnvFn, pgManagerFn, openStoreFn, signalCtxFn
	runtimeEnvFn = func(root string) (rt.Env, error) {
		return rt.Env{Home: f.home, ProjectRoot: root, GOOS: "linux", Getenv: func(k string) string { return f.envv[k] }}, nil
	}
	pgManagerFn = func(cfg pgmanaged.Config) *pgmanaged.Manager {
		return &pgmanaged.Manager{
			Cfg: cfg, Run: f.eng, LookPath: func(n string) (string, error) { return "/usr/bin/" + n, nil },
			PortFree: func(int) bool { return true }, Now: time.Now,
			Sleep: func(context.Context, time.Duration) error { return nil },
			Rand:  strings.NewReader(strings.Repeat("0123456789abcdef", 8)), ReadyTimeout: time.Second,
		}
	}
	openStoreFn = func(context.Context, string) (store.Store, error) { return f.st, nil }
	t.Cleanup(func() { runtimeEnvFn, pgManagerFn, openStoreFn, signalCtxFn = oldEnv, oldPG, oldStore, oldSig })
	return f
}

func (f *sfix) run(stdin string, args ...string) result {
	f.t.Helper()
	return runIn(f.t, stdin, false, f.t.TempDir(), args...)
}

func TestServerInstallDryRunChangesNothing(t *testing.T) {
	f := newSfix(t)
	r := f.run("", "memory", "server", "install", "--dry-run")
	if r.code != 0 || !strings.Contains(r.out, "create container gabitotl-memory-postgres") || !strings.Contains(r.out, "127.0.0.1:54329") || !strings.Contains(r.out, "Dry run") {
		t.Fatalf("%+v", r)
	}
	if len(f.eng.Mutations) != 0 || len(f.eng.Containers) != 0 {
		t.Fatal("a dry run must not mutate anything")
	}
	if _, err := os.Stat(pgmanaged.SecretsPath(f.home)); err == nil {
		t.Fatal("a dry run must not write credentials")
	}
}

func TestServerLifecycleCommands(t *testing.T) {
	f := newSfix(t)
	r := f.run("", "memory", "server", "install")
	if r.code != 0 || !strings.Contains(r.out, "installed") || !strings.Contains(r.out, "env:GABITOTL_MEMORY_DATABASE_URL") {
		t.Fatalf("install: %+v", r)
	}
	creds, err := pgmanaged.ReadCredentials(pgmanaged.SecretsPath(f.home))
	if err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(pgmanaged.SecretsPath(f.home)); st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode().Perm())
	}
	if again := f.run("", "memory", "server", "install"); again.code != 0 || !strings.Contains(again.out, "already installed") {
		t.Fatalf("idempotent install: %+v", again)
	}
	if st := f.run("", "memory", "server", "status"); st.code != 0 || !strings.Contains(st.out, "ready:       yes") || !strings.Contains(st.out, "127.0.0.1:54329") {
		t.Fatalf("status: %+v", st)
	}
	if r := f.run("", "memory", "server", "stop"); r.code != 0 || f.eng.Containers[pgmanaged.DefaultContainer].Running {
		t.Fatalf("stop: %+v", r)
	}
	if st := f.run("", "memory", "server", "status"); st.code == 0 {
		t.Fatal("a stopped server is not a healthy status")
	}
	if r := f.run("", "memory", "server", "start"); r.code != 0 || !f.eng.Containers[pgmanaged.DefaultContainer].Running {
		t.Fatalf("start: %+v", r)
	}
	if r := f.run("", "memory", "server", "logs", "--tail", "5"); r.code != 0 || !strings.Contains(r.out, "ready to accept") {
		t.Fatalf("logs: %+v", r)
	}
	bk := f.run("", "memory", "server", "backup")
	if bk.code != 0 || !strings.Contains(bk.out, filepath.Join(f.home, ".gabitotl", "backups", "memory-postgres")) {
		t.Fatalf("backup: %+v", bk)
	}
	// Remove keeps the volume.
	rm := f.run("", "memory", "server", "remove")
	if rm.code != 0 || !strings.Contains(rm.out, "Kept volume gabitotl-memory-pgdata") || len(f.eng.Volumes) != 1 || len(f.eng.Containers) != 0 {
		t.Fatalf("remove: %+v", rm)
	}
	// Purge: wrong confirmation changes nothing; the right one deletes.
	f.run("", "memory", "server", "install")
	if r := f.run("nope\n", "memory", "server", "remove", "--purge-data"); r.code == 0 || len(f.eng.Volumes) != 1 || len(f.eng.Containers) != 1 {
		t.Fatalf("a wrong confirmation must not purge: %+v", r)
	}
	if r := f.run("", "memory", "server", "remove", "--purge-data"); r.code == 0 || len(f.eng.Volumes) != 1 {
		t.Fatalf("no confirmation (EOF) must not purge: %+v", r)
	}
	if r := f.run("", "memory", "server", "remove", "--purge-data", "--dry-run"); r.code != 0 || !strings.Contains(r.out, "Would DELETE") || len(f.eng.Volumes) != 1 {
		t.Fatalf("dry-run purge: %+v", r)
	}
	if r := f.run(pgmanaged.DefaultVolume+"\n", "memory", "server", "remove", "--purge-data"); r.code != 0 || len(f.eng.Volumes) != 0 {
		t.Fatalf("purge: %+v", r)
	}
	// Nothing printed anywhere ever contained the password.
	for _, c := range f.eng.Calls {
		if strings.Contains(strings.Join(c.Args, " "), creds.Password) {
			t.Fatal("password in a process argument")
		}
	}
	if bad := f.eng.Unlabeled(); len(bad) != 0 {
		t.Fatal(bad)
	}
}

func TestServerCommandsNeverPrintTheSecret(t *testing.T) {
	f := newSfix(t)
	var all strings.Builder
	for _, a := range [][]string{{"install"}, {"status"}, {"logs"}, {"backup"}, {"install", "--dry-run"}} {
		r := f.run("", append([]string{"memory", "server"}, a...)...)
		all.WriteString(r.out + r.err)
	}
	creds, _ := pgmanaged.ReadCredentials(pgmanaged.SecretsPath(f.home))
	if strings.Contains(all.String(), creds.Password) || strings.Contains(all.String(), "postgres://") {
		t.Fatalf("a secret leaked:\n%s", all.String())
	}
}

func TestServerRefusesAForeignContainer(t *testing.T) {
	f := newSfix(t)
	f.eng.Containers[pgmanaged.DefaultContainer] = &pgmanaged.FakeContainer{ID: "f1", Name: pgmanaged.DefaultContainer, Image: "postgres:15", Running: true, HostIP: "0.0.0.0", HostPort: "5432", Labels: map[string]string{}}
	before := f.eng.Snapshot()
	for _, args := range [][]string{{"install"}, {"start"}, {"stop"}, {"remove"}, {"remove", "--purge-data"}, {"backup"}} {
		r := f.run(pgmanaged.DefaultVolume+"\n", append([]string{"memory", "server"}, args...)...)
		if r.code == 0 || !strings.Contains(r.err, "not created by GabitoTL") {
			t.Errorf("%v must refuse and explain: %+v", args, r)
		}
	}
	if r := f.run("", "memory", "server", "install", "--dry-run"); r.code == 0 || !strings.Contains(r.out, "CONFLICT") {
		t.Fatalf("dry-run must show the conflict: %+v", r)
	}
	if len(f.eng.Mutations) != 0 || strings.Join(f.eng.Snapshot(), "|") != strings.Join(before, "|") {
		t.Fatal("the foreign container must be untouched")
	}
}

func TestServerFlagValidation(t *testing.T) {
	f := newSfix(t)
	for _, args := range [][]string{
		{"install", "--port", "5432"}, {"install", "--engine", "nerdctl"}, {"install", "--port", "80"},
	} {
		if r := f.run("", append([]string{"memory", "server"}, args...)...); r.code == 0 {
			t.Errorf("%v must be rejected: %+v", args, r)
		}
	}
	if r := f.run("", "memory", "server"); r.code != ExitUsage {
		t.Fatalf("usage: %+v", r)
	}
}

func TestTokenCommands(t *testing.T) {
	f := newSfix(t)
	f.envv[pgmanaged.EnvDatabaseURL] = "postgres://ignored"
	r := f.run("", "memory", "token", "create", "--user", "gabi", "--all-projects")
	tok := strings.TrimSpace(r.out)
	if r.code != 0 || !strings.HasPrefix(tok, "gbt_") || strings.Contains(tok, "\n") || !strings.Contains(r.err, "only time it is shown") {
		t.Fatalf("create must print only the token on stdout: %+v", r)
	}
	list := f.run("", "memory", "token", "list", "--user", "gabi")
	if list.code != 0 || !strings.Contains(list.out, "gabi") || !strings.Contains(list.out, "active") {
		t.Fatalf("list: %+v", list)
	}
	secret := strings.SplitN(tok, ".", 2)[1]
	if strings.Contains(list.out+list.err, secret) {
		t.Fatal("list must never show the secret")
	}
	if r := f.run("", "memory", "token", "create", "--user", "legacy", "--all-projects"); r.code == 0 {
		t.Fatal("legacy is reserved")
	}
	if r := f.run("", "memory", "token", "create"); r.code != ExitUsage {
		t.Fatalf("--user is required: %+v", r)
	}
	for _, args := range [][]string{
		{"--user", "gabi"}, // no scope given: refused, never read as "everything"
		{"--user", "gabi", "--projects", "a", "--all-projects"},
	} {
		if r := f.run("", append([]string{"memory", "token", "create"}, args...)...); r.code != ExitUsage || r.out != "" {
			t.Fatalf("%v must be a usage error without a token: %+v", args, r)
		}
	}
	sc := f.run("", "memory", "token", "create", "--user", "gonza", "--projects", "shop, kraken")
	if sc.code != 0 || !strings.Contains(sc.err, "shop, kraken") {
		t.Fatalf("scoped create: %+v", sc)
	}
	if l := f.run("", "memory", "token", "list", "--user", "gonza"); !strings.Contains(l.out, "projects: shop, kraken") {
		t.Fatalf("list must show the scope: %+v", l)
	}
	if l := f.run("", "memory", "token", "list", "--user", "gabi"); !strings.Contains(l.out, "all projects") {
		t.Fatalf("list must show all projects: %+v", l)
	}
	if w := f.run("", "memory", "token", "create", "--user", "pepe", "--all-projects"); !strings.Contains(w.err, "EVERY project") {
		t.Fatalf("all-projects must warn: %+v", w)
	}
	if r := f.run("", "memory", "token", "create", "--user", "gabi", "--all-projects", "--dsn", "postgres://u:p@h/d"); r.code == 0 || strings.Contains(r.err, "p@h") {
		t.Fatalf("a literal DSN must be refused without echoing it: %+v", r)
	}
	if rv := f.run("", "memory", "token", "revoke", "--user", "gabi"); rv.code != 0 || !strings.Contains(rv.out, "Revoked 1") {
		t.Fatalf("revoke: %+v", rv)
	}
	if rv := f.run("", "memory", "token", "revoke", "--user", "gabi"); rv.code != 0 || !strings.Contains(rv.out, "no active tokens") {
		t.Fatalf("revoke again: %+v", rv)
	}
	if rv := f.run("", "memory", "token", "revoke", "--user", "gabi", "--id", "deadbeefdeadbeef"); rv.code == 0 {
		t.Fatal("unknown id must fail")
	}
}

func TestDSNResolutionUsesTheManagedCredentialsFile(t *testing.T) {
	f := newSfix(t)
	if r := f.run("", "memory", "token", "list"); r.code == 0 || !strings.Contains(r.err, pgmanaged.EnvDatabaseURL) {
		t.Fatalf("without any source the error names the variable: %+v", r)
	}
	if err := pgmanaged.WriteCredentials(pgmanaged.SecretsPath(f.home), pgmanaged.Credentials{Password: "pw", URL: "postgres://u:pw@127.0.0.1:54329/d"}); err != nil {
		t.Fatal(err)
	}
	if r := f.run("", "memory", "token", "list"); r.code != 0 {
		t.Fatalf("the managed credentials file must satisfy the default reference: %+v", r)
	}
}

func TestServeOverHTTPWithTokenAuth(t *testing.T) {
	f := newSfix(t)
	f.envv[pgmanaged.EnvDatabaseURL] = "x"
	tr := f.run("", "memory", "token", "create", "--user", "gabi", "--all-projects")
	tok := strings.TrimSpace(tr.out)

	ctx, cancel := context.WithCancel(context.Background())
	signalCtxFn = func() (context.Context, context.CancelFunc) { return ctx, func() {} }
	var out, errb safeBuf
	done := make(chan int, 1)
	go func() {
		done <- Run([]string{"memory", "serve", "--http", "127.0.0.1:0", "--project", "shop"},
			Env{Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errb, Getwd: func() (string, error) { return t.TempDir(), nil }})
	}()
	var url string
	for i := 0; i < 200 && url == ""; i++ {
		for _, ln := range strings.Split(errb.String(), "\n") {
			if _, after, ok := strings.Cut(ln, "listening on "); ok {
				url = strings.TrimSpace(after)
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if url == "" {
		t.Fatalf("server did not start: %s", errb.String())
	}
	connect := func(token string) (*mcp.ClientSession, error) {
		return mcp.NewClient(&mcp.Implementation{Name: "t", Version: "0"}, nil).Connect(context.Background(), &mcp.StreamableClientTransport{
			Endpoint: url, DisableStandaloneSSE: true,
			HTTPClient: &http.Client{Transport: roundTripFn(func(r *http.Request) (*http.Response, error) {
				r = r.Clone(r.Context())
				r.Header.Set("Authorization", "Bearer "+token)
				return http.DefaultTransport.RoundTrip(r)
			})},
		}, nil)
	}
	if _, err := connect("gbt_0000000000000000.wrongwrongwrongwrongwrong"); err == nil {
		t.Fatal("a bad token must not connect")
	}
	cs, err := connect(tok)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "memory_save", Arguments: map[string]any{"type": "decision", "title": "Served", "content": "through the CLI"}})
	if err != nil || res.IsError {
		t.Fatalf("save: %v %+v", err, res)
	}
	res, err = cs.CallTool(ctx, &mcp.CallToolParams{Name: "memory_save", Arguments: map[string]any{"type": "decision", "title": "Forged", "content": "x", "actor": "gonza"}})
	if err != nil || !res.IsError {
		t.Fatalf("a spoofed actor must be rejected: %v %+v", err, res)
	}
	rs, _ := f.st.Search(ctx, memoryQuery("served", "shop"))
	if len(rs) != 1 || rs[0].Actor != "gabi" {
		t.Fatalf("stored: %+v", rs)
	}
	cancel()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit %d: %s", code, errb.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the server must stop when interrupted")
	}
	if strings.Contains(errb.String(), tok) || strings.Contains(errb.String(), "through the CLI") {
		t.Fatalf("logs leak: %s", errb.String())
	}
}

func TestServeArgumentChecks(t *testing.T) {
	f := newSfix(t)
	f.envv[pgmanaged.EnvDatabaseURL] = "x"
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	signalCtxFn = func() (context.Context, context.CancelFunc) { return ctx, func() {} }
	for name, tc := range map[string]struct {
		args []string
		code int
	}{
		"neither":        {[]string{"memory", "serve"}, ExitUsage},
		"both":           {[]string{"memory", "serve", "--http", "127.0.0.1:0", "--stdio"}, ExitUsage},
		"remote plain":   {[]string{"memory", "serve", "--http", "0.0.0.0:0"}, ExitError},
		"literal dsn":    {[]string{"memory", "serve", "--http", "127.0.0.1:0", "--dsn", "postgres://u:p@h/d"}, ExitError},
		"actor on http":  {[]string{"memory", "serve", "--http", "127.0.0.1:0", "--actor", "gabi"}, ExitUsage},
		"stdio os actor": {[]string{"memory", "serve", "--stdio"}, ExitOK}, // actor derived from the OS user
		"tls half":       {[]string{"memory", "serve", "--http", "127.0.0.1:0", "--tls-cert", "c.pem"}, ExitError},
	} {
		if r := f.run("", tc.args...); r.code != tc.code || strings.Contains(r.err+r.out, "p@h") {
			t.Errorf("%s: want %d, got %+v", name, tc.code, r)
		}
	}
}

type roundTripFn func(*http.Request) (*http.Response, error)

func (f roundTripFn) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func memoryQuery(text, project string) memory.Query {
	return memory.Query{Text: text, Project: project}
}

func TestServerInstanceFlagUsesOwnNamesAndCredentials(t *testing.T) {
	f := newSfix(t)
	if r := f.run("", "memory", "server", "install", "--instance", "trial"); r.code != 0 || !strings.Contains(r.out, "gabitotl-memory-postgres-trial") || !strings.Contains(r.out, "--instance trial") {
		t.Fatalf("%+v", r)
	}
	if f.eng.Containers["gabitotl-memory-postgres-trial"] == nil || f.eng.Containers[pgmanaged.DefaultContainer] != nil {
		t.Fatal("only the named instance may exist")
	}
	if _, err := os.Stat(filepath.Join(f.home, ".gabitotl", "secrets", "memory-server-trial.env")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(pgmanaged.SecretsPath(f.home)); err == nil {
		t.Fatal("the default credentials file must not be touched")
	}
	if r := f.run("", "memory", "token", "list", "--instance", "trial"); r.code != 0 {
		t.Fatalf("the instance credentials back the default reference: %+v", r)
	}
	if r := f.run("", "memory", "token", "list"); r.code == 0 {
		t.Fatal("without the instance there is no credential source")
	}
	if r := f.run("", "memory", "server", "install", "--instance", "Bad Name"); r.code == 0 {
		t.Fatal("invalid instance")
	}
}

func compactProject(t *testing.T, f *sfix) string {
	t.Helper()
	root := t.TempDir()
	r := runIn(t, "", false, t.TempDir(), "init", "--root", root, "--name", "Shop", "--kind", "new",
		"--user-id", "gabi", "--user-name", "Gabi", "--mode", "team", "--provider", "memory-mcp", "--yes")
	if r.code != 0 {
		t.Fatalf("init: %s", r.err)
	}
	f.envv["GABITOTL_MEMORY_DATABASE_URL"] = "postgres://fake"
	cfg, err := gabitotl.LoadMemory(gabitotl.NewLayout(root))
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().AddDate(0, -3, 0)
	f.st.Now = func() time.Time { return old }
	for _, e := range []memory.Entry{
		{Type: memory.TypeProgress, Title: "a", Content: "one"},
		{Type: memory.TypeProgress, Title: "b", Content: "two"},
		{Type: memory.TypeDecision, Title: "d", Content: "keep"},
	} {
		e.Project, e.Actor = cfg.ProjectKey, "gabi"
		if _, err := f.st.Save(context.Background(), e); err != nil {
			t.Fatal(err)
		}
	}
	f.st.Now = nil
	return root
}

func TestMemoryCompactDryRunThenApply(t *testing.T) {
	f := newSfix(t)
	root := compactProject(t, f)
	r := runIn(t, "", false, root, "memory", "compact")
	if r.code != 0 || !strings.Contains(r.out, "2 would be folded") || !strings.Contains(r.out, "Dry run") {
		t.Fatalf("%+v", r)
	}
	if st, _ := f.st.Stats(context.Background()); st.Observations != 3 || st.Deleted != 0 {
		t.Fatalf("dry run changed the store: %+v", st)
	}
	if r := runIn(t, "", false, root, "memory", "compact", "--apply"); r.code != ExitError {
		t.Fatalf("apply without a terminal or --yes must refuse: %+v", r)
	}
	r = runIn(t, "", false, root, "memory", "compact", "--apply", "--yes")
	if r.code != 0 || !strings.Contains(r.out, "1 digests written, 2 entries soft-deleted") || !strings.Contains(r.out, "backup: ") {
		t.Fatalf("%+v", r)
	}
	if st, _ := f.st.Stats(context.Background()); st.Observations != 2 || st.Deleted != 2 {
		t.Fatalf("stats %+v", st)
	}
	if m, _ := filepath.Glob(filepath.Join(root, ".gabitotl", ".bak", "memory-compact-*.json")); len(m) != 1 {
		t.Fatalf("backup files: %v", m)
	}
}

func TestMemoryCompactRefusesEngramAndBadAge(t *testing.T) {
	f := newSfix(t)
	root := initProject(t)
	if r := runIn(t, "", false, root, "memory", "compact", "--apply", "--yes"); r.code != ExitError || !strings.Contains(r.err, "cannot delete entries") {
		t.Fatalf("%+v", r)
	}
	if r := runIn(t, "", false, compactProject(t, f), "memory", "compact", "--older-than", "3x"); r.code != ExitUsage {
		t.Fatalf("%+v", r)
	}
}
