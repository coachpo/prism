package sidecars

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/coachpo/prism/backend/internal/endpointdomain"
)

type Store struct {
	pool                *pgxpool.Pool
	now                 func() time.Time
	secretEncryptionKey string
}

type StoreOptions struct {
	Pool                *pgxpool.Pool
	Now                 func() time.Time
	SecretEncryptionKey string
}

type sidecarSQLExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

const sidecarAuthSnapshotReplaceChunkSize = 250

func NewStore(options StoreOptions) *Store {
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &Store{
		pool:                options.Pool,
		now:                 now,
		secretEncryptionKey: options.SecretEncryptionKey,
	}
}

const sidecarWatchdogAdvisoryLockNamespace int64 = 0x707269

type postgresWatchdogRunLease struct {
	conn *pgxpool.Conn
	key  int64
}

func (s *Store) TryAcquireWatchdogLease(ctx context.Context, sidecarID int) (watchdogRunLease, bool, error) {
	if err := s.requirePool(); err != nil {
		return nil, false, err
	}
	if sidecarID <= 0 {
		return nil, false, invalidInputError("sidecar_id is required")
	}
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("acquire sidecar watchdog lease connection: %w", err)
	}
	key := sidecarWatchdogAdvisoryLockKey(sidecarID)
	var acquired bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, key).Scan(&acquired); err != nil {
		conn.Release()
		return nil, false, fmt.Errorf("acquire sidecar watchdog advisory lock: %w", err)
	}
	if !acquired {
		conn.Release()
		return nil, false, nil
	}
	return &postgresWatchdogRunLease{conn: conn, key: key}, true, nil
}

func (lease *postgresWatchdogRunLease) Release(ctx context.Context) error {
	if lease == nil || lease.conn == nil {
		return nil
	}
	defer lease.conn.Release()
	var released bool
	if err := lease.conn.QueryRow(ctx, `SELECT pg_advisory_unlock($1)`, lease.key).Scan(&released); err != nil {
		return fmt.Errorf("release sidecar watchdog advisory lock: %w", err)
	}
	if !released {
		return fmt.Errorf("sidecar watchdog advisory lock was not held")
	}
	return nil
}

func sidecarWatchdogAdvisoryLockKey(sidecarID int) int64 {
	return (sidecarWatchdogAdvisoryLockNamespace << 32) | int64(uint32(sidecarID))
}

func (s *Store) CreateSidecarInstance(ctx context.Context, input SidecarInstanceInput) (SidecarInstance, error) {
	if err := s.requirePool(); err != nil {
		return SidecarInstance{}, err
	}
	normalized, err := s.normalizeInstanceInput(input)
	if err != nil {
		return SidecarInstance{}, err
	}
	row := s.pool.QueryRow(ctx, `INSERT INTO sidecar_instances (
name, base_url, base_url_canonical, management_password, enabled, environment_label,
sync_interval_seconds, request_timeout_seconds, allow_private_network, allow_insecure_http,
skip_tls_verify, management_auth_state, auth_failure_pause_until)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
RETURNING `+sidecarInstanceSelectColumns,
		normalized.Name,
		normalized.BaseURL,
		normalized.BaseURLCanonical,
		normalized.ManagementPassword,
		boolValueOr(normalized.Enabled, true),
		nullStringArg(normalized.EnvironmentLabel),
		normalized.SyncIntervalSeconds,
		normalized.RequestTimeoutSeconds,
		normalized.AllowPrivateNetwork,
		normalized.AllowInsecureHTTP,
		normalized.SkipTLSVerify,
		normalized.ManagementAuthState,
		nullTimeArg(normalized.AuthFailurePauseUntil),
	)
	record, scanErr := scanSidecarInstance(row)
	if scanErr != nil {
		return SidecarInstance{}, mapStoreError(scanErr)
	}
	return record, nil
}

func (s *Store) UpdateSidecarInstance(ctx context.Context, id int, input SidecarInstanceInput) (SidecarInstance, error) {
	if err := s.requirePool(); err != nil {
		return SidecarInstance{}, err
	}
	normalized, err := s.normalizeInstanceInput(input)
	if err != nil {
		return SidecarInstance{}, err
	}
	updatedAt := s.currentTime()
	row := s.pool.QueryRow(ctx, `UPDATE sidecar_instances SET
name = $2, base_url = $3, base_url_canonical = $4, management_password = $5,
enabled = $6, environment_label = $7, sync_interval_seconds = $8,
request_timeout_seconds = $9, allow_private_network = $10, allow_insecure_http = $11,
skip_tls_verify = $12, management_auth_state = $13, auth_failure_pause_until = $14,
updated_at = $15
WHERE id = $1 AND deleted_at IS NULL
RETURNING `+sidecarInstanceSelectColumns,
		id,
		normalized.Name,
		normalized.BaseURL,
		normalized.BaseURLCanonical,
		normalized.ManagementPassword,
		boolValueOr(normalized.Enabled, true),
		nullStringArg(normalized.EnvironmentLabel),
		normalized.SyncIntervalSeconds,
		normalized.RequestTimeoutSeconds,
		normalized.AllowPrivateNetwork,
		normalized.AllowInsecureHTTP,
		normalized.SkipTLSVerify,
		normalized.ManagementAuthState,
		nullTimeArg(normalized.AuthFailurePauseUntil),
		updatedAt,
	)
	record, scanErr := scanSidecarInstance(row)
	if scanErr == pgx.ErrNoRows {
		return SidecarInstance{}, notFoundError("sidecar instance not found")
	}
	if scanErr != nil {
		return SidecarInstance{}, mapStoreError(scanErr)
	}
	return record, nil
}

func (s *Store) GetSidecarInstance(ctx context.Context, id int) (SidecarInstance, bool, error) {
	if err := s.requirePool(); err != nil {
		return SidecarInstance{}, false, err
	}
	row := s.pool.QueryRow(ctx, `SELECT `+sidecarInstanceSelectColumns+` FROM sidecar_instances WHERE id = $1 AND deleted_at IS NULL`, id)
	record, err := scanSidecarInstance(row)
	if err == pgx.ErrNoRows {
		return SidecarInstance{}, false, nil
	}
	if err != nil {
		return SidecarInstance{}, false, fmt.Errorf("load sidecar instance %d: %w", id, err)
	}
	return record, true, nil
}

