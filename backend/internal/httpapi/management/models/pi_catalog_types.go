package models

import (
	"time"
)

// Common Pi catalog wire shapes shared by the export source, directory
// search, and single-model management read. They carry discovery evidence
// only; persisted binding and render authority remain separate.
type piCatalogWire struct {
	Revision       string `json:"revision,omitempty"`
	Status         string `json:"status"` // fresh|stale|unavailable
	MinimumVersion string `json:"minimum_version,omitempty"`
	ETag           string `json:"etag,omitempty"`
}

type piCandidateWire struct {
	ProviderID       string             `json:"provider_id"`
	ModelID          string             `json:"model_id"`
	API              string             `json:"api"`
	Name             *string            `json:"name,omitempty"`
	Reasoning        *bool              `json:"reasoning,omitempty"`
	Input            []string           `json:"input,omitempty"`
	ContextWindow    *int64             `json:"context_window,omitempty"`
	MaxTokens        *int64             `json:"max_tokens,omitempty"`
	ThinkingLevelMap map[string]*string `json:"thinking_level_map,omitempty"`
	Compat           map[string]any     `json:"compat,omitempty"`
	DroppedFields    []string           `json:"dropped_fields,omitempty"`
}

// piExportIdentityWire is Prism's own authoritative identity for a bind
// decision. A pi.dev coordinate never replaces any of these fields.
type piExportIdentityWire struct {
	ModelConfigID    int    `json:"model_config_id"`
	ModelID          string `json:"model_id"`
	API              string `json:"api"`
	ProviderIDSource string `json:"provider_id_source"`
}

// piCatalogSearchResponse publishes one bounded, permanently-unselected
// directory search page and the exact snapshot evidence behind it.
type piCatalogSearchResponse struct {
	Query     string        `json:"query"`
	API       string        `json:"api"`
	Limit     int           `json:"limit"`
	Offset    int           `json:"offset"`
	Total     int           `json:"total"`
	Returned  int           `json:"returned"`
	Truncated bool          `json:"truncated"`
	Selected  bool          `json:"selected"`
	Catalog   piCatalogWire `json:"catalog"`
	FetchedAt time.Time     `json:"fetched_at"`
	// CheckedAt is when the served revision was last revalidated (including
	// 304s); FetchedAt is when the content itself was originally fetched.
	CheckedAt      time.Time            `json:"checked_at"`
	ExportIdentity piExportIdentityWire `json:"export_identity"`
	Results        []piCandidateWire    `json:"results"`
}

// piCatalogReadWire is the catalog evidence block of a single-model Pi read.
// It carries the revision trust state plus both timestamps: `fetched_at` is
// when the content was originally downloaded and `checked_at` when the served
// revision was last revalidated (a 304 refreshes checked_at, not fetched_at).
// When the catalog is unavailable the revision and timestamps stay absent.
type piCatalogReadWire struct {
	Status         string     `json:"status"` // fresh|stale|unavailable
	Revision       string     `json:"revision,omitempty"`
	MinimumVersion string     `json:"minimum_version,omitempty"`
	ETag           string     `json:"etag,omitempty"`
	FetchedAt      *time.Time `json:"fetched_at,omitempty"`
	CheckedAt      *time.Time `json:"checked_at,omitempty"`
}

// piModelIdentityWire is the Prism-owned identity block of a single-model Pi
// read. `pi_api` is Prism's own final Pi API mapping for the model, or empty
// when the family/accepted-format pair has none — the UI never re-derives it.
type piModelIdentityWire struct {
	ModelConfigID int    `json:"model_config_id"`
	ModelID       string `json:"model_id"`
	APIFamily     string `json:"api_family"`
	PiAPI         string `json:"pi_api,omitempty"`
}

// piModelReadResponse is the single-model Pi management read behind
// GET /api/models/{model_config_id}/pi. It is the smallest surface that lets
// the model detail panel manage one Pi binding: current Prism identity, one
// catalog evidence block, live exact-candidate discovery evidence, and the
// persisted binding truth. It never loads export targets, pricing plans,
// source digests, credentials, render results, or any runtime graph, and it
// never selects or gates anything by itself.
type piModelReadResponse struct {
	Model piModelIdentityWire `json:"model"`
	// Catalog is live pi.dev evidence (fresh, stale last-known-good, or
	// unavailable). Persisted binding truth below is independent of it.
	Catalog piCatalogReadWire `json:"catalog"`
	// CandidateStatus summarizes the default exact-id search over the live
	// catalog (not_in_catalog/api_mismatch/single/multiple/catalog_unavailable).
	CandidateStatus string            `json:"candidate_status"`
	Candidates      []piCandidateWire `json:"candidates"`
	// BindingStatus reports the persisted model_pi_catalog_bindings row
	// health (unbound/bound/bound_drifted); BindingRenderable is the explicit
	// render health gate. Binding preserves the raw coordinate even after
	// Prism identity/API drift.
	BindingStatus     string            `json:"binding_status"`
	BindingRenderable bool              `json:"binding_renderable"`
	Binding           piBindingResponse `json:"binding"`
}
