package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const proxyKeyLimit = 100

type domainError struct {
	StatusCode int
	Detail     string
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
	ID                            int
	AuthEnabled                   bool
	Username                      sql.NullString
	Email                         sql.NullString
	PendingEmail                  sql.NullString
	PasswordHash                  sql.NullString
	EmailBoundAt                  sql.NullTime
	EmailVerificationCodeHash     sql.NullString
	EmailVerificationExpiresAt    sql.NullTime
	EmailVerificationAttemptCount int
	MustChangePassword            bool
	LastLoginAt                   sql.NullTime
	TokenVersion                  int
	CreatedAt                     time.Time
	UpdatedAt                     time.Time
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

type passwordResetChallengeRow struct {
	ID            int
	AuthSubjectID int
	OTPHash       string
	ExpiresAt     time.Time
	ConsumedAt    sql.NullTime
	AttemptCount  int
	RequestedIP   sql.NullString
	CreatedAt     time.Time
}

type sessionBundle struct {
	SettingsRow      appAuthSettingsRow
	AccessToken      string
	RefreshToken     string
	RefreshExpiresAt time.Time
	SessionDuration  sessionDuration
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
			email,
			pending_email,
			password_hash,
			email_bound_at,
			email_verification_code_hash,
			email_verification_expires_at,
			email_verification_attempt_count,
			must_change_password,
			last_login_at,
			token_version,
			created_at,
			updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		RETURNING id, auth_enabled, username, email, pending_email, password_hash, email_bound_at,
			email_verification_code_hash, email_verification_expires_at, email_verification_attempt_count,
			must_change_password, last_login_at, token_version, created_at, updated_at`,
		"app",
		false,
		nil,
		nil,
		nil,
		nil,
		nil,
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
		`SELECT id, auth_enabled, username, email, pending_email, password_hash, email_bound_at,
			email_verification_code_hash, email_verification_expires_at, email_verification_attempt_count,
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
		&row.Email,
		&row.PendingEmail,
		&row.PasswordHash,
		&row.EmailBoundAt,
		&row.EmailVerificationCodeHash,
		&row.EmailVerificationExpiresAt,
		&row.EmailVerificationAttemptCount,
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

func (s *Service) authenticateUser(ctx context.Context, tx pgx.Tx, username string, password string, duration sessionDuration, userAgent string, ipAddress string) (sessionBundle, error) {
	settingsRow, err := s.loadOrCreateAppAuthSettings(ctx, tx)
	if err != nil {
		return sessionBundle{}, fmt.Errorf("load auth settings: %w", err)
	}
	if !settingsRow.AuthEnabled {
		return sessionBundle{}, &domainError{StatusCode: 400, Detail: "Authentication is not enabled"}
	}
	if !settingsRow.Username.Valid || !settingsRow.PasswordHash.Valid || settingsRow.Username.String != username || !verifyPassword(password, settingsRow.PasswordHash.String) {
		return sessionBundle{}, &domainError{StatusCode: 401, Detail: "Invalid credentials"}
	}
	return s.createSessionForSettingsRow(ctx, tx, settingsRow, duration, userAgent, ipAddress, nil)
}

func (s *Service) createSessionForSettingsRow(ctx context.Context, tx pgx.Tx, settingsRow appAuthSettingsRow, duration sessionDuration, userAgent string, ipAddress string, refreshExpiry *time.Time) (sessionBundle, error) {
	now := s.nowUTC()
	var expiresAt time.Time
	if refreshExpiry != nil {
		expiresAt = refreshExpiry.UTC()
	} else {
		expiresAt = duration.refreshExpiry(now, s.refreshTokenTTL)
	}
	accessToken, err := createAccessToken(now, s.accessTokenTTL, s.authJWTSecret, settingsRow.ID, stringValue(settingsRow.Username), settingsRow.TokenVersion)
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
		RETURNING id, auth_enabled, username, email, pending_email, password_hash, email_bound_at,
			email_verification_code_hash, email_verification_expires_at, email_verification_attempt_count,
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

func (s *Service) rotateRefreshToken(ctx context.Context, tx pgx.Tx, rawRefreshToken string, userAgent string, ipAddress string) (sessionBundle, error) {
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
	return s.createRotatedSession(ctx, tx, settingsRow, duration, userAgent, ipAddress, refreshRow.ID, refreshRow.ExpiresAt)
}

func (s *Service) createRotatedSession(ctx context.Context, tx pgx.Tx, settingsRow appAuthSettingsRow, duration sessionDuration, userAgent string, ipAddress string, rotatedFromID int, refreshExpiry time.Time) (sessionBundle, error) {
	now := s.nowUTC()
	accessToken, err := createAccessToken(now, s.accessTokenTTL, s.authJWTSecret, settingsRow.ID, stringValue(settingsRow.Username), settingsRow.TokenVersion)
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

func (s *Service) revokeRefreshToken(ctx context.Context, tx pgx.Tx, rawRefreshToken string) error {
	_, err := tx.Exec(
		ctx,
		`UPDATE refresh_tokens
		SET revoked_at = $2
		WHERE token_hash = $1 AND revoked_at IS NULL`,
		hashOpaqueToken(rawRefreshToken),
		s.nowUTC(),
	)
	if err != nil {
		return fmt.Errorf("revoke refresh token: %w", err)
	}
	return nil
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

func (s *Service) updateAuthSettings(ctx context.Context, tx pgx.Tx, settingsRow appAuthSettingsRow, request authSettingsUpdateRequest) (appAuthSettingsRow, error) {
	username := normalizeUsername(request.Username)
	password := normalizePassword(request.Password)
	revokeSessions := false
	now := s.nowUTC()
	updatedRow := settingsRow

	if request.AuthEnabled {
		if username == nil || *username == "" {
			return appAuthSettingsRow{}, &domainError{StatusCode: 400, Detail: "username is required"}
		}
		if !settingsRow.Email.Valid || !settingsRow.EmailBoundAt.Valid {
			return appAuthSettingsRow{}, &domainError{StatusCode: 400, Detail: "A verified email is required"}
		}
		if !settingsRow.PasswordHash.Valid && (password == nil || *password == "") {
			return appAuthSettingsRow{}, &domainError{StatusCode: 400, Detail: "password is required"}
		}
		updatedRow.AuthEnabled = true
		updatedRow.Username = toNullString(username)
		if password != nil && *password != "" {
			hash, err := hashPassword(*password)
			if err != nil {
				return appAuthSettingsRow{}, err
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
				return appAuthSettingsRow{}, err
			}
			updatedRow.PasswordHash = sql.NullString{String: hash, Valid: true}
			updatedRow.TokenVersion++
			revokeSessions = true
		}
	}

	if revokeSessions {
		if err := s.revokeAllRefreshTokens(ctx, tx, settingsRow.ID); err != nil {
			return appAuthSettingsRow{}, err
		}
	}

	scanner := tx.QueryRow(
		ctx,
		`UPDATE app_auth_settings
		SET auth_enabled = $2, username = $3, password_hash = $4, token_version = $5, updated_at = $6
		WHERE id = $1
		RETURNING id, auth_enabled, username, email, pending_email, password_hash, email_bound_at,
			email_verification_code_hash, email_verification_expires_at, email_verification_attempt_count,
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
		return appAuthSettingsRow{}, fmt.Errorf("update auth settings: %w", err)
	}
	return row, nil
}

func (s *Service) beginEmailVerification(ctx context.Context, tx pgx.Tx, settingsRow appAuthSettingsRow, email string) (appAuthSettingsRow, string, error) {
	otpCode, err := generateOTPCode()
	if err != nil {
		return appAuthSettingsRow{}, "", err
	}
	now := s.nowUTC()
	expiresAt := now.Add(s.resetCodeTTL)
	scanner := tx.QueryRow(
		ctx,
		`UPDATE app_auth_settings
		SET pending_email = $2,
			email_verification_code_hash = $3,
			email_verification_expires_at = $4,
			email_verification_attempt_count = 0,
			updated_at = $5
		WHERE id = $1
		RETURNING id, auth_enabled, username, email, pending_email, password_hash, email_bound_at,
			email_verification_code_hash, email_verification_expires_at, email_verification_attempt_count,
			must_change_password, last_login_at, token_version, created_at, updated_at`,
		settingsRow.ID,
		email,
		hashOpaqueToken(otpCode),
		expiresAt,
		now,
	)
	updatedRow, err := scanAppAuthSettings(scanner)
	if err != nil {
		return appAuthSettingsRow{}, "", fmt.Errorf("begin email verification: %w", err)
	}
	return updatedRow, otpCode, nil
}

func (s *Service) confirmEmailVerification(ctx context.Context, tx pgx.Tx, settingsRow appAuthSettingsRow, otpCode string) (appAuthSettingsRow, error) {
	now := s.nowUTC()
	if !settingsRow.PendingEmail.Valid || !settingsRow.EmailVerificationCodeHash.Valid || !settingsRow.EmailVerificationExpiresAt.Valid || settingsRow.EmailVerificationExpiresAt.Time.Before(now) {
		return appAuthSettingsRow{}, &domainError{StatusCode: 400, Detail: "Email verification code is invalid or expired"}
	}
	if settingsRow.EmailVerificationAttemptCount >= 5 {
		return appAuthSettingsRow{}, &domainError{StatusCode: 429, Detail: "Too many verification attempts"}
	}
	attemptCount := settingsRow.EmailVerificationAttemptCount + 1
	if !verifyOpaqueToken(otpCode, settingsRow.EmailVerificationCodeHash.String) {
		if _, err := tx.Exec(
			ctx,
			`UPDATE app_auth_settings SET email_verification_attempt_count = $2, updated_at = $3 WHERE id = $1`,
			settingsRow.ID,
			attemptCount,
			now,
		); err != nil {
			return appAuthSettingsRow{}, fmt.Errorf("record email verification attempt: %w", err)
		}
		return appAuthSettingsRow{}, &domainError{StatusCode: 400, Detail: "Email verification code is invalid or expired"}
	}
	scanner := tx.QueryRow(
		ctx,
		`UPDATE app_auth_settings
		SET email = $2,
			email_bound_at = $3,
			pending_email = NULL,
			email_verification_code_hash = NULL,
			email_verification_expires_at = NULL,
			email_verification_attempt_count = 0,
			updated_at = $3
		WHERE id = $1
		RETURNING id, auth_enabled, username, email, pending_email, password_hash, email_bound_at,
			email_verification_code_hash, email_verification_expires_at, email_verification_attempt_count,
			must_change_password, last_login_at, token_version, created_at, updated_at`,
		settingsRow.ID,
		settingsRow.PendingEmail.String,
		now,
	)
	updatedRow, err := scanAppAuthSettings(scanner)
	if err != nil {
		return appAuthSettingsRow{}, fmt.Errorf("confirm email verification: %w", err)
	}
	return updatedRow, nil
}

func (s *Service) createPasswordResetChallenge(ctx context.Context, tx pgx.Tx, settingsRow appAuthSettingsRow, requestedIP string) (string, error) {
	otpCode, err := generateOTPCode()
	if err != nil {
		return "", err
	}
	now := s.nowUTC()
	if _, err := tx.Exec(
		ctx,
		`UPDATE password_reset_challenges SET consumed_at = $2 WHERE auth_subject_id = $1 AND consumed_at IS NULL`,
		settingsRow.ID,
		now,
	); err != nil {
		return "", fmt.Errorf("revoke existing password reset challenges: %w", err)
	}
	if _, err := tx.Exec(
		ctx,
		`INSERT INTO password_reset_challenges (
			auth_subject_id,
			otp_hash,
			expires_at,
			consumed_at,
			attempt_count,
			requested_ip,
			created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		settingsRow.ID,
		hashOpaqueToken(otpCode),
		now.Add(s.resetCodeTTL),
		nil,
		0,
		nullableTrimmedString(requestedIP),
		now,
	); err != nil {
		return "", fmt.Errorf("insert password reset challenge: %w", err)
	}
	return otpCode, nil
}

func (s *Service) loadLatestPasswordResetChallenge(ctx context.Context, exec queryExecutor, authSubjectID int) (passwordResetChallengeRow, error) {
	scanner := exec.QueryRow(
		ctx,
		`SELECT id, auth_subject_id, otp_hash, expires_at, consumed_at, attempt_count, requested_ip, created_at
		FROM password_reset_challenges
		WHERE auth_subject_id = $1 AND consumed_at IS NULL
		ORDER BY id DESC
		LIMIT 1`,
		authSubjectID,
	)
	row := passwordResetChallengeRow{}
	err := scanner.Scan(
		&row.ID,
		&row.AuthSubjectID,
		&row.OTPHash,
		&row.ExpiresAt,
		&row.ConsumedAt,
		&row.AttemptCount,
		&row.RequestedIP,
		&row.CreatedAt,
	)
	if err != nil {
		return passwordResetChallengeRow{}, err
	}
	return row, nil
}

func (s *Service) consumePasswordResetChallenge(ctx context.Context, tx pgx.Tx, otpCode string, newPassword string) error {
	settingsRow, err := s.loadOrCreateAppAuthSettings(ctx, tx)
	if err != nil {
		return fmt.Errorf("load auth settings: %w", err)
	}
	challenge, err := s.loadLatestPasswordResetChallenge(ctx, tx, settingsRow.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return &domainError{StatusCode: 400, Detail: "Reset code is invalid or expired"}
		}
		return fmt.Errorf("load password reset challenge: %w", err)
	}
	now := s.nowUTC()
	if challenge.ExpiresAt.Before(now) {
		return &domainError{StatusCode: 400, Detail: "Reset code is invalid or expired"}
	}
	if challenge.AttemptCount >= 5 {
		return &domainError{StatusCode: 429, Detail: "Too many reset attempts"}
	}
	newAttemptCount := challenge.AttemptCount + 1
	if !verifyOpaqueToken(otpCode, challenge.OTPHash) {
		if _, err := tx.Exec(ctx, `UPDATE password_reset_challenges SET attempt_count = $2 WHERE id = $1`, challenge.ID, newAttemptCount); err != nil {
			return fmt.Errorf("record password reset attempt: %w", err)
		}
		return &domainError{StatusCode: 400, Detail: "Reset code is invalid or expired"}
	}
	hash, err := hashPassword(newPassword)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(
		ctx,
		`UPDATE password_reset_challenges SET consumed_at = $2, attempt_count = $3 WHERE id = $1`,
		challenge.ID,
		now,
		newAttemptCount,
	); err != nil {
		return fmt.Errorf("consume password reset challenge: %w", err)
	}
	if err := s.revokeAllRefreshTokens(ctx, tx, settingsRow.ID); err != nil {
		return err
	}
	if _, err := tx.Exec(
		ctx,
		`UPDATE app_auth_settings
		SET password_hash = $2, token_version = token_version + 1, must_change_password = FALSE, updated_at = $3
		WHERE id = $1`,
		settingsRow.ID,
		hash,
		now,
	); err != nil {
		return fmt.Errorf("update password reset auth settings: %w", err)
	}
	return nil
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

func (s *Service) createProxyAPIKey(ctx context.Context, tx pgx.Tx, name string, notes *string, authSubjectID *int) (string, proxyAPIKeyRow, error) {
	var count int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM proxy_api_keys`).Scan(&count); err != nil {
		return "", proxyAPIKeyRow{}, fmt.Errorf("count proxy api keys: %w", err)
	}
	if count >= proxyKeyLimit {
		return "", proxyAPIKeyRow{}, &domainError{StatusCode: 409, Detail: fmt.Sprintf("Maximum %d proxy API keys reached", proxyKeyLimit)}
	}
	now := s.nowUTC()
	for attempt := 0; attempt < 5; attempt++ {
		rawKey, keyPrefix, lastFour, keyHash, err := buildProxyAPIKey()
		if err != nil {
			return "", proxyAPIKeyRow{}, err
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
			nil,
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
			return rawKey, row, nil
		}
		if !isUniqueConstraintError(scanErr, "uq_proxy_api_keys_prefix") {
			return "", proxyAPIKeyRow{}, fmt.Errorf("insert proxy api key: %w", scanErr)
		}
	}
	return "", proxyAPIKeyRow{}, &domainError{StatusCode: 500, Detail: "Failed to generate a unique proxy API key"}
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

func (s *Service) updateProxyAPIKey(ctx context.Context, tx pgx.Tx, keyID int, name string, notes *string, isActive *bool) (proxyAPIKeyRow, error) {
	current, err := s.loadProxyAPIKeyByID(ctx, tx, keyID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return proxyAPIKeyRow{}, &domainError{StatusCode: 404, Detail: "Proxy API key not found"}
		}
		return proxyAPIKeyRow{}, fmt.Errorf("load proxy api key: %w", err)
	}
	activeValue := current.IsActive
	if isActive != nil {
		activeValue = *isActive
	}
	scanner := tx.QueryRow(
		ctx,
		`UPDATE proxy_api_keys
		SET name = $2, notes = $3, is_active = $4, updated_at = $5
		WHERE id = $1
		RETURNING id, name, key_prefix, key_hash, last_four, is_active, expires_at, last_used_at,
			last_used_ip, created_by_auth_subject_id, notes, rotated_from_id, created_at, updated_at`,
		keyID,
		name,
		nullableTrimmedStringPtr(notes),
		activeValue,
		s.nowUTC(),
	)
	row, err := scanProxyAPIKey(scanner)
	if err != nil {
		return proxyAPIKeyRow{}, fmt.Errorf("update proxy api key: %w", err)
	}
	return row, nil
}

func (s *Service) rotateProxyAPIKey(ctx context.Context, tx pgx.Tx, keyID int) (string, proxyAPIKeyRow, error) {
	if _, err := s.loadProxyAPIKeyByID(ctx, tx, keyID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", proxyAPIKeyRow{}, &domainError{StatusCode: 404, Detail: "Proxy API key not found"}
		}
		return "", proxyAPIKeyRow{}, fmt.Errorf("load proxy api key: %w", err)
	}
	now := s.nowUTC()
	for attempt := 0; attempt < 5; attempt++ {
		rawKey, keyPrefix, lastFour, keyHash, err := buildProxyAPIKey()
		if err != nil {
			return "", proxyAPIKeyRow{}, err
		}
		scanner := tx.QueryRow(
			ctx,
			`UPDATE proxy_api_keys
			SET key_prefix = $2, key_hash = $3, last_four = $4, updated_at = $5
			WHERE id = $1
			RETURNING id, name, key_prefix, key_hash, last_four, is_active, expires_at, last_used_at,
				last_used_ip, created_by_auth_subject_id, notes, rotated_from_id, created_at, updated_at`,
			keyID,
			keyPrefix,
			keyHash,
			lastFour,
			now,
		)
		row, scanErr := scanProxyAPIKey(scanner)
		if scanErr == nil {
			return rawKey, row, nil
		}
		if !isUniqueConstraintError(scanErr, "uq_proxy_api_keys_prefix") {
			return "", proxyAPIKeyRow{}, fmt.Errorf("rotate proxy api key: %w", scanErr)
		}
	}
	return "", proxyAPIKeyRow{}, &domainError{StatusCode: 500, Detail: "Failed to rotate proxy API key"}
}

func (s *Service) deleteProxyAPIKey(ctx context.Context, tx pgx.Tx, keyID int) error {
	commandTag, err := tx.Exec(ctx, `DELETE FROM proxy_api_keys WHERE id = $1`, keyID)
	if err != nil {
		return fmt.Errorf("delete proxy api key: %w", err)
	}
	if commandTag.RowsAffected() == 0 {
		return &domainError{StatusCode: 404, Detail: "Proxy API key not found"}
	}
	return nil
}

func (s *Service) verifyProxyAPIKey(ctx context.Context, rawKey string) (*proxyAPIKeyRow, error) {
	normalizedKey, keyPrefix, err := parseProxyAPIKey(rawKey)
	if err != nil {
		return nil, nil
	}

	loadDecision := func() (RuntimeProxyKeyDecision, error) {
		row, err := s.loadProxyAPIKeyByPrefix(ctx, s.pool, keyPrefix)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return RuntimeProxyKeyDecision{}, nil
			}
			return RuntimeProxyKeyDecision{}, fmt.Errorf("load proxy api key by prefix: %w", err)
		}
		if !row.IsActive {
			return RuntimeProxyKeyDecision{}, nil
		}
		now := s.nowUTC()
		if row.ExpiresAt.Valid && row.ExpiresAt.Time.Before(now) {
			return RuntimeProxyKeyDecision{}, nil
		}
		if !verifyOpaqueToken(normalizedKey, row.KeyHash) {
			return RuntimeProxyKeyDecision{}, nil
		}
		decision := RuntimeProxyKeyDecision{Allowed: true, KeyID: row.ID, KeyName: row.Name}
		if row.ExpiresAt.Valid {
			expiresAt := row.ExpiresAt.Time.UTC()
			decision.ExpiresAt = &expiresAt
		}
		return decision, nil
	}

	var decision RuntimeProxyKeyDecision
	if s.runtimeCache != nil {
		decision, err = s.runtimeCache.LoadRuntimeProxyKeyDecision(s.nowUTC(), ProxyKeyDecisionCacheKey(normalizedKey), loadDecision)
		if isRuntimeCacheLoadInvalidated(err) {
			decision, err = s.runtimeCache.LoadRuntimeProxyKeyDecision(s.nowUTC(), ProxyKeyDecisionCacheKey(normalizedKey), loadDecision)
		}
	} else {
		decision, err = loadDecision()
	}
	if err != nil {
		return nil, err
	}
	if !decision.Allowed {
		return nil, nil
	}
	return &proxyAPIKeyRow{ID: decision.KeyID, Name: decision.KeyName}, nil
}

func (s *Service) loadProxyAPIKeyByPrefix(ctx context.Context, exec queryExecutor, keyPrefix string) (proxyAPIKeyRow, error) {
	scanner := exec.QueryRow(
		ctx,
		`SELECT id, name, key_prefix, key_hash, last_four, is_active, expires_at, last_used_at,
			last_used_ip, created_by_auth_subject_id, notes, rotated_from_id, created_at, updated_at
		FROM proxy_api_keys WHERE key_prefix = $1 ORDER BY id ASC LIMIT 1`,
		keyPrefix,
	)
	return scanProxyAPIKey(scanner)
}

func (s *Service) recordProxyAPIKeyUsage(ctx context.Context, keyID int, lastUsedAt time.Time, lastUsedIP string) error {
	_, err := s.pool.Exec(
		ctx,
		`UPDATE proxy_api_keys SET last_used_at = $2, last_used_ip = $3, updated_at = GREATEST(updated_at, $2) WHERE id = $1`,
		keyID,
		lastUsedAt,
		nullableTrimmedString(lastUsedIP),
	)
	if err != nil {
		return fmt.Errorf("record proxy api key usage: %w", err)
	}
	return nil
}

func (s *Service) buildAuthSettingsResponse(row appAuthSettingsRow) authSettingsResponse {
	return authSettingsResponse{
		AuthEnabled:               row.AuthEnabled,
		Username:                  nullableString(row.Username),
		Email:                     nullableString(row.Email),
		EmailBoundAt:              nullableTime(row.EmailBoundAt),
		PendingEmail:              nullableString(row.PendingEmail),
		EmailVerificationRequired: row.PendingEmail.Valid,
		HasPassword:               row.PasswordHash.Valid,
		ProxyKeyLimit:             proxyKeyLimit,
	}
}

func (s *Service) buildEmailVerificationResponse(row appAuthSettingsRow) emailVerificationResponse {
	return emailVerificationResponse{
		Success:      true,
		PendingEmail: nullableString(row.PendingEmail),
		Email:        nullableString(row.Email),
		EmailBoundAt: nullableTime(row.EmailBoundAt),
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

func isUniqueConstraintError(err error, constraintName string) bool {
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) {
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

func validateEmail(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if !strings.Contains(trimmed, "@") || strings.HasPrefix(trimmed, "@") || strings.HasSuffix(trimmed, "@") {
		return "", &domainError{StatusCode: 400, Detail: "email must be valid"}
	}
	if len(trimmed) > 320 {
		return "", &domainError{StatusCode: 400, Detail: "email must be at most 320 characters"}
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
