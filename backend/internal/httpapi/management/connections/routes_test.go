package connections

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
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

	var domainErr *domainError
	if !errors.As(err, &domainErr) {
		t.Fatalf("expected domainError, got %T", err)
	}
	if domainErr.StatusCode != status || domainErr.Detail != detail {
		t.Fatalf("expected domainError (%d, %q), got (%d, %q)", status, detail, domainErr.StatusCode, domainErr.Detail)
	}
}

func TestNormalizeConnectionPriorities(t *testing.T) {
	now := time.Date(2026, time.April, 19, 12, 0, 0, 0, time.UTC)
	items := []connectionResponse{{Priority: 5}, {Priority: 1}}
	if changed := normalizeConnectionPriorities(items, now); !changed {
		t.Fatal("expected mismatched priorities to be normalized")
	}
	if items[0].Priority != 0 || items[1].Priority != 1 {
		t.Fatalf("expected normalized priorities [0 1], got [%d %d]", items[0].Priority, items[1].Priority)
	}
	if !items[0].UpdatedAt.Equal(now) {
		t.Fatalf("expected normalized item updated_at to be set, got %v", items[0].UpdatedAt)
	}

	stable := []connectionResponse{{Priority: 0}, {Priority: 1}}
	if changed := normalizeConnectionPriorities(stable, now); changed {
		t.Fatal("expected already-normalized priorities not to report a change")
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
		requireConnectionDomainError(t, err, http.StatusUnprocessableEntity, "auth_type must be one of 'openai', 'anthropic', or 'gemini'")
	}
}

func TestResolveOpenAIProbeEndpointVariantAndHelpers(t *testing.T) {
	if got, err := resolveOpenAIProbeEndpointVariant("openai", nil); err != nil || got == nil || *got != defaultOpenAIProbeEndpointVariant {
		t.Fatalf("expected default OpenAI probe variant, got value=%#v err=%v", got, err)
	}
	if got, err := resolveOpenAIProbeEndpointVariant("openai", connectionStringRef("chat_completions_minimal")); err != nil || got == nil || *got != "chat_completions_minimal" {
		t.Fatalf("expected explicit OpenAI probe variant, got value=%#v err=%v", got, err)
	}
	if _, err := resolveOpenAIProbeEndpointVariant("openai", connectionStringRef("bogus")); err == nil {
		t.Fatal("expected invalid OpenAI probe variant to fail")
	} else {
		requireConnectionDomainError(t, err, http.StatusUnprocessableEntity, "openai_probe_endpoint_variant is invalid")
	}
	if _, err := resolveOpenAIProbeEndpointVariant("gemini", connectionStringRef("responses_minimal")); err == nil {
		t.Fatal("expected non-OpenAI probe variant to fail")
	} else {
		requireConnectionDomainError(t, err, http.StatusUnprocessableEntity, "openai_probe_endpoint_variant is only supported for OpenAI-family connections")
	}

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
