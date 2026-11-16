package github

import (
	"context"
	"net/http"
	"strings"
	"time"

	gh "github.com/google/go-github/v93/github"
)

// TokenCheck is what GitHub says of a person's token (scp enterprise): whose it is and when it ends.
type TokenCheck struct {
	Login   string
	Email   string    // the account's public email; empty when it shows none
	Expires time.Time // zero: GitHub named no end
}

// CheckToken asks GitHub with token who it belongs to (GET /user) and whether it reads any repository of the organisation owner
// (GET /orgs/{owner}/repos, one). The token goes straight to GitHub, past the conditional cache: its answers are not kept. A token that
// reads nothing of owner is the Permanent error reads_nothing; the others are classified as every call's.
func (s *Source) CheckToken(ctx context.Context, owner string, token []byte) (TokenCheck, error) {
	c, err := s.newClient(s.base, token)
	if err != nil {
		return TokenCheck{}, err
	}
	u, resp, err := c.Users.Get(ctx, "")
	if err != nil {
		return TokenCheck{}, classify(resp, err)
	}
	check := TokenCheck{Login: u.GetLogin(), Email: u.GetEmail(), Expires: tokenEnd(resp.Response)}
	repos, resp, err := c.Repositories.ListByOrg(ctx, owner, &gh.RepositoryListByOrgOptions{ListOptions: gh.ListOptions{PerPage: 1}})
	if err != nil {
		return TokenCheck{}, classify(resp, err)
	}
	if len(repos) == 0 {
		return TokenCheck{}, &Error{Kind: Permanent, Code: "reads_nothing", Err: errNothing(owner)}
	}
	return check, nil
}

type errNothing string

func (e errNothing) Error() string { return "the token reads no repository of " + string(e) }

// tokenEnd is the end GitHub names for the token that made the request (GitHub-Authentication-Token-Expiration), zero without one.
func tokenEnd(r *http.Response) time.Time {
	if r == nil {
		return time.Time{}
	}
	v := strings.TrimSpace(r.Header.Get("GitHub-Authentication-Token-Expiration"))
	for _, layout := range []string{"2006-01-02 15:04:05 MST", "2006-01-02 15:04:05 -0700"} {
		if t, err := time.Parse(layout, v); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}
