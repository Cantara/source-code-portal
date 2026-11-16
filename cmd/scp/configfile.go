package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"

	"go.yaml.in/yaml/v4"

	"github.com/cantara/source-code-portal/internal/feature/announce"
	"github.com/cantara/source-code-portal/internal/feature/layout"
	"github.com/cantara/source-code-portal/internal/feature/visibility"
	"github.com/cantara/source-code-portal/internal/platform/config"
	"github.com/cantara/source-code-portal/internal/platform/httpapi"
)

// The configuration as the base of what admins edit in the pages (design/config-baseline.md, CF1): `visibility:`, `layouts:` and
// `announcements:` in scp.yaml are the base, the store keeps the admins' changes, and the admin downloads the file with the changes
// written into it.

// configPath is the admin's download of the configuration with the changes kept in the store.
const configPath = "/api/v1/admin/config.yaml"

// baseline parses the file's `visibility:` and `layouts:` with the features' own parsers.
func baseline(b config.Baseline) (visibility.Doc, map[string]layout.Layout, []string) {
	var problems []string
	vis := visibility.Empty()
	if b.Visibility != nil {
		d, err := visibility.Parse(b.Visibility)
		if err != nil {
			problems = append(problems, "visibility: "+err.Error())
		} else {
			vis = d
		}
	}
	layouts := map[string]layout.Layout{}
	for page, raw := range b.Layouts {
		if !layout.Known(page) {
			problems = append(problems, fmt.Sprintf("layouts.%s: not a page with a layout (dashboard, present)", page))
			continue
		}
		l, err := layout.Parse(raw)
		if err != nil {
			problems = append(problems, fmt.Sprintf("layouts.%s: %v", page, err))
			continue
		}
		layouts[page] = l
	}
	return vis, layouts, problems
}

