package installer

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/identity"
	"github.com/winnstorm/gabitotl/internal/memory"
	"github.com/winnstorm/gabitotl/internal/memory/mcp"
	"github.com/winnstorm/gabitotl/internal/memory/migrate"
	"github.com/winnstorm/gabitotl/internal/memory/store"
	"github.com/winnstorm/gabitotl/internal/pgmanaged"
	rt "github.com/winnstorm/gabitotl/internal/runtime"
)

// Team memory: provider memory-mcp, served by GabitoTL's own Memory MCP server
// over a managed, isolated PostgreSQL (internal/pgmanaged). Two roles:
//
//   - host: this machine runs the server. The pgmanaged plan goes through the
//     same pipeline as everything else (snapshot first, verify, rollback on
//     failure). Rollback of a run that created the container removes the
//     container and its network and KEEPS the volume and the credentials; a
//     server that already existed is never touched by a rollback.
//   - client: this machine only connects to an existing server (URL and the NAME
//     of the variable holding the token; the token itself never goes into the
//     repository).
//
// No role (memory-mcp chosen without one) keeps the older behavior: team mode is
// recorded and the bridge entry is configured, but the connection is left for
// the user to complete.

// Defaults of the team setup.
const (
	DefaultTokenEnv  = "GABITOTL_MEMORY_TOKEN"
	DefaultServerURL = "http://127.0.0.1:8765/mcp"
)

// DefaultDatabaseRef is the reference the host's memory.md keeps for the database
// connection (resolved by `memory serve`; never a connection string).
const DefaultDatabaseRef = "env:GABITOTL_MEMORY_DATABASE_URL"

// PhaseServer is the phase that installs the managed server and issues tokens.
const PhaseServer Phase = "memory-server"

// Errors of team option validation.
var ErrTeamOptions = errors.New("invalid team memory options")

// Plan notes of the team setup.
const (
	NoteNoContainerEngine = "no-container-engine"
	NoteServerConflict    = "server-conflict"
	NoteTeamUnconfigured  = "team-unconfigured"
	NoteImportUnavailable = "import-unavailable"
	NoteImportNeedsHost   = "import-needs-host"
	NoteTeamNotReady      = "team-not-ready"
	NoteTeamURLMissing    = "team-url-missing"
	NoteTeamTokenMissing  = "team-token-missing"
)

// Notices of a run (Result.Notices); presenters have a note.<code> message for each.
const (
	NoticeImportSkippedHost    = "import-skipped-host"
	NoticeImportSkippedClient  = "import-skipped-client"
	NoticeImportSkippedNoToken = "import-skipped-no-token"
)

var envNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// TeamEnv carries the team-memory dependencies. A nil hook disables the part
// that needs it, and the plan says so.
type TeamEnv struct {
	// Manager builds the managed-PostgreSQL manager (a fake engine in tests).
	Manager func(instance, engine string, port int) (*pgmanaged.Manager, error)
	// OpenStore opens the team database of this machine (host role): the managed
	// credentials resolve the connection. The returned func closes it.
	OpenStore func(ctx context.Context, instance string) (store.Store, func(), error)
	// NewToken mints a project-scoped token for user and returns its plaintext.
	NewToken func(ctx context.Context, ts store.TokenStore, user string, projects []string) (string, error)
}

// ServerPlan is the read-only plan of the managed server (host role).
type ServerPlan struct {
	Plan     pgmanaged.Plan
	Instance string
	Engine   string
	Port     int
}

// ImportPlan describes the Engram import that would run (host role).
type ImportPlan struct {
	Observations int
	Project      string // Engram project
	Target       string // project key in the team memory
}

// TeamPlan is the team part of a Plan; nil unless a role was chosen.
type TeamPlan struct {
	Role string
	// URL is the address teammates use: the shared memory.md url. A client connects
	// to it; a host may not know it yet (""), and then nothing is shared.
	URL string
	// LocalURL is what this machine's own process uses when it differs (host role:
	// the loopback address of the server it runs). It never reaches the shared file.
	LocalURL   string
	TokenEnv   string
	StoreToken bool
	Server     *ServerPlan
	Import     *ImportPlan
	Invites    []string
	// Unchanged is true when this run would find the team setup already in place
	// (memory.md, server and credentials) and change nothing: a re-run then is a
	// no-op with no new snapshot.
	Unchanged bool
}

