package layout

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/cantara/source-code-portal/internal/httpapi"
	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
	"github.com/cantara/source-code-portal/internal/snapshot"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

func quiet() *slog.Logger { return slog.New(slog.DiscardHandler) }

func TestParse(t *testing.T) {
	for name, tc := range map[string]struct {
		body, want string
	}{
		"default":           {body: `{"elements":[{"kind":"telemetry"},{"kind":"fleet-map"},{"kind":"products","view":"table","groups":["a","b-c"]},{"kind":"latest-commits","count":5}]}`},
		"heading":           {body: `{"elements":[{"kind":"heading","text":"  Our\nproducts ","note":"by team"}]}`},
		"empty":             {body: `{"elements":[]}`, want: "elements: 1..50"},
		"unknown kind":      {body: `{"elements":[{"kind":"chart"}]}`, want: "unknown kind"},
		"unknown field":     {body: `{"elements":[{"kind":"telemetry","colour":"red"}]}`, want: "unknown field"},
		"foreign option":    {body: `{"elements":[{"kind":"telemetry","count":3}]}`, want: "count is not an option"},
		"bad view":          {body: `{"elements":[{"kind":"products","view":"list"}]}`, want: "view"},
		"present":           {body: `{"elements":[{"kind":"fleet-map","color":"health"},{"kind":"products","view":"tiles"}]}`},
		"bad color":         {body: `{"elements":[{"kind":"fleet-map","color":"red"}]}`, want: "color"},
		"foreign color":     {body: `{"elements":[{"kind":"products","color":"health"}]}`, want: "color is not an option"},
		"no groups":         {body: `{"elements":[{"kind":"fleet-map","groups":[]}]}`, want: "groups: 1..100"},
		"bad group":         {body: `{"elements":[{"kind":"fleet-map","groups":["Not An Id"]}]}`, want: "groups[0]"},
		"twice":             {body: `{"elements":[{"kind":"fleet-map","groups":["a","a"]}]}`, want: "listed twice"},
		"count":             {body: `{"elements":[{"kind":"latest-commits","count":51}]}`, want: "count: 1..50"},
		"heading text":      {body: "{\"elements\":[{\"kind\":\"heading\",\"text\":\"\u200b \"}]}", want: "text: required"},
		"two values":        {body: `{"elements":[{"kind":"telemetry"}]}{}`, want: "more than one"},
		"not json":          {body: `elements`, want: "not a layout"},
		"too large":         {body: `{"elements":[` + strings.Repeat(" ", maxBytes) + `]}`, want: "larger than"},
		"too many elements": {body: `{"elements":[` + strings.Repeat(`{"kind":"telemetry"},`, 50) + `{"kind":"telemetry"}]}`, want: "1..50"},
	} {
		t.Run(name, func(t *testing.T) {
			l, err := Parse([]byte(tc.body))
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				if name == "heading" && (*l.Elements[0].Text != "Our products" || *l.Elements[0].Note != "by team") {
					t.Errorf("heading not cleaned: %q %q", *l.Elements[0].Text, *l.Elements[0].Note)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want %q", err, tc.want)
			}
		})
	}
	for _, page := range pages {
		if err := Default(page).Validate(); err != nil {
			t.Errorf("the default layout of %s: %v", page, err)
		}
	}
}

func layoutOf(kinds ...oapi.LayoutElementKind) Layout {
	l := Layout{}
	for _, k := range kinds {
		l.Elements = append(l.Elements, oapi.LayoutElement{Kind: k})
	}
	return l
}

