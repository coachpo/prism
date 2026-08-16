package runtime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/coachpo/prism/backend/internal/domain/loadbalance"
	"github.com/coachpo/prism/backend/internal/domain/safediag"
	"github.com/coachpo/prism/backend/internal/domain/terminaltarget"
	"github.com/coachpo/prism/backend/internal/platform/config"
	"github.com/coachpo/prism/backend/internal/providerauth"
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

	plan, err := service.buildRequestPlanFromSnapshot(request, nil, RuntimeProxyConfigSnapshot{}, operationMatch, requestPlanTestProfileID, snapshot)
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

		_, err := service.buildRequestPlanFromSnapshot(request, rawBody, RuntimeProxyConfigSnapshot{}, operationMatch, requestPlanTestProfileID, snapshot)
		assertPlanDomainError(t, err, http.StatusBadRequest, "unsupported model binding source")
	})

	t.Run("generic OpenAI v1 path is not a planning fallback", func(t *testing.T) {
		service := newRequestPlanUnitService()
		snapshot := newRequestPlanSnapshot(runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "gpt-4o"})
		rawBody := []byte(`{"model":"gpt-4o"}`)
		request := httptest.NewRequest(http.MethodPost, "/v1/models", nil)
		operationMatch := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/chat/completions")

		_, err := service.buildRequestPlanFromSnapshot(request, rawBody, RuntimeProxyConfigSnapshot{}, operationMatch, requestPlanTestProfileID, snapshot)
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

func TestHeaderHelpers(t *testing.T) {
	if got, ok := normalizeHeaderValue("  keep  "); !ok || got != "keep" {
		t.Fatalf("expected normalized header value, got value=%q ok=%v", got, ok)
	}
	if _, ok := normalizeHeaderValue("bad\nvalue"); ok {
		t.Fatal("expected control-character header value to be rejected")
	}

	rules := []headerBlocklistRule{{MatchType: "exact", Pattern: "x-remove"}, {MatchType: "prefix", Pattern: "x-secret-"}}
	sanitized := sanitizeHeaders(map[string]string{"X-Trace-Id": "1", "x-secret-token": "blocked", "X-Remove": "gone"}, rules)
	if !reflect.DeepEqual(sanitized, map[string]string{"X-Trace-Id": "1"}) {
		t.Fatalf("expected blocklisted headers to be removed, got %v", sanitized)
	}

	filtered := filterResponseHeaders(http.Header{"Connection": []string{"keep-alive"}, "X-Request-Id": []string{"abc"}})
	if filtered.Get("Connection") != "" || filtered.Get("X-Request-Id") != "abc" {
		t.Fatalf("expected hop-by-hop response headers to be filtered, got %v", filtered)
	}
}

func TestRequestWantsStreamUsesGeminiStreamingPath(t *testing.T) {
	if !requestWantsStream(nil, "/v1beta/models/gemini-2.5-pro:streamGenerateContent") {
		t.Fatal("expected Gemini streamGenerateContent path to force streaming")
	}
	if requestWantsStream(nil, "/v1beta/models/gemini-2.5-pro:generateContent?alt=sse") {
		t.Fatal("expected generateContent path without stream body flag to remain non-streaming")
	}
	if !requestWantsStream([]byte(`{"stream":true}`), "/v1/chat/completions") {
		t.Fatal("expected explicit stream body flag to remain streaming for generic routes")
	}
}

func TestClassifySSEStreamOutcome(t *testing.T) {
	writeErr := errors.New("client write failed")
	upstreamErr := errors.New("upstream read failed")
	canceledContext, cancel := context.WithCancel(context.Background())
	cancel()

	tests := []struct {
		name        string
		ctx         context.Context
		terminal    sseTerminalSignal
		upstreamErr error
		writeErr    error
		wantOutcome string
		wantKind    *string
	}{
		{name: "client write failure", terminal: sseTerminalSignalNone, writeErr: writeErr, wantOutcome: runtimeStreamOutcomeClientDisconnected, wantKind: stringPtr(runtimeStreamErrorKindClientWriteFailed)},
		{name: "client write failure beats canceled context", ctx: canceledContext, terminal: sseTerminalSignalCompleted, upstreamErr: upstreamErr, writeErr: writeErr, wantOutcome: runtimeStreamOutcomeClientDisconnected, wantKind: stringPtr(runtimeStreamErrorKindClientWriteFailed)},
		{name: "request context canceled", ctx: canceledContext, terminal: sseTerminalSignalNone, wantOutcome: runtimeStreamOutcomeClientDisconnected, wantKind: stringPtr(runtimeStreamErrorKindRequestContextCanceled)},
		{name: "canceled context beats completed", ctx: canceledContext, terminal: sseTerminalSignalCompleted, wantOutcome: runtimeStreamOutcomeClientDisconnected, wantKind: stringPtr(runtimeStreamErrorKindRequestContextCanceled)},
		{name: "canceled context beats provider incomplete", ctx: canceledContext, terminal: sseTerminalSignalProviderIncomplete, wantOutcome: runtimeStreamOutcomeClientDisconnected, wantKind: stringPtr(runtimeStreamErrorKindRequestContextCanceled)},
		{name: "upstream read error", terminal: sseTerminalSignalNone, upstreamErr: upstreamErr, wantOutcome: runtimeStreamOutcomeUpstreamReadError, wantKind: stringPtr(runtimeStreamErrorKindUpstreamReadFailed)},
		{name: "upstream read error beats completed", terminal: sseTerminalSignalCompleted, upstreamErr: upstreamErr, wantOutcome: runtimeStreamOutcomeUpstreamReadError, wantKind: stringPtr(runtimeStreamErrorKindUpstreamReadFailed)},
		{name: "upstream read error beats provider incomplete", terminal: sseTerminalSignalProviderIncomplete, upstreamErr: upstreamErr, wantOutcome: runtimeStreamOutcomeUpstreamReadError, wantKind: stringPtr(runtimeStreamErrorKindUpstreamReadFailed)},
		{name: "completed", terminal: sseTerminalSignalCompleted, wantOutcome: runtimeStreamOutcomeCompleted},
		{name: "provider incomplete", terminal: sseTerminalSignalProviderIncomplete, wantOutcome: runtimeStreamOutcomeProviderIncomplete},
		{name: "missing terminal", terminal: sseTerminalSignalNone, wantOutcome: runtimeStreamOutcomeUpstreamEndedWithoutTerminal, wantKind: stringPtr(runtimeStreamErrorKindMissingTerminalEvent)},
		{name: "eof without terminal", terminal: sseTerminalSignalNone, upstreamErr: io.EOF, wantOutcome: runtimeStreamOutcomeUpstreamEndedWithoutTerminal, wantKind: stringPtr(runtimeStreamErrorKindMissingTerminalEvent)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := classifySSEStreamOutcome(test.ctx, test.terminal, test.upstreamErr, test.writeErr)
			if got.outcome != test.wantOutcome {
				t.Fatalf("expected outcome %q, got %+v", test.wantOutcome, got)
			}
			if !reflect.DeepEqual(got.kind, test.wantKind) {
				t.Fatalf("expected error kind %+v, got %+v", test.wantKind, got.kind)
			}
		})
	}
}

