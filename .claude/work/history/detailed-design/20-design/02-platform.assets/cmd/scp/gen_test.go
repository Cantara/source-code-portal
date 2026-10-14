package main

import (
	"fmt"
	"os"
	"testing"

	"github.com/cantara/source-code-portal/internal/platform/config"
)

func TestGenArtifacts(t *testing.T) {
	dir := os.Getenv("GEN_DIR")
	if dir == "" {
		t.Skip()
	}
	ref, err := config.Reference(&Config{}, defaultYAML)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(dir+"/reference.md", []byte(ref), 0o644)
	sch, err := config.Schema(&Config{}, defaultYAML)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(dir+"/schema.json", sch, 0o644)
	root, _ := config.Describe(&Config{})
	var s string
	for _, n := range config.EnvNames(root) {
		s += n + "\n"
	}
	os.WriteFile(dir+"/env.txt", []byte(s), 0o644)
	fmt.Println(len(ref), len(sch))
}
