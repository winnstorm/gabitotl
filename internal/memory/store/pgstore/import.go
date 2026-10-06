package pgstore

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/winnstorm/gabitotl/internal/memory"
	"github.com/winnstorm/gabitotl/internal/memory/store"
)

var _ store.Importer = (*Store)(nil)

const importSQL = `
INSERT INTO observations (project, type, title, content, topic_key, actor, created_by, task, branch, "commit", files,
                          validation, next_step, ojo, source, created_at, updated_at, source_ref, meta)
VALUES ($1, $2, $3, $4,
        CASE WHEN $5 <> '' AND EXISTS (SELECT 1 FROM observations o WHERE o.project = $1 AND o.topic_key = $5 AND o.deleted_at IS NULL)
             THEN '' ELSE $5 END,
        $6, $6, $7, $8, $9, $10, $11, $12, $13, COALESCE(NULLIF($14, ''), 'gabitotl'),
        COALESCE($15::timestamptz, now()), COALESCE($16::timestamptz, $15::timestamptz, now()), $17, $18::jsonb)
ON CONFLICT (source_ref) WHERE source_ref <> '' DO NOTHING
RETURNING id::text, topic_key`

func nullTime(t interface{ IsZero() bool }, v any) any {
	if t.IsZero() {
		return nil
	}
	return v
}

// Import implements store.Importer. The insert is one statement guarded by the
// unique source_ref index, so concurrent or repeated imports cannot duplicate.
func (s *Store) Import(ctx context.Context, rec store.ImportRecord) (store.ImportResult, error) {
	if err := store.CheckImport(rec); err != nil {
		return store.ImportResult{}, err
	}
	meta := rec.Meta
	if meta == nil {
		meta = map[string]string{}
	}
	e := rec.Entry
	for attempt := 0; attempt < 2; attempt++ {
		topic := e.TopicKey
		if attempt == 1 { // the topic was taken between the check and the insert
			topic = ""
		}
		mb, _ := json.Marshal(meta)
		var id, gotTopic string
		err := s.pool.QueryRow(ctx, importSQL, e.Project, string(e.Type), e.Title, e.Content, topic, e.Actor, e.Task, e.Branch,
			e.Commit, files(e.Files), e.Validation, e.NextStep, e.Ojo, e.Source,
			nullTime(rec.CreatedAt, rec.CreatedAt), nullTime(rec.UpdatedAt, rec.UpdatedAt), rec.SourceRef, string(mb)).Scan(&id, &gotTopic)
		switch {
		case err == nil:
			res := store.ImportResult{ID: memory.ID(id), Created: true, TopicKeyDropped: e.TopicKey != "" && gotTopic == ""}
			if res.TopicKeyDropped {
				meta["dropped_topic_key"] = e.TopicKey
				mb, _ := json.Marshal(meta)
				if _, err := s.pool.Exec(ctx, `UPDATE observations SET meta = $2::jsonb WHERE id = $1::uuid`, id, string(mb)); err != nil {
					return res, wrap("import", err)
				}
			}
			return res, nil
		case errors.Is(err, pgx.ErrNoRows): // ON CONFLICT DO NOTHING: already imported
			var existing string
			if err := s.pool.QueryRow(ctx, `SELECT id::text FROM observations WHERE source_ref = $1`, rec.SourceRef).Scan(&existing); err != nil {
				return store.ImportResult{}, wrap("import", err)
			}
			return store.ImportResult{ID: memory.ID(existing)}, nil
		}
		var pe *pgconn.PgError
		if errors.As(err, &pe) && pe.Code == "23505" && pe.ConstraintName == "observations_topic_live" && attempt == 0 {
			continue
		}
		return store.ImportResult{}, wrap("import", err)
	}
	return store.ImportResult{}, wrap("import", errors.New("topic key kept conflicting"))
}

// ImportSummary implements store.Importer.
func (s *Store) ImportSummary(ctx context.Context, ref string, sum memory.Summary) (bool, error) {
	if err := store.CheckSummaryImport(ref, sum); err != nil {
		return false, err
	}
	tag, err := s.pool.Exec(ctx, `INSERT INTO session_summaries (project, actor, task, branch, goal, discoveries, accomplished, next_steps, files, created_at, source_ref)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, COALESCE($10::timestamptz, now()), $11)
		ON CONFLICT (source_ref) WHERE source_ref <> '' DO NOTHING`,
		sum.Project, sum.Actor, sum.Task, sum.Branch, sum.Goal, sum.Discoveries, sum.Accomplished, sum.NextSteps, files(sum.Files),
		nullTime(sum.Timestamp, sum.Timestamp), ref)
	if err != nil {
		return false, wrap("import summary", err)
	}
	return tag.RowsAffected() == 1, nil
}

// BySourceRef implements store.Importer (deleted rows included).
func (s *Store) BySourceRef(ctx context.Context, ref string) (store.Record, error) {
	if ref == "" {
		return store.Record{}, store.ErrNotFound
	}
	var meta []byte
	r, err := scan(s.pool.QueryRow(ctx, `SELECT `+cols+`, meta FROM observations WHERE source_ref = $1`, ref), &meta)
	if err != nil {
		return store.Record{}, wrap("by source ref", err)
	}
	r.SourceRef = ref
	if len(meta) > 0 {
		var m map[string]string
		if json.Unmarshal(meta, &m) == nil && len(m) > 0 {
			r.Meta = m
		}
	}
	return r, nil
}
