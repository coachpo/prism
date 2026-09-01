package runtime

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/coachpo/prism/backend/internal/domain/terminaltarget"
	"github.com/coachpo/prism/backend/internal/providerauth"
)

// TestCustomRequestParametersPlanMaterializesImmutablePerAttemptBodies verifies
// the hedge/concurrent-candidate invariant: every planned terminal attempt
// carries its own overlay from its own Connection configuration, and no two
// attempts share mutable body storage (maps, slices, or serialization
// buffers). The runtime builds all candidate bodies up front so failover,
// retry, or hedge launch can never mutate or inherit another attempt's body.
func TestCustomRequestParametersPlanMaterializesImmutablePerAttemptBodies(t *testing.T) {
	service := newRequestPlanUnitService()
	snapshot := newRequestPlanSnapshot(
		runtimeModelRecord{ModelID: "target-openai", APIFamily: "openai", AuditEnabled: true, AuditCaptureBodies: true},
	)
	targetModel := snapshot.ModelsByID["target-openai"]

	firstConfig, firstErr := terminaltarget.ParseCustomRequestParametersJSON([]byte(`{"temperature":0.1,"provider":{"only":["first-provider"]}}`))
	if firstErr != nil {
		t.Fatalf("parse first config: %v", firstErr)
	}
	secondConfig, secondErr := terminaltarget.ParseCustomRequestParametersJSON([]byte(`{"temperature":0.9,"provider":{"only":["second-provider"]}}`))
	if secondErr != nil {
		t.Fatalf("parse second config: %v", secondErr)
	}
	firstUpstreamModelID := "provider/First-Model"
	secondUpstreamModelID := "provider/Second-Model"
	snapshot.TerminalTargetsByID[1001] = runtimeConnection{
		ID:                      1001,
		ProfileID:               requestPlanTestProfileID,
		APIFamily:               "openai",
		ModelConfigID:           targetModel.ID,
		EndpointID:              1,
		Priority:                0,
		UpstreamModelID:         stringPtr(firstUpstreamModelID),
		CustomRequestParameters: firstConfig,
		OpenAITextCapability:    stringPtr(providerauth.OpenAITextCapabilityDualNative),
		Endpoint:                runtimeEndpoint{ID: 1, BaseURL: "https://upstream.example"},
	}
	snapshot.TerminalTargetsByID[1002] = runtimeConnection{
		ID:                      1002,
		ProfileID:               requestPlanTestProfileID,
		APIFamily:               "openai",
		ModelConfigID:           targetModel.ID,
		EndpointID:              2,
		Priority:                1,
		UpstreamModelID:         stringPtr(secondUpstreamModelID),
		CustomRequestParameters: secondConfig,
		OpenAITextCapability:    stringPtr(providerauth.OpenAITextCapabilityDualNative),
		Endpoint:                runtimeEndpoint{ID: 2, BaseURL: "https://upstream.example"},
	}
	snapshot.AccessTargetsBySourceModelID[targetModel.ID] = []runtimeAccessTargetRecord{
		{
			ID: 2001, ProfileID: requestPlanTestProfileID, SourceModelConfigID: targetModel.ID,
			TargetType: runtimeAccessTargetTypeConnection, TargetConnectionID: intPtr(1001),
			TargetConnectionProfileID: requestPlanTestProfileID, TargetConnectionAPIFamily: "openai",
			Position: 0, IsEnabled: true,
		},
		{
			ID: 2002, ProfileID: requestPlanTestProfileID, SourceModelConfigID: targetModel.ID,
			TargetType: runtimeAccessTargetTypeConnection, TargetConnectionID: intPtr(1002),
			TargetConnectionProfileID: requestPlanTestProfileID, TargetConnectionAPIFamily: "openai",
			Position: 1, IsEnabled: true,
		},
	}

	rawBody := []byte(`{"model":"target-openai","messages":[{"role":"user","content":"isolation"}],"temperature":0.5}`)
	plan, err := buildRequestPlanForTest(t, service, snapshot, "/v1/chat/completions", rawBody, RuntimeProxyConfigSnapshot{})
	if err != nil {
		t.Fatalf("build request plan: %v", err)
	}

	attempts := plan.orderedTerminalAttempts()
	if len(attempts) != 2 {
		t.Fatalf("expected two planned attempts, got %d", len(attempts))
	}
	byConnection := map[int]runtimeTerminalAttempt{}
	for _, attempt := range attempts {
		byConnection[attempt.Connection.ID] = attempt
	}
	first, ok := byConnection[1001]
	if !ok {
		t.Fatalf("expected attempt for connection 1001, got %+v", byConnection)
	}
	second, ok := byConnection[1002]
	if !ok {
		t.Fatalf("expected attempt for connection 1002, got %+v", byConnection)
	}

	assertPlannedAttemptBody(t, first.UpstreamBody, firstUpstreamModelID, 0.1, "first-provider")
	assertPlannedAttemptBody(t, second.UpstreamBody, secondUpstreamModelID, 0.9, "second-provider")

	if bytes.Equal(first.UpstreamBody, second.UpstreamBody) {
		t.Fatalf("attempt bodies must differ per Connection, both %s", first.UpstreamBody)
	}
	// No shared backing storage: mutating one body slice must not affect the
	// other attempt's body.
	secondBodyBefore := append([]byte(nil), second.UpstreamBody...)
	for index := range first.UpstreamBody {
		first.UpstreamBody[index] = 'X'
	}
	if !bytes.Equal(second.UpstreamBody, secondBodyBefore) {
		t.Fatalf("attempt bodies share backing storage")
	}
	if bytes.Equal(first.UpstreamBody, secondBodyBefore) {
		t.Fatalf("expected mutation to alter the local copy only")
	}

	// Plan-level generation params come from the first attempt's effective
	// body, while each attempt carries its own snapshot.
	if first.RequestGenerationParams.Params == nil || first.RequestGenerationParams.Params.Temperature == nil || *first.RequestGenerationParams.Params.Temperature != 0.1 {
		t.Fatalf("expected first attempt generation snapshot from its own body, got %+v", first.RequestGenerationParams)
	}
	if second.RequestGenerationParams.Params == nil || second.RequestGenerationParams.Params.Temperature == nil || *second.RequestGenerationParams.Params.Temperature != 0.9 {
		t.Fatalf("expected second attempt generation snapshot from its own body, got %+v", second.RequestGenerationParams)
	}
}

func assertPlannedAttemptBody(t *testing.T, rawBody []byte, wantModelID string, wantTemperature float64, wantProvider string) {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(rawBody, &payload); err != nil {
		t.Fatalf("decode planned attempt body: %v", err)
	}
	if payload["temperature"] != wantTemperature {
		t.Fatalf("expected temperature %v, got %v in %s", wantTemperature, payload["temperature"], rawBody)
	}
	provider, ok := payload["provider"].(map[string]any)
	if !ok {
		t.Fatalf("expected provider object, got %T in %s", payload["provider"], rawBody)
	}
	only, ok := provider["only"].([]any)
	if !ok || len(only) != 1 || only[0] != wantProvider {
		t.Fatalf("expected provider.only [%q], got %+v in %s", wantProvider, provider["only"], rawBody)
	}
	if payload["model"] != wantModelID {
		t.Fatalf("expected rewritten upstream model %q to survive overlay, got %v", wantModelID, payload["model"])
	}
}
