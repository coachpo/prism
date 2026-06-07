package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/coachpo/prism/backend/internal/platform/background"
	"github.com/coachpo/prism/backend/internal/platform/config"
)

func TestOverflowAffinityCacheExpiresEntries(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC)
	cache := newOverflowAffinityCache(func() time.Time { return now })
	key := overflowAffinityCacheKey{profileID: 42, affinityDigest: "digest", contextBucket: "unknown"}
	entry := overflowAffinityCacheEntry{promotionTargetID: "promoted-model"}

	cache.put(key, entry)
	stored, ok := cache.get(key)
	if !ok {
		t.Fatal("expected entry to be available immediately after put")
	}
	if !stored.expiresAt.Equal(now.Add(overflowAffinityCacheTTL)) {
		t.Fatalf("expected fixed TTL expiry, got %s", stored.expiresAt)
	}

	now = now.Add(overflowAffinityCacheTTL - time.Nanosecond)
	if _, ok := cache.get(key); !ok {
		t.Fatal("expected entry to remain available just before expiry")
	}

	now = now.Add(time.Nanosecond)
	if _, ok := cache.get(key); ok {
		t.Fatal("expected entry to expire exactly at TTL boundary")
	}
	if got := cache.sizeForTest(); got != 0 {
		t.Fatalf("expected expired entry to be pruned on read, got size %d", got)
	}
}

func TestOverflowAffinityCachePruneExpired(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC)
	cache := newOverflowAffinityCache(func() time.Time { return now })
	firstKey := overflowAffinityCacheKey{profileID: 1, affinityDigest: "first", contextBucket: "unknown"}
	secondKey := overflowAffinityCacheKey{profileID: 1, affinityDigest: "second", contextBucket: "unknown"}
	cache.put(firstKey, overflowAffinityCacheEntry{promotionTargetID: "first-target"})
	now = now.Add(overflowAffinityCacheTTL / 2)
	cache.put(secondKey, overflowAffinityCacheEntry{promotionTargetID: "second-target"})

	cache.pruneExpired(now.Add(overflowAffinityCacheTTL / 2))
	if _, ok := cache.get(firstKey); ok {
		t.Fatal("expected first entry to be pruned")
	}
	if _, ok := cache.get(secondKey); !ok {
		t.Fatal("expected second entry to remain")
	}
}

func TestOverflowAffinityCacheHashesAffinityHeaders(t *testing.T) {
	t.Parallel()

	rawAffinity := "session-affinity-raw-value"
	rawParent := "parent-session-raw-value"
	headers := http.Header{}
	headers.Add("X-Session-Affinity", "  "+rawAffinity+"  ")
	headers.Add("X-Parent-Session-Id", "\t"+rawParent+"\n")

	fallbackKey, fallbackMaterial, ok := buildOverflowAffinityCacheKey(testOverflowAffinityCacheKeyInput(headers, ""))
	if !ok {
		t.Fatal("expected valid affinity headers to build cache key")
	}
	expectedFallback := sha256.Sum256([]byte("42:" + rawAffinity))
	if fallbackKey.affinityDigest != hex.EncodeToString(expectedFallback[:]) {
		t.Fatalf("expected profile-scoped SHA-256 fallback digest, got %q", fallbackKey.affinityDigest)
	}

	secretKey, secretMaterial, ok := buildOverflowAffinityCacheKey(testOverflowAffinityCacheKeyInput(headers, "process-local-secret"))
	if !ok {
		t.Fatal("expected valid affinity headers with secret to build cache key")
	}
	if secretKey.affinityDigest == fallbackKey.affinityDigest {
		t.Fatal("expected HMAC digest to differ from fallback digest")
	}
	if len(secretMaterial.affinityHashPrefix) != overflowAffinityHashPrefixLength {
		t.Fatalf("expected affinity hash prefix length %d, got %d", overflowAffinityHashPrefixLength, len(secretMaterial.affinityHashPrefix))
	}
	if secretMaterial.parentHashPrefix == nil || len(*secretMaterial.parentHashPrefix) != overflowAffinityHashPrefixLength {
		t.Fatalf("expected parent hash prefix metadata, got %+v", secretMaterial.parentHashPrefix)
	}

	entry := buildOverflowAffinityCacheEntry(testOverflowAffinityCacheEntryInput(), secretMaterial)
	debug := fmt.Sprintf("%#v %#v %#v", secretKey, secretMaterial, entry)
	for _, rawValue := range []string{rawAffinity, rawParent} {
		if strings.Contains(debug, rawValue) {
			t.Fatalf("expected debug metadata to exclude raw header value %q: %s", rawValue, debug)
		}
	}
	if fallbackMaterial.affinityHashPrefix == rawAffinity {
		t.Fatal("expected affinity metadata prefix to be hashed")
	}
}

