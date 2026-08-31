package contracttest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// The single-model Pi management read contract: GET /api/models/{id}/pi must
// serve the model identity, one catalog evidence block, live exact-candidate
// evidence, and the persisted binding truth — without loading export
// targets, pricing plans, source digests, credentials, or any runtime graph.

func piReadGet(t *testing.T, harness *contractHarness, modelConfigID int, status int) (map[string]any, http.Header) {
	t.Helper()
	return exportRequestWithHeaders(t, harness, http.MethodGet, fmt.Sprintf("/api/models/%d/pi", modelConfigID), nil, status)
}

func assertPiReadHasNoExportFields(t *testing.T, payload map[string]any) {
	t.Helper()
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("encode single-model read payload: %v", err)
	}
	encodedText := string(encoded)
	// Export-only surfaces never appear on the single-model read: no target
	// rows, no price cards, no source digest, no credential material, no
	// endpoint identities, no render results.
	for _, forbidden := range []string{
		"source_digest", "export_identity", "price_risk", "pricing", "targets",
		"completeness", "merged_metadata", "metadata_provenance", "missing_metadata",
		"api.openai.example", "sk-export", "endpoint", "file_name", "mime_type", "content_sha256",
	} {
		if strings.Contains(encodedText, forbidden) {
			t.Fatalf("single-model Pi read leaked export-only field %q: %s", forbidden, encodedText)
		}
	}
}

func TestModelPiReadUnboundFreshCatalog(t *testing.T) {
	harness := newExportContractHarness(t, exportServingCatalog, piServingCatalogHandler)
	modelConfigID, _ := exportSeedModel(t, harness, "gpt-export", "openai", "dual_native")

	payload, headers := piReadGet(t, harness, modelConfigID, http.StatusOK)
	if got := headers.Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("single-model read cache policy = %q, want private, no-store", got)
	}
	model := asMap(t, payload["model"])
	if model["model_id"] != "gpt-export" || model["api_family"] != "openai" || model["pi_api"] != "openai-responses" {
		t.Fatalf("model identity block drifted: %+v", model)
	}
	if jsonInt(t, model["model_config_id"]) != modelConfigID {
		t.Fatalf("model_config_id must echo the route: %+v", model)
	}
	catalog := asMap(t, payload["catalog"])
	if catalog["status"] != "fresh" {
		t.Fatalf("a served fetch must be fresh: %+v", catalog)
	}
	if revision, ok := catalog["revision"].(string); !ok || !strings.HasPrefix(revision, "sha256-") {
		t.Fatalf("catalog evidence must carry the trusted body-SHA revision: %+v", catalog)
	}
	if catalog["fetched_at"] == nil || catalog["checked_at"] == nil {
		t.Fatalf("catalog evidence must carry both stamps: %+v", catalog)
	}
	if payload["candidate_status"] != "single" {
		t.Fatalf("exact candidate status = %+v, want single", payload["candidate_status"])
	}
	if candidates := payload["candidates"].([]any); len(candidates) != 1 {
		t.Fatalf("live candidate evidence missing: %+v", payload["candidates"])
	}
	if payload["binding_status"] != "unbound" || payload["binding_renderable"] != false {
		t.Fatalf("fresh unbound model must report unbound: %+v", payload)
	}
	binding := asMap(t, payload["binding"])
	if binding["bound"] != false {
		t.Fatalf("binding block must be unbound: %+v", binding)
	}
	assertPiReadHasNoExportFields(t, payload)
}

func TestModelPiReadUnboundUnavailableCatalog(t *testing.T) {
	harness := newExportContractHarness(t, exportServingCatalog, piFailingCatalogHandler)
	modelConfigID, _ := exportSeedModel(t, harness, "gpt-export", "openai", "dual_native")

	// A cold catalog outage cannot manufacture unbound or empty evidence.
	payload, headers := piReadGet(t, harness, modelConfigID, http.StatusOK)
	if got := headers.Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("unavailable-read cache policy = %q, want private, no-store", got)
	}
	if asMap(t, payload["catalog"])["status"] != "unavailable" {
		t.Fatalf("failed cold fetch must be unavailable: %+v", payload["catalog"])
	}
	if payload["candidate_status"] != "catalog_unavailable" {
		t.Fatalf("candidate status under outage = %+v, want catalog_unavailable", payload["candidate_status"])
	}
	if payload["binding_status"] != "unbound" {
		t.Fatalf("a genuinely unbound model stays unbound under outage: %+v", payload)
	}
	assertPiReadHasNoExportFields(t, payload)
}

