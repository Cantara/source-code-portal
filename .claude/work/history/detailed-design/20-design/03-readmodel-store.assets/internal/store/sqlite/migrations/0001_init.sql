-- 0001_init.sql: forward-only. The database is a rebuildable cache of GitHub: "down" is deleting the file.
-- Time columns are UTC unix seconds written by the application (never CURRENT_TIMESTAMP).
PRAGMA application_id = 1396790321; -- 0x53435031 "SCP1": a file with another non-zero id is refused, never quarantined

CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL) STRICT;          -- created_at, last_open_at, build_id, probe

CREATE TABLE repo (                                                            -- the persisted catalogue
  id         INTEGER PRIMARY KEY,
  source     TEXT    NOT NULL,
  owner      TEXT    NOT NULL,
  name       TEXT    NOT NULL,
  github_id  INTEGER,                                                          -- stable across rename/transfer; NULL when unknown
  slug       TEXT    NOT NULL,
  seen_at    INTEGER NOT NULL,                                                 -- last reconcile that saw it
  meta       TEXT    NOT NULL CHECK (json_valid(meta)),                        -- JSON: description, default_branch, topics, visibility, aliases
  UNIQUE (source, owner, name),
  UNIQUE (slug)
) STRICT;
CREATE UNIQUE INDEX repo_github_id ON repo (source, github_id) WHERE github_id IS NOT NULL;

CREATE TABLE item (                                                            -- one row per (repo, kind)
  id               INTEGER PRIMARY KEY,
  repo_id          INTEGER NOT NULL REFERENCES repo(id) ON DELETE CASCADE,
  kind             TEXT    NOT NULL,
  status           TEXT    NOT NULL CHECK (status IN ('ok','not_found','unsupported','too_large','no_content','error','never')),
  fetched_at       INTEGER,                                                    -- last successful determination OR successful 304 validation
  checked_at       INTEGER NOT NULL,                                           -- last attempt
  next_due_at      INTEGER,                                                    -- planner-owned (SavePlans)
  attempts         INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),           -- store-owned: consecutive failures
  unchanged_streak INTEGER NOT NULL DEFAULT 0 CHECK (unchanged_streak >= 0),   -- planner-owned (SavePlans)
  etag             TEXT,
  content_sha      TEXT,
  error            TEXT,                                                       -- stable code of the last failed attempt
  payload          BLOB,                                                       -- LAST column: metadata scans stay off overflow pages
  UNIQUE (repo_id, kind),
  CHECK (status NOT IN ('ok','not_found','unsupported','too_large','no_content') OR fetched_at IS NOT NULL)
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

CREATE TABLE task_state (name TEXT PRIMARY KEY, last_attempt INTEGER, last_success INTEGER, last_error TEXT) STRICT;
CREATE TABLE parked     (key  TEXT PRIMARY KEY, since INTEGER NOT NULL, reason TEXT NOT NULL, retry_at INTEGER NOT NULL) STRICT;

CREATE TABLE cache (                                                           -- generic bounded cache, namespaces owned by features
  ns      TEXT    NOT NULL,
  key     TEXT    NOT NULL,
  data    BLOB    NOT NULL,
  size    INTEGER NOT NULL,
  use_seq INTEGER NOT NULL,                                                    -- logical recency tick, not a time (deterministic LRU)
  PRIMARY KEY (ns, key)
) STRICT;
CREATE INDEX cache_lru ON cache (ns, use_seq);
