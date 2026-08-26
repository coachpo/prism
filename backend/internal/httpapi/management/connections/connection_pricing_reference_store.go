package connections

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

func validatePricingTemplateID(ctx context.Context, exec queryExecutor, profileID int, pricingTemplateID *int) (*int, error) {
	if pricingTemplateID == nil {
		return nil, nil
	}
	var existingID int
	err := exec.QueryRow(ctx, `SELECT id FROM pricing_templates WHERE profile_id = $1 AND id = $2 LIMIT 1`, profileID, *pricingTemplateID).Scan(&existingID)
	if err == pgx.ErrNoRows {
		return nil, &DomainError{StatusCode: 404, Detail: "Pricing template not found"}
	}
	if err != nil {
		return nil, fmt.Errorf("load pricing template %d for profile %d: %w", *pricingTemplateID, profileID, err)
	}
	resolved := existingID
	return &resolved, nil
}
