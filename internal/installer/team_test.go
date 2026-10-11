package installer

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/memory"
	"github.com/winnstorm/gabitotl/internal/memory/mcp"
	"github.com/winnstorm/gabitotl/internal/memory/migrate"
	"github.com/winnstorm/gabitotl/internal/memory/migrate/engrammcp"
	"github.com/winnstorm/gabitotl/internal/memory/server"
	"github.com/winnstorm/gabitotl/internal/memory/store"
	"github.com/winnstorm/gabitotl/internal/memory/store/memstore"
	"github.com/winnstorm/gabitotl/internal/pgmanaged"
)

// teamRig adds a fake container engine, an in-memory team database and token
// minting to the standard fixture. Nothing real runs.
type teamRig struct {
	*fixture
	eng       *pgmanaged.FakeEngine
	st        *memstore.Store
	noEngine  bool
	failStore int // OpenStore fails from this call on (0: never)
	opens     int
	doctor    func(cfg gabitotl.MemoryConfig) memory.Report
}

func newTeamRig(t *testing.T) *teamRig {
	t.Helper()
	r := &teamRig{fixture: newFixture(t), eng: pgmanaged.NewFakeEngine(), st: memstore.New()}
	r.doctor = func(gabitotl.MemoryConfig) memory.Report {
		return memory.Report{Provider: "memory-mcp", Checks: []memory.Check{{Name: "server", Status: memory.StatusOK}}}
	}
	reg := migrate.NewRegistry()
	_ = reg.Register(engrammcp.New(func(context.Context, string) (store.Importer, func(), error) { return r.st, func() {}, nil }))
	r.env.Migrators = reg
	r.env.Team = TeamEnv{
		Manager: func(instance, engine string, port int) (*pgmanaged.Manager, error) {
			cfg := pgmanaged.DefaultConfig(r.home)
			cfg, err := cfg.WithInstance(instance)
			if err != nil {
				return nil, err
			}
			cfg.Engine = "auto"
			return &pgmanaged.Manager{Cfg: cfg, Run: r.eng,
				LookPath: func(n string) (string, error) {
					if r.noEngine {
						return "", os.ErrNotExist
					}
					return "/usr/bin/" + n, nil
				},
				PortFree: func(int) bool { return true }, Now: time.Now,
				Sleep: func(context.Context, time.Duration) error { return nil },
				Rand:  strings.NewReader(strings.Repeat("0123456789abcdef", 8)), ReadyTimeout: time.Second}, nil
		},
		OpenStore: func(context.Context, string) (store.Store, func(), error) {
			r.opens++
			if r.failStore > 0 && r.opens >= r.failStore {
				return nil, nil, errors.New("database unreachable")
			}
			return r.st, func() {}, nil
		},
		NewToken: func(ctx context.Context, ts store.TokenStore, user string, projects []string) (string, error) {
			p, _, err := server.NewScopedToken(ctx, ts, user, projects, false, time.Now())
			return p, err
		},
	}
	r.env.OpenMemory = func(cfg gabitotl.MemoryConfig) (memory.Provider, error) { return teamProvider{r, cfg}, nil }
	return r
}

type teamProvider struct {
	r   *teamRig
	cfg gabitotl.MemoryConfig
}

func (p teamProvider) Search(context.Context, memory.Query) ([]memory.Entry, error) { return nil, nil }
func (p teamProvider) Save(ctx context.Context, e memory.Entry) (memory.ID, error) {
	return p.r.mem.Save(ctx, e)
}
func (p teamProvider) Update(context.Context, memory.ID, memory.EntryPatch) error { return nil }
func (p teamProvider) SessionSummary(context.Context, memory.Summary) error       { return nil }
func (p teamProvider) CurrentProject(context.Context) (string, error)             { return "shop", nil }
func (p teamProvider) Doctor(context.Context) memory.Report                       { return p.r.doctor(p.cfg) }

func (r *teamRig) plan(opts Options) (Inventory, Plan, error) {
	r.t.Helper()
	inv := BuildInventory(context.Background(), r.env, nil)
	pl, err := BuildPlan(context.Background(), r.env, inv, opts)
	return inv, pl, err
}

func hostOpts() Options {
	return Options{Memory: "memory-mcp", MemoryRole: "host", User: "gabi", UserName: "Gabi", Runtimes: []string{"claude-code"}}
}

