---
name: guide-page
description: Maintain the portal's Getting started page (/guide, "Guide" in the header) - its screenshots of the dashboard's cards and signals in both themes, the numbered callouts on the product card, and the text that explains the signals, the health rules, the work that counts and how to run a portal. Use when the dashboard, the cards, the health rules, the commit kinds, Settings or the configuration change, when the guide's pictures look stale, or when asked to improve, extend or restyle the guide.
---

# Maintain the guide page

`/guide` is the portal's introduction for its readers: how to read the signals (telemetry, fleet map, product card, health,
the work that counts, tables and commits) and how to run a portal of their own. It is a server-rendered page without data or
JavaScript of its own; its pictures are screenshots of a made-up catalogue. Design matters on this page: it is the first page a
newcomer reads. The design note is `.claude/work/design/guide.md`.

## Where things are

| Path | What it is |
|---|---|
| `web/src/features/guide/view.tsx` | The page: sections, text, the penalty table, the annotated card (`AnnotatedCard`), the run steps |
| `web/src/features/guide/route.tsx` | `/guide`: metadata; the release (`SCP_VERSION`) for the commands |
| `web/src/features/guide/shots.json` | Each picture's size in CSS pixels and the card's callout positions: written by `publish.mjs`, never by hand |
| `web/public/guide/<name>-<light\|dark>.webp` | The pictures |
| `cmd/scp/guide_test.go` | `TestServeForGuide`: the demo catalogue (organisation `acme`, products Payments, Identity, Developer tools) |
| `.claude/skills/guide-page/capture.js` | The screenshots and the callout measurements, for the Playwright MCP tool |
| `.claude/skills/guide-page/publish.mjs` | PNG to WebP (`cwebp`), sizes and callouts into `shots.json` |
| `web/src/app/guide.test.tsx`, `web/e2e/smoke.spec.ts` | The checks: one h1, contents match sections, pictures in both themes, axe |

## The facts come from the code

Never write a number or rule from memory. Read it where the pages take it, and keep the guide's words the portal's words:

- Health levels and penalties: `internal/health/health.go` (`LevelOf`, `Assess`, `weight`). The page's `findings` legend copies
  when each is found and its points; the kinds, icons, severities and the score ring come from `entities/health/kinds.tsx`, the
  level names from `entities/health/health.tsx` (`levels`).
- Fleet map ranges: `web/src/entities/repo/health.ts` (`activities`, imported).
- The kinds of work and their texts: `web/src/lib/preferences.ts` (`kinds`, imported); what is AI or automation:
  `internal/commits/kind.go` (`Classify`, the agents, the trailers, `botLike`) and sweeps in `internal/commits/model.go`.
- The card's rows: `web/src/features/dashboard/products.tsx` (`ProductCard`); the tiles: `telemetry.tsx`.
- Running a portal: `docs/operator-guide.md`, `docs/production.md` and `deploy/config.example.yaml`. The page keeps a short path
  (access, groups, check, run, the options) and links to the docs for the rest.

When one of these changed, update the text first, then the pictures.

## Retake the pictures

1. Build the bundle the demo serves: `make web` (from the repository root).
2. Start the demo catalogue in the background (it serves until stopped):
   `GUIDE_SCP_ADDR=127.0.0.1:9290 go test -count=1 -timeout 0 -run '^TestServeForGuide$' ./cmd/scp`
   Wait until `curl -sf 127.0.0.1:9290/api/v1/dashboard` answers, then about ten seconds more for every repository's commits.
3. `mkdir -p /tmp/scp-guide && rm -f /tmp/scp-guide/*.png`, then run the Playwright MCP tool `browser_run_code_unsafe` with
   `filename` set to the absolute path of `capture.js`. It writes `<name>-light.png` and `<name>-dark.png` at twice the pixels and
   returns the card's row positions as JSON.
4. Save that JSON as `/tmp/scp-guide/callouts.json` and run, from the repository root:
   `node .claude/skills/guide-page/publish.mjs /tmp/scp-guide /tmp/scp-guide/callouts.json` (needs `cwebp`: `brew install webp`).
5. Look at every picture (the Read tool shows images) in both themes before using it: no half-loaded page, no focus ring, no
   tooltip, the right product. Then `make web` again and look at `/guide` on the demo at 1280 px and 375 px, in both themes:
   the markers sit beside their rows, nothing overflows, the code blocks scroll.
6. Stop the demo (`kill $(lsof -ti tcp:9290 -sTCP:LISTEN)`).

A new picture: add its shot to `capture.js` (a locator's `screenshot`, both themes), use it with `<Shot name=... alt=...>` and a
`Figure` caption. A change to the card's rows: update `card` in `view.tsx` and the measured rows in `capture.js` in the same order.
A change to the demo (another signal to show): edit `TestServeForGuide`; keep it made up (`acme`, `Example` people) and relative to
now, so the pictures read the same whenever they are taken. Never take pictures of a real portal: no real names in the repository.

## Design rules

- One h1 (the hero), then two parts (reading the portal, then running one), each of numbered sections (`01` ...) with an h2 and a
  one-sentence lede; the contents (chips on a phone, the sticky "On this page" beside the page) list every section by part.
- Pictures at their own size, never enlarged: a figure fits its picture. The table is taken with the side panel folded.
- Explain in the reader's words, short sentences, no key names before section 8. A rule is said once, where it is used.
- Every picture comes in both themes (`dark:hidden` / `hidden dark:block`), with a true alt text and a caption; its width and
  height come from `shots.json` so nothing shifts while it loads.
- No inline styles anywhere (the CSP forbids them): positions are SVG attributes, colours are the theme's tokens (`primary`,
  `--ai`, `muted-foreground`, the `lvl-*` classes), never fixed colours.
- Phone first: one column below `lg`, no horizontal page scroll, code blocks scroll on their own (`overflow-x-auto`, focusable).
- Keep the weight down: WebP at quality 90, pictures cropped to the element (the latest commits stop after five), about 1 MB in all.

## Before you commit

- `npx -y pnpm@12.11.2 -C web exec vitest run src/app/guide.test.tsx` and `make GOLANGCI_LINT_VERSION=2.15.0 check` (axe on `/guide`
  in both themes, the pictures load, every section in the contents).
- Update `.claude/work/design/guide.md` when the page's structure changes.
- A Conventional Commit such as `docs(guide): ...` for text and pictures, `feat(guide): ...` for a new section.
