package startup

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/domain/safediag"
)

// requestAuditBackfillOwner is the crash-resumable background-jobs owner
// that scrubs legacy retained observability data after the v1 drain and
// before legacy read routes activate (Requests SPEC §5.6).
//
// Three domains run per profile in batches of at most 500 rows keyed by
// (created_at, id):
//   - request_urls:   rewrite request_logs.endpoint_base_url and
//     audit_logs.endpoint_base_url/request_url with the fixed §4.3 URL
//     scrubber and write url_scrub_provenance.
//   - request_metadata: irreversibly scrub/cap caller/upstream User-Agent,
//     provider correlation IDs, and other §4.3 external metadata, and write
//     the metadata_redacted_fields/metadata_truncated_fields arrays.
//   - audit_headers_urls: rewrite legacy audit header raw shadows into
//     scrubbed JSONB targets (all-values-redacted when no verifiable
//     request-time snapshot exists) and null the raw shadows transactionally.
//
// A domain is ready only after its scan is complete AND its raw shadows are
// null; read routes stay unavailable (503 observability_v2_backfill_in_progress)
// until the domain is ready. The upgrade gate is the persisted
// observability_v2_upgrade_state row and the durable per-profile checkpoints
// live in observability_v2_backfill_state; those database identifiers are
// intentionally unchanged by source-file naming work.
type requestAuditBackfillOwner struct {
	now func() time.Time
}

func newRequestAuditBackfillOwner(now func() time.Time) *requestAuditBackfillOwner {
	if now == nil {
		now = time.Now
	}
	return &requestAuditBackfillOwner{now: now}
}

const backfillBatchSize = 500

var backfillDomains = []string{"request_urls", "request_metadata", "audit_headers_urls"}

// requestURLsAuditPhaseCursor stores the audit_logs phase in the existing
// request_urls checkpoint without changing the persisted schema. PostgreSQL
// IDs are positive, so a negative last_id unambiguously means that the
// request_logs scan is complete and the encoded audit_logs cursor is active.
func requestURLsAuditPhaseCursor(id int64) int64 { return -id }

func isRequestURLsAuditPhaseCursor(id *int64) bool {
	return id != nil && *id < 0
}

func decodeRequestURLsAuditPhaseCursor(id int64) int64 { return -id }

// backfillDomainState is a durable checkpoint row.
type backfillDomainState struct {
	ProfileID         int
	Domain            string
	Status            string
	LastCreatedAt     *time.Time
	LastID            *int64
	LastSafeErrorCode *string
}

// EnsureAllDomainsReady runs the backfill for all profiles until every domain
// is ready. It is idempotent and crash-resumable.
func (owner *requestAuditBackfillOwner) EnsureAllDomainsReady(ctx context.Context, tx pgx.Tx) (complete bool, err error) {
	// Verify the upgrade is past the v1 drain before backfilling.
	var state string
	if err := tx.QueryRow(ctx, `SELECT state FROM observability_v2_upgrade_state WHERE id = 1`).Scan(&state); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return true, nil
		}
		return false, fmt.Errorf("load observability v2 upgrade state: %w", err)
	}
	if state == "final" || state == "backfill_ready" {
		return true, nil
	}
	if state == "draining_v1" {
		return false, nil
	}

	profiles, err := loadBackfillProfiles(ctx, tx)
	if err != nil {
		return false, err
	}
	allReady := true
	for _, profileID := range profiles {
		for _, domain := range backfillDomains {
			ready, runErr := owner.ensureDomainReady(ctx, tx, profileID, domain)
			if runErr != nil {
				return false, runErr
			}
			if !ready {
				allReady = false
			}
		}
	}
	if allReady {
		if _, err := tx.Exec(ctx, `UPDATE observability_v2_upgrade_state SET state = 'backfill_ready', writer_fence_active = true, updated_at = now() WHERE id = 1`); err != nil {
			return false, fmt.Errorf("advance backfill state: %w", err)
		}
		return true, nil
	}
	return false, nil
}

