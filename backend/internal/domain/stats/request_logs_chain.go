package stats

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
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
	EndpointID              *int
	TerminalTargetID        *int
	StatusFamily            *string
	StatusCode              *int
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
	RequestedModel                    *ModelRef    `json:"requested_model"`
	ResolvedModel                     *ModelRef    `json:"resolved_model"`
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
	ModelID                           string    `json:"model_id"`
	ResolvedTargetModelID             *string   `json:"resolved_target_model_id"`
	EndpointID                        *int      `json:"endpoint_id"`
	TerminalTargetID                  *int      `json:"terminal_target_id"`
	TerminalTargetLabel               *string   `json:"terminal_target_label"`
	TerminalTargetConfigured          bool      `json:"terminal_target_configured"`
	TerminalTargetOwnerModelID        *string   `json:"terminal_target_owner_model_id"`
	TotalTokens                       *int      `json:"total_tokens"`
	TotalCostUserCurrencyMicros       *int64    `json:"total_cost_user_currency_micros"`
	PricingStatus                     string    `json:"pricing_status"`
	UnpricedReason                    *string   `json:"unpriced_reason"`
	PricingEvidenceTrust              string    `json:"pricing_evidence_trust"`
	CreatedAt                         time.Time `json:"created_at"`
	IsCurrent                         bool      `json:"is_current,omitempty"`
}

// ChainResponse is the outer-page envelope.
type ChainResponse struct {
	// View is the fixed view discriminator of the ingress-chain envelope.
	View                         string             `json:"view"`
	QueryContext                 *string            `json:"query_context"`
	SourceIngressTotal           *int               `json:"source_ingress_total"`
	RetainedIngressTotal         int                `json:"retained_ingress_total"`
	RetainedUpstreamAttemptTotal int                `json:"retained_upstream_attempt_total"`
	RetainedRequestLogRowTotal   int                `json:"retained_request_log_row_total"`
	LegacyUnknownRowTotal        int                `json:"legacy_unknown_row_total"`
	PageIngressCount             int                `json:"page_ingress_count"`
	PageUpstreamAttemptCount     int                `json:"page_upstream_attempt_count"`
	PageRequestLogRowCount       int                `json:"page_request_log_row_count"`
	Items                        []ChainIngressItem `json:"items"`
	HasMoreChains                bool               `json:"has_more_chains"`
	NextChainCursor              *string            `json:"next_chain_cursor"`
	SourceCoverage               *json.RawMessage   `json:"source_coverage"`
	RawFinalizedCoverage         *json.RawMessage   `json:"raw_finalized_coverage"`
	AttemptCoverage              *json.RawMessage   `json:"attempt_coverage"`
	DrilldownCoverage            *json.RawMessage   `json:"drilldown_coverage"`
	OrderEvidenceState           string             `json:"order_evidence_state,omitempty"`
}

const defaultChainLimit = 20
const maxChainLimit = 50
const defaultChainRowLimit = 50
const maxChainRowLimit = 200

// chainCursorKey signs chain/row cursors with a fixed local key.
const chainCursorKey = "prism-chain-cursor-v1"

type chainCursorPayload struct {
	Version             int    `json:"v"`
	ProfileID           int    `json:"p"`
	OrderAt             string `json:"o"`
	IngressID           string `json:"i"`
	UsageEventID        int64  `json:"u"`
	Limit               int    `json:"l"`
	SortOrder           string `json:"s"`
	RetentionEpoch      int64  `json:"r"`
	RetentionGeneration int64  `json:"g"`
}

type rowCursorPayload struct {
	Version             int    `json:"v"`
	ProfileID           int    `json:"p"`
	IngressID           string `json:"i"`
	OrderAt             string `json:"o"`
	RequestLogID        string `json:"id"`
	Limit               int    `json:"l"`
	RetentionEpoch      int64  `json:"r"`
	RetentionGeneration int64  `json:"g"`
}

// encodeChainCursor signs and encodes an outer chain cursor.
func encodeChainCursor(payload chainCursorPayload) (string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	signature := signChainCursor(raw)
	return base64.RawURLEncoding.EncodeToString(raw) + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func decodeChainCursor(encoded string) (chainCursorPayload, error) {
	parts := strings.Split(encoded, ".")
	if len(parts) != 2 {
		return chainCursorPayload{}, fmt.Errorf("invalid chain cursor")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return chainCursorPayload{}, err
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return chainCursorPayload{}, err
	}
	if !hmac.Equal(signature, signChainCursor(raw)) {
		return chainCursorPayload{}, fmt.Errorf("invalid chain cursor signature")
	}
	var payload chainCursorPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return chainCursorPayload{}, err
	}
	if payload.Version != 1 {
		return chainCursorPayload{}, fmt.Errorf("unsupported chain cursor version")
	}
	return payload, nil
}

func signChainCursor(raw []byte) []byte {
	mac := hmac.New(sha256.New, []byte(chainCursorKey))
	_, _ = mac.Write(raw)
	return mac.Sum(nil)
}

func encodeRowCursor(payload rowCursorPayload) (string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	signature := signRowCursor(raw)
	return base64.RawURLEncoding.EncodeToString(raw) + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func decodeRowCursor(encoded string) (rowCursorPayload, error) {
	parts := strings.Split(encoded, ".")
	if len(parts) != 2 {
		return rowCursorPayload{}, fmt.Errorf("invalid row cursor")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return rowCursorPayload{}, err
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return rowCursorPayload{}, err
	}
	if !hmac.Equal(signature, signRowCursor(raw)) {
		return rowCursorPayload{}, fmt.Errorf("invalid row cursor signature")
	}
	var payload rowCursorPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return rowCursorPayload{}, err
	}
	if payload.Version != 1 {
		return rowCursorPayload{}, fmt.Errorf("unsupported row cursor version")
	}
	if strings.TrimSpace(payload.IngressID) == "" || payload.ProfileID <= 0 || payload.Limit <= 0 {
		return rowCursorPayload{}, fmt.Errorf("invalid row cursor scope")
	}
	if _, err := time.Parse(time.RFC3339Nano, payload.OrderAt); err != nil {
		return rowCursorPayload{}, fmt.Errorf("invalid row cursor timestamp: %w", err)
	}
	requestLogID, err := strconv.ParseInt(payload.RequestLogID, 10, 64)
	if err != nil || requestLogID <= 0 {
		return rowCursorPayload{}, fmt.Errorf("invalid row cursor request log id")
	}
	return payload, nil
}

