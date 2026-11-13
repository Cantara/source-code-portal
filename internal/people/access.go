package people

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"slices"
	"time"

	"github.com/cantara/source-code-portal/internal/github"
	"github.com/cantara/source-code-portal/internal/snapshot"
	"github.com/cantara/source-code-portal/internal/store"
)

// retryAfter is how long a listing that failed for a reason that passes (GitHub unavailable, a rate limit) waits to be tried again.
const retryAfter = time.Minute

// Run lists each person's tokens' repositories until ctx ends (design/enterprise.md, EN5): a token at once after it is linked, and
// again when its listing is older than Options.Refresh. A listing GitHub refuses (the token expired, was revoked or is refused by
// the enterprise's rules) drops what the token read; one that fails otherwise is tried again in a minute, and once its listing is
// older than Options.MaxAge it counts for nothing (fail closed).
func (p *People) Run(ctx context.Context) {
	for {
		next := p.listDue(ctx)
		timer := time.NewTimer(next)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-p.kick:
		case <-timer.C:
		}
		timer.Stop()
	}
}

func (p *People) wake() {
	select {
	case p.kick <- struct{}{}:
	default:
	}
}

func (p *People) changed(source string) {
	if p.o.Changed != nil {
		p.o.Changed(source)
	}
}

type due struct {
	person string
	slot   slot
	token  store.PersonToken
}

// listDue lists every token whose listing is due and returns how long until the next one is.
func (p *People) listDue(ctx context.Context) time.Duration {
	now := p.o.Now()
	next := p.o.Refresh
	var todo []due
	p.mu.Lock()
	for id, pp := range p.people {
		for sl, t := range pp.tokens {
			if t.State != "ok" {
				continue
			}
			at := t.ListedAt.Add(p.o.Refresh)
			if _, listed := pp.listed[sl]; !listed { // listed before the start: its repositories are not in memory
				at = now
			}
			if r, ok := pp.retry[sl]; ok && r.After(at) {
				at = r
			}
			if !at.After(now) {
				todo = append(todo, due{id, sl, t})
				continue
			}
			next = min(next, at.Sub(now))
		}
	}
	p.mu.Unlock()
	slices.SortFunc(todo, func(a, b due) int { return a.token.ListedAt.Compare(b.token.ListedAt) })
	for _, d := range todo {
		if ctx.Err() != nil {
			return next
		}
		if p.list(ctx, d) {
			next = min(next, retryAfter)
		}
	}
	return max(next, time.Second)
}

// list lists one token's repositories and records what it read; it reports whether the listing is to be tried again soon.
func (p *People) list(ctx context.Context, d due) (again bool) {
	src := p.sources[d.slot.source]
	token, err := p.sealer.open(d.person, d.slot.source, d.slot.owner, d.token.Sealed)
	if err != nil { // sealed under another key: the token cannot be used until it is linked again
		p.o.Log.Error("a token does not open: it is to be linked again", "owner", d.slot.owner, "err", err)
		p.refused(ctx, d, "sealed")
		return false
	}
	repos, err := src.List(ctx, d.slot.owner, github.Holder{Key: holderKey(d.person, d.slot, token), Token: token})
	var e *github.Error
	switch {
	case err == nil:
	case errors.As(err, &e) && (e.Kind == github.Auth || e.Code == "forbidden" || e.Kind == github.NotFound):
		p.o.Log.Info("GitHub refused a person's token: what it read is dropped", "owner", d.slot.owner, "code", e.Code)
		p.refused(ctx, d, e.Code)
		return false
	default:
		if ctx.Err() == nil {
			p.o.Log.Warn("a person's repositories could not be listed; tried again in a minute", "owner", d.slot.owner, "err", err)
		}
		p.mu.Lock()
		if pp := p.people[d.person]; pp != nil {
			pp.retry[d.slot] = p.o.Now().Add(retryAfter)
		}
		p.mu.Unlock()
		return true
	}
	ids := make([]int64, 0, len(repos))
	for _, r := range repos {
		ids = append(ids, r.ID.GitHubID)
	}
	slices.Sort(ids)
	now := p.o.Now()
	if err := p.o.Store.SetAccess(ctx, store.Access{Person: d.person, Source: d.slot.source, Owner: d.slot.owner, Repos: ids}, now); err != nil {
		if !errors.Is(err, store.ErrNoPersonToken) && ctx.Err() == nil { // a token removed meanwhile keeps nothing
			p.o.Log.Error("a person's repositories could not be kept", "err", err)
		}
		return !errors.Is(err, store.ErrNoPersonToken)
	}
	p.mu.Lock()
	pp := p.people[d.person]
	t, ok := pp.tokens[d.slot]
	if ok && slices.Equal(t.Sealed, d.token.Sealed) { // not replaced meanwhile
		t.ListedAt, t.State = now, "ok"
		pp.tokens[d.slot] = t
		pp.access[d.slot] = ids
		pp.listed[d.slot] = repos
		delete(pp.retry, d.slot)
	}
	p.mu.Unlock()
	p.changed(d.slot.source)
	return false
}

