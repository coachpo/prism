package runtime

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coachpo/prism/backend/internal/providerauth"
)

// setSnapshotUpstreamModelID replaces the whole runtimeConnection in the map
// because Go cannot assign to a field of a struct stored in a map.
func setSnapshotUpstreamModelID(snapshot *planningSnapshot, connectionID int, upstreamModelID string) {
	connection := snapshot.TerminalTargetsByID[connectionID]
	connection.UpstreamModelID = stringPtr(upstreamModelID)
	snapshot.TerminalTargetsByID[connectionID] = connection
}

// TestUpstreamModelIDPerAttemptRewrite is registry-complete: every model-bound
// operation must have a scenario, and streaming-capable body-bound operations
// also exercise both streaming and non-streaming intent.
func TestUpstreamModelIDPerAttemptRewrite(t *testing.T) {
	scenarios := []struct {
		name, apiFamily, path, body string
		stream                      bool
	}{
		{name: "openai chat non-stream", apiFamily: "openai", path: "/v1/chat/completions", body: `{"model":"entry-model","messages":[]}`},
		{name: "openai chat stream", apiFamily: "openai", path: "/v1/chat/completions", body: `{"model":"entry-model","stream":true,"messages":[]}`, stream: true},
		{name: "openai responses non-stream", apiFamily: "openai", path: "/v1/responses", body: `{"model":"entry-model","input":"hi"}`},
		{name: "openai responses stream", apiFamily: "openai", path: "/v1/responses", body: `{"model":"entry-model","stream":true,"input":"hi"}`, stream: true},
		{name: "openai responses input tokens", apiFamily: "openai", path: "/v1/responses/input_tokens", body: `{"model":"entry-model","input":"hi"}`},
		{name: "openai responses compact", apiFamily: "openai", path: "/v1/responses/compact", body: `{"model":"entry-model","input":"hi"}`},
		{name: "openai images generations", apiFamily: "openai", path: "/v1/images/generations", body: `{"model":"entry-model","prompt":"hi"}`},
		{name: "openai images edits", apiFamily: "openai", path: "/v1/images/edits", body: `{"model":"entry-model","prompt":"hi"}`},
		{name: "anthropic messages non-stream", apiFamily: "anthropic", path: "/v1/messages", body: `{"model":"entry-model","max_tokens":1,"messages":[]}`},
		{name: "anthropic messages stream", apiFamily: "anthropic", path: "/v1/messages", body: `{"model":"entry-model","stream":true,"max_tokens":1,"messages":[]}`, stream: true},
		{name: "anthropic count tokens", apiFamily: "anthropic", path: "/v1/messages/count_tokens", body: `{"model":"entry-model","messages":[]}`},
		{name: "gemini generate content", apiFamily: "gemini", path: "/v1beta/models/entry-model:generateContent", body: `{"contents":[]}`},
		{name: "gemini stream generate content", apiFamily: "gemini", path: "/v1beta/models/entry-model:streamGenerateContent", body: `{"contents":[]}`, stream: true},
		{name: "gemini count tokens", apiFamily: "gemini", path: "/v1beta/models/entry-model:countTokens", body: `{"contents":[]}`},
	}

	coveredOperations := map[string]bool{}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			service := newRequestPlanUnitService()
			model := runtimeModelRecord{ID: 1, APIFamily: scenario.apiFamily, ModelID: "entry-model"}
			if strings.HasPrefix(scenario.path, "/v1/images/") {
				model.OpenAIImageOperations = stringPtr(providerauth.OpenAIImageCapabilityGenerationsAndEdits)
			}
			snapshot := newRequestPlanSnapshot(model)
			setSnapshotUpstreamModelID(snapshot, 1001, "Vendor/Upstream Model-X")
			if strings.HasPrefix(scenario.path, "/v1/images/") {
				connection := snapshot.TerminalTargetsByID[1001]
				connection.OpenAIImageCapability = stringPtr(providerauth.OpenAIImageCapabilityGenerationsAndEdits)
				snapshot.TerminalTargetsByID[1001] = connection
			}

			operationMatch := mustResolveRuntimeOperation(t, http.MethodPost, scenario.path)
			plan, err := service.buildTestRequestPlanFromSnapshot(httptest.NewRequest(http.MethodPost, scenario.path, nil), []byte(scenario.body), RuntimeProxyConfigSnapshot{}, operationMatch, requestPlanTestProfileID, snapshot)
			if err != nil {
				t.Fatalf("build request plan: %v", err)
			}
			if len(plan.TerminalAttempts) != 1 {
				t.Fatalf("expected one attempt, got %d", len(plan.TerminalAttempts))
			}
			attempt := plan.TerminalAttempts[0]
			if operationMatch.Operation.ModelBindingSource == RuntimeOperationModelBindingPath {
				if !strings.Contains(attempt.EffectiveRequestPath, "/v1beta/models/Vendor/Upstream Model-X:") {
					t.Fatalf("expected path rewrite to explicit upstream id, got %q", attempt.EffectiveRequestPath)
				}
			} else {
				assertUpstreamBodyModel(t, attempt.UpstreamBody, "Vendor/Upstream Model-X")
			}
			if plan.IsStreamingRequest != scenario.stream {
				t.Fatalf("expected streaming=%v, got %v", scenario.stream, plan.IsStreamingRequest)
			}
			if plan.ResolvedTargetModelID == nil || *plan.ResolvedTargetModelID != "entry-model" || plan.ResolvedPricingModelID != "entry-model" {
				t.Fatalf("logical attribution drifted: resolved=%+v pricing=%q", plan.ResolvedTargetModelID, plan.ResolvedPricingModelID)
			}
			coveredOperations[operationMatch.Operation.Name] = true
		})
	}

	for _, operation := range runtimeOperationCatalog {
		if operation.ModelBindingSource != RuntimeOperationModelBindingNone && !coveredOperations[operation.Name] {
			t.Fatalf("model-bound operation %q has no upstream_model_id rewrite scenario", operation.Name)
		}
	}
}

func TestUpstreamModelIDPlanningFailsClosedWhenIdentityIsMissing(t *testing.T) {
	service := newRequestPlanUnitService()
	snapshot := newRequestPlanSnapshot(runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "entry-model"})
	connection := snapshot.TerminalTargetsByID[1001]
	connection.UpstreamModelID = nil
	snapshot.TerminalTargetsByID[1001] = connection

	operationMatch := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/chat/completions")
	_, err := service.buildTestRequestPlanFromSnapshot(httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil), []byte(`{"model":"entry-model","messages":[]}`), RuntimeProxyConfigSnapshot{}, operationMatch, requestPlanTestProfileID, snapshot)
	if err == nil || !strings.Contains(err.Error(), "missing upstream_model_id") {
		t.Fatalf("expected missing upstream identity to fail closed, got %v", err)
	}
}

func assertUpstreamBodyModel(t *testing.T, body []byte, want string) {
	t.Helper()
	payload := map[string]any{}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode upstream body: %v", err)
	}
	got, _ := payload["model"].(string)
	if got != want {
		t.Fatalf("expected upstream body model %q, got %q (body=%s)", want, got, string(body))
	}
}
