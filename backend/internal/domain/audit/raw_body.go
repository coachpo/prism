package audit

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// RawBodyDirection selects the audit body half for the raw download routes.
type RawBodyDirection string

const (
	RawBodyDirectionRequest  RawBodyDirection = "request"
	RawBodyDirectionResponse RawBodyDirection = "response"
)

// RawBodyResult is the exact stored BYTEA prefix plus capture provenance.
type RawBodyResult struct {
	Body                  []byte
	CaptureStatus         string
	CaptureEndState       *string
	BytesObserved         *int64
	BytesStored           *int64
	Truncated             bool
	OriginalContentType   *string
	ContentTypeState      string
	AuditEnabledAtRequest bool
}

// LoadRawAuditBody returns the exact stored BYTEA prefix for one direction.
func LoadRawAuditBody(ctx context.Context, exec queryExecutor, profileID int, logID int, direction RawBodyDirection) (*RawBodyResult, bool, error) {
	_, retentionFloor, err := loadAuditRetentionReadGate(ctx, exec, time.Now().UTC())
	if err != nil {
		return nil, false, err
	}
	var createdAt time.Time
	if err := exec.QueryRow(ctx, `SELECT created_at FROM audit_logs WHERE profile_id = $1 AND id = $2 ORDER BY created_at DESC LIMIT 1`, profileID, logID).Scan(&createdAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("load raw audit body %d timestamp for profile %d: %w", logID, profileID, err)
	}
	if retentionFloor != nil && createdAt.Before(*retentionFloor) {
		return nil, false, &HTTPError{StatusCode: 410, Code: "audit_evidence_revoked", Detail: "Audit evidence is no longer available."}
	}
	var body []byte
	var captureStatus sql.NullString
	var captureEndState sql.NullString
	var bytesObserved sql.NullInt64
	var bytesStored sql.NullInt64
	var truncated bool
	var originalContentType sql.NullString
	var auditEnabled bool
	column := "request_body"
	if direction == RawBodyDirectionResponse {
		column = "response_body"
	}
	query := fmt.Sprintf(`SELECT %s, %s_capture_status, %s_capture_end_state, %s_bytes_observed, %s_bytes_stored, %s_truncated, audit_enabled_at_request,
		CASE WHEN %s = 'request_body' THEN request_method || ' ' || request_url ELSE '' END AS original_content_type
		FROM audit_logs WHERE profile_id = $1 AND id = $2 AND created_at >= COALESCE($3::timestamptz, '-infinity'::timestamptz) ORDER BY created_at DESC LIMIT 1`,
		column, column, column, column, column, column, column)
	row := exec.QueryRow(ctx, query, profileID, logID, retentionFloor)
	var contentTypeHint sql.NullString
	if err := row.Scan(&body, &captureStatus, &captureEndState, &bytesObserved, &bytesStored, &truncated, &auditEnabled, &contentTypeHint); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("load raw audit body %d for profile %d: %w", logID, profileID, err)
	}
	_ = originalContentType
	_ = contentTypeHint
	result := &RawBodyResult{
		Body:                  body,
		CaptureStatus:         stringValue(nullableString(captureStatus)),
		CaptureEndState:       nullableString(captureEndState),
		BytesObserved:         nullableInt64(bytesObserved),
		BytesStored:           nullableInt64(bytesStored),
		Truncated:             truncated,
		ContentTypeState:      "absent",
		AuditEnabledAtRequest: auditEnabled,
	}
	return result, true, nil
}
