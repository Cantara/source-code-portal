# Store model for a personal edition and shared instances (SM1)

Status: design, verified by prototypes, not built (plan.md section 4, P-89). Binding order: ../plan.md > this note > `sync-snapshot-store.md`,
`access.md`, `hardening.md`, `auth.md`.

The owner wants a personal edition (one person signs in to GitHub, the portal reads the repositories that person can read; several
GitHub hosts) and, later, several people on one instance sharing what is safe to share while the rest stays private to each. The store
(`internal/store`) knows neither people nor credentials nor more than one audience. This note says how to model it, what stays, and how
the existing Cantara database is kept.

## The constraint: the security model does not weaken (owner, 2026-11-07)

Every decision below is checked against these. A step that cannot keep one of them does not ship.

- **I1. A view is a portal that never knew what the viewer may not read.** Today `Public()` is built from scratch, by one pass over
  the repositories followed by every feature's index (`publish.go`, `publicView`); `leak_test.go` and `twin_test.go` hold it. A view
  for any audience is built by the same function, from the repositories that audience may read, and nothing is ever filtered out of a
  shared derived index at read time. A survey of the derived state found why: the search ranking uses statistics over the visible
  set (`sums`, `count`), the commit sweep rule (a subject in more than 10 repositories on one day is not a person's work) depends on
  which repositories are in view, and the dependants' counts, the fleet totals, the group defaults and a view's `Version` are all
  functions of the visible set. Filtering at read time would leak through counts, scores, orders and versions.
- **I2. Fail closed.** Anything unknown, expired or failed about a viewer's entitlement is the public view (or no access with
  `access.require_auth`).
- **I3. One audience per instance stays the rule until the proof exists.** The personal edition is one principal on one instance:
  exactly today's model (one audience, one credential holder). More than one principal on an instance is not built until the leak
  and twin tests are generalised to many viewers and pass (the gates below).
- **I4. Credentials are sealed at rest** (AES-256-GCM under a key derived from the instance's secret, bound to the row they belong
  to), never logged, never returned by any API, never put in a snapshot.
- **I5. Read-only towards GitHub (P-71) is unchanged.**
- **I6. Data leaves with its last reader.** A private repository nobody can read any more leaves the store with its items.

## What is there (verified on a copy of the live Cantara database)

Schema `0001`: `meta` (key/value: the build marker, parked webhook events), `repo` (`key` primary, `source`, `github_id`, unique on
`(source, github_id)`), `item` (one row per sync job key: kind, `repo_key` with `ON DELETE CASCADE`, status, times, attempts,
`content_sha`, `error`, JSON `payload`). The engine is the only writer; handlers read the in-memory snapshot, the store is read once
at the warm start (`Load`). Sources, credentials and the catalogue's visibility are config and payload, not tables.

The Cantara database: 134 repositories of one source, 1,073 items, 27 MB of payload, all `public`. `commits` is 67% of it, `docs` 29%.
The payloads compress 5.5 times (zlib), so size is no argument for a second storage layer. Payloads stay JSON blobs: nothing queries
inside them except the migration below; a relational model of the content would buy nothing while the read path is the snapshot.

Three flaws the design has to fix, each shown by running the code:

1. **Two sources listing the same `owner/name` take the address from each other on every listing.** `replaceCatalogue` deletes the
   other source's row (a "transfer between sources") and the cascade deletes all its items. Run against the store: ownership
   alternated at each of four listings and no item survived one. It cannot happen today only because the config check rejects an
   account listed under two sources (`config/sections.go`); it becomes real with a wizard that adds hosts at runtime, and a person's
   own login can exist on two hosts.
