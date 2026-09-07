package runtimetest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
)

type opencodeProtocolCase struct {
	Name          string `json:"name"`
	Family        string `json:"family"`
	Mode          string `json:"mode,omitempty"`
	ModelID       string `json:"model_id"`
	SDK           string `json:"sdk"`
	Path          string `json:"path"`
	ModelConfigID int    `json:"model_config_id"`
}

// This opt-in boundary requires the actual fixed-version client. The normal
// runtime suite does not silently replace it with a mock when it is absent.
func TestOpenCodeProviderRoundTrip(t *testing.T) {
	binary := os.Getenv("PRISM_OPENCODE_BINARY")
	if binary == "" {
		t.Skip("set PRISM_OPENCODE_BINARY to real OpenCode 1.18.27 for C7 acceptance")
	}
	if !filepath.IsAbs(binary) {
		t.Fatal("PRISM_OPENCODE_BINARY must be an absolute binary path")
	}
	logs := &bytes.Buffer{}
	t.Cleanup(func() { opencodeEvidence(t, "client-run.log", logs.Bytes()) })
	h := newRuntimeHarness(t)
	key := h.enableRuntimeProxyAPIKeyAuth(t)
	loginRuntimeHarness(t, h, "runtime-proxy-user", "runtime-proxy-password-123")
	profileID := h.activeProfileID(t)
	if profileID != 1 {
		t.Fatalf("Default profile = %d", profileID)
	}
	upstream := &opencodeUpstream{}
	server := httptest.NewServer(upstream)
	defer server.Close()
	var ingressMu sync.Mutex
	ingress := []opencodeExchange{}
	gateway := httptest.NewServer(recordOpenCodeIngress(h.server.Config.Handler, &ingressMu, &ingress))
	defer gateway.Close()
	cases := []opencodeProtocolCase{
		{Name: "chat", Family: "openai", Mode: "chat_completions_only", ModelID: "fixture/chat", SDK: "@ai-sdk/openai-compatible", Path: "/v1/chat/completions"},
		{Name: "responses", Family: "openai", Mode: "responses_only", ModelID: "fixture/responses", SDK: "@ai-sdk/openai", Path: "/v1/responses"},
		{Name: "dual", Family: "openai", Mode: "dual_native", ModelID: "fixture/dual", SDK: "@ai-sdk/openai", Path: "/v1/responses"},
		{Name: "anthropic", Family: "anthropic", ModelID: "fixture-anthropic", SDK: "@ai-sdk/anthropic", Path: "/v1/messages"},
		{Name: "gemini", Family: "gemini", ModelID: "fixture-gemini", SDK: "@ai-sdk/google", Path: "/v1beta/models/fixture-gemini:streamGenerateContent?alt=sse"},
	}
	ids := []int{}
	for i := range cases {
		cases[i].ModelConfigID = seedOpenCodeProtocol(t, h, profileID, server.URL, cases[i])
		ids = append(ids, cases[i].ModelConfigID)
	}
	sourceResponse := h.requestJSON(t, http.MethodGet, "/api/models/exports/opencode/source", nil, nil)
	assertStatus(t, sourceResponse, http.StatusOK)
	var source map[string]any
	decodeJSONResponse(t, sourceResponse, &source)
	var loadedModes []any
	var runs []any
	defer func() {
		ingressMu.Lock()
		defer ingressMu.Unlock()
		upstream.mu.Lock()
		defer upstream.mu.Unlock()
		opencodeEvidenceJSON(t, "protocol-exchanges.json", map[string]any{"scope": "real OpenCode through local Prism to controlled loopback upstream; no vendor traffic", "cases": cases, "ingress": ingress, "upstream": upstream.exchanges, "runs": runs})
		opencodeEvidenceJSON(t, "loaded-models.json", loadedModes)
	}()
	for _, manual := range []bool{false, true} {
		mode := "environment"
		if manual {
			mode = "manual"
		}
		render := h.requestJSON(t, http.MethodPost, "/api/models/exports/opencode/render", map[string]any{
			"expected_source_digest": source["source_digest"], "model_config_ids": ids, "base_url": gateway.URL, "provider_id": "prism-acceptance", "credential": map[string]any{"include": manual, "api_key": map[bool]string{true: key, false: ""}[manual]},
		}, nil)
		assertStatus(t, render, http.StatusOK)
		var rendered struct {
			Content string `json:"content"`
		}
		decodeJSONResponse(t, render, &rendered)
		config := filepath.Join(t.TempDir(), "opencode-prism.json")
		if err := os.WriteFile(config, []byte(rendered.Content), 0600); err != nil {
			t.Fatal(err)
		}
		if !manual {
			opencodeEvidence(t, "generated-opencode-prism.json", []byte(rendered.Content))
		}
		opencodeEvidence(t, "generated-"+mode+"-opencode-prism.json", []byte(rendered.Content))
		envKey := key
		if manual {
			envKey = ""
		}
		client := newOpenCodeClient(t, binary, config, envKey, logs)
		upstream.toolFile = filepath.Join(client.root, "work", "controlled-tool-result.txt")
		if err := os.WriteFile(upstream.toolFile, []byte(opencodeToolMarker+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
		version := strings.TrimSpace(client.run("--version"))
		if version != "1.18.27" {
			t.Fatalf("client version %q, want 1.18.27", version)
		}
		opencodeEvidence(t, "client-version.txt", []byte(version+"\n"))
		models := parseOpenCodeModels(t, client.run("models", "prism-acceptance", "--verbose", "--pure"))
		assertOpenCodeRegistered(t, models, cases, gateway.URL)
		loadedModes = append(loadedModes, map[string]any{"credential_mode": mode, "models": models})
		for _, tc := range cases {
			ingressMu.Lock()
			startIngress := len(ingress)
			ingressMu.Unlock()
			upstream.mu.Lock()
			startUpstream := len(upstream.exchanges)
			upstream.mu.Unlock()
			out := client.run("run", "--pure", "--format", "json", "--title", "Prism controlled acceptance", "--model", "prism-acceptance/"+tc.ModelID, "Read the fixture file requested by the tool, then finish with the controlled marker.")
			assertOpenCodeClientResult(t, out)
			ingressMu.Lock()
			gotIngress := append([]opencodeExchange(nil), ingress[startIngress:]...)
			ingressMu.Unlock()
			upstream.mu.Lock()
			gotUpstream := append([]opencodeExchange(nil), upstream.exchanges[startUpstream:]...)
			upstream.mu.Unlock()
			assertOpenCodeRoundTrip(t, tc, key, gotIngress, gotUpstream)
			runs = append(runs, map[string]any{"credential_mode": mode, "model_id": tc.ModelID, "exit_code": 0, "text_before_tool": true, "tool_result_returned": true, "final_text": opencodeFinalMarker, "ingress_count": len(gotIngress), "upstream_count": len(gotUpstream)})
		}
	}
}

func seedOpenCodeProtocol(t *testing.T, h *runtimeHarness, profileID int, upstream string, tc opencodeProtocolCase) int {
	t.Helper()
	release := h.suspendRuntimeSnapshotRefresh()
	id := h.seedModel(t, profileID, tc.Family, tc.ModelID, "native", nil)
	var mode *string
	if tc.Mode != "" {
		mode = &tc.Mode
		h.setModelOpenAIAcceptedFormat(t, profileID, tc.ModelID, tc.Mode)
	}
	endpoint := h.seedEndpoint(t, profileID, "opencode-"+tc.Name, upstream, "controlled-upstream-key")
	connection := h.seedConnectionWithOpenAITextCapability(t, profileID, id, endpoint, "opencode-"+tc.Name, nil, nil, 0, mode)
	if _, err := h.conn.Exec(context.Background(), `UPDATE connections SET upstream_model_id=$2 WHERE id=$1`, connection, "upstream-"+tc.Name); err != nil {
		t.Fatal(err)
	}
	if _, err := h.conn.Exec(context.Background(), `INSERT INTO model_catalog_bindings (model_config_id,provider_id,catalog_model_id,match_source,catalog_revision,fetched_at,source_name,source_limit_context,source_limit_input,source_limit_output,source_reasoning,source_tool_call,source_temperature,source_modalities_input,source_modalities_output,override_reasoning,updated_at) VALUES ($1,'controlled-catalog',$2,'manual','controlled-revision',NOW(),$3,32768,30000,2048,TRUE,TRUE,FALSE,'["text"]','["text"]',FALSE,NOW())`, id, "catalog-"+tc.Name, "Catalog "+tc.Name); err != nil {
		t.Fatal(err)
	}
	if _, err := h.conn.Exec(context.Background(), `UPDATE model_configs SET display_name=$2 WHERE id=$1`, id, "Prism "+tc.Name+" {env:PRISM_ABSENT} {file:missing-controlled-file}"); err != nil {
		t.Fatal(err)
	}
	price := insertRuntimePricingTemplate(t, h.conn, profileID, "opencode-"+tc.Name, "USD", "1", "2", "0.1", "0.2", "2")
	attachRuntimeConnectionPricingTemplate(t, h, connection, price)
	release()
	h.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})
	return id
}

