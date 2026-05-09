package audit

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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
	ProfileID    int
	RequestLogID *int
	VendorID     *int
	ModelID      *string
	StatusCode   *int
	EndpointID   *int
	ConnectionID *int
	FromTime     *time.Time
	ToTime       *time.Time
	Limit        int
	Cursor       string
	Sort         string
}

type ListWindow struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

type listCursor struct {
	Version       int       `json:"v"`
	LastCreatedAt time.Time `json:"last_created_at"`
	LastID        int       `json:"last_id"`
	From          time.Time `json:"from"`
	To            time.Time `json:"to"`
	Sort          string    `json:"sort"`
	FiltersHash   string    `json:"filters_hash"`
}

type DeleteParams struct {
	ProfileID     int
	Before        *time.Time
	OlderThanDays *int
	DeleteAll     bool
	ReferenceNow  time.Time
}

type AuditLogListItem struct {
	ID                          int        `json:"id"`
	RequestLogID                *int       `json:"request_log_id"`
	RequestLogCreatedAt         *time.Time `json:"request_log_created_at"`
	IngressRequestID            *string    `json:"ingress_request_id"`
	RequestLogMissing           bool       `json:"request_log_missing"`
	ProfileID                   int        `json:"profile_id"`
	VendorID                    *int       `json:"vendor_id"`
	ModelID                     string     `json:"model_id"`
	EndpointID                  *int       `json:"endpoint_id"`
	ConnectionID                *int       `json:"connection_id"`
	EndpointBaseURL             *string    `json:"endpoint_base_url"`
	EndpointDescription         *string    `json:"endpoint_description"`
	RequestMethod               string     `json:"request_method"`
	RequestURL                  string     `json:"request_url"`
	RequestHeaders              string     `json:"request_headers"`
	RequestBodyPreview          *string    `json:"request_body_preview"`
	RequestBodyStored           bool       `json:"request_body_stored"`
	ResponseStatus              int        `json:"response_status"`
	ResponseBodyStored          bool       `json:"response_body_stored"`
	IsStream                    bool       `json:"is_stream"`
	DurationMS                  int        `json:"duration_ms"`
	AuditEnabledAtRequest       bool       `json:"audit_enabled_at_request"`
	AuditCaptureBodiesAtRequest bool       `json:"audit_capture_bodies_at_request"`
	CreatedAt                   time.Time  `json:"created_at"`
}

type AuditLogDetail struct {
	ID                          int        `json:"id"`
	RequestLogID                *int       `json:"request_log_id"`
	RequestLogCreatedAt         *time.Time `json:"request_log_created_at"`
	IngressRequestID            *string    `json:"ingress_request_id"`
	RequestLogMissing           bool       `json:"request_log_missing"`
	ProfileID                   int        `json:"profile_id"`
	VendorID                    *int       `json:"vendor_id"`
	ModelID                     string     `json:"model_id"`
	EndpointID                  *int       `json:"endpoint_id"`
	ConnectionID                *int       `json:"connection_id"`
	EndpointBaseURL             *string    `json:"endpoint_base_url"`
	EndpointDescription         *string    `json:"endpoint_description"`
	RequestMethod               string     `json:"request_method"`
	RequestURL                  string     `json:"request_url"`
	RequestHeaders              string     `json:"request_headers"`
	RequestBody                 *string    `json:"request_body"`
	RequestBodyStored           bool       `json:"request_body_stored"`
	ResponseStatus              int        `json:"response_status"`
	ResponseHeaders             *string    `json:"response_headers"`
	ResponseBody                *string    `json:"response_body"`
	ResponseBodyStored          bool       `json:"response_body_stored"`
	IsStream                    bool       `json:"is_stream"`
	DurationMS                  int        `json:"duration_ms"`
	AuditEnabledAtRequest       bool       `json:"audit_enabled_at_request"`
	AuditCaptureBodiesAtRequest bool       `json:"audit_capture_bodies_at_request"`
	CreatedAt                   time.Time  `json:"created_at"`
}

