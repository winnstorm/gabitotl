package gabitotl

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const teamURL = "https://memoria.example.com/mcp"

func sharedTeamFile(extra string) string {
	return "---\nmode: team\nprovider: memory-mcp\nbackend: postgresql\nproject_key: shop\n" + extra + "---\n\n# Memory\n\nmy notes\n"
}

func TestLayoutLocalMemoryPath(t *testing.T) {
	l := NewLayout("/p")
	if p, err := l.LocalMemory("ana"); err != nil || filepath.ToSlash(p) != "/p/.gabitotl/local/users/ana/memory.md" {
		t.Fatalf("got %s, %v", p, err)
	}
	if _, err := l.LocalMemory("../x"); err == nil {
		t.Fatal("an invalid id must be rejected")
	}
}

func TestLoadMemoryMergesLocalOverShared(t *testing.T) {
	l := NewLayout(t.TempDir())
	writeFile(t, l.Memory(), sharedTeamFile("url: "+teamURL+"\n"))
	p, _ := l.LocalMemory("ana")
	writeFile(t, p, "---\nrole: host\ninstance: acme\ntoken: env:MY_TOKEN\npostgres_connection: env:GABITOTL_MEMORY_DATABASE_URL\nurl: http://127.0.0.1:8765/mcp\n---\n")

	c, err := LoadMemoryFor(l, "ana")
	if err != nil {
		t.Fatal(err)
	}
	if c.URL != "http://127.0.0.1:8765/mcp" || c.Role != RoleHost || c.Instance != "acme" || c.Token != "env:MY_TOKEN" || c.ProjectKey != "shop" || c.Provider != MemoryMemoryMCP {
		t.Fatalf("local must win per field: %+v", c)
	}
	// Another person reads the shared settings only.
	b, err := LoadMemoryFor(l, "bob")
	if err != nil || b.URL != teamURL || b.Role != "" || b.Token != "" {
		t.Fatalf("bob = %+v, %v", b, err)
	}
	// A local file without a url leaves the shared one.
	writeFile(t, p, "---\nrole: client\ntoken: env:MY_TOKEN\n---\n")
	if c, _ = LoadMemoryFor(l, "ana"); c.URL != teamURL || c.Role != RoleClient {
		t.Fatalf("shared url must remain: %+v", c)
	}
}

func TestLoadMemoryLegacyFallback(t *testing.T) {
	l := NewLayout(t.TempDir())
	writeFile(t, l.Memory(), sharedTeamFile("postgres_connection: env:GABITOTL_MEMORY_DATABASE_URL\nurl: "+teamURL+"\ntoken: env:LEGACY\nrole: client\n"))
	c, err := LoadMemoryFor(l, "ana")
	if err != nil || c.Role != RoleClient || c.Token != "env:LEGACY" || c.URL != teamURL || c.PostgresConnection == "" {
		t.Fatalf("legacy fields must keep working: %+v, %v", c, err)
	}
	o := InspectMemory(l, "ana")
	if len(o.LegacyFields) != 3 || o.HasLocal || o.SharedURL != teamURL {
		t.Fatalf("origin = %+v", o)
	}
	// A local file wins over the legacy fields, field by field.
	p, _ := l.LocalMemory("ana")
	writeFile(t, p, "---\ntoken: env:MINE\n---\n")
	if c, _ = LoadMemoryFor(l, "ana"); c.Token != "env:MINE" || c.Role != RoleClient {
		t.Fatalf("local token must win, legacy role stay: %+v", c)
	}
}

