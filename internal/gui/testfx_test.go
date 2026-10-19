package gui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/winnstorm/gabitotl/internal/desktop"
	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/installer"
	"github.com/winnstorm/gabitotl/internal/memory"
	"github.com/winnstorm/gabitotl/internal/memory/migrate"
	"github.com/winnstorm/gabitotl/internal/runtime/all"
)

// rig is a fake machine: a temp HOME with a Claude Code and an OpenCode config,
// a temp project, and a runner that answers git. Nothing outside t.TempDir is read.
type rig struct {
	t      *testing.T
	home   string
	root   string
	runner *installer.FakeRunner
	bins   map[string]bool
	svc    *Service
	now    time.Time

	auth     SignIn
	hooksOn  *bool
	hooksCLI string
	index    *fakeIndex
}

type fakeIndex struct{ setups []string }

func (f *fakeIndex) Backends(string) []IndexBackend {
	return []IndexBackend{{Name: "codegraph", Installed: true}, {Name: "code-review-graph"}}
}
func (f *fakeIndex) Setup(_ context.Context, _ installer.Runner, _ string, tool string) error {
	f.setups = append(f.setups, tool)
	return nil
}

func write(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newRig(t *testing.T) *rig {
	t.Helper()
	r := &rig{t: t, home: t.TempDir(), root: t.TempDir(), now: time.Date(2026, 10, 5, 21, 0, 0, 0, time.UTC),
		bins: map[string]bool{"claude": true, "opencode": true, "git": true}, index: &fakeIndex{},
		auth: SignIn{Provider: "github", Known: true, OK: true, Status: "logged-in"}}
	write(t, filepath.Join(r.home, ".claude", "CLAUDE.md"), "# My rules\n\nBe kind.\n")
	write(t, filepath.Join(r.home, ".config", "opencode", "opencode.json"), "{\n  \"model\": \"anthropic/claude-sonnet-4-5\"\n}\n")
	write(t, filepath.Join(r.root, "go.mod"), "module shop\n")
	r.runner = &installer.FakeRunner{Handlers: map[string]func([]string) (string, error){
		"git": func(a []string) (string, error) {
			switch {
			case len(a) > 0 && a[0] == "rev-parse":
				return "true\n", nil
			case len(a) > 1 && a[0] == "config":
				return "Ada Lovelace\n", nil
			}
			return "https://github.com/acme/shop.git\n", nil
		},
	}}
	env := func(root string) (installer.Env, error) {
		return installer.Env{
			Home: r.home, ProjectRoot: root, GOOS: "linux", GOARCH: "amd64",
			Getenv: func(string) string { return "" }, Environ: func() []string { return nil },
			LookPath: func(n string) (string, error) {
				if r.bins[n] {
					return "/fake/bin/" + n, nil
				}
				return "", os.ErrNotExist
			},
			Runner: r.runner, Runtimes: all.Registry(), Migrators: migrate.NewRegistry(), Version: "test",
			Now:        func() time.Time { return r.now },
			OpenMemory: func(gabitotl.MemoryConfig) (memory.Provider, error) { return stubMemory{}, nil },
		}, nil
	}
	r.svc = New(Deps{
		Env:       env,
		CodeIndex: r.index,
		VCSAuth:   func(context.Context, string) (SignIn, error) { return r.auth, nil },
		GuardHistory: func(context.Context, string) (GuardScan, error) {
			return GuardScan{Mode: "strip", History: []GuardFinding{{Commit: "abc", Where: "message", Text: "x"}}}, nil
		},
		GuardHooks: func(_ context.Context, _ string, on bool, cli string) error {
			r.hooksOn, r.hooksCLI = &on, cli
			return nil
		},
		Version: "test",
	})
	return r
}

// answers is a minimal valid wizard run.
func (r *rig) answers() InstallAnswers {
	return InstallAnswers{
		Root: r.root, Lang: "en", Name: "Ada", User: "ada", PersonaMode: "lite", Runtimes: []string{"claude-code"},
		Memory:   MemoryAnswers{Mode: "solo"},
		Delivery: DeliveryAnswers{Provider: "github", Automation: "until-pr"},
		Windows:  WindowsAnswers{GuardHooks: false, CodeIndex: "none"},
	}
}

// install plans and applies answers, failing the test on any error.
func (r *rig) install(a InstallAnswers) InstallResult {
	r.t.Helper()
	pl, err := r.svc.PlanInstall(context.Background(), a)
	if err != nil {
		r.t.Fatal(err)
	}
	res, err := r.svc.Install(context.Background(), a, pl.ID, nil)
	if err != nil {
		r.t.Fatalf("install: %v (%+v)", err, res)
	}
	return res
}

// memDesktop is an in-memory Windows integration.
type memDesktop struct {
	files map[string]bool
	path  string
	links map[string]bool
	reg   *desktop.Registration
	calls []string

	failRegister bool
}

func (m *memDesktop) CopyFile(_, dst string) (int64, error) { m.files[dst] = true; return 10, nil }
func (m *memDesktop) WriteFile(_ string, d []byte) (int64, error) {
	return int64(len(d)), nil
}
func (m *memDesktop) Exists(p string) bool { return m.files[p] || m.links[p] }
func (m *memDesktop) RemoveAll(p string) error {
	delete(m.files, p)
	m.calls = append(m.calls, "rmall")
	return nil
}
func (m *memDesktop) ScheduleRemoval(string, []string) error {
	m.calls = append(m.calls, "schedule")
	return nil
}
func (m *memDesktop) Get() (string, error) { return m.path, nil }
func (m *memDesktop) Set(v string) error   { m.path = v; return nil }
func (m *memDesktop) Create(p string, _ desktop.Shortcut) error {
	m.links[p] = true
	return nil
}
func (m *memDesktop) Remove(p string) error { delete(m.links, p); return nil }
func (m *memDesktop) Register(r desktop.Registration) error {
	if m.failRegister {
		return errors.New("registry denied")
	}
	m.reg = &r
	return nil
}
func (m *memDesktop) Unregister() error { m.reg = nil; return nil }
func (m *memDesktop) Registered() (string, bool) {
	if m.reg == nil {
		return "", false
	}
	return m.reg.DisplayVersion, true
}

func (r *rig) withDesktop() *memDesktop {
	m := &memDesktop{files: map[string]bool{}, links: map[string]bool{}}
	base := r.t.TempDir()
	r.svc.d.Desktop = &desktop.Installer{
		Env: desktop.Env{
			ExePath: filepath.Join(base, "dl", desktop.ManagerExe), DefaultInstallDir: filepath.Join(base, "Programs", "GabitoTL"),
			StartMenuPrograms: filepath.Join(base, "Start"), DesktopDir: filepath.Join(base, "Desktop"),
			Protected: []string{base}, Today: func() string { return "20261005" },
		},
		FS: m, Path: m, Links: m, Reg: m,
	}
	return m
}

// stubMemory stands in for a reachable memory server.
type stubMemory struct{}

func (stubMemory) Search(context.Context, memory.Query) ([]memory.Entry, error) { return nil, nil }
func (stubMemory) Save(context.Context, memory.Entry) (memory.ID, error)        { return "1", nil }
func (stubMemory) Update(context.Context, memory.ID, memory.EntryPatch) error {
	return memory.ErrUnsupported
}
func (stubMemory) SessionSummary(context.Context, memory.Summary) error { return memory.ErrUnsupported }
func (stubMemory) CurrentProject(context.Context) (string, error)       { return "shop", nil }
func (stubMemory) Doctor(context.Context) memory.Report                 { return memory.Report{Provider: "memory-mcp"} }
