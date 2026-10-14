# Assets for 05-store-sync-engine.md

Research prototypes and measurement harnesses. NOT production code and not part of the future Go repo.
Run each directory on its own (`go test -race ./...`; Go >= 1.27 for `synctest.Sleep`, >= 1.26 for the drivers).

- engine/            keyed coalescing queue + per-upstream limits + retries + rate-limit pause + scheduler, tested with testing/synctest
- readmodel/         feature-agnostic typed-slot snapshot kernel with atomic publication and change notification
- snapshot-bench/    memory and CPU of rebuild vs copy-on-write at 1,000 repos (edit nRepos to 10000 for the scaling run)
- sqlite-bench/      modernc vs ncruces vs bbolt measurements, schema (ddl/0001_init.sql), statements, binary-size mains (cmd/)
- sqlite-in-synctest/ shows real SQLite I/O inside a synctest bubble
