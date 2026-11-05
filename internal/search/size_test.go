package search

import (
	"fmt"
	"log/slog"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/snapshot"
)

// corpus is a fleet the size of the load test's (1,000 repositories) with what a real one holds: a README and two documents of four
// sections, two manifests of 30 dependencies, two packages and 500 commits each, words drawn from a vocabulary of 5,000 with a
// long tail, and names, issue numbers and versions of their own.
func corpus(repos int) []fixture {
	rng := rand.New(rand.NewPCG(1, 2))
	vocab := make([]string, 5000)
	for i := range vocab {
		vocab[i] = fmt.Sprintf("w%xo%d", i*7919%65521, i%13)
	}
	word := func() string { return vocab[int(float64(len(vocab))*rng.Float64()*rng.Float64())] }
	sentence := func(n int) string {
		ws := make([]string, n)
		for i := range ws {
			ws[i] = word()
		}
		return strings.Join(ws, " ")
	}
	out := make([]fixture, repos)
	for r := range out {
		name := fmt.Sprintf("acme/service-%04d", r)
		docs := []Document{doc(KindRepo, name, "/repos/"+name, map[Field]string{FieldName: name, FieldDescription: sentence(12), FieldTags: "java iam",
			FieldMeta: "Java Apache-2.0 production"})}
		for d := range 3 {
			for s := range 4 {
				docs = append(docs, doc(KindDoc, fmt.Sprint("doc ", d), fmt.Sprintf("/repos/%s/docs/%d#s%d", name, d, s),
					map[Field]string{FieldTitle: fmt.Sprint("Guide ", d), FieldHeading: sentence(3), FieldText: sentence(300)}))
			}
		}
		var deps []string
		for m := range 2 {
			var ds []string
			for i := range 30 {
				ds = append(ds, fmt.Sprintf("org.lib%d:lib-%d", (r+i*31)%400, i))
			}
			deps = append(deps, ds...)
			docs = append(docs, doc(KindDep, name, "/repos/"+name+"#manifests", map[Field]string{FieldName: fmt.Sprintf("no.acme:service-%d-%d", r, m),
				FieldText: strings.Join(ds, "\n")}))
			docs = append(docs, doc(KindPackage, name, "/repos/"+name+"#packages", map[Field]string{FieldName: fmt.Sprintf("no.acme:service-%d-%d", r, m),
				FieldMeta: fmt.Sprintf("1.%d.%d mvnrepo", r%20, m)}))
		}
		for c := range 500 {
			d := doc(KindCommit, "commit", fmt.Sprintf("/repos/%s/commits?at=%d", name, c), map[Field]string{
				FieldTitle: fmt.Sprintf("%s (#%d)", sentence(7), r*1000+c), FieldText: sentence(rng.IntN(30)), FieldAuthor: fmt.Sprintf("Person %d p%d", c%40, c%40)})
			d.At, d.Item = day().Add(-time.Duration(c)*time.Hour), c
			docs = append(docs, d)
		}
		out[r].repo = Repository{Languages: []string{"Java"}, Topics: []string{"iam"}, Dependencies: deps, Documents: docs}
	}
	return out
}

// TestIndexSize holds the index to its budgets at 1,000 repositories (design/search.md): under 96 MB, a cold build under 3 s, a
// publish that changed one repository under 20 ms, and a query's p99 under 50 ms.
func TestIndexSize(t *testing.T) {
	if testing.Short() {
		t.Skip("the size check builds a large index")
	}
	const repos = 1000
	fs := corpus(repos)
	p := snapshot.NewPublisher(time.Hour, time.Hour, slog.New(slog.DiscardHandler), Indexer(testOptions()))
	defer p.Close()
	entries := make([]*snapshot.RepoEntry, repos)
	for i := range entries {
		entries[i] = entry(int64(i+1), fmt.Sprintf("service-%04d", i), "public")
		data.Set(entries[i], fs[i], snapshot.Freshness{})
	}
	start := time.Now()
	p.Update(func(b *snapshot.Builder) {
		b.ReplaceSource("gh", entries, nil, snapshot.SourceStatus{ID: "gh", Repos: repos})
		for i := range entries {
			k := entries[i].Repo.Key()
			b.Mark("fixture/"+string(k), k, uint64(i+1), uint64(i+1))
		}
	})
	p.Flush()
	cold := time.Since(start)
	st := Of(p.Current()).Stats()
	t.Logf("cold build %v: %d segments, %d terms, %.1f MB", cold, st.Segments, st.Terms, float64(st.Bytes)/(1<<20))
	if st.Bytes > 96<<20 {
		t.Errorf("the index holds %.1f MB, over 96 MB", float64(st.Bytes)/(1<<20))
	}
	if cold > 3*time.Second*slowdown {
		t.Errorf("cold build %v, over %v", cold, 3*time.Second*slowdown)
	}

	start = time.Now()
	p.Update(func(b *snapshot.Builder) {
		b.UpdateRepo("acme/service-0007", func(e *snapshot.RepoEntry) {
			f := fs[7]
			f.repo.Documents = slices.Clone(f.repo.Documents)
			f.repo.Documents[0].Fields[FieldDescription] = "rewritten"
			data.Set(e, f, snapshot.Freshness{})
		})
		b.Mark("fixture/acme/service-0007", "acme/service-0007", 9999, 9999)
	})
	p.Flush()
	if one := time.Since(start); one > 20*time.Millisecond*slowdown {
		t.Errorf("a publish of one changed repository took %v, over %v", one, 20*time.Millisecond*slowdown)
	}

	ix := Of(p.Current())
	queries := []string{"w0o0", "w1f0fo1 w3cd5o2", `"w0o0 w1f0fo1"`, "lib-3", "org.lib17:lib-4", "service-0042", "rewritten", "w0o", "in:commit author:p7",
		"w2o0 lang:java", "person 12 -w0o0", "dep:lib17 in:repo", "w9999o9", "w1f0fo1 OR w3cd5o2 in:doc"}
	var took []time.Duration
	for range 5 {
		for _, text := range queries {
			q, err := Parse(text)
			if err != nil {
				t.Fatalf("%q: %v", text, err)
			}
			start := time.Now()
			ix.Search(t.Context(), q, nil, 0, 25)
			took = append(took, time.Since(start))
		}
	}
	slices.Sort(took)
	p99 := took[len(took)*99/100]
	t.Logf("query p99 %v, median %v", p99, took[len(took)/2])
	if p99 > 50*time.Millisecond*slowdown {
		t.Errorf("query p99 %v, over %v", p99, 50*time.Millisecond*slowdown)
	}
}
