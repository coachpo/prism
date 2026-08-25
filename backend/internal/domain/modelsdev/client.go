package modelsdev

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// Catalog transport contract for the fixed official models.dev catalog.json.
const (
	// DefaultCatalogURL is the fixed official catalog document.
	DefaultCatalogURL = "https://models.dev/api.json"
	// DefaultCatalogHost is the only host the client is allowed to talk to,
	// including across redirects (same-origin redirect policy).
	DefaultCatalogHost = "models.dev"
	// MaxCatalogBytes caps the response body. One byte more fails closed so
	// a runaway mirror can never exhaust process memory.
	MaxCatalogBytes = 16 << 20 // 16 MiB
	// RequestTimeout bounds the whole exchange including body read.
	RequestTimeout = 10 * time.Second
	// maxRedirects bounds same-origin redirect chains.
	maxRedirects = 10
)

// ErrCatalogUnavailable marks every fetch failure (transport, limit, schema).
// Callers must treat it as "no fresh catalog data", never as a reason to
// silently serve stale data into a commit.
var ErrCatalogUnavailable = errors.New("models.dev catalog unavailable")

// ClientOptions configures the restricted catalog client. Tests override
// BaseURL (and may inject HTTPClient for httptest TLS fixtures); production
// wiring pins the official URL and leaves HTTPClient unset.
type ClientOptions struct {
	BaseURL string
	// HTTPClient replaces the default transport for tests only. The same-origin
	// redirect policy is applied to it regardless of the caller.
	HTTPClient *http.Client
	Now        func() time.Time
}

// Client fetches and caches the official models.dev catalog in-process.
// All network I/O happens through Fetch and never inside a database
// transaction; Snapshot exposes the last validated snapshot without I/O.
type Client struct {
	baseURL string
	// baseHost pins the single allowed origin: the initial request target
	// and every redirect hop must stay on this https host.
	baseHost string
	http     *http.Client
	now      func() time.Time

	mu     sync.Mutex
	cached *Catalog

	group singleflight.Group
}

// NewClient builds the restricted client: HTTPS enforced, redirects limited
// to the configured origin, whole-request timeout of RequestTimeout.
func NewClient(options ClientOptions) (*Client, error) {
	baseURL := options.BaseURL
	if strings.TrimSpace(baseURL) == "" {
		baseURL = DefaultCatalogURL
	}
	host, err := catalogHost(baseURL)
	if err != nil {
		return nil, err
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	transport := options.HTTPClient
	if transport == nil {
		transport = &http.Client{Timeout: RequestTimeout}
	}
	client := &Client{
		baseURL:  baseURL,
		baseHost: host,
		now:      now,
		http:     transport,
	}
	// Same-origin redirect policy: every hop must stay on the pinned https
	// origin the client was validated against.
	client.http.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects {
			return fmt.Errorf("%w: redirect chain exceeds %d hops", ErrCatalogUnavailable, maxRedirects)
		}
		if req.URL.Scheme != "https" {
			return fmt.Errorf("%w: redirect to %q abandoned the https scheme", ErrCatalogUnavailable, req.URL)
		}
		if req.URL.Host != client.baseHost {
			return fmt.Errorf("%w: redirect to %q left origin %q", ErrCatalogUnavailable, req.URL, client.baseHost)
		}
		return nil
	}
	return client, nil
}

func catalogHost(baseURL string) (string, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("%w: invalid catalog URL %q: %v", ErrCatalogUnavailable, baseURL, err)
	}
	if parsed.Scheme != "https" {
		return "", fmt.Errorf("%w: catalog URL %q must use https", ErrCatalogUnavailable, baseURL)
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("%w: catalog URL %q has no host", ErrCatalogUnavailable, baseURL)
	}
	return parsed.Host, nil
}

// Snapshot returns the current cached catalog without any network I/O. It is
// the only catalog source allowed inside transactions; commit paths must
// verify its ETag against the operator's preview revision before writing.
func (c *Client) Snapshot() *Catalog {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cached
}

// CurrentRevision returns the cached ETag ("" when no catalog was fetched yet).
func (c *Client) CurrentRevision() string {
	snapshot := c.Snapshot()
	if snapshot == nil {
		return ""
	}
	return snapshot.ETag
}

// Fetch returns the validated catalog. Concurrent callers share one HTTP
// round trip through a single-flight group. A conditional GET revalidates the
// cached snapshot via If-None-Match; a 304 keeps it. Transport failures and
// schema violations return errors and leave the previous snapshot untouched.
func (c *Client) Fetch(ctx context.Context) (*Catalog, error) {
	result, err, _ := c.group.Do("catalog", func() (any, error) {
		return c.fetchOnce(ctx)
	})
	if err != nil {
		return nil, err
	}
	catalog, _ := result.(*Catalog)
	if catalog == nil {
		return nil, fmt.Errorf("%w: single-flight returned no catalog", ErrCatalogUnavailable)
	}
	return catalog, nil
}

func (c *Client) fetchOnce(ctx context.Context) (*Catalog, error) {
	current := c.Snapshot()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: build request: %v", ErrCatalogUnavailable, err)
	}
	if current != nil && current.ETag != "" {
		request.Header.Set("If-None-Match", current.ETag)
	}
	response, err := c.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCatalogUnavailable, err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<16))
		_ = response.Body.Close()
	}()
	// Defense in depth: even with the redirect policy above, refuse any
	// final exchange that is not the configured https origin.
	if response.Request == nil || response.Request.URL == nil ||
		response.Request.URL.Scheme != "https" || response.Request.URL.Host != c.baseHost {
		return nil, fmt.Errorf("%w: final URL left the configured https origin", ErrCatalogUnavailable)
	}
	switch {
	case response.StatusCode == http.StatusNotModified:
		if current == nil {
			return nil, fmt.Errorf("%w: 304 without a cached snapshot", ErrCatalogUnavailable)
		}
		return current, nil
	case response.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("%w: unexpected status %d", ErrCatalogUnavailable, response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, MaxCatalogBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: read body: %v", ErrCatalogUnavailable, err)
	}
	if len(body) > MaxCatalogBytes {
		return nil, fmt.Errorf("%w: body exceeds the %d byte budget", ErrCatalogUnavailable, int64(MaxCatalogBytes))
	}
	providers, err := parseCatalog(body)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCatalogUnavailable, err)
	}
	etag := strings.TrimSpace(response.Header.Get("ETag"))
	catalog := &Catalog{ETag: etag, FetchedAt: c.now().UTC(), Providers: providers}
	c.mu.Lock()
	c.cached = catalog
	c.mu.Unlock()
	return catalog, nil
}
