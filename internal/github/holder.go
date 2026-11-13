package github

import (
	"context"

	gh "github.com/google/go-github/v93/github"

	"github.com/cantara/source-code-portal/internal/snapshot"
)

// Holder is one person's token a source read with people's tokens may use (scp enterprise). Key names the token, and changes when
// the token does: the credential made for it, with its rate book and its conditional answers, is kept under it.
type Holder struct {
	Key   string
	Token []byte
}

// holder is the credential for h, made at its first use.
func (s *Source) holder(h Holder) (*credential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c := s.persons[h.Key]; c != nil {
		return c, nil
	}
	rate := &rateBook{}
	client, err := s.newClient(s.transport(rate), h.Token)
	if err != nil {
		return nil, err
	}
	c := &credential{client: client, rate: rate}
	s.persons[h.Key] = c
	return c, nil
}

// Forget drops the credentials of the holders whose keys keep refuses: a removed or replaced token.
func (s *Source) Forget(keep func(key string) bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k := range s.persons {
		if !keep(k) {
			delete(s.persons, k)
		}
	}
}

// ListWith returns every repository of the organisation owner that h's token reads, most recently pushed first: what that person may
// see of owner. Any failed page fails the listing.
func (s *Source) ListWith(ctx context.Context, owner string, h Holder) ([]snapshot.Repo, error) {
	c, err := s.holder(h)
	if err != nil {
		return nil, err
	}
	return s.listAll(ctx, owner, func(ctx context.Context, p gh.ListOptions) ([]*gh.Repository, *gh.Response, error) {
		return c.client.Repositories.ListByOrg(ctx, owner, &gh.RepositoryListByOrgOptions{Type: "all", Sort: "pushed", Direction: "desc", ListOptions: p})
	})
}
