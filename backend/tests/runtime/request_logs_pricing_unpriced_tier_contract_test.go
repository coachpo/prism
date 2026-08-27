package runtimetest

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestRuntimeRequestLogPreservesUnpricedPricingPathways(t *testing.T) {
	baseUsage := map[string]any{
		"prompt_tokens":     10,
		"completion_tokens": 6,
		"total_tokens":      16,
	}
	componentUsage := map[string]any{
		"prompt_tokens":     10,
		"completion_tokens": 6,
		"total_tokens":      16,
		"prompt_tokens_details": map[string]any{
			"cached_tokens": 4,
		},
		"completion_tokens_details": map[string]any{
			"reasoning_tokens": 3,
		},
	}

	type pricingTemplateSpec struct {
		currencyCode  string
		inputPrice    string
		outputPrice   string
		cachedInput   string
		cacheCreation string
		reasoning     string
	}

	loadPayload := func(t *testing.T, harness *runtimeHarness, profileID int, path string) map[string]any {
		t.Helper()
		response := harness.requestJSON(t, http.MethodGet, path, nil, runtimeModelHeader(profileID))
		assertStatus(t, response, http.StatusOK)
		var payload map[string]any
		decodeJSONResponse(t, response, &payload)
		return payload
	}

	loadSingleListItem := func(t *testing.T, harness *runtimeHarness, profileID int) map[string]any {
		t.Helper()
		payload := loadPayload(t, harness, profileID, "/api/stats/requests?limit=50&offset=0")
		items, ok := payload["items"].([]any)
		if !ok || len(items) != 1 {
			t.Fatalf("expected one request-log list row, got %+v", payload)
		}
		return asMapRuntime(t, items[0])
	}

	tests := []struct {
		name           string
		usage          map[string]any
		requestContent string
		template       func(runtimeReportCurrencySnapshot) *pricingTemplateSpec
		attachTemplate func(*testing.T, *runtimeHarness, int, runtimeReportCurrencySnapshot, seededRuntimeRoute, string)
		want           func(runtimeReportCurrencySnapshot) runtimePersistedPricingRow
		assert         func(*testing.T, *runtimeHarness, int)
	}{
		{
			name:           "optional component prices without component usage counters",
			usage:          baseUsage,
			requestContent: "price omitted optional counters",
			template: func(reportCurrency runtimeReportCurrencySnapshot) *pricingTemplateSpec {
				return &pricingTemplateSpec{inputPrice: "2", outputPrice: "5", cachedInput: "11", cacheCreation: "13", reasoning: "17"}
			},
			want: func(reportCurrency runtimeReportCurrencySnapshot) runtimePersistedPricingRow {
				return wantPricedRow(func(row *runtimePersistedPricingRow) {
					row.InputTokens = runtimeNullInt64(10)
					row.OutputTokens = runtimeNullInt64(6)
					row.TotalTokens = runtimeNullInt64(16)
					row.InputCostMicros = runtimeNullInt64(20)
					row.OutputCostMicros = runtimeNullInt64(30)
					row.CacheReadInputCostMicros = runtimeNullInt64(0)
					row.CacheCreationInputCostMicros = runtimeNullInt64(0)
					row.ReasoningCostMicros = runtimeNullInt64(0)
					row.TotalCostOriginalMicros = runtimeNullInt64(50)
					row.TotalCostUserCurrencyMicros = runtimeNullInt64(50)
					row.CurrencyCodeOriginal = runtimeNullString(reportCurrency.Code)
					row.ReportCurrencyCode = runtimeNullString(reportCurrency.Code)
					row.ReportCurrencySymbol = runtimeNullString(reportCurrency.Symbol)
					row.PricingSnapshotInput = runtimeNullString("2")
					row.PricingSnapshotOutput = runtimeNullString("5")
					row.PricingSnapshotCacheReadInput = runtimeNullString("11")
					row.PricingSnapshotCacheCreationInput = runtimeNullString("13")
					row.PricingSnapshotReasoning = runtimeNullString("17")
				})
			},
			assert: func(t *testing.T, harness *runtimeHarness, profileID int) {
				t.Helper()
				streamRow := loadLatestRuntimeRequestLogStreamTelemetryRow(t, harness.conn, profileID)
				if streamRow.StreamOutcome != "not_streaming" || streamRow.StreamErrorKind.Valid || streamRow.StreamErrorDetail.Valid || !streamRow.TotalCostUserCurrencyMicros.Valid || streamRow.TotalCostUserCurrencyMicros.Int64 != 50 || !streamRow.CompletionDurationMS.Valid {
					t.Fatalf("expected non-stream request log to persist not_streaming while preserving pricing/timing, got %+v", streamRow)
				}
			},
		},
		{
			name:           "priced zero distinct from unpriced",
			usage:          componentUsage,
			requestContent: "keep priced zero distinct from unpriced",
			template: func(reportCurrency runtimeReportCurrencySnapshot) *pricingTemplateSpec {
				return &pricingTemplateSpec{inputPrice: "0", outputPrice: "0", cachedInput: "0", cacheCreation: "0", reasoning: "0"}
			},
			want: func(reportCurrency runtimeReportCurrencySnapshot) runtimePersistedPricingRow {
				return wantPricedRow(func(row *runtimePersistedPricingRow) {
					row.InputTokens = runtimeNullInt64(6)
					row.OutputTokens = runtimeNullInt64(3)
					row.TotalTokens = runtimeNullInt64(16)
					row.CacheReadInputTokens = runtimeNullInt64(4)
					row.ReasoningTokens = runtimeNullInt64(3)
					row.InputCostMicros = runtimeNullInt64(0)
					row.OutputCostMicros = runtimeNullInt64(0)
					row.CacheReadInputCostMicros = runtimeNullInt64(0)
					row.CacheCreationInputCostMicros = runtimeNullInt64(0)
					row.ReasoningCostMicros = runtimeNullInt64(0)
					row.TotalCostOriginalMicros = runtimeNullInt64(0)
					row.TotalCostUserCurrencyMicros = runtimeNullInt64(0)
					row.CurrencyCodeOriginal = runtimeNullString(reportCurrency.Code)
					row.ReportCurrencyCode = runtimeNullString(reportCurrency.Code)
					row.ReportCurrencySymbol = runtimeNullString(reportCurrency.Symbol)
					row.PricingSnapshotInput = runtimeNullString("0")
					row.PricingSnapshotOutput = runtimeNullString("0")
					row.PricingSnapshotCacheReadInput = runtimeNullString("0")
					row.PricingSnapshotCacheCreationInput = runtimeNullString("0")
					row.PricingSnapshotReasoning = runtimeNullString("0")
				})
			},
			assert: func(t *testing.T, harness *runtimeHarness, profileID int) {
				t.Helper()
				listItem := loadSingleListItem(t, harness, profileID)
				if pricingStatus, ok := listItem["pricing_status"].(string); !ok || pricingStatus != "priced" {
					t.Fatalf("expected priced-zero request-log list row pricing_status=priced, got %+v", listItem)
				}
				if unpricedReason, ok := listItem["unpriced_reason"]; !ok || unpricedReason != nil {
					t.Fatalf("expected priced-zero request-log list row unpriced_reason=null, got %+v", listItem)
				}
				if jsonInt(t, listItem["total_cost_user_currency_micros"]) != 0 {
					t.Fatalf("expected priced-zero request-log list row total_cost_user_currency_micros=0, got %+v", listItem)
				}

				detailPayload := loadLatestRuntimeRequestLogDetailPayload(t, harness, profileID)
				pricing := asMapRuntime(t, detailPayload["pricing"])
				if pricingStatus, ok := pricing["pricing_status"].(string); !ok || pricingStatus != "priced" {
					t.Fatalf("expected priced-zero request-log detail pricing.pricing_status=priced, got %+v", pricing)
				}
				if unpricedReason, ok := pricing["unpriced_reason"]; !ok || unpricedReason != nil {
					t.Fatalf("expected priced-zero request-log detail pricing.unpriced_reason=null, got %+v", pricing)
				}
				if jsonInt(t, pricing["total_cost_user_currency_micros"]) != 0 {
					t.Fatalf("expected priced-zero request-log detail pricing.total_cost_user_currency_micros=0, got %+v", pricing)
				}
				if pricing["fx_rate_used"] != "1" || pricing["fx_rate_source"] != "DEFAULT_1_TO_1" {
					t.Fatalf("expected priced-zero request-log detail pricing fx provenance to stay explicit, got %+v", pricing)
				}

				spendingPayload := loadPayload(t, harness, profileID, "/api/stats/spending?preset=1h&group_by=none&limit=50&offset=0")
				summary := asMapRuntime(t, spendingPayload["summary"])
				if jsonInt(t, summary["successful_request_count"]) != 1 || jsonInt(t, summary["priced_request_count"]) != 1 || jsonInt(t, summary["unpriced_request_count"]) != 0 || jsonInt(t, summary["known_cost_micros"]) != 0 {
					t.Fatalf("expected priced-zero spending summary to stay priced with zero cost, got %+v", summary)
				}
				unpricedBreakdown := asMapRuntime(t, spendingPayload["unpriced_breakdown"])
				if len(unpricedBreakdown) != 0 {
					t.Fatalf("expected priced-zero spending breakdown to stay empty, got %+v", unpricedBreakdown)
				}
			},
		},
		{
			name:           "management normalized component prices",
			usage:          componentUsage,
			requestContent: "price management-normalized optional defaults",
			attachTemplate: func(t *testing.T, harness *runtimeHarness, profileID int, reportCurrency runtimeReportCurrencySnapshot, route seededRuntimeRoute, suffix string) {
				t.Helper()
				createResponse := harness.requestJSON(t, http.MethodPost, "/api/pricing-templates", map[string]any{
					"name":          "Runtime Management Normalized Components " + suffix,
					"template_kind": "standard",
					"card":          map[string]any{"input_price": "2", "output_price": "5", "cached_input_price": "0", "cache_creation_price": "0", "reasoning_price": "0"},
				}, runtimeModelHeader(profileID))
				assertStatus(t, createResponse, http.StatusCreated)
				var createdTemplate map[string]any
				decodeJSONResponse(t, createResponse, &createdTemplate)
				pricingTemplateID := jsonInt(t, createdTemplate["id"])
				createdCard := asMapRuntime(t, createdTemplate["card"])
				if createdCard["cached_input_price"] != "0" || createdCard["cache_creation_price"] != "0" || createdCard["reasoning_price"] != "0" {
					t.Fatalf("expected management-created zero component prices to round-trip as explicit free pricing, got %+v", createdTemplate)
				}
				attachRuntimeConnectionPricingTemplate(t, harness, route.ConnectionID, pricingTemplateID)
			},
			want: func(reportCurrency runtimeReportCurrencySnapshot) runtimePersistedPricingRow {
				return wantPricedRow(func(row *runtimePersistedPricingRow) {
					row.InputTokens = runtimeNullInt64(6)
					row.OutputTokens = runtimeNullInt64(3)
					row.TotalTokens = runtimeNullInt64(16)
					row.CacheReadInputTokens = runtimeNullInt64(4)
					row.ReasoningTokens = runtimeNullInt64(3)
					row.InputCostMicros = runtimeNullInt64(12)
					row.OutputCostMicros = runtimeNullInt64(15)
					row.CacheReadInputCostMicros = runtimeNullInt64(0)
					row.CacheCreationInputCostMicros = runtimeNullInt64(0)
					row.ReasoningCostMicros = runtimeNullInt64(0)
					row.TotalCostOriginalMicros = runtimeNullInt64(27)
					row.TotalCostUserCurrencyMicros = runtimeNullInt64(27)
					row.CurrencyCodeOriginal = runtimeNullString(reportCurrency.Code)
					row.ReportCurrencyCode = runtimeNullString(reportCurrency.Code)
					row.ReportCurrencySymbol = runtimeNullString(reportCurrency.Symbol)
					row.PricingSnapshotInput = runtimeNullString("2")
					row.PricingSnapshotOutput = runtimeNullString("5")
					row.PricingSnapshotCacheReadInput = runtimeNullString("0")
					row.PricingSnapshotCacheCreationInput = runtimeNullString("0")
					row.PricingSnapshotReasoning = runtimeNullString("0")
				})
			},
		},
		{
			name:  "pricing disabled",
			usage: baseUsage,
			want: func(reportCurrency runtimeReportCurrencySnapshot) runtimePersistedPricingRow {
				return wantUnpricedRow("PRICING_DISABLED", func(row *runtimePersistedPricingRow) {
					row.InputTokens = runtimeNullInt64(10)
					row.OutputTokens = runtimeNullInt64(6)
					row.TotalTokens = runtimeNullInt64(16)
				})
			},
		},
		{
			name:  "invalid required price",
			usage: baseUsage,
			template: func(reportCurrency runtimeReportCurrencySnapshot) *pricingTemplateSpec {
				return &pricingTemplateSpec{inputPrice: "not-a-decimal", outputPrice: "5", cachedInput: "0", cacheCreation: "0", reasoning: "0"}
			},
			want: func(reportCurrency runtimeReportCurrencySnapshot) runtimePersistedPricingRow {
				return wantUnpricedRow("MISSING_PRICE_DATA", func(row *runtimePersistedPricingRow) {
					row.InputTokens = runtimeNullInt64(10)
					row.OutputTokens = runtimeNullInt64(6)
					row.TotalTokens = runtimeNullInt64(16)
					row.PricingSnapshotUnit = sql.NullString{}
					row.PricingSnapshotInput = sql.NullString{}
					row.PricingSnapshotOutput = sql.NullString{}
					row.PricingSnapshotCacheReadInput = sql.NullString{}
					row.PricingSnapshotCacheCreationInput = sql.NullString{}
					row.PricingSnapshotReasoning = sql.NullString{}
					row.PricingConfigVersionUsed = sql.NullInt64{}
				})
			},
		},
		{
			name:  "missing fx",
			usage: baseUsage,
			template: func(reportCurrency runtimeReportCurrencySnapshot) *pricingTemplateSpec {
				missingFXCurrencyCode := "EUR"
				if reportCurrency.Code == missingFXCurrencyCode {
					missingFXCurrencyCode = "USD"
				}
				return &pricingTemplateSpec{currencyCode: missingFXCurrencyCode, inputPrice: "2", outputPrice: "5", cachedInput: "0", cacheCreation: "0", reasoning: "0"}
			},
			want: func(reportCurrency runtimeReportCurrencySnapshot) runtimePersistedPricingRow {
				return wantUnpricedRow("MISSING_PRICE_DATA", func(row *runtimePersistedPricingRow) {
					row.InputTokens = runtimeNullInt64(10)
					row.OutputTokens = runtimeNullInt64(6)
					row.TotalTokens = runtimeNullInt64(16)
					row.PricingSnapshotUnit = sql.NullString{}
					row.PricingSnapshotInput = sql.NullString{}
					row.PricingSnapshotOutput = sql.NullString{}
					row.PricingSnapshotCacheReadInput = sql.NullString{}
					row.PricingSnapshotCacheCreationInput = sql.NullString{}
					row.PricingSnapshotReasoning = sql.NullString{}
					row.PricingConfigVersionUsed = sql.NullInt64{}
				})
			},
		},
		{
			name: "missing required usage",
			template: func(reportCurrency runtimeReportCurrencySnapshot) *pricingTemplateSpec {
				return &pricingTemplateSpec{inputPrice: "2", outputPrice: "5", cachedInput: "0", cacheCreation: "0", reasoning: "0"}
			},
			want: func(reportCurrency runtimeReportCurrencySnapshot) runtimePersistedPricingRow {
				return wantUnpricedRow("MISSING_TOKEN_USAGE", func(row *runtimePersistedPricingRow) {
					row.PricingSnapshotUnit = sql.NullString{}
					row.PricingSnapshotInput = sql.NullString{}
					row.PricingSnapshotOutput = sql.NullString{}
					row.PricingSnapshotCacheReadInput = sql.NullString{}
					row.PricingSnapshotCacheCreationInput = sql.NullString{}
					row.PricingSnapshotReasoning = sql.NullString{}
					row.PricingConfigVersionUsed = sql.NullInt64{}
				})
			},
			assert: func(t *testing.T, harness *runtimeHarness, profileID int) {
				t.Helper()
				pricing := asMapRuntime(t, loadLatestRuntimeRequestLogDetailPayload(t, harness, profileID)["pricing"])
				if pricing["pricing_template_kind"] != "standard" || pricing["pricing_selection_state"] != "selected" || pricing["pricing_card_role"] != "standard" || pricing["pricing_selector_threshold_tokens"] != nil || pricing["pricing_selector_basis_tokens"] != nil {
					t.Fatalf("expected typed standard selector evidence without a price snapshot, got %+v", pricing)
				}
			},
		},
		{
			name: "degraded component pricing",
			usage: map[string]any{
				"prompt_tokens":     10,
				"completion_tokens": 6,
				"total_tokens":      16,
				"completion_tokens_details": map[string]any{
					"reasoning_tokens": 3,
				},
			},
			requestContent: "degrade invalid reasoning pricing",
			template: func(reportCurrency runtimeReportCurrencySnapshot) *pricingTemplateSpec {
				return &pricingTemplateSpec{inputPrice: "2", outputPrice: "5", cachedInput: "11", cacheCreation: "13", reasoning: "not-a-decimal"}
			},
			want: func(reportCurrency runtimeReportCurrencySnapshot) runtimePersistedPricingRow {
				return wantUnpricedRow("MISSING_PRICE_DATA", func(row *runtimePersistedPricingRow) {
					row.InputTokens = runtimeNullInt64(10)
					row.OutputTokens = runtimeNullInt64(3)
					row.TotalTokens = runtimeNullInt64(16)
					row.ReasoningTokens = runtimeNullInt64(3)
					row.PricingSnapshotUnit = sql.NullString{}
					row.PricingSnapshotInput = sql.NullString{}
					row.PricingSnapshotOutput = sql.NullString{}
					row.PricingSnapshotCacheReadInput = sql.NullString{}
					row.PricingSnapshotCacheCreationInput = sql.NullString{}
					row.PricingSnapshotReasoning = sql.NullString{}
					row.PricingConfigVersionUsed = sql.NullInt64{}
				})
			},
			assert: func(t *testing.T, harness *runtimeHarness, profileID int) {
				t.Helper()
				listItem := loadSingleListItem(t, harness, profileID)
				if pricingStatus, ok := listItem["pricing_status"].(string); !ok || pricingStatus != "unpriced" {
					t.Fatalf("expected degraded request-log list row pricing_status=unpriced, got %+v", listItem)
				}
				if listItem["unpriced_reason"] != "MISSING_PRICE_DATA" {
					t.Fatalf("expected degraded request-log list row unpriced_reason=MISSING_PRICE_DATA, got %+v", listItem)
				}
				if totalCost, ok := listItem["total_cost_user_currency_micros"]; !ok || totalCost != nil {
					t.Fatalf("expected degraded request-log list row total_cost_user_currency_micros=null, got %+v", listItem)
				}

				detailPayload := loadLatestRuntimeRequestLogDetailPayload(t, harness, profileID)
				usage := asMapRuntime(t, detailPayload["usage"])
				pricing := asMapRuntime(t, detailPayload["pricing"])
				if pricingStatus, ok := pricing["pricing_status"].(string); !ok || pricingStatus != "unpriced" {
					t.Fatalf("expected degraded request-log detail pricing.pricing_status=unpriced, got %+v", pricing)
				}
				if pricing["unpriced_reason"] != "MISSING_PRICE_DATA" || jsonInt(t, usage["reasoning_tokens"]) != 3 {
					t.Fatalf("expected degraded request-log detail usage payload to expose missing-price reasoning tokens, got %+v", usage)
				}
				if totalCost, ok := pricing["total_cost_user_currency_micros"]; !ok || totalCost != nil {
					t.Fatalf("expected degraded request-log detail pricing.total_cost_user_currency_micros=null, got %+v", pricing)
				}

				usageSnapshotPayload := loadPayload(t, harness, profileID, "/api/stats/usage-snapshot?preset=1h")
				overview := asMapRuntime(t, usageSnapshotPayload["overview"])
				if jsonInt(t, overview["success_requests"]) != 1 || jsonInt(t, overview["reasoning_tokens"]) != 3 || jsonInt(t, overview["total_cost_micros"]) != 0 {
					t.Fatalf("expected degraded usage snapshot overview to keep reasoning tokens but zero cost, got %+v", overview)
				}
				costOverview := asMapRuntime(t, usageSnapshotPayload["cost_overview"])
				if jsonInt(t, costOverview["priced_request_count"]) != 0 || jsonInt(t, costOverview["unpriced_request_count"]) != 1 {
					t.Fatalf("expected degraded usage snapshot cost overview to count one unpriced request, got %+v", costOverview)
				}
				modelStatistics, ok := usageSnapshotPayload["model_statistics"].([]any)
				if !ok || len(modelStatistics) != 1 {
					t.Fatalf("expected one degraded usage snapshot model row, got %+v", usageSnapshotPayload)
				}
				modelRow := asMapRuntime(t, modelStatistics[0])
				if jsonInt(t, modelRow["priced_request_count"]) != 0 || jsonInt(t, modelRow["unpriced_request_count"]) != 1 || jsonInt(t, modelRow["total_cost_micros"]) != 0 {
					t.Fatalf("expected degraded usage snapshot model statistics to preserve unpriced counts, got %+v", modelRow)
				}

				spendingPayload := loadPayload(t, harness, profileID, "/api/stats/spending?preset=1h&group_by=none&limit=50&offset=0")
				summary := asMapRuntime(t, spendingPayload["summary"])
				if jsonInt(t, summary["successful_request_count"]) != 1 || jsonInt(t, summary["priced_request_count"]) != 0 || jsonInt(t, summary["unpriced_request_count"]) != 1 || jsonInt(t, summary["total_reasoning_tokens"]) != 3 || summary["known_cost_micros"] != nil {
					t.Fatalf("expected degraded spending summary to stay unpriced with zero cost, got %+v", summary)
				}
				unpricedBreakdown := asMapRuntime(t, spendingPayload["unpriced_breakdown"])
				if jsonInt(t, unpricedBreakdown["MISSING_PRICE_DATA"]) != 1 {
					t.Fatalf("expected degraded spending breakdown to count MISSING_PRICE_DATA, got %+v", unpricedBreakdown)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			harness := newRuntimeHarness(t)
			profileID := harness.activeProfileID(t)
			reportCurrency := loadRuntimeReportCurrencySnapshot(t, harness.conn, profileID)
			suffix := randomSuffix()
			slug := strings.ReplaceAll(test.name, " ", "-")
			responseBody := map[string]any{
				"id":     "chatcmpl-runtime-pricing-" + slug + "-" + suffix,
				"object": "chat.completion",
			}
			if test.usage != nil {
				responseBody["usage"] = test.usage
			}
			upstream := newScriptedUpstream(t, http.StatusOK, responseBody)
			route := harness.seedProxyRoute(t, runtimeRouteSeed{
				ProfileID:       profileID,
				APIFamily:       "openai",
				PublicModelID:   slug + "-public-" + suffix,
				TargetModelID:   slug + "-target-" + suffix,
				EndpointBaseURL: upstream.baseURL("/request-logs/pricing/" + slug),
				EndpointAPIKey:  "runtime-pricing-" + slug + "-key",
			})

			switch {
			case test.attachTemplate != nil:
				test.attachTemplate(t, harness, profileID, reportCurrency, route, suffix)
			case test.template != nil:
				spec := test.template(reportCurrency)
				currencyCode := spec.currencyCode
				if currencyCode == "" {
					currencyCode = reportCurrency.Code
				}
				pricingTemplateID := insertRuntimePricingTemplate(t, harness.conn, profileID, "runtime-pricing-"+slug+"-"+suffix, currencyCode, spec.inputPrice, spec.outputPrice, spec.cachedInput, spec.cacheCreation, spec.reasoning)
				attachRuntimeConnectionPricingTemplate(t, harness, route.ConnectionID, pricingTemplateID)
			}

			requestContent := test.requestContent
			if requestContent == "" {
				requestContent = "preserve " + test.name
			}
			response := harness.requestJSON(
				t,
				http.MethodPost,
				"/v1/chat/completions",
				map[string]any{
					"messages": []map[string]any{{"role": "user", "content": requestContent}},
					"model":    route.PublicModelID,
				},
				nil,
			)
			assertStatus(t, response, http.StatusOK)
			if got := len(upstream.requestsSnapshot()); got != 1 {
				t.Fatalf("expected %s request to hit upstream exactly once, got %d", test.name, got)
			}
			waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)
			assertLatestRuntimeAttemptCounts(t, harness.conn, profileID, 1, 1)
			assertLatestRuntimePricingRows(t, harness.conn, profileID, test.want(reportCurrency), test.name)
			if test.assert != nil {
				test.assert(t, harness, profileID)
			}
		})
	}
}

