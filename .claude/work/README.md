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
| `design/commits-navigator.md` | The commit navigator: the stored history, product timelines and the git graph navigator. |
| `design/auth.md` | S1, built: sign-in through OIDC, bearer tokens, sealed sessions, the admin role. |
| `design/assistant.md` | A1, in progress: LLM providers and the read-only assistant. |
| `design/seo.md` | M13, built: search engines. |
| `design/hardening.md` | H1, built: a hardened private instance (issue #5). |
| `design/agents.md` | W1 to W3, built: one tool catalogue for the assistant, MCP (`/mcp`) and WebMCP. |
| `design/guide.md` | G1, built: the Getting started page and its screenshots, the operators' Claude Code plugin. |
| `design/layout.md` | L1 to L4, built: the landing page of signals, layouts and the design mode, AI-assisted commits, the work that counts and Settings. |
| `design/packages.md` | R1 to R4, R1 in progress: published packages, one provider per package manager (Maven, npm, PyPI, Cargo, Go, Swift, NuGet, RubyGems, Packagist), the registries a build file names, configured private registries with their credentials, structured concurrency per job. |
| `design/embed.md` | E1 to E4, dequeued until the owner returns to them: the portal embedded in other sites as versioned HTML fragments composed on the host's server, themed by custom properties, live through custom elements; outbound snapshot webhooks; a typed client. |
| `history/` | Finished work: the M0..M11 plan and decision log, the earlier detailed design, the UI brief and screenshots. Not kept in sync. |

New decisions go into `decisions.md` and the note they change, in the same commit. When a milestone is done its note is folded into
the area notes and the milestone row leaves `plan.md`.
