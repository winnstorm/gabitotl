package installer

import (
	"context"
	"strings"
	"testing"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/memory/mcp"
)

func clientOpts() Options {
	return Options{Memory: "memory-mcp", MemoryRole: "client", MemoryURL: "https://memoria.example.com/mcp", Runtimes: []string{"claude-code"}}
}

func TestClientMemoryMDHasNoDatabaseReference(t *testing.T) {
	r := newTeamRig(t)
	inv, pl, err := r.plan(clientOpts())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), r.env, inv, pl, nil, Hooks{}); err != nil {
		t.Fatal(err)
	}
	l := gabitotl.NewLayout(r.root)
	md := slurp(t, l.Memory())
	if strings.Contains(md, "postgres_connection") || strings.Contains(md, "GABITOTL_MEMORY_DATABASE_URL") {
		t.Fatalf("a client has no database:\n%s", md)
	}
	cfg, err := gabitotl.LoadMemory(l)
	if err != nil || cfg.Role != "client" || cfg.URL != "https://memoria.example.com/mcp" || cfg.Token != "env:"+DefaultTokenEnv || cfg.PostgresConnection != "" {
		t.Fatalf("%+v %v", cfg, err)
	}
	// The provider factory and doctor work without it.
	if _, err := mcp.NewFromConfig(cfg, mcp.Resolver{Getenv: func(string) string { return "" }, Home: r.home}); err != nil {
		t.Fatalf("the provider must not require a database reference: %v", err)
	}
	for _, s := range Doctor(context.Background(), r.env).Sections {
		for _, ln := range s.Lines {
			if ln.Status == LineError && strings.Contains(strings.ToLower(ln.Detail), "postgres") {
				t.Fatalf("doctor must not ask a client for a database: %+v", ln)
			}
		}
	}
}

func TestHostMemoryMDKeepsTheDatabaseReference(t *testing.T) {
	r := newTeamRig(t)
	inv, pl, _ := r.plan(hostOpts())
	if _, err := Run(context.Background(), r.env, inv, pl, nil, Hooks{}); err != nil {
		t.Fatal(err)
	}
	cfg, err := gabitotl.LoadMemory(gabitotl.NewLayout(r.root))
	if err != nil || cfg.PostgresConnection != DefaultDatabaseRef {
		t.Fatalf("%+v %v", cfg, err)
	}
}

func TestClientInstallTidiesAnOlderMemoryMD(t *testing.T) {
	r := newTeamRig(t)
	l := gabitotl.NewLayout(r.root)
	// What an older install wrote for a client.
	writeFile(t, l.Memory(), "---\nmode: team\nprovider: memory-mcp\nbackend: postgresql\nproject_key: "+DetectProject(context.Background(), r.env).Key+
		"\npostgres_connection: env:GABITOTL_MEMORY_DATABASE_URL\nurl: https://memoria.example.com/mcp\ntoken: env:"+DefaultTokenEnv+"\nrole: client\n---\n\n# Memory\n")
	inv, pl, err := r.plan(clientOpts())
	if err != nil {
		t.Fatal(err)
	}
	if !pl.Mutates() {
		t.Fatal("an older client file with a database reference is something to tidy")
	}
	if _, err := Run(context.Background(), r.env, inv, pl, nil, Hooks{}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(slurp(t, l.Memory()), "postgres_connection") {
		t.Fatal("the database reference must be gone")
	}
	if _, p2, _ := r.plan(clientOpts()); p2.Mutates() {
		t.Fatalf("and then a re-run is a no-op: %+v", p2.Items)
	}
}