func TestSaveMemorySplitsFields(t *testing.T) {
	l := NewLayout(t.TempDir())
	writeFile(t, l.Memory(), sharedTeamFile("# keep me\nextra_key: kept\n"))
	c, _ := LoadMemoryFor(l, "ana")
	c.URL, c.Role, c.Token = teamURL, RoleClient, "env:GABITOTL_MEMORY_TOKEN"
	if err := SaveMemoryFor(l, "ana", c); err != nil {
		t.Fatal(err)
	}
	shared := slurp(t, l.Memory())
	for _, bad := range []string{"role:", "token:", "instance:", "postgres_connection:"} {
		if strings.Contains(shared, bad) {
			t.Fatalf("shared file must not carry %q:\n%s", bad, shared)
		}
	}
	for _, want := range []string{"url: " + teamURL, "extra_key: kept", "# keep me", "my notes"} {
		if !strings.Contains(shared, want) {
			t.Fatalf("shared file lost %q:\n%s", want, shared)
		}
	}
	p, _ := l.LocalMemory("ana")
	local := slurp(t, p)
	if !strings.Contains(local, "role: client") || !strings.Contains(local, "token: env:GABITOTL_MEMORY_TOKEN") || strings.Contains(local, "url:") {
		t.Fatalf("local file:\n%s", local)
	}
	if got, err := LoadMemoryFor(l, "ana"); err != nil || got != c {
		t.Fatalf("round trip = %+v, %v; want %+v", got, err, c)
	}
}

func TestSaveMemoryMovesLegacyOnce(t *testing.T) {
	l := NewLayout(t.TempDir())
	writeFile(t, l.Memory(), sharedTeamFile("postgres_connection: env:GABITOTL_MEMORY_DATABASE_URL\nurl: http://127.0.0.1:8765/mcp\ntoken: env:GABITOTL_MEMORY_TOKEN\nrole: host\ninstance: acme\nextra_key: kept\n"))
	before, _ := LoadMemoryFor(l, "ana")
	if err := SaveMemoryFor(l, "ana", before); err != nil {
		t.Fatal(err)
	}
	shared := slurp(t, l.Memory())
	for _, bad := range []string{"role:", "token:", "instance:", "postgres_connection:", "127.0.0.1", "url:"} {
		if strings.Contains(shared, bad) {
			t.Fatalf("legacy field %q must leave the shared file:\n%s", bad, shared)
		}
	}
	if !strings.Contains(shared, "extra_key: kept") || !strings.Contains(shared, "my notes") {
		t.Fatalf("unknown keys and body must stay:\n%s", shared)
	}
	if o := InspectMemory(l, "ana"); len(o.LegacyFields) != 0 || !o.HasLocal {
		t.Fatalf("origin after the move = %+v", o)
	}
	after, err := LoadMemoryFor(l, "ana")
	if err != nil || after != before {
		t.Fatalf("the merged view must not change: %+v vs %+v (%v)", after, before, err)
	}
	if _, err := os.Stat(l.Memory()); err != nil {
		t.Fatal("the shared file must never be deleted")
	}
}

func TestSaveMemoryTeamURLKeepsHostLocalOverride(t *testing.T) {
	l := NewLayout(t.TempDir())
	writeFile(t, l.Memory(), sharedTeamFile(""))
	c, _ := LoadMemoryFor(l, "ana")
	c.URL, c.Role, c.Token = "http://127.0.0.1:8765/mcp", RoleHost, "env:GABITOTL_MEMORY_TOKEN"
	if err := SaveMemoryTeamURL(l, "ana", c, teamURL); err != nil {
		t.Fatal(err)
	}
	if o := InspectMemory(l, "ana"); o.SharedURL != teamURL {
		t.Fatalf("shared url = %q", o.SharedURL)
	}
	got, _ := LoadMemoryFor(l, "ana")
	if got.URL != "http://127.0.0.1:8765/mcp" {
		t.Fatalf("the host runs against its local address: %q", got.URL)
	}
	// Saving again without a team url must keep both.
	if err := SaveMemoryFor(l, "ana", got); err != nil {
		t.Fatal(err)
	}
	if o := InspectMemory(l, "ana"); o.SharedURL != teamURL {
		t.Fatalf("a re-save must not drop the shared url: %q", o.SharedURL)
	}
	if again, _ := LoadMemoryFor(l, "ana"); again != got {
		t.Fatalf("re-save changed the view: %+v vs %+v", again, got)
	}
	// A teammate sees the team address, not the host's loopback.
	if b, _ := LoadMemoryFor(l, "bob"); b.URL != teamURL {
		t.Fatalf("bob = %q", b.URL)
	}
}

