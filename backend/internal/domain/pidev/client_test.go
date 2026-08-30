package pidev

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const minimalCatalog = `{"openai":{"gpt-export":{"id":"gpt-export","api":"openai-responses","provider":"openai"}}}`

// revisionFor computes the trusted "sha256-<hex>" revision for a body, the
// same way the client itself must validate the X-Pi-Model-Catalog-Revision
// header against the response it actually received.
func revisionFor(body string) string {
	sum := sha256.Sum256([]byte(body))
	return "sha256-" + hex.EncodeToString(sum[:])
}

func servingHandler(body string, extraHeaders map[string]string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if inm := r.Header.Get("If-None-Match"); inm != "" && inm == `"catalog-etag"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("ETag", `"catalog-etag"`)
		w.Header().Set("X-Pi-Model-Catalog-Revision", revisionFor(body))
		for key, value := range extraHeaders {
			w.Header().Set(key, value)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}
}

func newTestClient(t *testing.T, handler http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	client, err := NewClient(ClientOptions{BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client, server
}

func TestClientFetchAcceptsCorrectBodyRevision(t *testing.T) {
	client, _ := newTestClient(t, servingHandler(minimalCatalog, nil))
	catalog, err := client.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if catalog.Revision != revisionFor(minimalCatalog) {
		t.Fatalf("Revision = %q, want sha256 of body", catalog.Revision)
	}
	model, ok := catalog.Find("openai", "gpt-export")
	if !ok || model.API != "openai-responses" {
		t.Fatalf("Find(openai, gpt-export) = %+v, %v", model, ok)
	}
}

func TestClientFetchRejectsMissingRevisionHeader(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("ETag", `"catalog-etag"`)
		// Deliberately omit X-Pi-Model-Catalog-Revision.
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(minimalCatalog))
	})
	if _, err := client.Fetch(context.Background()); err == nil {
		t.Fatalf("fetch with no revision header must fail closed, not fall back to ETag")
	}
}

func TestClientFetchRejectsRevisionMismatchingBody(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("ETag", `"catalog-etag"`)
		w.Header().Set("X-Pi-Model-Catalog-Revision", revisionFor("{}"))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(minimalCatalog))
	})
	if _, err := client.Fetch(context.Background()); err == nil {
		t.Fatalf("a revision header that does not match the response body's SHA-256 must fail closed")
	}
}

func TestClientFetchNeverTrustsETagAsRevision(t *testing.T) {
	// A server that only ever advertises an ETag - never the revision header
	// - must never have that ETag silently promoted to a trusted revision.
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("ETag", revisionFor(minimalCatalog)) // looks right, but is only an ETag
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(minimalCatalog))
	})
	if _, err := client.Fetch(context.Background()); err == nil {
		t.Fatalf("ETag must never substitute for the X-Pi-Model-Catalog-Revision body-hash check")
	}
}

func TestClientFetch304RevalidatesCachedCatalog(t *testing.T) {
	var hits atomic.Int32
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		servingHandler(minimalCatalog, nil)(w, r)
	})
	first, err := client.Fetch(context.Background())
	if err != nil {
		t.Fatalf("first fetch: %v", err)
	}
	second, err := client.Fetch(context.Background())
	if err != nil {
		t.Fatalf("second fetch: %v", err)
	}
	if hits.Load() != 2 {
		t.Fatalf("expected two HTTP round trips (200 then conditional), got %d", hits.Load())
	}
	if second.Revision != first.Revision {
		t.Fatalf("a 304 must keep serving the previously validated revision: %q vs %q", second.Revision, first.Revision)
	}
}

func TestClientFetchRejectsHTTPBaseURL(t *testing.T) {
	server := httptest.NewServer(servingHandler(minimalCatalog, nil))
	defer server.Close()
	if _, err := NewClient(ClientOptions{BaseURL: server.URL}); err == nil {
		t.Fatalf("a plain-http catalog URL must be rejected at construction")
	}
}

func TestClientFetchRejectsCrossOriginRedirect(t *testing.T) {
	offOrigin := httptest.NewTLSServer(servingHandler(minimalCatalog, nil))
	defer offOrigin.Close()
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, offOrigin.URL, http.StatusFound)
	})
	if _, err := client.Fetch(context.Background()); err == nil {
		t.Fatalf("a redirect leaving the configured origin must be rejected")
	}
}

func TestClientFetchRejectsHTMLBody(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<!doctype html><html><body>not json</body></html>"))
	})
	if _, err := client.Fetch(context.Background()); err == nil {
		t.Fatalf("an HTML response must be rejected even with a 200 status")
	}
}

func TestClientFetchRequiresApplicationJSONContentType(t *testing.T) {
	for name, contentType := range map[string]string{
		"missing":     "",
		"plain text":  "text/plain",
		"json suffix": "application/catalog+json",
	} {
		t.Run(name, func(t *testing.T) {
			client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if contentType != "" {
					w.Header().Set("Content-Type", contentType)
				}
				w.Header().Set("X-Pi-Model-Catalog-Revision", revisionFor(minimalCatalog))
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(minimalCatalog))
			})
			if _, err := client.Fetch(context.Background()); err == nil {
				t.Fatalf("content-type %q must be rejected", contentType)
			}
		})
	}
}

func TestClientFetchRejectsOversizedBody(t *testing.T) {
	oversized := `{"openai":{"gpt-export":{"id":"gpt-export","api":"openai-responses","provider":"openai","name":"` + strings.Repeat("x", MaxCatalogBytes+1) + `"}}}`
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Pi-Model-Catalog-Revision", revisionFor(oversized))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(oversized))
	})
	if _, err := client.Fetch(context.Background()); err == nil {
		t.Fatalf("a body over the 16 MiB budget must be rejected")
	}
}

func TestClientFetchRespectsContextDeadline(t *testing.T) {
	unblock := make(chan struct{})
	defer close(unblock)
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		<-unblock
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := client.Fetch(ctx); err == nil {
		t.Fatalf("a request past its context deadline must fail")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("fetch must respect the context deadline rather than waiting out the full transport timeout: %s", elapsed)
	}
}

func TestClientFetchRejectsVersionNewerThanPinnedTarget(t *testing.T) {
	client, _ := newTestClient(t, servingHandler(minimalCatalog, map[string]string{
		"X-Pi-Model-Catalog-Minimum-Version": "99.0.0",
	}))
	_, err := client.Fetch(context.Background())
	if err == nil {
		t.Fatalf("a catalog requiring a newer Pi than %s must be rejected", PiTargetVersion)
	}
}

func TestClientFetchAcceptsVersionAtOrBelowPinnedTarget(t *testing.T) {
	client, _ := newTestClient(t, servingHandler(minimalCatalog, map[string]string{
		"X-Pi-Model-Catalog-Minimum-Version": "0.1.0",
	}))
	if _, err := client.Fetch(context.Background()); err != nil {
		t.Fatalf("a minimum version at or below the pinned target must be accepted: %v", err)
	}
}

func TestClientFetchRejectsMalformedMinimumVersion(t *testing.T) {
	for _, version := range []string{".", "0..84.3", "0.84.3.", "0.84", "0.84.3.0", "+0.84.3", "-1.0.0", "999999999999999999999999.0.0"} {
		t.Run(version, func(t *testing.T) {
			client, _ := newTestClient(t, servingHandler(minimalCatalog, map[string]string{
				"X-Pi-Model-Catalog-Minimum-Version": version,
			}))
			if _, err := client.Fetch(context.Background()); err == nil {
				t.Fatalf("malformed minimum version %q must fail closed", version)
			}
		})
	}
}

func TestClientFallsBackToLastKnownGoodOnFailure(t *testing.T) {
	var failing atomic.Bool
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if failing.Load() {
			http.Error(w, "boom", http.StatusBadGateway)
			return
		}
		servingHandler(minimalCatalog, nil)(w, r)
	})
	good, err := client.Fetch(context.Background())
	if err != nil {
		t.Fatalf("first fetch: %v", err)
	}
	failing.Store(true)
	if _, err := client.Fetch(context.Background()); err == nil {
		t.Fatalf("a failing fetch must return an error so callers can decide to serve the LKG snapshot")
	}
	// Fetch itself reports the failure; callers are expected to fall back to
	// Snapshot() for last-known-good retention, mirroring how the export
	// source route already does this (piCatalogForRead).
	snapshot := client.Snapshot()
	if snapshot == nil || snapshot.Revision != good.Revision {
		t.Fatalf("Snapshot() must keep serving the last successfully validated catalog after a failed fetch")
	}
}

func TestClientSnapshotsDoNotExposeMutableCatalogFields(t *testing.T) {
	client, _ := newTestClient(t, servingHandler(minimalCatalog, nil))
	fetched, err := client.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	fetched.CheckedAt = time.Time{}
	fetched.Revision = "mutated"
	delete(fetched.Providers, "openai")
	snapshot := client.Snapshot()
	if snapshot.CheckedAt.IsZero() || snapshot.Revision != revisionFor(minimalCatalog) || snapshot.Providers["openai"] == nil {
		t.Fatalf("caller mutation leaked into cached snapshot: %+v", snapshot)
	}
}

func TestParseCatalogRejectsIDMismatch(t *testing.T) {
	body := `{"openai":{"gpt-export":{"id":"different-id","api":"openai-responses","provider":"openai"}}}`
	if _, err := parseCatalog([]byte(body)); err == nil {
		t.Fatalf("a model whose id does not match its map key must be rejected")
	}
}

func TestParseCatalogRejectsProviderMismatch(t *testing.T) {
	body := `{"openai":{"gpt-export":{"id":"gpt-export","api":"openai-responses","provider":"anthropic"}}}`
	if _, err := parseCatalog([]byte(body)); err == nil {
		t.Fatalf("a model whose inner provider does not match its outer provider key must be rejected")
	}
}

func TestParseCatalogRejectsMissingRequiredFields(t *testing.T) {
	cases := map[string]string{
		"missing id":       `{"openai":{"gpt-export":{"api":"openai-responses","provider":"openai"}}}`,
		"missing api":      `{"openai":{"gpt-export":{"id":"gpt-export","provider":"openai"}}}`,
		"missing provider": `{"openai":{"gpt-export":{"id":"gpt-export","api":"openai-responses"}}}`,
		"empty name":       `{"openai":{"gpt-export":{"id":"gpt-export","name":"","api":"openai-responses","provider":"openai"}}}`,
		"null provider":    `{"openai":null}`,
		"no models":        `{"openai":{}}`,
		"empty document":   `{}`,
		"not an object":    `[]`,
		"trailing value":   minimalCatalog + ` {}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parseCatalog([]byte(body)); err == nil {
				t.Fatalf("%s: expected a schema validation error", name)
			}
		})
	}
}

