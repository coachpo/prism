package audit

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	statsdomain "github.com/coachpo/prism/backend/internal/domain/stats"
)

type HTTPError struct {
	StatusCode int
	Detail     string
	Code       string
	Details    map[string]any
}

func (err *HTTPError) Error() string {
	return err.Detail
}

type queryExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type ListParams struct {
	ProfileID     int
	RequestLogID  *int
	ModelID       *string
	StatusCode    *int
	EndpointID    *int
	ConnectionID  *int
	FromTime      *time.Time
	ToTime        *time.Time
	Limit         int
	Cursor        string
	Sort          string
	AnchorAuditID *int
	ReferenceNow  time.Time
}

type ListWindow struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

type listCursor struct {
	Version             int       `json:"v"`
	LastCreatedAt       time.Time `json:"last_created_at"`
	LastID              int       `json:"last_id"`
	From                time.Time `json:"from"`
	To                  time.Time `json:"to"`
	Limit               int       `json:"limit"`
	Sort                string    `json:"sort"`
	FiltersHash         string    `json:"filters_hash"`
	SourceRevision      string    `json:"source_revision"`
	RetentionEpoch      string    `json:"retention_epoch"`
	RetentionGeneration string    `json:"retention_generation"`
}

type AuditLogListItem struct {
	ID                                  int        `json:"id"`
	RequestLogID                        *string    `json:"request_log_id"`
	RequestLogCreatedAt                 *time.Time `json:"request_log_created_at"`
	IngressRequestID                    *string    `json:"ingress_request_id"`
	RequestLogMissing                   bool       `json:"request_log_missing"`
	ProfileID                           int        `json:"profile_id"`
	ModelID                             string     `json:"model_id"`
	EndpointID                          *int       `json:"endpoint_id"`
	ConnectionID                        *int       `json:"connection_id"`
	EndpointBaseURL                     *string    `json:"endpoint_base_url"`
	EndpointDescription                 *string    `json:"endpoint_description"`
	RequestMethod                       string     `json:"request_method"`
	RequestURL                          string     `json:"request_url"`
	RequestHeaders                      *string    `json:"request_headers"`
	RequestBodyPreview                  *string    `json:"request_body_preview"`
	RequestBodyPreviewTruncated         bool       `json:"request_body_preview_truncated"`
	RequestBodyPreviewUnavailableReason *string    `json:"request_body_preview_unavailable_reason"`
	RequestBodyStored                   bool       `json:"request_body_stored"`
	RequestBodyEncoding                 *string    `json:"request_body_encoding"`
	RequestBodyCaptureStatus            string     `json:"request_body_capture_status"`
	RequestBodyCaptureProvenance        string     `json:"request_body_capture_provenance"`
	RequestBodyCaptureEndState          *string    `json:"request_body_capture_end_state"`
	RequestBodyTruncated                bool       `json:"request_body_truncated"`
	RequestBodyBytesObserved            *int64     `json:"request_body_bytes_observed"`
	RequestBodyBytesStored              *int64     `json:"request_body_bytes_stored"`
	ResponseBodyStored                  bool       `json:"response_body_stored"`
	RowKind                             string     `json:"row_kind"`
	AttemptNumber                       *int       `json:"attempt_number"`
	AttemptDurationMS                   *int       `json:"attempt_duration_ms"`
	LegacyDurationMS                    *int       `json:"legacy_duration_ms"`
	UpstreamStatusCode                  *int       `json:"upstream_status_code"`
	GatewayStatusCode                   *int       `json:"gateway_status_code"`
	LegacyStatusCode                    *int       `json:"legacy_status_code"`
	RequestURLTruncated                 bool       `json:"request_url_truncated"`
	EndpointBaseURLTruncated            bool       `json:"endpoint_base_url_truncated"`
	IsStream                            bool       `json:"is_stream"`
	AuditEnabledAtRequest               bool       `json:"audit_enabled_at_request"`
	AuditCaptureBodiesAtRequest         bool       `json:"audit_capture_bodies_at_request"`
	CreatedAt                           time.Time  `json:"created_at"`
}

