package engrammcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/winnstorm/gabitotl/internal/memory"
	"github.com/winnstorm/gabitotl/internal/memory/migrate"
	"github.com/winnstorm/gabitotl/internal/memory/store"
	"github.com/winnstorm/gabitotl/internal/memory/store/memstore"
)

// fixture builds a hand-made export that follows the VERIFIED part of the
// schema documented in export.go. It is never produced by the real engram.
func fixture() map[string]any {
	obs := func(id int, sync, sess, typ, title, content, project, scope, topic, created, updated string, deleted any) map[string]any {
		o := map[string]any{"id": id, "sync_id": sync, "session_id": sess, "type": typ, "title": title, "content": content,
			"scope": scope, "revision_count": 1, "duplicate_count": 1, "created_at": created, "updated_at": updated}
		if project != "" {
			o["project"] = project
		}
		if topic != "" {
			o["topic_key"] = topic
		}
		if deleted != nil {
			o["deleted_at"] = deleted
		}
		return o
	}
	return map[string]any{
		"version": "0.2.0", "exported_at": "2026-10-05T21:00:00Z",
		"sessions": []any{
			map[string]any{"id": "s1", "project": "shop", "directory": "/work/shop", "started_at": "2026-01-01 09:00:00", "ended_at": "2026-01-01 10:00:00", "summary": "Moved checkout to the new API."},
			map[string]any{"id": "s2", "project": "kraken", "directory": "/work/kraken", "started_at": "2026-02-01 09:00:00"},
		},
		"observations": []any{
			obs(1, "sync-1", "s1", "decision", "Use PostgreSQL", "Shared memory for the team.", "shop", "project", "arch/db", "2026-01-01 09:10:00", "2026-01-02 09:10:00", nil),
			obs(2, "sync-2", "s1", "bugfix", "Fix refresh", "Expired token was reusable.\n\n**Actor**: gabi\n**Task**: auth-1\n**Branch**: fix/refresh\n**Files**: a.go, b.go\n**Timestamp**: 2026-01-03T10:00:00Z\n**Source**: gabitotl", "shop", "project", "", "2026-01-03T10:00:00Z", "2026-01-03T10:00:00Z", nil),
			obs(3, "sync-3", "s1", "pattern", "Repository pattern", "Keep SQL in repositories.", "shop", "project", "", "2026-01-04 08:00:00", "2026-01-04 08:00:00", nil),
			obs(4, "sync-4", "s1", "tool_use", "Ran tests", "go test ./...", "shop", "project", "", "2026-01-05 08:00:00", "2026-01-05 08:00:00", nil),
			obs(5, "sync-5", "s1", "decision", "Private note", "My own thoughts.", "shop", "personal", "", "2026-01-06 08:00:00", "2026-01-06 08:00:00", nil),
			obs(6, "sync-6", "s1", "decision", "Deleted one", "gone", "shop", "project", "", "2026-01-07 08:00:00", "2026-01-07 08:00:00", "2026-01-08 08:00:00"),
			obs(7, "sync-7", "s2", "discovery", "Kraken finding", "Different project.", "kraken", "project", "", "2026-02-02 08:00:00", "2026-02-02 08:00:00", nil),
			obs(8, "sync-8", "s2", "config", "Project from session", "No project on the row.", "", "project", "", "2026-02-03 08:00:00", "2026-02-03 08:00:00", nil),
			obs(9, "sync-9", "s1", "decision", "Empty body", "   ", "shop", "project", "", "2026-01-09 08:00:00", "2026-01-09 08:00:00", nil),
		},
		"prompts": []any{map[string]any{"id": 1, "sync_id": "p1", "session_id": "s1", "content": "do the thing", "created_at": "2026-01-01 09:01:00"}},
		"relations": []any{map[string]any{"sync_id": "r1", "source_id": "sync-3", "target_id": "sync-1", "relation": "related", "judgment_status": "judged",
			"created_at": "2026-01-10 08:00:00", "updated_at": "2026-01-10 08:00:00"}},
	}
}

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func sha(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

type rig struct {
	dir    string
	export string
	st     *memstore.Store
	m      *Migrator
	opens  int
}

func newRig(t *testing.T) *rig {
	t.Helper()
	r := &rig{dir: t.TempDir(), st: memstore.New()}
	r.export = filepath.Join(r.dir, "engram-export.json")
	writeJSON(t, r.export, fixture())
	r.m = New(func(context.Context, string) (store.Importer, func(), error) { r.opens++; return r.st, func() {}, nil })
	return r
}

func (r *rig) req() migrate.Request {
	return migrate.Request{ExportPath: r.export, BackupPath: r.export, ReportPath: filepath.Join(r.dir, "report.json")}
}

func TestDryRunReportsCountsAndTouchesNothing(t *testing.T) {
	r := newRig(t)
	before := sha(t, r.export)
	req := r.req()
	req.DryRun = true
	res, err := r.m.Migrate(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	rep := res.Report
	if rep.Status != "dry-run" || rep.Observations != 9 || rep.Eligible != 6 {
		t.Fatalf("%+v", rep)
	}
	if rep.Skipped[SkipDeleted] != 1 || rep.Skipped[SkipPersonal] != 1 || rep.Skipped[SkipEmpty] != 1 {
		t.Fatalf("skips: %+v", rep.Skipped)
	}
	if rep.ByProject["shop"] != 4 || rep.ByProject["kraken"] != 2 || rep.ByType["decision"] != 1 || rep.ByType["bugfix"] != 1 || rep.ByType["pattern"] != 1 {
		t.Fatalf("breakdown: %+v %+v", rep.ByProject, rep.ByType)
	}
	if rep.Prompts != 1 || rep.Summaries != 1 || rep.Relations != 1 {
		t.Fatalf("prompts/summaries/relations: %+v", rep)
	}
	if r.opens != 0 {
		t.Fatal("a dry run must not open the store")
	}
	if _, err := os.Stat(req.ReportPath); err == nil {
		t.Fatal("a dry run writes no report")
	}
	if st, _ := r.st.Stats(context.Background()); st.Observations != 0 || sha(t, r.export) != before {
		t.Fatal("nothing may change")
	}
}

func TestRefusesWithoutASafetyBackup(t *testing.T) {
	r := newRig(t)
	empty := filepath.Join(r.dir, "empty.json")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	notExport := filepath.Join(r.dir, "other.json")
	if err := os.WriteFile(notExport, []byte(`{"hello":"world"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, backup := range map[string]string{
		"not given": "", "missing": filepath.Join(r.dir, "nope.json"), "empty": empty, "not an export": notExport, "a directory": r.dir,
	} {
		req := r.req()
		req.BackupPath = backup
		if _, err := r.m.Migrate(context.Background(), req); !errors.Is(err, migrate.ErrNoBackup) {
			t.Errorf("%s: want ErrNoBackup, got %v", name, err)
		}
	}
	if r.opens != 0 {
		t.Fatal("the store must not even be opened without a backup")
	}
}

func TestImportPreservesWhatTheExportSays(t *testing.T) {
	r := newRig(t)
	exportBefore := sha(t, r.export)
	if err := os.Chmod(r.export, 0o400); err != nil { // the source is only ever read
		t.Fatal(err)
	}
	res, err := r.m.Migrate(context.Background(), r.req())
	if err != nil {
		t.Fatal(err)
	}
	rep := res.Report
	if rep.Status != "complete" || rep.Imported != 6 || rep.AlreadyPresent != 0 || !rep.Verified || rep.SampleChecked != 6 || rep.SampleOK != 6 || res.Migrated != 6 || res.Skipped != 3 {
		t.Fatalf("%+v", res)
	}
	ctx := context.Background()
	imp := store.Importer(r.st)
	get := func(id string) store.Record {
		t.Helper()
		rec, err := imp.BySourceRef(ctx, "engram:"+id)
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		return rec
	}
	d := get("sync-1")
	if d.Actor != memory.ActorLegacy || d.Source != SourceLabel || d.Project != "shop" || d.Type != memory.TypeDecision || d.TopicKey != "arch/db" ||
		d.Content != "Shared memory for the team." || d.CreatedAt.Format("2006-01-02 15:04:05") != "2026-01-01 09:10:00" || d.UpdatedAt.Format("2006-01-02") != "2026-01-02" {
		t.Fatalf("decision: %+v", d)
	}
	if got := d.Meta["engram_type"]; got != "decision" || d.Meta["engram_session"] != "s1" {
		t.Fatalf("meta: %+v", d.Meta)
	}
	// An entry GabitoTL itself saved keeps its original author and fields; the trailer is not duplicated into the body.
	b := get("sync-2")
	if b.Actor != "gabi" || b.Task != "auth-1" || b.Branch != "fix/refresh" || len(b.Files) != 2 || b.Content != "Expired token was reusable." || b.CreatedBy != "gabi" {
		t.Fatalf("bugfix: %+v", b)
	}
	// Types: ours pass through, the rest are mapped and the original is kept.
	if p := get("sync-3"); p.Type != memory.TypeConvention || p.Meta["engram_type"] != "pattern" || !strings.Contains(p.Meta["engram_relations"], "related->engram:sync-1") {
		t.Fatalf("pattern/relations: %+v", p)
	}
	if tu := get("sync-4"); tu.Type != memory.TypeDiscovery || tu.Meta["engram_type"] != "tool_use" {
		t.Fatalf("unknown type: %+v", tu)
	}
	if rep.MappedTypes["pattern -> convention"] != 1 || rep.MappedTypes["tool_use -> discovery"] != 1 {
		t.Fatalf("mapped: %+v", rep.MappedTypes)
	}
	// A row without a project takes its session's.
	if pr := get("sync-8"); pr.Project != "kraken" {
		t.Fatalf("session project: %+v", pr)
	}
	// Personal, deleted and empty observations never reach the team.
	for _, id := range []string{"sync-5", "sync-6", "sync-9"} {
		if _, err := imp.BySourceRef(ctx, "engram:"+id); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("%s must not be imported: %v", id, err)
		}
	}
	sums, _ := r.st.Summaries(ctx, "shop", 10)
	if len(sums) != 1 || sums[0].Actor != memory.ActorLegacy || sums[0].Accomplished != "Moved checkout to the new API." || rep.SummariesDone != 1 {
		t.Fatalf("summaries: %+v", sums)
	}
	if rep.KnownActors != 1 || rep.LegacyActors != 5 {
		t.Fatalf("actors: %+v", rep)
	}
	// Engram data is never modified: the export is byte-identical.
	if sha(t, r.export) != exportBefore {
		t.Fatal("the export was modified")
	}
	// The report is stored with mode 0600 and holds no content.
	rb, err := os.ReadFile(r.req().ReportPath)
	if err != nil || strings.Contains(string(rb), "Shared memory") || strings.Contains(string(rb), "go test") {
		t.Fatalf("report: %v %s", err, rb)
	}
	if st, _ := os.Stat(r.req().ReportPath); st.Mode().Perm() != 0o600 {
		t.Fatalf("report mode %v", st.Mode().Perm())
	}
}

func TestRerunNeverDuplicates(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	if _, err := r.m.Migrate(ctx, r.req()); err != nil {
		t.Fatal(err)
	}
	first, _ := r.st.Stats(ctx)
	res, err := r.m.Migrate(ctx, r.req())
	if err != nil {
		t.Fatal(err)
	}
	if res.Report.Imported != 0 || res.Report.AlreadyPresent != 6 || !res.Report.Verified || res.Report.SummariesDone != 0 {
		t.Fatalf("%+v", res.Report)
	}
	if again, _ := r.st.Stats(ctx); again.Observations != first.Observations || again.Summaries != first.Summaries {
		t.Fatalf("duplicated: %+v vs %+v", again, first)
	}
}

// flaky fails every Import after n successful ones with a non-validation error.
type flaky struct {
	*memstore.Store
	left int
}

func (f *flaky) Import(ctx context.Context, rec store.ImportRecord) (store.ImportResult, error) {
	if f.left == 0 {
		return store.ImportResult{}, errors.New("connection reset")
	}
	f.left--
	return f.Store.Import(ctx, rec)
}

func TestInterruptedRunResumes(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	fl := &flaky{Store: r.st, left: 3}
	r.m = New(func(context.Context, string) (store.Importer, func(), error) { return fl, func() {}, nil })
	res, err := r.m.Migrate(ctx, r.req())
	if err == nil || !strings.Contains(err.Error(), "resume") || res.Report.Status != "interrupted" || res.Report.Imported != 3 {
		t.Fatalf("want an interrupted run, got %+v %v", res.Report, err)
	}
	var saved migrate.Report
	b, _ := os.ReadFile(r.req().ReportPath)
	if json.Unmarshal(b, &saved) != nil || saved.Status != "interrupted" {
		t.Fatalf("the checkpoint must say so: %s", b)
	}
	// A cancelled context interrupts too.
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	r.m = New(func(context.Context, string) (store.Importer, func(), error) { return r.st, func() {}, nil })
	if _, err := r.m.Migrate(cctx, r.req()); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled: %v", err)
	}
	// Resume: the rest is imported, nothing twice.
	res, err = r.m.Migrate(ctx, r.req())
	if err != nil {
		t.Fatal(err)
	}
	if !res.Report.Resumed || res.Report.Imported != 3 || res.Report.AlreadyPresent != 3 || !res.Report.Verified || res.Report.Status != "complete" {
		t.Fatalf("resume: %+v", res.Report)
	}
	if st, _ := r.st.Stats(ctx); st.Observations != 6 {
		t.Fatalf("exactly the eligible observations, once: %+v", st)
	}
}

func TestTakenTopicKeyNeverOverwritesTeamMemory(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	if _, err := r.st.Save(ctx, memory.Entry{Type: memory.TypeDecision, Title: "Team DB decision", Content: "ours", Project: "shop", Actor: "gonza", TopicKey: "arch/db"}); err != nil {
		t.Fatal(err)
	}
	res, err := r.m.Migrate(ctx, r.req())
	if err != nil || res.Report.TopicKeysDropped != 1 || !res.Report.Verified {
		t.Fatalf("%+v %v", res.Report, err)
	}
	rs, _ := r.st.Search(ctx, memory.Query{Text: "Team DB", Project: "shop"})
	if len(rs) != 1 || rs[0].Content != "ours" || rs[0].Revision != 1 {
		t.Fatalf("the team entry must be untouched: %+v", rs)
	}
}

func TestFiltersRenameAndPersonalOptIn(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	req := r.req()
	req.Project, req.TargetProject, req.IncludePersonal = "shop", "tienda", true
	res, err := r.m.Migrate(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	rep := res.Report
	if rep.Imported != 5 || rep.Skipped[SkipOther] != 2 || rep.ByProject["tienda"] != 5 || rep.Skipped[SkipPersonal] != 0 {
		t.Fatalf("%+v", rep)
	}
	if _, err := r.st.BySourceRef(ctx, "engram:sync-5"); err != nil {
		t.Fatal("personal scope was opted in")
	}
	if rs, _ := r.st.Search(ctx, memory.Query{Text: "postgresql", Project: "tienda"}); len(rs) != 1 {
		t.Fatalf("renamed project: %+v", rs)
	}
	bad := r.req()
	bad.TargetProject = "x"
	if _, err := r.m.Migrate(ctx, bad); err == nil {
		t.Fatal("--target-project without --project must be refused")
	}
}

func TestLegacyExportWithoutSyncIDsIsStillIdempotent(t *testing.T) {
	r := newRig(t)
	doc := map[string]any{"version": "0.1.0", "sessions": []any{}, "observations": []any{
		map[string]any{"id": 1, "session_id": "s", "type": "decision", "title": "Old", "content": "from 0.1.0", "project": "shop",
			"scope": "project", "created_at": "2025-01-01 00:00:00", "updated_at": "2025-01-01 00:00:00"}}}
	writeJSON(t, r.export, doc)
	ctx := context.Background()
	if res, err := r.m.Migrate(ctx, r.req()); err != nil || res.Report.Imported != 1 || len(res.Report.Warnings) == 0 {
		t.Fatalf("%+v %v", res.Report, err)
	}
	if res, _ := r.m.Migrate(ctx, r.req()); res.Report.Imported != 0 || res.Report.AlreadyPresent != 1 {
		t.Fatalf("re-run: %+v", res.Report)
	}
}

func TestBadTimestampsWarnButDoNotFail(t *testing.T) {
	r := newRig(t)
	doc := fixture()
	doc["observations"] = []any{map[string]any{"id": 1, "sync_id": "x", "session_id": "s1", "type": "decision", "title": "T", "content": "C",
		"project": "shop", "scope": "project", "created_at": "garbage", "updated_at": "also garbage"}}
	writeJSON(t, r.export, doc)
	res, err := r.m.Migrate(context.Background(), r.req())
	if err != nil || res.Report.Imported != 1 || len(res.Report.Warnings) == 0 {
		t.Fatalf("%+v %v", res.Report, err)
	}
}

func TestRegistryPair(t *testing.T) {
	reg := migrate.NewRegistry()
	if err := reg.Register(New(nil)); err != nil {
		t.Fatal(err)
	}
	m, err := reg.Lookup("engram", "memory-mcp")
	if err != nil || m.From() != "engram" || m.To() != "memory-mcp" {
		t.Fatalf("%v %v", m, err)
	}
	if _, err := reg.Lookup("engram", "something-else"); !errors.Is(err, migrate.ErrNoTestedMigrator) {
		t.Fatal("only the proven pair exists")
	}
	if _, err := New(nil).Migrate(context.Background(), migrate.Request{}); !errors.Is(err, migrate.ErrNoBackup) {
		t.Fatal("no backup, no run")
	}
}
