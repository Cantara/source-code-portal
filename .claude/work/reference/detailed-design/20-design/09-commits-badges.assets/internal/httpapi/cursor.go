package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// MaxCursorLen is the longest cursor accepted (matches the Cursor parameter's maxLength in common.yaml).
const MaxCursorLen = 512

const cursorV1 = 0x01

var ErrInvalidCursor = errors.New("httpapi: invalid cursor")

// Cursor is the opaque token as received; "" means the first page.
type Cursor string

type PageParams struct {
	Limit  int
	Cursor Cursor
}

// ParsePage applies the paging defaults. limit 0 means "absent". The returned problem is a 400.
func ParsePage(limit int, cursor string, def, max int) (PageParams, *Problem) {
	if limit == 0 {
		limit = def
	}
	var errs []FieldError
	if limit < 1 || limit > max {
		errs = append(errs, FieldError{Pointer: "#/query/limit", Detail: "must be between 1 and " + strconv.Itoa(max)})
	}
	if cursor != "" && !validCursorSyntax(cursor) {
		p := InvalidCursor()
		return PageParams{}, &p
	}
	if len(errs) > 0 {
		p := Validation(errs...)
		return PageParams{}, &p
	}
	return PageParams{Limit: limit, Cursor: Cursor(cursor)}, nil
}

func validCursorSyntax(s string) bool {
	if len(s) == 0 || len(s) > MaxCursorLen {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// EncodeCursor returns base64url(0x01 || JSON(v)) without padding. It panics if v cannot be marshalled
// or the result would exceed MaxCursorLen (a programming error: keyset values are small).
func EncodeCursor(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic("httpapi: cursor marshal: " + err.Error())
	}
	s := base64.RawURLEncoding.EncodeToString(append([]byte{cursorV1}, b...))
	if len(s) > MaxCursorLen {
		panic("httpapi: cursor too long")
	}
	return s
}

// DecodeCursor decodes a cursor produced by EncodeCursor into `into` (a pointer to a struct).
// Unknown versions, unknown JSON fields, trailing data and over-long input yield ErrInvalidCursor.
func DecodeCursor(s string, into any) error {
	if !validCursorSyntax(s) {
		return ErrInvalidCursor
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || len(raw) < 2 || raw[0] != cursorV1 {
		return ErrInvalidCursor
	}
	dec := json.NewDecoder(bytes.NewReader(raw[1:]))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		return ErrInvalidCursor
	}
	if _, err := dec.Token(); err != io.EOF {
		return ErrInvalidCursor
	}
	return nil
}

// QueryFingerprint is a short stable digest of the named query parameters, for binding a cursor to the
// filter it was issued for (store it in the cursor struct and compare on decode).
func QueryFingerprint(q url.Values, keys ...string) string {
	ks := append([]string(nil), keys...)
	sort.Strings(ks)
	h := sha256.New()
	for _, k := range ks {
		vs := append([]string(nil), q[k]...)
		sort.Strings(vs)
		h.Write([]byte(k))
		h.Write([]byte{0})
		h.Write([]byte(strings.Join(vs, "\x00")))
		h.Write([]byte{1})
	}
	return hex.EncodeToString(h.Sum(nil)[:6])
}