func TestProxyEventStreamClassifiesWriteAndReadFailures(t *testing.T) {
	operation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/responses").Operation
	writeFailure := errors.New("write failed\nwith\tcontrol")
	var forwarded bytes.Buffer
	capture, err := proxyEventStreamAndCaptureCompletedResponse(operation, context.Background(), failingWriter{err: writeFailure}, strings.NewReader("event: response.created\ndata: {\"type\":\"response.created\"}\n\n"), time.Now, false)
	if !errors.Is(err, writeFailure) {
		t.Fatalf("expected write failure, got %v", err)
	}
	if capture.StreamOutcome != runtimeStreamOutcomeClientDisconnected || capture.StreamErrorKind == nil || *capture.StreamErrorKind != runtimeStreamErrorKindClientWriteFailed || capture.StreamErrorDetail == nil || *capture.StreamErrorDetail != "write failed with control" {
		t.Fatalf("expected sanitized client-disconnected capture, got %+v", capture)
	}

	readFailure := errors.New("upstream read failed")
	capture, err = proxyEventStreamAndCaptureCompletedResponse(operation, context.Background(), &forwarded, &errorAfterReader{payload: []byte("event: response.created\ndata: {\"type\":\"response.created\"}\n\n"), err: readFailure}, time.Now, false)
	if !errors.Is(err, readFailure) {
		t.Fatalf("expected upstream read failure, got %v", err)
	}
	if capture.StreamOutcome != runtimeStreamOutcomeUpstreamReadError || capture.StreamErrorKind == nil || *capture.StreamErrorKind != runtimeStreamErrorKindUpstreamReadFailed || capture.StreamErrorDetail == nil || *capture.StreamErrorDetail != "upstream read failed" {
		t.Fatalf("expected upstream read error capture, got %+v", capture)
	}
}

func TestProxyEventStreamClassifiesEOFWithoutTerminal(t *testing.T) {
	operation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/responses").Operation
	var forwarded bytes.Buffer
	capture, err := proxyEventStreamAndCaptureCompletedResponse(operation, context.Background(), &forwarded, strings.NewReader("event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"usage\":null}}\n\n"), time.Now, false)
	if err != nil {
		t.Fatalf("proxy SSE without terminal: %v", err)
	}
	if capture.StreamOutcome != runtimeStreamOutcomeUpstreamEndedWithoutTerminal || capture.StreamErrorKind == nil || *capture.StreamErrorKind != runtimeStreamErrorKindMissingTerminalEvent || capture.StreamErrorDetail != nil || capture.CompletedAt != nil || capture.Usage.hasValues() {
		t.Fatalf("expected missing-terminal EOF capture without usage, got %+v", capture)
	}
}

func TestProxyEventStreamPreservesTerminalTimingWhenTransportOutranksTerminal(t *testing.T) {
	operation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/responses").Operation
	terminalStream := "event: response.completed\ndata: {\"type\":\"response.completed\"}\n\n"

	canceledContext, cancel := context.WithCancel(context.Background())
	defer cancel()
	var forwarded bytes.Buffer
	cancelingWriter := cancelOnBlankLineWriter{dst: &forwarded, cancel: cancel}
	capture, err := proxyEventStreamAndCaptureCompletedResponse(operation, canceledContext, cancelingWriter, strings.NewReader(terminalStream), time.Now, false)
	if err != nil {
		t.Fatalf("proxy SSE with canceled context after terminal: %v", err)
	}
	if capture.StreamOutcome != runtimeStreamOutcomeClientDisconnected || capture.StreamErrorKind == nil || *capture.StreamErrorKind != runtimeStreamErrorKindRequestContextCanceled || capture.CompletedAt == nil {
		t.Fatalf("expected context cancellation to outrank terminal while preserving completion timing, got %+v", capture)
	}

	readFailure := errors.New("upstream read failed after terminal")
	forwarded.Reset()
	capture, err = proxyEventStreamAndCaptureCompletedResponse(operation, context.Background(), &forwarded, &errorAfterReader{payload: []byte(terminalStream), err: readFailure}, time.Now, false)
	if !errors.Is(err, readFailure) {
		t.Fatalf("expected upstream read failure, got %v", err)
	}
	if capture.StreamOutcome != runtimeStreamOutcomeUpstreamReadError || capture.StreamErrorKind == nil || *capture.StreamErrorKind != runtimeStreamErrorKindUpstreamReadFailed || capture.CompletedAt == nil {
		t.Fatalf("expected upstream read error to outrank terminal while preserving completion timing, got %+v", capture)
	}
}

func TestProxyEventStreamRecognizesOpenAIDONESentinel(t *testing.T) {
	operation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/chat/completions").Operation
	pricingTemplateSnapshot := &runtimePricingTemplateSnapshot{RevisionID: 1, PricingUnit: runtimePricingUnitPerMillion, PricingCurrencyCode: "USD", ReportingCurrencyEpoch: intPtr(1), InputPrice: "2", OutputPrice: "5", CachedInputPrice: "0", CacheCreationPrice: "0", ReasoningPrice: "0", Version: 1}
	reportCurrencySnapshot := runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$", Epoch: 1}

	t.Run("completed without usage", func(t *testing.T) {
		stream := "data: {\"id\":\"chatcmpl-done\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"}}]}\n\n" +
			"data:   [DONE]  \n\n"
		var forwarded bytes.Buffer
		capture, err := proxyEventStreamAndCaptureCompletedResponse(operation, context.Background(), &forwarded, strings.NewReader(stream), time.Now, false)
		if err != nil {
			t.Fatalf("proxy SSE with [DONE]: %v", err)
		}
		if forwarded.String() != stream {
			t.Fatalf("expected SSE stream to pass through unchanged, got %q", forwarded.String())
		}
		if capture.StreamOutcome != runtimeStreamOutcomeCompleted || capture.CompletedAt == nil || capture.StreamErrorKind != nil || capture.StreamErrorDetail != nil || capture.Usage.hasValues() {
			t.Fatalf("expected completed [DONE] capture without usage, got %+v", capture)
		}

		pricing := buildRuntimePricingResult(reportCurrencySnapshot, pricingTemplateSnapshot, nil, capture.Usage, capture.StreamOutcome)
		if pricing.UnpricedReason == nil || *pricing.UnpricedReason != runtimeUnpricedReasonMissingUsage {
			t.Fatalf("expected completed [DONE] stream without usage to keep missing-token reason, got %+v", pricing)
		}
	})

	t.Run("other non JSON remains missing terminal", func(t *testing.T) {
		var forwarded bytes.Buffer
		capture, err := proxyEventStreamAndCaptureCompletedResponse(operation, context.Background(), &forwarded, strings.NewReader("data: not-json\n\n"), time.Now, false)
		if err != nil {
			t.Fatalf("proxy SSE with non-JSON payload: %v", err)
		}
		if capture.StreamOutcome != runtimeStreamOutcomeUpstreamEndedWithoutTerminal || capture.StreamErrorKind == nil || *capture.StreamErrorKind != runtimeStreamErrorKindMissingTerminalEvent || capture.CompletedAt != nil {
			t.Fatalf("expected non-[DONE] non-JSON payload to remain missing terminal, got %+v", capture)
		}
	})
}

func TestProxyEventStreamMergesUsageBeforeOpenAIDONESentinel(t *testing.T) {
	operation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/chat/completions").Operation
	stream := "data: {\"id\":\"chatcmpl-usage\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"}}],\"usage\":{\"prompt_tokens\":999,\"completion_tokens\":999,\"total_tokens\":1998}}\n\n" +
		"data: {\"id\":\"chatcmpl-usage\",\"object\":\"chat.completion.chunk\",\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":6,\"total_tokens\":16,\"prompt_tokens_details\":{\"cached_tokens\":4},\"completion_tokens_details\":{\"reasoning_tokens\":3}}}\n\n" +
		"data: [DONE]\n\n"
	var forwarded bytes.Buffer
	capture, err := proxyEventStreamAndCaptureCompletedResponse(operation, context.Background(), &forwarded, strings.NewReader(stream), time.Now, false)
	if err != nil {
		t.Fatalf("proxy SSE with usage and [DONE]: %v", err)
	}
	if capture.StreamOutcome != runtimeStreamOutcomeCompleted || capture.CompletedAt == nil || capture.StreamErrorKind != nil || capture.StreamErrorDetail != nil {
		t.Fatalf("expected usage stream ending with [DONE] to complete, got %+v", capture)
	}
	wantUsage := responseUsage{InputTokens: intPtr(6), OutputTokens: intPtr(3), TotalTokens: intPtr(16), CacheReadInputTokens: intPtr(4), ReasoningTokens: intPtr(3)}
	if !reflect.DeepEqual(capture.Usage, wantUsage) {
		t.Fatalf("expected final include_usage chunk to be preserved: want %+v got %+v", wantUsage, capture.Usage)
	}

	pricingTemplateSnapshot := &runtimePricingTemplateSnapshot{RevisionID: 1, PricingUnit: runtimePricingUnitPerMillion, PricingCurrencyCode: "USD", ReportingCurrencyEpoch: intPtr(1), InputPrice: "2", OutputPrice: "5", CachedInputPrice: "0", CacheCreationPrice: "0", ReasoningPrice: "0", Version: 1}
	pricing := buildRuntimePricingResult(runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$", Epoch: 1}, pricingTemplateSnapshot, nil, capture.Usage, capture.StreamOutcome)
	if !pricing.Billable || !pricing.Priced || pricing.UnpricedReason != nil {
		t.Fatalf("expected observed usage before [DONE] to price normally, got %+v", pricing)
	}
}