func TestModelPiReadBoundStaleCatalogKeepsFrozenTruth(t *testing.T) {
	var catalogAvailable atomic.Bool
	catalogAvailable.Store(true)
	harness := newExportContractHarness(t, exportServingCatalog, func(w http.ResponseWriter, r *http.Request) {
		if catalogAvailable.Load() {
			piServingCatalogHandler(w, r)
			return
		}
		piFailingCatalogHandler(w, r)
	})
	modelConfigID, _ := exportSeedModel(t, harness, "gpt-export", "openai", "dual_native")
	bound := exportBindPi(t, harness, modelConfigID)
	catalogAvailable.Store(false)

	payload, _ := piReadGet(t, harness, modelConfigID, http.StatusOK)
	catalog := asMap(t, payload["catalog"])
	if catalog["status"] != "stale" || catalog["revision"] == nil || catalog["fetched_at"] == nil || catalog["checked_at"] == nil {
		t.Fatalf("failed refresh must retain labelled LKG evidence: %+v", catalog)
	}
	if payload["candidate_status"] != "single" || len(payload["candidates"].([]any)) != 1 {
		t.Fatalf("stale LKG must remain readable candidate evidence: %+v", payload)
	}
	if payload["binding_status"] != "bound" || payload["binding_renderable"] != true {
		t.Fatalf("catalog failure must not degrade a compatible frozen binding: %+v", payload)
	}
	binding := asMap(t, payload["binding"])
	if binding["provider_id"] != bound["provider_id"] || binding["catalog_model_id"] != bound["catalog_model_id"] || binding["prism_model_id_at_bind"] != "gpt-export" {
		t.Fatalf("stale read changed persisted binding evidence: %+v", binding)
	}
	assertPiReadHasNoExportFields(t, payload)
}

