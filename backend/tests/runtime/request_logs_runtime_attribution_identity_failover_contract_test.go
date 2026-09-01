package runtimetest

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
)

func TestRuntimeRequestLogPersistsAuditEnabledSnapshot(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	publicModelID := "audit-public-" + randomSuffix()
	targetModelID := "audit-target-" + randomSuffix()
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   publicModelID,
		TargetModelID:   targetModelID,
		EndpointBaseURL: harness.upstream.baseURL("/audit-enabled"),
		EndpointAPIKey:  "runtime-audit-key",
	})
	response := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/chat/completions",
		map[string]any{"messages": []map[string]any{{"role": "user", "content": "persist audit snapshot"}}, "model": route.PublicModelID},
		nil,
	)
	assertStatus(t, response, http.StatusOK)
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)

	var auditEnabledAtRequest bool
	var auditCaptureBodiesAtRequest bool
	if err := harness.conn.QueryRow(context.Background(), `SELECT audit_enabled_at_request, audit_capture_bodies_at_request FROM request_logs WHERE profile_id = $1 ORDER BY id DESC LIMIT 1`, profileID).Scan(&auditEnabledAtRequest, &auditCaptureBodiesAtRequest); err != nil {
		t.Fatalf("load persisted runtime audit snapshot: %v", err)
	}
	if auditEnabledAtRequest || auditCaptureBodiesAtRequest {
		t.Fatalf("expected runtime request log to persist absent audit family settings as false/false, got enabled=%v capture=%v", auditEnabledAtRequest, auditCaptureBodiesAtRequest)
	}
	assertLatestRuntimeAttemptCounts(t, harness.conn, profileID, 1, 1)
}

func TestRuntimeRequestLogPersistsAPIFamilyAuditSettingsSnapshot(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	seedRuntimeAuditFamilySetting(t, harness, profileID, "openai", true, false)
	seedRuntimeAuditFamilySetting(t, harness, profileID, "anthropic", false, false)
	seedRuntimeAuditFamilySetting(t, harness, profileID, "gemini", true, true)

	openAIRoute := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "audit-openai-public-" + randomSuffix(),
		TargetModelID:   "audit-openai-target-" + randomSuffix(),
		EndpointBaseURL: harness.upstream.baseURL("/audit-family/openai"),
		EndpointAPIKey:  "runtime-audit-openai-key",
	})
	anthropicUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "msg-audit-family", "type": "message", "role": "assistant", "content": []map[string]any{}, "usage": map[string]any{"input_tokens": 4, "output_tokens": 2}})
	anthropicRoute := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "anthropic",
		PublicModelID:   "audit-anthropic-public-" + randomSuffix(),
		TargetModelID:   "audit-anthropic-target-" + randomSuffix(),
		EndpointBaseURL: anthropicUpstream.baseURL("/audit-family/anthropic"),
		EndpointAPIKey:  "runtime-audit-anthropic-key",
	})
	geminiUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"candidates": []map[string]any{{"content": map[string]any{"parts": []map[string]any{{"text": "ok"}}}}}, "usageMetadata": map[string]any{"promptTokenCount": 3, "candidatesTokenCount": 2, "totalTokenCount": 5}})
	geminiRoute := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "gemini",
		PublicModelID:   "audit-gemini-public-" + randomSuffix(),
		TargetModelID:   "audit-gemini-target-" + randomSuffix(),
		EndpointBaseURL: geminiUpstream.baseURL("/audit-family/gemini"),
		EndpointAPIKey:  "runtime-audit-gemini-key",
	})

	openAIResponse := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "persist openai audit policy"}},
		"model":    openAIRoute.PublicModelID,
	}, nil)
	assertStatus(t, openAIResponse, http.StatusOK)
	anthropicResponse := harness.requestJSON(t, http.MethodPost, "/v1/messages", map[string]any{
		"model":      anthropicRoute.PublicModelID,
		"max_tokens": 16,
		"messages":   []map[string]any{{"role": "user", "content": "persist anthropic audit policy"}},
	}, nil)
	assertStatus(t, anthropicResponse, http.StatusOK)
	geminiResponse := harness.requestJSON(t, http.MethodPost, fmt.Sprintf("/v1beta/models/%s:generateContent", geminiRoute.PublicModelID), map[string]any{
		"contents": []map[string]any{{"role": "user", "parts": []map[string]any{{"text": "persist gemini audit policy"}}}},
	}, nil)
	assertStatus(t, geminiResponse, http.StatusOK)
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 3, UsageEvents: 3, OutboxRows: 0}, 5*time.Second)

	assertRuntimeRequestLogAuditSnapshot(t, harness, profileID, openAIRoute.PublicModelID, true, false)
	assertRuntimeRequestLogAuditSnapshot(t, harness, profileID, anthropicRoute.PublicModelID, false, false)
	assertRuntimeRequestLogAuditSnapshot(t, harness, profileID, geminiRoute.PublicModelID, true, true)
	assertRuntimeAuditLogSnapshot(t, harness, profileID, openAIRoute.PublicModelID, true, false)
	assertRuntimeAuditLogSnapshot(t, harness, profileID, geminiRoute.PublicModelID, true, true)
}

