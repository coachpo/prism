package stats

import (
	"net/http/httptest"
	"testing"

	statsdomain "github.com/coachpo/prism/backend/internal/domain/stats"
)

func TestAggregateEndpointCapabilitiesRejectAcceptedButIgnoredKeys(t *testing.T) {
	for _, item := range []struct {
		endpoint string
		scope    string
		keys     []string
	}{
		{"summary", statsdomain.ScopeIngress, []string{"scope", "proxy_api_key_id"}},
		{"throughput", statsdomain.ScopeIngress, []string{"scope", "group_by"}},
		{"spending", statsdomain.ScopeIngress, []string{"scope", "metric"}},
	} {
		if err := validateAggregateEndpointQueryKeys(item.endpoint, item.scope, item.keys); err == nil {
			t.Fatalf("%s/%s accepted unsupported keys %v", item.endpoint, item.scope, item.keys)
		}
	}
}

func TestUsageErrorsCapabilitiesAreScopeSpecific(t *testing.T) {
	attempt := httptest.NewRequest("GET", "/api/stats/usage-errors?query_context=x&attempt_result=stream_error", nil)
	if err := rejectUsageErrorsQueryKeys(attempt, statsdomain.ScopeRouteAttempt); err != nil {
		t.Fatalf("route-attempt selector rejected: %v", err)
	}
	if err := rejectUsageErrorsQueryKeys(attempt, statsdomain.ScopeIngress); err == nil {
		t.Fatal("ingress accepted route-attempt selector")
	}
	final := httptest.NewRequest("GET", "/api/stats/usage-errors?query_context=x&final_result=failed", nil)
	if err := rejectUsageErrorsQueryKeys(final, statsdomain.ScopeFinal); err != nil {
		t.Fatalf("final selector rejected: %v", err)
	}
	if err := rejectUsageErrorsQueryKeys(final, statsdomain.ScopeRouteAttempt); err == nil {
		t.Fatal("route-attempt accepted finalized selector")
	}
}