func (s *Store) ListSidecarInstances(ctx context.Context) ([]SidecarInstance, error) {
	if err := s.requirePool(); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT `+sidecarInstanceSelectColumns+` FROM sidecar_instances WHERE deleted_at IS NULL ORDER BY lower(name), id`)
	if err != nil {
		return nil, fmt.Errorf("query sidecar instances: %w", err)
	}
	defer rows.Close()
	records := make([]SidecarInstance, 0)
	for rows.Next() {
		record, scanErr := scanSidecarInstance(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sidecar instances: %w", err)
	}
	return records, nil
}

func (s *Store) SoftDeleteSidecarInstance(ctx context.Context, id int) (bool, error) {
	if err := s.requirePool(); err != nil {
		return false, err
	}
	deletedAt := s.currentTime()
	result, err := s.pool.Exec(ctx, `UPDATE sidecar_instances SET deleted_at = $2, updated_at = $2 WHERE id = $1 AND deleted_at IS NULL`, id, deletedAt)
	if err != nil {
		return false, fmt.Errorf("soft delete sidecar instance %d: %w", id, err)
	}
	return result.RowsAffected() > 0, nil
}

func (s *Store) UpdateSidecarSyncMetadata(ctx context.Context, input SidecarSyncMetadataInput) (SidecarInstance, error) {
	if err := s.requirePool(); err != nil {
		return SidecarInstance{}, err
	}
	if input.SidecarID <= 0 || input.LastSyncAt.IsZero() {
		return SidecarInstance{}, invalidInputError("sidecar_id and last_sync_at are required")
	}
	state := strings.TrimSpace(input.ManagementAuthState)
	if state == "" {
		state = ManagementAuthStateUnknown
	}
	row := s.pool.QueryRow(ctx, `UPDATE sidecar_instances SET
last_sync_at = $2, last_successful_sync_at = $3, snapshot_stale_after = $4,
last_sync_error = $5, management_auth_state = $6, auth_failure_pause_until = $7,
updated_at = $2
WHERE id = $1 AND deleted_at IS NULL
RETURNING `+sidecarInstanceSelectColumns,
		input.SidecarID,
		input.LastSyncAt.UTC(),
		nullTimeArg(input.LastSuccessfulSyncAt),
		nullTimeArg(input.SnapshotStaleAfter),
		nullStringArg(input.LastSyncError),
		state,
		nullTimeArg(input.AuthFailurePauseUntil),
	)
	record, err := scanSidecarInstance(row)
	if err == pgx.ErrNoRows {
		return SidecarInstance{}, notFoundError("sidecar instance not found")
	}
	if err != nil {
		return SidecarInstance{}, mapStoreError(err)
	}
	return record, nil
}

func (s *Store) SaveAuthSnapshot(ctx context.Context, input SidecarAuthSnapshotInput) (SidecarAuthSnapshot, error) {
	if err := s.requirePool(); err != nil {
		return SidecarAuthSnapshot{}, err
	}
	if input.SidecarID <= 0 || strings.TrimSpace(input.AuthID) == "" || strings.TrimSpace(input.Name) == "" {
		return SidecarAuthSnapshot{}, invalidInputError("sidecar_id, auth_id, and name are required")
	}
	observedAt := input.ObservedAt
	if observedAt.IsZero() {
		observedAt = s.currentTime()
	}
	if err := validateSidecarSnapshotJSON(input.SnapshotJSON); err != nil {
		return SidecarAuthSnapshot{}, err
	}
	row := s.pool.QueryRow(ctx, `INSERT INTO sidecar_auth_snapshots (
sidecar_id, auth_id, auth_index, name, provider, label, status, status_message,
disabled, unavailable, priority, quota_exceeded, quota_reason, quota_next_recover_at,
next_retry_after, success_count, failed_count, recent_requests_json, model_states_json,
snapshot_json, observed_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18::jsonb, $19::jsonb, $20::jsonb, $21)
ON CONFLICT (sidecar_id, auth_id) DO UPDATE SET
auth_index = EXCLUDED.auth_index, name = EXCLUDED.name, provider = EXCLUDED.provider,
label = EXCLUDED.label, status = EXCLUDED.status, status_message = EXCLUDED.status_message,
disabled = EXCLUDED.disabled, unavailable = EXCLUDED.unavailable, priority = EXCLUDED.priority,
quota_exceeded = EXCLUDED.quota_exceeded, quota_reason = EXCLUDED.quota_reason,
quota_next_recover_at = EXCLUDED.quota_next_recover_at, next_retry_after = EXCLUDED.next_retry_after,
success_count = EXCLUDED.success_count, failed_count = EXCLUDED.failed_count,
recent_requests_json = EXCLUDED.recent_requests_json, model_states_json = EXCLUDED.model_states_json,
snapshot_json = EXCLUDED.snapshot_json, observed_at = EXCLUDED.observed_at, updated_at = now()
RETURNING `+sidecarAuthSnapshotSelectColumns,
		input.SidecarID, strings.TrimSpace(input.AuthID), nullStringArg(input.AuthIndex), strings.TrimSpace(input.Name),
		nullStringArg(input.Provider), nullStringArg(input.Label), nullStringArg(input.Status), nullStringArg(input.StatusMessage),
		nullBoolArg(input.Disabled), nullBoolArg(input.Unavailable), nullIntArg(input.Priority), nullBoolArg(input.QuotaExceeded),
		nullStringArg(input.QuotaReason), nullTimeArg(input.QuotaNextRecoverAt), nullTimeArg(input.NextRetryAfter),
		nullIntArg(input.SuccessCount), nullIntArg(input.FailedCount), jsonbString(input.RecentRequestsJSON, "[]"),
		jsonbString(input.ModelStatesJSON, "{}"), jsonbString(input.SnapshotJSON, "{}"), observedAt,
	)
	record, err := scanSidecarAuthSnapshot(row)
	if err != nil {
		return SidecarAuthSnapshot{}, mapStoreError(err)
	}
	return record, nil
}

func (s *Store) ReplaceAuthSnapshots(ctx context.Context, sidecarID int, inputs []SidecarAuthSnapshotInput) ([]SidecarAuthSnapshot, error) {
	if err := s.requirePool(); err != nil {
		return nil, err
	}
	normalized, err := validateAuthSnapshotReplacementInputs(sidecarID, inputs)
	if err != nil {
		return nil, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin auth snapshot replacement: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `DELETE FROM sidecar_auth_snapshots WHERE sidecar_id = $1`, sidecarID); err != nil {
		return nil, fmt.Errorf("delete sidecar auth snapshots: %w", err)
	}
	records := make([]SidecarAuthSnapshot, 0, len(normalized))
	for start := 0; start < len(normalized); start += sidecarAuthSnapshotReplaceChunkSize {
		end := min(start+sidecarAuthSnapshotReplaceChunkSize, len(normalized))
		chunkRecords, err := s.insertAuthSnapshotReplacementChunk(ctx, tx, normalized[start:end])
		if err != nil {
			return nil, err
		}
		records = append(records, chunkRecords...)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit auth snapshot replacement: %w", err)
	}
	return records, nil
}

func (s *Store) insertAuthSnapshotReplacementChunk(ctx context.Context, tx pgx.Tx, inputs []SidecarAuthSnapshotInput) ([]SidecarAuthSnapshot, error) {
	if len(inputs) == 0 {
		return nil, nil
	}
	var builder strings.Builder
	builder.WriteString(`INSERT INTO sidecar_auth_snapshots (
sidecar_id, auth_id, auth_index, name, provider, label, status, status_message,
disabled, unavailable, priority, quota_exceeded, quota_reason, quota_next_recover_at,
next_retry_after, success_count, failed_count, recent_requests_json, model_states_json,
snapshot_json, observed_at) VALUES `)
	args := make([]any, 0, len(inputs)*21)
	for i, input := range inputs {
		if i > 0 {
			builder.WriteString(", ")
		}
		appendAuthSnapshotInsertPlaceholders(&builder, len(args)+1)
		observedAt := input.ObservedAt
		if observedAt.IsZero() {
			observedAt = s.currentTime()
		}
		args = append(args,
			input.SidecarID, input.AuthID, nullStringArg(input.AuthIndex), input.Name,
			nullStringArg(input.Provider), nullStringArg(input.Label), nullStringArg(input.Status), nullStringArg(input.StatusMessage),
			nullBoolArg(input.Disabled), nullBoolArg(input.Unavailable), nullIntArg(input.Priority), nullBoolArg(input.QuotaExceeded),
			nullStringArg(input.QuotaReason), nullTimeArg(input.QuotaNextRecoverAt), nullTimeArg(input.NextRetryAfter),
			nullIntArg(input.SuccessCount), nullIntArg(input.FailedCount), jsonbString(input.RecentRequestsJSON, "[]"),
			jsonbString(input.ModelStatesJSON, "{}"), jsonbString(input.SnapshotJSON, "{}"), observedAt,
		)
	}
	builder.WriteString(` RETURNING `)
	builder.WriteString(sidecarAuthSnapshotSelectColumns)
	rows, err := tx.Query(ctx, builder.String(), args...)
	if err != nil {
		return nil, mapStoreError(err)
	}
	defer rows.Close()
	records := make([]SidecarAuthSnapshot, 0, len(inputs))
	for rows.Next() {
		record, scanErr := scanSidecarAuthSnapshot(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate inserted sidecar auth snapshots: %w", err)
	}
	return records, nil
}

func appendAuthSnapshotInsertPlaceholders(builder *strings.Builder, first int) {
	builder.WriteByte('(')
	for index := range 21 {
		if index > 0 {
			builder.WriteString(", ")
		}
		_, _ = fmt.Fprintf(builder, "$%d", first+index)
		if index == 17 || index == 18 || index == 19 {
			builder.WriteString("::jsonb")
		}
	}
	builder.WriteByte(')')
}

func (s *Store) GetAuthSnapshot(ctx context.Context, sidecarID int, authID string) (SidecarAuthSnapshot, bool, error) {
	if err := s.requirePool(); err != nil {
		return SidecarAuthSnapshot{}, false, err
	}
	row := s.pool.QueryRow(ctx, `SELECT `+sidecarAuthSnapshotSelectColumns+` FROM sidecar_auth_snapshots WHERE sidecar_id = $1 AND auth_id = $2`, sidecarID, authID)
	record, err := scanSidecarAuthSnapshot(row)
	if err == pgx.ErrNoRows {
		return SidecarAuthSnapshot{}, false, nil
	}
	if err != nil {
		return SidecarAuthSnapshot{}, false, fmt.Errorf("load sidecar auth snapshot: %w", err)
	}
	return record, true, nil
}

func (s *Store) ListAuthSnapshots(ctx context.Context, sidecarID int) ([]SidecarAuthSnapshot, error) {
	if err := s.requirePool(); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT `+sidecarAuthSnapshotSelectColumns+` FROM sidecar_auth_snapshots WHERE sidecar_id = $1 ORDER BY name ASC, auth_id ASC`, sidecarID)
	if err != nil {
		return nil, fmt.Errorf("query sidecar auth snapshots: %w", err)
	}
	defer rows.Close()
	records := make([]SidecarAuthSnapshot, 0)
	for rows.Next() {
		record, scanErr := scanSidecarAuthSnapshot(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sidecar auth snapshots: %w", err)
	}
	return records, nil
}

