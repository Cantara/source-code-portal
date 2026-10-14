package pages

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/readmodel"
	"github.com/cantara/source-code-portal/internal/safehtml"
)

type fakeRenderer struct{}

func (fakeRenderer) RenderPage(_ context.Context, src []byte) (RenderedPage, error) {
	s := string(src)
	if strings.Contains(s, "BOOM") {
		return RenderedPage{}, io.ErrUnexpectedEOF
	}
	o := 5
	return RenderedPage{HTML: safehtml.Text(s), Title: strings.SplitN(s, "\n", 2)[0], Description: "d\x00\n  e", Order: &o}, nil
}

func write(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadConfigMapLayoutAndEscapes(t *testing.T) {
	root := t.TempDir()
	// Kubernetes ConfigMap layout: files are symlinks to ..data/<file>, ..data is a symlink to a timestamped directory.
	write(t, filepath.Join(root, "..2026_10_14", "about.md"), "About\nbody")
	write(t, filepath.Join(root, "..2026_10_14", "faq.md"), "FAQ")
	os.Symlink("..2026_10_14", filepath.Join(root, "..data"))
	os.Symlink(filepath.Join("..data", "about.md"), filepath.Join(root, "about.md"))
	os.Symlink(filepath.Join("..data", "faq.md"), filepath.Join(root, "faq.md"))
	// an escaping symlink and bad names must not be served
	outside := filepath.Join(t.TempDir(), "secret.md")
	write(t, outside, "SECRET")
	os.Symlink(outside, filepath.Join(root, "leak.md"))
	write(t, filepath.Join(root, "Bad_Name.md"), "x")
	write(t, filepath.Join(root, "boom.md"), "BOOM")
	write(t, filepath.Join(root, "big.md"), strings.Repeat("a", 300<<10))
	write(t, filepath.Join(root, "notes.txt"), "x")

	set, err := Load(context.Background(), root, DefaultConfig(), fakeRenderer{}, time.Unix(1000, 0))
	if err != nil {
		t.Fatal(err)
	}
	var slugs []string
	for _, p := range set.Pages {
		slugs = append(slugs, p.Slug)
		if strings.Contains(p.HTML.String(), "SECRET") {
			t.Fatal("escaped the root")
		}
	}
	if strings.Join(slugs, ",") != "about,faq" {
		t.Fatal(slugs, set.Problems)
	}
	codes := map[string]string{}
	for _, p := range set.Problems {
		codes[p.File] = p.Code
	}
	for f, c := range map[string]string{"leak.md": "page.unreadable", "Bad_Name.md": "page.bad-slug", "boom.md": "page.render-failed", "big.md": "page.too-large"} {
		if codes[f] != c {
			t.Errorf("%s: %q want %q (%v)", f, codes[f], c, set.Problems)
		}
	}
	if set.Pages[0].Summary != "d e" || set.Fingerprint == "" {
		t.Fatalf("%+v", set.Pages[0])
	}
	if _, err := Load(context.Background(), filepath.Join(root, "nope"), DefaultConfig(), fakeRenderer{}, time.Now()); err == nil {
		t.Fatal("missing dir must fail")
	}
}

func TestAnnouncementAndReload(t *testing.T) {
	c := DefaultConfig()
	c.Announcement = AnnouncementConfig{Enabled: true, Text: "  GitHub\x00 maintenance\n\tSaturday ", Level: "warning", Dismissible: true}
	a := announcementOf(c.Announcement)
	if a == nil || a.Text != "GitHub maintenance Saturday" || len(a.ID) != 8 {
		t.Fatalf("%+v", a)
	}
	c2 := c.Announcement
	c2.Text += "!"
	if announcementOf(c2).ID == a.ID {
		t.Fatal("id must change with the text")
	}
	if errs := (Config{MaxPages: 1, MaxPageBytes: 1 << 10, Announcement: AnnouncementConfig{Enabled: true, Level: "loud"}}).Validate("pages"); len(errs) != 2 {
		t.Fatal(errs)
	}

	dir := t.TempDir()
	write(t, filepath.Join(dir, "about.md"), "About")
	reg := &readmodel.Registry{}
	readmodel.Register[int](reg, "catalog.repo", readmodel.Existence())
	g := readmodel.RegisterGlobal[Set](reg, GlobalName)
	reg.Freeze()
	pub := readmodel.NewPublisher(reg, nil, nil, readmodel.Deps{})
	c.Dir = dir
	s, err := New(Deps{Cfg: c, Renderer: fakeRenderer{}, Pub: pub, Global: g, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	v1 := pub.Current().Version()
	if err := s.Reload(context.Background()); err != nil || pub.Current().Version() != v1 {
		t.Fatal("unchanged reload must not publish")
	}
	write(t, filepath.Join(dir, "more.md"), "More")
	if err := s.Reload(context.Background()); err != nil || pub.Current().Version() == v1 {
		t.Fatal("changed reload must publish")
	}
	os.RemoveAll(dir)
	if err := s.Reload(context.Background()); err == nil {
		t.Fatal("expected an error")
	}
	set, _ := readmodel.GetGlobal(pub.Current(), g)
	if len(set.Pages) != 2 || set.Info.State != "error" {
		t.Fatalf("stale-while-error: %d pages, state %s", len(set.Pages), set.Info.State)
	}
}

// TestFirstLoadFailurePublishesEmptySetWithAnnouncement pins V-10: the design promised an empty Set carrying the error,
// the first code published nothing, so the announcement (pure configuration) vanished with the directory.
func TestFirstLoadFailurePublishesEmptySetWithAnnouncement(t *testing.T) {
	c := DefaultConfig()
	c.Announcement = AnnouncementConfig{Enabled: true, Text: "Maintenance", Level: "info"}
	c.Dir = filepath.Join(t.TempDir(), "missing")
	reg := &readmodel.Registry{}
	readmodel.Register[int](reg, "catalog.repo", readmodel.Existence())
	g := readmodel.RegisterGlobal[Set](reg, GlobalName)
	reg.Freeze()
	pub := readmodel.NewPublisher(reg, nil, nil, readmodel.Deps{})
	s, err := New(Deps{Cfg: c, Renderer: fakeRenderer{}, Pub: pub, Global: g, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Reload(context.Background()); err == nil {
		t.Fatal("expected the directory error")
	}
	set, ok := readmodel.GetGlobal(pub.Current(), g)
	if !ok || len(set.Pages) != 0 || set.Announcement == nil || set.Info.State != "error" || set.Info.ErrCode != "pages.reload_failed" {
		t.Fatalf("%v %+v", ok, set)
	}
}

// TestNewProblemChangesFingerprint: a file that appears with a bad name changes /status, so it must publish.
func TestNewProblemChangesFingerprint(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "about.md"), "About")
	c := DefaultConfig()
	s1, _ := Load(context.Background(), dir, c, fakeRenderer{}, time.Now())
	write(t, filepath.Join(dir, "Bad_Name.md"), "x")
	s2, _ := Load(context.Background(), dir, c, fakeRenderer{}, time.Now())
	if s1.Fingerprint == s2.Fingerprint || len(s2.Problems) != 1 {
		t.Fatal(s1.Fingerprint, s2.Fingerprint, s2.Problems)
	}
}
