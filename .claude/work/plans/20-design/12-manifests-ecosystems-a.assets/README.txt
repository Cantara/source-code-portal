Assets of design unit 12-manifests-ecosystems-a (Go, Maven, Gradle, npm, Python, Cargo parsers and assemblers). NON-NORMATIVE: the design document
12-manifests-ecosystems-a.md wins on any difference. Built and tested on go1.27.1 darwin/arm64 on 2026-10-14.

overlay/         files added to the module of 11-manifests-core.assets (copy over it):
  internal/manifests/ecosystems/{golang,npm,python,maven,gradle,cargo}   the six packages with their tests, goldens (testdata/<case>/{input,expected.golden.json})
                                                                         and fuzz regression seeds (testdata/fuzz/FuzzParsers)
  internal/manifests/ecosystems/integration                             the end to end test: golden trees through the REAL manifests.refresh job
  internal/manifests/internal/{tomlx,expand,redact,linex,repopath}       five new rank-1 helper packages (design section 3.4)
  internal/manifests/tree.go                                             RunTree, the pure multi-file harness that mirrors the runner (requirement RR-A2)
  internal/manifests/parsertest/tree.go                                  TreeGolden, CheckTree, FuzzTree, CheckParse (requirement RR-A4)
patches/         core-rr-a.diff   model.go sanitize.go refresh.go (requirements RR-A1, RR-A3, RR-A5, RR-A6, RR-A7); go-mod.diff (go-toml/v2, x/mod)
oracle/          SOURCES.tsv (every real file with repository, git blob id, licence), fetch-oracle.sh (downloads and verifies the non-committed corpus)
tools/           corpus-main.go.txt  the timing and count program of design appendix B (copy to a directory of the module as main.go to run it)
apply.sh         builds the scratch module from the three parts (core, overlay, two patch files) and runs gofmt, go vet, the purity guard (no goroutine, init, clock, os, net in the ecosystem packages) and go test -race (about 40 s)

Fuzz (per package, 45 s was used): cd internal/manifests/ecosystems/<eco> && go test -run xxx -fuzz FuzzParsers -fuzztime 45s .
Update goldens after a reviewed change:  go test ./internal/manifests/ecosystems/<eco> -update-tree
Run the oracle program: sh oracle/fetch-oracle.sh $TMPDIR/oracle-root && cp tools/corpus-main.go.txt <module>/tmpcorpus/main.go && go run ./tmpcorpus $TMPDIR/oracle-root
  (the program expects the layout t/npm/..., t/py/..., t/rg, t/k8s, gradle/<name>, maven/pom.xml of SOURCES.tsv; adjust the paths at the top of main)