2. **A database newer than the binary is quarantined and the store starts empty** (`migrate`: "schema version is newer than this
   binary"). A rollback of the binary after a migration therefore resyncs everything. The data is moved aside, not lost, and only the
   newest two quarantined copies are kept.
3. **The source's catalogue listing is one item per source** (`catalog.reconcile/<source>`, one credential). A per-person listing
   needs its own item.

## The model

Migration `0002`, additive only: no existing column, key or payload is rewritten.

```sql
CREATE TABLE source (                 -- the hosts; 'config' ones mirror scp.yaml, 'wizard' ones are added at runtime
  id TEXT PRIMARY KEY, origin TEXT NOT NULL DEFAULT 'config' CHECK (origin IN ('config','wizard')),
  base_url TEXT, created_at INTEGER NOT NULL) STRICT;
ALTER TABLE repo ADD COLUMN visibility TEXT NOT NULL DEFAULT 'unknown'
  CHECK (visibility IN ('unknown','public','private','internal'));   -- backfilled from each source's catalogue item
CREATE TABLE principal (              -- a person (or the instance's own credential) on one host
  id INTEGER PRIMARY KEY, source TEXT NOT NULL, user_id INTEGER NOT NULL, login TEXT NOT NULL,
  kind TEXT NOT NULL CHECK (kind IN ('instance','user')), created_at INTEGER NOT NULL, UNIQUE (source, user_id)) STRICT;
CREATE TABLE credential (             -- sealed; bound to its row, its source and its kind
  id INTEGER PRIMARY KEY, principal_id INTEGER NOT NULL REFERENCES principal(id) ON DELETE CASCADE,
  kind TEXT NOT NULL CHECK (kind IN ('user_token','pat')), sealed BLOB NOT NULL, refresh_sealed BLOB,
  expires_at INTEGER, state TEXT NOT NULL CHECK (state IN ('ok','expired','revoked','error')),
  checked_at INTEGER, created_at INTEGER NOT NULL) STRICT;
CREATE TABLE visible (                -- what a principal's last listing held: its entitlement, and a repository's reference count
  principal_id INTEGER NOT NULL REFERENCES principal(id) ON DELETE CASCADE,
  repo_key TEXT NOT NULL REFERENCES repo(key) ON DELETE CASCADE, checked_at INTEGER NOT NULL,
  PRIMARY KEY (principal_id, repo_key)) STRICT, WITHOUT ROWID;
CREATE INDEX visible_repo ON visible (repo_key);
ALTER TABLE item ADD COLUMN credential_id INTEGER REFERENCES credential(id) ON DELETE SET NULL;  -- who collected it
```

- **Data is stored once per repository.** An item belongs to the repository, not to a person. A repository's data is the same for
  everyone who may read it, so it is collected once, by any credential that may read it and has the permissions the job needs.
  `credential_id` records who did, for audit and for choosing another when that credential goes.
- **Entitlement is `visible`.** A principal's listing (`GET /user/repos` and the organisations it reads, through its own token) is
  replaced as a whole, in one transaction. Public repositories need no entitlement to be read; their `visible` rows count the
  principals that list them. The instance's own credential (an App installation or `token_file`, as today) is a `principal` of kind
  `instance`: an operator deployment keeps its model, `access.private_groups` still says who sees the private repositories, and
  `visible` holds nothing that decides a view there.
- **I6 as a rule.** After a listing is applied, `DELETE FROM repo WHERE visibility <> 'public' AND NOT EXISTS (SELECT 1 FROM visible
  WHERE repo_key = repo.key)`; the items go with it by cascade. A public repository leaves by the two-complete-listings rule that
  exists (a partial listing never drops data).
- **Revocation.** A credential that GitHub refuses (401, or 403 for SSO) goes to `revoked`, its principal's `visible` rows are deleted
  in the same transaction, and the rule above runs: access ends at once, not at the next listing (I2). A person who loses a
  repository on GitHub loses it from the portal at their next listing; `visible.checked_at` older than `access.max_staleness`
  (default 1 hour) counts as no entitlement.
- **Listing jobs.** The kind `catalog.reconcile` keeps its key `catalog.reconcile/<source>` for the instance credential (the Cantara
  database needs no change). A person's listing is the kind `catalog.visible`, key `catalog.visible/<source>/<principal>`, run with
  that person's credential; `RetainSources` treats both.
- **Credentials.** `sealed = nonce || AES-256-GCM(HKDF(instance key), plain, aad = id|source|kind)`, the instance key from a key file
  (`store.key_file`, the session key's pattern in `auth/seal.go`). The GitHub client's ETag cache is keyed on the credential's
  `rateBook` by pointer today; a per-person token needs a stable credential id in that key, or a reused address could replay
  another person's entry. User tokens from a GitHub App expire in hours: the refresh token is sealed too and refreshed before use.

## Views and the viewer seam

`Public()` becomes a special case of `ViewOf(keep)`: the same code, with the predicate `Repo.Public()` replaced by the viewer's
entitlement. Nothing else about the model of two views changes.

- **One view per distinct entitlement set**, not per person: the set of private repositories a viewer may read, hashed. A view is
  built lazily at the first request for `(snapshot version, set digest)` and kept in a small LRU (default 16). Viewers with the same
  set share it. A set not in the cache that cannot be built in time falls back to the public view (I2).
- **The seam is one function**: `Current(ctx)` in `cmd/scp/serve.go` picks `cur` or `cur.Public()` from `httpapi.Private(ctx)`.
  It becomes `cur.For(viewer)`. Three places hard-code two views and change with it: `llms` and `sitemap` (a two-slot cache chosen by
  `s != s.Public()`, served `Cache-Control: public`: a view that is not the public one must be `private`), the web UI server's
  `X-Scp-Viewer` (`public`, `private`, `admin`) which must carry the view's identity, and the SSE streams (each calls `current(ctx)`
  on every publish, so the cache must make that a lookup).
- **Version, ETag and events follow the view**: `Version` is the sum of the digests of the view's repositories, as the public
  version is today, so a change a viewer may not read moves nothing they can observe (`TestPrivateChangeUnseen`, per pair of viewers).

## Addresses

`RepoKey` is `owner/name` and is the key of the store, the engine, the snapshot, the cluster's wire format, the search segments, the
URLs and the API. Making it unique across hosts touches all of them, so it is staged:

- **Now: first claim wins.** The address belongs to the source that listed it first; a second source's repository of the same
  address is a *conflict*: not stored, named in the Admin status and the log, never silently swapped. The config rule that one account
  belongs to one source stays for configured sources. Prototype: ownership stays with the first source, its items are kept.
- **If a real collision needs both:** a secondary source's repositories are addressed `owner@source/name` (a GitHub login holds
  only letters, digits and hyphens, so `@` is unambiguous, and the address stays two path segments so the routes, the proxy rules and
  the API paths keep their shape). Every place that cuts a `RepoKey` at `/` to call GitHub then takes the owner from the job's source
  instead; the survey lists them. Webhooks name no host, so a secondary source needs its own route or secret. That is its own
  design when it is needed.

