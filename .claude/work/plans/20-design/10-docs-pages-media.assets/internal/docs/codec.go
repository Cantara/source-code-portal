package docs

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/safehtml"
)

// payloadSchema is the inner schema field of the persisted payload. A newer value is refused (the row is skipped and
// the item becomes due), an older one is migrated by a decoder branch.
const payloadSchema = 1

// ErrPayload is returned by Decode for a payload that cannot be used (corrupt, newer schema).
var ErrPayload = errors.New("docs: unusable payload")

type payloadDoc struct {
	ID          string         `json:"id"`
	Path        string         `json:"path"`
	Title       string         `json:"title"`
	Description string         `json:"description,omitempty"`
	Format      Format         `json:"format"`
	Bridged     bool           `json:"bridged,omitempty"`
	State       State          `json:"state"`
	Order       *int           `json:"order,omitempty"`
	Tags        []string       `json:"tags,omitempty"`
	Section     []string       `json:"section,omitempty"`
	BlobSHA     string         `json:"blob"`
	UpdatedAt   int64          `json:"updated,omitempty"`
	Size        int64          `json:"size,omitempty"`
	Pending     bool           `json:"pending,omitempty"`
	Reason      string         `json:"reason,omitempty"`
	Aux         bool           `json:"aux,omitempty"`
	Images      []payloadImage `json:"images,omitempty"`
	HTML        string         `json:"html,omitempty"` // UNTRUSTED on the way back in: always passed through safehtml.Sanitize
	Source      string         `json:"source,omitempty"`
	TOC         []payloadTOC   `json:"toc,omitempty"`
	FrontMatter map[string]any `json:"fm,omitempty"`
	Links       []payloadLink  `json:"links,omitempty"`
	Problems    []Problem      `json:"problems,omitempty"`
	RenderedBy  string         `json:"by,omitempty"`
	Normalised  []string       `json:"norm,omitempty"`
	WordCount   int            `json:"words,omitempty"`
	FetchedAt   int64          `json:"fetched,omitempty"`
}

type payloadImage struct {
	P string `json:"p"`
	B string `json:"b"`
}

type payloadTOC struct {
	D int    `json:"d"`
	T string `json:"t"`
	I string `json:"i"`
}

type payloadLink struct {
	T string   `json:"t"`
	H string   `json:"h"`
	K LinkKind `json:"k"`
}

type payload struct {
	Schema      int               `json:"schema"`
	Pipeline    string            `json:"pipeline"`
	HeadSHA     string            `json:"head"`
	TreeOID     string            `json:"tree,omitempty"`
	Fingerprint string            `json:"fp"`
	Pending     int               `json:"pending,omitempty"`
	Readme      payloadDoc        `json:"readme"`
	Docs        []payloadDoc      `json:"docs"`
	Descriptor  payloadDescriptor `json:"descriptor"`
}

type payloadDescriptor struct {
	Present     bool                 `json:"present"`
	File        string               `json:"file,omitempty"`
	BlobSHA     string               `json:"blob,omitempty"`
	Value       Descriptor           `json:"value"`
	Problems    []payloadDescProblem `json:"problems,omitempty"`
	CatalogInfo *CatalogInfo         `json:"catalog,omitempty"`
	CatalogBlob string               `json:"catalog_blob,omitempty"`
	CodeOwners  []string             `json:"owners,omitempty"`
	OwnersBlob  string               `json:"owners_blob,omitempty"`
}

type payloadDescProblem struct {
	P string `json:"p"`
	C string `json:"c"`
	M string `json:"m"`
}