func TestModelPiReadBoundUnavailableCatalogKeepsFrozenTruth(t *testing.T) {
	harness := newExportContractHarness(t, exportServingCatalog, nil)
	modelConfigID, _ := exportSeedModel(t, harness, "gpt-offline", "openai", "dual_native")
	now := time.Now().UTC()
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO model_pi_catalog_bindings (
		model_config_id, provider_id, catalog_model_id, api, prism_model_id_at_bind, bind_source, catalog_revision, fetched_at,
		source_name, source_reasoning, source_input, source_context_window, source_max_tokens, source_dropped_fields, updated_at
	) VALUES ($1, 'openai', 'gpt-offline', 'openai-responses', 'gpt-offline', 'manual', 'sha256-offline-read-fixture', $2,
		'GPT Offline', TRUE, '["text"]'::jsonb, 200000, 16384, '[]'::jsonb, $2)`, modelConfigID, now); err != nil {
		t.Fatalf("seed frozen Pi binding: %v", err)
	}

	payload, _ := piReadGet(t, harness, modelConfigID, http.StatusOK)
	catalog := asMap(t, payload["catalog"])
	if catalog["status"] != "unavailable" || catalog["revision"] != nil || catalog["fetched_at"] != nil || catalog["checked_at"] != nil {
		t.Fatalf("cold unavailable evidence must not invent catalog stamps: %+v", catalog)
	}
	if payload["candidate_status"] != "catalog_unavailable" || len(payload["candidates"].([]any)) != 0 {
		t.Fatalf("cold outage candidate evidence must stay unavailable and empty: %+v", payload)
	}
	if payload["binding_status"] != "bound" || payload["binding_renderable"] != true {
		t.Fatalf("persisted compatible binding remains authoritative without a catalog client: %+v", payload)
	}
	binding := asMap(t, payload["binding"])
	if binding["provider_id"] != "openai" || binding["catalog_model_id"] != "gpt-offline" || binding["effective"] == nil {
		t.Fatalf("unavailable read omitted persisted binding truth: %+v", binding)
	}
	assertPiReadHasNoExportFields(t, payload)
}

func TestModelPiReadBoundDriftedAndRenderability(t *testing.T) {
	harness := newExportContractHarness(t, exportServingCatalog, piServingCatalogHandler)
	const prismModelID = "codex/gpt-export"
	modelConfigID, _ := exportSeedModel(t, harness, prismModelID, "openai", "dual_native")

	source := exportFetchSource(t, harness)
	revision := source["catalog"].(map[string]any)["revision"].(string)
	exportPiBind(t, harness, modelConfigID, map[string]any{
		"provider_id":               "openai",
		"catalog_model_id":          "gpt-export-lite",
		"expected_catalog_revision": revision,
		"expected_prism_model_id":   prismModelID,
		"expected_pi_api":           "openai-responses",
	}, http.StatusOK)

	payload, _ := piReadGet(t, harness, modelConfigID, http.StatusOK)
	// Cross-directory bind: the coordinate differs from the Prism id, but the
	// frozen identity matches, so the binding is compatible and renderable.
	if payload["binding_status"] != "bound" || payload["binding_renderable"] != true {
		t.Fatalf("compatible cross-directory binding must stay renderable: %+v", payload)
	}
	binding := asMap(t, payload["binding"])
	if binding["provider_id"] != "openai" || binding["catalog_model_id"] != "gpt-export-lite" {
		t.Fatalf("binding coordinate evidence drifted: %+v", binding)
	}
	if binding["prism_model_id_at_bind"] != prismModelID {
		t.Fatalf("frozen Prism identity must be reported: %+v", binding)
	}
	if binding["updated_at"] == nil || binding["catalog_revision"] == nil {
		t.Fatalf("binding must publish its CAS token evidence: %+v", binding)
	}
	assertPiReadHasNoExportFields(t, payload)
}

func TestModelPiReadIdentityDriftBlocksRenderability(t *testing.T) {
	harness := newExportContractHarness(t, exportServingCatalog, piServingCatalogHandler)
	const prismModelID = "codex/gpt-export"
	modelConfigID, _ := exportSeedModel(t, harness, prismModelID, "openai", "dual_native")

	source := exportFetchSource(t, harness)
	revision := source["catalog"].(map[string]any)["revision"].(string)
	exportPiBind(t, harness, modelConfigID, map[string]any{
		"provider_id":               "openai",
		"catalog_model_id":          "gpt-export",
		"expected_catalog_revision": revision,
		"expected_prism_model_id":   prismModelID,
		"expected_pi_api":           "openai-responses",
	}, http.StatusOK)

	// A Prism rename after the bind leaves the frozen coordinate non-renderable
	// while still reporting the persisted binding honestly.
	modelJSON[map[string]any](t, harness, modelLoadDefaultProfileID(t, harness), http.MethodPut, fmt.Sprintf("/api/models/%d", modelConfigID), map[string]any{
		"api_family": "openai", "model_id": "codex/gpt-export-renamed", "is_enabled": true,
	}, http.StatusOK)

	payload, _ := piReadGet(t, harness, modelConfigID, http.StatusOK)
	if payload["binding_status"] != "bound_drifted" || payload["binding_renderable"] != false {
		t.Fatalf("renamed Prism identity must fail renderability closed: %+v", payload)
	}
	binding := asMap(t, payload["binding"])
	if binding["prism_model_id_at_bind"] != prismModelID {
		t.Fatalf("the old frozen identity stays visible for diagnosis: %+v", binding)
	}
	assertPiReadHasNoExportFields(t, payload)
}

func TestModelPiReadUnknownModelFailsClosed(t *testing.T) {
	harness := newExportContractHarness(t, exportServingCatalog, piServingCatalogHandler)
	_, headers := piReadGet(t, harness, 999999, http.StatusNotFound)
	if got := headers.Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("error responses must keep the cache policy: %q", got)
	}
}
