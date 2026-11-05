package search

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/cantara/source-code-portal/internal/snapshot"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

func terms(text string) []string {
	var out []string
	tokens(text, func(t token) bool { out = append(out, t.term); return true })
	return out
}

func TestTokens(t *testing.T) {
	for _, tc := range []struct {
		text string
		want []string
	}{
		{"Whydah-TypeLib", []string{"whydah", "typelib", "type", "lib", "whydahtypelib"}},
		{"net.whydah.sso:Whydah-TypeLib", []string{"net", "whydah", "sso", "whydah", "typelib", "type", "lib", "netwhydahssowhydahtypelib"}},
		{"slf4j-api 2.0.17", []string{"slf4j", "api", "slf4japi", "2", "0", "17", "2017"}},
		{"XMLParser session_timeout.", []string{"xmlparser", "xml", "parser", "session", "timeout", "sessiontimeout"}},
		{"Ærlig ÅPEN ﬁle", []string{"ærlig", "åpen", "file"}},
		{"-- a --", []string{"a"}},
	} {
		if got := terms(tc.text); !slices.Equal(got, tc.want) {
			t.Errorf("tokens(%q) = %q, want %q", tc.text, got, tc.want)
		}
	}
	if got := terms(strings.Repeat("x", maxTerm+1) + " ok"); !slices.Equal(got, []string{"ok"}) {
		t.Errorf("a long token is kept: %q", got)
	}
	if got := words("Whydah-TypeLib"); !slices.Equal(got, []string{"whydah", "typelib"}) {
		t.Errorf("words = %q", got)
	}
}

func TestParse(t *testing.T) {
	q, err := Parse(`session "time out" -renovate oauth OR saml in:doc,commits group:IAM kind:ai author:"Ann Example" after:2026-10-01 sess`)
	if err != nil {
		t.Fatal(err)
	}
	if len(q.clauses) != 7 || q.clauses[0].alts[0] != "session" || !slices.Equal(q.clauses[3].alts, []string{"oauth", "saml"}) ||
		!q.clauses[4].prefix || q.clauses[4].alts[0] != "sess" || q.clauses[5].field != int(FieldAuthor) {
		t.Errorf("clauses: %+v", q.clauses)
	}
	if len(q.phrases) != 1 || !slices.Equal(q.phrases[0], []string{"time", "out"}) || len(q.not) != 1 || q.not[0][0] != "renovate" {
		t.Errorf("phrases %q, not %q", q.phrases, q.not)
	}
	if !slices.Equal(q.in, []Kind{KindDoc, KindCommit}) || q.group != "iam" || !slices.Equal(q.work, []string{"ai"}) ||
		!slices.Equal(q.author, []string{"ann", "example"}) || !q.after.Equal(time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("filters: %+v", q)
	}
	if q, _ := Parse("session "); q.clauses[0].prefix {
		t.Error("a word followed by a space is a prefix")
	}
	if q, _ := Parse("net.whydah.sso:Whydah-TypeLib"); len(q.clauses) != 5 {
		t.Errorf("an unknown key is words: %+v", q.clauses)
	}
	for _, bad := range []string{"", "  ", "in:nothing x", "kind:robot", "before:yesterday", strings.Repeat("a ", 13), strings.Repeat("é", MaxQuery+1),
		"group:a group:b group:c group:d group:e group:f group:g group:h group:i", "-only"} {
		if _, err := Parse(bad); !errors.Is(err, ErrQuery) {
			t.Errorf("Parse(%q): %v, want ErrQuery", bad, err)
		}
	}
	if _, err := Parse("in:commit"); err != nil {
		t.Errorf("a filter alone: %v", err)
	}
}

func TestSections(t *testing.T) {
	md := "---\ntitle: X\n---\n# Whydah Core\n\nIdentity [server](https://x) ![logo](l.png).\n\n## Configure\n\nSet `session.timeout` to <b>30</b>.\n\n" +
		"```yaml\n## not a heading\nsession: 1\n```\n\n### Run it ###\n\nGo.\n"
	got := Sections(md, []Anchor{{Text: "Configure", ID: "configure"}, {Text: "Run it", ID: "run-it"}})
	want := []Section{{Heading: "Whydah Core", Text: "Identity server logo."}, {Heading: "Configure", Anchor: "configure", Text: "Set session.timeout to  30 .\n\n## not a heading\nsession: 1"},
		{Heading: "Run it", Anchor: "run-it", Text: "Go."}}
	if !slices.Equal(got, want) {
		t.Errorf("Sections:\n got %q\nwant %q", got, want)
	}
}

