# Content: docs, commits, badges
Status: v1 design note, 2026-10-14. Binding order: ../plan.md > this note > ../reference/.

## Scope (v1)
- `internal/docs`: README and `docs/**` Markdown rendered to `SafeHTML` with a table of contents; raw source kept for `internal/llms`.
- `internal/safehtml`: the `SafeHTML` type and the single sanitiser policy.
- `internal/commits`: the latest 30 default-branch commits per repository, merged feeds for group and dashboard, `GET /api/v1/commits`.
- `internal/badges`: local flat SVG badges `last-commit`, `release`, `license`, `issues` per repository and `repos` per group.
- The `scp.yaml` descriptor is owned by `catalog.md`.

## Decisions
- Relative images resolve to `raw.githubusercontent.com` at the commit SHA; absolute `https` images are kept: images load from their hosts.
- CSP for the SPA is `img-src 'self' https:` (http-api.md); `<img>` gets `referrerpolicy="no-referrer"` and `loading="lazy"`: image hosts see no portal URL.
- `.adoc`/`.rst`/`.txt` and extensionless non-Markdown READMEs render as escaped text (`safehtml.Text`): every README shows something, with a typed state (GR-29).
- Mermaid fences render as code blocks (`<pre><code class="language-mermaid">`).
- goldmark v1.8 with `html.WithUnsafe` (raw `<details>`, `<img>`, `<kbd>`, `<sub>` are common in READMEs); sanitising after rendering is therefore mandatory.
- chroma v2 with `WithClasses(true)`: highlighting is classes only, so the CSP keeps `style-src 'self'`; the theme CSS ships with the SPA.
- bluemonday behind `safehtml.Sanitize`, the only constructor of `SafeHTML`: swappable, and the type proves sanitisation.
- Stored HTML is untrusted bytes; it is re-sanitised when loaded from SQLite (output is a sanitiser fixpoint, about 1 ms per document): the cache cannot inject script.
- Heading and author ids are prefixed `doc-`; `<a name>` is removed: document ids stay apart from SPA globals.
- A pipeline version integer is part of every cache key; a golden lock test fails until it is bumped: a library bump changes stored HTML only with a version bump.
- A linear shape guard runs before goldmark (nesting 64, one inline stream 40 KiB, sum of squared stream lengths 2e9): rendering time stays linear in input size.
- Highlighting runs for blocks up to 4 KiB and within 1.5 s per document: time stays bounded on crafted input.
- Front matter: four keys (`title`, `description`, `order`, `hidden`) are read, others are kept as text of the source; README is rendered whole, as GitHub does.
- Commit, release, licence and issue facts live in `internal/snapshot` value types; `badges` reads the snapshot (plan rule 2).
- `issues` shows GitHub's `open_issues_count` (includes pull requests) with that meaning documented: it comes with the listing.
- `release` reads `/releases/latest` (published, non-prerelease): one call, GitHub's own definition of "latest".
- Badge text widths come from a committed Go table in `internal/badges`, copied from the archive's DejaVu Sans 2.37 table, plus `textLength`.
- Badge ETag is a hash of the body: `last commit: today` turns `yesterday` at 00:00 UTC without a data change.
- One palette, WCAG AA contrast.

## Shapes
```go
// internal/safehtml
type SafeHTML struct{ s string }            // zero value = empty; String() for DTOs
func Sanitize(b []byte) (SafeHTML, error)   // only constructor from HTML; input cap 8 MiB
func Text(s string) SafeHTML                // escaped text in one <pre>
const PolicyVersion = 1

// internal/snapshot (shared value types)
type Commit struct{ SHA, Subject, AuthorName, AuthorLogin, URL string; CommittedAt time.Time }
type CommitRef struct{ Repo RepoKey; SHA string; At time.Time }
type Release struct{ Tag, Name, URL string; Published time.Time }
type Doc struct {
    Key, Path, Title, Description string; Order int; Format string // markdown | text
    HTML safehtml.SafeHTML; Source []byte; TOC []Heading; BlobSHA string
    Truncated bool; Fresh Freshness                                // Fresh.State: ok | no_content | too_large | unsupported | error
}
type Heading struct{ Level int; ID, Text string }   // h2..h3 only
```
Per repository the snapshot holds `RepoEntry{Repo, Commits, Docs, Release}` (sync-snapshot-store.md): `Docs` README first, then by
`order`, then path; `Commits` newest first, at most 30. Globals: `Latest` and `LatestByGroup` (`latest_n + 1` refs each).

