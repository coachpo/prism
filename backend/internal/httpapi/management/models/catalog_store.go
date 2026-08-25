package models

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/coachpo/prism/backend/internal/domain/modelsdev"
	"github.com/jackc/pgx/v5"
)

// modelCatalogMetadata is the storage form of one metadata projection. Every
// field is independently nullable so "unknown" and explicit values never
// collapse, and booleans can carry an override of false.
type modelCatalogMetadata struct {
	Name             *string
	Description      *string
	Family           *string
	ReleaseDate      *string
	LastUpdated      *string
	Knowledge        *string
	Attachment       *bool
	Reasoning        *bool
	ToolCall         *bool
	StructuredOutput *bool
	Temperature      *bool
	ModalitiesInput  []string
	ModalitiesOutput []string
	LimitContext     *int64
	LimitInput       *int64
	LimitOutput      *int64
	OpenWeights      *bool
	Status           *string
}

func (m modelCatalogMetadata) payload() *modelCatalogMetadataPayload {
	if m.empty() {
		return nil
	}
	return &modelCatalogMetadataPayload{
		Name: m.Name, Description: m.Description, Family: m.Family,
		ReleaseDate: m.ReleaseDate, LastUpdated: m.LastUpdated, Knowledge: m.Knowledge,
		Attachment: m.Attachment, Reasoning: m.Reasoning, ToolCall: m.ToolCall,
		StructuredOutput: m.StructuredOutput, Temperature: m.Temperature,
		ModalitiesInput: cloneStringSlice(m.ModalitiesInput), ModalitiesOutput: cloneStringSlice(m.ModalitiesOutput),
		LimitContext: copyInt64Ptr(m.LimitContext), LimitInput: copyInt64Ptr(m.LimitInput), LimitOutput: copyInt64Ptr(m.LimitOutput),
		OpenWeights: m.OpenWeights, Status: m.Status,
	}
}

func (m modelCatalogMetadata) empty() bool {
	return m.Name == nil && m.Description == nil && m.Family == nil &&
		m.ReleaseDate == nil && m.LastUpdated == nil && m.Knowledge == nil &&
		m.Attachment == nil && m.Reasoning == nil && m.ToolCall == nil &&
		m.StructuredOutput == nil && m.Temperature == nil &&
		m.ModalitiesInput == nil && m.ModalitiesOutput == nil &&
		m.LimitContext == nil && m.LimitInput == nil && m.LimitOutput == nil &&
		m.OpenWeights == nil && m.Status == nil
}

// effective merges the operator's per-field overrides over the source
// snapshot. Source fields never leak into display_name; the merge result is
// presentation metadata only.
func (m modelCatalogMetadata) effective(over modelCatalogMetadata) modelCatalogMetadata {
	pick := func(source, override *string) *string {
		if override != nil {
			return override
		}
		return source
	}
	pickBool := func(source, override *bool) *bool {
		if override != nil {
			return override
		}
		return source
	}
	pickInt := func(source, override *int64) *int64 {
		if override != nil {
			return override
		}
		return source
	}
	pickList := func(source, override []string) []string {
		if override != nil {
			return override
		}
		return source
	}
	return modelCatalogMetadata{
		Name: pick(m.Name, over.Name), Description: pick(m.Description, over.Description), Family: pick(m.Family, over.Family),
		ReleaseDate: pick(m.ReleaseDate, over.ReleaseDate), LastUpdated: pick(m.LastUpdated, over.LastUpdated),
		Knowledge:  pick(m.Knowledge, over.Knowledge),
		Attachment: pickBool(m.Attachment, over.Attachment), Reasoning: pickBool(m.Reasoning, over.Reasoning),
		ToolCall: pickBool(m.ToolCall, over.ToolCall), StructuredOutput: pickBool(m.StructuredOutput, over.StructuredOutput),
		Temperature:     pickBool(m.Temperature, over.Temperature),
		ModalitiesInput: pickList(m.ModalitiesInput, over.ModalitiesInput), ModalitiesOutput: pickList(m.ModalitiesOutput, over.ModalitiesOutput),
		LimitContext: pickInt(m.LimitContext, over.LimitContext), LimitInput: pickInt(m.LimitInput, over.LimitInput), LimitOutput: pickInt(m.LimitOutput, over.LimitOutput),
		OpenWeights: pickBool(m.OpenWeights, over.OpenWeights), Status: pick(m.Status, over.Status),
	}
}

type catalogBindingRecord struct {
	ModelConfigID   int
	ProviderID      string
	CatalogModelID  string
	MatchSource     string
	CatalogRevision string
	FetchedAt       time.Time
	UpdatedAt       time.Time
	Source          modelCatalogMetadata
	Override        modelCatalogMetadata
}

