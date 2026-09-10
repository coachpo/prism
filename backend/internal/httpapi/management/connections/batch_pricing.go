package connections

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"net/http"
	"time"
)

// BatchPricingAssignment is a validated, locked target assignment. Its snapshot
// carries only authoring facts; credentials and live capacity never enter CAS.
type BatchPricingAssignment struct {
	ID             int
	Label          string
	Before         map[string]any
	After          map[string]any
	ManualOverride bool
	current        catalogTargetState
	templateID     int
}

// PrepareBatchPricingAssignmentTx reuses the pricing and capability owners.
// The caller owns the transaction and must validate every item before Apply.
func PrepareBatchPricingAssignmentTx(ctx context.Context, tx pgx.Tx, profileID, id, templateID int, now time.Time) (BatchPricingAssignment, error) {
	out := BatchPricingAssignment{ID: id}
	target, found, err := loadConnectionRecord(ctx, tx, profileID, id, true, now)
	if err != nil {
		return out, err
	}
	if !found || target.ModelConfigID == nil {
		return out, &DomainError{StatusCode: http.StatusUnprocessableEntity, Detail: "Terminal Target not found"}
	}
	owner, found, err := loadModelRecord(ctx, tx, profileID, *target.ModelConfigID, false)
	if err != nil {
		return out, err
	}
	if !found {
		return out, &DomainError{StatusCode: 422, Detail: "Target owner not found"}
	}
	if err := validateCopyCapabilityDimensions(owner, target.OpenAITextCapability, target.OpenAIImageCapability); err != nil {
		return out, err
	}
	template, found, err := loadPricingTemplate(ctx, tx, profileID, templateID, true)
	if err != nil {
		return out, err
	}
	if !found || template.DeletedAt != nil {
		return out, &DomainError{StatusCode: 422, Detail: "Pricing template unavailable"}
	}
	out.Label = fmt.Sprintf("%s / #%d", owner.ModelID, id)
	out.Before = map[string]any{"pricing_template_id": target.PricingTemplateID, "updated_at": target.UpdatedAt, "owner_model_id": owner.ModelID}
	out.After = map[string]any{"pricing_template_id": template.ID, "pricing_template_name": template.Name, "revision_id": template.RevisionID, "currency": template.PricingCurrencyCode}
	out.ManualOverride = target.PricingTemplateID != nil && *target.PricingTemplateID != templateID
	out.current = catalogTargetState{ConnectionID: id, UpdatedAt: target.UpdatedAt, PricingTemplateID: target.PricingTemplateID}
	out.templateID = templateID
	return out, nil
}

func (assignment BatchPricingAssignment) Apply(ctx context.Context, tx pgx.Tx, profileID int, now time.Time) error {
	if !now.After(assignment.current.UpdatedAt) {
		now = assignment.current.UpdatedAt.Add(time.Microsecond)
	}
	return lockAndAssignCatalogTarget(ctx, tx, profileID, assignment.current, assignment.templateID, now)
}

// FinishBatchPricingAssignmentsTx invalidates reference-count snapshots once
// for the atomic batch, in the same transaction as the target assignments.
func FinishBatchPricingAssignmentsTx(ctx context.Context, tx pgx.Tx, profileID int, now time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE user_settings SET pricing_reference_generation=pricing_reference_generation+1, updated_at=$2 WHERE profile_id=$1`, profileID, now)
	return err
}

// LockBatchPricingScopeTx shares the existing pricing writer serialization
// boundary. It must precede model/target locks to avoid reversing the catalog
// commit's profile → settings → template → target order.
func LockBatchPricingScopeTx(ctx context.Context, tx pgx.Tx, profileID int) error {
	return lockProfileRow(ctx, tx, profileID)
}
