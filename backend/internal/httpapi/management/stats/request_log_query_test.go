package stats

import (
	"errors"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"
	"time"

	statsdomain "github.com/coachpo/prism/backend/internal/domain/stats"
)

func signedRequestLogQueryContext(t *testing.T, profileID int, key []byte, now time.Time, from time.Time, to time.Time) string {
	t.Helper()
	token := statsdomain.QueryContextToken{
		SchemaVersion:   1,
		Scope:           statsdomain.ScopeRouteAttempt,
		ProfileID:       profileID,
		RequestedPreset: "24h",
		UsageFrom:       from.Add(-30 * 24 * time.Hour).Format(time.RFC3339Nano),
		UsageTo:         to.Add(-30 * 24 * time.Hour).Format(time.RFC3339Nano),
		Domains: map[string]statsdomain.QueryContextDomainSnapshot{
			"request_logs": {
				Domain:   "request_logs",
				FromTime: from,
				ToTime:   to,
			},
		},
		IssuedAt:  now.Add(-time.Minute),
		ExpiresAt: now.Add(time.Hour),
	}
	signed, err := statsdomain.SignQueryContext(token, key)
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

func TestParseRequestLogListParamsSupportsRepeatedCommaAndNullSelectors(t *testing.T) {
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	requestFrom := now.Add(-6 * time.Hour)
	requestTo := now
	key := []byte("request-log-query-test-key")
	queryContext := signedRequestLogQueryContext(t, 1, key, now, requestFrom, requestTo)

	query := url.Values{}
	query.Set("view", "attempts")
	query.Set("query_context", queryContext)
	query.Set("from_time", "2020-01-01T00:00:00Z")
	query.Set("to_time", "2020-01-02T00:00:00Z")
	query.Add("final_result", "failed,client_disconnected")
	query.Add("final_result", "completed")
	query.Set("outcome_detail", "http_error,stream_error")
	query.Add("final_status_code", "500,502")
	query.Add("final_status_code", "503")
	query.Set("final_stream_outcome", "provider_incomplete,__null__")
	query.Set("final_stream_error_kind", "upstream_reset,__null__")
	query.Set("final_target_model_id", "model-a,model-b,__null__")
	query.Set("final_endpoint_id", "11,12,__null__")
	query.Set("final_terminal_target_id", "21,22,__null__")
	query.Set("attempt_target_model_id", "attempt-a,attempt-b,__null__")
	query.Set("api_family", "openai.chat,anthropic.messages,__null__")
	query.Set("row_kind", "upstream")
	query.Set("endpoint_id", "31,32,__null__")
	query.Set("terminal_target_id", "41,42,__null__")
	query.Set("status_code", "429,503,__null__")
	query.Set("attempt_trigger", "initial,failover,__null__")
	query.Set("attempt_result", "http_error,transport_error,__null__")

	request := httptest.NewRequest("GET", "/api/stats/requests?"+query.Encode(), nil)
	params, err := parseRequestLogListParams(request, 1, key, now)
	if err != nil {
		t.Fatal(err)
	}

	if params.FromTime == nil || !params.FromTime.Equal(requestFrom) || params.ToTime == nil || !params.ToTime.Equal(requestTo) {
		t.Fatalf("signed request bounds were not authoritative: from=%v to=%v", params.FromTime, params.ToTime)
	}
	if !reflect.DeepEqual(params.FinalResults, []string{"failed", "client_disconnected", "completed"}) {
		t.Fatalf("final results = %#v", params.FinalResults)
	}
	if !reflect.DeepEqual(params.FinalOutcomeDetails, []string{"http_error", "stream_error"}) {
		t.Fatalf("outcome details = %#v", params.FinalOutcomeDetails)
	}
	if !reflect.DeepEqual(params.FinalStatusCodes, []int{500, 502, 503}) {
		t.Fatalf("final status codes = %#v", params.FinalStatusCodes)
	}
	if !params.FinalStreamOutcomeIsNull || !reflect.DeepEqual(params.FinalStreamOutcomes, []string{"provider_incomplete"}) {
		t.Fatalf("final stream outcomes = %#v null=%t", params.FinalStreamOutcomes, params.FinalStreamOutcomeIsNull)
	}
	if !params.FinalStreamErrorKindIsNull || !reflect.DeepEqual(params.FinalStreamErrorKinds, []string{"upstream_reset"}) {
		t.Fatalf("final stream error kinds = %#v null=%t", params.FinalStreamErrorKinds, params.FinalStreamErrorKindIsNull)
	}
	if !params.FinalModelIDIsNull || !reflect.DeepEqual(params.FinalModelIDs, []string{"model-a", "model-b"}) {
		t.Fatalf("final models = %#v null=%t", params.FinalModelIDs, params.FinalModelIDIsNull)
	}
	if !params.FinalEndpointIDIsNull || !reflect.DeepEqual(params.FinalEndpointIDs, []int{11, 12}) {
		t.Fatalf("final endpoints = %#v null=%t", params.FinalEndpointIDs, params.FinalEndpointIDIsNull)
	}
	if !params.ResolvedTargetModelIDIsNull || !reflect.DeepEqual(params.ResolvedTargetModelIDs, []string{"attempt-a", "attempt-b"}) {
		t.Fatalf("attempt models = %#v null=%t", params.ResolvedTargetModelIDs, params.ResolvedTargetModelIDIsNull)
	}
	if !reflect.DeepEqual(params.RowKinds, []string{"upstream"}) {
		t.Fatalf("row kinds = %#v", params.RowKinds)
	}
	if !params.EndpointIDIsNull || !reflect.DeepEqual(params.EndpointIDs, []int{31, 32}) {
		t.Fatalf("ordinary endpoints = %#v null=%t", params.EndpointIDs, params.EndpointIDIsNull)
	}
	if !params.StatusCodeIsNull || !reflect.DeepEqual(params.StatusCodes, []int{429, 503}) {
		t.Fatalf("ordinary statuses = %#v null=%t", params.StatusCodes, params.StatusCodeIsNull)
	}
	if !params.AttemptTriggerIsNull || !reflect.DeepEqual(params.AttemptTriggers, []string{"initial", "failover"}) {
		t.Fatalf("attempt triggers = %#v null=%t", params.AttemptTriggers, params.AttemptTriggerIsNull)
	}
	if !params.AttemptResultIsNull || !reflect.DeepEqual(params.AttemptResults, []string{"http_error", "transport_error"}) {
		t.Fatalf("attempt results = %#v null=%t", params.AttemptResults, params.AttemptResultIsNull)
	}
}

func TestParseRequestLogListParamsRequiresContextForAttemptSelectors(t *testing.T) {
	request := httptest.NewRequest("GET", "/api/stats/requests?view=attempts&attempt_trigger=failover", nil)
	_, err := parseRequestLogListParams(request, 1, []byte("key"), time.Now().UTC())
	var httpErr *statsdomain.HTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != 422 || httpErr.Code != "query_context_required" {
		t.Fatalf("error = %#v", err)
	}
}

func TestParseRequestLogListParamsBindsContextWithoutSelectors(t *testing.T) {
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	from := now.Add(-time.Hour)
	key := []byte("key")
	context := signedRequestLogQueryContext(t, 1, key, now, from, now)
	query := url.Values{
		"query_context": {context},
		"from_time":     {"2020-01-01T00:00:00Z"},
		"to_time":       {"2020-01-02T00:00:00Z"},
	}
	request := httptest.NewRequest("GET", "/api/stats/requests?"+query.Encode(), nil)
	params, err := parseRequestLogListParams(request, 1, key, now)
	if err != nil {
		t.Fatal(err)
	}
	if params.FromTime == nil || !params.FromTime.Equal(from) || params.ToTime == nil || !params.ToTime.Equal(now) {
		t.Fatalf("signed request bounds were not authoritative: from=%v to=%v", params.FromTime, params.ToTime)
	}
}

func TestParseRequestLogListParamsRejectsInvalidListMember(t *testing.T) {
	now := time.Now().UTC()
	key := []byte("key")
	context := signedRequestLogQueryContext(t, 1, key, now, now.Add(-time.Hour), now)
	query := url.Values{"query_context": {context}, "attempt_result": {"http_error,not-a-result"}}
	request := httptest.NewRequest("GET", "/api/stats/requests?"+query.Encode(), nil)
	_, err := parseRequestLogListParams(request, 1, key, now)
	var httpErr *statsdomain.HTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != 422 {
		t.Fatalf("error = %#v", err)
	}
}

func TestSignedCohortDetectionIncludesOutcomeAndAttemptSelectors(t *testing.T) {
	for _, rawQuery := range []string{
		"outcome_detail=http_error",
		"attempt_trigger=initial",
		"attempt_result=http_error,transport_error",
		"final_stream_error_kind=__null__",
	} {
		request := httptest.NewRequest("GET", "/api/stats/requests?"+rawQuery, nil)
		if !requestLogHasSignedCohortSelector(request) {
			t.Fatalf("selector not detected: %s", rawQuery)
		}
	}
}

func TestRequestLogExportTreatsQueryContextAsBoundedRange(t *testing.T) {
	withContext := httptest.NewRequest("GET", "/api/stats/requests/export?view=attempts&query_context=signed", nil)
	if !requestLogExportHasBoundedRange(withContext) {
		t.Fatal("signed query context must satisfy export range preflight")
	}
	withoutRange := httptest.NewRequest("GET", "/api/stats/requests/export?view=attempts", nil)
	if requestLogExportHasBoundedRange(withoutRange) {
		t.Fatal("range-free export unexpectedly accepted")
	}
}
