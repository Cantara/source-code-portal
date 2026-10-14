package catalog

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/scm"
)

// RepoKey identifies a repository across sources (GitHub ids are only unique per host).
type RepoKey struct {
	Source scm.SourceID
	ID     scm.RepoID
}

func KeyOf(r scm.RepoRef) RepoKey { return RepoKey{r.Source, r.ID} }

// Tracked is one catalogue entry: what the portal knows about a repository besides the data other features hold in their own slots.
type Tracked struct {
	Repo      scm.Repository // Slug set. Repo.DefaultBranch is the GitHub default branch as listed.
	Aliases   []scm.Slug     // earlier slugs, newest first, at most MaxSlugAliasLen
	FirstSeen time.Time      // UTC
	SeenAt    time.Time      // UTC, last complete listing that contained it
	Misses    int            // consecutive complete listings without it (removal needs RemovalConfirmations)
}

// Held records that a mass removal is being held back (in memory only: a restart restarts the hold timer, which is the safe direction).
type Held struct {
	Fingerprint string
	Since       time.Time
	Candidates  int
}

// Quarantine is a listed repository that cannot be tracked (slug problem). It is reported, never fatal.
type Quarantine struct {
	Ref    scm.RepoRef
	Slug   scm.Slug
	Reason string // slug_collision, slug_reserved, slug_invalid, slug_too_long
	Detail string
}

// Removal names a repository leaving the catalogue and why.
type Removal struct {
	Ref    scm.RepoRef
	Reason string // absent, visibility, archived, fork, ungrouped, quarantined
}

// PlanInput is everything PlanReconcile needs; it performs no I/O and reads no clock.
type PlanInput struct {
	Source  scm.SourceID
	Policy  DiscoveryPolicy
	Listed  []scm.Repository // the COMPLETE listing of Source (every page); an incomplete listing is never planned
	Current []Tracked        // the whole catalogue, all sources
	Held    *Held            // the previous mass-removal hold of this source, if any
	Now     time.Time
	Prefix  func(src scm.SourceID, owner string) string // slug prefix, "" when none
	Grouped func(scm.Repository) bool                   // nil means catalog.ungrouped == track
}

// Plan is the pure result of one source's reconcile.
type Plan struct {
	Next        []Tracked // whole catalogue after the plan, sorted by folded slug
	Added       []scm.RepoRef
	Removed     []Removal
	Renamed     []Rename
	Updated     []scm.RepoRef
	Quarantined []Quarantine
	Excluded    map[Exclusion]int
	Held        *Held // non-nil: a mass removal of HeldCount repositories is held back
	HeldCount   int
	Findings    []Finding
}

// Rename is a slug change of a tracked repository (a rename, a transfer, or a changed slug prefix).
type Rename struct {
	Ref     scm.RepoRef
	OldSlug scm.Slug
	NewSlug scm.Slug
}

// guardFloor is the number of removals per pass that never triggers the mass-removal hold (so a three-repository portal can lose one).
const guardFloor = 2

