package contracttest

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

var invalidUpstreamModelIDs = []struct {
	name, reason string
	value        any
	limit        int
}{
	{name: "null", value: nil, reason: "required"},
	{name: "blank", value: " \t\n ", reason: "required"},
	{name: "too-long", value: strings.Repeat("长", 201), reason: "too_long", limit: 200},
}

// TestUpstreamModelIDContract covers both owner-scoped create chains, PATCH,
// copy, rename, persistence, and complete/nested Terminal Target projections.
func TestUpstreamModelIDContract(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "S31 Upstream Model Strategy")
	ownerID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "s31-upstream-owner", nil, "native", &strategyID, true)
	endpointID := modelInsertEndpoint(t, harness, profileID, "S31 Upstream Endpoint")
	createPath := fmt.Sprintf("/api/models/%d/connections", ownerID)
	base := map[string]any{"endpoint_id": endpointID, "openai_text_capability": "dual_native"}

	defaulted := upstreamConnectionMutation(t, harness, profileID, http.MethodPost, createPath, base, http.StatusCreated)
	defaultID := jsonInt(t, defaulted["id"])
	assertUpstreamValue(t, defaulted, "s31-upstream-owner")
	assertStoredUpstreamValue(t, harness, defaultID, "s31-upstream-owner")
	assertUpstreamReadProjection(t, harness, profileID, ownerID, defaultID, "s31-upstream-owner")

	explicitBody := map[string]any{"endpoint_id": endpointID, "openai_text_capability": "dual_native", "upstream_model_id": "  Vendor/Internal Model-X  "}
	explicit := upstreamConnectionMutation(t, harness, profileID, http.MethodPost, createPath, explicitBody, http.StatusCreated)
	assertUpstreamValue(t, explicit, "Vendor/Internal Model-X")
	assertStoredUpstreamValue(t, harness, jsonInt(t, explicit["id"]), "Vendor/Internal Model-X")

	composite := func(modelID string, value any, include bool, status int) map[string]any {
		initial := map[string]any{"endpoint_id": endpointID}
		if include {
			initial["upstream_model_id"] = value
		}
		return modelJSON[map[string]any](t, harness, profileID, http.MethodPost, "/api/models", map[string]any{
			"api_family": "openai", "model_id": modelID, "loadbalance_strategy_id": strategyID,
			"openai_accepted_format": "dual_native", "initial_terminal_target": initial,
		}, status)
	}
	rejectedCompositeIndex := 0
	for _, surface := range []struct {
		name string
		call func(any) map[string]any
	}{
		{name: "owner create", call: func(value any) map[string]any {
			body := map[string]any{"endpoint_id": endpointID, "openai_text_capability": "dual_native", "upstream_model_id": value}
			return modelJSON[map[string]any](t, harness, profileID, http.MethodPost, createPath, body, http.StatusUnprocessableEntity)
		}},
		{name: "composite create", call: func(value any) map[string]any {
			rejectedCompositeIndex++
			return composite(fmt.Sprintf("s31-composite-rejected-%d", rejectedCompositeIndex), value, true, http.StatusUnprocessableEntity)
		}},
	} {
		t.Run(surface.name+" rejects invalid identities atomically", func(t *testing.T) {
			for _, invalid := range invalidUpstreamModelIDs {
				before := upstreamContractCounts(t, harness, profileID)
				assertUpstreamFieldError(t, surface.call(invalid.value), invalid.reason, invalid.limit)
				if after := upstreamContractCounts(t, harness, profileID); after != before {
					t.Fatalf("%s %s wrote rows: before=%v after=%v", surface.name, invalid.name, before, after)
				}
			}
		})
	}

	patchID := jsonInt(t, upstreamConnectionMutation(t, harness, profileID, http.MethodPost, createPath, map[string]any{
		"endpoint_id": endpointID, "openai_text_capability": "dual_native", "upstream_model_id": "s31-patch-keeper",
	}, http.StatusCreated)["id"])
	patchPath := fmt.Sprintf("/api/models/%d/connections/%d", ownerID, patchID)
	assertUpstreamValue(t, upstreamConnectionMutation(t, harness, profileID, http.MethodPatch, patchPath, map[string]any{"name": "renamed"}, http.StatusOK), "s31-patch-keeper")
	patchBefore := loadStoredUpstreamValue(t, harness, patchID)
	for _, invalid := range invalidUpstreamModelIDs {
		body := modelJSON[map[string]any](t, harness, profileID, http.MethodPatch, patchPath, map[string]any{"upstream_model_id": invalid.value}, http.StatusUnprocessableEntity)
		assertUpstreamFieldError(t, body, invalid.reason, invalid.limit)
		if after := loadStoredUpstreamValue(t, harness, patchID); after != patchBefore {
			t.Fatalf("invalid PATCH %s changed row: before=%+v after=%+v", invalid.name, patchBefore, after)
		}
	}
	assertUpstreamValue(t, upstreamConnectionMutation(t, harness, profileID, http.MethodPatch, patchPath, map[string]any{"upstream_model_id": "s31-patch-replacement"}, http.StatusOK), "s31-patch-replacement")

	for _, created := range []struct {
		envelope map[string]any
		want     string
	}{
		{envelope: composite("s31-composite-default", nil, false, http.StatusCreated), want: "s31-composite-default"},
		{envelope: composite("s31-composite-explicit", "  vendor/Model Explicit  ", true, http.StatusCreated), want: "vendor/Model Explicit"},
	} {
		connection := asMap(t, created.envelope["connection"])
		assertUpstreamValue(t, connection, created.want)
		assertModelPayloadUpstreamProjection(t, asMap(t, created.envelope["model"]), jsonInt(t, connection["id"]), created.want)
		assertStoredUpstreamValue(t, harness, jsonInt(t, connection["id"]), created.want)
	}

	destinationID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "s31-upstream-destination", nil, "native", &strategyID, true)
	source := upstreamConnectionMutation(t, harness, profileID, http.MethodPost, createPath, map[string]any{
		"endpoint_id": endpointID, "openai_text_capability": "dual_native", "upstream_model_id": "s31-source-upstream",
	}, http.StatusCreated)
	copyBody := modelJSON[map[string]any](t, harness, profileID, http.MethodPost, fmt.Sprintf("/api/models/%d/connections/%d/copies", ownerID, jsonInt(t, source["id"])), map[string]any{"destination_model_config_ids": []int{destinationID}}, http.StatusCreated)
	summary := asMap(t, asMap(t, copyBody["items"].([]any)[0])["connection_summary"])
	copiedID := jsonInt(t, summary["id"])
	assertUpstreamValue(t, summary, "s31-source-upstream")
	assertStoredUpstreamValue(t, harness, copiedID, "s31-source-upstream")
	assertUpstreamReadProjection(t, harness, profileID, destinationID, copiedID, "s31-source-upstream")

	renameID := jsonInt(t, upstreamConnectionMutation(t, harness, profileID, http.MethodPost, createPath, base, http.StatusCreated)["id"])
	modelJSON[map[string]any](t, harness, profileID, http.MethodPut, fmt.Sprintf("/api/models/%d", ownerID), map[string]any{"model_id": "s31-upstream-owner-renamed"}, http.StatusOK)
	assertStoredUpstreamValue(t, harness, renameID, "s31-upstream-owner")
	newAfterRename := upstreamConnectionMutation(t, harness, profileID, http.MethodPost, createPath, base, http.StatusCreated)
	assertUpstreamValue(t, newAfterRename, "s31-upstream-owner-renamed")
}