// walkContains reports whether any file under dirs contains needle.
func walkContains(t *testing.T, needle string, dirs ...string) bool {
	t.Helper()
	found := false
	for _, d := range dirs {
		_ = filepath.WalkDir(d, func(p string, e fs.DirEntry, err error) error {
			if err == nil && !e.IsDir() {
				if b, _ := os.ReadFile(p); strings.Contains(string(b), needle) {
					found = true
				}
			}
			return nil
		})
	}
	return found
}

func TestHostDryRunDescribesEverythingAndChangesNothing(t *testing.T) {
	r := newTeamRig(t)
	before := treeHash(t, r.home, r.root)
	o := hostOpts()
	o.DryRun = true
	_, pl, err := r.plan(o)
	if err != nil {
		t.Fatal(err)
	}
	if pl.Team == nil || pl.Team.Role != "host" || pl.Team.Server == nil || pl.Team.URL != DefaultServerURL || pl.Team.TokenEnv != DefaultTokenEnv {
		t.Fatalf("%+v", pl.Team)
	}
	var creates []string
	for _, it := range pl.Items {
		if strings.HasPrefix(it.Target, "servidor de memoria:") {
			creates = append(creates, it.Target)
		}
	}
	if len(creates) < 4 {
		t.Fatalf("the plan must list the network, volume, container and credentials: %v", creates)
	}
	inv := BuildInventory(context.Background(), r.env, nil)
	res, err := Run(context.Background(), r.env, inv, pl, nil, Hooks{})
	if err != nil || !res.DryRun {
		t.Fatalf("%+v %v", res, err)
	}
	if len(r.eng.Mutations) != 0 || treeHash(t, r.home, r.root) != before {
		t.Fatal("a dry run must change nothing: no containers, no files")
	}
	if r.runner.Count("engram export") != 0 {
		t.Fatal("a dry run must not export")
	}
}

func TestHostInstallEndToEnd(t *testing.T) {
	r := newTeamRig(t)
	o := hostOpts()
	o.Invites = []string{"gonza"}
	inv, pl, err := r.plan(o)
	if err != nil {
		t.Fatal(err)
	}
	res, err := Run(context.Background(), r.env, inv, pl, nil, Hooks{})
	if err != nil {
		t.Fatalf("%v (%+v)", err, res.Checks)
	}
	// The isolated server exists, labeled, and only labeled resources were touched.
	if c := r.eng.Containers[pgmanaged.DefaultContainer]; c == nil || !c.Running {
		t.Fatal("the managed container must be running")
	}
	for _, m := range r.eng.Mutations {
		if strings.HasSuffix(m.Verb, "-create") && !m.Labeled {
			t.Fatalf("unlabeled creation: %+v", m)
		}
	}
	if res.Server == nil || !res.Server.Created || res.Team == nil || res.Team.Role != "host" {
		t.Fatalf("%+v %+v", res.Server, res.Team)
	}
	// memory.md: references only.
	l := gabitotl.NewLayout(r.root)
	cfg, err := gabitotl.LoadMemory(l)
	if err != nil || cfg.Mode != "team" || cfg.Provider != "memory-mcp" || cfg.URL != DefaultServerURL || cfg.Token != "env:"+DefaultTokenEnv || cfg.Role != "host" {
		t.Fatalf("%+v %v", cfg, err)
	}
	if err := gabitotl.ValidateNoSecrets([]byte(slurp(t, l.Memory()))); err != nil {
		t.Fatal(err)
	}
	// The developer's token is in the 0600 secrets file; teammates' tokens are only in the result.
	secrets := mcp.ClientSecretsPath(r.home)
	if st, err := os.Stat(secrets); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("secrets file: %v %v", st, err)
	}
	mine := (mcp.Resolver{Home: r.home, Getenv: func(string) string { return "" }}).Lookup(DefaultTokenEnv)
	if !strings.HasPrefix(mine, "gbt_") || len(res.Tokens) != 1 || res.Tokens[0].User != "gonza" || !strings.HasPrefix(res.Tokens[0].Token, "gbt_") {
		t.Fatalf("tokens: mine=%q %+v", mine, res.Tokens)
	}
	// Scoped to the project, and attributable to the right users.
	ts, _ := r.st.ListTokens(context.Background(), "")
	if len(ts) != 2 {
		t.Fatalf("two tokens: %+v", ts)
	}
	for _, tk := range ts {
		if tk.AllProjects || len(tk.Projects) != 1 || tk.Projects[0] != inv.Project.Key {
			t.Fatalf("tokens must be scoped to the project: %+v", tk)
		}
	}
	// No token or password anywhere it must not be: repo, snapshot, logs, reports, state.
	secret := strings.SplitN(res.Tokens[0].Token, ".", 2)[1]
	mineSecret := strings.SplitN(mine, ".", 2)[1]
	for _, needle := range []string{secret, mineSecret, "0123456789abcdef0123456789abcdef"} {
		if walkContains(t, needle, r.root, filepath.Join(r.home, ".gabitotl", "migrations"), filepath.Join(r.home, ".gabitotl", "logs"),
			filepath.Join(r.home, ".gabitotl", "installations"), filepath.Join(r.home, ".claude")) {
			t.Fatalf("a secret leaked into the repo, snapshot, logs or configs: %q", needle[:6])
		}
	}
	// The runtime got the bridge entry (and Engram's own, which is present on this machine).
	cj := slurp(t, filepath.Join(r.home, ".claude.json"))
	if !strings.Contains(cj, "gabitotl-memory") || strings.Count(cj, "gabitotl-memory") != 1 {
		t.Fatalf(".claude.json:\n%s", cj)
	}
	// Idempotent: running again changes nothing and never reinstalls.
	inv2, pl2, err := r.plan(o)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range pl2.Team.Server.Plan.Steps {
		if s.Action == "create" {
			t.Fatalf("a second plan must not create anything: %+v", s)
		}
	}
	_ = inv2
}

