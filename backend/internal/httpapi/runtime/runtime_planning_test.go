package runtime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/coachpo/prism/backend/internal/domain/loadbalance"
	"github.com/coachpo/prism/backend/internal/domain/terminaltarget"
	"github.com/coachpo/prism/backend/internal/providerauth"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestModelResolutionAndRewriteHelpers(t *testing.T) {
	rawBody := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`)
	chatOperation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/chat/completions")
	if got, err := resolveModelIDForOperation(rawBody, "application/json", chatOperation); err != nil || got != "gpt-4o" {
		t.Fatalf("expected body model id, got model=%q err=%v", got, err)
	}
	responsesBody := []byte(`{"model":"gpt-4o","input":"hello"}`)
	responsesOperation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/responses")
	if got, err := resolveModelIDForOperation(responsesBody, "application/json", responsesOperation); err != nil || got != "gpt-4o" {
		t.Fatalf("expected Responses body model id, got model=%q err=%v", got, err)
	}
	geminiOperation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1beta/models/gemini-2.5-pro:generateContent")
	if got, err := resolveModelIDForOperation(nil, "", geminiOperation); err != nil || got != "gemini-2.5-pro" {
		t.Fatalf("expected path model id, got model=%q err=%v", got, err)
	}
	rewrittenBody := rewriteModelInBody(rawBody, "gpt-4o-mini")
	if got := extractModelFromBody(rewrittenBody); got != "gpt-4o-mini" {
		t.Fatalf("expected rewritten model id in body, got %q", got)
	}
	if got := rewriteModelInPath("/v1beta/models/gemini-1.5-pro:generateContent", "gemini-1.5-pro", "gemini-2.5-pro"); got != "/v1beta/models/gemini-2.5-pro:generateContent" {
		t.Fatalf("expected rewritten Gemini path, got %q", got)
	}
	if _, err := resolveModelIDForOperation([]byte(`{"messages":[]}`), "application/json", chatOperation); err == nil {
		t.Fatal("expected missing model id to fail")
	}
	if _, err := resolveModelIDForOperation([]byte(`{"input":"hello"}`), "application/json", responsesOperation); err == nil {
		t.Fatal("expected missing Responses model id to fail")
	}
}

func TestBuildRequestPlanCarriesOperation(t *testing.T) {
	service := newRequestPlanUnitService()
	snapshot := newRequestPlanSnapshot(runtimeModelRecord{ID: 1, APIFamily: "gemini", ModelID: "gemini-public"})
	request := httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-public:generateContent", nil)
	operationMatch := mustResolveRuntimeOperation(t, http.MethodPost, request.URL.Path)

	plan, err := service.buildTestRequestPlanFromSnapshot(request, nil, RuntimeProxyConfigSnapshot{}, operationMatch, requestPlanTestProfileID, snapshot)
	if err != nil {
		t.Fatalf("build request plan: %v", err)
	}
	operationMatch.PathParams["model"] = "mutated"

	if plan.RuntimeOperation.Name != "gemini.generate_content" {
		t.Fatalf("expected plan operation gemini.generate_content, got %+v", plan.RuntimeOperation)
	}
	if plan.RuntimeOperation.HookCollectionID != "gemini.generate_content" {
		t.Fatalf("expected hook collection id to travel with operation, got %q", plan.RuntimeOperation.HookCollectionID)
	}
	if got := plan.RuntimeOperationPathParams["model"]; got != "gemini-public" {
		t.Fatalf("expected path model param gemini-public, got %q", got)
	}
}

func TestBuildRequestPlanClassifiesGeminiStreamingByOperation(t *testing.T) {
	service := newRequestPlanUnitService()
	snapshot := newRequestPlanSnapshot(runtimeModelRecord{ID: 1, APIFamily: "gemini", ModelID: "gemini-public"})

	tests := []struct {
		name       string
		path       string
		rawBody    []byte
		wantStream bool
	}{
		{
			name:       "generateContent ignores body stream flag",
			path:       "/v1beta/models/gemini-public:generateContent",
			rawBody:    []byte(`{"contents":[],"stream":true}`),
			wantStream: false,
		},
		{
			name:       "streamGenerateContent is path authoritative",
			path:       "/v1beta/models/gemini-public:streamGenerateContent",
			rawBody:    []byte(`{"contents":[],"stream":false}`),
			wantStream: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan := mustBuildRequestPlanForTest(t, service, snapshot, test.path, test.rawBody, RuntimeProxyConfigSnapshot{})
			if plan.IsStreamingRequest != test.wantStream {
				t.Fatalf("expected IsStreamingRequest=%v for %s, got %v", test.wantStream, plan.RuntimeOperation.Name, plan.IsStreamingRequest)
			}
		})
	}
}

func TestBuildRequestPlan_ContextEstimationUnavailablePassesThrough(t *testing.T) {
	forEachRequestPlanService(t, "responses", func(t *testing.T, service *Service) {
		snapshot := newRequestPlanSnapshot(runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "gpt-4o", OpenAIAcceptedFormat: stringPtr(providerauth.OpenAITextCapabilityResponsesOnly)})
		model := snapshot.ModelsByID["gpt-4o"]
		snapshot.AccessTargetsBySourceModelID[model.ID] = nil
		addRequestPlanConnectionTargetWithOptions(snapshot, model, 2_711, 9_711, 0, requestPlanConnectionTargetOptions{
			openAITextCapability: stringPtr(providerauth.OpenAITextCapabilityResponsesOnly),
		})
		plan := mustBuildRequestPlanForTest(t, service, snapshot, "/v1/responses", []byte(`{"model":"gpt-4o","previous_response_id":"resp_123","input":"hello"}`), RuntimeProxyConfigSnapshot{})
		if plan.EffectiveRequestPath != "/v1/responses" {
			t.Fatalf("expected native responses path, got %q", plan.EffectiveRequestPath)
		}
		if plan.SelectedTerminalTargetID == nil || *plan.SelectedTerminalTargetID != 2_711 {
			t.Fatalf("expected selected terminal target 2711, got %+v", plan.SelectedTerminalTargetID)
		}
	})
}

func TestBuildRequestPlan_ContextEstimationUnavailableChatPassesThroughWithoutTransportCall(t *testing.T) {
	forEachRequestPlanService(t, "chat", func(t *testing.T, service *Service) {
		transport := &ingressRoundTripRecorder{}
		snapshot := newRequestPlanSnapshot(runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "gpt-4o", OpenAIAcceptedFormat: stringPtr(providerauth.OpenAITextCapabilityChatCompletionsOnly)})
		model := snapshot.ModelsByID["gpt-4o"]
		snapshot.AccessTargetsBySourceModelID[model.ID] = nil
		addRequestPlanConnectionTargetWithOptions(snapshot, model, 2_712, 9_712, 0, requestPlanConnectionTargetOptions{
			openAITextCapability: stringPtr(providerauth.OpenAITextCapabilityChatCompletionsOnly),
		})
		plan := mustBuildRequestPlanForTest(t, service, snapshot, "/v1/chat/completions", []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.invalid/image.png","detail":"high"}}]}]}`), RuntimeProxyConfigSnapshot{HTTPClient: &http.Client{Transport: transport}})
		if plan.EffectiveRequestPath != "/v1/chat/completions" {
			t.Fatalf("expected native chat path, got %q", plan.EffectiveRequestPath)
		}
		if got := transport.calls.Load(); got != 0 {
			t.Fatalf("expected request planning to avoid transport calls, got %d", got)
		}
	})
}