func (s *Store) SaveProviderSnapshot(ctx context.Context, input SidecarProviderSnapshotInput) (SidecarProviderSnapshot, error) {
	if err := s.requirePool(); err != nil {
		return SidecarProviderSnapshot{}, err
	}
	if input.SidecarID <= 0 || strings.TrimSpace(input.ProviderKey) == "" || strings.TrimSpace(input.ProviderItemKey) == "" {
		return SidecarProviderSnapshot{}, invalidInputError("sidecar_id, provider_key, and provider_item_key are required")
	}
	observedAt := input.ObservedAt
	if observedAt.IsZero() {
		observedAt = s.currentTime()
	}
	if err := validateSidecarSnapshotJSON(input.SnapshotJSON); err != nil {
		return SidecarProviderSnapshot{}, err
	}
	row := s.pool.QueryRow(ctx, `INSERT INTO sidecar_provider_snapshots (
sidecar_id, provider_key, provider_item_key, name, label, status, disabled, snapshot_json, observed_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9)
ON CONFLICT (sidecar_id, provider_key, provider_item_key) DO UPDATE SET
name = EXCLUDED.name, label = EXCLUDED.label, status = EXCLUDED.status,
disabled = EXCLUDED.disabled, snapshot_json = EXCLUDED.snapshot_json,
observed_at = EXCLUDED.observed_at, updated_at = now()
RETURNING `+sidecarProviderSnapshotSelectColumns,
		input.SidecarID, strings.TrimSpace(input.ProviderKey), strings.TrimSpace(input.ProviderItemKey),
		nullStringArg(input.Name), nullStringArg(input.Label), nullStringArg(input.Status),
		nullBoolArg(input.Disabled), jsonbString(input.SnapshotJSON, "{}"), observedAt,
	)
	record, err := scanSidecarProviderSnapshot(row)
	if err != nil {
		return SidecarProviderSnapshot{}, mapStoreError(err)
	}
	return record, nil
}

func (s *Store) ReplaceProviderSnapshots(ctx context.Context, sidecarID int, providerKey string, inputs []SidecarProviderSnapshotInput) ([]SidecarProviderSnapshot, error) {
	if err := s.requirePool(); err != nil {
		return nil, err
	}
	providerKey = strings.TrimSpace(providerKey)
	if sidecarID <= 0 || providerKey == "" {
		return nil, invalidInputError("sidecar_id and provider_key are required")
	}
	for _, input := range inputs {
		if input.SidecarID != sidecarID || strings.TrimSpace(input.ProviderKey) != providerKey || strings.TrimSpace(input.ProviderItemKey) == "" {
			return nil, invalidInputError("provider replacement input does not match provider batch")
		}
		if err := validateSidecarSnapshotJSON(input.SnapshotJSON); err != nil {
			return nil, err
		}
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin provider snapshot replacement: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `DELETE FROM sidecar_provider_snapshots WHERE sidecar_id = $1 AND provider_key = $2`, sidecarID, providerKey); err != nil {
		return nil, fmt.Errorf("delete sidecar provider snapshots: %w", err)
	}
	records := make([]SidecarProviderSnapshot, 0, len(inputs))
	for _, input := range inputs {
		observedAt := input.ObservedAt
		if observedAt.IsZero() {
			observedAt = s.currentTime()
		}
		row := tx.QueryRow(ctx, `INSERT INTO sidecar_provider_snapshots (
sidecar_id, provider_key, provider_item_key, name, label, status, disabled, snapshot_json, observed_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9)
RETURNING `+sidecarProviderSnapshotSelectColumns,
			input.SidecarID, providerKey, strings.TrimSpace(input.ProviderItemKey),
			nullStringArg(input.Name), nullStringArg(input.Label), nullStringArg(input.Status),
			nullBoolArg(input.Disabled), jsonbString(input.SnapshotJSON, "{}"), observedAt,
		)
		record, err := scanSidecarProviderSnapshot(row)
		if err != nil {
			return nil, mapStoreError(err)
		}
		records = append(records, record)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit provider snapshot replacement: %w", err)
	}
	return records, nil
}

func (s *Store) GetProviderSnapshot(ctx context.Context, sidecarID int, providerKey string, providerItemKey string) (SidecarProviderSnapshot, bool, error) {
	if err := s.requirePool(); err != nil {
		return SidecarProviderSnapshot{}, false, err
	}
	row := s.pool.QueryRow(ctx, `SELECT `+sidecarProviderSnapshotSelectColumns+` FROM sidecar_provider_snapshots WHERE sidecar_id = $1 AND provider_key = $2 AND provider_item_key = $3`, sidecarID, providerKey, providerItemKey)
	record, err := scanSidecarProviderSnapshot(row)
	if err == pgx.ErrNoRows {
		return SidecarProviderSnapshot{}, false, nil
	}
	if err != nil {
		return SidecarProviderSnapshot{}, false, fmt.Errorf("load sidecar provider snapshot: %w", err)
	}
	return record, true, nil
}