// instance is the named managed instance this setup serves: only a host has one
// ("" is the default instance).
func (t *TeamPlan) instance() string {
	if t.Role == gabitotl.RoleHost && t.Server != nil {
		return t.Server.Instance
	}
	return ""
}

// ServerResult is what the host setup did.
type ServerResult struct {
	Created     bool
	Port        int
	SecretsPath string
	Engine      string
}

// TeamResult is the connection summary presenters print.
type TeamResult struct {
	Role, URL, TokenEnv, Project string
	// Instance is the named managed instance a host serves ("" for the default).
	Instance string
}

// IssuedToken is a token created for a teammate. It is held in memory only:
// presenters show it once and nothing persists or logs it.
type IssuedToken struct {
	User     string
	Token    string
	Projects []string
}

func validTokenEnv(n string) bool { return envNameRe.MatchString(n) }

func checkEndpoint(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return fmt.Errorf("%w: the server url must look like https://host:port/mcp (no credentials), got %q", ErrTeamOptions, raw)
	}
	return nil
}

// planTeam fills pl.Team. Validation errors abort BuildPlan; everything the
// machine lacks (no container engine, name conflicts) becomes a note, so a dry
// run can still describe what would happen.
func planTeam(ctx context.Context, env Env, inv Inventory, opts Options, pl *Plan) error {
	if pl.MemoryChoice != gabitotl.MemoryMemoryMCP {
		if opts.MemoryRole != "" || opts.ImportEngram {
			return fmt.Errorf("%w: --memory-role and --import-engram need --memory memory-mcp", ErrTeamOptions)
		}
		return nil
	}
	role := opts.MemoryRole
	var current gabitotl.MemoryConfig
	if inv.Project.Exists {
		current, _ = gabitotl.LoadMemory(gabitotl.NewLayout(env.ProjectRoot))
	}
	if role == "" && opts.MemoryURL != "" {
		role = gabitotl.RoleClient
		if current.Role == gabitotl.RoleHost { // `--url` on a host sets the address teammates use
			role = gabitotl.RoleHost
		}
	}
	if role == "" && opts.MemoryURL == "" && current.AwaitsClientSetup() {
		// A teammate after `git pull`: the shared file names the server, nothing is
		// configured for this person. The role is client and the url is the shared one.
		role, opts.MemoryURL = gabitotl.RoleClient, current.URL
	}
	switch role {
	case "":
		pl.note(NoteTeamUnconfigured, "")
		if opts.ImportEngram {
			return fmt.Errorf("%w: --import-engram needs the host role (--memory-role host)", ErrTeamOptions)
		}
		return nil
	case gabitotl.RoleHost, gabitotl.RoleClient:
	default:
		return fmt.Errorf("%w: --memory-role must be host or client, got %q", ErrTeamOptions, role)
	}
	t := &TeamPlan{Role: role, URL: opts.MemoryURL, TokenEnv: opts.MemoryTokenEnv, StoreToken: opts.StoreToken || opts.TokenSecret != "", Invites: opts.Invites}
	if t.TokenEnv == "" {
		t.TokenEnv = DefaultTokenEnv
	}
	if !validTokenEnv(t.TokenEnv) {
		return fmt.Errorf("%w: the token variable must be a NAME like %s, never the token itself", ErrTeamOptions, DefaultTokenEnv)
	}
	pl.Team = t
	switch role {
	case gabitotl.RoleClient:
		if t.URL == "" {
			return fmt.Errorf("%w: --memory-role client needs --memory-url", ErrTeamOptions)
		}
		if err := checkEndpoint(t.URL); err != nil {
			return err
		}
		if err := mcp.CheckURL(t.URL, false); err != nil {
			return fmt.Errorf("%w: %v", ErrTeamOptions, err)
		}
		if opts.ImportEngram {
			pl.note(NoteImportNeedsHost, "")
			return fmt.Errorf("%w: importing Engram needs the database, so it is done on the machine that hosts the server (gabitotl memory migrate engram); a client cannot import", ErrTeamOptions)
		}
		if len(t.Invites) > 0 {
			return fmt.Errorf("%w: only the host issues tokens for teammates", ErrTeamOptions)
		}
		if t.StoreToken && opts.TokenSecret == "" && env.Var(t.TokenEnv) == "" {
			return fmt.Errorf("%w: --memory-store-token copies the token from the environment variable %s, which is not set", ErrTeamOptions, t.TokenEnv)
		}
		if !t.StoreToken && env.Var(t.TokenEnv) == "" {
			pl.note(NoteTeamTokenMissing, t.TokenEnv)
		}
	case gabitotl.RoleHost:
		// opts.MemoryURL is the address teammates use. It is what the shared file
		// gets, so a loopback address is refused: nobody else could reach it. Without
		// one, the host is set up locally and nothing is shared (see the note).
		t.LocalURL = DefaultServerURL
		if t.URL == "" && inv.Project.Exists {
			if o := gabitotl.InspectMemory(gabitotl.NewLayout(env.ProjectRoot), identity.OSUserID()); o.SharedURL != "" && !gabitotl.IsLoopbackURL(o.SharedURL) {
				t.URL = o.SharedURL // an earlier run already shared it
			}
		}
		if t.URL != "" {
			if err := ValidateTeamHostURL(t.URL); err != nil {
				if errors.Is(err, ErrTeamOptions) {
					return err
				}
				return fmt.Errorf("%w: %v", ErrTeamOptions, err)
			}
		} else {
			pl.note(NoteTeamURLMissing, "")
		}
		if opts.User == "" {
			// The identity is the OS user: the host needs no extra question.
			opts.User = identity.OSUserID()
			pl.Options.User = opts.User
		}
		if opts.User == "" {
			return fmt.Errorf("%w: the host creates its own token, so it needs --user", ErrTeamOptions)
		}
		for _, u := range t.Invites {
			if !gabitotl.ValidID(u) || u == memory.ActorLegacy {
				return fmt.Errorf("%w: %q is not a valid user id", ErrTeamOptions, u)
			}
		}
		sp := &ServerPlan{Instance: opts.ServerInstance, Engine: opts.ServerEngine, Port: opts.ServerPort}
		pl.Team.Server = sp
		if env.Team.Manager == nil {
			pl.note(NoteNoContainerEngine, "no container support in this build")
		} else if m, err := env.Team.Manager(sp.Instance, sp.Engine, sp.Port); err != nil {
			return fmt.Errorf("%w: %v", ErrTeamOptions, err)
		} else if p, err := m.Plan(ctx); err != nil {
			return fmt.Errorf("memory server plan: %w", err)
		} else {
			sp.Plan = p
			switch {
			case p.EngineErr != nil:
				pl.note(NoteNoContainerEngine, "")
			case len(p.Conflicts) > 0:
				pl.note(NoteServerConflict, strings.Join(p.Conflicts, "; "))
			}
			for _, s := range p.Steps {
				pl.Items = append(pl.Items, PlanItem{Kind: serverKind(s.Action), Target: "servidor de memoria: " + s.Resource + " " + s.Name, Detail: s.Detail})
			}
		}
	}
	t.Unchanged = teamUnchanged(ctx, env, inv, opts, t)
	if opts.ImportEngram {
		switch {
		case role != gabitotl.RoleHost:
		case !inv.Memory.Present():
			pl.note(NoteImportUnavailable, "no Engram memory was found")
		case !inv.Memory.HasBinary:
			pl.note(NoteImportUnavailable, "Engram data was found but not its binary, so no safety export can be made")
		case inv.Memory.ProjectMatch == "":
			pl.note(NoteImportUnavailable, "no Engram project matches this project; use `gabitotl memory migrate engram --project NAME`")
		default:
			if _, err := env.Migrators.Lookup(gabitotl.MemoryEngram, gabitotl.MemoryMemoryMCP); err != nil {
				pl.note(NoteImportUnavailable, "no tested migrator")
			} else {
				pl.Team.Import = &ImportPlan{Observations: inv.Memory.Observations, Project: inv.Memory.ProjectMatch, Target: inv.Project.Key}
				pl.Items = append(pl.Items, PlanItem{Kind: KindImport, Target: "memoria de Engram -> memoria compartida", Count: inv.Memory.Observations})
			}
		}
	}
	return nil
}

