package people

import (
	"net/url"
	"strconv"
	"strings"
)

// Login rules: how a token's GitHub login is tied to the identity claim of the person who links it (enterprise.github[].login).
const (
	// Managed: a managed user's login is the identity before its @, every character but a letter, a digit or - as -, then _ and the
	// enterprise's short code. The login without its last _ part must be the identity so made, letter case aside.
	Managed = "managed"
	// Exact: the login is the identity claim, letter case aside.
	Exact = "exact"
)

// Bound reports whether login belongs to the person whose identity claim is identity, by rule.
func Bound(rule, identity, login string) bool {
	if identity == "" || login == "" {
		return false
	}
	if rule == Exact {
		return strings.EqualFold(identity, login)
	}
	local, _, _ := strings.Cut(identity, "@")
	name, code, ok := cutLast(login, "_")
	if !ok || code == "" || local == "" {
		return false
	}
	return strings.EqualFold(name, normalise(local))
}

func cutLast(s, sep string) (before, after string, found bool) {
	i := strings.LastIndex(s, sep)
	if i < 0 {
		return s, "", false
	}
	return s[:i], s[i+len(sep):], true
}

func normalise(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' {
			return r
		}
		return '-'
	}, s)
}

// Permissions are the repository permissions a person's token needs, all read: the contents and metadata, the pull requests, the
// issues and the commit statuses. Nothing else, and no write.
var Permissions = []string{"contents", "metadata", "pull_requests", "issues", "statuses"} //nolint:gochecknoglobals // a fixed list

// tokenDays is the lifetime the creation link proposes; an organisation's policy may allow less.
const tokenDays = 90

// CreationLink is GitHub's page for a new fine-grained token with its fields filled in: the name, a description, the organisation as
// resource owner, 90 days and the read permissions. The person still chooses "All repositories" and generates it.
func CreationLink(webURL, owner, description string) string {
	q := url.Values{"name": {"Source Code Portal (" + owner + ")"}, "description": {description}, "target_name": {owner},
		"expires_in": {strconv.Itoa(tokenDays)}}
	for _, p := range Permissions {
		q.Set(p, "read")
	}
	return strings.TrimSuffix(webURL, "/") + "/settings/personal-access-tokens/new?" + q.Encode()
}

func lower(s string) string { return strings.ToLower(s) }