func (s *Store) ListProviderSnapshots(ctx context.Context, sidecarID int) ([]SidecarProviderSnapshot, error) {
	if err := s.requirePool(); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT `+sidecarProviderSnapshotSelectColumns+` FROM sidecar_provider_snapshots WHERE sidecar_id = $1 ORDER BY provider_key ASC, provider_item_key ASC`, sidecarID)
	if err != nil {
		return nil, fmt.Errorf("query sidecar provider snapshots: %w", err)
	}
	defer rows.Close()
	records := make([]SidecarProviderSnapshot, 0)
	for rows.Next() {
		record, scanErr := scanSidecarProviderSnapshot(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sidecar provider snapshots: %w", err)
	}
	return records, nil
}

func (s *Store) GetOrCreateWatchdogPolicy(ctx context.Context, sidecarID int) (SidecarWatchdogPolicy, error) {
	if err := s.requirePool(); err != nil {
		return SidecarWatchdogPolicy{}, err
	}
	if sidecarID <= 0 {
		return SidecarWatchdogPolicy{}, invalidInputError("sidecar_id is required")
	}
	row := s.pool.QueryRow(ctx, `INSERT INTO sidecar_watchdog_policies (sidecar_id) VALUES ($1)
ON CONFLICT (sidecar_id) DO UPDATE SET sidecar_id = sidecar_watchdog_policies.sidecar_id
RETURNING `+sidecarWatchdogPolicySelectColumns, sidecarID)
	record, err := scanSidecarWatchdogPolicy(row)
	if err != nil {
		return SidecarWatchdogPolicy{}, mapStoreError(err)
	}
	return record, nil
}

func (s *Store) UpsertWatchdogPolicy(ctx context.Context, input SidecarWatchdogPolicyInput) (SidecarWatchdogPolicy, error) {
	if err := s.requirePool(); err != nil {
		return SidecarWatchdogPolicy{}, err
	}
	if input.SidecarID <= 0 {
		return SidecarWatchdogPolicy{}, invalidInputError("sidecar_id is required")
	}
	preservePrioritizedPriority := input.PrioritizedPriority <= 0
	preserveProbeBatchSize := input.ProbeBatchSize <= 0
	preserveProbeTimeoutSeconds := input.ProbeTimeoutSeconds <= 0
	if preservePrioritizedPriority || preserveProbeBatchSize || preserveProbeTimeoutSeconds {
		existing, existingErr := s.GetOrCreateWatchdogPolicy(ctx, input.SidecarID)
		if existingErr != nil {
			return SidecarWatchdogPolicy{}, existingErr
		}
		if preservePrioritizedPriority {
			input.PrioritizedPriority = existing.PrioritizedPriority
		}
		if preserveProbeBatchSize {
			input.ProbeBatchSize = existing.ProbeBatchSize
		}
		if preserveProbeTimeoutSeconds {
			input.ProbeTimeoutSeconds = existing.ProbeTimeoutSeconds
		}
	}
	normalized, err := normalizePolicyInput(input)
	if err != nil {
		return SidecarWatchdogPolicy{}, err
	}
	row := s.pool.QueryRow(ctx, `INSERT INTO sidecar_watchdog_policies (
sidecar_id, enabled, failure_threshold, failure_window_seconds, fallback_cooldown_seconds,
deprioritized_priority, prioritized_priority, manual_override_pause_seconds,
probe_batch_size, probe_timeout_seconds)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (sidecar_id) DO UPDATE SET enabled = EXCLUDED.enabled,
failure_threshold = EXCLUDED.failure_threshold, failure_window_seconds = EXCLUDED.failure_window_seconds,
fallback_cooldown_seconds = EXCLUDED.fallback_cooldown_seconds,
deprioritized_priority = EXCLUDED.deprioritized_priority,
prioritized_priority = CASE WHEN $11 THEN sidecar_watchdog_policies.prioritized_priority ELSE EXCLUDED.prioritized_priority END,
manual_override_pause_seconds = EXCLUDED.manual_override_pause_seconds,
probe_batch_size = CASE WHEN $12 THEN sidecar_watchdog_policies.probe_batch_size ELSE EXCLUDED.probe_batch_size END,
probe_timeout_seconds = CASE WHEN $13 THEN sidecar_watchdog_policies.probe_timeout_seconds ELSE EXCLUDED.probe_timeout_seconds END,
updated_at = now()
RETURNING `+sidecarWatchdogPolicySelectColumns,
		normalized.SidecarID, normalized.Enabled, normalized.FailureThreshold,
		normalized.FailureWindowSeconds, normalized.FallbackCooldownSeconds,
		normalized.DeprioritizedPriority, normalized.PrioritizedPriority,
		normalized.ManualOverridePauseSeconds, normalized.ProbeBatchSize,
		normalized.ProbeTimeoutSeconds, preservePrioritizedPriority,
		preserveProbeBatchSize, preserveProbeTimeoutSeconds,
	)
	record, err := scanSidecarWatchdogPolicy(row)
	if err != nil {
		return SidecarWatchdogPolicy{}, mapStoreError(err)
	}
	return record, nil
}

func (s *Store) CreateWatchdogProbeObservation(ctx context.Context, input SidecarWatchdogProbeObservationInput) (SidecarWatchdogProbeObservation, error) {
	if err := s.requirePool(); err != nil {
		return SidecarWatchdogProbeObservation{}, err
	}
	record, err := s.insertWatchdogProbeObservation(ctx, s.pool, input)
	if err != nil {
		return SidecarWatchdogProbeObservation{}, mapStoreError(err)
	}
	return record, nil
}

func (s *Store) ListWatchdogProbeObservations(ctx context.Context, sidecarID int, limit int) ([]SidecarWatchdogProbeObservation, error) {
	if err := s.requirePool(); err != nil {
		return nil, err
	}
	if sidecarID <= 0 {
		return nil, invalidInputError("sidecar_id is required")
	}
	query := `SELECT ` + sidecarWatchdogProbeObservationSelectColumns + ` FROM sidecar_watchdog_probe_observations WHERE sidecar_id = $1 ORDER BY probed_at DESC, id DESC`
	args := []any{sidecarID}
	if limit > 0 {
		query += ` LIMIT $2`
		args = append(args, limit)
	}
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query sidecar watchdog probe observations: %w", err)
	}
	defer rows.Close()
	records := make([]SidecarWatchdogProbeObservation, 0)
	for rows.Next() {
		record, scanErr := scanSidecarWatchdogProbeObservation(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sidecar watchdog probe observations: %w", err)
	}
	return records, nil
}

func (s *Store) CleanupWatchdogProbeObservations(ctx context.Context) (int64, error) {
	if err := s.requirePool(); err != nil {
		return 0, err
	}
	cutoff := s.currentTime().Add(-time.Duration(WatchdogProbeObservationRetentionDays) * 24 * time.Hour)
	result, err := s.pool.Exec(ctx, `DELETE FROM sidecar_watchdog_probe_observations WHERE probed_at < $1`, cutoff)
	if err != nil {
		return 0, mapStoreError(err)
	}
	return result.RowsAffected(), nil
}

func (s *Store) ListDueWatchdogHolds(ctx context.Context, sidecarID int, dueAt time.Time) ([]SidecarWatchdogHold, error) {
	if err := s.requirePool(); err != nil {
		return nil, err
	}
	if sidecarID <= 0 {
		return nil, invalidInputError("sidecar_id is required")
	}
	if dueAt.IsZero() {
		dueAt = s.currentTime()
	}
	rows, err := s.pool.Query(ctx, `SELECT `+sidecarWatchdogHoldSelectColumns+` FROM sidecar_watchdog_holds WHERE sidecar_id = $1 AND status = 'active' AND hold_until IS NOT NULL AND hold_until <= $2 ORDER BY hold_until ASC, id ASC`, sidecarID, dueAt)
	if err != nil {
		return nil, fmt.Errorf("query due sidecar watchdog holds: %w", err)
	}
	defer rows.Close()
	records := make([]SidecarWatchdogHold, 0)
	for rows.Next() {
		record, scanErr := scanSidecarWatchdogHold(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate due sidecar watchdog holds: %w", err)
	}
	return records, nil
}

func (s *Store) PersistWatchdogProbeDecision(ctx context.Context, decision SidecarWatchdogProbeDecision) (SidecarWatchdogProbeDecisionResult, error) {
	if err := s.requirePool(); err != nil {
		return SidecarWatchdogProbeDecisionResult{}, err
	}
	if decision.SidecarID <= 0 {
		return SidecarWatchdogProbeDecisionResult{}, invalidInputError("sidecar_id is required")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return SidecarWatchdogProbeDecisionResult{}, fmt.Errorf("begin sidecar watchdog probe decision: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	result := SidecarWatchdogProbeDecisionResult{Observations: make([]SidecarWatchdogProbeObservation, 0, len(decision.Observations))}
	for _, input := range decision.Observations {
		if input.SidecarID == 0 {
			input.SidecarID = decision.SidecarID
		}
		if input.SidecarID != decision.SidecarID {
			return SidecarWatchdogProbeDecisionResult{}, invalidInputError("probe observation sidecar_id must match decision sidecar_id")
		}
		record, insertErr := s.insertWatchdogProbeObservation(ctx, tx, input)
		if insertErr != nil {
			return SidecarWatchdogProbeDecisionResult{}, mapStoreError(insertErr)
		}
		result.Observations = append(result.Observations, record)
	}
	if decision.CreateHold != nil {
		input := *decision.CreateHold
		if input.SidecarID == 0 {
			input.SidecarID = decision.SidecarID
		}
		if input.SidecarID != decision.SidecarID {
			return SidecarWatchdogProbeDecisionResult{}, invalidInputError("created hold sidecar_id must match decision sidecar_id")
		}
		hold, createErr := insertWatchdogHold(ctx, tx, input)
		if createErr != nil {
			return SidecarWatchdogProbeDecisionResult{}, mapStoreError(createErr)
		}
		result.CreatedHold = &hold
	}
	if decision.UpdateHold != nil {
		input := decision.UpdateHold.Input
		if input.SidecarID == 0 {
			input.SidecarID = decision.SidecarID
		}
		if input.SidecarID != decision.SidecarID {
			return SidecarWatchdogProbeDecisionResult{}, invalidInputError("updated hold sidecar_id must match decision sidecar_id")
		}
		hold, updateErr := updateWatchdogHold(ctx, tx, decision.UpdateHold.ID, input)
		if updateErr != nil {
			return SidecarWatchdogProbeDecisionResult{}, mapStoreError(updateErr)
		}
		result.UpdatedHold = &hold
	}
	if decision.AdvanceProbeCursor {
		policy, cursorErr := updateWatchdogProbeCursor(ctx, tx, decision.SidecarID, decision.ProbeCursorAuthID)
		if cursorErr != nil {
			return SidecarWatchdogProbeDecisionResult{}, mapStoreError(cursorErr)
		}
		result.Policy = &policy
	}
	if err := tx.Commit(ctx); err != nil {
		return SidecarWatchdogProbeDecisionResult{}, fmt.Errorf("commit sidecar watchdog probe decision: %w", err)
	}
	return result, nil
}

func (s *Store) insertWatchdogProbeObservation(ctx context.Context, executor sidecarSQLExecutor, input SidecarWatchdogProbeObservationInput) (SidecarWatchdogProbeObservation, error) {
	normalized, err := normalizeWatchdogProbeObservationInput(input, s.currentTime())
	if err != nil {
		return SidecarWatchdogProbeObservation{}, err
	}
	row := executor.QueryRow(ctx, `INSERT INTO sidecar_watchdog_probe_observations (
sidecar_id, auth_id, auth_index, provider, probed_at, probe_status,
upstream_status_code, quota_exceeded, quota_reason, quota_reset_at,
blocking_window, windows_json, error_code)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb, $13)
RETURNING `+sidecarWatchdogProbeObservationSelectColumns,
		normalized.SidecarID, normalized.AuthID, nullStringArg(normalized.AuthIndex), nullStringArg(normalized.Provider),
		normalized.ProbedAt, normalized.ProbeStatus, nullIntArg(normalized.UpstreamStatusCode), normalized.QuotaExceeded,
		nullStringArg(normalized.QuotaReason), nullTimeArg(normalized.QuotaResetAt), nullStringArg(normalized.BlockingWindow),
		jsonbString(normalized.WindowsJSON, "[]"), nullStringArg(normalized.ErrorCode),
	)
	return scanSidecarWatchdogProbeObservation(row)
}

func updateWatchdogProbeCursor(ctx context.Context, executor sidecarSQLExecutor, sidecarID int, cursorAuthID *string) (SidecarWatchdogPolicy, error) {
	if sidecarID <= 0 {
		return SidecarWatchdogPolicy{}, invalidInputError("sidecar_id is required")
	}
	row := executor.QueryRow(ctx, `INSERT INTO sidecar_watchdog_policies (sidecar_id, probe_cursor_auth_id)
