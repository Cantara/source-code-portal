# Images and live updates
Status: v1 design note (M10), 2026-10-14. Binding order: ../plan.md > this note > ../reference/.

## Scope
- Image proxy: every image in a rendered document loads from the portal, `GET /images/{owner}/{name}/{id}`, so the shell's CSP is
  `img-src 'self'`. Repository files come from GitHub by blob SHA through the source's client (private repositories work for the viewers
  allowed to see them); external `https` images through a guarded client.
- Live updates: `GET /api/v1/events` is a server-sent event stream that announces every new snapshot; the web UI listens and stops
  polling while it is connected.
- The snapshot version is derived from its content, so every instance of a cluster gives the same snapshot the same version.

## Decisions
- The docs pipeline rewrites each `img src` to `/images/{owner}/{name}/{id}` and records the image in the document: a repository path
  (resolved against the document like links) or an absolute `https` URL. `id` is the first 16 bytes of SHA-256 over `path:<path>` or
  the URL, hex. The handler serves only images a document of the repository records, so it is no open proxy, and the viewer's snapshot
  view decides access: a private repository's images are 404 to a public viewer.
- The docs job stores each repository image's blob SHA at `HEAD` with the document on every run, so a changed image shows without a
  changed document. It looks images up in the trees it reads anyway (the root, `.github/`, `docs/` recursively) and, for images
  elsewhere, in the recursive tree of their top-level directory (at most 5 per run). Images larger than 5 MiB get no blob and answer 404.
- The rendered HTML depends on the image paths, not on their blobs, so the stored HTML stays valid while images change.
  `pipelineVersion` is 2.
- External fetches: `https` on port 443 only, no proxy from the environment, at most 3 redirects (each again `https`), the dial refuses
  addresses that are not public unicast (loopback, private, link-local, CGNAT, multicast, unspecified, and their IPv4-mapped forms), so
  DNS rebinding and redirects cannot reach the portal's network. 10 s per fetch, 5 MiB per image.
- Content types: sniffed (`http.DetectContentType`) and kept when PNG, JPEG, GIF, WebP, BMP, ICO or AVIF; SVG when the path ends in
  `.svg` or the upstream says `image/svg+xml`, and the first 4 KiB hold `<svg`. Anything else, an upstream 404 and a file without a
  blob answer 404; a failed fetch answers 502. Responses carry
  `X-Content-Type-Options: nosniff` and the CSP `default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'; sandbox`, so
  an SVG opened directly runs nothing.
- One in-memory cache per instance, least recently used out first, 64 MiB in total; concurrent requests for one image share one fetch
  (`singleflight`). Repository images are keyed by blob SHA and kept until evicted; external images for 1 h; a failed fetch is
  remembered for 5 min. Responses: `Cache-Control: public, max-age=3600` (private for a private viewer, as every response), a strong
  ETag (the blob SHA, or SHA-256 of the bytes), 304 on `If-None-Match`.
- Image fetches are made by the instance that serves the request, also a follower in a cluster: they read files, not job outcomes, and
  the cache bounds them.
- Events: `GET /api/v1/events` (`text/event-stream`, in the contract) sends `event: snapshot` with the `SnapshotInfo` of the viewer's
  view at connect and after every publish (a burst of publishes gives one event with the latest), a comment every 25 s, `retry: 5000`.
  The stream is exempt from the 30 s request deadline, the API's buffering for ETags and the server's write timeout (a write deadline
  of 30 s per event instead); `Cache-Control: no-store` and `X-Accel-Buffering: no`. At most 1,000 streams per instance (503 beyond);
  streams end when the server shuts down. Each instance announces its own snapshots.
- The snapshot version: the sum of a 64-bit FNV-1a digest per published item (key, state, error, content SHA, checked time in seconds,
  payload), kept by the builder as items are published and dropped, masked to 53 bits for JavaScript. Equal items give equal
  versions on every instance and after a restart; it identifies a snapshot and is compared for equality only.
- Web: the shell opens an `EventSource` on `/api/v1/events`; an event replaces the snapshot query's data, which invalidates the other
  queries as a polled version change did. While the stream is open the snapshot is not polled; on an error the browser reconnects and
  the 30 s polling runs until it does.

## Shapes
```go
// internal/snapshot
type Image struct{ ID, Path, URL, BlobSHA string } // Doc.Images; Path for repository files, URL for external images
func (b *Builder) Mark(key string, digest uint64)  // the item's digest; 0 drops it
// internal/sync: digest(store.Item) uint64 over the item fields, marked at every publish

// internal/docs
func (d *Docs) Images(current func(context.Context) *snapshot.Snapshot, external http.RoundTripper) http.Handler
func Guarded() http.RoundTripper // the external transport

// internal/httpapi
type Events struct{ ... }
func NewEvents(current func(context.Context) *snapshot.Snapshot, published func() <-chan struct{}) *Events
func (e *Events) Route() Route // GET /api/v1/events, Stream
func (e *Events) Close()        // ends the streams; registered with the server's shutdown
```

## Tests
- Docs: relative and external images are rewritten and recorded, the blob SHA follows a changed image without a re-render, an image
  in another top-level directory is found, `TestPipelineLock` pins version 2; the sanitiser admits only the proxy's `src`.
- Images handler over the fake GitHub and a loopback image server (through a test transport): a repository image by blob, an external
  one, an unknown id and a public viewer on a private repository are 404, a non-image is 502, ETag and 304, one fetch for concurrent
  requests, the cache bound, failures remembered, a redirect to `http` refused.
- Guard: the dial refuses loopback, private, link-local, CGNAT, mapped and unspecified addresses; `http` and non-443 URLs and
  redirects to them are refused.
- Events: the first event at connect, one event after a publish, a heartbeat, the stream cap, the end at shutdown, no gzip.
- Snapshot version: equal items in any order give equal versions; a changed or dropped item changes it; the cluster test compares
  `/api/v1/snapshot` on both instances.
- Playwright: a README image loads from `/images/` under `img-src 'self'`; a push made while the repository page is open shows its
  commit within a few seconds, well inside the 30 s polling interval, which is off while the stream is open.