func TestParseCatalogRejectsNonPositiveLimits(t *testing.T) {
	for field, value := range map[string]string{"contextWindow": "0", "maxTokens": "-1"} {
		body := `{"openai":{"gpt-export":{"id":"gpt-export","api":"openai-responses","provider":"openai","` + field + `":` + value + `}}}`
		if _, err := parseCatalog([]byte(body)); err == nil {
			t.Fatalf("%s=%s must be rejected for Pi 0.84.3", field, value)
		}
	}
}

// qwen3Dot8FlashFixture is the pi.dev directory shape docs/product.md §4.20
// documents as the confirmed multi-candidate example: qwen3.8-flash has two
// exact Chat candidates, one exact Anthropic candidate, no Responses
// candidate, and one decoy under a different exact id that must never match.
const qwen3Dot8FlashFixture = `{
	"qwen-token-plan": {"qwen3.8-flash": {"id": "qwen3.8-flash", "api": "openai-completions", "provider": "qwen-token-plan"}},
	"qwen-token-plan-cn": {"qwen3.8-flash": {"id": "qwen3.8-flash", "api": "openai-completions", "provider": "qwen-token-plan-cn"}},
	"opencode-go": {"qwen3.8-flash": {"id": "qwen3.8-flash", "api": "anthropic-messages", "provider": "opencode-go"}},
	"openrouter": {"qwen/qwen3.8-flash": {"id": "qwen/qwen3.8-flash", "api": "openai-completions", "provider": "openrouter"}}
}`