func engramExport(t *testing.T, project string) string {
	t.Helper()
	b, _ := json.Marshal(map[string]any{
		"version": "0.2.0", "sessions": []any{map[string]any{"id": "s1", "project": project, "started_at": "2026-01-01 09:00:00"}},
		"observations": []any{
			map[string]any{"id": 1, "sync_id": "a", "session_id": "s1", "type": "decision", "title": "Use PostgreSQL", "content": "Shared memory.", "project": project,
				"scope": "project", "created_at": "2026-01-01 09:10:00", "updated_at": "2026-01-01 09:10:00"},
			map[string]any{"id": 2, "sync_id": "b", "session_id": "s1", "type": "bugfix", "title": "Fix refresh", "content": "Token reuse.", "project": project,
				"scope": "project", "created_at": "2026-01-02 09:10:00", "updated_at": "2026-01-02 09:10:00"},
		}})
	return string(b)
}

func TestHostInstallImportsEngramWithoutTouchingIt(t *testing.T) {
	r := newTeamRig(t)
	key := DetectProject(context.Background(), r.env).Key
	r.runner.Handlers["engram projects"] = func([]string) (string, error) {
		return "Projects:\n  " + key + "  2 obs  1 sessions  0 prompts\n", nil
	}
	r.runner.Handlers["engram export"] = func(a []string) (string, error) {
		writeFile(t, a[1], engramExport(t, key))
		return "Exported to " + a[1] + "\n  Sessions: 1\n  Observations: 2\n  Prompts: 0\n", nil
	}
	engramBefore := treeHash(t, filepath.Join(r.home, ".engram"))
	o := hostOpts()
	o.ImportEngram = true
	inv, pl, err := r.plan(o)
	if err != nil {
		t.Fatal(err)
	}
	if pl.Team.Import == nil || pl.Team.Import.Project != key {
		t.Fatalf("import plan: %+v (notes %v)", pl.Team.Import, pl.Notes)
	}
	res, err := Run(context.Background(), r.env, inv, pl, nil, Hooks{})
	if err != nil {
		t.Fatalf("%v", err)
	}
	if res.Import == nil || res.Import.Imported != 2 || !res.Import.Verified || res.Import.Status != "complete" {
		t.Fatalf("%+v", res.Import)
	}
	rs, _ := r.st.Search(context.Background(), memory.Query{Text: "postgresql", Project: inv.Project.Key})
	if len(rs) != 1 || rs[0].Actor != "legacy" || rs[0].Source != "engram/gentle-ai" {
		t.Fatalf("imported: %+v", rs)
	}
	// Engram: only doctor/projects/export ran; no save, no modification of its data.
	if r.runner.Count("engram save") != 0 || treeHash(t, filepath.Join(r.home, ".engram")) != engramBefore {
		t.Fatal("Engram must never be modified")
	}
	if r.runner.Count("engram export") != 1 {
		t.Fatalf("exactly one safety export: %d", r.runner.Count("engram export"))
	}
	// The report is kept with the snapshot.
	if _, err := os.Stat(filepath.Join(res.Snapshot.Dir, "memory", "import-report.json")); err != nil {
		t.Fatal(err)
	}
	// Re-running the import (a second installation attempt) never duplicates.
	inv2, pl2, _ := r.plan(o)
	if _, err := Run(context.Background(), r.env, inv2, pl2, nil, Hooks{}); err != nil {
		t.Fatal(err)
	}
	if st, _ := r.st.Stats(context.Background()); st.Observations != 2 {
		t.Fatalf("no duplicates: %+v", st)
	}
}