func assertOpenCodeRegistered(t *testing.T, models []map[string]any, cases []opencodeProtocolCase, origin string) {
	t.Helper()
	if len(models) != len(cases) {
		t.Fatalf("registered %d models, expected %d: %+v", len(models), len(cases), models)
	}
	for _, tc := range cases {
		var found map[string]any
		for _, model := range models {
			if model["id"] == tc.ModelID {
				found = model
			}
		}
		if found == nil {
			t.Fatalf("real client did not register %q", tc.ModelID)
		}
		api, _ := found["api"].(map[string]any)
		path := "/v1"
		if tc.Family == "gemini" {
			path = "/v1beta"
		}
		if api["npm"] != tc.SDK || api["url"] != origin+path || api["id"] != tc.ModelID {
			t.Fatalf("actual client SDK/URL/ID mismatch: %+v", found)
		}
		limit, _ := found["limit"].(map[string]any)
		if limit["context"] != float64(32768) || limit["output"] != float64(2048) || limit["input"] != float64(30000) {
			t.Fatalf("actual client limits mismatch: %+v", found)
		}
		if found["name"] != "Prism "+tc.Name+" {env:PRISM_ABSENT} {file:missing-controlled-file}" {
			t.Fatalf("actual client name mismatch: %+v", found)
		}
		capabilities, _ := found["capabilities"].(map[string]any)
		if capabilities["reasoning"] != false || capabilities["toolcall"] != true || capabilities["temperature"] != false {
			t.Fatalf("actual client capabilities mismatch: %+v", found)
		}
		cost, _ := found["cost"].(map[string]any)
		cache, _ := cost["cache"].(map[string]any)
		if cost["input"] != float64(1) || cost["output"] != float64(2) || cache["read"] != 0.1 || cache["write"] != 0.2 {
			t.Fatalf("actual client cost mismatch: %+v", found)
		}
	}
}