func loadBackfillProfiles(ctx context.Context, tx pgx.Tx) ([]int, error) {
	rows, err := tx.Query(ctx, `SELECT id FROM profiles ORDER BY id ASC`)
	if err != nil {
		return nil, fmt.Errorf("load backfill profiles: %w", err)
	}
	defer rows.Close()
	profiles := make([]int, 0)
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan backfill profile: %w", err)
		}
		profiles = append(profiles, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate backfill profiles: %w", err)
	}
	if len(profiles) == 0 {
		profiles = []int{1}
	}
	return profiles, nil
}

func (owner *requestAuditBackfillOwner) ensureDomainReady(ctx context.Context, tx pgx.Tx, profileID int, domain string) (bool, error) {
	state := loadBackfillDomainState(ctx, tx, profileID, domain)
	switch state.Status {
	case "ready":
		return true, nil
	case "failed":
		// Permanent invariant: stay failed/unavailable rather than skip raw
		// rows. The operator must resolve the underlying data issue.
		return false, fmt.Errorf("backfill domain %s for profile %d is failed", domain, profileID)
	}
	if err := upsertBackfillDomainStatus(ctx, tx, profileID, domain, "running", state.LastCreatedAt, state.LastID); err != nil {
		return false, err
	}
	processed, err := owner.runBackfillBatch(ctx, tx, profileID, domain, state.LastCreatedAt, state.LastID)
	if err != nil {
		_ = upsertBackfillDomainStatus(ctx, tx, profileID, domain, "running", state.LastCreatedAt, state.LastID)
		return false, err
	}
	if !processed {
		// Scan complete: verify raw shadows are null before ready.
		shadowsNull, err := domainRawShadowsNull(ctx, tx, profileID, domain)
		if err != nil {
			_ = upsertBackfillDomainStatus(ctx, tx, profileID, domain, "failed", state.LastCreatedAt, state.LastID)
			return false, err
		}
		if !shadowsNull {
			_ = upsertBackfillDomainStatus(ctx, tx, profileID, domain, "failed", state.LastCreatedAt, state.LastID)
			return false, fmt.Errorf("backfill domain %s for profile %d finished with non-null raw shadows", domain, profileID)
		}
		if err := markBackfillDomainReady(ctx, tx, profileID, domain); err != nil {
			return false, err
		}
		return true, nil
	}
	// More rows remain; the checkpoint was advanced in the same transaction.
	return false, nil
}

func loadBackfillDomainState(ctx context.Context, tx pgx.Tx, profileID int, domain string) backfillDomainState {
	state := backfillDomainState{ProfileID: profileID, Domain: domain, Status: "pending"}
	err := tx.QueryRow(ctx, `SELECT status, last_created_at, last_id, last_safe_error_code FROM observability_v2_backfill_state WHERE profile_id = $1 AND domain = $2`, profileID, domain).
		Scan(&state.Status, &state.LastCreatedAt, &state.LastID, &state.LastSafeErrorCode)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		state.Status = "unavailable"
	}
	return state
}

func upsertBackfillDomainStatus(ctx context.Context, tx pgx.Tx, profileID int, domain string, status string, lastCreatedAt *time.Time, lastID *int64) error {
	_, err := tx.Exec(ctx, `INSERT INTO observability_v2_backfill_state (profile_id, domain, status, last_created_at, last_id, updated_at)
		VALUES ($1, $2, $3, $4, $5, now())
		ON CONFLICT (profile_id, domain) DO UPDATE SET status = EXCLUDED.status, last_created_at = EXCLUDED.last_created_at, last_id = EXCLUDED.last_id, updated_at = now()`,
		profileID, domain, status, lastCreatedAt, lastID)
	if err != nil {
		return fmt.Errorf("upsert backfill domain %s state for profile %d: %w", domain, profileID, err)
	}
	return nil
}

func markBackfillDomainReady(ctx context.Context, tx pgx.Tx, profileID int, domain string) error {
	_, err := tx.Exec(ctx, `UPDATE observability_v2_backfill_state SET status = 'ready', updated_at = now() WHERE profile_id = $1 AND domain = $2`, profileID, domain)
	if err != nil {
		return fmt.Errorf("mark backfill domain %s ready for profile %d: %w", domain, profileID, err)
	}
	return nil
}