## Behaviour
### Docs
1. Job `docs.refresh` per repository: TTL `docs.ttl`, plus `EventPush` (default-branch push or changed `pushed_at`); unchanged blob SHAs cost no fetch.
2. README via GitHub's readme endpoint (its resolution: `.github/`, root, `docs/`), conditional request; 404 is `no_content`.
3. Docs: tree entries under `docs.root` with extension `.md` or `.markdown` (case-insensitive), first `docs.max_docs_per_repo` in path order;
   a file equal to the resolved README is listed once; front matter `hidden: true` removes it from the index (display, not access control).
4. Format by the README's actual path: `.md`/`.markdown` Markdown; extensionless `README` is Markdown when valid UTF-8 and its first 4 KiB
   starts with `#` or contains a fence, `](` or a list marker; anything else is text.
5. Size: over `github.max_file_bytes` (1 MiB) is `too_large` (fetched again when the blob changes, GitHub link shown); over
   `docs.max_doc_bytes` renders the first bytes cut at a line boundary with `Truncated: true` (shown by the SPA).
6. Encoding: BOM stripped; UTF-16 or NUL-dense input is `unsupported`; other invalid UTF-8 replaced with U+FFFD.
7. Pipeline, fixed order: split front matter (16 KiB cap, anchors rejected) -> truncate -> shape guard -> goldmark parse (CommonMark 0.31 +
   GFM tables, task lists, strikethrough, `Linkify` autolinks, footnotes; GitHub-compatible heading ids; alerts `[!NOTE|TIP|IMPORTANT|WARNING|CAUTION]`
   as `div.md-alert.md-alert-<kind>` with `p.md-alert-title`) -> AST rewrite of links and images -> chroma -> `safehtml.Sanitize` (last).
8. Heading ids: author id kept, else github-slugger (lower-case, letters/numbers/`_`/`-`, spaces to `-`, duplicates `-1`, `-2` by a per-base
   counter, empty `heading`), then `doc-` prefix. Fragment links `#x` become `#doc-x`.
9. Links: relative paths resolve against the document's directory (leading `/` is repository-relative; a path above the root keeps only
   the text); an indexed doc becomes its portal route `/repos/{owner}/{name}/docs/{path}`; any other path `{html_url}/blob/{sha}/{path}`
   (`/tree/` for a trailing slash). Schemes `http`, `https`, `mailto`. External links get `rel="noopener noreferrer nofollow ugc"`.
10. Images: relative path -> `https://raw.githubusercontent.com/{owner}/{name}/{sha}/{path}`; absolute `https` kept; any other scheme
    keeps the alt text as text; `srcset` candidates follow the same rule (failing candidates removed); `alt` always present.
11. Code: `<pre class="chroma"><code class="language-x">`, language must match `^[A-Za-z0-9_+#.-]{1,32}$`; budgets per Decisions.
12. Sanitiser policy (`PolicyVersion` 1), starting from bluemonday `UGCPolicy`:
    - Allowed beyond UGC: `details`, `summary`, `kbd`, `sub`, `sup`, `picture`/`source[srcset]`, `input[type=checkbox][disabled][checked]`,
      `div[align]`, `p[align]`, `img[width,height,align,loading,referrerpolicy]`, `td/th[align]`, `role` on footnote elements.
    - `id`: `^doc-[A-Za-z0-9_:.-]{1,100}$`; `class`: allow-list (`chroma` token classes, `language-*`, `md-alert*`, `footnote*`, `task-list-item`).
    - URL schemes: `http`, `https`, `mailto` for `href`; `https` for `src`/`srcset`; relative URLs only to portal routes.
    - Stripped with content: `script`, `style`, `iframe`, `object`, `embed`, `svg`, `math`, `form`, `textarea`, `select`, `button`, `noscript`,
      `template`, `audio`, `video`, `canvas`, `link`, `meta`, `base`, comments. Attributes stripped everywhere: `on*`, `style`, `name`, `srcdoc`,
      `formaction`, `ping`, `data-*`. Other input types are removed.
