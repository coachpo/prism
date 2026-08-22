package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
)

// dummyPasswordHash is a precomputed valid bcrypt hash used for a single
// same-cost compare when the username is missing or the stored hash is
// absent, so known and unknown subjects perform identical password work.
var dummyPasswordHash = func() string {
	hash, err := bcrypt.GenerateFromPassword([]byte("prism-dummy-password-work"), bcrypt.DefaultCost)
	if err != nil {
		panic(fmt.Sprintf("precompute dummy bcrypt hash: %v", err))
	}
	return string(hash)
}()

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

func (s *Service) authenticateUser(ctx context.Context, tx pgx.Tx, authConfig RuntimeAuthConfigSnapshot, username string, password string, duration sessionDuration, userAgent string, ipAddress string) (loginAuthenticationResult, error) {
	settingsRow, err := s.loadOrCreateAppAuthSettings(ctx, tx)
	if err != nil {
		return loginAuthenticationResult{}, fmt.Errorf("load auth settings: %w", err)
	}
	if !settingsRow.AuthEnabled {
		return loginAuthenticationResult{DomainErr: &domainError{StatusCode: http.StatusBadRequest, Code: ProblemCodeAuthNotEnabled, Detail: "本实例未启用身份验证"}}, nil
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
	// Known and unknown subjects perform exactly one same-cost bcrypt
	// compare: a missing/mismatched username compares against the
	// precomputed dummy hash instead of skipping password work.
	passwordHash := settingsRow.PasswordHash.String
	if !settingsRow.Username.Valid || settingsRow.Username.String != username || !settingsRow.PasswordHash.Valid {
		passwordHash = dummyPasswordHash
	}
	passwordMatches := s.verifyPasswordOnce(password, passwordHash)
	if !settingsRow.Username.Valid || settingsRow.Username.String != username || !passwordMatches {
		if recordErr := s.recordLoginFailure(ctx, tx, throttleKey); recordErr != nil {
			if domainErr, ok := errors.AsType[*domainError](recordErr); ok {
				return loginAuthenticationResult{DomainErr: domainErr}, nil
			}
			return loginAuthenticationResult{}, recordErr
		}
		return loginAuthenticationResult{DomainErr: &domainError{StatusCode: http.StatusUnauthorized, Code: ProblemCodeAuthInvalidCredentials, Detail: "用户名或密码不正确"}}, nil
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
	// The transitional in-place credential columns exist only while the schema
	// is additive; the finalizer drops them once every verifier consumes the
	// pointer.
	var legacyColumnsExist bool
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) > 0 FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'app_auth_settings' AND column_name = 'auth_enabled'`).Scan(&legacyColumnsExist); err != nil {
		return appAuthSettingsRow{}, fmt.Errorf("check auth legacy columns: %w", err)
	}
	if legacyColumnsExist {
		scanner := tx.QueryRow(
			ctx,
			`UPDATE app_auth_settings
			SET last_login_at = $2, updated_at = $2
			WHERE id = $1
			RETURNING id, auth_enabled, username, password_hash,
				must_change_password, last_login_at, token_version, created_at, updated_at,
				effective_auth_generation, auth_transition_state, auth_transition_operation_id,
				auth_transition_retry_after_at, auth_transition_attempts`,
			settingsID,
			timestamp,
		)
		row, err := scanAppAuthSettings(scanner)
		if err != nil {
			return appAuthSettingsRow{}, fmt.Errorf("update auth settings last_login_at: %w", err)
		}
		return row, nil
	}
	var row appAuthSettingsRow
	err := tx.QueryRow(
		ctx,
		`UPDATE app_auth_settings
		SET last_login_at = $2, updated_at = $2
		WHERE id = $1
		RETURNING id, auth_revision, must_change_password, last_login_at, created_at, updated_at`,
		settingsID,
		timestamp,
	).Scan(&row.ID, &row.Revision, &row.MustChangePassword, &row.LastLoginAt, &row.CreatedAt, &row.UpdatedAt)
	if err != nil {
		return appAuthSettingsRow{}, fmt.Errorf("update auth settings last_login_at: %w", err)
	}
	// The effective enforcement mode and identity live in the immutable
	// config version pointer (the finalizer dropped the in-place columns).
	var configMode string
	var configUsername sql.NullString
	if err := tx.QueryRow(ctx, `SELECT COALESCE(v.desired_mode, 'disabled'), v.username FROM app_auth_settings AS a
		LEFT JOIN auth_config_versions AS v ON v.id = a.effective_config_version_id
		WHERE a.id = $1`, settingsID).Scan(&configMode, &configUsername); err != nil {
		return appAuthSettingsRow{}, fmt.Errorf("load effective auth mode: %w", err)
	}
	row.AuthEnabled = configMode == "enabled"
	row.Username = configUsername
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

func nullableTrimmedString(value string) any {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return trimmed
}

func toInt32Slice(values []int) []int32 {
	result := make([]int32, 0, len(values))
	for _, value := range values {
		result = append(result, int32(value))
	}
	return result
}
