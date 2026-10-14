# Changelog

Notable changes, newest first. Versions follow [Semantic Versioning](https://semver.org/); the GitHub release of each tag lists every
change from its Conventional Commit subjects.

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