13. TOC: h2 and h3 of the rendered document (id, plain text capped at 120 runes), in order; served in the doc API, rendered by the SPA.
14. Title: front matter `title`, else first H1 text, else file name; description: front matter, else first paragraph with visible text (300 runes).
15. Raw source (after truncation, before rendering) is stored in `Doc.Source`; `internal/llms` serves it, removing HTML comments and
    invisible/bidi/control characters on output. Raw `.md` routes exist for Markdown documents.
16. Failure: a GitHub error keeps the previous `Doc` and marks it stale; a render error stores the document as `text` with the error code.

### Commits
1. Job `commits.refresh` per repository: TTL `commits.ttl` (2 h safety net), plus `EventPush`; the head probe returns early when the
   default-branch head SHA is unchanged.
2. One request: list commits on the default branch, `per_page = 30` (one page).
3. Per commit: SHA lower-case hex 40 or 64 (else dropped); first line of the message, invisible/bidi/control characters removed, capped at 200
   runes with `…`; author name capped at 100 runes; author login when it matches GitHub login syntax (else empty); committed date UTC whole
   seconds, missing date drops the commit, a date beyond now+24h is clamped to ingestion time (kept across refreshes); URL `https`, else
   derived `{html_url}/commit/{sha}`. Commits store author name and login.
4. Order newest first by `(committedAt desc, sha desc)`; duplicates by SHA dropped.
5. Storage: the 30 commits are the item's payload, replaced on each successful run; a removed repository's item cascades.
6. Aggregation at snapshot build: `Latest` and `LatestByGroup` are the newest `latest_n + 1` refs (the sentinel tells the first page that
   more exist); a repository in several groups appears in each group feed.
7. API `GET /api/v1/commits?group=&repo=&limit=&cursor=` (`repo` is `owner/name`): `limit` 1..200 (default 50); a first unfiltered or group
   page with `limit <= latest_n` is served from the feed; otherwise a heap merge of the repositories' commits. Order `(committedAt desc,
   repo asc, sha desc)`; the cursor is an opaque key position bound to the filter (survives new commits at the top); `repo` and `group` intersect.
8. Freshness of a feed: `stale` when any repository in scope has failed, has no value yet, or is older than 1.5 x `commits.ttl`.
9. Day grouping happens in the SPA in the viewer's time zone; the server returns UTC.

### Badges
1. Routes: `GET /badges/repos/{owner}/{name}/{kind}.svg` (`last-commit`, `release`, `license`, `issues`) and `GET /badges/groups/{group}/repos.svg`.
2. Job `badges.release` per repository: TTL `badges.release_ttl`, plus `EventTag`; `LatestRelease`, 404 means "no release".
3. Messages: `last-commit` `today`/`yesterday`/`N days ago` (2..13)/`N weeks ago`/`N months ago`/`N years ago` by UTC calendar days, green up
   to 90 days, blue to 365, amber beyond; `release` tag name; `license` SPDX id (`NOASSERTION`/`OTHER` -> `other`); `issues` `{n} open`
   (green at 0); `repos` count. Counts: 0..999, then `1.2k`, `12k`, `1.2M` (truncated).
4. Unknown repository, kind or group: `404` with a small grey `unknown` SVG, `Cache-Control: public, max-age=60`, no ETag.
5. Known repository without data: `200` grey `unknown`; known absence (no release, no licence, no commits): `200` grey `none`; both `max-age=60`.
6. Data badge: `200`, `Cache-Control: public, max-age=<badges.cache_max_age>`, strong ETag of the body, `304` on match.
7. Snapshot not ready: `503` grey `starting`, `Retry-After: 15`, `no-store`.
8. Every response: `Content-Type: image/svg+xml`, `nosniff`, `Content-Security-Policy: default-src 'none'; sandbox`,
   `Cross-Origin-Resource-Policy: cross-origin`; same-origin headers only, no cookies. Owner and name match case-insensitively; an old
   address serves through the alias map.
9. Rendering: typed structs through `encoding/xml`; elements `svg`, `title`, `g`, `rect`, `text`; height 20, 5 px padding, label and
   message capped at 40 runes with `…`; characters illegal in XML 1.0 become U+FFFD; `role="img"`, `aria-label` and `<title>` =
   `label: message`; white text; palette label `#555555`, success `#1a7f37`, info `#0b61a4`, warning `#b45309`, neutral `#6e6e6e` (all >= 4.5:1).
