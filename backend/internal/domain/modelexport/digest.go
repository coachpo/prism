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
// and render replays against. Every field that can change rendered bytes or
// the persisted source evidence asserted by render participates in the digest;
// wall-clock stamps are excluded so unchanged facts remain stable over time.
type SourceFacts struct {
	TargetVersion string `json:"target_version"`
	// PiCatalog is the current live pi.dev fetch's top-level evidence
	// (fresh/stale/unavailable, current revision). It is excluded from the
	// digest (json:"-"): whether this particular request's live fetch
	// happened to succeed never affects the rendered bytes of an
	// already-bound model; render reads no live or last-known-good catalog.
	PiCatalog PiCatalogEvidence `json:"-"`
	// Models carries the per-model fact rows in model_config_id order.
	Models []ModelFact `json:"models"`
}

// ModelFact is one model's export-relevant truth. It intentionally mirrors
// what the renderer consumes: identity, ordered reachable targets with their
// current price shapes, a persisted Pi coordinate/template, and first-party
// Prism metadata.
type ModelFact struct {
	ModelConfigID         int     `json:"model_config_id"`
	ModelID               string  `json:"model_id"`
	APIFamily             string  `json:"api_family"`
	DisplayName           *string `json:"display_name,omitempty"`
	IsEnabled             bool    `json:"is_enabled"`
	Selectable            bool    `json:"selectable"`
	UnselectableReason    *string `json:"unselectable_reason,omitempty"`
	OpenAIAcceptedFormat  *string `json:"openai_accepted_format,omitempty"`
	OpenAIImageOperations *string `json:"openai_image_operations,omitempty"`
	// PiCandidates/PiCandidateStatus are live pi.dev catalog evidence: every
	// entry currently matching this model's exact model_id and expected Pi
	// API, and a summary of that search (not_in_catalog/api_mismatch/single/
	// multiple/catalog_unavailable). Both are excluded from the digest
	// (json:"-") for the same reason as SourceFacts.PiCatalog: this
	// transient live-fetch outcome never affects rendered bytes and is not
	// read by render.
	PiCandidates      []PiCandidate `json:"-"`
	PiCandidateStatus string        `json:"-"`
	// PiSelected is the persisted model_pi_catalog_bindings coordinate, when
	// one exists. It is authoritative independently of the live catalog, but
	// render still rejects it if its full model id/API no longer matches the
	// current Prism model. The raw coordinate participates in the digest so
	// drift is visible and a rebind always moves source evidence.
	PiSelected *SelectedCoordinate `json:"pi_selected,omitempty"`
	// PiTemplate is the effective, API-sanitized source snapshot persisted
	// with PiSelected. It is part of the digest and is the only catalog
	// metadata the renderer consumes; live candidates never participate in
	// rendering an already-bound model.
	PiTemplate PiTemplate `json:"pi_template"`
	// PiBindingStatus reports the persisted binding's health against the
	// live catalog evidence (unbound/bound/bound_drifted). Like the live
	// evidence above, it is excluded from the digest (json:"-"): render
	// never gates on it; render separately requires PiSelected to be present,
	// current-identity/API compatible, and equal to the caller's assertion at
	// the management boundary.
	PiBindingStatus string `json:"-"`
	// PrismMetadata contains only first-party Prism metadata. models.dev
	// bindings and their manual overrides never enter the Pi export fact set.
	PrismMetadata map[string]json.RawMessage `json:"prism_metadata"`
	// Targets are this model's enabled, active Terminal Targets in authored
	// position order with their normalized current price shape.
	Targets []TargetFact `json:"targets"`
}

type PiCatalogEvidence struct {
	Revision       string `json:"revision,omitempty"`
	Status         string `json:"status"` // fresh|stale|unavailable
	MinimumVersion string `json:"minimum_version,omitempty"`
	ETag           string `json:"etag,omitempty"`
}

// SelectedCoordinate is the effective Pi binding for one model: the frozen
// coordinate plus the catalog_revision it was bound or last refreshed
// against. The revision participates in the digest so a rebind or a refresh
// moves the digest even when the coordinate itself is unchanged.
type SelectedCoordinate struct {
	ProviderID      string `json:"provider_id"`
	ModelID         string `json:"model_id"`
	API             string `json:"api"`
	CatalogRevision string `json:"catalog_revision,omitempty"`
}

type PiCandidate struct {
	ProviderID    string   `json:"provider_id"`
	ModelID       string   `json:"model_id"`
	API           string   `json:"api"`
	Name          *string  `json:"name,omitempty"`
	DroppedFields []string `json:"dropped_fields,omitempty"`
}

// TargetFact is one reachable Terminal Target's export truth.
type TargetFact struct {
	TerminalTargetID     int                  `json:"terminal_target_id"`
	Position             int                  `json:"position"`
	EndpointID           int                  `json:"endpoint_id"`
	EndpointName         string               `json:"endpoint_name"`
	OpenAITextCapability *string              `json:"openai_text_capability,omitempty"`
	Pricing              *TargetPriceSnapshot `json:"pricing"`
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
