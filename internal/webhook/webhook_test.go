package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/sync"
)

type call struct {
	ev          sync.Event
	owner, name string
}

func sign(secret, body string) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(body))
	return "sha256=" + hex.EncodeToString(m.Sum(nil))
}

func deliver(t *testing.T, h http.Handler, event, delivery, sig, body string, mut func(*http.Request)) (int, string) {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/webhooks/github", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-GitHub-Event", event)
	r.Header.Set("X-GitHub-Delivery", delivery)
	r.Header.Set("X-Hub-Signature-256", sig)
	if mut != nil {
		mut(r)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var got struct{ Status string }
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	return w.Code, got.Status
}

const pushMain = `{"ref":"refs/heads/main","repository":{"name":"core","default_branch":"main","owner":{"login":"Acme"}}}`

func TestReceiver(t *testing.T) {
	var calls []call
	rc := New([]byte("new"), []byte("old"), func(ev sync.Event, owner, name string) { calls = append(calls, call{ev, owner, name}) },
		slog.New(slog.DiscardHandler))
	n := 0
	id := func() string { n++; return "d-" + strings.Repeat("x", n%3) + string(rune('a'+n)) }
	for _, tc := range []struct {
		name, event, body, sig string
		mut                    func(*http.Request)
		code                   int
		status                 string
	}{
		{"push", "push", pushMain, sign("new", pushMain), nil, 202, "accepted"},
		{"previous secret", "push", pushMain, sign("old", pushMain), nil, 202, "accepted"},
		{"wrong secret", "push", pushMain, sign("other", pushMain), nil, 401, ""},
		{"malformed signature", "push", pushMain, "sha1=abc", nil, 401, ""},
		{"content type", "push", pushMain, sign("new", pushMain), func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }, 415, ""},
		{"form encoded", "push", pushMain, sign("new", pushMain),
			func(r *http.Request) { r.Header.Set("Content-Type", "application/x-www-form-urlencoded") }, 415, ""},
		{"bad delivery id", "push", pushMain, sign("new", pushMain), func(r *http.Request) { r.Header.Set("X-GitHub-Delivery", "a b") }, 400, ""},
		{"no event", "", pushMain, sign("new", pushMain), nil, 400, ""},
		{"not json", "push", "{", sign("new", "{"), nil, 400, ""},
		{"ping", "ping", `{"zen":"x"}`, sign("new", `{"zen":"x"}`), nil, 202, "pong"},
		{"unknown event", "star", `{}`, sign("new", `{}`), nil, 202, "ignored"},
	} {
		code, status := deliver(t, rc, tc.event, id(), tc.sig, tc.body, tc.mut)
		if code != tc.code || status != tc.status {
			t.Errorf("%s: %d %q, want %d %q", tc.name, code, status, tc.code, tc.status)
		}
	}
	if len(calls) != 2 || calls[0] != (call{sync.EventPush, "Acme", "core"}) {
		t.Errorf("sink calls = %+v", calls)
	}
}

func TestDuplicateAndOversize(t *testing.T) {
	calls := 0
	rc := New([]byte("s"), nil, func(sync.Event, string, string) { calls++ }, slog.New(slog.DiscardHandler))
	var observed []string
	rc.Observe(func(event, result string) { observed = append(observed, event+":"+result) })
	defer func() {
		if got := strings.Join(observed, ","); got != "push:accepted,push:duplicate,:too_large,:too_large" {
			t.Errorf("observed %s", got)
		}
	}()
	for i, want := range []string{"accepted", "duplicate"} {
		if code, status := deliver(t, rc, "push", "same", sign("s", pushMain), pushMain, nil); code != 202 || status != want {
			t.Errorf("delivery %d: %d %q, want %s", i, code, status, want)
		}
	}
	if calls != 1 {
		t.Errorf("sink called %d times", calls)
	}
	big := `{"x":"` + strings.Repeat("a", MaxBody) + `"}`
	if code, _ := deliver(t, rc, "push", "big", sign("s", big), big, nil); code != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized: %d", code)
	}
	if code, _ := deliver(t, rc, "push", "big2", sign("s", big), big, func(r *http.Request) { r.ContentLength = -1 }); code != 413 {
		t.Errorf("oversized without a length: %d", code)
	}
}

func TestDedupeExpires(t *testing.T) {
	d := &dedupe{at: map[string]time.Time{}}
	now := time.Now()
	if !d.add("a", now) || d.add("a", now.Add(time.Minute)) || !d.add("a", now.Add(dedupeTTL)) {
		t.Error("a delivery id is a duplicate only within the TTL")
	}
	for i := range dedupeMax + 5 {
		d.add(string(rune(i+0x100)), now)
	}
	if len(d.at) > dedupeMax || len(d.order) != len(d.at) {
		t.Errorf("remembered %d ids (order %d)", len(d.at), len(d.order))
	}
}

