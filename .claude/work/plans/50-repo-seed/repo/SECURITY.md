# Security policy

## Reporting a vulnerability

Do not open a public issue. Use GitHub's private vulnerability reporting for this repository (Security tab, "Report a vulnerability").
Include the version (`scp version`), the configuration section involved (redact secrets) and a reproduction. You will get an
acknowledgement within five working days.

## Supported versions

The latest released minor version receives fixes. Release archives are signed (cosign, keyless) and carry SBOMs; verification steps are in
the release notes (design 16 section 4.9.4).

## Scope and posture

Trust boundaries, threats and the structural controls (SSRF-guarded outbound clients, `safehtml.SafeHTML`, visibility enforcement, webhook
signature verification, static CSP, read-only root filesystem and non-root image) are described in `docs/design/10-architecture.md`
section 17 and ADR-025.
