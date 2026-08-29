package stats

import (
	"net/http"
	"strconv"
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
	hasFinalSelector := requestLogHasSignedCohortSelector(r)
	var queryContextFrom, queryContextTo *time.Time
	if rawContext != "" || hasFinalSelector {
		if rawContext == "" {
			return statsdomain.RequestLogListParams{}, &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Code: "query_context_required", Detail: "query_context is required with signed cohort selectors"}
		}
		token, err := statsdomain.VerifyQueryContext(rawContext, observabilitySigningKey, referenceNow)
		if err != nil {
			return statsdomain.RequestLogListParams{}, err
		}
		if token.ProfileID != profileID {
			return statsdomain.RequestLogListParams{}, &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Detail: "query_context scope mismatch"}
		}
		requestBounds, err := statsdomain.QueryBoundsForDomain(token, "request_logs")
		if err != nil {
			return statsdomain.RequestLogListParams{}, &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Detail: "invalid query_context"}
		}
		from := requestBounds.UsageFrom.UTC()
		to := requestBounds.UsageTo.UTC()
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
	if queryContextFrom != nil && queryContextTo != nil {
		from := queryContextFrom.UTC()
		to := queryContextTo.UTC()
		fromTime = &from
		toTime = &to
	}
	modelID, modelIDs, modelIDIsNull := parseRepeatedStringOrNull(r, "ingress_model_id")
	resolvedTargetModelID, resolvedTargetModelIDs, resolvedTargetModelIDIsNull := parseRepeatedStringOrNull(r, "attempt_target_model_id")
	_, apiFamilies, apiFamilyIsNull := parseRepeatedStringOrNull(r, "api_family")
	_, rowKinds, rowKindIsNull := parseRepeatedStringOrNull(r, "row_kind")
	rowKinds = lowerSelectorValues(rowKinds)
	if rowKindIsNull {
		return statsdomain.RequestLogListParams{}, invalidQueryParameter("row_kind", "does not support __null__")
	}
	if err := validateSelectorValues("row_kind", rowKinds, "planning", "admission", "upstream", "legacy_unknown"); err != nil {
		return statsdomain.RequestLogListParams{}, err
	}
	endpointID, endpointIDs, endpointIDIsNull, err := parseRepeatedIntOrNull(r, "endpoint_id", true)
	if err != nil {
		return statsdomain.RequestLogListParams{}, err
	}
	terminalTargetID, terminalTargetIDs, terminalTargetIDIsNull, err := parseRepeatedIntOrNull(r, "terminal_target_id", true)
	if err != nil {
		return statsdomain.RequestLogListParams{}, err
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
	statusCode, statusCodes, statusCodeIsNull, err := parseRepeatedIntOrNull(r, "status_code", false)
	if err != nil {
		return statsdomain.RequestLogListParams{}, err
	}
	_, streamOutcomes, streamOutcomeIsNull := parseRepeatedStringOrNull(r, "stream_outcome")
	streamOutcomes = lowerSelectorValues(streamOutcomes)
	if err := validateSelectorValues("stream_outcome", streamOutcomes,
		"not_streaming", "completed", "gateway_timeout", "provider_incomplete", "client_disconnected", "upstream_read_error", "upstream_ended_without_terminal", "unknown"); err != nil {
		return statsdomain.RequestLogListParams{}, err
	}
	_, streamErrorKinds, streamErrorKindIsNull := parseRepeatedStringOrNull(r, "stream_error_kind")
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
	unpricedReasons := collectRepeatedCommaValues(r, "unpriced_reason")
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
	finalResults := lowerSelectorValues(collectRepeatedCommaValues(r, "final_result"))
	if err := validateSelectorValues("final_result", finalResults, "completed", "failed", "client_disconnected"); err != nil {
		return statsdomain.RequestLogListParams{}, err
	}
	finalOutcomeDetails := lowerSelectorValues(collectRepeatedCommaValues(r, "outcome_detail"))
	if err := validateSelectorValues("outcome_detail", finalOutcomeDetails, "completed", "http_error", "stream_error", "client_disconnected"); err != nil {
		return statsdomain.RequestLogListParams{}, err
	}
	var finalResult *string
	if len(finalResults) > 0 {
		normalized := finalResults[0]
		finalResult = &normalized
	}
	finalModelID, finalModelIDs, finalModelIDIsNull := parseRepeatedStringOrNull(r, "final_target_model_id")
	finalEndpointID, finalEndpointIDs, finalEndpointIsNull, err := parseRepeatedIntOrNull(r, "final_endpoint_id", true)
	if err != nil {
		return statsdomain.RequestLogListParams{}, err
	}
	finalTerminalTargetID, finalTerminalTargetIDs, finalTerminalIsNull, err := parseRepeatedIntOrNull(r, "final_terminal_target_id", true)
	if err != nil {
		return statsdomain.RequestLogListParams{}, err
	}
	finalPricingStatus, finalPricingStatuses, finalPricingStatusIsNull := parseRepeatedStringOrNull(r, "final_pricing_status")
	finalPricingStatuses = lowerSelectorValues(finalPricingStatuses)
	if err := validateSelectorValues("final_pricing_status", finalPricingStatuses, "priced", "unpriced", "ineligible", "unknown"); err != nil {
		return statsdomain.RequestLogListParams{}, err
	}
	if len(finalPricingStatuses) > 0 {
		first := finalPricingStatuses[0]
		finalPricingStatus = &first
	}
	finalUnpricedReasons := collectRepeatedCommaValues(r, "final_unpriced_reason")
	for _, reason := range finalUnpricedReasons {
		if _, err := parseUnpricedReasonValue(reason); err != nil {
			return statsdomain.RequestLogListParams{}, err
		}
	}
	reportingEpoch, reportingEpochs, reportingEpochIsNull := parseRepeatedStringOrNull(r, "reporting_currency_epoch")
	for index, value := range reportingEpochs {
		if value == "__legacy_unknown__" {
			reportingEpochIsNull = true
			reportingEpochs = append(reportingEpochs[:index], reportingEpochs[index+1:]...)
			break
		}
	}
	if reportingEpoch == nil && reportingEpochIsNull {
		legacyUnknown := "__legacy_unknown__"
		reportingEpoch = &legacyUnknown
	}
	_, finalStatusCodes, finalStatusCodeIsNull, err := parseRepeatedIntOrNull(r, "final_status_code", false)
	if err != nil {
		return statsdomain.RequestLogListParams{}, err
	}
	_, finalStreamOutcomes, finalStreamOutcomeIsNull := parseRepeatedStringOrNull(r, "final_stream_outcome")
	finalStreamOutcomes = lowerSelectorValues(finalStreamOutcomes)
	if err := validateSelectorValues("final_stream_outcome", finalStreamOutcomes,
		"not_streaming", "completed", "gateway_timeout", "provider_incomplete", "client_disconnected", "upstream_read_error", "upstream_ended_without_terminal", "unknown"); err != nil {
		return statsdomain.RequestLogListParams{}, err
	}
	_, finalStreamErrorKinds, finalStreamErrorKindIsNull := parseRepeatedStringOrNull(r, "final_stream_error_kind")
	finalExclusion, err := parseFinalizedCohortExclusion(r)
	if err != nil {
		return statsdomain.RequestLogListParams{}, err
	}
	sortBy, sortOrder, err := parseRequestLogSort(r)
	if err != nil {
		return statsdomain.RequestLogListParams{}, err
	}
	_, attemptTriggers, attemptTriggerIsNull := parseRepeatedStringOrNull(r, "attempt_trigger")
	attemptTriggers = lowerSelectorValues(attemptTriggers)
	if err := validateSelectorValues("attempt_trigger", attemptTriggers, "initial", "retry_same_target", "hedge", "failover"); err != nil {
		return statsdomain.RequestLogListParams{}, err
	}
	_, attemptResults, attemptResultIsNull := parseRepeatedStringOrNull(r, "attempt_result")
	attemptResults = lowerSelectorValues(attemptResults)
	if err := validateSelectorValues("attempt_result", attemptResults, "completed", "http_error", "stream_error", "transport_error", "cancelled", "client_disconnected", "unknown"); err != nil {
		return statsdomain.RequestLogListParams{}, err
	}
	coveragePreset := strings.TrimSpace(r.URL.Query().Get("time_range"))
	return statsdomain.RequestLogListParams{
		ProfileID: profileID, IngressFinalResult: ingressFinalResult, ConfirmedFailover: confirmedFailover,
		IngressRequestID: normalizedQueryString(r, "ingress_request_id"),
		ModelID:          modelID, ModelIDs: modelIDs, ModelIDIsNull: modelIDIsNull,
		ResolvedTargetModelID: resolvedTargetModelID, ResolvedTargetModelIDs: resolvedTargetModelIDs, ResolvedTargetModelIDIsNull: resolvedTargetModelIDIsNull,
		APIFamilies: apiFamilies, APIFamilyIsNull: apiFamilyIsNull, RowKinds: rowKinds,
		StatusFamily: statusFamily, StatusCode: statusCode, StatusCodes: statusCodes, StatusCodeIsNull: statusCodeIsNull,
		StreamOutcomes: streamOutcomes, StreamOutcomeIsNull: streamOutcomeIsNull,
		StreamErrorKinds: streamErrorKinds, StreamErrorKindIsNull: streamErrorKindIsNull,
		ErrorText:     normalizedQueryString(r, "error_text"),
		PricingStatus: pricingStatus, UnpricedReasons: unpricedReasons, PricingCardRole: pricingCardRole, PricingSelectionState: pricingSelectionState,
		FromTime: fromTime, ToTime: toTime,
		EndpointID: endpointID, EndpointIDs: endpointIDs, EndpointIDIsNull: endpointIDIsNull,
		TerminalTargetID: terminalTargetID, TerminalTargetIDs: terminalTargetIDs, TerminalTargetIDIsNull: terminalTargetIDIsNull,
		ProxyAPIKeyID: proxyAPIKeyID, ClientRuleID: clientRuleID,
		QueryContextFrom: queryContextFrom, QueryContextTo: queryContextTo,
		FinalResult: finalResult, FinalResults: finalResults, FinalOutcomeDetails: finalOutcomeDetails,
		FinalStatusCodes: finalStatusCodes, FinalStatusCodeIsNull: finalStatusCodeIsNull,
		FinalStreamOutcomes: finalStreamOutcomes, FinalStreamOutcomeIsNull: finalStreamOutcomeIsNull,
		FinalStreamErrorKinds: finalStreamErrorKinds, FinalStreamErrorKindIsNull: finalStreamErrorKindIsNull,
		FinalModelID: finalModelID, FinalModelIDs: finalModelIDs, FinalModelIDIsNull: finalModelIDIsNull,
		FinalEndpointID: finalEndpointID, FinalEndpointIDs: finalEndpointIDs, FinalEndpointIDIsNull: finalEndpointIsNull,
		FinalTerminalTargetID: finalTerminalTargetID, FinalTerminalTargetIDs: finalTerminalTargetIDs, FinalTerminalTargetIDIsNull: finalTerminalIsNull,
		FinalPricingStatus: finalPricingStatus, FinalPricingStatuses: finalPricingStatuses, FinalPricingStatusIsNull: finalPricingStatusIsNull, FinalUnpricedReasons: finalUnpricedReasons,
		FinalReportingEpoch: reportingEpoch, FinalReportingEpochs: reportingEpochs, FinalReportingEpochIsNull: reportingEpochIsNull,
		FinalExclusion:  finalExclusion,
		AttemptTriggers: attemptTriggers, AttemptTriggerIsNull: attemptTriggerIsNull,
		AttemptResults: attemptResults, AttemptResultIsNull: attemptResultIsNull,
		CoveragePreset: coveragePreset, CoverageRequestedFrom: fromTime, CoverageRequestedTo: toTime, CoverageReferenceNow: referenceNow.UTC(),
		SortBy: sortBy, SortOrder: sortOrder, Limit: limit, Offset: offset,
	}, nil
}

