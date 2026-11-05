# Search: one index of what the portal holds (Q1 to Q3)

Status: queued (plan.md section 4), next. Binding order: ../plan.md > this note.

A reader looks for something the portal already knows: the repository that does OAuth, the document that explains the session
timeout, who uses `slf4j-api`, the commit that rotated the keys. Today there is no search: the assistant's `search_repositories`
tool matches words against names, descriptions and topics, and nothing else. Q1 to Q3 give every reader, every agent and the
assistant one search over the portal's own data, answered from memory in the reader's view.

## The choice

**An inverted index in memory, one segment per repository, built from the snapshot's facets, queried through the view's segments.**

- The data is already fetched, cleaned and bounded: indexing it costs no GitHub call, and a query costs no database read, no network
  and no egress. Requests keep reading only the snapshot (plan.md section 2); SQLite stays a rebuildable cache.
- A segment is the index of one repository's documents. It is built when that repository's data changes and reused at every other
  publish, so a publish rebuilds the few repositories that changed, not the fleet.
- A view's index is the set of its repositories' segments. The public view holds only public repositories' segments, so a public
  query never reads a private repository's terms, counts or text: privacy by construction, as the snapshot's two views already give
  every other surface (P-35), not a filter after the fact.
- Every instance of a cluster builds the same index from the same snapshot and answers the same query alike; nothing is shared and
  nothing is stored.

Rejected:
- **Source code search.** Not by GitHub's code search API (10 requests a minute per token, the legacy engine without regular
  expressions, the default branch only, no archived repositories and none quiet for a year, no visibility qualifier, so private
  matches inflate its counts), and not by checking out repositories and indexing their files (runners, disk and a second store for
  private source code). Owner, 2026-11-05: neither. The portal searches what it reads: names, descriptors, documents, manifests,
  packages and commits.
- **A shared index store** (Hazelcast, Redis, Postgres): owner, 2026-11-05: none. A code-sized index would not fit them either: a
  query would read many keys over the network, the per-query cost this design avoids.
- **SQLite FTS5 in the store**: a query would read the database, against the rule that handlers read only the snapshot, and the view
  would be a `WHERE` clause on every query, a second place for a leak.
- **Bleve** (a full-text engine in Go): a large dependency with one mutable index across snapshots, where the view would be a filter
  on its hits; its analysers, stemmers and on-disk formats are more than a catalogue of this size needs.
- **Semantic search with embeddings**: a running cost and a provider for every change; not now.

## What is indexed

Each repository yields documents of five kinds. Text goes in as the portal holds it: cleaned repository text (`untrusted`), the
documents' Markdown source after truncation, the parsed manifests. Nothing is fetched for search.

| Kind | Document | Fields (weight) | Hit opens |
|---|---|---|---|
| `repo` | the repository | name and owner (8), descriptor title (6), topics and tags (4), description (3), language, licence, lifecycle, owners (2), the newest release tag (1) | the repository page |
| `doc` | one section of a document: the text between two headings (h1 to h3), the README's first section included | the document's title (5), the section's heading (4), its text (1) | the document at the heading's anchor (`DocPage#id`) |
| `dep` | one manifest | its module coordinates (6), each dependency's name (3) | the repository page's manifests panel |
| `package` | one published package | its coordinates (6), the newest version and registry (1) | the repository page's packages panel |
| `commit` | one default-branch commit | subject (2), body's first 4 KiB (0.5), author name and login (2) | the commit in the navigator (`?at=sha`) |

Groups are searched too: their name (8) and description (3), from `snapshot.Groups` per view, a handful of documents built with the
view's index (not per repository). A group's members never reach a public result through it: a group in the public view lists only
its public members.

Not indexed: other branches' commits (they rotate, and the default branch is where work lands), the commit list beyond what
`commits` keeps (500 a repository), images, binary files and source code.

## Q1: the index (`internal/search`)

A feature, as `llms`: it never imports another feature; `cmd/scp/search.go` hands it each repository's documents through
`search.Options` (functions, as `llms.Options`), mapping the facets of `catalog`, `docs`, `manifests`, `packages`, `commits` and
`badges` to `search.Document{Kind, Ref, Title, Fields, At}`. The commits' sweeps (fleet-wide) and kinds come through an option too,
read at query time, so a segment depends only on its repository.

**Text analysis.** Lower case after NFKC; tokens are runs of letters and digits; a compound (`Whydah-TypeLib`, `TypeLib`,
`session_timeout`, `net.whydah.sso`) is indexed whole and in its parts (`whydahtypelib`, `whydah`, `typelib`), so either finds it.
No stemming: the text is English and Norwegian mixed and mostly names; a query's last word matches as a prefix ("sess" finds
"session"), and a word with no hit of at least five letters is tried again within one edit ("sesion"), named on the page as such.
Tokens longer than 64 bytes are dropped; a document indexes at most 64 KiB of text.

