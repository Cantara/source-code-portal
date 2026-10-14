package docs

import (
	"bytes"
	lru "container/list"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"path"
	"strings"
	gosync "sync"
	"syscall"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/cantara/source-code-portal/internal/github"
	"github.com/cantara/source-code-portal/internal/httpapi"
	"github.com/cantara/source-code-portal/internal/snapshot"
)

const (
	cacheBytes    = 64 << 20
	externalTTL   = time.Hour
	failureTTL    = 5 * time.Minute
	fetchTimeout  = 10 * time.Second
	maxRedirects  = 3
	imageMaxAge   = "public, max-age=3600"
	imageCSP      = "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'; sandbox"
	failureWeight = 256 // what a remembered failure counts against the cache bound
)

// errNoImage is an answer that is not an image (wrong type, too large, upstream 404): the request gets 404.
var errNoImage = errors.New("not an image")

// Images serves the images the documents show, GET /images/{owner}/{name}/{id}: repository files by blob through the source's
// client, external https images through external (Guarded in production). Only images a document of the repository records are
// served, and only from the viewer's view (current).
func (d *Docs) Images(current func(context.Context) *snapshot.Snapshot, external http.RoundTripper) http.Handler {
	return &images{current: current, sources: d.sources, cache: newCache(cacheBytes),
		client: &http.Client{Transport: external, Timeout: fetchTimeout, CheckRedirect: func(r *http.Request, via []*http.Request) error {
			if len(via) > maxRedirects || r.URL.Scheme != "https" {
				return errors.New("redirect refused")
			}
			return nil
		}}}
}

type images struct {
	current func(context.Context) *snapshot.Snapshot
	sources map[string]GitHub
	client  *http.Client
	cache   *cache
	flight  singleflight.Group
}

func (h *images) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	owner, name, id := r.PathValue("owner"), r.PathValue("name"), r.PathValue("id")
	e, moved := h.current(r.Context()).Repo(snapshot.KeyOf(owner, name))
	if e == nil {
		httpapi.WriteProblem(w, http.StatusNotFound, "not-found", "No such repository.")
		return
	}
	img, ok := findImage(e.Docs, id)
	if !ok || img.URL == "" && img.BlobSHA == "" {
		httpapi.WriteProblem(w, http.StatusNotFound, "not-found", "No such image.")
		return
	}
	if moved {
		w.Header().Set("Location", "/images/"+string(e.Repo.Key())+"/"+img.ID)
		w.WriteHeader(http.StatusPermanentRedirect)
		return
	}
	key := "url:" + img.URL
	if img.URL == "" {
		key = "blob:" + e.Repo.ID.Source + ":" + img.BlobSHA
	}
	it := h.get(key, func(ctx context.Context) (item, time.Duration) {
		if img.URL != "" {
			return h.external(ctx, img.URL), externalTTL
		}
		return h.blob(ctx, e.Repo, img), 0
	})
	switch {
	case errors.Is(it.err, errNoImage):
		httpapi.WriteProblem(w, http.StatusNotFound, "not-found", "The image is not available.")
		return
	case it.err != nil:
		httpapi.WriteProblem(w, http.StatusBadGateway, "image-unavailable", "The image could not be fetched.")
		return
	}
	hd := w.Header()
	hd.Set("Content-Type", it.ctype)
	hd.Set("Content-Security-Policy", imageCSP)
	hd.Set("Cache-Control", imageMaxAge)
	hd.Set("ETag", it.etag)
	if matches(r.Header.Get("If-None-Match"), it.etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	if r.Method != http.MethodHead {
		_, _ = w.Write(it.body)
	}
}

// findImage is the image with id among the documents.
func findImage(docs []snapshot.Doc, id string) (snapshot.Image, bool) {
	for _, d := range docs {
		for _, img := range d.Images {
			if img.ID == id {
				return img, true
			}
		}
	}
	return snapshot.Image{}, false
}

func matches(inm, etag string) bool {
	for t := range strings.SplitSeq(inm, ",") {
		if t = strings.TrimSpace(t); t == "*" || strings.TrimPrefix(t, "W/") == etag {
			return true
		}
	}
	return false
}

// get answers from the cache or makes one fetch for all concurrent requests; ttl 0 keeps the answer until it is evicted.
func (h *images) get(key string, fetch func(context.Context) (item, time.Duration)) item {
	if it, ok := h.cache.get(key, time.Now()); ok {
		return it
	}
	v, _, _ := h.flight.Do(key, func() (any, error) {
		ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout) // not the request's: others wait for this fetch
		defer cancel()
		it, ttl := fetch(ctx)
		if it.err != nil {
			ttl = failureTTL
		}
		h.cache.put(key, it, ttl, time.Now())
		return it, nil
	})
	return v.(item) //nolint:forcetypeassert // the only type stored
}

