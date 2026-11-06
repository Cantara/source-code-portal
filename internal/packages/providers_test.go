package packages

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/snapshot"
	"github.com/cantara/source-code-portal/internal/store"
)

// tagged is a fake repository with tags.
type tagged struct {
	repo
	tags []string
}

func (t tagged) Tags(context.Context, string, string) ([]string, error) { return t.tags, nil }

func private(snapshot.RepoKey) (snapshot.Repo, bool) {
	return snapshot.Repo{Visibility: "private", HTMLURL: "https://github.com/acme/x"}, true
}

func one(t *testing.T, gh GitHub, pr Provider, reg *registry, repo func(snapshot.RepoKey) (snapshot.Repo, bool)) Package {
	t.Helper()
	p := New(map[string]GitHub{"gh": gh}, Options{Providers: []Provider{pr}, Client: NewClient(reg.Client().Transport, nil, nil), Public: true, Repo: repo})
	_, got := run(t, p, store.Item{})
	if len(got.Packages) != 1 {
		t.Fatalf("packages = %+v", got.Packages)
	}
	return got.Packages[0]
}

func TestNewestStable(t *testing.T) {
	for want, vs := range map[string][]string{
		"1.10.0":     {"1.9.0", "1.10.0", "1.2"},
		"2.0":        {"2.0", "2.1.0rc1", "2.1.0.dev3", "2.1.0-beta.2", "2.1.0a1"},
		"1.0.post2":  {"1.0", "1.0.post2", "1.0.post1"},
		"v3.2.1":     {"v3.2.1", "v3.2.1-rc.1", "v3.10.0-alpha"},
		"3.0.21":     {"3.0.21", "3.0.22-SNAPSHOT", "3.0.20"},
		"1.2.3+meta": {"1.2.3+meta", "1.2.2"},
		"":           {"1.0.0-alpha", "latest", ""},
	} {
		if got := newestStable(vs); got != want {
			t.Errorf("newestStable(%v) = %q, want %q", vs, got, want)
		}
	}
}

func TestPyPI(t *testing.T) {
	reg := newRegistry(t)
	reg.set("/simple/acme-tools/", `{"meta":{"api-version":"1.1"},"name":"acme-tools","versions":["1.0","1.1","2.0b1","1.2"],
		"files":[{"filename":"acme_tools-1.1-py3-none-any.whl","upload-time":"2025-01-02T00:00:00Z","yanked":false},
		{"filename":"acme_tools-1.2.tar.gz","upload-time":"2025-03-04T10:00:00.123456Z","yanked":"broken build"},
		{"filename":"acme_tools-1.2-py3-none-any.whl","upload-time":"2025-03-04T10:00:00Z","yanked":true},
		{"filename":"acme_tools-2.0b1.tar.gz","upload-time":"2025-06-01T00:00:00Z"}]}`)
	files := repo{"pyproject.toml": "[project]\nname = \"Acme_Tools\"\nversion = \"1.2\"\n",
		"tools/setup.cfg":    "[metadata]\nname = acme-private\nclassifiers =\n  Private :: Do Not Upload\n",
		"web/pyproject.toml": "[tool.poetry]\nname = \"site\"\npackage-mode = false\n"}
	pk := one(t, files, PyPI{Simple: reg.URL + "/simple/"}, reg, public)
	v := pk.Latest
	if pk.Name != "Acme_Tools" || v == nil || v.Version != "1.1" || v.Published != time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC) {
		t.Errorf("package = %+v, latest %+v (1.2 is yanked, 2.0b1 a pre-release)", pk, v)
	}
}

func TestPyPISetupCfg(t *testing.T) {
	reg := newRegistry(t)
	reg.set("/simple/legacy/", `{"files":[{"filename":"legacy-0.9.tar.gz","upload-time":"2020-01-01T00:00:00Z"},
		{"filename":"legacy-0.10.zip","upload-time":"2021-01-01T00:00:00Z"}]}`)
	pk := one(t, repo{"setup.cfg": "[metadata]\nname = legacy\n"}, PyPI{Simple: reg.URL + "/simple/"}, reg, public)
	if pk.Latest == nil || pk.Latest.Version != "0.10" {
		t.Errorf("latest = %+v (an index without PEP 700: the versions of its files)", pk.Latest)
	}
}