func TestRuntimePricingTierPersistsWholeCardEvidence(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	upstream := newScriptedUpstream(t, http.StatusOK, map[string]any{
		"id": "chatcmpl-runtime-tier-" + suffix,
		"usage": map[string]any{
			"prompt_tokens":             272001,
			"completion_tokens":         10,
			"total_tokens":              272011,
			"completion_tokens_details": map[string]any{"reasoning_tokens": 2},
		},
	})
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID: profileID, APIFamily: "openai", PublicModelID: "runtime-tier-public-" + suffix,
		TargetModelID: "runtime-tier-target-" + suffix, EndpointBaseURL: upstream.baseURL("/request-logs/pricing/tier"), EndpointAPIKey: "runtime-tier-key-" + suffix,
	})
	templateID := insertRuntimePricingTemplate(t, harness.conn, profileID, "runtime-tier-template-"+suffix, loadRuntimeReportCurrencyCode(t, harness.conn, profileID), "2", "5", "1", "2", "3")
	advanceRuntimePricingTemplateRevisionWithTier(t, harness.conn, templateID)
	attachRuntimeConnectionPricingTemplate(t, harness, route.ConnectionID, templateID)

	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"model":    route.PublicModelID,
		"messages": []map[string]any{{"role": "user", "content": "cross the input threshold"}},
	}, nil)
	assertStatus(t, response, http.StatusOK)
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)

	var kind, state, role string
	var threshold int
	var basis int64
	var snapshotInput, snapshotOutput, snapshotReasoning string
	var inputCost, outputCost, reasoningCost, totalCost int64
	if err := harness.conn.QueryRow(context.Background(), `SELECT pricing_template_kind, pricing_selection_state, pricing_card_role, pricing_selector_threshold_tokens, pricing_selector_basis_tokens, pricing_snapshot_input, pricing_snapshot_output, pricing_snapshot_reasoning, input_cost_micros, output_cost_micros, reasoning_cost_micros, total_cost_original_micros FROM request_logs WHERE profile_id = $1 AND ingress_request_id = (SELECT ingress_request_id FROM request_logs WHERE profile_id = $1 ORDER BY created_at DESC, id DESC LIMIT 1) ORDER BY created_at DESC, id DESC LIMIT 1`, profileID).Scan(&kind, &state, &role, &threshold, &basis, &snapshotInput, &snapshotOutput, &snapshotReasoning, &inputCost, &outputCost, &reasoningCost, &totalCost); err != nil {
		t.Fatalf("load tiered request-log pricing evidence: %v", err)
	}
	if kind != "tiered" || state != "selected" || role != "tier_above" || threshold != 272000 || basis != 272001 || snapshotInput != "4" || snapshotOutput != "18" || snapshotReasoning != "20" {
		t.Fatalf("expected typed card evidence and all five selected rates, got kind=%q state=%q role=%q threshold=%d basis=%d snapshots=%q/%q/%q", kind, state, role, threshold, basis, snapshotInput, snapshotOutput, snapshotReasoning)
	}
	if inputCost != 1088004 || outputCost != 144 || reasoningCost != 40 || totalCost != 1088188 {
		t.Fatalf("expected whole-card non-marginal costs, got input=%d output=%d reasoning=%d total=%d", inputCost, outputCost, reasoningCost, totalCost)
	}

	var usageKind, usageState, usageRole string
	var usageThreshold int
	var usageBasis int64
	if err := harness.conn.QueryRow(context.Background(), `SELECT pricing_template_kind, pricing_selection_state, pricing_card_role, pricing_selector_threshold_tokens, pricing_selector_basis_tokens FROM usage_request_events WHERE profile_id = $1 ORDER BY created_at DESC, id DESC LIMIT 1`, profileID).Scan(&usageKind, &usageState, &usageRole, &usageThreshold, &usageBasis); err != nil {
		t.Fatalf("load tiered usage-event evidence: %v", err)
	}
	if usageKind != "tiered" || usageState != "selected" || usageRole != "tier_above" || usageThreshold != 272000 || usageBasis != 272001 {
		t.Fatalf("expected usage-event typed card evidence, got kind=%q state=%q role=%q threshold=%d basis=%d", usageKind, usageState, usageRole, usageThreshold, usageBasis)
	}
	pricing := asMapRuntime(t, loadLatestRuntimeRequestLogDetailPayload(t, harness, profileID)["pricing"])
	if pricing["pricing_template_kind"] != "tiered" || pricing["pricing_selection_state"] != "selected" || pricing["pricing_card_role"] != "tier_above" || jsonInt(t, pricing["pricing_selector_threshold_tokens"]) != 272000 || jsonInt(t, pricing["pricing_selector_basis_tokens"]) != 272001 {
		t.Fatalf("expected request-log detail tier projection, got %+v", pricing)
	}
}

