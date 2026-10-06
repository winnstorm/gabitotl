package pgstore

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/winnstorm/gabitotl/internal/memory/store"
	"github.com/winnstorm/gabitotl/internal/memory/store/storetest"
	"github.com/winnstorm/gabitotl/internal/pgmanaged/pgtest"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Main(m)) }

func open(t *testing.T) *Store {
	t.Helper()
	dsn := pgtest.Database(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	s, err := Open(ctx, dsn, Options{MaxConns: 20})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func TestConformance(t *testing.T) {
	storetest.Run(t, func(t *testing.T) store.Store { return open(t) })
}

func TestMigrationsAreIdempotentAndConcurrencySafe(t *testing.T) {
	dsn := pgtest.Database(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	errs := make([]error, 6)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, err := Open(ctx, dsn, Options{})
			if err == nil {
				s.Close()
			}
			errs[i] = err
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("migrator %d: %v", i, err)
		}
	}
	s, err := Open(ctx, dsn, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	st, err := s.Stats(ctx)
	if err != nil || st.SchemaVersion != LatestVersion() {
		t.Fatalf("schema version: %+v %v", st, err)
	}
	var rows int
	if err := s.Pool().QueryRow(ctx, `SELECT count(*) FROM schema_version`).Scan(&rows); err != nil || rows != LatestVersion() {
		t.Fatalf("each migration is recorded exactly once, got %d rows (%v)", rows, err)
	}
	rep := s.Doctor(ctx)
	if !rep.OK() {
		t.Fatalf("doctor: %+v", rep)
	}
}

func TestDoctorFlagsANewerSchema(t *testing.T) {
	dsn := pgtest.Database(t)
	ctx := context.Background()
	s, err := Open(ctx, dsn, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.Pool().Exec(ctx, `INSERT INTO schema_version (version, name) VALUES ($1, 'future')`, LatestVersion()+1); err != nil {
		t.Fatal(err)
	}
	if s.Doctor(ctx).OK() {
		t.Fatal("a schema newer than the build must be an error")
	}
	if _, err := Open(ctx, dsn, Options{}); err == nil {
		t.Fatal("opening must refuse a newer schema")
	}
}
