package httpx

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/cantara/source-code-portal/internal/platform/logx"
	"github.com/cantara/source-code-portal/internal/platform/secret"
)

// Class describes how one request is treated. cmd/scp builds Classify from the
// routes.Table; httpx never sees route names.
type Class struct {
	RateClass string      // key into Deps.Limiters; "" means not limited
	Headers   HeaderClass // default security headers
	BodyLimit int64       // 0 means Deps.BodyLimit
	Quiet     bool        // probes: no span, access log at debug
}

// Hooks are the response renderers cmd/scp injects from httpapi, so httpx imports neither httpapi nor routes.
// The first three are exactly the values 06-httpapi section 2.5 says cmd/scp must pass.
type Hooks struct {
	// Recovered renders the 500 after a handler panic and logs it (httpapi.Recovered(log)). It receives a request
	// whose Pattern is the matched route pattern.
	Recovered func(w http.ResponseWriter, r *http.Request, rec any)
	// RateLimited renders the 429 with Retry-After (httpapi.RateLimitedFunc()).
	RateLimited func(w http.ResponseWriter, r *http.Request, retry time.Duration)
	// DenyCrossOrigin is the http.CrossOriginProtection deny handler (httpapi.DenyCrossOrigin()).
	DenyCrossOrigin http.Handler
	// Unauthorized renders the admin bearer-token 401. Optional: nil falls back to a minimal problem+json written here.
	Unauthorized func(w http.ResponseWriter, r *http.Request)
}

// Deps is everything the chains need.
type Deps struct {
	Log            *slog.Logger
	MeterProvider  metric.MeterProvider
	TracerProvider trace.TracerProvider
	Propagator     propagation.TextMapPropagator
	Clients        *ClientIPResolver
	Classify       func(*http.Request) Class
	Hooks          Hooks
	Limiters       map[string]*Limiter
	BodyLimit      int64    // default request body cap, 1 MiB
	PublicURL      string   // origin trusted by the CSRF check; may be empty
	HSTS           bool     // honoured only on HTTPS requests
	CSRFBypass     []string // ServeMux patterns exempt from CrossOriginProtection
	AdminHosts     []string // admin chain only: accepted Host header values (DNS-rebinding guard); empty = no check
	// AfterClientIP are extra middlewares installed directly after client-IP resolution and before the security
	// headers. cmd/scp puts access.Middleware here (identity is established after the peer is known and before
	// anything is rate limited or answered); httpx cannot import access.
	AfterClientIP []func(http.Handler) http.Handler
}

type ctxKey struct{}

// state is the per-request record shared by the outer middlewares: the access
// log sits outside the client-IP middleware but still prints the client IP.
type state struct {
	id, inbound string
	ip          string
	pattern     string // the matched ServeMux pattern, captured by routeCapture
	cls         Class  // memoised Deps.Classify result (one private-mux lookup per request)
	clsDone     bool
}

// classOf returns the request's Class, computed once per request.
func classOf(d Deps, r *http.Request) Class {
	st := stateOf(r)
	if !st.clsDone {
		if d.Classify != nil {
			st.cls = d.Classify(r)
		}
		st.clsDone = true
	}
	return st.cls
}

// PublicChain wraps the public mux. Outermost first: request id, access log (whose writer also applies the class's default
// security headers, so every response carries them, including a recovered 500 and anything an AfterClientIP middleware writes),
// recover, otelhttp, body limit, client IP, AfterClientIP, rate limit, cross-origin protection, route capture.
func PublicChain(d Deps) func(http.Handler) http.Handler {
	mustDeps(d, true)
	return func(next http.Handler) http.Handler {
		h := routeCapture(next)
		h = crossOrigin(d, h)
		h = rateLimit(d, h)
		for i := len(d.AfterClientIP) - 1; i >= 0; i-- {
			h = d.AfterClientIP[i](h)
		}
		h = clientIP(d, h)
		h = bodyLimit(d, h)
		h = tracing(d, h)
		h = recoverer(d, h)
		h = accessLog(d, h, nil)
		h = requestID(d, h)
		return h
	}
}