// Encode serialises rd for store.Item.Payload. The result is deterministic for equal input (struct order, sorted map keys).
func Encode(rd RepoDocs) ([]byte, error) {
	p := payload{Schema: payloadSchema, Pipeline: rd.Pipeline, HeadSHA: rd.HeadSHA, TreeOID: rd.TreeOID, Fingerprint: rd.Fingerprint,
		Pending: rd.Pending, Readme: toPayloadDoc(rd.Readme), Docs: make([]payloadDoc, 0, len(rd.Docs))}
	for _, d := range rd.Docs {
		p.Docs = append(p.Docs, toPayloadDoc(d))
	}
	dv := rd.Descriptor
	p.Descriptor = payloadDescriptor{Present: dv.Present, File: dv.File, BlobSHA: dv.BlobSHA, Value: dv.Value, CatalogInfo: dv.CatalogInfo,
		CatalogBlob: dv.CatalogBlob, CodeOwners: dv.CodeOwners, OwnersBlob: dv.OwnersBlob}
	for _, x := range dv.Problems {
		p.Descriptor.Problems = append(p.Descriptor.Problems, payloadDescProblem{x.Pointer, x.Code, x.Message})
	}
	return json.Marshal(p)
}

func toPayloadDoc(d DocView) payloadDoc {
	e := d.Entry
	o := payloadDoc{ID: string(e.ID), Path: e.Path, Title: e.Title, Description: e.Description, Format: e.Format, Bridged: e.Bridged, State: e.State,
		Order: e.Order, Tags: e.Tags, Section: e.Section, BlobSHA: e.BlobSHA, Size: e.Size, Pending: e.Pending, Reason: e.Reason, Aux: e.Aux,
		HTML: d.HTML.String(), Source: d.Source, FrontMatter: d.FrontMatter, Problems: d.Problems, RenderedBy: d.RenderedBy,
		Normalised: d.Normalised, WordCount: d.WordCount}
	if !e.UpdatedAt.IsZero() {
		o.UpdatedAt = e.UpdatedAt.Unix()
	}
	if !d.FetchedAt.IsZero() {
		o.FetchedAt = d.FetchedAt.Unix()
	}
	for _, t := range d.TOC {
		o.TOC = append(o.TOC, payloadTOC{t.Depth, t.Text, t.ID})
	}
	for _, im := range d.Images {
		o.Images = append(o.Images, payloadImage{im.Path, im.BlobSHA})
	}
	for _, l := range d.Links {
		o.Links = append(o.Links, payloadLink{l.Text, l.Href, l.Kind})
	}
	return o
}

// Decode rebuilds a RepoDocs from stored bytes. Stored HTML is untrusted: every document body goes through
// safehtml.Sanitize with p (idempotent for clean input, about 1 ms per document). Decode never re-renders; a payload
// whose Pipeline differs from the current one is returned as is (re-sanitised) and the first docs.refresh run
// re-renders from Source without any GitHub call. normalise rebuilds the LLM Markdown variant from Source.
func Decode(data []byte, info freshness.Info, p *safehtml.Policy, normalise func(source string) (string, []string)) (RepoDocs, error) {
	var in payload
	if err := json.Unmarshal(data, &in); err != nil {
		return RepoDocs{}, fmt.Errorf("%w: %v", ErrPayload, err)
	}
	if in.Schema < 1 || in.Schema > payloadSchema {
		return RepoDocs{}, fmt.Errorf("%w: schema %d", ErrPayload, in.Schema)
	}
	if err := in.validate(); err != nil {
		return RepoDocs{}, fmt.Errorf("%w: %v", ErrPayload, err)
	}
	rd := RepoDocs{HeadSHA: in.HeadSHA, TreeOID: in.TreeOID, Fingerprint: in.Fingerprint, Pipeline: in.Pipeline, Info: info, Pending: in.Pending}
	rd.Readme = fromPayloadDoc(in.Readme, p, normalise)
	for _, d := range in.Docs {
		rd.Docs = append(rd.Docs, fromPayloadDoc(d, p, normalise))
	}
	dv := in.Descriptor
	rd.Descriptor = DescriptorView{Present: dv.Present, File: dv.File, BlobSHA: dv.BlobSHA, Value: dv.Value, CatalogInfo: dv.CatalogInfo,
		CatalogBlob: dv.CatalogBlob, CodeOwners: dv.CodeOwners, OwnersBlob: dv.OwnersBlob}
	for _, x := range dv.Problems {
		rd.Descriptor.Problems = append(rd.Descriptor.Problems, DescriptorProblem{x.P, x.C, x.M})
	}
	return rd, nil
}