type AuditLogListResponse struct {
	Items      []AuditLogListItem `json:"items"`
	NextCursor *string            `json:"next_cursor"`
	HasMore    bool               `json:"has_more"`
	Window     ListWindow         `json:"window"`
	Limit      int                `json:"limit"`
	Sort       string             `json:"sort"`
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
		if cursor.Version != 1 || !cursor.From.Equal(fromTime) || !cursor.To.Equal(toTime) || cursor.Sort != sortOrder || cursor.FiltersHash != cursorFiltersHash {
			return AuditLogListResponse{}, &HTTPError{StatusCode: 400, Code: "audit_cursor_scope_mismatch", Detail: "Audit cursor does not match the requested window, sort, or filters."}
		}
		decodedCursor = &cursor
	}
	whereClause, args := buildListWhere(params)
	visibleWhereClause := whereClause + ` AND ` + auditLogReadVisibilityClause("audit_logs")
	if decodedCursor != nil {
		args = append(args, decodedCursor.LastCreatedAt.UTC(), decodedCursor.LastID)
		visibleWhereClause += fmt.Sprintf(" AND (created_at, id) < ($%d, $%d)", len(args)-1, len(args))
	}
	rows, err := exec.Query(ctx, `SELECT id, request_log_id, request_log_created_at, ingress_request_id, request_log_id IS NOT NULL AND request_log_created_at IS NOT NULL AND NOT EXISTS (SELECT 1 FROM request_logs WHERE request_logs.profile_id = audit_logs.profile_id AND request_logs.id = audit_logs.request_log_id AND request_logs.created_at = audit_logs.request_log_created_at) AS request_log_missing, profile_id, vendor_id, model_id, endpoint_id, connection_id, endpoint_base_url, endpoint_description, request_method, request_url, request_headers, request_body, request_body_stored, response_status, response_body_stored, is_stream, duration_ms, audit_enabled_at_request, audit_capture_bodies_at_request, created_at FROM audit_logs WHERE `+visibleWhereClause+` ORDER BY created_at DESC, id DESC LIMIT $`+fmt.Sprintf("%d", len(args)+1), append(args, limit+1)...)
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
		encoded, err := encodeListCursor(listCursor{Version: 1, LastCreatedAt: last.CreatedAt.UTC(), LastID: last.ID, From: fromTime, To: toTime, Sort: sortOrder, FiltersHash: cursorFiltersHash})
		if err != nil {
			return AuditLogListResponse{}, err
		}
		nextCursor = &encoded
	}
	return AuditLogListResponse{Items: items, NextCursor: nextCursor, HasMore: hasMore, Window: ListWindow{From: fromTime, To: toTime}, Limit: limit, Sort: sortOrder}, nil
}

