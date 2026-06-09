package core

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"testing"
	"time"
)

func TestHookPhasesExposeCanonicalOrder(t *testing.T) {
	got := OrderedHookPhases()
	want := []HookPhase{
		HookPhaseOnIngress,
		HookPhaseOnAuth,
		HookPhaseOnPreNormalize,
		HookPhaseOnPostNormalize,
		HookPhaseOnModelResolved,
		HookPhaseOnPreTokenCount,
		HookPhaseOnContextOverflow,
		HookPhaseOnRouteCandidates,
		HookPhaseOnBeforeDispatch,
		HookPhaseOnResponseHeaders,
		HookPhaseOnStreamEvent,
		HookPhaseOnResponseComplete,
		HookPhaseOnUsageExtracted,
		HookPhaseOnPriceCalculated,
		HookPhaseOnError,
		HookPhaseOnAudit,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected hook phase order: %+v", got)
	}
}

func TestHookExecutorDeterministicOrder(t *testing.T) {
	var observed []string
	executor := newTestHookExecutor(t, []Hook{
		testHook("beta", HookPhaseOnAuth, 20, nil, func(context.Context, HookPayload) (HookResult, error) {
			observed = append(observed, "beta")
			return ContinueHookResult(), nil
		}),
		testHook("ingress", HookPhaseOnIngress, 10, nil, func(context.Context, HookPayload) (HookResult, error) {
			observed = append(observed, "ingress")
			return ContinueHookResult(), nil
		}),
		testHook("alpha", HookPhaseOnAuth, 20, nil, func(context.Context, HookPayload) (HookResult, error) {
			observed = append(observed, "alpha")
			return ContinueHookResult(), nil
		}),
		testHook("first-auth", HookPhaseOnAuth, 1, nil, func(context.Context, HookPayload) (HookResult, error) {
			observed = append(observed, "first-auth")
			return ContinueHookResult(), nil
		}),
	})
	_, err := executor.Execute(context.Background(), HookPayloadInput{Envelope: testHookEnvelope()})
	if err != nil {
		t.Fatalf("execute hooks: %v", err)
	}
	want := []string{"ingress", "first-auth", "alpha", "beta"}
	if !reflect.DeepEqual(observed, want) {
		t.Fatalf("expected deterministic order %v, got %v", want, observed)
	}
}

func TestHookTimeoutMapsToGatewayError(t *testing.T) {
	executor := newTestHookExecutor(t, []Hook{
		testHookWithTimeout("slow", HookPhaseOnAuth, 0, 10*time.Millisecond, nil, func(context.Context, HookPayload) (HookResult, error) {
			time.Sleep(100 * time.Millisecond)
			return ContinueHookResult(), nil
		}),
	})
	_, err := executor.ExecutePhase(context.Background(), HookPhaseOnAuth, HookPayloadInput{Envelope: testHookEnvelope()})
	assertGatewayError(t, err, ErrorTypeInternal, "hook_timeout", http.StatusGatewayTimeout)
}

func TestHookFailureMapsToDeterministicGatewayError(t *testing.T) {
	executor := newTestHookExecutor(t, []Hook{
		testHook("failing", HookPhaseOnAuth, 0, nil, func(context.Context, HookPayload) (HookResult, error) {
			return HookResult{}, errors.New("provider credential super-secret should not leak")
		}),
	})
	_, err := executor.ExecutePhase(context.Background(), HookPhaseOnAuth, HookPayloadInput{Envelope: testHookEnvelope()})
	gatewayErr := assertGatewayError(t, err, ErrorTypeInternal, "hook_execution_failed", http.StatusInternalServerError)
	if gatewayErr.Detail != `hook "failing" failed in phase "on_auth"` {
		t.Fatalf("unexpected deterministic detail: %q", gatewayErr.Detail)
	}
}

func TestHookRejectMapsToTypedGatewayError(t *testing.T) {
	rejectErr := NewGatewayError(ErrorTypeValidation, "policy_reject", "request rejected by policy", http.StatusForbidden)
	executor := newTestHookExecutor(t, []Hook{
		testHook("reject", HookPhaseOnAuth, 0, []HookPermission{HookPermissionReject}, func(context.Context, HookPayload) (HookResult, error) {
			return RejectHookResult(rejectErr), nil
		}),
	})
	execution, err := executor.ExecutePhase(context.Background(), HookPhaseOnAuth, HookPayloadInput{Envelope: testHookEnvelope()})
	assertGatewayError(t, err, ErrorTypeValidation, "policy_reject", http.StatusForbidden)
	if execution.Reject == nil || execution.Reject.Code != "policy_reject" {
		t.Fatalf("expected reject result to be recorded, got %+v", execution)
	}
}

