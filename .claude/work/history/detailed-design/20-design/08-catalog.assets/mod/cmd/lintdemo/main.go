// Command lintdemo prints Lint findings for a catalog YAML against a repository list in the GitHub listing shape (throwaway).
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"

	"go.yaml.in/yaml/v3"

	"github.com/cantara/source-code-portal/internal/catalog"
	"github.com/cantara/source-code-portal/internal/scm"
)

func main() {
	y, _ := os.ReadFile(os.Args[1])
	b, _ := os.ReadFile(os.Args[2])
	var infos []struct {
		Name           string
		Default_branch string
		Archived, Fork bool
		Topics         []string
	}
	json.Unmarshal(b, &infos)
	var repos []scm.Repository
	for i, r := range infos {
		repos = append(repos, scm.Repository{Ref: scm.RepoRef{Source: "github", ID: scm.RepoID(i + 1), Owner: "acme", Name: r.Name}, Topics: r.Topics, Archived: r.Archived, Fork: r.Fork, Visibility: scm.VisPublic})
	}
	d := struct {
		Catalog catalog.Config `yaml:"catalog"`
	}{catalog.DefaultConfig()}
	dec := yaml.NewDecoder(bytes.NewReader(y))
	dec.KnownFields(true)
	if err := dec.Decode(&d); err != nil {
		fmt.Println("DECODE ERROR:", err)
		os.Exit(1)
	}
	fs := catalog.Lint(d.Catalog, catalog.ValidateContext{Sources: []scm.SourceID{"github"}}, nil, repos)
	for _, f := range fs {
		fmt.Println(f)
	}
	fmt.Printf("%d findings, errors=%v\n", len(fs), catalog.HasErrors(fs))
}
