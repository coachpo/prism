package core

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"sort"
	"strings"
	"time"
)

type HookPhase string

const (
	HookPhaseOnIngress          HookPhase = "on_ingress"
	HookPhaseOnAuth             HookPhase = "on_auth"
	HookPhaseOnPreNormalize     HookPhase = "on_pre_normalize"
	HookPhaseOnPostNormalize    HookPhase = "on_post_normalize"
	HookPhaseOnModelResolved    HookPhase = "on_model_resolved"
	HookPhaseOnPreTokenCount    HookPhase = "on_pre_token_count"
	HookPhaseOnContextOverflow  HookPhase = "on_context_overflow"
	HookPhaseOnRouteCandidates  HookPhase = "on_route_candidates"
	HookPhaseOnBeforeDispatch   HookPhase = "on_before_dispatch"
	HookPhaseOnResponseHeaders  HookPhase = "on_response_headers"
	HookPhaseOnStreamEvent      HookPhase = "on_stream_event"
	HookPhaseOnResponseComplete HookPhase = "on_response_complete"
	HookPhaseOnUsageExtracted   HookPhase = "on_usage_extracted"
	HookPhaseOnPriceCalculated  HookPhase = "on_price_calculated"
	HookPhaseOnError            HookPhase = "on_error"
	HookPhaseOnAudit            HookPhase = "on_audit"
)

