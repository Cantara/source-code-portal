package docs

import "github.com/cantara/source-code-portal/internal/docs/schema"

// DescriptorSchema returns the embedded JSON Schema of scp.yaml v1, for `scp config schema --descriptor` and the generated
// reference. cmd/scp calls this instead of the leaf package docs/schema, whose only exports are the embedded files
// (cross-design review CD-IF-04: unit 16 had assumed schema.Descriptor() and schema.Lint(), which unit 10 never exported).
func DescriptorSchema() []byte { return schema.ScpV1() }

// LintDescriptor validates one scp.yaml document with the same loader the refresh job uses and returns every problem (an empty slice
// means the file is valid). It reads nothing from the network or the disk; `scp descriptor lint FILE...` calls it per file.
func LintDescriptor(src []byte) ([]DescriptorProblem, error) {
	l, err := NewDescriptorLoader()
	if err != nil {
		return nil, err
	}
	_, ps := l.Load(src)
	return ps, nil
}
