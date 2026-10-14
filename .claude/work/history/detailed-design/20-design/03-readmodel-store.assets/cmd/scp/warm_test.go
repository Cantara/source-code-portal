package scp_test

// The warm-start protocol of design section 4.6 as an executable example: existence slot first, then every other slot,
// one Apply, orphans impossible by construction.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/readmodel"
	"github.com/cantara/source-code-portal/internal/store"
	"github.com/cantara/source-code-portal/internal/store/memory"
)

type repoVal = freshness.Value[string]

func TestWarmStartProtocol(t *testing.T) {
	ctx := context.Background()
	st := memory.New(memory.Options{})
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	k := store.RepoKey{Source: "s", Owner: "o", Name: "a"}
	if _, err := st.ReplaceCatalogue(ctx, []store.Repo{{Key: k, Slug: "a", Meta: []byte(`{"d":"x"}`), SeenAt: now}}); err != nil {
		t.Fatal(err)
	}
	_ = st.PutItem(ctx, store.Item{Key: store.ItemKey{Repo: k, Kind: "docs.refresh"}, State: "ok", FetchedAt: now, CheckedAt: now, Payload: []byte("readme")})

	reg := &readmodel.Registry{}
	repoSlot := readmodel.Register[repoVal](reg, "catalog.repo", readmodel.Existence())
	docsSlot := readmodel.Register[repoVal](reg, "docs.repo")
	pub := readmodel.NewPublisher(reg, nil, nil, readmodel.Deps{})

	// 1. all store I/O and decoding happen BEFORE Apply, with no lock held (Apply's fn runs under the publish mutex and MUST NOT do I/O)
	cat, err := st.LoadCatalogue(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var loaded []store.Item
	if err := st.LoadItems(ctx, func(it store.Item) error { // the callback only collects (or decodes); it never calls the store
		if it.Key.Kind == "docs.refresh" {
			loaded = append(loaded, it)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	bySlug := map[store.RepoKey]readmodel.EntryID{}
	// 2. one Apply, no I/O: the existence slot first, then every other slot
	_, err = pub.Apply(func(b *readmodel.Builder) error {
		for _, r := range cat {
			bySlug[r.Key] = readmodel.EntryID(r.Slug)
			if err := readmodel.Put(b, bySlug[r.Key], repoSlot, repoVal{V: string(r.Meta), Info: freshness.Info{State: "ok", FetchedAt: r.SeenAt}}); err != nil {
				return err
			}
		}
		for _, it := range loaded {
			err := readmodel.Put(b, bySlug[it.Key.Repo], docsSlot, repoVal{V: string(it.Payload), Info: freshness.Info{State: it.State, FetchedAt: it.FetchedAt}})
			if errors.Is(err, readmodel.ErrUnknownRepo) {
				continue // cannot happen with a consistent store (FK), tolerated defensively
			}
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	s := pub.Current()
	if v, ok := readmodel.Get(s, "a", docsSlot); !ok || v.V != "readme" || s.Version() != 1 {
		t.Fatal("warm start did not build snapshot version 1")
	}
}
