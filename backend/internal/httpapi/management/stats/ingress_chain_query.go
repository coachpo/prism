package stats

import (
	"fmt"
	"net/http"
	"strings"

	statsdomain "github.com/coachpo/prism/backend/internal/domain/stats"
)

// parseChainQueryParams parses the canonical ingress-chain query (Requests
// SPEC §6.1). Chain view only accepts created_at sort; row-scoped filters
// select the ingress cohort server-side before pagination.
func parseChainQueryParams(r *http.Request, profileID int) (statsdomain.ChainQueryParams, error) {
	allowed := map[string]struct{}{
		"view": {}, "q": {}, "ingress_request_id": {}, "ingress_final_result": {}, "final_result": {}, "row_result": {}, "confirmed_failover": {},
		"pricing_status": {}, "unpriced_reason": {}, "pricing_card_role": {}, "pricing_selection_state": {}, "reporting_currency_epoch": {}, "cost_segment_key": {},
		"is_stream": {}, "stream_outcome": {}, "stream_error_kind": {}, "final_stream_outcome": {}, "final_stream_error_kind": {}, "upstream_status_code": {}, "gateway_status_code": {}, "legacy_status_code": {}, "ingress_final_status_code": {}, "final_status_code": {},
		"ingress_model_id": {}, "attempt_target_model_id": {}, "final_target_model_id": {}, "endpoint_id": {}, "terminal_target_id": {}, "status_family": {}, "status_code": {}, "client_rule_id": {}, "error_text": {}, "proxy_api_key_id": {},
		"from_time": {}, "to_time": {}, "time_range": {}, "sort_by": {}, "sort_order": {}, "chain_limit": {}, "chain_cursor": {}, "chain_row_limit": {}, "row_cursor": {}, "anchor_request_log_id": {},
	}
	for key := range r.URL.Query() {
		if _, ok := allowed[key]; !ok {
			return statsdomain.ChainQueryParams{}, &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Code: "unknown_query_key", Detail: "Unknown query key: " + key}
		}
	}
	params := statsdomain.ChainQueryParams{
		ProfileID:              profileID,
		View:                   "ingress_chains",
		IngressRequestID:       normalizedQueryString(r, "ingress_request_id"),
		IngressFinalResult:     normalizedQueryString(r, "ingress_final_result"),
		RowResult:              normalizedQueryString(r, "row_result"),
		PricingStatus:          normalizedQueryString(r, "pricing_status"),
		ReportingCurrencyEpoch: normalizedQueryString(r, "reporting_currency_epoch"),
		CostSegmentKey:         normalizedQueryString(r, "cost_segment_key"),
		PricingCardRole:        normalizedQueryString(r, "pricing_card_role"),
		PricingSelectionState:  normalizedQueryString(r, "pricing_selection_state"),
		ModelID:                normalizedQueryString(r, "ingress_model_id"),
		ResolvedTargetModelID:  normalizedQueryString(r, "attempt_target_model_id"),
		FinalTargetModelID:     normalizedQueryString(r, "final_target_model_id"),
		StatusFamily:           normalizedQueryString(r, "status_family"),
		ErrorText:              normalizedQueryString(r, "error_text"),
	}
	if value := normalizedQueryString(r, "final_result"); value != nil {
		params.IngressFinalResult = value
	}
	var err error
	params.CostSegmentKey, err = statsdomain.NormalizeCostSegmentKey(r.URL.Query().Get("cost_segment_key"))
	if err != nil {
		return statsdomain.ChainQueryParams{}, err
	}
	for key, value := range map[string]*string{"pricing_card_role": params.PricingCardRole, "pricing_selection_state": params.PricingSelectionState} {
		if value == nil {
			continue
		}
		valid := false
		if key == "pricing_card_role" {
			for _, candidate := range []string{"standard", "tier_base", "tier_above", "peak", "offpeak"} {
				valid = valid || *value == candidate
			}
		} else {
			for _, candidate := range []string{"not_evaluated", "not_applicable", "selected", "unresolved"} {
				valid = valid || *value == candidate
			}
		}
		if !valid {
			return statsdomain.ChainQueryParams{}, &statsdomain.HTTPError{StatusCode: http.StatusBadRequest, Code: key + "_invalid", Detail: key + " is invalid"}
		}
	}
	params.FromTime, err = parseOptionalTime(r, "from_time")
	if err != nil {
		return statsdomain.ChainQueryParams{}, err
	}
	params.CoveragePreset = strings.TrimSpace(r.URL.Query().Get("time_range"))
	params.ToTime, err = parseOptionalTime(r, "to_time")
	if err != nil {
		return statsdomain.ChainQueryParams{}, err
	}
	params.EndpointID, err = parseOptionalInt(r, "endpoint_id")
	if err != nil {
		return statsdomain.ChainQueryParams{}, err
	}
	params.TerminalTargetID, err = parseOptionalInt(r, "terminal_target_id")
	if err != nil {
		return statsdomain.ChainQueryParams{}, err
	}
	if params.TerminalTargetID != nil && *params.TerminalTargetID <= 0 {
		return statsdomain.ChainQueryParams{}, &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Code: "invalid_terminal_target_id", Detail: "invalid terminal_target_id"}
	}
	params.StatusCode, err = parseOptionalInt(r, "status_code")
	if err != nil {
		return statsdomain.ChainQueryParams{}, err
	}
	var proxyAPIKeyID *int
	if rawValues, present := r.URL.Query()["proxy_api_key_id"]; present {
		if len(rawValues) != 1 || strings.TrimSpace(rawValues[0]) == "" {
			return statsdomain.ChainQueryParams{}, &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Code: "invalid_proxy_api_key_id", Detail: "invalid proxy_api_key_id"}
		}
		proxyAPIKeyID, err = parseOptionalInt(r, "proxy_api_key_id")
	}
	if err != nil || (proxyAPIKeyID != nil && *proxyAPIKeyID <= 0) {
		return statsdomain.ChainQueryParams{}, &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Code: "invalid_proxy_api_key_id", Detail: "invalid proxy_api_key_id"}
	}
	params.ProxyAPIKeyID = proxyAPIKeyID
	params.ConfirmedFailover, err = parseOptionalBool(r, "confirmed_failover")
	if err != nil {
		return statsdomain.ChainQueryParams{}, err
	}
	params.IsStream, err = parseOptionalBool(r, "is_stream")
	if err != nil {
		return statsdomain.ChainQueryParams{}, err
	}
	params.UnpricedReasons = repeatableQueryValues(r, "unpriced_reason")
	params.StreamOutcomes = repeatableQueryValues(r, "stream_outcome")
	params.StreamErrorKinds = repeatableQueryValues(r, "stream_error_kind")
	if values := repeatableQueryValues(r, "final_stream_outcome"); len(values) > 0 {
		params.StreamOutcomes = values
	}
	if values := repeatableQueryValues(r, "final_stream_error_kind"); len(values) > 0 {
		params.StreamErrorKinds = values
	}
	params.UpstreamStatusCodes, err = repeatableQueryInts(r, "upstream_status_code")
	if err != nil {
		return statsdomain.ChainQueryParams{}, err
	}
	params.GatewayStatusCodes, err = repeatableQueryInts(r, "gateway_status_code")
	if err != nil {
		return statsdomain.ChainQueryParams{}, err
	}
	params.LegacyStatusCodes, err = repeatableQueryInts(r, "legacy_status_code")
	if err != nil {
		return statsdomain.ChainQueryParams{}, err
	}
	params.IngressFinalStatusCodes, err = repeatableQueryInts(r, "ingress_final_status_code")
	if err != nil {
		return statsdomain.ChainQueryParams{}, err
	}
	if values, parseErr := repeatableQueryInts(r, "final_status_code"); parseErr != nil {
		return statsdomain.ChainQueryParams{}, parseErr
	} else if len(values) > 0 {
		params.IngressFinalStatusCodes = values
	}
	params.SortBy = strings.ToLower(strings.TrimSpace(r.URL.Query().Get("sort_by")))
	if params.SortBy == "" {
		params.SortBy = "created_at"
	}
	if params.SortBy != "created_at" {
		return statsdomain.ChainQueryParams{}, &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Code: "chain_sort_unsupported", Detail: "Ingress chain view only supports created_at sorting."}
	}
	params.SortOrder = strings.ToLower(strings.TrimSpace(r.URL.Query().Get("sort_order")))
	params.ChainLimit, err = parsePositiveIntWithDefault(r, "chain_limit", 20)
	if err != nil {
		return statsdomain.ChainQueryParams{}, err
	}
	if params.ChainLimit > 50 {
		return statsdomain.ChainQueryParams{}, &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Code: "chain_limit_exceeded", Detail: "chain_limit must be between 1 and 50."}
	}
	params.ChainRowLimit, err = parsePositiveIntWithDefault(r, "chain_row_limit", 50)
	if err != nil {
		return statsdomain.ChainQueryParams{}, err
	}
	if params.ChainRowLimit > 200 {
		return statsdomain.ChainQueryParams{}, &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Code: "chain_row_limit_exceeded", Detail: "chain_row_limit must be between 1 and 200."}
	}
	params.ChainCursor = normalizedQueryString(r, "chain_cursor")
	params.RowCursor = normalizedQueryString(r, "row_cursor")
	if rawAnchor := strings.TrimSpace(r.URL.Query().Get("anchor_request_log_id")); rawAnchor != "" {
		var anchor int64
		if _, err := fmt.Sscanf(rawAnchor, "%d", &anchor); err != nil || anchor <= 0 {
			return statsdomain.ChainQueryParams{}, &statsdomain.HTTPError{StatusCode: http.StatusBadRequest, Code: "anchor_invalid", Detail: "anchor_request_log_id must be a positive decimal string."}
		}
		params.AnchorRequestLogID = &anchor
	}
	return params, nil
}
