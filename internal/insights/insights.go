// Package insights answers what the data the portal holds says about the fleet (design/insights.md, P-83): how often it ships, what it
// is built on, what it leans on, where knowledge sits with one person and what has gone quiet. Everything is derived from the snapshot
// on request: no job, no GitHub call. It names repositories, never people. The features' data arrives as Facts, wired in cmd/scp.
package insights

import (
	"cmp"
	"context"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/health"
	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
	"github.com/cantara/source-code-portal/internal/scope"
	"github.com/cantara/source-code-portal/internal/snapshot"
)

// The thresholds of the insights, told with every answer.
const (
	Months              = 12 // months of releases counted
	Releases            = 5  // stable releases a product's median interval reads
	SingleAuthorShare   = 90 // percent of the people's commits one author wrote
	SingleAuthorCommits = 20 // people's commits that must be held before the rule judges
	Libraries           = 10 // shared libraries listed
	listed              = 10 // dependants named per library
	dormantListed       = 50 // dormant repositories listed
	day                 = 24 * time.Hour
)

// Facts is what the features hold of one repository that the insights read.
type Facts struct {
	Stable     []time.Time // stable releases held, newest first
	LatestTag  string      // the newest stable release's tag
	Language   string
	BuildTool  string
	Runtimes   [][2]string // toolchain name and version a manifest states
	UsedBy     []snapshot.RepoKey
	People     int // people's commits among the newest held
	Top        int // of them, the most by one author
	LastChange time.Time
}

// API serves GET /api/v1/insights. It implements the insights method of the strict server.
type API struct {
	Current func(context.Context) *snapshot.Snapshot // the viewer's view
	Facts   func(*snapshot.Snapshot, *snapshot.RepoEntry) Facts
	Retired func(*snapshot.RepoEntry) oapi.AlumniReason
	Fresh   func(*snapshot.RepoEntry) snapshot.Freshness // what the answer's freshness follows
}

func (a API) GetInsights(ctx context.Context, req oapi.GetInsightsRequestObject) (oapi.GetInsightsResponseObject, error) {
	s, now := a.Current(ctx), time.Now().UTC()
	_, in, missing := scope.Of(s, req.Params.Group, req.Params.Repo, scope.Feed[struct{}]{
		All:     func(*snapshot.Snapshot) []struct{} { return nil },
		OfGroup: func(*snapshot.Snapshot, string) []struct{} { return nil },
		OfRepo:  func(*snapshot.Snapshot, snapshot.RepoKey) []struct{} { return nil },
	})
	if missing != "" {
		return oapi.GetInsights404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse: oapi.NotFoundApplicationProblemPlusJSONResponse(
			oapi.NewProblem(http.StatusNotFound, "not-found", "Not Found", missing))}, nil
	}
	return oapi.GetInsights200JSONResponse{Body: a.build(s, in, now)}, nil
}

type row struct {
	key snapshot.RepoKey
	e   *snapshot.RepoEntry
	Facts
}

func (a API) build(s *snapshot.Snapshot, scopeKeys []snapshot.RepoKey, now time.Time) oapi.Insights {
	var live []row
	retired := map[snapshot.RepoKey]oapi.AlumniReason{}
	for _, k := range scopeKeys {
		e := s.Repos[k]
		if e == nil {
			continue
		}
		if why := a.Retired(e); why != "" {
			retired[k] = why
			continue
		}
		live = append(live, row{key: k, e: e, Facts: a.Facts(s, e)})
	}
	out := oapi.Insights{
		Repos: len(live),
		Thresholds: oapi.InsightThresholds{Months: Months, Releases: Releases, SingleAuthorShare: SingleAuthorShare,
			SingleAuthorCommits: SingleAuthorCommits, SharedLibraries: Libraries},
		Cadence:      oapi.InsightCadence{Months: months(live, now), Products: products(s, scopeKeys, live)},
		Technology:   technology(live),
		Libraries:    libraries(s, scopeKeys, a),
		SingleAuthor: singleAuthor(live),
		Ageing:       ageing(live, now),
		Freshness:    scope.Freshness(s, scopeKeys, now, a.Fresh),
	}
	return out
}

