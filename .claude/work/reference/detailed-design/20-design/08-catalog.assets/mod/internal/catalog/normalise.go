package catalog

import (
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/cantara/source-code-portal/internal/scm"
)

// Limits on text that arrives from GitHub and is stored and served verbatim by the catalogue (everything here is untrusted input:
// anyone with write access to a repository can set its description and topics).
const (
	maxDescriptionBytes = 1000 // GitHub allows 350 characters; the margin covers multi-byte text
	maxTopics           = 20   // GitHub's own limit
	maxURLBytes         = 2000 // the API contract (common.yaml HttpsUrl) allows 2000
)

// normaliseListed validates and cleans one listed repository. It returns false when the repository cannot be tracked at all
// (the plan counts it as excluded "invalid"): a zero id, an owner or name outside the GitHub grammar, or a default branch that is not a
// legal ref. Free text is cleaned, not rejected: control characters and Unicode bidirectional controls are removed from the description,
// it is truncated at a rune boundary, topics outside the GitHub grammar are dropped, and a URL that is not https is dropped.
func normaliseListed(r scm.Repository) (scm.Repository, bool) {
	// scm.RepoRef.Validate is the authority (owner and name are embedded in job keys, store keys and URLs): it also refuses the names "." and "..".
	if r.Ref.ID == 0 || r.Ref.Validate() != nil {
		return r, false
	}
	if r.DefaultBranch != "" && !validRef(r.DefaultBranch) {
		return r, false
	}
	r.Description = cleanText(r.Description, maxDescriptionBytes)
	if len(r.Topics) > 0 {
		keep := make([]string, 0, min(len(r.Topics), maxTopics))
		for _, t := range r.Topics {
			if len(keep) == maxTopics {
				break
			}
			if topicRe.MatchString(t) {
				keep = append(keep, t)
			}
		}
		r.Topics = keep
	}
	r.HTMLURL = cleanURL(r.HTMLURL)
	return r, true
}

// cleanText removes control characters (a newline becomes a space) and bidirectional formatting characters, collapses nothing else,
// and truncates to max bytes at a rune boundary.
func cleanText(s string, max int) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\n' || r == '\t' || r == '\r':
			b.WriteByte(' ')
		case unicode.IsControl(r), isBidi(r), r == utf8.RuneError, r == 0xFEFF:
			// dropped
		default:
			b.WriteRune(r)
		}
		if b.Len() >= max {
			break
		}
	}
	out := b.String()
	if len(out) > max {
		out = out[:max]
		for !utf8.ValidString(out) {
			out = out[:len(out)-1]
		}
	}
	return strings.TrimSpace(out)
}

func isBidi(r rune) bool {
	return (r >= 0x202A && r <= 0x202E) || (r >= 0x2066 && r <= 0x2069) || r == 0x200E || r == 0x200F || r == 0x061C
}

func cleanURL(s string) string {
	if s == "" || len(s) > maxURLBytes {
		return ""
	}
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil { // the public API type is HttpsUrl (^https://)
		return ""
	}
	for _, r := range s {
		if r <= 0x20 || r == 0x7f {
			return ""
		}
	}
	return s
}
