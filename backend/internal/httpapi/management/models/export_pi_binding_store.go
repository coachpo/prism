package models

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

const piBindingSelectColumns = `bindings.model_config_id, bindings.provider_id, bindings.catalog_model_id, bindings.api, bindings.prism_model_id_at_bind, bindings.bind_source, bindings.catalog_revision, bindings.fetched_at, bindings.updated_at,
	bindings.source_name, bindings.source_reasoning, bindings.source_input::text, bindings.source_context_window, bindings.source_max_tokens, bindings.source_thinking_level_map::text, bindings.source_compat::text,
	bindings.source_dropped_fields::text,
	bindings.override_name, bindings.override_reasoning, bindings.override_input::text, bindings.override_context_window, bindings.override_max_tokens, bindings.override_thinking_level_map::text, bindings.override_compat::text`

func scanPiBindingRow(scanner interface{ Scan(...any) error }) (piBindingRecord, error) {
	record := piBindingRecord{}
	var sourceInput, sourceThinking, sourceCompat, sourceDroppedFields any
	var overrideInput, overrideThinking, overrideCompat any
	err := scanner.Scan(
		&record.ModelConfigID, &record.ProviderID, &record.CatalogModelID, &record.API, &record.PrismModelIDAtBind, &record.BindSource, &record.CatalogRevision, &record.FetchedAt, &record.UpdatedAt,
		&record.Source.Name, &record.Source.Reasoning, &sourceInput, &record.Source.ContextWindow, &record.Source.MaxTokens, &sourceThinking, &sourceCompat, &sourceDroppedFields,
		&record.Override.Name, &record.Override.Reasoning, &overrideInput, &record.Override.ContextWindow, &record.Override.MaxTokens, &overrideThinking, &overrideCompat,
	)
	if err != nil {
		return record, err
	}
	if record.Source.Input, err = decodeStringSliceColumn(sourceInput); err != nil {
		return record, fmt.Errorf("decode source input for model %d: %w", record.ModelConfigID, err)
	}
	if record.Source.ThinkingLevelMap, err = decodeThinkingLevelMapColumn(sourceThinking); err != nil {
		return record, fmt.Errorf("decode source thinking_level_map for model %d: %w", record.ModelConfigID, err)
	}
	if record.Source.Compat, err = decodeCompatColumn(sourceCompat); err != nil {
		return record, fmt.Errorf("decode source compat for model %d: %w", record.ModelConfigID, err)
	}
	if record.DroppedFields, err = decodeStringSliceColumn(sourceDroppedFields); err != nil {
		return record, fmt.Errorf("decode source dropped_fields for model %d: %w", record.ModelConfigID, err)
	}
	record.DroppedFields = normalizePiDroppedFields(record.DroppedFields)
	if record.Override.Input, err = decodeStringSliceColumn(overrideInput); err != nil {
		return record, fmt.Errorf("decode override input for model %d: %w", record.ModelConfigID, err)
	}
	if record.Override.ThinkingLevelMap, err = decodeThinkingLevelMapColumn(overrideThinking); err != nil {
		return record, fmt.Errorf("decode override thinking_level_map for model %d: %w", record.ModelConfigID, err)
	}
	if record.Override.Compat, err = decodeCompatColumn(overrideCompat); err != nil {
		return record, fmt.Errorf("decode override compat for model %d: %w", record.ModelConfigID, err)
	}
	return record, nil
}

func decodeStringSliceColumn(raw any) ([]string, error) {
	text, ok, err := jsonColumnText(raw)
	if !ok || err != nil {
		return nil, err
	}
	var values []string
	if err := json.Unmarshal([]byte(text), &values); err != nil {
		return nil, err
	}
	return values, nil
}

func decodeThinkingLevelMapColumn(raw any) (map[string]*string, error) {
	text, ok, err := jsonColumnText(raw)
	if !ok || err != nil {
		return nil, err
	}
	var values map[string]*string
	if err := json.Unmarshal([]byte(text), &values); err != nil {
		return nil, err
	}
	return values, nil
}

func decodeCompatColumn(raw any) (map[string]any, error) {
	text, ok, err := jsonColumnText(raw)
	if !ok || err != nil {
		return nil, err
	}
	return decodePiCompatJSON([]byte(text))
}

func jsonColumnText(raw any) (string, bool, error) {
	if raw == nil {
		return "", false, nil
	}
	switch value := raw.(type) {
	case string:
		return value, true, nil
	case []byte:
		return string(value), true, nil
	default:
		return "", false, fmt.Errorf("jsonb column must be json text")
	}
}

