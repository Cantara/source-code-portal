# ADR-007: LLM surface: `llms.txt` family, raw Markdown, negotiation and structured JSON

Status: Accepted. Date: 2026-10-14. Refines 04 D-06 (A-06).

## Context
D-06/D-16 require an LLM-friendly, server-generated surface that does not depend on the SPA. The llms.txt proposal (v2, modified 2026-08-10) defines an H1, a blockquote, headingless prose and H2 link lists with an `Optional` section; `llms-full.txt` is a convention. Hidden HTML-comment prompt injection in repositories is documented.

## Decision
- `GET /llms.txt` per spec v2 with scoped files at `/groups/{id}/llms.txt` and `/repos/{repo}/llms.txt` (+ `llms-full.txt` variants); the root `llms-full.txt` is capped (2 MiB) with a truncation marker; all URLs absolute from `server.public_url`.
- Raw Markdown per document (`/repos/{repo}/readme.md`, `/repos/{repo}/docs/{path}.md`, `agents.md`, `changelog.md`) with portal metadata under the `scp:` front-matter namespace; HTML comments and invisible/control/bidi characters are removed from LLM-facing output; `ETag "<blob_sha>-<pipeline_version>"`; `Link rel=canonical`.
- `Accept: text/markdown` on human URLs serves the same body (`httpapi.Negotiate`, `Vary: Accept`); HTML responses carry `Link rel=describedby` to `/llms.txt`.
- Structured JSON under `/api/v1` with provenance on every resolved field and `/api/v1/openapi.yaml`; dependencies as `dependencies.md` with `trust: untrusted-data` and names/versions only (ADR-022).
- Hidden documents are excluded everywhere (not an access control). Repository-authored text is flagged as untrusted data in the prose.

## Alternatives considered
SPA-only (invisible to agents); an MCP server (natural follow-up, not researched); automatic AsciiDoc-to-Markdown conversion (lossy).

## Consequences
One generation path (`overview` + `docs` + `manifests`) must stay deterministic and cheap: ordering fixed, ETag over the body, `max-age=300`, never 5xx (serve last good). The `.md` variants add reserved routes that must never fall back to the SPA.

## Evidence
`05-research/01-docs-format-llm.md` sections 2.6, 6; `05-research/07-manifest-parsing.md` section 9.3.
