package packages

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"
)

const (
	maxPOM         = 512 << 10 // bytes of a POM read
	maxMetadata    = 4 << 20   // bytes of a maven-metadata.xml read (one line per version)
	maxParents     = 5         // levels of the parent chain followed
	maxXMLDepth    = 32
	maxXMLTokens   = 200_000
	maxValue       = 512 // bytes of a value kept
	mavenCentral   = "https://repo1.maven.org/maven2/"
	centralBrowse  = "https://central.sonatype.com/artifact/"
	mavenXMLAccept = "application/xml, text/xml;q=0.9, */*;q=0.1"
)

// Maven is the provider of Maven repositories (Java, Kotlin, Scala): the POM and its parent chain say where a package is published,
// the repository's maven-metadata.xml says its newest release.
type Maven struct {
	Central string // the public repository; Maven Central when empty
}

func (m Maven) central() string { return cmp.Or(m.Central, mavenCentral) }

// Ecosystem is "maven".
func (Maven) Ecosystem() string { return "maven" }

// Wants reports whether path is a POM or a Gradle build's file: build.gradle(.kts), settings.gradle(.kts), gradle.properties.
func (Maven) Wants(p string) bool {
	switch path.Base(p) {
	case "pom.xml", "build.gradle", "build.gradle.kts", "settings.gradle", "settings.gradle.kts", "gradle.properties":
		return true
	}
	return false
}

// Coordinates reads every POM (its group or its parent's, artifact, parent, and the repositories it publishes to and reads from)
// and every Gradle build that publishes (maven-publish or a publishing block): its group and root project's name and the
// repositories of its publishing block.
func (Maven) Coordinates(files []File) []Coordinate {
	out := gradleCoordinates(files)
	for _, f := range files {
		if path.Base(f.Path) != "pom.xml" {
			continue
		}
		p, err := readPOM(bytes.NewReader(f.Content))
		if err != nil || p.artifact == "" || p.group() == "" || strings.Contains(p.group()+p.artifact, "${") {
			continue
		}
		c := Coordinate{Ecosystem: "maven", Name: p.group() + ":" + p.artifact, Path: f.Path, Resolve: p.repositories}
		if p.distribution != "" {
			c.Publish = []string{p.distribution}
		}
		if p.parentGAV() != "" {
			c.Parent = p.parentGAV()
		}
		out = append(out, c)
	}
	return out
}

// Latest asks, in order, the repository the POM (or its parent chain) publishes to, the configured Maven registries and Maven
// Central, for the package's maven-metadata.xml.
func (m Maven) Latest(ctx context.Context, l *Lookup, c Coordinate) (*Version, error) {
	group, artifact, _ := strings.Cut(c.Name, ":")
	publish := c.Publish
	if len(publish) == 0 && c.Parent != "" {
		var err error
		if publish, err = m.inherited(ctx, l, c); err != nil {
			return nil, err
		}
	}
	var configured []string
	for _, r := range l.Configured("maven") {
		configured = append(configured, r.URL)
	}
	var public []string
	if l.Public() {
		public = []string{m.central()}
	}
	var failed error
	for _, repo := range distinct(normalise(publish), normalise(configured), public) {
		v, err := m.metadata(ctx, l, repo, group, artifact)
		switch {
		case errors.Is(err, ErrNotFound):
			continue
		case err != nil:
			failed = cmp.Or(failed, err) // another repository may still hold it
			continue
		}
		return v, nil
	}
	return nil, failed
}

// inherited follows the parent chain to the first distributionManagement, each parent read from the repositories its child reads
// from, the configured ones and Maven Central; a released parent never changes and is kept.
func (m Maven) inherited(ctx context.Context, l *Lookup, c Coordinate) ([]string, error) {
	resolve, gav := c.Resolve, c.Parent
	for range maxParents {
		if gav == "" {
			return nil, nil
		}
		p, err := m.parent(ctx, l, gav, resolve)
		if err != nil || p == nil {
			return nil, err
		}
		if p.distribution != "" {
			return []string{p.distribution}, nil
		}
		resolve, gav = distinct(resolve, p.repositories), p.parentGAV()
	}
	return nil, nil
}

// kept is the part of a parent POM the chain needs, as it is kept.
type keptPOM struct {
	Distribution string   `json:"distribution,omitempty"`
	Repositories []string `json:"repositories,omitempty"`
	Parent       string   `json:"parent,omitempty"`
}

