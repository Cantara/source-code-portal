-- Sealed credentials (design/personal.md): a token the portal reads GitHub with, kept encrypted (AES-256-GCM, the id, source and kind
-- as additional data) under a key that is never in this file. The store holds the sealed bytes and never opens them. Additive.
CREATE TABLE credential (
  id         TEXT PRIMARY KEY,
  source     TEXT NOT NULL,
  kind       TEXT NOT NULL,
  sealed     BLOB NOT NULL,
  expires_at INTEGER,
  created_at INTEGER NOT NULL
) STRICT;