## Migration and rollout

- **Cantara's database is kept.** `0002` adds tables and two columns and backfills `repo.visibility` from the catalogue items;
  `source` gets one row per existing source id. `user_version` becomes 2.
- **A copy before the migration** (`VACUUM INTO scp.db.pre-0002`, kept until the next start finds the migration done and the
  operator removes it; the rollback is to put it back). Rolling the binary back without it quarantines the database (flaw 2) and
  resyncs.
- **The warm start is unchanged**: `Load` reads the same three tables; the new ones are read only by the code that needs them.
- The earlier rule "greenfield: no migrations" is withdrawn for the store from this point (P-89); config keys and the contract
  stay greenfield.

## Verified (prototypes, thrown away; the tests below become the first tests of the build)

On a copy of the live database (never the original), in a scratch worktree:

| Question | Result |
|---|---|
| Does `0002` keep the data? | `Load()` (every repository, every item field, every payload byte) hashes identically before and after; integrity check ok, foreign-key check 0 violations; 134 repositories backfilled `public`; migration 47 ms including opening |
| Is the pre-migration copy cheap and exact? | `VACUUM INTO` 44 ms, 26 MB; the copy opens and hashes to the same digest |
| Does a view of any subset equal a portal that only knew that subset? | A generalised `publicView` (`ViewOf(keep)`) against a snapshot fed only the subset: 200 random subsets over 14 repositories on 2 sources with aliases, 3 groups (one default hidden, one without a default) and an index; repositories, aliases, groups (members, default, `DefaultMissing`), source counts, `Version` and the index all equal. Three mutations of the view (version from the full set, default left set, counts left unfiltered) each fail it. The existing `TestPublicView` passes unchanged |
| What does a view cost at the real scale? | 1,000 repositories with every real index (search, feeds, graph, groups): 1.9 ms for 100 repositories, 2.9 ms for 500, 3.7 ms for 900; 20 distinct views of 500 held: +31 MB (1.6 MB each). A view per distinct set is affordable; filtering at read time is unnecessary |
| Do two sources with one address lose data? | Yes (flaw 1): the store alternates owners at every listing and no item survives. With "first claim wins" the owner stays and the item is kept |
| Are credentials safe at rest? | A sealed value opens only under its own row id, source, kind and instance key; a flipped bit is refused; the token is not in the database file or its WAL in plaintext |
| Does the reference-count rule keep I6? | Two people on one private repository, one on another: when the first leaves, only the repository nobody reads goes, with its items; when the second leaves, the other goes; public repositories are untouched |

## Gates before the build starts on more than one principal

1. `leak_test.go` and `twin_test.go` generalised to many viewers: two or more hidden repositories with distinct marks, viewers none,
   A, B, A+B and all, every route, tool and MCP call; for viewer U and every repository X it may not read, no trace of X and the
   address of X answers as one that does not exist. A per-viewer `Version` test (a change a viewer may not read moves nothing it sees).
2. Adversarial fixtures for the cross-repository statistics that make a count or an order depend on the viewer's set: a commit
   subject in more than 10 repositories on one day where some are hidden, the same package name used by hidden and visible
   repositories, a group whose default is hidden, a search term present only in hidden documents.
3. The view cache's fallback to the public view tested under load and under a failed build.

## Order

SM-a (the migration, `repo.visibility`, the conflict rule, the pre-migration copy; no behaviour change) → SM-b (`ViewOf`, `Public()`
as its special case, the viewer seam, the gates' test generalisation, still with two views) → SM-c (the personal edition: one
principal, sealed credential, the person's listing, the wizard; a design note of its own) → SM-d (several principals on one
instance: only after the gates pass; it may never ship).
