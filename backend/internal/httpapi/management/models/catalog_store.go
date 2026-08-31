package models

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Every column is table-qualified: the read joins model_configs for profile
// scoping, and both sides carry created_at/updated_at.
const catalogBindingSelectColumns = `bindings.model_config_id, bindings.provider_id, bindings.catalog_model_id, bindings.match_source, bindings.catalog_revision, bindings.fetched_at, bindings.updated_at,
	bindings.source_name, bindings.source_description, bindings.source_family, bindings.source_release_date, bindings.source_last_updated, bindings.source_knowledge,
	bindings.source_attachment, bindings.source_reasoning, bindings.source_tool_call, bindings.source_structured_output, bindings.source_temperature,
	bindings.source_modalities_input::text, bindings.source_modalities_output::text, bindings.source_limit_context, bindings.source_limit_input, bindings.source_limit_output,
	bindings.source_open_weights, bindings.source_status,
	bindings.override_name, bindings.override_description, bindings.override_family, bindings.override_release_date, bindings.override_last_updated, bindings.override_knowledge,
	bindings.override_attachment, bindings.override_reasoning, bindings.override_tool_call, bindings.override_structured_output, bindings.override_temperature,
	bindings.override_modalities_input::text, bindings.override_modalities_output::text, bindings.override_limit_context, bindings.override_limit_input, bindings.override_limit_output,
	bindings.override_open_weights, bindings.override_status`

func scanCatalogBindingRow(scanner interface{ Scan(...any) error }) (catalogBindingRecord, error) {
	record := catalogBindingRecord{}
	var modalitiesInput, modalitiesOutput any
	var overrideModalitiesInput, overrideModalitiesOutput any
	err := scanner.Scan(
		&record.ModelConfigID, &record.ProviderID, &record.CatalogModelID, &record.MatchSource, &record.CatalogRevision, &record.FetchedAt, &record.UpdatedAt,
		&record.Source.Name, &record.Source.Description, &record.Source.Family, &record.Source.ReleaseDate, &record.Source.LastUpdated, &record.Source.Knowledge,
		&record.Source.Attachment, &record.Source.Reasoning, &record.Source.ToolCall, &record.Source.StructuredOutput, &record.Source.Temperature,
		&modalitiesInput, &modalitiesOutput, &record.Source.LimitContext, &record.Source.LimitInput, &record.Source.LimitOutput,
		&record.Source.OpenWeights, &record.Source.Status,
		&record.Override.Name, &record.Override.Description, &record.Override.Family, &record.Override.ReleaseDate, &record.Override.LastUpdated, &record.Override.Knowledge,
		&record.Override.Attachment, &record.Override.Reasoning, &record.Override.ToolCall, &record.Override.StructuredOutput, &record.Override.Temperature,
		&overrideModalitiesInput, &overrideModalitiesOutput, &record.Override.LimitContext, &record.Override.LimitInput, &record.Override.LimitOutput,
		&record.Override.OpenWeights, &record.Override.Status,
	)
	if err != nil {
		return record, err
	}
	if record.Source.ModalitiesInput, err = decodeModalityColumn(modalitiesInput); err != nil {
		return record, fmt.Errorf("decode source modalities_input for model %d: %w", record.ModelConfigID, err)
	}
	if record.Source.ModalitiesOutput, err = decodeModalityColumn(modalitiesOutput); err != nil {
		return record, fmt.Errorf("decode source modalities_output for model %d: %w", record.ModelConfigID, err)
	}
	if record.Override.ModalitiesInput, err = decodeModalityColumn(overrideModalitiesInput); err != nil {
		return record, fmt.Errorf("decode override modalities_input for model %d: %w", record.ModelConfigID, err)
	}
	if record.Override.ModalitiesOutput, err = decodeModalityColumn(overrideModalitiesOutput); err != nil {
		return record, fmt.Errorf("decode override modalities_output for model %d: %w", record.ModelConfigID, err)
	}
	return record, nil
}

