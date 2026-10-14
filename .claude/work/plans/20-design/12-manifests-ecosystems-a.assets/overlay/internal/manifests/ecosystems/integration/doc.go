// Package integration holds the end-to-end test of unit 12: every ecosystem of the unit registered in one real manifests.Registry, a fake GitHub
// (tree and blob readers over the golden inputs), and the real manifests.refresh job. The test compares the units the job publishes with the
// units of manifests.RunTree (the pure harness the golden tests use), so the harness cannot drift from the runner.
package integration
