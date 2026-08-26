package modelexport

import (
	"encoding/json"
	"sort"
	"strings"
	"unicode"
)

// MetadataSource names the layer that contributed a metadata leaf.
type MetadataSource string

const (
	// SourcePrism marks leaves resolved from stored Prism binding metadata
	// (source columns after per-field overrides).
	SourcePrism MetadataSource = "prism"
	// SourceModelsDev marks leaves filled from the in-memory models.dev
	// catalog parse.
	SourceModelsDev MetadataSource = "models_dev"
	// SourceManual marks leaves contributed by the operator's manual
	// enhancement payload.
	SourceManual MetadataSource = "manual"
)

// Known metadata leaf names shared by both platforms' enrichment surfaces.
// These are the only manual-overridable known metadata leaves; everything
// outside this set that the manual layer fills is a platform extension field.
const (
	MetaName             = "name"
	MetaDescription      = "description"
	MetaFamily           = "family"
	MetaReasoning        = "reasoning"
	MetaAttachment       = "attachment"
	MetaToolCall         = "tool_call"
	MetaTemperature      = "temperature"
	MetaContextWindow    = "context_window"
	MetaMaxOutputTokens  = "max_output_tokens"
	MetaMaxInputTokens   = "max_input_tokens"
	MetaModalitiesInput  = "modalities_input"
	MetaModalitiesOutput = "modalities_output"
	MetaStatus           = "status"
	MetaReleaseDate      = "release_date"
	MetaKnowledge        = "knowledge"
	MetaInterleaved      = "interleaved"
)

// KnownMetadataLeaves lists every known metadata leaf in stable order. The
// merge provenance and missing reports use exactly this order.
func KnownMetadataLeaves() []string {
	return []string{
		MetaName, MetaDescription, MetaFamily, MetaReasoning, MetaAttachment,
		MetaToolCall, MetaTemperature, MetaContextWindow, MetaMaxOutputTokens,
		MetaMaxInputTokens, MetaModalitiesInput, MetaModalitiesOutput,
		MetaStatus, MetaReleaseDate, MetaKnowledge, MetaInterleaved,
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

// MergeResult carries the three-layer merge output plus its audit trail.
type MergeResult struct {
	// Merged holds every present leaf after Prism → models.dev fill → manual
	// application.
	Merged MetadataLayer
	// Provenance maps each merged leaf to the layer it came from.
	Provenance map[string]MetadataSource
	// Missing lists known metadata leaves absent from every layer, in
	// KnownMetadataLeaves order.
	Missing []string
}

// MergeOptions configures one three-layer merge.
type MergeOptions struct {
	// Prism is the stored Prism effective metadata (source after overrides).
	Prism MetadataLayer
	// ModelsDev is the live models.dev parse for the bound offering. It may
	// be empty when the catalog was unavailable or the offering vanished.
	ModelsDev MetadataLayer
	// Manual is the operator-authored enhancement payload for this model.
	Manual MetadataLayer
	// OverrideFields lists manual leaves allowed to replace values that are
	// already present in the Prism or models.dev layers.
	OverrideFields []string
}

// sensitiveSubstrings is matched case-insensitively against every manual key.
// Any hit fails the whole enhancement closed rather than silently dropping
// credential-shaped material.
var sensitiveSubstrings = []string{
	"apikey", "authorization", "authtoken", "proxykey", "secret", "password",
	"passwd", "credential", "cookie", "sessionkey", "accesskey", "privatekey",
	"bearer", "signature", "satoken", "clientsecret", "accesstoken",
	"sessiontoken", "refreshtoken", "idtoken",
}

// KeyLooksSensitive reports whether a JSON object key carries credential-like
// material anywhere in its name.
func KeyLooksSensitive(key string) bool {
	lower := strings.ToLower(strings.TrimSpace(key))
	if lower == "" {
		return true
	}
	var compactBuilder strings.Builder
	compactBuilder.Grow(len(lower))
	for _, r := range lower {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			compactBuilder.WriteRune(r)
		}
	}
	compact := compactBuilder.String()
	for _, needle := range sensitiveSubstrings {
		if strings.Contains(lower, needle) || strings.Contains(compact, needle) {
			return true
		}
	}
	return false
}

// MergeKnownMetadata applies the three-layer leaf merge over the known
// metadata surface (error types live in errors.go):
//
//  1. Prism effective values win by default.
//  2. models.dev fills only leaves Prism left absent.
//  3. Manual values fill only still-absent leaves unless their name is listed
//     in OverrideFields, in which case they overwrite.
//
// Presence is preserved exactly: explicit false/0/""/[] stay present values
// and never count as missing. Locked fields cannot appear in the metadata
// surface, but the guard is kept here so future callers fail closed too.
func MergeKnownMetadata(options MergeOptions) (MergeResult, error) {
	override := make(map[string]struct{}, len(options.OverrideFields))
	for _, field := range options.OverrideFields {
		override[strings.TrimSpace(field)] = struct{}{}
	}
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
	for _, leaf := range options.ModelsDev.Leaves() {
		if _, present := merged[leaf]; present {
			continue
		}
		value, _ := options.ModelsDev.Get(leaf)
		merged[leaf] = value
		result.Provenance[leaf] = SourceModelsDev
	}
	for _, leaf := range options.Manual.Leaves() {
		if err := rejectLockedMetadataLeaf(leaf); err != nil {
			return MergeResult{}, err
		}
		if KeyLooksSensitive(leaf) {
			return MergeResult{}, &ErrSensitiveField{Field: leaf}
		}
		value, _ := options.Manual.Get(leaf)
		if _, present := merged[leaf]; present {
			if _, allowed := override[leaf]; !allowed {
				continue
			}
		}
		merged[leaf] = value
		result.Provenance[leaf] = SourceManual
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
// metadata evidence. A bound but unavailable models.dev offering is distinct
// from ordinary missing metadata, and an unbound model never receives the
// enrichment warning.
func MetadataWarningCodes(platform Platform, fact ModelFact, merged MetadataLayer) []string {
	warnings := []string{}
	if fact.CatalogBinding.Bound && !fact.Enrichment.Available {
		warnings = append(warnings, WarningEnrichmentUnavailable)
	}
	if platform == PlatformPi && fact.APIFamily == "openai" && fact.CatalogBinding.Bound &&
		fact.CatalogBinding.ProviderID != "" && fact.CatalogBinding.ProviderID != "openai" {
		warnings = append(warnings, WarningPiCompatMayRequireManualOverride)
	}
	if platform == PlatformPi {
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
	}
	relevant := []string{}
	switch platform {
	case PlatformPi:
		relevant = []string{MetaName, MetaReasoning, MetaContextWindow, MetaMaxOutputTokens, MetaModalitiesInput}
	case PlatformOpenCode:
		relevant = []string{
			MetaName, MetaFamily, MetaReleaseDate, MetaAttachment, MetaReasoning,
			MetaTemperature, MetaToolCall, MetaContextWindow, MetaMaxInputTokens,
			MetaMaxOutputTokens, MetaModalitiesInput, MetaModalitiesOutput,
		}
	}
	for _, leaf := range relevant {
		if _, present := merged.Get(leaf); !present {
			warnings = append(warnings, WarningMetadataIncomplete)
			break
		}
	}
	return sortWarningCodes(warnings)
}
