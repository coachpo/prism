package modelexport

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

// SourceFacts is the clock-free fact set one source response is built from
// and render replays against. Every field that can change the rendered file
// participates in the digest; every wall-clock stamp is excluded by design so
// a digest computed minutes apart stays stable while the facts are unchanged.
type SourceFacts struct {
	Platform        Platform                  `json:"platform"`
	TargetVersion   string                    `json:"target_version"`
	CatalogRevision string                    `json:"catalog_revision,omitempty"`
	Enrichment      map[int]PlatformCandidate `json:"enrichment_candidates,omitempty"`
	// Models carries the per-model fact rows in model_config_id order.
	Models []ModelFact `json:"models"`
}

// ModelFact is one model's export-relevant truth. It intentionally mirrors
// what renderers consume: identity, ordered reachable targets with their
// current price shapes, binding evidence, enrichment coordinates, and the
// stored metadata layers.
type ModelFact struct {
	ModelConfigID         int                `json:"model_config_id"`
	ModelID               string             `json:"model_id"`
	APIFamily             string             `json:"api_family"`
	DisplayName           *string            `json:"display_name,omitempty"`
	IsEnabled             bool               `json:"is_enabled"`
	Selectable            bool               `json:"selectable"`
	UnselectableReason    *string            `json:"unselectable_reason,omitempty"`
	OpenAIAcceptedFormat  *string            `json:"openai_accepted_format,omitempty"`
	OpenAIImageOperations *string            `json:"openai_image_operations,omitempty"`
	CatalogBinding        CatalogEvidence    `json:"catalog_binding"`
	Enrichment            EnrichmentEvidence `json:"enrichment"`
	// PrismMetadata is the stored effective metadata layer.
	PrismMetadata map[string]json.RawMessage `json:"prism_metadata"`
	// Targets are this model's enabled, active Terminal Targets in authored
	// position order with their normalized current price shape.
	Targets []TargetFact `json:"targets"`
}

// CatalogEvidence records the models.dev binding backing the metadata. It is
// management-only projection data and never enters runtime planning.
type CatalogEvidence struct {
	Bound           bool   `json:"bound"`
	ProviderID      string `json:"provider_id,omitempty"`
	CatalogModelID  string `json:"catalog_model_id,omitempty"`
	CatalogRevision string `json:"catalog_revision,omitempty"`
	MatchSource     string `json:"match_source,omitempty"`
	// OverrideFields lists the leaves carrying an operator override; it is
	// derived state of the stored row and part of the merge contract.
	HasOverrides bool `json:"has_overrides"`
}

// EnrichmentEvidence records the server-bound models.dev offering coordinates
// and availability backing the merged metadata. It participates in the digest
// together with the exact candidate; render never trusts a request candidate.
type EnrichmentEvidence struct {
	Available          bool   `json:"available"`
	OfferingProviderID string `json:"offering_provider_id,omitempty"`
	OfferingModelID    string `json:"offering_model_id,omitempty"`
}

// TargetFact is one reachable Terminal Target's export truth.
type TargetFact struct {
	TerminalTargetID     int                  `json:"terminal_target_id"`
	Position             int                  `json:"position"`
	EndpointID           int                  `json:"endpoint_id"`
	EndpointName         string               `json:"endpoint_name"`
	OpenAITextCapability *string              `json:"openai_text_capability,omitempty"`
	Pricing              *TargetPriceSnapshot `json:"pricing"`
	// EndpointBaseURL is retained only so older verification fixtures continue
	// to compile while the export contract moves URL ownership to the
	// operator-supplied Prism gateway origin. Production fact builders leave it
	// empty, renderers never read it, and json:"-" keeps upstream URLs out of
	// source_digest and all response projections.
	EndpointBaseURL string `json:"-"`
}

// ComputeSourceDigest derives the deterministic source_digest: SHA-256 over
// the canonical JSON encoding of the sorted, clock-free fact set. The bytes
// are independent of Go map iteration order because every nested collection
// in the fact types is either a slice (ordered upstream) or serialized via
// sorted-key canonical JSON.
func ComputeSourceDigest(facts SourceFacts) (string, error) {
	canonical, err := CanonicalJSON(facts)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

// CanonicalJSON encodes any JSON-serializable value with object keys sorted
// and no insignificant whitespace. RawMessage members are re-canonicalized so
// caller-built payloads cannot smuggle formatting into the digest.
func CanonicalJSON(value any) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("canonical encode failed: %w", err)
	}
	var decoded any
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return nil, fmt.Errorf("canonical decode failed: %w", err)
	}
	normalized := canonicalize(decoded)
	out, err := json.Marshal(normalized)
	if err != nil {
		return nil, fmt.Errorf("canonical re-encode failed: %w", err)
	}
	return out, nil
}

func canonicalize(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		ordered := make(map[string]any, len(typed))
		for _, key := range keys {
			ordered[key] = canonicalize(typed[key])
		}
		return ordered
	case []any:
		items := make([]any, len(typed))
		for index, item := range typed {
			items[index] = canonicalize(item)
		}
		return items
	default:
		return value
	}
}