func TestHookPatchRequestPatchRouteAndEmitEvent(t *testing.T) {
	route := RoutePlan{OperationName: "openai.chat_completions", RequestedModelID: "public", EffectiveModelID: "old", RouteReason: RouteReasonDirectMatch}
	executor := newTestHookExecutor(t, []Hook{
		testHook("patch-request", HookPhaseOnBeforeDispatch, 0, []HookPermission{HookPermissionPatchRequest}, func(context.Context, HookPayload) (HookResult, error) {
			return PatchRequestHookResult(HookRequestPatch{Headers: map[string][]string{"X-Hook": {"enabled"}, "Authorization": {"forbidden"}}, Metadata: map[string]string{"hook": "request"}, Body: []byte(`{"model":"patched"}`)}), nil
		}),
		testHook("patch-route", HookPhaseOnBeforeDispatch, 1, []HookPermission{HookPermissionPatchRoute}, func(context.Context, HookPayload) (HookResult, error) {
			return PatchRouteHookResult(HookRoutePatch{EffectiveModelID: "target", RouteReason: RouteReasonModelRedirect, UpstreamID: "upstream-a"}), nil
		}),
		testHook("emit", HookPhaseOnBeforeDispatch, 2, []HookPermission{HookPermissionEmitEvent}, func(context.Context, HookPayload) (HookResult, error) {
			return EmitEventHookResult(HookEvent{Name: "hook.event", Metadata: map[string]string{"safe": "true"}}), nil
		}),
	})
	execution, err := executor.ExecutePhase(context.Background(), HookPhaseOnBeforeDispatch, HookPayloadInput{Envelope: testHookEnvelope(), Route: &route})
	if err != nil {
		t.Fatalf("execute phase: %v", err)
	}
	if got := execution.RequestPatch.Headers["X-Hook"]; !reflect.DeepEqual(got, []string{"enabled"}) {
		t.Fatalf("expected request patch header, got %+v", execution.RequestPatch.Headers)
	}
	if _, ok := execution.RequestPatch.Headers["Authorization"]; ok {
		t.Fatal("sensitive authorization header must not be retained in request patch")
	}
	if execution.RoutePatch.EffectiveModelID != "target" || execution.RoutePatch.RouteReason != RouteReasonModelRedirect || execution.RoutePatch.UpstreamID != "upstream-a" {
		t.Fatalf("unexpected route patch: %+v", execution.RoutePatch)
	}
	if len(execution.Events) != 1 || execution.Events[0].Name != "hook.event" {
		t.Fatalf("expected emitted event, got %+v", execution.Events)
	}
}

func TestHookPermissionDenial(t *testing.T) {
	executor := newTestHookExecutor(t, []Hook{
		testHook("no-permission", HookPhaseOnBeforeDispatch, 0, nil, func(context.Context, HookPayload) (HookResult, error) {
			return PatchRouteHookResult(HookRoutePatch{EffectiveModelID: "target"}), nil
		}),
	})
	_, err := executor.ExecutePhase(context.Background(), HookPhaseOnBeforeDispatch, HookPayloadInput{Envelope: testHookEnvelope()})
	assertGatewayError(t, err, ErrorTypeValidation, "hook_permission_denied", http.StatusInternalServerError)
}

func TestHookCommittedStreamPhaseRejectsMutation(t *testing.T) {
	executor := newTestHookExecutor(t, []Hook{
		testHook("late-mutation", HookPhaseOnStreamEvent, 0, []HookPermission{HookPermissionPatchRequest}, func(context.Context, HookPayload) (HookResult, error) {
			return PatchRequestHookResult(HookRequestPatch{Metadata: map[string]string{"late": "true"}}), nil
		}),
	})
	_, err := executor.ExecutePhase(context.Background(), HookPhaseOnStreamEvent, HookPayloadInput{Envelope: testHookEnvelope()})
	assertGatewayError(t, err, ErrorTypeValidation, "hook_phase_action_forbidden", http.StatusInternalServerError)
}

