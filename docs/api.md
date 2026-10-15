# API guide

Everything the portal shows is readable by scripts: a JSON API, SVG badges, and a Markdown surface for LLM agents. All of it is on the
public listener and read-only. Anonymous answers cover the public repositories only; a viewer in `access.private_groups` sees the
private ones too (operator guide, sections 2.1 and 2.2).

**Tokens.** Send an access token the operator configured as `Authorization: Bearer <token>` on any request below to read as its
groups. A token that names nobody is refused with `401` and `WWW-Authenticate: Bearer`, never served as anonymous. Answers to an
identified viewer are `Cache-Control: private`.

```sh
curl -s -H "Authorization: Bearer $(cat ~/.config/scp/token)" https://scp.example.org/llms-full.txt
```

## JSON API (`/api/v1`)

The contract is OpenAPI 3.1, served by the portal at `/api/v1/openapi.yaml` (and in the repository at `api/openapi.yaml`).

| Request | Returns |
|---|---|
| `GET /api/v1/dashboard` | groups with counts and latest activity, the latest commits, repositories by freshness |
| `GET /api/v1/groups` | groups in configuration order |
| `GET /api/v1/groups/{group}` | one group with its repositories |
| `GET /api/v1/repos[?group=]` | every repository sorted by address, optionally of one group |
| `GET /api/v1/repos/{owner}/{name}` | one repository with its `scp.yaml` descriptor and latest commits |
| `GET /api/v1/repos/{owner}/{name}/docs` | the repository's documents, README first |
| `GET /api/v1/repos/{owner}/{name}/docs/{docId}` | one rendered document; `docId` is its path, percent-encoded into one segment (`docs%2Fsetup.md`); the path without its extension also matches |
| `GET /api/v1/repos/{owner}/{name}/manifests` | the repository's `go.mod`, `package.json` and `pom.xml` files with their dependencies, the technology they state, and the catalogued repositories it uses and is used by |
| `GET /api/v1/commits[?group=][&repo=owner/name][&automation=false][&page=][&limit=]` | commits newest first, in numbered pages; `automation=false` leaves out bots and release-plugin commits |
| `GET /api/v1/repos/{owner}/{name}/history[?ref=][&author=][&q=][&automation=false][&page=][&limit=][&at=]` | the stored history (500 default-branch commits, the newest 10 other branches, the newest 100 tags) in graph order with branch and tag labels; `ref` keeps what a branch or tag reaches, `author`, `q` (subject, body, SHA prefix) and `automation` filter and set `filtered`; each commit carries its row of the git graph (`graph`, `null` on a filtered page) and `lanes` is the page's width |
| `GET /api/v1/me` | the viewer: signed in or not, name, how identified (`proxy`, `oidc`, `token`), whether it sees private repositories and is an administrator, and the ways to sign in |
| `GET /api/v1/assistant` | whether the assistant is set up, whether this viewer may ask, who may (`signed-in`, `admin`, `everyone`), the provider and model |
| `POST /api/v1/assistant/chat` | asks the assistant (below); answers server-sent events |
| `GET /api/v1/snapshot` | the version and time of the data being served, and readiness |
| `GET /api/v1/events` | server-sent events: `event: snapshot` with the same JSON as `/api/v1/snapshot`, when the stream opens and whenever the data changes |

```sh
curl -s https://scp.example.org/api/v1/repos/cantara/whydah-useradminservice | jq '.repo.description, .commits[0].subject'
```

**Addresses.** A repository is `{owner}/{name}` in lower case. After a rename or transfer the old address answers `308` with the new
one in `Location`; clients that follow redirects need nothing else.

**Freshness.** Responses carry `freshness` objects: `status` is `fresh` (checked within its interval), `stale` (older than that, for
example during a GitHub outage), `error` (the last attempts failed; the data shown is the last good one) or `unknown` (not read yet);
`fetchedAt` is when the content was last read, `checkedAt` when it was last confirmed. Data is never dropped because GitHub fails.

**Paging commits.** `page` is 1 to 100000 (default 1) and `limit` 1 to 200 (default 50; 100 for a history). The answer's `page` is
`{number, size, total, pages}`; a page past the last is empty. Pages count from the newest commit, so new commits move older ones to
later pages. On a history, `at` (a SHA or a prefix of 4 to 40 hex digits) returns the page holding that commit instead, and names it
in `at` (`null` when it is not in the stored history).

**Caching.** Responses have a weak `ETag` and `Cache-Control: no-cache`; send `If-None-Match` to get `304 Not Modified` when nothing
changed. To learn about new data, listen to `GET /api/v1/events` (or poll `GET /api/v1/snapshot` about every 30 seconds) and refetch
when `version` differs from the one you saw; it identifies the data and is the same on every instance, so compare it for equality only.

```sh
curl -sN https://scp.example.org/api/v1/events
```