// teamUnchanged reports whether the team setup of this run is already in place
// exactly as requested, so applying it would change nothing. It only reads.
func teamUnchanged(ctx context.Context, env Env, inv Inventory, opts Options, t *TeamPlan) bool {
	if opts.ImportEngram || t.Import != nil || len(t.Invites) > 0 || t.StoreToken || !inv.Project.Exists {
		return false
	}
	l := gabitotl.NewLayout(env.ProjectRoot)
	cfg, err := gabitotl.LoadMemory(l)
	want := t.URL
	if t.Role == gabitotl.RoleHost {
		want = t.LocalURL
	}
	if err != nil || cfg.Mode != gabitotl.ModeTeam || cfg.Provider != gabitotl.MemoryMemoryMCP ||
		cfg.URL != want || cfg.Token != "env:"+t.TokenEnv || cfg.Role != t.Role || cfg.Instance != t.instance() {
		return false
	}
	if o := gabitotl.InspectMemory(l, identity.OSUserID()); len(o.LegacyFields) > 0 || !o.HasLocal || (t.Role == gabitotl.RoleHost && t.URL != "" && o.SharedURL != t.URL) {
		return false // per-machine fields still in the shared file, or a team url to share
	}
	if t.Role == gabitotl.RoleClient {
		return cfg.PostgresConnection == "" // an older client file still names a database: tidy it
	}
	if t.Server == nil || t.Server.Plan.EngineErr != nil || len(t.Server.Plan.Conflicts) > 0 {
		return false
	}
	for _, s := range t.Server.Plan.Steps {
		if s.Action == "create" || s.Action == "start" {
			return false
		}
	}
	if env.Team.OpenStore == nil || inv.Project.Key == "" {
		return false
	}
	st, closeFn, err := env.Team.OpenStore(ctx, t.Server.Instance) // read-only: is the stored token still valid?
	if err != nil {
		return false
	}
	defer closeFn()
	_, ok := reusableToken(ctx, env, st, t.TokenEnv, opts.User, inv.Project.Key)
	return ok
}

