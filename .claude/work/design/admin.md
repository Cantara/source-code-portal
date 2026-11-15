# The admin page in sections (AD1)

Status: built (plan.md section 4, P-123). Binding order: ../plan.md > this note > `auth.md`, `visibility.md`, `announcements.md`,
`prerequisites.md`, `assistant.md`.

## Goal

An administrator opens `/admin` and sees first what needs action, then reaches each area in one step, on a desk or a phone, without
one long page of cards where the figures run together.

## What it is

- **Sections** (`web/src/features/admin/view.tsx`, `sections`), one shown at a time, each at its own address: *Overview* (`/admin`),
  *Who sees what* (`#visibility`), *Announcements* (`#announcements`), *GitHub* (`#github`), *Assistant* (`#assistant`). The address
  is the fragment (`useSection`, `useSyncExternalStore` over `hashchange`), so a link, the back button and a reload keep the section,
  and nothing is asked of the server for a change of section. An unknown fragment shows the overview.
- **The side menu** (`SectionNav`, `nav` "Admin sections"): a column beside the section from `lg`, sticky; a row that scrolls
  sideways above it on a phone (one column, no horizontal page scroll). The current section has `aria-current="page"`; a dot marks a
  section that needs action: the overview when the status has warnings, GitHub when a prerequisite is unmet or a source's listing is
  not fresh.
- **Overview** (`Overview`): the status's warnings first, then four tiles, each with what it means and a link to where it is acted on
  (repositories with how many everyone, only signed-in viewers and no one see, private ones included, from the counts of
  `GET /api/v1/admin/visibility`, GitHub with what is to fix, events or polling with the last
  delivery, the jobs running and waiting), then the instance and the snapshot (with `parked` and `signedIn`, the public-on-GitHub repositories only signed-in viewers
  see).
- **GitHub** (`GitHub`): the sources at full width (padded cells, counts right-aligned, the table scrolls inside its card), the
  webhook and the prerequisites.
- The other sections are the cards they were: `VisibilityCard`, `AnnouncementsCard`, `AssistantCard`.

## Acceptance

1. The overview leads with the warnings and the tiles; the menu shows one section at a time and marks it; the sources' id and count
   are cells of their own (`web/src/app/signin.test.tsx`).
2. Every section reached from the menu passes axe in both themes, and the address names it (`web/e2e/visibility.spec.ts`); the
   announcements are written at `/admin#announcements` (`web/e2e/announcements.spec.ts`).
3. At 375 px every section fits the screen (checked in a browser; the table and the menu scroll inside themselves).
