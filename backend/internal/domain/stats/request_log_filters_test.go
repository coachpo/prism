package stats

import (
	"database/sql"
	"reflect"
	"testing"
)

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