// months counts the stable releases of the live repositories per month, the last Months months ending with this one, oldest first.
func months(live []row, now time.Time) []oapi.InsightMonth {
	first := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -(Months - 1), 0)
	out := make([]oapi.InsightMonth, Months)
	for i := range out {
		out[i].Month = first.AddDate(0, i, 0).Format("2006-01")
	}
	for _, r := range live {
		for _, t := range r.Stable {
			if i := (t.Year()-first.Year())*12 + int(t.Month()-first.Month()); i >= 0 && i < Months {
				out[i].Releases++
			}
		}
	}
	return out
}

// products is each group's stable releases over its live members in scope, and the median days between its newest Releases.
func products(s *snapshot.Snapshot, scopeKeys []snapshot.RepoKey, live []row) []oapi.InsightProduct {
	byKey := map[snapshot.RepoKey]*row{}
	for i := range live {
		byKey[live[i].key] = &live[i]
	}
	inScope := map[snapshot.RepoKey]bool{}
	for _, k := range scopeKeys {
		inScope[k] = true
	}
	out := []oapi.InsightProduct{}
	for _, g := range s.Groups {
		var times []time.Time
		for _, m := range g.Members {
			if r := byKey[m]; r != nil && inScope[m] {
				times = append(times, r.Stable...)
			}
		}
		if len(times) == 0 && !slices.ContainsFunc(g.Members, func(m snapshot.RepoKey) bool { return byKey[m] != nil }) {
			continue
		}
		slices.SortFunc(times, func(x, y time.Time) int { return y.Compare(x) })
		p := oapi.InsightProduct{Group: g.ID, Name: g.Name, Releases: len(times)}
		if len(times) > 0 {
			p.Last = &times[0]
		}
		if n := min(len(times), Releases); n >= 2 {
			gaps := make([]int, 0, n-1)
			for i := 0; i+1 < n; i++ {
				gaps = append(gaps, int(times[i].Sub(times[i+1])/day))
			}
			slices.Sort(gaps)
			m := (gaps[(len(gaps)-1)/2] + gaps[len(gaps)/2]) / 2
			p.MedianDays = &m
		}
		out = append(out, p)
	}
	return out
}

func counted(names map[string]int, total int) []oapi.InsightCount {
	out := make([]oapi.InsightCount, 0, len(names))
	for n, c := range names {
		out = append(out, oapi.InsightCount{Name: n, Repos: c, Share: float32(c) / float32(max(total, 1))})
	}
	slices.SortFunc(out, func(x, y oapi.InsightCount) int {
		return cmp.Or(cmp.Compare(y.Repos, x.Repos), cmp.Compare(x.Name, y.Name))
	})
	return out
}

var number = regexp.MustCompile(`\d+(\.\d+)*`)

// version is the first dotted number in a stated toolchain version ("go 1.21", "node >=18", "17"); false when there is none.
func version(v string) ([]int, bool) {
	m := number.FindString(v)
	if m == "" {
		return nil, false
	}
	var out []int
	for _, p := range strings.Split(m, ".") {
		n, _ := strconv.Atoi(p)
		out = append(out, n)
	}
	return out, true
}

func technology(live []row) oapi.InsightTechnology {
	langs, tools := map[string]int{}, map[string]int{}
	type span struct {
		repos          int
		oldest, newest string
		lo, hi         []int
	}
	runtimes := map[string]*span{}
	t := oapi.InsightTechnology{Languages: []oapi.InsightCount{}, BuildTools: []oapi.InsightCount{}, Runtimes: []oapi.InsightRuntime{}}
	for _, r := range live {
		if r.Language == "" {
			t.Unknown++
		} else {
			langs[r.Language]++
		}
		if r.BuildTool != "" {
			tools[r.BuildTool]++
		}
		seen := map[string]bool{}
		for _, rt := range r.Runtimes {
			v, ok := version(rt[1])
			if !ok {
				continue
			}
			sp := runtimes[rt[0]]
			if sp == nil {
				sp = &span{}
				runtimes[rt[0]] = sp
			}
			if !seen[rt[0]] {
				sp.repos++
				seen[rt[0]] = true
			}
			if sp.lo == nil || slices.Compare(v, sp.lo) < 0 {
				sp.lo, sp.oldest = v, rt[1]
			}
			if sp.hi == nil || slices.Compare(v, sp.hi) > 0 {
				sp.hi, sp.newest = v, rt[1]
			}
		}
	}
	t.Languages, t.BuildTools = counted(langs, len(live)), counted(tools, len(live))
	for n, sp := range runtimes {
		t.Runtimes = append(t.Runtimes, oapi.InsightRuntime{Name: n, Repos: sp.repos, Oldest: sp.oldest, Newest: sp.newest})
	}
	slices.SortFunc(t.Runtimes, func(x, y oapi.InsightRuntime) int { return cmp.Compare(x.Name, y.Name) })
	return t
}

