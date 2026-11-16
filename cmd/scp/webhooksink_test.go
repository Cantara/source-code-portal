package main

import (
	"testing"

	"github.com/cantara/source-code-portal/internal/config"
	"github.com/cantara/source-code-portal/internal/snapshot"
	"github.com/cantara/source-code-portal/internal/sync"
)

// An event of a repository the portal does not know yet lists its source at once (added to the App's selection, then pushed to); a
// known one goes to its own jobs; an owner no source lists is ignored.
func TestWebhookSinkDiscovers(t *testing.T) {
	type took struct {
		ev     sync.Event
		repo   snapshot.RepoKey
		source string
	}
	var got []took
	take := func(ev sync.Event, repo snapshot.RepoKey, source string) error {
		got = append(got, took{ev, repo, source})
		return nil
	}
	known := func(k snapshot.RepoKey) bool { return k == "oranheim/dossier" }
	sink := webhookSink(take, known, []config.Source{{ID: "oranheim", Orgs: []string{"oranheim"}}})
	// A second unknown repository right after the first asks for no second listing: a stream of them costs one.
	for _, e := range []struct{ owner, name string }{{"oranheim", "dossier"}, {"Oranheim", "OsloFotoBilder"}, {"oranheim", "bjelketun-rapporter"}, {"stranger", "x"}} {
		if err := sink(sync.EventPush, e.owner, e.name); err != nil {
			t.Fatal(err)
		}
	}
	want := []took{{sync.EventPush, "oranheim/dossier", ""}, {sync.EventSourceChanged, "", "oranheim"}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("took %+v, want %+v", got, want)
	}
}
