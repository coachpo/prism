package connections

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

func connectionStringRef(value string) *string {
	return &value
}

func connectionIntRef(value int) *int {
	return &value
}

func requireConnectionDomainError(t *testing.T, err error, status int, detail string) {
	t.Helper()

	var domainErr *DomainError
	if !errors.As(err, &domainErr) {
		t.Fatalf("expected DomainError, got %T", err)
	}
	if domainErr.StatusCode != status || domainErr.Detail != detail {
		t.Fatalf("expected DomainError (%d, %q), got (%d, %q)", status, detail, domainErr.StatusCode, domainErr.Detail)
	}
}

func TestValidateLimiterAndAuthType(t *testing.T) {
	if err := validateLimiter("qps_limit", nil); err != nil {
		t.Fatalf("expected nil limiter to pass, got %v", err)
	}
	if err := validateLimiter("qps_limit", connectionIntRef(1)); err != nil {
		t.Fatalf("expected positive limiter to pass, got %v", err)
	}
	if err := validateLimiter("qps_limit", connectionIntRef(0)); err == nil {
		t.Fatal("expected zero limiter to fail")
	} else {
		requireConnectionDomainError(t, err, http.StatusUnprocessableEntity, "qps_limit must be >= 1 when provided")
	}
	if got, err := validateAuthType(connectionStringRef(" Anthropic ")); err != nil || got == nil || *got != "anthropic" {
		t.Fatalf("expected normalized auth type, got value=%#v err=%v", got, err)
	}
	if _, err := validateAuthType(connectionStringRef(" ")); err == nil {
		t.Fatal("expected blank auth type to fail")
	} else {
		requireConnectionDomainError(t, err, http.StatusUnprocessableEntity, "auth_type must be one of 'openai', 'anthropic', 'gemini', or 'gemini_api_key'")
	}
}

func TestConnectionRequestsRejectOpenAIProbeEndpointVariant(t *testing.T) {
	for _, test := range []struct {
		name   string
		target any
	}{
		{name: "create", target: &connectionCreateRequest{}},
		{name: "update", target: &connectionUpdateRequest{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/models/1/connections", strings.NewReader(`{"openai_probe_endpoint_variant":"responses_minimal"}`))
			if err := decodeJSONBody(request, test.target); err == nil {
				t.Fatal("expected removed probe variant field to be rejected")
			}
		})
	}
}

func TestConnectionHelpers(t *testing.T) {
	if got := normalizeHeaders(map[string]string{}); got != nil {
		t.Fatalf("expected empty headers to normalize to nil, got %#v", got)
	}
	headers := map[string]string{"X-Test": "1"}
	if got := normalizeHeaders(headers); got == nil || got["X-Test"] != "1" {
		t.Fatalf("expected headers to pass through, got %#v", got)
	}
	if got := normalizeOptionalString(connectionStringRef(" value ")); got == nil || *got != " value " {
		t.Fatalf("expected optional string to preserve its raw value, got %#v", got)
	}
	if !reflect.DeepEqual(dedupeIntValues([]int{3, 1, 3, 2, 1}), []int{3, 1, 2}) {
		t.Fatalf("expected deduped ints to preserve first-seen order")
	}
}

