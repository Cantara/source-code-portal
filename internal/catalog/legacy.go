package catalog

import (
	"context"
	"net/http"
	"slices"
	"strings"

	"github.com/cantara/source-code-portal/internal/httpapi"
	"github.com/cantara/source-code-portal/internal/snapshot"
)

// legacyBadges maps the Java portal's badge names to this portal's repository badges.
var legacyBadges = map[string]string{ //nolint:gochecknoglobals // lookup table
	"license": "license", "shields-issues": "issues", "shields-commits": "last-commit", "shields-releases": "release",
}

// Legacy answers the addresses of the Java portal that links and READMEs still hold: 308 to the page or badge that replaced
// them, 410 for what has none (Jenkins and Snyk badges, the wiki, the old webhook), 404 for a group or repository the viewer
// cannot see. Its repositories had no owner in the address: the name is looked up among all repositories.
func Legacy(current func(context.Context) *snapshot.Snapshot) []httpapi.Route {
	to := func(w http.ResponseWriter, r *http.Request, target string) {
		http.Redirect(w, r, target, http.StatusPermanentRedirect)
	}
	gone := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "This address of the earlier portal has no successor.", http.StatusGone)
	})
	home := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { to(w, r, "/") })
	return []httpapi.Route{
		{Pattern: "GET /dashboard", Handler: home},
		{Pattern: "GET /index", Handler: home},
		{Pattern: "GET /group/{id}", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			g, ok := legacyGroup(current(r.Context()), r.PathValue("id"))
			if !ok {
				http.NotFound(w, r)
				return
			}
			to(w, r, snapshot.GroupPage(g.ID))
		})},
		{Pattern: "GET /contents/{owner}/{name}/{branch}", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			to(w, r, snapshot.RepoPage(snapshot.KeyOf(r.PathValue("owner"), r.PathValue("name"))))
		})},
		{Pattern: "GET /commits/{owner}/{name}", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			to(w, r, snapshot.RepoPage(snapshot.KeyOf(r.PathValue("owner"), r.PathValue("name")))+"/commits")
		})},
		{Pattern: "GET /commits/{id}", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			s := current(r.Context())
			if g, ok := legacyGroup(s, r.PathValue("id")); ok {
				to(w, r, snapshot.GroupPage(g.ID)+"/commits")
				return
			}
			if k, ok := legacyRepo(s, r.PathValue("id")); ok {
				to(w, r, snapshot.RepoPage(k)+"/commits")
				return
			}
			http.NotFound(w, r)
		})},
		{Pattern: "GET /badge/{kind}/{name}/{branch}", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			badge, ok := legacyBadges[r.PathValue("kind")]
			if !ok {
				gone(w, r)
				return
			}
			k, ok := legacyRepo(current(r.Context()), r.PathValue("name"))
			if !ok {
				http.NotFound(w, r)
				return
			}
			owner, name, _ := strings.Cut(string(k), "/")
			to(w, r, "/badges/repos/"+snapshot.Segment(owner)+"/"+snapshot.Segment(name)+"/"+badge+".svg")
		})},
		{Pattern: "GET /wiki", Handler: gone},
		{Pattern: "GET /wiki/", Handler: gone},
		{Pattern: "POST /github/webhook", Handler: gone},
	}
}

// legacyGroup finds a group by its Java id: case and dashes do not matter (ConfigService is configservice or config-service).
func legacyGroup(s *snapshot.Snapshot, id string) (snapshot.Group, bool) {
	fold := func(v string) string { return strings.ReplaceAll(strings.ToLower(v), "-", "") }
	for _, g := range s.Groups {
		if fold(g.ID) == fold(id) {
			return g, true
		}
	}
	return snapshot.Group{}, false
}

// legacyRepo finds a repository by its name alone; with the same name under several owners, the first address in order.
func legacyRepo(s *snapshot.Snapshot, name string) (snapshot.RepoKey, bool) {
	var found []snapshot.RepoKey
	for k := range s.Repos {
		if _, n, _ := strings.Cut(string(k), "/"); n == strings.ToLower(name) {
			found = append(found, k)
		}
	}
	if len(found) == 0 {
		return "", false
	}
	return slices.Min(found), true
}