func TestProxyEventStreamMergesUsageFromOpenAIChatTerminalChoiceChunk(t *testing.T) {
	tests := []struct {
		name        string
		finishField string
	}{
		{name: "snake_case_finish_reason", finishField: `"finish_reason":"stop"`},
		{name: "camelCase_finishReason", finishField: `"finishReason":"stop"`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			operation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/chat/completions").Operation
			stream := "data: {\"id\":\"chatcmpl-usage\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"}}],\"usage\":{\"prompt_tokens\":999,\"completion_tokens\":999,\"total_tokens\":1998}}\n\n" +
				"data: {\"id\":\"chatcmpl-usage\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"final\"}," + test.finishField + "}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":6,\"total_tokens\":16,\"prompt_tokens_details\":{\"cached_tokens\":4},\"completion_tokens_details\":{\"reasoning_tokens\":3}}}\n\n" +
				"data: [DONE]\n\n"
			var forwarded bytes.Buffer
			capture, err := proxyEventStreamAndCaptureCompletedResponse(operation, context.Background(), &forwarded, strings.NewReader(stream), time.Now, false)
			if err != nil {
				t.Fatalf("proxy SSE with terminal choice usage and [DONE]: %v", err)
			}
			if capture.StreamOutcome != runtimeStreamOutcomeCompleted || capture.CompletedAt == nil || capture.StreamErrorKind != nil || capture.StreamErrorDetail != nil {
				t.Fatalf("expected usage stream ending with [DONE] to complete, got %+v", capture)
			}
			wantUsage := responseUsage{InputTokens: intPtr(6), OutputTokens: intPtr(3), TotalTokens: intPtr(16), CacheReadInputTokens: intPtr(4), ReasoningTokens: intPtr(3)}
			if !reflect.DeepEqual(capture.Usage, wantUsage) {
				t.Fatalf("expected terminal choice usage to win over earlier bogus root usage: want %+v got %+v", wantUsage, capture.Usage)
			}
		})
	}
}

func TestProxyEventStreamIgnoresOpenAIChatUsageWhenAnyChoiceIsNonTerminal(t *testing.T) {
	operation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/chat/completions").Operation
	stream := "data: {\"id\":\"chatcmpl-usage\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"},\"finish_reason\":\"stop\"},{\"index\":1,\"delta\":{\"content\":\"still-running\"}}],\"usage\":{\"prompt_tokens\":999,\"completion_tokens\":999,\"total_tokens\":1998}}\n\n" +
		"data: [DONE]\n\n"
	var forwarded bytes.Buffer
	capture, err := proxyEventStreamAndCaptureCompletedResponse(operation, context.Background(), &forwarded, strings.NewReader(stream), time.Now, false)
	if err != nil {
		t.Fatalf("proxy SSE with mixed choice usage and [DONE]: %v", err)
	}
	if capture.StreamOutcome != runtimeStreamOutcomeCompleted || capture.CompletedAt == nil || capture.StreamErrorKind != nil || capture.StreamErrorDetail != nil {
		t.Fatalf("expected usage stream ending with [DONE] to complete, got %+v", capture)
	}
	if capture.Usage.hasValues() {
		t.Fatalf("expected mixed terminal/non-terminal choice chunk usage to be ignored, got %+v", capture.Usage)
	}
}

func TestInsertedAuditTimesExcludeSuppressedRows(t *testing.T) {
	times := appendAuditTimeIfInserted(nil, time.Unix(0, 0), false)
	times = appendAuditTimeIfInserted(times, time.Unix(1, 0), true)
	if len(times) != 1 || !times[0].Equal(time.Unix(1, 0)) {
		t.Fatalf("expected only the inserted audit timestamp, got %v", times)
	}
}

func TestProxyEventStreamCapturesRawAuditBodyWhenEnabled(t *testing.T) {
	operation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/responses").Operation
	stream := "event: response.created\n" +
		"data: {\"type\":\"response.created\",\"response\":{\"usage\":{\"input_tokens\":999,\"output_tokens\":999,\"total_tokens\":1998}}}\n\n" +
		"event: response.completed\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":9,\"output_tokens\":7,\"total_tokens\":16,\"input_tokens_details\":{\"cached_tokens\":2},\"output_tokens_details\":{\"reasoning_tokens\":5}}}}\n\n"
	var forwarded bytes.Buffer
	capture, err := proxyEventStreamAndCaptureCompletedResponse(operation, context.Background(), &forwarded, strings.NewReader(stream), time.Now, true)
	if err != nil {
		t.Fatalf("proxy SSE with audit capture: %v", err)
	}
	if forwarded.String() != stream {
		t.Fatalf("expected SSE stream to pass through unchanged, got %q", forwarded.String())
	}
	if string(capture.AuditBody) != stream || !strings.Contains(string(capture.AuditBody), "event: response.completed") {
		t.Fatalf("expected raw framed SSE audit body, got %q", string(capture.AuditBody))
	}
	if bytes.Equal(capture.AuditBody, capture.Body) {
		t.Fatalf("expected raw audit body to differ from parsed completion body, got %q", string(capture.AuditBody))
	}
	wantUsage := responseUsage{InputTokens: intPtr(7), OutputTokens: intPtr(2), TotalTokens: intPtr(16), CacheReadInputTokens: intPtr(2), ReasoningTokens: intPtr(5)}
	if got := capture.extractedUsage(); !reflect.DeepEqual(got, wantUsage) {
		t.Fatalf("expected usage extraction to survive audit capture: want %+v got %+v", wantUsage, got)
	}
}

func TestAnthropicMessagesStreamUsageUsesFinalCumulativeOutput(t *testing.T) {
	operation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/messages").Operation
	stream := "event: message_start\n" +
		"data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":7,\"cache_read_input_tokens\":2,\"cache_creation_input_tokens\":3,\"output_tokens\":1}}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"do not synthesize reasoning\"}}\n\n" +
		"event: message_delta\n" +
		"data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":5}}\n\n" +
		"event: message_delta\n" +
		"data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":13}}\n\n" +
		"event: message_stop\n" +
		"data: {\"type\":\"message_stop\"}\n\n"
	var forwarded bytes.Buffer
	capture, err := proxyEventStreamAndCaptureCompletedResponse(operation, context.Background(), &forwarded, strings.NewReader(stream), time.Now, false)
	if err != nil {
		t.Fatalf("proxy Anthropic SSE stream: %v", err)
	}
	if forwarded.String() != stream {
		t.Fatalf("expected SSE stream to pass through unchanged, got %q", forwarded.String())
	}
	if capture.StreamOutcome != runtimeStreamOutcomeCompleted || capture.CompletedAt == nil || capture.StreamErrorKind != nil || capture.StreamErrorDetail != nil {
		t.Fatalf("expected Anthropic stream to complete cleanly, got %+v", capture)
	}
	wantUsage := responseUsage{InputTokens: intPtr(7), OutputTokens: intPtr(13), TotalTokens: intPtr(25), CacheReadInputTokens: intPtr(2), CacheCreationInputTokens: intPtr(3)}
	if got := capture.extractedUsage(); !reflect.DeepEqual(got, wantUsage) {
		t.Fatalf("expected final cumulative output without summing deltas: want %+v got %+v", wantUsage, got)
	}
}

