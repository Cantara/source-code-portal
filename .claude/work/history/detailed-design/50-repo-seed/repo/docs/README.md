# docs/

User documentation of Source Code Portal: for operators who run it and for authors whose repositories it shows. Planning and design
material never lives here; the design package, the ADRs and the planning notes are under `.claude/work/` (start with
`.claude/work/plans/README.md`).

| Path | What |
|---|---|
| `reference/` | Generated reference (configuration keys, JSON Schema, environment variables, metrics). Created by `go generate ./cmd/scp` and the metrics reference test once those exist; never edited by hand. |
| `examples/` | Shipped configuration examples, linted `--strict` in CI (`make config-lint`). Created with unit 02 / task B-4. |
| `operations/`, `authoring.md` | Runbooks and the guide for repository authors (descriptor, front matter). Written with the units that own them (design 14, 10, 16). |
