// Package archtest holds the closed-world import-boundary rules of the module (architecture 5.2, ADR-018).
//
// rules.go is the single source of truth: the test in arch_test.go checks the real tree against it,
// and the depguard section of .golangci.yml is generated from it (depguard.go, TestDepguardInSync).
package archtest

// Rule is one row of the allowed-import matrix. Paths are module-relative ("internal/scm", "cmd/scp").
type Rule struct {
	// Pkg is the package path, or a pattern with '*' standing for one path segment
	// ("internal/manifests/ecosystems/*"). A pattern row must match at least one package unless Planned.
	Pkg string
	// Rank is the rank of the architecture 5.2 table (0 kernel .. 6 cmd). RankTest exempts the package from the rank proof.
	Rank int
	// Allow lists the module-relative packages (or '*' patterns) that non-test files of the package may import.
	Allow []string
	// AllowAll lets the package import every package of the module except test helpers and archtest (cmd/scp).
	AllowAll bool
	// Test lists packages that only the package's _test.go files may import in addition to Allow (test helpers).
	Test []string
	// TestHelper marks packages that may be imported by _test.go files and by rows that list them explicitly.
	TestHelper bool
	// Planned rows describe packages that do not exist yet (gitsource, manifests/providers/*).
	Planned bool
	// Note carries the reason for a deviation from the architecture table, if any.
	Note string
}

// RankTest marks the archtest package itself.
const RankTest = -1

// Owned binds a heavy or dangerous import (path prefix) to the only packages that may import it (rule G6, G2).
type Owned struct {
	Prefix string
	Owners []string // module-relative packages or '*' patterns; empty means nobody
	// Exact matches the import path itself (stdlib entries such as "log", which must not match "log/slog");
	// otherwise the entry matches the path and everything below it ("github.com/yuin/goldmark/ast").
	Exact bool
	// TestOK lets any package's _test.go files import it (02 D14, 14 D-9: tests observe spans and metrics with the SDK's in-memory
	// readers and exporters). Non-test code stays confined to Owners.
	TestOK bool
	Why    string
}

func k4() []string {
	return []string{
		"internal/scm", "internal/freshness", "internal/job", "internal/routes", "internal/readmodel", "internal/store",
		"internal/httpapi", "internal/httpapi/authz", "internal/platform/secret", "internal/platform/logx",
	}
}

func with(base []string, more ...string) []string {
	return append(append([]string(nil), base...), more...)
}

func feature(name string, more ...string) Rule {
	return Rule{Pkg: "internal/" + name, Rank: 4, Allow: with(k4(), more...), Test: []string{"internal/httpapi/apitest"}}
}

func api(owner string, rank int) Rule {
	return Rule{Pkg: "internal/" + owner + "/" + lastWord(owner) + "api", Rank: rank, Allow: []string{"internal/httpapi/openapi"}}
}

func lastWord(s string) string {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '/' {
			return s[i+1:]
		}
	}
	return s
}

