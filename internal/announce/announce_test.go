package announce

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/cantara/source-code-portal/internal/config"
	"github.com/cantara/source-code-portal/internal/httpapi"
	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
	"github.com/cantara/source-code-portal/internal/store"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

func quiet() *slog.Logger { return slog.New(slog.DiscardHandler) }

var now = time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC) //nolint:gochecknoglobals // the tests' clock

// checked is list as the configuration hands it over: cleaned, defaulted and parsed.
func checked(t *testing.T, list ...config.Announcement) []config.Announcement {
	t.Helper()
	if p := config.CheckAnnouncements("announcements", list); len(p) > 0 {
		t.Fatal(p)
	}
	return list
}

func ids(list []oapi.Announcement) []string {
	out := []string{}
	for _, a := range list {
		out = append(out, a.Id)
	}
	return out
}

func TestFor(t *testing.T) {
	off := false
	configured := checked(t,
		config.Announcement{ID: "juleol", Title: "Juleølsmaking", Tone: "event", Starts: "2026-11-26T18:00:00+01:00", Ends: "2026-11-26T23:00:00+01:00",
			Link: config.AnnouncementLink{Label: "Meld deg på her", URL: "https://example.org/pamelding"}, Lang: "nb"},
		config.Announcement{ID: "rotation", Title: "Key rotation", Tone: "notice", Until: "2026-12-01T00:00:00Z", Audience: "signed-in"},
		config.Announcement{ID: "draft", Title: "Not yet", Until: "2026-12-01T00:00:00Z", Published: &off},
		config.Announcement{ID: "later", Title: "Next year", From: "2027-01-01T00:00:00Z", Until: "2027-02-01T00:00:00Z"},
		config.Announcement{ID: "over", Title: "Gone", Until: "2026-11-01T00:00:00Z"},
		config.Announcement{ID: "desk", Title: "On the dashboard", Pages: []string{"dashboard"}, Until: "2026-12-01T00:00:00Z"},
		config.Announcement{ID: "early", Title: "Sooner event", Tone: "event", Starts: "2026-11-21T09:00:00Z", Until: "2026-11-22T00:00:00Z"},
	)
	n := Open(context.Background(), configured, nil, func() time.Time { return now }, quiet())
	public := context.Background()
	private := httpapi.WithViewer(public, httpapi.Viewer{Private: true})
	if got := ids(n.For(public, "present")); !slices.Equal(got, []string{"early", "juleol"}) {
		t.Errorf("public on present = %v: events by their start, no draft, none outside its dates, none for the signed-in", got)
	}
	if got := ids(n.For(private, "dashboard")); !slices.Equal(got, []string{"rotation", "early", "juleol"}) {
		t.Errorf("private on the dashboard = %v: a notice first, at most three", got)
	}
	a := n.For(public, "present")[1]
	if a.Revision == "" || a.StartsAt.Format(time.RFC3339) != "2026-11-26T18:00:00+01:00" || !a.Until.Equal(*a.EndsAt) || *a.Lang != "nb" {
		t.Errorf("served: %+v", a)
	}
	if a.Qr == nil || a.Qr.Size < 21 || !strings.HasPrefix(a.Qr.Path, "M0 0h7") {
		t.Errorf("the link's QR code starts with its finder pattern: %+v", a.Qr)
	}
	b, _ := json.Marshal(a)
	if !strings.Contains(string(b), `"startsAt":"2026-11-26T18:00:00+01:00"`) {
		t.Errorf("the time keeps the offset it was written with: %s", b)
	}
	if again := n.For(public, "present")[1]; again.Revision != a.Revision || again.Qr != a.Qr {
		t.Error("the revision and the code are stable")
	}
}

