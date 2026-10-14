# SCP development package

Everything needed to build Source Code Portal v1 in Go. Binding order: `plan.md` > `design/` > `decisions.md`.

| File | What it holds |
|---|---|
| `plan.md` | What SCP is, scope (v1, later), architecture, stack, milestones M0..M11. Read first. |
| `decisions.md` | Every decision, dated, with where it is recorded. |
| `design/principles.md` | The 18 principles every change keeps, each with its check. |
| `design/build-ops.md` | Repository layout, Cobra CLI, Viper config, runtime, Makefile, CI, image, release, test strategy. M0. |
| `design/github.md` | GitHub App auth, the REST calls v1 uses, ETags, rate limits, discovery, webhook events, the fake GitHub. M1. |
| `design/sync-snapshot-store.md` | Scheduler, job queue, freshness, the immutable snapshot, SQLite store, readiness, shutdown. M1. |
| `design/catalog.md` | Sources, repository identity, groups and matchers, the `scp.yaml` descriptor. M1. |
| `design/http-api.md` | Listeners, routes, OpenAPI contract, middleware, security headers, webhooks receiver, `llms.txt`, metrics. M1..M4. |
| `design/web.md` | The React SPA: technical frame (structure, routes, data access, build and embed, tests); visual design from Claude Design. M0, M5. |
| `design/content.md` | Docs rendering and sanitising, commits, SVG badges. M2..M3. |
| `design/manifests.md` | `go.mod`, `package.json`, `pom.xml`: technology, dependencies, internal dependencies. M7. |
| `design/access.md` | Private repositories behind a trusted authenticating proxy: the viewer, the public view. M8. |
| `design/cluster.md` | Several instances on Hazelcast: the leader, the shared state, webhook de-duplication. M9. |
| `design/images-events.md` | The image proxy for document images, server-sent events, the content-derived snapshot version. M10. |
| `reference/detailed-design/` | The earlier detailed design (archive). Background material, consulted per note's Reference section. |

Work one milestone at a time: read `plan.md` section 5, then the notes tagged with that milestone. New decisions go into
`decisions.md` and the note they change, in the same commit.