10. Width: sum of the table's advances (font units, U+0020..U+017F; marks 0, wide 2048, else 1271) x 11 / 2048, truncated once, rounded up
    to odd; `textLength` fixes the drawn length for any viewer font.

## Config (v1 keys only)
| Key | Default | Range |
|---|---|---|
| `docs.root` | `docs` | repository-relative, no `..`, at most 200 bytes |
| `docs.ttl` | `1h` | 5m..24h |
| `docs.max_doc_bytes` | 512000 | 1 KiB..1 MiB |
| `docs.max_docs_per_repo` | 50 | 1..200 |
| `commits.latest_n` | 50 | 1..200 |
| `commits.ttl` | `2h` | 1m..24h |
| `badges.cache_max_age` | `5m` | 0..1h (`0` = `no-cache`) |
| `badges.release_ttl` | `24h` | 1h..168h |

## Tests
- Golden files under `testdata/golden/`: rendered HTML for a corpus (GFM features, alerts, footnotes, heading collisions, link and image
  rewrite cases, `README.rst`/`README.adoc`/empty/binary/2 MiB, truncation mid-fence); badge SVGs byte-exact (each kind, unknown, none,
  long, hostile `<script>&"'`, non-ASCII); commit normalisation tables (bad SHA, no date, future date, bidi subject, 300-rune subject).
- Pipeline lock: a digest of all goldens; changing output without bumping the pipeline version fails.
- Fuzz: `FuzzRender` (no panic, within time, output passes the allow-list walk), `FuzzSanitizeFixpoint` (`Sanitize(Sanitize(x)) == Sanitize(x)`),
  `FuzzFrontMatter`, `FuzzBadgeWellFormed` (strict XML, closed element and attribute set), `FuzzCursor` (decode never panics).
- XSS corpus (javascript/data/vbscript, entity-obfuscated schemes, svg/math mXSS, `srcset` tricks, `id=__proto__`, `<base>`, custom elements):
  no banned token in output. Allow-list differential walk written independently of the policy.
- Bounds: worst accepted document renders under about 2 s; adversarial nesting and inline streams rejected in microseconds; highlight budget.
- Commits: fast path equals merge path byte for byte; cursor survives insertion at the top; ETag of `last-commit` changes at midnight (synctest).
- Badges: 404 body identical for unknown repository and unknown kind; golden widths for a sample of strings pin the width table.

## Reference (archive pointers)
- `../reference/detailed-design/20-design/10-docs-pages-media.md` sections 4.0 (limits, measurements), 4.2, 4.4, 4.5 (HTML pass rules), 4.7, 4.15.
- `../reference/detailed-design/20-design/09-commits-badges.md` sections 4.2-4.3, 4.10-4.13, 7.4 (normalisation, endpoint, SVG spec, headers, width table).
- `../reference/detailed-design/11-adr/ADR-005-*`, `ADR-006-*`, `ADR-011-*`; `10-architecture.md` section 14; `02-design-guardrails.md` GR-16, GR-28, GR-29, GR-32, GR-33, GR-37.