func decodeModalityColumn(raw any) ([]string, error) {
	if raw == nil {
		return nil, nil
	}
	var text string
	switch value := raw.(type) {
	case string:
		text = value
	case []byte:
		text = string(value)
	default:
		return nil, fmt.Errorf("modalities column must be json text")
	}
	var values []string
	if err := json.Unmarshal([]byte(text), &values); err != nil {
		return nil, err
	}
	return values, nil
}

func encodeModalityColumn(values []string) any {
	if values == nil {
		return nil
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return nil
	}
	return string(encoded)
}

func loadCatalogBinding(ctx context.Context, exec queryExecutor, profileID int, modelConfigID int) (catalogBindingRecord, bool, error) {
	row := exec.QueryRow(ctx,
		`SELECT `+catalogBindingSelectColumns+` FROM model_catalog_bindings AS bindings
		 JOIN model_configs AS configs ON configs.id = bindings.model_config_id
		 WHERE bindings.model_config_id = $1 AND configs.profile_id = $2`,
		modelConfigID, profileID)
	record, err := scanCatalogBindingRow(row)
	if err == pgx.ErrNoRows {
		return catalogBindingRecord{}, false, nil
	}
	if err != nil {
		return catalogBindingRecord{}, false, fmt.Errorf("load catalog binding for model %d: %w", modelConfigID, err)
	}
	return record, true, nil
}

// loadCatalogBindingForUpdate reads one binding with SELECT ... FOR UPDATE OF
// bindings so a concurrent bind/refresh/override serializes behind the lock.
// Callers must already hold the owning model row lock (model first, then
// binding, everywhere) so the lock ordering can never deadlock.
func loadCatalogBindingForUpdate(ctx context.Context, tx pgx.Tx, profileID int, modelConfigID int) (catalogBindingRecord, bool, error) {
	row := tx.QueryRow(ctx,
		`SELECT `+catalogBindingSelectColumns+` FROM model_catalog_bindings AS bindings
		 JOIN model_configs AS configs ON configs.id = bindings.model_config_id
		 WHERE bindings.model_config_id = $1 AND configs.profile_id = $2
			 FOR UPDATE OF bindings`,
		modelConfigID, profileID)
	record, err := scanCatalogBindingRow(row)
	if err == pgx.ErrNoRows {
		return catalogBindingRecord{}, false, nil
	}
	if err != nil {
		return catalogBindingRecord{}, false, fmt.Errorf("lock catalog binding for model %d: %w", modelConfigID, err)
	}
	return record, true, nil
}

