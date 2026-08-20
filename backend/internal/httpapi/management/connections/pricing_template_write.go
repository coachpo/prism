package connections

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/coachpo/prism/backend/internal/domain/pricingkind"
	"github.com/coachpo/prism/backend/internal/domain/terminaltarget"
	"github.com/jackc/pgx/v5"
)

func nullableCardString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func cloneTemplateInt(value *int) *int {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func createPricingTemplateWithShape(ctx context.Context, tx pgx.Tx, profileID int, currentTime time.Time, name string, description *string, shape pricingTemplateShape) (pricingTemplateResponse, error) {
	var epochID int64
	var epochOrdinal int
	var epochCode string
	if err := tx.QueryRow(ctx, `SELECT epochs.id, epochs.epoch, epochs.currency_code FROM reporting_currency_epochs AS epochs JOIN user_settings AS settings ON settings.current_reporting_currency_epoch_id = epochs.id WHERE settings.profile_id = $1 AND epochs.superseded_at IS NULL FOR UPDATE OF settings`, profileID).Scan(&epochID, &epochOrdinal, &epochCode); err != nil {
		return pricingTemplateResponse{}, fmt.Errorf("load active reporting currency epoch for profile %d: %w", profileID, err)
	}
	var templateID int
	if err := tx.QueryRow(ctx, `INSERT INTO pricing_templates (profile_id, name, description, current_revision_id, created_at, updated_at) VALUES ($1, $2, $3, NULL, $4, $4) RETURNING id`, profileID, name, description, currentTime).Scan(&templateID); err != nil {
		return pricingTemplateResponse{}, fmt.Errorf("insert logical pricing template %q: %w", name, err)
	}
	operationID, err := reserveAndRecordPricingMutation(ctx, tx, profileID, "template_create", templateID, name, currentTime)
	if err != nil {
		return pricingTemplateResponse{}, err
	}
	revisionID, err := insertPricingRevisionWithShape(ctx, tx, templateID, 1, epochCode, epochID, epochOrdinal, "manual_create", operationID, currentTime, shape)
	if err != nil {
		return pricingTemplateResponse{}, err
	}
	if err := insertPricingMutationResultItem(ctx, tx, operationID, 1, templateID, "created", intPtr(1), &revisionID, currentTime, name); err != nil {
		return pricingTemplateResponse{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE pricing_templates SET current_revision_id = $1, updated_at = $2 WHERE id = $3`, revisionID, currentTime, templateID); err != nil {
		return pricingTemplateResponse{}, fmt.Errorf("close pricing template current revision pointer: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE user_settings SET pricing_template_generation = pricing_template_generation + 1, updated_at = $2 WHERE profile_id = $1`, profileID, currentTime); err != nil {
		return pricingTemplateResponse{}, fmt.Errorf("advance pricing template generation for profile %d: %w", profileID, err)
	}
	created, found, err := loadPricingTemplate(ctx, tx, profileID, templateID, false)
	if err != nil {
		return pricingTemplateResponse{}, err
	}
	if !found {
		return pricingTemplateResponse{}, fmt.Errorf("created pricing template %d disappeared", templateID)
	}
	return created, nil
}

func insertPricingRevisionWithShape(ctx context.Context, tx pgx.Tx, templateID, version int, currencyCode string, epochID int64, epochOrdinal int, createdByKind, operationID string, currentTime time.Time, shape pricingTemplateShape) (int64, error) {
	var revisionID int64
	var timezone, digest any
	if shape.Timezone != nil {
		timezone = *shape.Timezone
	}
	if shape.Kind == pricingkind.PeakValley {
		digest = shape.Digest
	}
	if err := tx.QueryRow(ctx, `INSERT INTO pricing_template_revisions (template_id, version, pricing_unit, currency_code, reporting_currency_epoch_id, reporting_currency_epoch, currency_attribution, template_kind, tier_input_tokens_above, pricing_schedule_timezone, pricing_schedule_digest, effective_at, created_at, created_by_kind, created_by_operation_id) VALUES ($1,$2,'PER_1M',$3,$4,$5,'active_epoch',$6,$7,$8,$9,$10,$10,$11,$12) RETURNING id`, templateID, version, currencyCode, epochID, epochOrdinal, string(shape.Kind), shape.TierThreshold, timezone, digest, currentTime, createdByKind, operationID).Scan(&revisionID); err != nil {
		return 0, fmt.Errorf("insert pricing template revision: %w", err)
	}
	for role, card := range shape.Cards {
		if _, err := tx.Exec(ctx, `INSERT INTO pricing_template_cards (revision_id, template_kind, card_role, input_price, output_price, cached_input_price, cache_creation_price, reasoning_price) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, revisionID, string(shape.Kind), role, card.InputPrice, card.OutputPrice, nullableCardString(card.CachedInputPrice), nullableCardString(card.CacheCreationPrice), nullableCardString(card.ReasoningPrice)); err != nil {
			return 0, fmt.Errorf("insert pricing template card %s: %w", role, err)
		}
	}
	for _, window := range shape.Windows {
		if _, err := tx.Exec(ctx, `INSERT INTO pricing_template_windows (revision_id, weekday_mask, start_minute, end_minute, created_at) VALUES ($1,$2,$3,$4,$5)`, revisionID, window.WeekdayMask, window.StartMinute, window.EndMinute, currentTime); err != nil {
			return 0, fmt.Errorf("insert pricing template window: %w", err)
		}
	}
	return revisionID, nil
}

func updatePricingTemplateWithShape(ctx context.Context, tx pgx.Tx, profileID int, current pricingTemplateResponse, nextName string, nextDescription *string, shape pricingTemplateShape, currentTime time.Time) error {
	currentShape := pricingTemplateShapeFromResponse(current)
	shapeChanged := !pricingTemplateShapesEqual(currentShape, shape)
	nameChanged := nextName != current.Name
	descriptionChanged := !stringsEqualPointers(nextDescription, current.Description)
	if !shapeChanged && !nameChanged && !descriptionChanged {
		return nil
	}
	if !shapeChanged {
		operationID, err := reserveAndRecordPricingMutation(ctx, tx, profileID, "template_update", current.ID, nextName, currentTime)
		if err != nil {
			return err
		}
		if err := insertPricingMutationResultItem(ctx, tx, operationID, 1, current.ID, "metadata_updated", nil, nil, currentTime, nextName); err != nil {
			return err
		}
	} else {
		var epochID int64
		var epochOrdinal int
		var epochCode string
		if err := tx.QueryRow(ctx, `SELECT epochs.id, epochs.epoch, epochs.currency_code FROM reporting_currency_epochs AS epochs JOIN user_settings AS settings ON settings.current_reporting_currency_epoch_id = epochs.id WHERE settings.profile_id = $1 AND epochs.superseded_at IS NULL FOR UPDATE OF settings`, profileID).Scan(&epochID, &epochOrdinal, &epochCode); err != nil {
			return err
		}
		operationID, err := reserveAndRecordPricingMutation(ctx, tx, profileID, "template_update", current.ID, nextName, currentTime)
		if err != nil {
			return err
		}
		revisionID, err := insertPricingRevisionWithShape(ctx, tx, current.ID, current.Version+1, epochCode, epochID, epochOrdinal, "manual_edit", operationID, currentTime, shape)
		if err != nil {
			return err
		}
		action := "revision_created"
		if nameChanged || descriptionChanged {
			action = "metadata_and_revision"
		}
		if err := insertPricingMutationResultItem(ctx, tx, operationID, 1, current.ID, action, intPtr(current.Version+1), &revisionID, currentTime, nextName); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE pricing_templates SET current_revision_id = $1 WHERE id = $2`, revisionID, current.ID); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE pricing_templates SET name = $2, description = $3, updated_at = $4 WHERE id = $1`, current.ID, nextName, nextDescription, currentTime); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE user_settings SET pricing_template_generation = pricing_template_generation + 1, updated_at = $2 WHERE profile_id = $1`, profileID, currentTime)
	return err
}

func pricingTemplateShapeFromResponse(item pricingTemplateResponse) pricingTemplateShape {
	shape := pricingTemplateShape{Kind: pricingkind.Kind(item.TemplateKind), Cards: make(map[string]pricingTemplateCard), Windows: append([]terminaltarget.Window(nil), item.windows...)}
	for role, card := range item.cards {
		shape.Cards[role] = card
	}
	if item.Tier != nil && item.Tier.InputTokensAbove > 0 {
		shape.TierThreshold = intPtr(item.Tier.InputTokensAbove)
	}
	if item.Schedule != nil {
		timezone := strings.TrimSpace(item.Schedule.Timezone)
		shape.Timezone = &timezone
	}
	if shape.Kind == pricingkind.PeakValley {
		shape.Digest = pricingTemplateWindowsDigest(shape.Windows)
	}
	return shape
}

func pricingTemplateShapesEqual(left, right pricingTemplateShape) bool {
	if left.Kind != right.Kind || (left.TierThreshold == nil) != (right.TierThreshold == nil) || (left.TierThreshold != nil && *left.TierThreshold != *right.TierThreshold) || !stringsEqualPointers(left.Timezone, right.Timezone) || left.Digest != right.Digest || len(left.Cards) != len(right.Cards) || len(left.Windows) != len(right.Windows) {
		return false
	}
	for role, card := range left.Cards {
		other, ok := right.Cards[role]
		if !ok || card.InputPrice != other.InputPrice || card.OutputPrice != other.OutputPrice || !stringsEqualPointers(card.CachedInputPrice, other.CachedInputPrice) || !stringsEqualPointers(card.CacheCreationPrice, other.CacheCreationPrice) || !stringsEqualPointers(card.ReasoningPrice, other.ReasoningPrice) {
			return false
		}
	}
	for i := range left.Windows {
		if left.Windows[i] != right.Windows[i] {
			return false
		}
	}
	return true
}

func pricingTemplateCreateRequestFromUpdate(current pricingTemplateResponse, request pricingTemplateUpdateRequest) (pricingTemplateCreateRequest, error) {
	shape := pricingTemplateShapeFromResponse(current)
	result := pricingTemplateCreateRequest{Name: current.Name, Description: current.Description, TemplateKind: string(shape.Kind)}
	toInput := func(card pricingTemplateCard) *pricingTemplateCardInput {
		return &pricingTemplateCardInput{InputPrice: stringPtr(card.InputPrice), OutputPrice: stringPtr(card.OutputPrice), CachedInputPrice: cloneString(card.CachedInputPrice), CacheCreationPrice: cloneString(card.CacheCreationPrice), ReasoningPrice: cloneString(card.ReasoningPrice)}
	}
	if card, ok := shape.Cards[pricingkind.RoleStandard]; ok {
		result.Card = toInput(card)
	}
	if card, ok := shape.Cards[pricingkind.RoleTierBase]; ok {
		result.BaseCard = toInput(card)
	}
	if card, ok := shape.Cards[pricingkind.RoleTierAbove]; ok {
		result.Tier = &pricingTemplateTierInput{InputTokensAbove: cloneTemplateInt(shape.TierThreshold), Card: toInput(card)}
	}
	if card, ok := shape.Cards[pricingkind.RolePeak]; ok {
		result.PeakCard = toInput(card)
	}
	if card, ok := shape.Cards[pricingkind.RoleOffpeak]; ok {
		result.OffpeakCard = toInput(card)
	}
	if shape.Timezone != nil {
		result.Schedule = &pricingTemplateScheduleInput{Timezone: *shape.Timezone}
		for _, window := range shape.Windows {
			result.Schedule.Windows = append(result.Schedule.Windows, pricingTemplateWindowInput{WeekdayMask: window.WeekdayMask, StartMinute: window.StartMinute, EndMinute: window.EndMinute})
		}
	}
	if request.TemplateKind.Set {
		result.TemplateKind = strings.TrimSpace(derefString(request.TemplateKind.Value))
	}
	// Retype is a complete shape replacement. Do not let the current kind's
	// branch leak into the target variant and trigger a false mixed-shape
	// rejection (or, worse, silently preserve an incompatible card).
	switch pricingkind.Kind(result.TemplateKind) {
	case pricingkind.Standard:
		result.BaseCard, result.Tier, result.PeakCard, result.OffpeakCard, result.Schedule = nil, nil, nil, nil, nil
	case pricingkind.Tiered:
		result.Card, result.PeakCard, result.OffpeakCard, result.Schedule = nil, nil, nil, nil
	case pricingkind.PeakValley:
		result.Card, result.BaseCard, result.Tier = nil, nil, nil
	}
	applyRaw := func(raw optionalRawPricingShape, target **pricingTemplateCardInput) error {
		if !raw.Set {
			return nil
		}
		if bytes.Equal(bytes.TrimSpace(raw.Raw), []byte("null")) {
			*target = nil
			return nil
		}
		var value pricingTemplateCardInput
		decoder := json.NewDecoder(bytes.NewReader(raw.Raw))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&value); err != nil {
			return err
		}
		*target = &value
		return nil
	}
	if err := applyRaw(request.Card, &result.Card); err != nil {
		return pricingTemplateCreateRequest{}, err
	}
	if err := applyRaw(request.BaseCard, &result.BaseCard); err != nil {
		return pricingTemplateCreateRequest{}, err
	}
	if request.Tier.Set {
		result.Tier = request.Tier.Value
	}
	if err := applyRaw(request.PeakCard, &result.PeakCard); err != nil {
		return pricingTemplateCreateRequest{}, err
	}
	if err := applyRaw(request.OffpeakCard, &result.OffpeakCard); err != nil {
		return pricingTemplateCreateRequest{}, err
	}
	if request.Schedule.Set {
		if bytes.Equal(bytes.TrimSpace(request.Schedule.Raw), []byte("null")) {
			result.Schedule = nil
		} else {
			var value pricingTemplateScheduleInput
			decoder := json.NewDecoder(bytes.NewReader(request.Schedule.Raw))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&value); err != nil {
				return pricingTemplateCreateRequest{}, err
			}
			result.Schedule = &value
		}
	}
	return result, nil
}