// Rules returns a fresh copy of the matrix (a function, not a package variable: rule G5).
func Rules() []Rule {
	r := []Rule{
		// rank 0: kernel and leaf infrastructure
		{Pkg: "internal/scm"}, {Pkg: "internal/freshness"}, {Pkg: "internal/routes"}, {Pkg: "internal/safehtml"},
		{Pkg: "internal/platform/secret"}, {Pkg: "internal/platform/netguard"}, {Pkg: "internal/platform/telemetry"},
		{Pkg: "internal/platform/lifecycle"}, {Pkg: "internal/readmodel"}, {Pkg: "internal/webapp"}, {Pkg: "internal/apispec"},
		{Pkg: "internal/httpapi/openapi"}, {Pkg: "internal/docs/schema"}, {Pkg: "web"},

		// rank 1
		{Pkg: "internal/job", Rank: 1, Allow: []string{"internal/freshness"}},
		{Pkg: "internal/platform/logx", Rank: 1, Allow: []string{"internal/platform/secret"}},
		{Pkg: "internal/platform/config", Rank: 1, Allow: []string{"internal/platform/secret"}},
		{Pkg: "internal/store", Rank: 1, Allow: []string{"internal/freshness", "internal/scm"}},
		{Pkg: "internal/github/auth", Rank: 1, Allow: []string{"internal/scm", "internal/platform/secret"}},
		{Pkg: "internal/github/githubtest", Rank: 1, Allow: []string{"internal/scm"}, TestHelper: true},
		api("catalog", 1), api("commits", 1), api("docs", 1), api("manifests", 1), api("pages", 1), api("live", 1), api("ops", 1), api("overview", 1),
		{Pkg: "internal/docs/internal/render", Rank: 1, Allow: []string{"internal/scm", "internal/freshness", "internal/platform/secret", "internal/safehtml"}},
		{Pkg: "internal/manifests/internal/*", Rank: 1},

		// rank 2
		{Pkg: "internal/platform/httpx", Rank: 2, Allow: []string{"internal/platform/logx", "internal/platform/secret"}, Note: "platform/secret added: 02-platform deviation D2"},
		{Pkg: "internal/refresh", Rank: 2, Allow: []string{"internal/job", "internal/freshness", "internal/platform/logx"}},
		{Pkg: "internal/reactor", Rank: 2, Allow: []string{"internal/job", "internal/scm", "internal/platform/logx"}},
		{Pkg: "internal/store/sqlite", Rank: 2, Allow: []string{"internal/store", "internal/freshness"}, Test: []string{"internal/store/storetest"}},
		{Pkg: "internal/store/memory", Rank: 2, Allow: []string{"internal/store", "internal/freshness"}, Test: []string{"internal/store/storetest"}},
		{Pkg: "internal/store/storetest", Rank: 2, Allow: []string{"internal/store", "internal/freshness"}, TestHelper: true},
		{Pkg: "internal/github", Rank: 2, Test: []string{"internal/github/githubtest"},
			Allow: []string{"internal/scm", "internal/freshness", "internal/platform/secret", "internal/platform/logx", "internal/platform/netguard", "internal/github/auth"}},
		{Pkg: "internal/gitsource", Rank: 2, Planned: true, Test: []string{"internal/github/githubtest"},
			Allow: []string{"internal/scm", "internal/freshness", "internal/platform/secret", "internal/platform/logx", "internal/platform/netguard", "internal/github/auth"}},
		{Pkg: "internal/httpapi", Rank: 2, Allow: []string{"internal/routes", "internal/platform/logx", "internal/httpapi/openapi"}},

		// rank 3
		{Pkg: "internal/httpapi/authz", Rank: 3, Allow: []string{"internal/scm", "internal/freshness", "internal/readmodel", "internal/httpapi", "internal/platform/logx"}},
		{Pkg: "internal/httpapi/validate", Rank: 3, Allow: []string{"internal/httpapi", "internal/httpapi/openapi", "internal/platform/logx"}, Test: []string{"internal/httpapi/apitest"}},
		{Pkg: "internal/httpapi/apitest", Rank: 3, Allow: []string{"internal/httpapi", "internal/httpapi/openapi", "internal/apispec"}, TestHelper: true},

		// rank 4: features and services
		feature("catalog", "internal/catalog/catalogapi"),
		feature("commits", "internal/commits/commitsapi", "internal/httpapi/openapi"), // A-30: shared generated schemas (design 07 alias files make this removable)
		feature("docs", "internal/docs/docsapi", "internal/docs/internal/render", "internal/docs/schema", "internal/safehtml"),
		feature("manifests", "internal/manifests/manifestsapi", "internal/manifests/internal/*", "internal/httpapi/openapi"), // A-30, as commits
		feature("badges"),
		feature("pages", "internal/pages/pagesapi", "internal/safehtml"),
		feature("integrations"),
		feature("media"),
		{Pkg: "internal/webhook", Rank: 4, Allow: []string{"internal/scm", "internal/job", "internal/httpapi", "internal/platform/logx"},
			Note: "04 D1 (freshness) was WITHDRAWN by unit 04's verification: webhook stays inside the architecture row (cross-design review CD-DM-04)"},
		{Pkg: "internal/access", Rank: 4, Allow: []string{"internal/scm", "internal/httpapi", "internal/httpapi/authz", "internal/platform/logx", "internal/platform/secret"},
			Note: "platform/secret added: 14-integrations-access deviation D-1"},
		{Pkg: "internal/live", Rank: 4, Allow: []string{"internal/readmodel", "internal/httpapi", "internal/platform/logx", "internal/live/liveapi"}, Test: []string{"internal/httpapi/apitest"}},
		{Pkg: "internal/ops", Rank: 4, Allow: []string{"internal/freshness", "internal/readmodel", "internal/httpapi", "internal/httpapi/authz", "internal/platform/logx", "internal/ops/opsapi"}, Test: []string{"internal/httpapi/apitest"}},

		// rank 5
		{Pkg: "internal/overview", Rank: 5, Test: []string{"internal/httpapi/apitest"}, Allow: []string{
			"internal/scm", "internal/freshness", "internal/routes", "internal/readmodel", "internal/httpapi", "internal/httpapi/authz", "internal/platform/logx", "internal/overview/overviewapi",
			"internal/catalog", "internal/commits", "internal/docs", "internal/manifests", "internal/badges", "internal/pages", "internal/integrations",
			"internal/httpapi/openapi"}, Note: "A-30: httpapi/openapi added (shared generated schemas; design 07 alias files make this removable)"},
		{Pkg: "cmd/scp/internal/bridge", Rank: 5, Allow: []string{
			"internal/scm", "internal/freshness", "internal/job", "internal/store", "internal/readmodel", "internal/refresh",
			"internal/catalog", "internal/commits", "internal/docs", "internal/manifests", "internal/badges", "internal/pages", "internal/integrations", "internal/media",
			"internal/webhook", "internal/access", "internal/live", "internal/ops", "internal/reactor", "internal/github"},
			Note: "reactor (01 section 9.1, the reactor metrics bridge) and github (05 D-3, RateGate and the ops bridges) added"},
		{Pkg: "internal/integrations/jenkins", Rank: 5, Allow: []string{"internal/integrations", "internal/scm", "internal/freshness", "internal/platform/secret"}},
		{Pkg: "internal/integrations/snyk", Rank: 5, Allow: []string{"internal/integrations", "internal/scm", "internal/freshness", "internal/platform/secret"}},
		{Pkg: "internal/manifests/ecosystems/*", Rank: 5, Allow: []string{"internal/manifests", "internal/manifests/internal/*"}, Test: []string{"internal/manifests/parsertest"}},
		{Pkg: "internal/manifests/providers/*", Rank: 5, Planned: true, Allow: []string{"internal/manifests", "internal/scm", "internal/freshness"}},
		{Pkg: "internal/manifests/parsertest", Rank: 5, Allow: []string{"internal/manifests", "internal/manifests/internal/*"}, TestHelper: true},

		// rank 6: edges
		{Pkg: "cmd/scp", Rank: 6, AllowAll: true},
		{Pkg: "cmd/fakegithub", Rank: 6, Allow: []string{"internal/github/githubtest", "internal/scm"}},

		{Pkg: "internal/archtest", Rank: RankTest},
	}
	// Feature tests may use the manifests parser test helper through the feature's own row only.
	for i := range r {
		if r[i].Pkg == "internal/manifests" {
			r[i].Test = append(r[i].Test, "internal/manifests/parsertest")
		}
	}
	return r
}

