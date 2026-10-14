// Package auth provides the TokenSource strategies: GitHub App (own JWT encoder over crypto.Signer, installation tokens), PAT,
// OAuth user token, external command and anonymous.
//
// Architecture: rank 1 (internal/github/auth); the closed-world row is in internal/archtest/rules.go and is the only authority.
// Allowed internal imports: internal/scm, internal/platform/secret.
// Third-party libraries owned here: x/sync/singleflight.
//
// Design: .claude/work/plans/20-design/05-github-adapter.md; architecture sections 4 and 5.2.
package auth
