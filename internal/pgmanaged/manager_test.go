package pgmanaged

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/memory"
)

var ownLabels = map[string]string{LabelManaged: "true", LabelRole: RoleContainer}

type fx struct {
	t    *testing.T
	home string
	eng  *FakeEngine
	m    *Manager
	busy map[int]bool
	now  time.Time
}

func newFx(t *testing.T) *fx {
	t.Helper()
	f := &fx{t: t, home: t.TempDir(), eng: NewFakeEngine(), busy: map[int]bool{}, now: time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)}
	f.m = &Manager{
		Cfg: DefaultConfig(f.home), Run: f.eng,
		LookPath: func(n string) (string, error) { return "/usr/bin/" + n, nil },
		PortFree: func(p int) bool { return !f.busy[p] },
		Now:      func() time.Time { return f.now },
		Sleep:    func(_ context.Context, d time.Duration) error { f.now = f.now.Add(d); return nil },
		Rand:     strings.NewReader(strings.Repeat("0123456789abcdef", 8)), ReadyTimeout: time.Second,
	}
	return f
}

func (f *fx) install() InstallResult {
	f.t.Helper()
	r, err := f.m.Install(context.Background())
	if err != nil {
		f.t.Fatalf("install: %v", err)
	}
	return r
}

func TestConfigValidation(t *testing.T) {
	ok := DefaultConfig("/h")
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	bad := map[string]func(*Config){
		"foreign container name": func(c *Config) { c.Container = "postgres" },
		"foreign volume name":    func(c *Config) { c.Volume = "pgdata" },
		"foreign network name":   func(c *Config) { c.Network = "bridge" },
		"same names":             func(c *Config) { c.Volume = c.Container },
		"port 5432":              func(c *Config) { c.Port = 5432 },
		"privileged port":        func(c *Config) { c.Port = 80 },
		"engine":                 func(c *Config) { c.Engine = "nerdctl" },
		"image option injection": func(c *Config) { c.Image = "--privileged" },
		"image with space":       func(c *Config) { c.Image = "postgres:17 --privileged" },
		"user":                   func(c *Config) { c.User = "Robert'); DROP" },
		"no home":                func(c *Config) { c.Home = " " },
		"uppercase name":         func(c *Config) { c.Container = "gabitotl-Memory" },
	}
	for name, mut := range bad {
		c := ok
		mut(&c)
		if err := c.Validate(); err == nil {
			t.Errorf("%s: must be rejected", name)
		}
	}
}

func TestPlanIsReadOnlyAndDescribesTheInstance(t *testing.T) {
	f := newFx(t)
	p, err := f.m.Plan(context.Background())
	if err != nil || p.Err() != nil {
		t.Fatalf("plan: %v %v", err, p.Err())
	}
	if p.Port != DefaultPort || len(p.Steps) < 5 {
		t.Fatalf("plan: %+v", p)
	}
	for _, c := range f.eng.Calls {
		if c.Args[0] != "version" && c.Args[0] != "inspect" && c.Args[1] != "inspect" {
			t.Fatalf("a dry run must only read, ran %v", c.Args)
		}
	}
	if len(f.eng.Mutations) != 0 {
		t.Fatalf("mutations: %+v", f.eng.Mutations)
	}
	out := p.Render()
	for _, want := range []string{"127.0.0.1:54329", "create container gabitotl-memory-postgres", "io.gabitotl.managed=true", "credentials"} {
		if !strings.Contains(out, want) {
			t.Errorf("plan lacks %q:\n%s", want, out)
		}
	}
	if err := gabitotl.ValidateNoSecrets([]byte(out)); err != nil {
		t.Fatalf("the plan must not look like it holds a secret: %v", err)
	}
	if _, err := os.Stat(SecretsPath(f.home)); err == nil {
		t.Fatal("a plan must not write credentials")
	}
}

