// Package pgstore is the PostgreSQL store.Store (pgx v5 pool). Schema changes
// ship as embedded, ordered, idempotent migrations applied under an advisory
// lock. The connection string is only ever passed to the driver: it is never
// logged, returned in a report or put in an error by this package.
package pgstore

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/winnstorm/gabitotl/internal/memory"
	"github.com/winnstorm/gabitotl/internal/memory/store"
)

// Options configure Open.
type Options struct {
	// MaxConns bounds the pool (default 10).
	MaxConns int32
	// ConnectTimeout bounds the first connection (default 10s).
	ConnectTimeout time.Duration
	// NoMigrate skips applying pending migrations (read-only diagnostics).
	NoMigrate bool
}

// Store is the PostgreSQL implementation.
type Store struct {
	pool *pgxpool.Pool
}

var _ store.Store = (*Store)(nil)

// Open connects, verifies the connection and (unless NoMigrate) migrates.
func Open(ctx context.Context, dsn string, opt Options) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, errors.New("pgstore: the connection string is not valid")
	}
	if opt.MaxConns <= 0 {
		opt.MaxConns = 10
	}
	cfg.MaxConns = opt.MaxConns
	if opt.ConnectTimeout <= 0 {
		opt.ConnectTimeout = 10 * time.Second
	}
	cfg.ConnConfig.ConnectTimeout = opt.ConnectTimeout
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, errors.New("pgstore: cannot create the connection pool")
	}
	pctx, cancel := context.WithTimeout(ctx, opt.ConnectTimeout)
	defer cancel()
	if err := pool.Ping(pctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pgstore: cannot reach the database: %s", sqlState(err))
	}
	if !opt.NoMigrate {
		if err := Migrate(ctx, pool); err != nil {
			pool.Close()
			return nil, err
		}
	}
	return &Store{pool: pool}, nil
}

// Close releases the pool.
func (s *Store) Close() { s.pool.Close() }

// Pool exposes the pool for tests and maintenance tooling.
func (s *Store) Pool() *pgxpool.Pool { return s.pool }

// sqlState describes a driver error without echoing connection details.
func sqlState(err error) string {
	var ne interface{ SQLState() string }
	if errors.As(err, &ne) && ne.SQLState() != "" {
		return "SQLSTATE " + ne.SQLState()
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timed out"
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	return "connection failed"
}

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

const cols = `id::text, project, scope, type, title, content, topic_key, actor, created_by, task, branch, "commit",
	files, validation, next_step, ojo, source, created_at, updated_at, revision`

func scan(row pgx.Row, extra ...any) (store.Record, error) {
	var r store.Record
	var typ string
	dest := []any{&r.ID, &r.Project, &r.Scope, &typ, &r.Title, &r.Content, &r.TopicKey, &r.Actor, &r.CreatedBy, &r.Task,
		&r.Branch, &r.Commit, &r.Files, &r.Validation, &r.NextStep, &r.Ojo, &r.Source, &r.CreatedAt, &r.UpdatedAt, &r.Revision}
	if err := row.Scan(append(dest, extra...)...); err != nil {
		return store.Record{}, err
	}
	r.Type = memory.EntryType(typ)
	r.Timestamp = r.UpdatedAt
	if len(r.Files) == 0 {
		r.Files = nil
	}
	return r, nil
}

func wrap(op string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return store.ErrNotFound
	}
	return fmt.Errorf("pgstore %s: %w", op, err)
}

func files(f []string) []string {
	if f == nil {
		return []string{}
	}
	return f
}

const saveSQL = `
INSERT INTO observations (project, type, title, content, topic_key, actor, created_by, task, branch, "commit", files,
                          validation, next_step, ojo, source)
VALUES ($1, $2, $3, $4, $5, $6, $6, $7, $8, $9, $10, $11, $12, $13, COALESCE(NULLIF($14, ''), 'gabitotl'))
ON CONFLICT (project, topic_key) WHERE deleted_at IS NULL AND topic_key <> ''
DO UPDATE SET type = EXCLUDED.type, title = EXCLUDED.title, content = EXCLUDED.content, actor = EXCLUDED.actor,
              task = EXCLUDED.task, branch = EXCLUDED.branch, "commit" = EXCLUDED."commit", files = EXCLUDED.files,
              validation = EXCLUDED.validation, next_step = EXCLUDED.next_step, ojo = EXCLUDED.ojo,
              source = COALESCE(NULLIF($14, ''), observations.source),
              updated_at = now(), revision = observations.revision + 1
RETURNING id::text, revision, (xmax = 0)`