// AdminChain wraps the admin mux: request id, recover, access log, body limit,
// client IP, headers (API class), optional bearer token. No rate limit, no CSRF
// check (the admin API is authenticated by network and bearer, and mutations
// additionally demand X-SCP-Request in their handlers).
func AdminChain(d Deps, token secret.Secret) func(http.Handler) http.Handler {
	mustDeps(d, false)
	return func(next http.Handler) http.Handler {
		h := routeCapture(next)
		if !token.IsZero() {
			h = bearer(d, token, h)
		}
		if len(d.AdminHosts) > 0 {
			h = hostGuard(d, h)
		}
		h = clientIP(d, h)
		h = bodyLimit(d, h)
		h = recoverer(d, h)
		api := HeadersAPI
		h = accessLog(d, h, &api)
		h = requestID(d, h)
		return h
	}
}

func requestID(_ Deps, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		st := &state{id: newRequestID(), inbound: inbound(r)}
		ctx := context.WithValue(r.Context(), ctxKey{}, st)
		ctx = logx.WithRequestID(ctx, st.id)
		w.Header().Set(HeaderRequestID, st.id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func stateOf(r *http.Request) *state {
	if st, ok := r.Context().Value(ctxKey{}).(*state); ok {
		return st
	}
	return &state{}
}

// recorder is the outermost response writer (created by accessLog). It captures status and size and applies the
// class's default security headers exactly once, at the first final WriteHeader or Write, only for headers the handler
// has not set itself (webapp sets its own document CSP this way). Because it sits outside recover and every other
// middleware, a recovered 500, a 429, a 403 and anything an AfterClientIP middleware writes carry the headers too.
type recorder struct {
	http.ResponseWriter
	status   int
	bytes    int64
	wrote    bool
	defaults func(http.Header) // nil: set nothing
}

func (w *recorder) apply() {
	if f := w.defaults; f != nil {
		w.defaults = nil
		f(w.ResponseWriter.Header())
	}
}

func (w *recorder) WriteHeader(code int) {
	if code >= 100 && code < 200 && code != http.StatusSwitchingProtocols { // interim response: the final one is still to come
		w.ResponseWriter.WriteHeader(code)
		return
	}
	if w.wrote {
		return
	}
	w.wrote = true
	w.status = code
	w.apply()
	w.ResponseWriter.WriteHeader(code)
}

func (w *recorder) Write(b []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += int64(n)
	return n, err
}

func (w *recorder) Flush() {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

// finish applies the defaults to a handler that returned without writing (net/http then sends an implicit 200).
func (w *recorder) finish() {
	if !w.wrote {
		w.apply()
	}
}

func (w *recorder) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *recorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return http.NewResponseController(w.ResponseWriter).Hijack()
}

func asRecorder(w http.ResponseWriter) *recorder {
	if rw, ok := w.(*recorder); ok {
		return rw
	}
	return nil
}

func recoverer(d Deps, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			v := recover()
			if v == nil {
				return
			}
			if v == http.ErrAbortHandler {
				panic(v)
			}
			d.Log.ErrorContext(r.Context(), "panic in handler",
				slog.Any("panic", v), slog.String("stack", string(debug.Stack())))
			if rw := asRecorder(w); rw != nil && rw.wrote {
				return // headers are out; the connection will be cut by the server
			}
			rr := r.WithContext(r.Context())
			rr.Pattern = stateOf(r).pattern // the matched pattern lives in the state, the mux set it on an inner copy
			d.Hooks.Recovered(w, rr, v)
		}()
		next.ServeHTTP(w, r)
	})
}

