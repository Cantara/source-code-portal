package packages

import (
	"strings"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/store"
)

func TestNuGet(t *testing.T) {
	reg := newRegistry(t)
	reg.set("/flat/acme.payments/index.json", `{"versions":["1.0.0","1.2.0","1.10.0","2.0.0-preview.1"]}`)
	reg.set("/flat/acme.cli/index.json", `{"versions":["0.3.0"]}`)
	files := repo{
		"src/Acme.Payments/Acme.Payments.csproj": `<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><TargetFramework>net10.0</TargetFramework>` +
			`<PackageId>Acme.Payments</PackageId></PropertyGroup></Project>`,
		"src/Acme.Cli/Acme.Cli.fsproj": `<Project><PropertyGroup><IsPackable>true</IsPackable></PropertyGroup></Project>`,
		"src/App/App.csproj":           `<Project><PropertyGroup><OutputType>Exe</OutputType></PropertyGroup></Project>`,
	}
	p := New(map[string]GitHub{"gh": files}, Options{Providers: []Provider{NuGet{Flat: reg.URL + "/flat/"}},
		Client: NewClient(reg.Client().Transport, nil, nil), Public: true, Repo: public})
	_, got := run(t, p, store.Item{})
	byName := map[string]*Version{}
	for _, pk := range got.Packages {
		byName[pk.Name] = pk.Latest
	}
	if len(byName) != 2 {
		t.Fatalf("packages = %+v (an application that does not pack is no package)", got.Packages)
	}
	if v := byName["Acme.Payments"]; v == nil || v.Version != "1.10.0" || v.URL != "https://www.nuget.org/packages/Acme.Payments/1.10.0" {
		t.Errorf("Acme.Payments = %+v", v)
	}
	if v := byName["Acme.Cli"]; v == nil || v.Version != "0.3.0" {
		t.Errorf("Acme.Cli (named by its project file) = %+v", v)
	}
}

func TestNuGetFeed(t *testing.T) {
	reg := newRegistry(t)
	reg.set("/feed/index.json", `{"version":"3.0.0","resources":[{"@id":"`+reg.URL+`/feed/v3/content","@type":"PackageBaseAddress/3.0.0"}]}`)
	reg.set("/feed/v3/content/acme.secret/index.json", `{"versions":["4.1.0"]}`)
	reg.set("/elsewhere/index.json", `{"resources":[{"@id":"`+reg.URL+`/other/","@type":"PackageBaseAddress/3.0.0"}]}`)
	files := repo{"Acme.Secret.nuspec": `<?xml version="1.0"?><package xmlns="http://schemas.microsoft.com/packaging/2013/05/nuspec.xsd">` +
		`<metadata><id>Acme.Secret</id><version>4.1.0</version></metadata></package>`}
	lookup := func(feed string) Package {
		p := New(map[string]GitHub{"gh": files}, Options{Providers: []Provider{NuGet{Flat: reg.URL + "/flat/"}},
			Client: NewClient(reg.Client().Transport, nil, nil), Registries: []Registry{{ID: "feed", Ecosystem: "nuget", URL: reg.URL + feed}},
			Public: true, Repo: private})
		_, got := run(t, p, store.Item{})
		if len(got.Packages) != 1 {
			t.Fatalf("packages = %+v", got.Packages)
		}
		return got.Packages[0]
	}
	if v := lookup("/feed/").Latest; v == nil || v.Version != "4.1.0" || v.Registry != "feed" {
		t.Errorf("latest = %+v", v)
	}
	// A feed whose packages lie outside its url is not read: its credentials would not go there.
	before := len(reg.ledger())
	if pk := lookup("/elsewhere/"); pk.Latest != nil || pk.Problem == "" {
		t.Errorf("a feed outside its url = %+v", pk)
	}
	for _, asked := range reg.ledger()[before:] {
		if strings.HasPrefix(asked, "/other/") || strings.HasPrefix(asked, "/flat/") {
			t.Errorf("asked %s (outside the feed, or the public registry for a private repository)", asked)
		}
	}
}