func upstreamConnectionMutation(t *testing.T, harness *contractHarness, profileID int, method, path string, body any, status int) map[string]any {
	t.Helper()
	return asMap(t, modelJSON[map[string]any](t, harness, profileID, method, path, body, status)["connection"])
}

func upstreamContractCounts(t *testing.T, harness *contractHarness, profileID int) (counts [3]int) {
	t.Helper()
	if err := harness.conn.QueryRow(context.Background(), `SELECT (SELECT COUNT(*) FROM model_configs WHERE profile_id=$1), (SELECT COUNT(*) FROM connections WHERE profile_id=$1), (SELECT COUNT(*) FROM model_access_targets WHERE profile_id=$1)`, profileID).Scan(&counts[0], &counts[1], &counts[2]); err != nil {
		t.Fatalf("load upstream contract counts: %v", err)
	}
	return counts
}

type storedUpstreamValue struct {
	value     string
	updatedAt time.Time
}

func loadStoredUpstreamValue(t *testing.T, harness *contractHarness, connectionID int) storedUpstreamValue {
	t.Helper()
	var got *string
	var stored storedUpstreamValue
	if err := harness.conn.QueryRow(context.Background(), `SELECT upstream_model_id, updated_at FROM connections WHERE id=$1`, connectionID).Scan(&got, &stored.updatedAt); err != nil || got == nil {
		t.Fatalf("load stored upstream_model_id for connection %d: value=%+v err=%v", connectionID, got, err)
	}
	stored.value = *got
	return stored
}

