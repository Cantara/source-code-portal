package github

import (
	"bytes"
	"container/list"
	"crypto/sha256"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"
)

const maxCachedBody = 1 << 20

// notModifiedHeader marks a response replayed from the conditional cache after a 304.
const notModifiedHeader = "X-Scp-Not-Modified"

// scpTransport sits below authentication: it limits requests in flight, sends conditional GETs and replays the cached body on
// 304 (go-github treats a 304 as an error), and records the rate-limit headers of every response.
type scpTransport struct {
	base  http.RoundTripper
	cache *etagCache
	rate  *rateBook
	sem   chan struct{}
	src   *Source // its observe hook sees every status, 304s included
}

func (t *scpTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	select {
	case t.sem <- struct{}{}:
	case <-req.Context().Done():
		return nil, req.Context().Err()
	}
	defer func() { <-t.sem }()

	req = req.Clone(req.Context())
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	key := ""
	var cached *cacheEntry
	if req.Method == http.MethodGet {
		key = cacheKey(req)
		if cached = t.cache.get(key); cached != nil {
			req.Header.Set("If-None-Match", cached.etag)
		}
	}
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	t.rate.observe(resp.Header)
	if t.src.observe != nil {
		t.src.observe(t.src.id, resp.StatusCode)
	}

	switch {
	case resp.StatusCode == http.StatusNotModified && cached != nil:
		_ = resp.Body.Close()
		h := cached.header.Clone()
		for _, k := range []string{"X-Ratelimit-Limit", "X-Ratelimit-Remaining", "X-Ratelimit-Reset", "X-Ratelimit-Resource"} {
			if v := resp.Header.Get(k); v != "" {
				h.Set(k, v)
			}
		}
		h.Set(notModifiedHeader, "1")
		return &http.Response{Status: "200 OK", StatusCode: http.StatusOK, Proto: resp.Proto, ProtoMajor: resp.ProtoMajor,
			ProtoMinor: resp.ProtoMinor, Header: h, Body: io.NopCloser(bytes.NewReader(cached.body)),
			ContentLength: int64(len(cached.body)), Request: req}, nil
	case resp.StatusCode == http.StatusOK && key != "" && resp.Header.Get("ETag") != "":
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxCachedBody+1))
		_ = resp.Body.Close()
		if err != nil {
			return nil, err
		}
		if len(body) <= maxCachedBody {
			t.cache.put(key, &cacheEntry{etag: resp.Header.Get("ETag"), body: body, header: resp.Header.Clone()})
		}
		resp.Body = io.NopCloser(bytes.NewReader(body))
	}
	return resp, nil
}

// cacheKey binds an entry to the credential: GitHub's ETags depend on the Authorization header.
func cacheKey(req *http.Request) string {
	h := sha256.New()
	for _, s := range []string{req.Header.Get("Authorization"), req.URL.String(), req.Header.Get("Accept")} {
		h.Write([]byte(s))
		h.Write([]byte{0})
	}
	return string(h.Sum(nil))
}

type cacheEntry struct {
	etag   string
	body   []byte
	header http.Header
}

// etagCache is a bounded LRU of conditional-request entries.
type etagCache struct {
	mu    sync.Mutex
	max   int
	order *list.List // front = most recent; values are keys
	items map[string]*list.Element
	data  map[string]*cacheEntry
}

func newETagCache(max int) *etagCache {
	return &etagCache{max: max, order: list.New(), items: map[string]*list.Element{}, data: map[string]*cacheEntry{}}
}

func (c *etagCache) get(key string) *cacheEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		c.order.MoveToFront(el)
		return c.data[key]
	}
	return nil
}

func (c *etagCache) put(key string, e *cacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		c.order.MoveToFront(el)
	} else {
		c.items[key] = c.order.PushFront(key)
	}
	c.data[key] = e
	for c.order.Len() > c.max {
		oldest := c.order.Back()
		k := oldest.Value.(string) //nolint:forcetypeassert // only strings are stored
		c.order.Remove(oldest)
		delete(c.items, k)
		delete(c.data, k)
	}
}

// rateBook holds the last core rate-limit headers seen for one credential.
type rateBook struct {
	mu          sync.Mutex
	limit, left int
	reset       time.Time
	known       bool
}

func (r *rateBook) observe(h http.Header) {
	if res := h.Get("X-Ratelimit-Resource"); res != "" && res != "core" {
		return
	}
	limit, err1 := strconv.Atoi(h.Get("X-Ratelimit-Limit"))
	remaining, err2 := strconv.Atoi(h.Get("X-Ratelimit-Remaining"))
	reset, err3 := strconv.ParseInt(h.Get("X-Ratelimit-Reset"), 10, 64)
	if err1 != nil || err2 != nil || err3 != nil || limit <= 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.limit, r.left, r.reset, r.known = limit, remaining, time.Unix(reset, 0), true
}

// fraction returns the fraction left; 1 when no limit is known or the window has reset.
func (r *rateBook) fraction() float64 {
	f, _ := r.remaining()
	return f
}

// remaining returns the fraction left and the reset time; 1 when no limit is known or the window has reset.
func (r *rateBook) remaining() (float64, time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.known || !time.Now().Before(r.reset) { // at the reset instant the window is new
		return 1, time.Time{}
	}
	return float64(r.left) / float64(r.limit), r.reset
}