func TestInstallCreatesLabeledResourcesAndIsIdempotent(t *testing.T) {
	f := newFx(t)
	r := f.install()
	if !r.Created || !r.Ready || r.Port != DefaultPort {
		t.Fatalf("result: %+v", r)
	}
	ct := f.eng.Containers[DefaultContainer]
	if ct == nil || ct.Labels[LabelManaged] != "true" || ct.Labels[LabelRole] != RoleContainer || ct.HostIP != "127.0.0.1" || ct.HostPort != "54329" {
		t.Fatalf("container: %+v", ct)
	}
	if !ownedLabels(f.eng.Volumes[DefaultVolume]) || !ownedLabels(f.eng.Networks[DefaultNetwork]) {
		t.Fatal("volume and network must be labeled")
	}
	if !slices.Equal(ct.Volumes, []string{DefaultVolume}) || ct.Image != DefaultImage {
		t.Fatalf("container: %+v", ct)
	}
	// The password is generated, stored 0600, and never an argument.
	creds, err := ReadCredentials(SecretsPath(f.home))
	if err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(SecretsPath(f.home))
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("credentials mode %v", st.Mode().Perm())
	}
	if d, _ := os.Stat(SecretsDir(f.home)); d.Mode().Perm() != 0o700 {
		t.Fatalf("secrets dir mode %v", d.Mode().Perm())
	}
	for _, c := range f.eng.Calls {
		if strings.Contains(strings.Join(c.Args, " "), creds.Password) {
			t.Fatalf("password in a process argument: %v", c.Args)
		}
	}
	var run Call
	for _, c := range f.eng.Calls {
		if c.Args[0] == "run" {
			run = c
		}
	}
	joined := strings.Join(run.Args, " ")
	for _, want := range []string{"-p 127.0.0.1:54329:5432", "-e POSTGRES_PASSWORD -e", "--network " + DefaultNetwork, "-v " + DefaultVolume + ":/var/lib/postgresql/data", DefaultImage} {
		if !strings.Contains(joined, want) {
			t.Errorf("run args lack %q: %s", want, joined)
		}
	}
	if !slices.Contains(run.Env, "POSTGRES_PASSWORD=<redacted>") {
		t.Fatalf("the password must travel in the environment: %v", run.Env)
	}
	if !strings.Contains(creds.URL, "@127.0.0.1:54329/"+DefaultDatabase) || strings.Contains(creds.URL, ":5432/") {
		t.Fatalf("url: %s", creds.URL)
	}
	// Second run: nothing new is created, credentials unchanged.
	before := len(f.eng.Mutations)
	r2 := f.install()
	if r2.Created || !r2.Ready {
		t.Fatalf("second install: %+v", r2)
	}
	for _, mu := range f.eng.Mutations[before:] {
		if mu.Verb != "exec" {
			t.Fatalf("idempotent install changed something: %+v", mu)
		}
	}
	if again, _ := ReadCredentials(SecretsPath(f.home)); again != creds {
		t.Fatal("credentials must not change on re-install")
	}
}

func TestInstallStartsAStoppedManagedContainer(t *testing.T) {
	f := newFx(t)
	f.install()
	if err := f.m.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.eng.Containers[DefaultContainer].Running {
		t.Fatal("stop must stop")
	}
	if r := f.install(); r.Created {
		t.Fatal("must start, not recreate")
	}
	if !f.eng.Containers[DefaultContainer].Running {
		t.Fatal("install must start a stopped instance")
	}
}

