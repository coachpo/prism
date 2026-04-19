package auth

import (
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func authStringRef(value string) *string {
	return &value
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

func TestNormalizeUsernameAndNotes(t *testing.T) {
	if got := normalizeUsername(nil); got != nil {
		t.Fatalf("expected nil username when source is nil, got %v", *got)
	}
	if got := normalizeUsername(authStringRef("  admin  ")); got == nil || *got != "admin" {
		t.Fatalf("expected trimmed username, got %#v", got)
	}
	if got := normalizeUsername(authStringRef("   ")); got != nil {
		t.Fatalf("expected blank username to normalize to nil, got %v", *got)
	}
	if got := normalizeNotes(authStringRef("  ops key  ")); got == nil || *got != "ops key" {
		t.Fatalf("expected trimmed notes, got %#v", got)
	}
	if got := normalizeNotes(authStringRef("   ")); got != nil {
		t.Fatalf("expected blank notes to normalize to nil, got %v", *got)
	}
}

func TestValidateProxyKeyNameAndEmail(t *testing.T) {
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

	if got, err := validateEmail("  admin@example.com  "); err != nil || got != "admin@example.com" {
		t.Fatalf("expected trimmed email, got email=%q err=%v", got, err)
	}
	if _, err := validateEmail("invalid"); err == nil {
		t.Fatal("expected invalid email to fail")
	} else {
		requireAuthDomainError(t, err, 400, "email must be valid")
	}
	if _, err := validateEmail(strings.Repeat("a", 321) + "@example.com"); err == nil {
		t.Fatal("expected overly long email to fail")
	} else {
		requireAuthDomainError(t, err, 400, "email must be at most 320 characters")
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