func TestHostWithoutAContainerEngineRefusesAndChangesNothing(t *testing.T) {
	r := newTeamRig(t)
	r.noEngine = true
	before := treeHash(t, r.home, r.root)
	inv, pl, err := r.plan(hostOpts())
	if err != nil {
		t.Fatal(err)
	}
	if !pl.HasNote(NoteNoContainerEngine) || pl.TeamBlocker() == "" {
		t.Fatalf("the plan must say there is no engine: %v", pl.Notes)
	}
	if _, err := Run(context.Background(), r.env, inv, pl, nil, Hooks{}); !errors.Is(err, ErrTeamBlocked) {
		t.Fatalf("want ErrTeamBlocked, got %v", err)
	}
	if treeHash(t, r.home, r.root) != before || len(r.eng.Mutations) != 0 {
		t.Fatal("nothing may change, and no engine is installed behind the user's back")
	}
	// The client role and Engram stay available on the same machine.
	c := Options{Memory: "memory-mcp", MemoryRole: "client", MemoryURL: "https://memoria.example.com/mcp", Runtimes: []string{"claude-code"}}
	if _, p, err := r.plan(c); err != nil || p.TeamBlocker() != "" {
		t.Fatalf("client without docker: %v %v", err, p.Notes)
	}
}

func TestRollbackOfAFreshInstallKeepsTheVolumeAndRestoresSecrets(t *testing.T) {
	r := newTeamRig(t)
	// An existing secrets file must come back byte for byte.
	if err := mcp.WriteClientSecret(r.home, "OTHER", "keep-me"); err != nil {
		t.Fatal(err)
	}
	secretsBefore := slurp(t, mcp.ClientSecretsPath(r.home))
	r.failStore = 2 // the first open (tokens) works, the verification open fails
	inv, pl, err := r.plan(hostOpts())
	if err != nil {
		t.Fatal(err)
	}
	res, err := Run(context.Background(), r.env, inv, pl, nil, Hooks{})
	if err == nil || !res.RolledBack {
		t.Fatalf("want a rolled back run, got %v %+v", err, res.Checks)
	}
	if _, ok := r.eng.Containers[pgmanaged.DefaultContainer]; ok {
		t.Fatal("the container created by this run must be removed on rollback")
	}
	if _, ok := r.eng.Volumes[pgmanaged.DefaultVolume]; !ok {
		t.Fatal("the data volume must be KEPT on rollback")
	}
	if slurp(t, mcp.ClientSecretsPath(r.home)) != secretsBefore {
		t.Fatal("the secrets file must be restored")
	}
	if _, err := os.Stat(gabitotl.NewLayout(r.root).Memory()); err == nil {
		t.Fatal("memory.md created by the run must be gone")
	}
	for _, m := range r.eng.Mutations {
		if m.Verb == "volume-rm" {
			t.Fatal("a rollback never deletes a volume")
		}
	}
}

func TestRollbackLeavesAPreExistingServerAlone(t *testing.T) {
	r := newTeamRig(t)
	mgr, _ := r.env.Team.Manager("", "auto", 0)
	if _, err := mgr.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	mutBefore := len(r.eng.Mutations)
	r.failStore = 2
	inv, pl, _ := r.plan(hostOpts())
	if _, err := Run(context.Background(), r.env, inv, pl, nil, Hooks{}); err == nil {
		t.Fatal("expected a failure")
	}
	if c := r.eng.Containers[pgmanaged.DefaultContainer]; c == nil || !c.Running {
		t.Fatal("a server that existed before must not be removed by a rollback")
	}
	for _, m := range r.eng.Mutations[mutBefore:] {
		if m.Verb != "exec" { // the readiness probe is the only thing a re-install may run
			t.Fatalf("a rollback must not touch a pre-existing server: %+v", m)
		}
	}
}