// refused records that GitHub refused a token (state) and drops what it read.
func (p *People) refused(ctx context.Context, d due, state string) {
	if err := p.o.Store.SetTokenState(ctx, d.person, d.slot.source, d.slot.owner, state); err != nil && ctx.Err() == nil {
		p.o.Log.Error("a refused token could not be recorded", "err", err)
	}
	p.mu.Lock()
	if pp := p.people[d.person]; pp != nil {
		if t, ok := pp.tokens[d.slot]; ok && slices.Equal(t.Sealed, d.token.Sealed) {
			t.State, t.ListedAt = state, time.Time{}
			pp.tokens[d.slot] = t
			delete(pp.access, d.slot)
			delete(pp.listed, d.slot)
		}
	}
	p.mu.Unlock()
	p.changed(d.slot.source)
}

// holderKey names a person's token for one owner, and changes with the token.
func holderKey(person string, sl slot, token []byte) string {
	sum := sha256.Sum256(append([]byte(person+"\x00"+sl.source+"\x00"+sl.owner+"\x00"), token...))
	return hex.EncodeToString(sum[:12])
}

// Allowed is what the person with subject may read: the repositories their tokens listed within Options.MaxAge, and a key equal for
// equal sets. Nothing for a person unknown, without a token or whose listings are all older (fail closed).
func (p *People) Allowed(subject string) (key string, repos map[snapshot.RepoID]bool) {
	now := p.o.Now()
	p.mu.Lock()
	defer p.mu.Unlock()
	pp := p.people[p.id(subject)]
	if pp == nil {
		return "", nil
	}
	var ids []snapshot.RepoID
	for sl, t := range pp.tokens {
		if t.State != "ok" || t.ListedAt.IsZero() || now.Sub(t.ListedAt) > p.o.MaxAge {
			continue
		}
		for _, id := range pp.access[sl] {
			ids = append(ids, snapshot.RepoID{Source: sl.source, GitHubID: id})
		}
	}
	if len(ids) == 0 {
		return "", nil
	}
	slices.SortFunc(ids, func(a, b snapshot.RepoID) int {
		if a.Source != b.Source {
			if a.Source < b.Source {
				return -1
			}
			return 1
		}
		return int(min(max(a.GitHubID-b.GitHubID, -1), 1))
	})
	ids = slices.Compact(ids)
	h := sha256.New()
	repos = make(map[snapshot.RepoID]bool, len(ids))
	var n [8]byte
	for _, id := range ids {
		repos[id] = true
		h.Write([]byte(id.Source + "\x00"))
		binary.BigEndian.PutUint64(n[:], uint64(id.GitHubID)) //nolint:gosec // G115: a GitHub id is positive
		h.Write(n[:])
	}
	return hex.EncodeToString(h.Sum(nil)[:16]), repos
}
