package packages

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"path"
	"regexp"
	"time"

	"github.com/cantara/source-code-portal/internal/jsonstream"
)

const (
	npmjs       = "https://registry.npmjs.org/"
	npmjsBrowse = "https://www.npmjs.com/package/"
	maxTimes    = 20_000 // versions' times read from a document before its dist-tags
)

// npmName is a valid npm package name, scoped or not.
var npmName = regexp.MustCompile(`^(?:@[a-z0-9][a-z0-9._~-]*/)?[a-z0-9][a-z0-9._~-]{0,213}$`)

// NPM is the provider of npm registries (TypeScript, JavaScript, React): package.json names the package and where it publishes;
// the registry's document of the package says its latest release.
type NPM struct {
	Registry string // the public registry; registry.npmjs.org when empty
}

func (n NPM) public() string { return cmp.Or(n.Registry, npmjs) }

// Ecosystem is "npm".
func (NPM) Ecosystem() string { return "npm" }

// Wants reports whether path is a package.json.
func (NPM) Wants(p string) bool { return path.Base(p) == "package.json" }

// Coordinates reads every package.json that names a package and is not private.
func (NPM) Coordinates(files []File) []Coordinate {
	var out []Coordinate
	for _, f := range files {
		var pj struct {
			Name          string `json:"name"`
			Private       bool   `json:"private"`
			PublishConfig struct {
				Registry string `json:"registry"`
			} `json:"publishConfig"`
		}
		if json.Unmarshal(f.Content, &pj) != nil || pj.Private || !npmName.MatchString(pj.Name) {
			continue
		}
		c := Coordinate{Ecosystem: "npm", Name: pj.Name, Path: f.Path}
		if pj.PublishConfig.Registry != "" {
			c.Publish = []string{pj.PublishConfig.Registry}
		}
		out = append(out, c)
	}
	return out
}

// Latest asks the registry the package publishes to, the configured npm registries and the public one for the package's document.
func (n NPM) Latest(ctx context.Context, l *Lookup, c Coordinate) (*Version, error) {
	var configured []string
	for _, r := range l.Configured("npm") {
		configured = append(configured, r.URL)
	}
	var public []string
	if l.Public() {
		public = []string{n.public()}
	}
	var failed error
	for _, reg := range distinct(normalise(c.Publish), configured, public) {
		v, err := n.document(ctx, l, reg, c.Name)
		switch {
		case errors.Is(err, ErrNotFound):
			continue
		case err != nil:
			failed = cmp.Or(failed, err)
			continue
		}
		return v, nil
	}
	return nil, failed
}

// document reads reg's document of name as a stream: its dist-tags' latest and that version's time; the versions are skipped.
func (n NPM) document(ctx context.Context, l *Lookup, reg, name string) (*Version, error) {
	escaped := url.PathEscape(name) // a scoped name's slash is %2F, as the registries expect
	var latest string
	times := map[string]string{}
	err := l.Get(ctx, reg+escaped, "application/json", func(r io.Reader) error {
		d := jsonstream.New(r)
		return d.Object(func(key string) error {
			switch key {
			case "dist-tags":
				return d.Object(func(tag string) error {
					if tag != "latest" {
						return d.Skip()
					}
					var err error
					latest, err = d.String(maxValue)
					return err
				})
			case "time":
				return d.Object(func(v string) error {
					if latest != "" && v != latest || len(times) >= maxTimes {
						return d.Skip()
					}
					t, err := d.String(64)
					times[v] = t
					return err
				})
			}
			return d.Skip()
		})
	})
	if err != nil {
		return nil, err
	}
	if latest == "" {
		return nil, ErrNotFound // a document without a release: unpublished
	}
	out := &Version{Version: latest, Registry: l.Name(reg), URL: reg + escaped}
	if reg == npmjs {
		out.URL = npmjsBrowse + name + "/v/" + url.PathEscape(latest)
	}
	if t, err := time.Parse(time.RFC3339, times[latest]); err == nil {
		out.Published = t.UTC()
	}
	return out, nil
}
