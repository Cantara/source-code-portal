package manifests

import (
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestDefaultConfigValid(t *testing.T) {
	if err := DefaultConfig().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestConfigValidateCollectsAll(t *testing.T) {
	c := DefaultConfig()
	c.Refresh.TTL = 0
	c.Acquisition.FetchConcurrency = 99
	c.Limits.LockfileBytes = 64 << 20
	c.Acquisition.DisabledEcosystems = []string{"NuGet"}
	err := c.Validate()
	if err == nil {
		t.Fatal("want errors")
	}
	for _, want := range []string{"manifests.refresh.ttl", "manifests.acquisition.fetch_concurrency", "manifests.limits.lockfile_bytes", "manifests.acquisition.disabled_ecosystems"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %s in %v", want, err)
		}
	}
}

func TestYAMLStrictDecodeAndByteSize(t *testing.T) {
	c := DefaultConfig()
	dec := yaml.NewDecoder(strings.NewReader("limits: { lockfile_bytes: 16MiB, manifest_bytes: 512KiB }\nacquisition: { max_files: 50 }\nrefresh: { ttl: 45m }\n"))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil {
		t.Fatal(err)
	}
	if c.Limits.LockfileBytes != 16<<20 || c.Limits.ManifestBytes != 512<<10 || c.Acquisition.MaxFiles != 50 || c.Refresh.TTL.Minutes() != 45 {
		t.Fatalf("%+v", c)
	}
	dec = yaml.NewDecoder(strings.NewReader("limits: { lockfile_byts: 1 }\n"))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err == nil {
		t.Fatal("unknown key must fail")
	}
}
