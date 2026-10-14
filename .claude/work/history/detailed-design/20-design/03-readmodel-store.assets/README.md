# Reference prototype for design unit 03 (readmodel and store)

Throw-away, compile-checked and tested reference code for `20-design/03-readmodel-store.md`. It is NOT the shipped code: the
implementation copies it into `internal/` of the real module and adds what the design lists as missing. Since the 2026-10-14
verification `internal/scm`, `internal/freshness` and `internal/job` are the REAL kernel packages copied (non-test files only)
from `01-kernel.assets`; the earlier hand-written stubs hid defect V-01.

    go vet ./... && go test -count=1 -race ./...          # about 25 s
    go test -run xxx -bench . -benchtime 300x ./internal/store/sqlite ./internal/readmodel
    go test -run xxx -fuzz FuzzDSN -fuzztime 8s ./internal/store/sqlite

Verified 2026-10-14 with go1.27.1 darwin/arm64, modernc.org/sqlite v1.60.1 (SQLite 3.53.4), github.com/ncruces/go-sqlite3 v0.35.6,
go.opentelemetry.io/otel{,/metric,/trace} v1.47.0.

Verification re-run 2026-10-14 (go1.27.1 darwin/arm64, real kernel packages): `go vet ./...` clean; `go test -count=1 -race ./...` green
(`store/sqlite` about 80 s: two drivers x 46 conformance subtests). Tests that need both Store implementations, the real `job` package or
the YAML decoder live in `cmd/scp` because of the import matrix (design section 2.1). New dependency for those tests: go.yaml.in/yaml/v3 v3.0.5.