func TestSSEStreamHooksByOperation(t *testing.T) {
	tests := []struct {
		name         string
		requestPath  string
		stream       string
		wantProvider string
		wantKind     operationResponseKind
		wantOutcome  string
		wantUsage    responseUsage
	}{
		{
			name:         "openai responses completed owns response terminal",
			requestPath:  "/v1/responses",
			stream:       "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"response\":{\"usage\":{\"input_tokens\":999,\"output_tokens\":999,\"total_tokens\":1998}},\"delta\":\"partial\"}\n\nevent: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":5,\"output_tokens\":8,\"total_tokens\":13,\"input_tokens_details\":{\"cached_tokens\":2},\"output_tokens_details\":{\"reasoning_tokens\":3}}}}\n\n",
			wantProvider: "openai",
			wantKind:     operationResponseKindTextGeneration,
			wantOutcome:  runtimeStreamOutcomeCompleted,
			wantUsage:    responseUsage{InputTokens: intPtr(3), OutputTokens: intPtr(5), TotalTokens: intPtr(13), CacheReadInputTokens: intPtr(2), ReasoningTokens: intPtr(3)},
		},
		{
			name:         "openai responses incomplete owns provider incomplete terminal",
			requestPath:  "/v1/responses",
			stream:       "event: response.incomplete\ndata: {\"type\":\"response.incomplete\"}\n\n",
			wantProvider: "openai",
			wantKind:     operationResponseKindTextGeneration,
			wantOutcome:  runtimeStreamOutcomeProviderIncomplete,
		},
		{
			name:         "openai responses failed owns provider incomplete terminal",
			requestPath:  "/v1/responses",
			stream:       "event: response.failed\ndata: {\"type\":\"response.failed\"}\n\n",
			wantProvider: "openai",
			wantKind:     operationResponseKindTextGeneration,
			wantOutcome:  runtimeStreamOutcomeProviderIncomplete,
		},
		{
			name:         "anthropic messages owns message stop",
			requestPath:  "/v1/messages",
			stream:       "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":11}}}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":7,\"total_tokens\":18}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
			wantProvider: "anthropic",
			wantKind:     operationResponseKindTextGeneration,
			wantOutcome:  runtimeStreamOutcomeCompleted,
			wantUsage:    responseUsage{InputTokens: intPtr(11), OutputTokens: intPtr(7), TotalTokens: intPtr(18)},
		},
		{
			name:         "gemini stream generate owns usage metadata terminal",
			requestPath:  "/v1beta/models/gemini-2.5-pro:streamGenerateContent",
			stream:       "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hello\"}]}}],\"usageMetadata\":{\"promptTokenCount\":7,\"candidatesTokenCount\":13,\"totalTokenCount\":25,\"cachedContentTokenCount\":3,\"thoughtsTokenCount\":5}}\n\n",
			wantProvider: "gemini",
			wantKind:     operationResponseKindTextGeneration,
			wantOutcome:  runtimeStreamOutcomeCompleted,
			wantUsage:    responseUsage{InputTokens: intPtr(4), OutputTokens: intPtr(13), TotalTokens: intPtr(25), CacheReadInputTokens: intPtr(3), ReasoningTokens: intPtr(5)},
		},
		{
			name:         "gemini stream generate owns done terminal",
			requestPath:  "/v1beta/models/gemini-2.5-pro:streamGenerateContent",
			stream:       "data: {\"done\":true}\n\n",
			wantProvider: "gemini",
			wantKind:     operationResponseKindTextGeneration,
			wantOutcome:  runtimeStreamOutcomeCompleted,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			operation := mustResolveRuntimeOperation(t, http.MethodPost, test.requestPath).Operation
			hooks, ok := streamHooksForProxyResponse(operation, true)
			if !ok {
				t.Fatalf("expected stream hooks for %s", operation.Name)
			}
			if hooks.Provider != test.wantProvider || hooks.Kind != test.wantKind {
				t.Fatalf("expected %s/%s stream hooks, got %+v", test.wantProvider, test.wantKind, hooks)
			}
			var forwarded bytes.Buffer
			capture, err := proxyEventStreamAndCaptureCompletedResponse(operation, context.Background(), &forwarded, strings.NewReader(test.stream), time.Now, false)
			if err != nil {
				t.Fatalf("proxy SSE stream: %v", err)
			}
			if forwarded.String() != test.stream {
				t.Fatalf("expected SSE stream to pass through unchanged, got %q", forwarded.String())
			}
			if capture.StreamOutcome != test.wantOutcome {
				t.Fatalf("expected outcome %q, got %+v", test.wantOutcome, capture)
			}
			if got := capture.extractedUsage(); !reflect.DeepEqual(got, test.wantUsage) {
				t.Fatalf("expected usage %+v, got %+v", test.wantUsage, got)
			}
		})
	}

	responsesHooks, _ := streamHooksForOperation(mustResolveRuntimeOperation(t, http.MethodPost, "/v1/responses").Operation)
	if got := responsesHooks.terminalSignal("message_stop", map[string]any{"type": "message_stop"}); got != sseTerminalSignalNone {
		t.Fatalf("expected OpenAI responses hook to ignore Anthropic message_stop, got %d", got)
	}
	anthropicHooks, _ := streamHooksForOperation(mustResolveRuntimeOperation(t, http.MethodPost, "/v1/messages").Operation)
	if got := anthropicHooks.terminalSignal("response.completed", map[string]any{"type": "response.completed"}); got != sseTerminalSignalNone {
		t.Fatalf("expected Anthropic hook to ignore OpenAI response.completed, got %d", got)
	}
	geminiHooks, _ := streamHooksForOperation(mustResolveRuntimeOperation(t, http.MethodPost, "/v1beta/models/gemini-2.5-pro:streamGenerateContent").Operation)
	if got := geminiHooks.terminalSignal("response.completed", map[string]any{"type": "response.completed"}); got != sseTerminalSignalNone {
		t.Fatalf("expected Gemini hook to ignore OpenAI response.completed, got %d", got)
	}
}

func TestNonStreamOperationsCannotUseSSEHooks(t *testing.T) {
	responsesOperation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/responses").Operation
	if _, ok := streamHooksForProxyResponse(responsesOperation, false); ok {
		t.Fatal("expected non-stream OpenAI responses request to skip SSE hook selection")
	}

	geminiStreamPath := "/v1beta/models/gemini-2.5-pro:streamGenerateContent"
	geminiStreamOperation := mustResolveRuntimeOperation(t, http.MethodPost, geminiStreamPath).Operation
	if !requestWantsStreamForOperation(geminiStreamOperation, nil, geminiStreamPath) {
		t.Fatal("expected Gemini streamGenerateContent path to imply streaming")
	}
	if _, ok := streamHooksForProxyResponse(geminiStreamOperation, true); !ok {
		t.Fatal("expected Gemini streamGenerateContent to select SSE hooks")
	}

	tests := []struct {
		name        string
		requestPath string
	}{
		{name: "anthropic count tokens", requestPath: "/v1/messages/count_tokens"},
		{name: "gemini generate content", requestPath: "/v1beta/models/gemini-2.5-pro:generateContent"},
		{name: "gemini count tokens", requestPath: "/v1beta/models/gemini-2.5-pro:countTokens"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			operation := mustResolveRuntimeOperation(t, http.MethodPost, test.requestPath).Operation
			if _, ok := streamHooksForOperation(operation); ok {
				t.Fatalf("expected no SSE hooks for %s", operation.Name)
			}
			if _, ok := streamHooksForProxyResponse(operation, true); ok {
				t.Fatalf("expected %s to skip SSE parser dispatch even when forced streaming", operation.Name)
			}
		})
	}
}

