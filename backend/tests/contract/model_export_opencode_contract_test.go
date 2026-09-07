package contracttest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func openCodeSeedModel(t *testing.T, harness *contractHarness, name, family, mode string) int {
	t.Helper()
	id, _ := exportSeedModel(t, harness, name, family, mode)
	_, err := harness.conn.Exec(t.Context(), `INSERT INTO model_catalog_bindings
		(model_config_id, provider_id, catalog_model_id, match_source, catalog_revision, fetched_at, updated_at,
		 source_name, source_reasoning, source_tool_call, source_limit_context, source_limit_output, override_reasoning, override_limit_input)
		VALUES ($1, 'test-catalog', 'different-directory-id', 'manual', 'test-revision', NOW(), NOW(),
		 'Catalog model', TRUE, TRUE, 131072, 8192, FALSE, 0)`, id)
	if err != nil {
		t.Fatalf("seed persisted OpenCode metadata: %v", err)
	}
	return id
}

func openCodeSource(t *testing.T, harness *contractHarness) map[string]any {
	t.Helper()
	source, headers := exportRequestWithHeaders(t, harness, http.MethodGet, "/api/models/exports/opencode/source", nil, http.StatusOK)
	if headers.Get("Cache-Control") != "private, no-store" {
		t.Fatal("OpenCode source must be private/no-store")
	}
	logOpenCodeObservation(t, "source", map[string]any{"asserted_http_status": http.StatusOK, "target_version": source["target_version"], "source_digest": source["source_digest"], "cache_control": headers.Get("Cache-Control")})
	return source
}

func logOpenCodeObservation(t *testing.T, event string, data map[string]any) {
	t.Helper()
	encoded, err := json.Marshal(map[string]any{"test": t.Name(), "event": event, "data": data})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("OPENCODE_API_OBSERVATION %s", encoded)
}

func openCodeRenderBody(source map[string]any, ids ...int) map[string]any {
	return map[string]any{"expected_source_digest": source["source_digest"], "model_config_ids": ids, "base_url": "https://prism.example"}
}

func TestOpenCodeExportUsesPersistedMetadataWithoutCatalogOrPi(t *testing.T) {
	var calls atomic.Int32
	harness := newExportContractHarness(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); exportServingCatalog(w, r) }, piFailingCatalogHandler)
	id := openCodeSeedModel(t, harness, "vendor/model", "openai", "dual_native")
	before := calls.Load()
	persisted := loadDirectRequestEntryPersistenceSnapshot(t, harness, 1, id)
	source := openCodeSource(t, harness)
	otherProfileHeader := requestJSONStatus[map[string]any](t, harness, http.MethodGet, "/api/models/exports/opencode/source", nil, map[string]string{"X-Profile-Id": "999"}, http.StatusOK)
	if otherProfileHeader["source_digest"] != source["source_digest"] {
		t.Fatal("OpenCode source must stay on Default profile 1")
	}
	sourceBytes, _ := json.Marshal(source)
	if strings.Contains(string(sourceBytes), "sk-export-live-key") || strings.Contains(string(sourceBytes), "export.example") {
		t.Fatal("source must not expose stored credentials or upstream URL")
	}
	row := exportSourceRow(t, source, id)
	if row["selectable"] != true || row["npm"] != "@ai-sdk/openai" || row["api_path"] != "/v1" {
		t.Fatalf("source identity/eligibility: %+v", row)
	}
	merged, provenance := asMap(t, row["merged_metadata"]), asMap(t, row["metadata_provenance"])
	if merged["reasoning"] != false || merged["limit_input"] != float64(0) || provenance["reasoning"] != "models_dev_override" {
		t.Fatalf("overrides must retain false and zero: %+v", row)
	}
	body := openCodeRenderBody(source, id)
	first, headers := exportRequestWithHeaders(t, harness, http.MethodPost, "/api/models/exports/opencode/render", body, http.StatusOK)
	second := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/models/exports/opencode/render", body, nil, http.StatusOK)
	content := first["content"].(string)
	sum := sha256.Sum256([]byte(content))
	if content != second["content"] || first["content_sha256"] != hex.EncodeToString(sum[:]) || !strings.HasSuffix(content, "\n") || strings.HasSuffix(content, "\n\n") {
		t.Fatal("render byte/hash determinism contract failed")
	}
	if first["file_name"] != "opencode-prism.json" || first["mime_type"] != "application/json;charset=utf-8" || headers.Get("Cache-Control") != "private, no-store" {
		t.Fatalf("render delivery contract: %+v", first)
	}
	var document map[string]any
	if err := json.Unmarshal([]byte(content), &document); err != nil {
		t.Fatal(err)
	}
	provider := asMap(t, asMap(t, document["provider"])["prism"])
	if provider["options"] != nil || fmt.Sprint(provider["env"]) != "[PRISM_API_KEY]" {
		t.Fatalf("environment credential provider: %+v", provider)
	}
	model := asMap(t, asMap(t, provider["models"])["vendor/model"])
	if asMap(t, model["provider"])["api"] != "https://prism.example/v1" || model["id"] != nil || model["cost"] == nil {
		t.Fatalf("gateway ID/price authority: %+v", model)
	}
	for _, forbidden := range []string{"sk-export-live-key", "export.example", "different-directory-id", "thinkingLevelMap", "pi_selected"} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("export leaked %s", forbidden)
		}
	}
	if calls.Load() != before {
		t.Fatal("OpenCode source/render must not fetch models.dev")
	}
	after := loadDirectRequestEntryPersistenceSnapshot(t, harness, 1, id)
	if after != persisted {
		t.Fatal("source/render changed persisted model, binding or runtime generation state")
	}
	logOpenCodeObservation(t, "render_and_read_only_snapshot", map[string]any{
		"asserted_http_status": http.StatusOK, "target_version": first["target_version"], "source_digest": first["source_digest"],
		"content_sha256": first["content_sha256"], "computed_sha256": hex.EncodeToString(sum[:]), "file_name": first["file_name"], "mime_type": first["mime_type"],
		"cache_control": headers.Get("Cache-Control"), "identical_repeat_bytes": content == second["content"], "single_trailing_newline": strings.HasSuffix(content, "\n") && !strings.HasSuffix(content, "\n\n"),
		"catalog_requests_before": before, "catalog_requests_after": calls.Load(), "persisted_snapshot_unchanged": after == persisted,
		"runtime_generations_unchanged": after.RuntimeGenerationRows == persisted.RuntimeGenerationRows && after.RouteWitnessGeneration == persisted.RouteWitnessGeneration,
		"other_profile_header_digest":   otherProfileHeader["source_digest"], "env": provider["env"], "provider_options_absent": provider["options"] == nil,
		"model_id": row["model_id"], "npm": row["npm"], "api_path": row["api_path"], "merged_metadata": merged, "metadata_provenance": provenance,
	})
}