type runtimePersistedPricingRow struct {
	AttemptMetric                     int
	PricingStatus                     sql.NullString
	PricingEvidenceTrust              sql.NullString
	UnpricedReason                    sql.NullString
	InputTokens                       sql.NullInt64
	OutputTokens                      sql.NullInt64
	TotalTokens                       sql.NullInt64
	CacheReadInputTokens              sql.NullInt64
	CacheCreationInputTokens          sql.NullInt64
	ReasoningTokens                   sql.NullInt64
	InputCostMicros                   sql.NullInt64
	OutputCostMicros                  sql.NullInt64
	CacheReadInputCostMicros          sql.NullInt64
	CacheCreationInputCostMicros      sql.NullInt64
	ReasoningCostMicros               sql.NullInt64
	TotalCostOriginalMicros           sql.NullInt64
	TotalCostUserCurrencyMicros       sql.NullInt64
	CurrencyCodeOriginal              sql.NullString
	ReportCurrencyCode                sql.NullString
	ReportCurrencySymbol              sql.NullString
	FXRateUsed                        sql.NullString
	FXRateSource                      sql.NullString
	PricingSnapshotUnit               sql.NullString
	PricingSnapshotInput              sql.NullString
	PricingSnapshotOutput             sql.NullString
	PricingSnapshotCacheReadInput     sql.NullString
	PricingSnapshotCacheCreationInput sql.NullString
	PricingSnapshotReasoning          sql.NullString
	PricingConfigVersionUsed          sql.NullInt64
}

