package commits

import (
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/readmodel"
	"github.com/cantara/source-code-portal/internal/scm"
)

func bigEnv(b *testing.B, n int) (*env, []scm.Repository) {
	b.Helper()
	t := &testing.T{}
	var rs []scm.Repository
	for i := 0; i < n; i++ {
		rs = append(rs, repo(fmt.Sprintf("repo-%04d", i), scm.VisPublic))
	}
	e := newEnv(t, false, rs...)
	now := time.Now().UTC()
	_, err := e.pub.Apply(func(bd *readmodel.Builder) error {
		for i, r := range rs {
			v := View{Latest: commitList(30, i%97, now), Info: freshness.Info{State: freshness.StateOK, FetchedAt: now, TTL: time.Hour}}
			if err := readmodel.Put(bd, readmodel.EntryID(r.Slug), e.d.View, v); err != nil {
				return err
			}
		}
		// 50 groups of 40 repositories plus the catch-all group
		groups := []scm.GroupID{"all"}
		members := map[scm.GroupID][]scm.Slug{}
		for _, r := range rs {
			members["all"] = append(members["all"], r.Slug)
		}
		for g := 0; g < 50; g++ {
			id := scm.GroupID(fmt.Sprintf("g%02d", g))
			groups = append(groups, id)
			for j := 0; j < 40; j++ {
				members[id] = append(members[id], rs[(g*40+j)%n].Slug)
			}
		}
		return readmodel.PutGlobal(bd, e.d.Members, testMembership(b, rs, groups, members))
	})
	if err != nil {
		b.Fatal(err)
	}
	return e, rs
}

// BenchmarkPublish measures one real publish at 1,000 repositories and 51 groups of 40 members: Apply with one Put of
// one repository's view, the commits.latest projector (both classes), the existence check and the commits validator.
func BenchmarkPublish(b *testing.B) {
	e, rs := bigEnv(b, 1000)
	now := time.Now().UTC()
	views := make([]View, 7)
	for i := range views {
		views[i] = View{Latest: commitList(30, i*3, now), Info: freshness.Info{State: freshness.StateOK, FetchedAt: now, TTL: time.Hour}}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := e.pub.Apply(func(bd *readmodel.Builder) error {
			return readmodel.Put(bd, readmodel.EntryID(rs[i%len(rs)].Slug), e.d.View, views[i%len(views)])
		}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkResolve(b *testing.B) {
	e, rs := bigEnv(b, 1000)
	snap := e.pub.Current()
	ref := rs[999].Ref
	if ref.ID == rs[0].Ref.ID {
		b.Fatal("ids must be unique")
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, _, ok := e.svc.resolve(snap, ref); !ok {
			b.Fatal("miss")
		}
	}
}

// BenchmarkList measures Service.List (without JSON encoding) over the 1,000-repository snapshot: the precomputed first
// page, a first page of the merge path (limit at the feed length), a cursor into the middle of the result, and a group page.
func BenchmarkList(b *testing.B) {
	e, _ := bigEnv(b, 1000)
	r := httptest.NewRequest("GET", "/api/v1/commits", nil)
	ctx := r.Context()
	lim := func(n int) *int { return &n }
	first, err := e.svc.List(ctx, r, ListQuery{Limit: lim(200)})
	if err != nil || first.Page.NextCursor == nil {
		b.Fatal(err, first.Page)
	}
	deep, err := e.svc.List(ctx, r, ListQuery{Limit: lim(200), Cursor: *first.Page.NextCursor})
	if err != nil || deep.Page.NextCursor == nil {
		b.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		q    ListQuery
	}{
		{"fast_first_50", ListQuery{Limit: lim(50)}},
		{"merge_first_200", ListQuery{Limit: lim(200)}},
		{"merge_cursor_50", ListQuery{Limit: lim(50), Cursor: *first.Page.NextCursor}},
		{"group_first_50", ListQuery{Group: "g07", Limit: lim(50)}},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := e.svc.List(ctx, r, tc.q); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