func serverKind(action string) rt.Kind {
	switch action {
	case "reuse":
		return rt.NoAction
	case "start":
		return rt.Modify
	}
	return rt.Create
}

// teamNeedsServer reports whether the run installs the managed server.
func (p Plan) teamNeedsServer() bool {
	return p.Team != nil && p.Team.Role == gabitotl.RoleHost && p.Team.Server != nil
}

// TeamBlocker explains why a host setup cannot run on this machine ("" when it can).
func (p Plan) TeamBlocker() string {
	switch {
	case p.HasNote(NoteNoContainerEngine):
		return NoteNoContainerEngine
	case p.HasNote(NoteServerConflict):
		return NoteServerConflict
	}
	return ""
}

// teamConfig applies the team fields to memory.md. References only.
func (r *run) teamConfig(l gabitotl.Layout) error {
	t := r.pl.Team
	if t == nil {
		return nil
	}
	cfg, err := gabitotl.LoadMemory(l)
	if err != nil {
		return err
	}
	cfg.Mode, cfg.Provider = gabitotl.ModeTeam, gabitotl.MemoryMemoryMCP
	cfg.Token, cfg.Role = "env:"+t.TokenEnv, t.Role
	cfg.Instance = t.instance()
	teamURL := "" // what the shared file gets besides what SaveMemory infers
	if t.Role == gabitotl.RoleClient {
		cfg.URL = t.URL
		cfg.PostgresConnection = "" // a client has no database: it only holds a url and a token reference
	} else {
		cfg.URL, teamURL = t.LocalURL, t.URL // the host runs against its own address; teammates get the team one
		if cfg.PostgresConnection == "" {
			cfg.PostgresConnection = DefaultDatabaseRef
		}
	}
	// Role, token, database and a loopback url go to this person's local file; the
	// committed memory.md only gets the team-reachable url.
	return gabitotl.SaveMemoryTeamURL(l, identity.OSUserID(), cfg, teamURL)
}

// secretBackup remembers the client secrets file so a rollback restores it.
type secretBackup struct {
	taken   bool
	existed bool
	data    []byte
}

func (r *run) backupSecrets() {
	if r.secrets.taken {
		return
	}
	r.secrets.taken = true
	b, err := os.ReadFile(mcp.ClientSecretsPath(r.env.Home))
	r.secrets.existed, r.secrets.data = err == nil, b
}

func (r *run) restoreSecrets() {
	if !r.secrets.taken {
		return
	}
	p := mcp.ClientSecretsPath(r.env.Home)
	if !r.secrets.existed {
		_ = os.Remove(p)
		return
	}
	_ = os.WriteFile(p, r.secrets.data, 0o600)
}

