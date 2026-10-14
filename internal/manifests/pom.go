package manifests

import (
	"bufio"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/cantara/source-code-portal/internal/snapshot"
)

const (
	maxXMLDepth      = 64
	maxXMLTokens     = 200_000
	maxXMLText       = 4 << 10 // bytes kept per element
	maxPOMProperties = 500
)

var propertyRef = regexp.MustCompile(`\$\{([A-Za-z0-9_.-]{1,100})\}`)

// javaProperties name the Java version, most specific first.
var javaProperties = []string{"maven.compiler.release", "maven.compiler.target", "maven.compiler.source", "java.version"} //nolint:gochecknoglobals // fixed table

// pomDependency is one dependency element.
type pomDependency struct {
	groupID, artifactID, version, scope, typ string
	managed                                  bool // inside dependencyManagement
}

// pom is what the parser keeps of a POM.
type pom struct {
	groupID, artifactID, version             string
	parentGroup, parentArtifact, parentVer   string
	properties                               map[string]string
	dependencies                             []pomDependency
	tooManyProperties, tooManyDeps, overlong bool
}

// parsePOM reads a Maven pom.xml.
func parsePOM(b []byte) snapshot.Manifest {
	m := snapshot.Manifest{Ecosystem: "maven", Language: "Java", BuildTool: "Maven"}
	p, err := readPOM(b)
	if err != nil {
		var se *xml.SyntaxError
		code, ptr := "syntax", ""
		switch {
		case errors.As(err, &se):
			ptr = fmt.Sprintf("/line/%d", se.Line)
		case errors.Is(err, errEncoding):
			code = "encoding"
		case errors.Is(err, errBounds):
			code = "too-complex"
		}
		m.Problems = append(m.Problems, problem(ptr, code, "%s", strings.TrimPrefix(err.Error(), "XML syntax error on line ")))
		return m
	}
	vars := map[string]string{}
	for k, v := range p.properties {
		vars[k] = v
	}
	group := p.groupID
	if group == "" {
		group = p.parentGroup
	}
	ver := p.version
	if ver == "" {
		ver = p.parentVer
	}
	expand := func(s string) string { // one pass: a value that names another property stays as it is
		return propertyRef.ReplaceAllStringFunc(s, func(ref string) string {
			if v, ok := vars[ref[2:len(ref)-1]]; ok {
				return v
			}
			return ref
		})
	}
	group, ver = expand(group), expand(ver)
	vars["project.groupId"], vars["project.artifactId"], vars["project.version"] = group, expand(p.artifactID), ver
	vars["project.parent.groupId"], vars["project.parent.version"] = expand(p.parentGroup), expand(p.parentVer)
	vars["pom.groupId"], vars["pom.version"] = group, ver
	if g, a := group, vars["project.artifactId"]; g != "" && a != "" {
		m.Name = name(g + ":" + a)
	}
	m.Version = version(ver)
	for _, k := range javaProperties {
		if v := version(expand(p.properties[k])); v != "" {
			m.Toolchain = append(m.Toolchain, snapshot.Toolchain{Name: "java", Version: v})
			break
		}
	}
	var d deps
	if p.parentGroup != "" && p.parentArtifact != "" {
		d.add(expand(p.parentGroup)+":"+expand(p.parentArtifact), expand(p.parentVer), "parent")
	}
	for _, dep := range p.dependencies {
		g, a := expand(dep.groupID), expand(dep.artifactID)
		if g == "" || a == "" {
			continue
		}
		scope := strings.TrimSpace(expand(dep.scope))
		switch {
		case dep.managed && scope == "import" && dep.typ == "pom":
			scope = "import"
		case dep.managed:
			continue // a version constraint, not a dependency
		case scope == "test":
		case scope == "provided" || scope == "system":
			scope = "provided"
		default:
			scope = "runtime"
		}
		d.add(g+":"+a, expand(dep.version), scope)
		if g == "org.jetbrains.kotlin" && strings.HasPrefix(a, "kotlin-stdlib") {
			m.Language = "Kotlin"
		}
	}
	d.cut = d.cut || p.tooManyDeps
	d.finish(&m)
	if p.tooManyProperties {
		m.Problems = append(m.Problems, problem("", "too-many-properties", "only the first %d properties are read", maxPOMProperties))
	}
	if p.overlong {
		m.Problems = append(m.Problems, problem("", "too-long", "a value over %d bytes was cut", maxXMLText))
	}
	return m
}

