package people

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/cantara/source-code-portal/internal/github"
	"github.com/cantara/source-code-portal/internal/store"
)

// Source is a GitHub source read with its people's tokens.
type Source struct {
	ID     string
	WebURL string   // the web address the creation link is built on
	Owners []string // the organisations the portal reads, as configured
	Login  string   // Managed or Exact
	// Check asks GitHub with token who it belongs to and whether it reads owner (github.Source.CheckToken).
	Check func(ctx context.Context, owner string, token []byte) (github.TokenCheck, error)
}

// Options are what People needs.
type Options struct {
	Store   *store.DB
	Key     []byte // enterprise.token_key_file
	Issuer  string // the OIDC issuer: a person is its subject
	Sources []Source
	Portal  string // the portal's address, in the token's description
	Now     func() time.Time
	Log     *slog.Logger
}

// People holds the enterprise mode's people in memory, loaded from the store at the start and written through before any change is
// acknowledged (design/enterprise.md, EN4).
type People struct {
	o       Options
	sealer  *sealer
	sources map[string]Source

	mu     sync.Mutex
	people map[string]*person
}

type person struct {
	store.Person
	tokens map[slot]store.PersonToken
	access map[slot][]int64
}

type slot struct{ source, owner string }

// Who is the signed-in person a request is made for.
type Who struct {
	Subject  string // the provider's subject
	Identity string // the identity claim's value
	Name     string
}

// seenEvery is how stale a person's last sign-in may be before it is written again.
const seenEvery = time.Hour

// Open loads the people from the store.
func Open(ctx context.Context, o Options) (*People, error) {
	if o.Now == nil {
		o.Now = time.Now
	}
	s, err := newSealer(o.Key)
	if err != nil {
		return nil, err
	}
	p := &People{o: o, sealer: s, sources: map[string]Source{}, people: map[string]*person{}}
	for _, src := range o.Sources {
		p.sources[src.ID] = src
	}
	people, tokens, access, err := o.Store.People(ctx)
	if err != nil {
		return nil, fmt.Errorf("people: %w", err)
	}
	for _, sp := range people {
		p.people[sp.ID] = &person{Person: sp, tokens: map[slot]store.PersonToken{}, access: map[slot][]int64{}}
	}
	for _, t := range tokens {
		if pp := p.people[t.Person]; pp != nil {
			pp.tokens[slot{t.Source, t.Owner}] = t
		}
	}
	for _, a := range access {
		if pp := p.people[a.Person]; pp != nil {
			pp.access[slot{a.Source, a.Owner}] = a.Repos
		}
	}
	return p, nil
}

// id is the store's id of the person with subject: the issuer and the subject.
func (p *People) id(subject string) string { return p.o.Issuer + "#" + subject }

// Seen records that w signed in: a new person is written, a known one when their identity or name changed or their last sign-in is
// older than an hour.
func (p *People) Seen(ctx context.Context, w Who) error {
	if w.Subject == "" {
		return errors.New("people: no subject")
	}
	id, now := p.id(w.Subject), p.o.Now()
	p.mu.Lock()
	pp := p.people[id]
	if pp != nil && pp.Identity == w.Identity && pp.Name == w.Name && now.Sub(pp.LastAt) < seenEvery {
		p.mu.Unlock()
		return nil
	}
	rec := store.Person{ID: id, Identity: w.Identity, Name: w.Name, FirstAt: now, LastAt: now}
	if pp != nil {
		rec.FirstAt = pp.FirstAt
	}
	p.mu.Unlock()
	if err := p.o.Store.PutPerson(ctx, rec); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if pp = p.people[id]; pp == nil {
		pp = &person{tokens: map[slot]store.PersonToken{}, access: map[slot][]int64{}}
		p.people[id] = pp
	}
	pp.Person = rec
	return nil
}

// Refusal is why a token was not linked, in words for the person who pasted it.
type Refusal struct {
	Code   string
	Detail string
}

func (r *Refusal) Error() string { return r.Code + ": " + r.Detail }

// tokenPrefix is how a fine-grained personal access token begins; no other kind is linked (a classic token carries write scopes).
const tokenPrefix = "github_pat_" //nolint:gosec // G101: the prefix of a kind of token, not a credential