func assertStoredUpstreamValue(t *testing.T, harness *contractHarness, connectionID int, want string) {
	t.Helper()
	if got := loadStoredUpstreamValue(t, harness, connectionID).value; got != want {
		t.Fatalf("stored upstream_model_id for connection %d = %q, want %q", connectionID, got, want)
	}
}

func assertUpstreamValue(t *testing.T, payload map[string]any, want string) {
	t.Helper()
	if payload["upstream_model_id"] != want {
		t.Fatalf("upstream_model_id = %+v, want %q", payload["upstream_model_id"], want)
	}
}

func assertUpstreamReadProjection(t *testing.T, harness *contractHarness, profileID, modelID, connectionID int, want string) {
	t.Helper()
	connection := modelJSON[map[string]any](t, harness, profileID, http.MethodGet, fmt.Sprintf("/api/connections/%d", connectionID), nil, http.StatusOK)
	assertUpstreamValue(t, connection, want)
	assertListed := func(items []map[string]any) {
		for _, item := range items {
			if jsonInt(t, item["id"]) == connectionID {
				assertUpstreamValue(t, item, want)
				return
			}
		}
		t.Fatalf("connection %d missing from list", connectionID)
	}
	for _, path := range []string{"/api/connections", fmt.Sprintf("/api/models/%d/connections", modelID)} {
		assertListed(modelJSON[[]map[string]any](t, harness, profileID, http.MethodGet, path, nil, http.StatusOK))
	}
	batch := modelJSON[map[string]any](t, harness, profileID, http.MethodPost, "/api/models/connections/batch", map[string]any{"model_config_ids": []int{modelID}}, http.StatusOK)
	batchConnections := asMap(t, batch["items"].([]any)[0])["connections"].([]any)
	assertUpstreamValue(t, asMap(t, batchConnections[0]), want)
	model := modelJSON[map[string]any](t, harness, profileID, http.MethodGet, fmt.Sprintf("/api/models/%d", modelID), nil, http.StatusOK)
	assertModelPayloadUpstreamProjection(t, model, connectionID, want)
	for _, listed := range modelJSON[[]map[string]any](t, harness, profileID, http.MethodGet, "/api/models", nil, http.StatusOK) {
		if jsonInt(t, listed["id"]) == modelID {
			assertModelPayloadUpstreamProjection(t, listed, connectionID, want)
			return
		}
	}
	t.Fatalf("model %d missing from model list", modelID)
}

func assertModelPayloadUpstreamProjection(t *testing.T, model map[string]any, connectionID int, want string) {
	t.Helper()
	for _, raw := range model["access_targets"].([]any) {
		target := asMap(t, raw)
		if target["connection_id"] != nil && jsonInt(t, target["connection_id"]) == connectionID {
			assertUpstreamValue(t, asMap(t, target["connection"]), want)
			assertUpstreamValue(t, asMap(t, target["terminal_target"]), want)
			return
		}
	}
	t.Fatalf("connection %d missing from model access targets", connectionID)
}

func assertUpstreamFieldError(t *testing.T, body map[string]any, reason string, limit int) {
	t.Helper()
	if body["field"] != "upstream_model_id" || body["path"] != "upstream_model_id" || body["reason"] != reason {
		t.Fatalf("unexpected upstream_model_id error: %+v", body)
	}
	if (limit > 0 && jsonInt(t, body["limit"]) != limit) || (limit == 0 && body["limit"] != nil) {
		t.Fatalf("unexpected upstream_model_id limit: %+v", body)
	}
}
