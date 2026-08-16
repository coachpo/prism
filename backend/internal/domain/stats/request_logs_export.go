package stats

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Full filtered CSV export (Requests SPEC §6.8). The handler runs coverage
// resolution + preflight count + row scan inside ONE REPEATABLE READ snapshot,
// spools rows to a 0600 temp file under a 128 MiB hard cap, verifies row/byte
// counts + SHA-256, closes the DB cursor, then streams to the client. No
// partial file is ever produced on typed rejection, spool failure, or drift.

const (
	exportMaxRows       = 100000
	exportMaxSpoolBytes = 128 * 1024 * 1024
	exportMaxRangeDays  = 31
)

// ExportParams mirrors the attempt-view browse query without pagination.
type ExportParams struct {
	RequestLogListParams
	View string
}

// ExportResult is the verified spooled export content.
type ExportResult struct {
	RowCount     int
	ByteCount    int64
	DigestSHA256 string
	View         string
	Content      []byte
}

// ExportCSV exports the full filtered result set. The rows are scanned from
// the provided transaction snapshot (caller must hold one REPEATABLE READ tx).
func ExportCSV(ctx context.Context, tx pgx.Tx, params ExportParams) (ExportResult, error) {
	view := strings.TrimSpace(params.View)
	if view == "" {
		view = "ingress_chains"
	}

	// Resolve the same owner-backed interval as the JSON attempt list and keep
	// the CSV predicates on that exact interval inside this snapshot. Exact
	// ingress selectors waive the 31-day cap, but keep the owner/default bound.
	exactIngress := params.IngressRequestID != nil && strings.TrimSpace(*params.IngressRequestID) != ""
	coverage, err := resolveOrdinaryRequestLogCoverage(ctx, tx, params.RequestLogListParams)
	if err != nil {
		return ExportResult{}, err
	}
	effectiveFrom := coverage.EffectiveFromTime.UTC()
	effectiveTo := coverage.EffectiveToTime.UTC()
	if !exactIngress && effectiveTo.Sub(effectiveFrom) > exportMaxRangeDays*24*time.Hour {
		return ExportResult{}, &HTTPError{StatusCode: 422, Code: "export_range_exceeded", Detail: "Export range must not exceed 31 days."}
	}
	params.FromTime = &effectiveFrom
	params.ToTime = &effectiveTo
	if params.ClientRuleID != nil {
		rule, found, ruleErr := loadCompiledUserAgentRuleByID(ctx, tx, params.ProfileID, *params.ClientRuleID)
		if ruleErr != nil {
			return ExportResult{}, ruleErr
		}
		if !found {
			return ExportResult{}, &HTTPError{StatusCode: 400, Detail: "invalid client_rule_id"}
		}
		params.ClientRulePattern = &rule.RawPattern
	}

	// Preflight count in the same snapshot.
	countQuery, countArgs := buildExportCountQuery(params)
	var matched int
	if err := tx.QueryRow(ctx, countQuery, countArgs...).Scan(&matched); err != nil {
		return ExportResult{}, fmt.Errorf("count export rows: %w", err)
	}
	if matched > exportMaxRows {
		return ExportResult{}, &HTTPError{StatusCode: 422, Code: "request_export_too_large", Detail: "Export exceeds the 100,000 row limit.", Details: map[string]any{"matched_rows": matched, "max_rows": exportMaxRows}}
	}

	// Spool to a 0600 temp file.
	spool, err := os.CreateTemp("", "prism-export-*.csv")
	if err != nil {
		return ExportResult{}, fmt.Errorf("create export spool: %w", err)
	}
	spoolPath := spool.Name()
	defer func() {
		_ = spool.Close()
		_ = os.Remove(spoolPath)
	}()
	if err := os.Chmod(spoolPath, 0o600); err != nil {
		return ExportResult{}, fmt.Errorf("chmod export spool: %w", err)
	}

	rowQuery, rowArgs := buildExportRowQuery(params)
	rows, err := tx.Query(ctx, rowQuery, rowArgs...)
	if err != nil {
		return ExportResult{}, fmt.Errorf("query export rows: %w", err)
	}
	writer := csv.NewWriter(spool)
	// RFC 4180 CSV uses CRLF record terminators; consumers (including the
	// server-side export contract) split on \r\n.
	writer.UseCRLF = true
	writeErr := writeExportRows(ctx, writer, rows, params)
	rows.Close()
	if writeErr != nil {
		return ExportResult{}, writeErr
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return ExportResult{}, fmt.Errorf("flush export spool: %w", err)
	}
	byteCount, digest, err := finalizeExportSpool(spool)
	if err != nil {
		return ExportResult{}, err
	}
	content, err := os.ReadFile(spoolPath)
	if err != nil {
		return ExportResult{}, fmt.Errorf("read export spool: %w", err)
	}
	if int64(len(content)) != byteCount {
		return ExportResult{}, &HTTPError{StatusCode: 422, Code: "export_spool_drift", Detail: "Export spool drifted after verification."}
	}
	return ExportResult{RowCount: matched, ByteCount: byteCount, DigestSHA256: digest, View: view, Content: content}, nil
}