func TestOverflowAffinityCacheMultipleHeaderHandling(t *testing.T) {
	t.Parallel()

	t.Run("missing disables", func(t *testing.T) {
		t.Parallel()
		if _, _, ok := buildOverflowAffinityCacheKey(testOverflowAffinityCacheKeyInput(http.Header{}, "")); ok {
			t.Fatal("expected missing affinity to disable key creation")
		}
	})
	t.Run("empty disables", func(t *testing.T) {
		t.Parallel()
		headers := http.Header{}
		headers.Add("x-session-affinity", " \t ")
		if _, _, ok := buildOverflowAffinityCacheKey(testOverflowAffinityCacheKeyInput(headers, "")); ok {
			t.Fatal("expected empty affinity to disable key creation")
		}
	})
	t.Run("oversized disables", func(t *testing.T) {
		t.Parallel()
		headers := http.Header{}
		headers.Add("x-session-affinity", strings.Repeat("x", overflowAffinityMaxHeaderBytes+1))
		if _, _, ok := buildOverflowAffinityCacheKey(testOverflowAffinityCacheKeyInput(headers, "")); ok {
			t.Fatal("expected oversized affinity to disable key creation")
		}
	})
	t.Run("distinct disables", func(t *testing.T) {
		t.Parallel()
		headers := http.Header{}
		headers.Add("x-session-affinity", "first")
		headers.Add("X-Session-Affinity", "second")
		if _, _, ok := buildOverflowAffinityCacheKey(testOverflowAffinityCacheKeyInput(headers, "")); ok {
			t.Fatal("expected multiple distinct affinity values to disable key creation")
		}
	})
	t.Run("duplicates normalize", func(t *testing.T) {
		t.Parallel()
		single := http.Header{}
		single.Add("x-session-affinity", "same")
		duplicates := http.Header{}
		duplicates.Add("x-session-affinity", " same ")
		duplicates.Add("X-Session-Affinity", "same")

		singleKey, _, singleOK := buildOverflowAffinityCacheKey(testOverflowAffinityCacheKeyInput(single, ""))
		duplicateKey, _, duplicateOK := buildOverflowAffinityCacheKey(testOverflowAffinityCacheKeyInput(duplicates, ""))
		if !singleOK || !duplicateOK {
			t.Fatal("expected duplicate matching affinity values to build cache keys")
		}
		if singleKey.affinityDigest != duplicateKey.affinityDigest {
			t.Fatalf("expected duplicate matching affinity values to use same digest, got %q and %q", singleKey.affinityDigest, duplicateKey.affinityDigest)
		}
	})
}

func TestOverflowAffinityCacheGenerationMismatchBypassesEntry(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC)
	cache := newOverflowAffinityCache(func() time.Time { return now })
	headers := http.Header{}
	headers.Add("x-session-affinity", "shared-affinity")

	firstVector := RuntimeGenerationVector{"runtime_planning:global:*": 1}
	secondVector := RuntimeGenerationVector{"runtime_planning:global:*": 2}
	firstInput := testOverflowAffinityCacheKeyInput(headers, "secret")
	firstInput.routingGenerationToken = runtimeGenerationVectorToken(firstVector)
	firstKey, material, ok := buildOverflowAffinityCacheKey(firstInput)
	if !ok {
		t.Fatal("expected first generation cache key")
	}
	entryInput := testOverflowAffinityCacheEntryInput()
	entryInput.generationToken = firstInput.routingGenerationToken
	cache.put(firstKey, buildOverflowAffinityCacheEntry(entryInput, material))

	secondInput := firstInput
	secondInput.routingGenerationToken = runtimeGenerationVectorToken(secondVector)
	secondKey, _, ok := buildOverflowAffinityCacheKey(secondInput)
	if !ok {
		t.Fatal("expected second generation cache key")
	}
	if firstKey == secondKey {
		t.Fatal("expected generation token to participate in cache key equality")
	}
	if _, ok := cache.get(secondKey); ok {
		t.Fatal("expected changed generation token to bypass stale cache entry")
	}
	if _, ok := cache.get(firstKey); !ok {
		t.Fatal("expected original generation token to still retrieve stored entry")
	}
}