// Save implements store.Store. The upsert is a single INSERT ... ON CONFLICT
// against the partial unique index, so concurrent writers of one topic_key
// cannot create duplicates.
func (s *Store) Save(ctx context.Context, e memory.Entry) (store.SaveResult, error) {
	if err := store.CheckEntry(e); err != nil {
		return store.SaveResult{}, err
	}
	var res store.SaveResult
	var id string
	err := s.pool.QueryRow(ctx, saveSQL, e.Project, string(e.Type), e.Title, e.Content, e.TopicKey, e.Actor, e.Task,
		e.Branch, e.Commit, files(e.Files), e.Validation, e.NextStep, e.Ojo, e.Source).Scan(&id, &res.Revision, &res.Created)
	if err != nil {
		return store.SaveResult{}, wrap("save", err)
	}
	res.ID = memory.ID(id)
	return res, nil
}

// Get implements store.Store.
func (s *Store) Get(ctx context.Context, project string, id memory.ID) (store.Record, error) {
	if !uuidRe.MatchString(string(id)) {
		return store.Record{}, store.ErrNotFound
	}
	r, err := scan(s.pool.QueryRow(ctx, `SELECT `+cols+` FROM observations
		WHERE id = $1::uuid AND deleted_at IS NULL AND ($2 = '' OR project = $2)`, string(id), project))
	if err != nil {
		return store.Record{}, wrap("get", err)
	}
	return r, nil
}