func TestBuildRequestPlan_ResponsesRejectsChatOnlyTarget(t *testing.T) {
	forEachRequestPlanService(t, "responses-chat-only", func(t *testing.T, service *Service) {
		transport := &ingressRoundTripRecorder{}
		snapshot := newRequestPlanSnapshot(
			runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "responses-public"},
			runtimeModelRecord{ID: 2, APIFamily: "openai", ModelID: "chat-target-model"},
		)
		addRequestPlanProxyTarget(snapshot, "responses-public", "chat-target-model")
		child := snapshot.ModelsByID["chat-target-model"]
		snapshot.AccessTargetsBySourceModelID[child.ID] = nil
		addRequestPlanConnectionTargetWithOptions(snapshot, child, 2_713, 9_713, 0, requestPlanConnectionTargetOptions{
			openAITextCapability: stringPtr(providerauth.OpenAITextCapabilityChatCompletionsOnly),
		})
		err := buildRequestPlanErrorForTest(t, service, snapshot, "/v1/responses", []byte(`{"model":"responses-public","input":"hello"}`), RuntimeProxyConfigSnapshot{HTTPClient: &http.Client{Transport: transport}})
		assertPlanDomainError(t, err, http.StatusBadRequest, openAIRequestTranslationUnsupportedDetail)
		domainErr, ok := isRequestTranslationUnsupportedError(err)
		if !ok {
			t.Fatalf("expected typed unsupported-wire error, got %v", err)
		}
		if got := stringValue(domainErr.Fields["translation_mode"]); got != "none" {
			t.Fatalf("expected translation mode none, got %q", got)
		}
		if got := stringValue(domainErr.Fields["unsupported_reason"]); got != openAIRequestTranslationUnsupportedReason {
			t.Fatalf("expected unsupported reason %q, got %q", openAIRequestTranslationUnsupportedReason, got)
		}
		if got := transport.calls.Load(); got != 0 {
			t.Fatalf("expected planner to reject incompatible graph before transport, got %d calls", got)
		}
	})
}

func TestBuildRequestPlan_SkipsNonNativeConnection(t *testing.T) {
	service := newRequestPlanUnitService()
	snapshot := newRequestPlanSnapshot(
		runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "responses-public", OpenAIAcceptedFormat: stringPtr(providerauth.OpenAITextCapabilityResponsesOnly)},
		runtimeModelRecord{ID: 2, APIFamily: "openai", ModelID: "chat-child", OpenAIAcceptedFormat: stringPtr(providerauth.OpenAITextCapabilityChatCompletionsOnly)},
	)
	model := snapshot.ModelsByID["responses-public"]
	addRequestPlanProxyTarget(snapshot, "responses-public", "chat-child")
	child := snapshot.ModelsByID["chat-child"]
	snapshot.AccessTargetsBySourceModelID[child.ID] = nil
	addRequestPlanConnectionTargetWithOptions(snapshot, child, 2_714, 9_714, 0, requestPlanConnectionTargetOptions{
		openAITextCapability: stringPtr(providerauth.OpenAITextCapabilityChatCompletionsOnly),
	})
	addRequestPlanConnectionTargetWithOptions(snapshot, model, 2_715, 9_715, 1, requestPlanConnectionTargetOptions{
		openAITextCapability: stringPtr(providerauth.OpenAITextCapabilityResponsesOnly),
	})

	plan := mustBuildRequestPlanForTest(t, service, snapshot, "/v1/responses", []byte(`{"model":"responses-public","input":"hello"}`), RuntimeProxyConfigSnapshot{})
	if plan.SelectedTerminalTargetID == nil || *plan.SelectedTerminalTargetID != 2_715 {
		t.Fatalf("expected planner to skip chat-only connection and select 2715, got %+v", plan.SelectedTerminalTargetID)
	}
	if len(plan.TerminalAttempts) != 1 || plan.TerminalAttempts[0].Connection.ID != 2_715 || plan.TerminalAttempts[0].TranslationMode != TranslationModeNone {
		t.Fatalf("expected only the native responses attempt, got %+v", plan.TerminalAttempts)
	}
}

