package catalog

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"

	"github.com/cantara/source-code-portal/internal/snapshot"
	"github.com/cantara/source-code-portal/internal/untrusted"
)

const (
	maxDescriptorBytes = 64 << 10
	maxDescriptorDepth = 32
	maxListItems       = 20
)

var (
	ownerPattern = regexp.MustCompile(`^@[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})(?:/[A-Za-z0-9._-]{1,100})?$`)
	tagPattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
)

// Descriptor is the repository's own scp.yaml. It affects display only.
type Descriptor struct {
	Present     bool               `json:"present"`
	File        string             `json:"file,omitempty"` // ".github/scp.yaml" etc.
	Title       string             `json:"title,omitempty"`
	Description string             `json:"description,omitempty"`
	Lifecycle   string             `json:"lifecycle,omitempty"` // "" | experimental | production | deprecated
	Owners      []string           `json:"owners,omitempty"`
	Tags        []string           `json:"tags,omitempty"`
	Links       []Link             `json:"links,omitempty"`
	Problems    []snapshot.Problem `json:"problems,omitempty"`
}

// Link is a titled https link.
type Link struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

const descriptorFacet snapshot.Facet[Descriptor] = "descriptor"

// DescriptorOf returns the descriptor of e with its freshness.
func DescriptorOf(e *snapshot.RepoEntry) (Descriptor, snapshot.Freshness) {
	return descriptorFacet.Get(e)
}

// validLifecycle reports whether s is an accepted lifecycle.
func validLifecycle(s string) bool {
	return s == "experimental" || s == "production" || s == "deprecated"
}

// ParseDescriptor reads an scp.yaml. A document that is too large, not one YAML mapping, uses anchors, aliases or merge keys, nests
// too deep or lacks schema: 1 is rejected whole with one problem. Otherwise each field is checked on its own: an invalid field or
// item is dropped with a problem and its neighbours are kept; unknown keys are reported and ignored. Present is true when a
// document was accepted.
func ParseDescriptor(b []byte) Descriptor {
	reject := func(code, msg string) Descriptor {
		return Descriptor{Problems: []snapshot.Problem{{Code: code, Message: msg}}}
	}
	if len(b) > maxDescriptorBytes {
		return reject("descriptor.too_large", fmt.Sprintf("the file is larger than %d bytes", maxDescriptorBytes))
	}
	if !utf8.Valid(b) {
		return reject("descriptor.syntax", "the file is not UTF-8")
	}
	dec := yaml.NewDecoder(bytes.NewReader(b))
	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			return reject("descriptor.syntax", "the file is empty")
		}
		return reject("descriptor.syntax", "the file is not valid YAML")
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return reject("descriptor.multiple_documents", "the file must hold one YAML document")
	}
	if code, msg := checkShape(&doc, 0); code != "" {
		return reject(code, msg)
	}
	if len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return reject("descriptor.not_mapping", "the document must be a mapping")
	}
	top := doc.Content[0]
	fields := map[string]*yaml.Node{}
	var order []string
	for i := 0; i+1 < len(top.Content); i += 2 {
		k := top.Content[i].Value
		if _, dup := fields[k]; !dup {
			order = append(order, k)
		}
		fields[k] = top.Content[i+1]
	}
	if s := fields["schema"]; s == nil || s.Kind != yaml.ScalarNode || s.Value != "1" {
		return reject("descriptor.schema", "schema: 1 is required")
	}

	d := Descriptor{Present: true}
	problem := func(key, msg string) {
		d.Problems = append(d.Problems, snapshot.Problem{Pointer: "/" + key, Code: "field.invalid", Message: msg})
	}
	for _, key := range order {
		n := fields[key]
		switch key {
		case "schema":
		case "title":
			if v, ok := text(n, 120); ok {
				d.Title = v
			} else {
				problem(key, "title must be text of 1 to 120 characters")
			}
		case "description":
			if v, ok := text(n, 500); ok {
				d.Description = v
			} else {
				problem(key, "description must be text of 1 to 500 characters")
			}
		case "lifecycle":
			if n.Kind == yaml.ScalarNode && validLifecycle(n.Value) {
				d.Lifecycle = n.Value
			} else {
				problem(key, "lifecycle must be experimental, production or deprecated")
			}
		case "owners":
			d.Owners = stringList(n, key, ownerPattern, "an owner must be @user or @org/team", &d.Problems)
		case "tags":
			d.Tags = stringList(n, key, tagPattern, "a tag must match "+tagPattern.String(), &d.Problems)
		case "links":
			d.Links = links(n, &d.Problems)
		default:
			d.Problems = append(d.Problems, snapshot.Problem{Pointer: "/" + pointerEscape(untrusted.Text(key, 40)), Code: "field.unknown",
				Message: "unknown key, ignored"})
		}
	}
	return d
}

