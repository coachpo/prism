package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"hash/fnv"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/coachpo/prism/backend/internal/httpapi/proxykeyusage"
)

const proxyKeyLimit = 100
const loginThrottleFailureLimit = 5
const loginThrottleWindow = 15 * time.Minute
const loginThrottleLockoutDuration = 15 * time.Minute
const loginThrottleLockoutDetail = "Too many login attempts. Please try again later."

type domainError struct {
	StatusCode int
	Code       string
	Detail     string
	Fields     map[string]any
}

func (err *domainError) Error() string {
	return err.Detail
}

type queryExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type appAuthSettingsRow struct {
	ID                 int
	AuthEnabled        bool
	Username           sql.NullString
	PasswordHash       sql.NullString
	MustChangePassword bool
	LastLoginAt        sql.NullTime
	TokenVersion       int
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type AppAuthSettingsSnapshot struct {
	ID           int
	AuthEnabled  bool
	Username     string
	TokenVersion int
}

func appAuthSettingsSnapshotFromRow(row appAuthSettingsRow) AppAuthSettingsSnapshot {
	return AppAuthSettingsSnapshot{
		ID:           row.ID,
		AuthEnabled:  row.AuthEnabled,
		Username:     stringValue(row.Username),
		TokenVersion: row.TokenVersion,
	}
}

type refreshTokenRow struct {
	ID              int
	AuthSubjectID   int
	TokenHash       string
	SessionDuration string
	ExpiresAt       time.Time
	RotatedFromID   sql.NullInt32
	RevokedAt       sql.NullTime
	LastUsedAt      sql.NullTime
	UserAgent       sql.NullString
	IPAddress       sql.NullString
	CreatedAt       time.Time
}

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
	RotatedFromID          sql.NullInt32
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

type loginThrottleKey struct {
	SubjectKey    string
	RemoteAddress string
}

type loginThrottleDecision struct {
	FailureCount int
	LockedUntil  sql.NullTime
}

type sessionBundle struct {
	SettingsRow      appAuthSettingsRow
	AccessToken      string
	RefreshToken     string
	RefreshExpiresAt time.Time
	SessionDuration  sessionDuration
}

type loginAuthenticationResult struct {
	Bundle    sessionBundle
	DomainErr *domainError
}

type authSettingsMutationResult struct {
	Row                appAuthSettingsRow
	SessionInvalidated bool
}

func (s *Service) loadOrCreateAppAuthSettings(ctx context.Context, exec queryExecutor) (appAuthSettingsRow, error) {
	row, err := loadAppAuthSettings(ctx, exec)
	if err == nil {
		return row, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return appAuthSettingsRow{}, err
	}

	now := s.nowUTC()
	scanner := exec.QueryRow(
		ctx,
		`INSERT INTO app_auth_settings (
			singleton_key,
			auth_enabled,
			username,
			password_hash,
			email_verification_attempt_count,
			must_change_password,
			last_login_at,
			token_version,
			created_at,
			updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id, auth_enabled, username, password_hash,
			must_change_password, last_login_at, token_version, created_at, updated_at`,
		"app",
		false,
		nil,
		nil,
		0,
		false,
		nil,
		0,
		now,
		now,
	)
	return scanAppAuthSettings(scanner)
}

func loadAppAuthSettings(ctx context.Context, exec queryExecutor) (appAuthSettingsRow, error) {
	scanner := exec.QueryRow(
		ctx,
		`SELECT id, auth_enabled, username, password_hash,
			must_change_password, last_login_at, token_version, created_at, updated_at
		FROM app_auth_settings
		WHERE singleton_key = $1
		ORDER BY id ASC
		LIMIT 1`,
		"app",
	)
	return scanAppAuthSettings(scanner)
}

func scanAppAuthSettings(scanner interface{ Scan(...any) error }) (appAuthSettingsRow, error) {
	row := appAuthSettingsRow{}
	err := scanner.Scan(
		&row.ID,
		&row.AuthEnabled,
		&row.Username,
		&row.PasswordHash,
		&row.MustChangePassword,
		&row.LastLoginAt,
		&row.TokenVersion,
		&row.CreatedAt,
		&row.UpdatedAt,
	)
	if err != nil {
		return appAuthSettingsRow{}, err
	}
	return row, nil
}

func loginThrottleKeyFor(username string, ipAddress string) loginThrottleKey {
	return loginThrottleKey{
		SubjectKey:    normalizeLoginThrottleSubject(username),
		RemoteAddress: normalizeLoginThrottleRemoteAddress(ipAddress),
	}
}

func normalizeLoginThrottleSubject(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizeLoginThrottleRemoteAddress(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "unknown"
	}
	return strings.ToLower(trimmed)
}

func (key loginThrottleKey) advisoryLockID() int64 {
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(key.SubjectKey))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(key.RemoteAddress))
	return int64(hash.Sum64())
}

