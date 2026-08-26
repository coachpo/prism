package contracttest

import (
	"context"
	"net/http"
	"testing"
)

func TestCurrencyMigrationAtomicCutover(t *testing.T) {
	harness := newS11ContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)
	profileID := defaultProfileID

	// Seed two active pricing templates with current prices.
	templateA := insertContractPricingTemplateWithPrices(t, harness, profileID, "Migrate Template A", "2", "5", "0", "0", "0")
	templateB := insertContractPricingTemplateWithPrices(t, harness, profileID, "Migrate Template B", "1", "3", "0.5", "0", "0")

	settings := requestJSONStatus[map[string]any](t, harness, http.MethodGet, "/api/settings/costing", nil, modelHeader(profileID), http.StatusOK)
	updatedAt := settings["updated_at"].(string)

	// Draft creation and chunk upload keep each request bounded; preview only
	// references the sealed server-side draft.
	var preview map[string]any
	var draftID, operationID string
	if err := harness.conn.QueryRow(context.Background(), `SELECT gen_random_uuid()::text, gen_random_uuid()::text`).Scan(&draftID, &operationID); err != nil {
		t.Fatalf("generate currency draft identifiers: %v", err)
	}
	templates := requestJSONStatus[[]map[string]any](t, harness, http.MethodGet, "/api/pricing-templates", nil, modelHeader(profileID), http.StatusOK)
	if len(templates) != 2 {
		t.Fatalf("expected two active pricing templates, got %+v", templates)
	}
	draft := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/settings/costing/currency-migration-drafts", map[string]any{
		"draft_id": draftID, "migration_operation_id": operationID, "operation_kind": "currency_cutover",
		"target_currency_code": "EUR", "target_currency_symbol": "€", "expected_inventory_id": nil,
		"expected_inventory_hash": nil, "expected_inventory_generation": nil, "expected_reporting_currency_epoch": 1,
		"expected_settings_updated_at": updatedAt,
	}, modelHeader(profileID), http.StatusCreated)
	chunkItems := make([]map[string]any, 0, len(templates))
	for _, template := range templates {
		chunkItems = append(chunkItems, map[string]any{
			"template_id": template["id"], "expected_version": template["version"], "expected_updated_at": template["updated_at"],
			"template_kind": template["template_kind"], "cards": currencyMigrationCardsForTemplate(t, template),
		})
	}
	requestJSONStatus[map[string]any](t, harness, http.MethodPut, "/api/settings/costing/currency-migration-drafts/"+draftID+"/chunks/1", map[string]any{"items": chunkItems}, modelHeader(profileID), http.StatusOK)
	draft = requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/settings/costing/currency-migration-drafts/"+draftID+"/seal", nil, modelHeader(profileID), http.StatusOK)
	preview = requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/settings/costing/currency-migrations/preview", map[string]any{
		"operation_kind": "currency_cutover", "migration_operation_id": operationID, "draft_id": draftID, "draft_hash": draft["draft_hash"],
	}, modelHeader(profileID), http.StatusOK)
	if preview["current_currency_code"] != "USD" || jsonInt(t, preview["current_epoch"]) != 1 || jsonInt(t, preview["next_epoch"]) != 2 || preview["epoch_change"] != true {
		t.Fatalf("expected USD epoch 1 -> EUR epoch 2 preview, got %+v", preview)
	}
	if jsonInt(t, preview["template_count"]) != 2 || jsonInt(t, preview["revision_change_count"]) != 2 {
		t.Fatalf("expected two templates to bump versions, got %+v", preview)
	}
	previewHash := preview["preview_hash"].(string)

	// No writes happened during preview.
	var epochCount int
	if err := harness.conn.QueryRow(context.Background(), `SELECT count(*) FROM reporting_currency_epochs WHERE profile_id = $1`, profileID).Scan(&epochCount); err != nil {
		t.Fatalf("count epochs after preview: %v", err)
	}
	if epochCount != 1 {
		t.Fatalf("preview must not create epochs, got %d", epochCount)
	}
	var settingsCurrency string
	if err := harness.conn.QueryRow(context.Background(), `SELECT report_currency_code FROM user_settings WHERE profile_id = $1`, profileID).Scan(&settingsCurrency); err != nil {
		t.Fatalf("load settings currency: %v", err)
	}
	if settingsCurrency != "USD" {
		t.Fatalf("preview must not change settings currency, got %q", settingsCurrency)
	}

	// Stale commit (wrong preview hash) fails closed.
	staleCommit := harness.requestJSON(t, harness.client, http.MethodPost, "/api/settings/costing/currency-migrations/commit", map[string]any{
		"operation_kind": "currency_cutover", "migration_operation_id": operationID, "draft_id": draftID,
		"draft_hash": draft["draft_hash"], "preview_hash": "not-the-hash",
	}, modelHeader(profileID))
	assertErrorResponse(t, staleCommit, http.StatusConflict, "currency_migration_stale: preview no longer matches the sealed draft or current settings")

	// Commit atomically cuts over.
	commit := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/settings/costing/currency-migrations/commit", map[string]any{
		"operation_kind": "currency_cutover", "migration_operation_id": operationID, "draft_id": draftID,
		"draft_hash": draft["draft_hash"], "preview_hash": previewHash,
	}, modelHeader(profileID), http.StatusOK)
	if commit["new_currency_code"] != "EUR" || jsonInt(t, commit["new_epoch"]) != 2 || jsonInt(t, commit["revision_change_count"]) != 2 {
		t.Fatalf("expected EUR epoch 2 with two revisions, got %+v", commit)
	}

	// Settings + epoch pointers switched.
	var settingsCurrency2 string
	var settingsEpoch int
	if err := harness.conn.QueryRow(context.Background(), `SELECT settings.report_currency_code, epochs.epoch FROM user_settings AS settings JOIN reporting_currency_epochs AS epochs ON epochs.id = settings.current_reporting_currency_epoch_id WHERE settings.profile_id = $1`, profileID).Scan(&settingsCurrency2, &settingsEpoch); err != nil {
		t.Fatalf("load migrated settings: %v", err)
	}
	if settingsCurrency2 != "EUR" || settingsEpoch != 2 {
		t.Fatalf("expected settings to point at EUR epoch 2, got code=%q epoch=%d", settingsCurrency2, settingsEpoch)
	}

	// Old epoch superseded; new epoch active.
	var oldSuperseded bool
	var newActive bool
	if err := harness.conn.QueryRow(context.Background(), `SELECT superseded_at IS NOT NULL FROM reporting_currency_epochs WHERE profile_id = $1 AND epoch = 1`, profileID).Scan(&oldSuperseded); err != nil {
		t.Fatalf("load old epoch state: %v", err)
	}
	if err := harness.conn.QueryRow(context.Background(), `SELECT superseded_at IS NULL FROM reporting_currency_epochs WHERE profile_id = $1 AND epoch = 2`, profileID).Scan(&newActive); err != nil {
		t.Fatalf("load new epoch state: %v", err)
	}
	if !oldSuperseded || !newActive {
		t.Fatalf("expected old epoch superseded and new epoch active, got %v/%v", oldSuperseded, newActive)
	}

	// Both templates moved to v2 with EUR revisions; history retained.
	assertTemplateRevisionAtEpoch(t, harness, profileID, templateA, 2, "EUR", "2", "5")
	assertTemplateRevisionAtEpoch(t, harness, profileID, templateB, 2, "EUR", "1", "3")
	assertTemplateRevisionCount(t, harness, templateA, 2)

	// Immutable migration ledger records the cutover.
	var ledgerCount int
	var ledgerKind string
	if err := harness.conn.QueryRow(context.Background(), `SELECT count(*), operation_kind FROM currency_migration_ledger WHERE profile_id = $1 GROUP BY operation_kind`, profileID).Scan(&ledgerCount, &ledgerKind); err != nil {
		t.Fatalf("load migration ledger: %v", err)
	}
	if ledgerCount != 1 || ledgerKind != "currency_cutover" {
		t.Fatalf("expected one currency_cutover ledger, got %d %q", ledgerCount, ledgerKind)
	}

	// Costing settings now report EUR epoch 2.
	after := requestJSONStatus[map[string]any](t, harness, http.MethodGet, "/api/settings/costing", nil, modelHeader(profileID), http.StatusOK)
	if after["report_currency_code"] != "EUR" || after["reporting_currency_epoch"] != "2" {
		t.Fatalf("expected costing settings to report EUR epoch 2, got %+v", after)
	}

	// A second draft with the SAME target fails closed even when there are no
	// active templates left to migrate; there is no empty-instance shortcut.
	var duplicateDraftID, duplicateOperationID string
	if err := harness.conn.QueryRow(context.Background(), `SELECT gen_random_uuid()::text, gen_random_uuid()::text`).Scan(&duplicateDraftID, &duplicateOperationID); err != nil {
		t.Fatalf("generate duplicate currency draft identifiers: %v", err)
	}
	duplicate := harness.requestJSON(t, harness.client, http.MethodPost, "/api/settings/costing/currency-migration-drafts", map[string]any{
		"draft_id": duplicateDraftID, "migration_operation_id": duplicateOperationID, "operation_kind": "currency_cutover",
		"target_currency_code": "EUR", "target_currency_symbol": "€", "expected_inventory_id": nil,
		"expected_inventory_hash": nil, "expected_inventory_generation": nil, "expected_reporting_currency_epoch": 2,
		"expected_settings_updated_at": after["updated_at"].(string),
	}, modelHeader(profileID))
	assertErrorResponse(t, duplicate, http.StatusConflict, "currency_migration_required: target currency must differ from the current reporting currency")
}