VALUES ($1, $2)
ON CONFLICT (sidecar_id) DO UPDATE SET probe_cursor_auth_id = EXCLUDED.probe_cursor_auth_id, updated_at = now()
RETURNING `+sidecarWatchdogPolicySelectColumns, sidecarID, nullStringArg(cursorAuthID))
	return scanSidecarWatchdogPolicy(row)
}

func (s *Store) CreateWatchdogHold(ctx context.Context, input SidecarWatchdogHoldInput) (SidecarWatchdogHold, error) {
	if err := s.requirePool(); err != nil {
		return SidecarWatchdogHold{}, err
	}
	record, err := insertWatchdogHold(ctx, s.pool, input)
	if err != nil {
		return SidecarWatchdogHold{}, mapStoreError(err)
	}
	return record, nil
}

func insertWatchdogHold(ctx context.Context, executor sidecarSQLExecutor, input SidecarWatchdogHoldInput) (SidecarWatchdogHold, error) {
	if input.SidecarID <= 0 || strings.TrimSpace(input.AuthID) == "" || strings.TrimSpace(input.Reason) == "" || strings.TrimSpace(input.ConditionHash) == "" || strings.TrimSpace(input.Status) == "" {
		return SidecarWatchdogHold{}, invalidInputError("sidecar_id, auth_id, reason, condition_hash, and status are required")
	}
	row := executor.QueryRow(ctx, `INSERT INTO sidecar_watchdog_holds (
sidecar_id, auth_id, auth_index, provider, reason, condition_hash, previous_priority,
target_priority, hold_until, manual_pause_until, status, last_action_id, released_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
RETURNING `+sidecarWatchdogHoldSelectColumns,
		input.SidecarID, strings.TrimSpace(input.AuthID), nullStringArg(input.AuthIndex), nullStringArg(input.Provider),
		strings.TrimSpace(input.Reason), strings.TrimSpace(input.ConditionHash), nullIntArg(input.PreviousPriority),
		input.TargetPriority, nullTimeArg(input.HoldUntil), nullTimeArg(input.ManualPauseUntil), strings.TrimSpace(input.Status),
		nullIntArg(input.LastActionID), nullTimeArg(input.ReleasedAt),
	)
	return scanSidecarWatchdogHold(row)
}

func (s *Store) GetActiveWatchdogHold(ctx context.Context, sidecarID int, authID string) (SidecarWatchdogHold, bool, error) {
	if err := s.requirePool(); err != nil {
		return SidecarWatchdogHold{}, false, err
	}
	if sidecarID <= 0 || strings.TrimSpace(authID) == "" {
		return SidecarWatchdogHold{}, false, invalidInputError("sidecar_id and auth_id are required")
	}
	row := s.pool.QueryRow(ctx, `SELECT `+sidecarWatchdogHoldSelectColumns+` FROM sidecar_watchdog_holds WHERE sidecar_id = $1 AND auth_id = $2 AND status IN ('active', 'paused') ORDER BY created_at DESC, id DESC LIMIT 1`, sidecarID, strings.TrimSpace(authID))
	record, err := scanSidecarWatchdogHold(row)
	if err == pgx.ErrNoRows {
		return SidecarWatchdogHold{}, false, nil
	}
	if err != nil {
		return SidecarWatchdogHold{}, false, fmt.Errorf("load active sidecar watchdog hold: %w", err)
	}
	return record, true, nil
}

func (s *Store) ListActiveWatchdogHolds(ctx context.Context, sidecarID int) ([]SidecarWatchdogHold, error) {
	if err := s.requirePool(); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT `+sidecarWatchdogHoldSelectColumns+` FROM sidecar_watchdog_holds WHERE sidecar_id = $1 AND status IN ('active', 'paused') ORDER BY created_at ASC, id ASC`, sidecarID)
	if err != nil {
		return nil, fmt.Errorf("query active sidecar watchdog holds: %w", err)
	}
	defer rows.Close()
	records := make([]SidecarWatchdogHold, 0)
	for rows.Next() {
		record, scanErr := scanSidecarWatchdogHold(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active sidecar watchdog holds: %w", err)
	}
	return records, nil
}

func (s *Store) UpdateWatchdogHold(ctx context.Context, id int, input SidecarWatchdogHoldInput) (SidecarWatchdogHold, error) {
	if err := s.requirePool(); err != nil {
		return SidecarWatchdogHold{}, err
	}
	record, err := updateWatchdogHold(ctx, s.pool, id, input)
	if err != nil {
		return SidecarWatchdogHold{}, mapStoreError(err)
	}
	return record, nil
}

func updateWatchdogHold(ctx context.Context, executor sidecarSQLExecutor, id int, input SidecarWatchdogHoldInput) (SidecarWatchdogHold, error) {
	if id <= 0 || input.SidecarID <= 0 || strings.TrimSpace(input.AuthID) == "" || strings.TrimSpace(input.Reason) == "" || strings.TrimSpace(input.ConditionHash) == "" || strings.TrimSpace(input.Status) == "" {
		return SidecarWatchdogHold{}, invalidInputError("id, sidecar_id, auth_id, reason, condition_hash, and status are required")
	}
	row := executor.QueryRow(ctx, `UPDATE sidecar_watchdog_holds SET
	auth_index = $2, provider = $3, reason = $4, condition_hash = $5,
	previous_priority = $6, target_priority = $7, hold_until = $8,
	manual_pause_until = $9, status = $10, last_action_id = $11,
	released_at = $12, updated_at = now()
WHERE id = $1 AND sidecar_id = $13
RETURNING `+sidecarWatchdogHoldSelectColumns,
		id, nullStringArg(input.AuthIndex), nullStringArg(input.Provider), strings.TrimSpace(input.Reason), strings.TrimSpace(input.ConditionHash),
		nullIntArg(input.PreviousPriority), input.TargetPriority, nullTimeArg(input.HoldUntil), nullTimeArg(input.ManualPauseUntil), strings.TrimSpace(input.Status),
		nullIntArg(input.LastActionID), nullTimeArg(input.ReleasedAt), input.SidecarID,
	)
	record, err := scanSidecarWatchdogHold(row)
	if err == pgx.ErrNoRows {
		return SidecarWatchdogHold{}, notFoundError("sidecar watchdog hold not found")
	}
	return record, err
}

func (s *Store) CreateWatchdogAction(ctx context.Context, input SidecarWatchdogActionInput) (SidecarWatchdogAction, error) {
	if err := s.requirePool(); err != nil {
		return SidecarWatchdogAction{}, err
	}
	if input.SidecarID <= 0 || strings.TrimSpace(input.ActionType) == "" || strings.TrimSpace(input.Status) == "" {
		return SidecarWatchdogAction{}, invalidInputError("sidecar_id, action_type, and status are required")
	}
	row := s.pool.QueryRow(ctx, `INSERT INTO sidecar_watchdog_actions (
sidecar_id, auth_snapshot_id, hold_id, auth_id, auth_name, auth_index, provider, action_type,
reason, previous_priority, target_priority, hold_until, status, error_message, completed_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
RETURNING `+sidecarWatchdogActionSelectColumns,
		input.SidecarID, nullIntArg(input.AuthSnapshotID), nullIntArg(input.HoldID), nullStringArg(input.AuthID),
		nullStringArg(input.AuthName), nullStringArg(input.AuthIndex), nullStringArg(input.Provider), strings.TrimSpace(input.ActionType),
		nullStringArg(input.Reason), nullIntArg(input.PreviousPriority), nullIntArg(input.TargetPriority),
		nullTimeArg(input.HoldUntil), strings.TrimSpace(input.Status), nullStringArg(input.ErrorMessage),
		nullTimeArg(input.CompletedAt),
	)
	record, err := scanSidecarWatchdogAction(row)
	if err != nil {
		return SidecarWatchdogAction{}, mapStoreError(err)
	}
	return record, nil
}