func TestPortSelection(t *testing.T) {
	f := newFx(t)
	f.busy[54329], f.busy[54330] = true, true
	p, _ := f.m.Plan(context.Background())
	if p.Port != 54331 {
		t.Fatalf("default busy must auto-pick the next free port, got %d", p.Port)
	}
	f.m.Cfg.Port = 54340
	f.busy[54340] = true
	if _, err := f.m.Install(context.Background()); !errors.Is(err, ErrPortInUse) {
		t.Fatalf("explicit busy port: %v", err)
	}
	if len(f.eng.Mutations) != 0 {
		t.Fatal("nothing may be created when the port is busy")
	}
	f.m.Cfg.Port = 5432
	if _, err := f.m.Install(context.Background()); err == nil {
		t.Fatal("5432 must be rejected")
	}
	f.m.Cfg.Port = 0
	for p := DefaultPort; p < DefaultPort+PortSearchSpan; p++ {
		f.busy[p] = true
	}
	if _, err := f.m.Install(context.Background()); !errors.Is(err, ErrPortInUse) {
		t.Fatalf("no free port: %v", err)
	}
}

func TestNameCollisionRefusesEverything(t *testing.T) {
	collisions := map[string]func(*FakeEngine){
		"container": func(e *FakeEngine) {
			e.Containers[DefaultContainer] = &FakeContainer{ID: "abc", Name: DefaultContainer, Image: "postgres:16", Running: true, HostPort: "5432", HostIP: "0.0.0.0", Labels: map[string]string{}}
		},
		"volume":  func(e *FakeEngine) { e.Volumes[DefaultVolume] = map[string]string{} },
		"network": func(e *FakeEngine) { e.Networks[DefaultNetwork] = map[string]string{"other": "label"} },
	}
	for what, seed := range collisions {
		t.Run(what, func(t *testing.T) {
			f := newFx(t)
			seed(f.eng)
			before := f.eng.Snapshot()
			ctx := context.Background()
			plan, err := f.m.Plan(ctx)
			if err != nil || !errors.Is(plan.Err(), ErrNotManaged) || !strings.Contains(plan.Render(), "CONFLICT") {
				t.Fatalf("plan must report the conflict: %v / %v", err, plan.Err())
			}
			if _, err := f.m.Install(ctx); !errors.Is(err, ErrNotManaged) {
				t.Fatalf("install: %v", err)
			}
			ops := map[string]error{
				"remove": func() error { _, e := f.m.Remove(ctx, RemoveOptions{}); return e }(),
				"purge":  func() error { _, e := f.m.Remove(ctx, RemoveOptions{Purge: true, Confirmed: true}); return e }(),
				"start":  f.m.Start(ctx),
				"stop":   f.m.Stop(ctx),
				"backup": func() error { _, e := f.m.Backup(ctx); return e }(),
				"logs":   func() error { _, e := f.m.Logs(ctx, 10); return e }(),
			}
			if what == "container" {
				for name, e := range ops {
					if !errors.Is(e, ErrNotManaged) {
						t.Errorf("%s on a foreign container must refuse with ErrNotManaged, got %v", name, e)
					}
				}
				if rep := f.m.Verify(ctx); rep.OK() {
					t.Error("verify must report the conflict")
				}
			}
			if bad := f.eng.Unlabeled(); len(bad) != 0 {
				t.Fatalf("a mutation reached an unlabeled resource: %+v", bad)
			}
			if after := f.eng.Snapshot(); !slices.Equal(before, after) {
				t.Fatalf("foreign resources changed:\n%v\n%v", before, after)
			}
			if len(f.eng.Mutations) != 0 {
				t.Fatalf("nothing may be created or changed: %+v", f.eng.Mutations)
			}
		})
	}
}

