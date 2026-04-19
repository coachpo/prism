package audit

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type HTTPError struct {
	StatusCode int
	Detail     string
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
	Offset       int
}

type DeleteParams struct {
	ProfileID     int
	Before        *time.Time
	OlderThanDays *int
	DeleteAll     bool
	ReferenceNow  time.Time
}

type AuditLogListItem struct {
	ID                  int       `json:"id"`
	RequestLogID        *int      `json:"request_log_id"`
	ProfileID           int       `json:"profile_id"`
	VendorID            *int      `json:"vendor_id"`
	ModelID             string    `json:"model_id"`
	EndpointID          *int      `json:"endpoint_id"`
	ConnectionID        *int      `json:"connection_id"`
	EndpointBaseURL     *string   `json:"endpoint_base_url"`
	EndpointDescription *string   `json:"endpoint_description"`
	RequestMethod       string    `json:"request_method"`
	RequestURL          string    `json:"request_url"`
	RequestHeaders      string    `json:"request_headers"`
	RequestBodyPreview  *string   `json:"request_body_preview"`
	ResponseStatus      int       `json:"response_status"`
	IsStream            bool      `json:"is_stream"`
	DurationMS          int       `json:"duration_ms"`
	CreatedAt           time.Time `json:"created_at"`
}

type AuditLogDetail struct {
	ID                  int       `json:"id"`
	RequestLogID        *int      `json:"request_log_id"`
	ProfileID           int       `json:"profile_id"`
	VendorID            *int      `json:"vendor_id"`
	ModelID             string    `json:"model_id"`
	EndpointID          *int      `json:"endpoint_id"`
	ConnectionID        *int      `json:"connection_id"`
	EndpointBaseURL     *string   `json:"endpoint_base_url"`
	EndpointDescription *string   `json:"endpoint_description"`
	RequestMethod       string    `json:"request_method"`
	RequestURL          string    `json:"request_url"`
	RequestHeaders      string    `json:"request_headers"`
	RequestBody         *string   `json:"request_body"`
	ResponseStatus      int       `json:"response_status"`
	ResponseHeaders     *string   `json:"response_headers"`
	ResponseBody        *string   `json:"response_body"`
	IsStream            bool      `json:"is_stream"`
	DurationMS          int       `json:"duration_ms"`
	CreatedAt           time.Time `json:"created_at"`
}

type AuditLogListResponse struct {
	Items  []AuditLogListItem `json:"items"`
	Total  int                `json:"total"`
	Limit  int                `json:"limit"`
	Offset int                `json:"offset"`
}

func ListLogs(ctx context.Context, exec queryExecutor, params ListParams) (AuditLogListResponse, error) {
	limit := params.Limit
	if limit <= 0 {
		limit = 50
	}
	offset := params.Offset
	if offset < 0 {
		offset = 0
	}
	whereClause, args := buildListWhere(params)
	var total int
	if err := exec.QueryRow(ctx, `SELECT COUNT(*) FROM audit_logs WHERE `+whereClause, args...).Scan(&total); err != nil {
		return AuditLogListResponse{}, fmt.Errorf("count audit logs for profile %d: %w", params.ProfileID, err)
	}
	rows, err := exec.Query(ctx, `SELECT id, request_log_id, profile_id, vendor_id, model_id, endpoint_id, connection_id, endpoint_base_url, endpoint_description, request_method, request_url, request_headers, request_body, response_status, is_stream, duration_ms, created_at FROM audit_logs WHERE `+whereClause+` ORDER BY created_at DESC LIMIT $`+fmt.Sprintf("%d", len(args)+1)+` OFFSET $`+fmt.Sprintf("%d", len(args)+2), append(append(args, limit), offset)...)
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
	return AuditLogListResponse{Items: items, Total: total, Limit: limit, Offset: offset}, nil
}

