package auth

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/coachpo/prism/backend/internal/platform/config"
	"github.com/jackc/pgx/v5/pgconn"
)

type testRuntimeAuthConfigProvider struct {
	snapshot RuntimeAuthConfigSnapshot
}

func (p testRuntimeAuthConfigProvider) AuthRuntimeConfigSnapshot() RuntimeAuthConfigSnapshot {
	return p.snapshot
}

func requireAuthDomainError(t *testing.T, err error, status int, detail string) {
	t.Helper()

	var domainErr *domainError
	if !errors.As(err, &domainErr) {
		t.Fatalf("expected domainError, got %T", err)
	}
	if domainErr.StatusCode != status || domainErr.Detail != detail {
		t.Fatalf("expected domainError (%d, %q), got (%d, %q)", status, detail, domainErr.StatusCode, domainErr.Detail)
	}
}

func TestRuntimeAuthConfigSnapshotFallsBackToStaticSettings(t *testing.T) {
	service := &Service{staticAuthRuntimeConfig: runtimeAuthConfigSnapshotFromSettings(config.Settings{
		AuthAccessTokenTTLSeconds:  11,
		AuthRefreshTokenTTLSeconds: 13,
		AuthCookieName:             " static_access ",
		AuthRefreshCookieName:      " static_refresh ",
		AuthCookieSecure:           true,
	})}

	snapshot := service.runtimeAuthConfigSnapshot()
	if snapshot.AccessTokenTTL != 11*time.Second || snapshot.RefreshTokenTTL != 13*time.Second {
		t.Fatalf("unexpected static TTL snapshot: %+v", snapshot)
	}
	if snapshot.AccessCookieName != "static_access" || snapshot.RefreshCookieName != "static_refresh" || !snapshot.CookieSecure {
		t.Fatalf("unexpected static cookie snapshot: %+v", snapshot)
	}
}

func TestRuntimeAuthConfigSnapshotUsesProviderWhenPresent(t *testing.T) {
	service := &Service{
		staticAuthRuntimeConfig: runtimeAuthConfigSnapshotFromSettings(config.Settings{
			AuthAccessTokenTTLSeconds:  11,
			AuthRefreshTokenTTLSeconds: 13,
			AuthCookieName:             "static_access",
			AuthRefreshCookieName:      "static_refresh",
		}),
		authRuntimeConfigProvider: testRuntimeAuthConfigProvider{snapshot: RuntimeAuthConfigSnapshot{
			AccessTokenTTL:    19 * time.Second,
			RefreshTokenTTL:   23 * time.Second,
			AccessCookieName:  "hot_access",
			RefreshCookieName: "hot_refresh",
			CookieSecure:      true,
		}},
	}

	snapshot := service.runtimeAuthConfigSnapshot()
	if snapshot.AccessTokenTTL != 19*time.Second || snapshot.RefreshTokenTTL != 23*time.Second {
		t.Fatalf("unexpected provider TTL snapshot: %+v", snapshot)
	}
	if snapshot.AccessCookieName != "hot_access" || snapshot.RefreshCookieName != "hot_refresh" || !snapshot.CookieSecure {
		t.Fatalf("unexpected provider cookie snapshot: %+v", snapshot)
	}
}

func TestNormalizeNotes(t *testing.T) {
	notes := "  ops key  "
	if got := normalizeNotes(&notes); got == nil || *got != "ops key" {
		t.Fatalf("expected trimmed notes, got %#v", got)
	}
	blankNotes := "   "
	if got := normalizeNotes(&blankNotes); got != nil {
		t.Fatalf("expected blank notes to normalize to nil, got %v", *got)
	}
}

func TestValidateProxyKeyName(t *testing.T) {
	if got, err := validateProxyKeyName("  Primary Key  "); err != nil || got != "Primary Key" {
		t.Fatalf("expected trimmed proxy key name, got name=%q err=%v", got, err)
	}
	if _, err := validateProxyKeyName("   "); err == nil {
		t.Fatal("expected empty proxy key name to fail")
	} else {
		requireAuthDomainError(t, err, 400, "name must not be empty")
	}
	if _, err := validateProxyKeyName(strings.Repeat("x", 201)); err == nil {
		t.Fatal("expected overly long proxy key name to fail")
	} else {
		requireAuthDomainError(t, err, 400, "name must be at most 200 characters")
	}

}

func TestLoginThrottleKeyNormalizesSubjectAndRemoteAddress(t *testing.T) {
	key := loginThrottleKeyFor("  Admin@Example.COM  ", "  LOCALHOST  ")
	if key.SubjectKey != "admin@example.com" || key.RemoteAddress != "localhost" {
		t.Fatalf("unexpected throttle key: %+v", key)
	}
	if blankRemote := loginThrottleKeyFor("admin", "   "); blankRemote.RemoteAddress != "unknown" {
		t.Fatalf("expected blank remote address to normalize to unknown, got %+v", blankRemote)
	}
	if key.advisoryLockID() != loginThrottleKeyFor("admin@example.com", "localhost").advisoryLockID() {
		t.Fatal("expected equivalent throttle keys to share advisory lock id")
	}
}

func TestIsUniqueConstraintError(t *testing.T) {
	if !isUniqueConstraintError(&pgconn.PgError{ConstraintName: "proxy_api_keys_name_key"}, "proxy_api_keys_name_key") {
		t.Fatal("expected pg error constraint match to count as unique constraint error")
	}
	if !isUniqueConstraintError(errors.New("duplicate key value violates unique constraint proxy_api_keys_name_key"), "proxy_api_keys_name_key") {
		t.Fatal("expected text fallback to count as unique constraint error")
	}
	if isUniqueConstraintError(errors.New("different failure"), "proxy_api_keys_name_key") {
		t.Fatal("expected unrelated error not to count as unique constraint error")
	}
}

// countingPasswordVerifier counts compares so tests can prove the
// anti-enumeration contract: exactly one same-cost compare per login attempt
// for known, missing and mismatched usernames.
type countingPasswordVerifier struct {
	compares int
}

func (v *countingPasswordVerifier) Verify(password string, passwordHash string) bool {
	v.compares++
	return verifyPassword(password, passwordHash)
}

func TestVerifyPasswordOncePerformsExactlyOneCompare(t *testing.T) {
	service := &Service{passwordVerifier: &countingPasswordVerifier{}}
	hash, err := hashPassword("correct-password")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if !service.verifyPasswordOnce("correct-password", hash) {
		t.Fatal("expected correct password to verify")
	}
	if service.passwordVerifier.(*countingPasswordVerifier).compares != 1 {
		t.Fatalf("expected exactly one compare on success, got %d", service.passwordVerifier.(*countingPasswordVerifier).compares)
	}
	if service.verifyPasswordOnce("wrong-password", hash) {
		t.Fatal("expected wrong password to fail")
	}
	if service.passwordVerifier.(*countingPasswordVerifier).compares != 2 {
		t.Fatalf("expected exactly one compare per attempt, got %d", service.passwordVerifier.(*countingPasswordVerifier).compares)
	}
}

func TestVerifyPasswordOnceFallsBackToBcryptWhenNil(t *testing.T) {
	service := &Service{}
	hash, err := hashPassword("fallback-password")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if !service.verifyPasswordOnce("fallback-password", hash) {
		t.Fatal("expected nil verifier to fall back to bcrypt compare")
	}
}