func (h *images) blob(ctx context.Context, repo snapshot.Repo, img snapshot.Image) item {
	gh := h.sources[repo.ID.Source]
	if gh == nil {
		return item{err: errNoImage}
	}
	b, err := gh.Blob(ctx, repo.Owner, repo.Name, img.BlobSHA)
	switch {
	case github.IsNotFound(err):
		return item{err: errNoImage}
	case err != nil:
		return item{err: err}
	case len(b) > maxImageBytes:
		return item{err: errNoImage}
	}
	return image(b, strings.EqualFold(path.Ext(img.Path), ".svg"))
}

func (h *images) external(ctx context.Context, url string) item {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return item{err: errNoImage}
	}
	req.Header.Set("Accept", "image/*")
	req.Header.Set("User-Agent", "source-code-portal (image proxy)")
	resp, err := h.client.Do(req)
	if err != nil {
		return item{err: err}
	}
	defer func() { _ = resp.Body.Close() }()
	switch {
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return item{err: errNoImage}
	case resp.StatusCode != http.StatusOK:
		return item{err: fmt.Errorf("upstream status %d", resp.StatusCode)}
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxImageBytes+1))
	switch {
	case err != nil:
		return item{err: err}
	case len(b) > maxImageBytes:
		return item{err: errNoImage}
	}
	mt, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	return image(b, mt == "image/svg+xml" || strings.EqualFold(path.Ext(req.URL.Path), ".svg"))
}

// image types b by its bytes: the raster formats browsers show, and SVG when the source says SVG and the start holds an svg element.
func image(b []byte, svg bool) item {
	ct := http.DetectContentType(b)
	switch ct {
	case "image/png", "image/jpeg", "image/gif", "image/webp", "image/bmp", "image/x-icon", "image/avif":
	default:
		head := b[:min(len(b), 4096)]
		if !svg || !bytes.Contains(bytes.ToLower(head), []byte("<svg")) {
			return item{err: errNoImage}
		}
		ct = "image/svg+xml"
	}
	sum := sha256.Sum256(b)
	return item{body: b, ctype: ct, etag: `"` + hex.EncodeToString(sum[:16]) + `"`}
}

// item is a cached answer: an image or a failure.
type item struct {
	body        []byte
	ctype, etag string
	err         error
}

// cache holds answers by key up to a total size, least recently used out first.
type cache struct {
	mu    gosync.Mutex
	max   int
	size  int
	order *lru.List // front: most recent
	byKey map[string]*lru.Element
}

type entry struct {
	key     string
	it      item
	expires time.Time // zero: until evicted
}

func newCache(maxBytes int) *cache {
	return &cache{max: maxBytes, order: lru.New(), byKey: map[string]*lru.Element{}}
}

func weight(it item) int {
	if it.err != nil {
		return failureWeight
	}
	return len(it.body)
}

func (c *cache) get(key string, now time.Time) (item, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.byKey[key]
	if !ok {
		return item{}, false
	}
	en := el.Value.(*entry) //nolint:forcetypeassert // the only type stored
	if !en.expires.IsZero() && now.After(en.expires) {
		c.remove(el)
		return item{}, false
	}
	c.order.MoveToFront(el)
	return en.it, true
}

func (c *cache) put(key string, it item, ttl time.Duration, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.byKey[key]; ok {
		c.remove(el)
	}
	if weight(it) > c.max {
		return
	}
	en := &entry{key: key, it: it}
	if ttl > 0 {
		en.expires = now.Add(ttl)
	}
	c.byKey[key] = c.order.PushFront(en)
	c.size += weight(it)
	for c.size > c.max {
		c.remove(c.order.Back())
	}
}

func (c *cache) remove(el *lru.Element) {
	en := el.Value.(*entry) //nolint:forcetypeassert // the only type stored
	c.order.Remove(el)
	delete(c.byKey, en.key)
	c.size -= weight(en.it)
}

// Guarded is the transport for external images: https to port 443 of public unicast addresses only (checked on the address dialled,
// so a redirect or a DNS answer cannot reach the portal's network), no proxy from the environment.
func Guarded() http.RoundTripper {
	d := &net.Dialer{Timeout: 5 * time.Second, Control: publicOnly}
	return &http.Transport{DialContext: d.DialContext, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: fetchTimeout,
		MaxIdleConns: 16, IdleConnTimeout: time.Minute, ForceAttemptHTTP2: true}
}

// publicOnly is the dial control of Guarded.
func publicOnly(_, address string, _ syscall.RawConn) error {
	ap, err := netip.ParseAddrPort(address)
	if err != nil {
		return err
	}
	if ap.Port() != 443 || !public(ap.Addr()) {
		return fmt.Errorf("image proxy: %s is not a public https address", address)
	}
	return nil
}

var nonPublic = []netip.Prefix{ //nolint:gochecknoglobals // a constant table
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"), // carrier-grade NAT
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"), // NAT64 reaches IPv4 addresses
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"), // 6to4 embeds IPv4 addresses
}

// public reports whether a is a public unicast address.
func public(a netip.Addr) bool {
	a = a.Unmap()
	if !a.IsGlobalUnicast() || a.IsPrivate() {
		return false
	}
	for _, p := range nonPublic {
		if p.Contains(a) {
			return false
		}
	}
	return true
}
