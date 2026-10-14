module github.com/Acme/Billing/v2

go 1.24.0

toolchain go1.24.3

godebug default=go1.21

require (
	github.com/Acme/core-lib v1.4.2
	golang.org/x/text v0.43.0
	golang.org/x/tools v0.42.0
	github.com/stretchr/testify v1.9.0
	github.com/pkg/errors v0.9.1 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

require github.com/old/lib v0.0.0-20230102030405-abcdef123456

tool golang.org/x/tools/cmd/stringer

tool example.com/missing/cmd/x

replace github.com/Acme/core-lib => ../core-lib

replace github.com/old/lib v0.0.0-20230102030405-abcdef123456 => github.com/acme-fork/lib v1.0.1

exclude golang.org/x/text v0.1.0

retract v2.0.0 // published by mistake
