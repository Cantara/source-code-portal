// Package jenkins is the Jenkins StatusProvider.
//
// Architecture: rank 5 (internal/integrations/jenkins); the closed-world row is in internal/archtest/rules.go and is the only
// authority.
// Allowed internal imports: internal/integrations, internal/scm, internal/freshness, internal/platform/secret.
// Third-party libraries owned here: none.
//
// Design: docs/design/20-design/14-integrations-access.md; architecture sections 4 and 5.2.
package jenkins
