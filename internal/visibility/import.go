package visibility

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cantara/source-code-portal/internal/docstore"
)

// The documents of 2.2.19 and before that this one replaces: the parked repositories and what was kept out of search engines.
const (
	oldParked   = "parked"
	oldIndexing = "indexing"
)

// Import gives a store that has no `visibility` document one made from the old `parked` and `indexing` documents, once: a parked
// repository is seen by no one; an excluded product or repository is seen by everyone and kept from search engines; the include
// patterns become rules that offer the matching repositories to search engines, ahead of the exclude patterns, which become rules
// that keep them out. The old documents are not changed, so an older release still reads them after a rollback. Nothing to take
// over, nothing is written.
func Import(ctx context.Context, db docstore.Backend) error {
	if _, _, ok, err := db.Document(ctx, Name); err != nil || ok {
		return err
	}
	d := Empty()
	found := false
	if b, _, ok, err := db.Document(ctx, oldParked); err != nil {
		return err
	} else if ok {
		var p struct {
			Repos []string `json:"repos"`
		}
		if err := json.Unmarshal(b, &p); err != nil {
			return fmt.Errorf("%s: %w", oldParked, err)
		}
		for _, r := range p.Repos {
			d.Repos[strings.ToLower(r)] = Setting{Audience: NoOne, Search: true}
		}
		found = found || len(p.Repos) > 0
	}
	if b, _, ok, err := db.Document(ctx, oldIndexing); err != nil {
		return err
	} else if ok {
		var x struct {
			Groups  []string `json:"groups"`
			Repos   []string `json:"repos"`
			Exclude []string `json:"exclude"`
			Include []string `json:"include"`
		}
		if err := json.Unmarshal(b, &x); err != nil {
			return fmt.Errorf("%s: %w", oldIndexing, err)
		}
		for _, g := range x.Groups {
			d.Groups[strings.ToLower(g)] = Setting{Audience: Everyone, Search: false}
		}
		for _, r := range x.Repos {
			if _, parked := d.Repos[strings.ToLower(r)]; !parked {
				d.Repos[strings.ToLower(r)] = Setting{Audience: Everyone, Search: false}
			}
		}
		for _, p := range x.Include {
			d.Rules = append(d.Rules, Rule{Pattern: p, Setting: Setting{Audience: Everyone, Search: true}})
		}
		for _, p := range x.Exclude {
			d.Rules = append(d.Rules, Rule{Pattern: p, Setting: Setting{Audience: Everyone, Search: false}})
		}
		found = found || len(x.Groups)+len(x.Repos)+len(x.Include)+len(x.Exclude) > 0
	}
	if !found {
		return nil
	}
	d, err := clean(d)
	if err != nil {
		return err
	}
	b, err := encode(d)
	if err != nil {
		return err
	}
	return db.PutDocument(ctx, Name, b, docstore.Digest(b), "")
}

// encode is the stored text of d.
func encode(d Doc) ([]byte, error) {
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}
