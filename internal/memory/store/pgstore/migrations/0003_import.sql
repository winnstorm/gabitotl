-- Import path (history from another system, for example an Engram export).
-- source_ref is the idempotency key of an imported row; meta keeps origin
-- details that have no column of their own.
ALTER TABLE observations ADD COLUMN source_ref text NOT NULL DEFAULT '';
ALTER TABLE observations ADD COLUMN meta jsonb NOT NULL DEFAULT '{}'::jsonb;
-- Deleted rows keep their source_ref, so a re-run never resurrects them.
CREATE UNIQUE INDEX observations_source_ref ON observations (source_ref) WHERE source_ref <> '';

ALTER TABLE session_summaries ADD COLUMN source_ref text NOT NULL DEFAULT '';
CREATE UNIQUE INDEX session_summaries_source_ref ON session_summaries (source_ref) WHERE source_ref <> '';