var (
	errEncoding = errors.New("the file is not UTF-8, US-ASCII or ISO-8859-1")
	errBounds   = errors.New("the document is too deeply nested or has too many elements")
)

// readPOM walks the token stream within the bounds and keeps the elements the parser reads. The decoder is strict and knows only
// the predefined entities, so DTD entity tricks fail as syntax errors.
func readPOM(b []byte) (*pom, error) {
	d := xml.NewDecoder(bytes.NewReader(b))
	d.Strict = true
	d.CharsetReader = charsetReader
	p := &pom{properties: map[string]string{}}
	var stack []string
	var text strings.Builder
	var dep *pomDependency
	rooted := false
	for tokens := 0; ; tokens++ {
		if tokens == maxXMLTokens {
			return nil, errBounds
		}
		tok, err := d.Token()
		if errors.Is(err, io.EOF) {
			if !rooted {
				return nil, errors.New("the document has no project element")
			}
			return p, nil
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if len(stack) == maxXMLDepth {
				return nil, errBounds
			}
			if len(stack) == 0 {
				if rooted || t.Name.Local != "project" {
					return nil, errors.New("the root element is not project")
				}
				rooted = true
			}
			stack = append(stack, t.Name.Local)
			text.Reset()
			if at := strings.Join(stack, "/"); at == "project/dependencies/dependency" || at == "project/dependencyManagement/dependencies/dependency" {
				dep = &pomDependency{managed: strings.Contains(at, "dependencyManagement")}
			}
		case xml.CharData:
			if text.Len()+len(t) > maxXMLText {
				p.overlong = true
				t = t[:max(0, maxXMLText-text.Len())]
			}
			text.Write(t)
		case xml.EndElement:
			at := strings.Join(stack, "/")
			v := strings.TrimSpace(text.String())
			p.keep(at, v, dep)
			if dep != nil && strings.HasSuffix(at, "dependencies/dependency") {
				if len(p.dependencies) == maxDependencies {
					p.tooManyDeps = true
				} else {
					p.dependencies = append(p.dependencies, *dep)
				}
				dep = nil
			}
			stack = stack[:len(stack)-1]
			text.Reset()
		}
	}
}

// keep records the value of the element at path.
func (p *pom) keep(at, v string, dep *pomDependency) {
	switch at {
	case "project/groupId":
		p.groupID = v
	case "project/artifactId":
		p.artifactID = v
	case "project/version":
		p.version = v
	case "project/parent/groupId":
		p.parentGroup = v
	case "project/parent/artifactId":
		p.parentArtifact = v
	case "project/parent/version":
		p.parentVer = v
	}
	if k, ok := strings.CutPrefix(at, "project/properties/"); ok && !strings.Contains(k, "/") {
		if len(p.properties) == maxPOMProperties {
			p.tooManyProperties = true
		} else {
			p.properties[k] = v
		}
	}
	if dep == nil {
		return
	}
	switch at[strings.LastIndexByte(at, '/')+1:] {
	case "groupId":
		dep.groupID = v
	case "artifactId":
		dep.artifactID = v
	case "version":
		dep.version = v
	case "scope":
		dep.scope = v
	case "type":
		dep.typ = v
	}
}

// charsetReader accepts the encodings POMs are written in besides UTF-8.
func charsetReader(label string, in io.Reader) (io.Reader, error) {
	switch strings.ToLower(label) {
	case "us-ascii", "ascii":
		return in, nil
	case "iso-8859-1", "latin1", "latin-1":
		return latin1{bufio.NewReader(in)}, nil
	}
	return nil, errEncoding
}

// latin1 decodes ISO-8859-1 to UTF-8: every byte is the code point of the same value.
type latin1 struct{ r *bufio.Reader }

func (l latin1) Read(p []byte) (int, error) {
	n := 0
	for n+2 <= len(p) { // a Latin-1 character is at most two bytes in UTF-8
		c, err := l.r.ReadByte()
		if err != nil {
			if n > 0 && errors.Is(err, io.EOF) {
				return n, nil
			}
			return n, err
		}
		n += utf8.EncodeRune(p[n:], rune(c))
	}
	return n, nil
}
