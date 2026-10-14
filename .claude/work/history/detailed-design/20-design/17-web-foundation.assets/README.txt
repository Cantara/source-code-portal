Companion files of 17-web-foundation.md. THROW-AWAY PROTOTYPE (verified 2026-10-14), not the product tree.

web/       the prototype of the web/ tooling and shared layers: configs, scripts, src/{app,lib,shared,components/ui,features stubs,test}, e2e/
           src/lib/api/schema.d.ts is omitted (generated: `pnpm gen:api` from ../api/openapi-admin.yaml)
api/       copies of the OpenAPI fragments the prototype generated its client from (units 06/07)
go/        cmd/e2eserve: serves a built web/dist through the real internal/webapp (unit 06 prototype) plus fixture API, for Playwright
pins.py    registry check used to verify every pinned version on 2026-10-14

Reproduce (from this directory, Node 24.21 or 26, pnpm 12.10.1 first on PATH):
  cd web && pnpm install --frozen-lockfile
  pnpm gen:api && pnpm extract && pnpm check && BUILD_ID=abc123def456 pnpm build
  (copy go/cmd/e2eserve into a checkout of the 06-httpapi prototype, go build -o bin/e2eserve ./cmd/e2eserve)
  ../bin/e2eserve -dist dist -fixtures e2e/fixtures/api -addr 127.0.0.1:9090 -build abc123def456
  pnpm exec playwright test        # PW_CHANNEL=chrome uses the system Chrome

Verification 2026-10-14 (independent): see 30-verification/design-17-web-foundation.md. After the fixes: pnpm check = 144 tests in 21 files, test:browser = 2, Playwright = 62 runs.
api/ was refreshed to the 07 documents of 2026-10-14 03:12. lib/doc-id.ts was removed (owned by unit 18).