// parent reads the POM group:artifact:version from the first repository that holds it; nil when none does.
func (m Maven) parent(ctx context.Context, l *Lookup, gav string, resolve []string) (*pom, error) {
	key := "maven:pom:" + gav
	if v, ok := l.Kept(key); ok {
		var k keptPOM
		if json.Unmarshal([]byte(v), &k) == nil {
			return keptToPOM(k), nil
		}
	}
	parts := strings.Split(gav, ":")
	if len(parts) != 3 || strings.HasSuffix(parts[2], "-SNAPSHOT") {
		return nil, nil //nolint:nilnil // a snapshot parent changes: it is not followed
	}
	g, a, v := parts[0], parts[1], parts[2]
	var configured []string
	for _, r := range l.Configured("maven") {
		configured = append(configured, r.URL)
	}
	var failed error
	for _, repo := range distinct(normalise(resolve), normalise(configured), []string{m.central()}) {
		var p *pom
		err := l.Get(ctx, repo+groupPath(g)+"/"+a+"/"+v+"/"+a+"-"+v+".pom", mavenXMLAccept, func(r io.Reader) error {
			var err error
			p, err = readPOM(io.LimitReader(r, maxPOM))
			return err
		})
		switch {
		case errors.Is(err, ErrNotFound):
			continue
		case err != nil:
			failed = cmp.Or(failed, err)
			continue
		}
		k := keptPOM{Distribution: p.distribution, Repositories: p.repositories, Parent: p.parentGAV()}
		b, _ := json.Marshal(k)
		l.Keep(key, string(b))
		return keptToPOM(k), nil
	}
	return nil, failed
}

func keptToPOM(k keptPOM) *pom {
	p := &pom{distribution: k.Distribution, repositories: k.Repositories}
	if parts := strings.Split(k.Parent, ":"); len(parts) == 3 {
		p.parent = [3]string{parts[0], parts[1], parts[2]}
	}
	return p
}

// metadata reads repo's maven-metadata.xml of group:artifact: the release it names, else the newest version that is no snapshot.
func (m Maven) metadata(ctx context.Context, l *Lookup, repo, group, artifact string) (*Version, error) {
	dir := repo + groupPath(group) + "/" + artifact + "/"
	var md metadata
	err := l.Get(ctx, dir+"maven-metadata.xml", mavenXMLAccept, func(r io.Reader) error {
		var err error
		md, err = readMetadata(io.LimitReader(r, maxMetadata))
		return err
	})
	if err != nil {
		return nil, err
	}
	v := md.newest()
	if v == "" {
		return nil, ErrNotFound
	}
	out := &Version{Version: v, Registry: l.Name(repo), URL: dir + v + "/"}
	if repo == mavenCentral {
		out.URL = centralBrowse + group + "/" + artifact + "/" + v
	}
	if t, err := time.ParseInLocation("20060102150405", md.updated, time.UTC); err == nil {
		out.Published = t
	}
	return out, nil
}

func normalise(us []string) []string {
	out := make([]string, 0, len(us))
	for _, u := range us {
		if r := registryURL(u); r != "" {
			out = append(out, r)
		}
	}
	return out
}

func groupPath(g string) string { return strings.ReplaceAll(g, ".", "/") }

// pom is what a POM says about where its package goes.
type pom struct {
	groupID, artifact string
	parent            [3]string // group, artifact, version
	distribution      string
	repositories      []string
	properties        map[string]string
}

func (p *pom) group() string { return cmp.Or(p.groupID, p.parent[0]) }

func (p *pom) parentGAV() string {
	if p.parent[0] == "" || p.parent[1] == "" || p.parent[2] == "" || strings.Contains(strings.Join(p.parent[:], ""), "${") {
		return ""
	}
	return strings.Join(p.parent[:], ":")
}

var property = regexp.MustCompile(`\$\{([A-Za-z0-9._-]{1,100})\}`)

// readPOM walks a POM's XML tokens within depth and token caps, keeping only the elements it names.
func readPOM(r io.Reader) (*pom, error) {
	p := &pom{properties: map[string]string{}}
	var repos []string
	err := walkXML(r, func(at []string, text string) {
		switch strings.Join(at, "/") {
		case "project/groupId":
			p.groupID = text
		case "project/artifactId":
			p.artifact = text
		case "project/parent/groupId":
			p.parent[0] = text
		case "project/parent/artifactId":
			p.parent[1] = text
		case "project/parent/version":
			p.parent[2] = text
		case "project/distributionManagement/repository/url":
			p.distribution = text
		case "project/repositories/repository/url":
			repos = append(repos, text)
		default:
			if len(at) == 3 && at[0] == "project" && at[1] == "properties" && len(p.properties) < 200 {
				p.properties[at[2]] = text
			}
		}
	})
	if err != nil {
		return nil, err
	}
	expand := func(s string) string {
		return property.ReplaceAllStringFunc(s, func(m string) string {
			if v, ok := p.properties[m[2:len(m)-1]]; ok && !strings.Contains(v, "${") {
				return v
			}
			return m
		})
	}
	p.groupID, p.distribution = expand(p.groupID), expand(p.distribution)
	for _, u := range repos {
		p.repositories = append(p.repositories, expand(u))
	}
	if strings.Contains(p.distribution, "${") {
		p.distribution = ""
	}
	p.repositories = slices.DeleteFunc(p.repositories, func(u string) bool { return strings.Contains(u, "${") })
	return p, nil
}