func (s *Service) authenticateUser(ctx context.Context, tx pgx.Tx, authConfig RuntimeAuthConfigSnapshot, username string, password string, duration sessionDuration, userAgent string, ipAddress string) (loginAuthenticationResult, error) {
	settingsRow, err := s.loadOrCreateAppAuthSettings(ctx, tx)
	if err != nil {
		return loginAuthenticationResult{}, fmt.Errorf("load auth settings: %w", err)
	}
	if !settingsRow.AuthEnabled {
		return loginAuthenticationResult{DomainErr: &domainError{StatusCode: 400, Detail: "Authentication is not enabled"}}, nil
	}
	throttleKey := loginThrottleKeyFor(username, ipAddress)
	if err := s.lockLoginThrottleKey(ctx, tx, throttleKey); err != nil {
		return loginAuthenticationResult{}, err
	}
	if err := s.requireLoginNotLocked(ctx, tx, throttleKey); err != nil {
		if domainErr, ok := errors.AsType[*domainError](err); ok {
			return loginAuthenticationResult{DomainErr: domainErr}, nil
		}
		return loginAuthenticationResult{}, err
	}
	if !settingsRow.Username.Valid || !settingsRow.PasswordHash.Valid || settingsRow.Username.String != username || !verifyPassword(password, settingsRow.PasswordHash.String) {
		if recordErr := s.recordLoginFailure(ctx, tx, throttleKey); recordErr != nil {
			if domainErr, ok := errors.AsType[*domainError](recordErr); ok {
				return loginAuthenticationResult{DomainErr: domainErr}, nil
			}
			return loginAuthenticationResult{}, recordErr
		}
		return loginAuthenticationResult{DomainErr: &domainError{StatusCode: 401, Detail: "Invalid credentials"}}, nil
	}
	if err := s.clearLoginFailures(ctx, tx, throttleKey); err != nil {
		return loginAuthenticationResult{}, err
	}
	bundle, err := s.createSessionForSettingsRow(ctx, tx, authConfig, settingsRow, duration, userAgent, ipAddress, nil)
	if err != nil {
		return loginAuthenticationResult{}, err
	}
	return loginAuthenticationResult{Bundle: bundle}, nil
}

