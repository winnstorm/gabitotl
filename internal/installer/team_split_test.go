package installer

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
)

const teamAddr = "https://memoria.example.com/mcp"

// splitRig is a team rig whose active person is "ana".
func splitRig(t *testing.T) (*teamRig, gabitotl.Layout, string) {
	t.Helper()
	t.Setenv("GABITOTL_USER", "ana")
	r := newTeamRig(t)
	l := gabitotl.NewLayout(r.root)
	local, err := l.LocalMemory("ana")
	if err != nil {
		t.Fatal(err)
	}
	return r, l, local
}

func applyOpts(t *testing.T, r *teamRig, o Options) Result {
	t.Helper()
	inv, pl, err := r.plan(o)
	if err != nil {
		t.Fatal(err)
	}
	res, err := Run(context.Background(), r.env, inv, pl, nil, Hooks{})
	if err != nil {
		t.Fatalf("%v (%+v)", err, res.Checks)
	}
	return res
}

func TestHostWithATeamURLSharesItAndKeepsTheLoopbackLocal(t *testing.T) {
	r, l, local := splitRig(t)
	o := hostOpts()
	o.MemoryURL = teamAddr
	res := applyOpts(t, r, o)

	shared := slurp(t, l.Memory())
	if !strings.Contains(shared, "url: "+teamAddr) {
		t.Fatalf("the shared file must carry the team address:\n%s", shared)
	}
	for _, bad := range []string{"role:", "instance:", "token:", "postgres_connection:", "127.0.0.1", "localhost"} {
		if strings.Contains(shared, bad) {
			t.Fatalf("shared file must not carry %q:\n%s", bad, shared)
		}
	}
	mine := slurp(t, local)
	for _, want := range []string{"role: host", "token: env:" + DefaultTokenEnv, "postgres_connection: " + DefaultDatabaseRef, "url: " + DefaultServerURL} {
		if !strings.Contains(mine, want) {
			t.Fatalf("local file lacks %q:\n%s", want, mine)
		}
	}
	cfg, err := gabitotl.LoadMemory(l)
	if err != nil || cfg.URL != DefaultServerURL || cfg.Role != "host" {
		t.Fatalf("the host runs against its own address: %+v %v", cfg, err)
	}
	if mate, _ := gabitotl.LoadMemoryFor(l, "bob"); mate.URL != teamAddr || mate.Role != "" || mate.Token != "" {
		t.Fatalf("a teammate sees the team address and nothing of the host: %+v", mate)
	}
	if res.Team == nil || res.Team.URL != teamAddr {
		t.Fatalf("the summary names the address teammates use: %+v", res.Team)
	}
	// A re-run changes nothing.
	if _, p2, err := r.plan(o); err != nil || p2.Mutates() {
		t.Fatalf("a re-run must be a no-op: %v %+v", err, p2.Items)
	}
}

func TestHostRejectsALoopbackAddressForTheSharedFile(t *testing.T) {
	r, l, _ := splitRig(t)
	for _, bad := range []string{"http://127.0.0.1:8765/mcp", "http://localhost:8765/mcp", "http://[::1]:8765/mcp", "http://0.0.0.0:8765/mcp"} {
		o := hostOpts()
		o.MemoryURL = bad
		_, _, err := r.plan(o)
		if !errors.Is(err, ErrTeamOptions) || !strings.Contains(err.Error(), "teammates cannot use it") {
			t.Errorf("%s: want a clear ErrTeamOptions, got %v", bad, err)
		}
	}
	if _, err := gabitotl.LoadMemory(l); err == nil {
		t.Fatal("a refused plan must write nothing")
	}
}

func TestHostWithoutATeamURLSharesNoAddressAndSaysSo(t *testing.T) {
	r, l, local := splitRig(t)
	inv, pl, err := r.plan(hostOpts())
	if err != nil {
		t.Fatal(err)
	}
	if !pl.HasNote(NoteTeamURLMissing) {
		t.Fatalf("one line must say teammates cannot connect yet: %+v", pl.Notes)
	}
	if _, err := Run(context.Background(), r.env, inv, pl, nil, Hooks{}); err != nil {
		t.Fatal(err)
	}
	shared := slurp(t, l.Memory())
	if strings.Contains(shared, "url:") || strings.Contains(shared, "127.0.0.1") {
		t.Fatalf("no loopback address may be committed:\n%s", shared)
	}
	if !strings.Contains(slurp(t, local), "url: "+DefaultServerURL) {
		t.Fatalf("the host still runs against its own address:\n%s", slurp(t, local))
	}
	// Publishing the address later is the same flow, and moves nothing else.
	o := hostOpts()
	o.MemoryURL = teamAddr
	applyOpts(t, r, o)
	if !strings.Contains(slurp(t, l.Memory()), "url: "+teamAddr) {
		t.Fatalf("the address must be published:\n%s", slurp(t, l.Memory()))
	}
	if cfg, _ := gabitotl.LoadMemory(l); cfg.URL != DefaultServerURL {
		t.Fatalf("the local override stays: %+v", cfg)
	}
}

