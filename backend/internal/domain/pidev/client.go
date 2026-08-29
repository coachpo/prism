package pidev

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
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
	transport := &http.Client{Timeout: RequestTimeout}
	if options.HTTPClient != nil {
		copied := *options.HTTPClient
		transport = &copied
		if transport.Timeout == 0 {
			transport.Timeout = RequestTimeout
		}
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
	return cloneCatalog(c.cached)
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
	return cloneCatalog(catalog), nil
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
	switch {
	case resp.StatusCode == http.StatusNotModified:
		if current == nil {
			return nil, fmt.Errorf("%w: 304 without cached snapshot", ErrCatalogUnavailable)
		}
		// Publish a new top-level snapshot. The provider/model graph remains
		// immutable after a successful parse, so readers never observe a field
		// mutation on a shared Catalog pointer.
		current.CheckedAt = c.now().UTC()
		c.mu.Lock()
		c.cached = current
		c.mu.Unlock()
		return current, nil
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("%w: unexpected status %d", ErrCatalogUnavailable, resp.StatusCode)
	}
	contentType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || !strings.EqualFold(mediaType, "application/json") {
		return nil, fmt.Errorf("%w: unexpected content-type %q; application/json is required", ErrCatalogUnavailable, contentType)
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
	now := c.now().UTC()
	catalog := &Catalog{
		ETag:           etag,
		Revision:       revision,
		MinimumVersion: minVersion,
		LastModified:   lastModified,
		FetchedAt:      now,
		CheckedAt:      now,
		Providers:      providers,
	}
	c.mu.Lock()
	c.cached = catalog
	c.mu.Unlock()
	return catalog, nil
}

func cloneCatalog(catalog *Catalog) *Catalog {
	if catalog == nil {
		return nil
	}
	clone := *catalog
	clone.Providers = make(map[string]*Provider, len(catalog.Providers))
	for providerID, provider := range catalog.Providers {
		providerClone := &Provider{ID: provider.ID, Models: make(map[string]*Model, len(provider.Models))}
		for modelID, model := range provider.Models {
			providerClone.Models[modelID] = cloneModel(model)
		}
		clone.Providers[providerID] = providerClone
	}
	return &clone
}

func cloneModel(model *Model) *Model {
	if model == nil {
		return nil
	}
	clone := *model
	clone.Name = cloneString(model.Name)
	clone.Reasoning = cloneBool(model.Reasoning)
	clone.Input = append([]string(nil), model.Input...)
	clone.ContextWindow = cloneInt64(model.ContextWindow)
	clone.MaxTokens = cloneInt64(model.MaxTokens)
	clone.ThinkingLevelMap = make(map[string]*string, len(model.ThinkingLevelMap))
	for level, value := range model.ThinkingLevelMap {
		clone.ThinkingLevelMap[level] = cloneString(value)
	}
	if model.ThinkingLevelMap == nil {
		clone.ThinkingLevelMap = nil
	}
	clone.Compat = cloneAnyMap(model.Compat)
	clone.DroppedFields = append([]string(nil), model.DroppedFields...)
	return &clone
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}

func cloneBool(value *bool) *bool {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}

func cloneAnyMap(value map[string]any) map[string]any {
	if value == nil {
		return nil
	}
	clone := make(map[string]any, len(value))
	for key, item := range value {
		clone[key] = cloneAny(item)
	}
	return clone
}

func cloneAny(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneAnyMap(typed)
	case []any:
		clone := make([]any, len(typed))
		for index, item := range typed {
			clone[index] = cloneAny(item)
		}
		return clone
	default:
		return value
	}
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
	for i := range targetParts {
		if minParts[i] > targetParts[i] {
			return fmt.Errorf("catalog requires Pi %s but this Prism pins %s", minVersion, PiTargetVersion)
		}
		if minParts[i] < targetParts[i] {
			return nil
		}
	}
	return nil
}

func parseSemver(v string) []int {
	parts := strings.Split(strings.TrimSpace(v), ".")
	if len(parts) != 3 {
		return nil
	}
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			return nil
		}
		for _, r := range p {
			if r < '0' || r > '9' {
				return nil
			}
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil
		}
		out = append(out, n)
	}
	return out
}