func (s *Store) UpdateWatchdogAction(ctx context.Context, id int, input SidecarWatchdogActionInput) (SidecarWatchdogAction, error) {
	if err := s.requirePool(); err != nil {
		return SidecarWatchdogAction{}, err
	}
	if id <= 0 || input.SidecarID <= 0 || strings.TrimSpace(input.ActionType) == "" || strings.TrimSpace(input.Status) == "" {
		return SidecarWatchdogAction{}, invalidInputError("id, sidecar_id, action_type, and status are required")
	}
	row := s.pool.QueryRow(ctx, `UPDATE sidecar_watchdog_actions SET
	auth_snapshot_id = $2, hold_id = $3, auth_id = $4, auth_name = $5, auth_index = $6,
	provider = $7, action_type = $8, reason = $9, previous_priority = $10,
	target_priority = $11, hold_until = $12, status = $13,
	error_message = $14, completed_at = $15, updated_at = now()
WHERE id = $1 AND sidecar_id = $16
RETURNING `+sidecarWatchdogActionSelectColumns,
		id, nullIntArg(input.AuthSnapshotID), nullIntArg(input.HoldID), nullStringArg(input.AuthID),
		nullStringArg(input.AuthName), nullStringArg(input.AuthIndex), nullStringArg(input.Provider), strings.TrimSpace(input.ActionType),
		nullStringArg(input.Reason), nullIntArg(input.PreviousPriority), nullIntArg(input.TargetPriority),
		nullTimeArg(input.HoldUntil), strings.TrimSpace(input.Status), nullStringArg(input.ErrorMessage),
		nullTimeArg(input.CompletedAt), input.SidecarID,
	)
	record, err := scanSidecarWatchdogAction(row)
	if err == pgx.ErrNoRows {
		return SidecarWatchdogAction{}, notFoundError("sidecar watchdog action not found")
	}
	if err != nil {
		return SidecarWatchdogAction{}, mapStoreError(err)
	}
	return record, nil
}

