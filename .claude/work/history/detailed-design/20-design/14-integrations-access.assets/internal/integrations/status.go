package integrations

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/scm"
)

// State is the provider-neutral outcome. The zero value is StateUnknown on purpose: an unset Status never reads as healthy.
type State uint8

const (
	StateUnknown State = iota
	StateOK
	StateFailing
	StateUnstable
	StateDisabled
)

func (s State) String() string {
	switch s {
	case StateOK:
		return "ok"
	case StateFailing:
		return "failing"
	case StateUnstable:
		return "unstable"
	case StateDisabled:
		return "disabled"
	default:
		return "unknown"
	}
}

func parseState(s string) State {
	// StateDisabled is deliberately not parseable: it is a read-time value of Disabled(), never stored.
	for _, c := range []State{StateOK, StateFailing, StateUnstable} {
		if c.String() == s {
			return c
		}
	}
	return StateUnknown
}

// Target says what to ask a provider about one repository. Merged from the descriptor, group defaults and instance
// defaults (in that order, per key); it never carries a host name.
type Target struct {
	Repo     scm.RepoRef
	Instance string // Jenkins instance name ("" for Snyk)
	Job      string // Jenkins job path, "/"-separated, validated by ParseJobPath
	Project  string // Snyk target display name to match, "owner/repo", lower-cased by the matcher
	Branch   string // Jenkins multibranch ref: the repository's resolved default branch
}

// Status is a structured provider answer. Summary is generated from a closed vocabulary and numbers: no
// provider-supplied free text ever reaches it (the UI and llms outputs print it).
type Status struct {
	Provider   string
	State      State
	Summary    string // at most 120 runes, plain text
	DetailURL  string // https (or http for allow_http instances) and the configured host only; empty unless exposed
	ObservedAt time.Time
	Counts     map[string]int // snyk: critical/high/medium/low
}

// Statuses is the slot value: provider name -> status with freshness. Disabled providers are absent.
// It is immutable once Put (the writer clones before Put and never touches it again).
type Statuses map[string]freshness.Value[Status]

const maxSummaryRunes = 120

// CapSummary truncates on a rune boundary.
func CapSummary(s string) string {
	if utf8.RuneCountInString(s) <= maxSummaryRunes {
		return s
	}
	r := []rune(s)
	return string(r[:maxSummaryRunes-1]) + "…"
}

// Disabled is what consumers show for a provider that is not configured. Never "ok" (GR-41).
func Disabled(provider string) Status { return Status{Provider: provider, State: StateDisabled} }

type statusPayload struct {
	Schema    int            `json:"schema"`
	State     string         `json:"state"`
	Summary   string         `json:"summary"`
	DetailURL string         `json:"detailUrl,omitempty"`
	Observed  time.Time      `json:"observedAt"`
	Counts    map[string]int `json:"counts,omitempty"`
}

const payloadSchema = 1

func encodeStatus(s Status) ([]byte, string) {
	b, _ := json.Marshal(statusPayload{Schema: payloadSchema, State: s.State.String(), Summary: s.Summary,
		DetailURL: s.DetailURL, Observed: s.ObservedAt.UTC(), Counts: s.Counts})
	sum := sha256.Sum256(canonical(s))
	return b, hex.EncodeToString(sum[:8])
}

// canonical excludes ObservedAt so an unchanged answer has an unchanged content SHA (a 304-equivalent: Revalidated, no payload write).
func canonical(s Status) []byte {
	keys := slices.Sorted(maps.Keys(s.Counts))
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s|%s|%s|%s", s.Provider, s.State, s.Summary, s.DetailURL)
	for _, k := range keys {
		fmt.Fprintf(&sb, "|%s=%d", k, s.Counts[k])
	}
	return []byte(sb.String())
}

// decodeStatus reads stored bytes as untrusted: unknown schema, bad JSON or an out-of-range value yields an error
// and the caller drops the row (the next sweep rebuilds it).
func decodeStatus(provider string, b []byte) (Status, error) {
	var p statusPayload
	if err := json.Unmarshal(b, &p); err != nil {
		return Status{}, err
	}
	if p.Schema != payloadSchema {
		return Status{}, fmt.Errorf("integrations: payload schema %d, want %d", p.Schema, payloadSchema)
	}
	return Status{Provider: provider, State: parseState(p.State), Summary: CapSummary(p.Summary),
		DetailURL: safeDetailURL(p.DetailURL), ObservedAt: p.Observed, Counts: cleanCounts(p.Counts)}, nil
}

// safeDetailURL re-validates a stored link: the SPA renders it as an anchor, so a tampered row must not smuggle in a
// javascript: or data: URL. Only absolute http(s) URLs with a host and no userinfo survive.
func safeDetailURL(raw string) string {
	if raw == "" || len(raw) > 2048 {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil {
		return ""
	}
	return u.String()
}

// cleanCounts keeps only the four severity keys, clamped to 0..MaxCount (stored bytes are untrusted).
func cleanCounts(in map[string]int) map[string]int {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]int, len(severities))
	for _, k := range severities {
		if v, ok := in[k]; ok {
			out[k] = min(max(v, 0), MaxCount)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// MaxCount is the clamp for any issue count taken from an upstream or from storage. Values above it saturate; they
// never wrap to 0 (a count that reads as "no issues" would fail open).
const MaxCount = 1_000_000

// Tone is the colour intent for a badge; badges maps it to its closed palette.
type Tone uint8

const (
	ToneNeutral Tone = iota
	ToneGood
	ToneWarn
	ToneBad
)

// String is the name badges' bridge maps by (unit 09 section 2.4: success, warning, danger, neutral). The names are a
// cross-unit contract: an unknown name reads as neutral there, so a rename here would silently grey every badge.
func (t Tone) String() string {
	switch t {
	case ToneGood:
		return "success"
	case ToneWarn:
		return "warning"
	case ToneBad:
		return "danger"
	default:
		return "neutral"
	}
}

// Fact is what the badges bridge reads (ADR-011 "build" and "security" kinds): one short message and a tone.
type Fact struct {
	Provider string
	Message  string // "passing", "failing", "unstable", "unknown", or for snyk "no issues" / "3 high"
	Tone     Tone
	Stale    bool // derived freshness is stale: Tone is forced to neutral, Message is the last known one
}

// FactOf derives a Fact from a stored value at read time.
func FactOf(v freshness.Value[Status], now time.Time) Fact {
	f := Fact{Provider: v.V.Provider, Message: "unknown", Tone: ToneNeutral}
	switch v.Info.Derive(now) {
	case freshness.StatusUnknown, freshness.StatusError:
		return f
	case freshness.StatusStale:
		f.Stale = true
	}
	s := v.V
	switch s.Provider {
	case "snyk":
		f.Message = snykMessage(s)
	default:
		f.Message = map[State]string{StateOK: "passing", StateFailing: "failing", StateUnstable: "unstable"}[s.State]
	}
	if f.Message == "" {
		f.Message = "unknown"
	}
	if !f.Stale {
		f.Tone = map[State]Tone{StateOK: ToneGood, StateUnstable: ToneWarn, StateFailing: ToneBad}[s.State]
	}
	return f
}

var severities = []string{"critical", "high", "medium", "low"}

func snykMessage(s Status) string {
	switch s.State {
	case StateOK:
		return "no issues"
	case StateFailing, StateUnstable:
		for _, sev := range severities {
			if n := s.Counts[sev]; n > 0 {
				return fmt.Sprintf("%d %s", n, sev)
			}
		}
	}
	return ""
}
