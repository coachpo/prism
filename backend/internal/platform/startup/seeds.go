package startup

import (
	"context"
	"fmt"

	"github.com/coachpo/prism/backend/internal/endpointdomain"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	"github.com/jackc/pgx/v5"
)

func (s Service) normalizeEndpointSecrets(ctx context.Context, conn *pgx.Conn) error {
	return pgxutil.InTx(ctx, conn, "startup", func(tx pgx.Tx) error {
		rows, err := tx.Query(
			ctx,
			`SELECT id, api_key, api_key_fingerprint FROM endpoints ORDER BY id ASC`,
		)
		if err != nil {
			return fmt.Errorf("query endpoints for secret normalization: %w", err)
		}
		defer rows.Close()

		type endpointRow struct {
			ID          int
			APIKey      string
			Fingerprint *string
		}
		endpoints := []endpointRow{}
		for rows.Next() {
			var row endpointRow
			if err := rows.Scan(&row.ID, &row.APIKey, &row.Fingerprint); err != nil {
				return fmt.Errorf("scan endpoint for secret normalization: %w", err)
			}
			endpoints = append(endpoints, row)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("iterate endpoints for secret normalization: %w", err)
		}

		for _, endpoint := range endpoints {
			plaintext, err := endpointdomain.DecryptSecret(endpoint.APIKey, s.secretEncryptionKey)
			if err != nil {
				return fmt.Errorf("normalize endpoint secret %d: %w", endpoint.ID, err)
			}
			var fingerprint *string
			if plaintext != "" {
				derived := endpointdomain.APIKeyFingerprint(s.secretEncryptionKey, plaintext)
				fingerprint = &derived
			}
			encrypted, err := endpointdomain.EncryptSecret(endpoint.APIKey, s.secretEncryptionKey, s.now)
			if err != nil {
				return fmt.Errorf("normalize endpoint secret %d: %w", endpoint.ID, err)
			}
			unchanged := encrypted == endpoint.APIKey && sameOptionalString(endpoint.Fingerprint, fingerprint)
			if unchanged {
				continue
			}
			if _, err := tx.Exec(
				ctx,
				`UPDATE endpoints SET api_key = $2, api_key_fingerprint = $3 WHERE id = $1`,
				endpoint.ID,
				encrypted,
				fingerprint,
			); err != nil {
				return fmt.Errorf("update normalized endpoint secret %d: %w", endpoint.ID, err)
			}
		}

		return nil
	})
}

func sameOptionalString(left *string, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
