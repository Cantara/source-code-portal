# Keeping pages out of search engines (IX1)

Status: built (plan.md section 4, P-115, reworked by P-116). Binding order: ../plan.md > this note.

## Goal

An administrator chooses, in the portal's Admin page, which products (groups) and repositories search engines such as Google are not
offered. Those pages leave the sitemap and say `noindex`. The portal's own pages show everything as before.

## What it is

- **The list** is the document `indexing` in the store (`internal/indexing`, through `docstore`, as the announcements are):
  `{"groups": [ids], "repos": ["owner/name"], "exclude": [patterns], "include": [patterns]}`, at most 2,000 groups and repositories
  and 50 patterns of 256 characters each. A group is kept out by its id, a repository by its address. A repository that is in groups
  that are **all** kept out is kept out too; one in no group is touched only by name or pattern, and one in a group that is not kept
  out stays (a repository in two products is as public as the more public one).
- **The rules** (P-116) are what keeps out the repositories that arrive later: an `exclude` pattern (RE2, case-insensitive, not
  anchored, matched against `owner/name`) keeps out the repository it matches unless an `include` pattern matches it too. The order
  is: named, then the products, then the patterns (`Set.Why`: `named`, `product`, `pattern`, or empty). A pattern is validated and
  compiled when the document is saved or loaded; RE2 cannot take long on an address.
- **Private repositories** are never offered to search engines (the sitemap and the header follow the public view) and are neither
  listed nor named by the card or its routes: it says how many there are.
- **The sitemap** (`internal/llms`) is the public view for every reader, a signed-in one included: search engines never sign in, and the
  list shows what they are offered (a signed-in viewer saw private repositories and products in it until P-117). It leaves out a kept-out group's page and a kept-out repository's page with its documents
  (`Options.Indexed`, `IndexedGroup`); the listing pages and the other products stay. It is built again when the list changes
  (`IndexRevision`), not at the next snapshot.
- **noindex** is the header `X-Robots-Tag: noindex, nofollow` on every answer for `/repos/{owner}/{name}...` (the page, its documents and
  history, the Markdown) and `/groups/{id}...` of a kept-out item (`httpapi.Options.NoIndex`, from the public view: what a search
  engine sees). A header works for the Markdown answers a meta tag cannot reach. No `robots.txt` rule is written, because a search
  engine must be able to fetch a page to read its noindex.
- **The Admin card** "Search engines" (`web/src/features/admin/indexing.tsx`): the public products with their repositories nested, a
  checkbox each (a tick keeps it out of the sitemap), each repository saying "in the sitemap" or "kept out" with its reason (ticked, with
  its product, by a rule) and a count of both; below, folded as advanced, the two boxes of rules, one pattern a line, each with the
  count of public repositories it matches; Save with the revision it was read at (someone else's save in between is
  refused and offered to load). A change is previewed by a dry run (`PUT` with `dryRun`) a moment after it is typed.
- **The routes** `GET`/`PUT /api/v1/admin/indexing` (admin only, same origin, outside the contract); the body is the list, the tree
  of public products with their repositories and why each is kept out, the ungrouped public repositories, the matches of each
  pattern, the number of private repositories, the revision.

## What it does not do

- It does not hide anything from people: a private repository is private by access, not by this list. It is not an access control.
- `/llms.txt`, `/llms-full.txt` and `/knowledge.yaml` list the portal's repositories for agents, not for search engines, and are not
  changed. A search engine drops a page it already holds only after it fetches the noindex again.

## Acceptance

1. A repository kept out is not in `/sitemap.xml` and its page, documents and Markdown answer with
   `X-Robots-Tag: noindex, nofollow`; its API answer and the rest of the portal are unchanged (`cmd/scp/indexing_test.go`).
2. A group kept out is not in the sitemap and says noindex; a repository only in kept-out groups follows it, one in another group does
   not (`internal/indexing`).
3. A pattern keeps out a repository that arrives later, an include pattern excepts it, a private repository is never listed by the
   card, a dry run saves nothing and a pattern that is no expression is refused (`internal/indexing`, `cmd/scp/indexing_test.go`).
4. A save shows in the sitemap at once; a save with an old revision is refused; only an admin from the portal's own pages saves.
5. The card saves the whole list with its revision and previews a change (`indexing.test.tsx`).