// PlanReconcile is the reconcile algorithm of section 4.4 (steps R1-R8).
func PlanReconcile(in PlanInput) Plan {
	p := Plan{Excluded: map[Exclusion]int{}}
	pol := in.Policy

	// R1 normalise the listing: last duplicate wins; invalid entries are excluded.
	listed := map[RepoKey]scm.Repository{}
	for _, r := range in.Listed {
		r.Ref.Source = in.Source
		r, ok := normaliseListed(r)
		if !ok {
			p.Excluded[ExcludedInvalid]++
			continue
		}
		listed[KeyOf(r.Ref)] = r
	}
	// R2 classify every listed repository against the policy (visibility first).
	eligible := map[RepoKey]scm.Repository{}
	excl := map[RepoKey]Exclusion{}
	for _, k := range sortedKeys(listed) {
		r := listed[k]
		e := pol.Admit(r)
		if e == ExcludedNone && in.Grouped != nil && !in.Grouped(r) {
			e = ExcludedUngrouped
		}
		if e != ExcludedNone {
			p.Excluded[e]++
			excl[k] = e
			continue
		}
		eligible[k] = r
	}

	// R3 classify the repositories this source already tracks.
	cur := map[RepoKey]Tracked{}
	var others []Tracked
	for _, t := range in.Current {
		if t.Repo.Ref.Source == in.Source {
			cur[KeyOf(t.Repo.Ref)] = t
		} else {
			others = append(others, t)
		}
	}
	keep := map[RepoKey]Tracked{}
	var candidates []Removal // removals subject to confirmation and the mass guard
	for _, k := range sortedTracked(cur) {
		t := cur[k]
		if r, ok := eligible[k]; ok {
			t.Misses, t.SeenAt = 0, in.Now
			if !sameRepo(t.Repo, r) {
				p.Updated = append(p.Updated, r.Ref)
			}
			slug := t.Repo.Slug
			t.Repo = r
			t.Repo.Slug = slug // recomputed in R6
			keep[k] = t
			continue
		}
		if e, ok := excl[k]; ok { // listed but no longer eligible: the listing says so explicitly
			if e == ExcludedVisibility {
				p.Removed = append(p.Removed, Removal{t.Repo.Ref, e.String()}) // security: immediate, never held
				continue
			}
			t.Repo = listed[k]
			t.Repo.Slug = cur[k].Repo.Slug
			keep[k] = t
			candidates = append(candidates, Removal{t.Repo.Ref, e.String()})
			continue
		}
		// absent from a complete listing
		t.Misses++
		keep[k] = t
		if t.Misses >= pol.RemovalConfirmations {
			candidates = append(candidates, Removal{t.Repo.Ref, "absent"})
		}
	}

	// R4 the mass-removal guard.
	prev := len(cur)
	allowed := max(int(pol.MaxRemovalRatio*float64(prev)), guardFloor)
	var added []RepoKey
	for _, k := range sortedKeys(eligible) {
		if _, ok := cur[k]; !ok {
			added = append(added, k)
		}
	}
	resultEmpty := prev > 0 && prev-len(candidates)+len(added) == 0
	if len(candidates) > 0 && (len(candidates) > allowed || resultEmpty) {
		fp := fingerprint(candidates)
		since := in.Now
		if in.Held != nil && in.Held.Fingerprint == fp {
			since = in.Held.Since
		}
		if in.Held != nil && in.Held.Fingerprint == fp && in.Now.Sub(in.Held.Since) >= pol.MassRemovalHold {
			// the same removal has been observed for the whole hold period: apply it
		} else {
			p.Held = &Held{Fingerprint: fp, Since: since, Candidates: len(candidates)}
			p.HeldCount = len(candidates)
			p.Findings = append(p.Findings, Finding{Code: "R301", Severity: SevWarning, Path: "/sources/" + string(in.Source),
				Message: fmt.Sprintf("holding back the removal of %d of %d repositories (limit %d per pass); applies after %s if the listing stays the same",
					len(candidates), prev, allowed, pol.MassRemovalHold),
				Hint: "raise discovery.max_removal_ratio if this is intended"})
			candidates = nil
		}
	}
	for _, c := range candidates {
		delete(keep, KeyOf(c.Ref))
		p.Removed = append(p.Removed, c)
	}

	// R5 additions.
	for _, k := range added {
		r := eligible[k]
		keep[k] = Tracked{Repo: r, FirstSeen: in.Now, SeenAt: in.Now}
		p.Added = append(p.Added, r.Ref)
	}

	// R6 slugs: derive, then resolve collisions across all sources with a stable winner.
	type cand struct {
		k    RepoKey
		slug scm.Slug
		t    Tracked
		held bool // already owned this slug before this plan
	}
	taken := map[string]RepoKey{}
	for _, o := range others {
		taken[FoldSlug(o.Repo.Slug)] = KeyOf(o.Repo.Ref)
	}
	var cs []cand
	for _, k := range sortedTracked(keep) {
		t := keep[k]
		prefix := ""
		if in.Prefix != nil {
			prefix = in.Prefix(in.Source, t.Repo.Ref.Owner)
		}
		slug, err := MakeSlug(prefix, t.Repo.Ref.Name)
		if err != nil {
			se := err.(*SlugError)
			p.quarantine(t, "slug_"+se.Code, se.Msg)
			delete(keep, k)
			if _, was := cur[k]; was {
				p.Removed = append(p.Removed, Removal{t.Repo.Ref, "quarantined"})
			}
			continue
		}
		cs = append(cs, cand{k, slug, t, cur[k].Repo.Slug == slug})
	}
	sort.SliceStable(cs, func(i, j int) bool {
		a, b := cs[i], cs[j]
		if a.held != b.held {
			return a.held
		}
		if !a.t.FirstSeen.Equal(b.t.FirstSeen) {
			return a.t.FirstSeen.Before(b.t.FirstSeen)
		}
		if a.k.Source != b.k.Source {
			return a.k.Source < b.k.Source
		}
		return a.k.ID < b.k.ID
	})
	for _, c := range cs {
		f := FoldSlug(c.slug)
		if owner, clash := taken[f]; clash && owner != c.k {
			p.quarantine(c.t, "slug_collision", fmt.Sprintf("slug %q is already used by repository id %d of source %s", c.slug, owner.ID, owner.Source))
			delete(keep, c.k)
			if _, was := cur[c.k]; was {
				p.Removed = append(p.Removed, Removal{c.t.Repo.Ref, "quarantined"})
			}
			continue
		}
		taken[f] = c.k
		t := keep[c.k]
		if old, was := cur[c.k]; was && old.Repo.Slug != c.slug {
			t.Aliases = pushAlias(t.Aliases, old.Repo.Slug, c.slug)
			p.Renamed = append(p.Renamed, Rename{Ref: t.Repo.Ref, OldSlug: old.Repo.Slug, NewSlug: c.slug})
		}
		t.Repo.Slug = c.slug
		keep[c.k] = t
	}

	// R7 assemble the next catalogue, sorted.
	next := slices.Clone(others)
	for _, k := range sortedTracked(keep) {
		next = append(next, keep[k])
	}
	sort.SliceStable(next, func(i, j int) bool { return FoldSlug(next[i].Repo.Slug) < FoldSlug(next[j].Repo.Slug) })
	p.Next = next
	sortRefs(p.Added)
	sortRefs(p.Updated)
	sort.Slice(p.Removed, func(i, j int) bool { return p.Removed[i].Ref.ID < p.Removed[j].Ref.ID })
	sort.Slice(p.Renamed, func(i, j int) bool { return p.Renamed[i].Ref.ID < p.Renamed[j].Ref.ID })
	sort.Slice(p.Quarantined, func(i, j int) bool { return p.Quarantined[i].Ref.ID < p.Quarantined[j].Ref.ID })
	for _, q := range p.Quarantined {
		p.Findings = append(p.Findings, Finding{Code: "R302", Severity: SevError, Path: "/repos/" + q.Ref.Key(), Message: fmt.Sprintf("%s: %s", q.Reason, q.Detail),
			Hint: "set catalog.slug_prefix_by_source for this source or rename the repository"})
	}
	SortFindings(p.Findings)
	return p
}

