package httpx

import (
	"net/http"
	"regexp"
	"uuid"
)

const (
	HeaderRequestID = "X-Request-Id"
	// KeyInboundID is the log field for a caller-supplied id (never trusted as ours).
	KeyInboundID = "inbound_request_id"
)

var inboundOK = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,64}$`)

// newRequestID returns a random UUIDv4 (06-httpapi D4): httpapi turns it into a
// valid "urn:uuid:" problem instance. Inbound X-Request-Id values are never
// adopted as the id, only logged as inbound_request_id.
func newRequestID() string { return uuid.NewV4().String() }

// inbound returns a caller-supplied X-Request-Id if it is short and boring.
func inbound(r *http.Request) string {
	v := r.Header.Get(HeaderRequestID)
	if inboundOK.MatchString(v) {
		return v
	}
	return ""
}
