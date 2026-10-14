// docs/design is a mirror of the design documents. Its reference assets contain Go files and nested go.mod files; this file makes the whole
// subtree a module of its own so that go list ./..., go mod tidy, golangci-lint, go vet and the architecture test of the product never see
// it. It is never built or imported. Do not add requirements here.
module scp.invalid/design-mirror

go 1.27
