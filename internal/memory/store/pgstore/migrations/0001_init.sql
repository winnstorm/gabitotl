-- GabitoTL memory server schema, version 1.

CREATE TABLE observations (
    id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    project     text        NOT NULL CHECK (btrim(project) <> ''),
    scope       text        NOT NULL DEFAULT 'project',
    type        text        NOT NULL,
    title       text        NOT NULL,
    content     text        NOT NULL,
    topic_key   text        NOT NULL DEFAULT '',
    -- actor is the author of the current revision; created_by never changes.
    actor       text        NOT NULL CHECK (btrim(actor) <> ''),
    created_by  text        NOT NULL CHECK (btrim(created_by) <> ''),
    task        text        NOT NULL DEFAULT '',
    branch      text        NOT NULL DEFAULT '',
    "commit"    text        NOT NULL DEFAULT '',
    files       text[]      NOT NULL DEFAULT '{}',
    validation  text        NOT NULL DEFAULT '',
    next_step   text        NOT NULL DEFAULT '',
    ojo         text        NOT NULL DEFAULT '',
    source      text        NOT NULL DEFAULT 'gabitotl',
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    deleted_at  timestamptz,
    deleted_by  text,
    revision    integer     NOT NULL DEFAULT 1,
    -- The 'simple' configuration neither stems nor drops stopwords, so Spanish
    -- and English text rank the same way. Title weighs most, then topic/task.
    search      tsvector GENERATED ALWAYS AS (
        setweight(to_tsvector('simple'::regconfig, title), 'A') ||
        setweight(to_tsvector('simple'::regconfig, topic_key || ' ' || task), 'B') ||
        setweight(to_tsvector('simple'::regconfig, content), 'C')
    ) STORED
);

-- One live entry per (project, topic_key): the target of the concurrent upsert.
CREATE UNIQUE INDEX observations_topic_live
    ON observations (project, topic_key)
    WHERE deleted_at IS NULL AND topic_key <> '';
CREATE INDEX observations_project_actor ON observations (project, actor) WHERE deleted_at IS NULL;
CREATE INDEX observations_project_task  ON observations (project, task)  WHERE deleted_at IS NULL;
CREATE INDEX observations_project_recent ON observations (project, updated_at DESC) WHERE deleted_at IS NULL;
CREATE INDEX observations_search ON observations USING gin (search);

CREATE TABLE session_summaries (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    project      text        NOT NULL CHECK (btrim(project) <> ''),
    actor        text        NOT NULL CHECK (btrim(actor) <> ''),
    task         text        NOT NULL DEFAULT '',
    branch       text        NOT NULL DEFAULT '',
    goal         text        NOT NULL DEFAULT '',
    discoveries  text        NOT NULL DEFAULT '',
    accomplished text        NOT NULL DEFAULT '',
    next_steps   text        NOT NULL DEFAULT '',
    files        text[]      NOT NULL DEFAULT '{}',
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX session_summaries_project ON session_summaries (project, created_at DESC);

-- Only a hash of the secret is stored; the id is public and selects the row.
CREATE TABLE api_tokens (
    id         text        PRIMARY KEY,
    user_id    text        NOT NULL CHECK (btrim(user_id) <> ''),
    hash       bytea       NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    revoked_at timestamptz
);
CREATE INDEX api_tokens_user ON api_tokens (user_id);
