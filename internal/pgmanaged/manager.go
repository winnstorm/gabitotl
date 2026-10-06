package pgmanaged

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/winnstorm/gabitotl/internal/memory"
)

// ErrNotInstalled means the managed container does not exist.
var ErrNotInstalled = errors.New("pgmanaged: the managed PostgreSQL is not installed (run `gabitotl memory server install`)")

// Manager drives one managed instance. Every dependency is injectable.
type Manager struct {
	Cfg      Config
	Run      Runner
	LookPath func(string) (string, error)
	// PortFree reports whether a loopback port can be bound.
	PortFree func(port int) bool
	Now      func() time.Time
	Sleep    func(ctx context.Context, d time.Duration) error
	Rand     io.Reader
	// ReadyTimeout bounds the wait for the database to accept connections.
	ReadyTimeout time.Duration
}

// New builds a Manager with the real engine runner.
func New(cfg Config) *Manager {
	return &Manager{
		Cfg: cfg, Run: ExecRunner{}, LookPath: exec.LookPath, PortFree: loopbackPortFree,
		Now: func() time.Time { return time.Now().UTC() }, Sleep: sleep, Rand: rand.Reader, ReadyTimeout: 90 * time.Second,
	}
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// loopbackPortFree tries to listen on the port exactly as the published
// binding would (127.0.0.1), so another process, or another container's
// published port, makes it busy.
func loopbackPortFree(port int) bool {
	l, err := net.Listen("tcp", net.JoinHostPort(Bind, strconv.Itoa(port)))
	if err != nil {
		return false
	}
	_ = l.Close()
	return true
}

// Step is one thing Install would do.
type Step struct {
	Action   string // create, reuse, start, wait, write
	Resource string // network, volume, container, credentials
	Name     string
	Detail   string
}

// Plan is the read-only description of what Install would do.
type Plan struct {
	Engine EngineInfo
	// EngineErr is set when no engine is usable (a dry run can still describe the steps).
	EngineErr error
	Port      int
	// PortErr is set when no host port can be used (Install refuses; a dry run still describes the plan).
	PortErr error
	Steps   []Step
	// Conflicts lists resources that carry our name but not our label.
	Conflicts []string
	Notes     []string
}

// Err is non-nil when the plan cannot be executed.
func (p Plan) Err() error {
	switch {
	case len(p.Conflicts) > 0:
		return fmt.Errorf("%w: %s", ErrNotManaged, strings.Join(p.Conflicts, "; "))
	case p.EngineErr != nil:
		return p.EngineErr
	case p.PortErr != nil:
		return p.PortErr
	}
	return nil
}

// Render is the human-readable plan. It never contains credentials.
func (p Plan) Render() string {
	var b strings.Builder
	if p.Engine.Name != "" {
		fmt.Fprintf(&b, "Engine: %s %s\n", p.Engine.Name, p.Engine.Version)
	}
	if p.Port != 0 {
		fmt.Fprintf(&b, "Address: %s:%d (loopback only)\n", Bind, p.Port)
	}
	for _, s := range p.Steps {
		fmt.Fprintf(&b, "  - %s %s %s", s.Action, s.Resource, s.Name)
		if s.Detail != "" {
			fmt.Fprintf(&b, " (%s)", s.Detail)
		}
		b.WriteByte('\n')
	}
	for _, c := range p.Conflicts {
		fmt.Fprintf(&b, "CONFLICT: %s\n", c)
	}
	for _, n := range p.Notes {
		fmt.Fprintf(&b, "Note: %s\n", n)
	}
	if p.EngineErr != nil {
		fmt.Fprintf(&b, "Engine problem: %v\n", p.EngineErr)
	}
	return b.String()
}

func conflict(kind, name string) string {
	return fmt.Sprintf("a %s named %q already exists and was not created by GabitoTL (no %s=true label); GabitoTL will not touch it. Pick another name or remove it yourself if it is yours to remove", kind, name, LabelManaged)
}

// world is what Plan found.
type world struct {
	engine    EngineInfo
	container containerInfo
	volume    resource
	network   resource
}

func (m *Manager) survey(ctx context.Context) (world, error) {
	var w world
	var err error
	if w.engine, err = m.Detect(ctx); err != nil {
		return w, err
	}
	if w.container, err = m.inspectContainer(ctx, w.engine); err != nil {
		return w, err
	}
	if w.volume, err = m.inspectNamed(ctx, w.engine, "volume", m.Cfg.Volume); err != nil {
		return w, err
	}
	if w.network, err = m.inspectNamed(ctx, w.engine, "network", m.Cfg.Network); err != nil {
		return w, err
	}
	return w, nil
}

func (w world) conflicts(c Config) []string {
	var out []string
	if w.container.Exists && !w.container.Managed {
		out = append(out, conflict("container", c.Container))
	}
	if w.volume.Exists && !w.volume.Managed {
		out = append(out, conflict("volume", c.Volume))
	}
	if w.network.Exists && !w.network.Managed {
		out = append(out, conflict("network", c.Network))
	}
	return out
}

// choosePort returns the host port for a new container.
func (m *Manager) choosePort() (int, error) {
	if m.Cfg.Port != 0 {
		if !m.PortFree(m.Cfg.Port) {
			return 0, fmt.Errorf("%w: %d (pick another with --port, or omit it to let GabitoTL choose)", ErrPortInUse, m.Cfg.Port)
		}
		return m.Cfg.Port, nil
	}
	for p := DefaultPort; p < DefaultPort+PortSearchSpan; p++ {
		if m.PortFree(p) {
			return p, nil
		}
	}
	return 0, fmt.Errorf("%w: none free in %d-%d", ErrPortInUse, DefaultPort, DefaultPort+PortSearchSpan-1)
}

// Plan inspects (read-only) and describes what Install would do.
func (m *Manager) Plan(ctx context.Context) (Plan, error) {
	if err := m.Cfg.Validate(); err != nil {
		return Plan{}, err
	}
	w, err := m.survey(ctx)
	if err != nil {
		if errors.Is(err, ErrNoEngine) {
			p := Plan{EngineErr: err}
			p.Notes = append(p.Notes, "no container engine detected; the steps below are what would run once docker or podman is available")
			p.Steps = m.stepsFor(world{}, 0)
			return p, nil
		}
		return Plan{}, err
	}
	p := Plan{Engine: w.engine, Conflicts: w.conflicts(m.Cfg)}
	if len(p.Conflicts) > 0 {
		return p, nil
	}
	if w.container.Exists {
		p.Port = w.container.HostPort
	} else if p.Port, err = m.choosePort(); err != nil {
		p.PortErr = err
		p.Notes = append(p.Notes, err.Error())
	}
	p.Steps = m.stepsFor(w, p.Port)
	if !w.container.Exists {
		p.Notes = append(p.Notes, fmt.Sprintf("the image %s is pulled by the engine if it is not present yet", m.Cfg.Image))
	}
	return p, nil
}

func (m *Manager) stepsFor(w world, port int) []Step {
	step := func(exists bool, kind, name, detail string) Step {
		if exists {
			return Step{Action: "reuse", Resource: kind, Name: name, Detail: "already ours"}
		}
		return Step{Action: "create", Resource: kind, Name: name, Detail: detail}
	}
	steps := []Step{
		step(w.network.Exists, "network", m.Cfg.Network, "label "+LabelManaged+"=true"),
		step(w.volume.Exists, "volume", m.Cfg.Volume, "label "+LabelManaged+"=true; keeps the data"),
	}
	switch {
	case !w.container.Exists:
		steps = append(steps, Step{Action: "create", Resource: "container", Name: m.Cfg.Container,
			Detail: fmt.Sprintf("%s, %s:%d->5432, label %s=true", m.Cfg.Image, Bind, port, LabelManaged)})
	case !w.container.Running:
		steps = append(steps, Step{Action: "start", Resource: "container", Name: m.Cfg.Container})
	default:
		steps = append(steps, Step{Action: "reuse", Resource: "container", Name: m.Cfg.Container, Detail: "already running"})
	}
	steps = append(steps,
		Step{Action: "write", Resource: "credentials", Name: SecretsPath(m.Cfg.Home), Detail: "mode 0600; generated once, never printed"},
		Step{Action: "wait", Resource: "container", Name: m.Cfg.Container, Detail: "until PostgreSQL accepts connections"})
	return steps
}

// InstallResult is what Install did.
type InstallResult struct {
	Plan        Plan
	Port        int
	SecretsPath string
	Created     bool // the container was created by this call
	Ready       bool
}

// Install creates the network, volume and container if they are absent and
// waits for the database. It is idempotent: a second run changes nothing.
func (m *Manager) Install(ctx context.Context) (InstallResult, error) {
	plan, err := m.Plan(ctx)
	if err != nil {
		return InstallResult{Plan: plan}, err
	}
	if err := plan.Err(); err != nil {
		return InstallResult{Plan: plan}, err
	}
	w, err := m.survey(ctx)
	if err != nil {
		return InstallResult{Plan: plan}, err
	}
	res := InstallResult{Plan: plan, Port: plan.Port, SecretsPath: SecretsPath(m.Cfg.Home)}
	creds, err := m.prepareCredentials(w, plan.Port)
	if err != nil {
		return res, err
	}
	if !w.network.Exists {
		if err := m.createLabeled(ctx, w.engine, "network", m.Cfg.Network, RoleNetwork); err != nil {
			return res, err
		}
	}
	if !w.volume.Exists {
		if err := m.createLabeled(ctx, w.engine, "volume", m.Cfg.Volume, RoleVolume); err != nil {
			return res, err
		}
	}
	id := w.container.ID
	switch {
	case !w.container.Exists:
		if id, err = m.runContainer(ctx, w.engine, plan.Port, creds.Password); err != nil {
			return res, err
		}
		res.Created = true
	case !w.container.Running:
		if err := m.start(ctx, w.engine, w.container); err != nil {
			return res, err
		}
	}
	if err := m.waitReady(ctx, w.engine, id); err != nil {
		return res, err
	}
	res.Ready = true
	return res, nil
}

// prepareCredentials decides which password the instance uses and persists
// the connection file. It runs before anything is created, so a failure here
// leaves no half-installed instance.
func (m *Manager) prepareCredentials(w world, port int) (Credentials, error) {
	existing, err := ReadCredentials(m.Cfg.Home)
	haveFile := err == nil
	if err != nil && !errors.Is(err, ErrNoCredentials) {
		return Credentials{}, err
	}
	dataExists := w.container.Exists || w.volume.Exists
	switch {
	case dataExists && !haveFile:
		return Credentials{}, fmt.Errorf("%w: the data volume %q already exists but %s does not. Restore that file, or remove the instance with --purge-data to start over", ErrNoCredentials, m.Cfg.Volume, SecretsPath(m.Cfg.Home))
	case dataExists:
		creds := Credentials{Password: existing.Password, URL: m.Cfg.DatabaseURL(existing.Password, port)}
		if creds.URL != existing.URL {
			if err := WriteCredentials(m.Cfg.Home, creds); err != nil {
				return Credentials{}, err
			}
		}
		return creds, nil
	}
	pw, err := randomPassword(m.Rand)
	if err != nil {
		return Credentials{}, err
	}
	creds := Credentials{Password: pw, URL: m.Cfg.DatabaseURL(pw, port)}
	if err := WriteCredentials(m.Cfg.Home, creds); err != nil {
		return Credentials{}, err
	}
	return creds, nil
}

// createLabeled creates a volume or network carrying our labels. Callers have
// just verified the name is free.
func (m *Manager) createLabeled(ctx context.Context, e EngineInfo, kind, name, role string) error {
	args := append([]string{kind, "create"}, labelArgs(role)...)
	_, err := m.call(ctx, e, nil, append(args, name)...)
	if err != nil && kind == "network" && poolExhausted(err) {
		return m.createNetworkWithSubnet(ctx, e, name, role)
	}
	if err != nil {
		return fmt.Errorf("pgmanaged: create %s: %w", kind, err)
	}
	return nil
}

// poolExhausted recognizes the engines' "no address left for another network"
// failures (a machine with many compose projects hits it).
func poolExhausted(err error) bool {
	l := strings.ToLower(err.Error())
	return strings.Contains(l, "fully subnetted") || strings.Contains(l, "could not find free subnet") ||
		strings.Contains(l, "no available") && strings.Contains(l, "pool")
}

// createNetworkWithSubnet retries with a tiny explicit subnet. The engine
// itself rejects an overlap with any existing network, so no other network is
// inspected: candidates that are taken simply fail and the next one is tried.
func (m *Manager) createNetworkWithSubnet(ctx context.Context, e EngineInfo, name, role string) error {
	var h uint32 = 2166136261
	for _, c := range []byte(name) {
		h = (h ^ uint32(c)) * 16777619
	}
	var last error
	for i := 0; i < 16; i++ {
		third := (int(h) + i*7) & 0xff
		subnet := fmt.Sprintf("10.213.%d.0/28", third)
		args := append([]string{"network", "create", "--subnet", subnet}, labelArgs(role)...)
		if _, err := m.call(ctx, e, nil, append(args, name)...); err == nil {
			return nil
		} else {
			last = err
		}
	}
	return fmt.Errorf("pgmanaged: create network: the engine has no free address range left (%w); remove unused docker networks (`docker network prune` only after checking them) and retry", last)
}

func (m *Manager) runContainer(ctx context.Context, e EngineInfo, port int, password string) (string, error) {
	args := []string{"run", "-d", "--name", m.Cfg.Container}
	args = append(args, labelArgs(RoleContainer)...)
	args = append(args,
		"--network", m.Cfg.Network,
		"--restart", "unless-stopped",
		"--security-opt", "no-new-privileges",
		"-p", fmt.Sprintf("%s:%d:%d", Bind, port, containerPort),
		"-v", m.Cfg.Volume+":/var/lib/postgresql/data",
		// Name-only -e flags: the engine copies the value from the environment of
		// this command, so the password never appears in a process argument.
		"-e", "POSTGRES_PASSWORD",
		"-e", "POSTGRES_USER="+m.Cfg.User,
		"-e", "POSTGRES_DB="+m.Cfg.Database,
		m.Cfg.Image)
	res, err := m.call(ctx, e, []string{"POSTGRES_PASSWORD=" + password}, args...)
	if err != nil {
		// A failed run may leave a created container behind. It is ours (we
		// just named it), but verify before removing.
		if ci, ierr := m.inspectContainer(ctx, e); ierr == nil && ci.Exists && ci.Managed {
			_, _ = m.call(ctx, e, nil, "rm", "-f", ci.ID)
		}
		return "", fmt.Errorf("pgmanaged: create container: %s", redact(err.Error(), password))
	}
	return strings.TrimSpace(string(res.Stdout)), nil
}

func redact(s, secret string) string {
	if secret == "" {
		return s
	}
	return strings.ReplaceAll(s, secret, "***")
}

func (m *Manager) start(ctx context.Context, e EngineInfo, c containerInfo) error {
	if !c.Managed {
		return ErrNotManaged
	}
	if _, err := m.call(ctx, e, nil, "start", c.ID); err != nil {
		return fmt.Errorf("pgmanaged: start: %w", err)
	}
	return nil
}

func (m *Manager) ready(ctx context.Context, e EngineInfo, id string) bool {
	// TCP, not the socket: during first initialization PostgreSQL answers on the
	// socket from a temporary server before the real one listens.
	_, err := m.call(ctx, e, nil, "exec", id, "pg_isready", "-h", "127.0.0.1", "-U", m.Cfg.User, "-d", m.Cfg.Database)
	return err == nil
}

func (m *Manager) waitReady(ctx context.Context, e EngineInfo, id string) error {
	deadline := m.Now().Add(m.ReadyTimeout)
	for {
		if m.ready(ctx, e, id) {
			return nil
		}
		if !m.Now().Before(deadline) {
			return fmt.Errorf("pgmanaged: PostgreSQL did not accept connections within %s (see `gabitotl memory server logs`)", m.ReadyTimeout)
		}
		if err := m.Sleep(ctx, 500*time.Millisecond); err != nil {
			return err
		}
	}
}

// owned inspects the container and demands it is ours.
func (m *Manager) owned(ctx context.Context) (EngineInfo, containerInfo, error) {
	if err := m.Cfg.Validate(); err != nil {
		return EngineInfo{}, containerInfo{}, err
	}
	e, err := m.Detect(ctx)
	if err != nil {
		return e, containerInfo{}, err
	}
	c, err := m.inspectContainer(ctx, e)
	if err != nil {
		return e, c, err
	}
	switch {
	case !c.Exists:
		return e, c, ErrNotInstalled
	case !c.Managed:
		return e, c, fmt.Errorf("%w: %s", ErrNotManaged, conflict("container", m.Cfg.Container))
	}
	return e, c, nil
}

// Start starts the managed container (if stopped) and waits for the database.
func (m *Manager) Start(ctx context.Context) error {
	e, c, err := m.owned(ctx)
	if err != nil {
		return err
	}
	if !c.Running {
		if err := m.start(ctx, e, c); err != nil {
			return err
		}
	}
	return m.waitReady(ctx, e, c.ID)
}

// Stop stops the managed container (a no-op when already stopped).
func (m *Manager) Stop(ctx context.Context) error {
	e, c, err := m.owned(ctx)
	if err != nil {
		return err
	}
	if !c.Running {
		return nil
	}
	if _, err := m.call(ctx, e, nil, "stop", c.ID); err != nil {
		return fmt.Errorf("pgmanaged: stop: %w", err)
	}
	return nil
}

// Status is a read-only snapshot.
type Status struct {
	Engine      EngineInfo
	Installed   bool
	Running     bool
	Ready       bool
	Port        int
	Image       string
	Health      string
	Conflicts   []string
	VolumeOK    bool
	NetworkOK   bool
	HasSecrets  bool
	SecretsPath string
}

// Status inspects the instance without changing anything.
func (m *Manager) Status(ctx context.Context) (Status, error) {
	if err := m.Cfg.Validate(); err != nil {
		return Status{}, err
	}
	w, err := m.survey(ctx)
	st := Status{Engine: w.engine, SecretsPath: SecretsPath(m.Cfg.Home)}
	if err != nil {
		return st, err
	}
	st.Conflicts = w.conflicts(m.Cfg)
	st.VolumeOK = w.volume.Exists && w.volume.Managed
	st.NetworkOK = w.network.Exists && w.network.Managed
	_, cerr := ReadCredentials(m.Cfg.Home)
	st.HasSecrets = cerr == nil
	if w.container.Exists && w.container.Managed {
		st.Installed, st.Running, st.Port, st.Image, st.Health = true, w.container.Running, w.container.HostPort, w.container.Image, w.container.Health
		if st.Running {
			st.Ready = m.ready(ctx, w.engine, w.container.ID)
		}
	}
	return st, nil
}

// Logs returns the last n log lines of the managed container.
func (m *Manager) Logs(ctx context.Context, n int) (string, error) {
	e, c, err := m.owned(ctx)
	if err != nil {
		return "", err
	}
	if n <= 0 || n > 5000 {
		n = 100
	}
	res, err := m.call(ctx, e, nil, "logs", "--tail", strconv.Itoa(n), c.ID)
	if err != nil {
		return "", fmt.Errorf("pgmanaged: logs: %w", err)
	}
	// The engine multiplexes the container's stdout and stderr; PostgreSQL logs to stderr.
	return string(res.Stdout) + string(res.Stderr), nil
}

// BackupResult describes a finished dump.
type BackupResult struct {
	Path  string
	Bytes int64
}

// Backup writes a custom-format pg_dump of the managed database to
// <home>/.gabitotl/backups/memory-postgres/. The dump is streamed from the
// container into a 0600 file; restore with pg_restore (see docs/MEMORY_SERVER.md).
func (m *Manager) Backup(ctx context.Context) (BackupResult, error) {
	e, c, err := m.owned(ctx)
	if err != nil {
		return BackupResult{}, err
	}
	if !c.Running {
		return BackupResult{}, errors.New("pgmanaged: the managed PostgreSQL is stopped; start it before taking a backup")
	}
	dir := BackupsDir(m.Cfg.Home)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return BackupResult{}, err
	}
	path := fmt.Sprintf("%s/memory-%s.dump", dir, m.Now().Format("20060102T150405Z"))
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return BackupResult{}, err
	}
	_, runErr := m.Run.Run(ctx, Call{Bin: e.Path, Stdout: f,
		Args: []string{"exec", c.ID, "pg_dump", "-U", m.Cfg.User, "-d", m.Cfg.Database, "--format=custom", "--no-owner"}})
	closeErr := f.Close()
	st, statErr := os.Stat(path)
	if runErr != nil || closeErr != nil || statErr != nil || st.Size() == 0 {
		_ = os.Remove(path)
		if runErr == nil {
			runErr = errors.New("the dump is empty")
		}
		return BackupResult{}, fmt.Errorf("pgmanaged: backup failed: %w", runErr)
	}
	return BackupResult{Path: path, Bytes: st.Size()}, nil
}

