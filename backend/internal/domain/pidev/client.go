package pidev

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

const (
	DefaultCatalogURL  = "https://pi.dev/api/models"
	DefaultCatalogHost = "pi.dev"
	MaxCatalogBytes    = 16 << 20 // 16 MiB
	RequestTimeout     = 4 * time.Second
	maxRedirects       = 5
)

var ErrCatalogUnavailable = errors.New("pi.dev catalog unavailable")
var ErrVersionTooNew = errors.New("pi.dev catalog requires newer Pi version")

type ClientOptions struct {
	BaseURL    string
	HTTPClient *http.Client
	Now        func() time.Time
}

type Client struct {
	baseURL  string
	baseHost string
	http     *http.Client
	now      func() time.Time

	mu     sync.Mutex
	cached *Catalog

	group singleflight.Group
}

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
	} else if transport.Timeout == 0 {
		transport.Timeout = RequestTimeout
	}
	client := &Client{
		baseURL:  baseURL,
		baseHost: host,
		now:      now,
		http:     transport,
	}
	// Ensure timeout is enforced even if caller provided transport without timeout.
	if client.http.Timeout == 0 {
		client.http.Timeout = RequestTimeout
	}
	client.http.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects {
			return fmt.Errorf("%w: redirect chain exceeds %d hops", ErrCatalogUnavailable, maxRedirects)
		}
		if req.URL.Scheme != "https" {
			return fmt.Errorf("%w: redirect to %q abandoned https", ErrCatalogUnavailable, req.URL.String())
		}
		if req.URL.Host != client.baseHost {
			return fmt.Errorf("%w: redirect to %q left origin %q", ErrCatalogUnavailable, req.URL.String(), client.baseHost)
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

func (c *Client) Snapshot() *Catalog {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cached
}

func (c *Client) CurrentRevision() string {
	snap := c.Snapshot()
	if snap == nil {
		return ""
	}
	return snap.Revision
}

// Fetch performs a network fetch with ETag/304, singleflight, limited body,
// HTTPS-only same-origin redirects, content-type check, schema and version
// validation, and LKG retention on failure. Callers outside transactions
// should call Fetch; callers inside transactions must only use Snapshot.
func (c *Client) Fetch(ctx context.Context) (*Catalog, error) {
	result, err, _ := c.group.Do("pidev-catalog", func() (any, error) {
		return c.fetchOnce(ctx)
	})
	if err != nil {
		return nil, err
	}
	catalog, _ := result.(*Catalog)
	if catalog == nil {
		return nil, fmt.Errorf("%w: singleflight returned no catalog", ErrCatalogUnavailable)
	}
	return catalog, nil
}

func (c *Client) fetchOnce(ctx context.Context) (*Catalog, error) {
	current := c.Snapshot()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: build request: %v", ErrCatalogUnavailable, err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "prism-pidev/1.0")
	if current != nil && current.ETag != "" {
		req.Header.Set("If-None-Match", current.ETag)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCatalogUnavailable, err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		_ = resp.Body.Close()
	}()
	if resp.Request == nil || resp.Request.URL == nil || resp.Request.URL.Scheme != "https" || resp.Request.URL.Host != c.baseHost {
		return nil, fmt.Errorf("%w: final URL left the configured https origin", ErrCatalogUnavailable)
	}
	// Content-type must be JSON, not HTML
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	if strings.Contains(ct, "text/html") {
		return nil, fmt.Errorf("%w: unexpected content-type %q (html is not allowed)", ErrCatalogUnavailable, resp.Header.Get("Content-Type"))
	}
	switch {
	case resp.StatusCode == http.StatusNotModified:
		if current == nil {
			return nil, fmt.Errorf("%w: 304 without cached snapshot", ErrCatalogUnavailable)
		}
		// Update CheckedAt but keep same catalog
		current.CheckedAt = c.now().UTC()
		return current, nil
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("%w: unexpected status %d", ErrCatalogUnavailable, resp.StatusCode)
	}
	if !strings.Contains(ct, "application/json") {
		// Some servers omit content-type; we still check body is JSON via parsing, but warn if html signature
		// We already rejected text/html, so allow missing.
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxCatalogBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: read body: %v", ErrCatalogUnavailable, err)
	}
	if len(body) > MaxCatalogBytes {
		return nil, fmt.Errorf("%w: body exceeds %d byte budget", ErrCatalogUnavailable, int64(MaxCatalogBytes))
	}
	// Quick HTML detection even if content-type lied
	trimmed := strings.TrimSpace(string(body))
	if strings.HasPrefix(strings.ToLower(trimmed), "<!doctype html") || strings.HasPrefix(strings.ToLower(trimmed), "<html") {
		return nil, fmt.Errorf("%w: body looks like HTML, not JSON", ErrCatalogUnavailable)
	}
	providers, err := parseCatalog(body)
	if err != nil {
		return nil, fmt.Errorf("%w: schema violation: %v", ErrCatalogUnavailable, err)
	}
	etag := strings.TrimSpace(resp.Header.Get("ETag"))
	revision := strings.TrimSpace(resp.Header.Get("X-Pi-Model-Catalog-Revision"))
	lastModified := strings.TrimSpace(resp.Header.Get("Last-Modified"))
	minVersion := strings.TrimSpace(resp.Header.Get("X-Pi-Model-Catalog-Minimum-Version"))
	// The trusted revision is always the response body's own SHA-256, never the
	// ETag: ETag is only a 304 revalidation token and is never trustworthy
	// evidence of content identity on its own. A missing or mismatched
	// revision header fails the fetch closed instead of silently trusting
	// whatever the transport-level ETag happened to say.
	bodySum := sha256.Sum256(body)
	expectedRevision := "sha256-" + hex.EncodeToString(bodySum[:])
	if revision == "" || revision != expectedRevision {
		return nil, fmt.Errorf("%w: catalog revision header %q does not match the response body's SHA-256 %q", ErrCatalogUnavailable, revision, expectedRevision)
	}
	if minVersion != "" {
		if err := validateMinimumVersion(minVersion); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrVersionTooNew, err)
		}
	}
	catalog := &Catalog{
		ETag:           etag,
		Revision:       revision,
		MinimumVersion: minVersion,
		LastModified:   lastModified,
		FetchedAt:      c.now().UTC(),
		CheckedAt:      c.now().UTC(),
		Providers:      providers,
	}
	c.mu.Lock()
	c.cached = catalog
	c.mu.Unlock()
	return catalog, nil
}