func TestStoreSaveConflictReset(t *testing.T) {
	dir := t.TempDir()
	s := Open(dir, quiet())
	if p := s.Get("dashboard"); p.Stored || p.Revision != "" || len(p.Layout.Elements) != 7 {
		t.Fatalf("before a save: %+v", p)
	}
	rev, err := s.Save("dashboard", layoutOf(oapi.LayoutElementKindTelemetry), "")
	if err != nil || rev == "" {
		t.Fatal(rev, err)
	}
	if p := s.Get("dashboard"); !p.Stored || p.Revision != rev || len(p.Layout.Elements) != 1 {
		t.Errorf("after a save: %+v", p)
	}
	if _, err := s.Save("dashboard", layoutOf(oapi.LayoutElementKindFleetMap), ""); !errors.Is(err, ErrConflict) {
		t.Errorf("a save on a stale revision: %v", err)
	}
	if _, err := s.Save("dashboard", Layout{}, rev); !errors.As(err, new(Invalid)) {
		t.Errorf("an empty layout: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != "dashboard.json" {
		t.Errorf("files = %v, want only dashboard.json (no temporary file left)", entries)
	}
	if again := Open(dir, quiet()).Get("dashboard"); again.Revision != rev || !again.Stored {
		t.Errorf("reopened: %+v", again)
	}
	if err := s.Reset("dashboard", "stale"); !errors.Is(err, ErrConflict) {
		t.Errorf("a reset on a stale revision: %v", err)
	}
	if err := s.Reset("dashboard", rev); err != nil {
		t.Fatal(err)
	}
	if p := s.Get("dashboard"); p.Stored || len(p.Layout.Elements) != 7 {
		t.Errorf("after a reset: %+v", p)
	}
	if _, err := os.Stat(filepath.Join(dir, "dashboard.json")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the file is still there: %v", err)
	}
	if _, err := Open("", quiet()).Save("dashboard", layoutOf(oapi.LayoutElementKindTelemetry), ""); !errors.Is(err, ErrReadOnly) {
		t.Errorf("without a directory: %v", err)
	}
}

// TestStoreReload: a file written by hand is picked up, a broken one keeps the last good layout with a problem an admin can save
// over, and a removed one brings back the built-in layout.
func TestStoreReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dashboard.json")
	s := Open(dir, quiet())
	write := func(body string, mod time.Time) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, mod, mod); err != nil {
			t.Fatal(err)
		}
		s.reload("dashboard")
	}
	base := time.Now().Add(-time.Hour)
	write(`{"elements":[{"kind":"telemetry"},{"kind":"fleet-map"}]}`, base)
	if p := s.Get("dashboard"); !p.Stored || len(p.Layout.Elements) != 2 {
		t.Fatalf("written by hand: %+v", p)
	}
	write(`{"elements":[{"kind":"chart"}]}`, base.Add(time.Minute))
	p := s.Get("dashboard")
	if !p.Stored || len(p.Layout.Elements) != 2 || !strings.Contains(p.Problem, "unknown kind") || len(s.Problems()) != 1 {
		t.Fatalf("broken: %+v %v", p, s.Problems())
	}
	if _, err := s.Save("dashboard", layoutOf(oapi.LayoutElementKindTelemetry), p.Revision); err != nil {
		t.Errorf("saving over a broken file: %v", err)
	}
	if s.Get("dashboard").Problem != "" {
		t.Error("the problem stays after a good save")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	s.reload("dashboard")
	if p := s.Get("dashboard"); p.Stored || len(p.Layout.Elements) != 7 {
		t.Errorf("removed: %+v", p)
	}
}

func TestResolve(t *testing.T) {
	pub := snapshot.NewPublisher(time.Hour, time.Hour, quiet())
	pub.Update(func(b *snapshot.Builder) {
		b.SetGroups([]snapshot.Group{{ID: "open", Members: []snapshot.RepoKey{"acme/a"}}, {ID: "hidden", Members: []snapshot.RepoKey{}}})
	})
	pub.Flush()
	s := pub.Current()
	l := Layout{Elements: []oapi.LayoutElement{
		{Kind: oapi.LayoutElementKindProducts, Groups: &[]string{"hidden", "open", "gone"}},
		{Kind: oapi.LayoutElementKindFleetMap, Groups: &[]string{"hidden"}},
		{Kind: oapi.LayoutElementKindTelemetry},
	}}
	got := Resolve(l, s)
	if len(got) != 2 || !slices.Equal(*got[0].Groups, []string{"open"}) || got[1].Kind != oapi.LayoutElementKindTelemetry {
		t.Errorf("resolved = %+v", got)
	}
	if len(*l.Elements[0].Groups) != 3 {
		t.Error("Resolve changed the stored layout")
	}
	if got := Resolve(Layout{Elements: []oapi.LayoutElement{{Kind: oapi.LayoutElementKindPulls}, {Kind: oapi.LayoutElementKindTelemetry}}}, s,
		oapi.LayoutElementKindPulls); len(got) != 1 || got[0].Kind != oapi.LayoutElementKindTelemetry {
		t.Errorf("switched off = %+v", got)
	}
}

