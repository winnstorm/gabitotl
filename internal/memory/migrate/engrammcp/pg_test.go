package engrammcp

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/winnstorm/gabitotl/internal/memory"
	"github.com/winnstorm/gabitotl/internal/memory/migrate"
	"github.com/winnstorm/gabitotl/internal/memory/store"
	"github.com/winnstorm/gabitotl/internal/memory/store/pgstore"
	"github.com/winnstorm/gabitotl/internal/pgmanaged/pgtest"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Main(m)) }

// TestMigrationAgainstPostgreSQL runs the import twice over the real store
// (opt-in: GABITOTL_TEST_DOCKER=1 or GABITOTL_TEST_PG_DSN).
func TestMigrationAgainstPostgreSQL(t *testing.T) {
	ctx := context.Background()
	pg, err := pgstore.Open(ctx, pgtest.Database(t), pgstore.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pg.Close)
	dir := t.TempDir()
	export := filepath.Join(dir, "engram-export.json")
	writeJSON(t, export, fixture())
	if _, err := pg.Save(ctx, memory.Entry{Type: memory.TypeDecision, Title: "Team", Content: "ours", Project: "shop", Actor: "gonza", TopicKey: "arch/db"}); err != nil {
		t.Fatal(err)
	}
	m := New(func(context.Context, string) (store.Importer, func(), error) { return pg, func() {}, nil })
	r1, err := m.Migrate(ctx, migrateReq(export, filepath.Join(dir, "r.json")))
	if err != nil || r1.Report.Imported != 6 || !r1.Report.Verified || r1.Report.TopicKeysDropped != 1 {
		t.Fatalf("first run: %+v %v", r1.Report, err)
	}
	r2, err := m.Migrate(ctx, migrateReq(export, filepath.Join(dir, "r.json")))
	if err != nil || r2.Report.Imported != 0 || r2.Report.AlreadyPresent != 6 || !r2.Report.Verified {
		t.Fatalf("second run: %+v %v", r2.Report, err)
	}
	st, _ := pg.Stats(ctx)
	if st.Observations != 7 || st.Summaries != 1 {
		t.Fatalf("stats: %+v", st)
	}
	rec, err := pg.BySourceRef(ctx, "engram:sync-2")
	if err != nil || rec.Actor != "gabi" || rec.Meta["engram_type"] != "bugfix" || rec.CreatedAt.Format("2006-01-02") != "2026-01-03" {
		t.Fatalf("%+v %v", rec, err)
	}
}

func migrateReq(export, report string) migrate.Request {
	return migrate.Request{ExportPath: export, BackupPath: export, ReportPath: report}
}
