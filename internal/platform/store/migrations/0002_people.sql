-- The enterprise mode's people (design/enterprise.md, EN4 and EN5): who signed in, the GitHub tokens they linked, sealed (AES-256-GCM,
-- the person, source and owner as additional data, under a key that is never in this file), and the repositories each token lists.
-- The other modes leave these tables empty.
CREATE TABLE person (
  id       TEXT PRIMARY KEY, -- the OIDC issuer and subject
  identity TEXT NOT NULL,    -- the identity claim's value at the last sign-in
  name     TEXT NOT NULL,
  first_at INTEGER NOT NULL,
  last_at  INTEGER NOT NULL
) STRICT;

CREATE TABLE person_token (
  person     TEXT NOT NULL,
  source     TEXT NOT NULL,
  owner      TEXT NOT NULL,    -- the organisation the token reads, lower case
  login      TEXT NOT NULL,    -- the token's GitHub login
  sealed     BLOB NOT NULL,
  expires_at INTEGER,          -- NULL: GitHub named no end
  created_at INTEGER NOT NULL,
  state      TEXT NOT NULL,    -- ok, or why GitHub refused it last
  listed_at  INTEGER,          -- when its repositories were last listed; NULL: never
  PRIMARY KEY (person, source, owner)
) STRICT;

CREATE TABLE person_access (
  person  TEXT NOT NULL,
  source  TEXT NOT NULL,
  owner   TEXT NOT NULL,
  repo_id INTEGER NOT NULL,    -- the repository's GitHub id
  PRIMARY KEY (person, source, owner, repo_id)
) STRICT;
