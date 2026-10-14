package access

import (
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/cantara/source-code-portal/internal/httpapi/authz"
	"github.com/cantara/source-code-portal/internal/scm"
)

// Metric names (constants next to their instruments, G11).
const (
	MetricSpoof    = "scp_identity_header_spoof_total"
	MetricRequests = "scp_access_requests_total"
)

// Deps are the injected collaborators (G11: Meter from cmd/scp; no global accessors).
type Deps struct {
	Logger *slog.Logger
	Meter  metric.Meter
}

// Access is the optional trusted-proxy identity service. Construct with New; safe for concurrent use.
type Access struct {
	cfg      Config
	id       *identifier
	az       *authorizer
	log      *slog.Logger
	spoof    metric.Int64Counter
	requests metric.Int64Counter
	spoofN   atomic.Int64
	logAt    sync.Map // Reason -> *atomic.Int64: unix nanoseconds of the last warn line for that reason
	now      func() time.Time
}

// logEvery bounds the warn lines for dropped identity headers: one per reason per interval. The counter
// scp_identity_header_spoof_total carries the volume; this log line exists to tell an operator which reason fires.
// (The rate limiter sits AFTER this middleware in the chain and therefore cannot bound these lines.)
const logEvery = time.Minute

func (a *Access) shouldLog(r Reason) bool {
	v, _ := a.logAt.LoadOrStore(r, new(atomic.Int64))
	last := v.(*atomic.Int64)
	now := a.now().UnixNano()
	prev := last.Load()
	return (prev == 0 || now-prev >= int64(logEvery)) && last.CompareAndSwap(prev, now)
}

// New validates cfg and builds the service. It returns the same errors as cfg.Validate.
func New(cfg Config, d Deps) (*Access, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	var prefixes = []netipPrefix(nil)
	if cfg.Mode == ModeTrustedProxy {
		p, err := ParsePrefixes(cfg.TrustedProxies, cfg.AllowPublicProxies)
		if err != nil {
			return nil, err
		}
		prefixes = p
	}
	a := &Access{cfg: cfg, id: newIdentifier(cfg, prefixes), az: newAuthorizer(cfg), log: d.Logger, now: time.Now}
	if a.log == nil {
		a.log = slog.New(slog.DiscardHandler)
	}
	if d.Meter != nil {
		var err error
		if a.spoof, err = d.Meter.Int64Counter(MetricSpoof, metric.WithUnit("{request}"),
			metric.WithDescription("Requests whose identity headers were dropped, by reason.")); err != nil {
			return nil, err
		}
		if a.requests, err = d.Meter.Int64Counter(MetricRequests, metric.WithUnit("{request}"),
			metric.WithDescription("Public-listener requests by principal kind and view class.")); err != nil {
			return nil, err
		}
	}
	return a, nil
}

// Authorizer is the httpapi/authz.Authorizer for the public listener (public-only in mode public).
func (a *Access) Authorizer() authz.Authorizer { return a.az }

// Operator returns the authorizer for the admin listener: the operator sees everything (the listener is network-restricted).
func Operator() authz.Authorizer { return operator{} }

// Middleware establishes the principal. Install it on the PUBLIC chain only, directly after client-IP resolution
// (identity-trust logic never runs on the admin listener, ADR-025).
func (a *Access) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, reason := a.id.identify(r.RemoteAddr, r.Header)
		if reason != ReasonNone {
			a.spoofN.Add(1)
			if a.spoof != nil {
				a.spoof.Add(r.Context(), 1, metric.WithAttributes(attribute.String("reason", string(reason))))
			}
			if a.shouldLog(reason) {
				a.log.LogAttrs(r.Context(), slog.LevelWarn, "identity headers dropped (sampled: one line per reason per minute; see "+MetricSpoof+")", slog.String("reason", string(reason)))
			}
		}
		class := scm.ViewPublic
		if !p.Anonymous && a.az.CanView(r.Context(), p, scm.VisPrivate) {
			class = scm.ViewAll
		}
		if a.requests != nil {
			kind := "anonymous"
			if !p.Anonymous {
				kind = "authenticated"
			}
			a.requests.Add(r.Context(), 1, metric.WithAttributes(attribute.String("principal", kind),
				attribute.String("class", map[scm.ViewClass]string{scm.ViewPublic: "public", scm.ViewAll: "all"}[class])))
		}
		ctx := authz.WithPrincipal(r.Context(), p)
		if class == scm.ViewAll {
			w = &privateWriter{ResponseWriter: w} // never let a shared cache store a response that may contain non-public data
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Info is the /status contribution.
type Info struct {
	Mode          Mode
	TrustedRanges int
	SpoofDropped  int64
	ProxySecret   bool
	Warnings      []string // stable codes shown in /status: "no_proxy_secret", "broad_trusted_range"
}

func (a *Access) Info() Info {
	in := Info{Mode: a.cfg.Mode, TrustedRanges: len(a.id.trusted), SpoofDropped: a.spoofN.Load(), ProxySecret: a.id.haveSec}
	if a.cfg.Mode == ModeTrustedProxy {
		if !a.id.haveSec {
			in.Warnings = append(in.Warnings, "no_proxy_secret")
		}
		for _, p := range a.id.trusted {
			// wider than a /16 (IPv4) or /48 (IPv6): any compromised neighbour could present identity headers
			if (p.Addr().Is4() && p.Bits() < 16) || (p.Addr().Is6() && !p.Addr().Is4In6() && p.Bits() < 48) {
				in.Warnings = append(in.Warnings, "broad_trusted_range")
				break
			}
		}
	}
	return in
}

// SubjectHash is the only form of a subject that reaches logs: 8 hex chars of SHA-256, enough to correlate, not to identify.
func SubjectHash(subject string) string {
	h := sha256.Sum256([]byte(subject))
	return hex.EncodeToString(h[:4])
}

// privateWriter makes Cache-Control private on responses for principals that can see non-public repositories.
type privateWriter struct {
	http.ResponseWriter
	done bool
}

func (w *privateWriter) WriteHeader(code int) {
	if code >= 100 && code <= 199 && code != http.StatusSwitchingProtocols {
		w.ResponseWriter.WriteHeader(code) // informational (103 Early Hints): the final status still gets the rewrite
		return
	}
	if !w.done {
		w.done = true
		h := w.Header()
		cc := h.Get("Cache-Control")
		switch {
		case strings.Contains(cc, "private"), strings.Contains(cc, "no-store"):
		case strings.Contains(cc, "public"):
			h.Set("Cache-Control", strings.Replace(cc, "public", "private", 1))
		case cc == "":
			h.Set("Cache-Control", "private")
		default:
			h.Set("Cache-Control", "private, "+cc)
		}
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *privateWriter) Write(b []byte) (int, error) {
	if !w.done {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

func (w *privateWriter) Flush() {
	if !w.done {
		w.WriteHeader(http.StatusOK)
	}
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap lets http.ResponseController reach the real writer (SSE write deadlines).
func (w *privateWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
