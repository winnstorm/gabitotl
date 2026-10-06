-- Project-scoped API tokens. Every token that exists when this migration runs
-- keeps working on every project (all_projects = true): scoping is opt-in and
-- only applies to tokens created afterwards with an explicit project list.
ALTER TABLE api_tokens ADD COLUMN all_projects boolean NOT NULL DEFAULT true;
ALTER TABLE api_tokens ADD COLUMN projects text[] NOT NULL DEFAULT '{}';
-- From now on a token that does not say otherwise is NOT an all-project token.
ALTER TABLE api_tokens ALTER COLUMN all_projects SET DEFAULT false;
