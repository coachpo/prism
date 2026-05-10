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
	normalized := normalizePolicyInput(input)
	row := s.pool.QueryRow(ctx, `INSERT INTO sidecar_watchdog_policies (
sidecar_id, enabled, failure_threshold, failure_window_seconds, fallback_cooldown_seconds,
deprioritized_priority, manual_override_pause_seconds)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (sidecar_id) DO UPDATE SET enabled = EXCLUDED.enabled,
failure_threshold = EXCLUDED.failure_threshold, failure_window_seconds = EXCLUDED.failure_window_seconds,
fallback_cooldown_seconds = EXCLUDED.fallback_cooldown_seconds,
deprioritized_priority = EXCLUDED.deprioritized_priority,
manual_override_pause_seconds = EXCLUDED.manual_override_pause_seconds, updated_at = now()
RETURNING `+sidecarWatchdogPolicySelectColumns,
		normalized.SidecarID, normalized.Enabled, normalized.FailureThreshold,
		normalized.FailureWindowSeconds, normalized.FallbackCooldownSeconds,
		normalized.DeprioritizedPriority, normalized.ManualOverridePauseSeconds,
	)
	record, err := scanSidecarWatchdogPolicy(row)
	if err != nil {
		return SidecarWatchdogPolicy{}, mapStoreError(err)
	}
	return record, nil
}

func (s *Store) CreateWatchdogHold(ctx context.Context, input SidecarWatchdogHoldInput) (SidecarWatchdogHold, error) {
	if err := s.requirePool(); err != nil {
		return SidecarWatchdogHold{}, err
	}
	if input.SidecarID <= 0 || strings.TrimSpace(input.AuthID) == "" || strings.TrimSpace(input.Reason) == "" || strings.TrimSpace(input.ConditionHash) == "" || strings.TrimSpace(input.Status) == "" {
		return SidecarWatchdogHold{}, invalidInputError("sidecar_id, auth_id, reason, condition_hash, and status are required")
	}
	row := s.pool.QueryRow(ctx, `INSERT INTO sidecar_watchdog_holds (
sidecar_id, auth_id, auth_index, provider, reason, condition_hash, previous_priority,
target_priority, hold_until, manual_pause_until, status, last_action_id, released_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
RETURNING `+sidecarWatchdogHoldSelectColumns,
		input.SidecarID, strings.TrimSpace(input.AuthID), nullStringArg(input.AuthIndex), nullStringArg(input.Provider),
		strings.TrimSpace(input.Reason), strings.TrimSpace(input.ConditionHash), nullIntArg(input.PreviousPriority),
		input.TargetPriority, nullTimeArg(input.HoldUntil), nullTimeArg(input.ManualPauseUntil), strings.TrimSpace(input.Status),
		nullIntArg(input.LastActionID), nullTimeArg(input.ReleasedAt),
	)
	record, err := scanSidecarWatchdogHold(row)
	if err != nil {
		return SidecarWatchdogHold{}, mapStoreError(err)
	}
	return record, nil
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
	if id <= 0 || input.SidecarID <= 0 || strings.TrimSpace(input.AuthID) == "" || strings.TrimSpace(input.Reason) == "" || strings.TrimSpace(input.ConditionHash) == "" || strings.TrimSpace(input.Status) == "" {
		return SidecarWatchdogHold{}, invalidInputError("id, sidecar_id, auth_id, reason, condition_hash, and status are required")
	}
	row := s.pool.QueryRow(ctx, `UPDATE sidecar_watchdog_holds SET
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
	if err != nil {
		return SidecarWatchdogHold{}, mapStoreError(err)
	}
	return record, nil
}

func (s *Store) CreateWatchdogAction(ctx context.Context, input SidecarWatchdogActionInput) (SidecarWatchdogAction, error) {
	if err := s.requirePool(); err != nil {
		return SidecarWatchdogAction{}, err
	}
	if input.SidecarID <= 0 || strings.TrimSpace(input.ActionType) == "" || strings.TrimSpace(input.Status) == "" {
		return SidecarWatchdogAction{}, invalidInputError("sidecar_id, action_type, and status are required")
	}
	row := s.pool.QueryRow(ctx, `INSERT INTO sidecar_watchdog_actions (
sidecar_id, auth_snapshot_id, hold_id, auth_id, auth_index, provider, action_type,
reason, previous_priority, target_priority, hold_until, status, error_message, completed_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
RETURNING `+sidecarWatchdogActionSelectColumns,
		input.SidecarID, nullIntArg(input.AuthSnapshotID), nullIntArg(input.HoldID), nullStringArg(input.AuthID),
		nullStringArg(input.AuthIndex), nullStringArg(input.Provider), strings.TrimSpace(input.ActionType),
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
fallback_cooldown_seconds, deprioritized_priority, manual_override_pause_seconds, created_at, updated_at`

const sidecarWatchdogHoldSelectColumns = `id, sidecar_id, auth_id, auth_index, provider, reason, condition_hash,
previous_priority, target_priority, hold_until, manual_pause_until, status, last_action_id,
created_at, updated_at, released_at`

const sidecarWatchdogActionSelectColumns = `id, sidecar_id, auth_snapshot_id, hold_id, auth_id, auth_index,
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

func normalizePolicyInput(input SidecarWatchdogPolicyInput) SidecarWatchdogPolicyInput {
	if input.FailureThreshold <= 0 {
		input.FailureThreshold = DefaultFailureThreshold
	}
	if input.FailureWindowSeconds <= 0 {
		input.FailureWindowSeconds = DefaultFailureWindowSeconds
	}
	if input.FallbackCooldownSeconds <= 0 {
		input.FallbackCooldownSeconds = DefaultFallbackCooldownSeconds
	}
	if input.ManualOverridePauseSeconds <= 0 {
		input.ManualOverridePauseSeconds = DefaultManualOverridePauseSeconds
	}
	return input
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
	err := scanner.Scan(
		&record.ID,
		&record.SidecarID,
		&record.Enabled,
		&record.FailureThreshold,
		&record.FailureWindowSeconds,
		&record.FallbackCooldownSeconds,
		&record.DeprioritizedPriority,
		&record.ManualOverridePauseSeconds,
		&record.CreatedAt,
		&record.UpdatedAt,
	)
	if err != nil {
		return SidecarWatchdogPolicy{}, err
	}
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
	var authID, authIndex, provider, reason, errorMessage sql.NullString
	var holdUntil, completedAt sql.NullTime
	err := scanner.Scan(
		&record.ID,
		&record.SidecarID,
		&authSnapshotID,
		&holdID,
		&authID,
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
