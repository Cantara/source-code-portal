package ghverify

import (
	"net/http"
	"os"
	"testing"

	"github.com/cantara/source-code-portal/internal/scm"
)

func TestOfficialVector(t *testing.T) {
	h := "sha256=757107ea0eb2509fc211221cce984b8a37570b6d7586c22c46f4379c8b043e17"
	secret := []byte("It's a Secret to Everybody")
	if err := VerifySignature(h, []byte("Hello, World!"), secret); err != nil {
		t.Fatal(err)
	}
	if Sign(secret, []byte("Hello, World!")) != h {
		t.Fatal("Sign mismatch")
	}
}

func TestSignatureRejections(t *testing.T) {
	secret := []byte("s")
	body := []byte("{}")
	good := Sign(secret, body)
	for name, c := range map[string]struct {
		hdr     string
		body    []byte
		secrets [][]byte
	}{
		"empty header":      {"", body, [][]byte{secret}},
		"sha1 header value": {"sha1=" + good[7:47], body, [][]byte{secret}},
		"no prefix":         {good[7:], body, [][]byte{secret}},
		"short":             {good[:20], body, [][]byte{secret}},
		"non hex":           {"sha256=" + string(make([]byte, 64)), body, [][]byte{secret}},
		"wrong body":        {good, []byte("{ }"), [][]byte{secret}},
		"wrong secret":      {good, body, [][]byte{[]byte("t")}},
		"empty secret":      {Sign(nil, body), body, [][]byte{nil}},
		"no secrets":        {good, body, nil},
		"uppercase prefix":  {"SHA256=" + good[7:], body, [][]byte{secret}},
	} {
		err := VerifySignature(c.hdr, c.body, c.secrets...)
		var e *Error
		if err == nil || !asErr(err, &e) || !e.Unauthenticated() {
			t.Errorf("%s: err=%v", name, err)
		}
	}
	// rotation: previous secret still accepted
	if err := VerifySignature(Sign([]byte("old"), body), body, []byte("new"), []byte("old")); err != nil {
		t.Fatal(err)
	}
	// upper-case hex digits are accepted (hex.DecodeString is case-insensitive)
	if err := VerifySignature("sha256="+upper(good[7:]), body, secret); err != nil {
		t.Fatal(err)
	}
}

func upper(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'f' {
			b[i] = c - 32
		}
	}
	return string(b)
}

func asErr(err error, t **Error) bool {
	for err != nil {
		if e, ok := err.(*Error); ok {
			*t = e
			return true
		}
		type u interface{ Unwrap() []error }
		if j, ok := err.(u); ok {
			for _, x := range j.Unwrap() {
				if asErr(x, t) {
					return true
				}
			}
			return false
		}
		return false
	}
	return false
}

// Recorded push deliveries (organisation webhook, 2018) carry the per-commit arrays and tree_id.
func TestDecodeRecordedPush(t *testing.T) {
	body, err := os.ReadFile("../../testdata/push_two_commits.json")
	if err != nil {
		t.Fatal(err)
	}
	secret := []byte("x")
	h := http.Header{}
	h.Set("X-Hub-Signature-256", Sign(secret, body))
	h.Set("X-GitHub-Event", "push")
	h.Set("X-GitHub-Delivery", "d-1")
	ev, err := Verify("acme", [][]byte{secret}, h, body)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Kind != scm.EventPush || ev.Ref != "refs/heads/master" || ev.CommitCount != 2 || !ev.PathsComplete || ev.HeadTreeOID == "" {
		t.Fatalf("%+v", ev)
	}
	if len(ev.ChangedPaths) != 2 || ev.ChangedPaths[0] != "module1/internal/foo/foo.go" || ev.ChangedPaths[1] != "README.md" {
		t.Fatalf("paths %v", ev.ChangedPaths)
	}
	if ev.Repo.Owner != "acme" || ev.Repo.Name == "" {
		t.Fatalf("repo %+v", ev.Repo)
	}
}

func TestMissingArraysMeanIncompleteNotEmpty(t *testing.T) {
	body := []byte(`{"ref":"refs/heads/main","commits":[{"id":"a"}],"repository":{"id":1,"name":"r","owner":{"login":"o"}}}`)
	secret := []byte("x")
	h := http.Header{}
	h.Set("X-Hub-Signature-256", Sign(secret, body))
	h.Set("X-GitHub-Event", "push")
	ev, err := Verify("s", [][]byte{secret}, h, body)
	if err != nil || ev.PathsComplete {
		t.Fatalf("%+v %v", ev, err)
	}
}