func TestHostKeepsAnAddressAnEarlierRunShared(t *testing.T) {
	r, l, _ := splitRig(t)
	o := hostOpts()
	o.MemoryURL = teamAddr
	applyOpts(t, r, o)
	// `--url` is not needed again: the shared address is kept and no warning is raised.
	_, pl, err := r.plan(hostOpts())
	if err != nil || pl.HasNote(NoteTeamURLMissing) || pl.Team.URL != teamAddr {
		t.Fatalf("%v %+v", err, pl.Team)
	}
	applyOpts(t, r, hostOpts())
	if !strings.Contains(slurp(t, l.Memory()), "url: "+teamAddr) {
		t.Fatal("the shared address must survive")
	}
}

func TestTeammateAfterPullBecomesAClientFromTheSharedFile(t *testing.T) {
	r, l, local := splitRig(t)
	writeFile(t, l.Memory(), "---\nmode: team\nprovider: memory-mcp\nbackend: postgresql\nproject_key: "+DetectProject(context.Background(), r.env).Key+
		"\nurl: "+teamAddr+"\n---\n\n# Memory\n")
	cfg, _ := gabitotl.LoadMemory(l)
	if !cfg.AwaitsClientSetup() {
		t.Fatalf("the pulled project must read as a teammate waiting for setup: %+v", cfg)
	}
	// Non-interactive: no role or url flags are needed.
	inv, pl, err := r.plan(Options{Runtimes: []string{"claude-code"}, MemoryTokenEnv: "MI_TOKEN"})
	if err != nil {
		t.Fatal(err)
	}
	if pl.Team == nil || pl.Team.Role != "client" || pl.Team.URL != teamAddr || pl.Team.TokenEnv != "MI_TOKEN" {
		t.Fatalf("%+v", pl.Team)
	}
	if !pl.HasNote(NoteTeamTokenMissing) {
		t.Fatalf("an unset token variable tells the person to ask the host: %+v", pl.Notes)
	}
	if _, err := Run(context.Background(), r.env, inv, pl, nil, Hooks{}); err != nil {
		t.Fatal(err)
	}
	mine := slurp(t, local)
	if !strings.Contains(mine, "role: client") || !strings.Contains(mine, "token: env:MI_TOKEN") || strings.Contains(mine, "url:") {
		t.Fatalf("per-person settings:\n%s", mine)
	}
	if shared := slurp(t, l.Memory()); !strings.Contains(shared, "url: "+teamAddr) || strings.Contains(shared, "role:") || strings.Contains(shared, "MI_TOKEN") {
		t.Fatalf("the committed file must stay shared-only:\n%s", shared)
	}
	if got, _ := gabitotl.LoadMemory(l); got.AwaitsClientSetup() || got.Role != "client" {
		t.Fatalf("%+v", got)
	}
}

func TestTeammateTokenNoteDisappearsWhenTheVariableIsSet(t *testing.T) {
	r, l, _ := splitRig(t)
	writeFile(t, l.Memory(), "---\nmode: team\nprovider: memory-mcp\nproject_key: p\nurl: "+teamAddr+"\n---\n")
	r.vars["MI_TOKEN"] = "set"
	_, pl, err := r.plan(Options{Runtimes: []string{"claude-code"}, MemoryTokenEnv: "MI_TOKEN"})
	if err != nil || pl.HasNote(NoteTeamTokenMissing) {
		t.Fatalf("%v %+v", err, pl.Notes)
	}
}

