# docs/

| Path | What |
|---|---|
| `design/` | The design of the product, mirrored from the design workspace by `bootstrap.sh`; binding. Start with `design/README.md`. |
| `adr/` | Architecture decision records written in this repository. ADR-001 to ADR-025 are the design-phase records in `design/11-adr/`. |
| `reference/` | Generated reference (configuration keys, JSON Schema, environment variables, metrics). Created by `go generate ./cmd/scp` and the metrics reference test once those exist; never edited by hand. |
| `examples/` | Shipped configuration examples, linted `--strict` in CI (`make config-lint`). Created with unit 02 / task B-4. |
| `operations/`, `authoring.md` | Runbooks and the guide for repository authors (descriptor, front matter). Written with the units that own them (design 14, 10, 16). |
