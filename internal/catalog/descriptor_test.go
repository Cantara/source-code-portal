package catalog

import (
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/snapshot"
	"github.com/cantara/source-code-portal/internal/store"
	"github.com/cantara/source-code-portal/internal/sync"
)

func codes(d snapshot.Descriptor) []string {
	var out []string
	for _, p := range d.Problems {
		out = append(out, p.Code+p.Pointer)
	}
	return out
}

func TestParseDescriptor(t *testing.T) {
	full := `schema: 1
title: Whydah SSO
description: Single sign-on
lifecycle: production
owners: ["@cantara", "@cantara/whydah", "nobody"]
tags: [iam, iam, Bad_Tag]
links:
  - {title: Docs, url: "https://wiki.cantara.no/whydah"}
  - {title: Plain, url: "http://insecure.example"}
colour: blue
`
	d := ParseDescriptor([]byte(full))
	if !d.Present || d.Title != "Whydah SSO" || d.Lifecycle != "production" || !slices.Equal(d.Owners, []string{"@cantara", "@cantara/whydah"}) ||
		!slices.Equal(d.Tags, []string{"iam"}) || len(d.Links) != 1 || d.Links[0].URL != "https://wiki.cantara.no/whydah" {
		t.Errorf("descriptor = %+v", d)
	}
	if got := codes(d); !slices.Equal(got, []string{"field.invalid/owners/2", "field.invalid/tags/2", "field.invalid/links/1", "field.unknown/colour"}) {
		t.Errorf("problems = %v", got)
	}

	rejected := map[string]string{
		"too large":     "schema: 1\ntitle: " + strings.Repeat("x", maxDescriptorBytes),
		"syntax":        "schema: [1\n",
		"empty":         "",
		"two documents": "schema: 1\n---\nschema: 1\n",
		"anchor":        "schema: 1\na: &x [1]\nb: *x\n",
		"merge":         "schema: 1\nbase: {title: x}\nother:\n  <<: {title: y}\n",
		"not a mapping": "- schema\n",
		"no schema":     "title: x\n",
		"schema 2":      "schema: 2\n",
		"deep":          "schema: 1\nx: " + strings.Repeat("[", 40) + strings.Repeat("]", 40) + "\n",
		"not utf-8":     "schema: 1\ntitle: \xff\n",
	}
	for name, doc := range rejected {
		if d := ParseDescriptor([]byte(doc)); d.Present || len(d.Problems) != 1 {
			t.Errorf("%s: %+v, want one problem and no descriptor", name, d)
		}
	}

	if d := ParseDescriptor([]byte("schema: 1\ntitle: \"\"\nlifecycle: retired\ndescription: [x]\n")); d.Title != "" || d.Lifecycle != "" ||
		len(d.Problems) != 3 {
		t.Errorf("invalid fields: %+v", d)
	}
}

func FuzzParseDescriptor(f *testing.F) {
	for _, s := range []string{"schema: 1\ntitle: x\n", "a: &a [*a]\n", "schema: 1\nlinks: [{title: t, url: 'https://x'}]\n"} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		d := ParseDescriptor(b)
		if !d.Present && len(d.Problems) != 1 {
			t.Fatalf("a rejected document has %d problems", len(d.Problems))
		}
		if len(d.Owners) > maxListItems || len(d.Tags) > maxListItems || len(d.Links) > maxListItems {
			t.Fatal("list over its cap")
		}
	})
}

func TestDescriptorJob(t *testing.T) {
	l := &lister{repos: map[string][]snapshot.Repo{"acme": {pub(1, "a")}}, files: map[string]string{
		"scp.yaml":         "schema: 1\ntitle: Root\n",
		".github/scp.yaml": "schema: 1\ntitle: Dotgithub\n",
	}}
	c := newCatalog(t, l)
	spec := c.DescriptorSpec()
	job := sync.Job{Key: DescriptorKind + "/acme/a", Source: "gh", Repo: "acme/a"}
	res, err := spec.Run(t.Context(), job)
	if err != nil || res.State != snapshot.StateOK || !strings.Contains(string(res.Payload), "Dotgithub") {
		t.Fatalf("first run = %+v, %v", res, err)
	}
	job.Prev = store.Item{Repo: "acme/a", State: res.State, ContentSHA: res.ContentSHA, Payload: res.Payload}
	if again, _ := spec.Run(t.Context(), job); !again.Unchanged {
		t.Error("same blob SHA is not Unchanged")
	}

	l.files[".github/scp.yml"] = "schema: 1\n"
	if res, _ := spec.Run(t.Context(), job); res.State != snapshot.StateNotFound || !strings.Contains(string(res.Payload), "descriptor.ambiguous") {
		t.Errorf("ambiguous = %+v", res)
	}
	delete(l.files, ".github/scp.yml")
	delete(l.files, ".github/scp.yaml")
	res, _ = spec.Run(t.Context(), job)
	if !strings.Contains(string(res.Payload), "Root") {
		t.Errorf("root fallback = %s", res.Payload)
	}

	p := snapshot.NewPublisher(time.Hour, time.Hour, slog.New(slog.DiscardHandler))
	p.Update(func(b *snapshot.Builder) {
		b.ReplaceSource("gh", []*snapshot.RepoEntry{{Repo: pub(1, "a")}}, nil, snapshot.SourceStatus{ID: "gh"})
		spec.Publish(b, store.Item{Repo: "acme/a", State: res.State, Payload: res.Payload}, snapshot.Freshness{State: res.State})
	})
	p.Flush()
	if d := p.Current().Repos["acme/a"].Descriptor; !d.Present || d.Title != "Root" || d.File != "scp.yaml" {
		t.Errorf("published descriptor = %+v", d)
	}
}