type AuditLogDetail struct {
	ID                            int        `json:"id"`
	RequestLogID                  *string    `json:"request_log_id"`
	RequestLogCreatedAt           *time.Time `json:"request_log_created_at"`
	IngressRequestID              *string    `json:"ingress_request_id"`
	RequestLogMissing             bool       `json:"request_log_missing"`
	ProfileID                     int        `json:"profile_id"`
	ModelID                       string     `json:"model_id"`
	EndpointID                    *int       `json:"endpoint_id"`
	ConnectionID                  *int       `json:"connection_id"`
	EndpointBaseURL               *string    `json:"endpoint_base_url"`
	EndpointDescription           *string    `json:"endpoint_description"`
	RequestMethod                 string     `json:"request_method"`
	RequestURL                    string     `json:"request_url"`
	RequestHeaders                *string    `json:"request_headers"`
	RequestBodyBase64             *string    `json:"request_body_base64"`
	RequestBodyStored             bool       `json:"request_body_stored"`
	RequestBodyEncoding           *string    `json:"request_body_encoding"`
	RequestBodyCaptureStatus      string     `json:"request_body_capture_status"`
	RequestBodyCaptureProvenance  string     `json:"request_body_capture_provenance"`
	RequestBodyCaptureEndState    *string    `json:"request_body_capture_end_state"`
	RequestBodyTruncated          bool       `json:"request_body_truncated"`
	RequestBodyBytesObserved      *int64     `json:"request_body_bytes_observed"`
	RequestBodyBytesStored        *int64     `json:"request_body_bytes_stored"`
	ResponseHeaders               *string    `json:"response_headers"`
	ResponseBodyBase64            *string    `json:"response_body_base64"`
	ResponseBodyStored            bool       `json:"response_body_stored"`
	ResponseBodyEncoding          *string    `json:"response_body_encoding"`
	ResponseBodyCaptureStatus     string     `json:"response_body_capture_status"`
	ResponseBodyCaptureProvenance string     `json:"response_body_capture_provenance"`
	ResponseBodyCaptureEndState   *string    `json:"response_body_capture_end_state"`
	ResponseBodyTruncated         bool       `json:"response_body_truncated"`
	ResponseBodyBytesObserved     *int64     `json:"response_body_bytes_observed"`
	ResponseBodyBytesStored       *int64     `json:"response_body_bytes_stored"`
	RowKind                       string     `json:"row_kind"`
	AttemptNumber                 *int       `json:"attempt_number"`
	AttemptDurationMS             *int       `json:"attempt_duration_ms"`
	LegacyDurationMS              *int       `json:"legacy_duration_ms"`
	UpstreamStatusCode            *int       `json:"upstream_status_code"`
	GatewayStatusCode             *int       `json:"gateway_status_code"`
	LegacyStatusCode              *int       `json:"legacy_status_code"`
	RequestURLTruncated           bool       `json:"request_url_truncated"`
	EndpointBaseURLTruncated      bool       `json:"endpoint_base_url_truncated"`
	IsStream                      bool       `json:"is_stream"`
	AuditEnabledAtRequest         bool       `json:"audit_enabled_at_request"`
	AuditCaptureBodiesAtRequest   bool       `json:"audit_capture_bodies_at_request"`
	CreatedAt                     time.Time  `json:"created_at"`
}

type AuditLogListResponse struct {
	Items      []AuditLogListItem `json:"items"`
	NextCursor *string            `json:"next_cursor"`
	HasMore    bool               `json:"has_more"`
	Window     ListWindow         `json:"window"`
	Limit      int                `json:"limit"`
	Sort       string             `json:"sort"`
	// AnchorItem carries the single anchored audit row when the anchor id is
	// outside the first page (SPEC: the first response must include the anchor
	// exactly once, never a sibling first and then a page flip).
	AnchorItem *AuditLogListItem `json:"anchor_item,omitempty"`
	// Coverage is the non-null query-scoped coverage projection (`known` |
	// `legacy_unknown` union, never null on successful responses).
	Coverage AuditQueryCoverage `json:"coverage"`
}

// AuditQueryCoverage mirrors statsdomain.QueryCoverage's JSON shape so the
// frontend consumes one coverage contract across Requests and Audit.
type AuditQueryCoverage struct {
	RequestedFromTime   time.Time                    `json:"requested_from_time"`
	RequestedToTime     time.Time                    `json:"requested_to_time"`
	EffectiveFromTime   time.Time                    `json:"effective_from_time"`
	EffectiveToTime     time.Time                    `json:"effective_to_time"`
	RetentionFromTime   *time.Time                   `json:"retention_from_time,omitempty"`
	Complete            bool                         `json:"complete"`
	Gaps                []AuditQueryCoverageGap      `json:"gaps"`
	Precision           *AuditQueryCoveragePrecision `json:"precision,omitempty"`
	State               string                       `json:"state"`
	SourceRevision      string                       `json:"source_revision"`
	RetentionEpoch      string                       `json:"retention_epoch,omitempty"`
	RetentionGeneration string                       `json:"retention_generation,omitempty"`
	PurgeState          string                       `json:"purge_state,omitempty"`
}

type AuditQueryCoverageGap struct {
	FromTime time.Time `json:"from_time"`
	ToTime   time.Time `json:"to_time"`
	Reason   string    `json:"reason"`
}

type AuditQueryCoveragePrecision struct {
	RowCount int `json:"row_count"`
}

const defaultCursorSigningKey = "prism-management-audit-cursor-v1"

