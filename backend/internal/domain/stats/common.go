package stats

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const rollingWindowMinutes = 30

// scopedRequestLogStatusSQL resolves the row-scoped HTTP status for a
// request_logs row: upstream rows use the real upstream HTTP status,
// planning/admission diagnostic rows use the synthesized gateway status, and
// legacy rows use the un-scoped legacy projection. No layer may COALESCE a
// numeric status across scopes (Requests SPEC §6.4/§6.9).
const scopedRequestLogStatusSQL = `CASE row_kind
	WHEN 'upstream' THEN upstream_status_code
	WHEN 'planning' THEN gateway_status_code
	WHEN 'admission' THEN gateway_status_code
	ELSE legacy_status_code
END`

// scopedRequestLogDurationSQL resolves the row-scoped end-to-end duration.
// Upstream rows prefer completion_duration_ms (the true stream finalization
// time); non-stream rows leave that column NULL and fall back to
// attempt_duration_ms; remaining rows use the legacy projection. Never fall
// back to attempt_duration_ms for streams: it only reaches response headers
// and understates real duration by one to two orders of magnitude.
const scopedRequestLogDurationSQL = `CASE WHEN row_kind = 'upstream'
	THEN COALESCE(completion_duration_ms, attempt_duration_ms)
	ELSE legacy_duration_ms END`

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

type usageEventRecord struct {
	ID                             int
	CreatedAt                      time.Time
	ProfileID                      int
	IngressRequestID               string
	ModelID                        string
	ResolvedTargetModelID          *string
	APIFamily                      string
	EndpointID                     *int
	EndpointLabelSnapshot          string
	ConnectionID                   *int
	ProxyAPIKeyID                  *int
	ProxyAPIKeyNameSnapshot        *string
	ProxyKeyAttributionState       string
	ProxyKeyAuthEnforcedAtRequest  *bool
	StatusCode                     int
	SuccessFlag                    bool
	PricingStatus                  string
	PricingEvidenceTrust           string
	UnpricedReason                 *string
	InputTokens                    int
	OutputTokens                   int
	HasOutputTokens                bool
	TotalTokens                    int
	CacheReadInputTokens           int
	CacheCreationInputTokens       int
	ReasoningTokens                int
	TotalCostUserCurrencyMicros    int64
	HasTotalCostUserCurrencyMicros bool
	AttemptCount                   int
	FinalAttemptNumber             *int
	RequestPath                    string
	StreamOutcome                  string
	ResponseTimeMS                 *int
	TTFTMS                         *int
	CompletionDurationMS           *int
	CurrentModelLabel              *string
	CurrentEndpointName            *string
	CurrentEndpointBaseURL         *string
	CurrentProxyAPIKeyName         *string
	CurrentProxyAPIKeyPrefix       *string
}

// Priced reports whether the record is in the priced four-state bucket.
func (record usageEventRecord) Priced() bool {
	return record.PricingStatus == "priced"
}

// Unpriced reports whether the record is in the unpriced four-state bucket.
func (record usageEventRecord) Unpriced() bool {
	return record.PricingStatus == "unpriced"
}

// TrustedKnownCost reports whether the record carries a canonical trusted
// known cost that may enter sums/sorts (priced + trusted only; legacy
// untrusted evidence never sums).
func (record usageEventRecord) TrustedKnownCost() bool {
	return record.PricingStatus == "priced" && record.PricingEvidenceTrust == "trusted"
}

type snapshotEvent struct {
	APIFamily                string
	AttemptCount             int
	PricingStatus            string
	CacheReadInputTokens     int
	CacheCreationInputTokens int
	ConnectionID             *int
	CreatedAt                time.Time
	EndpointID               *int
	EndpointLabel            string
	IngressRequestID         string
	InputTokens              int
	ModelID                  string
	ModelLabel               string
	OutputTokens             int
	ProxyAPIKeyID            *int
	ProxyAPIKeyLabel         *string
	ProxyAPIKeyStatsLabel    string
	ProxyKeyAttributionState string
	ProxyAPIKeyPrefix        *string
	ReasoningTokens          int
	RequestPath              string
	ResolvedTargetModelID    *string
	StatusCode               int
	SuccessFlag              bool
	ResponseTimeMS           *int
	TTFTMS                   *int
	CompletionDurationMS     *int
	HasOutputTokens          bool
	TotalCostMicros          int64
	HasKnownCost             bool
	TotalTokens              int
}

// priced reports whether the snapshot event is in the priced bucket.
func (event snapshotEvent) priced() bool {
	return event.PricingStatus == "priced"
}

func loadReportCurrencyPreferences(ctx context.Context, exec queryExecutor, profileID int) (string, string, error) {
	var code string
	var symbol string
	err := exec.QueryRow(ctx, `SELECT report_currency_code, report_currency_symbol FROM user_settings WHERE profile_id = $1 ORDER BY id ASC LIMIT 1`, profileID).Scan(&code, &symbol)
	if err == nil {
		return code, symbol, nil
	}
	if err == pgx.ErrNoRows {
		return "USD", "$", nil
	}
	return "", "", fmt.Errorf("load report currency preferences for profile %d: %w", profileID, err)
}

func usageEventEndpointLabel(record usageEventRecord) string {
	if label := strings.TrimSpace(record.EndpointLabelSnapshot); label != "" {
		return label
	}
	return "Unknown Endpoint"
}

func nullableString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	resolved := value.String
	return &resolved
}

func nullableBool(value sql.NullBool) *bool {
	if !value.Valid {
		return nil
	}
	resolved := value.Bool
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

func boolValue(value *bool) bool {
	return value != nil && *value
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func intValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func int64Value(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}