// RemoveOptions control Remove.
type RemoveOptions struct {
	// Purge also deletes the data volume and the credentials file.
	Purge bool
	// Confirmed must be true for Purge to proceed; the CLI sets it only after
	// the user typed the confirmation.
	Confirmed bool
}

// RemoveResult lists what was removed and what was deliberately kept.
type RemoveResult struct {
	Removed []string
	Kept    []string
}

// Remove deletes the managed container and network. The data volume (and the
// credentials that open it) are kept unless Purge is confirmed. Resources with
// our names but without our label are never touched.
func (m *Manager) Remove(ctx context.Context, o RemoveOptions) (RemoveResult, error) {
	var r RemoveResult
	if o.Purge && !o.Confirmed {
		return r, ErrPurgeUnconfirmed
	}
	if err := m.Cfg.Validate(); err != nil {
		return r, err
	}
	w, err := m.survey(ctx)
	if err != nil {
		return r, err
	}
	if w.container.Exists && !w.container.Managed {
		return r, fmt.Errorf("%w: %s", ErrNotManaged, conflict("container", m.Cfg.Container))
	}
	if w.container.Exists {
		if w.container.Running {
			if _, err := m.call(ctx, w.engine, nil, "stop", w.container.ID); err != nil {
				return r, fmt.Errorf("pgmanaged: stop: %w", err)
			}
		}
		if _, err := m.call(ctx, w.engine, nil, "rm", w.container.ID); err != nil {
			return r, fmt.Errorf("pgmanaged: remove container: %w", err)
		}
		r.Removed = append(r.Removed, "container "+m.Cfg.Container)
	}
	switch {
	case w.network.Exists && w.network.Managed:
		if _, err := m.call(ctx, w.engine, nil, "network", "rm", m.Cfg.Network); err != nil {
			return r, fmt.Errorf("pgmanaged: remove network: %w", err)
		}
		r.Removed = append(r.Removed, "network "+m.Cfg.Network)
	case w.network.Exists:
		r.Kept = append(r.Kept, "network "+m.Cfg.Network+" (not ours, untouched)")
	}
	switch {
	case w.volume.Exists && !w.volume.Managed:
		r.Kept = append(r.Kept, "volume "+m.Cfg.Volume+" (not ours, untouched)")
	case w.volume.Exists && o.Purge:
		if _, err := m.call(ctx, w.engine, nil, "volume", "rm", m.Cfg.Volume); err != nil {
			return r, fmt.Errorf("pgmanaged: remove volume: %w", err)
		}
		r.Removed = append(r.Removed, "volume "+m.Cfg.Volume+" (all data)")
		if err := os.Remove(SecretsPath(m.Cfg.Home)); err == nil {
			r.Removed = append(r.Removed, "credentials file")
		}
	case w.volume.Exists:
		r.Kept = append(r.Kept, "volume "+m.Cfg.Volume+" (your data; --purge-data deletes it)")
	}
	return r, nil
}

