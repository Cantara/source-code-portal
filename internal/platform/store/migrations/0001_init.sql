-- The store, one baseline (SC1, P-106). Times are UTC unix seconds. The marker below says this file was made by this schema's
-- generation: a store that holds data and does not carry it is from an earlier one and is moved aside (store.go, checkGeneration).
PRAGMA application_id = 0x53435031; -- "SCP1"

CREATE TABLE meta (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
) STRICT;

-- Catalogued repositories: identity, address and visibility. Items of a repository go with it.
CREATE TABLE repo (
  key        TEXT PRIMARY KEY,          -- lower-case owner/name
  source     TEXT NOT NULL,
  github_id  INTEGER NOT NULL,
  visibility TEXT NOT NULL DEFAULT 'unknown' CHECK (visibility IN ('unknown', 'public', 'private', 'internal')),
  UNIQUE (source, github_id)
) STRICT;

-- One row per sync job key: its last outcome, schedule and payload.
CREATE TABLE item (
  key         TEXT PRIMARY KEY,
  kind        TEXT NOT NULL,
  repo_key    TEXT REFERENCES repo (key) ON DELETE CASCADE, -- NULL for source jobs
  status      TEXT NOT NULL CHECK (status IN ('never', 'ok', 'not_found', 'too_large', 'no_content', 'unsupported', 'error')),
  fetched_at  INTEGER,
  checked_at  INTEGER,
  next_due_at INTEGER,
  attempts    INTEGER NOT NULL DEFAULT 0,
  content_sha TEXT,
  error       TEXT,
  payload     BLOB
) STRICT;

-- Sealed credentials (design/personal.md): a token the portal reads GitHub with, kept encrypted (AES-256-GCM, the id, source and kind
-- as additional data) under a key that is never in this file. The store holds the sealed bytes and never opens them.
CREATE TABLE credential (
  id         TEXT PRIMARY KEY,
  source     TEXT NOT NULL,
  kind       TEXT NOT NULL,
  sealed     BLOB NOT NULL,
  expires_at INTEGER,
  created_at INTEGER NOT NULL
) STRICT;

-- Documents (design/documents.md): what administrators edit in the pages (the layouts, the announcements), as JSON text under a name.
-- The revision is the content digest; a save names the revision it read, so a stale save is refused.
CREATE TABLE document (
  name       TEXT PRIMARY KEY,
  body       BLOB NOT NULL,
  revision   TEXT NOT NULL,
  updated_at INTEGER NOT NULL
) STRICT;
