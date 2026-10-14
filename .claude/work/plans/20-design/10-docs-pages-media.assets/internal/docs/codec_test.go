package docs

import (
	"strings"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/docs/internal/render"
	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/job"
	"github.com/cantara/source-code-portal/internal/safehtml"
)

func TestConfigDefaultsValidate(t *testing.T) {
	if errs := DefaultConfig().Validate("docs"); len(errs) != 0 {
		t.Fatal(errs)
	}
	c := DefaultConfig()
	c.MaxDocsPerRepo, c.TTL, c.DefaultRoot = 500, time.Second, "../x"
	if errs := c.Validate("docs"); len(errs) != 3 || !strings.Contains(errs[0].Error(), "docs.max_docs_per_repo") {
		t.Fatal(errs)
	}
}

func TestCodecRoundTripResanitises(t *testing.T) {
	pol := safehtml.DocsPolicy()
	src := "# Hi\n\n<!-- hidden -->\ntext\n"
	rd := RepoDocs{HeadSHA: "abc", Pipeline: "r1.p1.kdeadbeef", Fingerprint: "fp", Info: freshness.Info{State: freshness.StateOK}}
	rd.Readme = DocView{Entry: Entry{ID: ReadmeID, Path: "README.md", Title: "Hi", Format: FormatMarkdown, State: StateResident, BlobSHA: "b1"},
		HTML: safehtml.Sanitize(pol, []byte(`<h1 id="doc-hi">Hi</h1><p>text</p>`)), Source: src, TOC: []TOCEntry{{1, "Hi", "doc-hi"}}, RenderedBy: "scp"}
	b, err := Encode(rd)
	if err != nil {
		t.Fatal(err)
	}
	// tamper: inject script into the stored HTML
	bad := strings.Replace(string(b), `<p>text</p>`, `<p>text</p><script>alert(1)</script><img src=x onerror=alert(1)>`, 1)
	got, err := Decode([]byte(bad), rd.Info, pol, render.NormaliseSource)
	if err != nil {
		t.Fatal(err)
	}
	h := got.Readme.HTML.String()
	if strings.Contains(h, "script") || strings.Contains(h, "onerror") || !strings.Contains(h, `id="doc-hi"`) {
		t.Fatal(h)
	}
	if strings.Contains(got.Readme.Markdown, "hidden") || !strings.Contains(got.Readme.Markdown, "text") {
		t.Fatalf("normalised markdown: %q", got.Readme.Markdown)
	}
	if _, err := Decode([]byte(`{"schema":2}`), rd.Info, pol, nil); err == nil {
		t.Fatal("newer schema accepted")
	}
	if _, err := Decode([]byte(`not json`), rd.Info, pol, nil); err == nil {
		t.Fatal("garbage accepted")
	}
}

func TestTouchesDocs(t *testing.T) {
	for _, c := range []struct {
		p  []string
		ok bool
	}{
		{[]string{"README.md"}, true}, {[]string{"readme.RST"}, true}, {[]string{"docs/a/b.md"}, true}, {[]string{"docs/img.png"}, false},
		{[]string{"src/x.go"}, false}, {[]string{".github/scp.yaml"}, true}, {[]string{"CODEOWNERS"}, true}, {[]string{"AGENTS.md"}, true},
		{[]string{"sub/README.md"}, false}, {[]string{".github/README.md"}, true}, {[]string{".github/ISSUE_TEMPLATE/readme.md"}, false}, {[]string{"src/a.go", "CHANGELOG.md"}, true}, {nil, false},
	} {
		if got := TouchesDocs(c.p, "docs"); got != c.ok {
			t.Errorf("TouchesDocs(%v) = %v", c.p, got)
		}
	}
}

