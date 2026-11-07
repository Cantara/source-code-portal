package auth

import (
	"net/http"
	"net/http/cookiejar"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func launchOptions(now func() time.Time) Options {
	return Options{SessionKey: key(), Now: now,
		Tokens: []Token{{Name: "owner", Secret: []byte(strings.Repeat("o", 40)), Groups: []string{"owner"}, Admin: true}},
		Launch: &Launch{Code: strings.Repeat("c", 43), Token: "owner", Expires: time.Date(2026, 11, 7, 12, 10, 0, 0, time.UTC)}}
}

func TestLaunch(t *testing.T) {
	clock := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	now := func() time.Time { return clock }
	code := strings.Repeat("c", 43)

	t.Run("signs in once, as the token, and lands on the dashboard", func(t *testing.T) {
		p := newPortal(t, launchOptions(now))
		if _, body := p.get(t, "/"); body != "/    " {
			t.Fatalf("before: %q", body)
		}
		if at, body := p.get(t, "/auth/launch?code="+code); at != "/" || body != "/ token owner  owner" {
			t.Errorf("launch: %q %q", at, body)
		}
		if _, body := p.get(t, "/"); body != "/ token owner  owner" {
			t.Errorf("the session: %q", body)
		}
		// The link is used up: another browser gets nothing.
		other := newPortal(t, launchOptions(now))
		_, _ = other.get(t, "/auth/launch?code="+code)
		p2 := &portal{Server: other.Server, auth: other.auth}
		p2.jar, _ = cookiejar.New(nil)
		if at, body := p2.get(t, "/auth/launch?code="+code); at != "/signin?error=launch" || strings.Contains(body, "owner") {
			t.Errorf("second use: %q %q", at, body)
		}
	})
	t.Run("a wrong code signs nobody in and does not use the link up", func(t *testing.T) {
		p := newPortal(t, launchOptions(now))
		for _, bad := range []string{"", "x", strings.Repeat("c", 42), code + "c"} {
			if at, _ := p.get(t, "/auth/launch?code="+bad); at != "/signin?error=launch" {
				t.Errorf("code %q: %q", bad, at)
			}
		}
		if _, body := p.get(t, "/"); body != "/    " {
			t.Errorf("signed in by a wrong code: %q", body)
		}
		if at, _ := p.get(t, "/auth/launch?code="+code); at != "/" {
			t.Errorf("the right code after wrong ones: %q", at)
		}
	})
	t.Run("an old link is refused", func(t *testing.T) {
		late := func() time.Time { return clock.Add(11 * time.Minute) }
		p := newPortal(t, launchOptions(late))
		if at, _ := p.get(t, "/auth/launch?code="+code); at != "/signin?error=launch" {
			t.Errorf("expired: %q", at)
		}
	})
	t.Run("one of many wins", func(t *testing.T) {
		p := newPortal(t, launchOptions(now))
		var won atomic.Int32
		var wg sync.WaitGroup
		for range 20 {
			wg.Go(func() {
				c := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
				res, err := c.Get(p.URL + "/auth/launch?code=" + code)
				if err != nil {
					return
				}
				_ = res.Body.Close()
				if res.Header.Get("Location") == "/" {
					won.Add(1)
				}
			})
		}
		wg.Wait()
		if won.Load() != 1 {
			t.Errorf("%d requests signed in", won.Load())
		}
	})
	t.Run("sets no-store and a cookie that is HttpOnly", func(t *testing.T) {
		p := newPortal(t, launchOptions(now))
		c := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		res, err := c.Get(p.URL + "/auth/launch?code=" + code)
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
		ck := res.Cookies()
		if res.Header.Get("Cache-Control") != "no-store" || len(ck) != 1 || !ck[0].HttpOnly {
			t.Errorf("%v %v", res.Header, ck)
		}
	})
	t.Run("needs a code and a configured token", func(t *testing.T) {
		o := launchOptions(now)
		o.Launch.Token = "nobody"
		if _, err := New(o); err == nil {
			t.Error("a launch link for an unknown token")
		}
		o = launchOptions(now)
		o.Launch.Code = ""
		if _, err := New(o); err == nil {
			t.Error("a launch link without a code")
		}
	})
	t.Run("no link without the option", func(t *testing.T) {
		o := launchOptions(now)
		o.Launch = nil
		p := newPortal(t, o)
		if at, _ := p.get(t, "/auth/launch?code="+code); strings.HasPrefix(at, "/signin") {
			t.Errorf("a route without the option: %q", at)
		}
	})
}
