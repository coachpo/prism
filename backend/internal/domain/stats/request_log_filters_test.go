package stats

import (
	"database/sql"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestBuildRequestLogBrowseWhereKeepsPureAttemptSelectorsOnRequestLogs(t *testing.T) {
	from := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)
	where, args := buildRequestLogBrowseWhere(RequestLogListParams{
		ProfileID:            1,
		QueryContextFrom:     &from,
		QueryContextTo:       &to,
		AttemptTriggers:      []string{"initial", "failover"},
		AttemptTriggerIsNull: true,
		AttemptResults:       []string{"http_error", "transport_error"},
		AttemptResultIsNull:  true,
	})

	if strings.Contains(where, "usage_request_events") {
		t.Fatalf("pure attempt selectors must not require usage rows: %s", where)
	}
	for _, fragment := range []string{
		"attempt_trigger IN ($2,$3)",
		"attempt_trigger IS NULL",
		"attempt_result IN ($4,$5)",
		"attempt_result IS NULL",
		") AND (",
	} {
		if !strings.Contains(where, fragment) {
			t.Fatalf("where clause missing %q: %s", fragment, where)
		}
	}
	wantArgs := []any{1, "initial", "failover", "http_error", "transport_error"}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("args = %#v, want %#v", args, wantArgs)
	}
}

func TestBuildRequestLogBrowseWhereSupportsOrdinaryListsAndNull(t *testing.T) {
	where, args := buildRequestLogBrowseWhere(RequestLogListParams{
		ProfileID:                   7,
		ResolvedTargetModelIDs:      []string{"model-a", "model-b"},
		ResolvedTargetModelIDIsNull: true,
		APIFamilies:                 []string{"openai.chat", "anthropic.messages"},
		StatusCodes:                 []int{429, 503},
		StatusCodeIsNull:            true,
		EndpointIDs:                 []int{11, 12},
		EndpointIDIsNull:            true,
		TerminalTargetIDs:           []int{21, 22},
		TerminalTargetIDIsNull:      true,
	})

	for _, fragment := range []string{
		"resolved_target_model_id IN ($2,$3) OR resolved_target_model_id IS NULL",
		"NULLIF(api_family, '') IN ($4,$5)",
		"IN ($6,$7)",
		"CASE WHEN endpoint_id > 0 THEN endpoint_id END) IN ($8,$9)",
		"CASE WHEN endpoint_id > 0 THEN endpoint_id END) IS NULL",
		"CASE WHEN connection_id > 0 THEN connection_id END) IN ($10,$11)",
		"CASE WHEN connection_id > 0 THEN connection_id END) IS NULL",
	} {
		if !strings.Contains(where, fragment) {
			t.Fatalf("where clause missing %q: %s", fragment, where)
		}
	}
	wantArgs := []any{7, "model-a", "model-b", "openai.chat", "anthropic.messages", 429, 503, 11, 12, 21, 22}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("args = %#v, want %#v", args, wantArgs)
	}
}

func TestBuildRequestLogBrowseWhereSupportsFinalListsExactDetailAndNull(t *testing.T) {
	from := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)
	where, args := buildRequestLogBrowseWhere(RequestLogListParams{
		ProfileID:                   1,
		QueryContextFrom:            &from,
		QueryContextTo:              &to,
		FinalResults:                []string{"failed", "client_disconnected"},
		FinalOutcomeDetails:         []string{"http_error", "stream_error"},
		FinalStatusCodes:            []int{500, 502},
		FinalModelIDs:               []string{"model-a", "model-b"},
		FinalModelIDIsNull:          true,
		FinalEndpointIDs:            []int{11, 12},
		FinalEndpointIDIsNull:       true,
		FinalTerminalTargetIDs:      []int{21, 22},
		FinalTerminalTargetIDIsNull: true,
		FinalStreamErrorKinds:       []string{"upstream_reset"},
		FinalStreamErrorKindIsNull:  true,
		FinalStreamOutcomes:         []string{"provider_incomplete"},
		FinalStreamOutcomeIsNull:    true,
	})

	for _, fragment := range []string{
		"EXISTS (SELECT 1 FROM usage_request_events ue",
		"ue.resolved_target_model_id IN",
		"ue.resolved_target_model_id IS NULL",
		"CASE WHEN ue.endpoint_id > 0 THEN ue.endpoint_id END) IN",
		"CASE WHEN ue.endpoint_id > 0 THEN ue.endpoint_id END) IS NULL",
		"CASE WHEN ue.connection_id > 0 THEN ue.connection_id END) IN",
		"CASE WHEN ue.connection_id > 0 THEN ue.connection_id END) IS NULL",
		"'http_error'",
		"'stream_error'",
		"NULLIF(ue.stream_error_kind, '') IS NULL",
		"ue.stream_outcome IS NULL",
	} {
		if !strings.Contains(where, fragment) {
			t.Fatalf("where clause missing %q: %s", fragment, where)
		}
	}
	if len(args) != 17 {
		t.Fatalf("args len = %d, want 17: %#v", len(args), args)
	}
}

