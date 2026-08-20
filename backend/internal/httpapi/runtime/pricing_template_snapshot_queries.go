package runtime

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/coachpo/prism/backend/internal/domain/terminaltarget"
	"github.com/jackc/pgx/v5"
)

func nullableSQLString(value sql.NullString) string {
	if !value.Valid {
		return ""
	}
	return value.String
}

func listPricingTemplateCardsForRevisions(ctx context.Context, tx pgx.Tx, revisionIDs []int64) (map[int64]map[string]runtimePricingCard, error) {
	result := make(map[int64]map[string]runtimePricingCard, len(revisionIDs))
	if len(revisionIDs) == 0 {
		return result, nil
	}
	rows, err := tx.Query(ctx, `
		SELECT revision_id, card_role, input_price, output_price, cached_input_price, cache_creation_price, reasoning_price
		FROM pricing_template_cards
		WHERE revision_id = ANY($1)
		ORDER BY revision_id ASC, card_role ASC`, revisionIDs)
	if err != nil {
		return nil, fmt.Errorf("query pricing template cards: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var revisionID int64
		var role string
		var input, output string
		var cached, creation, reasoning sql.NullString
		if err := rows.Scan(&revisionID, &role, &input, &output, &cached, &creation, &reasoning); err != nil {
			return nil, fmt.Errorf("scan pricing template card: %w", err)
		}
		if result[revisionID] == nil {
			result[revisionID] = make(map[string]runtimePricingCard)
		}
		result[revisionID][role] = runtimePricingCard{
			InputPrice: input, OutputPrice: output,
			CachedInputPrice: nullableSQLString(cached), CacheCreationPrice: nullableSQLString(creation),
			ReasoningPrice: nullableSQLString(reasoning),
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pricing template cards: %w", err)
	}
	return result, nil
}

func listPricingTemplateWindowsForRevisions(ctx context.Context, tx pgx.Tx, revisionIDs []int64) (map[int64][]terminaltarget.Window, error) {
	result := make(map[int64][]terminaltarget.Window, len(revisionIDs))
	if len(revisionIDs) == 0 {
		return result, nil
	}
	rows, err := tx.Query(ctx, `
		SELECT revision_id, weekday_mask, start_minute, end_minute
		FROM pricing_template_windows
		WHERE revision_id = ANY($1)
		ORDER BY revision_id ASC, weekday_mask ASC, start_minute ASC, end_minute ASC`, revisionIDs)
	if err != nil {
		return nil, fmt.Errorf("query pricing template windows: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var revisionID, mask, start, end int
		if err := rows.Scan(&revisionID, &mask, &start, &end); err != nil {
			return nil, fmt.Errorf("scan pricing template window: %w", err)
		}
		result[int64(revisionID)] = append(result[int64(revisionID)], terminaltarget.Window{WeekdayMask: mask, StartMinute: start, EndMinute: end})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pricing template windows: %w", err)
	}
	return result, nil
}
