package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/platform/logx"
)

// Stable machine codes (the `code` member). Features may add codes; these are the shared catalogue.
const (
	CodeValidationFailed     = "validation-failed"
	CodeInvalidCursor        = "invalid-cursor"
	CodeUnauthorized         = "unauthorized"
	CodeForbidden            = "forbidden"
	CodeNotFound             = "not-found"
	CodeMethodNotAllowed     = "method-not-allowed"
	CodeNotAcceptable        = "not-acceptable"
	CodePayloadTooLarge      = "payload-too-large"
	CodeUnsupportedMediaType = "unsupported-media-type"
	CodeRateLimited          = "rate-limited"
	CodeInternal             = "internal"
	CodeUpstreamUnavailable  = "upstream-unavailable"
	CodeNotReady             = "not-ready"
	// Added by the cross-design review (CD-ER-02, requirements R-06-1 of unit 15 and the catalogue of unit 07 section 7.5).
	CodeFeatureDisabled = "feature-disabled" // 404: the operation's feature is switched off
	CodeQueueFull       = "queue-full"       // 503 + Retry-After: a bounded queue refused work (job.ErrQueueFull)
	CodeRepoRenamed     = "repo-renamed"     // 308 + Location: the slug is an alias
	CodeUnhealthy       = "unhealthy"        // 503 + Retry-After: /livez failure (additive; the vocabulary is open)
	CodeOverloaded      = "overloaded"       // 503 + Retry-After: an admission limit (webhook body slots) is exhausted; not a job queue
	CodeRequestTimeout  = "request-timeout"  // 408: the request body was too slow (webhook body_timeout)
)

// TypeURI is the `type` member for a code. It is an identifier, not a dereferenceable URL.
func TypeURI(code string) string { return "urn:scp:problem:" + code }

type FieldError struct {
	Pointer string `json:"pointer"`
	Detail  string `json:"detail"`
}

// Problem is an RFC 9457 problem document. The SPA keys on Code, never on Detail.
type Problem struct {
	Type              string       `json:"type"`
	Title             string       `json:"title"`
	Status            int          `json:"status"`
	Detail            string       `json:"detail,omitempty"`
	Instance          string       `json:"instance,omitempty"`
	Code              string       `json:"code"`
	Errors            []FieldError `json:"errors,omitempty"`
	RetryAfterSeconds int          `json:"retryAfterSeconds,omitempty"`
	// Header carries response headers that belong to the problem (Allow, Retry-After, WWW-Authenticate and, for repo-renamed, Location only).
	Header http.Header `json:"-"`
}

func title(code string, status int) string {
	switch code {
	case CodeValidationFailed:
		return "Request validation failed"
	case CodeInvalidCursor:
		return "Invalid cursor"
	case CodeUnauthorized:
		return "Authentication required"
	case CodeForbidden:
		return "Forbidden"
	case CodeNotFound:
		return "Not found"
	case CodeMethodNotAllowed:
		return "Method not allowed"
	case CodeNotAcceptable:
		return "Not acceptable"
	case CodePayloadTooLarge:
		return "Payload too large"
	case CodeUnsupportedMediaType:
		return "Unsupported media type"
	case CodeRateLimited:
		return "Too many requests"
	case CodeInternal:
		return "Internal error"
	case CodeUpstreamUnavailable:
		return "Upstream unavailable"
	case CodeNotReady:
		return "Not ready"
	case CodeFeatureDisabled:
		return "Feature disabled"
	case CodeQueueFull:
		return "Queue full"
	case CodeRepoRenamed:
		return "Repository renamed"
	case CodeUnhealthy:
		return "Unhealthy"
	case CodeOverloaded:
		return "Overloaded"
	case CodeRequestTimeout:
		return "Request timeout"
	}
	return http.StatusText(status)
}

// New builds a problem with the standard title for code.
func New(status int, code, detail string) Problem {
	return Problem{Type: TypeURI(code), Title: title(code, status), Status: status, Code: code, Detail: detail}
}