// Update implements store.Store. The row is locked for the duration of the
// transaction, so the revision check and the write are atomic.
func (s *Store) Update(ctx context.Context, project string, id memory.ID, p memory.EntryPatch, actor string, expect int) (store.Record, error) {
	if strings.TrimSpace(actor) == "" {
		return store.Record{}, fmt.Errorf("%w: %v", store.ErrInvalid, memory.ErrActorRequired)
	}
	if !uuidRe.MatchString(string(id)) {
		return store.Record{}, store.ErrNotFound
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return store.Record{}, wrap("update", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	cur, err := scan(tx.QueryRow(ctx, `SELECT `+cols+` FROM observations
		WHERE id = $1::uuid AND deleted_at IS NULL AND ($2 = '' OR project = $2) FOR UPDATE`, string(id), project))
	if err != nil {
		return store.Record{}, wrap("update", err)
	}
	if expect > 0 && cur.Revision != expect {
		return store.Record{}, store.ErrConflict
	}
	next, err := store.ApplyPatch(cur.Entry, p)
	if err != nil {
		return store.Record{}, err
	}
	upd, err := scan(tx.QueryRow(ctx, `UPDATE observations SET type = $2, title = $3, content = $4, actor = $5, task = $6,
		branch = $7, "commit" = $8, files = $9, validation = $10, next_step = $11, ojo = $12,
		updated_at = now(), revision = revision + 1 WHERE id = $1::uuid RETURNING `+cols,
		string(id), string(next.Type), next.Title, next.Content, actor, next.Task, next.Branch, next.Commit,
		files(next.Files), next.Validation, next.NextStep, next.Ojo))
	if err != nil {
		return store.Record{}, wrap("update", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return store.Record{}, wrap("update", err)
	}
	return upd, nil
}

// Delete implements store.Store (soft delete).
func (s *Store) Delete(ctx context.Context, project string, id memory.ID, actor string) error {
	if strings.TrimSpace(actor) == "" {
		return fmt.Errorf("%w: %v", store.ErrInvalid, memory.ErrActorRequired)
	}
	if !uuidRe.MatchString(string(id)) {
		return store.ErrNotFound
	}
	tag, err := s.pool.Exec(ctx, `UPDATE observations SET deleted_at = now(), deleted_by = $3, updated_at = now()
		WHERE id = $1::uuid AND deleted_at IS NULL AND ($2 = '' OR project = $2)`, string(id), project, actor)
	if err != nil {
		return wrap("delete", err)
	}
	if tag.RowsAffected() == 0 {
		return store.ErrNotFound
	}
	return nil
}

// tsQuery builds a prefix tsquery from sanitized terms. Terms are letters and
// digits only (store.Terms), so quoting them is always safe.
func tsQuery(terms []string, any bool) string {
	op := " & "
	if any {
		op = " | "
	}
	parts := make([]string, len(terms))
	for i, t := range terms {
		parts[i] = "'" + t + "':*"
	}
	return strings.Join(parts, op)
}

// Search implements store.Store. Ranking is ts_rank_cd over the weighted
// generated column (title > topic/task > body); ties go to the newest entry.
func (s *Store) Search(ctx context.Context, q memory.Query) ([]store.Record, error) {
	limit := store.ClampLimit(q.Limit)
	terms := store.Terms(q.Text)
	var rows pgx.Rows
	var err error
	if len(terms) == 0 {
		rows, err = s.pool.Query(ctx, `SELECT `+cols+`, 0::float8 FROM observations
			WHERE deleted_at IS NULL AND ($1 = '' OR project = $1) AND ($2 = '' OR type = $2)
			ORDER BY updated_at DESC, id LIMIT $3`, q.Project, string(q.Type), limit)
	} else {
		rows, err = s.pool.Query(ctx, `SELECT `+cols+`, ts_rank_cd(search, query)::float8 AS score
			FROM observations, to_tsquery('simple', $4) AS query
			WHERE deleted_at IS NULL AND ($1 = '' OR project = $1) AND ($2 = '' OR type = $2) AND search @@ query
			ORDER BY score DESC, updated_at DESC, id LIMIT $3`, q.Project, string(q.Type), limit, tsQuery(terms, q.MatchAny))
	}
	if err != nil {
		return nil, wrap("search", err)
	}
	return collect(rows, "search")
}

func collect(rows pgx.Rows, op string) ([]store.Record, error) {
	defer rows.Close()
	var out []store.Record
	for rows.Next() {
		var score float64
		r, err := scan(rows, &score)
		if err != nil {
			return nil, wrap(op, err)
		}
		r.Score = score
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, wrap(op, err)
	}
	return out, nil
}

// Active implements store.Store.
func (s *Store) Active(ctx context.Context, q store.ActiveQuery) ([]store.Record, error) {
	var since any
	if !q.Since.IsZero() {
		since = q.Since
	}
	rows, err := s.pool.Query(ctx, `SELECT `+cols+`, 0::float8 FROM observations
		WHERE deleted_at IS NULL AND ($1 = '' OR project = $1) AND ($2 = '' OR actor = $2) AND ($3 = '' OR task = $3)
		  AND ($4::timestamptz IS NULL OR updated_at >= $4)
		ORDER BY updated_at DESC, id LIMIT $5`, q.Project, q.Actor, q.Task, since, store.ClampLimit(q.Limit))
	if err != nil {
		return nil, wrap("active", err)
	}
	return collect(rows, "active")
}

// SaveSummary implements store.Store.
func (s *Store) SaveSummary(ctx context.Context, sum memory.Summary) error {
	if err := store.CheckSummary(sum); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO session_summaries (project, actor, task, branch, goal, discoveries, accomplished, next_steps, files)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`, sum.Project, sum.Actor, sum.Task, sum.Branch, sum.Goal, sum.Discoveries,
		sum.Accomplished, sum.NextSteps, files(sum.Files))
	if err != nil {
		return wrap("summary", err)
	}
	return nil
}

// Summaries implements store.Store.
func (s *Store) Summaries(ctx context.Context, project string, limit int) ([]memory.Summary, error) {
	rows, err := s.pool.Query(ctx, `SELECT project, actor, task, branch, goal, discoveries, accomplished, next_steps, files, created_at
		FROM session_summaries WHERE ($1 = '' OR project = $1) ORDER BY created_at DESC, id LIMIT $2`, project, store.ClampLimit(limit))
	if err != nil {
		return nil, wrap("summaries", err)
	}
	defer rows.Close()
	var out []memory.Summary
	for rows.Next() {
		var m memory.Summary
		if err := rows.Scan(&m.Project, &m.Actor, &m.Task, &m.Branch, &m.Goal, &m.Discoveries, &m.Accomplished, &m.NextSteps, &m.Files, &m.Timestamp); err != nil {
			return nil, wrap("summaries", err)
		}
		if len(m.Files) == 0 {
			m.Files = nil
		}
		out = append(out, m)
	}
	return out, wrap("summaries", rows.Err())
}

// Stats implements store.Store.
func (s *Store) Stats(ctx context.Context) (store.Stats, error) {
	var st store.Stats
	err := s.pool.QueryRow(ctx, `SELECT
		COALESCE((SELECT max(version) FROM schema_version), 0),
		(SELECT count(*) FROM observations WHERE deleted_at IS NULL),
		(SELECT count(*) FROM observations WHERE deleted_at IS NOT NULL),
		(SELECT count(*) FROM session_summaries),
		(SELECT count(DISTINCT project) FROM observations WHERE deleted_at IS NULL),
		(SELECT count(DISTINCT actor) FROM observations WHERE deleted_at IS NULL),
		(SELECT count(*) FROM api_tokens WHERE revoked_at IS NULL)`).
		Scan(&st.SchemaVersion, &st.Observations, &st.Deleted, &st.Summaries, &st.Projects, &st.Actors, &st.Tokens)
	if err != nil {
		return store.Stats{}, wrap("stats", err)
	}
	return st, nil
}

// Doctor implements store.Store. Messages never contain the connection string.
func (s *Store) Doctor(ctx context.Context) memory.Report {
	rep := memory.Report{Provider: "postgresql"}
	add := func(name, status, msg string) {
		rep.Checks = append(rep.Checks, memory.Check{Name: name, Status: status, Message: msg})
	}
	var version string
	if err := s.pool.QueryRow(ctx, `SHOW server_version`).Scan(&version); err != nil {
		add("database", memory.StatusError, "cannot query the database: "+sqlState(err))
		return rep
	}
	add("database", memory.StatusOK, "PostgreSQL "+version+" answers")
	st, err := s.Stats(ctx)
	switch {
	case err != nil:
		add("schema", memory.StatusError, "schema not readable (run the server once to migrate)")
		return rep
	case st.SchemaVersion < LatestVersion():
		add("schema", memory.StatusWarn, fmt.Sprintf("schema version %d, this build expects %d", st.SchemaVersion, LatestVersion()))
	case st.SchemaVersion > LatestVersion():
		add("schema", memory.StatusError, fmt.Sprintf("schema version %d is newer than this build (%d)", st.SchemaVersion, LatestVersion()))
	default:
		add("schema", memory.StatusOK, fmt.Sprintf("schema version %d", st.SchemaVersion))
	}
	add("data", memory.StatusOK, fmt.Sprintf("%d entries, %d projects, %d actors, %d summaries, %d active tokens",
		st.Observations, st.Projects, st.Actors, st.Summaries, st.Tokens))
	return rep
}

// CreateToken implements store.TokenStore.
func (s *Store) CreateToken(ctx context.Context, t store.TokenRecord) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO api_tokens (id, user_id, hash) VALUES ($1, $2, $3)`, t.ID, t.User, t.Hash)
	if err != nil {
		return fmt.Errorf("%w: %s", store.ErrInvalid, sqlState(err))
	}
	return nil
}

func scanToken(row pgx.Row) (store.TokenRecord, error) {
	var t store.TokenRecord
	var revoked *time.Time
	if err := row.Scan(&t.ID, &t.User, &t.Hash, &t.CreatedAt, &revoked); err != nil {
		return store.TokenRecord{}, err
	}
	if revoked != nil {
		t.RevokedAt = *revoked
	}
	return t, nil
}

// TokenByID implements store.TokenStore.
func (s *Store) TokenByID(ctx context.Context, id string) (store.TokenRecord, error) {
	t, err := scanToken(s.pool.QueryRow(ctx, `SELECT id, user_id, hash, created_at, revoked_at FROM api_tokens WHERE id = $1`, id))
	if err != nil {
		return store.TokenRecord{}, wrap("token", err)
	}
	return t, nil
}

// ListTokens implements store.TokenStore.
func (s *Store) ListTokens(ctx context.Context, user string) ([]store.TokenRecord, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, user_id, ''::bytea, created_at, revoked_at FROM api_tokens
		WHERE ($1 = '' OR user_id = $1) ORDER BY created_at, id`, user)
	if err != nil {
		return nil, wrap("tokens", err)
	}
	defer rows.Close()
	var out []store.TokenRecord
	for rows.Next() {
		t, err := scanToken(rows)
		if err != nil {
			return nil, wrap("tokens", err)
		}
		t.Hash = nil
		out = append(out, t)
	}
	return out, wrap("tokens", rows.Err())
}

// RevokeToken implements store.TokenStore.
func (s *Store) RevokeToken(ctx context.Context, user, id string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE api_tokens SET revoked_at = now() WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL`, id, user)
	if err != nil {
		return wrap("revoke", err)
	}
	if tag.RowsAffected() == 0 {
		return store.ErrNotFound
	}
	return nil
}