func wantRuntimePricingRow(mutate func(*runtimePersistedPricingRow)) runtimePersistedPricingRow {
	row := runtimePersistedPricingRow{
		AttemptMetric:        1,
		PricingStatus:        runtimeNullString("priced"),
		PricingEvidenceTrust: runtimeNullString("trusted"),
	}
	if mutate != nil {
		mutate(&row)
	}
	return row
}

func wantPricedRow(mutate func(*runtimePersistedPricingRow)) runtimePersistedPricingRow {
	return wantRuntimePricingRow(func(row *runtimePersistedPricingRow) {
		row.PricingStatus = runtimeNullString("priced")
		row.FXRateUsed = runtimeNullString("1")
		row.FXRateSource = runtimeNullString("DEFAULT_1_TO_1")
		row.PricingSnapshotUnit = runtimeNullString("PER_1M")
		row.PricingConfigVersionUsed = runtimeNullInt64(1)
		if mutate != nil {
			mutate(row)
		}
	})
}

func wantUnpricedRow(reason string, mutate func(*runtimePersistedPricingRow)) runtimePersistedPricingRow {
	return wantRuntimePricingRow(func(row *runtimePersistedPricingRow) {
		row.PricingStatus = runtimeNullString("unpriced")
		row.UnpricedReason = runtimeNullString(reason)
		if mutate != nil {
			mutate(row)
		}
	})
}

