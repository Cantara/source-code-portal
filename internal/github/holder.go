package github

import (
	"context"
	"fmt"

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

// Broker is what a source read with people's tokens asks of the people (scp enterprise): every repository of owner their fresh
// listings hold, and the tokens of the people whose fresh listing holds owner/name.
type Broker interface {
	Listed(source, owner string) ([]snapshot.Repo, bool)
	Holders(source, owner, name string) []Holder
}

// SetBroker gives a people source its broker. Call it before the first request; without one the source reads nothing.
func (s *Source) SetBroker(b Broker) { s.broker = b }

// listed is owner's repositories as the people's fresh listings hold them, most recently pushed first: the catalogue of a people source.
func (s *Source) listed(owner string) ([]snapshot.Repo, error) {
	if s.broker != nil {
		if repos, ok := s.broker.Listed(s.id, owner); ok {
			return repos, nil
		}
	}
	return nil, &Error{Kind: Waiting, Code: "no_person", Err: fmt.Errorf("no linked person's token has listed %s lately", owner)}
}

// holderFor is the client of the token, among those of the people who read owner/name, with the most of its rate limit left.
func (s *Source) holderFor(owner, name string) (*gh.Client, error) {
	var holders []Holder
	if s.broker != nil && name != "" {
		holders = s.broker.Holders(s.id, owner, name)
	}
	var best *credential
	bestLeft := -1.0
	for _, h := range holders {
		c, err := s.holder(h)
		if err != nil {
			return nil, err
		}
		if left := c.rate.fraction(); left > bestLeft {
			best, bestLeft = c, left
		}
	}
	if best == nil {
		return nil, &Error{Kind: Waiting, Code: "no_person", Err: fmt.Errorf("no linked person's token reads %s/%s", owner, name)}
	}
	return best.client, nil
}
