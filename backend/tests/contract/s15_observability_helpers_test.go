package contract_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

const updateS15GoldensEnv = "PRISM_UPDATE_S15_OBSERVABILITY_GOLDENS"

func assertJSONIntFields(t *testing.T, payload map[string]any, want map[string]int) {
	t.Helper()
	for key, expected := range want {
		if got := jsonInt(t, payload[key]); got != expected {
			t.Fatalf("expected %s=%d, got %+v", key, expected, payload)
		}
	}
}

func assertS15GoldenJSON(t *testing.T, name string, payload any) {
	t.Helper()
	actual, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		t.Fatalf("marshal golden payload %s: %v", name, err)
	}
	path := s15GoldenPath(name)
	if os.Getenv(updateS15GoldensEnv) == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create golden directory for %s: %v", path, err)
		}
		if err := os.WriteFile(path, append(bytes.TrimRight(actual, "\n"), '\n'), 0o644); err != nil {
			t.Fatalf("update golden %s: %v", path, err)
		}
	}
	expected, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v", path, err)
	}
	expected = bytes.TrimRight(expected, "\n")
	actual = bytes.TrimRight(actual, "\n")
	if !bytes.Equal(actual, expected) {
		t.Fatalf("golden %s mismatch\nexpected: %s\nactual:   %s", name, expected, actual)
	}
}

func s15GoldenPath(name string) string {
	return filepath.Join("testdata", "s15_observability", name)
}

func s15DashboardShapeProjection(payload map[string]any) map[string]any {
	return map[string]any{
		"keys":                    s15PresentKeys(payload),
		"legacy_keys_present":     s15PresentKeysFrom(payload, "window", "covers", "freshness", "metrics", "recent_requests"),
		"strategy_family_present": len(s15PresentKeysFrom(payload, "strategy_family_summary")) > 0,
	}
}

func s15UsageSnapshotProjection(t *testing.T, payload map[string]any) map[string]any {
	t.Helper()
	return map[string]any{
		"currency":                 payload["currency"],
		"time_range":               payload["time_range"],
		"overview":                 payload["overview"],
		"cost_overview":            payload["cost_overview"],
		"endpoint_statistics":      payload["endpoint_statistics"],
		"model_statistics":         payload["model_statistics"],
		"proxy_api_key_statistics": payload["proxy_api_key_statistics"],
	}
}

func s15DashboardSnapshotProjection(t *testing.T, payload map[string]any) map[string]any {
	t.Helper()
	routingHealthMap := asMap(t, payload["routing_health_map"])
	return map[string]any{
		"shape":               s15DashboardShapeProjection(payload),
		"coverage_24h":        s15CoveragePresenceProjection(t, payload["coverage_24h"]),
		"coverage_30d":        s15CoveragePresenceProjection(t, payload["coverage_30d"]),
		"health":              payload["health"],
		"metric_snapshot":     payload["metric_snapshot"],
		"api_family_rows":     payload["api_family_rows"],
		"top_spending_models": payload["top_spending_models"],
		"routing_health_map": map[string]any{
			"node_count":    len(routingHealthMap["nodes"].([]any)),
			"link_count":    len(routingHealthMap["links"].([]any)),
			"endpointCount": jsonInt(t, routingHealthMap["endpointCount"]),
			"modelCount":    jsonInt(t, routingHealthMap["modelCount"]),
		},
	}
}