func validateMinimumVersion(minVersion string) error {
	// Compare semver: minVersion must be <= PiTargetVersion (0.84.3)
	// Use simple numeric compare: split by "."
	targetParts := parseSemver(PiTargetVersion)
	minParts := parseSemver(minVersion)
	if minParts == nil {
		return fmt.Errorf("invalid minimum version %q", minVersion)
	}
	// if min > target => require newer Pi
	for i := 0; i < len(targetParts) && i < len(minParts); i++ {
		if minParts[i] > targetParts[i] {
			return fmt.Errorf("catalog requires Pi %s but this Prism pins %s", minVersion, PiTargetVersion)
		}
		if minParts[i] < targetParts[i] {
			return nil
		}
	}
	if len(minParts) > len(targetParts) {
		// e.g., target 0.84.3 vs min 0.84.3.1 => min newer
		for _, p := range minParts[len(targetParts):] {
			if p > 0 {
				return fmt.Errorf("catalog requires Pi %s but this Prism pins %s", minVersion, PiTargetVersion)
			}
		}
	}
	return nil
}

func parseSemver(v string) []int {
	parts := strings.Split(strings.TrimSpace(v), ".")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		n, err := strconvAtoi(p)
		if err != nil {
			return nil
		}
		out = append(out, n)
	}
	return out
}

func strconvAtoi(s string) (int, error) {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("not numeric")
		}
		n = n*10 + int(r-'0')
	}
	return n, nil
}
