package manifests

import "testing"

// TestSanitizeKeepsForeignEcosystem pins RR-A3 (unit 12): a target of another registered ecosystem is normalised by that ecosystem's rules and keeps its
// ecosystem (Gradle emits maven targets, GitHub Actions emit docker targets); an empty ecosystem defaults to the parser's.
func TestSanitizeKeepsForeignEcosystem(t *testing.T) {
	reg := NewRegistry()
	spec, ok := reg.Spec(NPM)
	if !ok {
		t.Fatal("no npm spec")
	}
	p := Parsed{Dependencies: []Dependency{
		{Target: ComponentID{Ecosystem: Maven, Namespace: "org.Slf4j", Name: "Slf4j-API"}, Source: SourceRegistry},
		{Target: ComponentID{Name: "left-pad"}, Source: SourceRegistry},
	}}
	sanitizeParsed(&p, spec, DefaultConfig().Limits, 10)
	if len(p.Dependencies) != 2 {
		t.Fatalf("dependencies dropped: %+v", p.Dependencies)
	}
	if got := p.Dependencies[0].Target; got.Ecosystem != Maven || got.Namespace != "org.Slf4j" || got.Name != "Slf4j-API" {
		t.Errorf("maven target changed: %+v", got)
	}
	if got := p.Dependencies[1].Target; got.Ecosystem != NPM || got.Name != "left-pad" {
		t.Errorf("empty ecosystem did not default to the parser's: %+v", got)
	}
}
