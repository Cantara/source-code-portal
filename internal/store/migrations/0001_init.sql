-- The store is a rebuildable cache of the last good state. Times are UTC unix seconds.
CREATE TABLE meta (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
) STRICT;

-- Catalogued repositories: identity and address. Items of a repository go with it.
CREATE TABLE repo (
  key       TEXT PRIMARY KEY,          -- lower-case owner/name
  source    TEXT NOT NULL,
  github_id INTEGER NOT NULL,
  UNIQUE (source, github_id)
) STRICT;

-- One row per sync job key: its last outcome, schedule and payload.
CREATE TABLE item (
  key         TEXT PRIMARY KEY,
  kind        TEXT NOT NULL,
  repo_key    TEXT REFERENCES repo (key) ON DELETE CASCADE, -- NULL for source jobs
  status      TEXT NOT NULL CHECK (status IN ('never', 'ok', 'not_found', 'too_large', 'no_content', 'error')),
  fetched_at  INTEGER,
  checked_at  INTEGER,
  next_due_at INTEGER,
  attempts    INTEGER NOT NULL DEFAULT 0,
  content_sha TEXT,
  error       TEXT,
  payload     BLOB
) STRICT;