// OwnedImports returns rule G6/G2: heavy libraries and banned imports (stdlib paths included).
func OwnedImports() []Owned {
	return []Owned{
		{Prefix: "github.com/microcosm-cc/bluemonday", Owners: []string{"internal/safehtml"}, Why: "G6: the sanitiser is the only SafeHTML constructor"},
		{Prefix: "github.com/yuin/goldmark", Owners: []string{"internal/docs/internal/render"}, Why: "G6"},
		{Prefix: "github.com/alecthomas/chroma", Owners: []string{"internal/docs/internal/render"}, Why: "G6"},
		{Prefix: "modernc.org/sqlite", Owners: []string{"internal/store/sqlite"}, Why: "G6"},
		{Prefix: "modernc.org/libc", Owners: []string{"internal/store/sqlite"}, Why: "G6"},
		{Prefix: "go.opentelemetry.io/otel/sdk", Owners: []string{"internal/platform/telemetry"}, TestOK: true, Why: "G6/G11: SDK confined to telemetry (tests may use its in-memory readers: 02 D14)"},
		{Prefix: "go.opentelemetry.io/otel/exporters", Owners: []string{"internal/platform/telemetry"}, Why: "G6/G11"},
		{Prefix: "go.opentelemetry.io/otel", Exact: true, Owners: []string{"cmd/scp", "internal/platform/telemetry"}, Why: "G11: the root package carries the global accessors; features use otel/metric, otel/trace, otel/attribute and otel/codes through Deps"},
		{Prefix: "github.com/getkin/kin-openapi", Owners: []string{"internal/httpapi/validate", "internal/httpapi/apitest"}, Why: "G6"},
		{Prefix: "github.com/oapi-codegen/nethttp-middleware", Owners: []string{"internal/httpapi/validate"}, Why: "G6"},
		{Prefix: "github.com/oapi-codegen/runtime", Owners: []string{"internal/httpapi/openapi", "internal/*/*api"}, Why: "generated code only"},
		{Prefix: "code.dny.dev/ssrf", Owners: []string{"internal/platform/netguard"}, Why: "G6"},
		{Prefix: "github.com/golang-jwt/jwt", Owners: nil, Why: "A-30: the App JWT is signed by an own encoder over crypto.Signer; no JWT library"},
		{Prefix: "github.com/sony/gobreaker", Owners: []string{"internal/github"}, Why: "GR-22: breakers only in adapters"},
		{Prefix: "go.yaml.in/yaml", Owners: []string{"internal/platform/config", "internal/platform/secret", "internal/docs", "internal/docs/internal/render", "internal/manifests/ecosystems/*", "cmd/scp"}, Why: "G6; docs/internal/render reads front matter (A-30)"},
		{Prefix: "github.com/pelletier/go-toml", Owners: []string{"internal/manifests/ecosystems/*"}, Why: "G6"},
		{Prefix: "github.com/hashicorp/hcl", Owners: []string{"internal/manifests/ecosystems/terraform"}, Why: "G6"},
		{Prefix: "github.com/CycloneDX/cyclonedx-go", Owners: []string{"internal/manifests"}, Why: "G6: export boundary"},
		{Prefix: "github.com/prometheus/client_golang", Owners: []string{"internal/platform/telemetry", "internal/ops"}, TestOK: true, Why: "G6 (tests of live, ops and overview read the exporter: A-30)"},
		{Prefix: "os/exec", Exact: true, Owners: []string{"internal/gitsource", "cmd/scp", "internal/archtest"}, Why: "G6/GR-36: no subprocess outside git mode (internal/gitsource) and the token_command runner of cmd/scp"},
		{Prefix: "net/http/pprof", Exact: true, Owners: []string{"internal/ops"}, Why: "GR-17: pprof only on the admin mux"},
		{Prefix: "html/template", Exact: true, Owners: []string{"internal/webapp", "cmd/scp"}, Why: "ADR-008"},
		{Prefix: "time/tzdata", Exact: true, Owners: []string{"cmd/scp"}, Why: "A-25: zone database embedded once, by the binary"},
		{Prefix: "log", Exact: true, Owners: nil, Why: "G2: use log/slog"},
		{Prefix: "math/rand", Exact: true, Owners: nil, Why: "G2: math/rand/v2 only"},
		{Prefix: "github.com/pkg/errors", Owners: nil, Why: "G2"},
		{Prefix: "gopkg.in/yaml", Owners: nil, Why: "A-07: go.yaml.in/yaml/v3 only (archived repository)"},
		{Prefix: "github.com/google/go-github", Owners: nil, Why: "ADR-004: own thin client"},
		{Prefix: "github.com/go-git/go-git", Owners: nil, Why: "D-17/8.15: git binary, not go-git"},
		{Prefix: "github.com/go-selenium", Owners: nil, Why: "GR-13: no browser automation"},
		{Prefix: "github.com/spf13/cobra", Owners: nil, Why: "CLI is stdlib flag"},
	}
}

// BannedPackageNames is rule G1.
func BannedPackageNames() []string {
	return []string{"util", "common", "helper", "helpers", "model", "models", "types", "api", "interfaces", "base"}
}
