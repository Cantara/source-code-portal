package releases

import (
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/snapshot"
	"github.com/cantara/source-code-portal/internal/store"
	"github.com/cantara/source-code-portal/internal/sync"
)

const changelog = `# Changelog

All notable changes are written here.

## [Unreleased]

- nothing yet

## [2.1.0] - 2026-10-20

### Added

- single sign-on

### Fixed

- a crash

## v2.0.0 (2026-09-01)

Breaking: the config moved.

` + "```" + `
## 9.9.9 in a fence is not a heading
` + "```" + `

## 1.9.0-rc.1

- candidate

## Older notes

- not a version

## [1.0.0](https://example.com/compare) (2026-01-01)

- first
`

func TestSections(t *testing.T) {
	got := sections(changelog)
	var versions []string
	for _, s := range got {
		versions = append(versions, s.Version)
	}
	if fmt.Sprint(versions) != "[2.1.0 2.0.0 1.9.0-rc.1 1.0.0]" {
		t.Fatalf("versions = %v: the Unreleased section and the prose are not versions", versions)
	}
	if n := got[0].Notes; !strings.Contains(n, "### Added") || !strings.Contains(n, "a crash") || strings.Contains(n, "Breaking") {
		t.Errorf("2.1.0 = %q: deeper headings belong to the section, the next version's text does not", n)
	}
	if n := got[1].Notes; !strings.Contains(n, "## 9.9.9 in a fence") || !strings.Contains(n, "config moved") {
		t.Errorf("2.0.0 = %q: a heading in a fence is text", n)
	}
	if n := got[2].Notes; strings.Contains(n, "not a version") {
		t.Errorf("1.9.0-rc.1 = %q: a heading that names no version ends a section", n)
	}
}

func TestSectionsBounds(t *testing.T) {
	var b strings.Builder
	for i := 20; i > 0; i-- {
		fmt.Fprintf(&b, "## %d.0.0\n\n%s\n\n", i, strings.Repeat("a line of notes\n", 400))
	}
	got := sections(b.String())
	if len(got) != keepSections {
		t.Fatalf("%d sections, want the newest %d", len(got), keepSections)
	}
	if !got[0].Cut || len(got[0].Notes) > maxSection || !strings.HasSuffix(got[0].Notes, "a line of notes") {
		t.Errorf("a long section is cut at a line end: %d bytes, cut=%v", len(got[0].Notes), got[0].Cut)
	}
}

func TestSectionOfIgnoresV(t *testing.T) {
	set := &Set{Changelog: []Section{{Version: "2.0.0", Notes: "x"}}}
	if sectionOf(set, "v2.0.0") == nil || sectionOf(set, "2.0.0") == nil || sectionOf(set, "2.0.1") != nil || sectionOf(nil, "2.0.0") != nil {
		t.Error("a tag with or without v finds its section, another version none")
	}
}

// TestJobKeepsNotes: a release's body is kept cleaned and cut, the changelog's newest sections beside the releases.
func TestJobKeepsNotes(t *testing.T) {
	long := "# Big\n\n" + strings.Repeat("- a change worth telling\n", 1000) + "\u202e"
	rels := []Release{{Tag: "v2.1.0", Notes: "## Highlights\n\n- sso\u202e\n", Published: day(3)}, {Tag: "v2.0.0", Notes: long, Published: day(1)}}
	res, err := New(map[string]Fetcher{"gh": fetcher{rels: rels, changelog: changelog}}).Spec().Run(t.Context(), sync.Job{Source: "gh", Repo: "acme/x"})
	if err != nil {
		t.Fatal(err)
	}
	p := snapshot.NewPublisher(time.Hour, time.Hour, slog.New(slog.DiscardHandler))
	spec := New(nil).Spec()
	p.Update(func(b *snapshot.Builder) {
		b.ReplaceSource("gh", []*snapshot.RepoEntry{{Repo: snapshot.Repo{ID: snapshot.RepoID{Source: "gh", GitHubID: 1}, Owner: "acme", Name: "x"}}}, nil, snapshot.SourceStatus{ID: "gh"})
		spec.Publish(b, store.Item{Repo: "acme/x", State: snapshot.StateOK, Payload: res.Payload}, snapshot.Freshness{State: snapshot.StateOK})
	})
	p.Flush()
	set, _ := Of(p.Current().Repos["acme/x"])
	if n := set.Releases[0].Notes; strings.ContainsRune(n, '\u202e') || !strings.HasPrefix(n, "## Highlights") || set.Releases[0].NotesCut {
		t.Errorf("notes = %q cut=%v: cleaned, whole", n, set.Releases[0].NotesCut)
	}
	if r := set.Releases[1]; !r.NotesCut || len(r.Notes) > maxNotes || strings.ContainsRune(r.Notes, '\u202e') {
		t.Errorf("long notes: %d bytes, cut=%v", len(r.Notes), r.NotesCut)
	}
	if len(set.Changelog) != 4 || set.Changelog[0].Version != "2.1.0" {
		t.Errorf("changelog = %+v", set.Changelog)
	}
}