// fixture is a repository's searchable data in the test's facet.
type fixture struct {
	repo Repository
	work []string // per commit item
}

const data snapshot.Facet[fixture] = "fixture"

func testOptions() Options {
	return Options{
		Read: func(e *snapshot.RepoEntry) Repository { f, _ := data.Get(e); return f.repo },
		Work: func(_ *snapshot.Snapshot, e *snapshot.RepoEntry, item int) string {
			f, _ := data.Get(e)
			if item < len(f.work) {
				return f.work[item]
			}
			return ""
		},
	}
}

func doc(kind Kind, title, url string, fs map[Field]string) Document {
	d := Document{Kind: kind, Title: title, URL: url}
	for f, v := range fs {
		d.Fields[f] = v
	}
	return d
}

func day() time.Time { return time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC) }

func core() fixture {
	commit := func(i int, subject, author string) Document {
		d := doc(KindCommit, subject, "/repos/acme/whydah-core/commits?at="+subject, map[Field]string{FieldTitle: subject, FieldAuthor: author})
		d.At, d.Item = day().Add(time.Duration(i)*time.Hour), i
		return d
	}
	return fixture{repo: Repository{Languages: []string{"Java"}, Topics: []string{"iam"}, Lifecycle: "production",
		Dependencies: []string{"org.slf4j:slf4j-api"}, Documents: []Document{
			doc(KindRepo, "acme/Whydah-Core", "/repos/acme/whydah-core", map[Field]string{FieldName: "acme/Whydah-Core",
				FieldDescription: "Identity and access server with OAuth", FieldMeta: "Java Apache-2.0 production"}),
			doc(KindDoc, "README", "/repos/acme/whydah-core#configure", map[Field]string{FieldTitle: "README", FieldHeading: "Configure",
				FieldText: "Set the session timeout in seconds. The session store keeps one session per user."}),
			doc(KindDep, "no.acme:whydah-core", "/repos/acme/whydah-core#manifests", map[Field]string{FieldName: "no.acme:whydah-core",
				FieldText: "org.slf4j:slf4j-api\njunit:junit"}),
			doc(KindPackage, "no.acme:whydah-core", "/repos/acme/whydah-core#packages", map[Field]string{FieldName: "no.acme:whydah-core",
				FieldMeta: "1.3.0 mvnrepo"}),
			commit(0, "Rotate the signing keys", "Ann Example ann"),
			commit(1, "chore(deps): update slf4j", "renovate[bot]"),
		}}, work: []string{"hand", "automation"}}
}

func secret() fixture {
	return fixture{repo: Repository{Languages: []string{"Go"}, Documents: []Document{
		doc(KindRepo, "acme/vault", "/repos/acme/vault", map[Field]string{FieldName: "acme/vault", FieldDescription: "Secrets of the staff, OAuth keys"}),
		doc(KindDoc, "README", "/repos/acme/vault", map[Field]string{FieldTitle: "README", FieldText: "The vault rotates keys nightly."}),
	}}}
}

func entry(id int64, name, visibility string) *snapshot.RepoEntry {
	return &snapshot.RepoEntry{Repo: snapshot.Repo{ID: snapshot.RepoID{Source: "gh", GitHubID: id}, Owner: "acme", Name: name, Visibility: visibility}}
}