func buildExportCountQuery(params ExportParams) (string, []any) {
	whereClause, args := buildRequestLogBrowseWhere(params.RequestLogListParams)
	return `SELECT COUNT(*) FROM request_logs WHERE ` + whereClause, args
}

func buildExportRowQuery(params ExportParams) (string, []any) {
	whereClause, args := buildRequestLogBrowseWhere(params.RequestLogListParams)
	query := `SELECT id, row_kind, ingress_request_id, model_id, resolved_target_model_id, api_family, operation_name,
		attempt_number, attempt_trigger, attempt_result, is_winner, attempt_duration_ms, legacy_duration_ms, ttft_ms, completion_duration_ms,
		upstream_status_code, gateway_status_code, legacy_status_code,
		error_source, error_code, failure_stage, error_detail, stream_error_detail,
		stream_outcome, stream_error_kind, endpoint_id, connection_id,
		input_tokens, output_tokens, total_tokens, cache_read_input_tokens, cache_creation_input_tokens, reasoning_tokens,
		total_cost_user_currency_micros, currency_code_original, report_currency_code, report_currency_symbol,
		fx_rate_used, fx_rate_source, pricing_status, unpriced_reason, pricing_resolution_kind, missing_price_components,
		pricing_evidence_trust, pricing_template_id_used, pricing_template_name_snapshot, pricing_template_revision_id_used,
		pricing_config_version_used, pricing_version_effective_at, reporting_currency_epoch,
		metadata_redacted_fields, metadata_truncated_fields, created_at
		FROM request_logs WHERE ` + whereClause + ` ` + requestLogOrderBy(params.SortBy, params.SortOrder)
	return query, args
}

// exportHeader is the fixed CSV column allowlist (Requests SPEC §6.8).
var exportHeader = []string{
	"row_kind", "request_log_id", "ingress_request_id", "attempt_number", "attempt_trigger", "attempt_result", "is_winner",
	"created_at", "model_id", "resolved_target_model_id", "api_family", "operation_name", "endpoint_id", "terminal_target_id",
	"upstream_status_code", "gateway_status_code", "legacy_status_code", "error_source", "error_code", "failure_stage",
	"error_detail", "stream_error_detail", "stream_outcome", "stream_error_kind",
	"attempt_duration_ms", "legacy_duration_ms", "ttft_ms", "total_duration_ms", "input_tokens", "output_tokens", "total_tokens",
	"cache_read_input_tokens", "cache_creation_input_tokens", "reasoning_tokens",
	"total_cost_user_currency_micros", "currency_code_original", "report_currency_code", "report_currency_symbol",
	"fx_rate_used", "fx_rate_source", "pricing_status", "unpriced_reason", "pricing_resolution_kind", "missing_price_components",
	"pricing_evidence_trust", "pricing_template_id_used", "pricing_template_name_snapshot", "pricing_template_revision_id_used",
	"pricing_config_version_used", "pricing_version_effective_at", "reporting_currency_epoch",
	"metadata_redacted_fields", "metadata_truncated_fields",
}