// storeToken writes this machine's token to the 0600 secrets file.
func (r *run) storeToken(value string) error {
	r.backupSecrets()
	return mcp.WriteClientSecret(r.env.Home, r.pl.Team.TokenEnv, value)
}

func (r *run) timeout(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, d)
}

// setupServer is phase "memory-server": install the managed PostgreSQL, then
// issue this developer's token (stored in the 0600 secrets file) and, for the
// host, the tokens of the invited teammates (returned once, never persisted).
func (r *run) setupServer(ctx context.Context) error {
	t := r.pl.Team
	if t == nil {
		return nil
	}
	if t.Role == gabitotl.RoleClient {
		if t.StoreToken {
			r.phase(PhaseServer)
			v := r.pl.Options.TokenSecret
			if v == "" {
				v = r.env.Var(t.TokenEnv)
			}
			if err := r.storeToken(v); err != nil {
				return fmt.Errorf("store token: %w", err)
			}
			r.emit(PhaseServer, "ok", "store token", "0600 secrets file")
		}
		return nil
	}
	if t.Server == nil {
		return nil
	}
	r.phase(PhaseServer)
	if blocker := r.pl.TeamBlocker(); blocker != "" {
		return fmt.Errorf("the managed server cannot be installed here (%s)", blocker)
	}
	mgr, err := r.env.Team.Manager(t.Server.Instance, t.Server.Engine, t.Server.Port)
	if err != nil {
		return err
	}
	res, err := mgr.Install(ctx)
	if res.Created {
		r.serverMgr, r.serverCreated = mgr, true
	}
	if err != nil {
		return fmt.Errorf("install the managed PostgreSQL: %w", err)
	}
	r.res.Server = &ServerResult{Created: res.Created, Port: res.Port, SecretsPath: res.SecretsPath, Engine: res.Plan.Engine.Name}
	r.emit(PhaseServer, "ok", "managed PostgreSQL", fmt.Sprintf("127.0.0.1:%d", res.Port))

	if r.env.Team.OpenStore == nil || r.env.Team.NewToken == nil {
		return errors.New("no way to open the team database from this build")
	}
	st, closeFn, err := r.env.Team.OpenStore(ctx, t.Server.Instance)
	if err != nil {
		return fmt.Errorf("open the team database: %w", err)
	}
	defer closeFn()
	projects := []string{r.inv.Project.Key}
	if projects[0] == "" {
		return errors.New("the project has no key to scope the tokens to")
	}
	if err := r.hostToken(ctx, st, t.Server.Instance, r.pl.Options.User, projects); err != nil {
		return fmt.Errorf("your token: %w", err)
	}
	for _, u := range t.Invites {
		it, err := r.env.Team.NewToken(ctx, st, u, projects)
		if err != nil {
			return fmt.Errorf("create the token of %s: %w", u, err)
		}
		r.res.Tokens = append(r.res.Tokens, IssuedToken{User: u, Token: it, Projects: projects})
	}
	if len(t.Invites) > 0 {
		r.emit(PhaseServer, "ok", "teammate tokens", fmt.Sprintf("%d (shown once, never stored)", len(t.Invites)))
	}
	return nil
}

// rollbackTeam reverts the team parts of a failed run. A server that existed
// before is left alone; one this run created loses its container and network
// but keeps the volume and the credentials (so a retry reuses them and no data
// is ever deleted by a rollback).
func (r *run) rollbackTeam(ctx context.Context) {
	r.restoreSecrets()
	r.discardNewToken(context.WithoutCancel(ctx))
	if r.serverCreated && r.serverMgr != nil {
		if _, err := r.serverMgr.Remove(context.WithoutCancel(ctx), pgmanaged.RemoveOptions{}); err != nil {
			r.warn("rollback: managed server: %v", err)
		} else {
			r.warn("the managed server created by this run was removed; its data volume and credentials were kept (`gabitotl memory server remove --purge-data` deletes them)")
		}
	}
}

