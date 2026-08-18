package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// proxyKeyReadinessSnapshot counts the key ledger exactly once at counted_at
// with the disabled -> expired -> active classification priority (SPEC §8.2).
// The generation and counts are persisted by the Proxy owner. Wall-clock
// passage across the 30-second safety frontier therefore advances the same
// generation as a key mutation instead of relying on an expiry worker.
type proxyKeyReadinessSnapshot struct {
	Generation          string
	LastReadyGeneration string
	CountedAt           time.Time
	Active              int64
	Expired             int64
	Disabled            int64
	SafeActive          int64
}

// lockProxyKeyReadiness is the Proxy-owned domain fence. Auth staging and all
// key-ledger mutations acquire it before the auth control singleton.
func lockProxyKeyReadiness(ctx context.Context, exec queryExecutor) error {
	var generation int64
	if err := exec.QueryRow(ctx, `SELECT generation FROM proxy_key_readiness_state WHERE id = 1 FOR UPDATE`).Scan(&generation); err != nil {
		return fmt.Errorf("lock proxy key readiness fence: %w", err)
	}
	if generation < 1 {
		return fmt.Errorf("proxy key readiness generation is invalid")
	}
	return nil
}

func (s *Service) captureProxyKeyReadiness(ctx context.Context, exec interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}) (proxyKeyReadinessSnapshot, error) {
	if err := lockProxyKeyReadiness(ctx, exec); err != nil {
		return proxyKeyReadinessSnapshot{}, err
	}
	var storedGeneration int64
	var storedFingerprint string
	if err := exec.QueryRow(ctx, `SELECT generation, classification_fingerprint
		FROM proxy_key_readiness_state WHERE id = 1`).Scan(&storedGeneration, &storedFingerprint); err != nil {
		return proxyKeyReadinessSnapshot{}, err
	}
	var countedAt time.Time
	if err := exec.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&countedAt); err != nil {
		return proxyKeyReadinessSnapshot{Generation: fmt.Sprintf("%d", storedGeneration), LastReadyGeneration: fmt.Sprintf("%d", storedGeneration)}, err
	}
	rows, err := exec.Query(ctx, `SELECT id, is_active, expires_at FROM proxy_api_keys`)
	if err != nil {
		return proxyKeyReadinessSnapshot{Generation: fmt.Sprintf("%d", storedGeneration), LastReadyGeneration: fmt.Sprintf("%d", storedGeneration), CountedAt: countedAt}, err
	}
	defer rows.Close()
	type keyState struct {
		id    int
		class string
		safe  bool
	}
	states := []keyState{}
	active, expired, disabled := int64(0), int64(0), int64(0)
	safeActive := int64(0)
	for rows.Next() {
		var id int
		var isActive bool
		var expiresAt *time.Time
		if err := rows.Scan(&id, &isActive, &expiresAt); err != nil {
			return proxyKeyReadinessSnapshot{}, err
		}
		state := keyState{id: id}
		switch {
		case !isActive:
			state.class = "disabled"
			disabled++
		case expiresAt != nil && !expiresAt.After(countedAt):
			state.class = "expired"
			expired++
		default:
			state.class = "active"
			active++
			state.safe = expiresAt == nil || expiresAt.After(countedAt.Add(authActivationSafetyHorizonSeconds*time.Second))
			if state.safe {
				safeActive++
			}
		}
		states = append(states, state)
	}
	if err := rows.Err(); err != nil {
		return proxyKeyReadinessSnapshot{}, err
	}
	sort.Slice(states, func(i, j int) bool { return states[i].id < states[j].id })
	hash := sha256.New()
	for _, state := range states {
		_, _ = hash.Write([]byte(fmt.Sprintf("%d:%s:%v|", state.id, state.class, state.safe)))
	}
	fingerprint := hex.EncodeToString(hash.Sum(nil))
	if storedFingerprint != fingerprint && storedFingerprint != "" {
		storedGeneration++
	}
	if storedGeneration < 1 {
		storedGeneration = 1
	}
	if _, err := exec.Exec(ctx, `UPDATE proxy_key_readiness_state SET
		generation = $1, classification_fingerprint = $2, counted_at = $3,
		active_count = $4, expired_count = $5, disabled_count = $6,
		safe_active_count = $7, updated_at = $3 WHERE id = 1`,
		storedGeneration, fingerprint, countedAt.UTC(), active, expired, disabled, safeActive); err != nil {
		return proxyKeyReadinessSnapshot{Generation: fmt.Sprintf("%d", storedGeneration), LastReadyGeneration: fmt.Sprintf("%d", storedGeneration), CountedAt: countedAt}, err
	}
	return proxyKeyReadinessSnapshot{
		Generation:          fmt.Sprintf("%d", storedGeneration),
		LastReadyGeneration: fmt.Sprintf("%d", storedGeneration),
		CountedAt:           countedAt,
		Active:              active,
		Expired:             expired,
		Disabled:            disabled,
		SafeActive:          safeActive,
	}, nil
}
