package gabitotl

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSlugAndValidID(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Gabi", "gabi"},
		{"Gabi Pérez", "gabi-perez"},
		{"  Ñandú  López ", "nandu-lopez"},
		{"a/b\\c..d", "a-b-c-d"},
		{"--x--", "x"},
		{"日本", ""},
		{"", ""},
	}
	for _, tc := range tests {
		if got := Slug(tc.in); got != tc.want {
			t.Errorf("Slug(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	for _, bad := range []string{"", "../x", "A", "a b", "a/b"} {
		if ValidID(bad) {
			t.Errorf("ValidID(%q) = true", bad)
		}
	}
	if !ValidID("auth-login") {
		t.Error("auth-login should be valid")
	}
	if len(Slug(strings.Repeat("a", 200))) > MaxIDLen {
		t.Error("slug not truncated")
	}
}

func TestLayoutPaths(t *testing.T) {
	l := NewLayout("/p")
	want := map[string]string{
		l.Project(): "/p/.gabitotl/project.md", l.Memory(): "/p/.gabitotl/memory.md",
		l.Runtime(): "/p/.gabitotl/runtime.md", l.VCS(): "/p/.gabitotl/vcs.md",
		l.Workflow(): "/p/.gabitotl/workflow.md", l.Imports(): "/p/.gabitotl/imports.md",
		l.LocalUser(): "/p/.gabitotl/local/user.md", l.LocalRuntime(): "/p/.gabitotl/local/runtime.md",
		l.LocalSession(): "/p/.gabitotl/local/session.md", l.Bak(): "/p/.gabitotl/.bak",
		l.Tmp(): "/p/.gabitotl/tmp", l.Gitignore(): "/p/.gabitotl/.gitignore",
	}
	for got, w := range want {
		if got != w {
			t.Errorf("got %s want %s", got, w)
		}
	}
	if p, err := l.UserFile("gabi"); err != nil || p != "/p/.gabitotl/users/gabi.md" {
		t.Errorf("UserFile = %q, %v", p, err)
	}
	if p, err := l.WorkFile("gabi", "auth-login"); err != nil || p != "/p/.gabitotl/work/gabi/auth-login.md" {
		t.Errorf("WorkFile = %q, %v", p, err)
	}
	if p, err := l.BakUser("gabi"); err != nil || p != "/p/.gabitotl/.bak/gabi" {
		t.Errorf("BakUser = %q, %v", p, err)
	}
	if _, err := l.WorkFile("../etc", "x"); err == nil {
		t.Error("path traversal must be rejected")
	}
	if _, err := l.UserFile("A B"); err == nil {
		t.Error("non-slug id must be rejected")
	}
}

func TestParseDoc(t *testing.T) {
	tests := []struct {
		name, in  string
		hasFront  bool
		front     string
		body      string
		wantError bool
	}{
		{"front and body", "---\na: 1\n---\n\n# T\n", true, "a: 1\n", "\n# T\n", false},
		{"no front matter", "# Only body\n", false, "", "# Only body\n", false},
		{"empty front", "---\n---\nbody", true, "", "body", false},
		{"crlf fences", "---\r\na: 1\r\n---\r\nbody\r\n", true, "a: 1\r\n", "body\r\n", false},
		{"bom", "\xef\xbb\xbf---\na: 1\n---\nx", true, "a: 1\n", "x", false},
		{"body has fence", "---\na: 1\n---\ntext\n---\nmore\n", true, "a: 1\n", "text\n---\nmore\n", false},
		{"unterminated", "---\na: 1\n", false, "", "", true},
		{"fence not first", "\n---\na: 1\n---\n", false, "", "\n---\na: 1\n---\n", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d, err := ParseDoc([]byte(tc.in))
			if (err != nil) != tc.wantError {
				t.Fatalf("err = %v", err)
			}
			if tc.wantError {
				return
			}
			if d.HasFront != tc.hasFront || string(d.Front) != tc.front || d.Body != tc.body {
				t.Fatalf("got %+v", d)
			}
		})
	}
}