// checkShape rejects anchors, aliases, merge keys and deep nesting anywhere in the document.
func checkShape(n *yaml.Node, depth int) (code, msg string) {
	switch {
	case depth > maxDescriptorDepth:
		return "descriptor.depth", fmt.Sprintf("the document nests deeper than %d levels", maxDescriptorDepth)
	case n.Kind == yaml.AliasNode || n.Anchor != "":
		return "descriptor.anchor", "anchors and aliases are not allowed"
	case n.Tag == "!!merge":
		return "descriptor.anchor", "merge keys are not allowed"
	}
	for _, c := range n.Content {
		if code, msg := checkShape(c, depth+1); code != "" {
			return code, msg
		}
	}
	return "", ""
}

func text(n *yaml.Node, maxRunes int) (string, bool) {
	if n.Kind != yaml.ScalarNode || n.Tag != "!!str" {
		return "", false
	}
	v := untrusted.Text(n.Value, maxRunes+1)
	if v == "" || utf8.RuneCountInString(v) > maxRunes {
		return "", false
	}
	return v, true
}

func stringList(n *yaml.Node, key string, pattern *regexp.Regexp, msg string, problems *[]snapshot.Problem) []string {
	if n.Kind != yaml.SequenceNode {
		*problems = append(*problems, snapshot.Problem{Pointer: "/" + key, Code: "field.invalid", Message: key + " must be a list"})
		return nil
	}
	var out []string
	for i, item := range n.Content {
		at := fmt.Sprintf("/%s/%d", key, i)
		switch {
		case i >= maxListItems:
			*problems = append(*problems, snapshot.Problem{Pointer: at, Code: "field.invalid",
				Message: fmt.Sprintf("at most %d items; the rest are ignored", maxListItems)})
			return out
		case item.Kind != yaml.ScalarNode || !pattern.MatchString(item.Value):
			*problems = append(*problems, snapshot.Problem{Pointer: at, Code: "field.invalid", Message: msg})
		case !slices.Contains(out, item.Value):
			out = append(out, item.Value)
		}
	}
	return out
}

func links(n *yaml.Node, problems *[]snapshot.Problem) []Link {
	if n.Kind != yaml.SequenceNode {
		*problems = append(*problems, snapshot.Problem{Pointer: "/links", Code: "field.invalid", Message: "links must be a list"})
		return nil
	}
	var out []Link
	for i, item := range n.Content {
		at := fmt.Sprintf("/links/%d", i)
		if i >= maxListItems {
			*problems = append(*problems, snapshot.Problem{Pointer: at, Code: "field.invalid",
				Message: fmt.Sprintf("at most %d items; the rest are ignored", maxListItems)})
			return out
		}
		var title, href string
		ok := item.Kind == yaml.MappingNode
		for j := 0; ok && j+1 < len(item.Content); j += 2 {
			switch v := item.Content[j+1]; item.Content[j].Value {
			case "title":
				title, ok = text(v, 80)
			case "url":
				href, ok = v.Value, v.Kind == yaml.ScalarNode && httpsOnly(v.Value) != ""
			default:
				ok = false
			}
		}
		if !ok || title == "" || href == "" {
			*problems = append(*problems, snapshot.Problem{Pointer: at, Code: "field.invalid",
				Message: "a link is {title: 1 to 80 characters, url: an https URL}"})
			continue
		}
		u, _ := url.Parse(href) // checked by httpsOnly
		out = append(out, Link{Title: title, URL: u.String()})
	}
	return out
}

func pointerEscape(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "~", "~0"), "/", "~1")
}
