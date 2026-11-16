# Who sees what (VS1)

Status: built (plan.md section 4, P-122). Binding order: ../plan.md > this note > `forks.md`, `seo.md`. It replaces IX1 (keeping
pages out of search engines, P-115, P-116) and PK1 (parked repositories, P-120), whose notes are in `../history/`.

## Goal

An administrator chooses, in one place, who sees each product and repository in the portal, and whether search engines are offered
what everyone sees. The three audiences are everyone, signed-in viewers only (work too early to promote) and no one (repositories that
should have been archived on GitHub). The portal keeps reading every repository from GitHub, webhook events included, whatever its
audience, so a change shows at the next publish with everything that happened meanwhile.

## What it is

- **The audiences.** `everyone` (the public views hold it), `signed-in` (only the views with private repositories hold it: the viewers
  `access.*` lets see private repositories) and `no-one` (no view holds it; the base snapshot keeps it). An item everyone sees has a
  search-engine switch (`search`); the others are never offered to search engines. GitHub's private and internal flags are a ceiling:
  such a repository is never shown to everyone (`Effective.Capped`).
- **The document** `visibility` in the store (`internal/feature/visibility`, through `docstore`):
  `{"groups": {id: setting}, "repos": {"owner/name": setting}, "rules": [{"pattern", "audience", "search"}]}`, a setting being
  `{"audience", "search"}`; at most 4,000 products and repositories and 50 rules of 256 characters. Only the choices made are stored.
- **Resolution** (`Doc.Repo`), most specific first: the repository's own choice; the first rule whose pattern (RE2, case-insensitive,
  not anchored, matched against `owner/name`) matches; its products, the widest audience among them (a product without a choice counts
  as the default, so it decides only when no product chose; at the widest audience, search on when any product there has it on); the
  default (everyone, search on). Then the GitHub ceiling. A product's own result (`Doc.Group`) is its choice or the default.
- **The snapshot** (`snapshot.Publisher.Audience`): asked at each publish for every repository with the ids of its groups
  (`Builder.finish` builds the memberships before it asks). The base names the `no-one` repositories (`Parked`, `ParkedCount`) and the
  public-on-GitHub `signed-in` ones (`Restricted`, `RestrictedCount`). View A is the base without the first; the public views are A
  without the private, internal and restricted repositories (`views()`), so every reader of the public view (pages, lists, search,
  tools, MCP, llms, badges, the sitemap, the twin and leak tests' surfaces) leaves them out with nothing of its own. A product all of
  whose repositories a view leaves out is not in that view (P-112). The enterprise mode's per-person views start from A.
- **The mark.** A repository's summary in the contract has `notPublic`: public on GitHub and restricted, so only a signed-in viewer
  reads it; the repository and product pages show it as the badge "Not public" (`VisibilityBadge`).
- **Search engines.** The sitemap (`internal/feature/llms`, the public view for every reader) lists a repository when everyone sees it and its
  switch is on (`Set.Indexed`) and a product's page when the product's own result is (`Set.IndexedGroup`); it is built again when the
  document changes (`Set.Revision`). Pages of a repository or product not offered answer `X-Robots-Tag: noindex, nofollow`
  (`Set.NoIndex`, `httpapi.Options.NoIndex`). No `robots.txt` rule: a search engine must fetch a page to read its noindex.
- **Taking over 2.2.19** (`Import`): a store without the document gets one, once, from the old `parked` and `indexing` documents: a
  parked repository is no one; an excluded product or repository is everyone with search off; the include patterns become rules that
  offer (first), the exclude patterns rules that keep out. The old documents are left as they are, so 2.2.19 still reads them after a
  rollback; the new document is never written back into them.
- **The routes** (admin only, same origin, outside the contract): `GET /api/v1/admin/visibility` (the document; every catalogued
  repository by product from the base, private ones and the ones no one sees included, each with its own choice, its result and why;
  the repositories in no product; the addresses the document names that the catalogue does not hold; each rule's matches; counts),
  `POST /api/v1/admin/visibility/preview` (the same for a draft, nothing saved; the access log tells a preview from a save) and
  `PUT /api/v1/admin/visibility` (the document with the revision it was read at; 412 on someone else's save; a save publishes again).
- **The Admin card** "Who sees what" (`web/src/features/admin/visibility.tsx`): counts; a filter and chips (everyone, signed in, no
  one, private on GitHub, kept from search engines, changed); every product and repository with a three-way choice (everyone locked
  for a private repository), the search switch for what everyone sees, where its audience comes from and a way back to inheriting;
  several rows set at once; the addresses no longer catalogued; the rules, folded, each with its matches. A change is a draft the
  portal previews; the bar at the bottom counts the changes; *Review and save* lists them grouped by what they do (hidden from
  everyone, leaving the public view, back in view, search engines), warns in words when five or more items come back into the public
  view or the sitemap, and saves; *Undo* saves the document held before.
- **The status.** `/api/v1/status` `snapshot.parked` (no one) and `snapshot.signedIn` (public on GitHub, signed in only).

## What it does not do

- It is not access control for private repositories: they are private by `access.*`; the choice only narrows who sees a repository.
- `github.sources[].exclude` keeps a repository out of the catalogue (never read, its events dropped); `no-one` keeps it read.
- A rename on GitHub: the choice was made on the old address, which the card lists as no longer catalogued until it is forgotten; a
  rule matches the new address as any other.

## Acceptance

1. A public-on-GitHub repository set to signed in is absent from every public answer (every contract GET, the pages' routes, search,
   llms, badges, the sitemap, answering as an address that does not exist) and present for a signed-in viewer with `notPublic`
   (`cmd/scp` `TestNoLeakOfSignedInRepositories`).
2. A repository set to no one answers 404 to everyone and is named in no list, feed, search, llms answer or sitemap; set back it
   returns with its commits; the status counts it; only an admin reads or changes the choices (`TestNoOneSeesWhatIsSetToNoOne`).
3. Search engines are offered only what everyone sees with its switch on: a repository, a product and a rule, the sitemap and noindex
   (`TestSearchEnginesAreOnlyOfferedWhatEveryoneSeesWithSearchOn`, `internal/feature/visibility` `TestAudiencesAndSearchEngines`).
4. The snapshot's views follow the audiences; a restricted repository's change moves no public view (`internal/platform/snapshot`
   `TestSignedInRepositoriesAreInNoPublicView`, `TestParkedRepositoriesAreInNoView`).
5. Resolution and the GitHub ceiling (`TestResolve`); taking over 2.2.19's documents once, leaving them unchanged (`TestImport`);
   preview, save, conflict and origin (`TestAdminReadsPreviewsAndSaves`).
6. The card lists, filters, previews, reviews, saves with the revision, undoes and sets several rows (`visibility.test.tsx`).