// parseFinalizedCohortExclusion parses the one structured, signed-only
// complement selector used by usage-errors Top-N remainders:
//
//	final_exclude=<facet>,<visible-value>,...
//
// It is deliberately capped at the usage-errors limit (50 visible values)
// and every facet/value is validated before the domain query sees it.
func parseFinalizedCohortExclusion(r *http.Request) (*statsdomain.FinalizedCohortExclusion, error) {
	parts := collectRepeatedCommaValues(r, "final_exclude")
	if len(parts) == 0 {
		return nil, nil
	}
	if len(parts) < 2 || len(parts) > 51 {
		return nil, invalidQueryParameter("final_exclude", "must contain one facet and between 1 and 50 visible values")
	}
	facet := strings.ToLower(strings.TrimSpace(parts[0]))
	exclusion := &statsdomain.FinalizedCohortExclusion{Facet: facet, Values: make([]string, 0, len(parts)-1)}
	seen := map[string]struct{}{}
	appendValue := func(value string) {
		if _, duplicate := seen[value]; duplicate {
			return
		}
		seen[value] = struct{}{}
		exclusion.Values = append(exclusion.Values, value)
	}
	for _, raw := range parts[1:] {
		value := strings.TrimSpace(raw)
		if value == "__null__" {
			exclusion.ExcludeNull = true
			continue
		}
		switch facet {
		case statsdomain.FinalExclusionStatusCode:
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed < 100 || parsed > 599 {
				return nil, invalidQueryParameter("final_exclude", "status_code values must be within [100, 599]")
			}
			appendValue(strconv.Itoa(parsed))
		case statsdomain.FinalExclusionStreamOutcome:
			value = strings.ToLower(value)
			if err := validateSelectorValues("final_exclude stream_outcome", []string{value},
				"not_streaming", "completed", "gateway_timeout", "provider_incomplete", "client_disconnected", "upstream_read_error", "upstream_ended_without_terminal", "unknown"); err != nil {
				return nil, err
			}
			appendValue(value)
		case statsdomain.FinalExclusionAPIFamily:
			value = strings.ToLower(value)
			if err := validateSelectorValues("final_exclude api_family", []string{value}, "openai", "anthropic", "gemini"); err != nil {
				return nil, err
			}
			appendValue(value)
		case statsdomain.FinalExclusionIngressModel, statsdomain.FinalExclusionFinalTargetModel:
			if len(value) > 200 {
				return nil, invalidQueryParameter("final_exclude", "model values must not exceed 200 characters")
			}
			appendValue(value)
		case statsdomain.FinalExclusionStreamErrorKind:
			if len(value) > 50 {
				return nil, invalidQueryParameter("final_exclude", "stream_error_kind values must not exceed 50 characters")
			}
			appendValue(value)
		case statsdomain.FinalExclusionFinalEndpoint, statsdomain.FinalExclusionFinalTerminalTarget:
			parsed, err := strconv.ParseInt(value, 10, 64)
			if err != nil || parsed <= 0 {
				return nil, invalidQueryParameter("final_exclude", "entity IDs must be positive integers")
			}
			appendValue(strconv.FormatInt(parsed, 10))
		default:
			return nil, invalidQueryParameter("final_exclude", "facet is not supported")
		}
	}
	if len(exclusion.Values) == 0 && !exclusion.ExcludeNull {
		return nil, invalidQueryParameter("final_exclude", "must exclude at least one visible value")
	}
	return exclusion, nil
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
func collectRepeatedCommaValues(r *http.Request, key string) []string {
	result := []string{}
	seen := map[string]struct{}{}
	for _, raw := range r.URL.Query()[key] {
		// Support both repeated keys and comma-separated URL values.
		for _, part := range strings.Split(raw, ",") {
			trimmed := strings.TrimSpace(part)
			if trimmed == "" {
				continue
			}
			if _, duplicate := seen[trimmed]; duplicate {
				continue
			}
			seen[trimmed] = struct{}{}
			result = append(result, trimmed)
		}
	}
	return result
}

func parseRepeatedStringOrNull(r *http.Request, key string) (*string, []string, bool) {
	rawValues := collectRepeatedCommaValues(r, key)
	values := make([]string, 0, len(rawValues))
	hasNull := false
	for _, value := range rawValues {
		if value == "__null__" {
			hasNull = true
			continue
		}
		values = append(values, value)
	}
	if len(values) == 0 {
		return nil, values, hasNull
	}
	first := values[0]
	return &first, values, hasNull
}

func parseRepeatedIntOrNull(r *http.Request, key string, positive bool) (*int, []int, bool, error) {
	values := collectRepeatedCommaValues(r, key)
	if len(values) == 0 {
		return nil, nil, false, nil
	}
	hasNull := false
	numbers := []int{}
	seen := map[int]struct{}{}
	var first *int
	for _, value := range values {
		if value == "__null__" {
			hasNull = true
			continue
		}
		parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 32)
		if err != nil || (positive && parsed <= 0) {
			reason := "must be an integer or __null__"
			if positive {
				reason = "must be a positive integer or __null__"
			}
			return nil, nil, false, invalidQueryParameter(key, reason)
		}
		num := int(parsed)
		if _, duplicate := seen[num]; duplicate {
			continue
		}
		seen[num] = struct{}{}
		numbers = append(numbers, num)
		if first == nil {
			first = &num
		}
	}
	return first, numbers, hasNull, nil
}