func TestRemoveKeepsTheVolumeUnlessPurged(t *testing.T) {
	f := newFx(t)
	f.install()
	ctx := context.Background()
	if _, err := f.m.Remove(ctx, RemoveOptions{Purge: true}); !errors.Is(err, ErrPurgeUnconfirmed) {
		t.Fatalf("purge needs confirmation: %v", err)
	}
	if len(f.eng.Containers) != 1 {
		t.Fatal("an unconfirmed purge must change nothing")
	}
	r, err := f.m.Remove(ctx, RemoveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.eng.Containers) != 0 || len(f.eng.Networks) != 0 || len(f.eng.Volumes) != 1 {
		t.Fatalf("default remove keeps the volume: %+v / %+v", r, f.eng)
	}
	if _, err := ReadCredentials(SecretsPath(f.home)); err != nil {
		t.Fatal("credentials open the kept volume and must stay")
	}
	// Re-installing over the kept volume reuses the password.
	before, _ := ReadCredentials(SecretsPath(f.home))
	f.install()
	if after, _ := ReadCredentials(SecretsPath(f.home)); after.Password != before.Password {
		t.Fatal("re-install over a kept volume must reuse its password")
	}
	r, err = f.m.Remove(ctx, RemoveOptions{Purge: true, Confirmed: true})
	if err != nil || len(f.eng.Volumes) != 0 {
		t.Fatalf("purge: %+v %v", r, err)
	}
	if _, err := ReadCredentials(SecretsPath(f.home)); !errors.Is(err, ErrNoCredentials) {
		t.Fatal("purge removes the credentials file")
	}
	if bad := f.eng.Unlabeled(); len(bad) != 0 {
		t.Fatal(bad)
	}
}

