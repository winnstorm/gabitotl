package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/winnstorm/gabitotl/internal/installer"
	"github.com/winnstorm/gabitotl/internal/memory"
	"github.com/winnstorm/gabitotl/internal/memory/store"
	rt "github.com/winnstorm/gabitotl/internal/runtime"
)

const migrateExport = `{"version":"0.2.0","sessions":[{"id":"s1","project":"shop","started_at":"2026-01-01 09:00:00","summary":"Did things."}],
"observations":[
 {"id":1,"sync_id":"a","session_id":"s1","type":"decision","title":"Use PostgreSQL","content":"Shared memory.","project":"shop","scope":"project","created_at":"2026-01-01 09:10:00","updated_at":"2026-01-01 09:10:00"},
 {"id":2,"sync_id":"b","session_id":"s1","type":"pattern","title":"Repo pattern","content":"SQL in repositories.","project":"shop","scope":"project","created_at":"2026-01-02 09:10:00","updated_at":"2026-01-02 09:10:00"},
 {"id":3,"sync_id":"c","session_id":"s1","type":"decision","title":"Private","content":"mine","project":"shop","scope":"personal","created_at":"2026-01-03 09:10:00","updated_at":"2026-01-03 09:10:00"},
 {"id":4,"sync_id":"d","session_id":"s2","type":"decision","title":"Other project","content":"x","project":"kraken","scope":"project","created_at":"2026-01-04 09:10:00","updated_at":"2026-01-04 09:10:00"}]}`

