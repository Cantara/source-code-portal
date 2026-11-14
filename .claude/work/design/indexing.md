# Keeping pages out of search engines (IX1)

Status: built (plan.md section 4, P-115). Binding order: ../plan.md > this note.

## Goal

An administrator chooses, in the portal's Admin page, which products (groups) and repositories search engines such as Google are not
offered. Those pages leave the sitemap and say `noindex`. The portal's own pages show everything as before.

## What it is

- **The list** is the document `indexing` in the store (`internal/indexing`, through `docstore`, as the announcements are):
  `{"groups": [ids], "repos": ["owner/name"]}`, at most 2,000 of each, lower case, sorted. A group is kept out by its id, a repository
  by its address. A repository that is in groups that are **all** kept out is kept out too; one in no group is touched only by name,
  and one in a group that is not kept out stays (a repository in two products is as public as the more public one).
- **The sitemap** (`internal/llms`) leaves out a kept-out group's page and a kept-out repository's page with its documents
  (`Options.Indexed`, `IndexedGroup`); the listing pages and the other products stay. It is built again when the list changes
  (`IndexRevision`), not at the next snapshot.
- **noindex** is the header `X-Robots-Tag: noindex, nofollow` on every answer for `/repos/{owner}/{name}...` (the page, its documents and
  history, the Markdown) and `/groups/{id}...` of a kept-out item (`httpapi.Options.NoIndex`, from the public view: what a search
  engine sees). A header works for the Markdown answers a meta tag cannot reach. No `robots.txt` rule is written, because a search
  engine must be able to fetch a page to read its noindex.
- **The Admin card** "Search engines" (`web/src/features/admin/indexing.tsx`): a checkbox per product, a filtered list of repositories,
  Save with the revision it was read at (someone else's save in between is refused and offered to load).
- **The routes** `GET`/`PUT /api/v1/admin/indexing` (admin only, same origin, outside the contract); the choices come from the
  admin's full view.

## What it does not do

- It does not hide anything from people: a private repository is private by access, not by this list. It is not an access control.
- `/llms.txt`, `/llms-full.txt` and `/knowledge.yaml` list the portal's repositories for agents, not for search engines, and are not
  changed. A search engine drops a page it already holds only after it fetches the noindex again.

## Acceptance

1. A repository kept out is not in `/sitemap.xml` (nor its documents) and its page, documents and Markdown answer with
   `X-Robots-Tag: noindex, nofollow`; its API answer and the rest of the portal are unchanged (`cmd/scp/indexing_test.go`).
2. A group kept out is not in the sitemap and says noindex; a repository only in kept-out groups follows it, one in another group does
   not (`internal/indexing`).
3. A save shows in the sitemap at once; a save with an old revision is refused; only an admin from the portal's own pages saves.
4. The card saves the whole list with its revision (`indexing.test.tsx`).
