# Presentation mode: the fleet on a wall (K1 to K2)

Status: K1 and K2 built (plan.md section 4). Binding order: ../plan.md > this note > `layout.md`.

A tablet or a screen on the wall shows the portal all day: an iPad in landscape beside the team, a TV in the corridor. The dashboard is
built for a reader who scrolls, hovers and clicks; on a wall nobody does. K1 and K2 give the portal one page made for being looked at
from two metres: every product, the fleet's health and the latest commits on one screen, live, and honest about its own freshness.

## The choice

**A page of its own, `/present`, composed from the layout store (`present.json`) of compact variants of the existing elements,
filling the viewport without scrolling, live through `/api/v1/events`.**

- One more layout page, not a second dashboard: the elements, their data and their gates are the dashboard's (`layout.md`); an admin
  arranges the page in the same design mode. No new API, no new data, no new store.
- The page fits the screen it is on. It never scrolls and never rotates through slides: what does not fit folds (a line "23 healthy"
  instead of 23 tiles). A carousel always hides the slide you needed.
- The exception first (P-36, rule 1 of `layout.md`): products at attention, then watch, then the rest, the healthy ones folded first.
- A screen that is wrong is worse than a screen that is off: the page says how old its data is at all times and says it loudly when
  the stream has stopped.

Rejected:
- **A compact switch on the dashboard** (a cookie or `?compact`): the dashboard keeps its header, scroll and hover; a wall needs none of
  them and a fixed viewport. Two concerns in one page make both worse.
- **Auto-rotating pages**: see above. Several screens show several pages (`?groups=`), each still, rather than one screen cycling.
- **A native app or a separate kiosk build**: the web page with a manifest does what an app would (full screen, wake lock, home
  screen) at no cost.

## The page

Landscape is the target (an iPad 11" is 1180×820 CSS pixels, 13" is 1366×1024; a 1080p TV 1920×1080); portrait stacks the same
parts in one column. The page is a CSS grid sized by the viewport (`100dvh`), its parts sized by container queries, so the same layout
fits from 1024 to 1920 pixels wide.

```
┌───────────────────────────────────────────────────────────┬────────────────────────┐
│ 134 repos · 13 products · ● 2 attention · ◆ 5 watch ·      │ Latest commits    12 s │
│ 18 people this month · 31% with AI         updated 12 s ● │ ────────────────────── │
├───────────────────────────────────────────────────────────┤ Whydah-UAS   · 4 min   │
│ Fleet health  ▪▪▪▪▪▪▪▪▪▪▪▪ ▪▪▪▪▪▪▪▪▪▪ ▪▪▪▪▪▪▪▪ ▪▪▪▪▪▪▪▪▪▪▪   │  totto  fix token ttl  │
│               ▪▪▪▪▪▪▪▪▪   ▪▪▪!▪▪▪▪   ▪▪▪▪▪▪   ▪▪▪▪▪▪▪▪     │ scp          · 11 min  │
├───────────────────────────────────────────────────────────┤  ove ✦ search box      │
│ ● Whydah        attention  2 │ ◆ Messi    watch           │ ...                    │
│   3 d · v2.4.1 · ▁▃▅▂▆▇▃     │   2 w · v1.0 · ▁▁▂▁▃       │                        │
│   no licence · 4 issues >20  │   release behind           │                        │
├──────────────┬──────────────┬──────────────┬──────────────┤                        │
│ Core   1 d ▅▇│ Tools  4 d ▃▅│ Libs  2 w ▁▂ │ 9 more good  │                        │
└──────────────┴──────────────┴──────────────┴──────────────┴────────────────────────┘
```

- **Status strip**, one line: the brand at its start (the mark and SCP, a link to the dashboard; the mark alone in a narrow column),
  then the dashboard's seven signals in the same order (`signals` in `telemetry.tsx`, one list for both pages), each its icon and
  value alone: the commits by hand, with AI and by automation (a commit with the kind's badge, in the kind's colour as the sparklines
  draw it: commits, not people or bots, P-62), the active repositories, the findings, the products (a grid) and the sync (a cloud
  with a check, crossed out in red while updates fail); the name, what it counts and its parts (the people, the assistants, the bots
  and sweeps, the activities, the costliest findings, the products by level, the repositories by freshness) in its hint. As the
  column narrows the active repositories go first, then the products, then the three kinds of commits together. The freshness indicator closes it.
- **Fleet health**: the `fleet-map` coloured by health level instead of activity (one square per repository, the level's colour and
  a shape, attention outlined and watch round, so colour is never alone), drawn dense: one strip of clusters, each product's squares in
  a hairline outline without a name (a tap on a square names its repository and its product; a screen reader hears each product's
  counts), so the fleet reads as its products in the least room (P-62). The legend counts the levels on the title line. On Cantara's 134
  repositories in 13 products it is 106 px high where the labelled map took 240; 1,000 repositories take about 15 rows.