func domainRawShadowsNull(ctx context.Context, tx pgx.Tx, profileID int, domain string) (bool, error) {
	var count int
	var err error
	switch domain {
	case "audit_headers_urls":
		exists, existsErr := auditRawShadowColumnsExist(ctx, tx)
		if existsErr != nil {
			return false, existsErr
		}
		if !exists {
			// 000011 already dropped the shadows: the domain is drained.
			return true, nil
		}
		err = tx.QueryRow(ctx, `SELECT COUNT(*) FROM audit_logs WHERE profile_id = $1 AND (request_headers_legacy_raw_text IS NOT NULL OR response_headers_legacy_raw_text IS NOT NULL)`, profileID).Scan(&count)
	default:
		// request_urls / request_metadata have no raw shadow columns.
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("check raw shadows for domain %s profile %d: %w", domain, profileID, err)
	}
	return count == 0, nil
}

func auditRawShadowColumnsExist(ctx context.Context, tx pgx.Tx) (bool, error) {
	var count int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM information_schema.columns WHERE table_name = 'audit_logs' AND column_name = 'request_headers_legacy_raw_text'`).Scan(&count); err != nil {
		return false, fmt.Errorf("check audit raw shadow columns: %w", err)
	}
	return count > 0, nil
}

func (owner *requestAuditBackfillOwner) runBackfillBatch(ctx context.Context, tx pgx.Tx, profileID int, domain string, afterCreatedAt *time.Time, afterID *int64) (processed bool, err error) {
	switch domain {
	case "request_urls":
		if isRequestURLsAuditPhaseCursor(afterID) {
			auditAfterID := decodeRequestURLsAuditPhaseCursor(*afterID)
			return owner.backfillAuditURLs(ctx, tx, profileID, afterCreatedAt, &auditAfterID)
		}
		return owner.backfillRequestURLs(ctx, tx, profileID, afterCreatedAt, afterID)
	case "request_metadata":
		return owner.backfillRequestMetadata(ctx, tx, profileID, afterCreatedAt, afterID)
	case "audit_headers_urls":
		return owner.backfillAuditHeadersURLs(ctx, tx, profileID, afterCreatedAt, afterID)
	default:
		return false, fmt.Errorf("unknown backfill domain %q", domain)
	}
}

// backfillRequestURLs rewrites legacy request/audit URLs with the fixed
// §4.3 scrubber and writes url_scrub_provenance.
func (owner *requestAuditBackfillOwner) backfillRequestURLs(ctx context.Context, tx pgx.Tx, profileID int, afterCreatedAt *time.Time, afterID *int64) (bool, error) {
	rows, err := tx.Query(ctx, `SELECT id, created_at, endpoint_base_url FROM request_logs
		WHERE profile_id = $1 AND url_scrub_provenance = 'legacy_unknown'
		AND ($2::timestamptz IS NULL OR created_at > $2 OR (created_at = $2 AND id > $3))
		ORDER BY created_at ASC, id ASC LIMIT `+fmt.Sprintf("%d", backfillBatchSize+1),
		profileID, afterCreatedAt, afterID)
	if err != nil {
		return false, fmt.Errorf("query legacy request URLs for profile %d: %w", profileID, err)
	}
	type urlRow struct {
		id      int64
		created time.Time
		baseURL *string
	}
	batch := make([]urlRow, 0, backfillBatchSize)
	for rows.Next() {
		var row urlRow
		var baseURL sql.NullString
		if err := rows.Scan(&row.id, &row.created, &baseURL); err != nil {
			rows.Close()
			return false, fmt.Errorf("scan legacy request URL row: %w", err)
		}
		if baseURL.Valid {
			row.baseURL = &baseURL.String
		}
		batch = append(batch, row)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("iterate legacy request URL rows: %w", err)
	}
	if len(batch) == 0 {
		// request_logs and audit_logs have independent (created_at, id)
		// sequences. Start the audit phase from its own beginning instead of
		// applying the request_logs cursor to audit_logs.
		return owner.backfillAuditURLs(ctx, tx, profileID, nil, nil)
	}
	requestRowsRemain := len(batch) > backfillBatchSize
	if requestRowsRemain {
		batch = batch[:backfillBatchSize]
	}
	for _, row := range batch {
		scrubbed := ""
		if row.baseURL != nil {
			scrubbed, _ = safediag.ScrubEndpointBaseURL(*row.baseURL)
		}
		var endpointBaseURL any
		if strings.TrimSpace(scrubbed) != "" {
			endpointBaseURL = scrubbed
		}
		if _, err := tx.Exec(ctx, `UPDATE request_logs SET endpoint_base_url = $1, url_scrub_provenance = 'legacy_rescrubbed' WHERE id = $2`, endpointBaseURL, row.id); err != nil {
			return false, fmt.Errorf("rewrite request URL for request log %d: %w", row.id, err)
		}
	}
	last := batch[len(batch)-1]
	if requestRowsRemain {
		if err := upsertBackfillDomainStatus(ctx, tx, profileID, "request_urls", "running", &last.created, &last.id); err != nil {
			return false, err
		}
		return true, nil
	}
	// The request_logs scan is complete in this transaction. Start the
	// independent audit_logs phase at its own origin so an earlier audit row
	// cannot be hidden by the request_logs cursor.
	return owner.backfillAuditURLs(ctx, tx, profileID, nil, nil)
}

func (owner *requestAuditBackfillOwner) backfillAuditURLs(ctx context.Context, tx pgx.Tx, profileID int, afterCreatedAt *time.Time, afterID *int64) (bool, error) {
	rows, err := tx.Query(ctx, `SELECT id, created_at, request_url, endpoint_base_url FROM audit_logs
		WHERE profile_id = $1 AND url_scrub_provenance = 'legacy_unknown'
		AND ($2::timestamptz IS NULL OR created_at > $2 OR (created_at = $2 AND id > $3))
		ORDER BY created_at ASC, id ASC LIMIT `+fmt.Sprintf("%d", backfillBatchSize),
		profileID, afterCreatedAt, afterID)
	if err != nil {
		return false, fmt.Errorf("query legacy audit URLs for profile %d: %w", profileID, err)
	}
	type auditURLRow struct {
		id               int64
		created          time.Time
		requestURL       string
		baseURL          string
		requestTruncated bool
		baseTruncated    bool
	}
	batch := make([]auditURLRow, 0, backfillBatchSize)
	for rows.Next() {
		var row auditURLRow
		var baseURL sql.NullString
		if err := rows.Scan(&row.id, &row.created, &row.requestURL, &baseURL); err != nil {
			rows.Close()
			return false, fmt.Errorf("scan legacy audit URL row: %w", err)
		}
		if baseURL.Valid {
			row.baseURL = baseURL.String
		}
		batch = append(batch, row)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("iterate legacy audit URL rows: %w", err)
	}
	if len(batch) == 0 {
		// request_urls domain complete.
		return false, nil
	}
	for index := range batch {
		row := &batch[index]
		scrubbedRequestURL, requestTruncated := safediag.ScrubRequestURL(row.requestURL)
		scrubbedBaseURL, baseTruncated := safediag.ScrubEndpointBaseURL(row.baseURL)
		row.requestTruncated = requestTruncated
		row.baseTruncated = baseTruncated
		var endpointBaseURL any
		if strings.TrimSpace(scrubbedBaseURL) != "" {
			endpointBaseURL = scrubbedBaseURL
		}
		if _, err := tx.Exec(ctx, `UPDATE audit_logs SET request_url = $1, endpoint_base_url = $2, url_scrub_provenance = 'legacy_rescrubbed', request_url_truncated = $3, endpoint_base_url_truncated = $4 WHERE id = $5`,
			scrubbedRequestURL, endpointBaseURL, requestTruncated, baseTruncated, row.id); err != nil {
			return false, fmt.Errorf("rewrite audit URL for audit log %d: %w", row.id, err)
		}
	}
	last := batch[len(batch)-1]
	encodedAuditID := requestURLsAuditPhaseCursor(last.id)
	if err := upsertBackfillDomainStatus(ctx, tx, profileID, "request_urls", "running", &last.created, &encodedAuditID); err != nil {
		return false, err
	}
	return true, nil
}

// backfillRequestMetadata irreversibly scrubs/caps §4.3 external metadata on
// legacy request logs and writes the provenance arrays.
func (owner *requestAuditBackfillOwner) backfillRequestMetadata(ctx context.Context, tx pgx.Tx, profileID int, afterCreatedAt *time.Time, afterID *int64) (bool, error) {
	rows, err := tx.Query(ctx, `SELECT id, created_at, caller_user_agent, upstream_user_agent, provider_correlation_id, caller_request_id FROM request_logs
		WHERE profile_id = $1 AND (metadata_redacted_fields = '{}' AND metadata_truncated_fields = '{}')
		AND ($2::timestamptz IS NULL OR created_at > $2 OR (created_at = $2 AND id > $3))
		ORDER BY created_at ASC, id ASC LIMIT `+fmt.Sprintf("%d", backfillBatchSize),
		profileID, afterCreatedAt, afterID)
	if err != nil {
		return false, fmt.Errorf("query legacy request metadata for profile %d: %w", profileID, err)
	}
	type metadataRow struct {
		id                int64
		created           time.Time
		callerUserAgent   *string
		upstreamUserAgent *string
		correlationID     *string
		callerRequestID   *string
	}
	batch := make([]metadataRow, 0, backfillBatchSize)
	for rows.Next() {
		var row metadataRow
		var callerUA, upstreamUA, correlationID, callerRequestID sql.NullString
		if err := rows.Scan(&row.id, &row.created, &callerUA, &upstreamUA, &correlationID, &callerRequestID); err != nil {
			rows.Close()
			return false, fmt.Errorf("scan legacy request metadata row: %w", err)
		}
		row.callerUserAgent = nullableStringFromSQL(callerUA)
		row.upstreamUserAgent = nullableStringFromSQL(upstreamUA)
		row.correlationID = nullableStringFromSQL(correlationID)
		row.callerRequestID = nullableStringFromSQL(callerRequestID)
		batch = append(batch, row)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("iterate legacy request metadata rows: %w", err)
	}
	if len(batch) == 0 {
		return false, nil
	}
	for _, row := range batch {
		callerUA := scrubLegacyMetadata(row.callerUserAgent)
		upstreamUA := scrubLegacyMetadata(row.upstreamUserAgent)
		correlationID := scrubLegacyMetadata(row.correlationID)
		callerRequestID := scrubLegacyMetadata(row.callerRequestID)
		redacted := []string{}
		truncated := []string{}
		if row.callerUserAgent != nil && callerUA != nil {
			redacted = append(redacted, "caller_user_agent")
		}
		if row.upstreamUserAgent != nil && upstreamUA != nil {
			redacted = append(redacted, "upstream_user_agent")
		}
		if row.correlationID != nil && correlationID != nil {
			redacted = append(redacted, "provider_correlation_id")
		}
		if row.callerRequestID != nil && callerRequestID != nil {
			redacted = append(redacted, "caller_request_id")
		}
		if _, err := tx.Exec(ctx, `UPDATE request_logs SET caller_user_agent = $1, upstream_user_agent = $2, provider_correlation_id = $3, caller_request_id = $4,
			metadata_redacted_fields = $5, metadata_truncated_fields = $6 WHERE id = $7`,
			callerUA, upstreamUA, correlationID, callerRequestID,
			legacyMetadataFieldArray(redacted), legacyMetadataFieldArray(truncated), row.id); err != nil {
			return false, fmt.Errorf("rewrite request metadata for request log %d: %w", row.id, err)
		}
	}
	last := batch[len(batch)-1]
	if err := upsertBackfillDomainStatus(ctx, tx, profileID, "request_metadata", "running", &last.created, &last.id); err != nil {
		return false, err
	}
	return true, nil
}

// backfillAuditHeadersURLs rewrites legacy audit header raw shadows into
// scrubbed JSONB targets and nulls the shadows transactionally.
func (owner *requestAuditBackfillOwner) backfillAuditHeadersURLs(ctx context.Context, tx pgx.Tx, profileID int, afterCreatedAt *time.Time, afterID *int64) (bool, error) {
	// 000011 drops the raw shadow columns after the backfill completed in a
	// previous lifecycle; a missing shadow column means the domain is already
	// drained and no further rewrite is possible or needed.
	exists, err := auditRawShadowColumnsExist(ctx, tx)
	if err != nil {
		return false, err
	}
	if !exists {
		return false, nil
	}
	rows, err := tx.Query(ctx, `SELECT id, created_at, request_headers_legacy_raw_text, response_headers_legacy_raw_text FROM audit_logs
		WHERE profile_id = $1 AND (request_headers_legacy_raw_text IS NOT NULL OR response_headers_legacy_raw_text IS NOT NULL)
		AND ($2::timestamptz IS NULL OR created_at > $2 OR (created_at = $2 AND id > $3))
		ORDER BY created_at ASC, id ASC LIMIT `+fmt.Sprintf("%d", backfillBatchSize),
		profileID, afterCreatedAt, afterID)
	if err != nil {
		return false, fmt.Errorf("query legacy audit headers for profile %d: %w", profileID, err)
	}
	type headerRow struct {
		id              int64
		created         time.Time
		requestHeaders  *string
		responseHeaders *string
	}
	batch := make([]headerRow, 0, backfillBatchSize)
	for rows.Next() {
		var row headerRow
		var requestHeaders, responseHeaders sql.NullString
		if err := rows.Scan(&row.id, &row.created, &requestHeaders, &responseHeaders); err != nil {
			rows.Close()
			return false, fmt.Errorf("scan legacy audit header row: %w", err)
		}
		row.requestHeaders = nullableStringFromSQL(requestHeaders)
		row.responseHeaders = nullableStringFromSQL(responseHeaders)
		batch = append(batch, row)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("iterate legacy audit header rows: %w", err)
	}
	if len(batch) == 0 {
		return false, nil
	}
	for _, row := range batch {
		requestHeadersJSON := legacyAllValuesRedactedHeadersOptional(row.requestHeaders)
		responseHeadersJSON := legacyAllValuesRedactedHeadersOptional(row.responseHeaders)
		requestCaptureStatus := "captured"
		responseCaptureStatus := "captured"
		if requestHeadersJSON == nil {
			requestCaptureStatus = "not_requested"
		}
		if responseHeadersJSON == nil {
			responseCaptureStatus = "not_requested"
		}
		if _, err := tx.Exec(ctx, `UPDATE audit_logs SET
			request_headers = $1, request_headers_scrub_provenance = 'legacy_all_values_redacted', request_headers_capture_status = $2,
			response_headers = $3, response_headers_scrub_provenance = 'legacy_all_values_redacted', response_headers_capture_status = $4,
			request_headers_legacy_raw_text = NULL, response_headers_legacy_raw_text = NULL
			WHERE id = $5`,
			requestHeadersJSON, requestCaptureStatus, responseHeadersJSON, responseCaptureStatus, row.id); err != nil {
			return false, fmt.Errorf("rewrite audit headers for audit log %d: %w", row.id, err)
		}
	}
	last := batch[len(batch)-1]
	if err := upsertBackfillDomainStatus(ctx, tx, profileID, "audit_headers_urls", "running", &last.created, &last.id); err != nil {
		return false, err
	}
	return true, nil
}

func nullableStringFromSQL(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func legacyMetadataFieldArray(fields []string) []string {
	if len(fields) == 0 {
		return []string{}
	}
	encoded, _ := json.Marshal(fields)
	var decoded []string
	_ = json.Unmarshal(encoded, &decoded)
	return decoded
}
