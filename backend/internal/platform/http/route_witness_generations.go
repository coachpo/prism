package platformhttp

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// advanceRouteWitnessGenerations advances the per-profile static route-witness
// generation exactly once per route-affecting mutation transaction (the
// invalidation middleware classifies which mutations are route-affecting). It
// is the sole writer of route_witness_generations: no other path may bump the
// generation, and no-op writes never advance it.
func advanceRouteWitnessGenerations(ctx context.Context, tx pgx.Tx, profileIDs []int) error {
	for _, profileID := range profileIDs {
		if _, err := tx.Exec(
			ctx,
			`INSERT INTO route_witness_generations (profile_id, generation, updated_at)
			VALUES ($1, 1, $2)
			ON CONFLICT (profile_id) DO UPDATE
			SET generation = route_witness_generations.generation + 1, updated_at = EXCLUDED.updated_at`,
			profileID,
			time.Now().UTC(),
		); err != nil {
			return fmt.Errorf("advance route witness generation for profile %d: %w", profileID, err)
		}
	}
	return nil
}