func TestRuntimeRequestLogsPreserveRequestedAndResolvedModelIdentity(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	upstream := newScriptedUpstream(t, http.StatusOK, map[string]any{
		"id":     "chatcmpl-runtime-requested-resolved-identity-" + suffix,
		"object": "chat.completion",
		"usage": map[string]any{
			"prompt_tokens":     8,
			"completion_tokens": 5,
			"total_tokens":      13,
		},
	})
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "requested-resolved-public-" + suffix,
		TargetModelID:   "requested-resolved-target-" + suffix,
		EndpointBaseURL: upstream.baseURL("/request-logs/requested-resolved"),
		EndpointAPIKey:  "runtime-requested-resolved-key",
	})

	response := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/chat/completions",
		map[string]any{
			"messages": []map[string]any{{"role": "user", "content": "preserve requested and resolved identity"}},
			"model":    route.PublicModelID,
		},
		nil,
	)
	assertStatus(t, response, http.StatusOK)
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)
	if got := requestModelID(t, upstream.lastRequest(t).Body); got != route.TargetModelID {
		t.Fatalf("expected upstream request model %q, got %q", route.TargetModelID, got)
	}
	assertLatestRuntimeAttemptCounts(t, harness.conn, profileID, 1, 1)
	assertLatestRuntimeModelIdentity(t, harness.conn, profileID, route.PublicModelID, route.TargetModelID)
}

func TestRuntimeRequestLogsPreserveRequestedPublicAndResolvedNativeIdentityForResponses(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	upstream := newScriptedUpstream(t, http.StatusOK, map[string]any{
		"id":     "resp-runtime-requested-resolved-identity-" + suffix,
		"object": "response",
		"usage": map[string]any{
			"input_tokens":  8,
			"output_tokens": 5,
			"total_tokens":  13,
		},
	})
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "requested-resolved-public-responses-" + suffix,
		TargetModelID:   "requested-resolved-target-responses-" + suffix,
		EndpointBaseURL: upstream.baseURL("/request-logs/requested-resolved-responses"),
		EndpointAPIKey:  "runtime-requested-resolved-key-responses",
	})

	response := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/responses",
		map[string]any{
			"input": "preserve requested and resolved identity in responses",
			"model": route.PublicModelID,
		},
		nil,
	)
	assertStatus(t, response, http.StatusOK)
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)
	if got := requestModelID(t, upstream.lastRequest(t).Body); got != route.TargetModelID {
		t.Fatalf("expected upstream request model %q, got %q", route.TargetModelID, got)
	}
	assertLatestRuntimeAttemptCounts(t, harness.conn, profileID, 1, 1)
	assertLatestRuntimeModelIdentity(t, harness.conn, profileID, route.PublicModelID, route.TargetModelID)
}