func (r catalogBindingRecord) response() *modelCatalogResponse {
	source := r.Source.payload()
	override := r.Override.payload()
	effective := r.Source.effective(r.Override).payload()
	fetchedAt := r.FetchedAt
	updatedAt := r.UpdatedAt
	return &modelCatalogResponse{
		Bound:           true,
		MatchSource:     r.MatchSource,
		ProviderID:      r.ProviderID,
		CatalogModelID:  r.CatalogModelID,
		CatalogRevision: r.CatalogRevision,
		FetchedAt:       &fetchedAt,
		UpdatedAt:       &updatedAt,
		Source:          source,
		Override:        override,
		Effective:       effective,
	}
}

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

// upsertCatalogBinding writes a full binding row exactly as given: callers
// are responsible for carrying forward the overrides they intend to keep.
// Bind flows reuse an existing row's overrides when the offering is unchanged
// and clear them otherwise; refresh flows never touch them; override writes
// store the operator's per-field edits.
func upsertCatalogBinding(ctx context.Context, tx pgx.Tx, record catalogBindingRecord, currentTime time.Time) error {
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
		currentTime,
		matchSource,
		override.Name, override.Description, override.Family, override.ReleaseDate, override.LastUpdated, override.Knowledge,
		override.Attachment, override.Reasoning, override.ToolCall, override.StructuredOutput, override.Temperature,
		encodeModalityColumn(override.ModalitiesInput), encodeModalityColumn(override.ModalitiesOutput), override.LimitContext, override.LimitInput, override.LimitOutput,
		override.OpenWeights, override.Status,
		currentTime,
	)
	if err != nil {
		return fmt.Errorf("upsert catalog binding for model %d: %w", record.ModelConfigID, err)
	}
	return nil
}

// catalogMetadataFromModel projects a validated catalog entry into the
// storage shape. Values are copied verbatim from the parsed document.
func catalogMetadataFromModel(model *modelsdev.Model) modelCatalogMetadata {
	return modelCatalogMetadata{
		Name: stringPointer(model.Name), Description: cloneStringPointer(model.Description), Family: cloneStringPointer(model.Family),
		ReleaseDate: cloneStringPointer(model.ReleaseDate), LastUpdated: cloneStringPointer(model.LastUpdated), Knowledge: cloneStringPointer(model.Knowledge),
		Attachment: model.Attachment, Reasoning: model.Reasoning, ToolCall: model.ToolCall,
		StructuredOutput: model.StructuredOutput, Temperature: model.Temperature,
		ModalitiesInput: append([]string(nil), model.ModalitiesInput...), ModalitiesOutput: append([]string(nil), model.ModalitiesOutput...),
		LimitContext: model.Limit.Context, LimitInput: model.Limit.Input, LimitOutput: model.Limit.Output,
		OpenWeights: model.OpenWeights, Status: model.Status,
	}
}

func stringPointer(value string) *string { return &value }