func fileHash(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func TestMemoryMigrateEngramDryRunThenImport(t *testing.T) {
	f := newSfix(t)
	opens := 0
	openStoreFn = func(context.Context, string) (store.Store, error) { opens++; return f.st, nil }
	f.envv["GABITOTL_MEMORY_DATABASE_URL"] = "x"
	dir := t.TempDir()
	export := filepath.Join(dir, "engram-export.json")
	if err := os.WriteFile(export, []byte(migrateExport), 0o600); err != nil {
		t.Fatal(err)
	}
	before := fileHash(t, export)

	// Dry run: counts, no store, no report.
	r := f.run("", "memory", "migrate", "engram", "--project", "shop", "--export", export, "--dry-run")
	if r.code != 0 || !strings.Contains(r.out, "2 se importarían") || !strings.Contains(r.out, "personal-scope 1") || !strings.Contains(r.out, "other-project 1") ||
		!strings.Contains(r.out, "no importé nada") || !strings.Contains(r.out, "legacy") {
		t.Fatalf("dry run: %+v", r)
	}
	if opens != 0 {
		t.Fatal("a dry run must not open the team database")
	}

	// Real run (no terminal needs --yes): imports, verifies, writes the report.
	r = f.run("", "memory", "migrate", "engram", "--project", "shop", "--export", export)
	if r.code == 0 || !strings.Contains(r.err, "terminal") {
		t.Fatalf("without a terminal and without --yes it must refuse: %+v", r)
	}
	r = f.run("", "memory", "migrate", "engram", "--project", "shop", "--export", export, "--yes")
	if r.code != 0 || !strings.Contains(r.out, "Importé 2 recuerdos") || !strings.Contains(r.out, "Verificado") || !strings.Contains(r.out, "Engram quedó intacto") {
		t.Fatalf("import: %+v", r)
	}
	rs, _ := f.st.Search(context.Background(), memory.Query{Text: "postgresql", Project: "shop"})
	if len(rs) != 1 || rs[0].Actor != "legacy" || rs[0].Source != "engram/gentle-ai" {
		t.Fatalf("%+v", rs)
	}
	// Idempotent re-run.
	r = f.run("", "memory", "migrate", "engram", "--project", "shop", "--export", export, "--yes")
	if r.code != 0 || !strings.Contains(r.out, "Importé 0 recuerdos; 2 ya estaban") {
		t.Fatalf("re-run: %+v", r)
	}
	if st, _ := f.st.Stats(context.Background()); st.Observations != 2 || st.Summaries != 1 {
		t.Fatalf("no duplicates: %+v", st)
	}
	if fileHash(t, export) != before {
		t.Fatal("the export (and so Engram's data) must stay byte-identical")
	}
	// The report lives under ~/.gabitotl/migrations and holds no content.
	reps, _ := filepath.Glob(filepath.Join(f.home, ".gabitotl", "migrations", "*_memory-import", "import-report.json"))
	if len(reps) == 0 {
		t.Fatal("the report must be written")
	}
	b, _ := os.ReadFile(reps[0])
	var rep map[string]any
	if json.Unmarshal(b, &rep) != nil || strings.Contains(string(b), "Shared memory") {
		t.Fatalf("report: %s", b)
	}
}

func TestMemoryMigrateEngramRefusals(t *testing.T) {
	f := newSfix(t)
	f.envv["GABITOTL_MEMORY_DATABASE_URL"] = "x"
	dir := t.TempDir()
	export := filepath.Join(dir, "e.json")
	_ = os.WriteFile(export, []byte(migrateExport), 0o600)
	empty := filepath.Join(dir, "empty.json")
	_ = os.WriteFile(empty, nil, 0o600)
	for name, args := range map[string][]string{
		"no scope":             {"--export", export, "--yes"},
		"both scopes":          {"--project", "shop", "--all-projects", "--export", export, "--yes"},
		"dry-run without file": {"--project", "shop", "--dry-run"},
		"empty backup":         {"--project", "shop", "--export", export, "--backup", empty, "--yes"},
		"missing backup":       {"--project", "shop", "--export", export, "--backup", filepath.Join(dir, "nope"), "--yes"},
		"target without proj":  {"--all-projects", "--target-project", "x", "--export", export, "--yes"},
		"not an export":        {"--project", "shop", "--export", empty, "--yes"},
	} {
		r := f.run("", append([]string{"memory", "migrate", "engram"}, args...)...)
		if r.code == 0 {
			t.Errorf("%s must be refused: %+v", name, r)
		}
	}
	if st, _ := f.st.Stats(context.Background()); st.Observations != 0 {
		t.Fatal("nothing may be imported by a refused run")
	}
	if r := f.run("", "memory", "migrate", "gentle"); r.code != ExitUsage {
		t.Fatalf("only engram is a known source: %+v", r)
	}
}

func TestMemoryMigrateEngramMakesTheSafetyExportWithTheOfficialCLI(t *testing.T) {
	f := newSfix(t)
	f.envv["GABITOTL_MEMORY_DATABASE_URL"] = "x"
	oldRT, oldRunner := runtimeEnvFn, installerRunnerFn
	t.Cleanup(func() { runtimeEnvFn, installerRunnerFn = oldRT, oldRunner })
	runtimeEnvFn = func(root string) (rt.Env, error) {
		return rt.Env{Home: f.home, ProjectRoot: root, GOOS: "linux", Getenv: func(k string) string { return f.envv[k] },
			LookPath: func(n string) (string, error) { return "/fake/bin/" + n, nil }}, nil
	}
	runner := &installer.FakeRunner{Handlers: map[string]func([]string) (string, error){
		"engram export": func(a []string) (string, error) {
			if err := os.WriteFile(a[1], []byte(migrateExport), 0o600); err != nil {
				return "", err
			}
			return "Exported to " + a[1] + "\n  Sessions: 1\n  Observations: 4\n  Prompts: 0\n", nil
		},
	}}
	installerRunnerFn = func() installer.Runner { return runner }
	r := f.run("", "memory", "migrate", "engram", "--project", "shop", "--yes")
	if r.code != 0 || !strings.Contains(r.out, "Importé 2 recuerdos") {
		t.Fatalf("%+v", r)
	}
	// Only the official export ran against Engram: no save, no doctor-driven change.
	for _, c := range runner.Calls {
		if !strings.HasPrefix(c, "engram export") {
			t.Fatalf("unexpected call against Engram: %q (all: %v)", c, runner.Calls)
		}
	}
	if len(runner.Calls) != 1 {
		t.Fatalf("exactly one export: %v", runner.Calls)
	}
}