func TestRuntimeRequestLogsSkipCrossFamilyProxyTargets(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	anthropicUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "msg-cross-family-anthropic-" + suffix, "type": "message"})
	openAIUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{
		"id":     "chatcmpl-cross-family-openai-" + suffix,
		"object": "chat.completion",
		"usage": map[string]any{
			"prompt_tokens":     4,
			"completion_tokens": 3,
			"total_tokens":      7,
		},
	})

	releaseRefresh := harness.suspendRuntimeSnapshotRefresh()
	anthropicStrategyID := harness.seedLegacyStrategy(t, profileID, "request-logs-cross-family-anthropic-"+suffix, "round-robin")
	openAIStrategyID := harness.seedLegacyStrategy(t, profileID, "request-logs-cross-family-openai-"+suffix, "round-robin")
	anthropicTargetModelID := "cross-family-anthropic-target-" + suffix
	openAITargetModelID := "cross-family-openai-target-" + suffix
	publicModelID := "cross-family-public-" + suffix
	anthropicTargetConfigID := harness.seedModel(t, profileID, "anthropic", anthropicTargetModelID, "native", &anthropicStrategyID)
	openAITargetConfigID := harness.seedModel(t, profileID, "openai", openAITargetModelID, "native", &openAIStrategyID)
	publicModelConfigID := harness.seedModel(t, profileID, "openai", publicModelID, "proxy", nil)
	harness.seedProxyTargetAtPosition(t, publicModelConfigID, anthropicTargetConfigID, 0)
	harness.seedProxyTargetAtPosition(t, publicModelConfigID, openAITargetConfigID, 1)
	anthropicEndpointID := harness.seedEndpoint(t, profileID, "request-logs-cross-family-anthropic-endpoint-"+suffix, anthropicUpstream.baseURL("/request-logs/cross-family/anthropic"), "runtime-cross-family-anthropic-key", 0)
	openAIEndpointID := harness.seedEndpoint(t, profileID, "request-logs-cross-family-openai-endpoint-"+suffix, openAIUpstream.baseURL("/request-logs/cross-family/openai"), "runtime-cross-family-openai-key", 1)
	harness.seedConnection(t, profileID, anthropicTargetConfigID, anthropicEndpointID, "request-logs-cross-family-anthropic-connection-"+suffix, nil, nil, 0)
	harness.seedConnection(t, profileID, openAITargetConfigID, openAIEndpointID, "request-logs-cross-family-openai-connection-"+suffix, nil, nil, 0)
	releaseRefresh()
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})

	response := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/chat/completions",
		map[string]any{
			"messages": []map[string]any{{"role": "user", "content": "skip cross-family proxy targets"}},
			"model":    publicModelID,
		},
		nil,
	)
	assertStatus(t, response, http.StatusOK)
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)
	if got := len(anthropicUpstream.requestsSnapshot()); got != 0 {
		t.Fatalf("expected cross-family anthropic target to be skipped, got %d upstream requests", got)
	}
	if got := len(openAIUpstream.requestsSnapshot()); got != 1 {
		t.Fatalf("expected valid same-family target to receive exactly one upstream request, got %d", got)
	}
	if got := requestModelID(t, openAIUpstream.lastRequest(t).Body); got != openAITargetModelID {
		t.Fatalf("expected same-family upstream request model %q, got %q", openAITargetModelID, got)
	}
	assertLatestRuntimeAttemptCounts(t, harness.conn, profileID, 1, 1)
	assertLatestRuntimeModelIdentity(t, harness.conn, profileID, publicModelID, openAITargetModelID)
}

type runtimePricingOwnerFixture struct {
	templateID, configVersion                                                                                                             int
	templateName, unit, currency, inputPrice, outputPrice, cachedInputPrice, cacheCreationPrice, reasoningPrice, reportCode, reportSymbol string
	revisionID                                                                                                                            int64
	effectiveAt                                                                                                                           time.Time
	reportingEpoch                                                                                                                        int
}

type runtimePricingOwnerFailoverFixture struct {
	harness                                          *runtimeHarness
	gate                                             *runtimeTelemetryMaterializeGate
	profileID                                        int
	publicModelID, targetModelID                     string
	primaryUpstream, secondaryUpstream               *scriptedUpstream
	primaryEndpointID, secondaryEndpointID           int
	primaryConnectionID, secondaryConnectionID       int
	primaryUpstreamModelID, secondaryUpstreamModelID string
	primaryTemplateID, secondaryTemplateID           int
	primaryOwner, secondaryOwner                     runtimePricingOwnerFixture
	reportCurrency                                   runtimeReportCurrencySnapshot
}

