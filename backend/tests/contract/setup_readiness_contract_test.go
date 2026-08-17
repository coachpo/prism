package contracttest

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	managementauth "github.com/coachpo/prism/backend/internal/httpapi/management/auth"
	managementconnections "github.com/coachpo/prism/backend/internal/httpapi/management/connections"
	managementendpoints "github.com/coachpo/prism/backend/internal/httpapi/management/endpoints"
	managementmodels "github.com/coachpo/prism/backend/internal/httpapi/management/models"
	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
	"github.com/coachpo/prism/backend/internal/platform/config"
	platformhttp "github.com/coachpo/prism/backend/internal/platform/http"
)

// TestSetupReadinessProjections covers the Auth/Landing SPEC §8.2 existing-API
// setup coordinator surface: the route-witness analyzer snapshot, the models
// include=route_readiness envelope, the bounded route-witness resolver, the
// Pricing setup_readiness projection and the Proxy key setup_readiness
// projection (auth disabled => not_required with optional attribution only).

func TestSetupReadinessModelsAndResolver(t *testing.T) {
	harness := newSetupReadinessHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	strategyID := modelInsertLoadbalanceStrategyWithType(t, harness, profileID, "Setup Fill First", "fill-first")
	endpointID := modelInsertEndpoint(t, harness, profileID, "Setup Endpoint")

	// Empty profile: readiness aggregate is authoritative not_ready (never a
	// guessed zero).
	empty := modelJSON[map[string]any](t, harness, profileID, http.MethodGet, "/api/models?include=route_readiness", nil, http.StatusOK)
	readiness, ok := empty["route_readiness"].(map[string]any)
	if !ok {
		t.Fatalf("expected route_readiness envelope, got %+v", empty)
	}
	if readiness["configuration"].(map[string]any)["state"] != "not_ready" {
		t.Fatalf("expected empty-profile configuration not_ready, got %+v", readiness)
	}
	generation := fmt.Sprintf("%v", readiness["route_witness_generation"])
	if generation == "" || generation == "0" {
		t.Fatalf("expected positive route_witness_generation, got %q", generation)
	}

	// Seed an enabled model WITHOUT terminal target (老数据: configuration
	// ready, application not_ready — the honest 3/4 state). SQL seeding does
	// not advance the witness generation.
	modelID := modelInsertModel(t, harness, profileID, "openai", "setup-model", nil, "native", &strategyID, true)

	// Generation unchanged after the SQL seed.
	afterCreate := modelJSON[map[string]any](t, harness, profileID, http.MethodGet, "/api/models?include=route_readiness", nil, http.StatusOK)
	readinessAfter := afterCreate["route_readiness"].(map[string]any)
	generationAfter := fmt.Sprintf("%v", readinessAfter["route_witness_generation"])
	if generationAfter != generation {
		t.Fatalf("expected generation to stay %q after SQL seed, got %q", generation, generationAfter)
	}
	if readinessAfter["configuration"].(map[string]any)["state"] != "ready" {
		t.Fatalf("expected configuration ready after model create, got %+v", readinessAfter)
	}
	if readinessAfter["application"].(map[string]any)["state"] != "not_ready" {
		t.Fatalf("expected application not_ready (no terminal target), got %+v", readinessAfter)
	}
	if count := intValue(readinessAfter["configuration_ready_model_count"]); count != 1 {
		t.Fatalf("expected 1 configuration-ready model, got %d", count)
	}
	items := afterCreate["items"].([]any)
	modelItem := items[0].(map[string]any)
	modelReadiness := modelItem["route_readiness"].(map[string]any)
	if modelReadiness["configuration"].(map[string]any)["state"] != "ready" || modelReadiness["application"].(map[string]any)["state"] != "not_ready" {
		t.Fatalf("unexpected per-model readiness, got %+v", modelReadiness)
	}

	// Add an enabled terminal target through the management API: this is a
	// route-affecting mutation, so the witness generation advances exactly
	// once and application becomes ready with a witness.
	harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/models/%d/connections", modelID), map[string]any{
		"endpoint_id":            endpointID,
		"name":                   "Setup Target",
		"openai_text_capability": "dual_native",
		"is_active":              true,
	}, modelHeader(profileID))

	// Resolver with the stale (pre-bump) generation -> typed 409.
	resolverResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/models/route-witnesses?generation="+generation, nil, modelHeader(profileID))
	assertStatus(t, resolverResponse, http.StatusConflict)

	// Resolver with a bogus generation label -> 400.
	bogus := harness.requestJSON(t, harness.client, http.MethodGet, "/api/models/route-witnesses?generation=abc", nil, modelHeader(profileID))
	assertStatus(t, bogus, http.StatusBadRequest)
	afterTarget := modelJSON[map[string]any](t, harness, profileID, http.MethodGet, "/api/models?include=route_readiness", nil, http.StatusOK)
	readinessTarget := afterTarget["route_readiness"].(map[string]any)
	if readinessTarget["application"].(map[string]any)["state"] != "ready" {
		t.Fatalf("expected application ready after terminal target, got %+v", readinessTarget)
	}
	if witnessCount := intValue(readinessTarget["route_witness_count"]); witnessCount < 1 {
		t.Fatalf("expected at least one route witness, got %d", witnessCount)
	}
	generationTarget := fmt.Sprintf("%v", readinessTarget["route_witness_generation"])
	representative := readinessTarget["representative_witness"].(map[string]any)
	if representative["operation_name"] != "openai.chat_completions" {
		t.Fatalf("expected representative chat_completions witness, got %+v", representative)
	}
	resolved := harness.requestJSON(t, harness.client, http.MethodGet, "/api/models/route-witnesses?generation="+generationTarget, nil, modelHeader(profileID))
	assertStatus(t, resolved, http.StatusOK)
	var resolvedPayload map[string]any
	decodeJSONResponse(t, resolved, &resolvedPayload)
	witness := resolvedPayload["witness"].(map[string]any)
	if witness["witness_id"] != representative["witness_id"] {
		t.Fatalf("expected resolver to return the representative witness, got %+v", witness)
	}
	selected := harness.requestJSON(t, harness.client, http.MethodGet, "/api/models/route-witnesses?generation="+generationTarget+"&selected_id=does-not-exist", nil, modelHeader(profileID))
	assertStatus(t, selected, http.StatusNotFound)
}

