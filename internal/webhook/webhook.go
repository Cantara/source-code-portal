// Package webhook receives GitHub deliveries on POST /webhooks/github: it verifies the signature over the raw body, drops repeated
// deliveries and hands each event, normalised to a sync event and a repository or owner, to a Sink. Events are hints; the scheduled
// listing repairs any delivery that never arrives.
package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"regexp"
	"strings"
	gosync "sync"
	"time"

	"github.com/cantara/source-code-portal/internal/httpapi"
	"github.com/cantara/source-code-portal/internal/sync"
)

// Pattern is the route of the receiver on the public listener.
const Pattern = "POST /webhooks/github"

const (
	MaxBody     = 25 << 20 // GitHub caps payloads at 25 MiB; the route must allow at least this
	readTimeout = 8 * time.Second
	dedupeTTL   = 15 * time.Minute
	dedupeMax   = 10_000
)

var (
	signaturePattern = regexp.MustCompile(`^sha256=[0-9a-f]{64}$`)
	deliveryPattern  = regexp.MustCompile(`^[0-9A-Za-z-]{1,64}$`)
	ownerPattern     = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})$`)
	namePattern      = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)
	eventPattern     = regexp.MustCompile(`^[a-z_]{1,40}$`) // event names as metric labels
)

// Sink receives normalised events. name is empty for events about an owner's set of repositories.
type Sink func(ev sync.Event, owner, name string)

// Receiver is the webhook handler.
type Receiver struct {
	secrets [][]byte // current first, then the previous one during a rotation
	sink    Sink
	log     *slog.Logger
	seen    *dedupe
	observe func(event, result string)
}

// New returns a receiver verifying with secret, then previous (may be empty).
func New(secret, previous []byte, sink Sink, log *slog.Logger) *Receiver {
	secrets := [][]byte{secret}
	if len(previous) > 0 {
		secrets = append(secrets, previous)
	}
	return &Receiver{secrets: secrets, sink: sink, log: log, seen: &dedupe{at: map[string]time.Time{}}, observe: func(string, string) {}}
}

// Observe sets fn to receive every delivery's event (empty before the signature is verified) and result: accepted, duplicate, ping,
// ignored, invalid_signature, too_large, malformed or unsupported_media_type.
func (rc *Receiver) Observe(fn func(event, result string)) { rc.observe = fn }

// ServeHTTP checks size and signature before anything reads the payload.
func (rc *Receiver) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		rc.observe("", "unsupported_media_type")
		httpapi.WriteProblem(w, http.StatusUnsupportedMediaType, "unsupported-media-type", "Deliveries must use content type application/json.")
		return
	}
	sig := r.Header.Get("X-Hub-Signature-256")
	if !signaturePattern.MatchString(sig) {
		rc.observe("", "invalid_signature")
		httpapi.WriteProblem(w, http.StatusUnauthorized, "bad-signature", "The signature is missing or invalid.")
		return
	}
	if r.ContentLength > MaxBody {
		rc.observe("", "too_large")
		httpapi.WriteProblem(w, http.StatusRequestEntityTooLarge, "too-large", "The payload is too large.")
		return
	}
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(readTimeout))
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBody))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			rc.observe("", "too_large")
			httpapi.WriteProblem(w, http.StatusRequestEntityTooLarge, "too-large", "The payload is too large.")
			return
		}
		rc.observe("", "malformed")
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad-body", "The payload could not be read.")
		return
	}
	delivery := r.Header.Get("X-GitHub-Delivery")
	if !rc.verify(body, sig) {
		rc.log.Warn("webhook signature mismatch", "delivery", printable(delivery))
		rc.observe("", "invalid_signature")
		httpapi.WriteProblem(w, http.StatusUnauthorized, "bad-signature", "The signature is missing or invalid.")
		return
	}
	event := r.Header.Get("X-GitHub-Event")
	if !deliveryPattern.MatchString(delivery) || event == "" || !json.Valid(body) {
		rc.observe("", "malformed")
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad-delivery", "The delivery id, event or payload is invalid.")
		return
	}
	label := event
	if !eventPattern.MatchString(label) {
		label = "other"
	}
	switch {
	case event == "ping":
		rc.observe(label, "ping")
		accepted(w, "pong", delivery)
	case !rc.seen.add(delivery, time.Now()):
		rc.observe(label, "duplicate")
		accepted(w, "duplicate", delivery)
	default:
		n, ok := Normalise(event, body)
		if !ok {
			rc.log.Debug("webhook ignored", "delivery", delivery, "event", printable(event))
			rc.observe(label, "ignored")
			accepted(w, "ignored", delivery)
			return
		}
		rc.log.Debug("webhook accepted", "delivery", delivery, "event", event, "owner", n.Owner, "name", n.Name)
		rc.sink(n.Event, n.Owner, n.Name)
		rc.observe(label, "accepted")
		accepted(w, "accepted", delivery)
	}
}

func (rc *Receiver) verify(body []byte, sig string) bool {
	want, err := hex.DecodeString(strings.TrimPrefix(sig, "sha256="))
	if err != nil {
		return false
	}
	for _, s := range rc.secrets {
		m := hmac.New(sha256.New, s)
		m.Write(body)
		if hmac.Equal(m.Sum(nil), want) {
			return true
		}
	}
	return false
}

func accepted(w http.ResponseWriter, status, delivery string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": status, "delivery": delivery})
}

// printable bounds a header value for a log line.
func printable(s string) string {
	if len(s) > 64 {
		s = s[:64]
	}
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r > 0x7e {
			return '?'
		}
		return r
	}, s)
}

// Normalised is a delivery reduced to what the sync engine needs.
type Normalised struct {
	Event       sync.Event
	Owner, Name string // Name is empty for owner-wide events
}

type payload struct {
	Ref        string `json:"ref"`
	Deleted    bool   `json:"deleted"`
	Action     string `json:"action"`
	Repository *struct {
		Name          string `json:"name"`
		DefaultBranch string `json:"default_branch"`
		Owner         struct {
			Login string `json:"login"`
		} `json:"owner"`
	} `json:"repository"`
	Installation *struct {
		Account struct {
			Login string `json:"login"`
		} `json:"account"`
	} `json:"installation"`
}

// sourceActions are the repository actions that change an owner's set of public repositories or their names. privatized and edited
// are among them: the listing drops a repository that is no longer public and picks up the new description and topics.
var sourceActions = map[string]bool{ //nolint:gochecknoglobals // lookup table
	"created": true, "deleted": true, "publicized": true, "privatized": true, "archived": true, "unarchived": true,
	"renamed": true, "transferred": true, "edited": true,
}

// Normalise maps a verified delivery to a sync event; ok is false for events v1 does not act on.
func Normalise(event string, body []byte) (_ Normalised, ok bool) {
	var p payload
	if json.Unmarshal(body, &p) != nil {
		return Normalised{}, false
	}
	var n Normalised
	switch event {
	case "push":
		if p.Repository == nil || p.Deleted {
			return Normalised{}, false
		}
		switch {
		case p.Repository.DefaultBranch != "" && p.Ref == "refs/heads/"+p.Repository.DefaultBranch:
			n.Event = sync.EventPush
		case strings.HasPrefix(p.Ref, "refs/tags/"):
			n.Event = sync.EventTag
		default:
			return Normalised{}, false
		}
		n.Owner, n.Name = p.Repository.Owner.Login, p.Repository.Name
	case "release":
		if p.Repository == nil {
			return Normalised{}, false
		}
		n = Normalised{Event: sync.EventTag, Owner: p.Repository.Owner.Login, Name: p.Repository.Name}
	case "repository":
		if p.Repository == nil || !sourceActions[p.Action] {
			return Normalised{}, false
		}
		n = Normalised{Event: sync.EventSourceChanged, Owner: p.Repository.Owner.Login}
	case "installation", "installation_repositories":
		if p.Installation == nil {
			return Normalised{}, false
		}
		n = Normalised{Event: sync.EventSourceChanged, Owner: p.Installation.Account.Login}
	default:
		return Normalised{}, false
	}
	if !ownerPattern.MatchString(n.Owner) || n.Name != "" && (!namePattern.MatchString(n.Name) || strings.Trim(n.Name, ".") == "") {
		return Normalised{}, false
	}
	return n, true
}

// dedupe remembers delivery ids for dedupeTTL, at most dedupeMax of them, dropping the oldest first.
type dedupe struct {
	mu    gosync.Mutex
	at    map[string]time.Time
	order []string // insertion order, which is expiry order
}

// add records id and reports whether it was new.
func (d *dedupe) add(id string, now time.Time) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	for len(d.order) > 0 && (len(d.order) >= dedupeMax || now.Sub(d.at[d.order[0]]) >= dedupeTTL) {
		delete(d.at, d.order[0])
		d.order = d.order[1:]
	}
	if _, seen := d.at[id]; seen {
		return false
	}
	d.at[id] = now
	d.order = append(d.order, id)
	return true
}
