-- 0001_init.sql  (forward-only; the database is a rebuildable cache of GitHub)
CREATE TABLE meta (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
) STRICT;

-- One row per tracked repository (the catalogue, persisted for warm start).
CREATE TABLE repo (
  id         INTEGER PRIMARY KEY,
  org        TEXT    NOT NULL,
  name       TEXT    NOT NULL,
  github_id  INTEGER,                         -- stable across renames
  seen_at    INTEGER NOT NULL,                -- unix seconds, last time reconcile saw it
  meta       TEXT    NOT NULL,                -- JSON: description, default_branch, topics, visibility...
  UNIQUE (org, name)
) STRICT;

-- One row per (repo, kind): readme, release, status.build, manifests.tree ...
-- Stale-while-error: failures update the error columns only; payload and fetched_at keep the last good value.
CREATE TABLE item (
  id          INTEGER PRIMARY KEY,
  repo_id     INTEGER NOT NULL REFERENCES repo(id) ON DELETE CASCADE,
  kind        TEXT    NOT NULL,
  status      TEXT    NOT NULL CHECK (status IN ('ok','not_found','unsupported','too_large','no_content','error','never')),
  fetched_at  INTEGER,                        -- last successful fetch OR successful 304 validation
  checked_at  INTEGER NOT NULL,               -- last attempt (success or failure)
  next_due_at INTEGER,                        -- planner state restored at warm start (jittered at write time); no index: the planner scans memory
  attempts    INTEGER NOT NULL DEFAULT 0,     -- consecutive failures
  etag        TEXT,
  content_sha TEXT,                           -- git blob SHA / content hash; render cache key
  error       TEXT,
  payload     BLOB,                           -- LAST column: keeps metadata scans off overflow pages
  UNIQUE (repo_id, kind)
) STRICT;

CREATE TABLE commit_log (
  id           INTEGER PRIMARY KEY,
  repo_id      INTEGER NOT NULL REFERENCES repo(id) ON DELETE CASCADE,
  sha          TEXT    NOT NULL,
  committed_at INTEGER NOT NULL,
  payload      BLOB    NOT NULL,
  UNIQUE (repo_id, sha)
) STRICT;
CREATE INDEX commit_log_recent ON commit_log (repo_id, committed_at DESC, id DESC);

-- Scheduler memory so a restart resumes cadences instead of re-running everything at t=0.
CREATE TABLE task_state (
  name         TEXT PRIMARY KEY,
  last_attempt INTEGER,
  last_success INTEGER,
  last_error   TEXT
) STRICT;

-- Permanently failed keys (dead letters): visible in /status, re-probed on a long TTL.
CREATE TABLE parked (
  key        TEXT PRIMARY KEY,
  since      INTEGER NOT NULL,
  reason     TEXT    NOT NULL,
  retry_at   INTEGER NOT NULL
) STRICT;