func TestSetupReadinessPricingProjection(t *testing.T) {
	harness := newSetupReadinessHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	strategyID := modelInsertLoadbalanceStrategyWithType(t, harness, profileID, "Setup Pricing Fill First", "fill-first")
	endpointID := modelInsertEndpoint(t, harness, profileID, "Setup Pricing Endpoint")

	modelID := modelInsertModel(t, harness, profileID, "openai", "setup-pricing-model", nil, "native", &strategyID, true)
	connectionID := modelInsertConnection(t, harness, profileID, modelID, endpointID, 0, true, nil)

	readiness := modelJSON[map[string]any](t, harness, profileID, http.MethodGet, "/api/models?include=route_readiness", nil, http.StatusOK)
	generation := fmt.Sprintf("%v", readiness["route_readiness"].(map[string]any)["route_witness_generation"])

	// No pricing template: configuration not_ready, application not_ready,
	// cost_ready=false (zero matching over the complete snapshot).
	pricing := modelJSON[map[string]any](t, harness, profileID, http.MethodGet, "/api/pricing-templates?limit=1&include=setup_readiness&expected_route_witness_generation="+generation, nil, http.StatusOK)
	if pricing["configuration"].(map[string]any)["state"] != "not_ready" {
		t.Fatalf("expected pricing configuration not_ready, got %+v", pricing)
	}
	if pricing["application"].(map[string]any)["state"] != "not_ready" {
		t.Fatalf("expected pricing application not_ready, got %+v", pricing)
	}
	if pricing["cost_ready"] != false {
		t.Fatalf("expected cost_ready=false with no template, got %+v", pricing)
	}

	// Create a complete template and attach it to the witness connection.
	createTemplate := harness.requestJSON(t, harness.client, http.MethodPost, "/api/pricing-templates", map[string]any{
		"name":                 "Setup Pricing Template",
		"input_price":          "2",
		"output_price":         "5",
		"cached_input_price":   "0",
		"cache_creation_price": "0",
		"reasoning_price":      "0",
	}, modelHeader(profileID))
	assertStatus(t, createTemplate, http.StatusCreated)
	var createdTemplate map[string]any
	decodeJSONResponse(t, createTemplate, &createdTemplate)
	templateID := intValue(createdTemplate["id"])
	if _, err := harness.conn.Exec(t.Context(), `UPDATE connections SET pricing_template_id = $1, updated_at = $2 WHERE id = $3`, templateID, createdTemplate["updated_at"], connectionID); err != nil {
		t.Fatalf("attach pricing template: %v", err)
	}
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})

	// The attach did not change the witness generation; the pricing
	// projection on the same generation now shows applied + cost_ready.
	attached := modelJSON[map[string]any](t, harness, profileID, http.MethodGet, "/api/pricing-templates?limit=1&include=setup_readiness&expected_route_witness_generation="+generation, nil, http.StatusOK)
	if attached["configuration"].(map[string]any)["state"] != "ready" {
		t.Fatalf("expected pricing configuration ready, got %+v", attached)
	}
	if attached["application"].(map[string]any)["state"] != "ready" {
		t.Fatalf("expected pricing application ready, got %+v", attached)
	}
	if attached["cost_ready"] != true {
		t.Fatalf("expected cost_ready=true with complete attached template, got %+v", attached)
	}
	if intValue(attached["applied_witness_count"]) < 1 || intValue(attached["cost_ready_witness_count"]) < 1 {
		t.Fatalf("expected applied/cost-ready witnesses, got %+v", attached)
	}
	matching := attached["representative_matching"].(map[string]any)
	modelRef := matching["model"].(map[string]any)
	if modelRef["kind"] != "model" || modelRef["name_source"] != "current" {
		t.Fatalf("expected canonical model ref in matching projection, got %+v", modelRef)
	}

	// Stale generation -> typed 409.
	stale := harness.requestJSON(t, harness.client, http.MethodGet, "/api/pricing-templates?limit=1&include=setup_readiness&expected_route_witness_generation=999999", nil, modelHeader(profileID))
	assertStatus(t, stale, http.StatusConflict)
}

