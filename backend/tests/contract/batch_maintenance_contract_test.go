package contracttest

import (
	"fmt"
	"net/http"
	"testing"
)

func batchCall(t *testing.T, h *contractHarness, action, phase string, body map[string]any, status int) map[string]any {
	t.Helper()
	return requestJSONStatus[map[string]any](t, h, http.MethodPost, "/api/models/batch/"+action+"/"+phase, body, nil, status)
}

func TestBatchMaintenanceAtomicPreviewCAS(t *testing.T) {
	h := newBatchContractHarness(t)
	profile := modelLoadDefaultProfileID(t, h)
	first := openCodeSeedModel(t, h, "batch-first", "openai", "dual_native")
	second := openCodeSeedModel(t, h, "batch-second", "openai", "dual_native")
	strategy := modelInsertLoadbalanceStrategy(t, h, profile, "Batch alternate policy")
	template := insertContractPricingTemplate(t, h, profile, "Batch price")
	var targets []int
	rows, err := h.conn.Query(t.Context(), `SELECT target_connection_id FROM model_access_targets WHERE source_model_config_id=ANY($1) AND target_type='connection' ORDER BY source_model_config_id`, []int{first, second})
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		targets = append(targets, id)
	}
	rows.Close()
	if len(targets) != 2 {
		t.Fatalf("expected two targets, got %v", targets)
	}
	cases := []struct {
		action        string
		ids           []int
		body          map[string]any
		table, column string
	}{
		{"model_limits", []int{first, second}, map[string]any{"client": "opencode"}, "model_catalog_bindings", "override_limit_context"},
		{"model_strategy", []int{first, second}, map[string]any{"reference_id": strategy}, "model_configs", "loadbalance_strategy_id"},
		{"target_pricing", targets, map[string]any{"reference_id": template}, "connections", "pricing_template_id"},
	}
	for _, tc := range cases {
		t.Run(tc.action, func(t *testing.T) {
			items := []map[string]any{{"id": tc.ids[0]}, {"id": tc.ids[1]}}
			if tc.action == "model_limits" {
				for _, item := range items {
					item["context_limit"] = 200000
					item["output_limit"] = 12000
				}
			}
			tc.body["items"] = items
			preview := batchCall(t, h, tc.action, "preview", tc.body, 200)
			if preview["can_apply"] != true {
				t.Fatalf("preview invalid: %+v", preview)
			}
			var before string
			key := "id"
			if tc.action == "model_limits" {
				key = "model_config_id"
			}
			read := func() string {
				var v string
				err := h.conn.QueryRow(t.Context(), fmt.Sprintf(`SELECT COALESCE(%s::text,'NULL') FROM %s WHERE %s=$1`, tc.column, tc.table, key), tc.ids[0]).Scan(&v)
				if err != nil {
					t.Fatal(err)
				}
				return v
			}
			before = read()
			var referenceGeneration int64
			if err := h.conn.QueryRow(t.Context(), `SELECT pricing_reference_generation FROM user_settings WHERE profile_id=$1`, profile).Scan(&referenceGeneration); err != nil {
				t.Fatal(err)
			}

			// A separately committed writer invalidates the second item. The first
			// item must stay untouched, proving validation precedes every write.
			_, err := h.conn.Exec(t.Context(), fmt.Sprintf(`UPDATE %s SET updated_at=updated_at+interval '1 second' WHERE %s=$1`, tc.table, key), tc.ids[1])
			if err != nil {
				t.Fatal(err)
			}
			tc.body["preview_token"] = preview["preview_token"]
			tc.body["confirm_manual_overrides"] = true
			batchCall(t, h, tc.action, "apply", tc.body, 409)
			if got := read(); got != before {
				t.Fatalf("conflict wrote first item: before=%s after=%s", before, got)
			}
			if tc.action != "model_limits" {
				fresh := batchCall(t, h, tc.action, "preview", tc.body, 200)
				referenceTable := "loadbalance_strategies"
				if tc.action == "target_pricing" {
					referenceTable = "pricing_templates"
				}
				_, err := h.conn.Exec(t.Context(), fmt.Sprintf(`UPDATE %s SET name=name || ' changed' WHERE id=$1`, referenceTable), tc.body["reference_id"])
				if err != nil {
					t.Fatal(err)
				}
				tc.body["preview_token"] = fresh["preview_token"]
				batchCall(t, h, tc.action, "apply", tc.body, 409)
				if got := read(); got != before {
					t.Fatal("reference drift wrote first item")
				}
			}
			// A missing item has explicit per-item feedback and cannot partially apply.
			items[1]["id"] = 99999999
			invalid := batchCall(t, h, tc.action, "preview", tc.body, 200)
			if invalid["can_apply"] != false {
				t.Fatal("missing item was applicable")
			}
			tc.body["preview_token"] = invalid["preview_token"]
			batchCall(t, h, tc.action, "apply", tc.body, 422)
			if got := read(); got != before {
				t.Fatal("invalid batch wrote first item")
			}
			items[1]["id"] = tc.ids[1]
			refreshed := batchCall(t, h, tc.action, "preview", tc.body, 200)
			tc.body["preview_token"] = refreshed["preview_token"]
			applied := batchCall(t, h, tc.action, "apply", tc.body, 200)
			if tc.action == "target_pricing" {
				var nextGeneration int64
				if err := h.conn.QueryRow(t.Context(), `SELECT pricing_reference_generation FROM user_settings WHERE profile_id=$1`, profile).Scan(&nextGeneration); err != nil {
					t.Fatal(err)
				}
				if nextGeneration != referenceGeneration+1 {
					t.Fatalf("reference generation not advanced exactly once: %d -> %d", referenceGeneration, nextGeneration)
				}
			}
			if applied["applied"] != true {
				t.Fatalf("apply missing authority: %+v", applied)
			}
			if got := read(); got == before {
				t.Fatalf("apply did not change authoring fact: %s", got)
			}
			for _, raw := range applied["items"].([]any) {
				item := asMap(t, raw)
				state := asMap(t, item["before"])
				field := "context_limit"
				want := 200000
				if tc.action == "model_strategy" {
					field = "loadbalance_strategy_id"
					want = strategy
				}
				if tc.action == "target_pricing" {
					field = "pricing_template_id"
					want = template
				}
				if jsonInt(t, state[field]) != want {
					t.Fatalf("readback is not authoritative: %+v", item)
				}
			}
			_, snapshot, err := h.runtimeCache.LoadFreshDefaultRuntimePlan(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if tc.action == "model_strategy" {
				for _, id := range tc.ids {
					if snapshot.StrategiesByModelID[id].ID != strategy {
						t.Fatal("runtime strategy cache stale")
					}
				}
			}
			if tc.action == "target_pricing" {
				for _, id := range tc.ids {
					target := snapshot.TerminalTargetsByID[id]
					if target.PricingTemplateID == nil || *target.PricingTemplateID != template {
						t.Fatal("runtime pricing cache stale")
					}
				}
			}
			t.Logf("BATCH_ACCEPTANCE action=%s conflict=409 zero_writes=true invalid=422 zero_writes=true applied=2 authoritative_readback=true", tc.action)
		})
	}
	// Replacing an existing manual limit requires an explicit confirmation.
	body := map[string]any{"items": []map[string]any{{"id": first, "context_limit": 250000, "output_limit": 14000}}, "client": "opencode"}
	preview := batchCall(t, h, "model_limits", "preview", body, 200)
	body["preview_token"] = preview["preview_token"]
	batchCall(t, h, "model_limits", "apply", body, 422)
	body["confirm_manual_overrides"] = true
	batchCall(t, h, "model_limits", "apply", body, 200)
	catalog := requestJSONStatus[map[string]any](t, h, http.MethodGet, fmt.Sprintf("/api/models/%d/catalog", first), nil, nil, 200)
	if asMap(t, catalog["override"])["reasoning"] != false {
		t.Fatal("unrelated false override was overwritten")
	}
}