func TestBuildRequestPlanAppliesOperationRewriteRules(t *testing.T) {
	t.Run("path-bound operation rewrites only path model", func(t *testing.T) {
		service := newRequestPlanUnitService()
		snapshot := newRequestPlanSnapshot(
			runtimeModelRecord{ID: 1, APIFamily: "gemini", ModelID: "public-gemini"},
			runtimeModelRecord{ID: 2, APIFamily: "gemini", ModelID: "target-gemini"},
		)
		addRequestPlanProxyTarget(snapshot, "public-gemini", "target-gemini")
		rawBody := []byte(`{"model":"body-should-not-change","contents":[]}`)
		plan := mustBuildRequestPlanForTest(t, service, snapshot, "/v1beta/models/public-gemini:generateContent", rawBody, RuntimeProxyConfigSnapshot{})

		if plan.EffectiveRequestPath != "/v1beta/models/target-gemini:generateContent" {
			t.Fatalf("expected rewritten Gemini path, got %q", plan.EffectiveRequestPath)
		}
		if !bytes.Equal(plan.UpstreamBody, rawBody) {
			t.Fatalf("expected path-bound operation to leave body unchanged, got %s", string(plan.UpstreamBody))
		}
		if got := extractModelFromBody(plan.UpstreamBody); got != "body-should-not-change" {
			t.Fatalf("expected body model to remain untouched, got %q", got)
		}
	})

	t.Run("body-bound operations rewrite deepest final target", func(t *testing.T) {
		tests := []struct {
			name        string
			apiFamily   string
			path        string
			rawBody     []byte
			publicModel string
			targetModel string
		}{
			{
				name:        "OpenAI Chat Completions",
				apiFamily:   "openai",
				path:        "/v1/chat/completions",
				rawBody:     []byte(`{"model":"public-openai-chat","messages":[]}`),
				publicModel: "public-openai-chat",
				targetModel: "target-openai-chat",
			},
			{
				name:        "OpenAI Responses",
				apiFamily:   "openai",
				path:        "/v1/responses",
				rawBody:     []byte(`{"model":"public-openai-responses","input":"hello"}`),
				publicModel: "public-openai-responses",
				targetModel: "target-openai-responses",
			},
			{
				name:        "Anthropic Messages",
				apiFamily:   "anthropic",
				path:        "/v1/messages",
				rawBody:     []byte(`{"model":"public-anthropic","messages":[],"max_tokens":16}`),
				publicModel: "public-anthropic",
				targetModel: "target-anthropic",
			},
			{
				name:        "Anthropic Count Tokens",
				apiFamily:   "anthropic",
				path:        "/v1/messages/count_tokens",
				rawBody:     []byte(`{"model":"public-anthropic-count","messages":[],"stream":true}`),
				publicModel: "public-anthropic-count",
				targetModel: "target-anthropic-count",
			},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				service := newRequestPlanUnitService()
				snapshot := newRequestPlanSnapshot(
					runtimeModelRecord{ID: 1, APIFamily: test.apiFamily, ModelID: test.publicModel},
					runtimeModelRecord{ID: 2, APIFamily: test.apiFamily, ModelID: "mid-" + test.publicModel},
					runtimeModelRecord{ID: 3, APIFamily: test.apiFamily, ModelID: test.targetModel},
				)
				addRequestPlanProxyTarget(snapshot, test.publicModel, "mid-"+test.publicModel)
				addRequestPlanProxyTarget(snapshot, "mid-"+test.publicModel, test.targetModel)
				plan := mustBuildRequestPlanForTest(t, service, snapshot, test.path, test.rawBody, RuntimeProxyConfigSnapshot{})
				if plan.EffectiveRequestPath != test.path {
					t.Fatalf("expected body-bound operation to leave path unchanged, got %q", plan.EffectiveRequestPath)
				}
				if got := extractModelFromBody(plan.UpstreamBody); got != test.targetModel {
					t.Fatalf("expected rewritten body model %q, got %q in %s", test.targetModel, got, string(plan.UpstreamBody))
				}
				if test.path == "/v1/messages/count_tokens" && plan.IsStreamingRequest {
					t.Fatalf("expected Anthropic count_tokens to remain non-streaming")
				}
			})
		}
	})

	t.Run("operation api family must match resolved target", func(t *testing.T) {
		service := newRequestPlanUnitService()
		snapshot := newRequestPlanSnapshot(runtimeModelRecord{ID: 1, APIFamily: "gemini", ModelID: "gemini-native"})
		rawBody := []byte(`{"model":"gemini-native","messages":[]}`)
		err := buildRequestPlanErrorForTest(t, service, snapshot, "/v1/chat/completions", rawBody, RuntimeProxyConfigSnapshot{})
		assertPlanDomainError(t, err, http.StatusBadRequest, "incompatible")
	})

	t.Run("unsupported model-binding source fails before model fallback", func(t *testing.T) {
		service := newRequestPlanUnitService()
		snapshot := newRequestPlanSnapshot(runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "gpt-4o"})
		rawBody := []byte(`{"model":"gpt-4o","messages":[]}`)
		request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		operationMatch := RuntimeOperationMatch{Operation: RuntimeOperation{
			Name:               "test.header_model",
			Method:             http.MethodPost,
			APIFamily:          "openai",
			PathTemplate:       "/v1/chat/completions",
			PathMatcher:        staticRuntimeOperationPath("/v1/chat/completions"),
			ModelBindingSource: RuntimeOperationModelBindingSource("header"),
		}}

		_, err := service.buildTestRequestPlanFromSnapshot(request, rawBody, RuntimeProxyConfigSnapshot{}, operationMatch, requestPlanTestProfileID, snapshot)
		assertPlanDomainError(t, err, http.StatusBadRequest, "unsupported model binding source")
	})

	t.Run("generic OpenAI v1 path is not a planning fallback", func(t *testing.T) {
		service := newRequestPlanUnitService()
		snapshot := newRequestPlanSnapshot(runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "gpt-4o"})
		rawBody := []byte(`{"model":"gpt-4o"}`)
		request := httptest.NewRequest(http.MethodPost, "/v1/models", nil)
		operationMatch := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/chat/completions")

		_, err := service.buildTestRequestPlanFromSnapshot(request, rawBody, RuntimeProxyConfigSnapshot{}, operationMatch, requestPlanTestProfileID, snapshot)
		assertPlanDomainError(t, err, http.StatusNotFound, runtimeOperationNotFoundDetail)
	})
}

func TestUnifiedModelRoutingResolvesDirectOneHopAndMultiHop(t *testing.T) {
	service := newRequestPlanUnitService()
	snapshot := newRequestPlanSnapshot(
		runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "public-openai"},
		runtimeModelRecord{ID: 2, APIFamily: "openai", ModelID: "mid-openai"},
		runtimeModelRecord{ID: 3, APIFamily: "openai", ModelID: "target-openai"},
	)
	addRequestPlanProxyTarget(snapshot, "public-openai", "mid-openai")
	addRequestPlanProxyTarget(snapshot, "mid-openai", "target-openai")

	plan := mustBuildRequestPlanForTest(t, service, snapshot, "/v1/chat/completions", []byte(`{"model":"public-openai","messages":[]}`), RuntimeProxyConfigSnapshot{})
	if plan.ResolvedTargetModelID == nil || *plan.ResolvedTargetModelID != "target-openai" {
		t.Fatalf("expected multi-hop final target target-openai, got %+v", plan.ResolvedTargetModelID)
	}
	if got := extractModelFromBody(plan.UpstreamBody); got != "target-openai" {
		t.Fatalf("expected upstream body to target deepest model, got %q", got)
	}

	directSnapshot := newRequestPlanSnapshot(runtimeModelRecord{ID: 4, APIFamily: "openai", ModelID: "direct-openai"})
	directPlan := mustBuildRequestPlanForTest(t, service, directSnapshot, "/v1/chat/completions", []byte(`{"model":"direct-openai","messages":[]}`), RuntimeProxyConfigSnapshot{})
	if directPlan.ResolvedTargetModelID == nil || *directPlan.ResolvedTargetModelID != "direct-openai" || len(directPlan.Connections) != 1 {
		t.Fatalf("expected direct model to resolve to its own connection, got target=%+v connections=%d", directPlan.ResolvedTargetModelID, len(directPlan.Connections))
	}
}