func (s *Service) lockLoginThrottleKey(ctx context.Context, tx pgx.Tx, key loginThrottleKey) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1::bigint)`, key.advisoryLockID()); err != nil {
		return fmt.Errorf("lock login throttle key: %w", err)
	}
	return nil
}

func (s *Service) requireLoginNotLocked(ctx context.Context, tx pgx.Tx, key loginThrottleKey) error {
	var lockedUntil sql.NullTime
	if err := tx.QueryRow(
		ctx,
		`SELECT locked_until
		FROM login_throttle_ledger
		WHERE subject_key = $1 AND remote_address = $2`,
		key.SubjectKey,
		key.RemoteAddress,
	).Scan(&lockedUntil); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("load login throttle ledger: %w", err)
	}
	if lockedUntil.Valid && lockedUntil.Time.After(s.nowUTC()) {
		return &domainError{StatusCode: 429, Detail: loginThrottleLockoutDetail}
	}
	return nil
}

func (s *Service) recordLoginFailure(ctx context.Context, tx pgx.Tx, key loginThrottleKey) error {
	now := s.nowUTC()
	windowStart := now.Add(-loginThrottleWindow)
	lockoutUntil := now.Add(loginThrottleLockoutDuration)
	decision := loginThrottleDecision{}
	if err := tx.QueryRow(
		ctx,
		`INSERT INTO login_throttle_ledger (
			subject_key,
			remote_address,
			failure_count,
			first_failed_at,
			last_failed_at,
			locked_until,
			created_at,
			updated_at
		) VALUES ($1, $2, 1, $3, $3, NULL::timestamp with time zone, $3, $3)
		ON CONFLICT (subject_key, remote_address) DO UPDATE SET
			failure_count = CASE
				WHEN login_throttle_ledger.first_failed_at < $4 THEN 1
				ELSE login_throttle_ledger.failure_count + 1
			END,
			first_failed_at = CASE
				WHEN login_throttle_ledger.first_failed_at < $4 THEN $3
				ELSE login_throttle_ledger.first_failed_at
			END,
			last_failed_at = $3,
			locked_until = CASE
				WHEN (CASE WHEN login_throttle_ledger.first_failed_at < $4 THEN 1 ELSE login_throttle_ledger.failure_count + 1 END) >= $5 THEN $6::timestamp with time zone
				ELSE NULL::timestamp with time zone
			END,
			updated_at = $3
		RETURNING failure_count, locked_until`,
		key.SubjectKey,
		key.RemoteAddress,
		now,
		windowStart,
		loginThrottleFailureLimit,
		lockoutUntil,
	).Scan(&decision.FailureCount, &decision.LockedUntil); err != nil {
		return fmt.Errorf("record login failure: %w", err)
	}
	if decision.LockedUntil.Valid && decision.LockedUntil.Time.After(now) {
		return &domainError{StatusCode: 429, Detail: loginThrottleLockoutDetail}
	}
	return nil
}

func (s *Service) clearLoginFailures(ctx context.Context, tx pgx.Tx, key loginThrottleKey) error {
	if _, err := tx.Exec(
		ctx,
		`DELETE FROM login_throttle_ledger WHERE subject_key = $1 AND remote_address = $2`,
		key.SubjectKey,
		key.RemoteAddress,
	); err != nil {
		return fmt.Errorf("clear login failures: %w", err)
	}
	return nil
}

func (s *Service) createSessionForSettingsRow(ctx context.Context, tx pgx.Tx, authConfig RuntimeAuthConfigSnapshot, settingsRow appAuthSettingsRow, duration sessionDuration, userAgent string, ipAddress string, refreshExpiry *time.Time) (sessionBundle, error) {
	now := s.nowUTC()
	var expiresAt time.Time
	if refreshExpiry != nil {
		expiresAt = refreshExpiry.UTC()
	} else {
		expiresAt = duration.refreshExpiry(now, authConfig.RefreshTokenTTL)
	}
	accessToken, err := createAccessToken(now, authConfig.AccessTokenTTL, s.authJWTSecret, settingsRow.ID, stringValue(settingsRow.Username), settingsRow.TokenVersion)
	if err != nil {
		return sessionBundle{}, err
	}
	rawRefreshToken, refreshHash, refreshExpiresAt, err := buildRefreshTokenRecord(expiresAt)
	if err != nil {
		return sessionBundle{}, err
	}
	if _, err := tx.Exec(
		ctx,
		`INSERT INTO refresh_tokens (
			auth_subject_id,
			token_hash,
			session_duration,
			expires_at,
			rotated_from_id,
			revoked_at,
			last_used_at,
			user_agent,
			ip_address,
			created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		settingsRow.ID,
		refreshHash,
		string(duration),
		refreshExpiresAt,
		nil,
		nil,
		nil,
		nullableTrimmedString(userAgent),
		nullableTrimmedString(ipAddress),
		now,
	); err != nil {
		return sessionBundle{}, fmt.Errorf("insert refresh token: %w", err)
	}
	updatedSettings, err := s.updateLastLogin(ctx, tx, settingsRow.ID, now)
	if err != nil {
		return sessionBundle{}, err
	}
	return sessionBundle{
		SettingsRow:      updatedSettings,
		AccessToken:      accessToken,
		RefreshToken:     rawRefreshToken,
		RefreshExpiresAt: refreshExpiresAt,
		SessionDuration:  duration,
	}, nil
}