func assertOpenCodeRoundTrip(t *testing.T, tc opencodeProtocolCase, key string, ingress, upstream []opencodeExchange) {
	t.Helper()
	if len(ingress) != 2 || len(upstream) != 2 {
		t.Fatalf("%s expected tool and continuation requests: ingress=%d upstream=%d", tc.Name, len(ingress), len(upstream))
	}
	for i, exchange := range ingress {
		var body map[string]any
		if err := json.Unmarshal(exchange.Body, &body); err != nil {
			t.Fatal(err)
		}
		if tc.Family != "gemini" && body["stream"] != true {
			t.Fatalf("%s client did not request streaming", tc.Name)
		}
		if (tc.Name == "responses" || tc.Name == "dual") && body["store"] != false {
			t.Fatalf("%s client default store must be false", tc.Name)
		}

		if exchange.Path != tc.Path {
			t.Fatalf("%s ingress path %s, expected %s", tc.Name, exchange.Path, tc.Path)
		}
		if exchange.Authorization != "Bearer "+key && exchange.APIKey != key && exchange.GoogleKey != key {
			t.Fatalf("%s ingress missing synthetic auth", tc.Name)
		}
		if tc.Family != "gemini" && !bytes.Contains(exchange.Body, []byte(`"model":"`+tc.ModelID+`"`)) {
			t.Fatalf("ingress model ID lost: %s", exchange.Body)
		}
		if (i == 1) != exchange.ToolResult {
			t.Fatalf("%s ingress tool-result boundary mismatch", tc.Name)
		}
	}
	for i, exchange := range upstream {
		wantPath := tc.Path
		if tc.Family == "gemini" {
			wantPath = "/v1beta/models/upstream-gemini:streamGenerateContent?alt=sse"
		}
		if exchange.Path != wantPath {
			t.Fatalf("%s upstream path %s expected %s", tc.Name, exchange.Path, wantPath)
		}
		if exchange.Authorization != "Bearer controlled-upstream-key" && exchange.APIKey != "controlled-upstream-key" && exchange.GoogleKey != "controlled-upstream-key" {
			t.Fatalf("%s upstream auth mismatch", tc.Name)
		}
		if tc.Family != "gemini" && !bytes.Contains(exchange.Body, []byte(fmt.Sprintf(`"model":"upstream-%s"`, tc.Name))) {
			t.Fatalf("upstream model mapping missing: %s", exchange.Body)
		}
		if (i == 1) != exchange.ToolResult {
			t.Fatalf("%s upstream tool-result boundary mismatch", tc.Name)
		}
	}
}