func TestOverflowAffinityCacheContextBucket(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		estimation *requestContextEstimation
		want       string
	}{
		{name: "unknown", want: "unknown"},
		{name: "zero", estimation: &requestContextEstimation{EstimatedTotalContextTokens: 0}, want: "0-16383"},
		{name: "end first bucket", estimation: &requestContextEstimation{EstimatedTotalContextTokens: 16383}, want: "0-16383"},
		{name: "start second bucket", estimation: &requestContextEstimation{EstimatedTotalContextTokens: 16384}, want: "16384-32767"},
		{name: "negative unavailable", estimation: &requestContextEstimation{EstimatedTotalContextTokens: -1}, want: "unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := overflowAffinityContextBucket(tc.estimation); got != tc.want {
				t.Fatalf("expected context bucket %q, got %q", tc.want, got)
			}
		})
	}
}

func TestOverflowAffinityCacheParentSupplementalMetadata(t *testing.T) {
	t.Parallel()

	parentOne := http.Header{}
	parentOne.Add("x-session-affinity", "shared-affinity")
	parentOne.Add("x-parent-session-id", "parent-one")
	parentTwo := http.Header{}
	parentTwo.Add("x-session-affinity", "shared-affinity")
	parentTwo.Add("x-parent-session-id", "parent-two")
	withoutParent := http.Header{}
	withoutParent.Add("x-session-affinity", "shared-affinity")

	keyOne, materialOne, ok := buildOverflowAffinityCacheKey(testOverflowAffinityCacheKeyInput(parentOne, "secret"))
	if !ok {
		t.Fatal("expected parent-one headers to build cache key")
	}
	keyTwo, materialTwo, ok := buildOverflowAffinityCacheKey(testOverflowAffinityCacheKeyInput(parentTwo, "secret"))
	if !ok {
		t.Fatal("expected parent-two headers to build cache key")
	}
	keyWithoutParent, materialWithoutParent, ok := buildOverflowAffinityCacheKey(testOverflowAffinityCacheKeyInput(withoutParent, "secret"))
	if !ok {
		t.Fatal("expected headers without parent to build cache key")
	}
	if keyOne != keyTwo || keyOne != keyWithoutParent {
		t.Fatalf("expected parent header to stay out of primary cache key: %#v %#v %#v", keyOne, keyTwo, keyWithoutParent)
	}
	if materialOne.parentHashPrefix == nil || materialTwo.parentHashPrefix == nil {
		t.Fatalf("expected parent prefixes, got %+v and %+v", materialOne.parentHashPrefix, materialTwo.parentHashPrefix)
	}
	if *materialOne.parentHashPrefix == *materialTwo.parentHashPrefix {
		t.Fatal("expected distinct parent values to produce distinct supplemental prefixes")
	}
	if materialWithoutParent.parentHashPrefix != nil {
		t.Fatalf("expected missing parent to omit supplemental prefix, got %q", *materialWithoutParent.parentHashPrefix)
	}

	ambiguousParent := http.Header{}
	ambiguousParent.Add("x-session-affinity", "shared-affinity")
	ambiguousParent.Add("x-parent-session-id", "parent-one")
	ambiguousParent.Add("X-Parent-Session-Id", "parent-two")
	ambiguousKey, ambiguousMaterial, ok := buildOverflowAffinityCacheKey(testOverflowAffinityCacheKeyInput(ambiguousParent, "secret"))
	if !ok {
		t.Fatal("expected ambiguous parent metadata not to disable affinity key creation")
	}
	if ambiguousKey != keyOne {
		t.Fatalf("expected ambiguous parent to stay out of primary key, got %#v and %#v", ambiguousKey, keyOne)
	}
	if ambiguousMaterial.parentHashPrefix != nil {
		t.Fatalf("expected ambiguous parent to omit supplemental prefix, got %q", *ambiguousMaterial.parentHashPrefix)
	}
}

