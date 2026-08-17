package runtime

import (
	"io"
	"net/http"
	"strings"

	"github.com/coachpo/prism/backend/internal/platform/bodylimits"
)

const (
	runtimeOperationNotFoundDetail          = "Runtime operation not found"
	runtimeOperationMethodNotAllowedDetail  = "Method not allowed for runtime operation"
	runtimeContentEncodingUnsupportedDetail = "Content-Encoding is not supported when custom request parameters are configured"
)

func resolveRuntimeOperationAtIngress(method string, requestPath string) (*RuntimeOperationMatch, []string) {
	if match, ok := ResolveRuntimeOperation(method, requestPath); ok {
		return &match, nil
	}
	allowedMethods := make([]string, 0, 1)
	seenMethods := map[string]struct{}{}
	for _, operation := range runtimeOperationCatalog {
		if _, ok := operation.PathMatcher.Match(requestPath); !ok {
			continue
		}
		if _, seen := seenMethods[operation.Method]; seen {
			continue
		}
		seenMethods[operation.Method] = struct{}{}
		allowedMethods = append(allowedMethods, operation.Method)
	}
	return nil, allowedMethods
}

func (s *Service) handleStreamingProxy(w http.ResponseWriter, r *http.Request) {
	if r.Body != nil {
		defer func() { _ = r.Body.Close() }()
	}
	operationMatch, allowedMethods := resolveRuntimeOperationAtIngress(r.Method, r.URL.Path)
	if len(allowedMethods) > 0 {
		w.Header().Set("Allow", strings.Join(allowedMethods, ", "))
		writeError(w, http.StatusMethodNotAllowed, "", runtimeOperationMethodNotAllowedDetail, nil)
		return
	}
	if operationMatch == nil {
		writeError(w, http.StatusNotFound, "", runtimeOperationNotFoundDetail, nil)
		return
	}

	if operationMatch.Operation.Name == runtimeOperationOpenAIModels {
		s.handleOpenAIModelsList(w, r)
		return
	}

	// Canonical accepted-operation identity: a lowercase UUIDv4 generated at
	// the runtime-operation boundary before planning. It is the grouping key
	// for all request/usage/audit rows and outbox items. Caller-supplied
	// X-Request-ID never becomes the grouping key; it is captured separately
	// as a scrubbed, bounded caller_request_id value.
	planningStartedAt := s.nowUTC()
	ingress := newRuntimeIngressContext(planningStartedAt)
	if callerRequestID := strings.TrimSpace(r.Header.Get("X-Request-ID")); callerRequestID != "" {
		ingress.callerRequestID = callerRequestID
	}
	r = r.WithContext(withRuntimeIngressContext(r.Context(), ingress))

	requestBodyLimit := runtimeRequestBodyLimitBytes(operationMatch.Operation, r.Header.Get("Content-Type"))
	if !limitRuntimeRequestBody(w, r, requestBodyLimit) {
		return
	}

	runtimeConfig := s.runtimeProxyConfigSnapshot()
	if canBuildStreamingRequestPlan(operationMatch.Operation) {
		plan, err := s.buildProxyProbeRequestPlan(r, runtimeConfig, *operationMatch)
		if err != nil {
			s.recordRuntimePlanningFailure(r, planningStartedAt, err)
			writeDomainError(w, err)
			return
		}
		if plan.requiresCustomRequestParametersOverlay() && !requestContentEncodingIsSupported(r) {
			// Gemini path-bound operations resolve candidates before the body
			// is read; a non-identity Content-Encoding cannot be re-encoded
			// after overlay, so reject before buffering.
			writeError(w, http.StatusUnsupportedMediaType, "", runtimeContentEncodingUnsupportedDetail, nil)
			return
		}
		if canStreamIncomingRequestBody(plan, operationMatch.Operation) {
			observer, ok := newRequestGenerationParamsStreamingObserver(operationMatch.Operation)
			if ok {
				plan.RequestGenerationSnapshot = observer.Snapshot
				s.handlePlannedProxy(w, r, plan, newStreamingRuntimeRequestBodySource(r.Body, r.ContentLength).withGenerationParamsObserver(observer))
				return
			}
		}
	}
	rawBody, err := readBufferedRequestBody(r.Body)
	if err != nil {
		if bodylimits.WriteMaxBytesError(w, err, requestBodyLimit) {
			return
		}
		writeError(w, http.StatusBadRequest, "", "Invalid request body", nil)
		return
	}
	plan, err := s.buildProxyRequestPlan(r, rawBody, runtimeConfig, *operationMatch)
	if err != nil {
		s.recordRuntimePlanningFailure(r, planningStartedAt, err)
		writeDomainError(w, err)
		return
	}
	s.handlePlannedProxy(w, r, plan, newBufferedRuntimeRequestBodySource(plan.UpstreamBody))
}

