package commits

import (
	"fmt"
	"time"

	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/readmodel"
	"github.com/cantara/source-code-portal/internal/scm"
)

// Slots are the snapshot handles commits reads and writes. cmd/scp creates View and Latest with RegisterSlots and the
// catalogue handles from the catalogue package's registration; the projector needs only Slots and Config, so it can be
// handed to readmodel.NewPublisher before the Service (which needs the Publisher) exists.
type Slots struct {
	Repos   readmodel.Slot[freshness.Value[scm.Repository]] // catalog.repo (existence slot)
	Stats   readmodel.Slot[freshness.Value[scm.RepoStats]]  // catalog.stats
	Members readmodel.Global[scm.Membership]                // catalog.membership
	Aliases readmodel.Global[map[scm.Slug]scm.Slug]         // catalog.aliases
	View    readmodel.Slot[View]                            // SlotName
	Latest  readmodel.Global[scm.Classed[Latest]]           // GlobalName
}

// RegisterSlots registers the two handles this package owns under their canonical names.
func RegisterSlots(reg *readmodel.Registry) (readmodel.Slot[View], readmodel.Global[scm.Classed[Latest]]) {
	return readmodel.Register[View](reg, SlotName), readmodel.RegisterGlobal[scm.Classed[Latest]](reg, GlobalName)
}

// Projection is the pure, publisher-side half of the package: the commits.latest projector and its validator.
type Projection struct {
	Slots
	cfg Config
}

func NewProjection(cfg Config, s Slots) *Projection { return &Projection{Slots: s, cfg: cfg} }

type repoEntry struct {
	slug scm.Slug
	vis  scm.Visibility
	src  Source
	info freshness.Info
	has  bool
}

// Projector derives the commits.latest global inside every Apply: for each view class the newest LatestN refs overall
// and per group, plus the freshness summary of the repositories behind each feed. It is pure and deterministic
// (a function of the candidate snapshot only) and allocation-bounded by the number of repositories.
func (p *Projection) Projector() readmodel.Projector {
	return func(b *readmodel.Builder) error {
		snap := b.View()
		repos := snap.Repos()
		entries := make(map[scm.Slug]*repoEntry, len(repos))
		order := make([]*repoEntry, 0, len(repos))
		nonPublic := false
		for _, id := range repos {
			rv, ok := readmodel.Get(snap, id, p.Repos)
			if !ok {
				continue
			}
			e := &repoEntry{slug: scm.Slug(id), vis: rv.V.Visibility}
			if v, ok := readmodel.Get(snap, id, p.View); ok {
				e.src = Source{Slug: e.slug, Commits: v.Latest}
				e.info, e.has = v.Info, true
			} else {
				e.src = Source{Slug: e.slug}
			}
			if e.vis != scm.VisPublic {
				nonPublic = true
			}
			entries[e.slug] = e
			order = append(order, e)
		}
		mem, _ := readmodel.GetGlobal(snap, p.Members)
		pub := p.buildLatest(order, entries, mem, scm.ViewPublic)
		all := pub
		if nonPublic {
			all = p.buildLatest(order, entries, mem, scm.ViewAll)
		}
		return readmodel.PutGlobal(b, p.Latest, scm.Classed[Latest]{Public: pub, All: all})
	}
}

func (p *Projection) buildLatest(order []*repoEntry, entries map[scm.Slug]*repoEntry, mem scm.Membership, class scm.ViewClass) Latest {
	n := p.cfg.LatestN + 1 // one sentinel beyond LatestN: it tells a page of exactly LatestN entries that more exist
	lat := Latest{ByGroup: map[scm.GroupID][]CommitRef{}, GroupInfo: map[scm.GroupID]FeedInfo{}}
	srcs := make([]Source, 0, len(order))
	var infos []*repoEntry
	for _, e := range order {
		if e.vis.Visible(class) {
			srcs = append(srcs, e.src)
			infos = append(infos, e)
		}
	}
	lat.All = refs(srcs, n)
	lat.AllInfo = p.feedInfo(infos)
	for _, g := range mem.Groups() {
		gs := srcs[:0:0]
		var gi []*repoEntry
		for _, slug := range mem.VisibleReposOf(g.ID, class) {
			if e, ok := entries[slug]; ok {
				gs = append(gs, e.src)
				gi = append(gi, e)
			}
		}
		lat.ByGroup[g.ID] = refs(gs, n)
		lat.GroupInfo[g.ID] = p.feedInfo(gi)
	}
	return lat
}

func refs(srcs []Source, n int) []CommitRef {
	es, _ := Merge(srcs, nil, n, nil)
	out := make([]CommitRef, len(es))
	for i, e := range es {
		out[i] = CommitRef{Slug: e.Slug, SHA: e.C.SHA, At: e.C.CommittedAt}
	}
	return out
}

func (p *Projection) feedInfo(es []*repoEntry) FeedInfo {
	fi := FeedInfo{Repos: len(es), TTL: p.cfg.RefreshTTL}
	var worst scm.Slug
	for _, e := range es {
		good := e.has && e.info.HasValue()
		if !good {
			fi.Never++
		} else if fi.OldestFetchedAt.IsZero() || e.info.FetchedAt.Before(fi.OldestFetchedAt) {
			fi.OldestFetchedAt = e.info.FetchedAt
		}
		if e.has && (e.info.State == freshness.StateError || e.info.ErrCode != "") {
			fi.Failing++
			if fi.ErrCode == "" || e.slug < worst {
				fi.ErrCode, worst = e.info.ErrCode, e.slug
			}
		}
	}
	return fi
}

// Validator rejects a candidate snapshot in which a feed refers to a commit that is not in its repository's view.
func (p *Projection) Validator() readmodel.Validator {
	return func(snap *readmodel.Snapshot) error {
		lat, ok := readmodel.GetGlobal(snap, p.Latest)
		if !ok {
			return nil // no projection yet (an Apply without projectors)
		}
		check := func(rs []CommitRef) error {
			for _, r := range rs {
				v, ok := readmodel.Get(snap, readmodel.EntryID(r.Slug), p.View)
				if !ok {
					return fmt.Errorf("commits.latest: %s has no view", r.Slug)
				}
				found := false
				for i := range v.Latest {
					if v.Latest[i].SHA == r.SHA {
						found = true
						break
					}
				}
				if !found {
					return fmt.Errorf("commits.latest: %s@%s not in the view", r.Slug, r.SHA)
				}
			}
			return nil
		}
		for _, l := range []Latest{lat.Public, lat.All} {
			if err := check(l.All); err != nil {
				return err
			}
			for _, rs := range l.ByGroup {
				if err := check(rs); err != nil {
					return err
				}
			}
		}
		return nil
	}
}

var _ = time.Second