// accessLog creates the recorder and writes the one access record after the response. fixed, when set, is the header
// class of every response (admin chain); otherwise the class comes from Deps.Classify.
func accessLog(d Deps, next http.Handler, fixed *HeaderClass) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &recorder{ResponseWriter: w, status: http.StatusOK}
		rw.defaults = func(h http.Header) {
			hc := HeadersNone
			if fixed != nil {
				hc = *fixed
			} else {
				hc = classOf(d, r).Headers
			}
			applyDefaults(h, hc, fixed == nil && d.HSTS && d.Clients.HTTPS(r))
		}
		next.ServeHTTP(rw, r)
		rw.finish()
		st := stateOf(r)
		lvl := slog.LevelInfo
		switch {
		case rw.status >= 500:
			lvl = slog.LevelError
		case classOf(d, r).Quiet:
			lvl = slog.LevelDebug
		}
		pattern := st.pattern
		if pattern == "" {
			pattern = "unmatched"
		}
		attrs := []slog.Attr{
			slog.String("method", r.Method),
			slog.String("route", pattern),
			slog.Int(logx.KeyStatus, rw.status),
			slog.Int64(logx.KeyDurationMS, time.Since(start).Milliseconds()),
			slog.Int64("bytes", rw.bytes),
			slog.String(logx.KeyClientIP, st.ip),
		}
		if st.inbound != "" {
			attrs = append(attrs, slog.String(KeyInboundID, st.inbound))
		}
		d.Log.LogAttrs(r.Context(), lvl, "http request", attrs...)
	})
}

func tracing(d Deps, next http.Handler) http.Handler {
	opts := []otelhttp.Option{
		otelhttp.WithMeterProvider(d.MeterProvider),
		otelhttp.WithTracerProvider(d.TracerProvider),
		otelhttp.WithPropagators(d.Propagator),
		// Callers are anonymous: their trace context becomes a link, never a parent.
		otelhttp.WithPublicEndpointFn(func(*http.Request) bool { return true }),
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string { return r.Method }),
		otelhttp.WithFilter(func(r *http.Request) bool { return !classOf(d, r).Quiet }),
	}
	return otelhttp.NewHandler(next, "http.server", opts...)
}

func bodyLimit(d Deps, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := d.BodyLimit
		if c := classOf(d, r); c.BodyLimit > 0 {
			n = c.BodyLimit
		}
		if n > 0 && r.Body != nil && r.Body != http.NoBody {
			r.Body = http.MaxBytesReader(w, r.Body, n)
		}
		next.ServeHTTP(w, r)
	})
}

func clientIP(d Deps, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip, _ := d.Clients.Resolve(r)
		s := ""
		if ip.IsValid() {
			s = ip.String()
		}
		stateOf(r).ip = s
		next.ServeHTTP(w, r.WithContext(logx.WithClientIP(r.Context(), s)))
	})
}