func (s *Service) updateLastLogin(ctx context.Context, tx pgx.Tx, settingsID int, timestamp time.Time) (appAuthSettingsRow, error) {
	scanner := tx.QueryRow(
		ctx,
		`UPDATE app_auth_settings
		SET last_login_at = $2, updated_at = $2
		WHERE id = $1
		RETURNING id, auth_enabled, username, password_hash,
			must_change_password, last_login_at, token_version, created_at, updated_at`,
		settingsID,
		timestamp,
	)
	row, err := scanAppAuthSettings(scanner)
	if err != nil {
		return appAuthSettingsRow{}, fmt.Errorf("update auth settings last_login_at: %w", err)
	}
	return row, nil
}

func (s *Service) loadRefreshTokenByHash(ctx context.Context, exec queryExecutor, refreshHash string) (refreshTokenRow, error) {
	scanner := exec.QueryRow(
		ctx,
		`SELECT id, auth_subject_id, token_hash, session_duration, expires_at, rotated_from_id,
			revoked_at, last_used_at, user_agent, ip_address, created_at
		FROM refresh_tokens
		WHERE token_hash = $1
		ORDER BY id ASC
		LIMIT 1`,
		refreshHash,
	)
	row := refreshTokenRow{}
	err := scanner.Scan(
		&row.ID,
		&row.AuthSubjectID,
		&row.TokenHash,
		&row.SessionDuration,
		&row.ExpiresAt,
		&row.RotatedFromID,
		&row.RevokedAt,
		&row.LastUsedAt,
		&row.UserAgent,
		&row.IPAddress,
		&row.CreatedAt,
	)
	if err != nil {
		return refreshTokenRow{}, err
	}
	return row, nil
}

func (s *Service) rotateRefreshToken(ctx context.Context, tx pgx.Tx, authConfig RuntimeAuthConfigSnapshot, rawRefreshToken string, userAgent string, ipAddress string) (sessionBundle, error) {
	refreshRow, err := s.loadRefreshTokenByHash(ctx, tx, hashOpaqueToken(rawRefreshToken))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return sessionBundle{}, &domainError{StatusCode: 401, Detail: "Invalid refresh token"}
		}
		return sessionBundle{}, fmt.Errorf("load refresh token: %w", err)
	}
	now := s.nowUTC()
	if refreshRow.ExpiresAt.Before(now) {
		return sessionBundle{}, &domainError{StatusCode: 401, Detail: "Invalid refresh token"}
	}
	if refreshRow.RevokedAt.Valid {
		if err := s.revokeRefreshTokenFamily(ctx, tx, refreshRow.ID); err != nil {
			return sessionBundle{}, err
		}
		return sessionBundle{}, &domainError{StatusCode: 401, Detail: "Invalid refresh token"}
	}
	settingsRow, err := s.loadOrCreateAppAuthSettings(ctx, tx)
	if err != nil {
		return sessionBundle{}, fmt.Errorf("load auth settings: %w", err)
	}
	if refreshRow.AuthSubjectID != settingsRow.ID || !settingsRow.AuthEnabled {
		return sessionBundle{}, &domainError{StatusCode: 401, Detail: "Invalid refresh token"}
	}
	duration, err := normalizeSessionDuration(refreshRow.SessionDuration)
	if err != nil {
		if _, updateErr := tx.Exec(ctx, `UPDATE refresh_tokens SET revoked_at = $2 WHERE id = $1`, refreshRow.ID, now); updateErr != nil {
			return sessionBundle{}, fmt.Errorf("revoke invalid refresh token duration: %w", updateErr)
		}
		return sessionBundle{}, &domainError{StatusCode: 401, Detail: "Invalid refresh token"}
	}
	if _, err := tx.Exec(
		ctx,
		`UPDATE refresh_tokens SET revoked_at = $2, last_used_at = $2 WHERE id = $1`,
		refreshRow.ID,
		now,
	); err != nil {
		return sessionBundle{}, fmt.Errorf("revoke rotated refresh token: %w", err)
	}
	return s.createRotatedSession(ctx, tx, authConfig, settingsRow, duration, userAgent, ipAddress, refreshRow.ID, refreshRow.ExpiresAt)
}

