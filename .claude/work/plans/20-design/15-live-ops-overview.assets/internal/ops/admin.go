package ops

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/cantara/source-code-portal/internal/httpapi"
	"github.com/cantara/source-code-portal/internal/ops/opsapi"
)

var jobKeyRE = regexp.MustCompile(`^[a-z][a-z0-9._-]*/[^\s]+$`)

const maxRetryBody = 4 << 10

// ValidJobKey reports whether k has the shape of an engine key: kind, a slash, then one or more parts without
// whitespace or control characters, at most 512 bytes.
func ValidJobKey(k string) bool {
	if len(k) < 3 || len(k) > 512 || !jobKeyRE.MatchString(k) {
		return false
	}
	for _, r := range k {
		if unicode.IsControl(r) || r == '\\' {
			return false
		}
	}
	return true
}

// RetryHandler serves api.admin.retry (admin listener only). Order of checks: media type, CSRF header, body size,
// strict JSON decode, key shape, then the Retrier. Every outcome is audit-logged and counted.
func RetryHandler(rt Retrier, log *slog.Logger, count func(result string)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fail := func(result string, p httpapi.Problem) {
			count(result)
			log.LogAttrs(r.Context(), slog.LevelWarn, "admin action refused", slog.String("action", "retry"), slog.String("result", result), slog.String("remote", r.RemoteAddr))
			httpapi.WriteProblem(w, r, p)
		}
		if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
			fail("unsupported_media_type", httpapi.Problem{Status: 415, Code: httpapi.CodeUnsupportedMediaType, Title: "Unsupported media type", Detail: "Content-Type must be application/json"})
			return
		}
		if r.Header.Get("X-SCP-Request") != "1" {
			fail("forbidden", httpapi.Problem{Status: 403, Code: httpapi.CodeForbidden, Title: "Forbidden", Detail: "header X-SCP-Request: 1 is required"})
			return
		}
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRetryBody))
		dec.DisallowUnknownFields()
		var req opsapi.RetryJobRequest
		if err := dec.Decode(&req); err != nil {
			var mbe *http.MaxBytesError
			if errors.As(err, &mbe) {
				fail("too_large", httpapi.Problem{Status: 413, Code: httpapi.CodePayloadTooLarge, Title: "Payload too large"})
				return
			}
			fail("invalid", httpapi.Problem{Status: 400, Code: httpapi.CodeValidationFailed, Title: "Invalid request", Errors: []httpapi.FieldError{{Pointer: "#", Detail: "body must be a JSON object with a string key"}}})
			return
		}
		if _, err := dec.Token(); !errors.Is(err, io.EOF) {
			fail("invalid", httpapi.Problem{Status: 400, Code: httpapi.CodeValidationFailed, Title: "Invalid request", Errors: []httpapi.FieldError{{Pointer: "#", Detail: "trailing data after the JSON object"}}})
			return
		}
		if !ValidJobKey(req.Key) {
			fail("invalid", httpapi.Problem{Status: 400, Code: httpapi.CodeValidationFailed, Title: "Invalid request", Errors: []httpapi.FieldError{{Pointer: "#/key", Detail: "must be <kind>/<part>[/<part>...], at most 512 bytes, no whitespace"}}})
			return
		}
		err := rt.Retry(req.Key)
		switch {
		case err == nil:
		case errors.Is(err, ErrUnknownKey):
			fail("unknown_key", httpapi.Problem{Status: 404, Code: httpapi.CodeNotFound, Title: "Unknown job kind"})
			return
		case errors.Is(err, ErrQueueFull):
			fail("queue_full", httpapi.WithRetry(httpapi.New(503, httpapi.CodeQueueFull, "the refresh queue is full; retry shortly"), 5*time.Second))
			return
		case errors.Is(err, ErrClosed):
			fail("closed", httpapi.WithRetry(httpapi.New(503, httpapi.CodeNotReady, "shutting down"), 5*time.Second))
			return
		default:
			log.LogAttrs(r.Context(), slog.LevelError, "admin retry failed", slog.String("key_kind", kindOf(req.Key)), slog.Any("err", err))
			count("error")
			httpapi.WriteProblem(w, r, httpapi.Problem{Status: 500, Code: httpapi.CodeInternal, Title: "Internal error"})
			return
		}
		count("accepted")
		log.LogAttrs(r.Context(), slog.LevelInfo, "admin action", slog.String("action", "retry"), slog.String("job_key", req.Key), slog.String("remote", r.RemoteAddr))
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(opsapi.RetryJobResponse{Key: req.Key, Accepted: true})
	})
}

func kindOf(k string) string { kind, _, _ := strings.Cut(k, "/"); return kind }