func TestRuntimePlanningEmitsOrderedTerminalAttemptsAcrossModelTargets(t *testing.T) {
	service := newRequestPlanUnitService()
	snapshot := newRequestPlanSnapshot(
		runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "public-terminal-openai"},
		runtimeModelRecord{ID: 2, APIFamily: "openai", ModelID: "first-terminal-openai"},
		runtimeModelRecord{ID: 3, APIFamily: "openai", ModelID: "second-terminal-openai"},
	)
	snapshot.AccessTargetsBySourceModelID[1] = nil
	addRequestPlanModelTargetAtPosition(snapshot, "public-terminal-openai", "first-terminal-openai", 0)
	addRequestPlanModelTargetAtPosition(snapshot, "public-terminal-openai", "second-terminal-openai", 1)

	plan := mustBuildRequestPlanForTest(t, service, snapshot, "/v1/chat/completions", []byte(`{"model":"public-terminal-openai","messages":[]}`), RuntimeProxyConfigSnapshot{})
	if len(plan.TerminalAttempts) != 2 || len(plan.Connections) != 2 {
		t.Fatalf("expected two ordered terminal attempts and connections, got attempts=%d connections=%d", len(plan.TerminalAttempts), len(plan.Connections))
	}
	if got := plan.TerminalAttempts[0].TargetModel.ModelID; got != "first-terminal-openai" {
		t.Fatalf("expected first terminal attempt to target first-terminal-openai, got %q", got)
	}
	if got := plan.TerminalAttempts[1].TargetModel.ModelID; got != "second-terminal-openai" {
		t.Fatalf("expected second terminal attempt to target second-terminal-openai, got %q", got)
	}
	if got := extractModelFromBody(plan.TerminalAttempts[1].UpstreamBody); got != "second-terminal-openai" {
		t.Fatalf("expected second terminal attempt body rewrite to second-terminal-openai, got %q", got)
	}
}

func TestRuntimePlanningRoundRobinCursorScopesToImmediateEnabledTargetSet(t *testing.T) {
	service := newRequestPlanUnitService()
	snapshot := newRequestPlanSnapshot(runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "rr-openai"})
	model := snapshot.ModelsByID["rr-openai"]
	roundRobin := "round-robin"
	strategy := snapshot.StrategiesByModelID[model.ID]
	strategy.LegacyStrategyType = &roundRobin
	snapshot.StrategiesByModelID[model.ID] = strategy
	addRequestPlanConnectionTarget(snapshot, model, 2_001, 2, 1)

	rawBody := []byte(`{"model":"rr-openai","messages":[]}`)
	first := mustBuildRequestPlanForTest(t, service, snapshot, "/v1/chat/completions", rawBody, RuntimeProxyConfigSnapshot{})
	second := mustBuildRequestPlanForTest(t, service, snapshot, "/v1/chat/completions", rawBody, RuntimeProxyConfigSnapshot{})
	if first.Connections[0].ID == second.Connections[0].ID {
		t.Fatalf("expected round-robin to rotate immediate targets, got %d twice", first.Connections[0].ID)
	}

	addRequestPlanConnectionTarget(snapshot, model, 3_001, 3, 2)
	reset := mustBuildRequestPlanForTest(t, service, snapshot, "/v1/chat/completions", rawBody, RuntimeProxyConfigSnapshot{})
	if reset.Connections[0].ID != first.Connections[0].ID {
		t.Fatalf("expected changed enabled target set hash to reset cursor to %d, got %d", first.Connections[0].ID, reset.Connections[0].ID)
	}
}

func TestRuntimePlanningCycleAndNoEligibleTargetsFailDeterministically(t *testing.T) {
	service := newRequestPlanUnitService()

	noTargets := newRequestPlanSnapshot(
		runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "public-openai"},
		runtimeModelRecord{ID: 2, APIFamily: "openai", ModelID: "empty-child-openai"},
	)
	addRequestPlanProxyTarget(noTargets, "public-openai", "empty-child-openai")
	noTargets.AccessTargetsBySourceModelID[2] = nil
	err := buildRequestPlanErrorForTest(t, service, noTargets, "/v1/chat/completions", []byte(`{"model":"public-openai","messages":[]}`), RuntimeProxyConfigSnapshot{})
	assertPlanDomainError(t, err, http.StatusServiceUnavailable, "No eligible targets available for model 'public-openai'.")

	cycle := newRequestPlanSnapshot(
		runtimeModelRecord{ID: 2, APIFamily: "openai", ModelID: "cycle-a"},
		runtimeModelRecord{ID: 3, APIFamily: "openai", ModelID: "cycle-b"},
	)
	addRequestPlanProxyTarget(cycle, "cycle-a", "cycle-b")
	addRequestPlanProxyTarget(cycle, "cycle-b", "cycle-a")
	err = buildRequestPlanErrorForTest(t, service, cycle, "/v1/chat/completions", []byte(`{"model":"cycle-a","messages":[]}`), RuntimeProxyConfigSnapshot{})
	assertPlanDomainError(t, err, http.StatusServiceUnavailable, "cycle detected")
}

func TestRuntimePlanningDepthOverflowFailsDeterministically(t *testing.T) {
	service := newRequestPlanUnitService()
	models := make([]runtimeModelRecord, 0, runtimeAccessResolverMaxDepth+3)
	for i := 0; i < runtimeAccessResolverMaxDepth+3; i++ {
		models = append(models, runtimeModelRecord{ID: i + 1, APIFamily: "openai", ModelID: fmt.Sprintf("depth-%02d", i)})
	}
	snapshot := newRequestPlanSnapshot(models...)
	for i := 0; i < len(models)-1; i++ {
		addRequestPlanProxyTarget(snapshot, models[i].ModelID, models[i+1].ModelID)
	}
	err := buildRequestPlanErrorForTest(t, service, snapshot, "/v1/chat/completions", []byte(`{"model":"depth-00","messages":[]}`), RuntimeProxyConfigSnapshot{})
	assertPlanDomainError(t, err, http.StatusServiceUnavailable, "exceeded maximum depth of 32")
}

