package settings

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

type computedCurrencyPreview struct {
	Response               currencyMigrationDraftPreviewResponse
	Items                  []currencyMigrationPreviewItem
	PreviewHash            string
	AuthoritativeTemplates []currencyDraftAuthoritativeTemplate
	DraftItems             []currencyMigrationDraftItem
}

func buildCurrencyMigrationPreview(ctx context.Context, tx pgx.Tx, header currencyMigrationDraftHeaderRow, profileID int) (computedCurrencyPreview, error) {
	settingsRow, found, err := loadUserSettings(ctx, tx, profileID, false)
	if err != nil || !found {
		if err != nil {
			return computedCurrencyPreview{}, err
		}
		return computedCurrencyPreview{}, fmt.Errorf("currency settings are missing")
	}
	return buildCurrencyMigrationPreviewWithSettings(ctx, tx, header, profileID, settingsRow, false)
}

func buildCurrencyMigrationPreviewWithSettings(ctx context.Context, tx pgx.Tx, header currencyMigrationDraftHeaderRow, profileID int, settingsRow userSettingsRow, forUpdate bool) (computedCurrencyPreview, error) {
	if header.Status != "sealed" && header.Status != "committed" {
		return computedCurrencyPreview{}, currencyMigrationDraftStateError(header.Status)
	}
	if header.DraftHash == nil {
		return computedCurrencyPreview{}, currencyMigrationDraftConflict()
	}
	currentEpoch, err := loadCurrencyMigrationEpochOnly(ctx, tx, settingsRow)
	if err != nil {
		return computedCurrencyPreview{}, err
	}
	authoritative, err := loadCurrencyDraftAuthoritativeTemplates(ctx, tx, profileID, forUpdate)
	if err != nil {
		return computedCurrencyPreview{}, err
	}
	draftItems, err := loadCurrencyDraftItems(ctx, tx, header.DraftID)
	if err != nil {
		return computedCurrencyPreview{}, err
	}
	byID := make(map[int]currencyMigrationDraftItem, len(draftItems))
	for _, item := range draftItems {
		byID[item.TemplateID] = item
	}
	previewItems := make([]currencyMigrationPreviewItem, 0, len(authoritative))
	for _, template := range authoritative {
		item, ok := byID[template.ID]
		if !ok {
			return computedCurrencyPreview{}, &domainError{StatusCode: http.StatusConflict, Detail: fmt.Sprintf("currency_migration_draft_template_set_changed: template_id=%d", template.ID)}
		}
		expected, parseErr := time.Parse(time.RFC3339Nano, item.ExpectedUpdatedAt)
		if parseErr != nil || expected.UTC() != template.UpdatedAt.UTC() || item.ExpectedVersion != template.Version {
			return computedCurrencyPreview{}, &domainError{StatusCode: http.StatusConflict, Detail: fmt.Sprintf("currency_migration_stale: template_id=%d", template.ID)}
		}
		if item.TemplateKind != template.TemplateKind || !currencyMigrationCardsHaveRoles(template.Cards, template.TemplateKind) || !currencyMigrationCardsHaveRoles(item.Cards, template.TemplateKind) {
			return computedCurrencyPreview{}, &domainError{StatusCode: http.StatusConflict, Detail: fmt.Sprintf("currency_migration_draft_shape_changed: template_id=%d", template.ID)}
		}
		previewItems = append(previewItems, currencyMigrationPreviewItem{
			TemplateID: template.ID, Name: template.Name, CurrentVersion: template.Version, NextVersion: template.Version + 1,
			TemplateKind: template.TemplateKind, CurrentCards: template.Cards, NewCards: item.Cards, ReferenceCount: template.ReferenceCount,
		})
		delete(byID, template.ID)
	}
	if len(byID) != 0 || len(previewItems) != len(draftItems) {
		return computedCurrencyPreview{}, &domainError{StatusCode: http.StatusConflict, Detail: "currency_migration_draft_template_set_changed"}
	}
	previewHash := hashCurrencyMigrationPreview(header, settingsRow, currentEpoch, previewItems)
	nextEpoch := 1
	if currentEpoch != nil {
		nextEpoch = *currentEpoch + 1
	}
	nextEpochValue := nextEpoch
	response := currencyMigrationDraftPreviewResponse{
		OperationKind: header.OperationKind, MigrationOperationID: header.MigrationOperationID, DraftID: header.DraftID, DraftHash: stringValue(header.DraftHash), PreviewHash: previewHash,
		TargetCurrencyCode: header.TargetCurrencyCode, TargetCurrencySymbol: header.TargetCurrencySymbol, CurrentCurrencyCode: nullableNonEmptyString(settingsRow.ReportCurrencyCode),
		CurrentEpoch: currentEpoch, NextEpoch: &nextEpochValue, TemplateCount: len(previewItems), RevisionChangeCount: len(previewItems), Committable: true, ValidationErrors: []map[string]any{}, EpochChange: true,
	}
	return computedCurrencyPreview{Response: response, Items: previewItems, PreviewHash: previewHash, AuthoritativeTemplates: authoritative, DraftItems: draftItems}, nil
}

func hashCurrencyMigrationPreview(header currencyMigrationDraftHeaderRow, settingsRow userSettingsRow, currentEpoch *int, items []currencyMigrationPreviewItem) string {
	canonical := struct {
		HeaderHash string                         `json:"header_hash"`
		DraftHash  string                         `json:"draft_hash"`
		SettingsAt string                         `json:"settings_updated_at"`
		Code       string                         `json:"code"`
		Epoch      *int                           `json:"epoch"`
		Items      []currencyMigrationPreviewItem `json:"items"`
	}{header.NormalizedHeaderHash, stringValue(header.DraftHash), settingsRow.UpdatedAt.UTC().Format(time.RFC3339Nano), settingsRow.ReportCurrencyCode, currentEpoch, items}
	raw, _ := json.Marshal(canonical)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
