package registry

import (
	"errors"
	"testing"
)

func TestRegistryRecordValidationReturnsTypedDeterministicErrors(t *testing.T) {
	tests := []struct {
		name      string
		validate  func() error
		wantCodes []string
	}{
		{
			name: "model profile",
			validate: func() error {
				return ModelProfile{ID: "public-model", APIFamily: " ", ContextWindowTokens: 1024, DefaultOutputTokenReserve: 1024, MaxContextUtilization: 1.2, RedirectModelID: "public-model"}.Validate()
			},
			wantCodes: []string{"model_profile_api_family_empty", "model_profile_output_reserve_invalid", "model_profile_context_utilization_invalid", "model_profile_redirect_self"},
		},
		{
			name: "upstream endpoint",
			validate: func() error {
				return UpstreamEndpoint{ID: "upstream-a", APIFamily: "openai", BaseURL: "://bad", SupportedOperations: []string{"openai.responses", "openai.responses"}, QPSLimit: -1}.Validate()
			},
			wantCodes: []string{"upstream_endpoint_base_url_invalid", "upstream_endpoint_supported_operations_duplicate", "upstream_endpoint_qps_limit_invalid"},
		},
		{
			name: "route rule",
			validate: func() error {
				return RouteRule{ID: "rule-a", PolicyID: " ", RedirectModelID: "model-b", RedirectUpstreamID: "upstream-a"}.Validate()
			},
			wantCodes: []string{"route_rule_policy_id_empty", "route_rule_match_empty", "route_rule_redirect_ambiguous"},
		},
		{
			name: "route policy",
			validate: func() error {
				return RoutePolicy{ID: "policy-a", Strategy: "random", RetryStatusCodes: []int{503, 429}, MaxAttempts: 0}.Validate()
			},
			wantCodes: []string{"route_policy_strategy_invalid", "route_policy_max_attempts_invalid", "route_policy_retry_status_order_invalid"},
		},
		{
			name: "route plan",
			validate: func() error {
				return RoutePlan{OperationName: "openai.responses", RequestedModelID: "public-a", EffectiveModelID: "public-a", RouteReason: "late_inference", PriceCatalogVersion: PriceCatalogVersion{CatalogID: "catalog", Version: 1}}.Validate()
			},
			wantCodes: []string{"route_plan_candidate_upstreams_empty", "route_plan_reason_invalid", "route_plan_attempts_empty"},
		},
		{
			name: "price catalog",
			validate: func() error {
				return PriceCatalog{Version: PriceCatalogVersion{CatalogID: "tokens", Version: 0}, CurrencyCode: "usd", Unit: PriceUnitPerMillionTokens, InputPriceMicrosPerUnit: -1, ImageOutputPriceNanosPerUnit: -2}.Validate()
			},
			wantCodes: []string{"price_catalog_version_invalid", "price_catalog_currency_invalid", "price_catalog_input_micros_invalid", "price_catalog_image_output_nanos_invalid"},
		},
		{
			name: "provider capability",
			validate: func() error {
				return ProviderCapability{ProviderID: "openai", APIFamily: "openai", NativeOperations: []string{"openai.responses"}, StreamingOperations: []string{"openai.chat_completions"}}.Validate()
			},
			wantCodes: []string{"provider_capability_streaming_not_native"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.validate()
			var validationErr ValidationError
			if !errors.As(err, &validationErr) {
				t.Fatalf("expected ValidationError, got %T: %v", err, err)
			}
			for _, code := range test.wantCodes {
				if !validationErr.HasCode(code) {
					t.Fatalf("expected code %q in %+v", code, validationErr.Issues)
				}
			}
			if validationErr.Error() != err.Error() {
				t.Fatalf("expected deterministic Error string")
			}
		})
	}
}

func TestPriceCatalogVersioningContractUsesIntegerMicrosAndNanos(t *testing.T) {
	catalog := PriceCatalog{
		Version:                              PriceCatalogVersion{CatalogID: "profile-default", Version: 3},
		CurrencyCode:                         "USD",
		Unit:                                 PriceUnitPerMillionTokens,
		InputPriceMicrosPerUnit:              2_000_000,
		OutputPriceMicrosPerUnit:             5_000_000,
		CacheReadInputPriceMicrosPerUnit:     500_000,
		CacheCreationInputPriceMicrosPerUnit: 750_000,
		ReasoningPriceMicrosPerUnit:          1_250_000,
		ImageInputPriceNanosPerUnit:          25_000_000,
		ImageOutputPriceNanosPerUnit:         50_000_000,
	}
	if err := catalog.Validate(); err != nil {
		t.Fatalf("expected price catalog to validate: %v", err)
	}
	if catalog.Version.CatalogID != "profile-default" || catalog.Version.Version != 3 {
		t.Fatalf("expected explicit price catalog version, got %+v", catalog.Version)
	}
}
