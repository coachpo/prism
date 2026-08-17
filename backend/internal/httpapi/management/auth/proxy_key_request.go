package auth

// This file owns proxy-key request validation, expiry normalization, and the
// transaction-bound usage handoff. Row scans, capacity locks, and secret
// replacement remain in proxy_key_store.go.

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/coachpo/prism/backend/internal/httpapi/proxykeyusage"
	"github.com/jackc/pgx/v5"
)

// RecordProxyAPIKeyUsageTx is the transaction-bound usage handoff used by
// runtime attribution.
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

// resolveProxyKeyCreateExpiry validates a create expiry: omitted/null means
// never expires; a value must be a strict future instant.
// resolveProxyKeyCreateExpiry applies the create-request omission rule.
func resolveProxyKeyCreateExpiry(expiresAt *time.Time, now time.Time) (*time.Time, error) {
	if expiresAt == nil {
		return nil, nil
	}
	return resolveProxyKeyFutureExpiry(expiresAt, now)
}

// resolveProxyKeyFutureExpiry rejects non-future instants with a locatable
// field error.
// resolveProxyKeyFutureExpiry is shared by create and update validation.
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