func GetLog(ctx context.Context, exec queryExecutor, profileID int, logID int) (*AuditLogDetail, error) {
	row := exec.QueryRow(ctx, `SELECT id, request_log_id, profile_id, vendor_id, model_id, endpoint_id, connection_id, endpoint_base_url, endpoint_description, request_method, request_url, request_headers, request_body, response_status, response_headers, response_body, is_stream, duration_ms, created_at FROM audit_logs WHERE profile_id = $1 AND id = $2 LIMIT 1`, profileID, logID)
	item, err := scanDetail(row)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load audit log %d for profile %d: %w", logID, profileID, err)
	}
	return &item, nil
}

func DeleteLogs(ctx context.Context, exec queryExecutor, params DeleteParams) error {
	provided := 0
	if params.Before != nil {
		provided++
	}
	if params.OlderThanDays != nil {
		provided++
	}
	if params.DeleteAll {
		provided++
	}
	if provided != 1 {
		return &HTTPError{StatusCode: 400, Detail: "Provide exactly one of 'before', 'older_than_days', or 'delete_all'"}
	}
	if params.DeleteAll {
		if _, err := exec.Exec(ctx, `DELETE FROM audit_logs WHERE profile_id = $1`, params.ProfileID); err != nil {
			return fmt.Errorf("delete audit logs for profile %d: %w", params.ProfileID, err)
		}
		return nil
	}
	if params.OlderThanDays != nil {
		cutoff := params.ReferenceNow.UTC().Add(-time.Duration(*params.OlderThanDays) * 24 * time.Hour)
		if _, err := exec.Exec(ctx, `DELETE FROM audit_logs WHERE profile_id = $1 AND created_at < $2`, params.ProfileID, cutoff); err != nil {
			return fmt.Errorf("delete audit logs older than %d days for profile %d: %w", *params.OlderThanDays, params.ProfileID, err)
		}
		return nil
	}
	if _, err := exec.Exec(ctx, `DELETE FROM audit_logs WHERE profile_id = $1 AND created_at < $2`, params.ProfileID, params.Before.UTC()); err != nil {
		return fmt.Errorf("delete audit logs before %s for profile %d: %w", params.Before.UTC().Format(time.RFC3339), params.ProfileID, err)
	}
	return nil
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
		clauses = append(clauses, fmt.Sprintf("created_at <= $%d", len(args)))
	}
	return strings.Join(clauses, " AND "), args
}

func scanListItem(scanner interface{ Scan(...any) error }) (AuditLogListItem, error) {
	var requestLogID sql.NullInt32
	var vendorID sql.NullInt32
	var endpointID sql.NullInt32
	var connectionID sql.NullInt32
	var endpointBaseURL sql.NullString
	var endpointDescription sql.NullString
	var requestBody sql.NullString
	item := AuditLogListItem{}
	if err := scanner.Scan(&item.ID, &requestLogID, &item.ProfileID, &vendorID, &item.ModelID, &endpointID, &connectionID, &endpointBaseURL, &endpointDescription, &item.RequestMethod, &item.RequestURL, &item.RequestHeaders, &requestBody, &item.ResponseStatus, &item.IsStream, &item.DurationMS, &item.CreatedAt); err != nil {
		return AuditLogListItem{}, fmt.Errorf("scan audit-log list row: %w", err)
	}
	item.RequestLogID = nullableInt32(requestLogID)
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
	var vendorID sql.NullInt32
	var endpointID sql.NullInt32
	var connectionID sql.NullInt32
	var endpointBaseURL sql.NullString
	var endpointDescription sql.NullString
	var requestBody sql.NullString
	var responseHeaders sql.NullString
	var responseBody sql.NullString
	item := AuditLogDetail{}
	if err := scanner.Scan(&item.ID, &requestLogID, &item.ProfileID, &vendorID, &item.ModelID, &endpointID, &connectionID, &endpointBaseURL, &endpointDescription, &item.RequestMethod, &item.RequestURL, &item.RequestHeaders, &requestBody, &item.ResponseStatus, &responseHeaders, &responseBody, &item.IsStream, &item.DurationMS, &item.CreatedAt); err != nil {
		return AuditLogDetail{}, err
	}
	item.RequestLogID = nullableInt32(requestLogID)
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

func nullableInt32(value sql.NullInt32) *int {
	if !value.Valid {
		return nil
	}
	resolved := int(value.Int32)
	return &resolved
}
