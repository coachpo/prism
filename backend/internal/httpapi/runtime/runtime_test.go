package runtime

import (
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestModelResolutionAndRewriteHelpers(t *testing.T) {
	rawBody := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`)
	if got, err := resolveModelID(rawBody, "/v1/chat/completions"); err != nil || got != "gpt-4o" {
		t.Fatalf("expected body model id, got model=%q err=%v", got, err)
	}
	if got, err := resolveModelID(nil, "/v1beta/models/gemini-2.5-pro:generateContent"); err != nil || got != "gemini-2.5-pro" {
		t.Fatalf("expected path model id, got model=%q err=%v", got, err)
	}
	if got := extractModelFromPath("/v1beta/models/gemini-2.5-pro:generateContent"); got != "gemini-2.5-pro" {
		t.Fatalf("expected path extraction to return Gemini model id, got %q", got)
	}

	rewrittenBody := rewriteModelInBody(rawBody, "gpt-4o-mini")
	if got := extractModelFromBody(rewrittenBody); got != "gpt-4o-mini" {
		t.Fatalf("expected rewritten model id in body, got %q", got)
	}
	if got := rewriteModelInPath("/v1beta/models/gemini-1.5-pro:generateContent", "gemini-1.5-pro", "gemini-2.5-pro"); got != "/v1beta/models/gemini-2.5-pro:generateContent" {
		t.Fatalf("expected rewritten Gemini path, got %q", got)
	}
	if _, err := resolveModelID([]byte(`{"messages":[]}`), "/v1/chat/completions"); err == nil {
		t.Fatal("expected missing model id to fail")
	}
}

func TestValidatePathCompatibilityAndHeaderHelpers(t *testing.T) {
	if err := validatePathCompatibility("openai", "/v1/chat/completions"); err != nil {
		t.Fatalf("expected OpenAI generic path to be valid, got %v", err)
	}
	if err := validatePathCompatibility("anthropic", "/v1/messages"); err != nil {
		t.Fatalf("expected Anthropic messages path to be valid, got %v", err)
	}
	if err := validatePathCompatibility("gemini", "/v1beta/models/gemini-2.5-pro:generateContent"); err != nil {
		t.Fatalf("expected Gemini native path to be valid, got %v", err)
	}

	err := validatePathCompatibility("openai", "/v1beta/models/gemini-2.5-pro:generateContent")
	var domainErr *domainError
	if !errors.As(err, &domainErr) || domainErr.StatusCode != http.StatusBadRequest || !strings.Contains(domainErr.Detail, "incompatible") {
		t.Fatalf("expected incompatibility domain error, got %v", err)
	}

	if got, ok := normalizeHeaderValue("  keep  "); !ok || got != "keep" {
		t.Fatalf("expected normalized header value, got value=%q ok=%v", got, ok)
	}
	if _, ok := normalizeHeaderValue("bad\nvalue"); ok {
		t.Fatal("expected control-character header value to be rejected")
	}

	rules := []headerBlocklistRule{{MatchType: "exact", Pattern: "x-remove"}, {MatchType: "prefix", Pattern: "x-secret-"}}
	sanitized := sanitizeHeaders(map[string]string{"X-Trace-Id": "1", "x-secret-token": "blocked", "X-Remove": "gone"}, rules)
	if !reflect.DeepEqual(sanitized, map[string]string{"X-Trace-Id": "1"}) {
		t.Fatalf("expected blocklisted headers to be removed, got %v", sanitized)
	}

	filtered := filterResponseHeaders(http.Header{"Connection": []string{"keep-alive"}, "X-Request-Id": []string{"abc"}})
	if filtered.Get("Connection") != "" || filtered.Get("X-Request-Id") != "abc" {
		t.Fatalf("expected hop-by-hop response headers to be filtered, got %v", filtered)
	}
}

func TestBuildRuntimePricingResult(t *testing.T) {
	cachedInputPrice := "1"
	cacheCreationPrice := "2"
	reasoningPrice := "3"
	pricingTemplateSnapshot := &runtimePricingTemplateSnapshot{
		ID:                  42,
		PricingUnit:         runtimePricingUnitPerMillion,
		PricingCurrencyCode: "USD",
		InputPrice:          "2",
		OutputPrice:         "5",
		CachedInputPrice:    &cachedInputPrice,
		CacheCreationPrice:  &cacheCreationPrice,
		ReasoningPrice:      &reasoningPrice,
		Version:             7,
	}
	reportCurrencySnapshot := runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$"}
	zero := 0
	positiveCacheRead := 4
	positiveCacheCreation := 5
	positiveReasoning := 6
	inputTokens := 10
	outputTokens := 10
	totalTokens := 20

	tests := []struct {
		name  string
		usage responseUsage
		want  runtimePricingResult
	}{
		{
			name: "prices base usage when optional counters are omitted",
			usage: responseUsage{
				InputTokens:  &inputTokens,
				OutputTokens: &outputTokens,
				TotalTokens:  &totalTokens,
			},
			want: runtimePricingResult{
				Billable:                          true,
				Priced:                            true,
				InputCostMicros:                   int64Ptr(20),
				OutputCostMicros:                  int64Ptr(50),
				CacheReadInputCostMicros:          int64Ptr(0),
				CacheCreationInputCostMicros:      int64Ptr(0),
				ReasoningCostMicros:               int64Ptr(0),
				TotalCostOriginalMicros:           int64Ptr(70),
				TotalCostUserCurrencyMicros:       int64Ptr(70),
				CurrencyCodeOriginal:              stringPtr("USD"),
				ReportCurrencyCode:                stringPtr("USD"),
				ReportCurrencySymbol:              stringPtr("$"),
				FXRateUsed:                        stringPtr("1"),
				FXRateSource:                      stringPtr(runtimeFXSourceDefaultOneToOne),
				PricingSnapshotUnit:               stringPtr(runtimePricingUnitPerMillion),
				PricingSnapshotInput:              stringPtr("2"),
				PricingSnapshotOutput:             stringPtr("5"),
				PricingSnapshotCacheReadInput:     stringPtr("1"),
				PricingSnapshotCacheCreationInput: stringPtr("2"),
				PricingSnapshotReasoning:          stringPtr("3"),
				PricingConfigVersionUsed:          intPtr(7),
			},
		},
		{
			name: "keeps missing token usage for missing required base usage",
			usage: responseUsage{
				OutputTokens: &outputTokens,
				TotalTokens:  &outputTokens,
			},
			want: runtimePricingResult{
				Billable:       true,
				UnpricedReason: stringPtr(runtimeUnpricedReasonMissingUsage),
			},
		},
		{
			name: "prices optional counters explicitly set to zero",
			usage: responseUsage{
				InputTokens:              &inputTokens,
				OutputTokens:             &outputTokens,
				TotalTokens:              &totalTokens,
				CacheReadInputTokens:     &zero,
				CacheCreationInputTokens: &zero,
				ReasoningTokens:          &zero,
			},
			want: runtimePricingResult{
				Billable:                          true,
				Priced:                            true,
				InputCostMicros:                   int64Ptr(20),
				OutputCostMicros:                  int64Ptr(50),
				CacheReadInputCostMicros:          int64Ptr(0),
				CacheCreationInputCostMicros:      int64Ptr(0),
				ReasoningCostMicros:               int64Ptr(0),
				TotalCostOriginalMicros:           int64Ptr(70),
				TotalCostUserCurrencyMicros:       int64Ptr(70),
				CurrencyCodeOriginal:              stringPtr("USD"),
				ReportCurrencyCode:                stringPtr("USD"),
				ReportCurrencySymbol:              stringPtr("$"),
				FXRateUsed:                        stringPtr("1"),
				FXRateSource:                      stringPtr(runtimeFXSourceDefaultOneToOne),
				PricingSnapshotUnit:               stringPtr(runtimePricingUnitPerMillion),
				PricingSnapshotInput:              stringPtr("2"),
				PricingSnapshotOutput:             stringPtr("5"),
				PricingSnapshotCacheReadInput:     stringPtr("1"),
				PricingSnapshotCacheCreationInput: stringPtr("2"),
				PricingSnapshotReasoning:          stringPtr("3"),
				PricingConfigVersionUsed:          intPtr(7),
			},
		},
		{
			name: "prices positive optional counters independently",
			usage: responseUsage{
				InputTokens:              &inputTokens,
				OutputTokens:             &outputTokens,
				TotalTokens:              &totalTokens,
				CacheReadInputTokens:     &positiveCacheRead,
				CacheCreationInputTokens: &positiveCacheCreation,
				ReasoningTokens:          &positiveReasoning,
			},
			want: runtimePricingResult{
				Billable:                          true,
				Priced:                            true,
				InputCostMicros:                   int64Ptr(20),
				OutputCostMicros:                  int64Ptr(50),
				CacheReadInputCostMicros:          int64Ptr(4),
				CacheCreationInputCostMicros:      int64Ptr(10),
				ReasoningCostMicros:               int64Ptr(18),
				TotalCostOriginalMicros:           int64Ptr(102),
				TotalCostUserCurrencyMicros:       int64Ptr(102),
				CurrencyCodeOriginal:              stringPtr("USD"),
				ReportCurrencyCode:                stringPtr("USD"),
				ReportCurrencySymbol:              stringPtr("$"),
				FXRateUsed:                        stringPtr("1"),
				FXRateSource:                      stringPtr(runtimeFXSourceDefaultOneToOne),
				PricingSnapshotUnit:               stringPtr(runtimePricingUnitPerMillion),
				PricingSnapshotInput:              stringPtr("2"),
				PricingSnapshotOutput:             stringPtr("5"),
				PricingSnapshotCacheReadInput:     stringPtr("1"),
				PricingSnapshotCacheCreationInput: stringPtr("2"),
				PricingSnapshotReasoning:          stringPtr("3"),
				PricingConfigVersionUsed:          intPtr(7),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := buildRuntimePricingResult(reportCurrencySnapshot, pricingTemplateSnapshot, nil, test.usage)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("expected pricing result %+v, got %+v", test.want, got)
			}
		})
	}
}
