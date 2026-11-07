package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"sync"
)

type launchState struct {
	Launch
	digest [sha256.Size]byte
	mu     sync.Mutex
	used   bool
}

// take reports whether code is the launch code and still unused and unexpired, and uses it up: the first valid request wins, a code
// tried wrongly is not consumed.
func (l *launchState) take(code string, now func() int64, expires int64) bool {
	got := sha256.Sum256([]byte(code))
	if subtle.ConstantTimeCompare(got[:], l.digest[:]) != 1 {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.used || now() >= expires {
		return false
	}
	l.used = true
	return true
}

// launchSignIn signs in the person who started the portal, once. It always lands on the dashboard: the link names no place to go.
func (a *Auth) launchSignIn(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	l := a.launch
	if !l.take(r.URL.Query().Get("code"), func() int64 { return a.o.Now().Unix() }, l.Expires.Unix()) {
		signInError(w, r, "launch")
		return
	}
	t, ok := a.named(l.Token)
	if !ok {
		signInError(w, r, "launch")
		return
	}
	if err := a.start(w, session{Subject: "token:" + t.Name, Name: t.Name, Token: t.Name}); err != nil {
		signInError(w, r, "launch")
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