func (s *Store) ListWatchdogActions(ctx context.Context, sidecarID int) ([]SidecarWatchdogAction, error) {
	if err := s.requirePool(); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT `+sidecarWatchdogActionSelectColumns+` FROM sidecar_watchdog_actions WHERE sidecar_id = $1 ORDER BY created_at DESC, id DESC`, sidecarID)
	if err != nil {
		return nil, fmt.Errorf("query sidecar watchdog actions: %w", err)
	}
	defer rows.Close()
	records := make([]SidecarWatchdogAction, 0)
	for rows.Next() {
		record, scanErr := scanSidecarWatchdogAction(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sidecar watchdog actions: %w", err)
	}
	return records, nil
}

const sidecarInstanceSelectColumns = `id, name, base_url, base_url_canonical, management_password, enabled, environment_label,
sync_interval_seconds, request_timeout_seconds, allow_private_network, allow_insecure_http,
skip_tls_verify, last_sync_at, last_successful_sync_at, snapshot_stale_after,
last_sync_error, management_auth_state, auth_failure_pause_until, deleted_at, created_at, updated_at`

const sidecarAuthSnapshotSelectColumns = `id, sidecar_id, auth_id, auth_index, name, provider, label, status,
status_message, disabled, unavailable, priority, quota_exceeded, quota_reason,
quota_next_recover_at, next_retry_after, success_count, failed_count,
recent_requests_json, model_states_json, snapshot_json, observed_at, created_at, updated_at`

const sidecarProviderSnapshotSelectColumns = `id, sidecar_id, provider_key, provider_item_key, name, label,
status, disabled, snapshot_json, observed_at, created_at, updated_at`

const sidecarWatchdogPolicySelectColumns = `id, sidecar_id, enabled, failure_threshold, failure_window_seconds,
fallback_cooldown_seconds, deprioritized_priority, prioritized_priority, manual_override_pause_seconds,
probe_batch_size, probe_timeout_seconds, probe_cursor_auth_id, created_at, updated_at`

const sidecarWatchdogProbeObservationSelectColumns = `id, sidecar_id, auth_id, auth_index, provider, probed_at,
probe_status, upstream_status_code, quota_exceeded, quota_reason, quota_reset_at,
blocking_window, windows_json, error_code, created_at`

const sidecarWatchdogHoldSelectColumns = `id, sidecar_id, auth_id, auth_index, provider, reason, condition_hash,
previous_priority, target_priority, hold_until, manual_pause_until, status, last_action_id,
created_at, updated_at, released_at`

const sidecarWatchdogActionSelectColumns = `id, sidecar_id, auth_snapshot_id, hold_id, auth_id, auth_name, auth_index,
provider, action_type, reason, previous_priority, target_priority, hold_until, status,
error_message, created_at, updated_at, completed_at`

func (s *Store) normalizeInstanceInput(input SidecarInstanceInput) (SidecarInstanceInput, error) {
	if strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.BaseURL) == "" || strings.TrimSpace(input.BaseURLCanonical) == "" {
		return SidecarInstanceInput{}, invalidInputError("name, base_url, and base_url_canonical are required")
	}
	trimmedPassword := strings.TrimSpace(input.ManagementPassword)
	if trimmedPassword == "" {
		return SidecarInstanceInput{}, invalidInputError("management_password is required")
	}
	if strings.HasPrefix(trimmedPassword, encryptedSecretPrefix) {
		if !input.ManagementPasswordIsEncrypted {
			return SidecarInstanceInput{}, invalidInputError("management_password must not use the reserved encrypted secret prefix")
		}
		input.ManagementPassword = trimmedPassword
	} else {
		encryptedPassword, err := endpointdomain.EncryptSecret(trimmedPassword, s.secretEncryptionKey, s.now)
		if err != nil {
			return SidecarInstanceInput{}, fmt.Errorf("encrypt sidecar management password: %w", err)
		}
		input.ManagementPassword = encryptedPassword
	}
	input.Name = strings.TrimSpace(input.Name)
	input.BaseURL = strings.TrimSpace(input.BaseURL)
	input.BaseURLCanonical = strings.TrimSpace(input.BaseURLCanonical)
	if input.SyncIntervalSeconds <= 0 {
		input.SyncIntervalSeconds = DefaultSyncIntervalSeconds
	}
	if input.RequestTimeoutSeconds <= 0 {
		input.RequestTimeoutSeconds = DefaultRequestTimeoutSeconds
	}
	input.ManagementAuthState = strings.TrimSpace(input.ManagementAuthState)
	if input.ManagementAuthState == "" {
		input.ManagementAuthState = ManagementAuthStateUnknown
	}
	return input, nil
}

func normalizePolicyInput(input SidecarWatchdogPolicyInput) (SidecarWatchdogPolicyInput, error) {
	if input.FailureThreshold <= 0 {
		input.FailureThreshold = DefaultFailureThreshold
	}
	if input.FailureWindowSeconds <= 0 {
		input.FailureWindowSeconds = DefaultFailureWindowSeconds
	}
	if input.FallbackCooldownSeconds <= 0 {
		input.FallbackCooldownSeconds = DefaultFallbackCooldownSeconds
	}
	if input.PrioritizedPriority <= 0 {
		input.PrioritizedPriority = DefaultPrioritizedPriority
		if input.PrioritizedPriority <= input.DeprioritizedPriority {
			input.PrioritizedPriority = input.DeprioritizedPriority + 1
		}
	}
	if input.ManualOverridePauseSeconds <= 0 {
		input.ManualOverridePauseSeconds = DefaultManualOverridePauseSeconds
	}
	if input.ProbeBatchSize <= 0 {
		input.ProbeBatchSize = DefaultProbeBatchSize
	}
	if input.ProbeTimeoutSeconds <= 0 {
		input.ProbeTimeoutSeconds = DefaultProbeTimeoutSeconds
	}
	if input.DeprioritizedPriority < 0 || input.PrioritizedPriority < 0 {
		return SidecarWatchdogPolicyInput{}, invalidInputError("watchdog priorities must be non-negative")
	}
	if input.DeprioritizedPriority >= input.PrioritizedPriority {
		return SidecarWatchdogPolicyInput{}, invalidInputError("deprioritized_priority must be less than prioritized_priority")
	}
	maxBudgetSeconds := watchdogProbeBudgetMaxSeconds()
	if input.ProbeTimeoutSeconds > maxBudgetSeconds {
		return SidecarWatchdogPolicyInput{}, invalidInputError("probe_timeout_seconds exceeds watchdog worker budget")
	}
	if input.ProbeBatchSize*input.ProbeTimeoutSeconds > maxBudgetSeconds {
		return SidecarWatchdogPolicyInput{}, invalidInputError("probe batch budget exceeds watchdog worker budget")
	}
	return input, nil
}

func watchdogProbeBudgetMaxSeconds() int {
	budget := int((sidecarWatchdogWorkerTimeout - sidecarWatchdogWorkerSafetyMargin()) / time.Second)
	if budget < 1 {
		return 1
	}
	return budget
}

func (s *Store) currentTime() time.Time {
	if s == nil || s.now == nil {
		return time.Now().UTC()
	}
	return s.now().UTC()
}

func (s *Store) requirePool() error {
	if s == nil || s.pool == nil {
		return invalidInputError("sidecar store pool is required")
	}
	return nil
}

func scanSidecarInstance(scanner interface{ Scan(...any) error }) (SidecarInstance, error) {
	var record SidecarInstance
	var environmentLabel, lastSyncError sql.NullString
	var lastSyncAt, lastSuccessfulSyncAt, snapshotStaleAfter sql.NullTime
	var authFailurePauseUntil, deletedAt sql.NullTime
	err := scanner.Scan(
		&record.ID,
		&record.Name,
		&record.BaseURL,
		&record.BaseURLCanonical,
		&record.EncryptedManagementPassword,
		&record.Enabled,
		&environmentLabel,
		&record.SyncIntervalSeconds,
		&record.RequestTimeoutSeconds,
		&record.AllowPrivateNetwork,
		&record.AllowInsecureHTTP,
		&record.SkipTLSVerify,
		&lastSyncAt,
		&lastSuccessfulSyncAt,
		&snapshotStaleAfter,
		&lastSyncError,
		&record.ManagementAuthState,
		&authFailurePauseUntil,
		&deletedAt,
		&record.CreatedAt,
		&record.UpdatedAt,
	)
	if err != nil {
		return SidecarInstance{}, err
	}
	record.EnvironmentLabel = stringFromNull(environmentLabel)
	record.LastSyncAt = timeFromNull(lastSyncAt)
	record.LastSuccessfulSyncAt = timeFromNull(lastSuccessfulSyncAt)
	record.SnapshotStaleAfter = timeFromNull(snapshotStaleAfter)
	record.LastSyncError = stringFromNull(lastSyncError)
	record.AuthFailurePauseUntil = timeFromNull(authFailurePauseUntil)
	record.DeletedAt = timeFromNull(deletedAt)
	return record, nil
}

func scanSidecarAuthSnapshot(scanner interface{ Scan(...any) error }) (SidecarAuthSnapshot, error) {
	var record SidecarAuthSnapshot
	var authIndex, provider, label, status, statusMessage, quotaReason sql.NullString
	var disabled, unavailable, quotaExceeded sql.NullBool
	var priority, successCount, failedCount sql.NullInt64
	var quotaNextRecoverAt, nextRetryAfter sql.NullTime
	var recentRequests, modelStates, snapshot []byte
	err := scanner.Scan(
		&record.ID,
		&record.SidecarID,
		&record.AuthID,
		&authIndex,
		&record.Name,
		&provider,
		&label,
		&status,
		&statusMessage,
		&disabled,
		&unavailable,
		&priority,
		&quotaExceeded,
		&quotaReason,
		&quotaNextRecoverAt,
		&nextRetryAfter,
		&successCount,
		&failedCount,
		&recentRequests,
		&modelStates,
		&snapshot,
		&record.ObservedAt,
		&record.CreatedAt,
		&record.UpdatedAt,
	)
	if err != nil {
		return SidecarAuthSnapshot{}, err
	}
	record.AuthIndex = stringFromNull(authIndex)
	record.Provider = stringFromNull(provider)
	record.Label = stringFromNull(label)
	record.Status = stringFromNull(status)
	record.StatusMessage = stringFromNull(statusMessage)
	record.Disabled = boolFromNull(disabled)
	record.Unavailable = boolFromNull(unavailable)
	record.Priority = intFromNull(priority)
	record.QuotaExceeded = boolFromNull(quotaExceeded)
	record.QuotaReason = stringFromNull(quotaReason)
	record.QuotaNextRecoverAt = timeFromNull(quotaNextRecoverAt)
	record.NextRetryAfter = timeFromNull(nextRetryAfter)
	record.SuccessCount = intFromNull(successCount)
	record.FailedCount = intFromNull(failedCount)
	record.RecentRequestsJSON = cloneJSON(recentRequests)
	record.ModelStatesJSON = cloneJSON(modelStates)
	record.SnapshotJSON = cloneJSON(snapshot)
	return record, nil
}

func scanSidecarProviderSnapshot(scanner interface{ Scan(...any) error }) (SidecarProviderSnapshot, error) {
	var record SidecarProviderSnapshot
	var name, label, status sql.NullString
	var disabled sql.NullBool
	var snapshot []byte
	err := scanner.Scan(
		&record.ID,
		&record.SidecarID,
		&record.ProviderKey,
		&record.ProviderItemKey,
		&name,
		&label,
		&status,
		&disabled,
		&snapshot,
		&record.ObservedAt,
		&record.CreatedAt,
		&record.UpdatedAt,
	)
	if err != nil {
		return SidecarProviderSnapshot{}, err
	}
	record.Name = stringFromNull(name)
	record.Label = stringFromNull(label)
	record.Status = stringFromNull(status)
	record.Disabled = boolFromNull(disabled)
	record.SnapshotJSON = cloneJSON(snapshot)
	return record, nil
}

func scanSidecarWatchdogPolicy(scanner interface{ Scan(...any) error }) (SidecarWatchdogPolicy, error) {
	var record SidecarWatchdogPolicy
	var probeCursorAuthID sql.NullString
	err := scanner.Scan(
		&record.ID,
		&record.SidecarID,
		&record.Enabled,
		&record.FailureThreshold,
		&record.FailureWindowSeconds,
		&record.FallbackCooldownSeconds,
		&record.DeprioritizedPriority,
		&record.PrioritizedPriority,
		&record.ManualOverridePauseSeconds,
		&record.ProbeBatchSize,
		&record.ProbeTimeoutSeconds,
		&probeCursorAuthID,
		&record.CreatedAt,
		&record.UpdatedAt,
	)
	if err != nil {
		return SidecarWatchdogPolicy{}, err
	}
	record.ProbeCursorAuthID = stringFromNull(probeCursorAuthID)
	return record, nil
}

func scanSidecarWatchdogProbeObservation(scanner interface{ Scan(...any) error }) (SidecarWatchdogProbeObservation, error) {
	var record SidecarWatchdogProbeObservation
	var authIndex, provider, quotaReason, blockingWindow, errorCode sql.NullString
	var upstreamStatusCode sql.NullInt64
	var quotaResetAt sql.NullTime
	var windows []byte
	err := scanner.Scan(
		&record.ID,
		&record.SidecarID,
		&record.AuthID,
		&authIndex,
		&provider,
		&record.ProbedAt,
		&record.ProbeStatus,
		&upstreamStatusCode,
		&record.QuotaExceeded,
		&quotaReason,
		&quotaResetAt,
		&blockingWindow,
		&windows,
		&errorCode,
		&record.CreatedAt,
	)
	if err != nil {
		return SidecarWatchdogProbeObservation{}, err
	}
	record.AuthIndex = stringFromNull(authIndex)
	record.Provider = stringFromNull(provider)
	record.UpstreamStatusCode = intFromNull(upstreamStatusCode)
	record.QuotaReason = stringFromNull(quotaReason)
	record.QuotaResetAt = timeFromNull(quotaResetAt)
	record.BlockingWindow = stringFromNull(blockingWindow)
	record.WindowsJSON = cloneJSON(windows)
	record.ErrorCode = stringFromNull(errorCode)
	return record, nil
}

func scanSidecarWatchdogHold(scanner interface{ Scan(...any) error }) (SidecarWatchdogHold, error) {
	var record SidecarWatchdogHold
	var authIndex, provider sql.NullString
	var previousPriority, lastActionID sql.NullInt64
	var holdUntil, manualPauseUntil, releasedAt sql.NullTime
	err := scanner.Scan(
		&record.ID,
		&record.SidecarID,
		&record.AuthID,
		&authIndex,
		&provider,
		&record.Reason,
		&record.ConditionHash,
		&previousPriority,
		&record.TargetPriority,
		&holdUntil,
		&manualPauseUntil,
		&record.Status,
		&lastActionID,
		&record.CreatedAt,
		&record.UpdatedAt,
		&releasedAt,
	)
	if err != nil {
		return SidecarWatchdogHold{}, err
	}
	record.AuthIndex = stringFromNull(authIndex)
	record.Provider = stringFromNull(provider)
	record.PreviousPriority = intFromNull(previousPriority)
	record.HoldUntil = timeFromNull(holdUntil)
	record.ManualPauseUntil = timeFromNull(manualPauseUntil)
	record.LastActionID = intFromNull(lastActionID)
	record.ReleasedAt = timeFromNull(releasedAt)
	return record, nil
}

func scanSidecarWatchdogAction(scanner interface{ Scan(...any) error }) (SidecarWatchdogAction, error) {
	var record SidecarWatchdogAction
	var authSnapshotID, holdID, previousPriority, targetPriority sql.NullInt64
	var authID, authName, authIndex, provider, reason, errorMessage sql.NullString
	var holdUntil, completedAt sql.NullTime
	err := scanner.Scan(
		&record.ID,
		&record.SidecarID,
		&authSnapshotID,
		&holdID,
		&authID,
		&authName,
		&authIndex,
		&provider,
		&record.ActionType,
		&reason,
		&previousPriority,
		&targetPriority,
		&holdUntil,
		&record.Status,
		&errorMessage,
		&record.CreatedAt,
		&record.UpdatedAt,
		&completedAt,
	)
	if err != nil {
		return SidecarWatchdogAction{}, err
	}
	record.AuthSnapshotID = intFromNull(authSnapshotID)
	record.HoldID = intFromNull(holdID)
	record.AuthID = stringFromNull(authID)
	record.AuthName = stringFromNull(authName)
	record.AuthIndex = stringFromNull(authIndex)
	record.Provider = stringFromNull(provider)
	record.Reason = stringFromNull(reason)
	record.PreviousPriority = intFromNull(previousPriority)
	record.TargetPriority = intFromNull(targetPriority)
	record.HoldUntil = timeFromNull(holdUntil)
	record.ErrorMessage = stringFromNull(errorMessage)
	record.CompletedAt = timeFromNull(completedAt)
	return record, nil
}

func normalizeWatchdogProbeObservationInput(input SidecarWatchdogProbeObservationInput, now time.Time) (SidecarWatchdogProbeObservationInput, error) {
	input.AuthID = strings.TrimSpace(input.AuthID)
	input.ProbeStatus = strings.TrimSpace(input.ProbeStatus)
	if input.SidecarID <= 0 || input.AuthID == "" || input.ProbeStatus == "" {
		return SidecarWatchdogProbeObservationInput{}, invalidInputError("sidecar_id, auth_id, and probe_status are required")
	}
	if input.ProbedAt.IsZero() {
		input.ProbedAt = now
	}
	if input.UpstreamStatusCode != nil && (*input.UpstreamStatusCode < 100 || *input.UpstreamStatusCode > 599) {
		return SidecarWatchdogProbeObservationInput{}, invalidInputError("upstream_status_code must be between 100 and 599")
	}
	if err := validateWatchdogProbeWindowsJSON(input.WindowsJSON); err != nil {
		return SidecarWatchdogProbeObservationInput{}, err
	}
	return input, nil
}

func validateWatchdogProbeWindowsJSON(raw json.RawMessage) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil
	}
	var windows []map[string]any
	if err := json.Unmarshal(trimmed, &windows); err != nil {
		return invalidInputError("probe observation windows_json must be an array of sanitized window summaries")
	}
	for _, window := range windows {
		for key, value := range window {
			if !isAllowedProbeWindowSummaryKey(key) {
				return invalidInputError("probe observation windows_json must contain only sanitized window summary fields")
			}
			switch value.(type) {
			case map[string]any, []any:
				return invalidInputError("probe observation windows_json must not contain nested raw payloads")
			}
		}
	}
	return nil
}

func isAllowedProbeWindowSummaryKey(key string) bool {
	switch normalizedSnapshotKey(key) {
	case "source", "windowtype", "usedpercent", "limitreached", "allowed", "resetat", "limitwindowseconds", "blocking":
		return true
	default:
		return false
	}
}

func validateSidecarSnapshotJSON(raw json.RawMessage) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil
	}
	var payload any
	if err := json.Unmarshal(trimmed, &payload); err != nil {
		return invalidInputError("sidecar snapshot JSON must be valid JSON")
	}
	var found error
	walkSidecarSnapshotJSON(payload, func(key string, value any) {
		if found != nil {
			return
		}
		if isSecretPresenceMarkerKey(key) {
			if _, ok := value.(bool); !ok {
				found = invalidInputError("sidecar snapshot secret_present marker must be boolean")
			}
			return
		}
		if !isSensitiveSnapshotKey(key) {
			return
		}
		text, ok := value.(string)
		if !ok || !isAllowedRedactedSecretValue(text) {
			found = invalidInputError("sidecar snapshot JSON must not contain raw secret values")
		}
	})
	return found
}

func validateAuthSnapshotReplacementInputs(sidecarID int, inputs []SidecarAuthSnapshotInput) ([]SidecarAuthSnapshotInput, error) {
	if sidecarID <= 0 {
		return nil, invalidInputError("sidecar_id is required")
	}
	normalized := make([]SidecarAuthSnapshotInput, len(inputs))
	seenAuthIDs := make(map[string]struct{}, len(inputs))
	for i, input := range inputs {
		input.AuthID = strings.TrimSpace(input.AuthID)
		input.Name = strings.TrimSpace(input.Name)
		if input.SidecarID != sidecarID || input.AuthID == "" || input.Name == "" {
			return nil, invalidInputError("auth replacement input does not match sidecar batch")
		}
		if _, exists := seenAuthIDs[input.AuthID]; exists {
			return nil, invalidInputError("auth replacement input has duplicate auth_id")
		}
		seenAuthIDs[input.AuthID] = struct{}{}
		if err := validateSidecarSnapshotJSON(input.SnapshotJSON); err != nil {
			return nil, err
		}
		if err := validateJSONBInput(input.RecentRequestsJSON, "recent_requests_json"); err != nil {
			return nil, err
		}
		if err := validateJSONBInput(input.ModelStatesJSON, "model_states_json"); err != nil {
			return nil, err
		}
		normalized[i] = input
	}
	return normalized, nil
}

func validateJSONBInput(raw json.RawMessage, field string) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil
	}
	var payload any
	if err := json.Unmarshal(trimmed, &payload); err != nil {
		return invalidInputError("sidecar snapshot " + field + " must be valid JSON")
	}
	return nil
}

func walkSidecarSnapshotJSON(value any, visit func(string, any)) {
	switch typed := value.(type) {
	case map[string]any:
		for key, nested := range typed {
			visit(key, nested)
			walkSidecarSnapshotJSON(nested, visit)
		}
	case []any:
		for _, nested := range typed {
			walkSidecarSnapshotJSON(nested, visit)
		}
	}
}

func isSecretPresenceMarkerKey(key string) bool {
	return normalizedSnapshotKey(key) == "secretpresent"
}

func isSensitiveSnapshotKey(key string) bool {
	normalized := normalizedSnapshotKey(key)
	return normalized == "apikey" ||
		normalized == "managementpassword" ||
		normalized == "authorization" ||
		strings.Contains(normalized, "cookie") ||
		normalized == "xapikey" ||
		normalized == "xapitoken" ||
		normalized == "token" ||
		strings.HasSuffix(normalized, "apikey") ||
		strings.HasSuffix(normalized, "token") ||
		strings.HasSuffix(normalized, "password") ||
		strings.HasSuffix(normalized, "managementkey") ||
		strings.Contains(normalized, "secret")
}

func normalizedSnapshotKey(key string) string {
	return strings.NewReplacer("-", "", "_", "", " ", "").Replace(strings.ToLower(strings.TrimSpace(key)))
}

func isAllowedRedactedSecretValue(value string) bool {
	trimmed := strings.TrimSpace(value)
	return trimmed == "" || strings.HasPrefix(strings.ToLower(trimmed), "redacted-") || strings.Contains(trimmed, "***")
}

func mapStoreError(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		if pgErr.Code == "23505" {
			switch pgErr.ConstraintName {
			case "uq_sidecar_instances_live_name":
				return &StoreError{Code: StoreErrorDuplicateSidecarName, Message: "sidecar name already exists", Err: err}
			case "uq_sidecar_instances_live_base_url_canonical":
				return &StoreError{Code: StoreErrorDuplicateSidecarCanonicalURL, Message: "sidecar canonical base URL already exists", Err: err}
			case "uq_sidecar_watchdog_holds_active_auth":
				return &StoreError{Code: StoreErrorDuplicateActiveHold, Message: "active sidecar watchdog hold already exists", Err: err}
			}
		}
		if pgErr.Code == "23503" || pgErr.Code == "23514" || pgErr.Code == "22P02" {
			return &StoreError{Code: StoreErrorInvalidInput, Message: "sidecar persistence input violates schema contract", Err: err}
		}
	}
	return err
}

func invalidInputError(message string) error {
	return &StoreError{Code: StoreErrorInvalidInput, Message: message}
}

func notFoundError(message string) error {
	return &StoreError{Code: StoreErrorNotFound, Message: message}
}

func boolValueOr(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func nullStringArg(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullBoolArg(value *bool) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullIntArg(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullTimeArg(value *time.Time) any {
	if value == nil {
		return nil
	}
	return *value
}

func stringFromNull(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	copied := value.String
	return &copied
}

func boolFromNull(value sql.NullBool) *bool {
	if !value.Valid {
		return nil
	}
	copied := value.Bool
	return &copied
}

func intFromNull(value sql.NullInt64) *int {
	if !value.Valid {
		return nil
	}
	copied := int(value.Int64)
	return &copied
}

func timeFromNull(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	copied := value.Time
	return &copied
}

func jsonbString(value json.RawMessage, fallback string) string {
	trimmed := bytes.TrimSpace(value)
	if len(trimmed) == 0 {
		return fallback
	}
	return string(trimmed)
}

func cloneJSON(value []byte) json.RawMessage {
	if len(value) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), value...)
}
