package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	auditdomain "github.com/coachpo/prism/backend/internal/domain/audit"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const proxyKeyLimit = 100

type proxyAPIKeyRow struct {
	ID                     int
	Name                   string
	KeyPrefix              string
	KeyHash                string
	LastFour               string
	IsActive               bool
	ExpiresAt              sql.NullTime
	LastUsedAt             sql.NullTime
	LastUsedIP             sql.NullString
	CreatedByAuthSubjectID sql.NullInt32
	Notes                  sql.NullString
	RotatedAt              sql.NullTime
	RotationCount          int
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

func (s *Service) listProxyAPIKeys(ctx context.Context, exec queryExecutor) ([]proxyAPIKeyRow, error) {
	rows, err := exec.Query(
		ctx,
		`SELECT id, name, key_prefix, key_hash, last_four, is_active, expires_at, last_used_at,
			last_used_ip, created_by_auth_subject_id, notes, rotated_at, rotation_count, created_at, updated_at
		FROM proxy_api_keys
		ORDER BY id ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("query proxy api keys: %w", err)
	}
	defer rows.Close()
	items := []proxyAPIKeyRow{}
	for rows.Next() {
		row, err := scanProxyAPIKey(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate proxy api keys: %w", err)
	}
	return items, nil
}

func scanProxyAPIKey(scanner interface{ Scan(...any) error }) (proxyAPIKeyRow, error) {
	row := proxyAPIKeyRow{}
	err := scanner.Scan(
		&row.ID,
		&row.Name,
		&row.KeyPrefix,
		&row.KeyHash,
		&row.LastFour,
		&row.IsActive,
		&row.ExpiresAt,
		&row.LastUsedAt,
		&row.LastUsedIP,
		&row.CreatedByAuthSubjectID,
		&row.Notes,
		&row.RotatedAt,
		&row.RotationCount,
		&row.CreatedAt,
		&row.UpdatedAt,
	)
	if err != nil {
		return proxyAPIKeyRow{}, err
	}
	return row, nil
}

func (s *Service) createProxyAPIKey(ctx context.Context, tx pgx.Tx, name string, notes *string, expiresAt *time.Time, authSubjectID *int) (string, proxyAPIKeyRow, proxyKeyCapacitySnapshot, error) {
	now := s.nowUTC()
	resolvedExpiresAt, expiryErr := resolveProxyKeyCreateExpiry(expiresAt, now)
	if expiryErr != nil {
		return "", proxyAPIKeyRow{}, proxyKeyCapacitySnapshot{}, expiryErr
	}
	if err := lockProxyKeyCapacitySerialization(ctx, tx); err != nil {
		return "", proxyAPIKeyRow{}, proxyKeyCapacitySnapshot{}, err
	}
	capacity, err := countProxyKeyCapacity(ctx, tx, now)
	if err != nil {
		return "", proxyAPIKeyRow{}, proxyKeyCapacitySnapshot{}, fmt.Errorf("count proxy api keys: %w", err)
	}
	if capacity.Used >= proxyKeyLimit {
		return "", proxyAPIKeyRow{}, proxyKeyCapacitySnapshot{}, &domainError{StatusCode: http.StatusConflict, Code: "proxy_key_capacity_exhausted", Detail: fmt.Sprintf("Maximum %d proxy API keys reached", proxyKeyLimit)}
	}
	for range 5 {
		rawKey, keyPrefix, lastFour, keyHash, err := buildProxyAPIKey()
		if err != nil {
			return "", proxyAPIKeyRow{}, proxyKeyCapacitySnapshot{}, err
		}
		scanner := tx.QueryRow(
			ctx,
			`INSERT INTO proxy_api_keys (
				name,
				key_prefix,
				key_hash,
				last_four,
				is_active,
				expires_at,
				last_used_at,
				last_used_ip,
				created_by_auth_subject_id,
				notes,
				created_at,
				updated_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
			RETURNING id, name, key_prefix, key_hash, last_four, is_active, expires_at, last_used_at,
				last_used_ip, created_by_auth_subject_id, notes, rotated_at, rotation_count, created_at, updated_at`,
			name,
			keyPrefix,
			keyHash,
			lastFour,
			true,
			nullableTimePtr(resolvedExpiresAt),
			nil,
			nil,
			nullableInt32(authSubjectID),
			nullableTrimmedStringPtr(notes),
			now,
			now,
		)
		row, scanErr := scanProxyAPIKey(scanner)
		if scanErr == nil {
			insertedCapacity, capacityErr := countProxyKeyCapacity(ctx, tx, s.nowUTC())
			if capacityErr != nil {
				return "", proxyAPIKeyRow{}, proxyKeyCapacitySnapshot{}, fmt.Errorf("count proxy api keys after insert: %w", capacityErr)
			}
			if _, readinessErr := s.captureProxyKeyReadiness(ctx, tx); readinessErr != nil {
				return "", proxyAPIKeyRow{}, proxyKeyCapacitySnapshot{}, fmt.Errorf("refresh proxy key readiness after create: %w", readinessErr)
			}
			return rawKey, row, insertedCapacity, nil
		}
		if !isUniqueConstraintError(scanErr, "uq_proxy_api_keys_prefix") {
			return "", proxyAPIKeyRow{}, proxyKeyCapacitySnapshot{}, fmt.Errorf("insert proxy api key: %w", scanErr)
		}
	}
	return "", proxyAPIKeyRow{}, proxyKeyCapacitySnapshot{}, &domainError{StatusCode: 500, Detail: "Failed to generate a unique proxy API key"}
}

func (s *Service) loadProxyAPIKeyByID(ctx context.Context, exec queryExecutor, keyID int) (proxyAPIKeyRow, error) {
	scanner := exec.QueryRow(
		ctx,
		`SELECT id, name, key_prefix, key_hash, last_four, is_active, expires_at, last_used_at,
			last_used_ip, created_by_auth_subject_id, notes, rotated_at, rotation_count, created_at, updated_at
		FROM proxy_api_keys WHERE id = $1 LIMIT 1`,
		keyID,
	)
	return scanProxyAPIKey(scanner)
}

func (s *Service) updateProxyAPIKey(ctx context.Context, tx pgx.Tx, keyID int, name string, notes *string, isActive *bool, expiry proxyKeyExpiryUpdate) (proxyAPIKeyRow, proxyKeyCapacitySnapshot, error) {
	if err := lockProxyKeyCapacitySerialization(ctx, tx); err != nil {
		return proxyAPIKeyRow{}, proxyKeyCapacitySnapshot{}, err
	}
	current, err := s.loadProxyAPIKeyByID(ctx, tx, keyID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return proxyAPIKeyRow{}, proxyKeyCapacitySnapshot{}, &domainError{StatusCode: 404, Code: "proxy_key_not_found", Detail: "Proxy API key not found"}
		}
		return proxyAPIKeyRow{}, proxyKeyCapacitySnapshot{}, fmt.Errorf("load proxy api key: %w", err)
	}
	now := s.nowUTC()
	activeValue := current.IsActive
	if isActive != nil {
		activeValue = *isActive
	}
	resolvedExpiresAt := current.ExpiresAt
	if expiry.present {
		if expiry.clear {
			resolvedExpiresAt = sql.NullTime{}
		} else {
			future, expiryErr := resolveProxyKeyFutureExpiry(expiry.value, now)
			if expiryErr != nil {
				return proxyAPIKeyRow{}, proxyKeyCapacitySnapshot{}, expiryErr
			}
			resolvedExpiresAt = sql.NullTime{Time: *future, Valid: true}
		}
	}
	scanner := tx.QueryRow(
		ctx,
		`UPDATE proxy_api_keys
		SET name = $2, notes = $3, is_active = $4, expires_at = $5, updated_at = $6
		WHERE id = $1
		RETURNING id, name, key_prefix, key_hash, last_four, is_active, expires_at, last_used_at,
			last_used_ip, created_by_auth_subject_id, notes, rotated_at, rotation_count, created_at, updated_at`,
		keyID,
		name,
		nullableTrimmedStringPtr(notes),
		activeValue,
		nullableTimePtr(nullableTime(resolvedExpiresAt)),
		now,
	)
	row, err := scanProxyAPIKey(scanner)
	if err != nil {
		return proxyAPIKeyRow{}, proxyKeyCapacitySnapshot{}, fmt.Errorf("update proxy api key: %w", err)
	}
	capacity, err := countProxyKeyCapacity(ctx, tx, s.nowUTC())
	if err != nil {
		return proxyAPIKeyRow{}, proxyKeyCapacitySnapshot{}, fmt.Errorf("count proxy api keys after update: %w", err)
	}
	if _, err := s.captureProxyKeyReadiness(ctx, tx); err != nil {
		return proxyAPIKeyRow{}, proxyKeyCapacitySnapshot{}, fmt.Errorf("refresh proxy key readiness after update: %w", err)
	}
	return row, capacity, nil
}

func (s *Service) rotateProxyAPIKey(ctx context.Context, tx pgx.Tx, keyID int) (string, proxyAPIKeyRow, proxyKeyCapacitySnapshot, error) {
	// Rotation replaces the secret on the same row, so the counted key set never
	// changes and no capacity headroom is required. Serialization is still taken
	// so the returned capacity snapshot and the readiness capture observe the
	// same state the other proxy-key writes do.
	if err := lockProxyKeyCapacitySerialization(ctx, tx); err != nil {
		return "", proxyAPIKeyRow{}, proxyKeyCapacitySnapshot{}, err
	}
	current, err := s.loadProxyAPIKeyByID(ctx, tx, keyID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", proxyAPIKeyRow{}, proxyKeyCapacitySnapshot{}, &domainError{StatusCode: 404, Code: "proxy_key_not_found", Detail: "Proxy API key not found"}
		}
		return "", proxyAPIKeyRow{}, proxyKeyCapacitySnapshot{}, fmt.Errorf("load proxy api key: %w", err)
	}
	now := s.nowUTC()
	if current.ExpiresAt.Valid && !current.ExpiresAt.Time.UTC().After(now) {
		return "", proxyAPIKeyRow{}, proxyKeyCapacitySnapshot{}, &domainError{StatusCode: http.StatusConflict, Code: "proxy_key_not_rotatable", Detail: "Expired proxy API keys cannot be rotated"}
	}
	if !current.IsActive {
		return "", proxyAPIKeyRow{}, proxyKeyCapacitySnapshot{}, &domainError{StatusCode: http.StatusConflict, Code: "proxy_key_not_rotatable", Detail: "Inactive proxy API keys cannot be rotated"}
	}
	for range 5 {
		rawKey, keyPrefix, lastFour, keyHash, err := buildProxyAPIKey()
		if err != nil {
			return "", proxyAPIKeyRow{}, proxyKeyCapacitySnapshot{}, err
		}
		// Identity fields stay untouched: id, name, notes, creator, active state,
		// expiry, and created_at all survive the rotation. Only the secret and its
		// derived preview change, and the usage trace is cleared because it
		// described the retired secret, not the new one.
		scanner := tx.QueryRow(
			ctx,
			`UPDATE proxy_api_keys
			SET key_prefix = $2,
				key_hash = $3,
				last_four = $4,
				last_used_at = NULL,
				last_used_ip = NULL,
				rotated_at = $5,
				rotation_count = rotation_count + 1,
				updated_at = $5
			WHERE id = $1
			RETURNING id, name, key_prefix, key_hash, last_four, is_active, expires_at, last_used_at,
				last_used_ip, created_by_auth_subject_id, notes, rotated_at, rotation_count, created_at, updated_at`,
			keyID,
			keyPrefix,
			keyHash,
			lastFour,
			now,
		)
		row, scanErr := scanProxyAPIKey(scanner)
		if scanErr == nil {
			rotatedCapacity, capacityErr := countProxyKeyCapacity(ctx, tx, s.nowUTC())
			if capacityErr != nil {
				return "", proxyAPIKeyRow{}, proxyKeyCapacitySnapshot{}, fmt.Errorf("count proxy api keys after rotate: %w", capacityErr)
			}
			if _, readinessErr := s.captureProxyKeyReadiness(ctx, tx); readinessErr != nil {
				return "", proxyAPIKeyRow{}, proxyKeyCapacitySnapshot{}, fmt.Errorf("refresh proxy key readiness after rotate: %w", readinessErr)
			}
			return rawKey, row, rotatedCapacity, nil
		}
		if !isUniqueConstraintError(scanErr, "uq_proxy_api_keys_prefix") {
			return "", proxyAPIKeyRow{}, proxyKeyCapacitySnapshot{}, fmt.Errorf("rotate proxy api key: %w", scanErr)
		}
	}
	return "", proxyAPIKeyRow{}, proxyKeyCapacitySnapshot{}, &domainError{StatusCode: 500, Detail: "Failed to rotate proxy API key"}
}

func (s *Service) deleteProxyAPIKey(ctx context.Context, tx pgx.Tx, keyID int) (proxyKeyCapacitySnapshot, error) {
	if err := lockProxyKeyCapacitySerialization(ctx, tx); err != nil {
		return proxyKeyCapacitySnapshot{}, err
	}
	commandTag, err := tx.Exec(ctx, `DELETE FROM proxy_api_keys WHERE id = $1`, keyID)
	if err != nil {
		return proxyKeyCapacitySnapshot{}, fmt.Errorf("delete proxy api key: %w", err)
	}
	if commandTag.RowsAffected() == 0 {
		return proxyKeyCapacitySnapshot{}, &domainError{StatusCode: 404, Code: "proxy_key_not_found", Detail: "Proxy API key not found"}
	}
	capacity, err := countProxyKeyCapacity(ctx, tx, s.nowUTC())
	if err != nil {
		return proxyKeyCapacitySnapshot{}, fmt.Errorf("count proxy api keys after delete: %w", err)
	}
	if _, err := s.captureProxyKeyReadiness(ctx, tx); err != nil {
		return proxyKeyCapacitySnapshot{}, fmt.Errorf("refresh proxy key readiness after delete: %w", err)
	}
	return capacity, nil
}

func (s *Service) verifyProxyAPIKey(ctx context.Context, rawKey string) (*proxyAPIKeyRow, error) {
	if s.runtimeCache == nil {
		return nil, runtimeSnapshotUnavailableError()
	}
	decision, err := s.runtimeCache.LoadFreshRuntimeProxyKeyDecision(ctx, s.nowUTC(), rawKey)
	if err != nil {
		return nil, err
	}
	if !decision.Allowed {
		return nil, nil
	}
	return &proxyAPIKeyRow{ID: decision.KeyID, Name: decision.KeyName}, nil
}

func (s *Service) serializeProxyAPIKey(row proxyAPIKeyRow) proxyAPIKeyResponse {
	visiblePrefix := row.KeyPrefix
	previewPrefixLength := len(proxyAPIKeyPrefix) + s.proxyKeyPreviewSize
	if strings.HasPrefix(row.KeyPrefix, proxyAPIKeyPrefix) && len(row.KeyPrefix) > previewPrefixLength {
		visiblePrefix = row.KeyPrefix[:previewPrefixLength]
	}
	return proxyAPIKeyResponse{
		ID:            row.ID,
		Name:          row.Name,
		KeyPrefix:     row.KeyPrefix,
		KeyPreview:    visiblePrefix + "••••••••" + row.LastFour,
		IsActive:      row.IsActive,
		ExpiresAt:     nullableTime(row.ExpiresAt),
		LastUsedAt:    nullableTime(row.LastUsedAt),
		LastUsedIP:    nullableString(row.LastUsedIP),
		Notes:         nullableString(row.Notes),
		RotatedAt:     nullableTime(row.RotatedAt),
		RotationCount: row.RotationCount,
		CreatedAt:     row.CreatedAt,
		UpdatedAt:     row.UpdatedAt,
	}
}

// countProxyKeyCapacity computes the authoritative capacity snapshot with the
// same predicate and server clock the create limit uses: used counts rows
// where expires_at IS NULL OR expires_at > counted_at; is_active is ignored.
func countProxyKeyCapacity(ctx context.Context, exec queryExecutor, countedAt time.Time) (proxyKeyCapacitySnapshot, error) {
	var used int
	if err := exec.QueryRow(ctx, `SELECT COUNT(*) FROM proxy_api_keys WHERE expires_at IS NULL OR expires_at > $1`, countedAt.UTC()).Scan(&used); err != nil {
		return proxyKeyCapacitySnapshot{}, fmt.Errorf("count proxy api keys: %w", err)
	}
	remaining := proxyKeyLimit - used
	if remaining < 0 {
		remaining = 0
	}
	return proxyKeyCapacitySnapshot{
		Limit:     proxyKeyLimit,
		Used:      used,
		Remaining: remaining,
		CountedAt: countedAt.UTC(),
	}, nil
}

// lockProxyKeyCapacitySerialization serializes capacity mutations on the
// singleton auth settings row so concurrent create/create and create/rotate
// can never commit a final used greater than the limit. An unlocked COUNT
// followed by INSERT is not acceptable.
func lockProxyKeyCapacitySerialization(ctx context.Context, tx pgx.Tx) error {
	if err := auditdomain.AcquireAffectedWriterAdmission(ctx, tx); err != nil {
		return &domainError{StatusCode: http.StatusServiceUnavailable, Code: "auth_settings_unavailable", Detail: "Authentication readiness is temporarily unavailable", Fields: map[string]any{
			"details": map[string]any{"recovery": "retry", "retry_after_seconds": 5},
		}}
	}
	if err := lockProxyKeyReadiness(ctx, tx); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `SELECT id FROM app_auth_settings WHERE singleton_key = 'app' FOR UPDATE`); err != nil {
		return fmt.Errorf("lock proxy api key capacity serialization: %w", err)
	}
	return nil
}

func isUniqueConstraintError(err error, constraintName string) bool {
	if pgError := new(pgconn.PgError); errors.As(err, &pgError) {
		return pgError.ConstraintName == constraintName || pgError.Code == "23505"
	}
	return strings.Contains(err.Error(), constraintName)
}

func nullableTrimmedStringPtr(value *string) any {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return trimmed
}

func nullableInt32(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableTime(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time.UTC()
	return &result
}

func nullableTimePtr(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC()
}