func TestAdminRoutes(t *testing.T) {
	s := Open(t.TempDir(), quiet())
	mux := http.NewServeMux()
	for _, r := range s.Routes("https://scp.example") {
		mux.Handle(r.Pattern, r.Handler)
	}
	admin := httpapi.Viewer{Identity: httpapi.Identity{User: "ada", Via: "oidc"}, Admin: true, Private: true}
	do := func(v httpapi.Viewer, method, target, body string, header ...string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequestWithContext(httpapi.WithViewer(context.Background(), v), method, target, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		for i := 0; i < len(header); i += 2 {
			r.Header.Set(header[i], header[i+1])
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	read := func(w *httptest.ResponseRecorder) adminBody {
		t.Helper()
		var b adminBody
		if err := json.NewDecoder(w.Body).Decode(&b); err != nil {
			t.Fatal(err)
		}
		return b
	}
	const at = "/api/v1/admin/layouts/dashboard"
	if w := do(httpapi.Viewer{}, "GET", at, ""); w.Code != http.StatusUnauthorized {
		t.Errorf("anonymous: %d", w.Code)
	}
	if w := do(httpapi.Viewer{Identity: httpapi.Identity{User: "bo", Via: "oidc"}}, "GET", at, ""); w.Code != http.StatusForbidden {
		t.Errorf("not an admin: %d", w.Code)
	}
	b := read(do(admin, "GET", at, ""))
	if b.Stored || !b.Writable || b.Revision != "" || len(b.Elements) != 7 {
		t.Fatalf("first read: %+v", b)
	}
	save := `{"revision":"","elements":[{"kind":"heading","text":"Ours"},{"kind":"products","view":"table"}]}`
	if w := do(admin, "PUT", at, save, "Cookie", "s=1", "Sec-Fetch-Site", "cross-site"); w.Code != http.StatusForbidden {
		t.Errorf("cross-origin: %d", w.Code)
	}
	w := do(admin, "PUT", at, save)
	if w.Code != http.StatusOK {
		t.Fatalf("save: %d %s", w.Code, w.Body)
	}
	saved := read(w)
	if !saved.Stored || saved.Revision == "" || len(saved.Elements) != 2 {
		t.Errorf("saved: %+v", saved)
	}
	if w := do(admin, "PUT", at, save); w.Code != http.StatusPreconditionFailed {
		t.Errorf("a second save on the old revision: %d", w.Code)
	}
	if w := do(admin, "PUT", at, `{"revision":"`+saved.Revision+`","elements":[{"kind":"chart"}]}`); w.Code != http.StatusUnprocessableEntity {
		t.Errorf("invalid: %d", w.Code)
	} else if body, _ := io.ReadAll(w.Body); !strings.Contains(string(body), "unknown kind") {
		t.Errorf("invalid says %s", body)
	}
	if w := do(admin, "PUT", at, `{"elements":[],"colour":1}`); w.Code != http.StatusBadRequest {
		t.Errorf("unknown field: %d", w.Code)
	}
	if w := do(admin, "PUT", "/api/v1/admin/layouts/other", save); w.Code != http.StatusNotFound {
		t.Errorf("unknown page: %d", w.Code)
	}
	if w := do(admin, "DELETE", at+"?revision="+saved.Revision, ""); w.Code != http.StatusOK || read(w).Stored {
		t.Errorf("reset: %d", w.Code)
	}
	ro := http.NewServeMux()
	for _, r := range Open("", quiet()).Routes("") {
		ro.Handle(r.Pattern, r.Handler)
	}
	r := httptest.NewRequestWithContext(httpapi.WithViewer(context.Background(), admin), "PUT", at, strings.NewReader(save))
	r.Header.Set("Content-Type", "application/json")
	rw := httptest.NewRecorder()
	ro.ServeHTTP(rw, r)
	if rw.Code != http.StatusConflict {
		t.Errorf("without layout.dir: %d", rw.Code)
	}
}
