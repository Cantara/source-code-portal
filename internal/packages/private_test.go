package packages

import (
	"net/http"
	"net/http/httptest"
	"strings"
	gosync "sync"
	"testing"

	"github.com/cantara/source-code-portal/internal/store"
)

// peer is a loopback registry host that records the credentials every request carried.
type peer struct {
	*httptest.Server
	mu    gosync.Mutex
	creds []string // path and its Authorization and X-JFrog-Art-Api headers
}

func newPeer(t *testing.T, tls bool, h func(w http.ResponseWriter, r *http.Request)) *peer {
	t.Helper()
	s := &peer{}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.creds = append(s.creds, r.URL.Path+" "+r.Header.Get("Authorization")+r.Header.Get("X-JFrog-Art-Api"))
		s.mu.Unlock()
		h(w, r)
	})
	if tls {
		s.Server = httptest.NewTLSServer(handler)
	} else {
		s.Server = httptest.NewServer(handler)
	}
	t.Cleanup(s.Close)
	return s
}

func (s *peer) seen() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.creds...)
}

// loopback trusts every loopback test server's certificate, as one transport reaches several hosts.
func loopback() http.RoundTripper {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.TLSClientConfig.InsecureSkipVerify = true // loopback test servers with their own certificates
	return t
}

const releaseMetadata = `<metadata><versioning><release>5.0</release></versioning></metadata>`

func TestAPrivateRegistryIsSignedInToAndNoOtherHost(t *testing.T) {
	other := newPeer(t, true, func(w http.ResponseWriter, _ *http.Request) { http.NotFound(w, nil) })
	nexus := newPeer(t, true, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/elsewhere/org/acme/lib/maven-metadata.xml":
			http.NotFound(w, r)
		case r.URL.Path == "/repository/moved/org/acme/lib/maven-metadata.xml":
			http.Redirect(w, r, other.URL+"/stolen", http.StatusFound)
		case r.Header.Get("Authorization") != "Basic Y2k6czNjcmV0": // ci:s3cret
			w.WriteHeader(http.StatusUnauthorized)
		case r.URL.Path == "/repository/releases/org/acme/lib/maven-metadata.xml":
			_, _ = w.Write([]byte(releaseMetadata))
		default:
			http.NotFound(w, r)
		}
	})
	files := repo{"pom.xml": `<project><groupId>org.acme</groupId><artifactId>lib</artifactId>
		<distributionManagement><repository><url>` + other.URL + `/releases/</url></repository></distributionManagement>
		<repositories><repository><url>` + nexus.URL + `/elsewhere/</url></repository></repositories></project>`}
	regs := []Registry{
		{ID: "moved", Ecosystem: "maven", URL: nexus.URL + "/repository/moved/", Credential: &Credential{Header: "X-JFrog-Art-Api", Secret: []byte("art-key")}},
		{ID: "nexus", Ecosystem: "maven", URL: nexus.URL + "/repository/releases/", Credential: &Credential{User: "ci", Secret: []byte("s3cret")}},
	}
	p := New(map[string]GitHub{"gh": files}, Options{Providers: []Provider{Maven{Central: other.URL + "/maven2/"}},
		Client: NewClient(loopback(), loopback(), regs), Registries: regs, Public: true, Repo: public})
	_, got := run(t, p, store.Item{})
	if v := got.Packages[0].Latest; v == nil || v.Version != "5.0" || v.Registry != "nexus" {
		t.Fatalf("latest = %+v, problem %q", v, got.Packages[0].Problem)
	}
	for _, s := range other.seen() {
		if strings.Contains(s, "Basic") || strings.Contains(s, "art-key") {
			t.Errorf("another host received a credential: %s", s)
		}
	}
	saw := false
	for _, s := range nexus.seen() {
		path, cred, _ := strings.Cut(s, " ")
		switch {
		case strings.HasPrefix(path, "/elsewhere/") && cred != "":
			t.Errorf("the registry's host outside its root received a credential: %s", s)
		case strings.HasPrefix(path, "/repository/moved/"):
			saw = saw || cred == "art-key"
		}
	}
	if !saw {
		t.Error("the moved registry was not asked with its header")
	}
}

func TestOnlyAPrivateNetworkRegistryIsReadOverHTTP(t *testing.T) {
	lan := newPeer(t, false, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/org/acme/lib/maven-metadata.xml" {
			_, _ = w.Write([]byte(releaseMetadata))
			return
		}
		http.NotFound(w, r)
	})
	files := repo{"pom.xml": `<project><groupId>org.acme</groupId><artifactId>lib</artifactId></project>`}
	for _, private := range []bool{true, false} {
		regs := []Registry{{ID: "lan", Ecosystem: "maven", URL: lan.URL + "/", Private: private}}
		p := New(map[string]GitHub{"gh": files}, Options{Providers: []Provider{Maven{}}, Client: NewClient(nil, http.DefaultTransport, regs),
			Registries: regs, Public: false, Repo: public})
		_, got := run(t, p, store.Item{})
		if v := got.Packages[0].Latest; (v != nil) != private {
			t.Errorf("private %v: latest %+v, problem %q", private, v, got.Packages[0].Problem)
		}
	}
}
