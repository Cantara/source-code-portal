package secret_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/cantara/source-code-portal/internal/platform/secret"
)

func TestNeverPrints(t *testing.T) {
	s := secret.New("hunter2-hunter2")
	type holder struct {
		Token secret.Secret
		Ptr   *secret.Secret
	}
	h := holder{Token: s, Ptr: &s}
	var buf bytes.Buffer
	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%X", "%d", "%t", "%p"} {
		fmt.Fprintf(&buf, verb+"|", s)
		fmt.Fprintf(&buf, verb+"|", h)
	}
	fmt.Fprint(&buf, s, h)
	fmt.Fprintln(&buf, s, h)
	j, _ := json.Marshal(h)
	buf.Write(j)
	y, _ := yaml.Marshal(h)
	buf.Write(y)
	lg := slog.New(slog.NewJSONHandler(&buf, nil))
	lg.Info("x", "token", s, slog.Any("h", h))
	if bytes.Contains(buf.Bytes(), []byte("hunter2")) {
		t.Fatalf("leak: %s", buf.String())
	}
}

func TestFileAndYAML(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "k")
	os.WriteFile(p, []byte("abc\r\n"), 0o600)
	s, err := secret.FromFile(p)
	if err != nil || s.Reveal() != "abc" {
		t.Fatalf("%v %q", err, s.Reveal())
	}
	os.WriteFile(p, []byte("\n"), 0o600)
	if _, err := secret.FromFile(p); err == nil {
		t.Fatal("empty file must fail")
	}
	var c struct {
		A secret.Secret `yaml:"a"`
	}
	if err := yaml.Unmarshal([]byte("a: {file: "+p+"}"), &c); err != nil || !c.A.Pending() {
		t.Fatalf("%v pending=%v", err, c.A.Pending())
	}
	if err := yaml.Unmarshal([]byte("a: plain"), &c); err == nil {
		t.Fatal("scalar must be refused")
	}
	if err := yaml.Unmarshal([]byte("a: {file: rel}"), &c); err == nil {
		t.Fatal("relative must be refused")
	}
}

func TestResolveRefusals(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, b []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, b, 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	if _, err := secret.FromFile(write("empty", []byte("\n"))); err == nil {
		t.Error("an empty file (only a newline) is unset, not a secret")
	}
	if _, err := secret.FromFile(write("big", bytes.Repeat([]byte("x"), secret.MaxFileSize+1))); err == nil {
		t.Error("over-size file accepted")
	}
	if _, err := secret.FromFile(dir); err == nil {
		t.Error("a directory is not a secret file")
	}
	if _, err := secret.FromFile("relative/path"); err == nil {
		t.Error("relative path accepted")
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(write("real", []byte("tok-1234567890\r\n")), link); err != nil {
		t.Fatal(err)
	}
	s, err := secret.FromFile(link) // symlink chains are how Kubernetes mounts secrets
	if err != nil || s.Reveal() != "tok-1234567890" {
		t.Fatalf("%v %q", err, s.Reveal())
	}
}