func TestRoundTrip(t *testing.T) {
	body := "\n# Title\n\nSome *markdown*\n\n---\n\n- item\n```yaml\nkey: value\n```\n"
	in := ProjectConfig{Name: "ecommerce", ProjectKind: KindExisting, Status: "active"}
	data, err := RenderDoc(in, body)
	if err != nil {
		t.Fatal(err)
	}
	d, err := ParseDoc(data)
	if err != nil {
		t.Fatal(err)
	}
	if d.Body != body {
		t.Fatalf("body changed: %q", d.Body)
	}
	var out ProjectConfig
	if err := d.Decode(&out); err != nil || out != in {
		t.Fatalf("decode = %+v, %v", out, err)
	}
	if string(d.Bytes()) != string(data) {
		t.Fatalf("Bytes() differs:\n%s\nvs\n%s", d.Bytes(), data)
	}
	if err := (Doc{Body: "x"}).Decode(&out); err == nil {
		t.Error("decode without front matter must fail")
	}
}

func TestSpecExamplesParse(t *testing.T) {
	spec := "---\nmode: team\nprovider: memory-mcp\nbackend: postgresql\nproject_key: ecommerce\npostgres_connection: env:GABITOTL_MEMORY_DATABASE_URL\n---\n\n# Memoria\n"
	d, err := ParseDoc([]byte(spec))
	if err != nil {
		t.Fatal(err)
	}
	var m MemoryConfig
	if err := d.Decode(&m); err != nil {
		t.Fatal(err)
	}
	want := MemoryConfig{Mode: ModeTeam, Provider: MemoryMemoryMCP, Backend: "postgresql", ProjectKey: "ecommerce", PostgresConnection: "env:GABITOTL_MEMORY_DATABASE_URL"}
	if !reflect.DeepEqual(m, want) || m.Validate() != nil {
		t.Fatalf("got %+v", m)
	}
}

func TestConfigValidation(t *testing.T) {
	good := DefaultVCS("github")
	tests := []struct {
		name string
		v    interface{ Validate() error }
		ok   bool
	}{
		{"project ok", &ProjectConfig{"x", KindNew, "active"}, true},
		{"project bad kind", &ProjectConfig{"x", "maybe", "active"}, false},
		{"project no name", &ProjectConfig{"", KindNew, "active"}, false},
		{"memory ok", &MemoryConfig{Mode: ModeSolo, Provider: MemoryEngram, ProjectKey: "k"}, true},
		{"memory bad mode", &MemoryConfig{Mode: "duo", Provider: MemoryEngram, ProjectKey: "k"}, false},
		{"memory bad provider", &MemoryConfig{Mode: ModeSolo, Provider: "redis", ProjectKey: "k"}, false},
		{"memory literal connection", &MemoryConfig{Mode: ModeTeam, Provider: MemoryMemoryMCP, ProjectKey: "k", PostgresConnection: "postgres://u:p@h/db"}, false},
		{"runtime ok", &RuntimeConfig{Runtime: "opencode"}, true},
		{"runtime empty", &RuntimeConfig{}, false},
		{"vcs ok", &good, true},
		{"vcs bad provider", func() *VCSConfig { c := DefaultVCS("svn"); return &c }(), false},
		{"vcs bad perm", func() *VCSConfig { c := DefaultVCS("git"); c.Permissions.Push = "maybe"; return &c }(), false},
		{"vcs literal auth", func() *VCSConfig { c := DefaultVCS("git"); c.Auth = "hunter2"; return &c }(), false},
		{"vcs native auth", func() *VCSConfig { c := DefaultVCS("github"); c.Auth = "native"; return &c }(), true},
		{"workflow ok", func() *WorkflowConfig { c := DefaultWorkflow(); return &c }(), true},
		{"workflow bad backup", func() *WorkflowConfig { c := DefaultWorkflow(); c.Backup.MaxSnapshots = 0; return &c }(), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.v.Validate(); (err == nil) != tc.ok {
				t.Fatalf("err = %v, want ok=%v", err, tc.ok)
			}
		})
	}
	if w := DefaultWorkflow(); w.Backup.MaxSnapshots != 20 || w.Backup.MaxAgeDays != 7 {
		t.Errorf("backup defaults = %+v", w.Backup)
	}
}

