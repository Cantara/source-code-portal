package maven_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/manifests"
	"github.com/cantara/source-code-portal/internal/manifests/ecosystems/maven"
	"github.com/cantara/source-code-portal/internal/manifests/parsertest"
)

// dirSource is the fake PomSource of the golden cases: files "<groupId>_<artifactId>_<version>.pom" in a directory.
type dirSource struct {
	dir   string
	calls atomic.Int32
	asked []string
}

func (d *dirSource) Fetch(_ context.Context, id manifests.ComponentID, version string) ([]byte, error) {
	group, artifact := id.Namespace, id.Name
	d.calls.Add(1)
	d.asked = append(d.asked, group+":"+artifact+":"+version)
	b, err := os.ReadFile(filepath.Join(d.dir, group+"_"+artifact+"_"+version+".pom"))
	if err != nil {
		return nil, errors.New("not found")
	}
	return b, nil
}

func newReg(t testing.TB, opts ...maven.Option) *manifests.Registry {
	reg := manifests.NewRegistry()
	if err := maven.Register(reg, opts...); err != nil {
		t.Fatal(err)
	}
	return reg
}

func TestGolden(t *testing.T) {
	dirs, _ := filepath.Glob("testdata/*")
	for _, d := range dirs {
		if st, err := os.Stat(filepath.Join(d, "input")); err != nil || !st.IsDir() {
			continue
		}
		t.Run(filepath.Base(d), func(t *testing.T) {
			var opts []maven.Option
			if st, err := os.Stat(filepath.Join(d, "pomsource")); err == nil && st.IsDir() {
				opts = append(opts, maven.WithPomSource(&dirSource{dir: filepath.Join(d, "pomsource")}, func(manifests.ComponentID) bool { return true }))
			}
			parsertest.TreeGolden(t, newReg(t, opts...), d)
		})
	}
}

func FuzzParsers(f *testing.F) {
	parsertest.FuzzTree(f, newReg(f), "testdata")
}

// TestPrivateGuard: the PomSource is never asked for a coordinate the guard names.
func TestPrivateGuard(t *testing.T) {
	src := &dirSource{dir: t.TempDir()}
	guard := manifests.NewPrivacyGuard(manifests.PomSourceConfig{PrivateGroupPrefixes: []string{"com.acme", "org.secret"}}, nil)
	reg := newReg(t, maven.WithPomSource(src, guard.Allow))
	tree := map[string][]byte{"pom.xml": []byte(`<project><modelVersion>4.0.0</modelVersion>
<parent><groupId>com.acme.platform</groupId><artifactId>acme-parent</artifactId><version>3</version><relativePath/></parent>
<artifactId>x</artifactId>
<dependencyManagement><dependencies>
 <dependency><groupId>org.secret.bom</groupId><artifactId>secret-bom</artifactId><version>1</version><type>pom</type><scope>import</scope></dependency>
 <dependency><groupId>org.public</groupId><artifactId>public-bom</artifactId><version>2</version><type>pom</type><scope>import</scope></dependency>
</dependencies></dependencyManagement></project>`)}
	res := manifests.RunTree(context.Background(), reg, parsertest.TreeConfig(), tree)
	for _, a := range src.asked {
		if strings.HasPrefix(a, "com.acme") || strings.HasPrefix(a, "org.secret") {
			t.Errorf("PomSource was asked for a private coordinate: %s", a)
		}
	}
	if len(src.asked) != 1 || !strings.HasPrefix(src.asked[0], "org.public") {
		t.Errorf("asked = %v, want only org.public", src.asked)
	}
	got := 0
	for _, d := range res.Units[0].Diagnostics {
		if d.Code == "maven.snapshot-private-skipped" {
			got++
		}
	}
	if got != 2 {
		t.Errorf("want 2 private-skipped diagnostics, got %d: %+v", got, res.Units[0].Diagnostics)
	}
}