// importEngram runs the tested migrator over the snapshot's safety export.
func (r *run) importEngram(ctx context.Context) error {
	t := r.pl.Team
	if t == nil || t.Import == nil {
		return nil
	}
	m, err := r.env.Migrators.Lookup(gabitotl.MemoryEngram, gabitotl.MemoryMemoryMCP)
	if err != nil {
		return err
	}
	if r.snap == nil || !r.snap.Manifest.Engram.Done {
		return fmt.Errorf("%w (the snapshot has no Engram export)", migrate.ErrNoBackup)
	}
	export := filepath.Join(r.snap.Dir, r.snap.Manifest.Engram.File)
	res, err := m.Migrate(ctx, migrate.Request{
		ExportPath: export, BackupPath: export, ReportPath: filepath.Join(r.snap.Dir, "memory", "import-report.json"),
		Project: t.Import.Project, TargetProject: t.Import.Target, StoreInstance: t.Server.Instance,
	})
	rep := res.Report
	r.res.Import = &rep
	if err != nil {
		return err
	}
	r.emit(PhaseMigrate, "ok", "import engram", fmt.Sprintf("%d imported, %d already present", rep.Imported, rep.AlreadyPresent))
	if !rep.Verified {
		return fmt.Errorf("the import could not be verified (%d/%d sampled entries matched)", rep.SampleOK, rep.SampleChecked)
	}
	return nil
}

// verifyTeam checks the team connection: the database for a host (the server
// process may not be running yet), the server for a client.
func (r *run) verifyTeam(ctx context.Context, group string) []Check {
	t := r.pl.Team
	if t == nil || r.env.ProjectRoot == "" {
		return nil
	}
	var out []Check
	if t.Role == gabitotl.RoleHost && r.env.Team.OpenStore != nil && r.res.Server != nil {
		st, closeFn, err := r.env.Team.OpenStore(ctx, t.Server.Instance)
		if err != nil {
			return append(out, Check{Group: group, Name: "team database", OK: false, Detail: err.Error()})
		}
		defer closeFn()
		rep := st.Doctor(ctx)
		out = append(out, Check{Group: group, Name: "team database", OK: rep.OK(), Detail: failedMemory(rep.Checks)})
	}
	cfg, err := gabitotl.LoadMemory(gabitotl.NewLayout(r.env.ProjectRoot))
	if err != nil || r.env.OpenMemory == nil {
		return out
	}
	p, err := r.env.OpenMemory(cfg)
	if err != nil {
		return append(out, Check{Group: group, Name: "memory doctor", OK: false, Detail: err.Error()})
	}
	dctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	rep := p.Doctor(dctx)
	bad := failedMemory(rep.Checks)
	switch {
	case rep.OK():
		out = append(out, Check{Group: group, Name: "memory doctor", OK: true, Detail: "server answered"})
	case t.Role == gabitotl.RoleHost:
		// The server process is started by the user (`gabitotl memory serve`).
		out = append(out, Check{Group: group, Name: "memory doctor", OK: true, Detail: "the server is not running yet: " + bad})
	default:
		for _, c := range rep.Checks {
			if c.Name == "connection" && c.Status == memory.StatusError { // the token is not exported in this shell yet
				return append(out, Check{Group: group, Name: "memory doctor", OK: true, Detail: "could not reach the server yet: " + c.Message + "; the setup is written, export the token and run `gabitotl memory doctor`"})
			}
		}
		out = append(out, Check{Group: group, Name: "memory doctor", OK: false, Detail: bad})
	}
	return out
}

// ValidateTeamURL checks a server endpoint the way the plan does (for forms).
func ValidateTeamURL(raw string) error {
	if err := checkEndpoint(raw); err != nil {
		return err
	}
	if err := mcp.CheckURL(raw, false); err != nil {
		return err
	}
	return nil
}

// ValidateTeamHostURL checks the address a host announces to its teammates: a
// valid server endpoint that is not loopback, since only the host could use it.
func ValidateTeamHostURL(raw string) error {
	if err := checkEndpoint(raw); err != nil {
		return err
	}
	if gabitotl.IsLoopbackURL(raw) {
		return fmt.Errorf("%q points at this machine, so teammates cannot use it: give the address they reach the server at, or leave it out to set up locally and share it later", raw)
	}
	return mcp.CheckURL(raw, false)
}

// ValidTokenEnv reports whether n is a variable NAME (never a token).
func ValidTokenEnv(n string) bool { return validTokenEnv(n) }

// ValidTeamUser reports whether id can own a token.
func ValidTeamUser(id string) bool { return gabitotl.ValidID(id) && id != memory.ActorLegacy }