func rateLimit(d Deps, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c := classOf(d, r); c.RateClass != "" {
			if lim := d.Limiters[c.RateClass]; lim != nil {
				key := RateKey(addrOf(stateOf(r).ip))
				if !lim.Allow(key) { // the limiter counts scp_ratelimit_rejected_total{class} itself
					d.Hooks.RateLimited(w, r, lim.RetryAfter(key))
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

func crossOrigin(d Deps, next http.Handler) http.Handler {
	cop := http.NewCrossOriginProtection()
	if d.PublicURL != "" {
		if u, err := url.Parse(d.PublicURL); err == nil {
			_ = cop.AddTrustedOrigin(u.Scheme + "://" + u.Host)
		}
	}
	for _, p := range d.CSRFBypass {
		cop.AddInsecureBypassPattern(p)
	}
	cop.SetDenyHandler(d.Hooks.DenyCrossOrigin)
	return cop.Handler(next)
}

// bearer demands "Authorization: Bearer <token>" (scheme case-insensitive, token compared in constant time). Probe routes
// (Class.Quiet) are exempt: the same routes are unauthenticated on the public listener, so nothing more is exposed.
func bearer(d Deps, token secret.Secret, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if classOf(d, r).Quiet {
			next.ServeHTTP(w, r)
			return
		}
		const prefix = "Bearer "
		h := r.Header.Get("Authorization")
		if len(h) <= len(prefix) || !strings.EqualFold(h[:len(prefix)], prefix) || !token.EqualString(h[len(prefix):]) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="scp-admin"`)
			if d.Hooks.Unauthorized != nil {
				d.Hooks.Unauthorized(w, r)
				return
			}
			problemJSON(w, r, http.StatusUnauthorized, "unauthorized", "Unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// hostGuard refuses an admin request whose Host header is not in Deps.AdminHosts (421): a DNS-rebinding page in an
// operator's browser carries the attacker's host name. Compared case-insensitively, with or without the port.
func hostGuard(d Deps, next http.Handler) http.Handler {
	ok := map[string]bool{}
	for _, h := range d.AdminHosts {
		ok[strings.ToLower(h)] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := strings.ToLower(r.Host)
		hn := host
		if h, _, err := net.SplitHostPort(host); err == nil {
			hn = h
		}
		if !classOf(d, r).Quiet && !ok[host] && !ok[hn] {
			problemJSON(w, r, http.StatusMisdirectedRequest, "forbidden", "Host not allowed")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// problemJSON writes the minimal problem document httpx itself needs (everything else is rendered by httpapi through Hooks).
func problemJSON(w http.ResponseWriter, r *http.Request, status int, code, title string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	b, _ := json.Marshal(map[string]any{"type": "urn:scp:problem:" + code, "title": title, "status": status, "code": code, "instance": "urn:uuid:" + logx.RequestID(r.Context())})
	_, _ = w.Write(b)
}

// RetryAfterSeconds formats a Retry-After value (whole seconds, at least 1).
func RetryAfterSeconds(d time.Duration) string {
	s := int((d + time.Second - 1) / time.Second)
	return strconv.Itoa(max(s, 1))
}

// ErrBodyTooLarge reports whether err came from a MaxBytesReader.
func ErrBodyTooLarge(err error) bool {
	var mbe *http.MaxBytesError
	return errors.As(err, &mbe)
}

func addrOf(s string) netip.Addr {
	a, _ := netip.ParseAddr(s)
	return a
}

// routeCapture is the innermost wrapper. The ServeMux sets r.Pattern on the
// *http.Request it receives; every middleware above this one that calls
// WithContext hands the mux a copy, so the outer layers never see the pattern.
// After the mux returns, this wrapper publishes it to the access log, to the
// otelhttp metric labels and to the span as http.route (a bounded value; raw
// paths never become labels).
func routeCapture(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() { // also on panic: the recoverer and the access log read the pattern from the state
			pat := r.Pattern
			if pat == "" {
				return
			}
			stateOf(r).pattern = pat
			route := attribute.String("http.route", routeOnly(pat))
			if l, ok := otelhttp.LabelerFromContext(r.Context()); ok {
				l.Add(route)
			}
			if sp := trace.SpanFromContext(r.Context()); sp.IsRecording() {
				sp.SetAttributes(route)
				sp.SetName(pat)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// routeOnly strips the method from "GET /api/v1/x/{id}".
func routeOnly(pat string) string {
	if i := strings.IndexByte(pat, ' '); i >= 0 {
		return pat[i+1:]
	}
	return pat
}

// mustDeps fails at construction, not at the first request, when a mandatory dependency is missing: a nil hook would
// otherwise be a nil-pointer panic inside the panic handler.
func mustDeps(d Deps, public bool) {
	var missing []string
	for name, nilp := range map[string]bool{
		"Log": d.Log == nil, "Clients": d.Clients == nil, "Hooks.Recovered": d.Hooks.Recovered == nil,
		"MeterProvider": public && d.MeterProvider == nil, "TracerProvider": public && d.TracerProvider == nil,
		"Propagator": public && d.Propagator == nil, "Hooks.RateLimited": public && d.Hooks.RateLimited == nil,
		"Hooks.DenyCrossOrigin": public && d.Hooks.DenyCrossOrigin == nil,
	} {
		if nilp {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		panic("httpx: missing Deps: " + strings.Join(missing, ", "))
	}
}