func assertRuntimePricingRowDiscardedUsage(t *testing.T, row runtimePersistedPricingRow, wantReason string) {
	t.Helper()
	if row.InputTokens.Valid || row.OutputTokens.Valid || row.TotalTokens.Valid || row.CacheReadInputTokens.Valid || row.CacheCreationInputTokens.Valid || row.ReasoningTokens.Valid {
		t.Fatalf("expected token usage to be discarded, got %+v", row)
	}
	if row.InputCostMicros.Valid || row.OutputCostMicros.Valid || row.CacheReadInputCostMicros.Valid || row.CacheCreationInputCostMicros.Valid || row.ReasoningCostMicros.Valid || row.TotalCostOriginalMicros.Valid || row.TotalCostUserCurrencyMicros.Valid {
		t.Fatalf("expected discarded usage to skip pricing costs, got %+v", row)
	}
	if !row.PricingStatus.Valid || row.PricingStatus.String != "unpriced" || !row.UnpricedReason.Valid || row.UnpricedReason.String != wantReason {
		t.Fatalf("expected discarded usage to be unpriced as %s, got %+v", wantReason, row)
	}
}

const runtimePersistedPricingRowSelectColumns = `pricing_status, pricing_evidence_trust, unpriced_reason, input_tokens, output_tokens, total_tokens, cache_read_input_tokens, cache_creation_input_tokens, reasoning_tokens, input_cost_micros, output_cost_micros, cache_read_input_cost_micros, cache_creation_input_cost_micros, reasoning_cost_micros, total_cost_original_micros, total_cost_user_currency_micros, currency_code_original, report_currency_code, report_currency_symbol, fx_rate_used, fx_rate_source, pricing_snapshot_unit, pricing_snapshot_input, pricing_snapshot_output, pricing_snapshot_cache_read_input, pricing_snapshot_cache_creation_input, pricing_snapshot_reasoning, pricing_config_version_used`

