CREATE TABLE repo (
  org TEXT NOT NULL, name TEXT NOT NULL, payload BLOB NOT NULL, updated_at INTEGER NOT NULL,
  PRIMARY KEY (org, name)) STRICT;
CREATE TABLE item (
  org TEXT NOT NULL, repo TEXT NOT NULL, kind TEXT NOT NULL,
  etag TEXT, content_sha TEXT, fetched_at INTEGER NOT NULL, status TEXT NOT NULL, error TEXT,
  attempts INTEGER NOT NULL DEFAULT 0, next_due_at INTEGER, payload BLOB,
  PRIMARY KEY (org, repo, kind)) STRICT;
CREATE TABLE commit_log (
  org TEXT NOT NULL, repo TEXT NOT NULL, sha TEXT NOT NULL, committed_at INTEGER NOT NULL, payload BLOB NOT NULL,
  PRIMARY KEY (org, repo, sha)) STRICT;
CREATE INDEX commit_log_recent ON commit_log (org, repo, committed_at DESC);