func TestNewServiceInitializesOverflowAffinityCachePerInstance(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC)
	firstOptions, firstCleanup := testOverflowAffinityServiceOptions(t, now)
	defer firstCleanup()
	firstService, err := NewService(config.Settings{}, firstOptions)
	if err != nil {
		t.Fatalf("new first service: %v", err)
	}
	defer firstService.Close()

	secondOptions, secondCleanup := testOverflowAffinityServiceOptions(t, now)
	defer secondCleanup()
	secondService, err := NewService(config.Settings{}, secondOptions)
	if err != nil {
		t.Fatalf("new second service: %v", err)
	}
	defer secondService.Close()

	if firstService.overflowAffinityCache == nil || secondService.overflowAffinityCache == nil {
		t.Fatalf("expected service overflow affinity caches, got first=%v second=%v", firstService.overflowAffinityCache, secondService.overflowAffinityCache)
	}
	if firstService.overflowAffinityCache == secondService.overflowAffinityCache {
		t.Fatal("expected each service instance to own a distinct overflow affinity cache")
	}

	key := overflowAffinityCacheKey{profileID: 42, affinityDigest: "digest", routingGenerationToken: "generation-token", contextBucket: "unknown"}
	firstService.overflowAffinityCache.put(key, overflowAffinityCacheEntry{promotionTargetID: "promoted-model"})
	stored, ok := firstService.overflowAffinityCache.get(key)
	if !ok {
		t.Fatal("expected service-owned cache to store entries")
	}
	if !stored.expiresAt.Equal(now.Add(overflowAffinityCacheTTL)) {
		t.Fatalf("expected service cache to use injected runtime clock, got expiry %s", stored.expiresAt)
	}
	if _, ok := secondService.overflowAffinityCache.get(key); ok {
		t.Fatal("expected second service cache to start empty")
	}
}

func TestOverflowAffinityCachePreselectsPromotionTargetFromSeededEntry(t *testing.T) {
	t.Parallel()

	fixture := newOverflowAffinityPreselectionFixture(t)
	fixture.seedCacheEntry(t)

	preselected, ok := fixture.service.preselectOverflowAffinityPromotionPlanForSnapshot(fixture.request, fixture.sourcePlan, requestPlanTestProfileID, fixture.snapshot, fixture.generationToken)
	if !ok {
		t.Fatal("expected seeded affinity cache entry to preselect promotion target")
	}
	if !preselected.ContextOverflowPromotionPreselected || planAllowsContextOverflowPromotion(preselected) {
		t.Fatal("expected accepted preselection to disable later overflow replay")
	}
	if preselected.RequestedModelID != fixture.sourcePlan.RequestedModelID || preselected.ProfileID != fixture.sourcePlan.ProfileID || preselected.APIFamily != fixture.sourcePlan.APIFamily || preselected.RuntimeOperation.Name != fixture.sourcePlan.RuntimeOperation.Name {
		t.Fatalf("expected requested identity and operation to be preserved, got requested=%q profile=%d family=%q operation=%q", preselected.RequestedModelID, preselected.ProfileID, preselected.APIFamily, preselected.RuntimeOperation.Name)
	}
	if !reflect.DeepEqual(preselected.ClientHeaders, fixture.sourcePlan.ClientHeaders) || !reflect.DeepEqual(preselected.RequestGenerationParamsSnapshot(), fixture.sourcePlan.RequestGenerationParamsSnapshot()) || preselected.ReportCurrencySnapshot.Code != fixture.sourcePlan.ReportCurrencySnapshot.Code {
		t.Fatal("expected client headers, generation params, and report currency to be preserved")
	}
	if preselected.ResolvedTargetModelID == nil || *preselected.ResolvedTargetModelID != fixture.promotedModelID {
		t.Fatalf("expected promoted resolved target %q, got %+v", fixture.promotedModelID, preselected.ResolvedTargetModelID)
	}
	if got := extractModelFromBody(preselected.UpstreamBody); got != fixture.promotedModelID {
		t.Fatalf("expected upstream body model rewritten to promoted target %q, got %q", fixture.promotedModelID, got)
	}
	if got := extractModelFromBody(preselected.RawRequestBody); got != fixture.sourceModelID {
		t.Fatalf("expected raw request body to preserve requested source model %q, got %q", fixture.sourceModelID, got)
	}
	attempts := preselected.orderedTerminalAttempts()
	if len(attempts) != 1 || attempts[0].TargetModel.ModelID != fixture.promotedModelID {
		t.Fatalf("expected terminal attempts to execute promoted target, got %+v", attempts)
	}
}