func FuzzVerifyNoPanic(f *testing.F) {
	f.Add("sha256=00", []byte("x"))
	f.Fuzz(func(t *testing.T, hdr string, body []byte) {
		_ = VerifySignature(hdr, body, []byte("k"))
		h := http.Header{}
		h.Set("X-Hub-Signature-256", hdr)
		h.Set("X-GitHub-Event", "push")
		_, _ = Verify("s", [][]byte{[]byte("k")}, h, body)
	})
}

func TestMissingEventHeaderIsMalformedAfterAuth(t *testing.T) {
	body := []byte(`{}`)
	h := http.Header{}
	h.Set("X-Hub-Signature-256", Sign([]byte("k"), body))
	_, err := Verify("s", [][]byte{[]byte("k")}, h, body)
	var e *Error
	if err == nil || !asErr(err, &e) || !e.Malformed() {
		t.Fatalf("%v", err)
	}
}

func verifyFixture(t *testing.T, name, event string) (scm.Event, error) {
	t.Helper()
	body, err := os.ReadFile("../../testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	h := http.Header{}
	h.Set("X-Hub-Signature-256", Sign([]byte("x"), body))
	h.Set("X-GitHub-Event", event)
	h.Set("X-GitHub-Delivery", "d-"+event)
	return Verify("acme", [][]byte{[]byte("x")}, h, body)
}

// Recorded deliveries (organisation webhook, 2018) trimmed to the fields used, plus minimal
// documented shapes for the events without a recording.
func TestDecodeOtherEvents(t *testing.T) {
	cases := []struct {
		file, event string
		kind        scm.EventKind
		ref, action string
		wantRepo    bool
		install     scm.InstallationID
	}{
		{"create_tag.json", "create", scm.EventCreate, "refs/tags/v0.0.1", "", true, 0},
		{"release_published.json", "release", scm.EventRelease, "refs/tags/v0.0.1", "published", true, 0},
		{"repository_renamed.json", "repository", scm.EventRepository, "", "renamed", true, 0},
		{"installation_repositories_added.json", "installation_repositories", scm.EventInstallationRepos, "", "added", false, 4242},
		{"installation_created.json", "installation", scm.EventInstallation, "", "created", false, 4242},
		{"ping.json", "ping", scm.EventPing, "", "", false, 0},
	}
	for _, c := range cases {
		ev, err := verifyFixture(t, c.file, c.event)
		if err != nil {
			t.Fatalf("%s: %v", c.file, err)
		}
		if ev.Kind != c.kind || ev.Ref != c.ref || ev.Action != c.action || (ev.Repo.ID != 0) != c.wantRepo || ev.Installation != c.install || ev.Source != "acme" {
			t.Fatalf("%s: %+v", c.file, ev)
		}
		if c.wantRepo && (ev.Repo.Owner == "" || ev.Repo.Name == "" || ev.Repo.Source != "acme") {
			t.Fatalf("%s repo %+v", c.file, ev.Repo)
		}
	}
	if _, err := verifyFixture(t, "ping.json", "star"); err == nil {
		t.Fatal("star must be ignored")
	}
	if _, err := verifyFixture(t, "ping.json", "create"); err == nil {
		t.Fatal("a create without repository is malformed")
	}
}

// A forced push, or a head that moved without any listed commit, has unknown changed paths: the docs and
// manifests reactors must refresh, not conclude "nothing changed".
func TestForcedOrCommitlessPushHasIncompletePaths(t *testing.T) {
	for name, body := range map[string]string{
		"forced":     `{"ref":"refs/heads/main","forced":true,"commits":[{"id":"a","added":[],"removed":[],"modified":["x"]}],"head_commit":{"tree_id":"t"},"repository":{"id":1,"name":"r","owner":{"login":"o"}}}`,
		"no commits": `{"ref":"refs/heads/main","commits":[],"head_commit":{"tree_id":"t"},"repository":{"id":1,"name":"r","owner":{"login":"o"}}}`,
	} {
		h := http.Header{}
		h.Set("X-Hub-Signature-256", Sign([]byte("k"), []byte(body)))
		h.Set("X-GitHub-Event", "push")
		ev, err := Verify("s", [][]byte{[]byte("k")}, h, []byte(body))
		if err != nil || ev.PathsComplete {
			t.Fatalf("%s: %+v %v", name, ev, err)
		}
	}
	// a deleted ref has nothing to refresh and is complete
	body := `{"ref":"refs/heads/old","deleted":true,"commits":[],"head_commit":null,"repository":{"id":1,"name":"r","owner":{"login":"o"}}}`
	h := http.Header{}
	h.Set("X-Hub-Signature-256", Sign([]byte("k"), []byte(body)))
	h.Set("X-GitHub-Event", "push")
	if ev, err := Verify("s", [][]byte{[]byte("k")}, h, []byte(body)); err != nil || !ev.PathsComplete || !ev.Deleted {
		t.Fatalf("%+v %v", ev, err)
	}
}