func TestHookRestrictedCredentialAndRawPayloadAccess(t *testing.T) {
	envelope := testHookEnvelope()
	envelope.Headers = map[string][]string{
		"Authorization":       {"Bearer provider-token"},
		"X-Api-Key":           {"provider-key"},
		"X-Request-Visible":   {"safe"},
		"X-Credential-Source": {"hidden"},
	}
	envelope.Body = []byte(`{"model":"gpt-4o","prompt":"raw prompt"}`)
	response := ClientResponse{Headers: map[string][]string{"Authorization": {"hidden"}, "X-Response-Visible": {"safe"}}, Body: []byte(`{"raw":"response"}`)}
	var defaultPayload HookPayload
	var allowedPayload HookPayload
	executor := newTestHookExecutor(t, []Hook{
		testHook("default", HookPhaseOnAudit, 0, nil, func(_ context.Context, payload HookPayload) (HookResult, error) {
			defaultPayload = payload
			return ContinueHookResult(), nil
		}),
		testHook("allowed", HookPhaseOnAudit, 1, []HookPermission{HookPermissionReadHeaders, HookPermissionReadRequestBody, HookPermissionReadResponseBody}, func(_ context.Context, payload HookPayload) (HookResult, error) {
			allowedPayload = payload
			return ContinueHookResult(), nil
		}),
	})
	_, err := executor.ExecutePhase(context.Background(), HookPhaseOnAudit, HookPayloadInput{Envelope: envelope, Response: &response})
	if err != nil {
		t.Fatalf("execute audit hooks: %v", err)
	}
	if len(defaultPayload.Headers) != 0 || len(defaultPayload.ResponseHeaders) != 0 || len(defaultPayload.RequestBody) != 0 || len(defaultPayload.ResponseBody) != 0 {
		t.Fatalf("default hook payload exposed restricted fields: %+v", defaultPayload)
	}
	if allowedPayload.Headers["Authorization"] != nil || allowedPayload.Headers["X-Api-Key"] != nil || allowedPayload.Headers["X-Credential-Source"] != nil {
		t.Fatalf("sensitive request headers leaked to hook: %+v", allowedPayload.Headers)
	}
	if got := allowedPayload.Headers["X-Request-Visible"]; !reflect.DeepEqual(got, []string{"safe"}) {
		t.Fatalf("expected safe request header, got %+v", allowedPayload.Headers)
	}
	if allowedPayload.ResponseHeaders["Authorization"] != nil || !reflect.DeepEqual(allowedPayload.ResponseHeaders["X-Response-Visible"], []string{"safe"}) {
		t.Fatalf("unexpected response headers: %+v", allowedPayload.ResponseHeaders)
	}
	if string(allowedPayload.RequestBody) != string(envelope.Body) || string(allowedPayload.ResponseBody) != string(response.Body) {
		t.Fatalf("explicit body permissions did not expose expected bodies")
	}
}

func testHook(name string, phase HookPhase, order int, permissions []HookPermission, fn HookFunc) Hook {
	return testHookWithTimeout(name, phase, order, 0, permissions, fn)
}

func testHookWithTimeout(name string, phase HookPhase, order int, timeout time.Duration, permissions []HookPermission, fn HookFunc) Hook {
	return HookHandler{Def: HookDefinition{Name: name, Phase: phase, Order: order, Timeout: timeout, Permissions: permissions}, Fn: fn}
}

func newTestHookExecutor(t *testing.T, hooks []Hook) *HookExecutor {
	t.Helper()
	executor, err := NewHookExecutor(hooks, HookExecutorOptions{DefaultTimeout: time.Second})
	if err != nil {
		t.Fatalf("new hook executor: %v", err)
	}
	return executor
}

func testHookEnvelope() RequestEnvelope {
	return NewRequestEnvelope(RequestEnvelopeInput{
		Context:   RequestContext{RequestID: "req-1"},
		Operation: OperationDescriptor{Name: "openai.chat_completions", Method: http.MethodPost, APIFamily: APIFamilyOpenAI, PathTemplate: "/v1/chat/completions", Shape: EndpointShapeTextGeneration, ModelBindingSource: ModelBindingSourceBody},
		Method:    http.MethodPost,
		Path:      "/v1/chat/completions",
		Headers:   map[string][]string{"X-Test": {"true"}},
	})
}

func assertGatewayError(t *testing.T, err error, errorType ErrorType, code string, statusCode int) *GatewayError {
	t.Helper()
	if err == nil {
		t.Fatalf("expected gateway error %s", code)
	}
	gatewayErr, ok := err.(*GatewayError)
	if !ok {
		t.Fatalf("expected GatewayError, got %T: %v", err, err)
	}
	if gatewayErr.Type != errorType || gatewayErr.Code != code || gatewayErr.StatusCode != statusCode {
		t.Fatalf("expected %s/%s/%d, got %+v", errorType, code, statusCode, gatewayErr)
	}
	return gatewayErr
}
