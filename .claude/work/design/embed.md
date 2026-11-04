# Embedding SCP in other sites (E1 to E4)

Status: queued, after the next release (plan.md section 4). Binding order: ../plan.md > this note.

A host site (a company's own website, a CMS-built site, an intranet) shows the portal's signals in its own pages: the fleet map, a
product's card, the telemetry, the latest commits, a repository's health. The host may be built on any stack (Next.js, SvelteKit,
Express, static HTML), keeps its own design, and must not break when the portal releases. The portal's own pages and the embedded
ones must never drift apart.

## The choice

SCP publishes its UI as **versioned HTML fragments, composed on the host's server**, each in a **declarative shadow root** with its
own stylesheet, themed through a published set of **CSS custom properties**, and brought to life in the browser by **one small
module of custom elements**. The portal's own dashboard is assembled from the same fragments. Data stays available beside it
through the `/api/v1` contract and a typed client.

Rejected:

- **Iframes**: the content is not the host page's (search engines credit the frame's address), the host sizes a box it cannot
  measure without messages, focus and reading order cross a frame, theming crosses a boundary.
- **A package of the portal's React components**: ties every host to React and Next.js and couples two release trains; a SvelteKit
  or Express host cannot use it.
- **The API alone**: every host draws the fleet map and the cards again, and the copies drift from the portal's within releases.

Server-side composition of HTML fragments is the pattern of long-lived micro-frontend systems (ESI, Podium, Tailor/Mosaic). HTML is
the most stable contract the web has; declarative shadow DOM (in every current browser) isolates styles both ways without a frame.

## The layers

1. **Contract.** `api/openapi.yaml` stays the data contract. Beside it, the fragment contract: `GET /fragments/v1/{kind}` with
   documented parameters (`group`, `repo`, `work`, `theme`, `count`), the custom properties and `::part` names each fragment
   exposes, and the custom element that adopts it. `v1` is additive only; a breaking change is a `v2` served beside it.
2. **Fragments.** The web UI server renders each fragment as server-only HTML: one custom element (`<scp-product>`, `<scp-fleet>`,
   `<scp-telemetry>`, `<scp-latest>`, `<scp-health>`) holding a `<template shadowrootmode="open">` with a `<link rel="stylesheet">`
   to the fragment stylesheet and the content. No inline script or style (a host with a strict CSP takes it as is). Go serves
   `/fragments/` through the page proxy with a strong ETag of the snapshot version and the parameters, `Cache-Control: public,
   max-age=60` for the public view.
3. **Behaviour.** `/embed/v1/scp.js` (an ES module, its own size budget) defines the custom elements. An element whose shadow root
   came with the page adopts it; an element without one fetches its fragment. The elements add what the portal's pages have: the
   hint cards, keyboard behaviour, relative times that age, and live updates (one shared `/api/v1/events` stream per page, a
   fragment refetched when the snapshot's version changes). Without the module the fragment is complete, static content.
4. **Theming.** The design tokens become a published API of custom properties, prefixed `--scp-` (`--scp-primary`, `--scp-font`,
   `--scp-radius`, `--scp-surface`, the activity and health levels, `--scp-ai`), each with the portal's value as its default, in
   light and dark (`theme=light|dark|system`, or the host's `color-scheme`). The token names are tested like the API.
5. **Client.** `@scp/client` (TypeScript, published from this repository): types generated from `api/openapi.yaml`, fetch with ETags
   and snapshot-version caching, a subscription to `/api/v1/events`. For hosts that draw something of their own.

## Host integration

- A host's server fetches a fragment while rendering and puts it in the page, so the content is in the HTML search engines read; it
  caches it under the snapshot's version.
- **Outbound snapshot webhooks** (`embed.notify[]`: URL and secret file): on every new snapshot the portal POSTs
  `{version, builtAt}` signed with HMAC-SHA256 (`X-Scp-Signature-256`, as GitHub signs ours), retried with backoff, so a host
  revalidates its cached pages exactly when the data changes. Delivery is best effort and never blocks a publish.
- A CMS block (`kind`, `group`, `theme` as its fields) is the host's code, in the host's repository, over the fragment contract.

## Access and security

- `embed.origins[]`: the sites allowed to use the portal from a browser. They get CORS on `/fragments/` and `/api/v1` (GET, no
  credentials) and are named in `frame-ancestors` nowhere: fragments are not framed. Empty: no browser embedding; server-side
  composition needs no origin.
- Fragments are the public view. A host's server may send a bearer token (`auth.tokens`) for its viewers' private view; such an
  answer is `Cache-Control: private` and the token never reaches a browser. The leak test walks `/fragments/` like every route.
- The fragments carry the portal's sanitised content only (`SafeHTML`, `untrusted.Text`); the elements render text, never markup
  from data.

## Milestones

| M | Delivers | Done when |
|---|---|---|
| E1 | The contract: this note made exact, the fragment routes and parameters, the custom properties and parts, `embed.origins`, contract tests | a contract test fails when a fragment's parameter, part or custom property changes without a new version |
| E2 | The fragments: product card, fleet map, telemetry, latest commits, repository health; the dashboard built from them | the dashboard renders through the fragments with its e2e and axe gates unchanged; a fragment fetched alone renders the same content in a blank page with no script |
| E3 | The behaviour module: the custom elements, hints, keyboard, live updates, the theming tokens | a static host page with two fragments and the module themes them through `--scp-*`, updates both on one push, and passes axe; the module stays within its budget |
| E4 | The host side: outbound snapshot webhooks, `@scp/client`, a reference host (a small server-rendered page in `web/e2e`) composing fragments and revalidating on the webhook | the reference host shows a push within one snapshot, through the webhook, without polling; the leak test covers `/fragments/` |

## Tests

- Contract: fragment parameters, parts and custom properties checked against the contract file; `v1` additions only.
- Rendering: each fragment's HTML has one root element, a declarative shadow root, no inline script or style, and the same text as
  the dashboard's element for the same data.
- Browser (Playwright): a static host page in both themes and at phone width, with and without the module; axe; live update on a
  push through the e2e control endpoint.
- Security: the leak test over `/fragments/` as the public; CORS only for `embed.origins`; webhook signatures verified by the
  reference host.