func TestSecretGuard(t *testing.T) {
	reject := map[string]string{
		"password kv":       "password: hunter2",
		"password quoted":   `db_password: "s3cr3t!"`,
		"token kv":          "github_token: abc123",
		"api key":           "api_key: 12345",
		"api-key eq":        "API-KEY=zzz",
		"list item secret":  "- client_secret: abcdef",
		"url with password": "postgres_connection: postgres://admin:p4ss@db.internal:5432/app",
		"mongodb srv":       "uri: mongodb+srv://user:pw@cluster0.mongodb.net/db",
		"dsn password":      "Host=db;User=sa;Password=Sup3rS3cret;Database=x",
		"github pat":        "note: ghp_abcdefghijklmnopqrstuvwxyz0123456789",
		"github fine":       "x: github_pat_11ABCDEFG0abcdefghijklmnopqrstuv",
		"aws key":           "key id AKIAIOSFODNN7EXAMPLE here",
		"openai style":      "use sk-abcdefghijklmnopqrstuvwxyz123456",
		"slack":             "xoxb-123456789012-abcdefghijkl",
		"jwt":               "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.abcdefghijklmnop",
		"private key":       "-----BEGIN RSA PRIVATE KEY-----",
		"bearer":            "Authorization: Bearer abcdefghijklmnopqrstuvwxyz",
		"gitlab":            "glpat-abcdefghij0123456789",
	}
	for name, line := range reject {
		t.Run("reject "+name, func(t *testing.T) {
			err := ValidateNoSecrets([]byte("---\nok: 1\n---\n\n" + line + "\n"))
			se, ok := err.(*SecretError)
			if !ok {
				t.Fatalf("expected SecretError, got %v", err)
			}
			if se.Line != 5 {
				t.Errorf("line = %d, want 5", se.Line)
			}
			for _, frag := range []string{"hunter2", "s3cr3t", "p4ss", "Sup3rS3cret"} {
				if strings.Contains(se.Error(), frag) {
					t.Errorf("error leaks secret: %v", se)
				}
			}
		})
	}
	accept := []string{
		"postgres_connection: env:GABITOTL_MEMORY_DATABASE_URL",
		"github_auth: native",
		"api_key: keychain:gabitotl-api",
		"token: env:GITHUB_TOKEN # from the environment",
		"password: \"env:DB_PASSWORD\"",
		"branch_policy: task",
		"max_tokens: 4000",
		"See https://example.com/docs for details",
		"url: postgres://db.internal:5432/app",
		"Never store a password or token here.",
	}
	for _, line := range accept {
		if err := ValidateNoSecrets([]byte(line + "\n")); err != nil {
			t.Errorf("%q rejected: %v", line, err)
		}
	}
}

func TestWriteGuard(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.md")
	if err := WriteFile(p, []byte("password: hunter2\n")); err == nil {
		t.Fatal("secret write must fail")
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("file must not exist after rejected write")
	}
	if _, err := WriteFileIfMissing(p, []byte("token: abc\n")); err == nil {
		t.Fatal("secret create must fail")
	}
	c := DefaultVCS("github")
	c.Auth = "hunter2"
	if err := SaveVCS(NewLayout(dir), c); err == nil {
		t.Fatal("SaveVCS with literal auth must fail")
	}
	if err := WriteDoc(p, map[string]string{"api_key": "abc"}, "body"); err == nil {
		t.Fatal("WriteDoc with secret front matter must fail")
	}
}