func signRowCursor(raw []byte) []byte {
	mac := hmac.New(sha256.New, []byte("prism-row-cursor-v1"))
	_, _ = mac.Write(raw)
	return mac.Sum(nil)
}

// ListIngressChains returns one outer page of ingresses with bounded retained
// rows per ingress. Row-scoped filters select the ingress set first; the
// outer keyset cursor never splits an ingress across pages.
func ListIngressChains(ctx context.Context, exec queryExecutor, params ChainQueryParams) (ChainResponse, error) {
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
	params, err = resolveChainQueryBounds(ctx, exec, params, referenceNow, requestSource)
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
		if exactIngress == "" || decoded.ProfileID != params.ProfileID || decoded.IngressID != exactIngress || decoded.Limit != params.ChainRowLimit {
			return ChainResponse{}, &HTTPError{StatusCode: 422, Code: "row_cursor_scope_mismatch", Detail: "Retained-row cursor does not match the exact ingress query scope."}
		}
		if decoded.RetentionEpoch != requestEpoch {
			return ChainResponse{}, &HTTPError{StatusCode: 410, Code: "request_snapshot_revoked", Detail: "The retained-row snapshot was revoked; reload the first page."}
		}
		if decoded.RetentionGeneration != requestGeneration {
			return ChainResponse{}, &HTTPError{StatusCode: 410, Code: "request_snapshot_stale", Detail: "The retained-row snapshot is stale; reload the first page."}
		}
		rowCursor = &decoded
	}
	sortOrder := strings.ToLower(strings.TrimSpace(params.SortOrder))
	if sortOrder == "" {
		sortOrder = "desc"
	}
	if sortOrder != "desc" && sortOrder != "asc" {
		return ChainResponse{}, &HTTPError{StatusCode: 422, Code: "chain_sort_unsupported", Detail: "Ingress chain view only supports created_at asc|desc."}
	}

	// Resolve the chain cursor and freeze the outer order bound.
	var cursor chainCursorPayload
	var hasCursor bool
	if params.ChainCursor != nil && strings.TrimSpace(*params.ChainCursor) != "" {
		decoded, err := decodeChainCursor(*params.ChainCursor)
		if err != nil {
			return ChainResponse{}, &HTTPError{StatusCode: 400, Code: "chain_cursor_invalid", Detail: "Ingress chain cursor is invalid."}
		}
		if decoded.ProfileID != params.ProfileID || decoded.Limit != params.ChainLimit || decoded.SortOrder != sortOrder {
			return ChainResponse{}, &HTTPError{StatusCode: 422, Code: "chain_cursor_scope_mismatch", Detail: "Ingress chain cursor does not match the query scope."}
		}
		if decoded.RetentionEpoch != requestEpoch {
			return ChainResponse{}, &HTTPError{StatusCode: 410, Code: "request_snapshot_revoked", Detail: "The ingress-chain snapshot was revoked; reload the first page."}
		}
		if decoded.RetentionGeneration != requestGeneration {
			return ChainResponse{}, &HTTPError{StatusCode: 410, Code: "request_snapshot_stale", Detail: "The ingress-chain snapshot is stale; reload the first page."}
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

	items := make([]ChainIngressItem, 0, len(ingresses))
	pageUpstreamAttempts := 0
	pageRequestLogRows := 0
	for _, ingress := range ingresses {
		item, err := loadChainIngressItem(ctx, exec, params, ingress, params.ChainRowLimit, rowCursor, requestEpoch, requestGeneration, connectionCatalog)
		if err != nil {
			return ChainResponse{}, err
		}
		pageUpstreamAttempts += item.RetainedUpstreamAttemptCount
		pageRequestLogRows += item.RetainedRequestLogRowCount
		items = append(items, item)
	}

	response := ChainResponse{
		View:                     "ingress_chains",
		RetainedIngressTotal:     len(ingresses),
		PageIngressCount:         len(items),
		PageUpstreamAttemptCount: pageUpstreamAttempts,
		PageRequestLogRowCount:   pageRequestLogRows,
		Items:                    items,
		HasMoreChains:            hasMore,
		OrderEvidenceState:       "authoritative",
	}
	if hasMore && len(items) > 0 {
		last := ingresses[len(ingresses)-1]
		encoded, err := encodeChainCursor(chainCursorPayload{
			Version:             1,
			ProfileID:           params.ProfileID,
			OrderAt:             last.OrderAt.UTC().Format(time.RFC3339Nano),
			IngressID:           last.IngressRequestID,
			UsageEventID:        last.UsageEventID,
			Limit:               params.ChainLimit,
			SortOrder:           sortOrder,
			RetentionEpoch:      requestEpoch,
			RetentionGeneration: requestGeneration,
		})
		if err != nil {
			return ChainResponse{}, err
		}
		response.NextChainCursor = &encoded
	}
	if err := populateChainCoverage(ctx, exec, params, referenceNow, requestSource, &response); err != nil {
		return ChainResponse{}, err
	}
	// Full-cohort totals.
	if err := fillChainTotals(ctx, exec, params, &response); err != nil {
		return ChainResponse{}, err
	}
	return response, nil
}

// ResolveChainQueryBounds is the shared Requests/export entry point for the
// owner-resolved window. The export path must consume the same actual bounds
// as the interactive chain list rather than reintroducing a floor-only clip.
func ResolveChainQueryBounds(ctx context.Context, exec queryExecutor, params ChainQueryParams, referenceNow time.Time) (ChainQueryParams, error) {
	source, err := LoadRetentionSourceProjection(ctx, exec, "request_logs", referenceNow.UTC())
	if err != nil {
		return ChainQueryParams{}, err
	}
	if source.PurgeState == "running" || source.PurgeState == "recovery_required" {
		return ChainQueryParams{}, &HTTPError{StatusCode: 503, Code: "request_log_purge_in_progress", Detail: "request logs are temporarily unavailable while retention cleanup is publishing"}
	}
	return resolveChainQueryBounds(ctx, exec, params, referenceNow.UTC(), source)
}

func resolveChainQueryBounds(ctx context.Context, exec queryExecutor, params ChainQueryParams, referenceNow time.Time, source RetentionFloorEpochSource) (ChainQueryParams, error) {
	actual, err := LoadActualCoverageProjection(ctx, exec, source)
	if err != nil {
		return ChainQueryParams{}, err
	}
	preset, fromTime, toTime, err := normalizeActualCoveragePreset(params.CoveragePreset, params.FromTime, params.ToTime, referenceNow)
	if err != nil {
		return ChainQueryParams{}, err
	}
	bounds, err := ResolveQueryBoundsFromActualCoverage(preset, fromTime, toTime, referenceNow, source, actual)
	if err != nil {
		return ChainQueryParams{}, err
	}
	params.CoveragePreset = bounds.RequestedPreset
	params.CoverageRequestedFrom = bounds.RequestedFrom
	params.CoverageRequestedTo = bounds.RequestedTo
	from := bounds.UsageFrom.UTC()
	to := bounds.UsageTo.UTC()
	params.FromTime = &from
	params.ToTime = &to
	return params, nil
}

type chainIngressRef struct {
	IngressRequestID string
	UsageEventID     int64
	OrderAt          time.Time
}

func buildChainIngressWhere(params ChainQueryParams) (string, []any) {
	clauses := []string{"profile_id = $1"}
	args := []any{params.ProfileID}
	add := func(value any, clause string) {
		args = append(args, value)
		clauses = append(clauses, fmt.Sprintf(clause, len(args)))
	}
	if params.IngressRequestID != nil && strings.TrimSpace(*params.IngressRequestID) != "" {
		add(strings.TrimSpace(*params.IngressRequestID), "ingress_request_id = $%d")
	}
	if params.FromTime != nil {
		add(params.FromTime.UTC(), "created_at >= $%d")
	}
	if params.ToTime != nil {
		add(params.ToTime.UTC(), "created_at < $%d")
	}
	if params.IngressFinalResult != nil {
		add(*params.IngressFinalResult, `CASE WHEN status_code NOT BETWEEN 200 AND 299 THEN 'failed'
			WHEN stream_outcome = 'client_disconnected' THEN 'client_disconnected'
			WHEN stream_outcome IN ('provider_incomplete','upstream_read_error','gateway_timeout','upstream_ended_without_terminal','unknown') THEN 'failed'
			ELSE 'completed' END = $%d`)
	}
	if params.ConfirmedFailover != nil {
		add(*params.ConfirmedFailover, "failover_occurred = $%d")
	}
	if params.PricingStatus != nil {
		add(*params.PricingStatus, "pricing_status = $%d")
	}
	if len(params.UnpricedReasons) > 0 {
		placeholders := make([]string, 0, len(params.UnpricedReasons))
		for _, reason := range params.UnpricedReasons {
			args = append(args, reason)
			placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
		}
		clauses = append(clauses, "unpriced_reason IN ("+strings.Join(placeholders, ",")+")")
	}
	if params.ReportingCurrencyEpoch != nil && strings.TrimSpace(*params.ReportingCurrencyEpoch) != "" {
		if *params.ReportingCurrencyEpoch == "__legacy_unknown__" {
			clauses = append(clauses, "reporting_currency_epoch IS NULL")
		} else {
			add(*params.ReportingCurrencyEpoch, "reporting_currency_epoch = $%d")
		}
	}
	if params.IsStream != nil {
		add(*params.IsStream, "is_stream = $%d")
	}
	if len(params.StreamOutcomes) > 0 {
		placeholders := make([]string, 0, len(params.StreamOutcomes))
		for _, outcome := range params.StreamOutcomes {
			args = append(args, outcome)
			placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
		}
		clauses = append(clauses, "stream_outcome IN ("+strings.Join(placeholders, ",")+")")
	}
	if len(params.UpstreamStatusCodes) > 0 {
		placeholders := make([]string, 0, len(params.UpstreamStatusCodes))
		for _, code := range params.UpstreamStatusCodes {
			args = append(args, code)
			placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
		}
		clauses = append(clauses, "upstream_status_code IN ("+strings.Join(placeholders, ",")+")")
	}
	return strings.Join(clauses, " AND "), args
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
	useUsageSource := params.IngressFinalResult != nil || params.ConfirmedFailover != nil ||
		params.PricingStatus != nil || len(params.UnpricedReasons) > 0 ||
		params.ReportingCurrencyEpoch != nil || params.IsStream != nil ||
		len(params.IngressFinalStatusCodes) > 0 || params.CostSegmentKey != nil
	if !useUsageSource {
		return selectOrdinaryChainIngressSet(ctx, exec, params, cursor, hasCursor, sortOrder)
	}
	// Use the finalized usage events as the authoritative ingress set; rows
	// without usage evidence appear only when they are explicitly selected by
	// an exact ingress ID.
	query := `SELECT ingress_request_id, id, created_at FROM usage_request_events
		WHERE profile_id = $1`
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
		if strings.HasPrefix(segment, "e.") {
			query = fmt.Sprintf("%s AND ('e.' || COALESCE(reporting_currency_epoch::text, '')) = $%d", query, appendArg(segment))
		} else if strings.HasPrefix(segment, "l.") {
			query = fmt.Sprintf("%s AND ('l.' || COALESCE(report_currency_code, '__unknown__')) = $%d", query, appendArg(segment))
		}
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
// usage event id is resolved per ingress for the finalized summary join and
// is NULL when no finalized evidence exists.
func selectOrdinaryChainIngressSet(ctx context.Context, exec queryExecutor, params ChainQueryParams, cursor chainCursorPayload, hasCursor bool, sortOrder string) ([]chainIngressRef, error) {
	query := `SELECT rl.ingress_request_id,
		ue.id AS usage_event_id,
		MIN(rl.created_at) AS first_at
		FROM request_logs rl
		LEFT JOIN LATERAL (SELECT ue.id FROM usage_request_events ue
			WHERE ue.profile_id = rl.profile_id AND ue.ingress_request_id = rl.ingress_request_id
			ORDER BY ue.id ASC LIMIT 1) ue ON TRUE
		WHERE rl.profile_id = $1`
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
	if hasCursor {
		queryArgs = append(queryArgs, cursor.OrderAt, cursor.IngressID)
		if sortOrder == "desc" {
			havingClause = fmt.Sprintf(" HAVING MIN(rl.created_at) < $%d OR (MIN(rl.created_at) = $%d AND rl.ingress_request_id < $%d)", len(queryArgs)-1, len(queryArgs)-1, len(queryArgs))
		} else {
			havingClause = fmt.Sprintf(" HAVING MIN(rl.created_at) > $%d OR (MIN(rl.created_at) = $%d AND rl.ingress_request_id > $%d)", len(queryArgs)-1, len(queryArgs)-1, len(queryArgs))
		}
	}
	queryArgs = append(queryArgs, params.ChainLimit+1)
	query += fmt.Sprintf(" GROUP BY rl.ingress_request_id, ue.id%s ORDER BY first_at %s, rl.ingress_request_id %s LIMIT $%d", havingClause, strings.ToUpper(sortOrder), strings.ToUpper(sortOrder), len(queryArgs))

	rows, err := exec.Query(ctx, query, queryArgs...)
	if err != nil {
		return nil, fmt.Errorf("query ordinary chain ingress set for profile %d: %w", params.ProfileID, err)
	}
	defer rows.Close()
	ingresses := make([]chainIngressRef, 0)
	for rows.Next() {
		var ref chainIngressRef
		var usageEventID sql.NullInt64
		if err := rows.Scan(&ref.IngressRequestID, &usageEventID, &ref.OrderAt); err != nil {
			return nil, fmt.Errorf("scan ordinary chain ingress set: %w", err)
		}
		if usageEventID.Valid {
			ref.UsageEventID = usageEventID.Int64
		}
		ref.OrderAt = ref.OrderAt.UTC()
		ingresses = append(ingresses, ref)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ordinary chain ingress set: %w", err)
	}
	return ingresses, nil
}

func hasChainRowFilter(params ChainQueryParams) bool {
	return params.ModelID != nil || params.ResolvedTargetModelID != nil || params.EndpointID != nil ||
		params.TerminalTargetID != nil || params.StatusFamily != nil || params.StatusCode != nil ||
		params.ErrorText != nil || len(params.StreamOutcomes) > 0 || len(params.StreamErrorKinds) > 0 ||
		len(params.UpstreamStatusCodes) > 0 || len(params.GatewayStatusCodes) > 0 ||
		len(params.LegacyStatusCodes) > 0 || params.RowResult != nil || params.ClientRulePattern != nil
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

// buildRowResultExists returns a parameterized EXISTS clause that selects
// ingresses containing a retained request-log row with the given result.
func buildRowResultExists(rowResult string, profileID int, startArg int) string {
	statusExpr := scopedRequestLogStatusSQL
	inner := fmt.Sprintf(`SELECT 1 FROM request_logs rl WHERE rl.profile_id = $%d AND rl.ingress_request_id = usage_request_events.ingress_request_id`, startArg)
	switch rowResult {
	case "failed":
		inner += " AND (" + statusExpr + " IS NOT NULL AND NOT (" + statusExpr + " BETWEEN 200 AND 299))"
	case "client_disconnected":
		inner += " AND rl.attempt_result = 'client_disconnected'"
	case "cancelled":
		inner += " AND rl.attempt_result = 'cancelled'"
	default:
		inner += " AND FALSE"
	}
	return inner
}

type retainedRowCounts struct {
	Upstream int
	Total    int
	Legacy   int
	Matched  int
}

type retainedRowsPage struct {
	Rows    []ChainRowItem
	HasMore bool
}

// loadChainIngressItem loads one chain item: finalized summary, full-ingress
// retained counts, and one bounded retained-row page.
func loadChainIngressItem(
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
	item := ChainIngressItem{
		IngressRequestID:       ingress.IngressRequestID,
		ElapsedEvidenceState:   "authoritative",
		FinalizedEvidenceState: "authoritative",
	}
	summary, found, err := loadFinalizedSummary(ctx, exec, params.ProfileID, ingress.IngressRequestID)
	if err != nil {
		return ChainIngressItem{}, err
	}
	if found {
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

	counts, err := loadRetainedRowCounts(ctx, exec, params, ingress.IngressRequestID)
	if err != nil {
		return ChainIngressItem{}, err
	}
	page, err := loadRetainedRows(ctx, exec, params, ingress.IngressRequestID, rowLimit, rowCursor, connectionCatalog)
	if err != nil {
		return ChainIngressItem{}, err
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
		encoded, err := encodeRowCursor(rowCursorPayload{
			Version:             1,
			ProfileID:           params.ProfileID,
			IngressID:           ingress.IngressRequestID,
			OrderAt:             last.CreatedAt.UTC().Format(time.RFC3339Nano),
			RequestLogID:        last.RequestLogID,
			Limit:               rowLimit,
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

// loadFinalizedSummary loads the finalized usage projection for an ingress.
func loadFinalizedSummary(ctx context.Context, exec queryExecutor, profileID int, ingressRequestID string) (*FinalizedSummary, bool, error) {
	var summary FinalizedSummary
	var ingressStartedAt, ingressCompletedAt, effectiveAt *time.Time
	var resolvedModelID, ownerModelID *string
	var terminalTargetID *int
	var terminalTargetName *string
	var configured sql.NullBool
	var endpointID *int
	var endpointName *string
	var pricingStatus string
	var trust string
	var unpricedReason, resolutionKind *string
	var missingComponents []string
	var requestLogID sql.NullInt64
	err := exec.QueryRow(ctx, `SELECT
			final_request_log.id,
			usage_request_events.model_id,
			usage_request_events.resolved_target_model_id,
			usage_request_events.status_code,
			usage_request_events.success_flag,
			usage_request_events.stream_outcome,
			usage_request_events.endpoint_id,
			usage_request_events.endpoint_label_snapshot,
			usage_request_events.connection_id,
			connections.name,
			connections.is_active,
			owner_model_configs.model_id,
			usage_request_events.ttft_ms,
			usage_request_events.output_tokens,
			usage_request_events.completion_duration_ms,
			usage_request_events.total_tokens,
			usage_request_events.total_cost_user_currency_micros,
			usage_request_events.report_currency_code,
			usage_request_events.report_currency_symbol,
			usage_request_events.reporting_currency_epoch,
			usage_request_events.currency_attribution,
			usage_request_events.pricing_status,
			usage_request_events.unpriced_reason,
			usage_request_events.pricing_resolution_kind,
			usage_request_events.missing_price_components,
			usage_request_events.pricing_evidence_trust,
			usage_request_events.final_error_code,
			usage_request_events.final_attempt_number,
			usage_request_events.final_attempt_trigger,
			usage_request_events.final_target_entry_trigger,
			usage_request_events.same_target_retry_occurred,
			usage_request_events.hedge_occurred,
			usage_request_events.failover_occurred,
			usage_request_events.routing_evidence_complete,
			usage_request_events.attempt_count,
			usage_request_events.expected_request_log_row_count,
			usage_request_events.ingress_started_at,
			usage_request_events.ingress_completed_at,
			usage_request_events.pricing_template_id_used,
			usage_request_events.pricing_template_name_snapshot,
			usage_request_events.pricing_template_revision_id_used,
			usage_request_events.pricing_config_version_used,
			usage_request_events.pricing_version_effective_at,
			usage_request_events.pricing_snapshot_unit,
			usage_request_events.pricing_snapshot_input,
			usage_request_events.pricing_snapshot_output,
			usage_request_events.pricing_snapshot_cache_read_input,
			usage_request_events.pricing_snapshot_cache_creation_input,
			usage_request_events.pricing_snapshot_reasoning
		FROM usage_request_events
		LEFT JOIN LATERAL (
			SELECT request_logs.id
			FROM request_logs
			WHERE request_logs.profile_id = usage_request_events.profile_id
			  AND request_logs.ingress_request_id = usage_request_events.ingress_request_id
			ORDER BY
				(usage_request_events.final_attempt_number IS NOT NULL
					AND request_logs.attempt_number = usage_request_events.final_attempt_number) DESC,
				(request_logs.is_winner IS TRUE) DESC,
				request_logs.created_at DESC,
				request_logs.id DESC
			LIMIT 1
		) AS final_request_log ON TRUE
		LEFT JOIN connections ON connections.id = usage_request_events.connection_id
		LEFT JOIN LATERAL (
			SELECT model_configs.model_id
			FROM model_access_targets
			JOIN model_configs ON model_configs.id = model_access_targets.source_model_config_id
			WHERE model_access_targets.profile_id = usage_request_events.profile_id
			  AND model_access_targets.target_connection_id = usage_request_events.connection_id
			ORDER BY model_access_targets.position ASC, model_access_targets.id ASC
			LIMIT 1
		) AS owner_model_configs ON TRUE
		WHERE usage_request_events.profile_id = $1 AND usage_request_events.ingress_request_id = $2
		ORDER BY usage_request_events.id DESC LIMIT 1`,
		profileID, ingressRequestID).Scan(
		&requestLogID,
		&summary.RequestedModelID,
		&resolvedModelID,
		&summary.FinalStatusCode,
		&summary.SuccessFlag,
		&summary.StreamOutcome,
		&endpointID,
		&endpointName,
		&terminalTargetID,
		&terminalTargetName,
		&configured,
		&ownerModelID,
		&summary.TTFTMS,
		&summary.OutputTokens,
		&summary.CompletionDurationMS,
		&summary.TotalTokens,
		&summary.TotalCostUserCurrencyMicros,
		&summary.ReportCurrencyCode,
		&summary.ReportCurrencySymbol,
		&summary.ReportingCurrencyEpoch,
		&summary.CurrencyAttribution,
		&pricingStatus,
		&unpricedReason,
		&resolutionKind,
		&missingComponents,
		&trust,
		&summary.FinalErrorCode,
		&summary.FinalAttemptNumber,
		&summary.FinalAttemptTrigger,
		&summary.FinalTargetEntryTrigger,
		&summary.SameTargetRetryOccurred,
		&summary.HedgeOccurred,
		&summary.FailoverOccurred,
		&summary.RoutingEvidenceComplete,
		&summary.AttemptCount,
		&summary.ExpectedRequestLogRowCount,
		&ingressStartedAt,
		&ingressCompletedAt,
		&summary.PricingTemplateIDUsed,
		&summary.PricingTemplateNameSnapshot,
		&summary.PricingTemplateRevisionIDUsed,
		&summary.PricingConfigVersionUsed,
		&effectiveAt,
		&summary.PricingSnapshotUnit,
		&summary.PricingSnapshotInput,
		&summary.PricingSnapshotOutput,
		&summary.PricingSnapshotCacheReadInput,
		&summary.PricingSnapshotCacheCreationInput,
		&summary.PricingSnapshotReasoning,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("load finalized summary for ingress %s: %w", ingressRequestID, err)
	}
	if requestLogID.Valid {
		summary.RequestLogID = stringPointer(strconv.FormatInt(requestLogID.Int64, 10))
	}
	summary.RequestedModel = &ModelRef{ID: summary.RequestedModelID, Label: summary.RequestedModelID}
	if resolvedModelID != nil {
		summary.ResolvedModel = &ModelRef{ID: *resolvedModelID, Label: *resolvedModelID}
	}
	if terminalTargetID != nil {
		label := fmt.Sprintf("Terminal Target #%d", *terminalTargetID)
		if terminalTargetName != nil && strings.TrimSpace(*terminalTargetName) != "" {
			label = strings.TrimSpace(*terminalTargetName)
		}
		summary.TerminalTarget = &TargetRef{ID: *terminalTargetID, Label: label, Configured: configured.Valid && configured.Bool, OwnerModelID: ownerModelID}
	}
	if endpointID != nil {
		label := fmt.Sprintf("Endpoint %d", *endpointID)
		if endpointName != nil && strings.TrimSpace(*endpointName) != "" {
			label = strings.TrimSpace(*endpointName)
		}
		summary.Endpoint = &EndpointRef{ID: *endpointID, Label: label}
	}
	summary.FinalResult = deriveFinalResult(summary.FinalStatusCode, summary.SuccessFlag, summary.StreamOutcome)
	summary.FinalPricingStatus = pricingStatus
	summary.FinalUnpricedReason = unpricedReason
	summary.FinalPricingResolutionKind = resolutionKind
	summary.MissingPriceComponents = missingComponents
	summary.FinalPricingEvidenceTrust = trust
	summary.IngressStartedAt = utcTimePointer(ingressStartedAt)
	summary.IngressCompletedAt = utcTimePointer(ingressCompletedAt)
	summary.PricingVersionEffectiveAt = utcTimePointer(effectiveAt)
	if summary.OutputTokens != nil && summary.TTFTMS != nil && summary.CompletionDurationMS != nil && *summary.CompletionDurationMS-*summary.TTFTMS > 0 {
		rate := float64(*summary.OutputTokens) * 1000 / float64(*summary.CompletionDurationMS-*summary.TTFTMS)
		summary.OutputRateTPS = &rate
	}
	if summary.ReportingCurrencyEpoch != nil && *summary.ReportingCurrencyEpoch > 0 {
		key := fmt.Sprintf("e.%d", *summary.ReportingCurrencyEpoch)
		summary.CostSegmentKey = &key
	} else if summary.ReportCurrencyCode != nil {
		key := "l." + strings.ToUpper(*summary.ReportCurrencyCode)
		summary.CostSegmentKey = &key
	} else {
		key := "l.__unknown__"
		summary.CostSegmentKey = &key
	}
	return &summary, true, nil
}

func deriveFinalResult(statusCode int, successFlag bool, streamOutcome string) string {
	if statusCode < 200 || statusCode > 299 {
		return "failed"
	}
	switch streamOutcome {
	case "client_disconnected":
		return "client_disconnected"
	case "provider_incomplete", "upstream_read_error", "gateway_timeout", "upstream_ended_without_terminal", "unknown":
		return "failed"
	default:
		return "completed"
	}
}

func loadRetainedRowCounts(ctx context.Context, exec queryExecutor, params ChainQueryParams, ingressRequestID string) (retainedRowCounts, error) {
	var counts retainedRowCounts
	queryArgs := []any{params.ProfileID, ingressRequestID}
	matchPredicate := buildChainRowMatchPredicate(&queryArgs, params, "request_logs")
	err := exec.QueryRow(ctx, `SELECT
			COUNT(*) FILTER (WHERE row_kind = 'upstream'),
			COUNT(*),
			COUNT(*) FILTER (WHERE row_kind = 'legacy_unknown'),
			COUNT(*) FILTER (WHERE `+matchPredicate+`)
		FROM request_logs
		WHERE profile_id = $1 AND ingress_request_id = $2`, queryArgs...).Scan(
		&counts.Upstream,
		&counts.Total,
		&counts.Legacy,
		&counts.Matched,
	)
	if err != nil {
		return retainedRowCounts{}, fmt.Errorf("query retained row counts for ingress %s: %w", ingressRequestID, err)
	}
	return counts, nil
}

// loadRetainedRows loads one bounded retained-row page for one ingress. The
// limit+1 sentinel determines page completeness without deriving full-chain
// counts from the bounded page.
func loadRetainedRows(ctx context.Context, exec queryExecutor, params ChainQueryParams, ingressRequestID string, rowLimit int, cursor *rowCursorPayload, connectionCatalog map[int]connectionRecord) (retainedRowsPage, error) {
	profileID := params.ProfileID
	queryArgs := []any{profileID, ingressRequestID}
	matchPredicate := buildChainRowMatchPredicate(&queryArgs, params, "request_logs")
	query := `SELECT
			id, row_kind, ingress_request_id, attempt_number, attempt_trigger, attempt_result, is_winner,
			attempt_duration_ms, legacy_duration_ms, upstream_status_code, gateway_status_code, legacy_status_code,
			error_source, error_code, failure_stage, error_detail, error_detail_redacted, error_detail_truncated,
			stream_error_detail, stream_error_detail_redacted, stream_error_detail_truncated,
			stream_outcome, stream_error_kind, model_id, resolved_target_model_id, endpoint_id, connection_id,
			total_tokens, total_cost_user_currency_micros, pricing_status, unpriced_reason, pricing_evidence_trust, created_at,
			endpoint_base_url, endpoint_description,
			(` + matchPredicate + `) AS matched_by_filter
		FROM request_logs
		WHERE profile_id = $1 AND ingress_request_id = $2`
	if cursor != nil {
		orderAt, err := time.Parse(time.RFC3339Nano, cursor.OrderAt)
		if err != nil {
			return retainedRowsPage{}, fmt.Errorf("parse retained-row cursor timestamp: %w", err)
		}
		requestLogID, err := strconv.ParseInt(cursor.RequestLogID, 10, 64)
		if err != nil {
			return retainedRowsPage{}, fmt.Errorf("parse retained-row cursor request log id: %w", err)
		}
		orderAtArg := len(queryArgs) + 1
		requestLogIDArg := len(queryArgs) + 2
		query += fmt.Sprintf(" AND (created_at, id) > ($%d, $%d)", orderAtArg, requestLogIDArg)
		queryArgs = append(queryArgs, orderAt.UTC(), requestLogID)
	}
	queryArgs = append(queryArgs, rowLimit+1)
	query += fmt.Sprintf(" ORDER BY created_at ASC, id ASC LIMIT $%d", len(queryArgs))
	rows, err := exec.Query(ctx, query, queryArgs...)
	if err != nil {
		return retainedRowsPage{}, fmt.Errorf("query retained rows for ingress %s: %w", ingressRequestID, err)
	}
	defer rows.Close()
	items := make([]ChainRowItem, 0, rowLimit+1)
	for rows.Next() {
		var item ChainRowItem
		var requestLogID int64
		var errorDetail, streamErrorDetail *string
		var errorDetailRedacted, errorDetailTruncated, streamErrorDetailRedacted, streamErrorDetailTruncated bool
		var endpointBaseURL, endpointDescription *string
		if err := rows.Scan(
			&requestLogID, &item.RowKind, &item.IngressRequestID, &item.AttemptNumber, &item.AttemptTrigger, &item.AttemptResult, &item.IsWinner,
			&item.AttemptDurationMS, &item.LegacyDurationMS, &item.UpstreamStatusCode, &item.GatewayStatusCode, &item.LegacyStatusCode,
			&item.ErrorSource, &item.ErrorCode, &item.FailureStage, &errorDetail, &errorDetailRedacted, &errorDetailTruncated,
			&streamErrorDetail, &streamErrorDetailRedacted, &streamErrorDetailTruncated,
			&item.StreamOutcome, &item.StreamErrorKind, &item.ModelID, &item.ResolvedTargetModelID, &item.EndpointID, &item.TerminalTargetID,
			&item.TotalTokens, &item.TotalCostUserCurrencyMicros, &item.PricingStatus, &item.UnpricedReason, &item.PricingEvidenceTrust, &item.CreatedAt,
			&endpointBaseURL, &endpointDescription, &item.MatchedByFilter,
		); err != nil {
			return retainedRowsPage{}, fmt.Errorf("scan retained row: %w", err)
		}
		item.RequestLogID = strconv.FormatInt(requestLogID, 10)
		item.CreatedAt = item.CreatedAt.UTC()
		// Unified failure projection: error_detail wins; stream detail is
		// used for 2xx abnormal streams and client disconnects.
		detail := errorDetail
		source := "error_detail"
		redacted := errorDetailRedacted
		truncated := errorDetailTruncated
		if detail == nil && streamErrorDetail != nil {
			detail = streamErrorDetail
			source = "stream_error_detail"
			redacted = streamErrorDetailRedacted
			truncated = streamErrorDetailTruncated
		}
		if detail != nil {
			preview, previewTruncated := truncateCodePoints(*detail, 240)
			item.FailureDetailPreview = &preview
			item.FailureDetailPreviewTruncated = previewTruncated
			item.FailureDetailRedacted = redacted
			item.FailureDetailPersistenceTruncated = truncated
		}
		item.FailureDetailSource = source
		// Terminal target projection from the current connection catalog (no
		// historical snapshot; label follows renames).
		if item.TerminalTargetID != nil {
			target := resolveTerminalTargetProjection(connectionCatalog, item.TerminalTargetID)
			item.TerminalTargetLabel = target.Label
			item.TerminalTargetConfigured = target.Configured
			item.TerminalTargetOwnerModelID = target.OwnerModelID
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return retainedRowsPage{}, fmt.Errorf("iterate retained rows: %w", err)
	}
	hasMore := len(items) > rowLimit
	if hasMore {
		items = items[:rowLimit]
	}
	return retainedRowsPage{Rows: items, HasMore: hasMore}, nil
}

func truncateCodePoints(value string, limit int) (string, bool) {
	runes := []rune(value)
	if len(runes) <= limit {
		return value, false
	}
	return string(runes[:limit]), true
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
		switch {
		case strings.HasPrefix(segment, "e."):
			add(segment, "('e.' || COALESCE(final_rows.reporting_currency_epoch::text, '')) = $%d")
		case strings.HasPrefix(segment, "l."):
			add(segment, "('l.' || COALESCE(final_rows.report_currency_code, '__unknown__')) = $%d")
		}
	}
	return query + " AND EXISTS (SELECT 1 FROM usage_request_events final_rows WHERE " + strings.Join(clauses, " AND ") + ")"
}

// fillChainTotals computes the full-cohort retained totals for the response.
func fillChainTotals(ctx context.Context, exec queryExecutor, params ChainQueryParams, response *ChainResponse) error {
	var retainedIngressTotal int
	var upstreamAttemptTotal int
	var requestLogRowTotal int
	var legacyUnknownTotal int
	// Full retained cohort across the filtered ingress set.
	query := `SELECT
			COUNT(DISTINCT request_logs.ingress_request_id) AS ingresses,
			COUNT(*) FILTER (WHERE request_logs.row_kind = 'upstream') AS upstream_rows,
			COUNT(*) AS total_rows,
			COUNT(*) FILTER (WHERE request_logs.row_kind = 'legacy_unknown') AS legacy_rows
		FROM request_logs
		WHERE request_logs.profile_id = $1 AND request_logs.ingress_request_id IS NOT NULL`
	queryArgs := []any{params.ProfileID}
	if params.FromTime != nil {
		queryArgs = append(queryArgs, params.FromTime.UTC())
		query += fmt.Sprintf(" AND request_logs.created_at >= $%d", len(queryArgs))
	}
	if params.ToTime != nil {
		queryArgs = append(queryArgs, params.ToTime.UTC())
		query += fmt.Sprintf(" AND request_logs.created_at < $%d", len(queryArgs))
	}
	if params.ProxyAPIKeyID != nil {
		queryArgs = append(queryArgs, *params.ProxyAPIKeyID)
		query += fmt.Sprintf(" AND EXISTS (SELECT 1 FROM request_logs key_rows WHERE key_rows.profile_id = request_logs.profile_id AND key_rows.ingress_request_id = request_logs.ingress_request_id AND key_rows.proxy_api_key_id_snapshot = $%d)", len(queryArgs))
	}
	if params.IngressRequestID != nil && strings.TrimSpace(*params.IngressRequestID) != "" {
		queryArgs = append(queryArgs, strings.TrimSpace(*params.IngressRequestID))
		query += fmt.Sprintf(" AND request_logs.ingress_request_id = $%d", len(queryArgs))
	}
	if params.Q != nil && strings.TrimSpace(*params.Q) != "" {
		queryArgs = append(queryArgs, "%"+strings.TrimSpace(*params.Q)+"%")
		query += fmt.Sprintf(" AND request_logs.ingress_request_id ILIKE $%d", len(queryArgs))
	}
	if hasChainRowFilter(params) {
		query = appendChainRowCohortExists(query, &queryArgs, params, "request_logs")
	}
	if params.IngressFinalResult != nil || params.ConfirmedFailover != nil || params.PricingStatus != nil ||
		len(params.UnpricedReasons) > 0 || params.ReportingCurrencyEpoch != nil || params.IsStream != nil ||
		len(params.IngressFinalStatusCodes) > 0 || params.CostSegmentKey != nil {
		query = appendChainFinalizedCohortExists(query, &queryArgs, params, "request_logs")
	}
	if err := exec.QueryRow(ctx, query, queryArgs...).Scan(&retainedIngressTotal, &upstreamAttemptTotal, &requestLogRowTotal, &legacyUnknownTotal); err != nil {
		return fmt.Errorf("query retained chain totals: %w", err)
	}
	response.RetainedIngressTotal = retainedIngressTotal
	response.RetainedUpstreamAttemptTotal = upstreamAttemptTotal
	response.RetainedRequestLogRowTotal = requestLogRowTotal
	response.LegacyUnknownRowTotal = legacyUnknownTotal
	return nil
}

// populateChainCoverage keeps the chain envelope on the same owner projections
// as ordinary Requests and Observe. The JSON fields are intentionally raw so
// the domain-specific coverage contracts can evolve without a second chain
// policy or a browser-computed range.
func populateChainCoverage(ctx context.Context, exec queryExecutor, params ChainQueryParams, now time.Time, requestSource RetentionFloorEpochSource, response *ChainResponse) error {
	usageSource, err := LoadRetentionSourceProjection(ctx, exec, "usage_request_events", now)
	if err != nil {
		return err
	}
	requestCoverage, err := chainCoverageProjection(ctx, exec, params, now, requestSource, "request_logs")
	if err != nil {
		return err
	}
	usageCoverage, err := chainCoverageProjection(ctx, exec, params, now, usageSource, "usage_request_events")
	if err != nil {
		return err
	}
	response.SourceCoverage = &requestCoverage
	response.AttemptCoverage = &requestCoverage
	response.DrilldownCoverage = &requestCoverage
	response.RawFinalizedCoverage = &usageCoverage
	return nil
}

func chainCoverageProjection(ctx context.Context, exec queryExecutor, params ChainQueryParams, now time.Time, source RetentionFloorEpochSource, domain string) (json.RawMessage, error) {
	actual, err := LoadActualCoverageProjection(ctx, exec, source)
	if err != nil {
		return nil, err
	}
	preset, fromTime, toTime, err := normalizeActualCoveragePreset(params.CoveragePreset, params.CoverageRequestedFrom, params.CoverageRequestedTo, now)
	if err != nil {
		return nil, err
	}
	bounds, err := ResolveQueryBoundsFromActualCoverage(preset, fromTime, toTime, now, source, actual)
	if err != nil {
		return nil, err
	}
	requestedFrom := bounds.RequestedFrom
	if requestedFrom == nil {
		resolved := bounds.UsageFrom.UTC()
		requestedFrom = &resolved
	}
	requestedTo := bounds.RequestedTo
	if requestedTo == nil {
		resolved := bounds.UsageTo.UTC()
		requestedTo = &resolved
	}
	gaps := make([]map[string]any, 0, len(bounds.Gaps))
	for _, gap := range bounds.Gaps {
		gaps = append(gaps, map[string]any{
			"from_time": gap.FromTime.UTC().Format(time.RFC3339),
			"to_time":   gap.ToTime.UTC().Format(time.RFC3339),
			"reason":    gap.Reason,
		})
	}
	state := "known"
	if !bounds.Complete || actual.Freshness != "fresh" {
		state = "legacy_unknown"
	}
	payload := map[string]any{
		"domain":               domain,
		"requested_from_time":  requestedFrom.UTC().Format(time.RFC3339),
		"requested_to_time":    requestedTo.UTC().Format(time.RFC3339),
		"effective_from_time":  bounds.UsageFrom.UTC().Format(time.RFC3339),
		"effective_to_time":    bounds.UsageTo.UTC().Format(time.RFC3339),
		"retention_from_time":  formatChainTime(bounds.UsageRetentionFrom),
		"complete":             bounds.Complete,
		"gaps":                 gaps,
		"state":                state,
		"source_revision":      source.SourceRevision,
		"retention_epoch":      source.RetentionEpoch,
		"retention_generation": source.RetentionGeneration,
		"purge_state":          source.PurgeState,
		"actual_coverage": map[string]any{
			"from_time":           formatChainTime(actual.Earliest),
			"to_time":             formatChainTime(actual.Latest),
			"source":              actual.Source,
			"precision":           actual.Precision,
			"complete":            actual.Complete,
			"freshness":           actual.Freshness,
			"gap_reason":          actual.GapReason,
			"coverage_revision":   actual.Revision,
			"coverage_hash":       actual.Hash,
			"generated_at":        formatChainTime(actual.GeneratedAt),
			"materialization_cut": actual.MaterializationCut,
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal %s chain coverage: %w", domain, err)
	}
	return json.RawMessage(raw), nil
}

func formatChainTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC().Format(time.RFC3339)
}

func utcTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	normalized := value.UTC()
	return &normalized
}

func boolPtr(value bool) *bool { return &value }