// Link checks token with GitHub for w and the source's owner, and keeps it sealed when it is w's own and reads the owner: the token
// replaces w's earlier one for that owner. A *Refusal says why it was not kept.
func (p *People) Link(ctx context.Context, w Who, source, owner, token string) (Token, error) {
	src, ok := p.sources[source]
	owner = strings.ToLower(strings.TrimSpace(owner))
	if !ok || !slices.ContainsFunc(src.Owners, func(o string) bool { return strings.EqualFold(o, owner) }) {
		return Token{}, &Refusal{"unknown-owner", "This portal does not read " + owner + " on " + source + "."}
	}
	token = strings.TrimSpace(token)
	if !strings.HasPrefix(token, tokenPrefix) || len(token) > 255 || strings.ContainsFunc(token, func(r rune) bool { return r <= ' ' || r > '~' }) {
		return Token{}, &Refusal{"not-fine-grained", "Paste a fine-grained personal access token (it begins with " + tokenPrefix + "); other kinds are not taken."}
	}
	if err := p.Seen(ctx, w); err != nil {
		return Token{}, err
	}
	checked, err := src.Check(ctx, owner, []byte(token))
	if err != nil {
		return Token{}, refusal(err, owner)
	}
	if !Bound(src.Login, w.Identity, checked.Login) {
		return Token{}, &Refusal{"not-yours", fmt.Sprintf("The token belongs to the GitHub user %s, who is not you (%s). Make the token signed in to GitHub as yourself.",
			checked.Login, w.Identity)}
	}
	if now := p.o.Now(); !checked.Expires.IsZero() && !checked.Expires.After(now) {
		return Token{}, &Refusal{"expired", "The token has expired. Make a new one."}
	}
	id := p.id(w.Subject)
	t := store.PersonToken{Person: id, Source: source, Owner: owner, Login: checked.Login, Sealed: p.sealer.seal(id, source, owner, []byte(token)),
		ExpiresAt: checked.Expires, CreatedAt: p.o.Now(), State: "ok"}
	if err := p.o.Store.PutPersonToken(ctx, t); err != nil {
		return Token{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	pp := p.people[id]
	pp.tokens[slot{source, owner}] = t
	delete(pp.access, slot{source, owner})
	return view(t), nil
}

// refusal says in words why GitHub refused a token.
func refusal(err error, owner string) error {
	var e *github.Error
	if !errors.As(err, &e) {
		return &Refusal{"unavailable", "GitHub could not be asked about the token. Try again in a minute."}
	}
	switch {
	case e.Code == "reads_nothing":
		return &Refusal{"reads-nothing", "The token reads no repository of " + owner + ". Make it with " + owner + " as resource owner and All repositories."}
	case e.Kind == github.Auth:
		return &Refusal{"unauthorized", "GitHub refused the token: it is wrong, expired or revoked, or the enterprise's network rules refuse this portal's address."}
	case e.Code == "forbidden":
		return &Refusal{"forbidden", "GitHub refused the token for " + owner + ": the organisation may still have to approve it, or its rules refuse this portal's address."}
	case e.Kind == github.NotFound:
		return &Refusal{"reads-nothing", owner + " is not an organisation the token reads. Make it with " + owner + " as resource owner."}
	}
	return &Refusal{"unavailable", "GitHub could not be asked about the token. Try again in a minute."}
}

// Unlink removes w's token for the source's owner and what it listed.
func (p *People) Unlink(ctx context.Context, w Who, source, owner string) error {
	id, owner := p.id(w.Subject), strings.ToLower(owner)
	if err := p.o.Store.DeletePersonToken(ctx, id, source, owner); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if pp := p.people[id]; pp != nil {
		delete(pp.tokens, slot{source, owner})
		delete(pp.access, slot{source, owner})
	}
	return nil
}

// Token is what the portal shows of a linked token: never the token itself.
type Token struct {
	Source    string     `json:"source"`
	Owner     string     `json:"owner"`
	Login     string     `json:"login"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
	State     string     `json:"state"`
	ListedAt  *time.Time `json:"listedAt,omitempty"`
}

func view(t store.PersonToken) Token {
	v := Token{Source: t.Source, Owner: t.Owner, Login: t.Login, CreatedAt: t.CreatedAt.UTC(), State: t.State}
	if !t.ExpiresAt.IsZero() {
		e := t.ExpiresAt.UTC()
		v.ExpiresAt = &e
	}
	if !t.ListedAt.IsZero() {
		l := t.ListedAt.UTC()
		v.ListedAt = &l
	}
	return v
}

// Tokens are w's linked tokens, by source and owner.
func (p *People) Tokens(w Who) []Token {
	p.mu.Lock()
	defer p.mu.Unlock()
	pp := p.people[p.id(w.Subject)]
	if pp == nil {
		return nil
	}
	out := make([]Token, 0, len(pp.tokens))
	for _, t := range pp.tokens {
		out = append(out, view(t))
	}
	slices.SortFunc(out, func(a, b Token) int { return strings.Compare(a.Source+"/"+a.Owner, b.Source+"/"+b.Owner) })
	return out
}
