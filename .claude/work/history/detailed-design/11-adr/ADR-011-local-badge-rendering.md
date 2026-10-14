# ADR-011: Local badge rendering (flat-square SVG, generated width table)

Status: Accepted. Date: 2026-10-14. Implements 04 Q-05.

## Context
Shields.io is not called (Q-05); README embedders (including GitHub's Camo proxy) fetch `/badges/...`. Proxying third-party SVG leaks requests and trusts foreign markup, and string-replacing into XML breaks on attacker-influenced text, so badges are rendered locally with structural XML escaping (GR-33). Shields' widths depend on Verdana (proprietary).

## Decision
- Own renderer in `internal/badges` (about 120 lines): flat-square style only (no gradients, filters or ids), height 20, `role="img"`, `aria-label` and `<title>`, text via `text/template` with an XML escaper that drops characters illegal in XML 1.0; label from the badge-kind enum, message from catalogue data, length-capped with an ellipsis; colours from a closed enum plus validated `#rrggbb`, all pairs at least 4.5:1.
- Text width table generated at `go generate` time from DejaVu Sans 2.37.3 (within about +/-3% of Verdana; Bitstream Vera licence) into `widths_gen.go` (`[95]uint16` advances plus an average); no font file or `x/image` at runtime; a golden test pins about 30 strings.
- Endpoint `GET /badges/{repo}/{file}` (`file` = kind + `.svg`; `{kind}.svg` is an illegal ServeMux wildcard); kinds `issues`, `last-commit`, `release`, `licence`, `repos` (group form), `language`, `build-tool`, `latest-version`, `internal-deps`, and optional `build`/`security` from integrations. Headers: `image/svg+xml`, `nosniff`, `Content-Security-Policy: default-src 'none'; sandbox`, `Cross-Origin-Resource-Policy: cross-origin`, ETag from the repo generation, `Cache-Control: public, max-age=300`.
- Unknown repo/kind: 404 with a small grey "unknown" badge body, `max-age=60`; known repo with unavailable data: 200 grey "unknown", `max-age=60`.

## Alternatives considered
`narqo/go-badge` (init side effects, stale deps), `essentialkaos/go-badge` (needs Verdana at runtime), `factorysh/go-badge` (dead), `adequatedq/badge` (GPL); embedding anafanafo's Verdana table (licensing unresolved).

## Consequences
Widths differ from Shields by up to about 2 px per text run; `textLength` keeps the drawn text inside its box. Badges read facts through the `badges.Facts` bridge, so `badges` imports no other feature.

## Evidence
`05-research/06-web-ui-badges-security.md` section 7 (experiments E2, E7).