func TestClientInstallValidatesAndStoresTheTokenSafely(t *testing.T) {
	r := newTeamRig(t)
	r.vars[DefaultTokenEnv] = "gbt_0123456789abcdef.abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG"
	o := Options{Memory: "memory-mcp", MemoryRole: "client", MemoryURL: "https://memoria.example.com/mcp", StoreToken: true, Runtimes: []string{"claude-code"}}
	inv, pl, err := r.plan(o)
	if err != nil {
		t.Fatal(err)
	}
	var doctorSaw gabitotl.MemoryConfig
	r.doctor = func(c gabitotl.MemoryConfig) memory.Report {
		doctorSaw = c
		return memory.Report{Provider: "memory-mcp", Checks: []memory.Check{{Name: "server", Status: memory.StatusOK}}}
	}
	res, err := Run(context.Background(), r.env, inv, pl, nil, Hooks{})
	if err != nil {
		t.Fatalf("%v %+v", err, res.Checks)
	}
	if doctorSaw.URL != "https://memoria.example.com/mcp" || doctorSaw.Token != "env:"+DefaultTokenEnv || doctorSaw.Role != "client" {
		t.Fatalf("memory doctor must validate the written config: %+v", doctorSaw)
	}
	if v := (mcp.Resolver{Home: r.home, Getenv: func(string) string { return "" }}).Lookup(DefaultTokenEnv); v != r.vars[DefaultTokenEnv] {
		t.Fatal("the token must be in the 0600 secrets file")
	}
	if walkContains(t, "abcdefghijklmnopqrstuvwxyz", r.root, filepath.Join(r.home, ".gabitotl", "migrations"), filepath.Join(r.home, ".gabitotl", "logs"), filepath.Join(r.home, ".claude")) {
		t.Fatal("the token leaked")
	}
	if len(r.eng.Mutations) != 0 || res.Server != nil {
		t.Fatal("a client never touches containers")
	}
}

func TestClientDoctorFailureRollsBackButAMissingTokenOnlyWarns(t *testing.T) {
	r := newTeamRig(t)
	o := Options{Memory: "memory-mcp", MemoryRole: "client", MemoryURL: "https://memoria.example.com/mcp", Runtimes: []string{"claude-code"}}
	inv, pl, _ := r.plan(o)
	r.doctor = func(gabitotl.MemoryConfig) memory.Report {
		return memory.Report{Provider: "memory-mcp", Checks: []memory.Check{{Name: "server", Status: memory.StatusError, Message: "the memory server is unreachable"}}}
	}
	res, err := Run(context.Background(), r.env, inv, pl, nil, Hooks{})
	if err == nil || !res.RolledBack {
		t.Fatalf("an unreachable server must roll back: %v", err)
	}
	if _, err := os.Stat(filepath.Join(r.root, ".gabitotl", "memory.md")); err == nil {
		t.Fatal("memory.md must be gone after the rollback")
	}
	// A token that is simply not exported yet is a warning, not a failure.
	r2 := newTeamRig(t)
	r2.doctor = func(gabitotl.MemoryConfig) memory.Report {
		return memory.Report{Provider: "memory-mcp", Checks: []memory.Check{{Name: "connection", Status: memory.StatusError, Message: "environment variable X is not set"}}}
	}
	inv2, pl2, _ := r2.plan(o)
	if _, err := Run(context.Background(), r2.env, inv2, pl2, nil, Hooks{}); err != nil {
		t.Fatalf("a missing token only warns: %v", err)
	}
}

