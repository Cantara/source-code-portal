# Changelog

Notable changes, newest first. Versions follow [Semantic Versioning](https://semver.org/); the GitHub release of each tag lists every
change from its Conventional Commit subjects.

## 2.1.0

The completed rewrite: package manifests, private repositories, several instances, live updates and the server-rendered web UI.

- Package manifests: `go.mod`, `package.json` and `pom.xml` are read per repository; the repository page shows the technology and
  dependencies they declare and the catalogued repositories it uses and is used by (`/api/v1/repos/{owner}/{name}/manifests`, and in the
  repository's Markdown).
- Private and internal repositories (`include_private`), shown only to the groups in `access.private_groups` as named by a trusted
  authenticating proxy; everyone else sees the public repositories on every surface.
- Several instances serve one portal through Hazelcast (`cluster.addresses`): one leads and alone calls GitHub, every instance serves
  the same data, webhook deliveries are de-duplicated across instances and reach the leader from any of them, and another instance takes
  over when the leader stops.
- Images in documents load from the portal (`/images/...`), relative and external ones alike, also for private repositories; the web
  UI's CSP is `img-src 'self'`.
- Live updates: `/api/v1/events` streams server-sent events, and an open page shows new data within seconds without polling. The
  snapshot `version` now identifies the data and is equal on every instance.
- The web UI is rendered on the server with Next.js: every page arrives as complete HTML, readable without JavaScript, with its own
  title, description and canonical URL, and `/sitemap.xml` and `/robots.txt` list the pages. `scp serve` runs the web UI bundle with
  Node.js (`web.dir`, `web.node`); releases carry it as `scp-web_<version>.tar.gz`, and the image includes Node.js and the bundle.

## 2.0.0

The first release of the Go rewrite (2.0.0 follows the Java product's 1.x line): one static binary and container image serving an organisational dashboard for GitHub.

- Catalogue of the public repositories of one or more organisations (GitHub App or token, github.com or Enterprise Server), grouped by
  configured matchers, with renames and transfers followed.
- Pages: dashboard, group, repository (descriptor, badges, README, documents, commits), documentation viewer and commit logs, in light
  and dark themes, accessible (WCAG 2.2 AA).
- README and `docs/` rendering with highlighting and GitHub alerts through one sanitiser; the optional `scp.yaml` descriptor.
- Freshness from webhooks within seconds and scheduled sync otherwise; the last good data is served through GitHub outages.
- JSON API `/api/v1` (OpenAPI 3.1), SVG badges, `/llms.txt`, `/llms-full.txt` and Markdown answers for agents.
- Operations: separate admin listener with probes, Prometheus metrics and a status page; signed images for linux/amd64 and linux/arm64.