func newRuntimePricingOwnerFailoverFixture(t *testing.T) runtimePricingOwnerFailoverFixture {
	t.Helper()
	gate := newRuntimeTelemetryMaterializeGate()
	harness := newRuntimeHarnessWithConfig(t, runtimeHarnessConfig{RuntimeOptions: runtimeapi.Options{TelemetryOutbox: runtimeapi.TelemetryOutboxOptions{WorkerCount: 1, PollInterval: 25 * time.Millisecond, Hooks: &runtimeapi.TelemetryOutboxHooks{BeforeMaterialize: gate.Wait}}}})
	profileID, suffix := harness.activeProfileID(t), randomSuffix()
	publicModelID, targetModelID := "runtime-request-log-fill-first-"+suffix, "runtime-request-log-target-"+suffix
	primaryUpstream := newScriptedUpstream(t, http.StatusServiceUnavailable, map[string]any{"error": "primary unavailable"})
	secondaryUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-runtime-request-log-secondary", "usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2}})
	strategyID := harness.seedLegacyStrategy(t, profileID, "runtime-request-log-fill-first-"+suffix, "fill-first")
	targetModelConfigID := harness.seedModel(t, profileID, "openai", targetModelID, "native", &strategyID)
	publicModelConfigID := harness.seedModel(t, profileID, "openai", publicModelID, "proxy", nil)
	harness.seedProxyTarget(t, publicModelConfigID, targetModelConfigID)
	primaryEndpointID := harness.seedEndpoint(t, profileID, "runtime-request-log-primary-endpoint-"+suffix, primaryUpstream.baseURL("/request-logs/fill-first/primary"), "runtime-request-log-primary-key", 0)
	secondaryEndpointID := harness.seedEndpoint(t, profileID, "runtime-request-log-secondary-endpoint-"+suffix, secondaryUpstream.baseURL("/request-logs/fill-first/secondary"), "runtime-request-log-secondary-key", 1)
	primaryConnectionID := harness.seedConnection(t, profileID, targetModelConfigID, primaryEndpointID, "runtime-request-log-primary-connection-"+suffix, nil, nil, 0)
	secondaryConnectionID := harness.seedConnection(t, profileID, targetModelConfigID, secondaryEndpointID, "runtime-request-log-secondary-connection-"+suffix, nil, nil, 1)
	primaryUpstreamModelID, secondaryUpstreamModelID := "vendor/Model-A", "vendor/Model-B"
	setRuntimeConnectionUpstreamModelID(t, harness.conn, primaryConnectionID, primaryUpstreamModelID)
	setRuntimeConnectionUpstreamModelID(t, harness.conn, secondaryConnectionID, secondaryUpstreamModelID)
	reportCurrency := loadRuntimeReportCurrencySnapshot(t, harness.conn, profileID)
	primaryTemplateID := insertRuntimePricingTemplate(t, harness.conn, profileID, "runtime-request-log-primary-pricing-"+suffix, reportCurrency.Code, "1", "2", "3", "4", "5")
	secondaryTemplateID := insertRuntimePricingTemplate(t, harness.conn, profileID, "runtime-request-log-secondary-pricing-"+suffix, reportCurrency.Code, "6", "7", "8", "9", "10")
	advanceRuntimePricingTemplateRevision(t, harness.conn, primaryTemplateID)
	advanceRuntimePricingTemplateRevision(t, harness.conn, secondaryTemplateID)
	attachRuntimeConnectionPricingTemplate(t, harness, primaryConnectionID, primaryTemplateID)
	attachRuntimeConnectionPricingTemplate(t, harness, secondaryConnectionID, secondaryTemplateID)
	return runtimePricingOwnerFailoverFixture{harness: harness, gate: gate, profileID: profileID, publicModelID: publicModelID, targetModelID: targetModelID, primaryUpstream: primaryUpstream, secondaryUpstream: secondaryUpstream, primaryEndpointID: primaryEndpointID, secondaryEndpointID: secondaryEndpointID, primaryConnectionID: primaryConnectionID, secondaryConnectionID: secondaryConnectionID, primaryUpstreamModelID: primaryUpstreamModelID, secondaryUpstreamModelID: secondaryUpstreamModelID, primaryTemplateID: primaryTemplateID, secondaryTemplateID: secondaryTemplateID, primaryOwner: loadRuntimePricingOwnerFixture(t, harness.conn, primaryTemplateID, reportCurrency), secondaryOwner: loadRuntimePricingOwnerFixture(t, harness.conn, secondaryTemplateID, reportCurrency), reportCurrency: reportCurrency}
}