func TestTeamOptionValidation(t *testing.T) {
	r := newTeamRig(t)
	base := Options{Memory: "memory-mcp", Runtimes: []string{"claude-code"}}
	bad := map[string]func(*Options){
		"client without url": func(o *Options) { o.MemoryRole = "client" },
		"literal token as env": func(o *Options) {
			o.MemoryRole = "client"
			o.MemoryURL = "https://x/mcp"
			o.MemoryTokenEnv = "gbt_0123456789abcdef.secretsecretsecretsecretsecret"
		},
		"plain http to a remote":   func(o *Options) { o.MemoryRole = "client"; o.MemoryURL = "http://memoria.example.com/mcp" },
		"url with credentials":     func(o *Options) { o.MemoryRole = "client"; o.MemoryURL = "https://u:p@memoria.example.com/mcp" },
		"client import":            func(o *Options) { o.MemoryRole = "client"; o.MemoryURL = "https://x/mcp"; o.ImportEngram = true },
		"client invites":           func(o *Options) { o.MemoryRole = "client"; o.MemoryURL = "https://x/mcp"; o.Invites = []string{"a"} },
		"client store without var": func(o *Options) { o.MemoryRole = "client"; o.MemoryURL = "https://x/mcp"; o.StoreToken = true },
		"unknown role":             func(o *Options) { o.MemoryRole = "boss" },
		"host bad invite":          func(o *Options) { o.MemoryRole = "host"; o.User = "gabi"; o.Invites = []string{"legacy"} },
		"role without team memory": func(o *Options) { o.Memory = "engram"; o.MemoryRole = "host"; o.User = "gabi" },
		"import without a role":    func(o *Options) { o.ImportEngram = true },
	}
	for name, mut := range bad {
		o := base
		mut(&o)
		if _, _, err := r.plan(o); !errors.Is(err, ErrTeamOptions) {
			t.Errorf("%s: want ErrTeamOptions, got %v", name, err)
		}
	}
	// A host without --user takes the identity of the OS user.
	t.Setenv("GABITOTL_USER", "ana")
	h := base
	h.MemoryRole = "host"
	if _, pl, err := r.plan(h); err != nil || pl.Options.User != "ana" {
		t.Fatalf("host without --user: %v %q", err, pl.Options.User)
	}
	// A URL alone implies the client role.
	o := base
	o.MemoryURL = "https://memoria.example.com/mcp"
	if _, pl, err := r.plan(o); err != nil || pl.Team == nil || pl.Team.Role != "client" {
		t.Fatalf("%v %+v", err, pl.Team)
	}
	// No role at all keeps the older behavior with an honest note.
	if _, pl, err := r.plan(base); err != nil || pl.Team != nil || !pl.HasNote(NoteTeamUnconfigured) {
		t.Fatalf("%v %+v", err, pl.Notes)
	}
}

func TestImportUnavailableIsAnHonestNote(t *testing.T) {
	r := newTeamRig(t)
	r.bins["engram"] = false // data but no binary: no safety export is possible
	o := hostOpts()
	o.ImportEngram = true
	_, pl, err := r.plan(o)
	if err != nil {
		t.Fatal(err)
	}
	if pl.Team.Import != nil || !pl.HasNote(NoteImportUnavailable) {
		t.Fatalf("%+v %v", pl.Team.Import, pl.Notes)
	}
}

func TestDoctorCoversTeamMemory(t *testing.T) {
	r := newTeamRig(t)
	o := hostOpts()
	inv, pl, _ := r.plan(o)
	if _, err := Run(context.Background(), r.env, inv, pl, nil, Hooks{}); err != nil {
		t.Fatal(err)
	}
	r.doctor = func(gabitotl.MemoryConfig) memory.Report {
		return memory.Report{Provider: "memory-mcp", Checks: []memory.Check{
			{Name: "server", Status: memory.StatusOK, Message: "server answered"},
			{Name: "authentication", Status: memory.StatusOK, Message: "the token was accepted"},
			{Name: "server schema", Status: memory.StatusOK, Message: "schema version 3"},
			{Name: "project", Status: memory.StatusError, Message: "not allowed"},
		}}
	}
	before := treeHash(t, r.home, r.root)
	d := Doctor(context.Background(), r.env)
	var lines []Line
	for _, s := range d.Sections {
		if s.Name == "memory" {
			lines = s.Lines
		}
	}
	has := func(code, status, sub string) bool {
		for _, l := range lines {
			if l.Code == code && l.Status == status && strings.Contains(l.Detail, sub) {
				return true
			}
		}
		return false
	}
	if !has(DocMemTeamOK, LineOK, "schema version 3") || !has(DocMemTeamOK, LineOK, "authentication") || !has(DocMemTeamBad, LineError, "project: not allowed") {
		t.Fatalf("lines: %+v", lines)
	}
	if !has(DocMemTeamContainer, LineOK, "running yes") || !has(DocMemTeamContainer, LineOK, "ready yes") {
		t.Fatalf("a host must show the managed container: %+v", lines)
	}
	if d.Errors() == 0 {
		t.Fatal("a failing team check is an error")
	}
	if treeHash(t, r.home, r.root) != before {
		t.Fatal("doctor is read-only")
	}
}
