package indexing

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/cantara/source-code-portal/internal/httpapi"
	"github.com/cantara/source-code-portal/internal/snapshot"
	"github.com/cantara/source-code-portal/internal/store"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil)) //nolint:gochecknoglobals // test logger

func TestParse(t *testing.T) {
	e, err := Parse([]byte(`{"groups":["b","a","a"],"repos":["Acme/Zed"," acme/old "]}`))
	if err != nil || strings.Join(e.Groups, ",") != "a,b" || strings.Join(e.Repos, ",") != "acme/old,acme/zed" {
		t.Fatalf("a list is sorted, lower case and without repeats: %+v, %v", e, err)
	}
	if e, err = Parse([]byte(`{}`)); err != nil || e.Groups == nil || e.Repos == nil {
		t.Errorf("an empty list has empty lists, not null: %+v, %v", e, err)
	}
	for name, body := range map[string]string{
		"an unknown field": `{"groups":[],"repos":[],"x":1}`, "a bad group id": `{"groups":["A b"]}`, "a repository without an owner": `{"repos":["zed"]}`,
		"two values": `{} {}`, "not an object": `[]`, "a large list": `{"groups":["` + strings.Repeat("a", 200) + `"]}`,
	} {
		if _, err := Parse([]byte(body)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// view is a snapshot of the repositories acme/x (in group a), acme/y (in a and b), acme/z (in b) and acme/w (in none).
func view(t *testing.T) *snapshot.Snapshot {
	t.Helper()
	p := snapshot.NewPublisher(time.Hour, time.Hour, quiet)
	t.Cleanup(p.Close)
	entry := func(id int64, name string) *snapshot.RepoEntry {
		return &snapshot.RepoEntry{Repo: snapshot.Repo{ID: snapshot.RepoID{Source: "gh", GitHubID: id}, Owner: "acme", Name: name, Visibility: "public"}}
	}
	p.Update(func(b *snapshot.Builder) {
		b.ReplaceSource("gh", []*snapshot.RepoEntry{entry(1, "x"), entry(2, "y"), entry(3, "z"), entry(4, "w")}, nil, snapshot.SourceStatus{ID: "gh", Repos: 4})
		b.SetGroups([]snapshot.Group{{ID: "a", Name: "A", Members: []snapshot.RepoKey{"acme/x", "acme/y"}},
			{ID: "b", Name: "B", Members: []snapshot.RepoKey{"acme/y", "acme/z"}}})
	})
	p.Flush()
	return p.Current()
}

func open(t *testing.T) *Set {
	t.Helper()
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "scp.db"), quiet)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return Open(t.Context(), db, quiet)
}

func save(t *testing.T, s *Set, e Excluded) {
	t.Helper()
	b, _ := json.Marshal(e)
	if _, err := s.file.Save(t.Context(), b, s.Revision()); err != nil {
		t.Fatal(err)
	}
}

// A group keeps out its page and the repositories that are in no other group; a repository is also kept out by name.
func TestWhatIsKeptOut(t *testing.T) {
	v, s := view(t), open(t)
	for _, k := range []snapshot.RepoKey{"acme/x", "acme/y", "acme/w"} {
		if s.Repo(v, k) {
			t.Errorf("%s is kept out with nothing excluded", k)
		}
	}
	save(t, s, Excluded{Groups: []string{"a"}, Repos: []string{"acme/w"}})
	for key, want := range map[snapshot.RepoKey]bool{"acme/x": true, "acme/y": false, "acme/z": false, "acme/w": true} {
		if got := s.Repo(v, key); got != want {
			t.Errorf("%s kept out = %v, want %v (a repository in another group stays; one named is out)", key, got, want)
		}
	}
	if !s.Group("a") || s.Group("b") {
		t.Error("a group is kept out by its id only")
	}
	for path, want := range map[string]bool{
		"/repos/acme/x": true, "/repos/ACME/X/docs/guide": true, "/repos/acme/x/readme.md": true, "/repos/acme/w/history": true,
		"/repos/acme/y": false, "/groups/a": true, "/groups/a/": true, "/groups/b": false, "/commits": false, "/api/v1/repos/acme/x": false, "/repos/acme": false,
	} {
		if got := s.Path(v, path); got != want {
			t.Errorf("%s kept out = %v, want %v", path, got, want)
		}
	}
	save(t, s, Excluded{Groups: []string{"a", "b"}})
	if !s.Repo(v, "acme/y") || s.Repo(v, "acme/w") {
		t.Error("a repository whose every group is kept out is out; one in no group is not touched")
	}
}

func get(h http.Handler, method, body string, admin bool, hdr ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, AdminPath, strings.NewReader(body))
	if admin {
		r = r.WithContext(httpapi.WithViewer(r.Context(), httpapi.Viewer{Identity: httpapi.Identity{User: "ann", Via: "oidc"}, Private: true, Admin: true}))
	}
	r.Header.Set("Content-Type", "application/json")
	for i := 0; i+1 < len(hdr); i += 2 {
		r.Header.Set(hdr[i], hdr[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func TestAdminReadsAndSaves(t *testing.T) {
	v, s := view(t), open(t)
	mux := http.NewServeMux()
	for _, r := range s.Routes("https://scp.example", func(context.Context) *snapshot.Snapshot { return v }, quiet) {
		mux.Handle(r.Pattern, r.Handler)
	}
	if rec := get(mux, http.MethodGet, "", false); rec.Code == http.StatusOK {
		t.Fatalf("an anonymous read: %d", rec.Code)
	}
	rec := get(mux, http.MethodGet, "", true)
	var body struct {
		Excluded
		GroupRows []GroupRow `json:"groupRows"`
		RepoRows  []RepoRow  `json:"repoRows"`
		Revision  string     `json:"revision"`
		Writable  bool       `json:"writable"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || rec.Code != http.StatusOK || !body.Writable || len(body.GroupRows) != 2 || len(body.RepoRows) != 4 {
		t.Fatalf("read: %d %s", rec.Code, rec.Body)
	}
	save := func(excluded, revision string, hdr ...string) *httptest.ResponseRecorder {
		return get(mux, http.MethodPut, `{"groups":`+excluded+`,"repos":["acme/w"],"revision":"`+revision+`"}`, true, hdr...)
	}
	if rec := save(`["a"]`, body.Revision, "Cookie", "s=1", "Origin", "https://evil.example"); rec.Code != http.StatusForbidden {
		t.Errorf("a save from another origin: %d", rec.Code)
	}
	origin := []string{"Cookie", "s=1", "Origin", "https://scp.example"}
	if rec := save(`["A b"]`, body.Revision, origin...); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("a bad group id: %d", rec.Code)
	}
	rec = save(`["a"]`, body.Revision, origin...)
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || rec.Code != http.StatusOK || len(body.Groups) != 1 {
		t.Fatalf("save: %d %s", rec.Code, rec.Body)
	}
	via := map[string]RepoRow{}
	for _, r := range body.RepoRows {
		via[r.Address] = r
	}
	if !via["acme/x"].Via || via["acme/x"].Excluded || !via["acme/w"].Excluded || via["acme/y"].Via || via["acme/z"].Excluded {
		t.Errorf("rows after the save: %+v", via)
	}
	if rec := save(`[]`, "stale", origin...); rec.Code != http.StatusPreconditionFailed {
		t.Errorf("a save from an old revision: %d", rec.Code)
	}
	if !s.Group("a") || !s.Path(v, "/repos/acme/w") {
		t.Error("the save is in force")
	}
}