func TestRubyGems(t *testing.T) {
	reg := newRegistry(t)
	reg.set("/api/v1/versions/acme-tools.json", `[{"number":"2.0.0.pre1","prerelease":true,"created_at":"2025-09-01T00:00:00.000Z"},
		{"number":"1.10.0","platform":"java","prerelease":false,"created_at":"2025-06-01T08:00:00.000Z"},
		{"number":"1.10.0","platform":"ruby","prerelease":false,"created_at":"2025-06-01T08:00:00.000Z"},
		{"number":"1.9.3","prerelease":false,"created_at":"2025-01-01T00:00:00.000Z"}]`)
	files := repo{"acme-tools.gemspec": "Gem::Specification.new do |spec|\n  spec.name = \"acme-tools\"\n  spec.version = Acme::VERSION\nend\n",
		"vendor/other.gemspec": "spec.name = 'other'\n"}
	pk := one(t, files, RubyGems{Server: reg.URL + "/"}, reg, public)
	if v := pk.Latest; pk.Name != "acme-tools" || v == nil || v.Version != "1.10.0" || v.Published != time.Date(2025, 6, 1, 8, 0, 0, 0, time.UTC) {
		t.Errorf("package = %+v, latest %+v", pk, v)
	}
}

func TestRubyGemsCompactIndex(t *testing.T) {
	reg := newRegistry(t)
	reg.set("/gems/info/acme-secret", "---\n0.9.0 |checksum:aa\n1.0.0 rake:>= 0|checksum:bb\n1.0.0-java |checksum:cc\n1.0.1-x86_64-linux |checksum:ee\n1.1.0.rc1 |checksum:dd\n")
	files := repo{"acme-secret.gemspec": "Gem::Specification.new do |s|\n  s.name = 'acme-secret'\nend\n",
		"acme-unknown.gemspec": "Gem::Specification.new do |s|\n  s.name = 'acme-unknown'\nend\n"}
	p := New(map[string]GitHub{"gh": files}, Options{Providers: []Provider{RubyGems{Server: reg.URL + "/public/"}},
		Client: NewClient(reg.Client().Transport, nil, nil), Registries: []Registry{{ID: "gems", Ecosystem: "rubygems", URL: reg.URL + "/gems/"}},
		Public: true, Repo: private})
	_, got := run(t, p, store.Item{})
	byName := map[string]*Version{}
	for _, pk := range got.Packages {
		byName[pk.Name] = pk.Latest
	}
	if v := byName["acme-secret"]; v == nil || v.Version != "1.0.1" || v.Registry != "gems" {
		t.Errorf("latest = %+v (1.0.1 is built for one platform only, 1.1.0.rc1 a pre-release)", v)
	}
	for _, asked := range reg.ledger() {
		if strings.HasPrefix(asked, "/public/") {
			t.Errorf("a private repository's gem was named to the public server: %s", asked)
		}
	}
}

func TestPackagist(t *testing.T) {
	reg := newRegistry(t)
	reg.set("/p2/acme/http-client.json", `{"packages":{"acme/other":[{"version":"9.0.0"}],"acme/http-client":[
		{"name":"acme/http-client","version":"v2.1.0-RC1","time":"2025-10-01T00:00:00+00:00"},
		{"version":"v2.0.3","time":"2025-08-02T09:30:00+00:00"},
		{"version":"v1.9.0","time":"2024-01-01T00:00:00+00:00"}]},"minified":"composer/2.0"}`)
	files := repo{"composer.json": `{"name":"acme/http-client","type":"library","require":{"php":">=8.3"}}`,
		"site/composer.json": `{"name":"acme/site","type":"project"}`}
	pk := one(t, files, Packagist{Repository: reg.URL + "/"}, reg, public)
	if v := pk.Latest; pk.Name != "acme/http-client" || v == nil || v.Version != "v2.0.3" || v.Published != time.Date(2025, 8, 2, 9, 30, 0, 0, time.UTC) {
		t.Errorf("package = %+v, latest %+v", pk, v)
	}
}