func TestDoctorWarnsAboutALoopbackURLInTheSharedFile(t *testing.T) {
	r, l, _ := splitRig(t)
	// What the old team flow committed for a host.
	if _, err := gabitotl.Init(r.root, gabitotl.InitOptions{Name: "shop"}); err != nil {
		t.Fatal(err)
	}
	writeFile(t, l.Memory(), "---\nmode: team\nprovider: memory-mcp\nbackend: postgresql\nproject_key: p\npostgres_connection: env:GABITOTL_MEMORY_DATABASE_URL\nurl: http://127.0.0.1:8765/mcp\ntoken: env:"+DefaultTokenEnv+"\nrole: host\n---\n")
	var loop, legacy *Line
	for _, s := range Doctor(context.Background(), r.env).Sections {
		for i := range s.Lines {
			switch s.Lines[i].Code {
			case DocMemSharedLoopback:
				loop = &s.Lines[i]
			case DocMemLegacyFields:
				legacy = &s.Lines[i]
			}
		}
	}
	if loop == nil || loop.Status != LineWarn || !strings.Contains(loop.Detail, "127.0.0.1") {
		t.Fatalf("the committed loopback url must be warned about: %+v", loop)
	}
	if legacy == nil || legacy.Status != LineWarn || !strings.Contains(legacy.Detail, "role") || !strings.Contains(legacy.Detail, "token") {
		t.Fatalf("legacy per-machine fields must be listed: %+v", legacy)
	}
	// After the next write both are gone.
	cfg, _ := gabitotl.LoadMemory(l)
	if err := gabitotl.SaveMemory(l, cfg); err != nil {
		t.Fatal(err)
	}
	for _, s := range Doctor(context.Background(), r.env).Sections {
		for _, ln := range s.Lines {
			if ln.Code == DocMemSharedLoopback || ln.Code == DocMemLegacyFields {
				t.Fatalf("still warns after the split: %+v", ln)
			}
		}
	}
}

func TestDoctorRecognisesATeammateWaitingForTheirToken(t *testing.T) {
	r, l, _ := splitRig(t)
	if _, err := gabitotl.Init(r.root, gabitotl.InitOptions{Name: "shop"}); err != nil {
		t.Fatal(err)
	}
	writeFile(t, l.Memory(), "---\nmode: team\nprovider: memory-mcp\nproject_key: p\nurl: "+teamAddr+"\n---\n")
	var got *Line
	for _, s := range Doctor(context.Background(), r.env).Sections {
		for i := range s.Lines {
			if s.Lines[i].Code == DocMemTeammate {
				got = &s.Lines[i]
			}
		}
	}
	if got == nil || got.Status != LineWarn || got.Detail != teamAddr {
		t.Fatalf("%+v", got)
	}
}

func TestImportNoticeDependsOnTheRole(t *testing.T) {
	r := newTeamRig(t)
	notice := func(cfg gabitotl.MemoryConfig, why string) Note {
		run := &run{env: r.env, res: &Result{}}
		run.importSkipped(cfg, why)
		if len(run.res.Notices) != 1 {
			t.Fatalf("one notice expected: %+v", run.res.Notices)
		}
		return run.res.Notices[0]
	}
	why := "connection: environment variable GABITOTL_MEMORY_TOKEN is not set"
	host := gabitotl.MemoryConfig{Role: gabitotl.RoleHost, Token: "env:GABITOTL_MEMORY_TOKEN"}
	if n := notice(host, "unreachable"); n.Code != NoticeImportSkippedHost || n.Detail != "unreachable" {
		t.Fatalf("host advice stays for hosts: %+v", n)
	}
	client := gabitotl.MemoryConfig{Role: gabitotl.RoleClient, Token: "env:GABITOTL_MEMORY_TOKEN"}
	if n := notice(client, why); n.Code != NoticeImportSkippedNoToken || n.Detail != "GABITOTL_MEMORY_TOKEN" {
		t.Fatalf("a client without a token variable is told to ask the host: %+v", n)
	}
	r.vars["GABITOTL_MEMORY_TOKEN"] = "set"
	if n := notice(client, "unreachable"); n.Code != NoticeImportSkippedClient {
		t.Fatalf("a client with its token gets no host advice: %+v", n)
	}
}

func TestLocalMemoryFileIsPartOfTheSnapshot(t *testing.T) {
	r, l, local := splitRig(t)
	inv := BuildInventory(context.Background(), r.env, nil)
	pl, err := BuildPlan(context.Background(), r.env, inv, clientOpts())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range projectPaths(r.env, pl) {
		if filepath.Clean(p) == filepath.Clean(local) {
			found = true
		}
	}
	if !found {
		t.Fatalf("a rollback must restore %s", local)
	}
	_ = l
}