func TestSaveMemoryNeverWritesLoopbackToShared(t *testing.T) {
	l := NewLayout(t.TempDir())
	writeFile(t, l.Memory(), sharedTeamFile(""))
	c, _ := LoadMemoryFor(l, "ana")
	c.URL, c.Role = "http://localhost:8765/mcp", RoleHost
	if err := SaveMemoryFor(l, "ana", c); err != nil {
		t.Fatal(err)
	}
	if shared := slurp(t, l.Memory()); strings.Contains(shared, "localhost") || strings.Contains(shared, "url:") {
		t.Fatalf("loopback leaked into the shared file:\n%s", shared)
	}
	if got, _ := LoadMemoryFor(l, "ana"); got.URL != "http://localhost:8765/mcp" {
		t.Fatalf("local override lost: %q", got.URL)
	}
	for _, bad := range []string{"http://127.0.0.1:8765/mcp", "http://[::1]:8765/mcp", "http://0.0.0.0:8765/mcp", "http://LocalHost/mcp"} {
		if err := SaveMemoryTeamURL(l, "ana", c, bad); !errors.Is(err, ErrLoopbackTeamURL) {
			t.Errorf("%s as the team url: err = %v, want ErrLoopbackTeamURL", bad, err)
		}
	}
}

func TestSaveMemoryWithoutUserKeepsOneFile(t *testing.T) {
	l := NewLayout(t.TempDir())
	writeFile(t, l.Memory(), sharedTeamFile(""))
	c, _ := LoadMemoryFor(l, "")
	c.URL, c.Role, c.Token = teamURL, RoleClient, "env:T"
	if err := SaveMemoryFor(l, "", c); err != nil {
		t.Fatal(err)
	}
	if got, err := LoadMemoryFor(l, ""); err != nil || got != c {
		t.Fatalf("got %+v, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(l.Local(), "users")); err == nil {
		t.Fatal("no per-person directory without an id")
	}
}

func TestSaveMemoryClearingURL(t *testing.T) {
	l := NewLayout(t.TempDir())
	writeFile(t, l.Memory(), sharedTeamFile("url: "+teamURL+"\n"))
	c, _ := LoadMemoryFor(l, "ana")
	c.URL = ""
	if err := SaveMemoryFor(l, "ana", c); err != nil {
		t.Fatal(err)
	}
	if got, _ := LoadMemoryFor(l, "ana"); got.URL != "" {
		t.Fatalf("url = %q", got.URL)
	}
}

func TestAwaitsClientSetup(t *testing.T) {
	base := MemoryConfig{Mode: ModeTeam, Provider: MemoryMemoryMCP, ProjectKey: "p", URL: teamURL}
	if !base.AwaitsClientSetup() {
		t.Fatal("a shared url with nothing per person is a teammate waiting for setup")
	}
	for name, mut := range map[string]func(*MemoryConfig){
		"has role":     func(c *MemoryConfig) { c.Role = RoleClient },
		"has token":    func(c *MemoryConfig) { c.Token = "env:T" },
		"loopback url": func(c *MemoryConfig) { c.URL = "http://127.0.0.1:8765/mcp" },
		"no url":       func(c *MemoryConfig) { c.URL = "" },
		"engram":       func(c *MemoryConfig) { c.Provider = MemoryEngram },
	} {
		c := base
		mut(&c)
		if c.AwaitsClientSetup() {
			t.Errorf("%s: must not await client setup", name)
		}
	}
}

func TestInitKeepsDatabaseRefOutOfSharedFile(t *testing.T) {
	root := t.TempDir()
	t.Setenv(EnvUser, "ana")
	if _, err := Init(root, InitOptions{Provider: MemoryMemoryMCP, Mode: ModeTeam}); err != nil {
		t.Fatal(err)
	}
	l := NewLayout(root)
	if strings.Contains(slurp(t, l.Memory()), "postgres_connection") {
		t.Fatal("the database reference is per machine")
	}
	if c, err := LoadMemory(l); err != nil || c.PostgresConnection == "" {
		t.Fatalf("merged view must still carry it: %+v, %v", c, err)
	}
}

func slurp(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