func (s *Service) createRotatedSession(ctx context.Context, tx pgx.Tx, authConfig RuntimeAuthConfigSnapshot, settingsRow appAuthSettingsRow, duration sessionDuration, userAgent string, ipAddress string, rotatedFromID int, refreshExpiry time.Time) (sessionBundle, error) {
	now := s.nowUTC()
	accessToken, err := createAccessToken(now, authConfig.AccessTokenTTL, s.authJWTSecret, settingsRow.ID, stringValue(settingsRow.Username), settingsRow.TokenVersion)
	if err != nil {
		return sessionBundle{}, err
	}
	rawRefreshToken, refreshHash, expiresAt, err := buildRefreshTokenRecord(refreshExpiry)
	if err != nil {
		return sessionBundle{}, err
	}
	if _, err := tx.Exec(
		ctx,
		`INSERT INTO refresh_tokens (
			auth_subject_id,
			token_hash,
			session_duration,
			expires_at,
			rotated_from_id,
			revoked_at,
			last_used_at,
			user_agent,
			ip_address,
			created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		settingsRow.ID,
		refreshHash,
		string(duration),
		expiresAt,
		rotatedFromID,
		nil,
		nil,
		nullableTrimmedString(userAgent),
		nullableTrimmedString(ipAddress),
		now,
	); err != nil {
		return sessionBundle{}, fmt.Errorf("insert rotated refresh token: %w", err)
	}
	return sessionBundle{
		SettingsRow:      settingsRow,
		AccessToken:      accessToken,
		RefreshToken:     rawRefreshToken,
		RefreshExpiresAt: expiresAt,
		SessionDuration:  duration,
	}, nil
}

func (s *Service) revokeAllRefreshTokens(ctx context.Context, tx pgx.Tx, authSubjectID int) error {
	_, err := tx.Exec(
		ctx,
		`UPDATE refresh_tokens
		SET revoked_at = $2
		WHERE auth_subject_id = $1 AND revoked_at IS NULL`,
		authSubjectID,
		s.nowUTC(),
	)
	if err != nil {
		return fmt.Errorf("revoke refresh tokens: %w", err)
	}
	return nil
}

func (s *Service) revokeRefreshToken(ctx context.Context, tx pgx.Tx, rawRefreshToken string) (*int, error) {
	var authSubjectID int
	err := tx.QueryRow(
		ctx,
		`UPDATE refresh_tokens
		SET revoked_at = $2
		WHERE token_hash = $1 AND revoked_at IS NULL
		RETURNING auth_subject_id`,
		hashOpaqueToken(rawRefreshToken),
		s.nowUTC(),
	).Scan(&authSubjectID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("revoke refresh token: %w", err)
	}
	return &authSubjectID, nil
}

func (s *Service) revokeRefreshTokenFamily(ctx context.Context, tx pgx.Tx, refreshTokenID int) error {
	familyRootID := refreshTokenID
	for {
		var parentID sql.NullInt32
		err := tx.QueryRow(ctx, `SELECT rotated_from_id FROM refresh_tokens WHERE id = $1`, familyRootID).Scan(&parentID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				break
			}
			return fmt.Errorf("load refresh token parent: %w", err)
		}
		if !parentID.Valid {
			break
		}
		familyRootID = int(parentID.Int32)
	}

	familyIDs := map[int]struct{}{familyRootID: {}}
	frontier := []int{familyRootID}
	for len(frontier) > 0 {
		rows, err := tx.Query(ctx, `SELECT id FROM refresh_tokens WHERE rotated_from_id = ANY($1) ORDER BY id ASC`, toInt32Slice(frontier))
		if err != nil {
			return fmt.Errorf("query refresh token family children: %w", err)
		}
		nextFrontier := make([]int, 0)
		for rows.Next() {
			var childID int
			if err := rows.Scan(&childID); err != nil {
				rows.Close()
				return fmt.Errorf("scan refresh token child id: %w", err)
			}
			if _, seen := familyIDs[childID]; seen {
				continue
			}
			familyIDs[childID] = struct{}{}
			nextFrontier = append(nextFrontier, childID)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return fmt.Errorf("iterate refresh token family children: %w", err)
		}
		rows.Close()
		frontier = nextFrontier
	}

	ids := make([]int, 0, len(familyIDs))
	for id := range familyIDs {
		ids = append(ids, id)
	}
	_, err := tx.Exec(
		ctx,
		`UPDATE refresh_tokens SET revoked_at = $2 WHERE id = ANY($1) AND revoked_at IS NULL`,
		toInt32Slice(ids),
		s.nowUTC(),
	)
	if err != nil {
		return fmt.Errorf("revoke refresh token family: %w", err)
	}
	return nil
}

func (s *Service) updateAuthSettings(ctx context.Context, tx pgx.Tx, settingsRow appAuthSettingsRow, request authSettingsUpdateRequest) (authSettingsMutationResult, error) {
	username := normalizeUsername(request.Username)
	password := normalizePassword(request.Password)
	revokeSessions := false
	now := s.nowUTC()
	updatedRow := settingsRow

	if request.AuthEnabled {
		if username == nil || *username == "" {
			return authSettingsMutationResult{}, &domainError{StatusCode: 400, Detail: "username is required"}
		}
		if !settingsRow.PasswordHash.Valid && (password == nil || *password == "") {
			return authSettingsMutationResult{}, &domainError{StatusCode: 400, Detail: "password is required"}
		}
		updatedRow.AuthEnabled = true
		updatedRow.Username = toNullString(username)
		if settingsRow.AuthEnabled && stringValue(settingsRow.Username) != *username {
			updatedRow.TokenVersion++
			revokeSessions = true
		}
		if password != nil && *password != "" {
			hash, err := hashPassword(*password)
			if err != nil {
				return authSettingsMutationResult{}, err
			}
			updatedRow.PasswordHash = sql.NullString{String: hash, Valid: true}
			updatedRow.TokenVersion++
			revokeSessions = true
		}
	} else {
		if settingsRow.AuthEnabled {
			updatedRow.TokenVersion++
			revokeSessions = true
		}
		updatedRow.AuthEnabled = false
		if username != nil {
			updatedRow.Username = toNullString(username)
		}
		if password != nil && *password != "" {
			hash, err := hashPassword(*password)
			if err != nil {
				return authSettingsMutationResult{}, err
			}
			updatedRow.PasswordHash = sql.NullString{String: hash, Valid: true}
			updatedRow.TokenVersion++
			revokeSessions = true
		}
	}

	if revokeSessions {
		if err := s.revokeAllRefreshTokens(ctx, tx, settingsRow.ID); err != nil {
			return authSettingsMutationResult{}, err
		}
	}

	scanner := tx.QueryRow(
		ctx,
		`UPDATE app_auth_settings
		SET auth_enabled = $2, username = $3, password_hash = $4, token_version = $5, updated_at = $6
		WHERE id = $1
		RETURNING id, auth_enabled, username, password_hash,
			must_change_password, last_login_at, token_version, created_at, updated_at`,
		settingsRow.ID,
		updatedRow.AuthEnabled,
		nullStringValue(updatedRow.Username),
		nullStringValue(updatedRow.PasswordHash),
		updatedRow.TokenVersion,
		now,
	)
	row, err := scanAppAuthSettings(scanner)
	if err != nil {
		return authSettingsMutationResult{}, fmt.Errorf("update auth settings: %w", err)
	}
	return authSettingsMutationResult{Row: row, SessionInvalidated: settingsRow.AuthEnabled && revokeSessions}, nil
}

func (s *Service) listProxyAPIKeys(ctx context.Context, exec queryExecutor) ([]proxyAPIKeyRow, error) {
	rows, err := exec.Query(
		ctx,
		`SELECT id, name, key_prefix, key_hash, last_four, is_active, expires_at, last_used_at,
			last_used_ip, created_by_auth_subject_id, notes, rotated_from_id, created_at, updated_at
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
		&row.RotatedFromID,
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
				rotated_from_id,
				created_at,
				updated_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
			RETURNING id, name, key_prefix, key_hash, last_four, is_active, expires_at, last_used_at,
				last_used_ip, created_by_auth_subject_id, notes, rotated_from_id, created_at, updated_at`,
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
			nil,
			now,
			now,
		)
		row, scanErr := scanProxyAPIKey(scanner)
		if scanErr == nil {
			insertedCapacity, capacityErr := countProxyKeyCapacity(ctx, tx, s.nowUTC())
			if capacityErr != nil {
				return "", proxyAPIKeyRow{}, proxyKeyCapacitySnapshot{}, fmt.Errorf("count proxy api keys after insert: %w", capacityErr)
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
			last_used_ip, created_by_auth_subject_id, notes, rotated_from_id, created_at, updated_at
		FROM proxy_api_keys WHERE id = $1 LIMIT 1`,
		keyID,
	)
	return scanProxyAPIKey(scanner)
}