// bindCatalogBinding writes the full binding row exactly as given. Only the
// bind flow uses it, and only while holding both the model row lock and the
// binding row lock: the same-offering override/match-source carry-forward is
// read from the locked current row, never from a pre-transaction snapshot.
func bindCatalogBinding(ctx context.Context, tx pgx.Tx, record catalogBindingRecord, updatedAt time.Time) error {
	override := record.Override
	matchSource := record.MatchSource
	_, err := tx.Exec(ctx, `
		INSERT INTO model_catalog_bindings (
			model_config_id, provider_id, catalog_model_id, match_source, catalog_revision, fetched_at,
			source_name, source_description, source_family, source_release_date, source_last_updated, source_knowledge,
			source_attachment, source_reasoning, source_tool_call, source_structured_output, source_temperature,
			source_modalities_input, source_modalities_output, source_limit_context, source_limit_input, source_limit_output,
			source_open_weights, source_status,
			override_name, override_description, override_family, override_release_date, override_last_updated, override_knowledge,
			override_attachment, override_reasoning, override_tool_call, override_structured_output, override_temperature,
			override_modalities_input, override_modalities_output, override_limit_context, override_limit_input, override_limit_output,
			override_open_weights, override_status,
			updated_at
		) VALUES (
			$1,$2,$3,$4,$5,$6,
			$7,$8,$9,$10,$11,$12,
			$13,$14,$15,$16,$17,
			$18::jsonb,$19::jsonb,$20,$21,$22,
			$23,$24,
			$25,$26,$27,$28,$29,$30,
			$31,$32,$33,$34,$35,
			$36::jsonb,$37::jsonb,$38,$39,$40,
			$41,$42,
			$43
		)
		ON CONFLICT (model_config_id) DO UPDATE SET
			provider_id = EXCLUDED.provider_id,
			catalog_model_id = EXCLUDED.catalog_model_id,
			match_source = $44,
			catalog_revision = EXCLUDED.catalog_revision,
			fetched_at = EXCLUDED.fetched_at,
			source_name = EXCLUDED.source_name,
			source_description = EXCLUDED.source_description,
			source_family = EXCLUDED.source_family,
			source_release_date = EXCLUDED.source_release_date,
			source_last_updated = EXCLUDED.source_last_updated,
			source_knowledge = EXCLUDED.source_knowledge,
			source_attachment = EXCLUDED.source_attachment,
			source_reasoning = EXCLUDED.source_reasoning,
			source_tool_call = EXCLUDED.source_tool_call,
			source_structured_output = EXCLUDED.source_structured_output,
			source_temperature = EXCLUDED.source_temperature,
			source_modalities_input = EXCLUDED.source_modalities_input,
			source_modalities_output = EXCLUDED.source_modalities_output,
			source_limit_context = EXCLUDED.source_limit_context,
			source_limit_input = EXCLUDED.source_limit_input,
			source_limit_output = EXCLUDED.source_limit_output,
			source_open_weights = EXCLUDED.source_open_weights,
			source_status = EXCLUDED.source_status,
			override_name = $45,
			override_description = $46,
			override_family = $47,
			override_release_date = $48,
			override_last_updated = $49,
			override_knowledge = $50,
			override_attachment = $51,
			override_reasoning = $52,
			override_tool_call = $53,
			override_structured_output = $54,
			override_temperature = $55,
			override_modalities_input = $56,
			override_modalities_output = $57,
			override_limit_context = $58,
			override_limit_input = $59,
			override_limit_output = $60,
			override_open_weights = $61,
			override_status = $62,
			updated_at = $63`,
		record.ModelConfigID, record.ProviderID, record.CatalogModelID, record.MatchSource, record.CatalogRevision, record.FetchedAt,
		record.Source.Name, record.Source.Description, record.Source.Family, record.Source.ReleaseDate, record.Source.LastUpdated, record.Source.Knowledge,
		record.Source.Attachment, record.Source.Reasoning, record.Source.ToolCall, record.Source.StructuredOutput, record.Source.Temperature,
		encodeModalityColumn(record.Source.ModalitiesInput), encodeModalityColumn(record.Source.ModalitiesOutput), record.Source.LimitContext, record.Source.LimitInput, record.Source.LimitOutput,
		record.Source.OpenWeights, record.Source.Status,
		override.Name, override.Description, override.Family, override.ReleaseDate, override.LastUpdated, override.Knowledge,
		override.Attachment, override.Reasoning, override.ToolCall, override.StructuredOutput, override.Temperature,
		encodeModalityColumn(override.ModalitiesInput), encodeModalityColumn(override.ModalitiesOutput), override.LimitContext, override.LimitInput, override.LimitOutput,
		override.OpenWeights, override.Status,
		updatedAt,
		matchSource,
		override.Name, override.Description, override.Family, override.ReleaseDate, override.LastUpdated, override.Knowledge,
		override.Attachment, override.Reasoning, override.ToolCall, override.StructuredOutput, override.Temperature,
		encodeModalityColumn(override.ModalitiesInput), encodeModalityColumn(override.ModalitiesOutput), override.LimitContext, override.LimitInput, override.LimitOutput,
		override.OpenWeights, override.Status,
		updatedAt,
	)
	if err != nil {
		return fmt.Errorf("bind catalog binding for model %d: %w", record.ModelConfigID, err)
	}
	return nil
}

