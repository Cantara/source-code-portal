# api/

The contract lives here, spec first (ADR-020, design 07). Today both roots are placeholders with `paths: {}` so that the toolchain
(`make gen`, `make api-check`, `pnpm api:lint`) runs from the first commit.

| File | State | Replaced by |
|---|---|---|
| `openapi.yaml` | placeholder public root | design 07 task T-1: root that `$ref`s the path items of every `<feature>api` fragment |
| `openapi.admin.yaml` | placeholder admin root (superset: public plus `getStatus`, `retryJob`) | design 07 task T-1 |
| `oasdiff-severity.txt` | real: severity levels for `oasdiff breaking` (Makefile `api-check`) | (kept) |

The real contract is a design deliverable, mirrored by `bootstrap.sh` under `docs/design/20-design/api/` (`common.yaml`, `openapi-public.yaml`,
`openapi-admin.yaml`, `trusted-members.txt`) together with the endpoint catalogue `docs/design/20-design/07-openapi-contract-catalogue.md`.
Task T-1 turns those three single files into the repository layout once (`docs/design/20-design/07-openapi-contract.assets/tools/split.py`),
then generates `internal/httpapi/openapi`, the eight `<feature>api` packages and the two bundles in `internal/apispec/`
(`openapi.bundled.yaml`, `openapi.admin.bundled.yaml`, committed and drift-checked by `pnpm api:bundle-check`).

Rules: OpenAPI 3.1 subset only (W1); every generated file is committed; never hand-edit `*.gen.go`, `schema.d.ts` or a bundle.