func TestCargo(t *testing.T) {
	reg := newRegistry(t)
	reg.set("/api/v1/crates/acme-core", `{"crate":{"id":"acme-core","max_version":"0.5.0-rc.1","max_stable_version":"0.4.2"},
		"versions":[{"num":"0.5.0-rc.1","created_at":"2025-09-01T00:00:00.000000+00:00"},
		{"num":"0.4.2","created_at":"2025-07-01T12:00:00.000000+00:00","yanked":false}]}`)
	files := repo{"Cargo.toml": "[workspace]\nmembers = [\"core\", \"cli\"]\n",
		"core/Cargo.toml": "[package]\nname = \"acme-core\"\nversion = \"0.4.2\"\n",
		"cli/Cargo.toml":  "[package]\nname = \"acme-cli\"\npublish = false\n"}
	pk := one(t, files, Cargo{API: reg.URL + "/"}, reg, public)
	if v := pk.Latest; pk.Name != "acme-core" || v == nil || v.Version != "0.4.2" || v.Published != time.Date(2025, 7, 1, 12, 0, 0, 0, time.UTC) ||
		v.URL != "https://crates.io/crates/acme-core/0.4.2" {
		t.Errorf("package = %+v, latest %+v", pk, v)
	}
}

func TestCargoSparseIndex(t *testing.T) {
	reg := newRegistry(t)
	reg.set("/index/ac/me/acme-core", "{\"name\":\"acme-core\",\"vers\":\"1.0.0\"}\n{\"name\":\"acme-core\",\"vers\":\"1.1.0\",\"yanked\":true}\n"+
		"{\"name\":\"acme-core\",\"vers\":\"1.0.1\"}\n")
	files := repo{"Cargo.toml": "[package]\nname = \"acme-core\"\npublish = [\"acme\"]\n"}
	p := New(map[string]GitHub{"gh": files}, Options{Providers: []Provider{Cargo{API: reg.URL + "/"}}, Client: NewClient(reg.Client().Transport, nil, nil),
		Registries: []Registry{{ID: "acme", Ecosystem: "cargo", URL: reg.URL + "/index/"}}, Public: true, Repo: public})
	_, got := run(t, p, store.Item{})
	if v := got.Packages[0].Latest; v == nil || v.Version != "1.0.1" || v.Registry != "acme" {
		t.Errorf("latest = %+v", v)
	}
	for _, asked := range reg.ledger() {
		if strings.HasPrefix(asked, "/api/") {
			t.Errorf("crates.io was asked for a crate that publishes only to acme: %s", asked)
		}
	}
	if got := indexPath("A"); got != "1/a" {
		t.Errorf("indexPath(A) = %q", got)
	}
	if got := indexPath("abc"); got != "3/a/abc" {
		t.Errorf("indexPath(abc) = %q", got)
	}
}

func TestGo(t *testing.T) {
	reg := newRegistry(t)
	reg.set("/proxy/github.com/!acme/tool/@latest", `{"Version":"v1.4.0","Time":"2025-08-08T08:00:00Z"}`)
	reg.set("/proxy/github.com/acme/untagged/@latest", `{"Version":"v0.0.0-20250101000000-abcdefabcdef","Time":"2025-01-01T00:00:00Z"}`)
	proxy := Go{Proxy: reg.URL + "/proxy/"}

	pk := one(t, repo{"go.mod": "module github.com/Acme/tool\n\ngo 1.27\n"}, proxy, reg, public)
	if v := pk.Latest; v == nil || v.Version != "v1.4.0" || v.Published != time.Date(2025, 8, 8, 8, 0, 0, 0, time.UTC) {
		t.Errorf("proxied = %+v", v)
	}
	// A pseudo-version is no release: the repository's tags say the newest.
	pk = one(t, tagged{repo{"go.mod": "module github.com/acme/untagged\n"}, []string{"v0.2.0", "v0.10.0", "v1.0.0-rc.1"}}, proxy, reg, public)
	if v := pk.Latest; v == nil || v.Version != "v0.10.0" || v.Registry != "tags" || v.URL != "https://github.com/acme/x/tree/v0.10.0" {
		t.Errorf("untagged = %+v", v)
	}
	// A private module is never named to the public proxy; a module in a directory with a major version takes its own tags.
	asked := len(reg.ledger())
	pk = one(t, tagged{repo{"sdk/go.mod": "module example.org/secret/sdk/v2 // the SDK\n"},
		[]string{"v9.0.0", "sdk/v1.9.0", "sdk/v2.3.1", "sdk/v2.10.0", "sdk/v3.0.0"}}, proxy, reg, private)
	if v := pk.Latest; v == nil || v.Version != "v2.10.0" || len(reg.ledger()) != asked {
		t.Errorf("private = %+v, asked %v", v, reg.ledger()[asked:])
	}
}