func encodeJSONColumn(value any) (any, error) {
	if value == nil {
		return nil, nil
	}
	switch typed := value.(type) {
	case []string:
		if typed == nil {
			return nil, nil
		}
	case map[string]*string:
		if typed == nil {
			return nil, nil
		}
	case map[string]any:
		if typed == nil {
			return nil, nil
		}
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return string(encoded), nil
}

func exportModelConfigIDs(rows []exportModelRow) []int {
	ids := make([]int, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	return ids
}

func loadPiBinding(ctx context.Context, exec queryExecutor, profileID int, modelConfigID int) (piBindingRecord, bool, error) {
	row := exec.QueryRow(ctx,
		`SELECT `+piBindingSelectColumns+` FROM model_pi_catalog_bindings AS bindings
		 JOIN model_configs AS configs ON configs.id = bindings.model_config_id
		 WHERE bindings.model_config_id = $1 AND configs.profile_id = $2`,
		modelConfigID, profileID)
	record, err := scanPiBindingRow(row)
	if err == pgx.ErrNoRows {
		return piBindingRecord{}, false, nil
	}
	if err != nil {
		return piBindingRecord{}, false, fmt.Errorf("load pi binding for model %d: %w", modelConfigID, err)
	}
	return record, true, nil
}

func loadPiBindingForUpdate(ctx context.Context, tx pgx.Tx, profileID int, modelConfigID int) (piBindingRecord, bool, error) {
	row := tx.QueryRow(ctx,
		`SELECT `+piBindingSelectColumns+` FROM model_pi_catalog_bindings AS bindings
		 JOIN model_configs AS configs ON configs.id = bindings.model_config_id
		 WHERE bindings.model_config_id = $1 AND configs.profile_id = $2
		 FOR UPDATE OF bindings`,
		modelConfigID, profileID)
	record, err := scanPiBindingRow(row)
	if err == pgx.ErrNoRows {
		return piBindingRecord{}, false, nil
	}
	if err != nil {
		return piBindingRecord{}, false, fmt.Errorf("lock pi binding for model %d: %w", modelConfigID, err)
	}
	return record, true, nil
}

func nextPiBindingUpdatedAt(previous, proposed time.Time) time.Time {
	// pgx encodes PostgreSQL timestamps at microsecond precision. Compare the
	// exact values that can actually be stored, otherwise a later nanosecond in
	// the same microsecond can pass After and then collapse back to the previous
	// optimistic-lock token on write.
	proposed = proposed.UTC().Truncate(time.Microsecond)
	previous = previous.UTC().Truncate(time.Microsecond)
	if previous.IsZero() || proposed.After(previous) {
		return proposed
	}
	// PostgreSQL timestamps are microsecond-precision. Advance by one full
	// microsecond so a write always changes the optimistic-lock token even
	// under a fixed test clock or two writes in one clock tick.
	return previous.Add(time.Microsecond)
}

// loadPiBindingsForModels loads every persisted Pi binding for a set of
// profile-scoped model_config_ids in one query. Missing rows simply do not
// appear in the returned map: callers treat an absent key as unbound.
func loadPiBindingsForModels(ctx context.Context, exec queryExecutor, profileID int, modelConfigIDs []int) (map[int]piBindingRecord, error) {
	bindings := map[int]piBindingRecord{}
	if len(modelConfigIDs) == 0 {
		return bindings, nil
	}
	rows, err := exec.Query(ctx,
		`SELECT `+piBindingSelectColumns+` FROM model_pi_catalog_bindings AS bindings
		 JOIN model_configs AS configs ON configs.id = bindings.model_config_id
		 WHERE bindings.model_config_id = ANY($1) AND configs.profile_id = $2`,
		int32ArrayArg(modelConfigIDs), profileID)
	if err != nil {
		return nil, fmt.Errorf("load pi bindings: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		record, scanErr := scanPiBindingRow(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan pi binding row: %w", scanErr)
		}
		bindings[record.ModelConfigID] = record
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("load pi bindings: %w", err)
	}
	return bindings, nil
}

// upsertPiBinding writes a full binding row exactly as given: callers are
// responsible for carrying forward the overrides and bind_source they intend
// to keep. Same-coordinate binds return before this function, different-
// coordinate binds clear overrides, refreshes carry them forward, and
// override writes replace only the operator-authored projection.
func upsertPiBinding(ctx context.Context, tx pgx.Tx, record piBindingRecord, currentTime time.Time) error {
	sourceInput, err := encodeJSONColumn(record.Source.Input)
	if err != nil {
		return fmt.Errorf("encode source input for model %d: %w", record.ModelConfigID, err)
	}
	sourceThinking, err := encodeJSONColumn(record.Source.ThinkingLevelMap)
	if err != nil {
		return fmt.Errorf("encode source thinking_level_map for model %d: %w", record.ModelConfigID, err)
	}
	sourceCompat, err := encodeJSONColumn(record.Source.Compat)
	if err != nil {
		return fmt.Errorf("encode source compat for model %d: %w", record.ModelConfigID, err)
	}
	droppedFields := normalizePiDroppedFields(record.DroppedFields)
	if droppedFields == nil {
		droppedFields = []string{}
	}
	sourceDroppedFields, err := encodeJSONColumn(droppedFields)
	if err != nil {
		return fmt.Errorf("encode source dropped_fields for model %d: %w", record.ModelConfigID, err)
	}
	overrideInput, err := encodeJSONColumn(record.Override.Input)
	if err != nil {
		return fmt.Errorf("encode override input for model %d: %w", record.ModelConfigID, err)
	}
	overrideThinking, err := encodeJSONColumn(record.Override.ThinkingLevelMap)
	if err != nil {
		return fmt.Errorf("encode override thinking_level_map for model %d: %w", record.ModelConfigID, err)
	}
	overrideCompat, err := encodeJSONColumn(record.Override.Compat)
	if err != nil {
		return fmt.Errorf("encode override compat for model %d: %w", record.ModelConfigID, err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO model_pi_catalog_bindings (
			model_config_id, provider_id, catalog_model_id, api, prism_model_id_at_bind, bind_source, catalog_revision, fetched_at,
			source_name, source_reasoning, source_input, source_context_window, source_max_tokens, source_thinking_level_map, source_compat, source_dropped_fields,
			override_name, override_reasoning, override_input, override_context_window, override_max_tokens, override_thinking_level_map, override_compat,
			updated_at
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,
			$9,$10,$11::jsonb,$12,$13,$14::jsonb,$15::jsonb,$16::jsonb,
			$17,$18,$19::jsonb,$20,$21,$22::jsonb,$23::jsonb,
			$24
		)
		ON CONFLICT (model_config_id) DO UPDATE SET
			provider_id = EXCLUDED.provider_id,
			catalog_model_id = EXCLUDED.catalog_model_id,
			api = EXCLUDED.api,
			prism_model_id_at_bind = EXCLUDED.prism_model_id_at_bind,
			bind_source = EXCLUDED.bind_source,
			catalog_revision = EXCLUDED.catalog_revision,
			fetched_at = EXCLUDED.fetched_at,
			source_name = EXCLUDED.source_name,
			source_reasoning = EXCLUDED.source_reasoning,
			source_input = EXCLUDED.source_input,
			source_context_window = EXCLUDED.source_context_window,
			source_max_tokens = EXCLUDED.source_max_tokens,
			source_thinking_level_map = EXCLUDED.source_thinking_level_map,
			source_compat = EXCLUDED.source_compat,
			source_dropped_fields = EXCLUDED.source_dropped_fields,
			override_name = EXCLUDED.override_name,
			override_reasoning = EXCLUDED.override_reasoning,
			override_input = EXCLUDED.override_input,
			override_context_window = EXCLUDED.override_context_window,
			override_max_tokens = EXCLUDED.override_max_tokens,
			override_thinking_level_map = EXCLUDED.override_thinking_level_map,
			override_compat = EXCLUDED.override_compat,
			updated_at = EXCLUDED.updated_at`,
		record.ModelConfigID, record.ProviderID, record.CatalogModelID, record.API, record.PrismModelIDAtBind, record.BindSource, record.CatalogRevision, record.FetchedAt,
		record.Source.Name, record.Source.Reasoning, sourceInput, record.Source.ContextWindow, record.Source.MaxTokens, sourceThinking, sourceCompat, sourceDroppedFields,
		record.Override.Name, record.Override.Reasoning, overrideInput, record.Override.ContextWindow, record.Override.MaxTokens, overrideThinking, overrideCompat,
		currentTime,
	)
	if err != nil {
		return fmt.Errorf("upsert pi binding for model %d: %w", record.ModelConfigID, err)
	}
	return nil
}

func deletePiBinding(ctx context.Context, tx pgx.Tx, modelConfigID int) error {
	if _, err := tx.Exec(ctx, `DELETE FROM model_pi_catalog_bindings WHERE model_config_id = $1`, modelConfigID); err != nil {
		return fmt.Errorf("unbind pi binding for model %d: %w", modelConfigID, err)
	}
	return nil
}
