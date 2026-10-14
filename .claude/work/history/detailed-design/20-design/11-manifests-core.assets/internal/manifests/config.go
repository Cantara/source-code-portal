package manifests

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/store"
)

// ByteSize is a size in bytes that decodes from "1MiB", "256KiB", "64MiB" or a plain integer (text form, so YAML and env both work).
type ByteSize int64

func (b *ByteSize) UnmarshalText(t []byte) error {
	s := strings.TrimSpace(string(t))
	mult := int64(1)
	for _, u := range []struct {
		suf string
		m   int64
	}{{"KiB", 1 << 10}, {"MiB", 1 << 20}, {"GiB", 1 << 30}, {"B", 1}} {
		if strings.HasSuffix(s, u.suf) {
			s, mult = strings.TrimSpace(strings.TrimSuffix(s, u.suf)), u.m
			break
		}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 0 || n > (1<<40)/mult {
		return fmt.Errorf("invalid byte size %q (use e.g. 256KiB, 8MiB)", string(t))
	}
	*b = ByteSize(n * mult)
	return nil
}

func (b ByteSize) MarshalText() ([]byte, error) {
	n := int64(b)
	switch {
	case n != 0 && n%(1<<30) == 0:
		return []byte(strconv.FormatInt(n>>30, 10) + "GiB"), nil
	case n != 0 && n%(1<<20) == 0:
		return []byte(strconv.FormatInt(n>>20, 10) + "MiB"), nil
	case n != 0 && n%(1<<10) == 0:
		return []byte(strconv.FormatInt(n>>10, 10) + "KiB"), nil
	}
	return []byte(strconv.FormatInt(n, 10)), nil
}

// Config is the manifests block of the root configuration (key `manifests`). Every field has a consumer (documented keys == consumed keys) and
// carries the tags platform/config.Describe demands: a snake_case yaml name and a jsonschema_description (verified against the real
// package, design section 6). Ranges of durations and sizes are enforced by Validate (path-accurate, all violations at once).
type Config struct {
	Enabled     bool              `yaml:"enabled" jsonschema_description:"Switch for package manifest intelligence: jobs, API, text outputs and badge kinds."`
	Refresh     RefreshConfig     `yaml:"refresh" jsonschema_description:"When manifests are re-read."`
	Graph       GraphConfig       `yaml:"graph" jsonschema_description:"The organisation-wide dependency graph."`
	Acquisition AcquisitionConfig `yaml:"acquisition" jsonschema_description:"Which files are fetched and how."`
	Limits      LimitsConfig      `yaml:"limits" jsonschema_description:"Bounds on hostile input: file sizes, nesting, time, memory, counts."`
	PomSource   PomSourceConfig   `yaml:"pom_source" jsonschema_description:"Optional lookup of parent and BOM POMs outside the organisation."`
	Output      OutputConfig      `yaml:"output" jsonschema_description:"Page sizes and caps of the API and the text outputs."`
}

type RefreshConfig struct {
	TTL            time.Duration `yaml:"ttl" scp:"min=1m,max=24h" jsonschema_description:"Drift bound between refresh checks of one repository (the cost of an unchanged check is no API call)."`
	IdleBackoffMax time.Duration `yaml:"idle_backoff_max" scp:"min=1m,max=168h" jsonschema_description:"Longest interval between checks of a repository that keeps being unchanged; staleness of published facts is 1.5 times this."`
	Jitter         float64       `yaml:"jitter" jsonschema:"minimum=0,maximum=0.5" jsonschema_description:"Random spread of the refresh interval, as a fraction (0.1 is plus or minus 10 percent)."`
	JobTimeout     time.Duration `yaml:"job_timeout" scp:"min=10s,max=30m" jsonschema_description:"Deadline of one manifests.refresh run."`
	RepublishAfter time.Duration `yaml:"republish_after" scp:"min=1m,max=168h" jsonschema_description:"How often an unchanged result republishes the facts so their freshness timestamp advances."`
}

type GraphConfig struct {
	TTL               time.Duration `yaml:"ttl" scp:"min=1m,max=1h" jsonschema_description:"Repair interval of the dependency graph job."`
	Debounce          time.Duration `yaml:"debounce" scp:"min=0s,max=30s" jsonschema_description:"Delay between a repository refresh and the graph rebuild it requests, so a burst of refreshes causes one rebuild."`
	MaxEdges          int           `yaml:"max_edges" jsonschema:"minimum=1000,maximum=20000000" jsonschema_description:"Safety bound on declared dependencies in the graph; beyond it the build truncates and says so."`
	MaxCyclesReported int           `yaml:"max_cycles_reported" jsonschema:"minimum=0,maximum=1000" jsonschema_description:"Most dependency cycles listed by the graph operation."`
}

type AcquisitionConfig struct {
	DisabledEcosystems []string `yaml:"disabled_ecosystems" jsonschema_description:"Ecosystem ids whose parsers are switched off, for example nuget (the only per-ecosystem switch)."`
	ExcludeSegments    []string `yaml:"exclude_segments" jsonschema_description:"Path segments never descended into or fetched, for example node_modules."`
	FixtureSegments    []string `yaml:"fixture_segments" jsonschema_description:"Path segments that mark a unit as an example or test fixture, excluded from the graph, rollups and SBOM."`
	MaxDepth           int      `yaml:"max_depth" jsonschema:"minimum=1,maximum=32" jsonschema_description:"Deepest directory level searched for manifests."`
	MaxFiles           int      `yaml:"max_files" jsonschema:"minimum=1,maximum=2000" jsonschema_description:"Most manifest files parsed per repository."`
	MaxTreeCalls       int      `yaml:"max_tree_calls" jsonschema:"minimum=1,maximum=2000" jsonschema_description:"Most directory reads when GitHub truncates the recursive tree."`
	MaxTreeEntries     int      `yaml:"max_tree_entries" jsonschema:"minimum=1000,maximum=5000000" jsonschema_description:"Most tree entries read in one listing."`
	FetchConcurrency   int      `yaml:"fetch_concurrency" jsonschema:"minimum=1,maximum=16" jsonschema_description:"Parallel blob reads per repository."`
	BatchSize          int      `yaml:"batch_size" jsonschema:"minimum=1,maximum=100" jsonschema_description:"Blobs per batched read when the adapter offers one."`
	BatchMaxBlobBytes  ByteSize `yaml:"batch_max_blob_bytes" jsonschema_description:"Blobs above this size are read singly instead of in a batch (1KiB to 4MiB)."`
	RawBlobCacheMax    ByteSize `yaml:"raw_blob_cache_max" jsonschema_description:"Raw blobs up to this size are cached so a parser change re-parses without a network call (0 to 4MiB)."`
}

type LimitsConfig struct {
	ManifestBytes    ByteSize      `yaml:"manifest_bytes" jsonschema_description:"Largest manifest file parsed (1KiB to 8MiB)."`
	LockfileBytes    ByteSize      `yaml:"lockfile_bytes" jsonschema_description:"Largest lockfile parsed (manifest_bytes to 32MiB)."`
	MaxDepth         int           `yaml:"max_depth" jsonschema:"minimum=8,maximum=10000" jsonschema_description:"Deepest nesting a parser accepts."`
	MaxNodes         int           `yaml:"max_nodes" jsonschema:"minimum=1000,maximum=50000000" jsonschema_description:"Most tokens, elements or entries a parser visits per file."`
	MaxExpand        ByteSize      `yaml:"max_expand" jsonschema_description:"Longest value after property interpolation (1KiB to 16MiB)."`
	MaxExpandFile    ByteSize      `yaml:"max_expand_file" jsonschema_description:"Total interpolated bytes per file (max_expand to 64MiB)."`
	ParseTimeout     time.Duration `yaml:"parse_timeout" scp:"min=100ms,max=1m" jsonschema_description:"Time one parser may take for one file."`
	AssembleTimeout  time.Duration `yaml:"assemble_timeout" scp:"min=100ms,max=1m" jsonschema_description:"Time one ecosystem assembler may take per repository."`
	ParseConcurrency int           `yaml:"parse_concurrency" jsonschema:"minimum=0,maximum=64" jsonschema_description:"Parallel parses per repository; 0 means the smaller of the CPU count and 4."`
	ParseMemory      ByteSize      `yaml:"parse_memory" jsonschema_description:"Weighted memory budget of all running parses (8MiB to 4GiB)."`
	WatchdogGrace    time.Duration `yaml:"watchdog_grace" scp:"min=0s,max=10s" jsonschema_description:"Extra time after parse_timeout before a parser goroutine is abandoned."`
	AbandonTrip      int           `yaml:"abandon_trip" jsonschema:"minimum=1,maximum=100" jsonschema_description:"Abandoned runs after which a parser is disabled until restart."`
	MaxDepsPerFile   int           `yaml:"max_deps_per_file" jsonschema:"minimum=100,maximum=1000000" jsonschema_description:"Most dependencies kept from one file."`
	MaxLockedPerFile int           `yaml:"max_locked_per_file" jsonschema:"minimum=100,maximum=5000000" jsonschema_description:"Most locked components kept from one lockfile."`
	MaxUnitsPerRepo  int           `yaml:"max_units_per_repo" jsonschema:"minimum=1,maximum=5000" jsonschema_description:"Most manifest units kept per repository."`
	MaxFactsBytes    ByteSize      `yaml:"max_facts_bytes" jsonschema_description:"Largest stored facts payload per repository (64KiB to 16MiB, the store's payload bound); larger facts are trimmed."`
}

type PomSourceConfig struct {
	Enabled              bool     `yaml:"enabled" jsonschema_description:"Allow Maven parent and BOM POMs outside the organisation to be fetched from the hosts below."`
	Hosts                []string `yaml:"hosts" jsonschema_description:"Hosts the POM source may contact (allow-list of the hardened client)."`
	PrivateGroupPrefixes []string `yaml:"private_group_prefixes" jsonschema_description:"Maven groupId prefixes that are never looked up externally, for example com.acme."`
}

type OutputConfig struct {
	LLMMaxRows    int `yaml:"llm_max_rows" jsonschema:"minimum=10,maximum=5000" jsonschema_description:"Rows per table in the dependencies.md text outputs."`
	DefaultPage   int `yaml:"default_page" jsonschema:"minimum=1,maximum=1000" jsonschema_description:"Page size of list operations when the request gives none."`
	MaxPage       int `yaml:"max_page" jsonschema:"minimum=1,maximum=1000" jsonschema_description:"Largest page size a request may ask for."`
	VersionsTopN  int `yaml:"versions_top_n" jsonschema:"minimum=1,maximum=1000" jsonschema_description:"Versions listed per component in the version histograms."`
	SBOMMaxLocked int `yaml:"sbom_max_locked" jsonschema:"minimum=1000,maximum=5000000" jsonschema_description:"Most locked components written into one SBOM; the rest is omitted and the document says so."`
}

// DefaultConfig returns the documented defaults (the embedded default configuration is generated from this).
func DefaultConfig() Config {
	return Config{
		Enabled: true,
		Refresh: RefreshConfig{TTL: 30 * time.Minute, IdleBackoffMax: 4 * time.Hour, Jitter: 0.1, JobTimeout: 3 * time.Minute, RepublishAfter: time.Hour},
		Graph:   GraphConfig{TTL: 5 * time.Minute, Debounce: 2 * time.Second, MaxEdges: 2_000_000, MaxCyclesReported: 50},
		Acquisition: AcquisitionConfig{
			ExcludeSegments:   []string{"node_modules", "vendor", ".git", "target", "build", "dist", "out", ".gradle", ".venv", "venv", "__pycache__", "Pods", "testdata", "third_party"},
			FixtureSegments:   []string{"examples", "example", "samples", "sample", "test", "tests", "e2e", "fixtures", "demo"},
			MaxDepth:          12,
			MaxFiles:          200,
			MaxTreeCalls:      200,
			MaxTreeEntries:    500_000,
			FetchConcurrency:  4,
			BatchSize:         25,
			BatchMaxBlobBytes: 256 << 10,
			RawBlobCacheMax:   256 << 10,
		},
		Limits: LimitsConfig{
			ManifestBytes: 1 << 20, LockfileBytes: 8 << 20, MaxDepth: 256, MaxNodes: 2_000_000, MaxExpand: 1 << 20, MaxExpandFile: 8 << 20,
			ParseTimeout: 2 * time.Second, AssembleTimeout: 5 * time.Second, ParseConcurrency: 0, ParseMemory: 256 << 20,
			WatchdogGrace: 500 * time.Millisecond, AbandonTrip: 3, MaxDepsPerFile: 20_000, MaxLockedPerFile: 100_000, MaxUnitsPerRepo: 500, MaxFactsBytes: 8 << 20,
		},
		PomSource: PomSourceConfig{Hosts: []string{"repo1.maven.org"}},
		Output:    OutputConfig{LLMMaxRows: 200, DefaultPage: 50, MaxPage: 200, VersionsTopN: 100, SBOMMaxLocked: 200_000},
	}
}

// Validate collects every violation with its config path (never stops at the first).
func (c Config) Validate() error {
	var errs []error
	bad := func(path, why string) { errs = append(errs, fmt.Errorf("manifests.%s: %s", path, why)) }
	dur := func(path string, d, lo, hi time.Duration) {
		if d < lo || d > hi {
			bad(path, fmt.Sprintf("must be between %s and %s", lo, hi))
		}
	}
	num := func(path string, n, lo, hi int) {
		if n < lo || n > hi {
			bad(path, fmt.Sprintf("must be between %d and %d", lo, hi))
		}
	}
	size := func(path string, n ByteSize, lo, hi int64) {
		if int64(n) < lo || int64(n) > hi {
			bad(path, fmt.Sprintf("must be between %d and %d bytes", lo, hi))
		}
	}
	r := c.Refresh
	dur("refresh.ttl", r.TTL, time.Minute, 24*time.Hour)
	dur("refresh.idle_backoff_max", r.IdleBackoffMax, r.TTL, 7*24*time.Hour)
	dur("refresh.job_timeout", r.JobTimeout, 10*time.Second, 30*time.Minute)
	dur("refresh.republish_after", r.RepublishAfter, time.Minute, r.IdleBackoffMax)
	if r.Jitter < 0 || r.Jitter > 0.5 {
		bad("refresh.jitter", "must be between 0 and 0.5")
	}
	g := c.Graph
	dur("graph.ttl", g.TTL, time.Minute, time.Hour)
	dur("graph.debounce", g.Debounce, 0, 30*time.Second)
	num("graph.max_edges", g.MaxEdges, 1000, 20_000_000)
	num("graph.max_cycles_reported", g.MaxCyclesReported, 0, 1000)
	a := c.Acquisition
	num("acquisition.max_depth", a.MaxDepth, 1, 32)
	num("acquisition.max_files", a.MaxFiles, 1, 2000)
	num("acquisition.max_tree_calls", a.MaxTreeCalls, 1, 2000)
	num("acquisition.max_tree_entries", a.MaxTreeEntries, 1000, 5_000_000)
	num("acquisition.fetch_concurrency", a.FetchConcurrency, 1, 16)
	num("acquisition.batch_size", a.BatchSize, 1, 100)
	size("acquisition.batch_max_blob_bytes", a.BatchMaxBlobBytes, 1<<10, 4<<20)
	size("acquisition.raw_blob_cache_max", a.RawBlobCacheMax, 0, 4<<20)
	for _, e := range a.DisabledEcosystems {
		if e == "" || e != strings.ToLower(e) || strings.ContainsAny(e, " /*") {
			bad("acquisition.disabled_ecosystems", fmt.Sprintf("%q must be a lower-case ecosystem id such as nuget", e))
		}
	}
	for _, s := range append(append([]string{}, a.ExcludeSegments...), a.FixtureSegments...) {
		if s == "" || strings.ContainsAny(s, "/*?[") {
			bad("acquisition.exclude_segments/fixture_segments", fmt.Sprintf("%q must be a plain path segment", s))
		}
	}
	l := c.Limits
	size("limits.manifest_bytes", l.ManifestBytes, 1<<10, 8<<20)
	size("limits.lockfile_bytes", l.LockfileBytes, l.ManifestBytes64(), 32<<20)
	num("limits.max_depth", l.MaxDepth, 8, 10_000)
	num("limits.max_nodes", l.MaxNodes, 1000, 50_000_000)
	size("limits.max_expand", l.MaxExpand, 1<<10, 16<<20)
	size("limits.max_expand_file", l.MaxExpandFile, int64(l.MaxExpand), 64<<20)
	dur("limits.parse_timeout", l.ParseTimeout, 100*time.Millisecond, time.Minute)
	dur("limits.assemble_timeout", l.AssembleTimeout, 100*time.Millisecond, time.Minute)
	num("limits.parse_concurrency", l.ParseConcurrency, 0, 64)
	size("limits.parse_memory", l.ParseMemory, 8<<20, 4<<30)
	dur("limits.watchdog_grace", l.WatchdogGrace, 0, 10*time.Second)
	num("limits.abandon_trip", l.AbandonTrip, 1, 100)
	num("limits.max_deps_per_file", l.MaxDepsPerFile, 100, 1_000_000)
	num("limits.max_locked_per_file", l.MaxLockedPerFile, 100, 5_000_000)
	num("limits.max_units_per_repo", l.MaxUnitsPerRepo, 1, 5000)
	size("limits.max_facts_bytes", l.MaxFactsBytes, 64<<10, store.MaxPayload) // the item payload bound of the store
	for _, p := range c.PomSource.PrivateGroupPrefixes {
		if p == "" || strings.ContainsAny(p, " /*") {
			bad("pom_source.private_group_prefixes", fmt.Sprintf("%q must be a groupId prefix such as com.acme", p))
		}
	}
	o := c.Output
	num("output.llm_max_rows", o.LLMMaxRows, 10, 5000)
	num("output.max_page", o.MaxPage, 1, 1000)
	num("output.default_page", o.DefaultPage, 1, o.MaxPage)
	num("output.versions_top_n", o.VersionsTopN, 1, 1000)
	num("output.sbom_max_locked", o.SBOMMaxLocked, 1000, 5_000_000)
	return errors.Join(errs...)
}

func (l LimitsConfig) ManifestBytes64() int64 { return int64(l.ManifestBytes) }
