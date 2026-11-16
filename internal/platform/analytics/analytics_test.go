package analytics

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidateAndEnv(t *testing.T) {
	if Env("") != "" || len(Validate("", "")) != 0 {
		t.Error("without a site there is nothing to tell or to check")
	}
	if got, want := Env("scp.example.org"), `{"domain":"scp.example.org","path":"/_v/m"}`; got != want {
		t.Errorf("Env = %s, want %s", got, want)
	}
	for _, c := range []struct{ domain, endpoint, want string }{
		{"scp.example.org", "", ""},
		{"scp.example.org/app", "https://stats.example.org/api/event", ""},
		{"localhost", "http://127.0.0.1:8000/api/event", ""},
		{"not a domain", "", "web.plausible_domain"},
		{"scp.example.org", "http://stats.example.org/api/event", "must be https"},
		{"scp.example.org", "https://stats.example.org/e?x=1", "web.plausible_endpoint"},
		{"scp.example.org", "https://user:pw@stats.example.org/e", "web.plausible_endpoint"},
		{"", "https://stats.example.org/api/event", "needs web.plausible_domain"},
	} {
		p := strings.Join(Validate(c.domain, c.endpoint), "; ")
		if (c.want == "") != (p == "") || !strings.Contains(p, c.want) {
			t.Errorf("Validate(%q, %q) = %q, want %q", c.domain, c.endpoint, p, c.want)
		}
	}
}

// The relay sends the endpoint the event, the visitor's browser and address and a media type, and nothing else.
func TestRelayForwardsOnlyWhatPlausibleNeeds(t *testing.T) {
	var got http.Header
	var body string
	end := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, body = r.Header.Clone(), ""
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer end.Close()
	h := New(end.URL, func(*http.Request) string { return "203.0.113.9" }, nil).Handler()

	post := func(body string, hdr map[string]string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, Path, strings.NewReader(body))
		r.Header.Set("User-Agent", "Mozilla/5.0 test")
		r.Header.Set("Cookie", "scp_session=secret")
		r.Header.Set("Authorization", "Bearer secret")
		r.Header.Set("Referer", "https://scp.example/repos/acme/secret")
		r.Header.Set("Origin", "https://scp.example")
		r.Header.Set("X-Scp-Token", "t0ken")
		for k, v := range hdr {
			r.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec
	}
	if rec := post(`{"n":"pageview","u":"https://scp.example/"}`, nil); rec.Code != http.StatusAccepted {
		t.Fatalf("event: %d", rec.Code)
	}
	if body != `{"n":"pageview","u":"https://scp.example/"}` || got.Get("User-Agent") != "Mozilla/5.0 test" || got.Get("X-Forwarded-For") != "203.0.113.9" ||
		got.Get("Content-Type") != "text/plain" {
		t.Errorf("forwarded %q with %v", body, got)
	}
	for _, h := range []string{"Cookie", "Authorization", "Referer", "Origin", "X-Scp-Token"} {
		if got.Get(h) != "" {
			t.Errorf("%s went to the endpoint: %q", h, got.Get(h))
		}
	}
	if rec := post(`{"n":"pageview"}`, map[string]string{"Sec-Fetch-Site": "cross-site"}); rec.Code != http.StatusForbidden {
		t.Errorf("a request from another site: %d", rec.Code)
	}
	if rec := post("", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("no body: %d", rec.Code)
	}
	if rec := post(strings.Repeat("x", maxBody+1), nil); rec.Code != http.StatusBadRequest {
		t.Errorf("a large body: %d", rec.Code)
	}
}

func TestRelayAnswersForAnEndpointThatFails(t *testing.T) {
	end := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) }))
	defer end.Close()
	h := New(end.URL, func(*http.Request) string { return "" }, nil).Handler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, Path, strings.NewReader("{}")))
	if rec.Code != http.StatusBadGateway || rec.Body.Len() != 0 {
		t.Errorf("a failing endpoint: %d %q", rec.Code, rec.Body)
	}
	closed := New("http://127.0.0.1:1/", func(*http.Request) string { return "" }, nil).Handler()
	rec = httptest.NewRecorder()
	closed.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, Path, strings.NewReader("{}")))
	if rec.Code != http.StatusBadGateway {
		t.Errorf("an unreachable endpoint: %d", rec.Code)
	}
}

func TestRelayLimitsWhatIsInFlight(t *testing.T) {
	r := New("http://127.0.0.1:1/", func(*http.Request) string { return "" }, nil)
	for range inFlight {
		r.slots <- struct{}{}
	}
	rec := httptest.NewRecorder()
	r.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, Path, strings.NewReader("{}")))
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("past the limit: %d", rec.Code)
	}
}