// Verify checks the isolation guarantees and health of the instance. It is read-only.
func (m *Manager) Verify(ctx context.Context) memory.Report {
	rep := memory.Report{Provider: "pgmanaged"}
	add := func(name, status, msg string) {
		rep.Checks = append(rep.Checks, memory.Check{Name: name, Status: status, Message: msg})
	}
	if err := m.Cfg.Validate(); err != nil {
		add("config", memory.StatusError, err.Error())
		return rep
	}
	w, err := m.survey(ctx)
	if err != nil {
		add("engine", memory.StatusError, firstLine(err.Error()))
		return rep
	}
	add("engine", memory.StatusOK, w.engine.Name+" "+w.engine.Version)
	for _, c := range w.conflicts(m.Cfg) {
		add("conflict", memory.StatusError, c)
	}
	c := w.container
	if !c.Exists {
		add("container", memory.StatusError, "not installed")
		return rep
	}
	if !c.Managed {
		return rep
	}
	add("labels", memory.StatusOK, "container carries "+LabelManaged+"=true")
	if !w.volume.Exists || !w.volume.Managed {
		add("volume", memory.StatusError, "dedicated data volume is missing or not ours")
	} else if len(c.Volumes) != 1 || c.Volumes[0] != m.Cfg.Volume {
		add("volume", memory.StatusWarn, "the container does not mount the dedicated volume")
	} else {
		add("volume", memory.StatusOK, m.Cfg.Volume)
	}
	if !w.network.Exists || !w.network.Managed {
		add("network", memory.StatusWarn, "dedicated network is missing or not ours")
	} else {
		add("network", memory.StatusOK, m.Cfg.Network)
	}
	switch {
	case c.HostPort == containerPort:
		add("port", memory.StatusError, "published on the standard PostgreSQL port 5432")
	case !loopback(c.HostIP):
		add("port", memory.StatusError, fmt.Sprintf("published on %q, not loopback", c.HostIP))
	default:
		add("port", memory.StatusOK, fmt.Sprintf("%s:%d", c.HostIP, c.HostPort))
	}
	if c.Image != m.Cfg.Image {
		add("image", memory.StatusWarn, fmt.Sprintf("runs %s, configured %s", c.Image, m.Cfg.Image))
	} else {
		add("image", memory.StatusOK, c.Image)
	}
	m.verifySecrets(add, c.HostPort)
	switch {
	case !c.Running:
		add("running", memory.StatusWarn, "container is stopped")
	case m.ready(ctx, w.engine, c.ID):
		add("ready", memory.StatusOK, "PostgreSQL accepts connections")
	default:
		add("ready", memory.StatusError, "container runs but PostgreSQL does not accept connections")
	}
	return rep
}

func (m *Manager) verifySecrets(add func(name, status, msg string), port int) {
	st, err := os.Stat(SecretsPath(m.Cfg.Home))
	if err != nil {
		add("credentials", memory.StatusError, "credentials file is missing: "+SecretsPath(m.Cfg.Home))
		return
	}
	if st.Mode().Perm()&0o077 != 0 {
		add("credentials", memory.StatusError, fmt.Sprintf("credentials file mode is %o, expected 600", st.Mode().Perm()))
		return
	}
	cr, err := ReadCredentials(m.Cfg.Home)
	switch {
	case err != nil:
		add("credentials", memory.StatusError, firstLine(err.Error()))
	case cr.URL != m.Cfg.DatabaseURL(cr.Password, port):
		add("credentials", memory.StatusWarn, "the connection file points to another port; run install to refresh it")
	default:
		add("credentials", memory.StatusOK, SecretsPath(m.Cfg.Home)+" (0600)")
	}
}