func loadRuntimePricingOwnerFixture(t *testing.T, conn *pgx.Conn, templateID int, reportCurrency runtimeReportCurrencySnapshot) runtimePricingOwnerFixture {
	t.Helper()
	var owner runtimePricingOwnerFixture
	var revisionEpoch int
	if err := conn.QueryRow(context.Background(), `SELECT templates.id, templates.name, revisions.id, revisions.version, revisions.effective_at, revisions.reporting_currency_epoch, revisions.pricing_unit, revisions.currency_code, cards.input_price, cards.output_price, cards.cached_input_price, cards.cache_creation_price, cards.reasoning_price FROM pricing_templates AS templates JOIN pricing_template_revisions AS revisions ON revisions.id = templates.current_revision_id LEFT JOIN pricing_template_cards AS cards ON cards.revision_id = revisions.id AND cards.card_role = 'standard' WHERE templates.id = $1`, templateID).Scan(&owner.templateID, &owner.templateName, &owner.revisionID, &owner.configVersion, &owner.effectiveAt, &revisionEpoch, &owner.unit, &owner.currency, &owner.inputPrice, &owner.outputPrice, &owner.cachedInputPrice, &owner.cacheCreationPrice, &owner.reasoningPrice); err != nil {
		t.Fatalf("load request-time pricing owner for template %d: %v", templateID, err)
	}
	owner.reportingEpoch, owner.reportCode, owner.reportSymbol = reportCurrency.Epoch, reportCurrency.Code, reportCurrency.Symbol
	if owner.revisionID == int64(owner.configVersion) || revisionEpoch != reportCurrency.Epoch || owner.effectiveAt.IsZero() {
		t.Fatalf("expected distinct, effective active-epoch pricing revision identity, got %+v revision_epoch=%d", owner, revisionEpoch)
	}
	return owner
}

type runtimePersistedPricingOwnerFixture struct {
	owner         runtimePricingOwnerFixture
	pricingStatus string
	hasCosts      bool
}

func loadRuntimePersistedPricingOwnerFixture(t *testing.T, conn *pgx.Conn, profileID int, ingressRequestID string, tableName string, connectionID *int) runtimePersistedPricingOwnerFixture {
	t.Helper()
	const columns = `pricing_template_id_used, COALESCE(pricing_template_name_snapshot, ''), pricing_template_revision_id_used, COALESCE(pricing_version_effective_at, '0001-01-01T00:00:00Z'::timestamptz), COALESCE(reporting_currency_epoch, 0), COALESCE(pricing_config_version_used, 0), COALESCE(pricing_snapshot_unit, ''), COALESCE(currency_code_original, ''), COALESCE(pricing_snapshot_input, ''), COALESCE(pricing_snapshot_output, ''), COALESCE(pricing_snapshot_cache_read_input, ''), COALESCE(pricing_snapshot_cache_creation_input, ''), COALESCE(pricing_snapshot_reasoning, ''), COALESCE(report_currency_code, ''), COALESCE(report_currency_symbol, ''), pricing_status, input_cost_micros IS NOT NULL OR output_cost_micros IS NOT NULL OR cache_read_input_cost_micros IS NOT NULL OR cache_creation_input_cost_micros IS NOT NULL OR reasoning_cost_micros IS NOT NULL OR total_cost_original_micros IS NOT NULL OR total_cost_user_currency_micros IS NOT NULL`
	query, args := "", []any{profileID, ingressRequestID}
	switch tableName {
	case "request_logs":
		if connectionID == nil {
			t.Fatal("request_logs persisted pricing owner requires a connection ID")
		}
		query, args = `SELECT `+columns+` FROM request_logs WHERE profile_id = $1 AND ingress_request_id = $2 AND connection_id = $3`, append(args, *connectionID)
	case "usage_request_events":
		query = `SELECT ` + columns + ` FROM usage_request_events WHERE profile_id = $1 AND ingress_request_id = $2`
	default:
		t.Fatalf("unsupported persisted pricing owner table %q", tableName)
	}
	var persisted runtimePersistedPricingOwnerFixture
	owner := &persisted.owner
	if err := conn.QueryRow(context.Background(), query, args...).Scan(&owner.templateID, &owner.templateName, &owner.revisionID, &owner.effectiveAt, &owner.reportingEpoch, &owner.configVersion, &owner.unit, &owner.currency, &owner.inputPrice, &owner.outputPrice, &owner.cachedInputPrice, &owner.cacheCreationPrice, &owner.reasoningPrice, &owner.reportCode, &owner.reportSymbol, &persisted.pricingStatus, &persisted.hasCosts); err != nil {
		t.Fatalf("load persisted pricing owner from %s: %v", tableName, err)
	}
	return persisted
}

