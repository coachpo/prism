package connections

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/domain/modelsdev"
)

// dedupeCatalogTemplateName derives the source-linked template name from the
// offering coordinates and appends numeric suffixes until free.
func dedupeCatalogTemplateName(ctx context.Context, exec queryExecutor, profileID int, offering modelsdev.Offering) (string, error) {
	base := offering.ProviderID + "/" + offering.ModelID
	candidate := base
	for attempt := 1; attempt <= 50; attempt++ {
		var existingID int
		err := exec.QueryRow(ctx, `SELECT id FROM pricing_templates WHERE profile_id = $1 AND name = $2 AND deleted_at IS NULL LIMIT 1`, profileID, candidate).Scan(&existingID)
		if err == pgx.ErrNoRows {
			return candidate, nil
		}
		if err != nil {
			return "", fmt.Errorf("check catalog template name availability for %q: %w", candidate, err)
		}
		candidate = fmt.Sprintf("%s (%d)", base, attempt+1)
	}
	return "", &domainError{StatusCode: http.StatusConflict, Detail: fmt.Sprintf("unable to derive a free catalog template name from %q", base)}
}

// lockAndAssignCatalogTarget enforces the existing Terminal Target double CAS
// (updated_at + pricing_template_id) under a sorted row lock.
func lockAndAssignCatalogTarget(ctx context.Context, tx pgx.Tx, profileID int, expected catalogTargetState, templateID int, currentTime time.Time) error {
	var currentUpdatedAt time.Time
	var currentTemplateID *int
	err := tx.QueryRow(ctx,
		`SELECT updated_at, pricing_template_id FROM connections WHERE id = $1 AND profile_id = $2 FOR UPDATE`,
		expected.ConnectionID, profileID).Scan(&currentUpdatedAt, &currentTemplateID)
	if err == pgx.ErrNoRows {
		return &domainError{StatusCode: http.StatusConflict, Detail: fmt.Sprintf("Terminal Target %d disappeared; assignment aborted", expected.ConnectionID)}
	}
	if err != nil {
		return fmt.Errorf("lock Terminal Target %d for catalog assignment: %w", expected.ConnectionID, err)
	}
	if !currentUpdatedAt.Equal(expected.UpdatedAt) {
		return &domainError{
			StatusCode: http.StatusConflict,
			Detail:     fmt.Sprintf("Terminal Target %d changed since the preview; assignment aborted", expected.ConnectionID),
			Fields:     map[string]any{"pricing_cas_conflict": true, "connection_id": expected.ConnectionID},
		}
	}
	if (currentTemplateID == nil) != (expected.PricingTemplateID == nil) ||
		(currentTemplateID != nil && expected.PricingTemplateID != nil && *currentTemplateID != *expected.PricingTemplateID) {
		return &domainError{
			StatusCode: http.StatusConflict,
			Detail:     fmt.Sprintf("Terminal Target %d references a different pricing template since the preview; assignment aborted", expected.ConnectionID),
			Fields:     map[string]any{"pricing_cas_conflict": true, "connection_id": expected.ConnectionID},
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE connections SET pricing_template_id = $2, updated_at = $3 WHERE id = $1`, expected.ConnectionID, templateID, currentTime); err != nil {
		return fmt.Errorf("assign catalog template to Terminal Target %d: %w", expected.ConnectionID, err)
	}
	return nil
}