func (p *Plan) quarantine(t Tracked, reason, detail string) {
	p.Quarantined = append(p.Quarantined, Quarantine{Ref: t.Repo.Ref, Slug: t.Repo.Slug, Reason: reason, Detail: detail})
}

// pushAlias puts old at the front, drops duplicates and the current slug, and caps the list.
func pushAlias(as []scm.Slug, old, current scm.Slug) []scm.Slug {
	out := []scm.Slug{old}
	for _, a := range as {
		if a != old && FoldSlug(a) != FoldSlug(current) {
			out = append(out, a)
		}
	}
	if len(out) > MaxSlugAliasLen {
		out = out[:MaxSlugAliasLen]
	}
	return out
}

func fingerprint(rs []Removal) string {
	ks := make([]string, 0, len(rs))
	for _, r := range rs {
		ks = append(ks, fmt.Sprintf("%s/%d/%s", r.Ref.Source, r.Ref.ID, r.Reason))
	}
	sort.Strings(ks)
	sum := sha256.Sum256([]byte(strings.Join(ks, "\n")))
	return hex.EncodeToString(sum[:8])
}

func sameRepo(a, b scm.Repository) bool {
	return a.Ref == b.Ref && a.Description == b.Description && a.DefaultBranch == b.DefaultBranch && slices.Equal(a.Topics, b.Topics) &&
		a.Visibility == b.Visibility && a.Archived == b.Archived && a.Fork == b.Fork && a.HTMLURL == b.HTMLURL && a.CreatedAt.Equal(b.CreatedAt) && a.PushedAt.Equal(b.PushedAt)
}

func sortedKeys[V any](m map[RepoKey]V) []RepoKey {
	ks := make([]RepoKey, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Slice(ks, func(i, j int) bool { return ks[i].ID < ks[j].ID })
	return ks
}
func sortedTracked(m map[RepoKey]Tracked) []RepoKey { return sortedKeys(m) }
func sortRefs(rs []scm.RepoRef)                     { sort.Slice(rs, func(i, j int) bool { return rs[i].ID < rs[j].ID }) }