func TestSetupReadinessProxyProjection(t *testing.T) {
	harness := newSetupReadinessHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	strategyID := modelInsertLoadbalanceStrategyWithType(t, harness, profileID, "Setup Proxy Fill First", "fill-first")
	endpointID := modelInsertEndpoint(t, harness, profileID, "Setup Proxy Endpoint")
	modelID := modelInsertModel(t, harness, profileID, "openai", "setup-proxy-model", nil, "native", &strategyID, true)
	modelInsertConnection(t, harness, profileID, modelID, endpointID, 0, true, nil)

	readiness := modelJSON[map[string]any](t, harness, profileID, http.MethodGet, "/api/models?include=route_readiness", nil, http.StatusOK)
	generation := fmt.Sprintf("%v", readiness["route_readiness"].(map[string]any)["route_witness_generation"])

	// Auth disabled (default): application exactly not_required, optional
	// attribution 0 (no key), open access facts fixed fresh.
	proxy := modelJSON[map[string]any](t, harness, profileID, http.MethodGet, "/api/settings/auth/proxy-keys?include=setup_readiness&expected_route_witness_generation="+generation, nil, http.StatusOK)
	if proxy["application"].(map[string]any)["state"] != "not_required" {
		t.Fatalf("expected auth-disabled proxy application not_required, got %+v", proxy)
	}
	if optional := intValue(proxy["optional_attribution_witness_count"]); optional != 0 {
		t.Fatalf("expected 0 optional attribution witnesses without keys, got %d", optional)
	}

	// Create an active key: optional attribution becomes available, still
	// not_required.
	createKey := harness.requestJSON(t, harness.client, http.MethodPost, "/api/settings/auth/proxy-keys", map[string]any{"name": "setup-proxy-key"}, nil)
	assertStatus(t, createKey, http.StatusCreated)
	proxyWithKey := modelJSON[map[string]any](t, harness, profileID, http.MethodGet, "/api/settings/auth/proxy-keys?include=setup_readiness&expected_route_witness_generation="+generation, nil, http.StatusOK)
	if proxyWithKey["application"].(map[string]any)["state"] != "not_required" {
		t.Fatalf("expected auth-disabled application to stay not_required with a key, got %+v", proxyWithKey)
	}
	if optional := intValue(proxyWithKey["optional_attribution_witness_count"]); optional < 1 {
		t.Fatalf("expected optional attribution witnesses with an active key, got %d", optional)
	}

	// Auth enabled: application requires both an effective key and a witness.
	enableVerifiedAuth(t, harness, "setup-admin", "setup-password-123", "setup@example.com")
	loginResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/auth/login", map[string]any{"username": "setup-admin", "password": "setup-password-123", "session_duration": "7_days"}, nil)
	assertStatus(t, loginResponse, http.StatusOK)
	t.Logf("login headers: %v", loginResponse.Header)
	t.Logf("login body: %s", readResponseBody(t, loginResponse))
	if cookieValue(t, harness.client, harness.url, "prism_access_token") == "" {
		t.Fatal("expected access cookie after login")
	}
	proxyEnabled := modelJSON[map[string]any](t, harness, profileID, http.MethodGet, "/api/settings/auth/proxy-keys?include=setup_readiness&expected_route_witness_generation="+generation, nil, http.StatusOK)
	if proxyEnabled["configuration"].(map[string]any)["state"] != "ready" {
		t.Fatalf("expected auth-enabled proxy configuration ready, got %+v", proxyEnabled)
	}
	if proxyEnabled["application"].(map[string]any)["state"] != "ready" {
		t.Fatalf("expected auth-enabled proxy application ready, got %+v", proxyEnabled)
	}
	if intValue(proxyEnabled["matching_witness_count"]) < 1 {
		t.Fatalf("expected matching witnesses with effective key + route-ready model, got %+v", proxyEnabled)
	}
}

