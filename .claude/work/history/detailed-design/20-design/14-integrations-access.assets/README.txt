Companion to ../14-integrations-access.md. Reference implementation of unit 14 (integrations + access), compile-checked and
tested on 2026-10-14 with Go 1.27.1 (go vet clean, go test -race clean, fuzz 10 s per target).

REAL (the design's code; copy into the new repository under internal/):
  internal/access/                 identity, authorizer, middleware, config (+ tests)
  internal/integrations/           config, target resolution, status model, service/sweep (+ tests, testdata/)
  internal/integrations/jenkins/   Jenkins JSON API provider (+ tests)
  internal/integrations/snyk/      Snyk REST provider (+ tests)

STUBS (minimal stand-ins copied from 10-architecture.md so the module compiles; DO NOT copy, the owning units define the real ones):
  internal/scm, internal/freshness, internal/job, internal/readmodel, internal/store,
  internal/platform/secret, internal/httpapi/authz
  Note: authz.WithPrincipal is NOT in the architecture text; it is proposed deviation D-2 of the design.
  Note: scm.NewMembership and readmodel.Register(..., existence bool) are scratch conveniences, not architecture signatures.

Fixtures under internal/integrations/testdata are hand-written from the API schemas (Jenkins tree= output, Snyk OpenAPI
2024-10-15), not recorded; milestone M0 of the design re-records them against real instances.

Run: go vet ./... && go test -race ./...   (needs network once for go.opentelemetry.io/otel v1.47.0)

Verification 2026-10-14 (independent verifier): the REAL packages were corrected (per-job 403, partial instance abort, healthy-sweep rule,
count saturation, ASCII subject folding, untrusted warm-start payload, sampled spoof log, defaults applied by Validate/New, Tone.String()) and
extended with 15 tests (integrations/verify_test.go plus additions in the access, jenkins and snyk tests). See ../14-integrations-access.md section 16 and
../../30-verification/design-14-integrations-access.md. Re-run: go vet ./... && go test -race -count=3 ./...