func (s *Service) updateProxyAPIKey(ctx context.Context, tx pgx.Tx, keyID int, name string, notes *string, isActive *bool, expiry proxyKeyExpiryUpdate) (proxyAPIKeyRow, proxyKeyCapacitySnapshot, error) {
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
			last_used_ip, created_by_auth_subject_id, notes, rotated_from_id, created_at, updated_at`,
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
	return row, capacity, nil
}

func (s *Service) rotateProxyAPIKey(ctx context.Context, tx pgx.Tx, keyID int) (string, proxyAPIKeyRow, proxyKeyCapacitySnapshot, error) {
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
	var inheritedExpiry any
	if current.ExpiresAt.Valid {
		inheritedExpiry = current.ExpiresAt.Time.UTC()
	}
	// Serialize capacity mutations and evaluate the final used state: the
	// predecessor becomes non-counting in the same transaction as successor
	// creation, so a full-capacity rotation is net-neutral and must never see
	// a transient 101-row state.
	if err := lockProxyKeyCapacitySerialization(ctx, tx); err != nil {
		return "", proxyAPIKeyRow{}, proxyKeyCapacitySnapshot{}, err
	}
	if _, err := tx.Exec(
		ctx,
		`UPDATE proxy_api_keys
		SET is_active = FALSE, expires_at = $2, updated_at = $2
		WHERE id = $1`,
		keyID,
		now,
	); err != nil {
		return "", proxyAPIKeyRow{}, proxyKeyCapacitySnapshot{}, fmt.Errorf("expire rotated proxy api key: %w", err)
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
				rotated_from_id,
				created_at,
				updated_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
			RETURNING id, name, key_prefix, key_hash, last_four, is_active, expires_at, last_used_at,
				last_used_ip, created_by_auth_subject_id, notes, rotated_from_id, created_at, updated_at`,
			current.Name,
			keyPrefix,
			keyHash,
			lastFour,
			current.IsActive,
			inheritedExpiry,
			nil,
			nil,
			nullableInt32(nullableInt(current.CreatedByAuthSubjectID)),
			nullableTrimmedStringPtr(nullableString(current.Notes)),
			keyID,
			now,
			now,
		)
		row, scanErr := scanProxyAPIKey(scanner)
		if scanErr == nil {
			rotatedCapacity, capacityErr := countProxyKeyCapacity(ctx, tx, s.nowUTC())
			if capacityErr != nil {
				return "", proxyAPIKeyRow{}, proxyKeyCapacitySnapshot{}, fmt.Errorf("count proxy api keys after rotate: %w", capacityErr)
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

func RecordProxyAPIKeyUsageTx(ctx context.Context, tx pgx.Tx, keyID int, lastUsedAt time.Time, lastUsedIP string) error {
	return proxykeyusage.RecordTx(ctx, tx, keyID, lastUsedAt, lastUsedIP)
}

func (s *Service) recordProxyAPIKeyUsage(ctx context.Context, keyID int, lastUsedAt time.Time, lastUsedIP string) error {
	execPool := s.proxyKeyUsagePool
	if execPool == nil {
		return fmt.Errorf("record proxy api key usage: background jobs pool unavailable")
	}
	if err := proxykeyusage.RecordWithExecutor(ctx, execPool, keyID, lastUsedAt, lastUsedIP); err != nil {
		return fmt.Errorf("record proxy api key usage: %w", err)
	}
	return nil
}

func (s *Service) buildAuthSettingsResponse(row appAuthSettingsRow) authSettingsResponse {
	return authSettingsResponse{
		AuthEnabled:   row.AuthEnabled,
		Username:      nullableString(row.Username),
		HasPassword:   row.PasswordHash.Valid,
		ProxyKeyLimit: proxyKeyLimit,
	}
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
		RotatedFromID: nullableInt(row.RotatedFromID),
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
	if _, err := tx.Exec(ctx, `SELECT id FROM app_auth_settings WHERE singleton_key = 'app' FOR UPDATE`); err != nil {
		return fmt.Errorf("lock proxy api key capacity serialization: %w", err)
	}
	return nil
}

// resolveProxyKeyCreateExpiry validates a create expiry: omitted/null means
// never expires; a value must be a strict future instant.
func resolveProxyKeyCreateExpiry(expiresAt *time.Time, now time.Time) (*time.Time, error) {
	if expiresAt == nil {
		return nil, nil
	}
	return resolveProxyKeyFutureExpiry(expiresAt, now)
}

// resolveProxyKeyFutureExpiry rejects non-future instants with a locatable
// field error.
func resolveProxyKeyFutureExpiry(expiresAt *time.Time, now time.Time) (*time.Time, error) {
	if expiresAt == nil {
		return nil, nil
	}
	resolved := expiresAt.UTC()
	if !resolved.After(now) {
		return nil, &domainError{
			StatusCode: http.StatusUnprocessableEntity,
			Code:       "proxy_key_expiry_invalid",
			Detail:     "Expiry must be a future time",
			Fields:     map[string]any{"field": "expires_at"},
		}
	}
	return &resolved, nil
}

func isUniqueConstraintError(err error, constraintName string) bool {
	if pgError := new(pgconn.PgError); errors.As(err, &pgError) {
		return pgError.ConstraintName == constraintName || pgError.Code == "23505"
	}
	return strings.Contains(err.Error(), constraintName)
}

func normalizeUsername(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func normalizePassword(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := *value
	return &trimmed
}

func normalizeNotes(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func validateProxyKeyName(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", &domainError{StatusCode: 400, Detail: "name must not be empty"}
	}
	if len(trimmed) > 200 {
		return "", &domainError{StatusCode: 400, Detail: "name must be at most 200 characters"}
	}
	return trimmed, nil
}

func nullableTrimmedString(value string) any {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return trimmed
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

func nullableString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	result := value.String
	return &result
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

func nullableInt(value sql.NullInt32) *int {
	if !value.Valid {
		return nil
	}
	result := int(value.Int32)
	return &result
}

func toNullString(value *string) sql.NullString {
	if value == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *value, Valid: true}
}

func nullStringValue(value sql.NullString) any {
	if !value.Valid {
		return nil
	}
	return value.String
}

func stringValue(value sql.NullString) string {
	if !value.Valid {
		return ""
	}
	return value.String
}

func toInt32Slice(values []int) []int32 {
	result := make([]int32, 0, len(values))
	for _, value := range values {
		result = append(result, int32(value))
	}
	return result
}