func readBufferedRequestBody(body io.Reader) ([]byte, error) {
	rawBody, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}
	if len(rawBody) == 0 {
		return nil, nil
	}
	return rawBody, nil
}

func runtimeRequestBodyLimitBytes(RuntimeOperation, string) int64 {
	return bodylimits.RuntimeJSONRequestBodyLimitBytes
}

func limitRuntimeRequestBody(w http.ResponseWriter, r *http.Request, limitBytes int64) bool {
	if r == nil || limitBytes <= 0 {
		return true
	}
	if r.ContentLength > limitBytes {
		bodylimits.WriteRequestBodyTooLarge(w, limitBytes)
		return false
	}
	bodylimits.LimitRequestBody(w, r, limitBytes)
	return true
}

func (s *Service) buildProxyRequestPlan(r *http.Request, rawBody []byte, runtimeConfig RuntimeProxyConfigSnapshot, operationMatch RuntimeOperationMatch) (requestPlan, error) {
	return s.buildRequestPlan(r.Context(), r, rawBody, runtimeConfig, operationMatch)
}

// buildProxyProbeRequestPlan builds the rawBody == nil Gemini path-bound
// probe plan that only resolves operation, profile, path-bound model, routing
// candidates, and Connection metadata. It never requires the base body to be
// an object and never performs custom-request-parameter overlay.
func (s *Service) buildProxyProbeRequestPlan(r *http.Request, runtimeConfig RuntimeProxyConfigSnapshot, operationMatch RuntimeOperationMatch) (requestPlan, error) {
	if s.cache == nil {
		return requestPlan{}, runtimeSnapshotDomainError(ErrPublishedRuntimeSnapshotUnavailable)
	}
	defaultProfile, snapshot, err := s.cache.LoadFreshDefaultRuntimePlan(r.Context())
	if err != nil {
		return requestPlan{}, runtimeSnapshotDomainError(err)
	}
	return s.buildProbeRequestPlanFromSnapshot(r.WithContext(r.Context()), runtimeConfig, operationMatch, defaultProfile.ID, snapshot)
}

func requestContentEncodingIsSupported(request *http.Request) bool {
	encoding := strings.TrimSpace(request.Header.Get("Content-Encoding"))
	return encoding == "" || strings.EqualFold(encoding, "identity")
}

func canBuildStreamingRequestPlan(operation RuntimeOperation) bool {
	return operation.ModelBindingSource == RuntimeOperationModelBindingPath
}

func canStreamIncomingRequestBody(plan requestPlan, operation RuntimeOperation) bool {
	if operation.ModelBindingSource != RuntimeOperationModelBindingPath || !operation.Streaming {
		return false
	}
	if !strings.EqualFold(plan.APIFamily, operation.APIFamily) {
		return false
	}
	hooks, ok := requestHooksForOperation(operation)
	if !ok || hooks.NewGenerationParamsStreamingObserver == nil {
		return false
	}
	return !plan.requiresReplayableRequestBody()
}

// Streaming-first keeps downstream response passthrough as the default while
// buffering request bodies only for the cases that still need replayable or
// rewritable bytes: body-based model extraction, model rewrite safety, or any
// multi-connection plan that may fail over or hedge.
func (s *Service) handlePlannedProxy(w http.ResponseWriter, r *http.Request, plan requestPlan, bodySource *runtimeRequestBodySource) {
	startedAt := s.nowUTC()
	execution, err := s.executeRequest(r.Context(), r.Method, plan, r.URL.RawQuery, bodySource)
	if err != nil {
		s.recordRuntimeExecutionFailure(plan, execution, r, startedAt, err)
		writeDomainError(w, err)
		return
	}
	defer func() { _ = execution.Response.Body.Close() }()
	s.writeProxyResponse(w, r, plan, execution, startedAt)
}
