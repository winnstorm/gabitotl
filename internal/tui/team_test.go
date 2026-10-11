package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/installer"
	"github.com/winnstorm/gabitotl/internal/memory/mcp"
	"github.com/winnstorm/gabitotl/internal/memory/migrate"
	"github.com/winnstorm/gabitotl/internal/memory/migrate/engrammcp"
	"github.com/winnstorm/gabitotl/internal/memory/server"
	"github.com/winnstorm/gabitotl/internal/memory/store"
	"github.com/winnstorm/gabitotl/internal/memory/store/memstore"
	"github.com/winnstorm/gabitotl/internal/pgmanaged"
)

// withTeam gives the machine a fake container engine (docker on PATH), an
// in-memory team database and the tested Engram migrator.
func withTeam(mc *machine, docker bool) (*pgmanaged.FakeEngine, *memstore.Store) {
	eng, st := pgmanaged.NewFakeEngine(), memstore.New()
	mc.bins["docker"] = docker
	reg := migrate.NewRegistry()
	_ = reg.Register(engrammcp.New(func(context.Context, string) (store.Importer, func(), error) { return st, func() {}, nil }))
	mc.env.Migrators = reg
	mc.env.Team = installer.TeamEnv{
		Manager: func(instance, engine string, port int) (*pgmanaged.Manager, error) {
			cfg, err := pgmanaged.DefaultConfig(mc.home).WithInstance(instance)
			if err != nil {
				return nil, err
			}
			cfg.Engine = "auto"
			return &pgmanaged.Manager{Cfg: cfg, Run: eng,
				LookPath: func(n string) (string, error) {
					if docker {
						return "/usr/bin/" + n, nil
					}
					return "", os.ErrNotExist
				},
				PortFree: func(int) bool { return true }, Now: time.Now,
				Sleep: func(context.Context, time.Duration) error { return nil },
				Rand:  strings.NewReader(strings.Repeat("0123456789abcdef", 8)), ReadyTimeout: time.Second}, nil
		},
		OpenStore: func(context.Context, string) (store.Store, func(), error) { return st, func() {}, nil },
		NewToken: func(ctx context.Context, ts store.TokenStore, user string, projects []string) (string, error) {
			p, _, err := server.NewScopedToken(ctx, ts, user, projects, false, time.Now())
			return p, err
		},
	}
	return eng, st
}

func toTeamScreen(t *testing.T, d *driver) {
	t.Helper()
	d.pick(lbl("ask.action.keep"))
	d.require(lbl("tui.memory.title"))
	d.pick(lbl("tui.memory.team"))
	d.until("the team screen", func() bool { return d.m.screen == scrTeam || d.m.screen == scrForm })
}

func TestTeamScreenWithoutAContainerEngineOffersClientOrEngram(t *testing.T) {
	mc := newMachine(t)
	withTeam(mc, false)
	d := newDriver(t, mc.config(opts()))
	toTeamScreen(t, d)
	d.require(lbl("tui.team.noengine.title"))
	d.require("no instalo Docker por mi cuenta")
	var got []string
	for _, it := range d.m.items() {
		got = append(got, it.label)
	}
	if len(got) != 2 || got[0] != lbl("tui.team.client") || got[1] != lbl("tui.team.stay") {
		t.Fatalf("without an engine only client and Engram are offered: %v", got)
	}
	// Staying with Engram goes on as a solo install.
	before := mc.state()
	d.pick(lbl("tui.team.stay"))
	d.until("runtimes", func() bool { return d.m.screen == scrForm })
	if d.m.opts.Mode != "solo" || d.m.opts.Memory != "" || d.m.opts.MemoryRole != "" {
		t.Fatalf("%+v", d.m.opts)
	}
	if mc.state() != before {
		t.Fatal("nothing may change before the plan is applied")
	}
}

