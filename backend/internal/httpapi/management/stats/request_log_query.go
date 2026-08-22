package stats

import (
	"net/http"
	"strings"
	"time"

	statsdomain "github.com/coachpo/prism/backend/internal/domain/stats"
)

func parseRequestLogListParams(r *http.Request, profileID int, observabilitySigningKey []byte, referenceNow time.Time) (statsdomain.RequestLogListParams, error) {
	if err := rejectUnsupportedRequestLogQueryKeys(r); err != nil {
		return statsdomain.RequestLogListParams{}, err
	}
	// Observe signed-context deep link: query_context is required whenever any
	// final_* selector is present, and the final cohort is always resolved
	// through the authoritative finalized usage summary (never translated
	// into ordinary filters).
	rawContext := strings.TrimSpace(r.URL.Query().Get("query_context"))
	hasFinalSelector := r.URL.Query().Get("ingress_final_result") != "" ||
		r.URL.Query().Get("confirmed_failover") != "" ||
		r.URL.Query().Get("final_result") != "" ||
		r.URL.Query().Get("final_model_id") != "" ||
		r.URL.Query().Get("final_endpoint_id") != "" ||
		r.URL.Query().Get("final_terminal_target_id") != "" ||
		r.URL.Query().Get("final_pricing_status") != "" ||
		len(r.URL.Query()["final_unpriced_reason"]) > 0 ||
		r.URL.Query().Get("reporting_currency_epoch") != ""
	var queryContextFrom, queryContextTo *time.Time
	if rawContext != "" || hasFinalSelector {
		if rawContext == "" {
			return statsdomain.RequestLogListParams{}, &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Detail: "query_context_required"}
		}
		token, err := statsdomain.VerifyQueryContext(rawContext, observabilitySigningKey, referenceNow)
		if err != nil {
			return statsdomain.RequestLogListParams{}, err
		}
		if token.ProfileID != profileID {
			return statsdomain.RequestLogListParams{}, &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Detail: "query_context scope mismatch"}
		}
		from, err := time.Parse(time.RFC3339, token.UsageFrom)
		if err != nil {
			return statsdomain.RequestLogListParams{}, &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Detail: "invalid query_context"}
		}
		to, err := time.Parse(time.RFC3339, token.UsageTo)
		if err != nil {
			return statsdomain.RequestLogListParams{}, &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Detail: "invalid query_context"}
		}
		queryContextFrom = &from
		queryContextTo = &to
		// The two pricing cohort grammars must never select the same cohort
		// (Requests SPEC §6.x): final_* pricing selectors are exclusive with
		// the ordinary pricing_status/unpriced_reason filters.
		if (r.URL.Query().Get("final_pricing_status") != "" || len(r.URL.Query()["final_unpriced_reason"]) > 0) &&
			(r.URL.Query().Get("pricing_status") != "" || len(r.URL.Query()["unpriced_reason"]) > 0) {
			return statsdomain.RequestLogListParams{}, &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Code: "conflicting_pricing_cohort", Detail: "final pricing selectors cannot be combined with ordinary pricing filters"}
		}
	}
	fromTime, err := parseOptionalTime(r, "from_time")
	if err != nil {
		return statsdomain.RequestLogListParams{}, err
	}
	toTime, err := parseOptionalTime(r, "to_time")
	if err != nil {
		return statsdomain.RequestLogListParams{}, err
	}
	endpointID, err := parseOptionalInt(r, "endpoint_id")
	if err != nil {
		return statsdomain.RequestLogListParams{}, err
	}
	terminalTargetID, err := parseOptionalInt(r, "terminal_target_id")
	if err != nil {
		return statsdomain.RequestLogListParams{}, err
	}
	if terminalTargetID != nil && *terminalTargetID <= 0 {
		return statsdomain.RequestLogListParams{}, &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Code: "invalid_terminal_target_id", Detail: "invalid terminal_target_id"}
	}
	var proxyAPIKeyID *int
	if rawValues, present := r.URL.Query()["proxy_api_key_id"]; present {
		if len(rawValues) != 1 || strings.TrimSpace(rawValues[0]) == "" {
			return statsdomain.RequestLogListParams{}, &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Code: "invalid_proxy_api_key_id", Detail: "invalid proxy_api_key_id"}
		}
		proxyAPIKeyID, err = parseOptionalInt(r, "proxy_api_key_id")
	}
	if err != nil || (proxyAPIKeyID != nil && *proxyAPIKeyID <= 0) {
		return statsdomain.RequestLogListParams{}, &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Code: "invalid_proxy_api_key_id", Detail: "invalid proxy_api_key_id"}
	}
	clientRuleID, err := parseOptionalInt(r, "client_rule_id")
	if err != nil {
		return statsdomain.RequestLogListParams{}, &statsdomain.HTTPError{StatusCode: http.StatusBadRequest, Detail: "invalid client_rule_id"}
	}
	if clientRuleID != nil && *clientRuleID <= 0 {
		return statsdomain.RequestLogListParams{}, &statsdomain.HTTPError{StatusCode: http.StatusBadRequest, Detail: "invalid client_rule_id"}
	}
	limit, err := parsePositiveIntWithDefault(r, "limit", 50)
	if err != nil {
		return statsdomain.RequestLogListParams{}, err
	}
	offset, err := parseNonNegativeIntWithDefault(r, "offset", 0)
	if err != nil {
		return statsdomain.RequestLogListParams{}, err
	}
	statusFamily := normalizedQueryString(r, "status_family")
	if statusFamily != nil {
		normalized := strings.ToLower(strings.TrimSpace(*statusFamily))
		statusFamily = &normalized
	}
	statusCode, err := parseOptionalInt(r, "status_code")
	if err != nil {
		return statsdomain.RequestLogListParams{}, err
	}
	pricingStatus := normalizedQueryString(r, "pricing_status")
	if pricingStatus != nil {
		normalized := strings.ToLower(strings.TrimSpace(*pricingStatus))
		switch normalized {
		case "priced", "unpriced", "ineligible", "unknown":
		default:
			return statsdomain.RequestLogListParams{}, &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Code: "unknown_query_key", Detail: "Unknown pricing_status value: " + normalized}
		}
		pricingStatus = &normalized
	}
	unpricedReasons := repeatableQueryValues(r, "unpriced_reason")
	for _, reason := range unpricedReasons {
		if _, err := parseUnpricedReasonValue(reason); err != nil {
			return statsdomain.RequestLogListParams{}, err
		}
	}
	pricingCardRole := normalizedQueryString(r, "pricing_card_role")
	if pricingCardRole != nil {
		switch *pricingCardRole {
		case "standard", "tier_base", "tier_above", "peak", "offpeak":
		default:
			return statsdomain.RequestLogListParams{}, &statsdomain.HTTPError{StatusCode: http.StatusBadRequest, Code: "pricing_card_role_invalid", Detail: "pricing_card_role is invalid"}
		}
	}
	pricingSelectionState := normalizedQueryString(r, "pricing_selection_state")
	if pricingSelectionState != nil {
		switch *pricingSelectionState {
		case "not_evaluated", "not_applicable", "selected", "unresolved":
		default:
			return statsdomain.RequestLogListParams{}, &statsdomain.HTTPError{StatusCode: http.StatusBadRequest, Code: "pricing_selection_state_invalid", Detail: "pricing_selection_state is invalid"}
		}
	}
	ingressFinalResult := normalizedQueryString(r, "ingress_final_result")
	if ingressFinalResult != nil {
		normalized := strings.ToLower(strings.TrimSpace(*ingressFinalResult))
		switch normalized {
		case "completed", "failed", "client_disconnected":
		default:
			return statsdomain.RequestLogListParams{}, &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Code: "unknown_query_key", Detail: "Unknown ingress_final_result value: " + normalized}
		}
		ingressFinalResult = &normalized
	}
	confirmedFailover, err := parseOptionalBool(r, "confirmed_failover")
	if err != nil {
		return statsdomain.RequestLogListParams{}, err
	}
	finalResult := normalizedQueryString(r, "final_result")
	if finalResult != nil {
		switch *finalResult {
		case "completed", "failed", "client_disconnected":
		default:
			return statsdomain.RequestLogListParams{}, &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Code: "unknown_query_key", Detail: "Unknown final_result value: " + *finalResult}
		}
	}
	finalModelID := normalizedQueryString(r, "final_model_id")
	finalEndpointID, err := parseOptionalInt(r, "final_endpoint_id")
	if err != nil {
		return statsdomain.RequestLogListParams{}, err
	}
	finalTerminalTargetID, err := parseOptionalInt(r, "final_terminal_target_id")
	if err != nil {
		return statsdomain.RequestLogListParams{}, err
	}
	finalPricingStatus := normalizedQueryString(r, "final_pricing_status")
	if finalPricingStatus != nil {
		switch *finalPricingStatus {
		case "priced", "unpriced", "ineligible", "unknown":
		default:
			return statsdomain.RequestLogListParams{}, &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Code: "unknown_query_key", Detail: "Unknown final_pricing_status value: " + *finalPricingStatus}
		}
	}
	finalUnpricedReasons := repeatableQueryValues(r, "final_unpriced_reason")
	for _, reason := range finalUnpricedReasons {
		if _, err := parseUnpricedReasonValue(reason); err != nil {
			return statsdomain.RequestLogListParams{}, err
		}
	}
	reportingEpoch := normalizedQueryString(r, "reporting_currency_epoch")
	sortBy, sortOrder, err := parseRequestLogSort(r)
	if err != nil {
		return statsdomain.RequestLogListParams{}, err
	}
	coveragePreset := strings.TrimSpace(r.URL.Query().Get("time_range"))
	return statsdomain.RequestLogListParams{ProfileID: profileID, IngressFinalResult: ingressFinalResult, ConfirmedFailover: confirmedFailover, IngressRequestID: normalizedQueryString(r, "ingress_request_id"), ModelID: normalizedQueryString(r, "model_id"), ResolvedTargetModelID: normalizedQueryString(r, "resolved_target_model_id"), StatusFamily: statusFamily, StatusCode: statusCode, ErrorText: normalizedQueryString(r, "error_text"), PricingStatus: pricingStatus, UnpricedReasons: unpricedReasons, PricingCardRole: pricingCardRole, PricingSelectionState: pricingSelectionState, FromTime: fromTime, ToTime: toTime, EndpointID: endpointID, TerminalTargetID: terminalTargetID, ProxyAPIKeyID: proxyAPIKeyID, ClientRuleID: clientRuleID, QueryContextFrom: queryContextFrom, QueryContextTo: queryContextTo, FinalResult: finalResult, FinalModelID: finalModelID, FinalEndpointID: finalEndpointID, FinalTerminalTargetID: finalTerminalTargetID, FinalPricingStatus: finalPricingStatus, FinalUnpricedReasons: finalUnpricedReasons, FinalReportingEpoch: reportingEpoch, CoveragePreset: coveragePreset, CoverageRequestedFrom: fromTime, CoverageRequestedTo: toTime, CoverageReferenceNow: referenceNow.UTC(), SortBy: sortBy, SortOrder: sortOrder, Limit: limit, Offset: offset}, nil
}