func currencyMigrationCardsForTemplate(t *testing.T, template map[string]any) []map[string]any {
	t.Helper()
	card := func(role string, raw any) map[string]any {
		value, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("expected %s card object, got %T %+v", role, raw, raw)
		}
		return map[string]any{"card_role": role, "input_price": value["input_price"], "output_price": value["output_price"], "cached_input_price": value["cached_input_price"], "cache_creation_price": value["cache_creation_price"], "reasoning_price": value["reasoning_price"]}
	}
	kind, _ := template["template_kind"].(string)
	switch kind {
	case "standard":
		return []map[string]any{card("standard", template["card"])}
	case "tiered":
		tier, ok := template["tier"].(map[string]any)
		if !ok {
			t.Fatalf("expected tier object, got %T %+v", template["tier"], template["tier"])
		}
		return []map[string]any{card("tier_base", template["base_card"]), card("tier_above", tier["card"])}
	case "peak_valley":
		return []map[string]any{card("peak", template["peak_card"]), card("offpeak", template["offpeak_card"])}
	default:
		t.Fatalf("unsupported template kind %q", kind)
		return nil
	}
}

func TestCurrencyMigrationAllowsTypedTieredTemplate(t *testing.T) {
	harness := newS11ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	createdTemplate := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/pricing-templates", map[string]any{
		"name": "Tiered Currency Cards", "template_kind": "tiered", "base_card": map[string]any{"input_price": "2", "output_price": "5", "cached_input_price": "1", "cache_creation_price": "2", "reasoning_price": "3"},
		"tier": map[string]any{"input_tokens_above": 100, "card": map[string]any{"input_price": "4", "output_price": "18", "cached_input_price": "1", "cache_creation_price": "2", "reasoning_price": "3"}},
	}, modelHeader(profileID), http.StatusCreated)
	if createdTemplate["template_kind"] != "tiered" {
		t.Fatalf("expected tiered template, got %+v", createdTemplate)
	}
	createdPeakTemplate := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/pricing-templates", map[string]any{
		"name": "Peak Currency Cards", "template_kind": "peak_valley",
		"peak_card":    map[string]any{"input_price": "10", "output_price": "20", "cached_input_price": "1", "cache_creation_price": "2", "reasoning_price": "3"},
		"offpeak_card": map[string]any{"input_price": "1", "output_price": "2", "cached_input_price": "1", "cache_creation_price": "2", "reasoning_price": "3"},
		"schedule":     map[string]any{"timezone": "Europe/Helsinki", "windows": []map[string]any{{"weekday_mask": 31, "start_minute": 540, "end_minute": 720}}},
	}, modelHeader(profileID), http.StatusCreated)
	settings := requestJSONStatus[map[string]any](t, harness, http.MethodGet, "/api/settings/costing", nil, modelHeader(profileID), http.StatusOK)
	var draftID, operationID string
	if err := harness.conn.QueryRow(context.Background(), `SELECT gen_random_uuid()::text, gen_random_uuid()::text`).Scan(&draftID, &operationID); err != nil {
		t.Fatalf("generate currency card identifiers: %v", err)
	}
	requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/settings/costing/currency-migration-drafts", map[string]any{
		"draft_id": draftID, "migration_operation_id": operationID, "operation_kind": "currency_cutover", "target_currency_code": "EUR", "target_currency_symbol": "€",
		"expected_inventory_id": nil, "expected_inventory_hash": nil, "expected_inventory_generation": nil, "expected_reporting_currency_epoch": 1, "expected_settings_updated_at": settings["updated_at"],
	}, modelHeader(profileID), http.StatusCreated)
	templates := requestJSONStatus[[]map[string]any](t, harness, http.MethodGet, "/api/pricing-templates", nil, modelHeader(profileID), http.StatusOK)
	if len(templates) != 2 {
		t.Fatalf("expected tiered and peak typed templates, got %+v", templates)
	}
	chunks := make([]map[string]any, 0, len(templates))
	for _, template := range templates {
		chunks = append(chunks, map[string]any{"template_id": template["id"], "expected_version": template["version"], "expected_updated_at": template["updated_at"], "template_kind": template["template_kind"], "cards": currencyMigrationCardsForTemplate(t, template)})
	}
	requestJSONStatus[map[string]any](t, harness, http.MethodPut, "/api/settings/costing/currency-migration-drafts/"+draftID+"/chunks/1", map[string]any{"items": chunks}, modelHeader(profileID), http.StatusOK)
	sealed := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/settings/costing/currency-migration-drafts/"+draftID+"/seal", nil, modelHeader(profileID), http.StatusOK)
	preview := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/settings/costing/currency-migrations/preview", map[string]any{"operation_kind": "currency_cutover", "migration_operation_id": operationID, "draft_id": draftID, "draft_hash": sealed["draft_hash"]}, modelHeader(profileID), http.StatusOK)
	commit := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/settings/costing/currency-migrations/commit", map[string]any{"operation_kind": "currency_cutover", "migration_operation_id": operationID, "draft_id": draftID, "draft_hash": sealed["draft_hash"], "preview_hash": preview["preview_hash"]}, modelHeader(profileID), http.StatusOK)
	if commit["revision_change_count"] != float64(2) {
		t.Fatalf("expected two typed revision changes, got %+v", commit)
	}
	var kind, roles string
	if err := harness.conn.QueryRow(context.Background(), `SELECT revisions.template_kind, string_agg(cards.card_role, ',' ORDER BY cards.card_role) FROM pricing_template_revisions revisions JOIN pricing_templates templates ON templates.current_revision_id = revisions.id JOIN pricing_template_cards cards ON cards.revision_id = revisions.id WHERE templates.id = $1 GROUP BY revisions.template_kind`, intValue(createdTemplate["id"])).Scan(&kind, &roles); err != nil {
		t.Fatalf("load migrated typed cards: %v", err)
	}
	if kind != "tiered" || roles != "tier_above,tier_base" {
		t.Fatalf("expected complete tiered card set after migration, got kind=%q roles=%q", kind, roles)
	}
	var peakKind, peakRoles, timezone, digest string
	var windowCount int
	if err := harness.conn.QueryRow(context.Background(), `SELECT revisions.template_kind, string_agg(cards.card_role, ',' ORDER BY cards.card_role), revisions.pricing_schedule_timezone, revisions.pricing_schedule_digest, (SELECT count(*) FROM pricing_template_windows windows WHERE windows.revision_id = revisions.id) FROM pricing_template_revisions revisions JOIN pricing_templates templates ON templates.current_revision_id = revisions.id JOIN pricing_template_cards cards ON cards.revision_id = revisions.id WHERE templates.id = $1 GROUP BY revisions.id`, intValue(createdPeakTemplate["id"])).Scan(&peakKind, &peakRoles, &timezone, &digest, &windowCount); err != nil {
		t.Fatalf("load migrated peak typed cards: %v", err)
	}
	if peakKind != "peak_valley" || peakRoles != "offpeak,peak" || timezone != "Europe/Helsinki" || digest == "" || windowCount != 1 {
		t.Fatalf("expected peak selector and windows after migration, got kind=%q roles=%q timezone=%q digest=%q windows=%d", peakKind, peakRoles, timezone, digest, windowCount)
	}
}