func TestOverflowAffinityCachePreselectionRejectsUnsafeOrStaleEntries(t *testing.T) {
	t.Parallel()

	t.Run("missing affinity", func(t *testing.T) {
		t.Parallel()
		fixture := newOverflowAffinityPreselectionFixture(t)
		fixture.seedCacheEntry(t)
		fixture.request.Header.Del(overflowAffinityHeaderName)
		if _, ok := fixture.service.preselectOverflowAffinityPromotionPlanForSnapshot(fixture.request, fixture.sourcePlan, requestPlanTestProfileID, fixture.snapshot, fixture.generationToken); ok {
			t.Fatal("expected missing affinity to bypass preselection")
		}
	})
	t.Run("multiple distinct affinity", func(t *testing.T) {
		t.Parallel()
		fixture := newOverflowAffinityPreselectionFixture(t)
		fixture.seedCacheEntry(t)
		fixture.request.Header.Add(overflowAffinityHeaderName, "other-affinity")
		if _, ok := fixture.service.preselectOverflowAffinityPromotionPlanForSnapshot(fixture.request, fixture.sourcePlan, requestPlanTestProfileID, fixture.snapshot, fixture.generationToken); ok {
			t.Fatal("expected distinct affinity values to bypass preselection")
		}
	})
	t.Run("streaming request", func(t *testing.T) {
		t.Parallel()
		fixture := newOverflowAffinityPreselectionFixture(t)
		fixture.sourcePlan.IsStreamingRequest = true
		fixture.seedCacheEntry(t)
		if _, ok := fixture.service.preselectOverflowAffinityPromotionPlanForSnapshot(fixture.request, fixture.sourcePlan, requestPlanTestProfileID, fixture.snapshot, fixture.generationToken); ok {
			t.Fatal("expected streaming request to bypass preselection")
		}
	})
	t.Run("stale entry target", func(t *testing.T) {
		t.Parallel()
		fixture := newOverflowAffinityPreselectionFixture(t)
		fixture.seedCacheEntryWithTarget(t, "stale-promoted-model")
		if _, ok := fixture.service.preselectOverflowAffinityPromotionPlanForSnapshot(fixture.request, fixture.sourcePlan, requestPlanTestProfileID, fixture.snapshot, fixture.generationToken); ok {
			t.Fatal("expected stale target entry to bypass preselection")
		}
	})
	t.Run("target removed from current snapshot", func(t *testing.T) {
		t.Parallel()
		fixture := newOverflowAffinityPreselectionFixture(t)
		fixture.seedCacheEntry(t)
		delete(fixture.snapshot.ModelsByID, fixture.promotedModelID)
		if _, ok := fixture.service.preselectOverflowAffinityPromotionPlanForSnapshot(fixture.request, fixture.sourcePlan, requestPlanTestProfileID, fixture.snapshot, fixture.generationToken); ok {
			t.Fatal("expected missing current target model to bypass preselection")
		}
	})
}

type overflowAffinityPreselectionFixture struct {
	service         *Service
	snapshot        *planningSnapshot
	request         *http.Request
	sourcePlan      requestPlan
	sourceModelID   string
	promotedModelID string
	generationToken string
}

