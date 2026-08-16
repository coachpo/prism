package contracttest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestRoutingDiagnosticsContract(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Diagnostics Strategy")
	modelConfigID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "diagnostics-dual-model", nil, "native", &strategyID, true)
	endpointID := modelInsertEndpoint(t, harness, profileID, "Diagnostics Endpoint", 0)
	connectionID := modelInsertConnectionWithCapability(t, harness, profileID, modelConfigID, endpointID, "responses_only", 0, true)

	t.Run("full diagnostics shape", func(t *testing.T) {
		payload := s15GET[map[string]any](t, harness, profileID, fmt.Sprintf("/api/models/%d/routing-diagnostics", modelConfigID), http.StatusOK)
		if jsonInt(t, payload["model_config_id"]) != modelConfigID {
			t.Fatalf("expected diagnostics model_config_id %d, got %+v", modelConfigID, payload)
		}
		strategy := asMap(t, payload["strategy"])
		if jsonInt(t, strategy["id"]) != strategyID || strategy["type"] != "single" {
			t.Fatalf("expected diagnostics strategy snapshot, got %+v", strategy)
		}
		// Diagnostics analyze every registered OpenAI model-bound operation across
		// both capability dimensions; rows the model does not accept come back with
		// accepted=false rather than being omitted.
		accepted := payload["accepted_operations"].([]any)
		if len(accepted) != 6 {
			t.Fatalf("expected six canonical accepted operations, got %+v", accepted)
		}
		stages := payload["stages"].([]any)
		if len(stages) != 2 {
			t.Fatalf("expected two stages, got %+v", stages)
		}
		modelStage := asMap(t, stages[0])
		if modelStage["stage"] != "model_targets" || modelStage["order"] != float64(1) || modelStage["entered_when"] != "always" {
			t.Fatalf("unexpected model stage contract: %+v", modelStage)
		}
		terminalStage := asMap(t, stages[1])
		if terminalStage["stage"] != "terminal_targets" || terminalStage["order"] != float64(2) || terminalStage["entered_when"] != "model_targets_has_no_eligible_candidate" {
			t.Fatalf("unexpected terminal stage contract: %+v", terminalStage)
		}
		terminalTargets := terminalStage["targets"].([]any)
		if len(terminalTargets) != 1 {
			t.Fatalf("expected one terminal target row, got %+v", terminalTargets)
		}
		target := asMap(t, terminalTargets[0])
		if jsonInt(t, target["access_target_id"]) <= 0 || jsonInt(t, target["authored_stage_position"]) != 0 || target["enabled_strategy_index"] == nil {
			t.Fatalf("unexpected terminal target row contract: %+v", target)
		}
		if jsonInt(t, target["connection_id"]) != connectionID || target["coverage"] != "partial" {
			t.Fatalf("expected dual-model vs responses-only target to be partial, got %+v", target)
		}

		coverageByOperation := map[string]map[string]any{}
		for _, raw := range payload["operation_coverage"].([]any) {
			item := asMap(t, raw)
			coverageByOperation[item["operation_name"].(string)] = item
		}
		chat := coverageByOperation["openai.chat_completions"]
		if chat["accepted"] != true || chat["capability_covered"] != false || chat["statically_routable"] != false || chat["resolved_stage"] != nil {
			t.Fatalf("expected chat operation uncovered with no compatible leaf, got %+v", chat)
		}
		responses := coverageByOperation["openai.responses"]
		if responses["accepted"] != true || responses["capability_covered"] != true || responses["statically_routable"] != true || responses["resolved_stage"] != "terminal_targets" {
			t.Fatalf("expected responses operation routable via terminal stage, got %+v", responses)
		}
		for _, operation := range []string{"openai.responses.input_tokens", "openai.responses.compact"} {
			item := coverageByOperation[operation]
			if item["accepted"] != true || item["statically_routable"] != true {
				t.Fatalf("expected %s to resolve with the responses group, got %+v", operation, item)
			}
		}

		warnings := payload["configuration_warnings"].([]any)
		wantCodes := map[string]bool{}
		for _, raw := range warnings {
			warning := asMap(t, raw)
			wantCodes[warning["code"].(string)] = true
			if warning["model_config_id"] == nil {
				t.Fatalf("expected warning to carry model_config_id, got %+v", warning)
			}
			if strings.Contains(fmt.Sprintf("%+v", warning), "api_key") {
				t.Fatalf("diagnostics warning must not carry secrets, got %+v", warning)
			}
		}
		for _, code := range []string{"openai_target_partial_coverage", "openai_operation_uncovered"} {
			if !wantCodes[code] {
				t.Fatalf("expected warning code %s, got %v", code, wantCodes)
			}
		}
	})

	t.Run("diagnostics has no execution side effects", func(t *testing.T) {
		var logsBefore int
		if err := harness.conn.QueryRow(context.Background(), `SELECT COUNT(*) FROM request_logs`).Scan(&logsBefore); err != nil {
			t.Fatalf("count request logs before diagnostics: %v", err)
		}
		var planningVersionBefore int64
		if err := harness.conn.QueryRow(context.Background(), `SELECT version FROM runtime_cache_generations WHERE domain = 'runtime_planning' AND scope_type = 'global' AND scope_id = '*'`).Scan(&planningVersionBefore); err != nil {
			t.Fatalf("load planning generation before diagnostics: %v", err)
		}

		s15GET[map[string]any](t, harness, profileID, fmt.Sprintf("/api/models/%d/routing-diagnostics", modelConfigID), http.StatusOK)

		var logsAfter int
		if err := harness.conn.QueryRow(context.Background(), `SELECT COUNT(*) FROM request_logs`).Scan(&logsAfter); err != nil {
			t.Fatalf("count request logs after diagnostics: %v", err)
		}
		if logsAfter != logsBefore {
			t.Fatalf("diagnostics must not write request logs, before=%d after=%d", logsBefore, logsAfter)
		}
		var planningVersionAfter int64
		if err := harness.conn.QueryRow(context.Background(), `SELECT version FROM runtime_cache_generations WHERE domain = 'runtime_planning' AND scope_type = 'global' AND scope_id = '*'`).Scan(&planningVersionAfter); err != nil {
			t.Fatalf("load planning generation after diagnostics: %v", err)
		}
		if planningVersionAfter != planningVersionBefore {
			t.Fatalf("diagnostics must not invalidate planning, before=%d after=%d", planningVersionBefore, planningVersionAfter)
		}
	})

	t.Run("model list embeds routing summary", func(t *testing.T) {
		payload := s15GET[[]map[string]any](t, harness, profileID, "/api/models", http.StatusOK)
		found := false
		for _, item := range payload {
			if jsonInt(t, item["id"]) != modelConfigID {
				continue
			}
			found = true
			summary, ok := item["routing_summary"].(map[string]any)
			if !ok {
				t.Fatalf("expected model list item to embed routing_summary, got %+v", item)
			}
			if jsonInt(t, summary["enabled_access_target_count"]) != 1 || jsonInt(t, summary["total_access_target_count"]) != 1 {
				t.Fatalf("expected 1 enabled / 1 total access targets, got %+v", summary)
			}
			if summary["coverage"] != "partial" {
				t.Fatalf("expected partial overall coverage, got %+v", summary)
			}
			groups := summary["operation_groups"].([]any)
			if len(groups) != 2 {
				t.Fatalf("expected two operation groups, got %+v", groups)
			}
		}
		if !found {
			t.Fatalf("expected model list to include diagnostics model %d, got %+v", modelConfigID, payload)
		}
	})
}