func fromPayloadDoc(o payloadDoc, p *safehtml.Policy, normalise func(string) (string, []string)) DocView {
	e := Entry{ID: DocID(o.ID), Path: o.Path, Key: KeyOf(o.Path), Aux: o.Aux, Reason: o.Reason, Title: o.Title, Description: o.Description, Format: o.Format, Bridged: o.Bridged,
		State: o.State, Order: o.Order, Tags: o.Tags, Section: o.Section, BlobSHA: o.BlobSHA, Size: o.Size, Pending: o.Pending}
	if o.UpdatedAt != 0 {
		e.UpdatedAt = time.Unix(o.UpdatedAt, 0).UTC()
	}
	if e.ID == ReadmeID {
		e.Key = "" // the README has no route key (its portal URL is /repos/{repo})
	}
	d := DocView{Entry: e, FrontMatter: o.FrontMatter, Problems: o.Problems, RenderedBy: o.RenderedBy, Normalised: o.Normalised, WordCount: o.WordCount, Source: o.Source}
	if o.FetchedAt != 0 {
		d.FetchedAt = time.Unix(o.FetchedAt, 0).UTC()
	}
	if e.State == StateResident {
		if o.Format == FormatText && o.RenderedBy == "scp" && o.HTML != "" && len(o.Source) > 0 {
			d.HTML = safehtml.Text(o.Source) // text READMEs are rebuilt from their source: Text is the only minter for them
		} else {
			d.HTML = safehtml.Sanitize(p, []byte(o.HTML))
		}
		if o.Source != "" && normalise != nil {
			d.Markdown, _ = normalise(o.Source)
		}
	}
	for _, t := range o.TOC {
		d.TOC = append(d.TOC, TOCEntry{t.D, t.T, t.I})
	}
	for _, l := range o.Links {
		d.Links = append(d.Links, Link{l.T, l.H, l.K})
	}
	for _, im := range o.Images {
		d.Images = append(d.Images, ImageRef{im.P, im.B})
	}
	return d
}

// validate checks the stored fields that are NOT run through the sanitiser. A tampered or corrupt row must not put an
// unknown enum, an id that does not belong to the path, a script URL in a link or an id outside the "doc-" namespace into
// the snapshot; such a row is refused as a whole (the item becomes due and is rebuilt from GitHub). Verification V-08.
func (p payload) validate() error {
	if len(p.Docs) > MaxIndexed {
		return fmt.Errorf("%d documents", len(p.Docs))
	}
	check := func(d payloadDoc, readme bool) error {
		switch d.Format {
		case FormatMarkdown, FormatAsciiDoc, FormatText:
		default:
			return fmt.Errorf("format %q", d.Format)
		}
		switch d.State {
		case StateResident, StateNotResident, StateTooLarge, StateUnsupported, StateNoContent:
		default:
			return fmt.Errorf("state %q", d.State)
		}
		if readme {
			if d.ID != string(ReadmeID) {
				return fmt.Errorf("readme id %q", d.ID)
			}
		} else if id, ok := DocIDOf(d.Path); !ok || string(id) != d.ID {
			return fmt.Errorf("id %q does not belong to path", d.ID)
		}
		for _, t := range d.TOC {
			if !strings.HasPrefix(t.I, safehtml.IDPrefix) || t.D < 1 || t.D > 6 {
				return fmt.Errorf("toc entry %q", t.I)
			}
		}
		for _, l := range d.Links {
			switch l.K {
			case LinkDoc, LinkRepoFile, LinkExternal, LinkAnchor, LinkMedia:
			default:
				return fmt.Errorf("link kind %q", l.K)
			}
			if !(strings.HasPrefix(l.H, "https://") || strings.HasPrefix(l.H, "http://") || strings.HasPrefix(l.H, "mailto:") ||
				(strings.HasPrefix(l.H, "/") && !strings.HasPrefix(l.H, "//")) || strings.HasPrefix(l.H, "#")) {
				return fmt.Errorf("link href scheme")
			}
		}
		return nil
	}
	if err := check(p.Readme, true); err != nil {
		return err
	}
	for _, d := range p.Docs {
		if err := check(d, false); err != nil {
			return err
		}
	}
	return nil
}