// catalogue publishes Whydah-Core (public) and vault (private) in the groups iam and all.
func catalogue(t *testing.T) *snapshot.Publisher {
	t.Helper()
	p := snapshot.NewPublisher(time.Microsecond, time.Microsecond, slog.New(slog.DiscardHandler), Indexer(testOptions()))
	t.Cleanup(p.Close)
	p.Update(func(b *snapshot.Builder) {
		b.ReplaceSource("gh", []*snapshot.RepoEntry{entry(1, "Whydah-Core", "public"), entry(2, "vault", "private")}, nil,
			snapshot.SourceStatus{ID: "gh", Repos: 2})
		b.SetGroups([]snapshot.Group{{ID: "iam", Name: "Identity", Description: "Identity and access management", Members: []snapshot.RepoKey{"acme/whydah-core"}},
			{ID: "all", Name: "Everything", Members: []snapshot.RepoKey{"acme/vault", "acme/whydah-core"}}})
		b.UpdateRepo("acme/whydah-core", func(e *snapshot.RepoEntry) { data.Set(e, core(), snapshot.Freshness{}) })
		b.UpdateRepo("acme/vault", func(e *snapshot.RepoEntry) { data.Set(e, secret(), snapshot.Freshness{}) })
		b.Mark("fixture/acme/whydah-core", "acme/whydah-core", 1, 1)
		b.Mark("fixture/acme/vault", "acme/vault", 2, 2)
	})
	p.Flush()
	return p
}

func find(t *testing.T, s *snapshot.Snapshot, text string) Result {
	t.Helper()
	q, err := Parse(text)
	if err != nil {
		t.Fatalf("Parse(%q): %v", text, err)
	}
	return Of(s).Search(t.Context(), q, nil, 0, 50)
}

func urls(r Result) []string {
	out := make([]string, 0, len(r.Hits))
	for _, h := range r.Hits {
		out = append(out, h.URL)
	}
	return out
}

func text(ps []Part) string {
	var sb strings.Builder
	for _, p := range ps {
		if p.Mark {
			sb.WriteString("[" + p.Text + "]")
		} else {
			sb.WriteString(p.Text)
		}
	}
	return sb.String()
}

func TestSearchFindsEveryKind(t *testing.T) {
	full := catalogue(t).Current()
	for _, tc := range []struct {
		query string
		first string
	}{
		{"whydah", "/repos/acme/whydah-core"},
		{"Whydah-Core", "/repos/acme/whydah-core"},
		{"session timeout", "/repos/acme/whydah-core#configure"},
		{`"session timeout"`, "/repos/acme/whydah-core#configure"},
		{"slf4j-api", "/repos/acme/whydah-core#manifests"},
		{"1.3.0 mvnrepo", "/repos/acme/whydah-core#packages"},
		{"rotate signing", "/repos/acme/whydah-core/commits?at=Rotate the signing keys"},
		{"identity management", "/groups/iam"},
		{"sess", "/repos/acme/whydah-core#configure"},   // a prefix
		{"sesion", "/repos/acme/whydah-core#configure"}, // one edit
		{"vault", "/repos/acme/vault"},                  // private, in the full view
		{"author:ann", "/repos/acme/whydah-core/commits?at=Rotate the signing keys"},
	} {
		if got := urls(find(t, full, tc.query)); len(got) == 0 || got[0] != tc.first {
			t.Errorf("%q: %q, want %s first", tc.query, got, tc.first)
		}
	}
	r := find(t, full, "sesion")
	if len(r.Corrections) != 1 || r.Corrections[0] != (Correction{From: "sesion", To: "session"}) {
		t.Errorf("corrections: %+v", r.Corrections)
	}
	if got := text(r.Hits[0].Snippet); got != "Set the [session] timeout in seconds. The [session] store keeps one [session] per user." {
		t.Errorf("snippet: %s", got)
	}
	if r := find(t, full, `"timeout session"`); len(r.Hits) != 0 {
		t.Errorf("a phrase in another order: %q", urls(r))
	}
	if r := find(t, full, "oauth"); r.Counts[KindRepo] != 2 || r.Total != 2 {
		t.Errorf("oauth counts: %v total %d", r.Counts, r.Total)
	}
}