func TestSanitizedStreamErrorDetailNormalizesRedactsAndTruncates(t *testing.T) {
	detail := sanitizedStreamErrorDetail(errors.New("  first\nsecond\tthird  "))
	if detail == nil || *detail != "first second third" {
		t.Fatalf("expected whitespace-normalized detail, got %+v", detail)
	}

	redacted := sanitizedStreamErrorDetail(errors.New("upstream read failed; Authorization: Bearer secret-token-123; x-api-key=abc123; body={\"messages\":[{\"role\":\"user\",\"content\":\"hello\"}]}; Cookie: session=abc"))
	if redacted == nil {
		t.Fatal("expected redacted stream error detail")
	}
	if strings.Contains(*redacted, "secret-token-123") || strings.Contains(*redacted, "abc123") || strings.Contains(*redacted, "hello") || strings.Contains(*redacted, "session=abc") || strings.Contains(strings.ToLower(*redacted), "authorization:") || strings.Contains(strings.ToLower(*redacted), "cookie:") {
		t.Fatalf("expected secret-bearing fragments to be redacted, got %q", *redacted)
	}
	if !strings.Contains(*redacted, "upstream read failed") {
		t.Fatalf("expected useful operator detail to survive, got %q", *redacted)
	}

	longDetail := sanitizedStreamErrorDetail(errors.New(strings.Repeat("x", runtimeStreamErrorDetailMaxLength+20)))
	if longDetail == nil || len(*longDetail) != runtimeStreamErrorDetailMaxLength {
		t.Fatalf("expected truncated stream error detail, got length %d", len(*longDetail))
	}
}

func TestRuntimeDiagnosticFailureFieldsAlwaysWriteSafeReadableDetail(t *testing.T) {
	tests := []struct {
		name      string
		rowKind   string
		errorCode string
		wantStage string
		wantText  string
	}{
		{name: "routing fallback", rowKind: requestLogRowKindPlanning, wantStage: failureStageRouting, wantText: "routing"},
		{name: "admission reason", rowKind: requestLogRowKindAdmission, errorCode: "admission_exhausted", wantStage: failureStageAdmission, wantText: "admission_exhausted"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requestLog := requestLogInsert{RowKind: test.rowKind}
			runtimeErr := &domainError{
				ErrorCode: test.errorCode,
				Detail:    "",
			}
			applyRuntimeDiagnosticFailureFields(&requestLog, runtimeErr)

			if requestLog.ErrorDetail == nil || strings.TrimSpace(*requestLog.ErrorDetail) == "" {
				t.Fatal("expected non-empty error detail")
			}
			if len(*requestLog.ErrorDetail) > safediag.MaxErrorDetailBytes {
				t.Fatalf("expected error detail cap, got %d bytes", len(*requestLog.ErrorDetail))
			}
			if strings.Contains(*requestLog.ErrorDetail, "secret-token") {
				t.Fatalf("expected credential redaction, got %q", *requestLog.ErrorDetail)
			}
			if *requestLog.FailureStage != test.wantStage || !strings.Contains(*requestLog.ErrorDetail, test.wantText) {
				t.Fatalf("expected stage %q and readable reason containing %q, got stage=%q detail=%q", test.wantStage, test.wantText, *requestLog.FailureStage, *requestLog.ErrorDetail)
			}
		})
	}

	requestLog := requestLogInsert{RowKind: requestLogRowKindPlanning}
	applyRuntimeDiagnosticFailureFields(&requestLog, &domainError{Detail: "Authorization: Bearer secret-token; route rejected after policy evaluation"})
	if requestLog.ErrorDetail == nil || strings.Contains(*requestLog.ErrorDetail, "secret-token") || !requestLog.ErrorDetailRedacted {
		t.Fatalf("expected redacted diagnostic detail, got %+v", requestLog.ErrorDetail)
	}
}

func TestSyntheticFailureMetadataScrubPreservesCallerRequestID(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	request = request.WithContext(withRuntimeIngressContext(request.Context(), runtimeIngressContext{
		ingressRequestID: newRuntimeUUIDv4(),
		callerRequestID:  "syntheticcalleralpha",
	}))
	requestLog := requestLogInsert{
		OperationName:   mustResolveRuntimeOperation(t, http.MethodPost, "/v1/chat/completions").Operation.Name,
		CallerUserAgent: stringPtr("matrix-test-agent"),
	}

	applyRuntimePlanningFailureMetadataScrub(&requestLog, request)

	if requestLog.CallerRequestID == nil || *requestLog.CallerRequestID != "syntheticcalleralpha" {
		t.Fatalf("expected synthetic failure caller correlation, got %+v", requestLog.CallerRequestID)
	}
	if requestLog.RequestPath != "/v1/chat/completions" {
		t.Fatalf("expected scrubbed request path, got %q", requestLog.RequestPath)
	}
}

func TestBuildRuntimePricingResultUsesStreamUsageUnavailableOnlyForInterruptedStreams(t *testing.T) {
	pricingTemplateSnapshot := runtimePricingTemplateForTest(nil)
	reportCurrencySnapshot := runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$", Epoch: 1}
	inputTokens := 7
	outputTokens := 13

	priced := buildRuntimePricingResult(reportCurrencySnapshot, pricingTemplateSnapshot, nil, responseUsage{InputTokens: &inputTokens, OutputTokens: &outputTokens}, runtimeStreamOutcomeCompleted)
	if !priced.Billable || !priced.Priced || priced.UnpricedReason != nil || priced.TotalCostUserCurrencyMicros == nil || *priced.TotalCostUserCurrencyMicros != 79 {
		t.Fatalf("expected completed stream with observed usage to price normally, got %+v", priced)
	}

	tests := []struct {
		name       string
		outcome    string
		wantReason string
	}{
		{name: "completed", outcome: runtimeStreamOutcomeCompleted, wantReason: runtimeUnpricedReasonMissingUsage},
		{name: "not streaming", outcome: runtimeStreamOutcomeNotStreaming, wantReason: runtimeUnpricedReasonMissingUsage},
		{name: "provider incomplete", outcome: runtimeStreamOutcomeProviderIncomplete, wantReason: runtimeUnpricedReasonStreamUsageUnavailable},
		{name: "client disconnected", outcome: runtimeStreamOutcomeClientDisconnected, wantReason: runtimeUnpricedReasonStreamUsageUnavailable},
		{name: "upstream read error", outcome: runtimeStreamOutcomeUpstreamReadError, wantReason: runtimeUnpricedReasonStreamUsageUnavailable},
		{name: "missing terminal", outcome: runtimeStreamOutcomeUpstreamEndedWithoutTerminal, wantReason: runtimeUnpricedReasonStreamUsageUnavailable},
		{name: "unknown", outcome: runtimeStreamOutcomeUnknown, wantReason: runtimeUnpricedReasonStreamUsageUnavailable},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := buildRuntimePricingResult(reportCurrencySnapshot, pricingTemplateSnapshot, nil, responseUsage{}, test.outcome)
			if got.UnpricedReason == nil || *got.UnpricedReason != test.wantReason {
				t.Fatalf("expected missing usage with outcome %q to use %q, got %+v", test.outcome, test.wantReason, got)
			}
		})
	}
}

