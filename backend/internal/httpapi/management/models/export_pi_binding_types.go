package models

import (
	"sort"
	"time"
)

// Pi catalog binding match sources persisted on a binding row.
const (
	piBindSourceSingleCandidate = "single_candidate"
	piBindSourceManual          = "manual"
)

// piBindingMetadata is one metadata projection (source, override, or
// effective) of the seven safe pi.dev leaves. Every field is independently
// nullable so "unknown" and explicit values never collapse, and booleans can
// carry an override of false.
type piBindingMetadata struct {
	Name             *string
	Reasoning        *bool
	Input            []string
	ContextWindow    *int64
	MaxTokens        *int64
	ThinkingLevelMap map[string]*string
	Compat           map[string]any
}

func (m piBindingMetadata) empty() bool {
	return m.Name == nil && m.Reasoning == nil && m.Input == nil &&
		m.ContextWindow == nil && m.MaxTokens == nil &&
		m.ThinkingLevelMap == nil && m.Compat == nil
}

func (m piBindingMetadata) payload() *piBindingMetadataPayload {
	if m.empty() {
		return nil
	}
	return &piBindingMetadataPayload{
		Name: m.Name, Reasoning: m.Reasoning, Input: cloneStringSlice(m.Input),
		ContextWindow: copyInt64Ptr(m.ContextWindow), MaxTokens: copyInt64Ptr(m.MaxTokens),
		ThinkingLevelMap: cloneThinkingLevelMap(m.ThinkingLevelMap), Compat: cloneCompat(m.Compat),
	}
}

// effective merges the operator's per-field overrides over the source
// snapshot. Source fields never leak into runtime identity; the merge result
// is the rendered Pi metadata contribution only.
func (m piBindingMetadata) effective(over piBindingMetadata) piBindingMetadata {
	pickInt := func(source, override *int64) *int64 {
		if override != nil {
			return override
		}
		return source
	}
	result := piBindingMetadata{
		Name:          m.Name,
		Reasoning:     m.Reasoning,
		Input:         m.Input,
		ContextWindow: pickInt(m.ContextWindow, over.ContextWindow),
		MaxTokens:     pickInt(m.MaxTokens, over.MaxTokens),
	}
	if over.Name != nil {
		result.Name = over.Name
	}
	if over.Reasoning != nil {
		result.Reasoning = over.Reasoning
	}
	if over.Input != nil {
		result.Input = over.Input
	}
	if over.ThinkingLevelMap != nil {
		result.ThinkingLevelMap = over.ThinkingLevelMap
	} else {
		result.ThinkingLevelMap = m.ThinkingLevelMap
	}
	if over.Compat != nil {
		result.Compat = over.Compat
	} else {
		result.Compat = m.Compat
	}
	return result
}

func cloneThinkingLevelMap(values map[string]*string) map[string]*string {
	if values == nil {
		return nil
	}
	cloned := make(map[string]*string, len(values))
	for key, value := range values {
		cloned[key] = copyStringPtr(value)
	}
	return cloned
}