func TestSearchFilters(t *testing.T) {
	full := catalogue(t).Current()
	for _, tc := range []struct {
		query string
		want  []string
	}{
		{"oauth lang:go", []string{"/repos/acme/vault"}},
		{"oauth lang:java", []string{"/repos/acme/whydah-core"}},
		{"oauth group:iam", []string{"/repos/acme/whydah-core"}},
		{"oauth repo:vault", []string{"/repos/acme/vault"}},
		{"oauth topic:iam lifecycle:production dep:slf4j", []string{"/repos/acme/whydah-core"}},
		{"oauth -secrets", []string{"/repos/acme/whydah-core"}},
		{"in:commit kind:automation", []string{"/repos/acme/whydah-core/commits?at=chore(deps): update slf4j"}},
		{"in:commit after:2026-10-01", nil},
		{"in:commit before:2026-10-02 -slf4j", []string{"/repos/acme/whydah-core/commits?at=Rotate the signing keys"}},
		{"keys OR slf4j in:commit", []string{"/repos/acme/whydah-core/commits?at=chore(deps): update slf4j", "/repos/acme/whydah-core/commits?at=Rotate the signing keys"}},
		{"group:all in:group", []string{"/groups/all"}},
	} {
		if got := urls(find(t, full, tc.query)); !slices.Equal(got, tc.want) {
			t.Errorf("%q: %q, want %q", tc.query, got, tc.want)
		}
	}
	q, _ := Parse("oauth OR slf4j")
	only := KindDep
	if r := Of(full).Search(t.Context(), q, &only, 0, 10); r.Total != 1 || r.Counts[KindRepo] != 2 || r.Hits[0].Kind != KindDep {
		t.Errorf("a page of one kind: total %d, counts %v, hits %+v", r.Total, r.Counts, r.Hits)
	}
}

func TestPublicViewHoldsNoPrivateSegment(t *testing.T) {
	full := catalogue(t).Current()
	pub := full.Public()
	for _, sg := range Of(pub).segs {
		if sg.repo == "acme/vault" {
			t.Fatal("the public view's index holds the private repository")
		}
	}
	for _, query := range []string{"vault", "secrets", "nightly", "repo:vault", "lang:go", "group:all"} {
		r := find(t, pub, query)
		for _, h := range r.Hits {
			if h.Repo == "acme/vault" || strings.Contains(h.URL, "vault") {
				t.Errorf("public %q: %+v", query, h)
			}
		}
		if query != "group:all" && (r.Counts != [kinds]int{} || len(r.Corrections) > 0) {
			t.Errorf("public %q counts or corrects from the private repository: %v %v", query, r.Counts, r.Corrections)
		}
	}
	if got := Of(pub).Suggest(t.Context(), "va", 8); len(got) != 0 {
		t.Errorf("public suggestions: %+v", got)
	}
	if got := Of(full).Suggest(t.Context(), "va", 8); len(got) != 1 || got[0].URL != "/repos/acme/vault" {
		t.Errorf("staff suggestions: %+v", got)
	}
}

func TestSegmentsAreReused(t *testing.T) {
	p := catalogue(t)
	before := Of(p.Current())
	p.Update(func(b *snapshot.Builder) {
		b.UpdateRepo("acme/vault", func(e *snapshot.RepoEntry) {
			f := secret()
			f.repo.Documents[0].Fields[FieldDescription] = "Changed"
			data.Set(e, f, snapshot.Freshness{})
		})
		b.Mark("fixture/acme/vault", "acme/vault", 3, 3)
		b.Mark("fixture/acme/whydah-core", "acme/whydah-core", 4, 1) // checked again, nothing new
	})
	p.Flush()
	after := Of(p.Current())
	// The segments by key: acme/vault, acme/whydah-core, then the groups.
	if before.segs[1] != after.segs[1] || before.segs[1].repo != "acme/whydah-core" {
		t.Error("an unchanged repository's segment was rebuilt")
	}
	if before.segs[0] == after.segs[0] {
		t.Error("a changed repository's segment was kept")
	}
	if Of(p.Current().Public()).segs[0] != after.segs[1] {
		t.Error("the public view built its own segment of a public repository")
	}
	if r := find(t, p.Current(), "changed"); len(r.Hits) != 1 {
		t.Errorf("the changed text is not found: %q", urls(r))
	}
	if st := after.Stats(); st.Segments != 3 || st.Terms == 0 || st.Bytes == 0 {
		t.Errorf("stats: %+v", st)
	}
}

func TestSearchStopsWithItsContext(t *testing.T) {
	full := catalogue(t).Current()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	q, _ := Parse("oauth")
	if r := Of(full).Search(ctx, q, nil, 0, 10); len(r.Hits) != 0 {
		t.Errorf("a cancelled search found %d", len(r.Hits))
	}
}