func TestHintMapBoundedAndExpires(t *testing.T) {
	h := newHintMap(3, time.Minute)
	t0 := time.Unix(1000, 0)
	for i := 0; i < 10; i++ {
		h.Put(job.Key("docs.refresh/a/b/"+string(rune('a'+i))), "sha", t0.Add(time.Duration(i)*time.Second))
	}
	if h.Len() > 3 {
		t.Fatal(h.Len())
	}
	if _, ok := h.Peek("docs.refresh/a/b/j", t0.Add(time.Hour)); ok {
		t.Fatal("expired hint returned")
	}
	h2 := newHintMap(3, time.Minute)
	h2.Put("docs.refresh/a/b/x", "sha1", t0)
	for i := 0; i < 2; i++ { // a continuation run sees the same hint again
		if v, ok := h2.Peek("docs.refresh/a/b/x", t0.Add(10*time.Second)); !ok || v.After != "sha1" {
			t.Fatal("hint must survive a Peek")
		}
	}
	h2.Forget("docs.refresh/a/b/x")
	if _, ok := h2.Peek("docs.refresh/a/b/x", t0); ok {
		t.Fatal("forgotten hint returned")
	}
}

// TestCodecKeepsReasonAuxImagesAndRejectsTampering pins V-07 (Reason and Aux were not persisted, so after a warm start
// every not_resident document had an empty reason label) and V-08 (non-HTML fields were trusted).
func TestCodecKeepsReasonAuxImagesAndRejectsTampering(t *testing.T) {
	pol := safehtml.DocsPolicy()
	id, _ := DocIDOf("docs/Release Notes.md")
	aid, _ := DocIDOf("AGENTS.md")
	rd := RepoDocs{HeadSHA: "abc", Pipeline: "r1.p1.kdeadbeef", Fingerprint: "fp", Info: freshness.Info{State: freshness.StateOK}}
	rd.Readme = DocView{Entry: Entry{ID: ReadmeID, Path: "README.md", Key: "", Format: FormatMarkdown, State: StateNoContent}}
	rd.Docs = []DocView{
		{Entry: Entry{ID: id, Path: "docs/Release Notes.md", Key: "docs/Release Notes", Format: FormatMarkdown, State: StateNotResident, Reason: "budget"}},
		{Entry: Entry{ID: aid, Path: "AGENTS.md", Key: "AGENTS", Aux: true, Format: FormatMarkdown, State: StateResident},
			HTML: safehtml.Sanitize(pol, []byte("<p>x</p>")), Images: []ImageRef{{"img/a.png", "sha1"}}, TOC: []TOCEntry{{1, "x", "doc-x"}},
			Links: []Link{{"a", "/repos/x", LinkDoc}}},
	}
	b, err := Encode(rd)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(b, rd.Info, pol, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Docs[0].Entry.Reason != "budget" || !got.Docs[1].Entry.Aux || got.Docs[1].Images[0] != (ImageRef{"img/a.png", "sha1"}) || got.Readme.Entry.Key != "" {
		t.Fatalf("lost fields: %+v", got.Docs)
	}
	if m := got.ImageSet(); m["img/a.png"] != "sha1" {
		t.Fatal(m)
	}
	for name, mut := range map[string]func(string) string{
		"link scheme": func(s string) string { return strings.Replace(s, `"h":"/repos/x"`, `"h":"javascript:alert(1)"`, 1) },
		"state":       func(s string) string { return strings.Replace(s, `"state":"resident"`, `"state":"x"`, 1) },
		"id vs path":  func(s string) string { return strings.Replace(s, `"id":"AGENTS.md"`, `"id":"other"`, 1) },
		"toc id":      func(s string) string { return strings.Replace(s, `"i":"doc-x"`, `"i":"x"`, 1) },
		"format":      func(s string) string { return strings.Replace(s, `"format":"markdown"`, `"format":"html"`, 1) },
	} {
		if _, err := Decode([]byte(mut(string(b))), rd.Info, pol, nil); err == nil {
			t.Errorf("%s: tampered payload accepted", name)
		}
	}
}
