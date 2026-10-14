// Package httpapi is a STUB of unit 06 (design 06-httpapi.md section 2.2), reduced to the symbols internal/manifests uses.
// Signatures are copied from that document; bodies are minimal working stand-ins for the scratch tests.
package httpapi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/cantara/source-code-portal/internal/httpapi/openapi"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Mux interface {
	HandleFunc(pattern string, h func(http.ResponseWriter, *http.Request))
	http.Handler
}
type Mounter interface{ Mount(m Mux) }

type FieldError struct {
	Pointer string `json:"pointer"`
	Detail  string `json:"detail"`
}
type Problem struct {
	Type              string       `json:"type"`
	Title             string       `json:"title"`
	Status            int          `json:"status"`
	Detail            string       `json:"detail,omitempty"`
	Instance          string       `json:"instance,omitempty"`
	Code              string       `json:"code"`
	Errors            []FieldError `json:"errors,omitempty"`
	RetryAfterSeconds int          `json:"retryAfterSeconds,omitempty"`
	Header            http.Header  `json:"-"`
}

func New(status int, code, detail string) Problem {
	return Problem{Type: "urn:scp:problem:" + code, Title: http.StatusText(status), Status: status, Code: code, Detail: detail}
}
func Validation(errs ...FieldError) Problem {
	p := New(400, "validation-failed", "")
	p.Errors = errs
	return p
}
func InvalidCursor() Problem         { return New(400, "invalid-cursor", "") }
func NotFound(detail string) Problem { return New(404, "not-found", detail) }
func Internal() Problem              { return New(500, "internal", "") }
func NotReady(retry time.Duration) Problem {
	p := New(503, "not-ready", "")
	p.RetryAfterSeconds = int(retry.Seconds())
	return p
}
func WriteProblem(w http.ResponseWriter, r *http.Request, p Problem) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(p)
}
func WriteJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(v); err != nil {
		WriteProblem(w, r, Internal())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

var ErrInvalidCursor = errors.New("httpapi: invalid cursor")

type Cursor string
type PageParams struct {
	Limit  int
	Cursor Cursor
}

func ParsePage(limit int, cursor string, def, max int) (PageParams, *Problem) {
	if limit == 0 {
		limit = def
	}
	if limit < 1 || limit > max {
		p := Validation(FieldError{"#/query/limit", fmt.Sprintf("limit must be between 1 and %d", max)})
		return PageParams{}, &p
	}
	return PageParams{Limit: limit, Cursor: Cursor(cursor)}, nil
}
func EncodeCursor(v any) string {
	b, _ := json.Marshal(v)
	return base64.RawURLEncoding.EncodeToString(append([]byte{1}, b...))
}
func DecodeCursor(s string, into any) error {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || len(b) < 2 || b[0] != 1 || len(s) > 512 {
		return ErrInvalidCursor
	}
	d := json.NewDecoder(bytes.NewReader(b[1:]))
	d.DisallowUnknownFields()
	if d.Decode(into) != nil {
		return ErrInvalidCursor
	}
	return nil
}
func QueryFingerprint(q url.Values, keys ...string) string {
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "%s=%s;", k, strings.Join(q[k], ","))
	}
	return fmt.Sprintf("%x", fnv(b.String()))
}
func fnv(s string) uint64 {
	h := uint64(14695981039346656037)
	for i := 0; i < len(s); i++ {
		h = (h ^ uint64(s[i])) * 1099511628211
	}
	return h
}

type ETagKind byte

const (
	ETagSnapshot ETagKind = 'g'
	ETagRepo     ETagKind = 'r'
	ETagVersion  ETagKind = 'v'
)

func WeakETag(parts ...any) string { return WeakETagOf(ETagSnapshot, parts...) }
func WeakETagOf(kind ETagKind, parts ...any) string {
	s := make([]string, len(parts))
	for i, p := range parts {
		s[i] = fmt.Sprint(p)
	}
	return `W/"` + string(kind) + strings.Join(s, "-") + `"`
}
func FreshnessDigest(statuses ...string) string {
	all := true
	for _, s := range statuses {
		all = all && s == "fresh"
	}
	if all {
		return ""
	}
	return Digest(statuses...)
}
func Digest(parts ...string) string { return fmt.Sprintf("%016x", fnv(strings.Join(parts, "\x00"))) }
func WithDigest(etag, digest string) string {
	if digest == "" {
		return etag
	}
	return strings.TrimSuffix(etag, `"`) + "." + digest + `"`
}
func NotModified(r *http.Request, etag string) bool {
	if etag == "" || (r.Method != "GET" && r.Method != "HEAD") {
		return false
	}
	want := strings.TrimPrefix(etag, "W/")
	for _, line := range r.Header.Values("If-None-Match") {
		for _, tok := range strings.Split(line, ",") {
			tok = strings.TrimSpace(tok)
			if tok == "*" || strings.TrimPrefix(tok, "W/") == want {
				return true
			}
		}
	}
	return false
}

// FreshnessView is the input of NewFreshness (design 06 section 2.2).
type FreshnessView struct {
	Status                                 string // fresh | stale | error | unknown
	Kind                                   string // item family, "manifests.repo"; "" omits the member
	AsOf, FetchedAt, CheckedAt, StaleSince time.Time
	LastErrorCode                          string
}

func NewFreshness(v FreshnessView) openapi.Freshness {
	t := func(x time.Time) *time.Time {
		if x.IsZero() {
			return nil
		}
		u := x.UTC()
		return &u
	}
	f := openapi.Freshness{Status: v.Status, Kind: v.Kind, AsOf: t(v.AsOf), FetchedAt: t(v.FetchedAt), CheckedAt: t(v.CheckedAt), StaleSince: t(v.StaleSince)}
	if v.LastErrorCode != "" {
		f.LastErrorCode = &v.LastErrorCode
	}
	return f
}
func NewPageInfo(next string, limit int) openapi.PageInfo {
	p := openapi.PageInfo{Limit: limit}
	if next != "" {
		p.NextCursor = &next
	}
	return p
}