func TestInitCreatesAndIsIdempotent(t *testing.T) {
	root := t.TempDir()
	if Exists(root) {
		t.Fatal("Exists before init")
	}
	res, err := Init(root, InitOptions{Name: "Shop", Kind: KindNew, Mode: ModeTeam})
	if err != nil {
		t.Fatal(err)
	}
	if !Exists(root) || len(res.Created) == 0 || len(res.Skipped) != 0 {
		t.Fatalf("first init: %+v", res)
	}
	l := NewLayout(root)
	for _, p := range []string{l.Project(), l.Memory(), l.Runtime(), l.VCS(), l.Workflow(), l.Imports(), l.Gitignore(), l.Users(), l.Local(), l.Work()} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("missing %s", p)
		}
	}
	gi, _ := os.ReadFile(l.Gitignore())
	if string(gi) != "local/\n.bak/\ntmp/\n" {
		t.Errorf("gitignore = %q", gi)
	}
	mem, err := LoadMemory(l)
	if err != nil || mem.Mode != ModeTeam || mem.Provider != MemoryMemoryMCP || mem.PostgresConnection != "env:GABITOTL_MEMORY_DATABASE_URL" || mem.ProjectKey != "shop" {
		t.Fatalf("memory = %+v, %v", mem, err)
	}
	proj, err := LoadProject(l)
	if err != nil || proj.Name != "Shop" || proj.ProjectKind != KindNew {
		t.Fatalf("project = %+v, %v", proj, err)
	}
	if wf, err := LoadWorkflow(l); err != nil || wf != DefaultWorkflow() {
		t.Fatalf("workflow = %+v, %v", wf, err)
	}
	if _, err := LoadRuntime(l); err != nil {
		t.Fatal(err)
	}
	if v, err := LoadVCS(l); err != nil || v != DefaultVCS("git") {
		t.Fatalf("vcs = %+v, %v", v, err)
	}

	// User edits must survive a second init.
	custom := []byte("---\nname: Custom\nproject_kind: existing\nstatus: paused\n---\n\nhand written\n")
	if err := os.WriteFile(l.Project(), custom, 0o644); err != nil {
		t.Fatal(err)
	}
	res2, err := Init(root, InitOptions{Name: "Other", Kind: KindNew})
	if err != nil {
		t.Fatal(err)
	}
	if len(res2.Created) != 0 || len(res2.Skipped) != len(res.Created) {
		t.Fatalf("second init: %+v", res2)
	}
	if got, _ := os.ReadFile(l.Project()); string(got) != string(custom) {
		t.Fatalf("project.md overwritten: %q", got)
	}
}

func TestInitDefaultsAndErrors(t *testing.T) {
	root := t.TempDir()
	if _, err := Init(root, InitOptions{Kind: "bogus"}); err == nil {
		t.Error("invalid kind must fail")
	}
	if Exists(root) {
		t.Error("nothing should be created on validation failure")
	}
	if _, err := Init(root, InitOptions{}); err != nil {
		t.Fatal(err)
	}
	mem, _ := LoadMemory(NewLayout(root))
	if mem.Mode != ModeSolo || mem.Provider != MemoryEngram {
		t.Errorf("defaults = %+v", mem)
	}
	if home, err := os.UserHomeDir(); err == nil {
		if _, err := Init(home, InitOptions{}); err == nil {
			t.Error("init in $HOME must be refused")
		}
	}
}