func assertRuntimePricingOwnerFixture(t *testing.T, label string, got runtimePricingOwnerFixture, want runtimePricingOwnerFixture) {
	t.Helper()
	gotEffectiveAt, wantEffectiveAt := got.effectiveAt, want.effectiveAt
	got.effectiveAt, want.effectiveAt = time.Time{}, time.Time{}
	if got != want || !gotEffectiveAt.Equal(wantEffectiveAt) {
		t.Fatalf("expected %s to retain request-time owner %+v after current revision changed, got %+v", label, want, got)
	}
}

func TestRuntimeRequestLogPersistsFailoverAttemptRowsAndSingleUsageEvent(t *testing.T) {
	fixture := newRuntimePricingOwnerFailoverFixture(t)

	response := fixture.harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/chat/completions",
		map[string]any{
			"messages": []map[string]any{{"role": "user", "content": "persist failover attempt counts"}},
			"model":    fixture.publicModelID,
		},
		nil,
	)
	assertStatus(t, response, http.StatusOK)
	if got := requestModelID(t, fixture.primaryUpstream.lastRequest(t).Body); got != fixture.primaryUpstreamModelID {
		t.Fatalf("primary wire model = %q, want %q", got, fixture.primaryUpstreamModelID)
	}
	if got := requestModelID(t, fixture.secondaryUpstream.lastRequest(t).Body); got != fixture.secondaryUpstreamModelID {
		t.Fatalf("secondary wire model = %q, want %q", got, fixture.secondaryUpstreamModelID)
	}
	waitForRuntimeTelemetryCounts(t, fixture.harness.conn, fixture.profileID, runtimeTelemetryCounts{RequestLogs: 0, UsageEvents: 0, OutboxRows: 1}, 5*time.Second)
	setRuntimeConnectionUpstreamModelID(t, fixture.harness.conn, fixture.primaryConnectionID, "changed-after-enqueue-a")
	setRuntimeConnectionUpstreamModelID(t, fixture.harness.conn, fixture.secondaryConnectionID, "changed-after-enqueue-b")
	if replacementID := advanceRuntimePricingTemplateRevision(t, fixture.harness.conn, fixture.primaryTemplateID); replacementID == fixture.primaryOwner.revisionID {
		t.Fatalf("expected primary current revision to advance beyond %d", fixture.primaryOwner.revisionID)
	}
	if replacementID := advanceRuntimePricingTemplateRevision(t, fixture.harness.conn, fixture.secondaryTemplateID); replacementID == fixture.secondaryOwner.revisionID {
		t.Fatalf("expected secondary current revision to advance beyond %d", fixture.secondaryOwner.revisionID)
	}
	fixture.gate.Release()
	waitForRuntimeTelemetryCounts(t, fixture.harness.conn, fixture.profileID, runtimeTelemetryCounts{RequestLogs: 2, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)
	if got := len(fixture.primaryUpstream.requestsSnapshot()); got != 1 {
		t.Fatalf("expected primary upstream to receive one failover attempt, got %d requests", got)
	}
	if got := len(fixture.secondaryUpstream.requestsSnapshot()); got != 1 {
		t.Fatalf("expected secondary upstream to receive one failover attempt, got %d requests", got)
	}
	ingressRequestID := assertLatestRuntimeAttemptSequence(t, fixture.harness.conn, fixture.profileID, []runtimeRequestLogAttempt{{
		AttemptNumber:   1,
		ConnectionID:    fixture.primaryConnectionID,
		EndpointID:      fixture.primaryEndpointID,
		StatusCode:      http.StatusServiceUnavailable,
		SuccessFlag:     false,
		UpstreamModelID: fixture.primaryUpstreamModelID,
	}, {
		AttemptNumber:   2,
		ConnectionID:    fixture.secondaryConnectionID,
		EndpointID:      fixture.secondaryEndpointID,
		StatusCode:      http.StatusOK,
		SuccessFlag:     true,
		UpstreamModelID: fixture.secondaryUpstreamModelID,
	}})
	assertLatestRuntimeModelIdentity(t, fixture.harness.conn, fixture.profileID, fixture.publicModelID, fixture.targetModelID)
	assertRuntimeUpstreamReadProjections(t, fixture.harness, fixture.profileID, ingressRequestID, fixture.primaryUpstreamModelID, fixture.secondaryUpstreamModelID)
	primaryPersisted := loadRuntimePersistedPricingOwnerFixture(t, fixture.harness.conn, fixture.profileID, ingressRequestID, "request_logs", &fixture.primaryConnectionID)
	if primaryPersisted.owner.templateID != fixture.primaryOwner.templateID || primaryPersisted.owner.revisionID != fixture.primaryOwner.revisionID || primaryPersisted.owner.inputPrice != "" || primaryPersisted.owner.configVersion != 0 {
		t.Fatalf("expected failed primary attempt to retain identity but no selected-card snapshot, got %+v", primaryPersisted.owner)
	}
	if primaryPersisted.pricingStatus != "ineligible" || primaryPersisted.hasCosts {
		t.Fatalf("expected failed primary attempt to remain ineligible without costs, got %+v", primaryPersisted)
	}
	secondaryPersisted := loadRuntimePersistedPricingOwnerFixture(t, fixture.harness.conn, fixture.profileID, ingressRequestID, "request_logs", &fixture.secondaryConnectionID)
	assertRuntimePricingOwnerFixture(t, "winning secondary request log", secondaryPersisted.owner, fixture.secondaryOwner)
	usagePersisted := loadRuntimePersistedPricingOwnerFixture(t, fixture.harness.conn, fixture.profileID, ingressRequestID, "usage_request_events", nil)
	assertRuntimePricingOwnerFixture(t, "final usage event", usagePersisted.owner, fixture.secondaryOwner)
}