func TestAttemptErrorRequestFiltersPreserveORAndANDSemantics(t *testing.T) {
	model := "attempt-model"
	endpointID := 9
	params := UsageErrorsParams{AttemptTargetModelID: &model, EndpointID: &endpointID}

	base := attemptBaseRequestFilters(params)
	if !reflect.DeepEqual(base, map[string][]string{
		"attempt_target_model_id": {"attempt-model"},
		"endpoint_id":             {"9"},
		"row_kind":                {"upstream"},
	}) {
		t.Fatalf("base filters = %#v", base)
	}

	status := attemptHTTPStatusFilters(params, 503)
	if !reflect.DeepEqual(status["attempt_result"], failedAttemptRequestFilterValues) {
		t.Fatalf("attempt result OR values = %#v", status["attempt_result"])
	}
	if !reflect.DeepEqual(status["status_code"], []string{"503"}) {
		t.Fatalf("status filter = %#v", status["status_code"])
	}

	group := attemptGroupFilters(params, ErrorsGroup{EntityType: GroupAttemptTrigger, Label: "unknown"})
	if !reflect.DeepEqual(group["attempt_trigger"], []string{"__null__"}) {
		t.Fatalf("unattributed group filter = %#v", group["attempt_trigger"])
	}
	if !reflect.DeepEqual(group["attempt_result"], failedAttemptRequestFilterValues) {
		t.Fatalf("group failure filter = %#v", group["attempt_result"])
	}
}

func TestAttemptErrorGroupIdentitySeparatesLiteralUnknownFromNull(t *testing.T) {
	_, literalLabel, literalID := attemptErrorGroupIdentity(
		GroupAttemptResult,
		sql.NullString{}, sql.NullInt32{}, sql.NullInt32{}, sql.NullString{}, sql.NullString{},
		sql.NullString{String: "unknown", Valid: true},
	)
	_, nullLabel, nullID := attemptErrorGroupIdentity(
		GroupAttemptResult,
		sql.NullString{}, sql.NullInt32{}, sql.NullInt32{}, sql.NullString{}, sql.NullString{}, sql.NullString{},
	)
	if literalID == nil || *literalID != "unknown" || literalLabel != "unknown" {
		t.Fatalf("literal unknown identity = label %q id %#v", literalLabel, literalID)
	}
	if nullID != nil || nullLabel != "unknown" {
		t.Fatalf("null identity = label %q id %#v", nullLabel, nullID)
	}
	literalFilters := attemptGroupFilters(UsageErrorsParams{}, ErrorsGroup{EntityType: GroupAttemptResult, EntityID: literalID, Label: literalLabel})
	nullFilters := attemptGroupFilters(UsageErrorsParams{}, ErrorsGroup{EntityType: GroupAttemptResult, EntityID: nullID, Label: nullLabel})
	if !reflect.DeepEqual(literalFilters["attempt_result"], []string{"unknown"}) {
		t.Fatalf("literal filters = %#v", literalFilters)
	}
	if !reflect.DeepEqual(nullFilters["attempt_result"], []string{"__null__"}) {
		t.Fatalf("null filters = %#v", nullFilters)
	}
}

func TestFinalizedCohortExclusionBuildsParameterizedComplement(t *testing.T) {
	where, args := buildRequestLogBrowseWhere(RequestLogListParams{
		ProfileID: 1,
		FinalExclusion: &FinalizedCohortExclusion{
			Facet:       FinalExclusionStreamErrorKind,
			Values:      []string{"kind-a", "kind-b"},
			ExcludeNull: true,
		},
	})
	for _, fragment := range []string{
		"EXISTS (SELECT 1 FROM usage_request_events ue",
		"NULLIF(ue.stream_error_kind, '') IS NOT NULL",
		"NULLIF(ue.stream_error_kind, '') NOT IN ($2,$3)",
	} {
		if !strings.Contains(where, fragment) {
			t.Fatalf("where clause missing %q: %s", fragment, where)
		}
	}
	if !reflect.DeepEqual(args, []any{1, "kind-a", "kind-b"}) {
		t.Fatalf("args = %#v", args)
	}

	where, args = buildRequestLogBrowseWhere(RequestLogListParams{
		ProfileID: 1,
		FinalExclusion: &FinalizedCohortExclusion{
			Facet:  FinalExclusionIngressModel,
			Values: []string{"visible-model"},
		},
	})
	if !strings.Contains(where, "NULLIF(ue.model_id, '') IS NULL OR NULLIF(ue.model_id, '') NOT IN ($2)") {
		t.Fatalf("null-inclusive complement missing: %s", where)
	}
	if !reflect.DeepEqual(args, []any{1, "visible-model"}) {
		t.Fatalf("args = %#v", args)
	}
}

func TestFinalizedErrorRemaindersCarryExactReplaySelectors(t *testing.T) {
	httpFilters := httpStatusRemainderFilters(UsageErrorsParams{}, []ErrorsHTTPStatus{{StatusCode: 500}, {StatusCode: 503}})
	if !reflect.DeepEqual(httpFilters, map[string][]string{
		"final_result":   {"failed"},
		"outcome_detail": {"http_error"},
		"final_exclude":  {FinalExclusionStatusCode, "500", "503"},
	}) {
		t.Fatalf("http remainder filters = %#v", httpFilters)
	}

	kind := "protocol_error"
	kindFilters := streamKindRemainderFilters(UsageErrorsParams{}, "provider_incomplete", []ErrorsStreamKind{
		{StreamErrorKind: &kind},
		{StreamErrorKind: nil},
	})
	if !reflect.DeepEqual(kindFilters["final_stream_outcome"], []string{"provider_incomplete"}) ||
		!reflect.DeepEqual(kindFilters["final_exclude"], []string{FinalExclusionStreamErrorKind, "protocol_error", "__null__"}) {
		t.Fatalf("kind remainder filters = %#v", kindFilters)
	}

	apiFamily := "openai"
	groupFilters := groupRemainderFilters(UsageErrorsParams{GroupBy: GroupAPIFamily}, []ErrorsGroup{
		{EntityID: &apiFamily},
		{EntityID: nil},
	})
	if !reflect.DeepEqual(groupFilters["final_result"], []string{"failed", "client_disconnected"}) ||
		!reflect.DeepEqual(groupFilters["final_exclude"], []string{FinalExclusionAPIFamily, "openai", "__null__"}) {
		t.Fatalf("group remainder filters = %#v", groupFilters)
	}
}