func TestOpenCodeExportDigestStaleRecoveryAndIndependentFacts(t *testing.T) {
	harness := newExportContractHarness(t, exportServingCatalog, piServingCatalogHandler)
	id := openCodeSeedModel(t, harness, "gpt-export", "openai", "responses_only")
	source := openCodeSource(t, harness)
	exportBindPi(t, harness, id)
	afterPi := openCodeSource(t, harness)
	if afterPi["source_digest"] != source["source_digest"] {
		t.Fatal("Pi binding invalidated OpenCode digest")
	}
	if _, err := harness.conn.Exec(t.Context(), `UPDATE model_catalog_bindings SET source_description='irrelevant', catalog_revision='new-revision' WHERE model_config_id=$1`, id); err != nil {
		t.Fatal(err)
	}
	afterUnused := openCodeSource(t, harness)
	if afterUnused["source_digest"] != source["source_digest"] {
		t.Fatal("unused catalog facts invalidated OpenCode digest")
	}
	requestJSONStatus[map[string]any](t, harness, http.MethodPut, fmt.Sprintf("/api/models/%d/catalog/override", id), map[string]any{
		"expected_provider_id": "test-catalog", "expected_catalog_model_id": "different-directory-id", "override": map[string]any{"limit_output": 4096},
	}, nil, http.StatusOK)
	stale, headers := exportRequestWithHeaders(t, harness, http.MethodPost, "/api/models/exports/opencode/render", openCodeRenderBody(source, id), http.StatusConflict)
	if stale["code"] != "export_source_stale" || headers.Get("Cache-Control") != "private, no-store" {
		t.Fatalf("stale response: %+v", stale)
	}
	fresh := openCodeSource(t, harness)
	if fresh["source_digest"] == source["source_digest"] {
		t.Fatal("consumed metadata change must alter digest")
	}
	recovered := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/models/exports/opencode/render", openCodeRenderBody(fresh, id), nil, http.StatusOK)
	logOpenCodeObservation(t, "stale_and_refresh_recovery", map[string]any{
		"original_digest": source["source_digest"], "after_pi_binding_digest": afterPi["source_digest"], "after_unused_catalog_change_digest": afterUnused["source_digest"],
		"stale_asserted_http_status": http.StatusConflict, "stale_response_code": stale["code"], "stale_cache_control": headers.Get("Cache-Control"),
		"refreshed_digest": fresh["source_digest"], "recovered_asserted_http_status": http.StatusOK, "recovered_digest": recovered["source_digest"], "recovered_content_sha256": recovered["content_sha256"],
	})
}

