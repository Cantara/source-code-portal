package main

// The three generated reference files of the configuration (02 section 4.3.8, design 16 section 4.6). `make gen` runs them and
// `make gen-check` fails the pull request when the committed copies differ (GR-06: the documentation cannot drift from the code).
// The commands run the placeholder build of this package (no spa tag, no Node needed); sh -c carries the redirection, which
// the generate directive does not offer.
//
//go:generate sh -c "go run . config schema > ../../docs/reference/config.schema.json"
//go:generate sh -c "go run . config reference > ../../docs/reference/config.md"
//go:generate sh -c "go run . config env > ../../docs/reference/env.txt"
