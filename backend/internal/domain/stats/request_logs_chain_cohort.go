package stats

import (
	"fmt"
	"strings"
)

func hasChainRowFilter(params ChainQueryParams) bool {
	return params.ModelID != nil || params.ResolvedTargetModelID != nil || params.EndpointID != nil ||
		params.TerminalTargetID != nil || params.StatusFamily != nil || params.StatusCode != nil ||
		params.ErrorText != nil || len(params.StreamOutcomes) > 0 || len(params.StreamErrorKinds) > 0 ||
		len(params.UpstreamStatusCodes) > 0 || len(params.GatewayStatusCodes) > 0 ||
		len(params.LegacyStatusCodes) > 0 || params.RowResult != nil || params.ClientRulePattern != nil || params.PricingCardRole != nil || params.PricingSelectionState != nil
}

// appendChainRowCohortExists adds the Requests row-filter grammar as an
// ingress-level EXISTS. The outer query still returns the full retained chain
// for a matching ingress; this predicate only decides which ingresses enter
// the page and totals.
func appendChainRowCohortExists(query string, args *[]any, params ChainQueryParams, outerAlias string) string {
	clauses := []string{
		"match_rows.profile_id = " + outerAlias + ".profile_id",
		"match_rows.ingress_request_id = " + outerAlias + ".ingress_request_id",
	}
	clauses = append(clauses, buildChainRowMatchClauses(args, params, "match_rows")...)
	return query + " AND EXISTS (SELECT 1 FROM request_logs match_rows WHERE " + strings.Join(clauses, " AND ") + ")"
}

func buildChainRowMatchClauses(args *[]any, params ChainQueryParams, alias string) []string {
	clauses := make([]string, 0)
	add := func(value any, clause string) {
		*args = append(*args, value)
		clauses = append(clauses, fmt.Sprintf(clause, len(*args)))
	}
	if params.FromTime != nil {
		add(params.FromTime.UTC(), alias+".created_at >= $%d")
	}
	if params.ToTime != nil {
		add(params.ToTime.UTC(), alias+".created_at < $%d")
	}
	if params.ModelID != nil && strings.TrimSpace(*params.ModelID) != "" {
		add(strings.TrimSpace(*params.ModelID), alias+".model_id = $%d")
	}
	if params.ResolvedTargetModelID != nil && strings.TrimSpace(*params.ResolvedTargetModelID) != "" {
		add(strings.TrimSpace(*params.ResolvedTargetModelID), alias+".resolved_target_model_id = $%d")
	}
	if params.EndpointID != nil {
		add(*params.EndpointID, alias+".endpoint_id = $%d")
	}
	if params.TerminalTargetID != nil {
		add(*params.TerminalTargetID, alias+".connection_id = $%d")
	}
	statusExpr := scopedChainRowStatusSQL(alias)
	if params.StatusFamily != nil {
		switch strings.ToLower(strings.TrimSpace(*params.StatusFamily)) {
		case "2xx":
			clauses = append(clauses, "("+statusExpr+") BETWEEN 200 AND 299")
		case "4xx":
			clauses = append(clauses, "("+statusExpr+") BETWEEN 400 AND 499")
		case "5xx":
			clauses = append(clauses, "("+statusExpr+") BETWEEN 500 AND 599")
		}
	}
	if params.StatusCode != nil {
		add(*params.StatusCode, "("+statusExpr+") = $%d")
	}
	if params.ErrorText != nil && strings.TrimSpace(*params.ErrorText) != "" {
		value := "%" + strings.TrimSpace(*params.ErrorText) + "%"
		*args = append(*args, value)
		index := len(*args)
		clauses = append(clauses, fmt.Sprintf("(%s.error_detail ILIKE $%d OR %s.error_code ILIKE $%d OR %s.stream_error_detail ILIKE $%d OR %s.stream_error_kind ILIKE $%d)", alias, index, alias, index, alias, index, alias, index))
	}
	appendValues := func(values []string, column string) {
		if len(values) == 0 {
			return
		}
		placeholders := make([]string, 0, len(values))
		for _, value := range values {
			*args = append(*args, strings.TrimSpace(value))
			placeholders = append(placeholders, fmt.Sprintf("$%d", len(*args)))
		}
		clauses = append(clauses, column+" IN ("+strings.Join(placeholders, ",")+")")
	}
	appendValues(params.StreamOutcomes, alias+".stream_outcome")
	appendValues(params.StreamErrorKinds, alias+".stream_error_kind")
	if params.PricingCardRole != nil && strings.TrimSpace(*params.PricingCardRole) != "" {
		add(strings.TrimSpace(*params.PricingCardRole), alias+".pricing_card_role = $%d")
	}
	if params.PricingSelectionState != nil && strings.TrimSpace(*params.PricingSelectionState) != "" {
		add(strings.TrimSpace(*params.PricingSelectionState), alias+".pricing_selection_state = $%d")
	}
	if len(params.UpstreamStatusCodes) > 0 {
		values := make([]string, 0, len(params.UpstreamStatusCodes))
		for _, value := range params.UpstreamStatusCodes {
			*args = append(*args, value)
			values = append(values, fmt.Sprintf("$%d", len(*args)))
		}
		clauses = append(clauses, alias+".upstream_status_code IN ("+strings.Join(values, ",")+")")
	}
	if len(params.GatewayStatusCodes) > 0 {
		values := make([]string, 0, len(params.GatewayStatusCodes))
		for _, value := range params.GatewayStatusCodes {
			*args = append(*args, value)
			values = append(values, fmt.Sprintf("$%d", len(*args)))
		}
		clauses = append(clauses, alias+".gateway_status_code IN ("+strings.Join(values, ",")+")")
	}
	if len(params.LegacyStatusCodes) > 0 {
		values := make([]string, 0, len(params.LegacyStatusCodes))
		for _, value := range params.LegacyStatusCodes {
			*args = append(*args, value)
			values = append(values, fmt.Sprintf("$%d", len(*args)))
		}
		clauses = append(clauses, alias+".legacy_status_code IN ("+strings.Join(values, ",")+")")
	}
	if params.RowResult != nil {
		switch strings.TrimSpace(*params.RowResult) {
		case "failed":
			clauses = append(clauses, "("+statusExpr+") IS NOT NULL AND NOT (("+statusExpr+") BETWEEN 200 AND 299)")
		case "client_disconnected", "cancelled":
			add(strings.TrimSpace(*params.RowResult), alias+".attempt_result = $%d")
		}
	}
	if params.ClientRulePattern != nil && strings.TrimSpace(*params.ClientRulePattern) != "" {
		add(strings.TrimSpace(*params.ClientRulePattern), alias+".caller_user_agent IS NOT NULL AND btrim("+alias+".caller_user_agent) <> '' AND "+alias+".caller_user_agent ~* $%d")
	}
	return clauses
}

