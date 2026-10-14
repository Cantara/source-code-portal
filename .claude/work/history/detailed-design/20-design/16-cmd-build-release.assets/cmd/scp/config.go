package main

import (
	_ "embed"
	"fmt"
	"time"
	_ "time/tzdata" // the distroless static image has no zone database; site.display_time_zone is validated with time.LoadLocation (architecture 16)

	"github.com/cantara/source-code-portal/internal/platform/httpx"
	"github.com/cantara/source-code-portal/internal/platform/lifecycle"
	"github.com/cantara/source-code-portal/internal/platform/logx"
	"github.com/cantara/source-code-portal/internal/platform/telemetry"
)

//go:embed default.yaml
var defaultYAML []byte

// Config is the root configuration struct: composition only, every key lives in the feature-owned type (02 section 6.1). In the
// product the other units' Config types are fields of this struct, in the order of the architecture 16 outline; the scratch
// skeleton carries the platform sections plus the two stand-ins (Store, Refresh) that cmd/scp itself reads.
type Config struct {
	Version   int              `yaml:"version" jsonschema:"required,enum=1" jsonschema_description:"Config schema version. Must be 1."`
	Server    httpx.Config     `yaml:"server" jsonschema_description:"HTTP listeners."`
	Log       logx.Config      `yaml:"log" jsonschema_description:"Logging."`
	Telemetry telemetry.Config `yaml:"telemetry" jsonschema_description:"Metrics, traces and OTLP export."`
	RateLimit httpx.RateConfig `yaml:"ratelimit" jsonschema_description:"Keyed rate limiter."`
	Shutdown  ShutdownConfig   `yaml:"shutdown" jsonschema_description:"Graceful shutdown budget."`

	// Stand-ins for unit sections whose real types are other units' (catalog.Config, sqlite.Config). Only the keys cmd/scp reads.
	Catalog catalogStub   `yaml:"catalog" jsonschema_description:"Groups and repository selection."`
	Store   storeStub     `yaml:"store" jsonschema_description:"SQLite store."`
	Refresh RefreshConfig `yaml:"refresh" jsonschema_description:"Refresh engine: upstream pacing, backoff, grace."`
}

// catalogStub stands for catalog.Config (08 section 6); the union module replaces it by the real type, which is how
// a hand-written catalogue section is proved to load through the strict loader.
type catalogStub struct {
	Ungrouped string `yaml:"ungrouped" jsonschema_description:"track or ignore repositories no group selects."`
	Groups    []struct {
		ID          string `yaml:"id" jsonschema_description:"Group id."`
		DisplayName string `yaml:"display_name" jsonschema_description:"Display name."`
	} `yaml:"groups" jsonschema_description:"Groups."`
}

type storeStub struct {
	Path string `yaml:"path" jsonschema_description:"SQLite file; its directory must be a local writable volume (never NFS, EFS or SMB)."`
}

// ShutdownConfig is 02's lifecycle.Config plus the one key this unit adds (deviation D-2): the grace the supervisor gives
// (02 section 6.2 warns above "45 s in the reference manifest"; this key makes that number the operator's, default 45 s).
type ShutdownConfig struct {
	lifecycle.Config `yaml:",inline"`
	SupervisorGrace  time.Duration `yaml:"supervisor_grace" scp:"min=0s,max=10m" jsonschema_description:"The termination grace period the supervisor grants (Kubernetes terminationGracePeriodSeconds). Startup warns when the sum of the shutdown caps exceeds it; 0 disables the check."`
}

// abandonAfter is refresh.Tunables.AbandonAfter (04 section 2.5): the refresh stage stops within grace + abandonAfter + 1 s.
const abandonAfter = 3 * time.Second

// Caps of the stages that have no key. They cover joins that finish in milliseconds when nothing is wrong, so 02's
// lifecycle.Config.WorstCase leaves them out; planMargins is their sum, and the plan's absolute ceiling is WorstCase + planMargins.
const (
	sweeperStop  = 2 * time.Second // httpx.RunSweepers returns on cancel
	readinessCap = time.Second     // the Drain hook flips an atomic
	serversJoin  = time.Second     // StopTimeout of the servers stage is server_timeout + 1 s
	planMargins  = sweeperStop + readinessCap + serversJoin
)

// RefreshStop is the StopTimeout of the refresh stage (04 section 2.5, D9).
func (c Config) RefreshStop() time.Duration { return c.Refresh.stop() }

// Validate is the root's own cross-section rules (every violation is collected). Section rules live on the section types:
// listener uniqueness and trusted-proxy ranges are httpx.Config's, the shutdown sum is lifecycle.Config's. The product's root adds
// the rules that span owners (access.mode against visibility, a webhook route against a secret, reserved slugs); the skeleton
// carries one so the mechanism is tested.
func (c Config) Validate(report func(path, msg string)) {
	if g := c.Shutdown.SupervisorGrace; g > 0 && c.Shutdown.DrainDelay >= g {
		report("shutdown.drain_delay", "must be shorter than shutdown.supervisor_grace: the supervisor would kill the process while it is still draining")
	}
}

// startupWarnings are findings that are not violations: the process starts, the operator should know (02 section 6.2 and design 16 section 8.3).
func (c Config) startupWarnings() []string {
	var w []string
	if host, _ := splitHost(c.Server.Admin.Listen); !isLoopbackHost(host) && c.Server.Admin.BearerToken.IsZero() {
		w = append(w, "the admin listener is open to the network; restrict it by network policy or set server.admin.bearer_token")
	}
	if c.Server.Public.HSTS && len(c.Server.Public.PublicURL) >= 7 && c.Server.Public.PublicURL[:7] == "http://" {
		w = append(w, "server.public.public_url is http:// while server.public.hsts is true")
	}
	if g, b := c.Shutdown.SupervisorGrace, c.ShutdownBudget(); g > 0 && b > g {
		w = append(w, fmt.Sprintf("worst-case shutdown %s (every stage at its cap) exceeds shutdown.supervisor_grace %s: the supervisor may SIGKILL mid-flush", b, g))
	}
	return w
}

// ShutdownBudget is 02's worst case for the configured budgets: drain delay + server grace + store + telemetry + the refresh stage's
// stop (design 16 section 8.3). It is what startup logs and compares with shutdown.supervisor_grace.
func (c Config) ShutdownBudget() time.Duration { return c.Shutdown.WorstCase(c.RefreshStop()) }