func TestRemoveNeverTouchesAForeignVolumeOrNetwork(t *testing.T) {
	f := newFx(t)
	f.install()
	f.eng.Volumes[DefaultVolume] = map[string]string{} // the user swapped it for their own
	f.eng.Networks[DefaultNetwork] = map[string]string{}
	r, err := f.m.Remove(context.Background(), RemoveOptions{Purge: true, Confirmed: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := f.eng.Volumes[DefaultVolume]; !ok {
		t.Fatal("a volume without our label must survive a purge")
	}
	if _, ok := f.eng.Networks[DefaultNetwork]; !ok {
		t.Fatal("a network without our label must survive")
	}
	if len(r.Kept) != 2 {
		t.Fatalf("kept: %v", r.Kept)
	}
	if bad := f.eng.Unlabeled(); len(bad) != 0 {
		t.Fatal(bad)
	}
}

func TestMissingCredentialsWithExistingDataIsRefused(t *testing.T) {
	f := newFx(t)
	f.install()
	_, _ = f.m.Remove(context.Background(), RemoveOptions{})
	if err := os.Remove(SecretsPath(f.home)); err != nil {
		t.Fatal(err)
	}
	mutations := len(f.eng.Mutations)
	if _, err := f.m.Install(context.Background()); !errors.Is(err, ErrNoCredentials) {
		t.Fatalf("install over data without credentials must refuse: %v", err)
	}
	if len(f.eng.Mutations) != mutations {
		t.Fatal("nothing may be created")
	}
}

func TestBackupWritesA0600Dump(t *testing.T) {
	f := newFx(t)
	ctx := context.Background()
	if _, err := f.m.Backup(ctx); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("backup without instance: %v", err)
	}
	f.install()
	r, err := f.m.Backup(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(r.Path) != BackupsDir(f.home) || r.Bytes == 0 {
		t.Fatalf("backup: %+v", r)
	}
	if st, _ := os.Stat(r.Path); st.Mode().Perm() != 0o600 {
		t.Fatalf("dump mode %v", st.Mode().Perm())
	}
	_ = f.m.Stop(ctx)
	if _, err := f.m.Backup(ctx); err == nil {
		t.Fatal("backup of a stopped instance must fail with guidance")
	}
	if entries, _ := os.ReadDir(BackupsDir(f.home)); len(entries) != 1 {
		t.Fatalf("a failed backup leaves no file: %v", entries)
	}
}

func TestEngineProblemsAreReportedNotHidden(t *testing.T) {
	f := newFx(t)
	f.eng.Down = true
	ctx := context.Background()
	if _, err := f.m.Install(ctx); !errors.Is(err, ErrNoEngine) {
		t.Fatalf("install without a reachable engine: %v", err)
	}
	p, err := f.m.Plan(ctx)
	if err != nil || p.EngineErr == nil || !strings.Contains(p.Render(), "Engine problem") || len(p.Steps) == 0 {
		t.Fatalf("a dry run still describes the steps: %+v %v", p, err)
	}
	f.m.LookPath = func(string) (string, error) { return "", errors.New("not found") }
	if _, err := f.m.Detect(ctx); !errors.Is(err, ErrNoEngine) {
		t.Fatalf("detect: %v", err)
	}
	f.m.Cfg.Engine = "podman"
	if _, err := f.m.Detect(ctx); err == nil || !strings.Contains(err.Error(), "podman") {
		t.Fatalf("explicit engine: %v", err)
	}
}

func TestAutoEngineFallsBackToPodman(t *testing.T) {
	f := newFx(t)
	f.m.LookPath = func(n string) (string, error) {
		if n == "podman" {
			return "/usr/bin/podman", nil
		}
		return "", errors.New("missing")
	}
	e, err := f.m.Detect(context.Background())
	if err != nil || e.Name != "podman" {
		t.Fatalf("%+v %v", e, err)
	}
}

func TestVerifyReportsTheIsolationGuarantees(t *testing.T) {
	f := newFx(t)
	f.install()
	rep := f.m.Verify(context.Background())
	if !rep.OK() {
		t.Fatalf("a fresh install must verify: %+v", rep)
	}
	want := []string{"labels", "volume", "network", "port", "image", "credentials", "ready"}
	for _, w := range want {
		found := false
		for _, c := range rep.Checks {
			if c.Name == w && c.Status == memory.StatusOK {
				found = true
			}
		}
		if !found {
			t.Errorf("check %q missing or not ok: %+v", w, rep.Checks)
		}
	}
	f.eng.Containers[DefaultContainer].HostIP = "0.0.0.0"
	if f.m.Verify(context.Background()).OK() {
		t.Fatal("a non-loopback binding must fail verification")
	}
	f.eng.Containers[DefaultContainer].HostIP = "127.0.0.1"
	f.eng.Containers[DefaultContainer].HostPort = "5432"
	if f.m.Verify(context.Background()).OK() {
		t.Fatal("port 5432 must fail verification")
	}
	f.eng.Containers[DefaultContainer].HostPort = "54329"
	if err := os.Chmod(SecretsPath(f.home), 0o644); err != nil {
		t.Fatal(err)
	}
	if f.m.Verify(context.Background()).OK() {
		t.Fatal("a world-readable credentials file must fail verification")
	}
}

func TestFailedRunCleansUpOnlyItsOwnContainer(t *testing.T) {
	f := newFx(t)
	f.eng.FailRun = true
	if _, err := f.m.Install(context.Background()); err == nil {
		t.Fatal("run failure must surface")
	}
	if len(f.eng.Containers) != 0 {
		t.Fatal("the half-created container must be removed")
	}
	creds, _ := ReadCredentials(SecretsPath(f.home))
	for _, c := range f.eng.Calls {
		if strings.Contains(strings.Join(c.Args, " "), creds.Password) {
			t.Fatal("password in args")
		}
	}
}

func TestStatus(t *testing.T) {
	f := newFx(t)
	ctx := context.Background()
	st, err := f.m.Status(ctx)
	if err != nil || st.Installed {
		t.Fatalf("%+v %v", st, err)
	}
	f.install()
	if st, _ = f.m.Status(ctx); !st.Installed || !st.Running || !st.Ready || st.Port != DefaultPort || !st.VolumeOK || !st.HasSecrets {
		t.Fatalf("%+v", st)
	}
}

func TestFilteredEnvDropsEveryPostgresVariable(t *testing.T) {
	in := []string{"PATH=/bin", "PGHOST=prod", "PGPASSWORD=x", "PGPORT=5432", "DATABASE_URL=postgres://u:p@h/d", "POSTGRES_PASSWORD=x", "HOME=/h", "DOCKER_HOST=unix:///x"}
	got := FilteredEnv(in)
	if !slices.Equal(got, []string{"PATH=/bin", "HOME=/h", "DOCKER_HOST=unix:///x"}) {
		t.Fatalf("%v", got)
	}
}

func TestLookupSecret(t *testing.T) {
	home := t.TempDir()
	if err := WriteCredentials(SecretsPath(home), Credentials{Password: "pw", URL: "postgres://u:pw@127.0.0.1:54329/d"}); err != nil {
		t.Fatal(err)
	}
	none := func(string) string { return "" }
	if v := LookupSecret(EnvDatabaseURL, SecretsPath(home), none); !strings.HasPrefix(v, "postgres://") {
		t.Fatalf("file fallback: %q", v)
	}
	if v := LookupSecret(EnvDatabaseURL, SecretsPath(home), func(string) string { return "from-env" }); v != "from-env" {
		t.Fatalf("the process environment wins: %q", v)
	}
	if v := LookupSecret("DATABASE_URL", home, none); v != "" {
		t.Fatalf("only the two managed variables are read from the file: %q", v)
	}
}

func TestNetworkCreationSurvivesAddressPoolExhaustion(t *testing.T) {
	f := newFx(t)
	f.eng.PoolExhausted = true
	f.install()
	if !ownedLabels(f.eng.Networks[DefaultNetwork]) {
		t.Fatal("the network must be created, labeled, with an explicit subnet")
	}
	var subnet string
	for _, c := range f.eng.Calls {
		if len(c.Args) > 3 && c.Args[0] == "network" && c.Args[1] == "create" && c.Args[2] == "--subnet" {
			subnet = c.Args[3]
		}
	}
	if !strings.HasPrefix(subnet, "10.213.") || !strings.HasSuffix(subnet, "/28") {
		t.Fatalf("subnet fallback: %q", subnet)
	}
	for _, c := range f.eng.Calls {
		if len(c.Args) > 1 && c.Args[0] == "network" && c.Args[1] == "inspect" && c.Args[2] != DefaultNetwork {
			t.Fatalf("other networks must never be inspected: %v", c.Args)
		}
	}
}

func TestNamedInstancesNeverShareResources(t *testing.T) {
	base := DefaultConfig("/h")
	a, err := base.WithInstance("trial")
	if err != nil {
		t.Fatal(err)
	}
	if a.Container != "gabitotl-memory-postgres-trial" || a.Volume != "gabitotl-memory-pgdata-trial" || a.Network != "gabitotl-memory-net-trial" {
		t.Fatalf("%+v", a)
	}
	if a.SecretsFile() == base.SecretsFile() || !strings.HasSuffix(a.SecretsFile(), "memory-server-trial.env") {
		t.Fatalf("each instance has its own credentials file: %s vs %s", a.SecretsFile(), base.SecretsFile())
	}
	if err := a.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"Trial", "a b", "../x", strings.Repeat("a", 40), "-x"} {
		if _, err := base.WithInstance(bad); err == nil {
			t.Errorf("instance %q must be rejected", bad)
		}
	}
	// A named instance coexists with the default one in the same engine.
	f := newFx(t)
	f.install()
	f.m.Cfg, _ = f.m.Cfg.WithInstance("trial")
	f.busy[DefaultPort] = true // the default instance listens there; the next one takes another port
	if r := f.install(); r.Port == DefaultPort {
		t.Fatal("the second instance must pick another port")
	}
	if len(f.eng.Containers) != 2 || len(f.eng.Volumes) != 2 || len(f.eng.Networks) != 2 {
		t.Fatalf("two instances, two of each resource: %d/%d/%d", len(f.eng.Containers), len(f.eng.Volumes), len(f.eng.Networks))
	}
	if _, err := f.m.Remove(context.Background(), RemoveOptions{Purge: true, Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	if len(f.eng.Containers) != 1 || f.eng.Containers[DefaultContainer] == nil {
		t.Fatal("removing a named instance must leave the default one alone")
	}
}
