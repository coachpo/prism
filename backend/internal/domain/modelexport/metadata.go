package modelexport

import (
	"encoding/json"
	"sort"

	"github.com/coachpo/prism/backend/internal/domain/pidev"
)

// MetadataSource names the layer that contributed a metadata leaf.
type MetadataSource string

const (
	// SourcePrism marks leaves resolved from first-party Prism metadata.
	SourcePrism MetadataSource = "prism"
	// SourcePiCatalog marks leaves filled from the persisted, API-sanitized
	// pi.dev binding snapshot (including any validated per-field override).
	SourcePiCatalog MetadataSource = "pi_catalog"
)

// Known metadata leaf names are exactly the five ordinary Pi model fields the
// renderer projects. thinkingLevelMap and compat are validated derived fields
// on PiTemplate; OpenCode-only catalog metadata is intentionally absent.
const (
	MetaName            = "name"
	MetaReasoning       = "reasoning"
	MetaContextWindow   = "context_window"
	MetaMaxOutputTokens = "max_output_tokens"
	MetaModalitiesInput = "modalities_input"
)

// KnownMetadataLeaves lists every known metadata leaf in stable order. The
// merge provenance and missing reports use exactly this order.
func KnownMetadataLeaves() []string {
	return []string{
		MetaName,
		MetaReasoning,
		MetaContextWindow,
		MetaMaxOutputTokens,
		MetaModalitiesInput,
	}
}

// MetadataLayer is one presence-preserving metadata snapshot keyed by leaf
// name. Values are canonical JSON; explicit zeros/false/empty strings/arrays
// are present values, and a nil entry means the leaf is absent (unknown).
type MetadataLayer struct {
	values map[string]json.RawMessage
}

// NewMetadataLayer builds a layer from raw leaf values. Nil raw values are
// skipped so absent stays absent.
func NewMetadataLayer(values map[string]json.RawMessage) MetadataLayer {
	copied := make(map[string]json.RawMessage, len(values))
	for key, value := range values {
		if len(value) == 0 || string(value) == "null" {
			continue
		}
		if !json.Valid(value) {
			// Callers build layers from already-validated JSON; an invalid
			// value is a programming error and fails closed here.
			continue
		}
		copied[key] = append(json.RawMessage(nil), value...)
	}
	return MetadataLayer{values: copied}
}

// Get returns the raw value for one leaf and whether it is present.
func (m MetadataLayer) Get(leaf string) (json.RawMessage, bool) {
	if m.values == nil {
		return nil, false
	}
	value, ok := m.values[leaf]
	return value, ok
}

// Leaves returns the present leaf names in sorted order.
func (m MetadataLayer) Leaves() []string {
	leaves := make([]string, 0, len(m.values))
	for leaf := range m.values {
		leaves = append(leaves, leaf)
	}
	sort.Strings(leaves)
	return leaves
}

// Empty reports whether no leaf is present.
func (m MetadataLayer) Empty() bool {
	return len(m.values) == 0
}

// Values returns a defensive, presence-preserving copy suitable for digest
// and wire projections. Explicit empty arrays/strings and false/zero values
// remain present.
func (m MetadataLayer) Values() map[string]json.RawMessage {
	values := make(map[string]json.RawMessage, len(m.values))
	for key, value := range m.values {
		values[key] = append(json.RawMessage(nil), value...)
	}
	return values
}

// MergeResult carries the two-layer merge output plus its audit trail.
type MergeResult struct {
	// Merged holds every present leaf after Prism → persisted Pi fill.
	Merged MetadataLayer
	// Provenance maps each merged leaf to the layer it came from.
	Provenance map[string]MetadataSource
	// Missing lists known metadata leaves absent from every layer, in
	// KnownMetadataLeaves order.
	Missing []string
}

// MergeOptions configures one Pi-only two-layer merge.
type MergeOptions struct {
	// Prism is first-party Prism metadata, currently the operator-owned display
	// name. It wins when present.
	Prism MetadataLayer
	// Pi is the effective API-sanitized metadata frozen in the persisted Pi
	// binding. It fills only leaves Prism left absent.
	Pi MetadataLayer
}

// KeyLooksSensitive reports whether a JSON object key carries credential-like
// material anywhere in its name.
func KeyLooksSensitive(key string) bool {
	return pidev.KeyLooksSensitive(key)
}

// MergeKnownMetadata applies the two-layer leaf merge over the known
// metadata surface (error types live in errors.go):
//
//  1. Prism effective values win by default.
//  2. The persisted Pi binding fills only leaves Prism left absent.
//
// Presence is preserved exactly: explicit false/0/""/[] stay present values
// and never count as missing. Locked fields cannot appear in the metadata
// surface, but the guard is kept here so future callers fail closed too.
func MergeKnownMetadata(options MergeOptions) (MergeResult, error) {
	result := MergeResult{Provenance: map[string]MetadataSource{}}
	merged := map[string]json.RawMessage{}

	for _, leaf := range options.Prism.Leaves() {
		if KeyLooksSensitive(leaf) {
			return MergeResult{}, &ErrSensitiveField{Field: leaf}
		}
		value, _ := options.Prism.Get(leaf)
		merged[leaf] = value
		result.Provenance[leaf] = SourcePrism
	}
	for _, leaf := range options.Pi.Leaves() {
		if err := rejectLockedMetadataLeaf(leaf); err != nil {
			return MergeResult{}, err
		}
		if KeyLooksSensitive(leaf) {
			return MergeResult{}, &ErrSensitiveField{Field: leaf}
		}
		if _, present := merged[leaf]; present {
			continue
		}
		value, _ := options.Pi.Get(leaf)
		merged[leaf] = value
		result.Provenance[leaf] = SourcePiCatalog
	}
	for _, leaf := range KnownMetadataLeaves() {
		if _, present := merged[leaf]; !present {
			result.Missing = append(result.Missing, leaf)
		}
	}
	result.Merged = MetadataLayer{values: merged}
	return result, nil
}

// rejectLockedMetadataLeaf guards the metadata surface against the locked
// identity leaves even though renderers own most locked paths directly.
func rejectLockedMetadataLeaf(leaf string) error {
	switch leaf {
	case "model_id", "model_config_id", "api_family", "base_url", "provider_id":
		return &ErrLockedField{Field: leaf}
	default:
		return nil
	}
}

// MetadataWarningCodes derives stable per-model warnings from the merged
// Prism + persisted Pi metadata evidence.
func MetadataWarningCodes(merged MetadataLayer) []string {
	warnings := []string{}
	if raw, present := merged.Get(MetaModalitiesInput); present {
		var modalities []string
		if json.Unmarshal(raw, &modalities) == nil {
			for _, modality := range modalities {
				if modality != "text" && modality != "image" {
					warnings = append(warnings, WarningUnsupportedInputModality)
					break
				}
			}
		}
	}
	relevant := []string{MetaName, MetaReasoning, MetaContextWindow, MetaMaxOutputTokens, MetaModalitiesInput}
	for _, leaf := range relevant {
		if _, present := merged.Get(leaf); !present {
			warnings = append(warnings, WarningMetadataIncomplete)
			break
		}
	}
	return sortWarningCodes(warnings)
}