// configHandler answers the configuration file with `visibility:`, `layouts:` and `announcements:` as they are in force, every other
// line as the operator wrote it.
func configHandler(file []byte, vis *visibility.Set, notices *announce.Notices, layouts *layout.Store) http.Handler {
	return httpapi.AdminOnly(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if file == nil {
			// Configured by SCP_* variables alone: the three sections alone are not a configuration a portal starts from.
			httpapi.WriteProblem(w, http.StatusConflict, "no-file",
				"This portal was started without a configuration file, so there is no scp.yaml to write the changes into.")
			return
		}
		out, err := configYAML(file, []section{
			{"visibility", visibilityValue(vis.Doc())},
			{"layouts", layoutsValue(layouts)},
			{"announcements", announcementsValue(notices.List())},
		})
		if err != nil {
			httpapi.WriteProblem(w, http.StatusInternalServerError, "internal", "The configuration could not be written.")
			return
		}
		w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="scp.yaml"`)
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(out)
	}))
}

// section is a top-level key the download writes, with its value as JSON; nil removes the key.
type section struct {
	key   string
	value []byte
}

func visibilityValue(d visibility.Doc) []byte {
	type setting struct {
		Audience string `json:"audience"`
		Search   *bool  `json:"search,omitempty"` // written only when off: the file's default is on
	}
	type rule struct {
		Pattern string `json:"pattern"`
		setting
	}
	of := func(st visibility.Setting) setting {
		if st.Audience == visibility.Everyone && !st.Search {
			return setting{Audience: st.Audience, Search: new(false)}
		}
		return setting{Audience: st.Audience}
	}
	out := struct {
		Groups map[string]setting `json:"groups,omitempty"`
		Repos  map[string]setting `json:"repos,omitempty"`
		Rules  []rule             `json:"rules,omitempty"`
	}{}
	for k, st := range d.Groups {
		if out.Groups == nil {
			out.Groups = map[string]setting{}
		}
		out.Groups[k] = of(st)
	}
	for k, st := range d.Repos {
		if out.Repos == nil {
			out.Repos = map[string]setting{}
		}
		out.Repos[k] = of(st)
	}
	for _, r := range d.Rules {
		out.Rules = append(out.Rules, rule{Pattern: r.Pattern, setting: of(r.Setting)})
	}
	if out.Groups == nil && out.Repos == nil && out.Rules == nil {
		return nil
	}
	b, _ := json.Marshal(out) //nolint:errchkjson // plain values
	return b
}

func layoutsValue(s *layout.Store) []byte {
	pages := map[string]layout.Layout{}
	for _, page := range layout.Names() {
		if p := s.Get(page); p.Stored || p.Configured {
			pages[page] = p.Layout
		}
	}
	if len(pages) == 0 {
		return nil
	}
	b, _ := json.Marshal(pages) //nolint:errchkjson // plain values
	return b
}

func announcementsValue(list []config.Announcement) []byte {
	if len(list) == 0 {
		return nil
	}
	out := make([]config.Announcement, len(list))
	for i, a := range list {
		if a.IsPublished() {
			a.Published = nil // published is the file's default
		}
		out[i] = a
	}
	b, _ := json.Marshal(out) //nolint:errchkjson // plain values
	return b
}

// configYAML is file with each section's key set to its value, added at the end when the file lacks it, removed when the value is
// nil. Only the lines of a replaced section are written anew: every other line of the file, its comments, blank lines and long values
// included, comes back byte for byte, so the download differs from the file only where a section does.
func configYAML(file []byte, sections []section) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(file, &doc); err != nil {
		return nil, err
	}
	var keys []*yaml.Node // the top-level keys, in the file's order
	if doc.Kind != 0 {
		top := doc.Content[0]
		if top.Kind != yaml.MappingNode {
			return nil, errors.New("the configuration is not a mapping")
		}
		for i := 0; i+1 < len(top.Content); i += 2 {
			if top.Content[i].Line < 1 || top.Content[i].Column != 1 {
				return nil, errors.New("the configuration's keys are not each at the start of a line")
			}
			keys = append(keys, top.Content[i])
		}
	}
	lines := bytes.SplitAfter(file, []byte("\n"))
	if len(lines) > 0 && len(lines[len(lines)-1]) == 0 {
		lines = lines[:len(lines)-1]
	}
	replace := map[int][]byte{} // by the key's first line (0-based): what its lines become
	ends := map[int]int{}       // and the line after its section
	var added []byte
	for _, sc := range sections {
		var text []byte
		if sc.value != nil {
			v, err := nodeOf(sc.value)
			if err != nil {
				return nil, err
			}
			if text, err = encodeSection(sc.key, v); err != nil {
				return nil, err
			}
		}
		at := slices.IndexFunc(keys, func(k *yaml.Node) bool { return k.Value == sc.key })
		if at < 0 {
			added = append(added, text...)
			continue
		}
		first := keys[at].Line - 1
		end := len(lines)
		if at+1 < len(keys) {
			end = keys[at+1].Line - 1
		}
		for end > first+1 && belowSection(lines[end-1]) { // the next key's comment and the blank lines above it stay
			end--
		}
		replace[first], ends[first] = text, end
	}
	var out bytes.Buffer
	for i := 0; i < len(lines); {
		if text, ok := replace[i]; ok {
			out.Write(text)
			i = ends[i]
			continue
		}
		out.Write(lines[i])
		i++
	}
	if len(added) > 0 {
		if b := out.Bytes(); len(b) > 0 && b[len(b)-1] != '\n' {
			out.WriteByte('\n')
		}
		out.Write(added)
	}
	return out.Bytes(), nil
}

// belowSection reports whether line, at the end of a top-level section, belongs to what follows it: a blank line or a comment at the
// start of the line.
func belowSection(line []byte) bool {
	t := bytes.TrimRight(line, "\r\n")
	return len(bytes.TrimSpace(t)) == 0 || t[0] == '#'
}

// encodeSection is the YAML of key set to v, two spaces a level, lists indented under their key, long values on one line.
func encodeSection(key string, v *yaml.Node) ([]byte, error) {
	m := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, v}}
	var buf bytes.Buffer
	enc, err := yaml.NewDumper(&buf, yaml.WithV3Defaults(), yaml.WithIndent(2), yaml.WithLineWidth(-1))
	if err != nil {
		return nil, err
	}
	if err := enc.Dump(m); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// nodeOf is the JSON value b as a YAML node, its objects' keys in their order in b.
func nodeOf(b []byte) (*yaml.Node, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	n, err := nodeFrom(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("more than one JSON value")
	}
	return n, nil
}

func nodeFrom(dec *json.Decoder) (*yaml.Node, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		n := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		if t == '{' {
			n = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		}
		for dec.More() {
			if n.Kind == yaml.MappingNode {
				k, err := dec.Token()
				if err != nil {
					return nil, err
				}
				n.Content = append(n.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: fmt.Sprint(k)})
			}
			v, err := nodeFrom(dec)
			if err != nil {
				return nil, err
			}
			n.Content = append(n.Content, v)
		}
		if _, err := dec.Token(); err != nil { // the closing delimiter
			return nil, err
		}
		return n, nil
	case string:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: t}, nil
	case json.Number:
		tag := "!!int"
		if _, err := strconv.ParseInt(string(t), 10, 64); err != nil {
			tag = "!!float"
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: string(t)}, nil
	case bool:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(t)}, nil
	default:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}, nil
	}
}