func TestAttachRuntimePlanningFailureTelemetry_NoResolvedTargetLeavesResolvedTargetModelNil(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	operationMatch := mustResolveRuntimeOperation(t, http.MethodPost, request.URL.Path)
	requestedModel := runtimeModelRecord{
		ID:         1,
		APIFamily:  "openai",
		ModelID:    "no-target-model",
		VendorID:   intPtr(1),
		VendorKey:  stringPtr("openai"),
		VendorName: stringPtr("OpenAI"),
	}
	runtimeErr := &domainError{
		StatusCode: http.StatusServiceUnavailable,
		Detail:     "No eligible targets available for model 'no-target-model'.",
	}

	err := attachRuntimePlanningFailureTelemetry(runtimeErr, requestPlanningInput{
		Request:  request,
		RawBody:  []byte(`{"model":"no-target-model","messages":[{"role":"user","content":"hello"}]}`),
		Snapshot: &planningSnapshot{ReportCurrency: runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$"}},
	}, resolvedRequestOperation{Match: operationMatch, RequestedModelID: requestedModel.ModelID}, requestedModel)
	if err != runtimeErr {
		t.Fatalf("expected attach helper to return original domain error, got %v", err)
	}
	if runtimeErr.PlanningFailure == nil {
		t.Fatal("expected planning-failure telemetry to be attached")
	}
	if runtimeErr.ResolvedTargetModelID != nil {
		t.Fatalf("expected no resolved target model id on no-target planning failure, got %+v", runtimeErr.ResolvedTargetModelID)
	}
	if runtimeErr.PlanningFailure.RequestedModelID != requestedModel.ModelID {
		t.Fatalf("expected planning-failure telemetry to keep requested model %q, got %+v", requestedModel.ModelID, runtimeErr.PlanningFailure)
	}
}

func TestAttachRuntimePlanningFailureTelemetry_PreservesResolvedTargetModelWhenSelected(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	operationMatch := mustResolveRuntimeOperation(t, http.MethodPost, request.URL.Path)
	requestedModel := runtimeModelRecord{
		ID:         1,
		APIFamily:  "openai",
		ModelID:    "public-responses-model",
		VendorID:   intPtr(1),
		VendorKey:  stringPtr("openai"),
		VendorName: stringPtr("OpenAI"),
	}
	resolvedTargetModelID := "native-chat-target"
	selectedTerminalTargetID := 2841
	runtimeErr := &domainError{
		StatusCode:               http.StatusBadRequest,
		ErrorCode:                openAIRequestTranslationUnsupportedErrorCode,
		Detail:                   openAIRequestTranslationUnsupportedDetail,
		Fields:                   map[string]any{"translation_mode": "none", "unsupported_reason": openAIRequestTranslationUnsupportedReason},
		ResolvedTargetModelID:    &resolvedTargetModelID,
		SelectedTerminalTargetID: &selectedTerminalTargetID,
	}

	err := attachRuntimePlanningFailureTelemetry(runtimeErr, requestPlanningInput{
		Request:  request,
		RawBody:  []byte(`{"model":"public-responses-model","input":"hello","text":{"format":"json_schema"},"max_output_tokens":64}`),
		Snapshot: &planningSnapshot{ReportCurrency: runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$"}},
	}, resolvedRequestOperation{Match: operationMatch, RequestedModelID: requestedModel.ModelID}, requestedModel)
	if err != runtimeErr {
		t.Fatalf("expected attach helper to return original domain error, got %v", err)
	}
	if runtimeErr.PlanningFailure == nil {
		t.Fatal("expected planning-failure telemetry to be attached")
	}
	if runtimeErr.ResolvedTargetModelID == nil || *runtimeErr.ResolvedTargetModelID != resolvedTargetModelID {
		t.Fatalf("expected planning failure to preserve resolved target %q, got %+v", resolvedTargetModelID, runtimeErr.ResolvedTargetModelID)
	}
	if runtimeErr.PlanningFailure.SelectedTerminalTargetID == nil || *runtimeErr.PlanningFailure.SelectedTerminalTargetID != selectedTerminalTargetID {
		t.Fatalf("expected planning-failure telemetry to keep selected terminal target %d, got %+v", selectedTerminalTargetID, runtimeErr.PlanningFailure)
	}
	if runtimeErr.PlanningFailure.OperationTranslationMode == nil || *runtimeErr.PlanningFailure.OperationTranslationMode != "none" {
		t.Fatalf("expected planning-failure telemetry to retain translation mode none, got %+v", runtimeErr.PlanningFailure)
	}
}

func TestBuildRequestPlan_PreservesRecursiveModelStrategy(t *testing.T) {
	service := newRequestPlanUnitService()
	snapshot := newRequestPlanSnapshot(
		runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "public-openai"},
		runtimeModelRecord{ID: 2, APIFamily: "openai", ModelID: "child-openai"},
	)
	addRequestPlanProxyTarget(snapshot, "public-openai", "child-openai")

	childModel := snapshot.ModelsByID["child-openai"]
	snapshot.AccessTargetsBySourceModelID[childModel.ID] = nil
	childStrategyID := requestPlanTestStrategyID + 1
	roundRobin := "round-robin"
	snapshot.StrategiesByModelID[childModel.ID] = loadbalance.RuntimeStrategy{ID: childStrategyID, Name: "child round robin", LegacyStrategyType: &roundRobin}
	addRequestPlanConnectionTarget(snapshot, childModel, 2_001, 2_101, 0)
	addRequestPlanConnectionTarget(snapshot, childModel, 2_002, 2_102, 1)

	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	request.Header.Set("Content-Type", "application/json")
	operationMatch := mustResolveRuntimeOperation(t, http.MethodPost, request.URL.Path)
	input := requestPlanningInput{
		Request:         request,
		RawBody:         []byte(`{"model":"public-openai","messages":[]}`),
		RuntimeConfig:   RuntimeProxyConfigSnapshot{},
		OperationMatch:  operationMatch,
		ActiveProfileID: requestPlanTestProfileID,
		Snapshot:        snapshot,
		ReferenceNow:    service.nowUTC(),
	}

	operation, err := resolveRequestOperation(input)
	if err != nil {
		t.Fatalf("resolve request operation: %v", err)
	}
	requestedModel, err := resolveRequestedModel(input, operation)
	if err != nil {
		t.Fatalf("resolve requested model: %v", err)
	}

	firstTarget, err := service.resolveRequestPlanTarget(input, operation, requestedModel)
	if err != nil {
		t.Fatalf("resolve first recursive target: %v", err)
	}
	secondTarget, err := service.resolveRequestPlanTarget(input, operation, requestedModel)
	if err != nil {
		t.Fatalf("resolve second recursive target: %v", err)
	}
	if firstTarget.Strategy.ID != childStrategyID || secondTarget.Strategy.ID != childStrategyID {
		t.Fatalf("expected recursive child strategy %d to own terminal attempts, got first=%d second=%d", childStrategyID, firstTarget.Strategy.ID, secondTarget.Strategy.ID)
	}
	if len(firstTarget.TerminalAttempts) != 2 || len(secondTarget.TerminalAttempts) != 2 {
		t.Fatalf("expected two recursive terminal attempts on both resolutions, got first=%d second=%d", len(firstTarget.TerminalAttempts), len(secondTarget.TerminalAttempts))
	}
	if firstTarget.TerminalAttempts[0].Connection.ID == secondTarget.TerminalAttempts[0].Connection.ID {
		t.Fatalf("expected recursive child round-robin strategy to rotate terminal attempts, got %d twice", firstTarget.TerminalAttempts[0].Connection.ID)
	}

	firstAttempts, firstUpstream, err := buildPlannedTerminalAttempts(input, operation, firstTarget.TerminalAttempts)
	if err != nil {
		t.Fatalf("build first planned terminal attempts: %v", err)
	}
	secondAttempts, secondUpstream, err := buildPlannedTerminalAttempts(input, operation, secondTarget.TerminalAttempts)
	if err != nil {
		t.Fatalf("build second planned terminal attempts: %v", err)
	}
	if firstAttempts[0].Connection.ID != firstTarget.TerminalAttempts[0].Connection.ID {
		t.Fatalf("expected compiled attempts to preserve first recursive terminal order %d, got %d", firstTarget.TerminalAttempts[0].Connection.ID, firstAttempts[0].Connection.ID)
	}
	if secondAttempts[0].Connection.ID != secondTarget.TerminalAttempts[0].Connection.ID {
		t.Fatalf("expected compiled attempts to preserve second recursive terminal order %d, got %d", secondTarget.TerminalAttempts[0].Connection.ID, secondAttempts[0].Connection.ID)
	}
	if firstAttempts[0].Strategy.ID != childStrategyID || secondAttempts[0].Strategy.ID != childStrategyID {
		t.Fatalf("expected compiled attempts to retain recursive child strategy %d, got first=%d second=%d", childStrategyID, firstAttempts[0].Strategy.ID, secondAttempts[0].Strategy.ID)
	}
	if got := extractModelFromBody(firstUpstream.UpstreamBody); got != "child-openai" {
		t.Fatalf("expected first recursive upstream model child-openai, got %q", got)
	}
	if got := extractModelFromBody(secondUpstream.UpstreamBody); got != "child-openai" {
		t.Fatalf("expected second recursive upstream model child-openai, got %q", got)
	}
	if !bytes.Equal(firstAttempts[0].UpstreamBody, firstUpstream.UpstreamBody) {
		t.Fatalf("expected first compiled attempt body to match first upstream request")
	}
	if !bytes.Equal(secondAttempts[0].UpstreamBody, secondUpstream.UpstreamBody) {
		t.Fatalf("expected second compiled attempt body to match second upstream request")
	}
}

func TestPlanningReferenceNowIsSharedAcrossProbeAndFinalPlan(t *testing.T) {
	// Advancing clock: the first read returns T0, every later read T0+10m.
	// A Gemini path-bound ingress plans twice (probe then final) with an
	// upstream body read in between; before the shared ReferenceNow fix, the
	// final plan re-read the live clock and could treat a still-banned
	// connection as unbanned.
	baseTime := time.Unix(1_700_000_000, 0).UTC()
	advancingNow := baseTime
	service := &Service{
		runtimeState: loadbalance.NewLocalRuntimeStateStore(),
		now: func() time.Time {
			now := advancingNow
			advancingNow = advancingNow.Add(10 * time.Minute)
			return now
		},
	}
	snapshot := newRequestPlanSnapshot(
		runtimeModelRecord{ID: 1, APIFamily: "gemini", ModelID: "router-gemini"},
		runtimeModelRecord{ID: 2, APIFamily: "gemini", ModelID: "peer-gemini"},
	)
	// Router keeps its auto terminal (1001) plus one model peer; the peer's
	// only terminal is its auto connection 1002, which is what gets banned.
	addRequestPlanModelTargetWithMetadata(snapshot, "router-gemini", "peer-gemini", 0)

	request := httptest.NewRequest(http.MethodPost, "/v1beta/models/router-gemini:generateContent", nil)
	request.Header.Set("Content-Type", "application/json")
	// One ingress carries one planning clock for the whole request.
	request = request.WithContext(withRuntimeIngressContext(request.Context(), newRuntimeIngressContext(baseTime)))
	operationMatch := mustResolveRuntimeOperation(t, http.MethodPost, request.URL.Path)

	bannedUntil := baseTime.Add(5 * time.Minute)
	seededAt := baseTime.Add(-time.Minute)
	service.runtimeState.SeedConnectionState(requestPlanTestProfileID, 2, 1002, loadbalance.RuntimeConnectionState{ConnectionID: 1002, BanMode: "temporary", BannedUntilAt: &bannedUntil}, seededAt, seededAt)

	probePlan, err := service.buildRequestPlanFromSnapshotCoreWithProbe(request, nil, RuntimeProxyConfigSnapshot{}, operationMatch, requestPlanTestProfileID, snapshot, true)
	if err != nil {
		t.Fatalf("build probe plan: %v", err)
	}
	finalPlan, err := service.buildRequestPlanFromSnapshotCoreWithProbe(request, []byte(`{"contents":[{"parts":[{"text":"hi"}]}]}`), RuntimeProxyConfigSnapshot{}, operationMatch, requestPlanTestProfileID, snapshot, false)
	if err != nil {
		t.Fatalf("build final plan: %v", err)
	}
	if len(probePlan.TerminalAttempts) != len(finalPlan.TerminalAttempts) {
		t.Fatalf("expected probe and final plans to agree on the candidate set: probe=%d final=%d", len(probePlan.TerminalAttempts), len(finalPlan.TerminalAttempts))
	}
	// Connection 1002 is banned until T0+5m; with the shared T0 clock both
	// plans must exclude the peer. A final plan that re-read the clock at
	// T0+10m would include the peer and disagree.
	if len(finalPlan.TerminalAttempts) != 1 || finalPlan.TerminalAttempts[0].Connection.ID != 1001 {
		t.Fatalf("expected both plans to keep only the direct terminal attempt, got %+v", finalPlan.TerminalAttempts)
	}
}

func TestPlanningReferenceNowMissingOrZeroFailsClosed(t *testing.T) {
	service := newRequestPlanUnitService()
	snapshot := newRequestPlanSnapshot(runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "clock-required-openai"})
	operation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/chat/completions")
	for _, request := range []*http.Request{
		httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil),
		httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(withRuntimeIngressContext(context.Background(), newRuntimeIngressContext(time.Time{}))),
	} {
		_, err := service.buildRequestPlanFromSnapshot(request, []byte(`{"model":"clock-required-openai","messages":[]}`), RuntimeProxyConfigSnapshot{}, operation, requestPlanTestProfileID, snapshot)
		var domainErr *domainError
		if !errors.As(err, &domainErr) || domainErr.StatusCode != http.StatusServiceUnavailable || !strings.Contains(domainErr.Detail, "reference clock") {
			t.Fatalf("missing planning clock must fail closed, got %v", err)
		}
	}
}

