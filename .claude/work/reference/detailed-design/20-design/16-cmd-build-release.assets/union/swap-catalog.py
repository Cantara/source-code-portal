#!/usr/bin/env python3
"""Make the union module use the REAL catalog.Config as the `catalog:` section (replacing cmd/scp's stand-in), to reproduce the
cross-unit findings of design 16 section 14. Usage: swap-catalog.py <union-dir> [--propose-fixes]

Without --propose-fixes the build is the honest state of the sibling assets: `go test ./cmd/scp` then fails at config.Describe
(finding U-1: the 08 types carry no jsonschema_description, Matcher has unnamed fields, Config.Validate has another signature).
With --propose-fixes the three mechanical fixes proposed to unit 08 are applied to the union copy (never to 08's assets); what
still fails is finding U-2 (a one-key matcher mapping cannot be assigned by config.Load, which does not call UnmarshalYAML).
"""
import re
import sys

root = sys.argv[1]
fix = "--propose-fixes" in sys.argv

p = f"{root}/cmd/scp/config.go"
s = open(p).read()
s = s.replace("Catalog catalogStub", "Catalog catalog.Config")
a = s.index("// catalogStub stands for")
b = s.index("type storeStub struct")
s = s[:a] + s[b:]
s = s.replace('"github.com/cantara/source-code-portal/internal/platform/httpx"', '"github.com/cantara/source-code-portal/internal/catalog"\n\t"github.com/cantara/source-code-portal/internal/platform/httpx"', 1)
open(p, "w").write(s)

p = f"{root}/cmd/scp/default.yaml"
d = open(p).read()
d = d.replace("catalog:\n  ungrouped: track\n", "catalog:\n  ungrouped: track\n  heads: {batch_size: 50, interval: 5m, archived_interval: 24h}\n")
open(p, "w").write(d)

if fix:
    p = f"{root}/internal/catalog/config.go"
    s = open(p).read()

    def tag(m):
        line = m.group(0)
        if "jsonschema_description" in line:
            return line
        name = re.search(r'yaml:"([a-z_]+)"', line).group(1)
        return line.replace(f'`yaml:"{name}"`', f'`yaml:"{name}" jsonschema_description:"catalog key {name} (08 section 6)."`')

    s = re.sub(r'.*`yaml:"[a-z_]+"`.*', tag, s)
    s = s.replace("func (c Config) Validate(vc ValidateContext) []Finding", "func (c Config) Check(vc ValidateContext) []Finding")
    open(p, "w").write(s)
    for f in ("lint.go", "service.go"):
        q = f"{root}/internal/catalog/{f}"
        t = open(q).read().replace("cfg.Validate(vc)", "cfg.Check(vc)").replace("d.Config.Validate(ValidateContext", "d.Config.Check(ValidateContext")
        open(q, "w").write(t)
    q = f"{root}/internal/catalog/matcher.go"
    m = open(q).read().replace("\tKind  MatcherKind\n\tValue string", '\tKind  MatcherKind `yaml:"-"`\n\tValue string      `yaml:"-"`')
    open(q, "w").write(m)
print("catalog section swapped" + (" (with the proposed 08 fixes)" if fix else ""))
