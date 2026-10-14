package access

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"net/netip"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/cantara/source-code-portal/internal/httpapi/authz"
)

const (
	maxSubjectLen = 256
	maxGroupLen   = 128
	maxHeaderLen  = 4096
)

// Reason is the label of scp_identity_header_spoof_total.
type Reason string

const (
	ReasonNone          Reason = ""
	ReasonUntrustedPeer Reason = "untrusted_peer" // identity headers from a peer outside trusted_proxies
	ReasonBadSecret     Reason = "bad_secret"     // trusted peer, identity present, proxy secret missing or wrong
	ReasonDuplicate     Reason = "duplicate"      // more than one value line for a single-valued header
	ReasonMalformed     Reason = "malformed"      // control or bidi characters, over length, too many groups
	ReasonModePublic    Reason = "mode_public"    // identity headers present while mode is public
)

// stripped is the built-in deny list: removed from every request in every mode, in addition to the configured names,
// so reconfiguring header names can never leave a stale spoofable name behind. Canonical MIME form.
var stripped = []string{
	"X-Forwarded-User", "X-Forwarded-Email", "X-Forwarded-Groups", "X-Forwarded-Preferred-Username",
	"X-Auth-Request-User", "X-Auth-Request-Email", "X-Auth-Request-Groups", "X-Auth-Request-Preferred-Username",
	"X-Auth-Request-Access-Token", "X-Forwarded-Access-Token", "X-Remote-User", "X-Remote-Groups", "X-Remote-Email",
	"Remote-User", "Remote-Groups", "Remote-Email", "Remote-Name", "X-Authenticated-User", "X-User",
}

func canonical(h string) string { return http.CanonicalHeaderKey(h) }

type identifier struct {
	mode     Mode
	trusted  []netip.Prefix
	user     string
	email    string
	groups   string
	sep      string
	maxGroup int
	secHdr   string
	secSum   [sha256.Size]byte
	haveSec  bool
	strip    []string // canonical, deduplicated, includes underscore variants
}

func newIdentifier(c Config, trusted []netip.Prefix) *identifier {
	id := &identifier{mode: c.Mode, trusted: trusted}
	set := map[string]bool{}
	add := func(n string) {
		if n == "" {
			return
		}
		cn := canonical(n)
		set[cn] = true
		set[canonical(strings.ReplaceAll(cn, "-", "_"))] = true // never read, stripped anyway (proxies that map _ to -)
	}
	for _, n := range stripped {
		add(n)
	}
	if c.Mode == ModeTrustedProxy {
		i := c.Identity
		id.user, id.email, id.groups, id.sep, id.maxGroup = canonical(i.UserHeader), canonical(i.EmailHeader), canonical(i.GroupsHeader), i.GroupsSeparator, i.MaxGroups
		if i.GroupsHeader == "" {
			id.groups = ""
		}
		if i.EmailHeader == "" {
			id.email = ""
		}
		if i.ProxySecretHeader != "" {
			id.secHdr = canonical(i.ProxySecretHeader)
			id.secSum = sha256.Sum256([]byte(i.ProxySecret.Reveal()))
			id.haveSec = true
		}
		for _, n := range []string{i.UserHeader, i.EmailHeader, i.GroupsHeader, i.ProxySecretHeader} {
			add(n)
		}
	}
	for n := range set {
		id.strip = append(id.strip, n)
	}
	return id
}

// identify reads the identity headers, then removes every identity header from h (always, in every mode), and
// returns the principal. peer is the TCP peer (r.RemoteAddr), never a forwarded address.
func (id *identifier) identify(remoteAddr string, h http.Header) (authz.Principal, Reason) {
	anon := authz.Principal{Anonymous: true}
	var present bool
	for _, n := range id.strip {
		if len(h[n]) > 0 {
			present = true
			break
		}
	}
	// Collect before stripping.
	var userV, emailV, groupV, secV []string
	if id.mode == ModeTrustedProxy {
		userV, emailV, groupV, secV = h[id.user], h[id.email], nil, nil
		if id.groups != "" {
			groupV = h[id.groups]
		}
		if id.secHdr != "" {
			secV = h[id.secHdr]
		}
		if id.email == "" {
			emailV = nil
		}
	}
	for _, n := range id.strip {
		delete(h, n)
	}
	if !present {
		return anon, ReasonNone
	}
	if id.mode != ModeTrustedProxy {
		return anon, ReasonModePublic
	}
	if !id.peerTrusted(remoteAddr) {
		return anon, ReasonUntrustedPeer
	}
	if id.haveSec {
		if len(secV) != 1 {
			return anon, ReasonBadSecret
		}
		got := sha256.Sum256([]byte(secV[0]))
		if subtle.ConstantTimeCompare(got[:], id.secSum[:]) != 1 {
			return anon, ReasonBadSecret
		}
	}
	subject, reason := single(userV)
	if reason != ReasonNone {
		return anon, reason
	}
	if subject == "" {
		subject, reason = single(emailV)
		if reason != ReasonNone {
			return anon, reason
		}
	}
	if subject == "" {
		return anon, ReasonNone // trusted proxy, no user: anonymous (for example an SSO-bypass path)
	}
	if !validText(subject, maxSubjectLen) {
		return anon, ReasonMalformed
	}
	groups, ok := id.parseGroups(groupV)
	if !ok {
		return anon, ReasonMalformed
	}
	return authz.Principal{Subject: subject, Groups: groups}, ReasonNone
}

func (id *identifier) peerTrusted(remoteAddr string) bool {
	ap, err := netip.ParseAddrPort(remoteAddr)
	var a netip.Addr
	if err != nil {
		var perr error
		a, perr = netip.ParseAddr(remoteAddr)
		if perr != nil {
			return false // unix socket, empty, garbage: untrusted
		}
	} else {
		a = ap.Addr()
	}
	a = a.Unmap().WithZone("")
	for _, p := range id.trusted {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// single returns the one value of a single-valued header, "" when absent; more than one line is ambiguous.
func single(v []string) (string, Reason) {
	switch len(v) {
	case 0:
		return "", ReasonNone
	case 1:
		return strings.TrimSpace(v[0]), ReasonNone
	default:
		return "", ReasonDuplicate
	}
}

func (id *identifier) parseGroups(lines []string) ([]string, bool) {
	var out []string
	seen := map[string]bool{}
	for _, line := range lines {
		if len(line) > maxHeaderLen {
			return nil, false
		}
		for _, g := range strings.Split(line, id.sep) {
			g = strings.TrimSpace(g)
			if g == "" {
				continue
			}
			if !validText(g, maxGroupLen) {
				return nil, false
			}
			if !seen[g] {
				seen[g] = true
				out = append(out, g)
				if len(out) > id.maxGroup {
					return nil, false
				}
			}
		}
	}
	return out, true
}

// validText: valid UTF-8, within the byte limit, no control, format (bidi, zero-width), line/paragraph separator or private-use characters.
func validText(s string, max int) bool {
	if s == "" || len(s) > max || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		// Cc control, Cf format (bidi overrides, zero width), Zl/Zp (U+2028/2029), Co private use, U+FFFD.
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp, unicode.Co) || r == unicode.ReplacementChar {
			return false
		}
	}
	return true
}
