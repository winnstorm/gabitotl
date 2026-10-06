package pgstore

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// migrationLockKey is the session-level advisory lock that serializes
// migrators (two servers starting together). It is derived from the product
// name, not from anything a user database is likely to use.
const migrationLockKey int64 = 0x6761626974_6f_6c // "gabitol"

var migrationName = regexp.MustCompile(`^(\d{4})_([a-z0-9_]+)\.sql$`)

type migration struct {
	version int
	name    string
	sql     string
}

// migrations returns the embedded migrations in version order.
func migrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return nil, err
	}
	var out []migration
	for _, e := range entries {
		m := migrationName.FindStringSubmatch(e.Name())
		if m == nil {
			return nil, fmt.Errorf("pgstore: bad migration file name %q", e.Name())
		}
		body, err := fs.ReadFile(migrationFS, "migrations/"+e.Name())
		if err != nil {
			return nil, err
		}
		v, _ := strconv.Atoi(m[1])
		out = append(out, migration{version: v, name: m[2], sql: string(body)})
	}
	slices.SortFunc(out, func(a, b migration) int { return a.version - b.version })
	for i, m := range out {
		if m.version != i+1 {
			return nil, fmt.Errorf("pgstore: migrations must be numbered 0001.. without gaps (found %04d at position %d)", m.version, i+1)
		}
	}
	return out, nil
}

// LatestVersion is the schema version this build expects.
func LatestVersion() int {
	ms, err := migrations()
	if err != nil {
		return 0
	}
	return len(ms)
}

// Migrate applies every pending migration. It is idempotent and safe to call
// from several processes at once: an advisory lock serializes them, and each
// migration commits together with its schema_version row.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	ms, err := migrations()
	if err != nil {
		return err
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("pgstore: connect for migration: %w", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, migrationLockKey); err != nil {
		return fmt.Errorf("pgstore: migration lock: %w", err)
	}
	defer func() {
		// Best effort on a fresh context: the lock also dies with the session.
		_, _ = conn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock($1)`, migrationLockKey)
	}()
	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_version (
		version int PRIMARY KEY, name text NOT NULL, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return fmt.Errorf("pgstore: schema_version: %w", err)
	}
	var current int
	if err := conn.QueryRow(ctx, `SELECT COALESCE(max(version), 0) FROM schema_version`).Scan(&current); err != nil {
		return fmt.Errorf("pgstore: read schema version: %w", err)
	}
	if current > len(ms) {
		return fmt.Errorf("pgstore: database schema is version %d but this build only knows %d; upgrade GabitoTL", current, len(ms))
	}
	for _, m := range ms[current:] {
		// One simple-protocol message: PostgreSQL runs it as a single implicit
		// transaction, so the DDL and its version row land together or not at all.
		script := m.sql + fmt.Sprintf("\n;INSERT INTO schema_version (version, name) VALUES (%d, '%s');", m.version, m.name)
		if _, err := conn.Conn().PgConn().Exec(ctx, script).ReadAll(); err != nil {
			return fmt.Errorf("pgstore: migration %04d_%s: %w", m.version, m.name, err)
		}
	}
	return nil
}
