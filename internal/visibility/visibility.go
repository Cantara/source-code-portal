// Package visibility is who sees what (design/visibility.md, VS1): every product and repository has an audience, everyone, only
// signed-in viewers (those who see private repositories) or no one, and an item everyone sees may be kept from search engines. The
// admins' choices are the document `visibility`; a repository's audience is resolved from its own choice, then the first rule whose
// pattern matches its address, then its products, then the default (everyone, offered to search engines). GitHub's private and
// internal flags are a ceiling: such a repository is never shown to everyone. Every repository is read from GitHub whatever its
// audience, webhook events included.
package visibility

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/cantara/source-code-portal/internal/docstore"
	"github.com/cantara/source-code-portal/internal/snapshot"
)

// Name is the stored document.
const Name = "visibility"

// The audiences, as stored and answered.
const (
	Everyone = "everyone"  // the public sees it
	SignedIn = "signed-in" // only viewers who see private repositories see it
	NoOne    = "no-one"    // no view holds it; the portal still reads it
)

const (
	maxBytes   = 256 << 10 // the document or a request body
	maxEntries = 4000      // groups and repositories each
	maxPattern = 256       // characters of a pattern
	maxRules   = 50
)

var (
	groupID = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	repoKey = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*/[a-z0-9._-]+$`)
)

// Setting is a choice: who sees the item and, when everyone does, whether search engines are offered it.
type Setting struct {
	Audience string `json:"audience"`
	Search   bool   `json:"search"`
}

// Rule sets the audience of every repository whose address (owner/name) its pattern matches: an RE2 regular expression, not
// anchored, case-insensitive. The first rule that matches wins.
type Rule struct {
	Pattern string `json:"pattern"`
	Setting

	re *regexp.Regexp
}

// Doc is the stored document: the choices made on products (by id) and repositories (by address, lower case), and the rules.
type Doc struct {
	Groups map[string]Setting `json:"groups"`
	Repos  map[string]Setting `json:"repos"`
	Rules  []Rule             `json:"rules"`
}

// Empty is the document before anything is chosen.
func Empty() Doc {
	return Doc{Groups: map[string]Setting{}, Repos: map[string]Setting{}, Rules: []Rule{}}
}

// Parse decodes and checks the stored document: at most 256 KiB of one JSON object, unknown fields refused.
func Parse(b []byte) (Doc, error) {
	if len(b) > maxBytes {
		return Doc{}, fmt.Errorf("larger than %d KiB", maxBytes>>10)
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var d Doc
	if err := dec.Decode(&d); err != nil {
		return Doc{}, fmt.Errorf("not a visibility document: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return Doc{}, errors.New("not a visibility document: more than one JSON value")
	}
	return clean(d)
}

// clean checks d and returns it in its stored form: lower-case keys, compiled rules, empty collections (never null), and search on
// for an item not everyone sees (search engines are never offered it, so the switch means nothing there).
func clean(d Doc) (Doc, error) {
	out := Empty()
	if len(d.Groups) > maxEntries || len(d.Repos) > maxEntries {
		return Doc{}, fmt.Errorf("at most %d products and %d repositories", maxEntries, maxEntries)
	}
	for id, st := range d.Groups {
		id = strings.ToLower(strings.TrimSpace(id))
		if len(id) > 63 || !groupID.MatchString(id) {
			return Doc{}, fmt.Errorf("groups: %q is not a product id", id)
		}
		var err error
		if out.Groups[id], err = setting("groups."+id, st); err != nil {
			return Doc{}, err
		}
	}
	for addr, st := range d.Repos {
		addr = strings.ToLower(strings.TrimSpace(addr))
		if len(addr) > 200 || !repoKey.MatchString(addr) {
			return Doc{}, fmt.Errorf("repos: %q is not an address (owner/name)", addr)
		}
		var err error
		if out.Repos[addr], err = setting("repos."+addr, st); err != nil {
			return Doc{}, err
		}
	}
	if len(d.Rules) > maxRules {
		return Doc{}, fmt.Errorf("rules: at most %d", maxRules)
	}
	for i, r := range d.Rules {
		p := strings.TrimSpace(r.Pattern)
		if p == "" {
			continue
		}
		if len(p) > maxPattern {
			return Doc{}, fmt.Errorf("rules[%d]: longer than %d characters", i, maxPattern)
		}
		re, err := regexp.Compile("(?i)" + p)
		if err != nil {
			return Doc{}, fmt.Errorf("rules[%d]: %q is not a regular expression: %w", i, p, err)
		}
		st, err := setting(fmt.Sprintf("rules[%d]", i), r.Setting)
		if err != nil {
			return Doc{}, err
		}
		out.Rules = append(out.Rules, Rule{Pattern: p, Setting: st, re: re})
	}
	return out, nil
}

func setting(field string, st Setting) (Setting, error) {
	switch st.Audience {
	case Everyone:
	case SignedIn, NoOne:
		st.Search = true
	default:
		return Setting{}, fmt.Errorf("%s: audience %q is not one of %s, %s, %s", field, st.Audience, Everyone, SignedIn, NoOne)
	}
	return st, nil
}

// Why an item has its audience.
const (
	FromRepo    = "repository" // chosen on the repository
	FromRule    = "rule"       // a rule's pattern matches its address (Effective.Rule)
	FromProduct = "product"    // its products (Effective.Product: the one that decided)
	FromDefault = "default"    // nothing chosen
)

// Effective is an item's resolved audience and why. Capped is true when GitHub's private or internal flag lowered everyone to
// signed-in.
type Effective struct {
	Audience string `json:"audience"`
	Search   bool   `json:"search"`
	Why      string `json:"why"`
	Rule     int    `json:"rule"`    // the rule's index when Why is FromRule, else -1
	Product  string `json:"product"` // the product's id when Why is FromProduct
	Capped   bool   `json:"capped"`
}

// rank orders the audiences from the narrowest to the widest.
func rank(audience string) int {
	switch audience {
	case Everyone:
		return 2
	case SignedIn:
		return 1
	default:
		return 0
	}
}

// Group is the audience of the product id: its own choice, else the default.
func (d Doc) Group(id string) Effective {
	if st, ok := d.Groups[id]; ok {
		return Effective{Audience: st.Audience, Search: st.Search, Why: FromProduct, Rule: -1, Product: id}
	}
	return Effective{Audience: Everyone, Search: true, Why: FromDefault, Rule: -1}
}

// Repo is the audience of the repository at key, in the products groups (ids), public or not on GitHub.
func (d Doc) Repo(key snapshot.RepoKey, groups []string, public bool) Effective {
	e := d.resolve(key, groups)
	if !public && e.Audience == Everyone {
		e.Audience, e.Search, e.Capped = SignedIn, true, true
	}
	if e.Audience != Everyone {
		e.Search = true // never offered to search engines: the switch means nothing
	}
	return e
}

func (d Doc) resolve(key snapshot.RepoKey, groups []string) Effective {
	if st, ok := d.Repos[string(key)]; ok {
		return Effective{Audience: st.Audience, Search: st.Search, Why: FromRepo, Rule: -1}
	}
	for i, r := range d.Rules {
		if r.re != nil && r.re.MatchString(string(key)) {
			return Effective{Audience: r.Audience, Search: r.Search, Why: FromRule, Rule: i}
		}
	}
	if len(groups) == 0 {
		return Effective{Audience: Everyone, Search: true, Why: FromDefault, Rule: -1}
	}
	// The widest audience among its products; at that audience, offered to search engines when any of them is. A product without a
	// choice counts as the default, so it decides only when no product chose (then the answer is the default's).
	var best Effective
	chosen := false
	for _, id := range groups {
		g := d.Group(id)
		if g.Why == FromProduct {
			chosen = true
		}
		switch {
		case best.Why == "" || rank(g.Audience) > rank(best.Audience):
			best = g
		case rank(g.Audience) == rank(best.Audience) && g.Search && !best.Search:
			best.Search, best.Product, best.Why = true, g.Product, g.Why
		}
	}
	if !chosen {
		return Effective{Audience: Everyone, Search: true, Why: FromDefault, Rule: -1}
	}
	return best
}

// Set is who sees what: the configuration's `visibility:` (the base) with the admins' changes, the stored overrides
// (design/config-baseline.md).
type Set struct {
	overlay *docstore.Overlay[Doc, Overrides]
	changed func()
}

// Open reads the stored overrides of base from db (nil: base alone, and nothing can be saved). A store that has no `visibility`
// document is given one made from the `parked` and `indexing` documents it holds (Import), which are left as they are. changed is
// called after every save, so the snapshot is published again with the new audiences.
func Open(ctx context.Context, db docstore.Backend, base Doc, changed func(), log *slog.Logger) *Set {
	if changed == nil {
		changed = func() {}
	}
	log = log.With("store", Name)
	if db != nil {
		if err := Import(ctx, db); err != nil {
			log.Error("the parked and search-engine lists were not taken over; they are still stored", "err", err)
		}
	}
	ops := docstore.Ops[Doc, Overrides]{Merge: Merge, Diff: Diff, Count: Overrides.Count, Parse: ParseOverrides}
	return &Set{overlay: docstore.OpenOverlay(ctx, db, Name, base, ops, log), changed: changed}
}

// Problems is why the stored document is not served, for the admin status.
func (s *Set) Problems() []string {
	if p := s.overlay.State().Problem; p != "" {
		return []string{Name + ": " + p + "; the configuration's choices apply until it is saved again"}
	}
	return nil
}

// Changes is how many choices are kept only in the store, not in the configuration.
func (s *Set) Changes() int { return s.overlay.Changes() }

// Doc is the document in force.
func (s *Set) Doc() Doc { return s.overlay.Value() }

// Base is the configuration's `visibility:`.
func (s *Set) Base() Doc { return s.overlay.Base() }

// Revision identifies the stored content: a sitemap built from it is built again when it changes.
func (s *Set) Revision() string { return s.overlay.State().Revision }

// Audience is what snapshot.Publisher.Audience asks of a repository in the groups (ids).
func (s *Set) Audience(e *snapshot.RepoEntry, groups []string) snapshot.Audience {
	switch s.Doc().Repo(e.Repo.Key(), groups, e.Repo.Public()).Audience {
	case NoOne:
		return snapshot.NoOne
	case SignedIn:
		return snapshot.SignedIn
	}
	return snapshot.Everyone
}

// Indexed reports whether the repository at key, in the view v, is offered to search engines: everyone sees it and its search switch
// is on.
func (s *Set) Indexed(v *snapshot.Snapshot, key snapshot.RepoKey) bool {
	e, _ := v.Repo(key)
	if e == nil {
		return false
	}
	eff := s.Doc().Repo(e.Repo.Key(), v.GroupsOf(e.Repo.Key()), e.Repo.Public())
	return eff.Audience == Everyone && eff.Search
}

// IndexedGroup reports whether the product id's page is offered to search engines: everyone sees the product and its switch is on.
func (s *Set) IndexedGroup(id string) bool {
	g := s.Doc().Group(id)
	return g.Audience == Everyone && g.Search
}

// NoIndex reports whether the page at the address path, in the public view v, says noindex: a repository's page, document or
// Markdown, or a product's page, that is not offered to search engines.
func (s *Set) NoIndex(v *snapshot.Snapshot, path string) bool {
	if rest, ok := strings.CutPrefix(path, "/repos/"); ok {
		parts := strings.SplitN(rest, "/", 3)
		if len(parts) < 2 {
			return false
		}
		key := snapshot.KeyOf(parts[0], parts[1])
		if e, _ := v.Repo(key); e == nil {
			return false // not a page the public reads: the answer is a 404
		}
		return !s.Indexed(v, key)
	}
	if rest, ok := strings.CutPrefix(path, "/groups/"); ok {
		id, _, _ := strings.Cut(rest, "/")
		return !s.IndexedGroup(id)
	}
	return false
}

// sorted is d's keys in order, for answers that list them.
func sorted[V any](m map[string]V) []string { return slices.Sorted(maps.Keys(m)) }