func TestSaveKeepsBody(t *testing.T) {
	root := t.TempDir()
	l := NewLayout(root)
	if _, err := Init(root, InitOptions{}); err != nil {
		t.Fatal(err)
	}
	d, _ := ReadDoc(l.Runtime())
	custom := Doc{Front: d.Front, HasFront: true, Body: "\nmy notes\n"}
	if err := os.WriteFile(l.Runtime(), custom.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveRuntime(l, RuntimeConfig{Runtime: "opencode", Model: "m", AllowModelSwitch: false}); err != nil {
		t.Fatal(err)
	}
	d, _ = ReadDoc(l.Runtime())
	if d.Body != "\nmy notes\n" {
		t.Errorf("body lost: %q", d.Body)
	}
	if rt, _ := LoadRuntime(l); rt.Runtime != "opencode" || rt.AllowModelSwitch {
		t.Errorf("runtime = %+v", rt)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func gitInitCommit(t *testing.T, dir string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "init"},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

func TestDetectKind(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, root string)
		want  string
	}{
		{"empty dir", func(*testing.T, string) {}, KindNew},
		{"readme only is weak", func(t *testing.T, r string) { writeFile(t, filepath.Join(r, "README.md"), "# hi") }, KindNew},
		{"only .gabitotl", func(t *testing.T, r string) { writeFile(t, filepath.Join(r, ".gabitotl", "project.md"), "x") }, KindNew},
		{"code inside .gabitotl ignored", func(t *testing.T, r string) { writeFile(t, filepath.Join(r, ".gabitotl", "x.go"), "package x") }, KindNew},
		{"code inside .git ignored", func(t *testing.T, r string) { writeFile(t, filepath.Join(r, ".git", "hook.py"), "x") }, KindNew},
		{"node_modules ignored", func(t *testing.T, r string) { writeFile(t, filepath.Join(r, "node_modules", "a", "i.js"), "x") }, KindNew},
		{"go.mod", func(t *testing.T, r string) { writeFile(t, filepath.Join(r, "go.mod"), "module x") }, KindExisting},
		{"package.json", func(t *testing.T, r string) { writeFile(t, filepath.Join(r, "package.json"), "{}") }, KindExisting},
		{"pyproject", func(t *testing.T, r string) { writeFile(t, filepath.Join(r, "pyproject.toml"), "") }, KindExisting},
		{"pom.xml", func(t *testing.T, r string) { writeFile(t, filepath.Join(r, "pom.xml"), "") }, KindExisting},
		{"Cargo.toml", func(t *testing.T, r string) { writeFile(t, filepath.Join(r, "Cargo.toml"), "") }, KindExisting},
		{"nested code", func(t *testing.T, r string) { writeFile(t, filepath.Join(r, "src", "app", "main.ts"), "") }, KindExisting},
		{"git history", func(t *testing.T, r string) { gitInitCommit(t, r) }, KindExisting},
		{"git init without commits", func(t *testing.T, r string) {
			if _, err := exec.LookPath("git"); err != nil {
				t.Skip("git not available")
			}
			if out, err := exec.Command("git", "-C", r, "init", "-q").CombinedOutput(); err != nil {
				t.Fatalf("%v %s", err, out)
			}
		}, KindNew},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			tc.setup(t, root)
			got, err := DetectKind(root)
			if err != nil || got != tc.want {
				t.Fatalf("DetectKind = %q, %v; want %q", got, err, tc.want)
			}
		})
	}
	if _, err := DetectKind(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Error("missing dir must error")
	}
}

func TestFindRootAndValidateRoot(t *testing.T) {
	repo := t.TempDir()
	gitInitCommit(t, repo)
	sub := filepath.Join(repo, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := FindRoot(sub)
	if err != nil || resolve(got) != resolve(repo) {
		t.Fatalf("FindRoot(sub) = %q, %v; want %q", got, err, repo)
	}
	plain := t.TempDir()
	if got, err := FindRoot(plain); err != nil || got != plain {
		t.Fatalf("FindRoot(plain) = %q, %v", got, err)
	}
	if err := ValidateRoot(os.TempDir()); err == nil {
		t.Error("temp dir must be refused")
	}
	if err := ValidateRoot("/"); err == nil {
		t.Error("/ must be refused")
	}
	if home, err := os.UserHomeDir(); err == nil {
		if err := ValidateRoot(home); err == nil {
			t.Error("$HOME must be refused")
		}
	}
	if err := ValidateRoot(plain); err != nil {
		t.Errorf("subdir of tmp must be allowed: %v", err)
	}
}

func TestMemoryConfigTeamFields(t *testing.T) {
	ok := MemoryConfig{Mode: ModeTeam, Provider: MemoryMemoryMCP, ProjectKey: "p", URL: "https://memoria.example.com/mcp", Token: "env:GABITOTL_MEMORY_TOKEN", Role: RoleClient}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, mut := range map[string]func(*MemoryConfig){
		"literal token":  func(c *MemoryConfig) { c.Token = "gbt_0123456789abcdef.secretsecretsecretsecret" },
		"url with creds": func(c *MemoryConfig) { c.URL = "https://u:p@memoria.example.com/mcp" },
		"bad url":        func(c *MemoryConfig) { c.URL = "memoria" },
		"bad role":       func(c *MemoryConfig) { c.Role = "boss" },
	} {
		c := ok
		mut(&c)
		if c.Validate() == nil {
			t.Errorf("%s must be refused", name)
		}
	}
}