func Validation(errs ...FieldError) Problem {
	p := New(http.StatusBadRequest, CodeValidationFailed, "one or more request parameters are invalid")
	p.Errors = errs
	return p
}
func InvalidCursor() Problem {
	p := New(http.StatusBadRequest, CodeInvalidCursor, "the cursor is malformed, expired for this query, or was not issued by this API")
	p.Errors = []FieldError{{Pointer: "#/query/cursor", Detail: "invalid cursor"}}
	return p
}
func Unauthorized() Problem { return New(http.StatusUnauthorized, CodeUnauthorized, "") }
func Forbidden(detail string) Problem {
	return New(http.StatusForbidden, CodeForbidden, detail)
}
func NotFound(detail string) Problem { return New(http.StatusNotFound, CodeNotFound, detail) }
func MethodNotAllowed(allow ...string) Problem {
	p := New(http.StatusMethodNotAllowed, CodeMethodNotAllowed, "method not allowed for this resource")
	a := append([]string(nil), allow...)
	sort.Strings(a)
	if len(a) > 0 {
		p.Header = http.Header{"Allow": {strings.Join(a, ", ")}}
	}
	return p
}
func NotAcceptable(offered ...string) Problem {
	return New(http.StatusNotAcceptable, CodeNotAcceptable, "available representations: "+strings.Join(offered, ", "))
}
func PayloadTooLarge(max int64) Problem {
	return New(http.StatusRequestEntityTooLarge, CodePayloadTooLarge, "request body exceeds "+strconv.FormatInt(max, 10)+" bytes")
}
func UnsupportedMediaType(want string) Problem {
	return New(http.StatusUnsupportedMediaType, CodeUnsupportedMediaType, "expected Content-Type "+want)
}
func withRetry(p Problem, d time.Duration) Problem {
	s := int((d + time.Second - 1) / time.Second)
	if s < 1 {
		s = 1
	}
	p.RetryAfterSeconds = s
	p.Header = http.Header{"Retry-After": {strconv.Itoa(s)}}
	return p
}

// WithRetry is the exported form of withRetry (R-06-2): it sets RetryAfterSeconds and the Retry-After header together.
func WithRetry(p Problem, d time.Duration) Problem { return withRetry(p, d) }

func FeatureDisabled(detail string) Problem {
	return New(http.StatusNotFound, CodeFeatureDisabled, detail)
}
func QueueFull(retry time.Duration) Problem {
	return withRetry(New(http.StatusServiceUnavailable, CodeQueueFull, "a bounded queue refused the work"), retry)
}
func Unhealthy(retry time.Duration) Problem {
	return withRetry(New(http.StatusServiceUnavailable, CodeUnhealthy, "a liveness check failed"), retry)
}

// RepoRenamed is the 308 answer for an alias slug (R-06-3): the only problem below 400 that WriteProblem accepts. Location is the
// current URL; the caller validates it (the portal path of the current slug).
func RepoRenamed(location string) Problem {
	p := New(http.StatusPermanentRedirect, CodeRepoRenamed, "the repository was renamed; follow Location")
	p.Header = http.Header{"Location": {location}}
	return p
}

func RateLimited(retry time.Duration) Problem {
	return withRetry(New(http.StatusTooManyRequests, CodeRateLimited, "rate limit exceeded"), retry)
}
func Internal() Problem { return New(http.StatusInternalServerError, CodeInternal, "") }
func UpstreamUnavailable(retry time.Duration) Problem {
	return withRetry(New(http.StatusServiceUnavailable, CodeUpstreamUnavailable, "the upstream is unavailable and no data is held"), retry)
}
func NotReady(retry time.Duration) Problem {
	return withRetry(New(http.StatusServiceUnavailable, CodeNotReady, "the portal is warming up"), retry)
}

var uuidRE = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
var idSafe = regexp.MustCompile(`^[A-Za-z0-9._~-]{1,64}$`)

// InstanceURN renders a request id as the problem `instance`: a urn:uuid when the id is a UUID, else urn:scp:request:<id>.
func InstanceURN(requestID string) string {
	switch {
	case requestID == "":
		return ""
	case uuidRE.MatchString(requestID):
		return "urn:uuid:" + strings.ToLower(requestID)
	case idSafe.MatchString(requestID):
		return "urn:scp:request:" + requestID
	}
	return ""
}