- **Products as tiles**: a product at watch or attention takes a wide tile of three lines (name, then the level as the fleet map draws it, round for watch and outlined for attention, with how many repositories are at it, "● 6", the words in its hint;
  the last change, the release and the 12-week sparkline of the work that counts; its findings as the dashboard draws them: the finding's mark (the kind's icon on its severity's tile) and the repository, one link, without the reason; the mark's hint tells the kind, severity, cost, what was found and the next step, and the mark opens the finding on its repository's page; two lines of them, the most costly first). A good
  product takes a narrow tile of one line (name, last change, sparkline). When the tiles do not fit, they give up detail in order of
  importance: the good ones fold from the end into one tile, then the mildest at watch lose their wide tile, then they fold too
  ("3 more to watch, 7 good"), then those at attention lose theirs; a product at attention never folds. The page places the tiles as the
  browser will (`fits`: the dense grid's placement, each tile at the first free place from the top left, a wide tile two columns and
  two rows), takes the fewest steps that let the products needing a look fit, then shows as many good ones as fit beside them (so a
  column a wide tile cannot use is filled), and checks what it drew, folding one more while anything is cut off.
- **Latest commits**, the right column at full height: the author's avatar (initials) at the left while the column is at least
  17 rem wide, then the repository with the committer's alias and the time on the first line and the subject alone on the second,
  with the AI and automation marks of the commit lists; as many as fit. A new commit slides in at the top (no motion with
  `prefers-reduced-motion`). It follows the work that counts.
- **The commit column's width**: in landscape its left edge is a handle (`role=separator`), as wide as the gap it replaces and a hairline to see, its hit area reaching past it for a finger: dragged by a finger or a pointer, or
  moved with the arrow keys, from 20% to 60% of the screen in steps of 5% (written grid classes: the CSP allows no style), a double
  tap back to 30%. The width is the reader's, in the cookie `scp-present-column`, so the server draws it. The left column is a
  container, and as it narrows its elements keep what matters most (container queries): the strip drops the people and the share
  with AI first (below 48 rem), then the products (36 rem) and the repositories (28 rem); the good products fold sooner. The products at attention and at watch, their wide tiles and the data's age never go.
- **Text**: 15 px for what is read (tiles, commits, the strip), numbers at 20 px; only the section labels and the fleet map's product
  names are smaller. Dark theme unless the reader chose (`theme-init.js` and `lib/theme.ts` treat the system choice as dark on
  `/present`); the theme's tokens only.

## Behaviour

- **No chrome.** No header, search box, Settings or design mode. A tap or a key shows a small bar at the bottom for 5 s (clear of the strip): back to the dashboard,
  the theme, full screen where the browser has it. Nothing needs a hover: `Hint` cards are not used; what a hint would say is written.
- **Live.** The page follows `/api/v1/events` as the dashboard does and refreshes its data on each new snapshot; it polls only while
  the stream is closed (`features/status/sync.ts`).
- **Freshness.** The strip ends with how current the page is: *Live* while the event stream is open, *Polling* while it is closed and
  the snapshot was read in the last 2 minutes, *Stale* past 2 and *Offline* past 10 or at once while the server fails (a retryable
  error), with the data's age from the snapshot's time beside it ("data from 3m ago"). The colour and the word, never the colour alone.
- **Stays on.** The Screen Wake Lock API keeps the screen awake while the page is visible (asked again on `visibilitychange`; nothing
  where it is missing). A web app manifest (`display: standalone`, the portal's name and icon) lets "Add to Home Screen" open it full
  screen on an iPad.
- **Keeps itself current.** A new bundle (the build skew of `lib/build.ts`) reloads the page instead of offering to; the page reloads
  itself once an hour too, which reseals a signed-in viewer's session (`auth.md`: resealed past half its lifetime on a page request),
  so an iPad signed in once stays signed in on an instance with `access.require_auth`.
- **Burn-in.** Every 10 minutes the content shifts by up to 2 pixels (OLED screens: iPad Pro, TVs).
- **Several screens.** `?groups=core,tools` limits the page to those products, `?work=hand,ai` fixes the work that counts, so each
  screen is a bookmark. Neither is stored.

## Data and layout

- The layout store gets the page `present` (`layout.Store`, `<layout.dir>/present.json`) with a built-in default: `telemetry`,
  `fleet-map`, `products`, `latest-commits`. `GET /api/v1/pages/present` answers it resolved against the viewer's view, so the leak
  test covers it as it covers `dashboard`.
- Two optional element fields (P-39: a new option is one optional field): `fleet-map.color` (`activity`, the default, or `health`) and
  `products.view` gains `tiles`. The present page's defaults are `health` and `tiles`; the dashboard can use them too. The reader's
  products cookie does not apply to `/present`.
- The data is the dashboard's (`GET /api/v1/dashboard?work=`): the members' health and level, the groups' activity, findings and the
  latest commits are already there. No new field unless the tiles show one is missing; then it goes in the contract with the code.
- Design mode works on `/present` as on the dashboard, the draft drawn in the page's frame.

## Gates

- Playwright at 1180×820, 1366×1024 and 1920×1080, in both themes: no scroll (`scrollHeight == clientHeight`), every product present
  as a tile or in the fold, axe clean, the CSP unchanged.
- A fake stream that closes: the indicator reaches watch and attention on fake time; a new build reloads.
- The leak test walks `/present` and `/api/v1/pages/present` as the public.
- The JavaScript budget (P-41): the page reuses the dashboard's elements; it brought the sum to 329.6 KB.

## Work

| M | Delivers |
|---|---|
| K1 | `/present` from the layout page `present`: the grid, the status strip, `fleet-map.color: health`, `products.view: tiles` with the fold, latest commits at full height, the live data and the freshness indicator, `?groups=` and `?work=` |
| K2 | The wall: no chrome with the tap bar, wake lock, the manifest, reload on a new build and hourly, burn-in shift; design mode on `/present`; docs, the Guide (a section with a picture), the plugin |

## Not now

Per-screen layouts stored on the server (a screen is a bookmark), sound or notifications on a change, a TV app, rotating pages.