const (
	requestPlanTestProfileID  = 101
	requestPlanTestStrategyID = 202
)

func TestConnectionPlanningPreservesOpenAITextCapability(t *testing.T) {
	service := newRequestPlanUnitService()
	// Strict mode equality requires the requested model format to equal the
	// connection capability; both use chat_completions_only here.
	snapshot := newRequestPlanSnapshot(runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "capability-openai", OpenAIAcceptedFormat: stringPtr(providerauth.OpenAITextCapabilityChatCompletionsOnly)})
	model := snapshot.ModelsByID["capability-openai"]
	snapshot.AccessTargetsBySourceModelID[model.ID] = nil
	addRequestPlanConnectionTargetWithOptions(snapshot, model, 2_401, 9_401, 0, requestPlanConnectionTargetOptions{openAITextCapability: stringPtr(providerauth.OpenAITextCapabilityChatCompletionsOnly)})
	plan := mustBuildRequestPlanForTest(t, service, snapshot, "/v1/chat/completions", []byte(`{"model":"capability-openai","messages":[]}`), RuntimeProxyConfigSnapshot{})
	if len(plan.TerminalAttempts) != 1 {
		t.Fatalf("expected one terminal attempt, got %d", len(plan.TerminalAttempts))
	}
	attempt := plan.TerminalAttempts[0]
	if attempt.Connection.OpenAITextCapability == nil || *attempt.Connection.OpenAITextCapability != providerauth.OpenAITextCapabilityChatCompletionsOnly {
		t.Fatalf("expected connection text capability %q, got %+v", providerauth.OpenAITextCapabilityChatCompletionsOnly, attempt.Connection.OpenAITextCapability)
	}
}