// WriteProblem writes p as application/problem+json with Cache-Control: no-store.
// It fills Type, Title and Instance when empty, clamps Status to 400..599 (308 is allowed for repo-renamed only), removes headers that
// describe a different representation, and writes no body for HEAD.
func WriteProblem(w http.ResponseWriter, r *http.Request, p Problem) {
	if (p.Status < 400 && !(p.Status == http.StatusPermanentRedirect && p.Code == CodeRepoRenamed)) || p.Status > 599 {
		p.Status = http.StatusInternalServerError
	}
	if p.Code == "" {
		p.Code = CodeInternal
	}
	if p.Type == "" {
		p.Type = TypeURI(p.Code)
	}
	if p.Title == "" {
		p.Title = http.StatusText(p.Status)
	}
	if p.Instance == "" {
		p.Instance = InstanceURN(logx.RequestID(r.Context()))
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(p); err != nil { // cannot happen for this struct; fail closed
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	h := w.Header()
	for _, k := range [...]string{"ETag", "Content-Length", "Content-Encoding", "Content-Range", "Content-Disposition", "Last-Modified", "Link", "Accept-Ranges"} {
		h.Del(k)
	}
	for _, k := range [...]string{"Allow", "Retry-After", "WWW-Authenticate", "Location"} {
		if v := p.Header.Values(k); len(v) > 0 {
			h[http.CanonicalHeaderKey(k)] = append([]string(nil), v...)
		}
	}
	h.Set("Content-Type", "application/problem+json")
	h.Set("X-Content-Type-Options", "nosniff") // every problem, whoever wrote the headers before (ProblemMux rewrites keep it)
	h.Set("Cache-Control", "no-store")
	h.Set("Content-Length", strconv.Itoa(buf.Len()))
	w.WriteHeader(p.Status)
	if r.Method != http.MethodHead {
		_, _ = w.Write(buf.Bytes())
	}
}

// ProblemError lets a handler return a problem through the strict server's error path.
type ProblemError struct {
	Problem Problem
	Cause   error // logged by StrictErrors, never rendered
}

func (e *ProblemError) Error() string {
	if e.Cause != nil {
		return e.Problem.Code + ": " + e.Cause.Error()
	}
	return e.Problem.Code
}
func (e *ProblemError) Unwrap() error { return e.Cause }

// Err wraps p as an error; Wrap also records the internal cause.
func Err(p Problem) error               { return &ProblemError{Problem: p} }
func Wrap(p Problem, cause error) error { return &ProblemError{Problem: p, Cause: cause} }

func AsProblem(err error) (Problem, bool) {
	var pe *ProblemError
	if errors.As(err, &pe) {
		return pe.Problem, true
	}
	return Problem{}, false
}

// StrictErrors returns the two error handlers for oapi-codegen's StrictHTTPServerOptions
// (RequestErrorHandlerFunc, ResponseErrorHandlerFunc). Both render problem+json; neither leaks error text.
func StrictErrors(log *slog.Logger) (request, response func(http.ResponseWriter, *http.Request, error)) {
	request = func(w http.ResponseWriter, r *http.Request, err error) {
		var mbe *http.MaxBytesError
		var syn *json.SyntaxError
		switch {
		case errors.As(err, &mbe):
			WriteProblem(w, r, PayloadTooLarge(mbe.Limit))
		case errors.As(err, &syn):
			WriteProblem(w, r, Validation(FieldError{Pointer: "#/body", Detail: "malformed JSON"}))
		default: // library text (field names, Go types, client input) is never rendered
			WriteProblem(w, r, Validation(FieldError{Pointer: "#/body", Detail: "request body could not be decoded"}))
		}
	}
	response = func(w http.ResponseWriter, r *http.Request, err error) {
		if p, ok := AsProblem(err); ok {
			if p.Status >= 500 && log != nil {
				log.ErrorContext(r.Context(), "api handler error", "err", err, "pattern", r.Pattern, "code", p.Code)
			}
			WriteProblem(w, r, p)
			return
		}
		if errors.Is(err, context.Canceled) && r.Context().Err() != nil {
			return // the client is gone; nothing to write
		}
		if log != nil {
			log.ErrorContext(r.Context(), "api handler error", "err", err, "pattern", r.Pattern)
		}
		WriteProblem(w, r, Internal())
	}
	return request, response
}

// BindingError is for oapi-codegen's StdHTTPServerOptions.ErrorHandlerFunc (parameter binding failures; the
// request validator normally rejects these first, so this is the safety net). The generated error types are
// declared once per generated package (commitsapi.InvalidParamFormatError, ...), so httpapi cannot type-switch
// on them; the detail is therefore fixed text and the library message is never rendered.
func BindingError(w http.ResponseWriter, r *http.Request, err error) {
	WriteProblem(w, r, Validation(FieldError{Pointer: "#/", Detail: "a request parameter could not be bound"}))
}

// Recovered returns the hook for httpx's recover middleware.
func Recovered(log *slog.Logger) func(w http.ResponseWriter, r *http.Request, rec any) {
	return func(w http.ResponseWriter, r *http.Request, rec any) {
		if log != nil {
			log.ErrorContext(r.Context(), "panic in handler", "panic", rec, "pattern", r.Pattern)
		}
		WriteProblem(w, r, Internal())
	}
}

// RateLimitedFunc is the hook for httpx's rate limiter.
func RateLimitedFunc() func(w http.ResponseWriter, r *http.Request, retry time.Duration) {
	return func(w http.ResponseWriter, r *http.Request, retry time.Duration) {
		WriteProblem(w, r, RateLimited(retry))
	}
}

// DenyCrossOrigin is the deny handler for http.CrossOriginProtection.
func DenyCrossOrigin() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		WriteProblem(w, r, Forbidden("cross-origin request rejected"))
	})
}