// metadata is what a maven-metadata.xml says.
type metadata struct {
	release, latest, updated string
	versions                 []string
}

func (m metadata) newest() string {
	for _, v := range []string{m.release, m.latest} {
		if v != "" && !strings.HasSuffix(v, "-SNAPSHOT") {
			return v
		}
	}
	for _, v := range slices.Backward(m.versions) {
		if !strings.HasSuffix(v, "-SNAPSHOT") {
			return v
		}
	}
	return ""
}

func readMetadata(r io.Reader) (metadata, error) {
	var m metadata
	err := walkXML(r, func(at []string, text string) {
		switch strings.Join(at, "/") {
		case "metadata/versioning/release":
			m.release = text
		case "metadata/versioning/latest":
			m.latest = text
		case "metadata/versioning/lastUpdated":
			m.updated = text
		case "metadata/versioning/versions/version":
			if len(m.versions) < 10_000 {
				m.versions = append(m.versions, text)
			}
		}
	})
	return m, err
}

var errXML = errors.New("packages: not XML within bounds")

// walkXML calls each with the element path and the trimmed text of every element that holds text, within depth, token and value
// caps; namespaces are dropped (Maven's elements are in one).
func walkXML(r io.Reader, each func(at []string, text string)) error {
	d := xml.NewDecoder(r)
	d.Strict = false
	d.CharsetReader = func(_ string, in io.Reader) (io.Reader, error) { return in, nil } // the elements read are ASCII
	var at []string
	var text strings.Builder
	for n := 0; ; n++ {
		if n > maxXMLTokens {
			return errXML
		}
		tok, err := d.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return errors.Join(errXML, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if len(at) == maxXMLDepth {
				return errXML
			}
			at = append(at, t.Name.Local)
			text.Reset()
		case xml.CharData:
			if text.Len() < maxValue {
				text.Write(t[:min(len(t), maxValue-text.Len())])
			}
		case xml.EndElement:
			if len(at) == 0 {
				return errXML
			}
			if s := strings.TrimSpace(text.String()); s != "" {
				each(at, s)
			}
			at = at[:len(at)-1]
			text.Reset()
		}
	}
}

var (
	gradleGroup    = regexp.MustCompile(`(?m)^\s*group\s*=?\s*["']([A-Za-z0-9_.-]{1,200})["']`)
	gradleProperty = regexp.MustCompile(`(?m)^\s*group\s*[=:]\s*([A-Za-z0-9_.-]{1,200})\s*$`)
	gradleRoot     = regexp.MustCompile(`rootProject\.name\s*=\s*["']([A-Za-z0-9_.-]{1,200})["']`)
	gradleArtifact = regexp.MustCompile(`artifactId\s*=\s*["']([A-Za-z0-9_.-]{1,200})["']`)
	gradleURL      = regexp.MustCompile(`url\s*(?:=\s*)?(?:uri\s*\(\s*)?["'](https?://[^"'\s]{1,500})["']`)
)

// gradleCoordinates reads the root Gradle build, read as text (it is code, never run): a build that applies maven-publish or has a
// publishing block publishes group:name, the name its publication's artifactId or the root project's.
func gradleCoordinates(files []File) []Coordinate {
	var build, settings, props *File
	for i, f := range files {
		switch f.Path {
		case "build.gradle", "build.gradle.kts":
			build = &files[i]
		case "settings.gradle", "settings.gradle.kts":
			settings = &files[i]
		case "gradle.properties":
			props = &files[i]
		}
	}
	if build == nil {
		return nil
	}
	text := string(build.Content)
	at := strings.Index(text, "publishing")
	if at < 0 && !strings.Contains(text, "maven-publish") {
		return nil
	}
	group := ""
	if m := gradleGroup.FindStringSubmatch(text); m != nil {
		group = m[1]
	} else if props != nil {
		if m := gradleProperty.FindStringSubmatch(string(props.Content)); m != nil {
			group = m[1]
		}
	}
	artifact := ""
	if at >= 0 {
		if m := gradleArtifact.FindStringSubmatch(text[at:]); m != nil {
			artifact = m[1]
		}
	}
	if artifact == "" && settings != nil {
		if m := gradleRoot.FindStringSubmatch(string(settings.Content)); m != nil {
			artifact = m[1]
		}
	}
	if group == "" || artifact == "" {
		return nil
	}
	c := Coordinate{Ecosystem: "maven", Name: group + ":" + artifact, Path: build.Path}
	if at >= 0 {
		for _, m := range gradleURL.FindAllStringSubmatch(text[at:], 10) {
			c.Publish = append(c.Publish, m[1])
		}
	}
	return []Coordinate{c}
}