type exportRowScanner struct {
	values []string
}

func writeExportRows(ctx context.Context, writer *csv.Writer, rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}, params ExportParams) error {
	if err := writer.Write(exportHeader); err != nil {
		return fmt.Errorf("write export header: %w", err)
	}
	rowCount := 0
	for rows.Next() {
		rowCount++
		if rowCount > exportMaxRows {
			return &HTTPError{StatusCode: 422, Code: "request_export_too_large", Detail: "Export exceeds the 100,000 row limit."}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		var record exportRowRecord
		var resolvedTargetModelID, operationName, errorSource, errorCode, failureStage, errorDetail, streamErrorDetail, streamErrorKind, unpricedReason, resolutionKind, currencyCodeOriginal, reportCurrencyCode, reportCurrencySymbol, fxRateUsed, fxRateSource, templateNameSnapshot *string
		if err := rows.Scan(
			&record.ID, &record.RowKind, &record.IngressRequestID, &record.ModelID, &resolvedTargetModelID, &record.APIFamily, &operationName,
			&record.AttemptNumber, &record.AttemptTrigger, &record.AttemptResult, &record.IsWinner, &record.AttemptDurationMS, &record.LegacyDurationMS, &record.TTFTMS, &record.CompletionDurationMS,
			&record.UpstreamStatusCode, &record.GatewayStatusCode, &record.LegacyStatusCode,
			&errorSource, &errorCode, &failureStage, &errorDetail, &streamErrorDetail,
			&record.StreamOutcome, &streamErrorKind, &record.EndpointID, &record.ConnectionID,
			&record.InputTokens, &record.OutputTokens, &record.TotalTokens, &record.CacheReadInputTokens, &record.CacheCreationInputTokens, &record.ReasoningTokens,
			&record.TotalCostUserCurrencyMicros, &currencyCodeOriginal, &reportCurrencyCode, &reportCurrencySymbol,
			&fxRateUsed, &fxRateSource, &record.PricingStatus, &unpricedReason, &resolutionKind, &record.MissingPriceComponents,
			&record.PricingEvidenceTrust, &record.PricingTemplateIDUsed, &templateNameSnapshot, &record.PricingTemplateRevisionIDUsed,
			&record.PricingConfigVersionUsed, &record.PricingVersionEffectiveAt, &record.ReportingCurrencyEpoch,
			&record.MetadataRedactedFields, &record.MetadataTruncatedFields, &record.CreatedAt,
		); err != nil {
			return fmt.Errorf("scan export row: %w", err)
		}
		record.ResolvedTargetModelID = resolvedTargetModelID
		record.OperationName = derefExportString(operationName)
		record.ErrorSource = errorSource
		record.ErrorCode = errorCode
		record.FailureStage = failureStage
		record.ErrorDetail = errorDetail
		record.StreamErrorDetail = streamErrorDetail
		record.StreamErrorKind = streamErrorKind
		record.UnpricedReason = unpricedReason
		record.PricingResolutionKind = resolutionKind
		record.CurrencyCodeOriginal = currencyCodeOriginal
		record.ReportCurrencyCode = reportCurrencyCode
		record.ReportCurrencySymbol = reportCurrencySymbol
		record.FXRateUsed = fxRateUsed
		record.FXRateSource = fxRateSource
		record.PricingTemplateNameSnapshot = templateNameSnapshot
		cells := record.csvCells()
		safeCells := make([]string, len(cells))
		for index, cell := range cells {
			safeCells[index] = neutraliseCSVFormula(cell)
		}
		if err := writer.Write(safeCells); err != nil {
			return fmt.Errorf("write export row: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate export rows: %w", err)
	}
	return nil
}

// neutraliseCSVFormula detects leading whitespace/tab/CR/LF and apostrophe-
// neutralises the first meaningful character when it is = + - @ (Requests
// SPEC §6.8 formula-injection rule).
func neutraliseCSVFormula(cell string) string {
	index := 0
	prefix := ""
	for index < len(cell) && (cell[index] == ' ' || cell[index] == '\t' || cell[index] == '\r' || cell[index] == '\n') {
		prefix += string(cell[index])
		index++
	}
	if index >= len(cell) {
		return cell
	}
	switch cell[index] {
	case '=', '+', '-', '@':
		return prefix + "'" + cell[index:]
	default:
		return cell
	}
}

type exportRowRecord struct {
	ID                            int64
	RowKind                       string
	IngressRequestID              string
	ModelID                       string
	ResolvedTargetModelID         *string
	APIFamily                     string
	OperationName                 string
	AttemptNumber                 *int
	AttemptTrigger                *string
	AttemptResult                 *string
	IsWinner                      *bool
	AttemptDurationMS             *int
	LegacyDurationMS              *int
	TTFTMS                        *int
	CompletionDurationMS          *int
	UpstreamStatusCode            *int
	GatewayStatusCode             *int
	LegacyStatusCode              *int
	ErrorSource                   *string
	ErrorCode                     *string
	FailureStage                  *string
	ErrorDetail                   *string
	StreamErrorDetail             *string
	StreamOutcome                 string
	StreamErrorKind               *string
	EndpointID                    *int
	ConnectionID                  *int
	InputTokens                   *int
	OutputTokens                  *int
	TotalTokens                   *int
	CacheReadInputTokens          *int
	CacheCreationInputTokens      *int
	ReasoningTokens               *int
	TotalCostUserCurrencyMicros   *int64
	CurrencyCodeOriginal          *string
	ReportCurrencyCode            *string
	ReportCurrencySymbol          *string
	FXRateUsed                    *string
	FXRateSource                  *string
	PricingStatus                 string
	UnpricedReason                *string
	PricingResolutionKind         *string
	MissingPriceComponents        []string
	PricingEvidenceTrust          string
	PricingTemplateIDUsed         *int
	PricingTemplateNameSnapshot   *string
	PricingTemplateRevisionIDUsed *int64
	PricingConfigVersionUsed      *int
	PricingVersionEffectiveAt     *time.Time
	ReportingCurrencyEpoch        *int
	MetadataRedactedFields        []string
	MetadataTruncatedFields       []string
	CreatedAt                     time.Time
}

func (record exportRowRecord) csvCells() []string {
	missingComponents := ""
	if len(record.MissingPriceComponents) > 0 {
		raw, _ := json.Marshal(record.MissingPriceComponents)
		missingComponents = string(raw)
	}
	redacted := "[]"
	if len(record.MetadataRedactedFields) > 0 {
		raw, _ := json.Marshal(record.MetadataRedactedFields)
		redacted = string(raw)
	}
	truncated := "[]"
	if len(record.MetadataTruncatedFields) > 0 {
		raw, _ := json.Marshal(record.MetadataTruncatedFields)
		truncated = string(raw)
	}
	effectiveAt := ""
	if record.PricingVersionEffectiveAt != nil {
		effectiveAt = record.PricingVersionEffectiveAt.UTC().Format(time.RFC3339)
	}
	return []string{
		record.RowKind,
		int64String(record.ID),
		record.IngressRequestID,
		optionalIntString(record.AttemptNumber),
		optionalString(record.AttemptTrigger),
		optionalString(record.AttemptResult),
		optionalBoolString(record.IsWinner),
		record.CreatedAt.UTC().Format(time.RFC3339),
		record.ModelID,
		optionalString(record.ResolvedTargetModelID),
		record.APIFamily,
		record.OperationName,
		optionalIntString(record.EndpointID),
		optionalIntString(record.ConnectionID),
		optionalIntString(record.UpstreamStatusCode),
		optionalIntString(record.GatewayStatusCode),
		optionalIntString(record.LegacyStatusCode),
		optionalString(record.ErrorSource),
		optionalString(record.ErrorCode),
		optionalString(record.FailureStage),
		optionalString(record.ErrorDetail),
		optionalString(record.StreamErrorDetail),
		record.StreamOutcome,
		optionalString(record.StreamErrorKind),
		optionalIntString(record.AttemptDurationMS),
		optionalIntString(record.LegacyDurationMS),
		optionalIntString(record.TTFTMS),
		optionalIntString(record.CompletionDurationMS),
		optionalIntString(record.InputTokens),
		optionalIntString(record.OutputTokens),
		optionalIntString(record.TotalTokens),
		optionalIntString(record.CacheReadInputTokens),
		optionalIntString(record.CacheCreationInputTokens),
		optionalIntString(record.ReasoningTokens),
		optionalInt64String(record.TotalCostUserCurrencyMicros),
		optionalString(record.CurrencyCodeOriginal),
		optionalString(record.ReportCurrencyCode),
		optionalString(record.ReportCurrencySymbol),
		optionalString(record.FXRateUsed),
		optionalString(record.FXRateSource),
		record.PricingStatus,
		optionalString(record.UnpricedReason),
		optionalString(record.PricingResolutionKind),
		missingComponents,
		record.PricingEvidenceTrust,
		optionalIntString(record.PricingTemplateIDUsed),
		optionalString(record.PricingTemplateNameSnapshot),
		optionalInt64String(record.PricingTemplateRevisionIDUsed),
		optionalIntString(record.PricingConfigVersionUsed),
		effectiveAt,
		optionalIntString(record.ReportingCurrencyEpoch),
		redacted,
		truncated,
	}
}

func derefExportString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func int64String(value int64) string { return fmt.Sprintf("%d", value) }

func optionalIntString(value *int) string {
	if value == nil {
		return ""
	}
	return fmt.Sprintf("%d", *value)
}

func optionalInt64String(value *int64) string {
	if value == nil {
		return ""
	}
	return fmt.Sprintf("%d", *value)
}

func optionalString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func optionalBoolString(value *bool) string {
	if value == nil {
		return ""
	}
	if *value {
		return "true"
	}
	return "false"
}

// finalizeExportSpool verifies spool byte count and computes the SHA-256.
func finalizeExportSpool(spool *os.File) (int64, string, error) {
	if err := spool.Sync(); err != nil {
		return 0, "", fmt.Errorf("sync export spool: %w", err)
	}
	info, err := spool.Stat()
	if err != nil {
		return 0, "", fmt.Errorf("stat export spool: %w", err)
	}
	if info.Size() > exportMaxSpoolBytes {
		return 0, "", &HTTPError{StatusCode: 422, Code: "export_spool_too_large", Detail: "Export spool exceeds the 128 MiB limit."}
	}
	if _, err := spool.Seek(0, io.SeekStart); err != nil {
		return 0, "", fmt.Errorf("seek export spool: %w", err)
	}
	digest, err := sha256File(spool)
	if err != nil {
		return 0, "", err
	}
	return info.Size(), digest, nil
}

func sha256File(file *os.File) (string, error) {
	hasher := sha256.New()
	buffer := make([]byte, 64*1024)
	for {
		n, err := file.Read(buffer)
		if n > 0 {
			_, _ = hasher.Write(buffer[:n])
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("hash export spool: %w", err)
		}
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("rewind export spool: %w", err)
	}
	return fmt.Sprintf("%x", hasher.Sum(nil)), nil
}
