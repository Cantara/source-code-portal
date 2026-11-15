package config

import (
	"encoding/json"
	"fmt"
	"os"

	"go.yaml.in/yaml/v4"
)

// Baseline is what the file sets of what admins also edit in the pages (design/config-baseline.md, CF1): who sees what and the
// pages' layouts, as JSON for the features' own parsers, and the file itself for the download that writes the admins' changes back
// into it. The announcements are Config.Announcements. The store holds the admins' changes as overrides of these.
type Baseline struct {
	File       []byte            // the configuration file as it was read; nil without one
	Visibility []byte            // `visibility:` as JSON; nil when the file has none
	Layouts    map[string][]byte // `layouts:` as JSON per page
}

// readBaseline reads `visibility:` and `layouts:` from the file at path with the YAML decoder itself: viper lowercases every map key,
// which would change repository addresses and the layouts' field names.
func readBaseline(path string) (Baseline, []string) {
	b := Baseline{Layouts: map[string][]byte{}}
	if path == "" {
		return b, nil
	}
	raw, err := os.ReadFile(path) //nolint:gosec // G304: the operator's own configuration file
	if err != nil {
		return b, []string{fmt.Sprintf("read %s: %v", path, err)}
	}
	b.File = raw
	var top struct {
		Visibility any            `yaml:"visibility"`
		Layouts    map[string]any `yaml:"layouts"`
	}
	if err := yaml.Unmarshal(raw, &top); err != nil {
		return b, []string{err.Error()}
	}
	var p []string
	if top.Visibility != nil {
		js, err := json.Marshal(searchOn(top.Visibility))
		if err != nil {
			p = append(p, fmt.Sprintf("visibility: %v", err))
		}
		b.Visibility = js
	}
	for page, l := range top.Layouts {
		js, err := json.Marshal(l)
		if err != nil {
			p = append(p, fmt.Sprintf("layouts.%s: %v", page, err))
			continue
		}
		b.Layouts[page] = js
	}
	return b, p
}

// searchOn fills `search: true` where a choice in `visibility:` leaves it out: in the file, an item everyone sees is offered to search
// engines unless it says `search: false`.
func searchOn(v any) any {
	doc, ok := v.(map[string]any)
	if !ok {
		return v
	}
	fill := func(s any) {
		if m, ok := s.(map[string]any); ok {
			if _, set := m["search"]; !set {
				m["search"] = true
			}
		}
	}
	for _, section := range []string{"groups", "repos"} {
		if m, ok := doc[section].(map[string]any); ok {
			for _, s := range m {
				fill(s)
			}
		}
	}
	if rules, ok := doc["rules"].([]any); ok {
		for _, r := range rules {
			fill(r)
		}
	}
	return doc
}