func forEachRequestPlanService(t *testing.T, name string, fn func(*testing.T, *Service)) {
	t.Helper()
	for _, test := range []struct {
		name       string
		newService func() *Service
	}{
		{name: "legacy", newService: newRequestPlanUnitService},
		{name: "enforced", newService: newRequestPlanUnitService},
	} {
		t.Run(test.name+"/"+name, func(t *testing.T) {
			fn(t, test.newService())
		})
	}
}

func mustBuildRequestPlanForTest(t *testing.T, service *Service, snapshot *planningSnapshot, path string, rawBody []byte, runtimeConfig RuntimeProxyConfigSnapshot) requestPlan {
	t.Helper()
	plan, err := buildRequestPlanForTest(t, service, snapshot, path, rawBody, runtimeConfig)
	if err != nil {
		t.Fatalf("build request plan: %v", err)
	}
	return plan
}

func buildRequestPlanErrorForTest(t *testing.T, service *Service, snapshot *planningSnapshot, path string, rawBody []byte, runtimeConfig RuntimeProxyConfigSnapshot) error {
	t.Helper()
	_, err := buildRequestPlanForTest(t, service, snapshot, path, rawBody, runtimeConfig)
	return err
}

func buildRequestPlanForTest(t *testing.T, service *Service, snapshot *planningSnapshot, path string, rawBody []byte, runtimeConfig RuntimeProxyConfigSnapshot) (requestPlan, error) {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, path, nil)
	operationMatch := mustResolveRuntimeOperation(t, http.MethodPost, request.URL.Path)
	return service.buildTestRequestPlanFromSnapshot(request, rawBody, runtimeConfig, operationMatch, requestPlanTestProfileID, snapshot)
}

func newRequestPlanUnitService() *Service {
	return &Service{
		runtimeState: loadbalance.NewLocalRuntimeStateStore(),
		now: func() time.Time {
			return time.Unix(1_700_000_000, 0).UTC()
		},
	}
}

func newEnforcedRequestPlanUnitService() *Service {
	return newRequestPlanUnitService()
}

func newRequestPlanSnapshot(models ...runtimeModelRecord) *planningSnapshot {
	strategyID := requestPlanTestStrategyID
	legacyStrategyType := "fill-first"
	strategy := loadbalance.RuntimeStrategy{ID: strategyID, Name: "test legacy", LegacyStrategyType: &legacyStrategyType}
	snapshot := &planningSnapshot{
		ModelsByID:                   map[string]runtimeModelRecord{},
		AccessTargetsBySourceModelID: map[int][]runtimeAccessTargetRecord{},
		TerminalTargetsByID:          map[int]runtimeConnection{},
		StrategiesByModelID:          map[int]loadbalance.RuntimeStrategy{},
		ReportCurrency:               runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$"},
	}
	for index, model := range models {
		if model.ID == 0 {
			model.ID = index + 1
		}
		if model.ProfileID == 0 {
			model.ProfileID = requestPlanTestProfileID
		}
		if model.LoadbalanceStrategyID == nil {
			model.LoadbalanceStrategyID = &strategyID
		}
		if providerauth.IsOpenAI(model.APIFamily) && model.OpenAIAcceptedFormat == nil {
			model.OpenAIAcceptedFormat = stringPtr(providerauth.OpenAITextCapabilityDualNative)
		}
		snapshot.ModelsByID[model.ModelID] = model
		snapshot.StrategiesByModelID[model.ID] = strategy
		connectionID := 1_000 + model.ID
		var openAITextCapability *string
		if providerauth.IsOpenAI(model.APIFamily) {
			openAITextCapability = stringPtr(providerauth.OpenAITextCapabilityDualNative)
		}
		snapshot.TerminalTargetsByID[connectionID] = runtimeConnection{
			ID:                   connectionID,
			ProfileID:            model.ProfileID,
			APIFamily:            model.APIFamily,
			ModelConfigID:        model.ID,
			EndpointID:           1,
			Priority:             1,
			UpstreamModelID:      stringPtr(model.ModelID),
			OpenAITextCapability: openAITextCapability,
			Endpoint:             runtimeEndpoint{ID: 1, BaseURL: "https://upstream.example"},
		}
		snapshot.AccessTargetsBySourceModelID[model.ID] = []runtimeAccessTargetRecord{{
			ID:                        connectionID,
			ProfileID:                 model.ProfileID,
			SourceModelConfigID:       model.ID,
			TargetType:                runtimeAccessTargetTypeConnection,
			TargetConnectionID:        intPtr(connectionID),
			TargetConnectionProfileID: model.ProfileID,
			TargetConnectionAPIFamily: model.APIFamily,
			Position:                  0,
			IsEnabled:                 true,
		}}
	}
	return snapshot
}