func TestEndpointReferencesBatchContract(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "References Strategy")
	firstModelID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "references-owner-a", nil, "native", &strategyID, true)
	secondModelID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "references-owner-b", nil, "native", &strategyID, true)
	usedEndpointID := modelInsertEndpoint(t, harness, profileID, "References Used Endpoint", 0)
	spareEndpointID := modelInsertEndpoint(t, harness, profileID, "References Spare Endpoint", 1)
	emptyEndpointID := modelInsertEndpoint(t, harness, profileID, "References Empty Endpoint", 2)
	firstConnectionID := modelInsertConnection(t, harness, profileID, firstModelID, usedEndpointID, 0, true, nil)
	secondConnectionID := modelInsertConnection(t, harness, profileID, secondModelID, usedEndpointID, 0, true, nil)
	leadingConnectionID := modelInsertConnection(t, harness, profileID, firstModelID, spareEndpointID, 2, true, nil)
	if _, err := harness.conn.Exec(context.Background(), `UPDATE model_access_targets SET position = 3 WHERE source_model_config_id = $1 AND target_connection_id = $2`, firstModelID, firstConnectionID); err != nil {
		t.Fatalf("move first terminal target to a temporary position: %v", err)
	}
	if _, err := harness.conn.Exec(context.Background(), `UPDATE model_access_targets SET position = 0 WHERE source_model_config_id = $1 AND target_connection_id = $2`, firstModelID, leadingConnectionID); err != nil {
		t.Fatalf("seed non-requested leading terminal target position: %v", err)
	}
	if _, err := harness.conn.Exec(context.Background(), `UPDATE model_access_targets SET position = 1 WHERE source_model_config_id = $1 AND target_connection_id = $2`, firstModelID, firstConnectionID); err != nil {
		t.Fatalf("restore requested terminal target position: %v", err)
	}

	t.Run("batch returns direct references in input order without secrets", func(t *testing.T) {
		payload := s15JSON[map[string]any](t, harness, profileID, http.MethodPost, "/api/endpoints/references/batch", map[string]any{"endpoint_ids": []int{emptyEndpointID, usedEndpointID}}, http.StatusOK)
		items := payload["items"].([]any)
		if len(items) != 2 {
			t.Fatalf("expected two items in input order, got %+v", payload)
		}
		spare := asMap(t, items[0])
		if jsonInt(t, spare["endpoint_id"]) != emptyEndpointID {
			t.Fatalf("expected empty endpoint first, got %+v", spare)
		}
		spareSummary := asMap(t, spare["summary"])
		if jsonInt(t, spareSummary["direct_reference_count"]) != 0 {
			t.Fatalf("expected empty endpoint with no direct references, got %+v", spare)
		}
		used := asMap(t, items[1])
		if jsonInt(t, used["endpoint_id"]) != usedEndpointID {
			t.Fatalf("expected used endpoint item second, got %+v", used)
		}
		usedSummary := asMap(t, used["summary"])
		if jsonInt(t, usedSummary["direct_reference_count"]) != 2 ||
			jsonInt(t, usedSummary["referencing_model_count"]) != 2 ||
			jsonInt(t, usedSummary["enabled_reference_count"]) != 2 {
			t.Fatalf("expected two direct/enabled references owned by two models, got %+v", used)
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal references payload: %v", err)
		}
		for _, forbidden := range []string{"api_key", "sk-", "custom_headers", "header_value"} {
			if strings.Contains(string(raw), forbidden) {
				t.Fatalf("references payload must not echo secrets, found %q in %s", forbidden, raw)
			}
		}
	})

	t.Run("missing ids return typed 404", func(t *testing.T) {
		payload := s15JSON[map[string]any](t, harness, profileID, http.MethodPost, "/api/endpoints/references/batch", map[string]any{"endpoint_ids": []int{usedEndpointID, 999_999}}, http.StatusNotFound)
		missing := asMap(t, payload["detail"])
		missingIDs := missing["missing_endpoint_ids"].([]any)
		if len(missingIDs) != 1 || jsonInt(t, missingIDs[0]) != 999_999 {
			t.Fatalf("expected typed 404 with missing endpoint ids, got %+v", payload)
		}
	})

	t.Run("invalid requests are rejected", func(t *testing.T) {
		s15JSON[map[string]any](t, harness, profileID, http.MethodPost, "/api/endpoints/references/batch", map[string]any{"endpoint_ids": []int{}}, http.StatusUnprocessableEntity)
		s15JSON[map[string]any](t, harness, profileID, http.MethodPost, "/api/endpoints/references/batch", map[string]any{"endpoint_ids": []int{1, 1}}, http.StatusUnprocessableEntity)
	})

	t.Run("delete conflict lists the same blockers", func(t *testing.T) {
		blocked := s15JSON[map[string]any](t, harness, profileID, http.MethodDelete, fmt.Sprintf("/api/endpoints/%d", usedEndpointID), nil, http.StatusConflict)
		detail := asMap(t, blocked["detail"])
		if detail["code"] != "endpoint_in_use" {
			t.Fatalf("expected typed endpoint_in_use, got %+v", blocked)
		}
		referencePage := asMap(t, detail["reference_page"])
		references := referencePage["items"].([]any)
		if len(references) != 2 {
			t.Fatalf("expected delete conflict to list both direct references, got %+v", blocked)
		}
		ids := map[int]bool{}
		for _, raw := range references {
			ids[jsonInt(t, asMap(t, raw)["connection_id"])] = true
		}
		if !ids[firstConnectionID] || !ids[secondConnectionID] {
			t.Fatalf("expected delete conflict to include both connections, got %+v", ids)
		}
	})
}