func loadLatestRuntimePricingRowForTable(t *testing.T, conn *pgx.Conn, profileID int, tableName string) runtimePersistedPricingRow {
	t.Helper()
	ingressRequestID := loadLatestRuntimeIngressRequestID(t, conn, profileID)
	attemptMetricColumn := "attempt_number"
	orderBy := "attempt_number DESC, id DESC"
	switch tableName {
	case "request_logs":
	case "usage_request_events":
		attemptMetricColumn = "attempt_count"
		orderBy = "id DESC"
	default:
		t.Fatalf("unsupported runtime pricing table %q", tableName)
	}
	query := fmt.Sprintf(
		`SELECT %s, %s FROM %s WHERE profile_id = $1 AND ingress_request_id = $2 ORDER BY %s LIMIT 1`,
		attemptMetricColumn,
		runtimePersistedPricingRowSelectColumns,
		tableName,
		orderBy,
	)
	var row runtimePersistedPricingRow
	if err := conn.QueryRow(context.Background(), query, profileID, ingressRequestID).Scan(
		&row.AttemptMetric,
		&row.PricingStatus,
		&row.PricingEvidenceTrust,
		&row.UnpricedReason,
		&row.InputTokens,
		&row.OutputTokens,
		&row.TotalTokens,
		&row.CacheReadInputTokens,
		&row.CacheCreationInputTokens,
		&row.ReasoningTokens,
		&row.InputCostMicros,
		&row.OutputCostMicros,
		&row.CacheReadInputCostMicros,
		&row.CacheCreationInputCostMicros,
		&row.ReasoningCostMicros,
		&row.TotalCostOriginalMicros,
		&row.TotalCostUserCurrencyMicros,
		&row.CurrencyCodeOriginal,
		&row.ReportCurrencyCode,
		&row.ReportCurrencySymbol,
		&row.FXRateUsed,
		&row.FXRateSource,
		&row.PricingSnapshotUnit,
		&row.PricingSnapshotInput,
		&row.PricingSnapshotOutput,
		&row.PricingSnapshotCacheReadInput,
		&row.PricingSnapshotCacheCreationInput,
		&row.PricingSnapshotReasoning,
		&row.PricingConfigVersionUsed,
	); err != nil {
		t.Fatalf("load latest runtime pricing row from %s: %v", tableName, err)
	}
	return row
}

func assertLatestRuntimePricingRows(t *testing.T, conn *pgx.Conn, profileID int, want runtimePersistedPricingRow, label string) {
	t.Helper()
	requestLogRow := loadLatestRuntimePricingRowForTable(t, conn, profileID, "request_logs")
	if requestLogRow != want {
		t.Fatalf("expected %s request_logs pricing row %+v, got %+v", label, want, requestLogRow)
	}
	usageEventRow := loadLatestRuntimePricingRowForTable(t, conn, profileID, "usage_request_events")
	if usageEventRow != want {
		t.Fatalf("expected %s usage_request_events pricing row %+v, got %+v", label, want, usageEventRow)
	}
	if requestLogRow != usageEventRow {
		t.Fatalf("expected %s request_logs and usage_request_events rows to agree, got request_logs=%+v usage_request_events=%+v", label, requestLogRow, usageEventRow)
	}
}
