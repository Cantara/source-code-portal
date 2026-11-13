package people

import (
	"net/url"
	"strconv"
	"strings"
)

// Login rules: how a token's GitHub account is tied to the identity claim of the person who links it (enterprise.github[].login).
// Either rule also takes the account's email, when GitHub shows one, equal to the identity claim.
const (
	// Managed: a managed user's login is made by the enterprise from the identity: its part before @, every character but a letter, a
	// digit or - as -, on some hosts followed by _ and the enterprise's short code. The login, with or without that suffix, must be
	// the identity so made, letter case aside. Managed users cannot choose their login.
	Managed = "managed"
	// Exact: the login is the identity claim, letter case aside.
	Exact = "exact"
)

// Bound reports whether the GitHub account with login and email (empty when GitHub shows none) belongs to the person whose identity
// claim is identity, by rule.
func Bound(rule, identity, login, email string) bool {
	if identity == "" || login == "" {
		return false
	}
	if email != "" && strings.EqualFold(email, identity) {
		return true
	}
	if rule == Exact {
		return strings.EqualFold(identity, login)
	}
	local, _, _ := strings.Cut(identity, "@")
	if local == "" {
		return false
	}
	want := normalise(local)
	if strings.EqualFold(login, want) {
		return true
	}
	name, code, ok := cutLast(login, "_")
	return ok && code != "" && strings.EqualFold(name, want)
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
// issues, the commit statuses and the checks (a pull request's checks). Nothing else, and no write.
var Permissions = []string{"contents", "metadata", "pull_requests", "issues", "statuses", "checks"} //nolint:gochecknoglobals // a fixed list

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