func TestBuildRuntimePricingResultRequiresUsageBeforePriceData(t *testing.T) {
	tests := []struct {
		name                    string
		reportCurrencySnapshot  runtimeReportCurrencySnapshot
		pricingTemplateSnapshot *runtimePricingTemplateSnapshot
		endpointFXSnapshot      *runtimeEndpointFXSnapshot
		streamOutcome           string
		wantReason              string
	}{
		{
			name:                   "pricing disabled beats interrupted missing usage",
			reportCurrencySnapshot: runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$", Epoch: 1},
			streamOutcome:          runtimeStreamOutcomeUpstreamReadError,
			wantReason:             runtimeUnpricedReasonPricingOff,
		},
		{
			name:                   "interrupted missing usage beats invalid input price",
			reportCurrencySnapshot: runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$", Epoch: 1},
			pricingTemplateSnapshot: runtimePricingTemplateForTest(func(snapshot *runtimePricingTemplateSnapshot) {
				snapshot.InputPrice = "not-a-decimal"
			}),
			streamOutcome: runtimeStreamOutcomeUpstreamReadError,
			wantReason:    runtimeUnpricedReasonStreamUsageUnavailable,
		},
		{
			name:                   "completed missing usage beats invalid output price",
			reportCurrencySnapshot: runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$", Epoch: 1},
			pricingTemplateSnapshot: runtimePricingTemplateForTest(func(snapshot *runtimePricingTemplateSnapshot) {
				snapshot.OutputPrice = "not-a-decimal"
			}),
			streamOutcome: runtimeStreamOutcomeCompleted,
			wantReason:    runtimeUnpricedReasonMissingUsage,
		},
		{
			name:                    "interrupted missing usage beats missing fx",
			reportCurrencySnapshot:  runtimeReportCurrencySnapshot{Code: "EUR", Symbol: "EUR", Epoch: 1},
			pricingTemplateSnapshot: runtimePricingTemplateForTest(nil),
			streamOutcome:           runtimeStreamOutcomeUpstreamEndedWithoutTerminal,
			wantReason:              runtimeUnpricedReasonStreamUsageUnavailable,
		},
		{
			name:                    "completed missing usage beats invalid fx",
			reportCurrencySnapshot:  runtimeReportCurrencySnapshot{Code: "EUR", Symbol: "EUR", Epoch: 1},
			pricingTemplateSnapshot: runtimePricingTemplateForTest(nil),
			endpointFXSnapshot:      &runtimeEndpointFXSnapshot{FXRate: "not-a-decimal"},
			streamOutcome:           runtimeStreamOutcomeCompleted,
			wantReason:              runtimeUnpricedReasonMissingUsage,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := buildRuntimePricingResult(test.reportCurrencySnapshot, test.pricingTemplateSnapshot, test.endpointFXSnapshot, responseUsage{}, test.streamOutcome)
			if got.UnpricedReason == nil || *got.UnpricedReason != test.wantReason {
				t.Fatalf("expected reason %q, got %+v", test.wantReason, got)
			}
		})
	}
}

func TestBuildRuntimePricingResultValidatesPricingOwnerCoherence(t *testing.T) {
	inputTokens, outputTokens := 1, 1
	completeUsage := responseUsage{InputTokens: &inputTokens, OutputTokens: &outputTokens}
	tests := []struct {
		name           string
		report         runtimeReportCurrencySnapshot
		mutate         func(*runtimePricingTemplateSnapshot)
		usage          responseUsage
		wantStatus     string
		wantResolution string
	}{
		{name: "coherent snapshot prices", report: runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$", Epoch: 1}, usage: completeUsage, wantStatus: runtimePricingStatusPriced},
		{name: "missing revision beats missing usage", report: runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$", Epoch: 1}, mutate: func(snapshot *runtimePricingTemplateSnapshot) { snapshot.RevisionID = 0 }, wantStatus: runtimePricingStatusUnpriced, wantResolution: runtimePricingResolutionCurrencyMigrationRequired},
		{name: "missing template epoch beats missing usage", report: runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$", Epoch: 1}, mutate: func(snapshot *runtimePricingTemplateSnapshot) { snapshot.ReportingCurrencyEpoch = nil }, wantStatus: runtimePricingStatusUnpriced, wantResolution: runtimePricingResolutionCurrencyMigrationRequired},
		{name: "report epoch mismatch fails closed", report: runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$", Epoch: 2}, usage: completeUsage, wantStatus: runtimePricingStatusUnpriced, wantResolution: runtimePricingResolutionSnapshotIncoherent},
		{name: "currency mismatch fails closed", report: runtimeReportCurrencySnapshot{Code: "EUR", Symbol: "€", Epoch: 1}, usage: completeUsage, wantStatus: runtimePricingStatusUnpriced, wantResolution: runtimePricingResolutionSnapshotIncoherent},
		{name: "missing report epoch fails closed", report: runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$"}, usage: completeUsage, wantStatus: runtimePricingStatusUnpriced, wantResolution: runtimePricingResolutionSnapshotIncoherent},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := buildRuntimePricingResult(test.report, runtimePricingTemplateForTest(test.mutate), nil, test.usage, runtimeStreamOutcomeCompleted)
			if got.PricingStatus != test.wantStatus || got.Priced != (test.wantStatus == runtimePricingStatusPriced) || dereferenceString(got.PricingResolutionKind) != test.wantResolution {
				t.Fatalf("expected status=%q resolution=%q, got %+v", test.wantStatus, test.wantResolution, got)
			}
			if test.report.Epoch > 0 && (got.ReportingCurrencyEpoch == nil || *got.ReportingCurrencyEpoch != test.report.Epoch) {
				t.Fatalf("expected capture-time reporting epoch %d to survive classification, got %+v", test.report.Epoch, got)
			}
			if test.wantResolution != "" && (got.UnpricedReason == nil || *got.UnpricedReason != runtimeUnpricedReasonMissingData) {
				t.Fatalf("expected owner coherence failure to use missing-data reason, got %+v", got)
			}
		})
	}
}

func TestNewRuntimeHTTPClientUsesTransportDefaultsAndOverrides(t *testing.T) {
	defaultClient := newRuntimeHTTPClient(config.Load())
	if defaultClient.Timeout != 300*time.Second {
		t.Fatalf("expected canonical runtime HTTP client timeout 300s, got %v", defaultClient.Timeout)
	}
	defaultTransport, ok := defaultClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected canonical runtime HTTP transport, got %T", defaultClient.Transport)
	}
	if defaultTransport.MaxIdleConns != 100 || defaultTransport.MaxIdleConnsPerHost != 16 || defaultTransport.MaxConnsPerHost != 16 {
		t.Fatalf("expected canonical runtime transport caps 100/16/16, got %+v", defaultTransport)
	}
	if defaultTransport.IdleConnTimeout != 90*time.Second || defaultTransport.ResponseHeaderTimeout != 0 || defaultTransport.TLSHandshakeTimeout != 10*time.Second || defaultTransport.ExpectContinueTimeout != time.Second {
		t.Fatalf("unexpected canonical runtime transport timeouts: %+v", defaultTransport)
	}

	zeroSettings := config.Load()
	zeroSettings.RuntimeTransportConfig.MaxConnsPerHost = 0
	zeroClient := newRuntimeHTTPClient(zeroSettings)
	zeroTransport, ok := zeroClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected explicit-zero runtime HTTP transport, got %T", zeroClient.Transport)
	}
	if zeroTransport.MaxConnsPerHost != 0 {
		t.Fatalf("expected explicit maxConnsPerHost=0 to remain unlimited, got %+v", zeroTransport)
	}

	settings := config.Load()
	settings.RuntimeTransportConfig.RequestTimeout = 17 * time.Second
	configuredClient := newRuntimeHTTPClient(settings)
	if configuredClient.Timeout != 17*time.Second {
		t.Fatalf("expected configured runtime HTTP client timeout 17s, got %v", configuredClient.Timeout)
	}
}

func TestRuntimeProxyConfigProviderUpdatesNewPlansAndKeepsExistingPlanClient(t *testing.T) {
	oldClient := &http.Client{Timeout: 17 * time.Second}
	newClient := &http.Client{Timeout: 23 * time.Second}
	provider := &mutableRuntimeProxyConfigProvider{snapshot: RuntimeProxyConfigSnapshot{HTTPClient: oldClient}}
	service := &Service{runtimeProxyConfigProvider: provider}

	oldSnapshot := service.runtimeProxyConfigSnapshot()
	oldPlan := requestPlan{HTTPClient: oldSnapshot.HTTPClient}
	provider.snapshot = RuntimeProxyConfigSnapshot{HTTPClient: newClient}
	newSnapshot := service.runtimeProxyConfigSnapshot()
	newPlan := requestPlan{HTTPClient: newSnapshot.HTTPClient}

	if oldPlan.HTTPClient != oldClient || oldPlan.HTTPClient.Timeout != 17*time.Second {
		t.Fatalf("expected existing plan to keep old client snapshot, got %+v", oldPlan.HTTPClient)
	}
	if newPlan.HTTPClient != newClient || newPlan.HTTPClient.Timeout != 23*time.Second {
		t.Fatalf("expected new plan to use updated client snapshot, got %+v", newPlan.HTTPClient)
	}
}

func TestRuntimeProxyConfigProviderDoesNotRequireBufferingMode(t *testing.T) {
	client := &http.Client{Timeout: 19 * time.Second}
	provider := &mutableRuntimeProxyConfigProvider{snapshot: RuntimeProxyConfigSnapshot{HTTPClient: client}}
	service := &Service{runtimeProxyConfigProvider: provider}

	snapshot := service.runtimeProxyConfigSnapshot()
	if snapshot.HTTPClient != client {
		t.Fatalf("expected runtime proxy snapshot to carry HTTP client only, got %+v", snapshot.HTTPClient)
	}

	snapshotType := reflect.TypeOf(snapshot)
	for _, name := range []string{"BufferingMode", "bufferingMode"} {
		if _, ok := snapshotType.FieldByName(name); ok {
			t.Fatalf("%s still exposes %s", snapshotType.Name(), name)
		}
		if _, ok := snapshotType.MethodByName(name); ok {
			t.Fatalf("%s still exposes %s()", snapshotType.Name(), name)
		}
	}
}

func TestBuildRuntimePricingResult(t *testing.T) {
	pricingTemplateSnapshot := runtimePricingTemplateForTest(nil)
	reportCurrencySnapshot := runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$", Epoch: 1}
	zero := 0
	positiveCacheRead := 4
	positiveCacheCreation := 5
	positiveReasoning := 6
	inputTokens := 10
	outputTokens := 10
	totalTokens := 20

	tests := []struct {
		name     string
		template *runtimePricingTemplateSnapshot
		usage    responseUsage
		want     runtimePricingResult
	}{
		{
			name: "prices base usage when optional counters are omitted",
			usage: responseUsage{
				InputTokens:  &inputTokens,
				OutputTokens: &outputTokens,
				TotalTokens:  &totalTokens,
			},
			want: basePricedResult(nil),
		},
		{
			name: "keeps missing token usage for missing required base usage",
			usage: responseUsage{
				OutputTokens: &outputTokens,
				TotalTokens:  &outputTokens,
			},
			want: runtimePricingResult{
				Billable:                      true,
				UnpricedReason:                stringPtr(runtimeUnpricedReasonMissingUsage),
				PricingStatus:                 runtimePricingStatusUnpriced,
				PricingEvidenceTrust:          runtimePricingEvidenceTrust,
				PricingTemplateIDUsed:         intPtr(42),
				PricingTemplateRevisionIDUsed: int64Ptr(7),
				ReportingCurrencyEpoch:        intPtr(1),
			},
		},
		{
			name: "prices optional counters explicitly set to zero",
			usage: responseUsage{
				InputTokens:              &inputTokens,
				OutputTokens:             &outputTokens,
				TotalTokens:              &totalTokens,
				CacheReadInputTokens:     &zero,
				CacheCreationInputTokens: &zero,
				ReasoningTokens:          &zero,
			},
			want: basePricedResult(nil),
		},
		{
			name: "prices positive optional counters independently",
			usage: responseUsage{
				InputTokens:              &inputTokens,
				OutputTokens:             &outputTokens,
				TotalTokens:              &totalTokens,
				CacheReadInputTokens:     &positiveCacheRead,
				CacheCreationInputTokens: &positiveCacheCreation,
				ReasoningTokens:          &positiveReasoning,
			},
			want: basePricedResult(func(want *runtimePricingResult) {
				want.CacheReadInputCostMicros = int64Ptr(4)
				want.CacheCreationInputCostMicros = int64Ptr(10)
				want.ReasoningCostMicros = int64Ptr(18)
				want.TotalCostOriginalMicros = int64Ptr(102)
				want.TotalCostUserCurrencyMicros = int64Ptr(102)
			}),
		},
		{
			name: "prices positive component counters with concrete zero prices as free",
			template: runtimePricingTemplateForTest(func(snapshot *runtimePricingTemplateSnapshot) {
				snapshot.CachedInputPrice = "0"
				snapshot.CacheCreationPrice = "0"
				snapshot.ReasoningPrice = "0"
			}),
			usage: responseUsage{
				InputTokens:              &inputTokens,
				OutputTokens:             &outputTokens,
				TotalTokens:              &totalTokens,
				CacheReadInputTokens:     &positiveCacheRead,
				CacheCreationInputTokens: &positiveCacheCreation,
				ReasoningTokens:          &positiveReasoning,
			},
			want: basePricedResult(func(want *runtimePricingResult) {
				want.PricingSnapshotCacheReadInput = stringPtr("0")
				want.PricingSnapshotCacheCreationInput = stringPtr("0")
				want.PricingSnapshotReasoning = stringPtr("0")
			}),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			template := pricingTemplateSnapshot
			if test.template != nil {
				template = test.template
			}
			got := buildRuntimePricingResult(reportCurrencySnapshot, template, nil, test.usage, runtimeStreamOutcomeCompleted)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("expected pricing result %+v, got %+v", test.want, got)
			}
		})
	}
}

