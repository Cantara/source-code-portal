package httpx

import (
	"fmt"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/platform/secret"
)

// Config is the "server" section of the config file. Platform owns it.
type Config struct {
	Public   PublicConfig   `yaml:"public" jsonschema_description:"Public listener: SPA, API, badges, media, webhooks, probes."`
	Admin    AdminConfig    `yaml:"admin" jsonschema_description:"Admin listener: metrics, pprof and operator endpoints. Bind to a restricted network."`
	Timeouts TimeoutsConfig `yaml:"timeouts" jsonschema_description:"http.Server timeouts shared by both listeners."`
}

type PublicConfig struct {
	Listen    string `yaml:"listen" jsonschema_description:"Listen address host:port. An empty host binds every interface."`
	PublicURL string `yaml:"public_url" jsonschema_description:"External https URL of the portal, used for absolute links, webhook URLs and the CSRF trusted origin. Empty means relative links only."`
	HSTS      bool   `yaml:"hsts" jsonschema_description:"Send Strict-Transport-Security on HTTPS requests. Leave false when the TLS-terminating proxy owns HSTS."`
	// TrustedProxies are the peers whose X-Forwarded-For / X-Forwarded-Proto are believed for the client IP and rate-limit keys.
	// It is a different (weaker) statement than access.trusted_proxies, which authenticates users, and neither defaults to the other.
	TrustedProxies []string `yaml:"trusted_proxies" jsonschema_description:"CIDRs or addresses of reverse proxies whose X-Forwarded-For and X-Forwarded-Proto are believed for the client IP. 0.0.0.0/0 and ::/0 are refused. Independent of access.trusted_proxies."`
}

type AdminConfig struct {
	Listen       string        `yaml:"listen" jsonschema_description:"Listen address host:port. Never expose publicly."`
	BearerToken  secret.Secret `yaml:"bearer_token" jsonschema_description:"Optional bearer token required on every admin request except the probes (file reference or SCP_SERVER_ADMIN_BEARER_TOKEN[_FILE]). Empty means network restriction only."`
	AllowedHosts []string      `yaml:"allowed_hosts" jsonschema_description:"Optional Host header allow-list for the admin listener (host or host:port, case-insensitive); a request with another Host gets 421. A defence against DNS rebinding from an operator's browser. Empty means no check."`
}

type TimeoutsConfig struct {
	ReadHeader      time.Duration `yaml:"read_header" scp:"min=100ms,max=1m" jsonschema_description:"Time allowed to read request headers."`
	Read            time.Duration `yaml:"read" scp:"min=1s,max=10m" jsonschema_description:"Time allowed to read the whole request."`
	Write           time.Duration `yaml:"write" scp:"min=1s,max=10m" jsonschema_description:"Time allowed to write the response. Streaming handlers extend it per response."`
	Idle            time.Duration `yaml:"idle" scp:"min=1s,max=1h" jsonschema_description:"Keep-alive idle timeout."`
	MaxHeaderBytes  int64         `yaml:"max_header_bytes" scp:"unit=bytes,min=1KiB,max=1MiB" jsonschema_description:"Maximum request header size."`
	MaxHeaderValues int           `yaml:"max_header_values" jsonschema:"minimum=16,maximum=4096" jsonschema_description:"Maximum number of header values in one request (Go 1.27 MaxHeaderValueCount)."`
}

// Rate is a token bucket written as one scalar: "120/min burst 60", "5/s burst 20", "30/min" (burst defaults to
// the per-minute figure, at least 1). It implements encoding.TextUnmarshaler so a feature, or the config
// machinery, can decode it from YAML, an environment variable or a --set flag without a special case.
type Rate struct {
	PerMinute int // steady refill, requests per minute
	Burst     int // bucket capacity
}

var rateRE = regexp.MustCompile(`^([0-9]{1,7})/(s|min|h)(?: burst ([0-9]{1,7}))?$`)

func (r *Rate) UnmarshalText(b []byte) error {
	m := rateRE.FindStringSubmatch(strings.TrimSpace(string(b)))
	if m == nil {
		return fmt.Errorf("rate %q must look like 120/min burst 60 (units s, min, h)", b)
	}
	n, _ := strconv.Atoi(m[1])
	switch m[2] {
	case "s":
		n *= 60
	case "h":
		if n%60 != 0 { // PerMinute is an integer: 30/h cannot be represented and must not silently become 1/min
			return fmt.Errorf("rate %q: per-hour rates must be a multiple of 60 (use /min)", b)
		}
		n /= 60
	}
	burst := n
	if m[3] != "" {
		burst, _ = strconv.Atoi(m[3])
	}
	if n < 1 || burst < 1 {
		return fmt.Errorf("rate %q: both figures must be at least 1", b)
	}
	r.PerMinute, r.Burst = n, burst
	return nil
}

