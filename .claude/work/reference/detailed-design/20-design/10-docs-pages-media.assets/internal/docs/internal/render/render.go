// Package render is the private Markdown pipeline of internal/docs (compile-checked sketch of section 4 of 10-docs-pages-media.md).
// Rank 1: it imports only safehtml (and, in the real tree, scm/freshness/secret). It never builds a SafeHTML itself.
package render

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"

	"github.com/cantara/source-code-portal/internal/safehtml"
)

// Version is bumped by hand on any change of rendered output (guarded by testdata/golden/pipeline.lock).
const Version = 1

// Resolver maps URLs found in untrusted HTML to final URLs. Implemented by RepoResolver and PageResolver.
type Resolver interface {
	// Link maps an <a href> that is not a bare fragment. ok=false drops the href. external=true means "leaves the portal origin".
	Link(raw string) (href string, external bool, ok bool)
	// Image maps an <img src> or a <source srcset> candidate. ok=false drops the image.
	Image(raw string) (src string, ok bool)
}

type Limits struct {
	MaxSourceBytes int           // truncate above this (default 512000)
	MaxHTMLBytes   int           // typed error above this (default 8 MiB)
	MaxCodeBlock   int           // highlight blocks up to this size (default 64 KiB)
	Timeout        time.Duration // advisory deadline checked at stage boundaries
}

type Input struct {
	Source  []byte
	Path    string // repository path, for diagnostics and the SourceURL
	BlobSHA string
	Route   Resolver
	Limits  Limits
	Format  Format // FormatMarkdown, FormatGitHubHTML
	Split   bool   // split YAML front matter (docs and pages: true, README: false)
}

type Format uint8

const (
	FormatMarkdown Format = iota
	FormatGitHubHTML
)

type TOCEntry struct {
	Depth int
	Text  string
	ID    string // "doc-" namespaced
}

type Link struct {
	Text, Href string
	Kind       LinkKind
}

type LinkKind string

const (
	LinkAnchor   LinkKind = "anchor"
	LinkDoc      LinkKind = "doc"
	LinkRepoFile LinkKind = "repo-file"
	LinkExternal LinkKind = "external"
)

type Problem struct{ Path, Code, Message string }

type Notice string

const (
	NoticeTruncated        Notice = "truncated"
	NoticeEncodingReplaced Notice = "encoding.replaced"
)

type Output struct {
	HTML        safehtml.SafeHTML
	TOC         []TOCEntry
	Title       string // first H1 text, "" when none
	Description string // first non-empty paragraph, plain text, <= 300 runes
	FrontMatter map[string]any
	Links       []Link
	Problems    []Problem
	Notices     []Notice
	WordCount   int
	Body        string   // the text goldmark parsed: BOM stripped, UTF-8 repaired, truncated, front matter removed (what the store keeps as Source)
	LLMMarkdown string   // normalised body for the raw Markdown variant ("" for the GitHub HTML bridge)
	Normalised  []string // markers: html-comments-removed, invisible-characters-removed, tag-characters-removed
}

type Renderer interface {
	Markdown(ctx context.Context, in Input) (Output, error)
	Bridge(ctx context.Context, githubHTML string, in Input) (Output, error)
}

var ErrTooLarge = errors.New("render: output too large")

// Pipeline is safe for concurrent use.
type Pipeline struct {
	policy *safehtml.Policy
}

func New() *Pipeline { return &Pipeline{policy: safehtml.DocsPolicy()} }

func (p *Pipeline) newMarkdown(st *renderState) goldmark.Markdown {
	md := goldmark.New(
		goldmark.WithExtensions(extension.Linkify, extension.NewTable(extension.WithTableCellAlignMethod(extension.TableCellAlignAttribute)), extension.Strikethrough, extension.TaskList, extension.Footnote),
		goldmark.WithParserOptions(),
		goldmark.WithRendererOptions(html.WithUnsafe()),
	)
	md.Parser().AddOptions(parser.WithASTTransformers(util.Prioritized(alertTransformer{}, 100)))
	md.Renderer().AddOptions(renderer.WithNodeRenderers(
		util.Prioritized(&codeRenderer{st: st}, 100), util.Prioritized(alertRenderer{}, 100)))
	return md
}

func (p *Pipeline) Markdown(ctx context.Context, in Input) (Output, error) {
	var out Output
	src := in.Source
	src = bytes.TrimPrefix(src, []byte("\xef\xbb\xbf"))
	if !utf8.Valid(src) {
		src = []byte(strings.ToValidUTF8(string(src), "�"))
		out.Notices = append(out.Notices, NoticeEncodingReplaced)
	}
	if lim := in.Limits.MaxSourceBytes; lim > 0 && len(src) > lim {
		src = cutAtLine(src, lim)
		out.Notices = append(out.Notices, NoticeTruncated)
	}
	if in.Split {
		fm, body, ok := SplitFrontMatter(src)
		if ok {
			out.FrontMatter, out.Problems = ParseFrontMatter(fm)
			src = body
		}
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if err := checkShape(src); err != nil {
		return out, err
	}
	st := &renderState{maxBlock: 4 << 10, deadline: time.Now().Add(1500 * time.Millisecond)}
	md := p.newMarkdown(st)
	reader := text.NewReader(src)
	doc := md.Parser().Parse(reader)
	var buf bytes.Buffer
	if err := md.Renderer().Render(&buf, src, doc); err != nil {
		return out, fmt.Errorf("render: %w", err)
	}
	out.Body = string(src)
	out.Title, out.Description = titleAndDescription(doc, src)
	out.LLMMarkdown, out.Normalised = Normalise(doc, src)
	if err := ctx.Err(); err != nil {
		return out, err
	}
	res, err := rewriteHTML(buf.String(), in.Route, passOptions{})
	if err != nil {
		return out, err
	}
	if len(res.html) > maxHTML(in.Limits) {
		return out, ErrTooLarge
	}
	out.HTML = safehtml.Sanitize(p.policy, []byte(res.html))
	out.TOC, out.Links, out.WordCount = res.toc, res.links, res.words
	return out, nil
}

func (p *Pipeline) Bridge(ctx context.Context, githubHTML string, in Input) (Output, error) {
	var out Output
	res, err := rewriteHTML(githubHTML, in.Route, passOptions{bridge: true})
	if err != nil {
		return out, err
	}
	if len(res.html) > maxHTML(in.Limits) {
		return out, ErrTooLarge
	}
	out.HTML = safehtml.Sanitize(p.policy, []byte(res.html))
	out.TOC, out.Links, out.WordCount = res.toc, res.links, res.words
	out.Title, out.Description = res.title, res.description
	return out, nil
}

func maxHTML(l Limits) int {
	if l.MaxHTMLBytes > 0 {
		return l.MaxHTMLBytes
	}
	return 8 << 20
}

// cutAtLine cuts src to at most n bytes at a line boundary on a UTF-8 boundary.
func cutAtLine(src []byte, n int) []byte {
	if len(src) <= n {
		return src
	}
	cut := src[:n]
	if i := bytes.LastIndexByte(cut, '\n'); i > n/2 {
		return cut[:i+1]
	}
	for len(cut) > 0 && !utf8.Valid(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut
}

var _ ast.Node