func buildChainRowMatchPredicate(args *[]any, params ChainQueryParams, alias string) string {
	if !hasChainRowFilter(params) && params.ProxyAPIKeyID == nil {
		return "FALSE"
	}
	clauses := buildChainRowMatchClauses(args, params, alias)
	if params.ProxyAPIKeyID != nil {
		*args = append(*args, *params.ProxyAPIKeyID)
		clauses = append(clauses, fmt.Sprintf("%s.proxy_api_key_id_snapshot = $%d", alias, len(*args)))
	}
	if len(clauses) == 0 {
		return "FALSE"
	}
	return strings.Join(clauses, " AND ")
}

func scopedChainRowStatusSQL(alias string) string {
	return fmt.Sprintf(`CASE %s.row_kind
	WHEN 'upstream' THEN %s.upstream_status_code
	WHEN 'planning' THEN %s.gateway_status_code
	WHEN 'admission' THEN %s.gateway_status_code
	ELSE %s.legacy_status_code
END`, alias, alias, alias, alias, alias)
}

// appendChainFinalizedCohortExists constrains a retained-row query to the
// finalized usage event selectors. Final status/pricing/epoch facts remain
// usage-owner facts; this helper never substitutes a request-log row for
// those fields.
func appendChainFinalizedCohortExists(query string, args *[]any, params ChainQueryParams, outerAlias string) string {
	clauses := []string{
		"final_rows.profile_id = " + outerAlias + ".profile_id",
		"final_rows.ingress_request_id = " + outerAlias + ".ingress_request_id",
	}
	add := func(value any, clause string) {
		*args = append(*args, value)
		clauses = append(clauses, fmt.Sprintf(clause, len(*args)))
	}
	if params.IngressFinalResult != nil {
		classifier := `CASE WHEN final_rows.status_code NOT BETWEEN 200 AND 299 THEN 'failed'
			WHEN final_rows.stream_outcome = 'client_disconnected' THEN 'client_disconnected'
			WHEN final_rows.stream_outcome IN ('provider_incomplete','upstream_read_error','gateway_timeout','upstream_ended_without_terminal','unknown') THEN 'failed'
			ELSE 'completed' END`
		add(*params.IngressFinalResult, classifier+" = $%d")
	}
	if params.ConfirmedFailover != nil {
		add(*params.ConfirmedFailover, "final_rows.failover_occurred = $%d")
	}
	if params.FinalTargetModelID != nil && strings.TrimSpace(*params.FinalTargetModelID) != "" {
		add(strings.TrimSpace(*params.FinalTargetModelID), "final_rows.resolved_target_model_id = $%d")
	}
	if params.PricingStatus != nil {
		add(*params.PricingStatus, "final_rows.pricing_status = $%d")
	}
	if len(params.UnpricedReasons) > 0 {
		placeholders := make([]string, 0, len(params.UnpricedReasons))
		for _, reason := range params.UnpricedReasons {
			*args = append(*args, reason)
			placeholders = append(placeholders, fmt.Sprintf("$%d", len(*args)))
		}
		clauses = append(clauses, "final_rows.unpriced_reason IN ("+strings.Join(placeholders, ",")+")")
	}
	if params.ReportingCurrencyEpoch != nil && strings.TrimSpace(*params.ReportingCurrencyEpoch) != "" {
		if *params.ReportingCurrencyEpoch == "__legacy_unknown__" {
			clauses = append(clauses, "final_rows.reporting_currency_epoch IS NULL")
		} else {
			add(*params.ReportingCurrencyEpoch, "final_rows.reporting_currency_epoch = $%d")
		}
	}
	if params.IsStream != nil {
		add(*params.IsStream, "final_rows.is_stream = $%d")
	}
	if len(params.IngressFinalStatusCodes) > 0 {
		placeholders := make([]string, 0, len(params.IngressFinalStatusCodes))
		for _, code := range params.IngressFinalStatusCodes {
			*args = append(*args, code)
			placeholders = append(placeholders, fmt.Sprintf("$%d", len(*args)))
		}
		clauses = append(clauses, "final_rows.status_code IN ("+strings.Join(placeholders, ",")+")")
	}
	if params.CostSegmentKey != nil && strings.TrimSpace(*params.CostSegmentKey) != "" {
		segment := strings.TrimSpace(*params.CostSegmentKey)
		add(segment, canonicalCostSegmentKeySQLFor("final_rows")+" = $%d")
	}
	return query + " AND EXISTS (SELECT 1 FROM usage_request_events final_rows WHERE " + strings.Join(clauses, " AND ") + ")"
}