func TestEnforceRuntimeSpendCoherence(t *testing.T) {
	success := true
	cost := int64(1250)

	got := enforceRuntimeSpendCoherence(success, runtimePricingResult{
		Billable:                    true,
		Priced:                      true,
		TotalCostUserCurrencyMicros: nil,
	})
	if !got.Billable || got.Priced || got.UnpricedReason == nil || *got.UnpricedReason != runtimeUnpricedReasonMissingData {
		t.Fatalf("expected priced result without cost to degrade to missing price data, got %+v", got)
	}

	got = enforceRuntimeSpendCoherence(success, runtimePricingResult{
		Billable:                    true,
		Priced:                      false,
		TotalCostUserCurrencyMicros: &cost,
		CurrencyCodeOriginal:        stringPtr("USD"),
		ReportCurrencyCode:          stringPtr("USD"),
	})
	if !got.Priced || got.UnpricedReason != nil || got.FXRateUsed == nil || *got.FXRateUsed != "1" || got.FXRateSource == nil || *got.FXRateSource != runtimeFXSourceDefaultOneToOne {
		t.Fatalf("expected cost-bearing result to become priced with same-currency FX defaults, got %+v", got)
	}

	reason := "  MISSING_TOKEN_USAGE  "
	got = enforceRuntimeSpendCoherence(success, runtimePricingResult{
		Billable:       true,
		Priced:         true,
		UnpricedReason: &reason,
	})
	if got.Priced || got.UnpricedReason == nil || *got.UnpricedReason != runtimeUnpricedReasonMissingUsage {
		t.Fatalf("expected explicit unpriced reason to win and trim, got %+v", got)
	}
}

func TestBuildRuntimePricingResultRejectsInvalidConcretePriceWhenComponentIsUsed(t *testing.T) {
	inputTokens := 10
	outputTokens := 10
	totalTokens := 20
	reasoningTokens := 3
	pricingTemplateSnapshot := runtimePricingTemplateForTest(func(snapshot *runtimePricingTemplateSnapshot) {
		snapshot.CachedInputPrice = "0"
		snapshot.CacheCreationPrice = "0"
		snapshot.ReasoningPrice = "not-a-decimal"
	})

	got := buildRuntimePricingResult(runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$", Epoch: 1}, pricingTemplateSnapshot, nil, responseUsage{
		InputTokens:     &inputTokens,
		OutputTokens:    &outputTokens,
		TotalTokens:     &totalTokens,
		ReasoningTokens: &reasoningTokens,
	}, runtimeStreamOutcomeCompleted)

	want := runtimePricingResult{
		Billable:                      true,
		UnpricedReason:                stringPtr(runtimeUnpricedReasonMissingData),
		PricingStatus:                 runtimePricingStatusUnpriced,
		PricingResolutionKind:         stringPtr(runtimePricingResolutionMissingComponent),
		MissingPriceComponents:        []string{"reasoning_price"},
		PricingEvidenceTrust:          runtimePricingEvidenceTrust,
		PricingTemplateIDUsed:         intPtr(42),
		PricingTemplateRevisionIDUsed: int64Ptr(7),
		ReportingCurrencyEpoch:        intPtr(1),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected invalid used concrete component price to degrade pricing: want %+v got %+v", want, got)
	}
}