func modelInsertConnectionWithCapability(t *testing.T, harness *contractHarness, profileID int, modelConfigID int, endpointID int, capability string, priority int, isActive bool) int {
	t.Helper()
	var apiFamily string
	if err := harness.conn.QueryRow(context.Background(), `SELECT api_family FROM model_configs WHERE id = $1 AND profile_id = $2`, modelConfigID, profileID).Scan(&apiFamily); err != nil {
		t.Fatalf("load model %d api family: %v", modelConfigID, err)
	}
	connectionID := modelInsertConnectionRowWithCapability(t, harness, profileID, apiFamily, endpointID, capability, priority, isActive)
	modelInsertConnectionTarget(t, harness, profileID, modelConfigID, connectionID, priority, true)
	return connectionID
}

func modelInsertConnectionRowWithCapability(t *testing.T, harness *contractHarness, profileID int, apiFamily string, endpointID int, capability string, priority int, isActive bool) int {
	t.Helper()
	now := time.Now().UTC()
	var openAITextCapability any
	if apiFamily == "openai" {
		openAITextCapability = capability
	}
	var connectionID int
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO connections (profile_id, api_family, endpoint_id, pricing_template_id, qps_limit, max_in_flight_non_stream, max_in_flight_stream, openai_text_capability, is_active, priority, name, auth_type, custom_headers, health_status, health_detail, last_health_check, created_at, updated_at) VALUES ($1, $2, $3, NULL, NULL, NULL, NULL, $8, $4, $5, $6, NULL, NULL, 'healthy', NULL, NULL, $7, $7) RETURNING id`, profileID, apiFamily, endpointID, isActive, priority, fmt.Sprintf("conn-%s-%d", capability, priority), now, openAITextCapability).Scan(&connectionID); err != nil {
		t.Fatalf("insert connection with capability %q: %v", capability, err)
	}
	return connectionID
}