// libraries are the repositories of the scope that the most live repositories depend on, a retired one still used marked.
func libraries(s *snapshot.Snapshot, scopeKeys []snapshot.RepoKey, a API) []oapi.InsightLibrary {
	out := []oapi.InsightLibrary{}
	for _, k := range scopeKeys {
		e := s.Repos[k]
		if e == nil {
			continue
		}
		f := a.Facts(s, e)
		var users []string
		for _, u := range f.UsedBy {
			if ue := s.Repos[u]; ue != nil && !ue.Repo.Archived && a.Retired(ue) == "" {
				users = append(users, string(u))
			}
		}
		if len(users) == 0 {
			continue
		}
		slices.Sort(users)
		l := oapi.InsightLibrary{Repo: string(k), Dependants: len(users), DependantRepos: users[:min(len(users), listed)]}
		if f.LatestTag != "" {
			l.Release = &f.LatestTag
		}
		if why := a.Retired(e); why != "" {
			l.Retired = &why
		}
		out = append(out, l)
	}
	slices.SortFunc(out, func(x, y oapi.InsightLibrary) int {
		return cmp.Or(cmp.Compare(y.Dependants, x.Dependants), cmp.Compare(x.Repo, y.Repo))
	})
	return out[:min(len(out), Libraries)]
}

// singleAuthor are the live repositories where one author wrote at least SingleAuthorShare percent of the people's commits held,
// with at least SingleAuthorCommits of them. The author is never returned.
func singleAuthor(live []row) []oapi.InsightRepo {
	out := []oapi.InsightRepo{}
	for _, r := range live {
		if r.People >= SingleAuthorCommits && r.Top*100 >= r.People*SingleAuthorShare {
			out = append(out, oapi.InsightRepo{Repo: string(r.key), Commits: r.People})
		}
	}
	slices.SortFunc(out, func(x, y oapi.InsightRepo) int {
		return cmp.Or(cmp.Compare(y.Commits, x.Commits), cmp.Compare(x.Repo, y.Repo))
	})
	return out
}

func ageing(live []row, now time.Time) oapi.InsightAgeing {
	out := oapi.InsightAgeing{DormantRepos: []oapi.InsightDormant{}}
	dormant := []oapi.InsightDormant{}
	for _, r := range live {
		switch health.ActivityOf(r.LastChange, false, now) {
		case health.Active:
			out.Active++
		case health.Recent:
			out.Recent++
		case health.Quiet:
			out.Quiet++
		default:
			out.Dormant++
			d := oapi.InsightDormant{Repo: string(r.key)}
			if !r.LastChange.IsZero() {
				t := r.LastChange.UTC()
				d.LastChange = &t
			}
			dormant = append(dormant, d)
		}
	}
	slices.SortFunc(dormant, func(x, y oapi.InsightDormant) int {
		switch {
		case x.LastChange == nil && y.LastChange != nil:
			return -1
		case x.LastChange != nil && y.LastChange == nil:
			return 1
		case x.LastChange != nil:
			if c := x.LastChange.Compare(*y.LastChange); c != 0 {
				return c
			}
		}
		return cmp.Compare(x.Repo, y.Repo)
	})
	out.DormantRepos = dormant[:min(len(dormant), dormantListed)]
	return out
}