func copyInt64Ptr(value *int64) *int64 {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func cloneStringSlice(values []string) []string {
	if values == nil {
		return nil
	}
	return append([]string(nil), values...)
}

// catalogFieldOrder fixes the stable diff order of refresh previews.
var catalogFieldOrder = []struct {
	field   string
	get     func(m modelCatalogMetadata) *string
	set     func(*modelCatalogMetadata, *string)
	boolGet func(m modelCatalogMetadata) *bool
	boolSet func(*modelCatalogMetadata, *bool)
	intGet  func(m modelCatalogMetadata) *int64
	intSet  func(*modelCatalogMetadata, *int64)
	listGet func(m modelCatalogMetadata) []string
	listSet func(*modelCatalogMetadata, []string)
}{
	{field: "name", get: func(m modelCatalogMetadata) *string { return m.Name }, set: func(m *modelCatalogMetadata, v *string) { m.Name = v }},
	{field: "description", get: func(m modelCatalogMetadata) *string { return m.Description }, set: func(m *modelCatalogMetadata, v *string) { m.Description = v }},
	{field: "family", get: func(m modelCatalogMetadata) *string { return m.Family }, set: func(m *modelCatalogMetadata, v *string) { m.Family = v }},
	{field: "release_date", get: func(m modelCatalogMetadata) *string { return m.ReleaseDate }, set: func(m *modelCatalogMetadata, v *string) { m.ReleaseDate = v }},
	{field: "last_updated", get: func(m modelCatalogMetadata) *string { return m.LastUpdated }, set: func(m *modelCatalogMetadata, v *string) { m.LastUpdated = v }},
	{field: "knowledge", get: func(m modelCatalogMetadata) *string { return m.Knowledge }, set: func(m *modelCatalogMetadata, v *string) { m.Knowledge = v }},
	{field: "attachment", boolGet: func(m modelCatalogMetadata) *bool { return m.Attachment }, boolSet: func(m *modelCatalogMetadata, v *bool) { m.Attachment = v }},
	{field: "reasoning", boolGet: func(m modelCatalogMetadata) *bool { return m.Reasoning }, boolSet: func(m *modelCatalogMetadata, v *bool) { m.Reasoning = v }},
	{field: "tool_call", boolGet: func(m modelCatalogMetadata) *bool { return m.ToolCall }, boolSet: func(m *modelCatalogMetadata, v *bool) { m.ToolCall = v }},
	{field: "structured_output", boolGet: func(m modelCatalogMetadata) *bool { return m.StructuredOutput }, boolSet: func(m *modelCatalogMetadata, v *bool) { m.StructuredOutput = v }},
	{field: "temperature", boolGet: func(m modelCatalogMetadata) *bool { return m.Temperature }, boolSet: func(m *modelCatalogMetadata, v *bool) { m.Temperature = v }},
	{field: "modalities_input", listGet: func(m modelCatalogMetadata) []string { return m.ModalitiesInput }, listSet: func(m *modelCatalogMetadata, v []string) { m.ModalitiesInput = v }},
	{field: "modalities_output", listGet: func(m modelCatalogMetadata) []string { return m.ModalitiesOutput }, listSet: func(m *modelCatalogMetadata, v []string) { m.ModalitiesOutput = v }},
	{field: "limit_context", intGet: func(m modelCatalogMetadata) *int64 { return m.LimitContext }, intSet: func(m *modelCatalogMetadata, v *int64) { m.LimitContext = v }},
	{field: "limit_input", intGet: func(m modelCatalogMetadata) *int64 { return m.LimitInput }, intSet: func(m *modelCatalogMetadata, v *int64) { m.LimitInput = v }},
	{field: "limit_output", intGet: func(m modelCatalogMetadata) *int64 { return m.LimitOutput }, intSet: func(m *modelCatalogMetadata, v *int64) { m.LimitOutput = v }},
	{field: "open_weights", boolGet: func(m modelCatalogMetadata) *bool { return m.OpenWeights }, boolSet: func(m *modelCatalogMetadata, v *bool) { m.OpenWeights = v }},
	{field: "status", get: func(m modelCatalogMetadata) *string { return m.Status }, set: func(m *modelCatalogMetadata, v *string) { m.Status = v }},
}

// diffCatalogSource compares two metadata projections field by field in the
// stable order. Values render as their canonical strings so booleans, lists,
// and numbers diff uniformly.
func diffCatalogSource(current, next modelCatalogMetadata) ([]modelCatalogFieldChange, bool) {
	changes := make([]modelCatalogFieldChange, 0)
	renderString := func(value *string) *string { return value }
	renderBool := func(value *bool) *string {
		if value == nil {
			return nil
		}
		rendered := strconvFormatBool(*value)
		return &rendered
	}
	renderInt := func(value *int64) *string {
		if value == nil {
			return nil
		}
		rendered := strconv.FormatInt(*value, 10)
		return &rendered
	}
	renderList := func(value []string) *string {
		if value == nil {
			return nil
		}
		rendered := "[" + strings.Join(value, ",") + "]"
		return &rendered
	}
	for _, descriptor := range catalogFieldOrder {
		var currentValue, nextValue *string
		switch {
		case descriptor.get != nil:
			currentValue, nextValue = renderString(descriptor.get(current)), renderString(descriptor.get(next))
		case descriptor.boolGet != nil:
			currentValue, nextValue = renderBool(descriptor.boolGet(current)), renderBool(descriptor.boolGet(next))
		case descriptor.intGet != nil:
			currentValue, nextValue = renderInt(descriptor.intGet(current)), renderInt(descriptor.intGet(next))
		case descriptor.listGet != nil:
			currentValue, nextValue = renderList(descriptor.listGet(current)), renderList(descriptor.listGet(next))
		}
		switch {
		case currentValue == nil && nextValue == nil:
			continue
		case currentValue == nil:
			changes = append(changes, modelCatalogFieldChange{Field: descriptor.field, Current: nil, Next: nextValue, Kind: "added"})
		case nextValue == nil:
			changes = append(changes, modelCatalogFieldChange{Field: descriptor.field, Current: currentValue, Next: nil, Kind: "removed"})
		case *currentValue != *nextValue:
			changes = append(changes, modelCatalogFieldChange{Field: descriptor.field, Current: currentValue, Next: nextValue, Kind: "changed"})
		}
	}
	return changes, len(changes) > 0
}

func strconvFormatBool(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