func TestOpenCodeExportRejectsUnavailableModelsAndInvalidRequests(t *testing.T) {
	harness := newExportContractHarness(t, exportServingCatalog, piFailingCatalogHandler)
	valid := openCodeSeedModel(t, harness, "usable", "openai", "chat_completions_only")
	missing, _ := exportSeedModel(t, harness, "missing-limits", "anthropic", "")
	pathID := openCodeSeedModel(t, harness, "vendor/gemini", "gemini", "")
	source := openCodeSource(t, harness)
	for id, reason := range map[int]string{missing: "invalid_metadata_limits", pathID: "unaddressable_model_id"} {
		row := exportSourceRow(t, source, id)
		if row["selectable"] != false || row["unselectable_reason"] != reason {
			t.Fatalf("invalid source row: %+v", row)
		}
		requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/models/exports/opencode/render", openCodeRenderBody(source, id), nil, http.StatusUnprocessableEntity)
	}
	for _, test := range []struct {
		name   string
		patch  map[string]any
		status int
	}{
		{"unknown model", map[string]any{"model_config_ids": []int{-1}}, 422},
		{"empty selection", map[string]any{"model_config_ids": []int{}}, 422},
		{"builtin provider", map[string]any{"provider_id": "openai"}, 422},
		{"URL path", map[string]any{"base_url": "https://prism.example/v1"}, 422},
		{"URL credential", map[string]any{"base_url": "https://typed:secret@prism.example"}, 422},
		{"blank key", map[string]any{"credential": map[string]any{"include": true, "api_key": "  "}}, 422},
		{"unexpected key", map[string]any{"credential": map[string]any{"include": false, "api_key": "typed-secret"}}, 422},
		{"unknown target", map[string]any{"target_version": "latest"}, 400},
		{"Pi selections", map[string]any{"selections": map[string]any{}}, 400},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := openCodeRenderBody(source, valid)
			for key, value := range test.patch {
				body[key] = value
			}
			response, headers := exportRequestWithHeaders(t, harness, http.MethodPost, "/api/models/exports/opencode/render", body, test.status)
			encoded, _ := json.Marshal(response)
			if strings.Contains(string(encoded), "typed-secret") || headers.Get("Cache-Control") != "private, no-store" {
				t.Fatalf("private rejection: %s", encoded)
			}
			logOpenCodeObservation(t, "invalid_request", map[string]any{"case": test.name, "asserted_http_status": test.status, "response": response, "cache_control": headers.Get("Cache-Control")})
		})
	}
	body := openCodeRenderBody(source, valid)
	body["credential"] = map[string]any{"include": true, "api_key": "  typed-final-value  "}
	result := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/models/exports/opencode/render", body, nil, http.StatusOK)
	if !strings.Contains(result["content"].(string), `"apiKey": "typed-final-value"`) {
		t.Fatal("typed key must be trimmed")
	}
	logOpenCodeObservation(t, "manual_credential_render", map[string]any{"asserted_http_status": http.StatusOK, "content_sha256": result["content_sha256"], "file_name": result["file_name"], "mime_type": result["mime_type"], "final_key_trimmed": strings.Contains(result["content"].(string), `"apiKey": "typed-final-value"`)})
}

func TestOpenCodeExportModelIDAddressabilityFollowsClientURL(t *testing.T) {
	harness := newExportContractHarness(t, exportServingCatalog, piFailingCatalogHandler)
	for _, family := range []string{"gemini", "openai"} {
		id := openCodeSeedModel(t, harness, family+"-addressability", family, "responses_only")
		for _, test := range []struct {
			name, suffix string
			pathSafe     bool
		}{
			{"plain", "model", true},
			{"unicode and space", "模型 name", true},
			{"slash", "model/x", false},
			{"colon", "model:x", false},
			{"query", "model?x", false},
			{"fragment", "model#x", false},
			{"escaped byte", "model%41", false},
			{"invalid escape", "model%", false},
			{"backslash", `model\x`, false},
			{"tab", "model\tx", false},
			{"newline", "model\nx", false},
			{"delete control", "model\x7fx", false},
		} {
			t.Run(family+"/"+test.name, func(t *testing.T) {
				modelID := family + "-" + test.suffix
				update := map[string]any{"model_id": modelID}
				if family == "openai" {
					update["openai_accepted_format"] = "responses_only"
				}
				requestJSONStatus[map[string]any](t, harness, http.MethodPut, fmt.Sprintf("/api/models/%d", id), update, nil, http.StatusOK)
				source := openCodeSource(t, harness)
				row := exportSourceRow(t, source, id)
				selectable := family == "openai" || test.pathSafe
				if row["selectable"] != selectable || row["model_id"] != modelID {
					t.Fatalf("source must reflect unchanged identity addressability: %+v", row)
				}
				if !selectable {
					if row["unselectable_reason"] != "unaddressable_model_id" {
						t.Fatalf("missing addressability reason: %+v", row)
					}
					requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/models/exports/opencode/render", openCodeRenderBody(source, id), nil, http.StatusUnprocessableEntity)
					return
				}
				result := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/models/exports/opencode/render", openCodeRenderBody(source, id), nil, http.StatusOK)
				var document map[string]any
				if err := json.Unmarshal([]byte(result["content"].(string)), &document); err != nil {
					t.Fatal(err)
				}
				models := asMap(t, asMap(t, asMap(t, document["provider"])["prism"])["models"])
				if _, exists := models[modelID]; !exists {
					t.Fatalf("render rewrote model ID %q", modelID)
				}
			})
		}
	}
}