func cloneCompat(values map[string]any) map[string]any {
	if values == nil {
		return nil
	}
	cloned := make(map[string]any, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func copyStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func normalizePiDroppedFields(values []string) []string {
	normalized := cloneStringSlice(values)
	sort.Strings(normalized)
	if len(normalized) < 2 {
		return normalized
	}
	writeIndex := 1
	for _, value := range normalized[1:] {
		if value == normalized[writeIndex-1] {
			continue
		}
		normalized[writeIndex] = value
		writeIndex++
	}
	return normalized[:writeIndex]
}

// piBindingMetadataPayload is the wire shape of one metadata projection.
type piBindingMetadataPayload struct {
	Name             *string            `json:"name"`
	Reasoning        *bool              `json:"reasoning"`
	Input            []string           `json:"input"`
	ContextWindow    *int64             `json:"context_window"`
	MaxTokens        *int64             `json:"max_tokens"`
	ThinkingLevelMap map[string]*string `json:"thinking_level_map"`
	Compat           map[string]any     `json:"compat"`
}

// piBindingRecord is the full persisted state of one model's Pi catalog
// binding. It never enters runtime planning: it is a management-only
// projection consumed by the export source/render surface.
type piBindingRecord struct {
	ModelConfigID   int
	ProviderID      string
	CatalogModelID  string
	API             string
	BindSource      string
	CatalogRevision string
	FetchedAt       time.Time
	UpdatedAt       time.Time
	Source          piBindingMetadata
	Override        piBindingMetadata
	DroppedFields   []string
}

func (r piBindingRecord) bound() bool { return r.ModelConfigID != 0 && r.ProviderID != "" }

func (r piBindingRecord) response() piBindingResponse {
	if !r.bound() {
		return piBindingResponse{Bound: false}
	}
	fetchedAt := r.FetchedAt
	updatedAt := r.UpdatedAt
	return piBindingResponse{
		Bound:           true,
		BindSource:      r.BindSource,
		ProviderID:      r.ProviderID,
		CatalogModelID:  r.CatalogModelID,
		API:             r.API,
		CatalogRevision: r.CatalogRevision,
		FetchedAt:       &fetchedAt,
		UpdatedAt:       &updatedAt,
		Source:          r.Source.payload(),
		Override:        r.Override.payload(),
		Effective:       r.Source.effective(r.Override).payload(),
		DroppedFields:   normalizePiDroppedFields(r.DroppedFields),
	}
}

type piBindingResponse struct {
	Bound           bool                      `json:"bound"`
	BindSource      string                    `json:"bind_source,omitempty"`
	ProviderID      string                    `json:"provider_id,omitempty"`
	CatalogModelID  string                    `json:"catalog_model_id,omitempty"`
	API             string                    `json:"api,omitempty"`
	CatalogRevision string                    `json:"catalog_revision,omitempty"`
	FetchedAt       *time.Time                `json:"fetched_at,omitempty"`
	UpdatedAt       *time.Time                `json:"updated_at,omitempty"`
	Source          *piBindingMetadataPayload `json:"source"`
	Override        *piBindingMetadataPayload `json:"override"`
	Effective       *piBindingMetadataPayload `json:"effective"`
	DroppedFields   []string                  `json:"dropped_fields,omitempty"`
}

// piBindingFieldChange is one source-value diff row of a refresh preview.
type piBindingFieldChange struct {
	Field   string  `json:"field"`
	Current *string `json:"current"`
	Next    *string `json:"next"`
	Kind    string  `json:"kind"` // added | removed | changed
}

type piRefreshPreviewResponse struct {
	Bound            bool                   `json:"bound"`
	ProviderID       string                 `json:"provider_id,omitempty"`
	CatalogModelID   string                 `json:"catalog_model_id,omitempty"`
	API              string                 `json:"api,omitempty"`
	Changed          bool                   `json:"changed"`
	Changes          []piBindingFieldChange `json:"changes"`
	CatalogRevision  string                 `json:"catalog_revision"`
	FetchedAt        time.Time              `json:"fetched_at"`
	BindingUpdatedAt time.Time              `json:"binding_updated_at"`
}

type piRefreshCommitRequest struct {
	ExpectedProviderID       string    `json:"expected_provider_id"`
	ExpectedCatalogModelID   string    `json:"expected_catalog_model_id"`
	ExpectedAPI              string    `json:"expected_api"`
	ExpectedBindingUpdatedAt time.Time `json:"expected_binding_updated_at"`
	ExpectedCatalogRevision  string    `json:"expected_catalog_revision"`
}

type piBindRequest struct {
	ProviderID              string `json:"provider_id,omitempty"`
	CatalogModelID          string `json:"catalog_model_id,omitempty"`
	ExpectedCatalogRevision string `json:"expected_catalog_revision"`
}
