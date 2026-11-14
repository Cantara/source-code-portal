package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// TestAnalyticsEventsAreRelayed: with web.plausible_domain the tracker's events, posted to the portal's own address, go to the
// endpoint with the visitor's address and browser and nothing else, also when sign-in is required (the sign-in page is tracked
// too); without it the address is not served and nothing is sent.
func TestAnalyticsEventsAreRelayed(t *testing.T) {
	var mu sync.Mutex
	var seen []http.Header
	var bodies []string
	plausible := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		seen, bodies = append(seen, r.Header.Clone()), append(bodies, string(b))
		mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
	}))
	defer plausible.Close()

	get, _ := privatePortalAt(t, "access: {private_groups: [staff], require_auth: true}\nauth: {tokens: [{name: bot, secret_file: $DIR/bot, groups: [staff]}]}\n"+
		"web: {plausible_domain: scp.example.org, plausible_endpoint: \""+plausible.URL+"/api/event\"}\n")
	event := `{"n":"pageview","u":"https://scp.example.org/","d":"scp.example.org"}`
	code, _, h := get("/_v/m", ":post", event, "User-Agent", "Mozilla/5.0 test", "X-Forwarded-For", "198.51.100.7", "Cookie", "scp_session=secret")
	if code != http.StatusAccepted || h.Get("Cache-Control") != "no-store" {
		t.Fatalf("an anonymous event with sign-in required: %d %v", code, h)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 1 || bodies[0] != event || seen[0].Get("X-Forwarded-For") != "198.51.100.7" || seen[0].Get("User-Agent") != "Mozilla/5.0 test" {
		t.Fatalf("the endpoint got %q with %v", bodies, seen)
	}
	if seen[0].Get("Cookie") != "" || seen[0].Get("Authorization") != "" {
		t.Errorf("credentials went to the endpoint: %v", seen[0])
	}
}

func TestNoAnalyticsIsNoRelay(t *testing.T) {
	get, _ := publicTwin(t, "")
	if code, _, _ := get("/_v/m", ":post", `{"n":"pageview"}`); code != http.StatusNotFound && code != http.StatusServiceUnavailable && code != http.StatusMethodNotAllowed {
		t.Errorf("POST /_v/m without a site = %d", code)
	}
}
