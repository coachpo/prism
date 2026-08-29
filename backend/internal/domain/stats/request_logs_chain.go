package stats

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Ingress chain read model (Observe SPEC §6.11 base envelope; Requests SPEC
// §6.5 superset). The outer page is keyed by ingress; each item carries a
// bounded retained-row page plus completeness/coverage facts. All row-scoped
// filters select the ingress set BEFORE pagination via EXISTS; the outer page
// never splits one ingress across two pages.

// ChainQueryParams is the canonical ingress-chain query.
type ChainQueryParams struct {
	ProfileID               int
	View                    string // "ingress_chains" | "attempts"
	Q                       *string
	IngressRequestID        *string
	IngressFinalResult      *string
	RowResult               *string
	ConfirmedFailover       *bool
	PricingStatus           *string
	UnpricedReasons         []string
	PricingCardRole         *string
	PricingSelectionState   *string
	ReportingCurrencyEpoch  *string
	CostSegmentKey          *string
	IsStream                *bool
	StreamOutcomes          []string
	StreamErrorKinds        []string
	UpstreamStatusCodes     []int
	GatewayStatusCodes      []int
	LegacyStatusCodes       []int
	IngressFinalStatusCodes []int
	ModelID                 *string
	ResolvedTargetModelID   *string
	FinalTargetModelID      *string
	EndpointID              *int
	TerminalTargetID        *int
	StatusFamily            *string
	StatusCode              *int
	ClientRuleID            *int
	ClientRulePattern       *string
	ErrorText               *string
	ProxyAPIKeyID           *int
	FromTime                *time.Time
	ToTime                  *time.Time
	SortBy                  string
	SortOrder               string
	Limit                   int
	Cursor                  *string
	ChainLimit              int
	ChainCursor             *string
	ChainRowLimit           int
	RowCursor               *string
	AnchorRequestLogID      *int64
	CoveragePreset          string
	CoverageRequestedFrom   *time.Time
	CoverageRequestedTo     *time.Time
	// CoverageReferenceNow keeps the preset, SQL bounds, and owner projection
	// on the same server clock. Zero means use the domain wall clock.
	CoverageReferenceNow time.Time
}

// ChainIngressItem is one outer-page item.
type ChainIngressItem struct {
	IngressRequestID             string            `json:"ingress_request_id"`
	StartedAt                    *time.Time        `json:"started_at"`
	CompletedAt                  *time.Time        `json:"completed_at"`
	ElapsedMS                    *int64            `json:"elapsed_ms"`
	ElapsedEvidenceState         string            `json:"elapsed_evidence_state"`
	FinalizedEvidenceState       string            `json:"finalized_evidence_state"`
	FinalizedSummary             *FinalizedSummary `json:"finalized_summary"`
	ExpectedAttemptCount         *int              `json:"expected_attempt_count"`
	ExpectedRequestLogRowCount   *int              `json:"expected_request_log_row_count"`
	RetainedUpstreamAttemptCount int               `json:"retained_upstream_attempt_count"`
	RetainedRequestLogRowCount   int               `json:"retained_request_log_row_count"`
	LegacyUnknownRowCount        int               `json:"legacy_unknown_row_count"`
	ChainComplete                *bool             `json:"chain_complete"`
	SameTargetRetryOccurred      bool              `json:"same_target_retry_occurred"`
	HedgeOccurred                bool              `json:"hedge_occurred"`
	FailoverOccurred             bool              `json:"failover_occurred"`
	RoutingEvidenceComplete      *bool             `json:"routing_evidence_complete"`
	RetainedRowsLoadedCount      int               `json:"retained_rows_loaded_count"`
	RetainedRowsPageComplete     bool              `json:"retained_rows_page_complete"`
	RetainedRowCount             int               `json:"retained_row_count"`
	MatchedRowCount              int               `json:"matched_row_count"`
	NextRowCursor                *string           `json:"next_row_cursor"`
	RetainedRows                 []ChainRowItem    `json:"retained_rows"`
	OrderEvidenceState           string            `json:"order_evidence_state,omitempty"`
}

// FinalizedSummary is the authoritative finalized-ingress projection from
// usage_request_events.
type FinalizedSummary struct {
	RequestLogID                      *string      `json:"request_log_id,omitempty"`
	FinalStatusCode                   int          `json:"final_status_code"`
	FinalResult                       string       `json:"final_result"`
	FinalErrorCode                    *string      `json:"final_error_code"`
	RequestedModelID                  string       `json:"-"`
	RequestedModel                    *ModelRef    `json:"ingress_model"`
	ResolvedModel                     *ModelRef    `json:"final_target_model"`
	TerminalTarget                    *TargetRef   `json:"terminal_target"`
	Endpoint                          *EndpointRef `json:"endpoint"`
	TTFTMS                            *int         `json:"ttft_ms"`
	OutputRateTPS                     *float64     `json:"output_rate_tps"`
	OutputTokens                      *int         `json:"-"`
	CompletionDurationMS              *int         `json:"-"`
	TotalTokens                       *int         `json:"total_tokens"`
	TotalCostUserCurrencyMicros       *int64       `json:"total_cost_user_currency_micros"`
	ReportCurrencyCode                *string      `json:"report_currency_code"`
	ReportCurrencySymbol              *string      `json:"report_currency_symbol"`
	ReportingCurrencyEpoch            *int         `json:"reporting_currency_epoch"`
	CurrencyAttribution               string       `json:"currency_attribution"`
	CostSegmentKey                    *string      `json:"cost_segment_key"`
	FinalPricingStatus                string       `json:"final_pricing_status"`
	FinalUnpricedReason               *string      `json:"final_unpriced_reason"`
	FinalPricingResolutionKind        *string      `json:"final_pricing_resolution_kind"`
	MissingPriceComponents            []string     `json:"missing_price_components"`
	FinalPricingEvidenceTrust         string       `json:"final_pricing_evidence_trust"`
	PricingTemplateIDUsed             *int         `json:"pricing_template_id_used"`
	PricingTemplateNameSnapshot       *string      `json:"pricing_template_name_snapshot"`
	PricingTemplateRevisionIDUsed     *int64       `json:"pricing_template_revision_id_used"`
	PricingConfigVersionUsed          *int         `json:"pricing_config_version_used"`
	PricingVersionEffectiveAt         *time.Time   `json:"pricing_version_effective_at"`
	PricingSnapshotUnit               *string      `json:"pricing_snapshot_unit"`
	PricingSnapshotInput              *string      `json:"pricing_snapshot_input"`
	PricingSnapshotOutput             *string      `json:"pricing_snapshot_output"`
	PricingSnapshotCacheReadInput     *string      `json:"pricing_snapshot_cache_read_input"`
	PricingSnapshotCacheCreationInput *string      `json:"pricing_snapshot_cache_creation_input"`
	PricingSnapshotReasoning          *string      `json:"pricing_snapshot_reasoning"`
	AttemptCount                      int          `json:"attempt_count"`
	ExpectedRequestLogRowCount        *int         `json:"-"`
	FinalAttemptNumber                *int         `json:"final_attempt_number"`
	FinalAttemptTrigger               *string      `json:"final_attempt_trigger"`
	FinalTargetEntryTrigger           *string      `json:"final_target_entry_trigger"`
	SameTargetRetryOccurred           bool         `json:"-"`
	HedgeOccurred                     bool         `json:"-"`
	FailoverOccurred                  bool         `json:"-"`
	RoutingEvidenceComplete           *bool        `json:"-"`
	SuccessFlag                       bool         `json:"-"`
	StreamOutcome                     string       `json:"-"`
	IngressStartedAt                  *time.Time   `json:"-"`
	IngressCompletedAt                *time.Time   `json:"-"`
}

// ModelRef / TargetRef / EndpointRef are identity projections.
type ModelRef struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type TargetRef struct {
	ID           int     `json:"id"`
	Label        string  `json:"label"`
	Configured   bool    `json:"configured"`
	OwnerModelID *string `json:"owner_model_id"`
}

type EndpointRef struct {
	ID    int    `json:"id"`
	Label string `json:"label"`
}

// ChainRowItem is one retained request-log row within a chain item.
type ChainRowItem struct {
	RequestLogID                      string    `json:"request_log_id"`
	MatchedByFilter                   bool      `json:"matched_by_filter,omitempty"`
	RowKind                           string    `json:"row_kind"`
	IngressRequestID                  string    `json:"ingress_request_id"`
	AttemptNumber                     *int      `json:"attempt_number"`
	AttemptTrigger                    *string   `json:"attempt_trigger"`
	AttemptResult                     *string   `json:"attempt_result"`
	IsWinner                          *bool     `json:"is_winner"`
	AttemptDurationMS                 *int      `json:"attempt_duration_ms"`
	LegacyDurationMS                  *int      `json:"legacy_duration_ms"`
	UpstreamStatusCode                *int      `json:"upstream_status_code"`
	GatewayStatusCode                 *int      `json:"gateway_status_code"`
	LegacyStatusCode                  *int      `json:"legacy_status_code"`
	ErrorSource                       *string   `json:"error_source"`
	ErrorCode                         *string   `json:"error_code"`
	FailureStage                      *string   `json:"failure_stage"`
	FailureDetailPreview              *string   `json:"failure_detail_preview"`
	FailureDetailSource               string    `json:"failure_detail_source"`
	FailureDetailPreviewTruncated     bool      `json:"failure_detail_preview_truncated"`
	FailureDetailRedacted             bool      `json:"failure_detail_redacted"`
	FailureDetailPersistenceTruncated bool      `json:"failure_detail_persistence_truncated"`
	StreamOutcome                     string    `json:"stream_outcome"`
	StreamErrorKind                   *string   `json:"stream_error_kind"`
	ModelID                           string    `json:"ingress_model_id"`
	ResolvedTargetModelID             *string   `json:"attempt_target_model_id"`
	EndpointID                        *int      `json:"endpoint_id"`
	TerminalTargetID                  *int      `json:"terminal_target_id"`
	TerminalTargetLabel               *string   `json:"terminal_target_label"`
	TerminalTargetConfigured          bool      `json:"terminal_target_configured"`
	TerminalTargetOwnerModelID        *string   `json:"terminal_target_owner_model_id"`
	TotalTokens                       *int      `json:"total_tokens"`
	TotalCostUserCurrencyMicros       *int64    `json:"total_cost_user_currency_micros"`
	PricingStatus                     string    `json:"pricing_status"`
	UnpricedReason                    *string   `json:"unpriced_reason"`
	PricingResolutionKind             *string   `json:"pricing_resolution_kind"`
	PricingEvidenceTrust              string    `json:"pricing_evidence_trust"`
	PricingTemplateKind               *string   `json:"pricing_template_kind"`
	PricingSelectionState             *string   `json:"pricing_selection_state"`
	PricingCardRole                   *string   `json:"pricing_card_role"`
	PricingSelectorThresholdTokens    *int      `json:"pricing_selector_threshold_tokens"`
	PricingSelectorBasisTokens        *int64    `json:"pricing_selector_basis_tokens"`
	CreatedAt                         time.Time `json:"created_at"`
	IsCurrent                         bool      `json:"is_current,omitempty"`
}

// ChainResponse is the outer-page envelope.
type ChainResponse struct {
	// View is the fixed view discriminator of the ingress-chain envelope.
	View                         string                      `json:"view"`
	QueryContext                 *string                     `json:"query_context"`
	SourceIngressTotal           *int                        `json:"source_ingress_total"`
	RetainedIngressTotal         int                         `json:"retained_ingress_total"`
	RetainedUpstreamAttemptTotal int                         `json:"retained_upstream_attempt_total"`
	RetainedRequestLogRowTotal   int                         `json:"retained_request_log_row_total"`
	LegacyUnknownRowTotal        int                         `json:"legacy_unknown_row_total"`
	PageIngressCount             int                         `json:"page_ingress_count"`
	PageUpstreamAttemptCount     int                         `json:"page_upstream_attempt_count"`
	PageRequestLogRowCount       int                         `json:"page_request_log_row_count"`
	FilterOptions                RequestLogListFilterOptions `json:"filter_options"`
	Items                        []ChainIngressItem          `json:"items"`
	HasMoreChains                bool                        `json:"has_more_chains"`
	NextChainCursor              *string                     `json:"next_chain_cursor"`
	SourceCoverage               *json.RawMessage            `json:"source_coverage"`
	RawFinalizedCoverage         *json.RawMessage            `json:"raw_finalized_coverage"`
	AttemptCoverage              *json.RawMessage            `json:"attempt_coverage"`
	DrilldownCoverage            *json.RawMessage            `json:"drilldown_coverage"`
	OrderEvidenceState           string                      `json:"order_evidence_state,omitempty"`
	Caliber                      ScopeCaliber                `json:"caliber"`
	DatasetCoverage              DatasetCoverage             `json:"dataset_coverage"`
	Samples                      ScopeSampleCounts           `json:"samples"`
}

const defaultChainLimit = 20
const maxChainLimit = 50
const defaultChainRowLimit = 50
const maxChainRowLimit = 200

// ListIngressChains returns one outer page of ingresses with bounded retained
// rows per ingress. Row-scoped filters select the ingress set first; the
// outer keyset cursor never splits an ingress across pages.
func ListIngressChains(ctx context.Context, exec queryExecutor, params ChainQueryParams) (ChainResponse, error) {
	if params.CostSegmentKey != nil {
		normalized, err := NormalizeCostSegmentKey(*params.CostSegmentKey)
		if err != nil {
			return ChainResponse{}, err
		}
		params.CostSegmentKey = normalized
	}
	referenceNow := time.Now().UTC()
	if !params.CoverageReferenceNow.IsZero() {
		referenceNow = params.CoverageReferenceNow.UTC()
	}
	requestSource, err := LoadRetentionSourceProjection(ctx, exec, "request_logs", referenceNow)
	if err != nil {
		return ChainResponse{}, err
	}
	if requestSource.PurgeState == "running" || requestSource.PurgeState == "recovery_required" {
		return ChainResponse{}, &HTTPError{StatusCode: 503, Code: "request_log_purge_in_progress", Detail: "request logs are temporarily unavailable while retention cleanup is publishing"}
	}
	params, requestActual, err := resolveChainQueryBoundsWithOwnerReads(ctx, exec, params, referenceNow, requestSource)
	if err != nil {
		return ChainResponse{}, err
	}
	requestGeneration, _ := strconv.ParseInt(requestSource.RetentionGeneration, 10, 64)
	requestEpoch := requestSource.RevocationEpoch
	if params.ChainLimit <= 0 {
		params.ChainLimit = defaultChainLimit
	}
	if params.ChainLimit > maxChainLimit {
		params.ChainLimit = maxChainLimit
	}
	if params.ChainRowLimit <= 0 {
		params.ChainRowLimit = defaultChainRowLimit
	}
	if params.ChainRowLimit > maxChainRowLimit {
		params.ChainRowLimit = maxChainRowLimit
	}
	sortOrder := strings.ToLower(strings.TrimSpace(params.SortOrder))
	if sortOrder == "" {
		sortOrder = "desc"
	}
	if sortOrder != "desc" && sortOrder != "asc" {
		return ChainResponse{}, &HTTPError{StatusCode: 422, Code: "chain_sort_unsupported", Detail: "Ingress chain view only supports created_at asc|desc."}
	}
	params.SortOrder = sortOrder
	cohortHash, err := chainCohortFingerprint(params)
	if err != nil {
		return ChainResponse{}, fmt.Errorf("fingerprint ingress-chain cohort: %w", err)
	}
	if params.RowCursor != nil && strings.TrimSpace(*params.RowCursor) != "" &&
		params.ChainCursor != nil && strings.TrimSpace(*params.ChainCursor) != "" {
		return ChainResponse{}, &HTTPError{StatusCode: 422, Code: "chain_row_cursor_conflict", Detail: "chain_cursor and row_cursor cannot be used together."}
	}
	var rowCursor *rowCursorPayload
	if params.RowCursor != nil && strings.TrimSpace(*params.RowCursor) != "" {
		decoded, err := decodeRowCursor(strings.TrimSpace(*params.RowCursor))
		if err != nil {
			return ChainResponse{}, &HTTPError{StatusCode: 400, Code: "row_cursor_invalid", Detail: "Retained-row cursor is invalid."}
		}
		exactIngress := ""
		if params.IngressRequestID != nil {
			exactIngress = strings.TrimSpace(*params.IngressRequestID)
		}
		if exactIngress == "" || decoded.ProfileID != params.ProfileID || decoded.IngressID != exactIngress || decoded.Limit != params.ChainRowLimit || decoded.CohortHash != cohortHash {
			return ChainResponse{}, &HTTPError{StatusCode: 422, Code: "row_cursor_scope_mismatch", Detail: "Retained-row cursor does not match the exact ingress query scope."}
		}
		if decoded.RetentionEpoch != requestEpoch {
			return ChainResponse{}, &HTTPError{StatusCode: 410, Code: "request_snapshot_revoked", Detail: "The retained-row snapshot was revoked; reload the first page."}
		}
		if decoded.RetentionGeneration != requestGeneration {
			return ChainResponse{}, &HTTPError{StatusCode: 410, Code: "request_snapshot_stale", Detail: "The retained-row snapshot is stale; reload the first page."}
		}
		if err := applyChainCursorWindow(&params, decoded.WindowFrom, decoded.WindowTo); err != nil {
			return ChainResponse{}, &HTTPError{StatusCode: 400, Code: "row_cursor_invalid", Detail: "Retained-row cursor window is invalid."}
		}
		rowCursor = &decoded
	}
	if params.ClientRuleID != nil {
		rule, found, ruleErr := loadCompiledUserAgentRuleByID(ctx, exec, params.ProfileID, *params.ClientRuleID)
		if ruleErr != nil {
			return ChainResponse{}, ruleErr
		}
		if !found {
			return ChainResponse{}, &HTTPError{StatusCode: 400, Code: "invalid_client_rule_id", Detail: "invalid client_rule_id"}
		}
		params.ClientRulePattern = &rule.RawPattern
	}
	if params.Q != nil {
		trimmed := strings.TrimSpace(*params.Q)
		if trimmed == "" {
			params.Q = nil
		} else {
			params.Q = &trimmed
		}
	}

	// Resolve the chain cursor and freeze the outer order bound.
	var cursor chainCursorPayload
	var hasCursor bool
	if params.ChainCursor != nil && strings.TrimSpace(*params.ChainCursor) != "" {
		decoded, err := decodeChainCursor(*params.ChainCursor)
		if err != nil {
			return ChainResponse{}, &HTTPError{StatusCode: 400, Code: "chain_cursor_invalid", Detail: "Ingress chain cursor is invalid."}
		}
		if decoded.ProfileID != params.ProfileID || decoded.Limit != params.ChainLimit || decoded.SortOrder != sortOrder || decoded.CohortHash != cohortHash {
			return ChainResponse{}, &HTTPError{StatusCode: 422, Code: "chain_cursor_scope_mismatch", Detail: "Ingress chain cursor does not match the query scope."}
		}
		if decoded.RetentionEpoch != requestEpoch {
			return ChainResponse{}, &HTTPError{StatusCode: 410, Code: "request_snapshot_revoked", Detail: "The ingress-chain snapshot was revoked; reload the first page."}
		}
		if decoded.RetentionGeneration != requestGeneration {
			return ChainResponse{}, &HTTPError{StatusCode: 410, Code: "request_snapshot_stale", Detail: "The ingress-chain snapshot is stale; reload the first page."}
		}
		if err := applyChainCursorWindow(&params, decoded.WindowFrom, decoded.WindowTo); err != nil {
			return ChainResponse{}, &HTTPError{StatusCode: 400, Code: "chain_cursor_invalid", Detail: "Ingress-chain cursor window is invalid."}
		}
		cursor = decoded
		hasCursor = true
	}

	// Build the ingest set from usage events with retained coverage.
	ingresses, err := selectChainIngressSet(ctx, exec, params, cursor, hasCursor, sortOrder)
	if err != nil {
		return ChainResponse{}, err
	}
	hasMore := len(ingresses) > params.ChainLimit
	if hasMore {
		ingresses = ingresses[:params.ChainLimit]
	}

	connectionCatalog, err := loadCurrentConnections(ctx, exec, params.ProfileID)
	if err != nil {
		return ChainResponse{}, err
	}
	currentEndpoints, _, err := loadCurrentEndpoints(ctx, exec, params.ProfileID)
	if err != nil {
		return ChainResponse{}, err
	}
	currentModels, _, err := loadRequestLogModels(ctx, exec, params.ProfileID)
	if err != nil {
		return ChainResponse{}, err
	}
	rules, err := loadCompiledUserAgentRules(ctx, exec, params.ProfileID)
	if err != nil {
		return ChainResponse{}, err
	}

	items := make([]ChainIngressItem, 0, len(ingresses))
	pageUpstreamAttempts := 0
	pageRequestLogRows := 0
	if len(ingresses) > 0 {
		var loaded []ChainIngressItem
		if rowCursor != nil {
			// Exact row-cursor continuation keeps the precise single-chain
			// path: per-ingress loaders plus the signed keyset page bound.
			item, itemErr := loadExactChainIngressItem(ctx, exec, params, ingresses[0], params.ChainRowLimit, rowCursor, requestEpoch, requestGeneration, connectionCatalog)
			if itemErr != nil {
				return ChainResponse{}, itemErr
			}
			loaded = []ChainIngressItem{item}
		} else {
			// The default page loads finalized summaries, full-chain counts,
			// and first retained-row pages as three batched statements.
			batch, batchErr := loadChainIngressItemsBatch(ctx, exec, params, ingresses, params.ChainRowLimit, requestEpoch, requestGeneration, connectionCatalog)
			if batchErr != nil {
				return ChainResponse{}, batchErr
			}
			loaded = batch
		}
		for _, item := range loaded {
			pageUpstreamAttempts += item.RetainedUpstreamAttemptCount
			pageRequestLogRows += item.RetainedRequestLogRowCount
			items = append(items, item)
		}
	}

	response := ChainResponse{
		View:                     "ingress_chains",
		RetainedIngressTotal:     len(ingresses),
		PageIngressCount:         len(items),
		PageUpstreamAttemptCount: pageUpstreamAttempts,
		PageRequestLogRowCount:   pageRequestLogRows,
		FilterOptions: RequestLogListFilterOptions{
			Endpoints:            buildRequestLogEndpointOptions(currentEndpoints, params.EndpointID),
			Models:               buildRequestLogModelOptions(currentModels, params.ModelID),
			ResolvedTargetModels: buildRequestLogResolvedTargetModelOptions(currentModels, params.ResolvedTargetModelID),
			Clients:              buildRequestLogClientOptions(rules),
		},
		Items:              items,
		HasMoreChains:      hasMore,
		OrderEvidenceState: "authoritative",
		Caliber:            CaliberForScope(ScopeIngress),
		Samples:            ScopeSampleCounts{ObservationCount: len(items)},
	}
	if params.FromTime != nil && params.ToTime != nil {
		coverage := Coverage{
			RequestedPreset: params.CoveragePreset, FromTime: params.FromTime.UTC(), ToTime: params.ToTime.UTC(),
			RetentionFromTime: requestSource.PublishedFloor, Source: "raw",
			Complete: requestActual.Complete && requestActual.Freshness == "fresh", Gaps: []CoverageGap{},
			RetentionEpoch: requestSource.RetentionEpoch, RetentionGeneration: requestSource.RetentionGeneration,
			PurgeState: requestSource.PurgeState, SourceRevision: requestSource.SourceRevision,
		}
		response.DatasetCoverage = DatasetCoverage{RequestLogs: &coverage}
	}
	if hasMore && len(items) > 0 {
		last := ingresses[len(ingresses)-1]
		windowFrom, windowTo := chainCursorWindow(params)
		encoded, err := encodeChainCursor(chainCursorPayload{
			Version:             1,
			ProfileID:           params.ProfileID,
			OrderAt:             last.OrderAt.UTC().Format(time.RFC3339Nano),
			IngressID:           last.IngressRequestID,
			UsageEventID:        last.UsageEventID,
			Limit:               params.ChainLimit,
			SortOrder:           sortOrder,
			CohortHash:          cohortHash,
			WindowFrom:          windowFrom,
			WindowTo:            windowTo,
			RetentionEpoch:      requestEpoch,
			RetentionGeneration: requestGeneration,
		})
		if err != nil {
			return ChainResponse{}, err
		}
		response.NextChainCursor = &encoded
	}
	if err := populateChainCoverage(ctx, exec, params, referenceNow, requestSource, requestActual, &response); err != nil {
		return ChainResponse{}, err
	}
	// Full-cohort totals.
	if err := fillChainTotals(ctx, exec, params, &response); err != nil {
		return ChainResponse{}, err
	}
	return response, nil
}

type chainIngressRef struct {
	IngressRequestID string
	UsageEventID     int64
	OrderAt          time.Time
}

// selectChainIngressSet resolves the ordered ingress set. With finalized
// cohort selectors the authoritative finalized usage events are the ingress
// set (Requests SPEC §6.4); in ordinary mode the set comes from the retained
// request logs themselves so chains without finalized usage evidence still
// appear with finalized_summary=null / finalized_evidence_state=unavailable
// instead of being silently dropped.
func selectChainIngressSet(ctx context.Context, exec queryExecutor, params ChainQueryParams, cursor chainCursorPayload, hasCursor bool, sortOrder string) ([]chainIngressRef, error) {
	// Finalized-cohort selectors resolve through the authoritative usage
	// summary; they are never translated into retained-row facts.
	if !usesFinalizedChainCohort(params) {
		return selectOrdinaryChainIngressSet(ctx, exec, params, cursor, hasCursor, sortOrder)
	}
	// Use the finalized usage events as the authoritative ingress set; rows
	// without usage evidence appear only when they are explicitly selected by
	// an exact ingress ID.
	query := `SELECT ingress_request_id, id, created_at FROM usage_request_events
		WHERE profile_id = $1
		AND EXISTS (SELECT 1 FROM request_logs retained_rows
			WHERE retained_rows.profile_id = usage_request_events.profile_id
			AND retained_rows.ingress_request_id = usage_request_events.ingress_request_id)`
	queryArgs := []any{params.ProfileID}
	nextArg := func() int { return len(queryArgs) + 1 }
	appendArg := func(value any) int {
		index := nextArg()
		queryArgs = append(queryArgs, value)
		return index
	}
	if params.IngressRequestID != nil && strings.TrimSpace(*params.IngressRequestID) != "" {
		query = fmt.Sprintf("%s AND ingress_request_id = $%d", query, appendArg(strings.TrimSpace(*params.IngressRequestID)))
	}
	if params.Q != nil && strings.TrimSpace(*params.Q) != "" {
		query = fmt.Sprintf("%s AND ingress_request_id ILIKE $%d", query, appendArg("%"+strings.TrimSpace(*params.Q)+"%"))
	}
	if params.ProxyAPIKeyID != nil {
		query = fmt.Sprintf("%s AND proxy_api_key_id_snapshot = $%d", query, appendArg(*params.ProxyAPIKeyID))
	}
	if params.FromTime != nil {
		query = fmt.Sprintf("%s AND created_at >= $%d", query, appendArg(params.FromTime.UTC()))
	}
	if params.ToTime != nil {
		query = fmt.Sprintf("%s AND created_at < $%d", query, appendArg(params.ToTime.UTC()))
	}
	if params.IngressFinalResult != nil {
		// Shared finalized classifier (Observe SPEC §3.2): final_result is
		// derived, never a stored column.
		classifier := `CASE WHEN status_code NOT BETWEEN 200 AND 299 THEN 'failed'
			WHEN stream_outcome = 'client_disconnected' THEN 'client_disconnected'
			WHEN stream_outcome IN ('provider_incomplete','upstream_read_error','gateway_timeout','upstream_ended_without_terminal','unknown') THEN 'failed'
			ELSE 'completed' END`
		query = fmt.Sprintf("%s AND %s = $%d", query, classifier, appendArg(*params.IngressFinalResult))
	}
	if params.ConfirmedFailover != nil {
		query = fmt.Sprintf("%s AND failover_occurred = $%d", query, appendArg(*params.ConfirmedFailover))
	}
	if params.FinalTargetModelID != nil && strings.TrimSpace(*params.FinalTargetModelID) != "" {
		query = fmt.Sprintf("%s AND resolved_target_model_id = $%d", query, appendArg(strings.TrimSpace(*params.FinalTargetModelID)))
	}
	if params.PricingStatus != nil {
		query = fmt.Sprintf("%s AND pricing_status = $%d", query, appendArg(*params.PricingStatus))
	}
	if len(params.UnpricedReasons) > 0 {
		placeholders := make([]string, 0, len(params.UnpricedReasons))
		for _, reason := range params.UnpricedReasons {
			placeholders = append(placeholders, fmt.Sprintf("$%d", appendArg(reason)))
		}
		query += " AND unpriced_reason IN (" + strings.Join(placeholders, ",") + ")"
	}
	if params.ReportingCurrencyEpoch != nil && strings.TrimSpace(*params.ReportingCurrencyEpoch) != "" {
		if *params.ReportingCurrencyEpoch == "__legacy_unknown__" {
			query += " AND reporting_currency_epoch IS NULL"
		} else {
			query = fmt.Sprintf("%s AND reporting_currency_epoch = $%d", query, appendArg(*params.ReportingCurrencyEpoch))
		}
	}
	if params.IsStream != nil {
		query = fmt.Sprintf("%s AND is_stream = $%d", query, appendArg(*params.IsStream))
	}
	if len(params.StreamOutcomes) > 0 {
		placeholders := make([]string, 0, len(params.StreamOutcomes))
		for _, outcome := range params.StreamOutcomes {
			placeholders = append(placeholders, fmt.Sprintf("$%d", appendArg(outcome)))
		}
		query += " AND stream_outcome IN (" + strings.Join(placeholders, ",") + ")"
	}
	if len(params.UpstreamStatusCodes) > 0 {
		// Upstream status is a retained-row selector, not a finalized usage
		// fact. It is applied below through the ingress-level row EXISTS.
	}
	if params.IngressFinalStatusCodes != nil && len(params.IngressFinalStatusCodes) > 0 {
		placeholders := make([]string, 0, len(params.IngressFinalStatusCodes))
		for _, code := range params.IngressFinalStatusCodes {
			placeholders = append(placeholders, fmt.Sprintf("$%d", appendArg(code)))
		}
		query += " AND status_code IN (" + strings.Join(placeholders, ",") + ")"
	}
	if params.CostSegmentKey != nil && strings.TrimSpace(*params.CostSegmentKey) != "" {
		segment := strings.TrimSpace(*params.CostSegmentKey)
		query = fmt.Sprintf("%s AND %s = $%d", query, canonicalCostSegmentKeySQLFor(""), appendArg(segment))
	}
	if hasChainRowFilter(params) {
		query = appendChainRowCohortExists(query, &queryArgs, params, "usage_request_events")
	}
	// Keyset continuation.
	if hasCursor {
		queryArgs = append(queryArgs, cursor.OrderAt, cursor.IngressID)
		if sortOrder == "desc" {
			query += fmt.Sprintf(" AND (created_at, ingress_request_id) < ($%d, $%d)", len(queryArgs)-1, len(queryArgs))
		} else {
			query += fmt.Sprintf(" AND (created_at, ingress_request_id) > ($%d, $%d)", len(queryArgs)-1, len(queryArgs))
		}
	}
	queryArgs = append(queryArgs, params.ChainLimit+1)
	query += fmt.Sprintf(" ORDER BY created_at %s, ingress_request_id %s LIMIT $%d", strings.ToUpper(sortOrder), strings.ToUpper(sortOrder), len(queryArgs))

	rows, err := exec.Query(ctx, query, queryArgs...)
	if err != nil {
		return nil, fmt.Errorf("query chain ingress set for profile %d: %w", params.ProfileID, err)
	}
	defer rows.Close()
	ingresses := make([]chainIngressRef, 0)
	for rows.Next() {
		var ref chainIngressRef
		if err := rows.Scan(&ref.IngressRequestID, &ref.UsageEventID, &ref.OrderAt); err != nil {
			return nil, fmt.Errorf("scan chain ingress set: %w", err)
		}
		ref.OrderAt = ref.OrderAt.UTC()
		ingresses = append(ingresses, ref)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate chain ingress set: %w", err)
	}
	return ingresses, nil
}

// selectOrdinaryChainIngressSet resolves the ordinary-mode ingress set from
// the retained request logs themselves (no finalized cohort selectors). The
// usage-event owner is never consulted here: the finalized summary loader is
// the only consumer of usage facts, and it resolves them per page. Rows with
// a NULL ingress_request_id are diagnostic orphans and never form a chain.
func selectOrdinaryChainIngressSet(ctx context.Context, exec queryExecutor, params ChainQueryParams, cursor chainCursorPayload, hasCursor bool, sortOrder string) ([]chainIngressRef, error) {
	query := `SELECT rl.ingress_request_id,
		MIN(rl.created_at) AS first_at
		FROM request_logs rl
		WHERE rl.profile_id = $1 AND rl.ingress_request_id IS NOT NULL`
	queryArgs := []any{params.ProfileID}
	havingClause := ""
	nextArg := func() int { return len(queryArgs) + 1 }
	appendArg := func(value any) int {
		index := nextArg()
		queryArgs = append(queryArgs, value)
		return index
	}
	if params.IngressRequestID != nil && strings.TrimSpace(*params.IngressRequestID) != "" {
		query = fmt.Sprintf("%s AND rl.ingress_request_id = $%d", query, appendArg(strings.TrimSpace(*params.IngressRequestID)))
	}
	if params.Q != nil && strings.TrimSpace(*params.Q) != "" {
		query = fmt.Sprintf("%s AND rl.ingress_request_id ILIKE $%d", query, appendArg("%"+strings.TrimSpace(*params.Q)+"%"))
	}
	if params.ProxyAPIKeyID != nil {
		query = fmt.Sprintf("%s AND EXISTS (SELECT 1 FROM request_logs key_rows WHERE key_rows.profile_id = rl.profile_id AND key_rows.ingress_request_id = rl.ingress_request_id AND key_rows.proxy_api_key_id_snapshot = $%d)", query, appendArg(*params.ProxyAPIKeyID))
	}
	if params.FromTime != nil {
		query = fmt.Sprintf("%s AND rl.created_at >= $%d", query, appendArg(params.FromTime.UTC()))
	}
	if params.ToTime != nil {
		query = fmt.Sprintf("%s AND rl.created_at < $%d", query, appendArg(params.ToTime.UTC()))
	}
	if hasChainRowFilter(params) {
		query = appendChainRowCohortExists(query, &queryArgs, params, "rl")
	}
	if params.CostSegmentKey != nil && strings.TrimSpace(*params.CostSegmentKey) != "" {
		query = appendChainFinalizedCohortExists(query, &queryArgs, params, "rl")
	}
	if hasCursor {
		queryArgs = append(queryArgs, cursor.OrderAt, cursor.IngressID)
		if sortOrder == "desc" {
			havingClause = fmt.Sprintf(" HAVING MIN(rl.created_at) < $%d OR (MIN(rl.created_at) = $%d AND rl.ingress_request_id < $%d)", len(queryArgs)-1, len(queryArgs)-1, len(queryArgs))
		} else {
			havingClause = fmt.Sprintf(" HAVING MIN(rl.created_at) > $%d OR (MIN(rl.created_at) = $%d AND rl.ingress_request_id > $%d)", len(queryArgs)-1, len(queryArgs)-1, len(queryArgs))
		}
	}
	queryArgs = append(queryArgs, params.ChainLimit+1)
	query += fmt.Sprintf(" GROUP BY rl.ingress_request_id%s ORDER BY first_at %s, rl.ingress_request_id %s LIMIT $%d", havingClause, strings.ToUpper(sortOrder), strings.ToUpper(sortOrder), len(queryArgs))

	rows, err := exec.Query(ctx, query, queryArgs...)
	if err != nil {
		return nil, fmt.Errorf("query ordinary chain ingress set for profile %d: %w", params.ProfileID, err)
	}
	defer rows.Close()
	ingresses := make([]chainIngressRef, 0)
	for rows.Next() {
		var ref chainIngressRef
		if err := rows.Scan(&ref.IngressRequestID, &ref.OrderAt); err != nil {
			return nil, fmt.Errorf("scan ordinary chain ingress set: %w", err)
		}
		ref.OrderAt = ref.OrderAt.UTC()
		ingresses = append(ingresses, ref)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ordinary chain ingress set: %w", err)
	}
	return ingresses, nil
}

// loadExactChainIngressItem loads one chain item through the precise
// single-chain path: per-ingress summary/counts plus the signed row-cursor
// keyset page. Only this path may consume a row cursor.
func loadExactChainIngressItem(
	ctx context.Context,
	exec queryExecutor,
	params ChainQueryParams,
	ingress chainIngressRef,
	rowLimit int,
	rowCursor *rowCursorPayload,
	retentionEpoch int64,
	retentionGeneration int64,
	connectionCatalog map[int]connectionRecord,
) (ChainIngressItem, error) {
	ids := []string{ingress.IngressRequestID}
	summaries, err := loadFinalizedSummaries(ctx, exec, params.ProfileID, ids)
	if err != nil {
		return ChainIngressItem{}, err
	}
	countsByIngress, err := loadRetainedRowCountsBatch(ctx, exec, params, ids)
	if err != nil {
		return ChainIngressItem{}, err
	}
	page, err := loadRetainedRows(ctx, exec, params, ingress.IngressRequestID, rowLimit, rowCursor, connectionCatalog)
	if err != nil {
		return ChainIngressItem{}, err
	}
	counts := countsByIngress[ingress.IngressRequestID]
	return assembleChainIngressItem(params, ingress, summaries[ingress.IngressRequestID], counts, page, rowLimit, retentionEpoch, retentionGeneration)
}

// loadChainIngressItemsBatch loads a whole outer page with three batched
// statements: finalized summaries, full-chain retained counts, and first
// retained-row pages. Public item fields are assembled by the same routine
// as the exact single-chain path.
func loadChainIngressItemsBatch(
	ctx context.Context,
	exec queryExecutor,
	params ChainQueryParams,
	ingresses []chainIngressRef,
	rowLimit int,
	retentionEpoch int64,
	retentionGeneration int64,
	connectionCatalog map[int]connectionRecord,
) ([]ChainIngressItem, error) {
	ids := make([]string, 0, len(ingresses))
	for _, ingress := range ingresses {
		ids = append(ids, ingress.IngressRequestID)
	}
	summaries, err := loadFinalizedSummaries(ctx, exec, params.ProfileID, ids)
	if err != nil {
		return nil, err
	}
	countsByIngress, err := loadRetainedRowCountsBatch(ctx, exec, params, ids)
	if err != nil {
		return nil, err
	}
	pages, err := loadRetainedRowsBatch(ctx, exec, params, ids, rowLimit, connectionCatalog)
	if err != nil {
		return nil, err
	}
	items := make([]ChainIngressItem, 0, len(ingresses))
	for _, ingress := range ingresses {
		item, err := assembleChainIngressItem(params, ingress, summaries[ingress.IngressRequestID], countsByIngress[ingress.IngressRequestID], pages[ingress.IngressRequestID], rowLimit, retentionEpoch, retentionGeneration)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

// assembleChainIngressItem builds the public chain item from the loaded
// summary, full-ingress counts, and bounded retained-row page.
func assembleChainIngressItem(
	params ChainQueryParams,
	ingress chainIngressRef,
	summary *FinalizedSummary,
	counts retainedRowCounts,
	page retainedRowsPage,
	rowLimit int,
	retentionEpoch int64,
	retentionGeneration int64,
) (ChainIngressItem, error) {
	item := ChainIngressItem{
		IngressRequestID:       ingress.IngressRequestID,
		ElapsedEvidenceState:   "authoritative",
		FinalizedEvidenceState: "authoritative",
	}
	if summary != nil {
		item.FinalizedSummary = summary
		if summary.IngressStartedAt != nil && summary.IngressCompletedAt != nil {
			item.StartedAt = summary.IngressStartedAt
			item.CompletedAt = summary.IngressCompletedAt
			elapsed := summary.IngressCompletedAt.Sub(*summary.IngressStartedAt).Milliseconds()
			item.ElapsedMS = &elapsed
		} else {
			item.ElapsedEvidenceState = "unavailable"
		}
	} else {
		item.FinalizedEvidenceState = "unavailable"
		item.ElapsedEvidenceState = "unavailable"
		item.OrderEvidenceState = "retained_row_fallback"
	}
	item.RetainedRows = page.Rows
	item.RetainedRowsLoadedCount = len(page.Rows)
	item.RetainedRowsPageComplete = !page.HasMore
	item.RetainedRowCount = counts.Total
	item.RetainedUpstreamAttemptCount = counts.Upstream
	item.RetainedRequestLogRowCount = counts.Total
	item.LegacyUnknownRowCount = counts.Legacy
	item.MatchedRowCount = counts.Matched
	if page.HasMore && len(page.Rows) > 0 {
		last := page.Rows[len(page.Rows)-1]
		cohortHash, err := chainCohortFingerprint(params)
		if err != nil {
			return ChainIngressItem{}, fmt.Errorf("fingerprint retained-row cohort: %w", err)
		}
		windowFrom, windowTo := chainCursorWindow(params)
		encoded, err := encodeRowCursor(rowCursorPayload{
			Version:             1,
			ProfileID:           params.ProfileID,
			IngressID:           ingress.IngressRequestID,
			OrderAt:             last.CreatedAt.UTC().Format(time.RFC3339Nano),
			RequestLogID:        last.RequestLogID,
			Limit:               rowLimit,
			CohortHash:          cohortHash,
			WindowFrom:          windowFrom,
			WindowTo:            windowTo,
			RetentionEpoch:      retentionEpoch,
			RetentionGeneration: retentionGeneration,
		})
		if err != nil {
			return ChainIngressItem{}, err
		}
		item.NextRowCursor = &encoded
	}
	if summary != nil {
		expectedAttempts := summary.AttemptCount
		item.ExpectedAttemptCount = &expectedAttempts
		item.ExpectedRequestLogRowCount = summary.ExpectedRequestLogRowCount
		complete := expectedAttempts == counts.Upstream
		if summary.ExpectedRequestLogRowCount != nil {
			complete = complete && *summary.ExpectedRequestLogRowCount == counts.Total
		}
		item.ChainComplete = boolPtr(complete)
		item.SameTargetRetryOccurred = summary.SameTargetRetryOccurred
		item.HedgeOccurred = summary.HedgeOccurred
		item.FailoverOccurred = summary.FailoverOccurred
		item.RoutingEvidenceComplete = summary.RoutingEvidenceComplete
	}
	return item, nil
}

// fillChainTotals computes the full-cohort retained totals for the response.
func fillChainTotals(ctx context.Context, exec queryExecutor, params ChainQueryParams, response *ChainResponse) error {
	var retainedIngressTotal int
	var upstreamAttemptTotal int
	var requestLogRowTotal int
	var legacyUnknownTotal int
	// Build one row per retained ingress first, then aggregate those rows into
	// the exact public totals. This preserves the historical COUNT(DISTINCT)
	// semantics while allowing PostgreSQL to use a hash aggregate instead of
	// sorting the whole window for DISTINCT.
	whereClause, queryArgs := buildChainCohortWhere(params, "request_logs")
	innerQuery := `SELECT
			request_logs.ingress_request_id,
			COUNT(*) FILTER (WHERE request_logs.row_kind = 'upstream') AS upstream_rows,
			COUNT(*) AS total_rows,
			COUNT(*) FILTER (WHERE request_logs.row_kind = 'legacy_unknown') AS legacy_rows
		FROM request_logs
		WHERE ` + whereClause
	innerQuery += " GROUP BY request_logs.ingress_request_id"
	query := `SELECT
			COUNT(*) AS ingresses,
			COALESCE(SUM(upstream_rows), 0) AS upstream_rows,
			COALESCE(SUM(total_rows), 0) AS total_rows,
			COALESCE(SUM(legacy_rows), 0) AS legacy_rows
		FROM (` + innerQuery + `) AS per_ingress`
	if err := exec.QueryRow(ctx, query, queryArgs...).Scan(&retainedIngressTotal, &upstreamAttemptTotal, &requestLogRowTotal, &legacyUnknownTotal); err != nil {
		return fmt.Errorf("query retained chain totals: %w", err)
	}
	response.RetainedIngressTotal = retainedIngressTotal
	response.RetainedUpstreamAttemptTotal = upstreamAttemptTotal
	response.RetainedRequestLogRowTotal = requestLogRowTotal
	response.LegacyUnknownRowTotal = legacyUnknownTotal
	return nil
}

func buildChainCohortWhere(params ChainQueryParams, outerAlias string) (string, []any) {
	clauses := []string{
		outerAlias + ".profile_id = $1",
		outerAlias + ".ingress_request_id IS NOT NULL",
	}
	args := []any{params.ProfileID}
	if params.ProxyAPIKeyID != nil {
		args = append(args, *params.ProxyAPIKeyID)
		clauses = append(clauses, fmt.Sprintf("EXISTS (SELECT 1 FROM request_logs key_rows WHERE key_rows.profile_id = %s.profile_id AND key_rows.ingress_request_id = %s.ingress_request_id AND key_rows.proxy_api_key_id_snapshot = $%d)", outerAlias, outerAlias, len(args)))
	}
	if params.IngressRequestID != nil && strings.TrimSpace(*params.IngressRequestID) != "" {
		args = append(args, strings.TrimSpace(*params.IngressRequestID))
		clauses = append(clauses, fmt.Sprintf("%s.ingress_request_id = $%d", outerAlias, len(args)))
	}
	if params.Q != nil && strings.TrimSpace(*params.Q) != "" {
		args = append(args, "%"+strings.TrimSpace(*params.Q)+"%")
		clauses = append(clauses, fmt.Sprintf("%s.ingress_request_id ILIKE $%d", outerAlias, len(args)))
	}
	query := strings.Join(clauses, " AND ")
	if hasChainRowFilter(params) {
		query = appendChainRowCohortExists(query, &args, params, outerAlias)
	} else if !usesFinalizedChainCohort(params) {
		query = appendChainWindowCohortExists(query, &args, params, outerAlias)
	}
	if usesFinalizedChainCohort(params) {
		query = appendChainFinalizedCohortExists(query, &args, params, outerAlias)
	}
	return query, args
}

func usesFinalizedChainCohort(params ChainQueryParams) bool {
	return params.IngressFinalResult != nil || params.ConfirmedFailover != nil ||
		params.PricingStatus != nil || len(params.UnpricedReasons) > 0 ||
		params.ReportingCurrencyEpoch != nil || params.IsStream != nil ||
		len(params.IngressFinalStatusCodes) > 0 || params.FinalTargetModelID != nil ||
		params.CostSegmentKey != nil
}

func boolPtr(value bool) *bool { return &value }
