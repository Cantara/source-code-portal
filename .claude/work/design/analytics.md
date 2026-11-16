# Usage analytics with Plausible (AN1)

Status: built (plan.md section 4, P-113). Binding order: ../plan.md > this note.

## Goal

A deployment that uses Plausible Analytics (its service or a self-hosted one) can count the visits to its portal by setting one key.
Nothing is counted without it. The tracker is the npm package `@plausible-analytics/tracker`, part of the portal's own scripts, and
the browser only ever talks to the portal: no third-party script to load, no other origin in the pages' policy, and no filter list
with an address to block.

## Configuration

```yaml
web:
  plausible_domain: scp.example.org                       # the site as Plausible knows it; empty (the default): no tracker
  plausible_endpoint: https://plausible.io/api/event      # optional: a self-hosted Plausible's event address
```

`plausible_domain` is a host name, optionally with a path. `plausible_endpoint` is an absolute `https` URL without credentials, query
or fragment (`http` only on loopback); it needs `plausible_domain`. The environment variables are `SCP_WEB_PLAUSIBLE_DOMAIN` and
`SCP_WEB_PLAUSIBLE_ENDPOINT`.

## How it works

- **The tracker is in the bundle.** The root layout renders the `Tracker` (web/src/shared/analytics.tsx) when `SCP_ANALYTICS` holds a
  site; it loads the package as a chunk of its own, so a portal without the key loads nothing. `init` runs once (it throws when
  called twice, and React runs an effect twice in development).
- **Events go to the portal.** The tracker posts to `/_v/m` (`analytics.Path`), a name with none of the words filter lists match
  (plausible, analytics, stats, track, event). `internal/platform/analytics` forwards each event to the endpoint, built afresh: the visitor's
  browser (`User-Agent`), their address (`X-Forwarded-For`, from the trusted proxies' client address; Plausible counts a visitor by
  both and drops an event without the address as a bot) and the media type, never a cookie, token, referrer or origin of the portal.
  A request another site made (`Sec-Fetch-Site: cross-site`), one with no body or more than 8 KiB, and one past 16 in flight are
  refused; an endpoint that does not take the event is a 502; the answer carries nothing else.
- **`/_v/m` is open** (it answers before sign-in under `access.require_auth`, as the sign-in page is counted too) and reserved.
- **Privacy.** A page rendered for a viewer who sees private repositories is never tracked: its addresses carry those repositories'
  names (`privateViewer()`). A page's address goes without its query but the campaign's (`utm_*`, `ref`, `source`, `via`): a search or
  a login in a filter stays with the portal. The presentation page, which reloads itself on a wall, is not counted.
- **Plausible's own tools.** `bindToWindow` stays on, so the installation check of Plausible's site finds the tracker; the visitor's
  own opt-out (`localStorage.plausible_ignore = "true"`) works as with its script.

## Acceptance

1. Without `web.plausible_domain` the web UI server gets no `SCP_ANALYTICS`, `POST /_v/m` is not served and no page loads the tracker.
2. With it, an event posted to `/_v/m` reaches the endpoint with the visitor's browser and address and without any cookie,
   authorisation, referrer or origin, also under `access.require_auth` (`cmd/scp/analytics_test.go`, `internal/platform/analytics`).
3. The page policy is the same with or without it (the connection goes to the portal).
4. A viewer who sees private repositories is not tracked (`layout.tsx`); a query is not sent (`lib/analytics.test.ts`).
5. `scp config check` refuses a domain that is no host name, an endpoint over http (but loopback), with credentials or a query, and an
   endpoint without a domain.

## Not decided here

Custom events and goals (`track`), outbound links, file downloads and form submissions are off: one key counts visits, and each
more would be a key with a privacy cost. A portal that wants them asks.
