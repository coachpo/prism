package contracttest

import (
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
)

func TestClientReadinessRepairUsesRenderAuthority(t *testing.T) {
	harness := newExportContractHarness(t, exportServingCatalog, piServingCatalogHandler)
	id := openCodeSeedModel(t, harness, "gpt-export", "openai", "responses_only")
	// Represent retained incomplete source evidence; no guessed replacement.
	if _, err := harness.conn.Exec(t.Context(), `UPDATE model_catalog_bindings SET source_limit_output=NULL WHERE model_config_id=$1`, id); err != nil {
		t.Fatal(err)
	}
	before := openCodeSource(t, harness)
	readiness := asMap(t, exportSourceRow(t, before, id)["readiness"])
	if readiness["status"] != "blocked" || fmt.Sprint(readiness["blocking_reasons"]) != "[invalid_metadata_limits]" {
		t.Fatalf("missing output must block: %+v", readiness)
	}
	exportRequestWithHeaders(t, harness, http.MethodPost, "/api/models/exports/opencode/render", openCodeRenderBody(before, id), http.StatusUnprocessableEntity)
	piBefore := exportFetchSource(t, harness)
	if asMap(t, exportSourceRow(t, piBefore, id)["readiness"])["status"] != "blocked" {
		t.Fatal("unbound Pi must not inherit OpenCode metadata readiness")
	}
	requestJSONStatus[map[string]any](t, harness, http.MethodPut, fmt.Sprintf("/api/models/%d/catalog/override", id), map[string]any{
		"expected_provider_id": "test-catalog", "expected_catalog_model_id": "different-directory-id", "override": map[string]any{"limit_output": 4096},
	}, nil, http.StatusOK)
	after := openCodeSource(t, harness)
	if asMap(t, exportSourceRow(t, after, id)["readiness"])["status"] != "ready" {
		t.Fatal("verified override must repair OpenCode readiness")
	}
	exportRequestWithHeaders(t, harness, http.MethodPost, "/api/models/exports/opencode/render", openCodeRenderBody(before, id), http.StatusConflict)
	exportRequestWithHeaders(t, harness, http.MethodPost, "/api/models/exports/opencode/render", openCodeRenderBody(after, id), http.StatusOK)
	exportBindPi(t, harness, id)
	if asMap(t, exportSourceRow(t, exportFetchSource(t, harness), id)["readiness"])["status"] != "ready" {
		t.Fatal("Pi must become ready from its own binding")
	}
}

func TestPiCatalogFailureReadinessAndRecovery(t *testing.T) {
	var broken atomic.Bool
	harness := newExportContractHarness(t, exportServingCatalog, func(w http.ResponseWriter, r *http.Request) {
		if broken.Load() {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"private-url":"https://secret.example/token"}`))
			return
		}
		piServingCatalogHandler(w, r)
	})
	id, _ := exportSeedModel(t, harness, "gpt-export", "openai", "responses_only")
	exportBindPi(t, harness, id)
	good := exportFetchSource(t, harness)
	broken.Store(true)
	stale := exportFetchSource(t, harness)
	catalog := asMap(t, stale["catalog"])
	if catalog["status"] != "stale" || catalog["failure_code"] != "format" || catalog["checked_at"] == nil {
		t.Fatalf("typed stale evidence: %+v", catalog)
	}
	if stale["source_digest"] != good["source_digest"] || asMap(t, exportSourceRow(t, stale, id)["readiness"])["status"] != "ready" {
		t.Fatal("outage changed frozen render facts")
	}
	single := requestJSONStatus[map[string]any](t, harness, http.MethodGet, fmt.Sprintf("/api/models/%d/pi", id), nil, nil, http.StatusOK)
	if asMap(t, single["catalog"])["failure_code"] != "format" {
		t.Fatal("single-model read must retain safe failure category")
	}
	broken.Store(false)
	recovered := exportFetchSource(t, harness)
	catalog = asMap(t, recovered["catalog"])
	if catalog["status"] != "fresh" || catalog["failure_code"] != nil {
		t.Fatalf("recovery must clear failure: %+v", catalog)
	}
}