func TestAdminRoutes(t *testing.T) {
	d := db(t)
	configured := checked(t, config.Announcement{ID: "configured", Title: "From the deployment", Until: "2026-12-01T00:00:00Z"})
	n := Open(context.Background(), configured, d, func() time.Time { return now }, quiet())
	mux := http.NewServeMux()
	for _, r := range n.Routes("https://scp.example", quiet()) {
		mux.Handle(r.Pattern, r.Handler)
	}
	admin := httpapi.Viewer{Identity: httpapi.Identity{User: "ada", Via: "oidc"}, Admin: true, Private: true}
	do := func(v httpapi.Viewer, method, body string, header ...string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequestWithContext(httpapi.WithViewer(context.Background(), v), method, AdminPath, strings.NewReader(body))
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
	if w := do(httpapi.Viewer{}, "GET", ""); w.Code != http.StatusUnauthorized {
		t.Errorf("anonymous: %d", w.Code)
	}
	b := read(do(admin, "GET", ""))
	if len(b.Announcements) != 0 || len(b.Configured) != 1 || !b.Writable || b.Revision != "" {
		t.Fatalf("first read: %+v", b)
	}
	save := `{"revision":"","announcements":[{"id":"juleol","title":"Juleøl","tone":"event","ends":"2026-11-26T23:00:00+01:00"}]}`
	if w := do(admin, "PUT", save, "Cookie", "s=1", "Sec-Fetch-Site", "cross-site"); w.Code != http.StatusForbidden {
		t.Errorf("cross-origin: %d", w.Code)
	}
	w := do(admin, "PUT", save)
	if w.Code != http.StatusOK {
		t.Fatalf("save: %d %s", w.Code, w.Body)
	}
	saved := read(w)
	if len(saved.Announcements) != 1 || saved.Revision == "" || !saved.Announcements[0].IsPublished() || saved.Announcements[0].Published == nil {
		t.Errorf("saved, published explicitly: %+v", saved)
	}
	if got := ids(n.For(context.Background(), "dashboard")); !slices.Equal(got, []string{"juleol", "configured"}) {
		t.Errorf("served after the save: %v", got)
	}
	if w := do(admin, "PUT", save); w.Code != http.StatusPreconditionFailed {
		t.Errorf("a save on the old revision: %d", w.Code)
	}
	for body, want := range map[string]string{
		`{"revision":"` + saved.Revision + `","announcements":[{"id":"configured","title":"Mine","until":"2026-12-01T00:00:00Z"}]}`:                                       "a configured announcement's",
		`{"revision":"` + saved.Revision + `","announcements":[{"id":"x","title":"No end"}]}`:                                                                             "until: required",
		`{"revision":"` + saved.Revision + `","announcements":[{"id":"x","title":"T","until":"2026-12-01T00:00:00Z","link":{"label":"Go","url":"javascript:alert(1)"}}]}`: "link.url",
	} {
		w := do(admin, "PUT", body)
		if got, _ := io.ReadAll(w.Body); w.Code != http.StatusUnprocessableEntity || !strings.Contains(string(got), want) {
			t.Errorf("%s: %d %s, want %q", body, w.Code, got, want)
		}
	}
	if w := do(admin, "PUT", `{"announcements":[],"colour":1}`); w.Code != http.StatusBadRequest {
		t.Errorf("unknown field: %d", w.Code)
	}
	// A stored document that no longer parses (a later version tightened what it accepts) is not served after a restart: only the
	// configured announcements show, and the status carries the problem until an admin saves over it.
	if err := d.PutDocument(context.Background(), Name, []byte(`{"announcements":[{"id":"x"}]}`), "broken", n.file.Get().Revision); err != nil {
		t.Fatal(err)
	}
	n = Open(context.Background(), configured, d, func() time.Time { return now }, quiet())
	if got := ids(n.For(context.Background(), "dashboard")); !slices.Equal(got, []string{"configured"}) || len(n.Problems()) != 1 {
		t.Errorf("broken: %v %v", got, n.Problems())
	}
	ro := Open(context.Background(), nil, nil, time.Now, quiet())
	romux := http.NewServeMux()
	for _, r := range ro.Routes("", quiet()) {
		romux.Handle(r.Pattern, r.Handler)
	}
	r := httptest.NewRequestWithContext(httpapi.WithViewer(context.Background(), admin), "PUT", AdminPath, strings.NewReader(save))
	r.Header.Set("Content-Type", "application/json")
	rw := httptest.NewRecorder()
	romux.ServeHTTP(rw, r)
	if rw.Code != http.StatusConflict {
		t.Errorf("without a store: %d", rw.Code)
	}
}

// db is a store in a temporary directory.
func db(t *testing.T) *store.DB {
	t.Helper()
	d, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "scp.db"), quiet())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}