func s15TopologyProjection(t *testing.T, payload map[string]any, disabledModelID int, endpointID int, terminalTargetID int, modelToModelEdgeID int, modelToTerminalEdgeID int) map[string]any {
	t.Helper()
	topologyGraph := asMap(t, payload["topology_graph"])
	nodes := topologyGraph["nodes"].([]any)
	nodesByID := make(map[string]map[string]any, len(nodes))
	for _, raw := range nodes {
		node := asMap(t, raw)
		nodesByID[node["id"].(string)] = node
	}
	edges := topologyGraph["edges"].([]any)
	edgesByID := make(map[string]map[string]any, len(edges))
	for _, raw := range edges {
		edge := asMap(t, raw)
		edgesByID[edge["id"].(string)] = edge
	}

	disabledNode := maps.Clone(nodesByID[fmt.Sprintf("model-%d", disabledModelID)])
	disabledNode["id"] = "model-<disabled>"
	disabledNode["model_config_id"] = "<disabled-model-id>"

	terminalNode := maps.Clone(nodesByID[fmt.Sprintf("terminal-target-%d", terminalTargetID)])
	terminalNode["id"] = "terminal-target-<terminal>"
	terminalNode["connection_id"] = "<terminal-target-id>"
	terminalNode["terminal_target_id"] = "<terminal-target-id>"
	if terminalNode["last_request_at"] != nil {
		terminalNode["last_request_at"] = "<timestamp>"
	}

	endpointNode := maps.Clone(nodesByID[fmt.Sprintf("endpoint-%d", endpointID)])
	endpointNode["id"] = "endpoint-<endpoint>"
	endpointNode["endpoint_id"] = "<endpoint-id>"
	endpointNode["sublabel"] = "Endpoint <endpoint-id>"

	modelToModelEdge := maps.Clone(edgesByID[fmt.Sprintf("access-target-%d", modelToModelEdgeID)])
	modelToModelEdge["id"] = "access-target-<model-to-model>"
	modelToModelEdge["source_node_id"] = "model-<entry>"
	modelToModelEdge["source_model_config_id"] = "<entry-model-id>"
	modelToModelEdge["target_node_id"] = "model-<terminal>"
	modelToModelEdge["target_model_config_id"] = "<terminal-model-id>"

	modelToTerminalEdge := maps.Clone(edgesByID[fmt.Sprintf("access-target-%d", modelToTerminalEdgeID)])
	modelToTerminalEdge["id"] = "access-target-<model-to-terminal>"
	modelToTerminalEdge["connection_id"] = "<terminal-target-id>"
	modelToTerminalEdge["source_node_id"] = "model-<terminal>"
	modelToTerminalEdge["source_model_config_id"] = "<terminal-model-id>"
	modelToTerminalEdge["target_node_id"] = "terminal-target-<terminal>"
	modelToTerminalEdge["terminal_target_id"] = "<terminal-target-id>"

	bindingEdge := maps.Clone(edgesByID[fmt.Sprintf("terminal-target-binding-%d", terminalTargetID)])
	bindingEdge["connection_id"] = "<terminal-target-id>"
	bindingEdge["terminal_target_id"] = "<terminal-target-id>"
	bindingEdge["id"] = "terminal-target-binding-<terminal>"
	bindingEdge["source_node_id"] = "terminal-target-<terminal>"
	bindingEdge["target_node_id"] = "endpoint-<endpoint>"
	bindingEdge["endpoint_id"] = "<endpoint-id>"

	return map[string]any{
		"shape": s15DashboardShapeProjection(payload),
		"stats": topologyGraph["stats"],
		"nodes": map[string]any{
			"disabled_model":  disabledNode,
			"endpoint":        endpointNode,
			"terminal_target": terminalNode,
		},
		"edges": map[string]any{
			"model_to_model":     modelToModelEdge,
			"model_to_terminal":  modelToTerminalEdge,
			"terminal_to_target": bindingEdge,
		},
	}
}

func s15CoveragePresenceProjection(t *testing.T, raw any) map[string]any {
	t.Helper()
	coverage := asMap(t, raw)
	return map[string]any{
		"from_present": coverage["from"] != nil,
		"to_present":   coverage["to"] != nil,
	}
}

