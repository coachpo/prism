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
)

const loginThrottleFailureLimit = 5

const loginThrottleWindow = 15 * time.Minute

const loginThrottleLockoutDuration = 15 * time.Minute

const loginThrottleLockoutDetail = "登录尝试过多，请稍后重试"

type loginThrottleKey struct {
	SubjectKey    string
	RemoteAddress string
}

type loginThrottleDecision struct {
	FailureCount int
	LockedUntil  sql.NullTime
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

func (s *Service) lockLoginThrottleKey(ctx context.Context, tx pgx.Tx, key loginThrottleKey) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1::bigint)`, key.advisoryLockID()); err != nil {
		return fmt.Errorf("lock login throttle key: %w", err)
	}
	return nil
}

func (s *Service) lockedDomainError(now time.Time, lockedUntil sql.NullTime) *domainError {
	if !lockedUntil.Valid || !lockedUntil.Time.After(now) {
		return nil
	}
	retryAfter := int64(lockedUntil.Time.Sub(now).Seconds())
	if retryAfter < 0 {
		retryAfter = 0
	}
	return &domainError{
		StatusCode: http.StatusTooManyRequests,
		Code:       ProblemCodeAuthLoginLocked,
		Detail:     loginThrottleLockoutDetail,
		Details: AuthLoginLockedDetails{
			RetryAt:           lockedUntil.Time.UTC(),
			RetryAfterSeconds: retryAfter,
		},
	}
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
	if err := s.lockedDomainError(s.nowUTC(), lockedUntil); err != nil {
		return err
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
		return s.lockedDomainError(now, decision.LockedUntil)
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
