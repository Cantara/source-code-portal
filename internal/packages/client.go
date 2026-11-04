package packages

import (
	"bytes"
	"container/list"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	gosync "sync"
	"time"
)

const (
	perHost      = 4                // requests in flight per registry host
	requestLimit = 30 * time.Second // one request, its body read included
	keepMax      = 1 << 20          // bytes of an answer kept for its validators; a larger one streams and is not kept
	cacheMax     = 32 << 20         // bytes of answers kept in all
	pauseBase    = time.Minute      // after a 429 or 503 without Retry-After; doubles to pauseMax
	pauseMax     = 15 * time.Minute
)

// ErrNotFound is a registry's answer that it does not hold what was asked for: the next registry's turn.
var ErrNotFound = errors.New("packages: not at this registry")

// RegistryError is a registry's failure: its host and status (0 when it could not be reached). The message never holds a credential.
type RegistryError struct {
	Host   string
	Status int
	Err    error
}

func (e *RegistryError) Error() string {
	if e.Status != 0 {
		return fmt.Sprintf("%s answered %d", e.Host, e.Status)
	}
	return fmt.Sprintf("%s: %v", e.Host, e.Err)
}

func (e *RegistryError) Unwrap() error { return e.Err }

// Client reads registries for every job: a limit of requests in flight per host, a pause per host after it asked to slow down,
// conditional requests from the answers it keeps, and a deadline per request. One slow or failing registry holds no other.
type Client struct {
	http *http.Client
	now  func() time.Time

	mu    gosync.Mutex
	hosts map[string]*host
	cache *cache
}

type host struct {
	slots chan struct{}
	until time.Time     // paused until
	wait  time.Duration // the next pause without Retry-After
}

// NewClient reads registries through transport (egress.Guarded for the public ones).
func NewClient(transport http.RoundTripper) *Client {
	return &Client{
		http: &http.Client{Transport: transport, CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return errors.New("too many redirects")
			}
			return nil
		}},
		now:   time.Now,
		hosts: map[string]*host{},
		cache: &cache{order: list.New(), byURL: map[string]*list.Element{}},
	}
}

// Get reads u with the Accept header accept and hands the body to read, which reads it as a stream. A 404 or 410 is ErrNotFound;
// any other failure a *RegistryError.
func (c *Client) Get(ctx context.Context, u, accept string, read func(io.Reader) error) error {
	pu, err := url.Parse(u)
	if err != nil || pu.Scheme != "https" || pu.Host == "" {
		return &RegistryError{Host: "registry", Err: fmt.Errorf("%q is not an https URL", u)}
	}
	h := c.host(pu.Host)
	if until := c.paused(h); !until.IsZero() {
		return &RegistryError{Host: pu.Host, Err: fmt.Errorf("paused until %s", until.UTC().Format(time.RFC3339))}
	}
	select {
	case h.slots <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-h.slots }()

	ctx, cancel := context.WithTimeout(ctx, requestLimit)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return &RegistryError{Host: pu.Host, Err: err}
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("User-Agent", "source-code-portal (package versions)")
	key := accept + " " + u
	kept, ok := c.cache.get(key)
	if ok {
		if kept.etag != "" {
			req.Header.Set("If-None-Match", kept.etag)
		}
		if kept.modified != "" {
			req.Header.Set("If-Modified-Since", kept.modified)
		}
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return &RegistryError{Host: pu.Host, Err: err}
	}
	defer func() { _ = resp.Body.Close() }()
	switch {
	case resp.StatusCode == http.StatusNotModified && ok:
		return read(bytes.NewReader(kept.body))
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return ErrNotFound
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable:
		c.pause(h, resp.Header.Get("Retry-After"))
		return &RegistryError{Host: pu.Host, Status: resp.StatusCode}
	case resp.StatusCode != http.StatusOK:
		return &RegistryError{Host: pu.Host, Status: resp.StatusCode}
	}
	c.resume(h)
	etag, modified := resp.Header.Get("ETag"), resp.Header.Get("Last-Modified")
	if etag == "" && modified == "" {
		return c.read(pu.Host, resp.Body, read)
	}
	head, err := io.ReadAll(io.LimitReader(resp.Body, keepMax+1))
	if err != nil {
		return &RegistryError{Host: pu.Host, Err: err}
	}
	if len(head) <= keepMax {
		c.cache.put(key, answer{etag: etag, modified: modified, body: head})
		return c.read(pu.Host, bytes.NewReader(head), read)
	}
	return c.read(pu.Host, io.MultiReader(bytes.NewReader(head), resp.Body), read)
}

// read hands body to the caller's reader; a broken connection is the registry's failure, anything else the reader's.
func (c *Client) read(hostName string, body io.Reader, read func(io.Reader) error) error {
	var broken error
	err := read(readerFunc(func(p []byte) (int, error) {
		n, err := body.Read(p)
		if err != nil && !errors.Is(err, io.EOF) {
			broken = err
		}
		return n, err
	}))
	if broken != nil {
		return &RegistryError{Host: hostName, Err: broken}
	}
	return err
}

type readerFunc func([]byte) (int, error)

func (f readerFunc) Read(p []byte) (int, error) { return f(p) }

func (c *Client) host(name string) *host {
	c.mu.Lock()
	defer c.mu.Unlock()
	h := c.hosts[name]
	if h == nil {
		h = &host{slots: make(chan struct{}, perHost), wait: pauseBase}
		c.hosts[name] = h
	}
	return h
}

func (c *Client) paused(h *host) time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.now().Before(h.until) {
		return h.until
	}
	return time.Time{}
}

// pause stops requests to h for its Retry-After (seconds or a date), else for a wait that doubles to pauseMax.
func (c *Client) pause(h *host, retryAfter string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	d := h.wait
	if s, err := strconv.Atoi(retryAfter); err == nil && s >= 0 {
		d = time.Duration(s) * time.Second
	} else if t, err := http.ParseTime(retryAfter); err == nil {
		d = t.Sub(now)
	} else {
		h.wait = min(h.wait*2, pauseMax)
	}
	h.until = now.Add(min(max(d, 0), pauseMax))
}

func (c *Client) resume(h *host) {
	c.mu.Lock()
	defer c.mu.Unlock()
	h.wait = pauseBase
}

// answer is a kept answer with its validators.
type answer struct {
	etag, modified string
	body           []byte
}

// cache keeps answers up to cacheMax bytes, the least recently used dropped first.
type cache struct {
	mu    gosync.Mutex
	order *list.List // of *cached, the most recent at the front
	byURL map[string]*list.Element
	size  int
}

type cached struct {
	key string
	answer
}

func (c *cache) get(key string) (answer, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.byURL[key]
	if !ok {
		return answer{}, false
	}
	c.order.MoveToFront(el)
	return el.Value.(*cached).answer, true //nolint:forcetypeassert // the only type stored
}

func (c *cache) put(key string, a answer) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.byURL[key]; ok {
		c.size -= len(el.Value.(*cached).body) //nolint:forcetypeassert // the only type stored
		c.order.Remove(el)
	}
	c.byURL[key] = c.order.PushFront(&cached{key: key, answer: a})
	c.size += len(a.body)
	for c.size > cacheMax {
		el := c.order.Back()
		old := el.Value.(*cached) //nolint:forcetypeassert // the only type stored
		c.order.Remove(el)
		delete(c.byURL, old.key)
		c.size -= len(old.body)
	}
}