type runtimeRequestLogAttempt struct {
	AttemptNumber   int
	ConnectionID    int
	EndpointID      int
	StatusCode      int
	SuccessFlag     bool
	UpstreamModelID string
}

func assertLatestRuntimeAttemptCounts(t *testing.T, conn *pgx.Conn, profileID int, wantRequestLogAttempt int, wantUsageEventAttempt int) {
	t.Helper()
	ingressRequestID := loadLatestRuntimeIngressRequestID(t, conn, profileID)

	rows, err := conn.Query(
		context.Background(),
		`SELECT attempt_number FROM request_logs WHERE profile_id = $1 AND ingress_request_id = $2 ORDER BY attempt_number ASC, id ASC`,
		profileID,
		ingressRequestID,
	)
	if err != nil {
		t.Fatalf("query runtime request-log attempts: %v", err)
	}
	defer rows.Close()

	attemptNumbers := make([]int, 0)
	for rows.Next() {
		var attemptNumber int
		if err := rows.Scan(&attemptNumber); err != nil {
			t.Fatalf("scan runtime request-log attempt number: %v", err)
		}
		attemptNumbers = append(attemptNumbers, attemptNumber)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate runtime request-log attempt numbers: %v", err)
	}
	if len(attemptNumbers) != wantRequestLogAttempt {
		t.Fatalf("expected %d request_logs rows for ingress_request_id %q, got %v", wantRequestLogAttempt, ingressRequestID, attemptNumbers)
	}
	for index, attemptNumber := range attemptNumbers {
		wantAttemptNumber := index + 1
		if attemptNumber != wantAttemptNumber {
			t.Fatalf("expected request_logs attempt_number sequence %d..%d, got %v", 1, wantRequestLogAttempt, attemptNumbers)
		}
	}

	var usageEventCount int
	var usageEventAttempt int
	if err := conn.QueryRow(
		context.Background(),
		`SELECT COUNT(*), COALESCE(MAX(attempt_count), 0) FROM usage_request_events WHERE profile_id = $1 AND ingress_request_id = $2`,
		profileID,
		ingressRequestID,
	).Scan(&usageEventCount, &usageEventAttempt); err != nil {
		t.Fatalf("load runtime usage-event attempt count: %v", err)
	}
	if usageEventCount != 1 {
		t.Fatalf("expected exactly 1 usage_request_events row for ingress_request_id %q, got %d", ingressRequestID, usageEventCount)
	}
	if usageEventAttempt != wantUsageEventAttempt {
		t.Fatalf("expected usage_request_events attempt_count=%d, got %d", wantUsageEventAttempt, usageEventAttempt)
	}
}

