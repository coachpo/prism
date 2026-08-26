package modelsdev

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newTestClient(t *testing.T, handler http.Handler) (*Client, *httptest.Server) {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	client, err := NewClient(ClientOptions{BaseURL: server.URL, HTTPClient: server.Client(), Now: func() time.Time { return time.Unix(1700000000, 0).UTC() }})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client, server
}

func TestNewClientRejectsPlainHTTPBaseURL(t *testing.T) {
	_, err := NewClient(ClientOptions{BaseURL: "http://models.dev/api.json"})
	if err == nil {
		t.Fatal("plain http base URL must be rejected")
	}
	if _, err := NewClient(ClientOptions{BaseURL: DefaultCatalogURL}); err != nil {
		t.Fatalf("official base URL must be accepted: %v", err)
	}
}

func TestFetchRevalidatesWithETagAndKeepsCacheOn304(t *testing.T) {
	var requests atomic.Int32
	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if requests.Load() > 1 && r.Header.Get("If-None-Match") != `"cat-1"` {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if requests.Load() > 1 && r.Header.Get("If-None-Match") == `"cat-1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"cat-1"`)
		fmt.Fprint(w, fixtureCatalog)
	}))
	first, err := client.Fetch(context.Background())
	if err != nil {
		t.Fatalf("first fetch: %v", err)
	}
	firstFetchedAt := first.FetchedAt
	second, err := client.Fetch(context.Background())
	if err != nil {
		t.Fatalf("second fetch: %v", err)
	}
	if second != first {
		t.Fatal("304 must return the identical cached snapshot")
	}
	if !second.FetchedAt.Equal(firstFetchedAt) {
		t.Fatal("304 must keep the original fetched_at")
	}
}

func TestFetchSingleFlightsConcurrentCallers(t *testing.T) {
	var upstreamHits atomic.Int32
	release := make(chan struct{})
	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		<-release
		w.Header().Set("ETag", `"cat-1"`)
		fmt.Fprint(w, fixtureCatalog)
	}))
	const callers = 8
	type fetchOutcome struct {
		catalog *Catalog
		err     error
	}
	outcomes := make([]chan fetchOutcome, callers)
	var wg sync.WaitGroup
	for i := range callers {
		outcomes[i] = make(chan fetchOutcome, 1)
		wg.Add(1)
		go func(outcome chan<- fetchOutcome) {
			defer wg.Done()
			catalog, err := client.Fetch(context.Background())
			outcome <- fetchOutcome{catalog: catalog, err: err}
		}(outcomes[i])
	}
	// Wait until every caller joined the in-flight request, then release.
	deadline := time.After(5 * time.Second)
	for upstreamHits.Load() < 1 {
		select {
		case <-deadline:
			t.Fatal("upstream never hit")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	close(release)
	wg.Wait()
	first := <-outcomes[0]
	if first.err != nil {
		t.Fatalf("caller 0: %v", first.err)
	}
	for i := 1; i < callers; i++ {
		outcome := <-outcomes[i]
		if outcome.err != nil {
			t.Fatalf("caller %d: %v", i, outcome.err)
		}
		if outcome.catalog != first.catalog {
			t.Fatalf("caller %d received a different catalog instance", i)
		}
	}
	if hits := upstreamHits.Load(); hits != 1 {
		t.Fatalf("expected exactly one upstream round trip, got %d", hits)
	}
}

func TestFetchRejectsOversizedBody(t *testing.T) {
	payload := "{" + strings.Repeat(" ", MaxCatalogBytes) + "}"
	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, payload)
	}))
	if _, err := client.Fetch(context.Background()); err == nil {
		t.Fatal("oversized body must fail closed")
	}
}

func TestRedirectLeavingOriginIsRejected(t *testing.T) {
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("cross-origin redirect target must never be contacted")
	}))
	defer target.Close()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/api.json", http.StatusFound)
	}))
	defer server.Close()
	client, err := NewClient(ClientOptions{BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := client.Fetch(context.Background()); err == nil {
		t.Fatal("cross-origin redirect must fail closed")
	} else if !strings.Contains(err.Error(), "left origin") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRedirectToPlainHTTPIsRejected(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://"+r.Host+r.URL.Path, http.StatusMovedPermanently)
	}))
	defer server.Close()
	client, err := NewClient(ClientOptions{BaseURL: server.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := client.Fetch(context.Background()); err == nil {
		t.Fatal("https→http downgrade redirect must fail closed")
	}
}

func TestSameOriginRedirectIsFollowed(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/moved/api.json" {
			w.Header().Set("ETag", `"cat-1"`)
			fmt.Fprint(w, fixtureCatalog)
			return
		}
		http.Redirect(w, r, server.URL+"/moved/api.json", http.StatusFound)
	}))
	defer server.Close()
	client, err := NewClient(ClientOptions{BaseURL: server.URL + "/api.json", HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	catalog, err := client.Fetch(context.Background())
	if err != nil {
		t.Fatalf("same-origin redirect must succeed: %v", err)
	}
	if len(catalog.Providers) == 0 {
		t.Fatal("redirect target content missing")
	}
}

func TestSnapshotHasNoNetworkIO(t *testing.T) {
	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, fixtureCatalog)
	}))
	if client.Snapshot() != nil {
		t.Fatal("fresh client must have no snapshot")
	}
	if _, err := client.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if client.Snapshot() == nil || client.CurrentRevision() != "" {
		t.Fatal("snapshot must be populated after fetch; fixture carries no etag so revision stays empty")
	}
}