func assertTemplateRevisionAtEpoch(t *testing.T, harness *contractHarness, profileID int, templateID int, version int, currency string, input string, output string) {
	t.Helper()
	var gotVersion int
	var gotCurrency string
	var gotInput string
	var gotOutput string
	var gotEpoch int
	if err := harness.conn.QueryRow(context.Background(), `SELECT revisions.version, revisions.currency_code, cards.input_price, cards.output_price, revisions.reporting_currency_epoch FROM pricing_template_revisions AS revisions JOIN pricing_templates AS templates ON templates.current_revision_id = revisions.id JOIN pricing_template_cards AS cards ON cards.revision_id = revisions.id AND cards.card_role = 'standard' WHERE templates.id = $1`, templateID).Scan(&gotVersion, &gotCurrency, &gotInput, &gotOutput, &gotEpoch); err != nil {
		t.Fatalf("load migrated template %d revision: %v", templateID, err)
	}
	if gotVersion != version || gotCurrency != currency || gotInput != input || gotOutput != output || gotEpoch != 2 {
		t.Fatalf("template %d expected v%d %s %s/%s epoch 2, got v%d %s %s/%s epoch %d", templateID, version, currency, input, output, gotVersion, gotCurrency, gotInput, gotOutput, gotEpoch)
	}
}

func assertTemplateRevisionCount(t *testing.T, harness *contractHarness, templateID int, want int) {
	t.Helper()
	var got int
	if err := harness.conn.QueryRow(context.Background(), `SELECT count(*) FROM pricing_template_revisions WHERE template_id = $1`, templateID).Scan(&got); err != nil {
		t.Fatalf("count template %d revisions: %v", templateID, err)
	}
	if got != want {
		t.Fatalf("expected %d revisions for template %d, got %d", want, templateID, got)
	}
}