**Segment.** For one repository: its documents (kind, reference, the fields' lengths) and, per term, the postings (document number
and frequency per field), delta- and varint-encoded in one byte slice, with the terms sorted for prefix lookups. No positions: a
phrase (`"session timeout"`) is the terms' intersection checked against the document's text, which the facets already hold, for the
candidates alone. Budget at the load test's 1,000 repositories (cmd/scp/load_test.go): under 64 MB for every segment, a cold build
under 3 s, a publish that changed one repository under 20 ms.

**Reuse.** `snapshot.Snapshot.Digest(k)` (new): the sum of a content digest of each of the repository's items, kept by
`Builder.Mark` beside the version's digest (P-21), with the listing's fields hashed in. The version's digest also holds the item's
check times, so a check that found nothing new would rebuild the segment; the content digest holds only state, content SHA and
payload, so it changes exactly when anything a segment reads changes, parser and pipeline versions included (they are in the
payload). The indexer keeps the last build's segments by key and digest and reuses each whose digest is unchanged; the public view
is built after the full one from the same segments, so it costs only its assembly. A repository gone from the snapshot drops its
segment at the next publish.

**Query.** Words are all required; `"..."` is a phrase, `-word` excludes, `OR` between two words takes either. Filters: `in:` (a
kind), `group:`, `repo:` (owner/name or a part of it), `lang:`, `topic:`, `lifecycle:`, `author:` (commits), `kind:` (hand, ai,
automation, as the lens), `dep:` (a dependency's name), `before:` and `after:` (dates, commits). At most 256 characters, 12 words
and 8 filters; a prefix expands to at most 64 terms, the edit to 16. A query fans out over the view's segments (document frequencies
summed first, for the view's IDF), scores BM25F with the weights above and the kind's weight (repo 1.5, group 1.5, doc 1.2, package,
dep 1, commit 0.8), and keeps the best 1,000; equal scores order by recency, then reference. A query checks its context between
segments and stops at the request's deadline.

**Snippets.** For a page's hits only: up to 240 characters of the field that matched, around the first match, as plain text with
the matches' offsets (`highlights: [[start, end]]`). The page marks them itself; no HTML leaves the server, and repository text never
becomes markup (as the assistant's answers, P-28).

## Q2: the API, the tool and the leak test

- `GET /api/v1/search?q=&in=&page=&limit=` in the contract: the parsed query (words, filters, a corrected word), the count per kind,
  and a numbered page (P-26: 25, 50 or 100 a page) of hits `{kind, title, repo, url, snippet, highlights, at}`. `in` narrows to a
  kind; the counts are always all kinds, for the page's tabs. An empty query is 400 as a problem; nothing found is 200 with none.
- `GET /api/v1/search/suggest?q=`: up to 8 repositories, groups and documents whose names or titles start with the words, for the
  header's box; no snippets.
- Both are read from the viewer's view and are contract GETs, so the leak test walks them; it adds queries that name the private
  repository, a word only in its README, its commit subject and its dependency, and fails on any hit, count or suggestion.
- The tool catalogue: `search_repositories` becomes `search` over the index (every kind, the filters in its description), so the
  assistant, MCP and WebMCP get it at once; its answer is framed like every tool's (P-54).
- `/llms.txt` names `/api/v1/search`; the admin status shows the index (segments, terms, bytes, the last build's time); the metrics
  get `scp_search_requests_total{kind}` and `scp_search_duration_seconds`.

## Q3: the pages

- `/search?q=` (feature `search`): rendered on the server from the API, a plain `GET` form, so it works without JavaScript. Tabs by
  kind with their counts, the hits with their snippets (matches in `<mark>`), the filters as removable chips, the pager, "did you
  mean" for a corrected word, and an empty state that shows the query language. `noindex`, not in the sitemap; robots.txt disallows
  `/search`.
- The header gets a search box (a form to `/search`, so it too works without JavaScript); with JavaScript, `/` or ⌘K focuses it and
  an accessible combobox shows the suggestions as one types (debounced, the browser cache's `suggest`), Enter opens `/search`.
- `/opensearch.xml` and the dashboard's JSON-LD `WebSite` with a `SearchAction`, so a browser adds the portal as a search engine.
- The Guide gets a section on searching (the query language, what is and is not searched); the API guide and the operator guide name
  it; the plugin follows `docs/`.
- The header's box and the combobox count against the JavaScript budget (P-41); the new sum is recorded there when measured.

## Done when

- Q1: the index finds every kind on a test catalogue by words, phrase, prefix, one edit and each filter; a publish that changed one
  repository rebuilds one segment; the public view's index holds no private segment; the load test meets the budgets above.
- Q2: the contract's search answers a private viewer with the private repository's hits and the public with none (the leak test);
  the assistant's tests pass on the `search` tool.
- Q3: Playwright finds a document section by a word and opens it at its heading, with and without JavaScript, the combobox with the
  keyboard alone, axe clean in both themes.