func (r Rate) MarshalText() ([]byte, error) {
	return []byte(fmt.Sprintf("%d/min burst %d", r.PerMinute, r.Burst)), nil
}

func (r Rate) String() string { t, _ := r.MarshalText(); return string(t) }

// RateConfig is the "ratelimit" section.
type RateConfig struct {
	Badges         Rate          `yaml:"badges" jsonschema:"pattern=^[0-9]+/(s|min|h)( burst [0-9]+)?$" jsonschema_description:"Class badge: GET /badges/*."`
	API            Rate          `yaml:"api" jsonschema:"pattern=^[0-9]+/(s|min|h)( burst [0-9]+)?$" jsonschema_description:"Class api: /api/v1/*."`
	MediaExt       Rate          `yaml:"media_ext" jsonschema:"pattern=^[0-9]+/(s|min|h)( burst [0-9]+)?$" jsonschema_description:"Class media_ext: /media/ext/* (external image proxy)."`
	CSPReports     Rate          `yaml:"csp_reports" jsonschema:"pattern=^[0-9]+/(s|min|h)( burst [0-9]+)?$" jsonschema_description:"Class csp: POST /csp-reports."`
	WebhookInvalid Rate          `yaml:"webhook_invalid" jsonschema:"pattern=^[0-9]+/(s|min|h)( burst [0-9]+)?$" jsonschema_description:"Invalid webhook signatures allowed per client IP before 429. Valid HMACs are never limited."`
	SSEPerIP       int           `yaml:"sse_per_ip" jsonschema:"minimum=1,maximum=1000" jsonschema_description:"Concurrent SSE streams per client IP."`
	MaxKeys        int           `yaml:"max_keys" jsonschema:"minimum=1000,maximum=5000000" jsonschema_description:"Upper bound on tracked client keys per limiter class; excess keys share an overflow bucket."`
	IdleTTL        time.Duration `yaml:"idle_ttl" scp:"min=10s,max=24h" jsonschema_description:"A key idle for this long is evicted by the sweeper."`
}

// Validate implements config.Validator (builtin-typed signature, no config import).
func (c Config) Validate(report func(path, msg string)) {
	if _, err := ParseTrusted(c.Public.TrustedProxies); err != nil {
		report("public.trusted_proxies", err.Error())
	}
	if c.Public.PublicURL != "" {
		u, err := url.Parse(c.Public.PublicURL)
		switch {
		case err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http"):
			report("public.public_url", "must be an absolute http(s) URL")
		case u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/"):
			report("public.public_url", "must be an origin only: no userinfo, path, query or fragment")
		}
	}
	for path, addr := range map[string]string{"public.listen": c.Public.Listen, "admin.listen": c.Admin.Listen} {
		if _, err := netip.ParseAddrPort(normalizeListen(addr)); err != nil {
			report(path, "must be host:port, got "+addr)
		}
	}
	for _, h := range c.Admin.AllowedHosts {
		if strings.TrimSpace(h) == "" || strings.ContainsAny(h, " /@") {
			report("admin.allowed_hosts", fmt.Sprintf("%q is not a host or host:port", h))
		}
	}
	if c.Public.Listen == c.Admin.Listen && !strings.HasSuffix(c.Public.Listen, ":0") { // ":0" asks for an ephemeral port (tests)
		report("admin.listen", "must differ from public.listen")
	}
	if c.Timeouts.ReadHeader > c.Timeouts.Read {
		report("timeouts.read_header", "must not exceed timeouts.read")
	}
}

// Validate for RateConfig: a burst below one second of refill makes no sense.
func (c RateConfig) Validate(report func(path, msg string)) {
	for name, r := range map[string]Rate{"badges": c.Badges, "api": c.API, "media_ext": c.MediaExt, "csp_reports": c.CSPReports, "webhook_invalid": c.WebhookInvalid} {
		if float64(r.Burst) < float64(r.PerMinute)/60 {
			report(name, "burst must be at least one second of the steady rate")
		}
	}
}

// normalizeListen turns ":9090" into "0.0.0.0:9090" so ParseAddrPort accepts it;
// host names are not accepted (listeners bind literal addresses).
func normalizeListen(s string) string {
	if len(s) > 0 && s[0] == ':' {
		return "0.0.0.0" + s
	}
	return s
}