func intValue(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	case int64:
		return int(typed)
	default:
		return 0
	}
}

// newSetupReadinessHarness wires the full management service set (auth +
// models + connections + endpoints + settings) so setup readiness projections
// can be exercised end to end.
func newSetupReadinessHarness(t *testing.T) *contractHarness {
	t.Helper()
	return newContractHarnessFor(t, "setup_readiness_contract", contractHarnessOptions{
		SecretEncryptionKey: "setup-readiness-contract-secret",
		Version:             "setup-readiness-contract-test",
		DependenciesBuilder: func(t *testing.T, testContext context.Context, harness *contractHarness, settings config.Settings, pool *pgxpool.Pool) platformhttp.Dependencies {
			t.Helper()
			settings.AuthJWTSecret = "setup-readiness-jwt-secret"
			settings.AuthAccessTokenTTLSeconds = 900
			settings.AuthRefreshTokenTTLSeconds = 604800
			settings.AuthCookieName = "prism_access_token"
			settings.AuthRefreshCookieName = "prism_refresh_token"
			settings.AuthCookieSecure = false
			runtimeCache := runtimeapi.NewSharedCacheWithOptions(runtimeapi.SharedCacheOptions{RefreshPool: pool, SecretEncryptionKey: settings.SecretEncryptionKey})
			if err := runtimeCache.Bootstrap(testContext); err != nil {
				t.Fatalf("bootstrap published runtime snapshot: %v", err)
			}
			runtimeAuthCache := managementauth.NewRuntimeCacheFromShared(runtimeCache)
			startupRuntime, err := platformhttp.NewStartupConfigRuntime(settings)
			if err != nil {
				t.Fatalf("build startup config runtime: %v", err)
			}
			authService, err := managementauth.NewService(settings, managementauth.Options{
				CORSOriginProvider:        startupRuntime,
				AuthRuntimeConfigProvider: startupRuntime,
				Pool:                      pool,
				RuntimeCache:              runtimeAuthCache,
			})
			if err != nil {
				t.Fatalf("build auth service: %v", err)
			}
			t.Cleanup(authService.Close)
			endpointsService, err := managementendpoints.NewService(settings, managementendpoints.Options{Pool: pool})
			if err != nil {
				t.Fatalf("build endpoints service: %v", err)
			}
			t.Cleanup(endpointsService.Close)
			connectionsService, err := managementconnections.NewService(settings, managementconnections.Options{Pool: pool})
			if err != nil {
				t.Fatalf("build connections service: %v", err)
			}
			t.Cleanup(connectionsService.Close)
			modelsService, err := managementmodels.NewService(settings, managementmodels.Options{Pool: pool})
			if err != nil {
				t.Fatalf("build models service: %v", err)
			}
			t.Cleanup(modelsService.Close)
			modelsService.SetTerminalTargetCreator(connectionsService)
			return platformhttp.Dependencies{
				AuthService:               authService,
				RuntimeAuthService:        authService,
				RuntimeCache:              runtimeCache,
				StartupConfigRuntime: startupRuntime,
				EndpointsService:          endpointsService,
				ConnectionsService:        connectionsService,
				ModelsService:             modelsService,
			}
		},
	})
}

func setupReadinessNow() time.Time { return time.Now().UTC() }
