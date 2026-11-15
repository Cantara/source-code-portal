package visibility

import (
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

	"github.com/cantara/source-code-portal/internal/docstore"
	"github.com/cantara/source-code-portal/internal/httpapi"
	"github.com/cantara/source-code-portal/internal/snapshot"
	"github.com/cantara/source-code-portal/internal/store"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil)) //nolint:gochecknoglobals // test logger

func TestParse(t *testing.T) {
	d, err := Parse([]byte(`{"groups":{"Web":{"audience":"signed-in","search":false}},"repos":{" Acme/X ":{"audience":"everyone","search":false}},` +
		`"rules":[{"pattern":" ^acme/mcp- ","audience":"no-one","search":false},{"pattern":"","audience":"everyone","search":true}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if g := d.Groups["web"]; g.Audience != SignedIn || !g.Search {
		t.Errorf("a product id is lower case and an item not everyone sees has search on: %+v", d.Groups)
	}
	if r := d.Repos["acme/x"]; r.Audience != Everyone || r.Search {
		t.Errorf("an address is trimmed and lower case: %+v", d.Repos)
	}
	if len(d.Rules) != 1 || d.Rules[0].Pattern != "^acme/mcp-" || d.Rules[0].re == nil {
		t.Errorf("a rule is trimmed and compiled, an empty one dropped: %+v", d.Rules)
	}
	if d, err = Parse([]byte(`{}`)); err != nil || d.Groups == nil || d.Repos == nil || d.Rules == nil {
		t.Errorf("an empty document has empty collections: %+v, %v", d, err)
	}
	for name, body := range map[string]string{
		"an unknown field": `{"x":1}`, "an unknown audience": `{"repos":{"acme/x":{"audience":"public"}}}`,
		"no audience": `{"groups":{"a":{"search":true}}}`, "a bad product id": `{"groups":{"A b":{"audience":"everyone"}}}`,
		"a repository without an owner": `{"repos":{"x":{"audience":"everyone"}}}`, "a bad pattern": `{"rules":[{"pattern":"a(b","audience":"everyone"}]}`,
		"a long pattern": `{"rules":[{"pattern":"` + strings.Repeat("a", 300) + `","audience":"everyone"}]}`,
		"two values":     `{} {}`, "not an object": `[]`, "too many rules": `{"rules":[` + strings.Repeat(`{"pattern":"a","audience":"everyone"},`, 50) + `{"pattern":"b","audience":"everyone"}]}`,
	} {
		if _, err := Parse([]byte(body)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func doc(t *testing.T, s string) Doc {
	t.Helper()
	d, err := Parse([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// The repository's own choice wins, then the first rule that matches, then its products (the widest audience; a product without a
// choice counts only when none chose), then the default; GitHub's private flag caps everyone at signed-in.
func TestResolve(t *testing.T) {
	d := doc(t, `{"groups":{"a":{"audience":"signed-in"},"b":{"audience":"no-one"},"c":{"audience":"everyone","search":false},"e":{"audience":"everyone","search":true}},
		"repos":{"acme/own":{"audience":"no-one"}},
		"rules":[{"pattern":"^acme/mcp-","audience":"signed-in"},{"pattern":"^acme/m","audience":"everyone","search":false}]}`)
	for _, tc := range []struct {
		key     snapshot.RepoKey
		groups  []string
		public  bool
		want    string
		search  bool
		why     string
		capped  bool
		product string
		rule    int
	}{
		{"acme/own", []string{"e"}, true, NoOne, true, FromRepo, false, "", -1},
		{"acme/mcp-x", []string{"e"}, true, SignedIn, true, FromRule, false, "", 0},
		{"acme/more", nil, true, Everyone, false, FromRule, false, "", 1},
		{"acme/x", []string{"a", "b"}, true, SignedIn, true, FromProduct, false, "a", -1},
		{"acme/x", []string{"c", "e"}, true, Everyone, true, FromProduct, false, "e", -1},
		{"acme/x", []string{"c"}, true, Everyone, false, FromProduct, false, "c", -1},
		{"acme/x", []string{"a", "z"}, true, Everyone, true, FromDefault, false, "", -1},
		{"acme/x", []string{"z"}, true, Everyone, true, FromDefault, false, "", -1},
		{"acme/x", nil, false, SignedIn, true, FromDefault, true, "", -1},
		{"acme/x", []string{"c"}, false, SignedIn, true, FromProduct, true, "c", -1},
		{"acme/x", []string{"b"}, false, NoOne, true, FromProduct, false, "b", -1},
	} {
		e := d.Repo(tc.key, tc.groups, tc.public)
		if e.Audience != tc.want || e.Search != tc.search || e.Why != tc.why || e.Capped != tc.capped || e.Product != tc.product || e.Rule != tc.rule {
			t.Errorf("%s in %v (public %v): %+v", tc.key, tc.groups, tc.public, e)
		}
	}
	if g := d.Group("c"); g.Audience != Everyone || g.Search || g.Why != FromProduct {
		t.Errorf("a product's own choice: %+v", g)
	}
	if g := d.Group("zzz"); g.Audience != Everyone || !g.Search || g.Why != FromDefault {
		t.Errorf("a product without a choice: %+v", g)
	}
}

// view is a base snapshot of acme/x (in group a), acme/y (in a and b), acme/z (in b), acme/w (in none) and acme/secret (private, in a),
// published with the audiences s gives.
func view(t *testing.T, s *Set) *snapshot.Snapshot {
	t.Helper()
	p := snapshot.NewPublisher(time.Hour, time.Hour, quiet)
	t.Cleanup(p.Close)
	p.Audience(s.Audience)
	entry := func(id int64, name, visibility string) *snapshot.RepoEntry {
		return &snapshot.RepoEntry{Repo: snapshot.Repo{ID: snapshot.RepoID{Source: "gh", GitHubID: id}, Owner: "acme", Name: name, Visibility: visibility}}
	}
	p.Update(func(b *snapshot.Builder) {
		b.ReplaceSource("gh", []*snapshot.RepoEntry{entry(1, "x", "public"), entry(2, "y", "public"), entry(3, "z", "public"), entry(4, "w", "public"),
			entry(5, "secret", "private")}, nil, snapshot.SourceStatus{ID: "gh", Repos: 5})
		b.SetGroups([]snapshot.Group{{ID: "a", Name: "A", Members: []snapshot.RepoKey{"acme/secret", "acme/x", "acme/y"}},
			{ID: "b", Name: "B", Members: []snapshot.RepoKey{"acme/y", "acme/z"}}})
	})
	p.Flush()
	return p.Current()
}

func openStore(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "scp.db"), quiet)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func save(t *testing.T, s *Set, body string) {
	t.Helper()
	d := doc(t, body)
	b, _ := encode(d)
	if _, err := s.file.Save(t.Context(), b, s.Revision()); err != nil {
		t.Fatal(err)
	}
}

// The snapshot's views follow the audiences; search engines are offered only what everyone sees with its switch on, and a product's
// page follows the product's own choice.
func TestAudiencesAndSearchEngines(t *testing.T) {
	s := Open(t.Context(), openStore(t), nil, quiet)
	save(t, s, `{"groups":{"b":{"audience":"everyone","search":false}},"repos":{"acme/x":{"audience":"signed-in"},"acme/w":{"audience":"no-one"}}}`)
	v := view(t, s)
	pub := v.Public()
	if _, ok := pub.Repos["acme/x"]; ok || !v.Restricted("acme/x") || !v.Parked("acme/w") || pub.Repos["acme/y"] == nil {
		t.Errorf("public view %v", pub.Repos)
	}
	for key, want := range map[snapshot.RepoKey]bool{"acme/y": true, "acme/z": false, "acme/x": false, "acme/w": false, "acme/secret": false} {
		if got := s.Indexed(pub, key); got != want {
			t.Errorf("%s offered to search engines = %v, want %v", key, got, want)
		}
	}
	for path, want := range map[string]bool{
		"/repos/acme/z": true, "/repos/ACME/Z/docs/guide": true, "/repos/acme/y": false, "/repos/acme/x": false, "/groups/b": true,
		"/groups/a": false, "/commits": false, "/repos/acme": false,
	} {
		if got := s.NoIndex(pub, path); got != want {
			t.Errorf("%s noindex = %v, want %v", path, got, want)
		}
	}
	if !s.IndexedGroup("a") || s.IndexedGroup("b") {
		t.Error("a product's page follows its own choice")
	}
}

// A store without the document takes over the parked and search-engine lists of 2.2.19 once and leaves them as they are; one that has
// it, or has nothing to take over, is not written.
func TestImport(t *testing.T) {
	db := openStore(t)
	put := func(name, body string) {
		_, was, _, _ := db.Document(t.Context(), name)
		if err := db.PutDocument(t.Context(), name, []byte(body), docstore.Digest([]byte(body)), was); err != nil {
			t.Fatal(err)
		}
	}
	if err := Import(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	if _, _, ok, _ := db.Document(t.Context(), Name); ok {
		t.Fatal("nothing to take over wrote a document")
	}
	parked := `{"repos":["acme/old","acme/w"]}`
	indexing := `{"groups":["b"],"repos":["acme/w","acme/x"],"exclude":["^acme/tmp-"],"include":["^acme/tmp-keep$"]}`
	put(oldParked, parked)
	put(oldIndexing, indexing)
	s := Open(t.Context(), db, nil, quiet)
	d := s.Doc()
	if d.Repos["acme/old"].Audience != NoOne || d.Repos["acme/w"].Audience != NoOne || d.Repos["acme/x"] != (Setting{Audience: Everyone}) ||
		d.Groups["b"] != (Setting{Audience: Everyone}) || len(d.Rules) != 2 || d.Rules[0].Pattern != "^acme/tmp-keep$" || !d.Rules[0].Search ||
		d.Rules[1].Search {
		t.Errorf("taken over: %+v", d)
	}
	for name, want := range map[string]string{oldParked: parked, oldIndexing: indexing} {
		if b, _, _, _ := db.Document(t.Context(), name); string(b) != want {
			t.Errorf("%s changed: %s", name, b)
		}
	}
	// Taken over once: a later parked list does not come back over the saved choices.
	save(t, s, `{}`)
	put(oldParked, `{"repos":["acme/new"]}`)
	if d := Open(t.Context(), db, nil, quiet).Doc(); len(d.Repos) != 0 {
		t.Errorf("taken over again: %+v", d.Repos)
	}
}

func call(h http.Handler, method, path, body string, admin bool, hdr ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
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

func TestAdminReadsPreviewsAndSaves(t *testing.T) {
	changed := 0
	s := Open(t.Context(), openStore(t), func() { changed++ }, quiet)
	v := view(t, s)
	mux := http.NewServeMux()
	for _, r := range s.Routes("https://scp.example", func() *snapshot.Snapshot { return v }, quiet) {
		mux.Handle(r.Pattern, r.Handler)
	}
	if rec := call(mux, http.MethodGet, AdminPath, "", false); rec.Code == http.StatusOK {
		t.Fatalf("an anonymous read: %d", rec.Code)
	}
	rec := call(mux, http.MethodGet, AdminPath, "", true)
	var body adminBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || rec.Code != http.StatusOK || !body.Writable || len(body.Tree) != 2 ||
		len(body.Ungrouped) != 1 || len(body.Tree[0].Repos) != 3 || body.Counts != (Counts{Everyone: 4, SignedIn: 1, Private: 1}) {
		t.Fatalf("read: %d %s", rec.Code, rec.Body)
	}
	for _, l := range []string{`"groups":{}`, `"repos":{}`, `"rules":[]`, `"ruleMatches":[]`, `"missing":[]`, `"setting":null`} {
		if !strings.Contains(rec.Body.String(), l) {
			t.Errorf("nothing saved yet: no %s in %s", l, rec.Body)
		}
	}
	draft := `{"groups":{"b":{"audience":"everyone","search":false}},"repos":{"acme/w":{"audience":"no-one"},"acme/gone":{"audience":"signed-in"}},` +
		`"rules":[{"pattern":"^acme/[xy]$","audience":"signed-in"}],"revision":"%s"}`
	origin := []string{"Cookie", "s=1", "Origin", "https://scp.example"}
	if rec := call(mux, http.MethodPost, PreviewPath, strings.Replace(draft, "%s", body.Revision, 1), true, "Cookie", "s=1", "Origin", "https://evil.example"); rec.Code != http.StatusForbidden {
		t.Errorf("a preview from another origin: %d", rec.Code)
	}
	if rec := call(mux, http.MethodPut, AdminPath, `{"repos":{"x":{"audience":"everyone"}}}`, true, origin...); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("a bad address: %d", rec.Code)
	}
	// A preview answers as if the draft were saved and saves nothing.
	rec = call(mux, http.MethodPost, PreviewPath, strings.Replace(draft, "%s", body.Revision, 1), true, origin...)
	var preview adminBody
	if err := json.Unmarshal(rec.Body.Bytes(), &preview); err != nil || rec.Code != http.StatusOK ||
		preview.Counts != (Counts{Everyone: 1, SignedIn: 3, NoOne: 1, SearchOff: 1, Private: 1}) || len(preview.RuleMatches) != 1 ||
		preview.RuleMatches[0] != 2 || len(preview.Missing) != 1 || preview.Missing[0] != "acme/gone" || s.Revision() != "" || changed != 0 {
		t.Fatalf("a preview: %d %s", rec.Code, rec.Body)
	}
	rec = call(mux, http.MethodPut, AdminPath, strings.Replace(draft, "%s", body.Revision, 1), true, origin...)
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || rec.Code != http.StatusOK || body.Revision == "" || changed != 1 ||
		body.Counts != preview.Counts {
		t.Fatalf("save: %d %s", rec.Code, rec.Body)
	}
	why := map[string]Item{}
	for _, g := range body.Tree {
		for _, r := range g.Repos {
			why[g.ID+":"+r.Address] = r
		}
	}
	if x := why["a:acme/x"]; x.Audience != SignedIn || x.Why != FromRule || x.Setting != nil {
		t.Errorf("acme/x: %+v", x)
	}
	if z := why["b:acme/z"]; z.Audience != Everyone || z.Search || z.Why != FromProduct || z.Product != "b" {
		t.Errorf("acme/z: %+v", z)
	}
	if sec := why["a:acme/secret"]; sec.Audience != SignedIn || !sec.Capped || !sec.Private {
		t.Errorf("acme/secret: %+v", sec)
	}
	if w := body.Ungrouped[0]; w.Setting == nil || w.Setting.Audience != NoOne {
		t.Errorf("acme/w: %+v", w)
	}
	if rec := call(mux, http.MethodPut, AdminPath, `{"revision":"stale"}`, true, origin...); rec.Code != http.StatusPreconditionFailed {
		t.Errorf("a save from an old revision: %d", rec.Code)
	}
	if s.Doc().Repos["acme/w"].Audience != NoOne {
		t.Error("the save is in force")
	}
}
