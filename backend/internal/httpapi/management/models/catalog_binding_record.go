package models

import (
	"time"

	"github.com/coachpo/prism/backend/internal/domain/modelsdev"
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

// catalogBindingRecord is the full persisted state of one model's models.dev
// catalog binding. It never enters runtime planning: it is a management-only
// projection consumed by the catalog metadata surfaces.
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

func (r catalogBindingRecord) bound() bool { return r.ModelConfigID != 0 && r.ProviderID != "" }

func (r catalogBindingRecord) response() *modelCatalogResponse {
	if !r.bound() {
		return &modelCatalogResponse{Bound: false}
	}
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