func TestHostFlowShowsThePromiseAndInstallsTheServer(t *testing.T) {
	mc := newMachine(t)
	eng, st := withTeam(mc, true)
	d := newDriver(t, mc.config(opts()))
	toTeamScreen(t, d)
	d.require(lbl("tui.team.title"))
	d.require("no toco ningún PostgreSQL que ya tengas")
	d.require("docker")
	d.pick(lbl("tui.team.host"))
	d.until("invites form", func() bool { return d.m.screen == scrForm && d.m.formKind == "invites" })
	d.typeText("gonza")
	d.key("enter")
	// Spec 31: previous Engram memory -> import (recommended), keep, or start empty.
	d.until("engram question", func() bool { return d.m.screen == scrEngram })
	d.require(lbl("tui.engram.title"))
	for _, l := range []string{lbl("tui.engram.import"), lbl("tui.engram.keep"), lbl("tui.engram.empty")} {
		d.require(l)
	}
	if first := d.m.items()[0].label; first != lbl("tui.engram.import") || !strings.Contains(first, "recomendado") {
		t.Fatalf("import must be the recommended first option: %q", first)
	}
	d.pick(lbl("tui.engram.keep"))
	d.until("runtimes", func() bool { return d.m.screen == scrForm })
	d.key("enter")
	d.until("plan", func() bool { return d.m.screen == scrPlan || d.m.screen == scrPersona })
	if d.m.screen == scrPersona {
		d.pick(lbl("tui.persona.keep"))
		d.until("plan", func() bool { return d.m.screen == scrPlan })
	}
	d.pick(lbl("tui.plan.details"))
	d.require("servidor de memoria")
	if len(eng.Mutations) != 0 {
		t.Fatal("the plan screen must not have created anything")
	}
	d.pick(lbl("tui.plan.apply"))
	finishApply(d)
	out := d.m.Outcome()
	if out.Err != nil || out.Result.RolledBack || out.Result.Server == nil || !out.Result.Server.Created {
		t.Fatalf("outcome: %+v", out)
	}
	if c := eng.Containers[pgmanaged.DefaultContainer]; c == nil || !c.Running {
		t.Fatal("the server container must be running")
	}
	if len(out.Result.Tokens) != 1 || out.Result.Tokens[0].User != "gonza" {
		t.Fatalf("tokens: %+v", out.Result.Tokens)
	}
	d.require(lbl("result.team"))
	d.key("pgdown")
	d.require("gonza")
	if ts, _ := st.ListTokens(context.Background(), ""); len(ts) != 2 {
		t.Fatalf("host and invited teammate: %+v", ts)
	}
	if st2, _ := st.Stats(context.Background()); st2.Observations != 0 {
		t.Fatal("'keep Engram as history' imports nothing")
	}
}

func TestHostFlowImportsWhenAsked(t *testing.T) {
	mc := newMachine(t)
	_, st := withTeam(mc, true)
	mc.runner.Handlers["engram export"] = func(a []string) (string, error) {
		write(t, a[1], `{"version":"0.2.0","sessions":[],"observations":[{"id":1,"sync_id":"a","session_id":"s","type":"decision","title":"Use PostgreSQL","content":"Shared.","project":"shop","scope":"project","created_at":"2026-01-01 09:00:00","updated_at":"2026-01-01 09:00:00"}]}`)
		return "Exported to " + a[1] + "\n  Sessions: 0\n  Observations: 1\n  Prompts: 0\n", nil
	}
	d := newDriver(t, mc.config(opts()))
	toTeamScreen(t, d)
	d.pick(lbl("tui.team.host"))
	d.until("invites form", func() bool { return d.m.screen == scrForm && d.m.formKind == "invites" })
	d.key("enter") // nobody to invite
	d.until("engram question", func() bool { return d.m.screen == scrEngram })
	d.pick(lbl("tui.engram.import"))
	d.until("runtimes", func() bool { return d.m.screen == scrForm })
	d.key("enter")
	d.until("plan", func() bool { return d.m.screen == scrPlan || d.m.screen == scrPersona })
	if d.m.screen == scrPersona {
		d.pick(lbl("tui.persona.keep"))
		d.until("plan", func() bool { return d.m.screen == scrPlan })
	}
	d.pick(lbl("tui.plan.apply"))
	finishApply(d)
	out := d.m.Outcome()
	if out.Err != nil || out.Result.Import == nil || out.Result.Import.Imported != 1 || !out.Result.Import.Verified {
		t.Fatalf("outcome: %+v err=%v", out.Result.Import, out.Err)
	}
	if st2, _ := st.Stats(context.Background()); st2.Observations != 1 {
		t.Fatalf("%+v", st2)
	}
	d.key("pgdown", "pgdown")
	d.require("Importé 1 recuerdos de Engram")
	d.require("Engram quedó intacto")
}