func TestSwiftAndCocoaPods(t *testing.T) {
	reg := newRegistry(t)
	reg.set("/api/v1/pods/AcmeKit", `{"name":"AcmeKit","versions":[{"name":"2.1.0","created_at":"2025-04-04 10:00:00 UTC"},
		{"name":"2.2.0-beta","created_at":"2025-05-05 10:00:00 UTC"}]}`)
	files := tagged{repo{"Package.swift": "// swift-tools-version:5.9\nlet package = Package(\n    name: \"AcmeKit\",\n    products: []\n)\n",
		"AcmeKit.podspec": "Pod::Spec.new do |s|\n  s.name = 'AcmeKit'\n  s.version = '2.1.0'\nend\n"}, []string{"2.0.0", "2.1.0", "2.2.0-beta"}}
	p := New(map[string]GitHub{"gh": files}, Options{Providers: []Provider{Swift{}, CocoaPods{Trunk: reg.URL + "/"}},
		Client: NewClient(reg.Client().Transport, nil, nil), Public: true, Repo: public})
	_, got := run(t, p, store.Item{})
	byEco := map[string]*Version{}
	for _, pk := range got.Packages {
		byEco[pk.Ecosystem] = pk.Latest
	}
	if v := byEco["swift"]; v == nil || v.Version != "2.1.0" {
		t.Errorf("swift = %+v", v)
	}
	if v := byEco["cocoapods"]; v == nil || v.Version != "2.1.0" || v.Published != time.Date(2025, 4, 4, 10, 0, 0, 0, time.UTC) {
		t.Errorf("cocoapods = %+v", v)
	}
}

func TestGradle(t *testing.T) {
	reg := newRegistry(t)
	reg.set("/releases/org/acme/kotlin-kit/maven-metadata.xml", `<metadata><versioning><release>0.9.1</release></versioning></metadata>`)
	files := repo{"settings.gradle.kts": "rootProject.name = \"kotlin-kit\"\n", "gradle.properties": "group=org.acme\nversion=0.9.2-SNAPSHOT\n",
		"build.gradle.kts": "plugins { `maven-publish` }\npublishing {\n  repositories {\n    maven { url = uri(\"" + reg.URL + "/snapshots/\") }\n" +
			"    maven { url = uri(\"" + reg.URL + "/releases/\") }\n  }\n}\n"}
	pk := one(t, files, Maven{Central: reg.URL + "/maven2/"}, reg, public)
	if v := pk.Latest; pk.Name != "org.acme:kotlin-kit" || v == nil || v.Version != "0.9.1" || !slices.Equal(pk.Publish, []string{reg.URL + "/snapshots/", reg.URL + "/releases/"}) {
		t.Errorf("package = %+v, latest %+v", pk, v)
	}
	// A Gradle build that does not publish is no package.
	p := New(map[string]GitHub{"gh": repo{"build.gradle": "plugins { id 'application' }\ngroup = 'org.acme'\n", "settings.gradle": "rootProject.name = 'app'\n"}},
		Options{Providers: []Provider{Maven{Central: reg.URL + "/maven2/"}}, Client: NewClient(reg.Client().Transport, nil, nil), Public: true, Repo: public})
	if _, got := run(t, p, store.Item{}); len(got.Packages) != 0 {
		t.Errorf("an application build is a package: %+v", got.Packages)
	}
}

// The hosts a build file names are not remembered without bound.
func TestIdleHostsAreForgotten(t *testing.T) {
	c := NewClient(nil, nil, nil)
	for i := range 3 * hostsMax {
		c.host(fmt.Sprintf("registry-%d.example", i))
	}
	if n := len(c.hosts); n > hostsMax {
		t.Errorf("%d hosts kept, want at most %d", n, hostsMax)
	}
}
