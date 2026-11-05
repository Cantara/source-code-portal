# Announcements: news for the people who read the portal (N1, N2)

Status: N1 and N2 built (plan.md section 4). Binding order: ../plan.md > this note > `layout.md`.

The portal is on the team's screens all day: on the dashboard in a tab, on `/present` on a tablet or a wall. That makes it the right
place for the team's news: an event ("Det avholdes juleølsmaking på Rebel Makerspace i år den 26.11", "Meld deg på her"), planned
maintenance, a change everyone must know about. N1 and N2 let an admin, or the deployment's configuration, publish a short
announcement that stands out on the pages it is meant for, and goes away by itself when its time is over.

## The choice

**An announcement is a small structured notice, not free text: a title, an optional line, when and where, one link and a tone,
shown between two dates on the pages it names, to everyone or to signed-in viewers only. It comes from `scp.yaml` (locked) or from
an admin in `/admin` (the layout store's directory, beside the layouts), and a layout element, `announcements`, places it.**

- Structure is what lets it stand out without looking like a problem: a calendar tile for the date, the title one step larger, the
  link as a button, a tone in colours of its own. Free text would be a paragraph nobody reads, and HTML or Markdown would be a way
  to put anything on every page.
- It ends by itself: `until` is required (or the event's own end), so a forgotten announcement never stays up for months.
- A draft is a published flag off: an admin writes it, sees it previewed, and publishes it when it is ready.
- It is a layout element, so an admin moves it in the design mode like every other block, and a page without the element shows none.

Rejected:
- **Markdown or HTML bodies**: one sanitiser exists for repository content (`safehtml`); announcements need no formatting a title,
  a line and a button cannot carry, and plain text cannot be misused.
- **A banner in the header** above every page: the announcement belongs to the pages people watch, the dashboard and `/present`,
  and the header stays the portal's navigation.
- **A store of its own** (SQLite, the cluster map): the layout store's directory already is the admin's place for the pages,
  shared by every instance through the file system and reloaded every 5 seconds; announcements are one more file in it.
- **Per-group announcements**: the portal has no audiences but the public and the signed-in (P-35); per-group would be the first.

## An announcement

| Field | Example | Rules |
|---|---|---|
| `id` | `juleol-2026` | `^[a-z0-9][a-z0-9-]{0,62}$`, unique among the configured and the stored ones |
| `published` | `true` | off: a draft, shown only in `/admin`; configured ones are published unless `published: false` |
| `title` | Juleølsmaking på Rebel Makerspace | required, one line, at most 80 characters after cleaning (`untrusted.Text`) |
| `text` | Det avholdes juleølsmaking i år. Ta med en venn. | optional, one line, at most 280 characters |
| `tone` | `event` | `event` (violet), `info` (the portal's primary), `notice` (amber); default `info` |
| `starts`, `ends` | `2026-11-26T18:00:00+01:00` | optional RFC 3339 times with their offset; `ends` after `starts` |
| `place` | Rebel Makerspace | optional, one line, at most 80 characters |
| `link` | `{label: Meld deg på her, url: https://…}` | optional; https, a host, no user info, at most 2,048 bytes; a label of 1 to 40 characters with it |
| `lang` | `nb` | optional language tag for the text, so a screen reader reads it in its language |
| `pages` | `[dashboard, present]` | one or both; default both |
| `audience` | `everyone` | `everyone` or `signed-in` (a private viewer: P-35's full view) |
| `from`, `until` | `until: 2026-11-26T23:00:00+01:00` | shown from `from` (default: at once) until `until`; `until` is required unless `ends` is set, which is then its default |

At most 20 announcements are configured and 20 stored. The times keep the offset they were written with, and the pages show the
time as written (18:00 in Oslo's winter offset is 18:00 on every screen), never converted to the server's or the reader's zone.

## Where it comes from

- **Configuration**: `announcements[]` in `scp.yaml`, checked by `scp config check` with every other key. The admin card lists
  them, locked: they change with the deployment.
- **Admins**: `/admin` has an Announcements card: new, edit, publish or hold, remove, and a preview drawn by the same component the
  pages use. Saved to `<layout.dir>/announcements.json` (`{"announcements": [...]}`) through the same file store as the layouts
  (`internal/filestore`: atomic rename, a content-digest revision refusing a stale save, a reload every 5 seconds, the last good file
  served with its problem). Without `layout.dir` the card shows the configured ones and cannot save.
- `GET /api/v1/admin/announcements` answers the stored ones with the revision, the configured ones, whether it can save and the
  file's problem; `PUT` takes `{announcements, revision}`. Admin only, same origin, JSON, 64 KiB, out of the contract like the
  layouts' admin routes (P-39). A stored id equal to a configured one is refused.

## Who sees it

The page layout's answer carries them: `GET /api/v1/pages/{page}` (contract) gains `announcements`, the ones the viewer may see on
that page now: published, `from` passed, `until` not, the page among its `pages`, and `audience: signed-in` only for a private viewer.
Ordered by tone (`notice`, `event`, `info`), then by `starts` (the sooner first), then by id; at most 3. The layout feature takes them
through a hook (`Notices(ctx, page)`) that `cmd/scp` wires to the announcements feature; features never import each other. The leak
test walks the pages' GET as the public, so a signed-in announcement that leaks fails it.

Each served announcement carries `revision`, the digest of its fields: a reader's dismissal is kept for that revision, so an edit
shows it again. With a link it also carries `qr`, the link as a QR code (`rsc.io/qr`, level M) in SVG path data and its size in
modules: a wall cannot be clicked, so `/present` shows the code beside the button. Computed once per announcement and kept while its
link is.

## How it looks

A full-width card, taller than its neighbours, with a 4 px bar in its tone at the left, a tinted background, a calendar tile (the
day large, the month small, from `starts`), the title one step larger and semibold, the line, when (the time as written) · where ·
"in 3 weeks" (then "today", "now"), and the link as a solid button with ↗. Colours are tokens (`--announce-event`,
`--announce-info`, `--announce-notice` and their tints), defined for light and dark at AA contrast; none is a health colour
(`lvl-*`), so an announcement never reads as a product's problem. One fade on arrival, no motion after.

- **Dashboard**: dismissible by its ×, kept in the cookie `scp-announcements` (`id:revision` pairs, at most 20), read by the server
  so a dismissed one never paints. An announcement past its `until` while the page is open goes at once.
- **`/present`**: never dismissible (a wall has no reader), larger type and the QR code beside the button where the profile has room
  (Desk, Wall, Handheld landscape); it sits in the left column with the other elements and gives up its text line, then its place,
  as the column narrows (container queries), never its title, date or link.
- `role="region"` named "Announcements", each an `article` with its title as heading; `lang` on the title and the text.

## Built-in layouts

The element is the first row below the top bar: on the dashboard right below the header (announcements, telemetry, fleet map, products, latest commits), on `/present` right below its strip, which is that page's top bar (telemetry, announcements, fleet map, products, latest commits).
An element with nothing to show draws nothing, so a portal without announcements looks as before.

## Done when

- N1: a configured and a stored announcement are served on the pages they name between their dates, a signed-in one only to a
  private viewer (the leak test), a draft never; a stale admin save is refused; `scp config check` refuses a bad one.
- N2: Playwright sees the announcement on the dashboard and on `/present` in both themes (axe clean), dismisses it on the dashboard
  and it stays dismissed across a reload, sees its QR code on a desk; an admin creates, previews, publishes and removes one in
  `/admin`. Docs, the Guide and the operators' plugin tell it.