var orderedHookPhases = []HookPhase{
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

func OrderedHookPhases() []HookPhase {
	return append([]HookPhase(nil), orderedHookPhases...)
}

func (phase HookPhase) Valid() bool {
	return hookPhaseIndex(phase) >= 0
}

func hookPhaseIndex(phase HookPhase) int {
	for index, candidate := range orderedHookPhases {
		if candidate == phase {
			return index
		}
	}
	return -1
}

type HookPermission string

const (
	HookPermissionReadHeaders      HookPermission = "read_headers"
	HookPermissionReadRequestBody  HookPermission = "read_request_body"
	HookPermissionReadResponseBody HookPermission = "read_response_body"
	HookPermissionPatchRequest     HookPermission = "patch_request"
	HookPermissionPatchRoute       HookPermission = "patch_route"
	HookPermissionReject           HookPermission = "reject"
	HookPermissionEmitEvent        HookPermission = "emit_event"
)

type HookDefinition struct {
	Name        string           `json:"name"`
	Phase       HookPhase        `json:"phase"`
	Order       int              `json:"order"`
	Timeout     time.Duration    `json:"timeout"`
	Permissions []HookPermission `json:"permissions,omitempty"`
}

type Hook interface {
	Definition() HookDefinition
	Run(context.Context, HookPayload) (HookResult, error)
}

type HookFunc func(context.Context, HookPayload) (HookResult, error)

type HookHandler struct {
	Def HookDefinition
	Fn  HookFunc
}

func (hook HookHandler) Definition() HookDefinition {
	return hook.Def
}

func (hook HookHandler) Run(ctx context.Context, payload HookPayload) (HookResult, error) {
	if hook.Fn == nil {
		return ContinueHookResult(), nil
	}
	return hook.Fn(ctx, payload)
}

type HookAccess struct {
	HeadersAllowed      bool `json:"headers_allowed"`
	RequestBodyAllowed  bool `json:"request_body_allowed"`
	ResponseBodyAllowed bool `json:"response_body_allowed"`
}

type HookPayload struct {
	Context         RequestContext      `json:"context"`
	Phase           HookPhase           `json:"phase"`
	Operation       OperationDescriptor `json:"operation"`
	Method          string              `json:"method"`
	Path            string              `json:"path"`
	RawQuery        string              `json:"raw_query,omitempty"`
	PathParams      map[string]string   `json:"path_params,omitempty"`
	Headers         map[string][]string `json:"headers,omitempty"`
	Metadata        map[string]string   `json:"metadata,omitempty"`
	RequestBody     []byte              `json:"request_body,omitempty"`
	ResponseHeaders map[string][]string `json:"response_headers,omitempty"`
	ResponseBody    []byte              `json:"response_body,omitempty"`
	Route           *RoutePlan          `json:"route,omitempty"`
	Usage           UsageEnvelope       `json:"usage"`
	Error           *GatewayError       `json:"error,omitempty"`
	Access          HookAccess          `json:"access"`
}

type HookPayloadInput struct {
	Phase              HookPhase
	Permissions        []HookPermission
	Envelope           RequestEnvelope
	Route              *RoutePlan
	Response           *ClientResponse
	Usage              UsageEnvelope
	Error              *GatewayError
	AdditionalMetadata map[string]string
}

func NewHookPayload(input HookPayloadInput) HookPayload {
	permissions := hookPermissionSet(input.Permissions)
	payload := HookPayload{
		Context:    input.Envelope.Context,
		Phase:      input.Phase,
		Operation:  input.Envelope.Operation,
		Method:     input.Envelope.Method,
		Path:       input.Envelope.Path,
		RawQuery:   input.Envelope.RawQuery,
		PathParams: cloneStringMap(input.Envelope.PathParams),
		Metadata:   mergeStringMaps(input.Envelope.Metadata, input.AdditionalMetadata),
		Usage:      input.Usage,
		Error:      cloneGatewayError(input.Error),
		Access: HookAccess{
			HeadersAllowed:      permissions[HookPermissionReadHeaders],
			RequestBodyAllowed:  permissions[HookPermissionReadRequestBody],
			ResponseBodyAllowed: permissions[HookPermissionReadResponseBody],
		},
	}
	if permissions[HookPermissionReadHeaders] {
		payload.Headers = safeHookHeaders(input.Envelope.Headers)
	}
	if permissions[HookPermissionReadRequestBody] {
		payload.RequestBody = cloneBytes(input.Envelope.Body)
	}
	if input.Route != nil {
		route := cloneRoutePlan(*input.Route)
		payload.Route = &route
	}
	if input.Response != nil {
		if permissions[HookPermissionReadHeaders] {
			payload.ResponseHeaders = safeHookHeaders(input.Response.Headers)
		}
		if permissions[HookPermissionReadResponseBody] {
			payload.ResponseBody = cloneBytes(input.Response.Body)
		}
		if payload.Usage.Source == "" {
			payload.Usage = input.Response.Usage
		}
	}
	return payload
}

type HookAction string

const (
	HookActionContinue     HookAction = "continue"
	HookActionPatchRequest HookAction = "patch_request"
	HookActionPatchRoute   HookAction = "patch_route"
	HookActionReject       HookAction = "reject"
	HookActionEmitEvent    HookAction = "emit_event"
)

type HookRequestPatch struct {
	Headers  map[string][]string `json:"headers,omitempty"`
	Metadata map[string]string   `json:"metadata,omitempty"`
	Body     []byte              `json:"body,omitempty"`
}

type HookRoutePatch struct {
	EffectiveModelID string      `json:"effective_model_id,omitempty"`
	RouteReason      RouteReason `json:"route_reason,omitempty"`
	UpstreamID       string      `json:"upstream_id,omitempty"`
}

type HookEvent struct {
	Name     string            `json:"name"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type HookResult struct {
	Action       HookAction        `json:"action"`
	RequestPatch *HookRequestPatch `json:"request_patch,omitempty"`
	RoutePatch   *HookRoutePatch   `json:"route_patch,omitempty"`
	Reject       *GatewayError     `json:"reject,omitempty"`
	Event        *HookEvent        `json:"event,omitempty"`
}

func ContinueHookResult() HookResult {
	return HookResult{Action: HookActionContinue}
}

func PatchRequestHookResult(patch HookRequestPatch) HookResult {
	return HookResult{Action: HookActionPatchRequest, RequestPatch: &patch}
}

func PatchRouteHookResult(patch HookRoutePatch) HookResult {
	return HookResult{Action: HookActionPatchRoute, RoutePatch: &patch}
}

func RejectHookResult(err *GatewayError) HookResult {
	return HookResult{Action: HookActionReject, Reject: cloneGatewayError(err)}
}

func EmitEventHookResult(event HookEvent) HookResult {
	return HookResult{Action: HookActionEmitEvent, Event: &HookEvent{Name: strings.TrimSpace(event.Name), Metadata: cloneStringMap(event.Metadata)}}
}

type HookExecutionStatus string

const (
	HookExecutionStatusCompleted HookExecutionStatus = "completed"
	HookExecutionStatusFailed    HookExecutionStatus = "failed"
)

type HookExecutionRecord struct {
	Phase     HookPhase           `json:"phase"`
	Hook      string              `json:"hook"`
	Action    HookAction          `json:"action"`
	Status    HookExecutionStatus `json:"status"`
	ErrorCode string              `json:"error_code,omitempty"`
	Duration  time.Duration       `json:"duration"`
}

type HookPhaseExecution struct {
	Phase        HookPhase             `json:"phase"`
	Results      []HookResult          `json:"results,omitempty"`
	Events       []HookEvent           `json:"events,omitempty"`
	Records      []HookExecutionRecord `json:"records,omitempty"`
	RequestPatch *HookRequestPatch     `json:"request_patch,omitempty"`
	RoutePatch   *HookRoutePatch       `json:"route_patch,omitempty"`
	Reject       *GatewayError         `json:"reject,omitempty"`
}

type HookExecutorOptions struct {
	DefaultTimeout time.Duration
}

type HookExecutor struct {
	hooks          []Hook
	defaultTimeout time.Duration
}

func NewHookExecutor(hooks []Hook, options HookExecutorOptions) (*HookExecutor, error) {
	defaultTimeout := options.DefaultTimeout
	if defaultTimeout <= 0 {
		defaultTimeout = time.Second
	}
	validated := make([]Hook, 0, len(hooks))
	for _, hook := range hooks {
		if hook == nil {
			continue
		}
		if err := validateHookDefinition(hook.Definition()); err != nil {
			return nil, err
		}
		validated = append(validated, hook)
	}
	sort.SliceStable(validated, func(i, j int) bool {
		left := validated[i].Definition()
		right := validated[j].Definition()
		if hookPhaseIndex(left.Phase) != hookPhaseIndex(right.Phase) {
			return hookPhaseIndex(left.Phase) < hookPhaseIndex(right.Phase)
		}
		if left.Order != right.Order {
			return left.Order < right.Order
		}
		return strings.TrimSpace(left.Name) < strings.TrimSpace(right.Name)
	})
	return &HookExecutor{hooks: validated, defaultTimeout: defaultTimeout}, nil
}

func (executor *HookExecutor) ExecutePhase(ctx context.Context, phase HookPhase, input HookPayloadInput) (HookPhaseExecution, error) {
	if executor == nil {
		return HookPhaseExecution{Phase: phase}, nil
	}
	if !phase.Valid() {
		return HookPhaseExecution{}, NewGatewayError(ErrorTypeValidation, "hook_phase_invalid", fmt.Sprintf("hook phase %q is unsupported", phase), http.StatusInternalServerError)
	}
	execution := HookPhaseExecution{Phase: phase}
	for _, hook := range executor.hooks {
		definition := hook.Definition()
		if definition.Phase != phase {
			continue
		}
		startedAt := time.Now()
		input.Phase = phase
		input.Permissions = definition.Permissions
		result, err := executor.runHook(ctx, hook, NewHookPayload(input))
		record := HookExecutionRecord{Phase: phase, Hook: strings.TrimSpace(definition.Name), Action: normalizedHookAction(result.Action), Status: HookExecutionStatusCompleted, Duration: time.Since(startedAt)}
		if err == nil {
			err = validateHookResult(definition, result)
		}
		if err != nil {
			gatewayErr := hookError(definition, phase, err)
			record.Status = HookExecutionStatusFailed
			record.ErrorCode = gatewayErr.Code
			execution.Records = append(execution.Records, record)
			return execution, gatewayErr
		}
		execution.Records = append(execution.Records, record)
		execution.Results = append(execution.Results, cloneHookResult(result))
		applyHookResult(&execution, result)
		if execution.Reject != nil {
			return execution, execution.Reject
		}
	}
	return execution, nil
}

func (executor *HookExecutor) Execute(ctx context.Context, input HookPayloadInput) ([]HookPhaseExecution, error) {
	if executor == nil {
		return nil, nil
	}
	var executions []HookPhaseExecution
	for _, phase := range OrderedHookPhases() {
		execution, err := executor.ExecutePhase(ctx, phase, input)
		executions = append(executions, execution)
		if err != nil {
			return executions, err
		}
	}
	return executions, nil
}

type hookRunResult struct {
	result HookResult
	err    error
}

func (executor *HookExecutor) runHook(ctx context.Context, hook Hook, payload HookPayload) (HookResult, error) {
	definition := hook.Definition()
	timeout := definition.Timeout
	if timeout <= 0 {
		timeout = executor.defaultTimeout
	}
	hookCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	results := make(chan hookRunResult, 1)
	go func() {
		result, err := hook.Run(hookCtx, payload)
		results <- hookRunResult{result: result, err: err}
	}()
	select {
	case result := <-results:
		return result.result, result.err
	case <-hookCtx.Done():
		if ctx != nil && ctx.Err() != nil {
			return HookResult{}, NewGatewayError(ErrorTypeInternal, "hook_cancelled", fmt.Sprintf("hook %q was cancelled in phase %q", strings.TrimSpace(definition.Name), definition.Phase), http.StatusInternalServerError)
		}
		return HookResult{}, NewGatewayError(ErrorTypeInternal, "hook_timeout", fmt.Sprintf("hook %q timed out in phase %q", strings.TrimSpace(definition.Name), definition.Phase), http.StatusGatewayTimeout)
	}
}

func validateHookDefinition(definition HookDefinition) error {
	if strings.TrimSpace(definition.Name) == "" {
		return NewGatewayError(ErrorTypeValidation, "hook_name_required", "hook name is required", http.StatusInternalServerError)
	}
	if !definition.Phase.Valid() {
		return NewGatewayError(ErrorTypeValidation, "hook_phase_invalid", fmt.Sprintf("hook %q uses unsupported phase %q", strings.TrimSpace(definition.Name), definition.Phase), http.StatusInternalServerError)
	}
	return nil
}

func validateHookResult(definition HookDefinition, result HookResult) error {
	action := normalizedHookAction(result.Action)
	permissions := hookPermissionSet(definition.Permissions)
	switch action {
	case HookActionContinue:
		return nil
	case HookActionPatchRequest:
		if !permissions[HookPermissionPatchRequest] {
			return hookPermissionError(definition, HookPermissionPatchRequest)
		}
		if !phaseAllowsPatchRequest(definition.Phase) {
			return hookPhaseActionError(definition, action)
		}
		return nil
	case HookActionPatchRoute:
		if !permissions[HookPermissionPatchRoute] {
			return hookPermissionError(definition, HookPermissionPatchRoute)
		}
		if !phaseAllowsPatchRoute(definition.Phase) {
			return hookPhaseActionError(definition, action)
		}
		return nil
	case HookActionReject:
		if !permissions[HookPermissionReject] {
			return hookPermissionError(definition, HookPermissionReject)
		}
		if !phaseAllowsReject(definition.Phase) {
			return hookPhaseActionError(definition, action)
		}
		if result.Reject == nil {
			return NewGatewayError(ErrorTypeValidation, "hook_reject_error_required", fmt.Sprintf("hook %q returned reject without an error", strings.TrimSpace(definition.Name)), http.StatusInternalServerError)
		}
		return nil
	case HookActionEmitEvent:
		if !permissions[HookPermissionEmitEvent] {
			return hookPermissionError(definition, HookPermissionEmitEvent)
		}
		if result.Event == nil || strings.TrimSpace(result.Event.Name) == "" {
			return NewGatewayError(ErrorTypeValidation, "hook_event_name_required", fmt.Sprintf("hook %q emitted an unnamed event", strings.TrimSpace(definition.Name)), http.StatusInternalServerError)
		}
		return nil
	default:
		return NewGatewayError(ErrorTypeValidation, "hook_result_action_invalid", fmt.Sprintf("hook %q returned unsupported action %q", strings.TrimSpace(definition.Name), result.Action), http.StatusInternalServerError)
	}
}

func applyHookResult(execution *HookPhaseExecution, result HookResult) {
	switch normalizedHookAction(result.Action) {
	case HookActionPatchRequest:
		execution.RequestPatch = mergeRequestPatches(execution.RequestPatch, result.RequestPatch)
	case HookActionPatchRoute:
		execution.RoutePatch = mergeRoutePatches(execution.RoutePatch, result.RoutePatch)
	case HookActionReject:
		execution.Reject = cloneGatewayError(result.Reject)
	case HookActionEmitEvent:
		if result.Event != nil {
			execution.Events = append(execution.Events, HookEvent{Name: strings.TrimSpace(result.Event.Name), Metadata: cloneStringMap(result.Event.Metadata)})
		}
	}
}

func hookError(definition HookDefinition, phase HookPhase, err error) *GatewayError {
	if gatewayErr, ok := err.(*GatewayError); ok {
		return cloneGatewayError(gatewayErr)
	}
	return NewGatewayError(ErrorTypeInternal, "hook_execution_failed", fmt.Sprintf("hook %q failed in phase %q", strings.TrimSpace(definition.Name), phase), http.StatusInternalServerError)
}

func hookPermissionError(definition HookDefinition, permission HookPermission) *GatewayError {
	return NewGatewayError(ErrorTypeValidation, "hook_permission_denied", fmt.Sprintf("hook %q in phase %q lacks permission %q", strings.TrimSpace(definition.Name), definition.Phase, permission), http.StatusInternalServerError)
}

func hookPhaseActionError(definition HookDefinition, action HookAction) *GatewayError {
	return NewGatewayError(ErrorTypeValidation, "hook_phase_action_forbidden", fmt.Sprintf("hook %q cannot return %q during phase %q", strings.TrimSpace(definition.Name), action, definition.Phase), http.StatusInternalServerError)
}

func normalizedHookAction(action HookAction) HookAction {
	if action == "" {
		return HookActionContinue
	}
	return action
}

func hookPermissionSet(permissions []HookPermission) map[HookPermission]bool {
	set := make(map[HookPermission]bool, len(permissions))
	for _, permission := range permissions {
		set[permission] = true
	}
	return set
}

func phaseAllowsPatchRequest(phase HookPhase) bool {
	return hookPhaseIndex(phase) >= hookPhaseIndex(HookPhaseOnIngress) && hookPhaseIndex(phase) <= hookPhaseIndex(HookPhaseOnBeforeDispatch)
}

func phaseAllowsPatchRoute(phase HookPhase) bool {
	switch phase {
	case HookPhaseOnModelResolved, HookPhaseOnContextOverflow, HookPhaseOnRouteCandidates, HookPhaseOnBeforeDispatch:
		return true
	default:
		return false
	}
}

func phaseAllowsReject(phase HookPhase) bool {
	return hookPhaseIndex(phase) >= hookPhaseIndex(HookPhaseOnIngress) && hookPhaseIndex(phase) <= hookPhaseIndex(HookPhaseOnBeforeDispatch)
}

func safeHookHeaders(headers map[string][]string) map[string][]string {
	if len(headers) == 0 {
		return nil
	}
	filtered := make(map[string][]string, len(headers))
	for name, values := range headers {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" || isSensitiveHookHeader(trimmed) {
			continue
		}
		filtered[trimmed] = append([]string(nil), values...)
	}
	if len(filtered) == 0 {
		return nil
	}
	return filtered
}

func isSensitiveHookHeader(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "authorization", "proxy-authorization", "x-api-key", "x-goog-api-key", "api-key", "apikey", "openai-api-key", "anthropic-api-key":
		return true
	default:
		return strings.Contains(strings.ToLower(name), "secret") || strings.Contains(strings.ToLower(name), "credential")
	}
}

func mergeStringMaps(left map[string]string, right map[string]string) map[string]string {
	if len(left) == 0 && len(right) == 0 {
		return nil
	}
	merged := cloneStringMap(left)
	if merged == nil {
		merged = map[string]string{}
	}
	maps.Copy(merged, right)
	return merged
}

func mergeRequestPatches(left *HookRequestPatch, right *HookRequestPatch) *HookRequestPatch {
	if left == nil && right == nil {
		return nil
	}
	merged := HookRequestPatch{}
	if left != nil {
		merged.Headers = cloneStringSliceMap(left.Headers)
		merged.Metadata = cloneStringMap(left.Metadata)
		merged.Body = cloneBytes(left.Body)
	}
	if right != nil {
		if len(right.Headers) > 0 {
			if merged.Headers == nil {
				merged.Headers = map[string][]string{}
			}
			for key, value := range right.Headers {
				if isSensitiveHookHeader(key) {
					continue
				}
				merged.Headers[key] = append([]string(nil), value...)
			}
		}
		merged.Metadata = mergeStringMaps(merged.Metadata, right.Metadata)
		if len(right.Body) > 0 {
			merged.Body = cloneBytes(right.Body)
		}
	}
	return &merged
}

func mergeRoutePatches(left *HookRoutePatch, right *HookRoutePatch) *HookRoutePatch {
	if left == nil && right == nil {
		return nil
	}
	merged := HookRoutePatch{}
	if left != nil {
		merged = *left
	}
	if right != nil {
		if strings.TrimSpace(right.EffectiveModelID) != "" {
			merged.EffectiveModelID = strings.TrimSpace(right.EffectiveModelID)
		}
		if right.RouteReason != "" {
			merged.RouteReason = right.RouteReason
		}
		if strings.TrimSpace(right.UpstreamID) != "" {
			merged.UpstreamID = strings.TrimSpace(right.UpstreamID)
		}
	}
	return &merged
}

func cloneHookResult(result HookResult) HookResult {
	clone := HookResult{Action: normalizedHookAction(result.Action)}
	if result.RequestPatch != nil {
		clone.RequestPatch = mergeRequestPatches(nil, result.RequestPatch)
	}
	if result.RoutePatch != nil {
		clone.RoutePatch = mergeRoutePatches(nil, result.RoutePatch)
	}
	clone.Reject = cloneGatewayError(result.Reject)
	if result.Event != nil {
		clone.Event = &HookEvent{Name: strings.TrimSpace(result.Event.Name), Metadata: cloneStringMap(result.Event.Metadata)}
	}
	return clone
}

func cloneRoutePlan(route RoutePlan) RoutePlan {
	route.CandidateAttempts = append([]RouteAttempt(nil), route.CandidateAttempts...)
	return route
}

func cloneGatewayError(err *GatewayError) *GatewayError {
	if err == nil {
		return nil
	}
	return NewGatewayError(err.Type, err.Code, err.Detail, err.StatusCode, err.Fields...)
}