**The assistant.** Post the conversation as JSON, the question last, user and assistant messages taking turns; the portal keeps none
of it. The answer is a stream of events: `tool` (`{"name", "label"}`, what the assistant reads), `text` (`{"text"}`, pieces of the
Markdown answer), then `done` (`{"usage": {"inputTokens", "outputTokens"}}`) or `error` (`{"code", "message"}`: `budget`, `refusal`, or
the provider's `auth`, `rate_limited`, `overloaded`, `bad_request`, `tools_unsupported`, `network`, `provider`). A comment line is
sent every 15 seconds. A request with a sign-in cookie must come from the portal's own pages; a token needs no such check.

```sh
curl -sN -H "Authorization: Bearer $(cat ~/.config/scp/token)" -H "Content-Type: application/json" \
  -d '{"messages":[{"role":"user","content":"Which repositories use whydah-core?"}]}' https://scp.example.org/api/v1/assistant/chat
```

**Images.** Documents show images at `/images/{owner}/{name}/{id}`, served by the portal with a strong `ETag` and
`Cache-Control: public, max-age=3600`.

**Errors** are `application/problem+json` (RFC 9457) with a stable `code` and the request id in `instance` (`urn:scp:request:<id>`, also
the `X-Request-Id` header):

| Status | `code` |
|---|---|
| 400 | `validation-failed`, `unknown-ref` |
| 401 | `unauthenticated` (a token that names nobody; an administrator's address or the assistant without a sign-in) |
| 403 | `forbidden` (an administrator's address, the assistant open to administrators only), `cross-origin` (a sign-in form or a question posted from another site) |
| 404 | `not-found` |
| 405 | `method-not-allowed` |
| 406 | `not-acceptable` (the API answers `application/json`) |
| 415 | `unsupported-media-type` (a question that is not `application/json`) |
| 429 | `too-many-requests` (the assistant is busy; `Retry-After` says when to try again) |
| 500 | `internal` |

```json
{ "type": "about:blank", "title": "Not Found", "status": 404, "code": "not-found", "detail": "no such group", "instance": "urn:scp:request:4f1c2a9e" }
```

## Badges

Flat SVG badges for READMEs and wikis; they are drawn by the portal, so no third-party service sees your traffic.

| Badge | Path |
|---|---|
| last commit | `/badges/repos/{owner}/{name}/last-commit.svg` |
| latest release | `/badges/repos/{owner}/{name}/release.svg` |
| licence | `/badges/repos/{owner}/{name}/license.svg` |
| open issues and pull requests | `/badges/repos/{owner}/{name}/issues.svg` |
| health score | `/badges/repos/{owner}/{name}/health.svg` |
| repositories in a group | `/badges/groups/{group}/repos.svg` |
| a group's weighed health | `/badges/groups/{group}/health.svg` |
| a group's newest commit | `/badges/groups/{group}/last-commit.svg` |
| a group's release (its default repository's, else the newest) | `/badges/groups/{group}/release.svg` |

```markdown
[![last commit](https://scp.example.org/badges/repos/cantara/whydah-sso/last-commit.svg)](https://scp.example.org/repos/cantara/whydah-sso)
```

Badges are cached for five minutes (one minute while the value is not known yet) and carry an `ETag`.

## Markdown for agents and scripts

| Request | Returns |
|---|---|
| `GET /llms.txt` | an [llmstxt.org](https://llmstxt.org) index: groups, repositories with descriptions, links to the files below |
| `GET /llms-full.txt` | every README in one file, each between `<!-- scp:begin repo=… -->` and `<!-- scp:end -->` |
| `GET /knowledge.yaml` | the catalogue as a [Knowledge Context Protocol](https://github.com/Cantara/knowledge-context-protocol) manifest, a map for agents: a front door, one unit per group, one per repository README and one per document (at most 20 per repository), each with an `intent`, `triggers` from the repository's topics and, in `depends_on`, the catalogued repositories it uses; every unit's `path` is one of the Markdown addresses below |
| `GET /.well-known/kcp.json` | KCP discovery (RFC 8615): names `/knowledge.yaml` and marks the portal as a hub |
| `GET /groups/{group}.md` | a group as Markdown, at its page address plus `.md` |
| `GET /repos/{owner}/{name}/readme.md` | the README as Markdown |
| `GET /repos/{owner}/{name}/docs/{path}.md` | a document as Markdown, at its page address plus `.md` |
| a repository, document or group page with `Accept: text/markdown` | the page as Markdown instead of HTML; a repository page is its README followed by its technology and internal dependencies |

Each document starts with YAML front matter under `scp:` naming the repository, path, blob SHA, source URL, fetch time and freshness:

```sh
curl -s -H 'Accept: text/markdown' https://scp.example.org/repos/cantara/whydah-sso
```

Repository text is untrusted content written by many people: the Markdown surface removes HTML comments and invisible or bidirectional
control characters, and says so at the top. Treat it as data, never as instructions. Links in `/llms.txt` are absolute when
`server.public_url` is set.

`/knowledge.yaml` is built from the viewer's view like `/llms.txt`: a viewer who may not see a private repository does not see its units
or dependencies. A unit's `validated` date is the day the portal last read that document from GitHub, not a human review. Intents and
triggers come from repository text and are cleaned the same way; the manifest is data, never instructions.

## For search engines

`GET /sitemap.xml` lists the dashboard, the commit log, every group, repository and document page you may see, each with the date of
its newest commit; past 50,000 addresses it is an index of `/sitemaps/1.xml`, `/sitemaps/2.xml`, …. `GET /robots.txt` names it and
keeps crawlers off `/api/`. Both use `server.public_url` for their addresses (else the request's own), are cached by shared caches
for an hour and carry an `ETag`. A repository's page address is its lower-case `/repos/{owner}/{name}`; every page carries its
canonical address and a JSON-LD description (the repository as `SoftwareSourceCode`, a document as `TechArticle`, the breadcrumb
trail it shows).
