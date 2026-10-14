# ADR-006: Documentation format (CommonMark + GFM profile) and the `scp.yaml` descriptor

Status: Accepted. Date: 2026-10-14. Refines 04 D-06 and Q-06/Q-20 (A-06).

## Context
Owner directive: AsciiDoc is outdated; prefer an LLM-compliant format (Markdown/YAML semantics). Markdown lacks structured semantics (owner, lifecycle, links, doc ordering, integrations). Backstage `catalog-info.yaml` is `v1alpha1`, requires `spec.owner` and has no place for navigation.

## Decision
- Source of truth: CommonMark 0.31.2 + GFM (tables, task lists, strikethrough, autolinks), footnotes, GitHub alerts, Mermaid fences, collapsible `<details>`; README without front matter; `docs/**/*.md` with at most five portal keys (`title, description, order, tags, hidden`).
- Descriptor: `scp.yaml` at the repository root or `.github/scp.yaml` (`.github/` wins across locations; `.yaml` and `.yml` in one location is an error). Schema v1 (JSON Schema 2020-12, embedded, served at `/schemas/scp/v1.json`): `schema, title, summary, type, lifecycle (experimental|production|deprecated), owners (CODEOWNERS syntax), system, tags, links, docs {root, include, exclude, nav}, apis, dependsOn, integrations {jenkins, snyk}, hidden`.
- Lenient, field-granular loader: a violation drops only that field and records `problems[]`; hard rules: 64 KiB, single YAML document, anchors rejected by a node walk, `https` links only, `..` rejected, integrations may only name instances defined in portal config, owners are claims with provenance, `hidden`/`system` never change access control or membership.
- Fallback inputs with provenance: Backstage `catalog-info.yaml`, CODEOWNERS, GitHub topics; zero-config repositories are valid.
- AsciiDoc is supported through a bridge only: GitHub `html+json` rendering, sanitised, marked `bridged`, no engine and no subprocess.
- The lifecycle vocabulary is Backstage's (`experimental|production|deprecated`), not 04's `active`; the UI may label `production` as "Active".
- Q-20 is read as "no sub-project (monorepo) docs", not "no `docs/` directory": `docs.root` defaults to `docs`.

## Alternatives considered
Adopt Backstage entities as the descriptor (alpha, owner required, no nav); AsciiDoc-first (outdated); MDX/Docusaurus (a build step per repository); front matter only (no repo-level metadata).

## Consequences
Authors get a documented authoring contract (`docs/authoring.md`) and `scp descriptor lint`; the descriptor is repo-authored hence attacker-influenced, so every field is validated and nothing in it grants access. Owner confirmation needed on the lifecycle word and the Q-20 reading (risk W18).

## Evidence
`05-research/01-docs-format-llm.md` sections 2.2-2.3, 3, 4.1-4.3, 5, 7.