func TestCatalogCandidatesExactCaseSensitiveIDPlusAPIOnly(t *testing.T) {
	providers, err := parseCatalog([]byte(qwen3Dot8FlashFixture))
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	catalog := &Catalog{Providers: providers}

	chatCandidates := catalog.Candidates("qwen3.8-flash", "openai-completions")
	if len(chatCandidates) != 2 {
		t.Fatalf("Chat candidates = %+v, want exactly qwen-token-plan and qwen-token-plan-cn", chatCandidates)
	}
	seen := map[string]bool{}
	for _, c := range chatCandidates {
		seen[c.ProviderID] = true
	}
	if !seen["qwen-token-plan"] || !seen["qwen-token-plan-cn"] {
		t.Fatalf("Chat candidates missing expected providers: %+v", chatCandidates)
	}

	anthropicCandidates := catalog.Candidates("qwen3.8-flash", "anthropic-messages")
	if len(anthropicCandidates) != 1 || anthropicCandidates[0].ProviderID != "opencode-go" {
		t.Fatalf("Anthropic candidates = %+v, want exactly one opencode-go", anthropicCandidates)
	}

	responsesCandidates := catalog.Candidates("qwen3.8-flash", "openai-responses")
	if len(responsesCandidates) != 0 {
		t.Fatalf("Responses candidates = %+v, want none", responsesCandidates)
	}
	if !catalog.HasExactID("qwen3.8-flash") {
		t.Fatalf("HasExactID must find qwen3.8-flash under its other APIs even though Responses has no candidate")
	}

	// openrouter/qwen/qwen3.8-flash must never match a search for the bare
	// id "qwen3.8-flash": matching is complete-id exact, never contains/path.
	for _, c := range catalog.Candidates("qwen3.8-flash", "openai-completions") {
		if c.ProviderID == "openrouter" {
			t.Fatalf("openrouter's qwen/qwen3.8-flash must never match a search for qwen3.8-flash: %+v", c)
		}
	}
	if _, ok := catalog.Find("openrouter", "qwen3.8-flash"); ok {
		t.Fatalf("openrouter has no model keyed exactly qwen3.8-flash")
	}
}

func TestParsePositiveIntRejectsFractionalAndNegative(t *testing.T) {
	for _, literal := range []string{"1.5", "-1", "abc"} {
		if _, err := parsePositiveInt(literal); err == nil {
			t.Fatalf("parsePositiveInt(%q) must fail", literal)
		}
	}
	value, err := parsePositiveInt(strconv.Itoa(1 << 20))
	if err != nil || value != 1<<20 {
		t.Fatalf("parsePositiveInt(1<<20) = %d, %v", value, err)
	}
}