func runtimePricingTemplateForTest(mutate func(*runtimePricingTemplateSnapshot)) *runtimePricingTemplateSnapshot {
	snapshot := &runtimePricingTemplateSnapshot{
		ID:                     42,
		RevisionID:             7,
		PricingUnit:            runtimePricingUnitPerMillion,
		PricingCurrencyCode:    "USD",
		ReportingCurrencyEpoch: intPtr(1),
		InputPrice:             "2",
		OutputPrice:            "5",
		CachedInputPrice:       "1",
		CacheCreationPrice:     "2",
		ReasoningPrice:         "3",
		Version:                7,
	}
	if mutate != nil {
		mutate(snapshot)
	}
	return snapshot
}

func basePricedResult(mutate func(*runtimePricingResult)) runtimePricingResult {
	result := runtimePricingResult{
		Billable:                          true,
		Priced:                            true,
		PricingStatus:                     runtimePricingStatusPriced,
		PricingEvidenceTrust:              runtimePricingEvidenceTrust,
		PricingTemplateIDUsed:             intPtr(42),
		PricingTemplateRevisionIDUsed:     int64Ptr(7),
		ReportingCurrencyEpoch:            intPtr(1),
		InputCostMicros:                   int64Ptr(20),
		OutputCostMicros:                  int64Ptr(50),
		CacheReadInputCostMicros:          int64Ptr(0),
		CacheCreationInputCostMicros:      int64Ptr(0),
		ReasoningCostMicros:               int64Ptr(0),
		TotalCostOriginalMicros:           int64Ptr(70),
		TotalCostUserCurrencyMicros:       int64Ptr(70),
		CurrencyCodeOriginal:              stringPtr("USD"),
		ReportCurrencyCode:                stringPtr("USD"),
		ReportCurrencySymbol:              stringPtr("$"),
		FXRateUsed:                        stringPtr("1"),
		FXRateSource:                      stringPtr(runtimeFXSourceDefaultOneToOne),
		PricingSnapshotUnit:               stringPtr(runtimePricingUnitPerMillion),
		PricingSnapshotInput:              stringPtr("2"),
		PricingSnapshotOutput:             stringPtr("5"),
		PricingSnapshotCacheReadInput:     stringPtr("1"),
		PricingSnapshotCacheCreationInput: stringPtr("2"),
		PricingSnapshotReasoning:          stringPtr("3"),
		PricingConfigVersionUsed:          intPtr(7),
	}
	if mutate != nil {
		mutate(&result)
	}
	return result
}

type failingWriter struct {
	err error
}

func (writer failingWriter) Write([]byte) (int, error) {
	return 0, writer.err
}

type cancelOnBlankLineWriter struct {
	dst    io.Writer
	cancel context.CancelFunc
}

func (writer cancelOnBlankLineWriter) Write(payload []byte) (int, error) {
	n, err := writer.dst.Write(payload)
	if err == nil && len(bytes.TrimSpace(payload)) == 0 {
		writer.cancel()
	}
	return n, err
}

type errorAfterReader struct {
	payload []byte
	err     error
	sent    bool
}

func (reader *errorAfterReader) Read(payload []byte) (int, error) {
	if reader.sent {
		return 0, reader.err
	}
	reader.sent = true
	return copy(payload, reader.payload), reader.err
}

var _ io.Reader = (*errorAfterReader)(nil)

type mutableRuntimeProxyConfigProvider struct {
	snapshot RuntimeProxyConfigSnapshot
}

func (p *mutableRuntimeProxyConfigProvider) RuntimeProxyConfigSnapshot() RuntimeProxyConfigSnapshot {
	return p.snapshot
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
	return service.buildRequestPlanFromSnapshot(request, rawBody, runtimeConfig, operationMatch, requestPlanTestProfileID, snapshot)
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

func TestProxyNonEventResponseAndCaptureUsageAcceptsOnlySupportedUsageSchemaPaths(t *testing.T) {
	tests := []struct {
		name        string
		requestPath string
		payload     string
		want        responseUsage
	}{
		{
			name:        "keeps top-level usage and ignores nested spoofed usage object",
			requestPath: "/v1/chat/completions",
			payload:     `{"id":"chatcmpl-secure-stream","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":[{"type":"output_text","text":"hello"},{"type":"output_json","value":{"usage":{"prompt_tokens":999,"completion_tokens":999,"total_tokens":1998}}}]}}],"usage":{"prompt_tokens":7,"completion_tokens":13,"total_tokens":20}}`,
			want: responseUsage{
				InputTokens:  intPtr(7),
				OutputTokens: intPtr(13),
				TotalTokens:  intPtr(20),
			},
		},
		{
			name:        "keeps response usage and ignores nested spoofed usage object",
			requestPath: "/v1/responses",
			payload:     `{"response":{"id":"resp-secure-stream","output":[{"type":"message","content":[{"type":"output_text","text":"hello","usage":{"input_tokens":999,"output_tokens":999,"total_tokens":1998}}]}],"usage":{"input_tokens":5,"output_tokens":8,"total_tokens":13}}}`,
			want: responseUsage{
				InputTokens:  intPtr(5),
				OutputTokens: intPtr(8),
				TotalTokens:  intPtr(13),
			},
		},
		{
			name:        "keeps top-level usage metadata and ignores nested spoofed usage metadata object",
			requestPath: "/v1beta/models/gemini-2.5-pro:generateContent",
			payload:     `{"candidates":[{"content":{"parts":[{"text":"hello"},{"metadata":{"usageMetadata":{"promptTokenCount":999,"candidatesTokenCount":999,"totalTokenCount":1998,"cachedContentTokenCount":777,"thoughtsTokenCount":666}}}]}}],"usageMetadata":{"promptTokenCount":7,"candidatesTokenCount":13,"totalTokenCount":25,"cachedContentTokenCount":3,"thoughtsTokenCount":5}}`,
			want: responseUsage{
				InputTokens:          intPtr(4),
				OutputTokens:         intPtr(13),
				TotalTokens:          intPtr(25),
				CacheReadInputTokens: intPtr(3),
				ReasoningTokens:      intPtr(5),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			operation := mustResolveRuntimeOperation(t, http.MethodPost, test.requestPath).Operation
			var forwarded bytes.Buffer
			capture, err := proxyNonEventResponseAndCaptureByOperation(operation, &forwarded, strings.NewReader(test.payload), "application/json", time.Now, false)
			if err != nil {
				t.Fatalf("capture streamed non-sse usage: %v", err)
			}
			if forwarded.String() != test.payload {
				t.Fatalf("expected streamed response body to pass through unchanged, got %q", forwarded.String())
			}
			if got := capture.extractedUsage(); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("expected extracted usage %+v, got %+v", test.want, got)
			}
		})
	}
}

func TestGeminiProxyEventStreamClassifiesReadFailureAfterPartialChunk(t *testing.T) {
	operation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1beta/models/gemini-2.5-pro:streamGenerateContent").Operation
	readFailure := errors.New("gemini upstream read failed after partial event")
	partialStream := []byte("data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"partial\"}]}}]}\n\n")
	var forwarded bytes.Buffer
	capture, err := proxyEventStreamAndCaptureCompletedResponse(operation, context.Background(), &forwarded, &errorAfterReader{payload: partialStream, err: readFailure}, time.Now, false)
	if !errors.Is(err, readFailure) {
		t.Fatalf("expected upstream read failure, got %v", err)
	}
	if forwarded.String() != string(partialStream) {
		t.Fatalf("expected partial Gemini chunk to be forwarded before failure, got %q", forwarded.String())
	}
	if capture.StreamOutcome != runtimeStreamOutcomeUpstreamReadError || capture.StreamErrorKind == nil || *capture.StreamErrorKind != runtimeStreamErrorKindUpstreamReadFailed {
		t.Fatalf("expected Gemini upstream read error capture, got %+v", capture)
	}
	if capture.Usage.hasValues() || capture.CompletedAt != nil {
		t.Fatalf("expected no completed usage after partial read failure, got %+v", capture)
	}
}