func ListLogs(ctx context.Context, exec queryExecutor, params ListParams) (AuditLogListResponse, error) {
	limit := params.Limit
	if limit <= 0 {
		limit = 50
	}
	sortOrder := strings.TrimSpace(strings.ToLower(params.Sort))
	if sortOrder == "" {
		sortOrder = "desc"
	}
	if sortOrder != "desc" {
		return AuditLogListResponse{}, &HTTPError{StatusCode: 400, Code: "audit_sort_unsupported", Detail: "Only descending audit sort is supported."}
	}
	if params.FromTime == nil || params.ToTime == nil {
		return AuditLogListResponse{}, &HTTPError{StatusCode: 400, Code: "audit_window_required", Detail: "Audit list requires from and to query parameters."}
	}
	fromTime := params.FromTime.UTC()
	toTime := params.ToTime.UTC()
	cursorFiltersHash := auditListFiltersHash(params)
	var decodedCursor *listCursor
	if strings.TrimSpace(params.Cursor) != "" {
		cursor, err := decodeListCursor(params.Cursor)
		if err != nil {
			return AuditLogListResponse{}, &HTTPError{StatusCode: 400, Code: "audit_cursor_invalid", Detail: "Audit cursor is invalid."}
		}
		if cursor.Version != 2 || cursor.Limit != limit || cursor.Sort != sortOrder || cursor.FiltersHash != cursorFiltersHash {
			return AuditLogListResponse{}, &HTTPError{StatusCode: 400, Code: "audit_cursor_scope_mismatch", Detail: "Audit cursor does not match the requested window, sort, or filters."}
		}
		decodedCursor = &cursor
	}
	retentionSource, retentionFloor, err := loadAuditRetentionReadGate(ctx, exec, params.ReferenceNow)
	if err != nil {
		return AuditLogListResponse{}, err
	}
	actualCoverage, err := statsdomain.LoadActualCoverageProjection(ctx, exec, retentionSource)
	if err != nil {
		return AuditLogListResponse{}, err
	}
	requestedFromTime := fromTime
	requestedToTime := toTime
	coverageBounds, err := statsdomain.ResolveQueryBoundsFromActualCoverage("custom", &requestedFromTime, &requestedToTime, params.ReferenceNow.UTC(), retentionSource, actualCoverage)
	if err != nil {
		return AuditLogListResponse{}, err
	}
	fromTime = coverageBounds.UsageFrom.UTC()
	toTime = coverageBounds.UsageTo.UTC()
	retentionFloor = coverageBounds.UsageRetentionFrom
	params.FromTime = &fromTime
	params.ToTime = &toTime
	whereClause, args := buildListWhere(params)
	visibleWhereClause := whereClause + ` AND ` + auditLogReadVisibilityClause("audit_logs")
	if decodedCursor != nil {
		if !decodedCursor.From.Equal(fromTime) || !decodedCursor.To.Equal(toTime) {
			return AuditLogListResponse{}, &HTTPError{StatusCode: 400, Code: "audit_cursor_scope_mismatch", Detail: "Audit cursor does not match the requested window, sort, or filters."}
		}
		if decodedCursor.RetentionEpoch != retentionSource.RetentionEpoch {
			return AuditLogListResponse{}, &HTTPError{StatusCode: 410, Code: "audit_snapshot_revoked", Detail: "The audit snapshot was revoked; reload the first page."}
		}
		if decodedCursor.SourceRevision != retentionSource.SourceRevision || decodedCursor.RetentionGeneration != retentionSource.RetentionGeneration {
			return AuditLogListResponse{}, &HTTPError{StatusCode: 410, Code: "audit_snapshot_stale", Detail: "The audit snapshot is stale; reload the first page."}
		}
	}
	args = append(args, retentionFloor)
	visibleWhereClause += fmt.Sprintf(" AND created_at >= COALESCE($%d::timestamptz, '-infinity'::timestamptz)", len(args))
	if decodedCursor != nil {
		args = append(args, decodedCursor.LastCreatedAt.UTC(), decodedCursor.LastID)
		visibleWhereClause += fmt.Sprintf(" AND (created_at, id) < ($%d, $%d)", len(args)-1, len(args))
	}
	rows, err := exec.Query(ctx, `SELECT id, request_log_id::text, request_log_created_at, ingress_request_id, request_log_id IS NOT NULL AND request_log_created_at IS NOT NULL AND NOT EXISTS (SELECT 1 FROM request_logs WHERE request_logs.profile_id = audit_logs.profile_id AND request_logs.id = audit_logs.request_log_id AND request_logs.created_at = audit_logs.request_log_created_at) AS request_log_missing, profile_id, model_id, endpoint_id, connection_id, endpoint_base_url, endpoint_description, request_method, request_url, request_headers, request_body, request_body_bytes_observed, request_body_bytes_stored, request_body_encoding, request_body_capture_status, request_body_capture_provenance, request_body_capture_end_state, request_body_truncated, response_body IS NOT NULL AS response_body_stored, row_kind, attempt_number, attempt_duration_ms, legacy_duration_ms, upstream_status_code, gateway_status_code, legacy_status_code, request_url_truncated, endpoint_base_url_truncated, is_stream, audit_enabled_at_request, audit_capture_bodies_at_request, created_at FROM audit_logs WHERE `+visibleWhereClause+` ORDER BY created_at DESC, id DESC LIMIT $`+fmt.Sprintf("%d", len(args)+1), append(args, limit+1)...)
	if err != nil {
		return AuditLogListResponse{}, fmt.Errorf("query audit logs for profile %d: %w", params.ProfileID, err)
	}
	defer rows.Close()
	items := make([]AuditLogListItem, 0)
	for rows.Next() {
		item, scanErr := scanListItem(rows)
		if scanErr != nil {
			return AuditLogListResponse{}, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return AuditLogListResponse{}, fmt.Errorf("iterate audit logs for profile %d: %w", params.ProfileID, err)
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	var nextCursor *string
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		encoded, err := encodeListCursor(listCursor{Version: 2, LastCreatedAt: last.CreatedAt.UTC(), LastID: last.ID, From: fromTime, To: toTime, Limit: limit, Sort: sortOrder, FiltersHash: cursorFiltersHash, SourceRevision: retentionSource.SourceRevision, RetentionEpoch: retentionSource.RetentionEpoch, RetentionGeneration: retentionSource.RetentionGeneration})
		if err != nil {
			return AuditLogListResponse{}, err
		}
		nextCursor = &encoded
	}
	coverage := buildAuditQueryCoverage(coverageBounds, retentionSource, actualCoverage)
	response := AuditLogListResponse{Items: items, NextCursor: nextCursor, HasMore: hasMore, Window: ListWindow{From: fromTime, To: toTime}, Limit: limit, Sort: sortOrder, Coverage: coverage}
	if params.AnchorAuditID != nil && !auditItemsContain(items, *params.AnchorAuditID) {
		anchor, err := loadAnchorItem(ctx, exec, params.ProfileID, *params.AnchorAuditID, visibleWhereClause, args)
		if err != nil {
			return AuditLogListResponse{}, err
		}
		response.AnchorItem = anchor
	}
	return response, nil
}

// buildAuditQueryCoverage projects the non-null coverage union from the
// Requests/Audit actual-coverage owner and the same frozen read bounds used by
// the SQL predicate. A policy floor is only one possible gap; it is never the
// actual lower bound for an `all`-style owner query.
func buildAuditQueryCoverage(bounds statsdomain.QueryBounds, source statsdomain.RetentionFloorEpochSource, actual statsdomain.ActualCoverageProjection) AuditQueryCoverage {
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
	gaps := make([]AuditQueryCoverageGap, 0, len(bounds.Gaps))
	for _, gap := range bounds.Gaps {
		gaps = append(gaps, AuditQueryCoverageGap{FromTime: gap.FromTime.UTC(), ToTime: gap.ToTime.UTC(), Reason: gap.Reason})
	}
	state := "known"
	if !bounds.Complete || actual.Freshness != "fresh" {
		state = "legacy_unknown"
	}
	return AuditQueryCoverage{
		RequestedFromTime:   requestedFrom.UTC(),
		RequestedToTime:     requestedTo.UTC(),
		EffectiveFromTime:   bounds.UsageFrom.UTC(),
		EffectiveToTime:     bounds.UsageTo.UTC(),
		RetentionFromTime:   auditRetentionTime(bounds.UsageRetentionFrom),
		Complete:            bounds.Complete,
		Gaps:                gaps,
		Precision:           nil,
		State:               state,
		SourceRevision:      source.SourceRevision,
		RetentionEpoch:      source.RetentionEpoch,
		RetentionGeneration: source.RetentionGeneration,
		PurgeState:          source.PurgeState,
	}
}

// loadAuditRetentionReadGate is the shared Requests/Audit read fence. Detail,
// body, list and coverage reads must all observe the same owner purge state and
// effective floor; checking only the list coverage would leave a direct detail
// or raw-body URL able to bypass a running purge or an already-published floor.
func loadAuditRetentionReadGate(ctx context.Context, exec queryExecutor, referenceNow time.Time) (statsdomain.RetentionFloorEpochSource, *time.Time, error) {
	now := referenceNow.UTC()
	source, err := statsdomain.LoadRetentionSourceProjection(ctx, exec, "audit_logs", now)
	if err != nil {
		return statsdomain.RetentionFloorEpochSource{}, nil, fmt.Errorf("load audit retention source: %w", err)
	}
	protection, err := LoadAuditFenceMaterializerProjection(ctx, exec, now)
	if err != nil {
		return statsdomain.RetentionFloorEpochSource{}, nil, err
	}
	if source.PurgeState == "running" || source.PurgeState == "recovery_required" ||
		protection.ReaderFenceState != "clear" || protection.MaterializerState != "ready" {
		return statsdomain.RetentionFloorEpochSource{}, nil, &HTTPError{
			StatusCode: 503,
			Code:       "audit_purge_in_progress",
			Detail:     "audit data is temporarily unavailable while retention cleanup is publishing",
			Details:    map[string]any{"retry_after_seconds": 3},
		}
	}
	floor := source.ConfiguredCutoff
	if source.PublishedFloor != nil && (floor == nil || source.PublishedFloor.After(*floor)) {
		floor = source.PublishedFloor
	}
	return source, floor, nil
}

func auditRetentionTime(retentionFrom *time.Time) *time.Time {
	if retentionFrom == nil {
		return nil
	}
	resolved := retentionFrom.UTC()
	return &resolved
}

func auditItemsContain(items []AuditLogListItem, id int) bool {
	for _, item := range items {
		if item.ID == id {
			return true
		}
	}
	return false
}

// loadAnchorItem returns the anchored audit row when it is not already part of
// the first page, honoring the same visibility gate. Returns nil when the row
// is in-page or does not exist.
func loadAnchorItem(ctx context.Context, exec queryExecutor, profileID int, anchorID int, visibleWhereClause string, args []any) (*AuditLogListItem, error) {
	anchorArgs := append(append([]any{}, args...), anchorID)
	// Keep the anchor projection byte-for-byte aligned with the paged list
	// projection. The old shortcut selected the legacy body/status columns and
	// shifted scan positions, turning a valid anchor into a generic 500.
	row := exec.QueryRow(ctx, `SELECT id, request_log_id::text, request_log_created_at, ingress_request_id, request_log_id IS NOT NULL AND request_log_created_at IS NOT NULL AND NOT EXISTS (SELECT 1 FROM request_logs WHERE request_logs.profile_id = audit_logs.profile_id AND request_logs.id = audit_logs.request_log_id AND request_logs.created_at = audit_logs.request_log_created_at) AS request_log_missing, profile_id, model_id, endpoint_id, connection_id, endpoint_base_url, endpoint_description, request_method, request_url, request_headers, request_body, request_body_bytes_observed, request_body_bytes_stored, request_body_encoding, request_body_capture_status, request_body_capture_provenance, request_body_capture_end_state, request_body_truncated, response_body IS NOT NULL AS response_body_stored, row_kind, attempt_number, attempt_duration_ms, legacy_duration_ms, upstream_status_code, gateway_status_code, legacy_status_code, request_url_truncated, endpoint_base_url_truncated, is_stream, audit_enabled_at_request, audit_capture_bodies_at_request, created_at FROM audit_logs WHERE `+visibleWhereClause+` AND id = $`+fmt.Sprintf("%d", len(anchorArgs))+` ORDER BY created_at DESC LIMIT 1`, anchorArgs...)
	item, err := scanListItem(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load anchor audit item %d for profile %d: %w", anchorID, profileID, err)
	}
	return &item, nil
}

func GetLog(ctx context.Context, exec queryExecutor, profileID int, logID int) (*AuditLogDetail, error) {
	_, retentionFloor, err := loadAuditRetentionReadGate(ctx, exec, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	readState, found, err := loadAuditLogReadState(ctx, exec, profileID, logID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	if !readState.AuditEnabledAtRequest {
		return nil, &HTTPError{StatusCode: 409, Detail: "Audit capture unavailable for this request"}
	}
	var createdAt time.Time
	if err := exec.QueryRow(ctx, `SELECT created_at FROM audit_logs WHERE profile_id = $1 AND id = $2 ORDER BY created_at DESC LIMIT 1`, profileID, logID).Scan(&createdAt); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("load audit log %d timestamp for profile %d: %w", logID, profileID, err)
	}
	if retentionFloor != nil && createdAt.Before(*retentionFloor) {
		return nil, &HTTPError{StatusCode: 410, Code: "audit_evidence_revoked", Detail: "Audit evidence is no longer available."}
	}
	row := exec.QueryRow(ctx, `SELECT id, request_log_id::text, request_log_created_at, ingress_request_id, request_log_id IS NOT NULL AND request_log_created_at IS NOT NULL AND NOT EXISTS (SELECT 1 FROM request_logs WHERE request_logs.profile_id = audit_logs.profile_id AND request_logs.id = audit_logs.request_log_id AND request_logs.created_at = audit_logs.request_log_created_at) AS request_log_missing, profile_id, model_id, endpoint_id, connection_id, endpoint_base_url, endpoint_description, request_method, request_url, request_headers, request_body, request_body_bytes_observed, request_body_bytes_stored, request_body_encoding, request_body_capture_status, request_body_capture_provenance, request_body_capture_end_state, request_body_truncated, response_headers, response_body, response_body_bytes_observed, response_body_bytes_stored, response_body_encoding, response_body_capture_status, response_body_capture_provenance, response_body_capture_end_state, response_body_truncated, row_kind, attempt_number, attempt_duration_ms, legacy_duration_ms, upstream_status_code, gateway_status_code, legacy_status_code, request_url_truncated, endpoint_base_url_truncated, is_stream, audit_enabled_at_request, audit_capture_bodies_at_request, created_at FROM audit_logs WHERE profile_id = $1 AND id = $2 AND created_at >= COALESCE($3::timestamptz, '-infinity'::timestamptz) ORDER BY created_at DESC LIMIT 1`, profileID, logID, retentionFloor)
	item, err := scanDetail(row)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load audit log %d for profile %d: %w", logID, profileID, err)
	}
	return &item, nil
}

func buildListWhere(params ListParams) (string, []any) {
	clauses := []string{"profile_id = $1"}
	args := []any{params.ProfileID}
	if params.RequestLogID != nil {
		args = append(args, *params.RequestLogID)
		clauses = append(clauses, fmt.Sprintf("request_log_id = $%d", len(args)))
	}
	if params.ModelID != nil && strings.TrimSpace(*params.ModelID) != "" {
		args = append(args, strings.TrimSpace(*params.ModelID))
		clauses = append(clauses, fmt.Sprintf("model_id = $%d", len(args)))
	}
	if params.StatusCode != nil {
		args = append(args, *params.StatusCode)
		clauses = append(clauses, fmt.Sprintf("(upstream_status_code = $%d OR gateway_status_code = $%d OR legacy_status_code = $%d)", len(args), len(args), len(args)))
	}
	if params.EndpointID != nil {
		args = append(args, *params.EndpointID)
		clauses = append(clauses, fmt.Sprintf("endpoint_id = $%d", len(args)))
	}
	if params.ConnectionID != nil {
		args = append(args, *params.ConnectionID)
		clauses = append(clauses, fmt.Sprintf("connection_id = $%d", len(args)))
	}
	if params.FromTime != nil {
		args = append(args, params.FromTime.UTC())
		clauses = append(clauses, fmt.Sprintf("created_at >= $%d", len(args)))
	}
	if params.ToTime != nil {
		args = append(args, params.ToTime.UTC())
		clauses = append(clauses, fmt.Sprintf("created_at < $%d", len(args)))
	}
	return strings.Join(clauses, " AND "), args
}

func auditListFiltersHash(params ListParams) string {
	normalized := map[string]any{
		"profile_id": params.ProfileID,
	}
	if params.RequestLogID != nil {
		normalized["request_log_id"] = *params.RequestLogID
	}
	if params.ModelID != nil {
		normalized["model_id"] = strings.TrimSpace(*params.ModelID)
	}
	if params.StatusCode != nil {
		normalized["status_code"] = *params.StatusCode
	}
	if params.EndpointID != nil {
		normalized["endpoint_id"] = *params.EndpointID
	}
	if params.ConnectionID != nil {
		normalized["connection_id"] = *params.ConnectionID
	}
	raw, _ := json.Marshal(normalized)
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func encodeListCursor(cursor listCursor) (string, error) {
	raw, err := json.Marshal(cursor)
	if err != nil {
		return "", fmt.Errorf("encode audit cursor: %w", err)
	}
	signature := signListCursor(raw)
	return base64.RawURLEncoding.EncodeToString(raw) + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func decodeListCursor(encoded string) (listCursor, error) {
	parts := strings.Split(encoded, ".")
	if len(parts) != 2 {
		return listCursor{}, fmt.Errorf("invalid audit cursor")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return listCursor{}, err
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return listCursor{}, err
	}
	if !hmac.Equal(signature, signListCursor(raw)) {
		return listCursor{}, fmt.Errorf("invalid audit cursor signature")
	}
	var cursor listCursor
	if err := json.Unmarshal(raw, &cursor); err != nil {
		return listCursor{}, err
	}
	return cursor, nil
}

func signListCursor(raw []byte) []byte {
	mac := hmac.New(sha256.New, []byte(defaultCursorSigningKey))
	_, _ = mac.Write(raw)
	return mac.Sum(nil)
}

func auditLogReadVisibilityClause(tableName string) string {
	return fmt.Sprintf(`%s.audit_enabled_at_request = TRUE`, tableName)
}

type auditLogReadState struct {
	RequestLogID          *string
	AuditEnabledAtRequest bool
}

// LoadAuditReadState exposes the read-gate for management handlers (raw-body
// downloads reuse the same authorization boundary as detail reads).
func LoadAuditReadState(ctx context.Context, exec queryExecutor, profileID int, logID int) (auditLogReadState, bool, error) {
	return loadAuditLogReadState(ctx, exec, profileID, logID)
}

func loadAuditLogReadState(ctx context.Context, exec queryExecutor, profileID int, logID int) (auditLogReadState, bool, error) {
	var requestLogID sql.NullString
	var auditEnabledAtRequest bool
	if err := exec.QueryRow(ctx, `SELECT request_log_id::text, audit_enabled_at_request FROM audit_logs WHERE profile_id = $1 AND id = $2 ORDER BY created_at DESC LIMIT 1`, profileID, logID).Scan(&requestLogID, &auditEnabledAtRequest); err != nil {
		if err == pgx.ErrNoRows {
			return auditLogReadState{}, false, nil
		}
		return auditLogReadState{}, false, fmt.Errorf("load audit log %d read state for profile %d: %w", logID, profileID, err)
	}
	return auditLogReadState{RequestLogID: nullableString(requestLogID), AuditEnabledAtRequest: auditEnabledAtRequest}, true, nil
}

func scanListItem(scanner interface{ Scan(...any) error }) (AuditLogListItem, error) {
	var requestLogID sql.NullString
	var requestLogCreatedAt sql.NullTime
	var ingressRequestID sql.NullString
	var endpointID sql.NullInt32
	var connectionID sql.NullInt32
	var endpointBaseURL sql.NullString
	var endpointDescription sql.NullString
	var requestHeaders []byte
	var requestBody []byte
	var requestBodyBytesObserved sql.NullInt64
	var requestBodyBytesStored sql.NullInt64
	var requestBodyEncoding sql.NullString
	var requestBodyCaptureStatus sql.NullString
	var requestBodyCaptureProvenance sql.NullString
	var requestBodyCaptureEndState sql.NullString
	var attemptNumber sql.NullInt32
	var attemptDurationMS sql.NullInt32
	var legacyDurationMS sql.NullInt32
	var upstreamStatusCode sql.NullInt32
	var gatewayStatusCode sql.NullInt32
	var legacyStatusCode sql.NullInt32
	item := AuditLogListItem{}
	if err := scanner.Scan(&item.ID, &requestLogID, &requestLogCreatedAt, &ingressRequestID, &item.RequestLogMissing, &item.ProfileID, &item.ModelID, &endpointID, &connectionID, &endpointBaseURL, &endpointDescription, &item.RequestMethod, &item.RequestURL, &requestHeaders, &requestBody, &requestBodyBytesObserved, &requestBodyBytesStored, &requestBodyEncoding, &requestBodyCaptureStatus, &requestBodyCaptureProvenance, &requestBodyCaptureEndState, &item.RequestBodyTruncated, &item.ResponseBodyStored, &item.RowKind, &attemptNumber, &attemptDurationMS, &legacyDurationMS, &upstreamStatusCode, &gatewayStatusCode, &legacyStatusCode, &item.RequestURLTruncated, &item.EndpointBaseURLTruncated, &item.IsStream, &item.AuditEnabledAtRequest, &item.AuditCaptureBodiesAtRequest, &item.CreatedAt); err != nil {
		return AuditLogListItem{}, fmt.Errorf("scan audit-log list row: %w", err)
	}
	item.RequestLogID = nullableString(requestLogID)
	item.RequestLogCreatedAt = nullableTime(requestLogCreatedAt)
	item.IngressRequestID = nullableString(ingressRequestID)
	item.EndpointID = nullableInt32(endpointID)
	item.ConnectionID = nullableInt32(connectionID)
	item.EndpointBaseURL = nullableString(endpointBaseURL)
	item.EndpointDescription = nullableString(endpointDescription)
	item.RequestHeaders = auditHeadersJSON(requestHeaders)
	item.RequestBodyStored = len(requestBody) > 0
	item.RequestBodyEncoding = nullableString(requestBodyEncoding)
	item.RequestBodyCaptureStatus = stringValue(nullableString(requestBodyCaptureStatus))
	item.RequestBodyCaptureProvenance = stringValue(nullableString(requestBodyCaptureProvenance))
	item.RequestBodyCaptureEndState = nullableString(requestBodyCaptureEndState)
	item.RequestBodyBytesObserved = nullableInt64(requestBodyBytesObserved)
	item.RequestBodyBytesStored = nullableInt64(requestBodyBytesStored)
	item.RequestBodyPreview, item.RequestBodyPreviewTruncated, item.RequestBodyPreviewUnavailableReason = auditBodyPreview(requestBody)
	item.AttemptNumber = nullableInt32(attemptNumber)
	item.AttemptDurationMS = nullableInt32(attemptDurationMS)
	item.LegacyDurationMS = nullableInt32(legacyDurationMS)
	item.UpstreamStatusCode = nullableInt32(upstreamStatusCode)
	item.GatewayStatusCode = nullableInt32(gatewayStatusCode)
	item.LegacyStatusCode = nullableInt32(legacyStatusCode)
	item.CreatedAt = item.CreatedAt.UTC()
	return item, nil
}

func scanDetail(scanner interface{ Scan(...any) error }) (AuditLogDetail, error) {
	var requestLogID sql.NullString
	var requestLogCreatedAt sql.NullTime
	var ingressRequestID sql.NullString
	var endpointID sql.NullInt32
	var connectionID sql.NullInt32
	var endpointBaseURL sql.NullString
	var endpointDescription sql.NullString
	var requestHeaders []byte
	var requestBody []byte
	var requestBodyBytesObserved sql.NullInt64
	var requestBodyBytesStored sql.NullInt64
	var requestBodyEncoding sql.NullString
	var requestBodyCaptureStatus sql.NullString
	var requestBodyCaptureProvenance sql.NullString
	var requestBodyCaptureEndState sql.NullString
	var responseHeaders []byte
	var responseBody []byte
	var responseBodyBytesObserved sql.NullInt64
	var responseBodyBytesStored sql.NullInt64
	var responseBodyEncoding sql.NullString
	var responseBodyCaptureStatus sql.NullString
	var responseBodyCaptureProvenance sql.NullString
	var responseBodyCaptureEndState sql.NullString
	var attemptNumber sql.NullInt32
	var attemptDurationMS sql.NullInt32
	var legacyDurationMS sql.NullInt32
	var upstreamStatusCode sql.NullInt32
	var gatewayStatusCode sql.NullInt32
	var legacyStatusCode sql.NullInt32
	item := AuditLogDetail{}
	if err := scanner.Scan(&item.ID, &requestLogID, &requestLogCreatedAt, &ingressRequestID, &item.RequestLogMissing, &item.ProfileID, &item.ModelID, &endpointID, &connectionID, &endpointBaseURL, &endpointDescription, &item.RequestMethod, &item.RequestURL, &requestHeaders, &requestBody, &requestBodyBytesObserved, &requestBodyBytesStored, &requestBodyEncoding, &requestBodyCaptureStatus, &requestBodyCaptureProvenance, &requestBodyCaptureEndState, &item.RequestBodyTruncated, &responseHeaders, &responseBody, &responseBodyBytesObserved, &responseBodyBytesStored, &responseBodyEncoding, &responseBodyCaptureStatus, &responseBodyCaptureProvenance, &responseBodyCaptureEndState, &item.ResponseBodyTruncated, &item.RowKind, &attemptNumber, &attemptDurationMS, &legacyDurationMS, &upstreamStatusCode, &gatewayStatusCode, &legacyStatusCode, &item.RequestURLTruncated, &item.EndpointBaseURLTruncated, &item.IsStream, &item.AuditEnabledAtRequest, &item.AuditCaptureBodiesAtRequest, &item.CreatedAt); err != nil {
		return AuditLogDetail{}, err
	}
	item.RequestLogID = nullableString(requestLogID)
	item.RequestLogCreatedAt = nullableTime(requestLogCreatedAt)
	item.IngressRequestID = nullableString(ingressRequestID)
	item.EndpointID = nullableInt32(endpointID)
	item.ConnectionID = nullableInt32(connectionID)
	item.EndpointBaseURL = nullableString(endpointBaseURL)
	item.EndpointDescription = nullableString(endpointDescription)
	item.RequestHeaders = auditHeadersJSON(requestHeaders)
	item.RequestBodyBase64 = auditBodyBase64(requestBody)
	item.RequestBodyStored = len(requestBody) > 0
	item.RequestBodyEncoding = nullableString(requestBodyEncoding)
	item.RequestBodyCaptureStatus = stringValue(nullableString(requestBodyCaptureStatus))
	item.RequestBodyCaptureProvenance = stringValue(nullableString(requestBodyCaptureProvenance))
	item.RequestBodyCaptureEndState = nullableString(requestBodyCaptureEndState)
	item.RequestBodyBytesObserved = nullableInt64(requestBodyBytesObserved)
	item.RequestBodyBytesStored = nullableInt64(requestBodyBytesStored)
	item.ResponseHeaders = auditHeadersJSON(responseHeaders)
	item.ResponseBodyBase64 = auditBodyBase64(responseBody)
	item.ResponseBodyStored = len(responseBody) > 0
	item.ResponseBodyEncoding = nullableString(responseBodyEncoding)
	item.ResponseBodyCaptureStatus = stringValue(nullableString(responseBodyCaptureStatus))
	item.ResponseBodyCaptureProvenance = stringValue(nullableString(responseBodyCaptureProvenance))
	item.ResponseBodyCaptureEndState = nullableString(responseBodyCaptureEndState)
	item.ResponseBodyBytesObserved = nullableInt64(responseBodyBytesObserved)
	item.ResponseBodyBytesStored = nullableInt64(responseBodyBytesStored)
	item.AttemptNumber = nullableInt32(attemptNumber)
	item.AttemptDurationMS = nullableInt32(attemptDurationMS)
	item.LegacyDurationMS = nullableInt32(legacyDurationMS)
	item.UpstreamStatusCode = nullableInt32(upstreamStatusCode)
	item.GatewayStatusCode = nullableInt32(gatewayStatusCode)
	item.LegacyStatusCode = nullableInt32(legacyStatusCode)
	item.CreatedAt = item.CreatedAt.UTC()
	return item, nil
}

func nullableString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	resolved := value.String
	return &resolved
}

func nullableTime(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	resolved := value.Time.UTC()
	return &resolved
}

func nullableInt32(value sql.NullInt32) *int {
	if !value.Valid {
		return nil
	}
	resolved := int(value.Int32)
	return &resolved
}

func nullableInt64(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	resolved := value.Int64
	return &resolved
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func auditHeadersJSON(value []byte) *string {
	if len(value) == 0 || !json.Valid(value) {
		return nil
	}
	resolved := string(value)
	return &resolved
}

func auditBodyBase64(value []byte) *string {
	if value == nil {
		return nil
	}
	resolved := base64.StdEncoding.EncodeToString(value)
	return &resolved
}

func auditBodyPreview(value []byte) (*string, bool, *string) {
	if len(value) == 0 {
		return nil, false, nil
	}
	if !utf8.Valid(value) {
		reason := "invalid_utf8"
		return nil, false, &reason
	}
	const maxPreviewRunes = 4096
	runes := []rune(string(value))
	truncated := len(runes) > maxPreviewRunes
	if truncated {
		runes = runes[:maxPreviewRunes]
	}
	preview := string(runes)
	return &preview, truncated, nil
}