func assertLatestRuntimeModelIdentity(t *testing.T, conn *pgx.Conn, profileID int, wantModelID string, wantResolvedTargetModelID string) {
	t.Helper()
	assertLatestRuntimeModelIdentityState(t, conn, profileID, wantModelID, sql.NullString{String: wantResolvedTargetModelID, Valid: true})
}

func assertLatestRuntimeModelIdentityState(t *testing.T, conn *pgx.Conn, profileID int, wantModelID string, wantResolvedTargetModelID sql.NullString) {
	t.Helper()
	ingressRequestID := loadLatestRuntimeIngressRequestID(t, conn, profileID)

	var requestLogModelID string
	var requestLogResolvedTargetModelID sql.NullString
	if err := conn.QueryRow(
		context.Background(),
		`SELECT model_id, resolved_target_model_id FROM request_logs WHERE profile_id = $1 AND ingress_request_id = $2 ORDER BY attempt_number DESC, id DESC LIMIT 1`,
		profileID,
		ingressRequestID,
	).Scan(&requestLogModelID, &requestLogResolvedTargetModelID); err != nil {
		t.Fatalf("load runtime request-log model identity: %v", err)
	}
	if requestLogModelID != wantModelID || requestLogResolvedTargetModelID != wantResolvedTargetModelID {
		t.Fatalf("expected request_logs identity requested=%q resolved=%+v, got requested=%q resolved=%+v", wantModelID, wantResolvedTargetModelID, requestLogModelID, requestLogResolvedTargetModelID)
	}

	var usageEventModelID string
	var usageEventResolvedTargetModelID sql.NullString
	if err := conn.QueryRow(
		context.Background(),
		`SELECT model_id, resolved_target_model_id FROM usage_request_events WHERE profile_id = $1 AND ingress_request_id = $2 ORDER BY id DESC LIMIT 1`,
		profileID,
		ingressRequestID,
	).Scan(&usageEventModelID, &usageEventResolvedTargetModelID); err != nil {
		t.Fatalf("load runtime usage-event model identity: %v", err)
	}
	if usageEventModelID != wantModelID || usageEventResolvedTargetModelID != wantResolvedTargetModelID {
		t.Fatalf("expected usage_request_events identity requested=%q resolved=%+v, got requested=%q resolved=%+v", wantModelID, wantResolvedTargetModelID, usageEventModelID, usageEventResolvedTargetModelID)
	}
}

func assertLatestRuntimeAttemptSequence(t *testing.T, conn *pgx.Conn, profileID int, want []runtimeRequestLogAttempt) string {
	t.Helper()
	ingressRequestID := loadLatestRuntimeIngressRequestID(t, conn, profileID)

	rows, err := conn.Query(
		context.Background(),
		`SELECT attempt_number, connection_id, endpoint_id, upstream_status_code, success_flag, upstream_model_id FROM request_logs WHERE profile_id = $1 AND ingress_request_id = $2 AND row_kind = 'upstream' ORDER BY attempt_number ASC, id ASC`,
		profileID,
		ingressRequestID,
	)
	if err != nil {
		t.Fatalf("query runtime request-log attempt sequence: %v", err)
	}
	defer rows.Close()

	got := make([]runtimeRequestLogAttempt, 0, len(want))
	for rows.Next() {
		var attempt runtimeRequestLogAttempt
		if err := rows.Scan(&attempt.AttemptNumber, &attempt.ConnectionID, &attempt.EndpointID, &attempt.StatusCode, &attempt.SuccessFlag, &attempt.UpstreamModelID); err != nil {
			t.Fatalf("scan runtime request-log attempt sequence: %v", err)
		}
		got = append(got, attempt)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate runtime request-log attempt sequence: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d request-log attempt rows for ingress_request_id %q, got %+v", len(want), ingressRequestID, got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("expected request-log attempt sequence %+v for ingress_request_id %q, got %+v", want, ingressRequestID, got)
		}
	}
	return ingressRequestID
}