// TestPomSourceCoordinatesAreSafe (verification VF-12-03): a coordinate that carries path or URL syntax is never handed to the PomSource, whatever the guard says.
func TestPomSourceCoordinatesAreSafe(t *testing.T) {
	bad := [][3]string{
		{"..", "a", "1"}, {"a..b", "a", "1"}, {"org.x.", "a", "1"}, {"org.x", "..", "1"}, {"org.x", "a", ".."}, {"org.x", "a?x=1", "1"},
		{"org.x", "a#f", "1"}, {"org.x", "a%2e%2e", "1"}, {"org.x", "a", "1%2f2"}, {"org.x", "a", "1 .0"}, {"org.x", "a", "1\\2"}, {"org.x", "a", "1?2"},
	}
	for _, c := range bad {
		src := &dirSource{dir: t.TempDir()}
		reg := newReg(t, maven.WithPomSource(src, func(manifests.ComponentID) bool { return true }))
		pom := `<project><modelVersion>4.0.0</modelVersion><parent><groupId>` + c[0] + `</groupId><artifactId>` + c[1] + `</artifactId><version>` + c[2] +
			`</version><relativePath/></parent><artifactId>x</artifactId></project>`
		manifests.RunTree(context.Background(), reg, parsertest.TreeConfig(), map[string][]byte{"pom.xml": []byte(pom)})
		if len(src.asked) != 0 {
			t.Errorf("PomSource was asked for %v: %v", c, src.asked)
		}
	}
	// a normal coordinate still goes out
	src := &dirSource{dir: t.TempDir()}
	reg := newReg(t, maven.WithPomSource(src, func(manifests.ComponentID) bool { return true }))
	manifests.RunTree(context.Background(), reg, parsertest.TreeConfig(), map[string][]byte{"pom.xml": []byte(`<project><modelVersion>4.0.0</modelVersion><parent><groupId>org.apache.maven</groupId><artifactId>maven-parent</artifactId><version>1.0.0.Final+b1</version><relativePath/></parent><artifactId>x</artifactId></project>`)})
	if len(src.asked) != 1 {
		t.Errorf("a valid coordinate was not asked: %v", src.asked)
	}
}

// TestPomSourceEnabledChangesTheRegistryHash (VF-12-05): the stored facts depend on whether a PomSource is wired, so Registry.SetHash must differ.
func TestPomSourceEnabledChangesTheRegistryHash(t *testing.T) {
	off, on := newReg(t), newReg(t, maven.WithPomSource(&dirSource{dir: t.TempDir()}, func(manifests.ComponentID) bool { return true }))
	off.Freeze()
	on.Freeze()
	if off.SetHash() == on.SetHash() {
		t.Fatal("enabling the PomSource must change the registry set hash")
	}
	nilAllow := newReg(t, maven.WithPomSource(&dirSource{dir: t.TempDir()}, nil))
	nilAllow.Freeze()
	if nilAllow.SetHash() != off.SetHash() {
		t.Fatal("a source without a guard is disabled and must hash like no source")
	}
}

type blockingSource struct{ started chan struct{} }

func (b *blockingSource) Fetch(ctx context.Context, _ manifests.ComponentID, _ string) ([]byte, error) {
	close(b.started)
	<-ctx.Done()
	return nil, ctx.Err()
}

// TestAssembleReturnsContextErrorAfterAFetchTimeout (VF-12-04): a PomSource call cut short by the assembly deadline must not leave a unit that says "parent not found".
func TestAssembleReturnsContextErrorAfterAFetchTimeout(t *testing.T) {
	src := &blockingSource{started: make(chan struct{})}
	a := maven.NewAssembler(maven.WithPomSource(src, func(manifests.ComponentID) bool { return true }))
	pom := manifests.Parsed{File: manifests.FileRef{Path: "pom.xml", ParserID: maven.PomID, ParserVersion: 1}}
	p, _ := maven.NewPom().Parse(context.Background(), manifests.File{Path: "pom.xml", Data: []byte(`<project><modelVersion>4.0.0</modelVersion><parent><groupId>org.x</groupId><artifactId>p</artifactId><version>1</version><relativePath/></parent><artifactId>x</artifactId></project>`)}, manifests.Limits{})
	p.File = pom.File
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	units, err := a.Assemble(ctx, manifests.PathSet{"pom.xml"}, []manifests.Parsed{p})
	if err == nil || units != nil {
		t.Fatalf("Assemble = %d units, %v; want the context error and no units", len(units), err)
	}
}
