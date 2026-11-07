# Filters: one panel at the left of every list page (FP1)

Status: built (plan.md section 4, P-86). Binding order: ../plan.md > this note > `pulls.md`, `issues.md`, `releases.md`, `archive.md`,
`insights.md`, `search.md`, `web.md`.

The list pages (Commits, Pull requests, Issues, Releases, Archive, a repository's history) and the two that narrow what they show
(Insights, Search) had their selectors in a row above the list: selects that hide their choices, an Apply button, state chips beside
them. The owner (2026-11-07) wants one look: the selectors in a panel at the left with a hide and show button, like the dashboard's
panel at the right, and every choice in view at once, as in UniFi's console, where nothing waits behind a menu.

## The panel

- `shared/filters.tsx`: `FilterLayout` (the panel, its rail, the list beside it), `FilterForm`, `FilterGroup`, the choices `Choices`
  (radios with counts and dots), `Toggle` (a switch), `TextFilter` (a search box), `ProductChoices`, `SizeChoices`, and the hook
  `useListNav` (a page's state read from and written to its address; `pick`, `pickSize`, `clear`).
- **Every choice in view.** A choice is a radio (one of many) or a switch (on or off), never a select; the groups are open when the
  page loads and fold with their chevron. A long list (the products, the branches) keeps to a height and scrolls inside.
- **Counts.** The states of pull requests and issues, the variants of releases, the reasons of the archive and the kinds of search
  keep their counts, now in the panel (they were chips). A choice that holds none is dimmed and cannot be picked, unless it is the
  current one: the reader sees that it exists and that nothing is in it.
- **Applied at once.** A choice goes to the address as it is made (`router.push`, back works); the text fields apply on Enter, on
  leaving the field and half a second after the last key. There is no Apply button with JavaScript.
- **Without JavaScript** the panel is a GET form of the same controls (the server renders them checked from the address) and a
  `<noscript>` Apply button submits it.
- **Hide and show.** The panel's button hides it; a rail with a vertical "Filters" shows it again (the dashboard's side panel does
  the same at the right). The choice is the cookie `scp-filters` (`open` or `closed`) for every list page, rendered on the server
  (`filtersOf(ctx.cookies)`). A reader who never chose has it open on a wide screen and folded to a bar on a narrow one, where it
  would push the list below the first screen.
- **Clear** returns to the unfiltered list when a filter is set; the page size is not a filter and stays.
- The panel is sticky below the header and scrolls inside when it is taller than the window; it is an `aside` named for the page
  ("Filter pull requests"), its groups are `radiogroup`s with the group's name.
- Page size is a choice of the panel ("Per page"), kept in `scp-per-<list>` as before. A group's commits page has the page size only.

## The rows

`shared/list-row.tsx`, `ListRow`: every list draws its rows the same: an avatar at the left, the title (a link), the aside at the
right of the title's line, then the meta line, and under it a summary when there is one. The aside is what weighs the row: the
change's size for a commit and a pull request (`DiffStat`), the comments of an issue, when it was published for a release, when it
was last active for a retired repository. A list is one `panel` (a day's commits one each). `compact` keeps a row to two short lines
for the side panels. The git graph's rows keep their own layout (the graph cell takes the place of the aside's column).

## Tests

Unit: the pages show the choices with their counts and the dimmed ones; the panel's state per cookie. Playwright (`e2e/filters.spec.ts`
and the page specs): hide, show and remember across pages; a choice applies at once and Clear undoes it; axe in both themes; a phone
has the panel folded; the right edge of every list's aside is the same; without JavaScript each page's form filters.

## Out of scope

The Admin and Settings pages keep their forms (they are settings, not filters); the dashboard keeps its own panel at the right.
