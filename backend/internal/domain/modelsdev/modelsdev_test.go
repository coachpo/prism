package modelsdev

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const fixtureCatalog = `{
  "openai": {
    "id": "openai",
    "name": "OpenAI",
    "models": {
      "gpt-test": {
        "id": "gpt-test",
        "name": "GPT Test",
        "description": "fixture model",
        "family": "gpt-test",
        "attachment": false,
        "reasoning": true,
        "tool_call": true,
        "structured_output": true,
        "temperature": true,
        "knowledge": "2025-05",
        "release_date": "2026-01-15",
        "last_updated": "2026-02-20",
        "modalities": {"input": ["text", "image"], "output": ["text"]},
        "open_weights": false,
        "limit": {"context": 128000, "input": 100000, "output": 16384},
        "cost": {"input": 2.50, "output": 10, "cache_read": 0, "cache_write": 1.25}
      },
      "gpt-long": {
        "id": "gpt-long",
        "name": "GPT Long",
        "release_date": "2026-03",
        "last_updated": "2026-03",
        "open_weights": false,
        "limit": {"context": 400000, "output": 32768},
        "cost": {
          "input": 30, "output": 180,
          "tiers": [{"input": 60, "output": 270, "tier": {"type": "context", "size": 272000}}],
          "context_over_200k": {"input": 60, "output": 270}
        }
      },
      "gpt-tiered-cache": {
        "id": "gpt-tiered-cache",
        "name": "GPT Tiered Cache",
        "release_date": "2026-03",
        "last_updated": "2026-03",
        "open_weights": false,
        "cost": {
          "input": 4, "output": 20, "cache_read": 0.4, "cache_write": 5,
          "tiers": [{"input": 8, "output": 30, "cache_read": 0.8, "cache_write": 10, "tier": {"type": "context", "size": 272000}}]
        }
      },
      "gpt-audio": {
        "id": "gpt-audio",
        "name": "GPT Audio",
        "release_date": "2026-03",
        "last_updated": "2026-03",
        "open_weights": false,
        "cost": {"input": 5, "output": 20, "input_audio": 10, "output_audio": 40}
      }
    }
  },
  "anthropic": {
    "id": "anthropic",
    "name": "Anthropic",
    "models": {
      "claude-test": {
        "id": "claude-test",
        "name": "Claude Test",
        "release_date": "2026-04",
        "last_updated": "2026-04",
        "open_weights": false,
        "cost": {"input": 3, "output": 15, "cache_write": 3.75}
      },
      "shared-model": {
        "id": "shared-model",
        "name": "Shared Model",
        "release_date": "2026-04",
        "last_updated": "2026-04",
        "open_weights": true,
        "cost": {"input": 1, "output": 2}
      }
    }
  },
  "azure": {
    "id": "azure",
    "name": "Azure",
    "models": {
      "shared-model": {
        "id": "shared-model",
        "name": "Shared Model on Azure",
        "release_date": "2026-04",
        "last_updated": "2026-04",
        "open_weights": false,
        "cost": {"input": 1, "output": 2}
      }
    }
  },
  "google": {
    "id": "google",
    "name": "Google",
    "models": {
      "gemini-test": {
        "id": "gemini-test",
        "name": "Gemini Test",
        "release_date": "2026-02",
        "last_updated": "2026-02",
        "open_weights": true,
        "status": "deprecated",
        "cost": {"input": 1.25, "output": 10}
      }
    }
  }
}`

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

