# SCP development package

What Source Code Portal is, how it is built, and the work in progress. Binding order: `plan.md` > `design/` > `decisions.md`.

| File | What it holds |
|---|---|
| `plan.md` | What SCP is, architecture and rules, stack, the work in progress and queued. Read first. |
| `decisions.md` | The decisions in force. |
| `design/principles.md` | The principles every change keeps, each with its check. |
| `design/build-ops.md` | Layout, CLI, config, runtime, Makefile, CI, image, release, tests. |
| `design/github.md` | GitHub App and token auth, the REST calls, ETags, rate limits, discovery, webhook events, the fake GitHub. |
| `design/sync-snapshot-store.md` | Job engine, freshness, the immutable snapshot, SQLite store, readiness, shutdown. |
| `design/catalog.md` | Sources, repository identity, groups and matchers, the `scp.yaml` descriptor. |
| `design/http-api.md` | Listeners, routes, the OpenAPI contract, middleware, security headers, the webhook receiver, `llms.txt`, metrics. |
| `design/content.md` | Document rendering and sanitising, commits, SVG badges. |
| `design/manifests.md` | `go.mod`, `package.json`, `pom.xml`: technology and dependencies. |
| `design/access.md` | Private repositories behind a trusted authenticating proxy. |
| `design/cluster.md` | Several instances on Hazelcast. |
| `design/images-events.md` | The image proxy, server-sent events, the content-derived snapshot version. |
| `design/web.md` | The Next.js web UI. |
| `design/health.md` | Repository and product health: the score, its findings, the weights. |
| `design/commits-navigator.md` | C1, next: the product timeline and the git graph navigator. |
| `design/seo.md` | M13, queued: search engines. |
| `history/` | Finished work: the M0..M11 plan and decision log, the earlier detailed design, the UI brief and screenshots. Not kept in sync. |

New decisions go into `decisions.md` and the note they change, in the same commit. When a milestone is done its note is folded into
the area notes and the milestone row leaves `plan.md`.