func TestResolveOpenAITextCapability(t *testing.T) {
	if got, err := resolveOpenAITextCapabilityCreate("openai", connectionStringRef(" dual_native ")); err != nil || got == nil || *got != "dual_native" {
		t.Fatalf("expected normalized OpenAI text capability, got value=%#v err=%v", got, err)
	}
	// A missing text capability is no longer an error on its own: an image-only
	// Terminal Target legitimately has none. The joint requirement that at
	// least one dimension be present lives in
	// ensureOpenAIConnectionDimensionsPresent.
	if got, err := resolveOpenAITextCapabilityCreate("openai", nil); err != nil || got != nil {
		t.Fatalf("expected an absent OpenAI text capability to be allowed, got value=%#v err=%v", got, err)
	}
	if _, err := resolveOpenAITextCapabilityCreate("openai", connectionStringRef("bogus")); err == nil {
		t.Fatal("expected invalid OpenAI text capability to fail")
	} else {
		requireConnectionDomainError(t, err, http.StatusUnprocessableEntity, "openai_text_capability is invalid")
	}
	if _, err := resolveOpenAITextCapabilityCreate("anthropic", connectionStringRef("responses_only")); err == nil {
		t.Fatal("expected non-OpenAI text capability to fail")
	} else {
		requireConnectionDomainError(t, err, http.StatusUnprocessableEntity, "openai_text_capability is only supported for OpenAI-family connections")
	}
	if got, err := resolveOpenAITextCapabilityUpdate("openai", "openai", connectionStringRef("responses_only"), optionalString{}); err != nil || got == nil || *got != "responses_only" {
		t.Fatalf("expected update to preserve existing OpenAI text capability, got value=%#v err=%v", got, err)
	}
	if got, err := resolveOpenAITextCapabilityUpdate("anthropic", "openai", nil, optionalString{}); err != nil || got != nil {
		t.Fatalf("expected changing to OpenAI without a text capability to be allowed, got value=%#v err=%v", got, err)
	}
}

func TestResolveOpenAIImageCapability(t *testing.T) {
	if got, err := resolveOpenAIImageCapabilityCreate("openai", connectionStringRef(" generations_and_edits ")); err != nil || got == nil || *got != "generations_and_edits" {
		t.Fatalf("expected normalized OpenAI image capability, got value=%#v err=%v", got, err)
	}
	if got, err := resolveOpenAIImageCapabilityCreate("openai", nil); err != nil || got != nil {
		t.Fatalf("expected an absent OpenAI image capability to be allowed, got value=%#v err=%v", got, err)
	}
	if _, err := resolveOpenAIImageCapabilityCreate("openai", connectionStringRef("bogus")); err == nil {
		t.Fatal("expected an invalid OpenAI image capability to fail")
	} else {
		requireConnectionDomainError(t, err, http.StatusUnprocessableEntity, "openai_image_capability is invalid")
	}
	if _, err := resolveOpenAIImageCapabilityCreate("anthropic", connectionStringRef("generations")); err == nil {
		t.Fatal("expected a non-OpenAI image capability to fail")
	} else {
		requireConnectionDomainError(t, err, http.StatusUnprocessableEntity, "openai_image_capability is only supported for OpenAI-family connections")
	}
	if got, err := resolveOpenAIImageCapabilityUpdate("openai", "openai", connectionStringRef("edits"), optionalString{}); err != nil || got == nil || *got != "edits" {
		t.Fatalf("expected update to preserve the existing OpenAI image capability, got value=%#v err=%v", got, err)
	}
}

// An OpenAI Terminal Target must declare at least one dimension; either one on
// its own is enough, and non-OpenAI families are exempt.
func TestEnsureOpenAIConnectionDimensionsPresent(t *testing.T) {
	text := connectionStringRef("dual_native")
	image := connectionStringRef("generations")

	if err := ensureOpenAIConnectionDimensionsPresent("openai", nil, nil); err == nil {
		t.Fatal("expected an OpenAI connection with neither dimension to fail")
	} else {
		requireConnectionDomainError(t, err, http.StatusUnprocessableEntity, "at least one of openai_text_capability or openai_image_capability is required for OpenAI-family connections")
	}
	if err := ensureOpenAIConnectionDimensionsPresent("openai", text, nil); err != nil {
		t.Fatalf("expected a text-only OpenAI connection to be allowed, got %v", err)
	}
	if err := ensureOpenAIConnectionDimensionsPresent("openai", nil, image); err != nil {
		t.Fatalf("expected an image-only OpenAI connection to be allowed, got %v", err)
	}
	if err := ensureOpenAIConnectionDimensionsPresent("anthropic", nil, nil); err != nil {
		t.Fatalf("expected non-OpenAI families to be exempt, got %v", err)
	}
}