func TestClientFlowNeverShowsOrStoresTheTokenOutsideTheSecretsFile(t *testing.T) {
	mc := newMachine(t)
	eng, _ := withTeam(mc, false) // no docker needed to be a client
	d := newDriver(t, mc.config(opts()))
	toTeamScreen(t, d)
	d.pick(lbl("tui.team.client"))
	d.until("url form", func() bool { return d.m.screen == scrForm && d.m.formKind == "url" })
	d.typeText("http://memoria.example.com/mcp")
	d.key("enter")
	d.require("La URL no sirve") // plain http to a remote host is refused
	for range len("http://memoria.example.com/mcp") {
		d.key("backspace")
	}
	d.typeText("https://memoria.example.com/mcp")
	d.key("enter")
	d.until("token env form", func() bool { return d.m.screen == scrForm && d.m.formKind == "tokenenv" })
	d.require("GABITOTL_MEMORY_TOKEN")
	d.key("enter")
	d.until("store form", func() bool { return d.m.screen == scrForm && d.m.formKind == "storetoken" })
	d.key("up", "enter") // "yes, paste it now"
	d.until("secret form", func() bool { return d.m.screen == scrForm && d.m.formKind == "secret" })
	const secret = "gbt_0123456789abcdef.TOPSECRETTOPSECRETTOPSECRETTOPSECRET0000"
	d.typeText(secret)
	if strings.Contains(d.view(), "TOPSECRET") {
		t.Fatalf("the token must never be echoed:\n%s", d.view())
	}
	d.key("enter")
	d.until("runtimes", func() bool { return d.m.screen == scrForm })
	d.key("enter")
	d.until("plan", func() bool { return d.m.screen == scrPlan || d.m.screen == scrPersona })
	if d.m.screen == scrPersona {
		d.pick(lbl("tui.persona.keep"))
		d.until("plan", func() bool { return d.m.screen == scrPlan })
	}
	if strings.Contains(d.view(), "TOPSECRET") {
		t.Fatal("the plan must not show the token")
	}
	d.pick(lbl("tui.plan.apply"))
	finishApply(d)
	out := d.m.Outcome()
	if out.Err != nil || out.Result.RolledBack {
		t.Fatalf("outcome: %v %+v", out.Err, out.Result.Checks)
	}
	if len(eng.Mutations) != 0 || out.Result.Server != nil {
		t.Fatal("a client never touches containers")
	}
	for range 3 {
		if strings.Contains(d.view(), "TOPSECRET") {
			t.Fatal("the result must not show the token")
		}
		d.key("pgdown")
	}
	got := (mcp.Resolver{Home: mc.home, Getenv: func(string) string { return "" }}).Lookup("GABITOTL_MEMORY_TOKEN")
	if got != secret {
		t.Fatalf("the token must be in the 0600 secrets file, got %q", got)
	}
	for _, dir := range []string{mc.root, filepath.Join(mc.home, ".gabitotl", "migrations"), filepath.Join(mc.home, ".gabitotl", "logs"), filepath.Join(mc.home, ".claude")} {
		_ = filepath.WalkDir(dir, func(p string, e os.DirEntry, err error) error {
			if err == nil && !e.IsDir() {
				if b, _ := os.ReadFile(p); strings.Contains(string(b), "TOPSECRET") {
					t.Errorf("token leaked into %s", p)
				}
			}
			return nil
		})
	}
	mem := slurpFile(t, filepath.Join(mc.root, ".gabitotl", "memory.md"))
	for _, want := range []string{"provider: memory-mcp", "mode: team", "url: https://memoria.example.com/mcp"} {
		if !strings.Contains(mem, want) {
			t.Errorf("memory.md lacks %q:\n%s", want, mem)
		}
	}
	if strings.Contains(mem, "role:") || strings.Contains(mem, "token:") {
		t.Errorf("the committed memory.md must not carry per-machine settings:\n%s", mem)
	}
	localPath, _ := gabitotl.NewLayout(mc.root).LocalMemory(gabitotl.ActiveUserID())
	local := slurpFile(t, localPath)
	for _, want := range []string{"role: client", "token: env:GABITOTL_MEMORY_TOKEN"} {
		if !strings.Contains(local, want) {
			t.Errorf("the local memory.md lacks %q:\n%s", want, local)
		}
	}
}

func slurpFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// finishApply waits for the result, answering "keep Gentle-AI" if asked.
func finishApply(d *driver) {
	d.t.Helper()
	d.until("result", func() bool { return d.m.screen == scrResult || d.m.screen == scrRetire })
	if d.m.screen == scrRetire {
		d.pick(lbl("ask.retire.keep"))
		d.until("result", func() bool { return d.m.screen == scrResult })
	}
}
