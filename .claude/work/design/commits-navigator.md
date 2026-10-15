# Commit navigator (C1)
Status: as built. Binding order: ../plan.md > this note.

## Goal
`/commits` is where an owner follows the work of a product: the product's commits as one timeline, and for any of its repositories a
git graph navigator like the professional tools: compact rows, stable lanes, branch and tag labels, filters, keyboard, a detail
panel and pagination through the stored history.

## What the tools do (research)
- VS Code Git Graph (mhutchie.git-graph): coloured lanes (rounded or angular), branch and tag labels at their commits (local and remote
  combined), find (Ctrl/Cmd+F), branch filter, a details panel with the changed files, compare two commits.
- GitLab repository graph (`/-/network/<branch>`): a dot per commit, newest on top, grey branch labels at each head, picked by ref.
- Gitea and Forgejo `/{owner}/{repo}/graph`: a server-rendered graph with pagination and a branch filter: the closest model to a
  server-rendered portal.
- JetBrains Git log, Fork, GitKraken, Sourcetree: one compact row per commit (graph, refs and subject, author, date, hash), filters
  (branch, author, text, date), selection with a details pane, keyboard navigation, virtual scrolling over long histories.

Kept: the compact row with aligned columns, labels at heads, branch, author, text and automation filters, the details panel,
keyboard, virtual scrolling, cursor pagination with the server rendering the first page. Left out: write actions (checkout, merge,
cherry-pick: the portal reads), diffs and changed files (one GitHub call per commit viewed and a request-time fetch; the panel links to
GitHub's diff), compare. A graph across repositories is not drawn: commits of different repositories share no parents, so a product
is a timeline, and a repository is a graph.

## Data (`internal/commits`)
Today: the latest 30 default-branch commits, subject only. The `commits` job grows into the history a navigator needs, still read
only from the snapshot by the handlers:
1. Default branch: up to 500 commits (`per_page=100`). A cold run reads 5 pages; a later run reads page 1 (ETag, a 304 is free) and
   follows pages only until it reaches a SHA it holds.
2. Branches: `GET /branches` (one page, 100). The 10 newest branches by head date, without `renovate/`, `dependabot/`, `snyk-fix-`
   and `snyk-upgrade-` (automation
   branches merge themselves within hours and would push people's branches out), read up to 100 commits each, stopping at a SHA
   already held; a full re-read every 24 hours drops what was deleted.
3. Tags: `GET /tags` (one page, 100): labels for commits in the history.
4. A commit gains its message body (cleaned with `untrusted.Strip`, at most 4 KiB) and its committer; parents as today.
5. Payload schema 3; the facet holds commits (deduplicated by SHA) and refs (`{name, kind: branch|tag, sha, default}`).
   A cold start costs about 7 calls per repository (the load test's bound). Budget: at 1,000 repositories about 1,000 x 600 commits x 400 B, 240 MB worst case; the caps above hold it, and the load test
   measures it.

## API
- `GET /api/v1/repos/{owner}/{name}/history?ref=&author=&q=&automation=&cursor=&limit=` (limit 1..200, default 100): the commits in
  graph order (newest first by committer date, a parent never before its child), each with its refs; the cursor is a keyset over the
  graph order bound to the filter. `ref` limits to the commits reachable from that branch or tag; `author`, `q` (subject and body) and
  `automation=false` filter, and a filtered page says so (`filtered: true`), because a filtered list draws no graph.
- `GET /api/v1/commits?group=` (exists) gains `automation=false` and serves the product timeline.
- Contract in `api/openapi.yaml`; `make gen`.

## Web
- `/commits`: a product picker (the groups; `?group=`) and, inside a product, its repositories with their activity. The product's
  timeline is the merged feed grouped by day, a repository chip per row, filters for repository and automation. Choosing a
  repository opens its navigator (`/repos/{o}/{n}/commits`, which the repository page links to as today).
- Navigator (`features/commits/navigator.tsx`, `history.ts`): a listbox of rows 32 px with columns graph | refs and subject | author |
  date | hash, each row's graph cell drawn from `graph.ts` (lanes never shift, freed lanes reused, merge rings, dashed lines to
  parents in the API's `beyond`); branch and tag pills with their icons at their commits (the default branch marked, `+N` past
  three); window virtual scrolling (`@tanstack/react-virtual`) with spacers sized through the DOM (the CSP allows no style
  attribute), the first render drawing the whole first page; infinite loading on scroll and an "Older commits" link; selecting a
  commit highlights its lane and opens the details panel (message, author and committer, exact UTC time, parents and children that
  select their commit, refs, GitHub links to the commit and its files). A filtered page is a list without a graph.
- URL state: `group`, `ref`, `author`, `q`, `automation`, `sha` (the selected commit; a prefix of 4 or more hex digits selects too),
  filters pushed and the selection replaced through `history` (no server round trip); back and forward restore. A `sha` not yet loaded
  pages until it is; one outside the stored history says so and links to GitHub.
- Keyboard: up and down or j and k move the selection, Home and End, Enter opens the panel, Escape closes it, `/` focuses the search.
- Without JavaScript the first page renders on the server with older and newer links.

## Acceptance criteria
1. History: the fake GitHub's ledger proves the reads above: a cold run of a 700-commit repository with 3 branches stores 500 + its
   branch commits and the 100 newest tags; a warm run with no change makes only 304s and the branch and tag listings; a push reads
   page 1 and stops at the first held SHA. A warm start serves the history from the store before any GitHub call.
2. Order and pages: on random DAGs (merges, octopus merges, criss-cross, branches off and back) every page keeps a parent after its
   child; a page fetched after a push neither repeats nor skips a commit; a cursor of another filter is refused.
3. Layout: for every random DAG and page split, the rows of the earlier pages are identical after more pages load (prefix stability);
   every edge starts and ends at a node or a page end; at most 8 lanes, a commit beyond them sits in the last.
4. Labels: every branch and tag of the history shows at its commit; the default branch is marked; four or more labels show three and
   `+N`.
5. Performance: with a whole stored history loaded (500 rows in the e2e suite; the caps allow 1,500), at most 120 rows are in the DOM
   and a scroll through them has no long task over 50 ms; the history API answers p99 under 50 ms at 1,000 repositories in the load test; the JavaScript stays within the
   300 KB budget.
6. Navigation: product, repository, branch, author, text and automation filters work from the URL alone; back and forward restore;
   `?sha=` selects and scrolls to the commit or says it is older than the stored history.
7. Details: the panel shows the message body, author, committer, exact time, parents and children (each selects its commit when
   loaded), refs and the GitHub links.
8. Accessibility: axe clean in both themes on the timeline, the navigator and the panel; the whole flow works by keyboard; each row
   says in text what the graph shows (a merge, its parents, its labels); colour is never the only signal.
9. Without JavaScript the first page of the timeline and of the navigator render with working older and newer links (the SEO spec).
10. The product timeline merges only the product's repositories, grouped by day in UTC, with no graph across repositories.

## Where the criteria are proved
1. `internal/commits/github_test.go` (`TestHistory`: ledger of the cold, warm and push runs); `cmd/scp/run_test.go` (warm start).
2. `internal/commits/history_test.go` (`TestHistoryPages`, `TestHistoryPush`, `TestHistoryFilters`), `commits_test.go` (`TestGraphOrder`).
3. `web/src/entities/commit/graph.test.ts` (random histories).
4, 6, 7, 10. `web/e2e/navigator.spec.ts`.
5. `web/e2e/navigator.spec.ts` (rows and long tasks), `cmd/scp/load_test.go` (the history p99 under 50 ms), the bundle budget.
8. `web/e2e/smoke.spec.ts` (axe in both themes on the timeline, the navigator and the open panel), `navigator.spec.ts` (keyboard).
9. `web/e2e/seo.spec.ts` (older and newer links without JavaScript).
