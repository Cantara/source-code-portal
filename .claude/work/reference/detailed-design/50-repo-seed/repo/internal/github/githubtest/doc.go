// Package githubtest is the fake GitHub server with fault injection, used by tests, end-to-end runs and local development.
// Imported from _test.go files and cmd/fakegithub only.
//
// Architecture: rank 1 (internal/github/githubtest); the closed-world row is in internal/archtest/rules.go and is the only
// authority.
// Allowed internal imports: internal/scm.
// Third-party libraries owned here: none.
//
// Design: .claude/work/plans/20-design/05-github-adapter.md; architecture sections 4 and 5.2.
package githubtest