// updateCatalogBindingSource rewrites only the catalog-sourced columns: the
// five source_* projection groups, the revision, and the fetch stamp. Override
// columns are never touched, so a refresh can no longer restore an operator's
// override from a pre-transaction snapshot.
func updateCatalogBindingSource(ctx context.Context, tx pgx.Tx, modelConfigID int, source modelCatalogMetadata, revision string, fetchedAt, updatedAt time.Time) error {
	_, err := tx.Exec(ctx, `
		UPDATE model_catalog_bindings SET
			source_name = $2, source_description = $3, source_family = $4, source_release_date = $5, source_last_updated = $6, source_knowledge = $7,
			source_attachment = $8, source_reasoning = $9, source_tool_call = $10, source_structured_output = $11, source_temperature = $12,
			source_modalities_input = $13::jsonb, source_modalities_output = $14::jsonb, source_limit_context = $15, source_limit_input = $16, source_limit_output = $17,
			source_open_weights = $18, source_status = $19,
			catalog_revision = $20, fetched_at = $21, updated_at = $22
		WHERE model_config_id = $1`,
		modelConfigID,
		source.Name, source.Description, source.Family, source.ReleaseDate, source.LastUpdated, source.Knowledge,
		source.Attachment, source.Reasoning, source.ToolCall, source.StructuredOutput, source.Temperature,
		encodeModalityColumn(source.ModalitiesInput), encodeModalityColumn(source.ModalitiesOutput), source.LimitContext, source.LimitInput, source.LimitOutput,
		source.OpenWeights, source.Status,
		revision, fetchedAt, updatedAt,
	)
	if err != nil {
		return fmt.Errorf("refresh catalog binding source for model %d: %w", modelConfigID, err)
	}
	return nil
}

// updateCatalogBindingOverride rewrites only the override_* columns plus the
// CAS token. The operator's edit merges over the locked current row, so two
// concurrent sparse overrides of different fields both survive.
func updateCatalogBindingOverride(ctx context.Context, tx pgx.Tx, modelConfigID int, override modelCatalogMetadata, updatedAt time.Time) error {
	_, err := tx.Exec(ctx, `
		UPDATE model_catalog_bindings SET
			override_name = $2, override_description = $3, override_family = $4, override_release_date = $5, override_last_updated = $6, override_knowledge = $7,
			override_attachment = $8, override_reasoning = $9, override_tool_call = $10, override_structured_output = $11, override_temperature = $12,
			override_modalities_input = $13::jsonb, override_modalities_output = $14::jsonb, override_limit_context = $15, override_limit_input = $16, override_limit_output = $17,
			override_open_weights = $18, override_status = $19,
			updated_at = $20
		WHERE model_config_id = $1`,
		modelConfigID,
		override.Name, override.Description, override.Family, override.ReleaseDate, override.LastUpdated, override.Knowledge,
		override.Attachment, override.Reasoning, override.ToolCall, override.StructuredOutput, override.Temperature,
		encodeModalityColumn(override.ModalitiesInput), encodeModalityColumn(override.ModalitiesOutput), override.LimitContext, override.LimitInput, override.LimitOutput,
		override.OpenWeights, override.Status,
		updatedAt,
	)
	if err != nil {
		return fmt.Errorf("write catalog binding override for model %d: %w", modelConfigID, err)
	}
	return nil
}

func deleteCatalogBinding(ctx context.Context, tx pgx.Tx, modelConfigID int) error {
	_, err := tx.Exec(ctx, `DELETE FROM model_catalog_bindings WHERE model_config_id = $1`, modelConfigID)
	if err != nil {
		return fmt.Errorf("unbind catalog binding for model %d: %w", modelConfigID, err)
	}
	return nil
}

// nextCatalogBindingUpdatedAt is the models.dev binding CAS token helper.
// pgx encodes PostgreSQL timestamps at microsecond precision; comparing the
// exact storable values keeps a later nanosecond in the same microsecond from
// collapsing back onto the previous token, and the +1µs floor makes every
// write advance the token even under a fixed test clock or two writes in one
// clock tick. This helper is independent of the Pi binding's own token helper;
// the two sources must not share identity or naming.
func nextCatalogBindingUpdatedAt(previous, proposed time.Time) time.Time {
	proposed = proposed.UTC().Truncate(time.Microsecond)
	previous = previous.UTC().Truncate(time.Microsecond)
	if previous.IsZero() || proposed.After(previous) {
		return proposed
	}
	return previous.Add(time.Microsecond)
}
