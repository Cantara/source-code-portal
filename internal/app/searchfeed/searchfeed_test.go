package searchfeed

import (
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/cantara/source-code-portal/internal/feature/releases"
	"github.com/cantara/source-code-portal/internal/feature/search"
	"github.com/cantara/source-code-portal/internal/platform/snapshot"
	"github.com/cantara/source-code-portal/internal/platform/store"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

// TestSearchReadsReleases: each release held is a document of the kind release that opens the repository's releases.
func TestSearchReadsReleases(t *testing.T) {
	p := snapshot.NewPublisher(time.Hour, time.Hour, slog.New(slog.DiscardHandler))
	now := time.Now().UTC()
	spec := releases.New(nil).Spec()
	p.Update(func(b *snapshot.Builder) {
		b.ReplaceSource("gh", []*snapshot.RepoEntry{{Repo: snapshot.Repo{ID: snapshot.RepoID{Source: "gh", GitHubID: 1}, Owner: "acme", Name: "core", Visibility: "public"}}}, nil,
			snapshot.SourceStatus{ID: "gh"})
		payload, _ := json.Marshal(releases.Set{Releases: []releases.Release{
			{Tag: "v2.0.0", Name: "Two", Summary: "Single sign-on", Published: now, Author: "ann"},
			{Tag: "v1.0.0", Published: now.AddDate(0, 0, -30)},
		}})
		spec.Publish(b, store.Item{Repo: "acme/core", State: snapshot.StateOK, Payload: payload}, snapshot.Freshness{State: snapshot.StateOK})
	})
	p.Flush()
	var got []search.Document
	for _, d := range Read(p.Current().Repos["acme/core"]).Documents {
		if d.Kind == search.KindRelease {
			got = append(got, d)
		}
	}
	if len(got) != 2 || got[0].Title != "v2.0.0 Two" || got[1].Title != "v1.0.0" || got[0].URL != "/releases?repo=acme%2Fcore&variant=all" ||
		got[0].Fields[search.FieldText] != "Single sign-on" || got[0].Fields[search.FieldAuthor] != "ann" {
		t.Errorf("release documents = %+v", got)
	}
}