func addRequestPlanProxyTarget(snapshot *planningSnapshot, proxyModelID string, targetModelID string) {
	proxyModel := snapshot.ModelsByID[proxyModelID]
	snapshot.AccessTargetsBySourceModelID[proxyModel.ID] = nil
	addRequestPlanModelTargetAtPosition(snapshot, proxyModelID, targetModelID, 0)
}

func addRequestPlanModelTargetAtPosition(snapshot *planningSnapshot, proxyModelID string, targetModelID string, position int) {
	addRequestPlanModelTargetWithMetadata(snapshot, proxyModelID, targetModelID, position)
}

func addRequestPlanModelTargetWithMetadata(snapshot *planningSnapshot, proxyModelID string, targetModelID string, position int) {
	proxyModel := snapshot.ModelsByID[proxyModelID]
	targetModel := snapshot.ModelsByID[targetModelID]
	snapshot.AccessTargetsBySourceModelID[proxyModel.ID] = append(snapshot.AccessTargetsBySourceModelID[proxyModel.ID], runtimeAccessTargetRecord{
		ID:                   10_000 + targetModel.ID,
		ProfileID:            proxyModel.ProfileID,
		SourceModelConfigID:  proxyModel.ID,
		TargetType:           runtimeAccessTargetTypeModel,
		TargetModelConfigID:  intPtr(targetModel.ID),
		TargetModelID:        targetModel.ModelID,
		TargetModelProfileID: targetModel.ProfileID,
		TargetModelAPIFamily: targetModel.APIFamily,
		TargetModelEnabled:   true,
		Position:             position,
		IsEnabled:            true,
	})
}

type requestPlanConnectionTargetOptions struct {
	openAITextCapability    *string
	pricingTemplateSnapshot *runtimePricingTemplateSnapshot
	routingSchedule         terminaltarget.CompiledRoutingSchedule
}

func addRequestPlanConnectionTarget(snapshot *planningSnapshot, model runtimeModelRecord, connectionID int, targetID int, position int) {
	addRequestPlanConnectionTargetWithOptions(snapshot, model, connectionID, targetID, position, requestPlanConnectionTargetOptions{})
}

func addRequestPlanConnectionTargetWithOptions(snapshot *planningSnapshot, model runtimeModelRecord, connectionID int, targetID int, position int, options requestPlanConnectionTargetOptions) {
	openAITextCapability := options.openAITextCapability
	if openAITextCapability == nil && providerauth.IsOpenAI(model.APIFamily) {
		openAITextCapability = stringPtr(providerauth.OpenAITextCapabilityDualNative)
	}
	snapshot.TerminalTargetsByID[connectionID] = runtimeConnection{
		ID:                      connectionID,
		ProfileID:               model.ProfileID,
		APIFamily:               model.APIFamily,
		ModelConfigID:           model.ID,
		EndpointID:              1,
		Priority:                position,
		UpstreamModelID:         stringPtr(model.ModelID),
		PricingTemplateSnapshot: options.pricingTemplateSnapshot,
		OpenAITextCapability:    openAITextCapability,
		RoutingSchedule:         options.routingSchedule,
		Endpoint:                runtimeEndpoint{ID: 1, BaseURL: "https://upstream.example"},
	}
	snapshot.AccessTargetsBySourceModelID[model.ID] = append(snapshot.AccessTargetsBySourceModelID[model.ID], runtimeAccessTargetRecord{
		ID:                        targetID,
		ProfileID:                 model.ProfileID,
		SourceModelConfigID:       model.ID,
		TargetType:                runtimeAccessTargetTypeConnection,
		TargetConnectionID:        intPtr(connectionID),
		TargetConnectionProfileID: model.ProfileID,
		TargetConnectionAPIFamily: model.APIFamily,
		Position:                  position,
		IsEnabled:                 true,
	})
}

func assertRuntimeIntPtr(t *testing.T, got *int, want int, label string) {
	t.Helper()
	if got == nil || *got != want {
		t.Fatalf("expected %s %d, got %+v", label, want, got)
	}
}

func assertRuntimeStringPtr(t *testing.T, got *string, want string, label string) {
	t.Helper()
	if got == nil || *got != want {
		t.Fatalf("expected %s %q, got %+v", label, want, got)
	}
}

func mustResolveRuntimeOperation(t *testing.T, method string, requestPath string) RuntimeOperationMatch {
	t.Helper()
	operationMatch, ok := ResolveRuntimeOperation(method, requestPath)
	if !ok {
		t.Fatalf("expected runtime operation for %s %s", method, requestPath)
	}
	return operationMatch
}

func assertPlanDomainError(t *testing.T, err error, wantStatus int, detailContains string) {
	t.Helper()
	var domainErr *domainError
	if !errors.As(err, &domainErr) {
		t.Fatalf("expected domain error, got %v", err)
	}
	if domainErr.StatusCode != wantStatus {
		t.Fatalf("expected status %d, got %d with detail %q", wantStatus, domainErr.StatusCode, domainErr.Detail)
	}
	if !strings.Contains(domainErr.Detail, detailContains) {
		t.Fatalf("expected detail containing %q, got %q", detailContains, domainErr.Detail)
	}
}

func assertPlanDomainErrorCode(t *testing.T, err error, wantStatus int, wantCode string) {
	t.Helper()
	var domainErr *domainError
	if !errors.As(err, &domainErr) {
		t.Fatalf("expected domain error, got %v", err)
	}
	if domainErr.StatusCode != wantStatus {
		t.Fatalf("expected status %d, got %d with detail %q", wantStatus, domainErr.StatusCode, domainErr.Detail)
	}
	if domainErr.ErrorCode != wantCode {
		t.Fatalf("expected error code %q, got %q with detail %q", wantCode, domainErr.ErrorCode, domainErr.Detail)
	}
}