func s15PresentKeys(payload map[string]any) []string {
	keys := make([]string, 0, len(payload))
	for key := range payload {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func s15PresentKeysFrom(payload map[string]any, keys ...string) []string {
	present := make([]string, 0, len(keys))
	for _, key := range keys {
		if _, ok := payload[key]; ok {
			present = append(present, key)
		}
	}
	return present
}

func assertS15UsageSnapshotTokenTotals(t *testing.T, payload map[string]any, wantInput int, wantOutput int, wantTotal int, wantCached int, wantReasoning int) {
	t.Helper()
	overview := asMap(t, payload["overview"])
	assertJSONIntFields(t, overview, map[string]int{
		"input_tokens":     wantInput,
		"output_tokens":    wantOutput,
		"total_tokens":     wantTotal,
		"cached_tokens":    wantCached,
		"reasoning_tokens": wantReasoning,
	})
	wantTotals := s15UsageTokenTotals{inputTokens: wantInput, outputTokens: wantOutput, totalTokens: wantTotal, cachedTokens: wantCached, reasoningTokens: wantReasoning}
	if got := s15AllModelHourlyTokenTrendTotals(t, payload); got != wantTotals {
		t.Fatalf("expected token trend %+v, got %+v", wantTotals, got)
	}
	wantBreakdown := s15UsageTokenTotals{inputTokens: wantInput, outputTokens: wantOutput, cachedTokens: wantCached, reasoningTokens: wantReasoning}
	if got := s15HourlyTokenBreakdownTotals(t, payload); got != wantBreakdown {
		t.Fatalf("expected token breakdown %+v, got %+v", wantBreakdown, got)
	}
	if got := s15UsageModelTokenTotals(t, payload); got != wantTotals {
		t.Fatalf("expected model stats %+v, got %+v", wantTotals, got)
	}
}

type s15UsageTokenTotals struct {
	inputTokens     int
	outputTokens    int
	totalTokens     int
	cachedTokens    int
	reasoningTokens int
}

func s15AllModelHourlyTokenTrendTotals(t *testing.T, payload map[string]any) s15UsageTokenTotals {
	t.Helper()
	trends := asMap(t, payload["token_usage_trends"])
	for _, rawSeries := range trends["hourly"].([]any) {
		series := asMap(t, rawSeries)
		if series["key"] != "all" {
			continue
		}
		totals := s15UsageTokenTotals{}
		for _, rawPoint := range series["points"].([]any) {
			point := asMap(t, rawPoint)
			totals.inputTokens += jsonInt(t, point["input_tokens"])
			totals.outputTokens += jsonInt(t, point["output_tokens"])
			totals.totalTokens += jsonInt(t, point["total_tokens"])
			totals.cachedTokens += jsonInt(t, point["cached_tokens"])
			totals.reasoningTokens += jsonInt(t, point["reasoning_tokens"])
		}
		return totals
	}
	return s15UsageTokenTotals{}
}

func s15HourlyTokenBreakdownTotals(t *testing.T, payload map[string]any) s15UsageTokenTotals {
	t.Helper()
	breakdown := asMap(t, payload["token_type_breakdown"])
	totals := s15UsageTokenTotals{}
	for _, rawPoint := range breakdown["hourly"].([]any) {
		point := asMap(t, rawPoint)
		totals.inputTokens += jsonInt(t, point["input_tokens"])
		totals.outputTokens += jsonInt(t, point["output_tokens"])
		totals.cachedTokens += jsonInt(t, point["cached_tokens"])
		totals.reasoningTokens += jsonInt(t, point["reasoning_tokens"])
	}
	return totals
}

func s15UsageModelTokenTotals(t *testing.T, payload map[string]any) s15UsageTokenTotals {
	t.Helper()
	totals := s15UsageTokenTotals{}
	for _, rawModel := range payload["model_statistics"].([]any) {
		model := asMap(t, rawModel)
		totals.inputTokens += jsonInt(t, model["input_tokens"])
		totals.outputTokens += jsonInt(t, model["output_tokens"])
		totals.totalTokens += jsonInt(t, model["total_tokens"])
		totals.cachedTokens += jsonInt(t, model["cached_tokens"])
		totals.reasoningTokens += jsonInt(t, model["reasoning_tokens"])
	}
	return totals
}

func s15EndpointStatisticLabelsByID(t *testing.T, payload map[string]any) map[int]string {
	t.Helper()
	items := payload["endpoint_statistics"].([]any)
	labels := make(map[int]string, len(items))
	for _, raw := range items {
		item := asMap(t, raw)
		labels[jsonInt(t, item["endpoint_id"])] = item["endpoint_label"].(string)
	}
	return labels
}

func s15TopSpendingEndpointLabelsByID(t *testing.T, payload map[string]any) map[int]string {
	t.Helper()
	items := payload["top_spending_endpoints"].([]any)
	labels := make(map[int]string, len(items))
	for _, raw := range items {
		item := asMap(t, raw)
		labels[jsonInt(t, item["endpoint_id"])] = item["endpoint_label"].(string)
	}
	return labels
}

func assertUsageSnapshotRESTContract(t *testing.T, payload map[string]any, preset string) {
	t.Helper()
	for _, field := range []string{
		"generated_at",
		"time_range",
		"currency",
		"overview",
		"request_trends",
		"latency_trends",
		"token_usage_trends",
		"token_type_breakdown",
		"cost_overview",
		"endpoint_statistics",
		"model_statistics",
		"proxy_api_key_statistics",
	} {
		if _, ok := payload[field]; !ok {
			t.Fatalf("expected usage snapshot preset %s to include %q, got %+v", preset, field, payload)
		}
	}
	if _, ok := payload["service_health"]; ok {
		t.Fatalf("expected usage snapshot preset %s to omit top-level service_health, got %+v", preset, payload)
	}
}

func s15UsageSeed(id int, profileID int, ingressRequestID string, modelID string, createdAt time.Time, mutate func(*usageEventSeed)) usageEventSeed {
	seed := usageEventSeed{
		ID:               id,
		ProfileID:        profileID,
		IngressRequestID: ingressRequestID,
		ModelID:          modelID,
		APIFamily:        "openai",
		StatusCode:       http.StatusOK,
		SuccessFlag:      true,
		BillableFlag:     boolPtr(true),
		PricedFlag:       boolPtr(true),
		AttemptCount:     1,
		RequestPath:      "/v1/chat/completions",
		CreatedAt:        createdAt,
	}
	if mutate != nil {
		mutate(&seed)
	}
	return seed
}