func newOverflowAffinityPreselectionFixture(t *testing.T) overflowAffinityPreselectionFixture {
	t.Helper()

	sourceModelID := "overflow-affinity-source"
	promotedModelID := "overflow-affinity-promoted"
	target := promotedModelID
	service := newRequestPlanUnitService()
	service.secretEncryptionKey = "overflow-affinity-test-secret"
	service.overflowAffinityCache = newOverflowAffinityCache(service.nowUTC)
	snapshot := newRequestPlanSnapshot(
		runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: sourceModelID, ContextOverflowPromotionTargetID: &target},
		runtimeModelRecord{ID: 2, APIFamily: "openai", ModelID: promotedModelID},
	)
	rawBody := []byte(`{"model":"` + sourceModelID + `","messages":[{"role":"user","content":"reuse affinity route"}],"temperature":0.4}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(overflowAffinityHeaderName, "shared-affinity")
	request.Header.Set(overflowAffinityParentHeaderName, "parent-affinity")
	operationMatch := mustResolveRuntimeOperation(t, http.MethodPost, request.URL.Path)
	sourcePlan, err := service.buildRequestPlanFromSnapshot(request, rawBody, RuntimeProxyConfigSnapshot{}, operationMatch, requestPlanTestProfileID, snapshot)
	if err != nil {
		t.Fatalf("build source request plan: %v", err)
	}
	return overflowAffinityPreselectionFixture{
		service:         service,
		snapshot:        snapshot,
		request:         request,
		sourcePlan:      sourcePlan,
		sourceModelID:   sourceModelID,
		promotedModelID: promotedModelID,
		generationToken: "generation-token",
	}
}

func (fixture overflowAffinityPreselectionFixture) seedCacheEntry(t *testing.T) {
	t.Helper()
	fixture.seedCacheEntryWithTarget(t, fixture.promotedModelID)
}

func (fixture overflowAffinityPreselectionFixture) seedCacheEntryWithTarget(t *testing.T, promotionTargetModelID string) {
	t.Helper()
	contextBucket := overflowAffinityContextBucket(fixture.sourcePlan.RequestContextEstimation)
	key, material, ok := buildOverflowAffinityCacheKey(overflowAffinityCacheKeyInput{
		profileID:                   fixture.sourcePlan.ProfileID,
		operationName:               strings.TrimSpace(fixture.sourcePlan.RuntimeOperation.Name),
		sourceResolvedModelID:       fixture.sourceModelID,
		sourceSelectedTerminalID:    fixture.sourcePlan.selectedTerminalTargetID(),
		configuredPromotionTargetID: fixture.promotedModelID,
		affinityHeaders:             fixture.request.Header,
		processLocalSecret:          fixture.service.secretEncryptionKey,
		routingGenerationToken:      fixture.generationToken,
		contextBucket:               contextBucket,
	})
	if !ok {
		t.Fatal("expected cache key for fixture")
	}
	fixture.service.overflowAffinityCache.put(key, buildOverflowAffinityCacheEntry(overflowAffinityCacheEntryInput{
		promotionTargetID:        promotionTargetModelID,
		sourceModelID:            fixture.sourceModelID,
		sourceSelectedTerminalID: fixture.sourcePlan.selectedTerminalTargetID(),
		generationToken:          fixture.generationToken,
		contextBucket:            contextBucket,
	}, material))
}

func testOverflowAffinityCacheKeyInput(headers http.Header, secret string) overflowAffinityCacheKeyInput {
	terminalID := 17
	return overflowAffinityCacheKeyInput{
		profileID:                   42,
		operationName:               "openai.chat_completions",
		sourceResolvedModelID:       "source-model",
		sourceSelectedTerminalID:    &terminalID,
		configuredPromotionTargetID: "promoted-model",
		affinityHeaders:             headers,
		processLocalSecret:          secret,
		routingGenerationToken:      "generation-token",
		contextBucket:               "0-16383",
	}
}

func testOverflowAffinityCacheEntryInput() overflowAffinityCacheEntryInput {
	terminalID := 17
	return overflowAffinityCacheEntryInput{
		promotionTargetID:        "promoted-model",
		sourceModelID:            "source-model",
		sourceSelectedTerminalID: &terminalID,
		generationToken:          "generation-token",
		contextBucket:            "0-16383",
	}
}

func testOverflowAffinityServiceOptions(t *testing.T, now time.Time) (Options, func()) {
	t.Helper()

	executionPool := testOverflowAffinityPool(t, "overflow_affinity_execution")
	telemetryPool := testOverflowAffinityPool(t, "overflow_affinity_telemetry")
	feedbackPool := testOverflowAffinityPool(t, "overflow_affinity_feedback")
	cleanup := func() {
		executionPool.Close()
		telemetryPool.Close()
		feedbackPool.Close()
	}
	return Options{
		ExecutionPool:       executionPool,
		TelemetryPool:       telemetryPool,
		FeedbackPool:        feedbackPool,
		HTTPClient:          &http.Client{},
		Now:                 func() time.Time { return now },
		Cache:               NewSharedCache(0),
		LogPartitionEnsurer: overflowAffinityNoopPartitionEnsurer{},
		Scheduler:           background.NewScheduler(background.Config{}),
	}, cleanup
}

func testOverflowAffinityPool(t *testing.T, applicationName string) *pgxpool.Pool {
	t.Helper()

	config, err := pgxpool.ParseConfig("postgres://prism:prism@127.0.0.1:1/prism?sslmode=disable&application_name=" + applicationName)
	if err != nil {
		t.Fatalf("parse pgxpool config: %v", err)
	}
	config.MinConns = 0
	config.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatalf("create pgxpool: %v", err)
	}
	return pool
}

type overflowAffinityNoopPartitionEnsurer struct{}

func (overflowAffinityNoopPartitionEnsurer) EnsurePartitionForTime(context.Context, string, time.Time) error {
	return nil
}