// Image coverage is containment: a wider target covers a narrower owner, a
// narrower target does not cover a wider owner, and an owner without images
// imposes nothing.
func TestEnsureOpenAIImageCapabilityCoversOwnerOperations(t *testing.T) {
	generations := connectionStringRef("generations")
	both := connectionStringRef("generations_and_edits")

	if err := ensureOpenAIImageCapabilityCoversOwnerOperations("openai", generations, both); err != nil {
		t.Fatalf("expected a wider target to cover a narrower owner, got %v", err)
	}
	if err := ensureOpenAIImageCapabilityCoversOwnerOperations("openai", both, generations); err == nil {
		t.Fatal("expected a narrower target to leave an image operation uncovered")
	} else {
		requireConnectionDomainError(t, err, http.StatusUnprocessableEntity, "openai_image_capability must serve every image operation the owner model accepts")
	}
	if err := ensureOpenAIImageCapabilityCoversOwnerOperations("openai", nil, nil); err != nil {
		t.Fatalf("expected an owner without images to impose no requirement, got %v", err)
	}
	if err := ensureOpenAIImageCapabilityCoversOwnerOperations("openai", generations, nil); err == nil {
		t.Fatal("expected a target without an image capability to leave images uncovered")
	}
}

func TestRouteInt(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/connections/42", nil)
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("connection_id", "42")
	request = request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, routeContext))

	if got, err := routeInt(request, "connection_id"); err != nil || got != 42 {
		t.Fatalf("expected route param 42, got value=%d err=%v", got, err)
	}

	routeContext.URLParams.Values[0] = "bad"
	if _, err := routeInt(request, "connection_id"); err == nil {
		t.Fatal("expected invalid route param to fail")
	}
}

func TestTerminalTargetRecordAdapterPreservesConnectionResponseShape(t *testing.T) {
	modelConfigID := 7
	name := "primary"
	authType := "openai"
	textCapability := "chat_completions_only"
	pricingTemplateID := 11
	qpsLimit := 12
	maxNonStream := 3
	maxStream := 4
	now := time.Date(2026, time.June, 7, 12, 0, 0, 0, time.UTC)

	connection := connectionResponse{
		ID: 42, ProfileID: 5, ModelConfigID: &modelConfigID, APIFamily: "openai", EndpointID: 9,
		IsActive: true, Priority: 2, Name: &name, AuthType: &authType,
		CustomHeaders: map[string]string{"X-Test": "1"},
		// A non-zero routing schedule is required here: with the field left at
		// its zero value the round-trip passes even when both mappers drop it.
		RoutingSchedule: &RoutingSchedulePayload{
			Timezone: "Asia/Shanghai",
			Windows: []RoutingWindowPayload{
				{WeekdayMask: 31, StartMinute: 540, EndMinute: 1080},
				{WeekdayMask: 96, StartMinute: 1320, EndMinute: 1800},
			},
		},
		OpenAITextCapability: &textCapability, PricingTemplateID: &pricingTemplateID,
		QPSLimit: &qpsLimit, MaxInFlightNonStream: &maxNonStream, MaxInFlightStream: &maxStream,
		PricingTemplate: &connectionPricingTemplateSummary{ID: 11, Name: "standard", PricingUnit: "tokens", PricingCurrencyCode: "USD", Version: 1},
		CreatedAt:       now, UpdatedAt: now,
	}
	converted := connectionResponseFromTerminalTargetRecord(terminalTargetRecordFromConnectionResponse(connection))
	if !reflect.DeepEqual(converted, connection) {
		t.Fatalf("expected terminal-target adapter to preserve connection response\nwant: %#v\ngot:  %#v", connection, converted)
	}
}
