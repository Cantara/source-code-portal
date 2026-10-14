// Package access is the optional trusted-proxy identity: identity headers are honored only from trusted peers; implements
// httpapi.Authorizer.
//
// Architecture: rank 4 (internal/access); the closed-world row is in internal/archtest/rules.go and is the only authority.
// Allowed internal imports: internal/scm, internal/httpapi, internal/httpapi/authz, internal/platform/logx,
// internal/platform/secret.
// Third-party libraries owned here: none.
// Note: platform/secret added: 14-integrations-access deviation D-1.
//
// Design: docs/design/20-design/14-integrations-access.md; architecture sections 4 and 5.2.
package access