// parseRequestLogSort resolves the attempt-view sort grammar: `sort_by` over
// created_at|display_status|ttft_ms|total_tokens|total_cost_user_currency_micros
// and `sort_order` asc|desc. An unsupported value is rejected instead of
// falling back to created_at, so a sorted column header can never claim an
// order the returned rows do not have. The ingress-chain view keeps its own
// created_at-only restriction in parseChainQueryParams.
func parseRequestLogSort(r *http.Request) (string, string, error) {
	sortBy := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("sort_by")))
	if sortBy == "" {
		sortBy = "created_at"
	}
	switch sortBy {
	case "created_at", "display_status", "ttft_ms", "total_tokens", "total_cost_user_currency_micros":
	default:
		return "", "", &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Code: "sort_unsupported", Detail: "Unsupported sort_by: " + sortBy}
	}
	sortOrder := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("sort_order")))
	if sortOrder == "" {
		sortOrder = "desc"
	}
	if sortOrder != "asc" && sortOrder != "desc" {
		return "", "", &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Code: "sort_unsupported", Detail: "Unsupported sort_order: " + sortOrder}
	}
	return sortBy, sortOrder, nil
}

// rejectUnsupportedRequestLogQueryKeys enforces the strict request-log query
// grammar (Requests SPEC §6.1/§6.3): the legacy `priced=true|false` alias is
// not a compatibility branch — any unknown query key returns a typed
// 422 unknown_query_key with no migration hint. The Observe signed-context
// deep-link family (query_context + final_*) is part of the grammar and is
// never translated into ordinary filters.
func rejectUnsupportedRequestLogQueryKeys(r *http.Request) error {
	supported := map[string]struct{}{
		"ingress_request_id":       {},
		"ingress_final_result":     {},
		"confirmed_failover":       {},
		"model_id":                 {},
		"resolved_target_model_id": {},
		"status_family":            {},
		"status_code":              {},
		"error_text":               {},
		"pricing_status":           {},
		"unpriced_reason":          {},
		"pricing_card_role":        {},
		"pricing_selection_state":  {},
		"from_time":                {},
		"to_time":                  {},
		"time_range":               {},
		"endpoint_id":              {},
		"terminal_target_id":       {},
		"proxy_api_key_id":         {},
		"client_rule_id":           {},
		"limit":                    {},
		"offset":                   {},
		"query_context":            {},
		"final_result":             {},
		"final_model_id":           {},
		"final_endpoint_id":        {},
		"final_terminal_target_id": {},
		"final_pricing_status":     {},
		"final_unpriced_reason":    {},
		"reporting_currency_epoch": {},
		"view":                     {},
		"observe_return":           {},
		"sort_by":                  {},
		"sort_order":               {},
	}
	for key := range r.URL.Query() {
		if _, ok := supported[key]; !ok {
			return &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Code: "unknown_query_key", Detail: "Unknown query key: " + key}
		}
	}
	return nil
}