func lowerSelectorValues(values []string) []string {
	result := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		normalized := strings.ToLower(strings.TrimSpace(value))
		if _, duplicate := seen[normalized]; duplicate {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	return result
}

func validateSelectorValues(key string, values []string, allowedValues ...string) error {
	allowed := make(map[string]struct{}, len(allowedValues))
	for _, value := range allowedValues {
		allowed[value] = struct{}{}
	}
	for _, value := range values {
		if _, ok := allowed[value]; !ok {
			return &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Code: "unknown_query_key", Detail: "Unknown " + key + " value: " + value}
		}
	}
	return nil
}

func rejectUnsupportedRequestLogQueryKeys(r *http.Request) error {
	supported := map[string]struct{}{
		"ingress_request_id":       {},
		"ingress_final_result":     {},
		"confirmed_failover":       {},
		"ingress_model_id":         {},
		"attempt_target_model_id":  {},
		"api_family":               {},
		"row_kind":                 {},
		"attempt_trigger":          {},
		"attempt_result":           {},
		"stream_outcome":           {},
		"stream_error_kind":        {},
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
		"outcome_detail":           {},
		"final_status_code":        {},
		"final_stream_outcome":     {},
		"final_stream_error_kind":  {},
		"final_exclude":            {},
		"final_target_model_id":    {},
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
