package commits

import (
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cantara/source-code-portal/internal/scm"
)

const (
	maxSubjectRunes = 200
	maxNameRunes    = 100
	maxURLBytes     = 2048
	// futureTolerance bounds clock skew and fake dates: a commit dated later than now+futureTolerance is clamped to now,
	// otherwise a commit dated 2099 would stay at the top of every feed (and never age out of the ring) forever.
	futureTolerance = 24 * time.Hour
)

var minCommitDate = time.Date(1970, 1, 2, 0, 0, 0, 0, time.UTC)

// dropStats counts what normalise removed or changed; the job turns it into metrics and one log line.
type dropStats struct{ NoDate, InvalidSHA, NoURL, Duplicate, Clamped int }

// normalise turns the adapter's newest-first (branch order) list into the ring: validated, deduplicated, clamped,
// sanitised, sorted by Compare (newest first) and capped at keep. prior is the previous ring (may be nil). head is the SHA of the first input element (the
// branch tip) when it is a valid SHA, else "". normalise is a pure function of its arguments.
func normalise(in []scm.Commit, now time.Time, keep int, repoURL string, prior []scm.Commit) (out []scm.Commit, head string, st dropStats) {
	now = now.UTC().Truncate(time.Second)
	if len(in) > 0 {
		if sha, ok := cleanSHA(in[0].SHA); ok {
			head = sha
		}
	}
	seen := make(map[string]struct{}, len(in))
	out = make([]scm.Commit, 0, min(len(in), keep))
	for _, c := range in {
		sha, ok := cleanSHA(c.SHA)
		if !ok {
			st.InvalidSHA++
			continue
		}
		if _, dup := seen[sha]; dup {
			st.Duplicate++
			continue
		}
		at := c.CommittedAt.UTC()
		if at.Before(minCommitDate) {
			st.NoDate++
			continue
		}
		if at.After(now.Add(futureTolerance)) {
			at = now
			// A commit clamped at an earlier ingestion keeps that date: re-clamping to the new "now" at every
			// refresh would pin it above every real commit for as long as it stays in the fetch window.
			if pc, ok := priorDate(prior, sha); ok && !pc.After(now) {
				at = pc
			}
			st.Clamped++
		}
		u := cleanURL(c.URL)
		if u == "" {
			u = commitURL(repoURL, sha)
		}
		if u == "" { // the API contract requires an https link; neither the adapter's nor the repository's URL is usable
			st.NoURL++
			continue
		}
		seen[sha] = struct{}{}
		out = append(out, scm.Commit{
			SHA:         sha,
			Subject:     cleanLine(c.Subject, maxSubjectRunes),
			AuthorLogin: cleanLogin(c.AuthorLogin),
			AuthorName:  cleanLine(c.AuthorName, maxNameRunes),
			CommittedAt: at.Truncate(time.Second),
			URL:         u,
		})
	}
	slices.SortStableFunc(out, func(a, b scm.Commit) int { return compareWithin(a, b) })
	if len(out) > keep {
		out = out[:keep]
	}
	return out, head, st
}

func priorDate(prior []scm.Commit, sha string) (time.Time, bool) {
	for i := range prior {
		if prior[i].SHA == sha {
			return prior[i].CommittedAt, true
		}
	}
	return time.Time{}, false
}

func cleanSHA(s string) (string, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if len(s) != 40 && len(s) != 64 { // SHA-1 or SHA-256 object names
		return "", false
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return "", false
		}
	}
	return s, true
}

// cleanLine keeps the first line of s, cleans it with the kernel's scm.CleanLine (the one hostile-text cleaner of the
// portal, SC-09: controls and Unicode spaces to one space, bidirectional, zero-width, soft-hyphen and tag-block
// characters dropped) and caps it at maxRunes runes, the last one being an ellipsis when something was cut.
func cleanLine(s string, maxRunes int) string {
	line, _, _ := strings.Cut(s, "\n")
	out := scm.CleanLine(line, maxRunes+1) // one rune of look-ahead tells "exactly maxRunes" from "cut"
	if utf8.RuneCountInString(out) <= maxRunes {
		return out
	}
	return strings.TrimRight(trimToRunes(out, maxRunes-1), " ") + "…"
}

func trimToRunes(s string, n int) string {
	for i := range s {
		if n == 0 {
			return s[:i]
		}
		n--
	}
	return s
}

// cleanLogin accepts GitHub login syntax (alphanumerics and single hyphens, at most 39 characters, optional [bot]).
func cleanLogin(s string) string {
	base := strings.TrimSuffix(s, "[bot]")
	if len(base) == 0 || len(base) > 39 {
		return ""
	}
	for i := 0; i < len(base); i++ {
		c := base[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' && i > 0) {
			return ""
		}
	}
	return s
}

// cleanURL keeps only absolute https URLs without credentials, so the value is safe in an href and matches the API
// schema (HttpsUrl).
func cleanURL(s string) string {
	if len(s) == 0 || len(s) > maxURLBytes {
		return ""
	}
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return ""
	}
	return u.String()
}

// commitURL derives the GitHub commit page from the repository page when the adapter delivered no usable link.
func commitURL(repoURL, sha string) string {
	base := cleanURL(repoURL)
	if base == "" {
		return ""
	}
	return strings.TrimRight(base, "/") + "/commit/" + sha
}