func TestFetchParsesAndValidatesSchema(t *testing.T) {
	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"cat-1"`)
		fmt.Fprint(w, fixtureCatalog)
	}))
	catalog, err := client.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if catalog.ETag != `"cat-1"` {
		t.Fatalf("etag = %q", catalog.ETag)
	}
	model, ok := catalog.Find("openai", "gpt-test")
	if !ok {
		t.Fatal("gpt-test missing")
	}
	if model.Name != "GPT Test" || model.Description == nil || *model.Description != "fixture model" {
		t.Fatalf("unexpected metadata: %+v", model)
	}
	if model.Limit.Context == nil || *model.Limit.Context != 128000 {
		t.Fatalf("context limit = %v", model.Limit.Context)
	}
	if model.Cost.Base.Input != "2.5" || model.Cost.Base.Output != "10" {
		t.Fatalf("price canonicalization broke literals: %q/%q", model.Cost.Base.Input, model.Cost.Base.Output)
	}
	if model.Cost.Base.CachedInput == nil || *model.Cost.Base.CachedInput != "0" {
		t.Fatalf("explicit zero cache_read must stay \"0\": %v", model.Cost.Base.CachedInput)
	}
	if model.Cost.Base.CacheCreation == nil || *model.Cost.Base.CacheCreation != "1.25" {
		t.Fatalf("cache_write = %v", model.Cost.Base.CacheCreation)
	}
}

func TestFetchRejectsSchemaViolations(t *testing.T) {
	cases := map[string]string{
		"top level array":       `[{"models":{}}]`,
		"model not object":      `{"p":{"models":{"m":42}}}`,
		"id mismatch":           `{"p":{"models":{"m":{"id":"other"}}}}`,
		"bad status enum":       `{"p":{"models":{"m":{"status":"sunset"}}}}`,
		"bad modality":          `{"p":{"models":{"m":{"modalities":{"input":["smell"]}}}}}`,
		"fractional limit":      `{"p":{"models":{"m":{"limit":{"context":12.5}}}}}`,
		"negative price":        `{"p":{"models":{"m":{"cost":{"input":-1,"output":2}}}}}`,
		"exponent price":        `{"p":{"models":{"m":{"cost":{"input":1e3,"output":2}}}}}`,
		"input without output":  `{"p":{"models":{"m":{"cost":{"input":1}}}}}`,
		"tier missing size":     `{"p":{"models":{"m":{"cost":{"input":1,"output":2,"tiers":[{"type":"context"}]}}}}}`,
		"date format violated":  `{"p":{"models":{"m":{"release_date":"tomorrow"}}}}`,
		"empty provider id":     `{"":{"models":{}}}`,
		"context_over_200k bad": `{"p":{"models":{"m":{"cost":{"input":1,"output":2,"context_over_200k":{"input":"x","output":"y"}}}}}}`,
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, payload)
			}))
			if _, err := client.Fetch(context.Background()); err == nil {
				t.Fatal("expected schema rejection")
			}
		})
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

func TestExactMatchesUniquenessRules(t *testing.T) {
	catalog := loadFixtureCatalog(t)
	openaiMatches := ExactMatches(catalog, "openai", "gpt-test")
	if len(openaiMatches) != 1 || openaiMatches[0].ProviderID != "openai" {
		t.Fatalf("unique openai match expected, got %+v", openaiMatches)
	}
	if shared := ExactMatches(catalog, "anthropic", "shared-model"); len(shared) != 1 {
		t.Fatalf("anthropic scope must ignore azure's shared-model, got %+v", shared)
	}
	if matches := ExactMatches(catalog, "unknown-family", "gpt-test"); len(matches) != 0 {
		t.Fatalf("unmapped family must have no candidates, got %+v", matches)
	}
	if matches := ExactMatches(catalog, "openai", ""); len(matches) != 0 {
		t.Fatalf("blank id must match nothing, got %+v", matches)
	}
	if matches := ExactMatches(nil, "openai", "gpt-test"); matches != nil {
		t.Fatal("nil catalog yields no candidates")
	}
}

func TestSearchCandidatesBoundedAndScoped(t *testing.T) {
	catalog := loadFixtureCatalog(t)
	familyItems, familyTotal := SearchCandidates(catalog, "gemini", "", "family", 20, 0)
	if familyTotal != 1 || len(familyItems) != 1 || familyItems[0].ModelID != "gemini-test" {
		t.Fatalf("gemini family search = %+v total %d", familyItems, familyTotal)
	}
	allItems, allTotal := SearchCandidates(catalog, "openai", "shared", "all", 20, 0)
	if allTotal != 2 || len(allItems) != 2 {
		t.Fatalf("all-scope shared search = %+v total %d", allItems, allTotal)
	}
	page, pageTotal := SearchCandidates(catalog, "openai", "", "all", 2, 0)
	if len(page) != 2 || pageTotal <= 2 {
		t.Fatalf("bounded page = %d items, total %d", len(page), pageTotal)
	}
	overflow, overflowTotal := SearchCandidates(catalog, "openai", "", "all", 20, 999)
	if len(overflow) != 0 || overflowTotal != pageTotal {
		t.Fatalf("offset beyond end must be empty with stable total: %d vs %d", overflowTotal, pageTotal)
	}
	huge, _ := SearchCandidates(catalog, "openai", "", "all", 5000, 0)
	if len(huge) > 100 {
		t.Fatalf("candidate limit must clamp to 100, got %d", len(huge))
	}
}

func TestBuildPricePlanStandardMapping(t *testing.T) {
	catalog := loadFixtureCatalog(t)
	model, _ := catalog.Find("anthropic", "claude-test")
	plan := BuildPricePlan(Offering{ProviderID: "anthropic", ModelID: "claude-test"}, model, "USD")
	if !plan.Committable() || plan.Kind != "standard" {
		t.Fatalf("plan = %+v", plan)
	}
	card := plan.Cards[RoleStandard]
	if card.InputPrice != "3" || card.OutputPrice != "15" {
		t.Fatalf("base card = %+v", card)
	}
	if card.CachedInputPrice != nil {
		t.Fatal("absent cache_read must map to null")
	}
	if card.CacheCreationPrice == nil || *card.CacheCreationPrice != "3.75" {
		t.Fatalf("cache_write must map to cache_creation_price: %v", card.CacheCreationPrice)
	}
	if card.ReasoningPrice != nil {
		t.Fatal("absent reasoning must map to null")
	}
}

func TestBuildPricePlanOpenAISingleContextTierMapsSizeVerbatim(t *testing.T) {
	catalog := loadFixtureCatalog(t)
	model, _ := catalog.Find("openai", "gpt-long")
	plan := BuildPricePlan(Offering{ProviderID: "openai", ModelID: "gpt-long"}, model, "USD")
	if !plan.Committable() || plan.Kind != "tiered" {
		t.Fatalf("plan = %+v", plan)
	}
	if plan.TierThreshold == nil || *plan.TierThreshold != 272000 {
		t.Fatalf("tier size must land verbatim in the threshold: %v", plan.TierThreshold)
	}
	base, above := plan.Cards[RoleTierBase], plan.Cards[RoleTierAbove]
	if base.InputPrice != "30" || above.InputPrice != "60" || above.OutputPrice != "270" {
		t.Fatalf("cards = %+v / %+v", base, above)
	}
	if base.CachedInputPrice != nil || above.CachedInputPrice != nil {
		t.Fatal("both cards omit cache_read so both must stay null")
	}

	cachedModel, _ := catalog.Find("openai", "gpt-tiered-cache")
	cachedPlan := BuildPricePlan(Offering{ProviderID: "openai", ModelID: "gpt-tiered-cache"}, cachedModel, "USD")
	if !cachedPlan.Committable() {
		t.Fatalf("configured specialty shape must stay committable: %+v", cachedPlan.Incompatibilities)
	}
	cachedBase, cachedAbove := cachedPlan.Cards[RoleTierBase], cachedPlan.Cards[RoleTierAbove]
	if cachedBase.CacheCreationPrice == nil || *cachedBase.CacheCreationPrice != "5" {
		t.Fatalf("base cache_write = %v", cachedBase.CacheCreationPrice)
	}
	if cachedAbove.CachedInputPrice == nil || *cachedAbove.CachedInputPrice != "0.8" {
		t.Fatalf("tier cache_read = %v", cachedAbove.CachedInputPrice)
	}
}

func TestBuildPricePlanFailClosedReasons(t *testing.T) {
	catalog := loadFixtureCatalog(t)
	cases := []struct {
		name             string
		provider         string
		modelID          string
		currency         string
		wantIncompatible bool
		wantReason       string
	}{
		{name: "audio cost", provider: "openai", modelID: "gpt-audio", currency: "USD", wantIncompatible: true, wantReason: ReasonAudioCostPresent},
		{name: "non-USD reporting currency", provider: "openai", modelID: "gpt-test", currency: "CNY", wantIncompatible: true, wantReason: ReasonReportingCurrencyNotUSD},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			model, found := catalog.Find(testCase.provider, testCase.modelID)
			if !found {
				t.Fatal("model missing from fixture")
			}
			plan := BuildPricePlan(Offering{ProviderID: testCase.provider, ModelID: testCase.modelID}, model, testCase.currency)
			if plan.Committable() == testCase.wantIncompatible {
				t.Fatalf("committable=%v but wantIncompatible=%v (%+v)", plan.Committable(), testCase.wantIncompatible, plan.Incompatibilities)
			}
			foundReason := false
			for _, item := range plan.Incompatibilities {
				if item.Reason == testCase.wantReason {
					foundReason = true
				}
			}
			if !foundReason {
				t.Fatalf("reason %s missing from %+v", testCase.wantReason, plan.Incompatibilities)
			}
		})
	}
	t.Run("missing cost", func(t *testing.T) {
		plan := BuildPricePlan(Offering{ProviderID: "openai", ModelID: "no-cost"}, &Model{ProviderID: "openai", ModelID: "no-cost"}, "USD")
		if plan.Committable() {
			t.Fatal("cost-less model must fail closed")
		}
	})
	t.Run("nil model", func(t *testing.T) {
		plan := BuildPricePlan(Offering{ProviderID: "openai", ModelID: "gone"}, nil, "USD")
		if plan.Committable() {
			t.Fatal("nil model must fail closed")
		}
	})
	t.Run("multiple tiers", func(t *testing.T) {
		model := &Model{Cost: &Cost{Base: TierPrices{Input: "1", Output: "2"}, Tiers: []CostTier{
			{Type: "context", Size: 100, Prices: TierPrices{Input: "2", Output: "3"}},
			{Type: "context", Size: 200, Prices: TierPrices{Input: "3", Output: "4"}},
		}}}
		plan := BuildPricePlan(Offering{ProviderID: "openai", ModelID: "multi"}, model, "USD")
		if hasReason(plan, ReasonMultipleTiers) == false {
			t.Fatalf("multiple tiers reason missing: %+v", plan.Incompatibilities)
		}
	})
	t.Run("non-openai context tier", func(t *testing.T) {
		model := &Model{Cost: &Cost{Base: TierPrices{Input: "1", Output: "2"}, Tiers: []CostTier{
			{Type: "context", Size: 272000, Prices: TierPrices{Input: "2", Output: "3"}},
		}}}
		plan := BuildPricePlan(Offering{ProviderID: "anthropic", ModelID: "tiered"}, model, "USD")
		if !hasReason(plan, ReasonTierNotSupported) {
			t.Fatalf("non-openai tier must fail closed: %+v", plan.Incompatibilities)
		}
	})
	t.Run("non-context tier type on openai", func(t *testing.T) {
		model := &Model{Cost: &Cost{Base: TierPrices{Input: "1", Output: "2"}, Tiers: []CostTier{
			{Type: "cache", Size: 1000, Prices: TierPrices{Input: "2", Output: "3"}},
		}}}
		plan := BuildPricePlan(Offering{ProviderID: "openai", ModelID: "cachetier"}, model, "USD")
		if !hasReason(plan, ReasonTierNotSupported) {
			t.Fatalf("non-context tier must fail closed: %+v", plan.Incompatibilities)
		}
	})
	t.Run("legacy shape alone", func(t *testing.T) {
		model := &Model{Cost: &Cost{
			Base:                     TierPrices{Input: "1", Output: "2"},
			LegacyContextOver200k:    &TierPrices{Input: "2", Output: "3"},
			HasLegacyContextOver200k: true,
		}}
		plan := BuildPricePlan(Offering{ProviderID: "openai", ModelID: "legacy"}, model, "USD")
		if !hasReason(plan, ReasonLegacyTierShape) {
			t.Fatalf("bare legacy tier evidence must fail closed: %+v", plan.Incompatibilities)
		}
	})
	t.Run("legacy conflicts explicit tier", func(t *testing.T) {
		model := &Model{Cost: &Cost{
			Base: TierPrices{Input: "1", Output: "2"},
			Tiers: []CostTier{
				{Type: "context", Size: 272000, Prices: TierPrices{Input: "2", Output: "3"}},
			},
			LegacyContextOver200k:    &TierPrices{Input: "9", Output: "9"},
			HasLegacyContextOver200k: true,
		}}
		plan := BuildPricePlan(Offering{ProviderID: "openai", ModelID: "conflict"}, model, "USD")
		if !hasReason(plan, ReasonTierEvidenceConflict) {
			t.Fatalf("conflicting duplicate evidence must fail closed: %+v", plan.Incompatibilities)
		}
	})
	t.Run("specialty shape mismatch between base and tier", func(t *testing.T) {
		model := &Model{Cost: &Cost{
			Base: TierPrices{Input: "1", Output: "2", CachedInput: pointer("0.5")},
			Tiers: []CostTier{
				{Type: "context", Size: 272000, Prices: TierPrices{Input: "2", Output: "3"}},
			},
		}}
		plan := BuildPricePlan(Offering{ProviderID: "openai", ModelID: "parity"}, model, "USD")
		if !hasReason(plan, ReasonSpecialtyShapeMismatch) {
			t.Fatalf("specialty mismatch must fail closed: %+v", plan.Incompatibilities)
		}
	})
}

func hasReason(plan PricePlan, reason string) bool {
	for _, item := range plan.Incompatibilities {
		if item.Reason == reason {
			return true
		}
	}
	return false
}

func pointer(value string) *string { return &value }

func loadFixtureCatalog(t *testing.T) *Catalog {
	t.Helper()
	providers, err := parseCatalog([]byte(fixtureCatalog))
	if err != nil {
		t.Fatalf("fixture catalog must validate: %v", err)
	}
	return &Catalog{ETag: `"fixture"`, FetchedAt: time.Unix(1700000000, 0).UTC(), Providers: providers}
}

func TestCanonicalPriceNormalizesLiterals(t *testing.T) {
	cases := map[string]string{
		"0":     "0",
		"0.0":   "0",
		"00":    "0",
		"2.50":  "2.5",
		"10":    "10",
		"0.028": "0.028",
		".5":    "0.5",
	}
	for raw, want := range cases {
		got, err := CanonicalPrice(raw)
		if err != nil || got != want {
			t.Fatalf("CanonicalPrice(%q) = %q, %v; want %q", raw, got, err, want)
		}
	}
	for _, raw := range []string{"-1", "1e3", "", "123456789012345678901234567890"} {
		if _, err := CanonicalPrice(raw); err == nil {
			t.Fatalf("CanonicalPrice(%q) must fail", raw)
		}
	}
}

func TestParseCatalogPreservesNumbersWithoutFloatRoundTrip(t *testing.T) {
	payload := `{"p":{"models":{"m":{"name":"m","cost":{"input":0.1,"output":0.30000000000000004},"limit":{"context":9999999999}}}}}`
	providers, err := parseCatalog([]byte(payload))
	if err != nil {
		t.Fatalf("parseCatalog: %v", err)
	}
	model := providers["p"].Models["m"]
	if model.Cost.Base.Output != "0.30000000000000004" {
		t.Fatalf("long decimal literal lost: %q", model.Cost.Base.Output)
	}
	if model.Limit.Context == nil || *model.Limit.Context != 9999999999 {
		t.Fatalf("large token counts must survive as int64: %v", model.Limit.Context)
	}
	raw, _ := json.Marshal(model.Cost.Base.Input)
	if string(raw) != `"0.1"` {
		t.Fatalf("canonical input should serialize back as a string, got %s", raw)
	}
}

func TestAutoMatchProviderIDsStable(t *testing.T) {
	if ids := AutoMatchProviderIDs("openai"); len(ids) != 1 || ids[0] != "openai" {
		t.Fatalf("openai mapping = %v", ids)
	}
	if ids := AutoMatchProviderIDs("gemini"); len(ids) != 1 || ids[0] != "google" {
		t.Fatalf("gemini mapping = %v", ids)
	}
	if ids := AutoMatchProviderIDs("custom"); len(ids) != 0 {
		t.Fatalf("unmapped families carry no providers: %v", ids)
	}
}