func TestNormalise(t *testing.T) {
	repo := `"repository":{"name":"core","default_branch":"main","owner":{"login":"acme"}}`
	for _, tc := range []struct {
		event, body string
		want        Normalised
		ok          bool
	}{
		{"push", `{"ref":"refs/heads/main",` + repo + `}`, Normalised{sync.EventPush, "acme", "core"}, true},
		{"push", `{"ref":"refs/heads/main","deleted":true,` + repo + `}`, Normalised{}, false},
		{"push", `{"ref":"refs/heads/feature",` + repo + `}`, Normalised{}, false},
		{"push", `{"ref":"refs/tags/v1",` + repo + `}`, Normalised{sync.EventTag, "acme", "core"}, true},
		{"release", `{"action":"published",` + repo + `}`, Normalised{sync.EventTag, "acme", "core"}, true},
		{"repository", `{"action":"renamed",` + repo + `}`, Normalised{sync.EventSourceChanged, "acme", ""}, true},
		{"repository", `{"action":"privatized",` + repo + `}`, Normalised{sync.EventSourceChanged, "acme", ""}, true},
		{"repository", `{"action":"starred",` + repo + `}`, Normalised{}, false},
		{"installation", `{"action":"deleted","installation":{"account":{"login":"acme"}}}`, Normalised{sync.EventSourceChanged, "acme", ""}, true},
		{"installation_repositories", `{"action":"added","installation":{"account":{"login":"acme"}}}`,
			Normalised{sync.EventSourceChanged, "acme", ""}, true},
		{"push", `{"ref":"refs/heads/main","repository":{"name":"../x","default_branch":"main","owner":{"login":"acme"}}}`, Normalised{}, false},
		{"push", `{"ref":"refs/heads/","repository":{"name":"x","default_branch":"","owner":{"login":"acme"}}}`, Normalised{}, false},
		{"installation", `{}`, Normalised{}, false},
		// Any other delivery about a repository passes by its GitHub event name.
		{"check_suite", `{"action":"completed",` + repo + `}`, Normalised{sync.Event("check_suite"), "acme", "core"}, true},
		{"check_suite", `{"action":"completed"}`, Normalised{}, false},
		{"Bad-Name", `{` + repo + `}`, Normalised{}, false},
	} {
		got, ok := Normalise(tc.event, []byte(tc.body))
		if got != tc.want || ok != tc.ok {
			t.Errorf("%s %s = %+v %v, want %+v %v", tc.event, tc.body, got, ok, tc.want, tc.ok)
		}
	}
}

func FuzzNormalise(f *testing.F) {
	f.Add("push", []byte(pushMain))
	f.Add("repository", []byte(`{"action":"created","repository":{"name":"a","owner":{"login":"b"}}}`))
	f.Add("installation", []byte(`{"installation":{"account":{"login":"b"}}}`))
	f.Fuzz(func(t *testing.T, event string, body []byte) {
		n, ok := Normalise(event, body)
		if !ok {
			return
		}
		if !ownerPattern.MatchString(n.Owner) || n.Name != "" && !namePattern.MatchString(n.Name) || n.Name != "" && strings.Trim(n.Name, ".") == "" {
			t.Errorf("accepted %+v", n)
		}
		if n.Event == sync.EventSourceChanged && n.Name != "" {
			t.Errorf("owner-wide event with a name: %+v", n)
		}
	})
}

func TestSharedDedupe(t *testing.T) {
	calls := 0
	rc := New([]byte("s"), nil, func(sync.Event, string, string) { calls++ }, slog.New(slog.DiscardHandler))
	shared := map[string]bool{"elsewhere": true}
	down := false
	rc.Dedupe(func(_ context.Context, id string) (bool, error) {
		if down {
			return false, errors.New("cluster down")
		}
		first := !shared[id]
		shared[id] = true
		return first, nil
	})
	for _, tc := range []struct {
		id, want string
		down     bool
	}{{"elsewhere", "duplicate", false}, {"new", "accepted", false}, {"new", "duplicate", false}, {"local", "accepted", true},
		{"local", "duplicate", true}} {
		down = tc.down
		if _, status := deliver(t, rc, "push", tc.id, sign("s", pushMain), pushMain, nil); status != tc.want {
			t.Errorf("%s (cluster down %v): %q, want %q", tc.id, tc.down, status, tc.want)
		}
	}
	if calls != 2 {
		t.Errorf("sink called %d times, want 2", calls)
	}
}