func GetLog(ctx context.Context, exec queryExecutor, profileID int, logID int) (*AuditLogDetail, error) {
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
	row := exec.QueryRow(ctx, `SELECT id, request_log_id, request_log_created_at, ingress_request_id, request_log_id IS NOT NULL AND request_log_created_at IS NOT NULL AND NOT EXISTS (SELECT 1 FROM request_logs WHERE request_logs.profile_id = audit_logs.profile_id AND request_logs.id = audit_logs.request_log_id AND request_logs.created_at = audit_logs.request_log_created_at) AS request_log_missing, profile_id, vendor_id, model_id, endpoint_id, connection_id, endpoint_base_url, endpoint_description, request_method, request_url, request_headers, request_body, request_body_stored, response_status, response_headers, response_body, response_body_stored, is_stream, duration_ms, audit_enabled_at_request, audit_capture_bodies_at_request, created_at FROM audit_logs WHERE profile_id = $1 AND id = $2 ORDER BY created_at DESC LIMIT 1`, profileID, logID)
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
	if params.VendorID != nil {
		args = append(args, *params.VendorID)
		clauses = append(clauses, fmt.Sprintf("vendor_id = $%d", len(args)))
	}
	if params.ModelID != nil && strings.TrimSpace(*params.ModelID) != "" {
		args = append(args, strings.TrimSpace(*params.ModelID))
		clauses = append(clauses, fmt.Sprintf("model_id = $%d", len(args)))
	}
	if params.StatusCode != nil {
		args = append(args, *params.StatusCode)
		clauses = append(clauses, fmt.Sprintf("response_status = $%d", len(args)))
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
	if params.VendorID != nil {
		normalized["vendor_id"] = *params.VendorID
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
	RequestLogID          *int
	AuditEnabledAtRequest bool
}

func loadAuditLogReadState(ctx context.Context, exec queryExecutor, profileID int, logID int) (auditLogReadState, bool, error) {
	var requestLogID sql.NullInt32
	var auditEnabledAtRequest bool
	if err := exec.QueryRow(ctx, `SELECT request_log_id, audit_enabled_at_request FROM audit_logs WHERE profile_id = $1 AND id = $2 ORDER BY created_at DESC LIMIT 1`, profileID, logID).Scan(&requestLogID, &auditEnabledAtRequest); err != nil {
		if err == pgx.ErrNoRows {
			return auditLogReadState{}, false, nil
		}
		return auditLogReadState{}, false, fmt.Errorf("load audit log %d read state for profile %d: %w", logID, profileID, err)
	}
	return auditLogReadState{RequestLogID: nullableInt32(requestLogID), AuditEnabledAtRequest: auditEnabledAtRequest}, true, nil
}

func scanListItem(scanner interface{ Scan(...any) error }) (AuditLogListItem, error) {
	var requestLogID sql.NullInt32
	var requestLogCreatedAt sql.NullTime
	var ingressRequestID sql.NullString
	var vendorID sql.NullInt32
	var endpointID sql.NullInt32
	var connectionID sql.NullInt32
	var endpointBaseURL sql.NullString
	var endpointDescription sql.NullString
	var requestBody sql.NullString
	item := AuditLogListItem{}
	if err := scanner.Scan(&item.ID, &requestLogID, &requestLogCreatedAt, &ingressRequestID, &item.RequestLogMissing, &item.ProfileID, &vendorID, &item.ModelID, &endpointID, &connectionID, &endpointBaseURL, &endpointDescription, &item.RequestMethod, &item.RequestURL, &item.RequestHeaders, &requestBody, &item.RequestBodyStored, &item.ResponseStatus, &item.ResponseBodyStored, &item.IsStream, &item.DurationMS, &item.AuditEnabledAtRequest, &item.AuditCaptureBodiesAtRequest, &item.CreatedAt); err != nil {
		return AuditLogListItem{}, fmt.Errorf("scan audit-log list row: %w", err)
	}
	item.RequestLogID = nullableInt32(requestLogID)
	item.RequestLogCreatedAt = nullableTime(requestLogCreatedAt)
	item.IngressRequestID = nullableString(ingressRequestID)
	item.VendorID = nullableInt32(vendorID)
	item.EndpointID = nullableInt32(endpointID)
	item.ConnectionID = nullableInt32(connectionID)
	item.EndpointBaseURL = nullableString(endpointBaseURL)
	item.EndpointDescription = nullableString(endpointDescription)
	item.RequestBodyPreview = trimmedPreview(nullableString(requestBody), 200)
	item.CreatedAt = item.CreatedAt.UTC()
	return item, nil
}

func scanDetail(scanner interface{ Scan(...any) error }) (AuditLogDetail, error) {
	var requestLogID sql.NullInt32
	var requestLogCreatedAt sql.NullTime
	var ingressRequestID sql.NullString
	var vendorID sql.NullInt32
	var endpointID sql.NullInt32
	var connectionID sql.NullInt32
	var endpointBaseURL sql.NullString
	var endpointDescription sql.NullString
	var requestBody sql.NullString
	var responseHeaders sql.NullString
	var responseBody sql.NullString
	item := AuditLogDetail{}
	if err := scanner.Scan(&item.ID, &requestLogID, &requestLogCreatedAt, &ingressRequestID, &item.RequestLogMissing, &item.ProfileID, &vendorID, &item.ModelID, &endpointID, &connectionID, &endpointBaseURL, &endpointDescription, &item.RequestMethod, &item.RequestURL, &item.RequestHeaders, &requestBody, &item.RequestBodyStored, &item.ResponseStatus, &responseHeaders, &responseBody, &item.ResponseBodyStored, &item.IsStream, &item.DurationMS, &item.AuditEnabledAtRequest, &item.AuditCaptureBodiesAtRequest, &item.CreatedAt); err != nil {
		return AuditLogDetail{}, err
	}
	item.RequestLogID = nullableInt32(requestLogID)
	item.RequestLogCreatedAt = nullableTime(requestLogCreatedAt)
	item.IngressRequestID = nullableString(ingressRequestID)
	item.VendorID = nullableInt32(vendorID)
	item.EndpointID = nullableInt32(endpointID)
	item.ConnectionID = nullableInt32(connectionID)
	item.EndpointBaseURL = nullableString(endpointBaseURL)
	item.EndpointDescription = nullableString(endpointDescription)
	item.RequestBody = nullableString(requestBody)
	item.ResponseHeaders = nullableString(responseHeaders)
	item.ResponseBody = nullableString(responseBody)
	item.CreatedAt = item.CreatedAt.UTC()
	return item, nil
}

func trimmedPreview(value *string, length int) *string {
	if value == nil {
		return nil
	}
	if length <= 0 {
		trimmed := ""
		return &trimmed
	}
	runes := []rune(*value)
	if len(runes) <= length {
		resolved := *value
		return &resolved
	}
	resolved := string(runes[:length])
	return &resolved
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
