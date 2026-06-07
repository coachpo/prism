package runtime

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	loadbalancedomain "github.com/coachpo/prism/backend/internal/domain/loadbalance"
	"github.com/coachpo/prism/backend/internal/platform/background"
	"github.com/coachpo/prism/backend/internal/platform/config"
	"github.com/coachpo/prism/backend/internal/platform/logretention"
)

type DashboardUpdatePublisher interface {
	PublishDashboardUpdate(context.Context, int, int) (bool, error)
}

type AnalyticsUpdatePublisher interface {
	PublishAnalyticsUpdates(context.Context, int) (bool, error)
}

type Options struct {
	ExecutionPool              *pgxpool.Pool
	TelemetryPool              *pgxpool.Pool
	FeedbackPool               *pgxpool.Pool
	HTTPClient                 *http.Client
	RuntimeProxyConfigProvider RuntimeProxyConfigProvider
	Now                        func() time.Time
	DashboardUpdates           DashboardUpdatePublisher
	AnalyticsUpdates           AnalyticsUpdatePublisher
	Cache                      *SharedCache
	RuntimeState               *loadbalancedomain.LocalRuntimeStateStore
	LogPartitionEnsurer        LogPartitionEnsurer
	AssumeLogPartitionHorizon  bool
	TelemetryOutbox            TelemetryOutboxOptions
	FeedbackPipeline           RuntimeFeedbackPipelineOptions
	SideEffects                RuntimeSideEffectOptions
	Scheduler                  *background.Scheduler
}

type RuntimeProxyConfigSnapshot struct {
	HTTPClient *http.Client
}

type RuntimeProxyConfigProvider interface {
	RuntimeProxyConfigSnapshot() RuntimeProxyConfigSnapshot
}

type Service struct {
	executionPool                 *pgxpool.Pool
	telemetryPool                 *pgxpool.Pool
	feedbackPool                  *pgxpool.Pool
	feedbackStore                 *runtimeFeedbackStore
	httpClient                    *http.Client
	ownsHTTPClient                bool
	runtimeProxyConfigProvider    RuntimeProxyConfigProvider
	staticRuntimeProxyConfig      RuntimeProxyConfigSnapshot
	now                           func() time.Time
	secretEncryptionKey           string
	plannerMode                   config.RuntimeRoutingPlannerMode
	openAITerminalTranslationMode config.OpenAITerminalTranslationMode
	dashboardUpdates              DashboardUpdatePublisher
	analyticsUpdates              AnalyticsUpdatePublisher
	cache                         *SharedCache
	overflowAffinityCache         *overflowAffinityCache
	runtimeState                  *loadbalancedomain.LocalRuntimeStateStore
	runtimeMetrics                *runtimeMetrics
	telemetryOutbox               *runtimeTelemetryOutbox
	feedbackPipeline              *runtimeFeedbackPipeline
	runtimeSideEffects            *RuntimeSideEffectManager
	ownedScheduler                *background.Scheduler
}

type domainError struct {
	StatusCode               int
	ErrorCode                string
	Detail                   string
	Fields                   map[string]any
	ResolvedTargetModelID    *string
	ContextRouting           *runtimeContextRoutingDecision
	SelectedTerminalTargetID *int
	PlanningFailure          *runtimePlanningFailureTelemetry
}

func (err *domainError) Error() string {
	return err.Detail
}

func NewService(settings config.Settings, options Options) (*Service, error) {
	executionPool, telemetryPool, feedbackPool, err := resolveRuntimeServicePools(settings, options)
	if err != nil {
		return nil, err
	}
	client := options.HTTPClient
	ownsHTTPClient := false
	if client == nil {
		client = newRuntimeHTTPClient(settings)
		ownsHTTPClient = true
	}

	now := options.Now
	if now == nil {
		now = time.Now
	}
	runtimeState := options.RuntimeState
	if runtimeState == nil {
		runtimeState = loadbalancedomain.NewLocalRuntimeStateStore()
	}
	partitionEnsurer := options.LogPartitionEnsurer
	if partitionEnsurer == nil {
		partitionEnsurer = logretention.NewStore(logretention.Options{Pool: telemetryPool, Now: now})
	}
	logPartitions := newRuntimeLogPartitionCache(partitionEnsurer, now, options.AssumeLogPartitionHorizon)

	scheduler := options.Scheduler
	if scheduler == nil {
		scheduler = background.NewScheduler(background.Config{})
	}
	service := &Service{
		executionPool:                 executionPool,
		telemetryPool:                 telemetryPool,
		feedbackPool:                  feedbackPool,
		feedbackStore:                 newRuntimeFeedbackStore(feedbackPool),
		httpClient:                    client,
		ownsHTTPClient:                ownsHTTPClient,
		runtimeProxyConfigProvider:    options.RuntimeProxyConfigProvider,
		staticRuntimeProxyConfig:      RuntimeProxyConfigSnapshot{HTTPClient: client},
		now:                           now,
		secretEncryptionKey:           settings.SecretEncryptionKey,
		plannerMode:                   settings.RoutingPlannerMode(),
		openAITerminalTranslationMode: settings.ResolvedOpenAITerminalTranslationMode(),
		dashboardUpdates:              options.DashboardUpdates,
		analyticsUpdates:              options.AnalyticsUpdates,
		cache:                         options.Cache,
		overflowAffinityCache:         newOverflowAffinityCache(now),
		runtimeState:                  runtimeState,
		runtimeMetrics:                newRuntimeMetrics(),
	}
	telemetryOptions := options.TelemetryOutbox
	telemetryOptions.Scheduler = scheduler
	service.telemetryOutbox = newRuntimeTelemetryOutbox(telemetryPool, service.nowUTC, service.dashboardUpdates, service.analyticsUpdates, logPartitions, telemetryOptions)
	service.feedbackPipeline = newRuntimeFeedbackPipeline(service.feedbackStore, service.runtimeState, logPartitions, options.FeedbackPipeline)
	sideEffectOptions := options.SideEffects
	sideEffectOptions.Hooks = service.runtimeMetricSideEffectHooks(options.SideEffects.Hooks)
	service.runtimeSideEffects = NewRuntimeSideEffectManager(service.telemetryOutbox, sideEffectOptions)
	if options.Scheduler == nil {
		if err := service.RegisterBackgroundWorkers(scheduler); err != nil {
			return nil, err
		}
		if err := scheduler.Start(context.Background()); err != nil {
			return nil, err
		}
		service.ownedScheduler = scheduler
	}
	return service, nil
}

func (s *Service) runtimeMetricSideEffectHooks(existing *RuntimeSideEffectHooks) *RuntimeSideEffectHooks {
	return &RuntimeSideEffectHooks{
		AfterSubmit: func(result RuntimeSideEffectSubmitResult) {
			if existing != nil && existing.AfterSubmit != nil {
				existing.AfterSubmit(result)
			}
		},
		AfterCommit: func(intent RuntimeActivityIntent) {
			s.recordRuntimeOutboxEnqueue(intent.TraceContext.context(context.Background()), intent.Envelope.UsageEvent.OperationName, runtimeOutboxEnqueueAccepted)
			if existing != nil && existing.AfterCommit != nil {
				existing.AfterCommit(intent)
			}
		},
		TerminalFailure: func(intent RuntimeActivityIntent, err error) {
			s.recordRuntimeOutboxEnqueue(intent.TraceContext.context(context.Background()), intent.Envelope.UsageEvent.OperationName, runtimeOutboxEnqueueFailed)
			if existing != nil && existing.TerminalFailure != nil {
				existing.TerminalFailure(intent, err)
			}
		},
	}
}

func (s *Service) RegisterBackgroundWorkers(scheduler *background.Scheduler) error {
	if s == nil {
		return nil
	}
	if s.feedbackPipeline != nil {
		if err := s.feedbackPipeline.RegisterBackgroundWorker(scheduler); err != nil {
			return err
		}
	}
	if s.runtimeSideEffects != nil {
		if err := s.runtimeSideEffects.RegisterBackgroundWorker(scheduler); err != nil {
			return err
		}
	}
	if s.telemetryOutbox != nil {
		return s.telemetryOutbox.RegisterBackgroundWorker(scheduler)
	}
	return nil
}

func newRuntimeHTTPClient(settings config.Settings) *http.Client {
	transportConfig := settings.RuntimeTransport()
	return &http.Client{
		Timeout: transportConfig.RequestTimeout,
		Transport: &http.Transport{
			DisableCompression:    true,
			MaxIdleConns:          transportConfig.MaxIdleConns,
			MaxIdleConnsPerHost:   transportConfig.MaxIdleConnsPerHost,
			MaxConnsPerHost:       transportConfig.MaxConnsPerHost,
			IdleConnTimeout:       transportConfig.IdleConnTimeout,
			ResponseHeaderTimeout: transportConfig.ResponseHeaderTimeout,
			TLSHandshakeTimeout:   transportConfig.TLSHandshakeTimeout,
			ExpectContinueTimeout: transportConfig.ExpectContinueTimeout,
		},
	}
}

func (s *Service) runtimeProxyConfigSnapshot() RuntimeProxyConfigSnapshot {
	if s == nil {
		return RuntimeProxyConfigSnapshot{}
	}
	if s.runtimeProxyConfigProvider != nil {
		return s.runtimeProxyConfigProvider.RuntimeProxyConfigSnapshot()
	}
	return s.staticRuntimeProxyConfig
}

func (s *Service) DrainSideEffects() {
	if s == nil {
		return
	}
	if s.runtimeSideEffects != nil {
		result := s.runtimeSideEffects.Close()
		if result.TimedOut || result.ForcedAbandoned > 0 {
			slog.Warn("runtime side effects close timed out", "elapsed", result.Elapsed, "pending", result.Pending, "forced_abandoned", result.ForcedAbandoned)
		}
	}
	if s.telemetryOutbox != nil {
		result := s.telemetryOutbox.Close()
		if result.TimedOut {
			slog.Warn("runtime telemetry outbox close timed out", "elapsed", result.Elapsed, "pending_rows", result.PendingRows, "inflight", result.Inflight)
		}
	}
	if s.feedbackPipeline != nil {
		s.feedbackPipeline.Close()
	}
}

func (s *Service) Close() {
	if s == nil {
		return
	}
	s.DrainSideEffects()
	if s.ownedScheduler != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.ownedScheduler.Stop(ctx, time.Now().Add(5*time.Second))
	}
	if s.ownsHTTPClient && s.httpClient != nil {
		if closer, ok := s.httpClient.Transport.(interface{ CloseIdleConnections() }); ok {
			closer.CloseIdleConnections()
		}
	}
}

func (s *Service) Handler() http.Handler {
	return http.HandlerFunc(s.handleProxy)
}

func (s *Service) nowUTC() time.Time {
	return s.now().UTC()
}

func (s *Service) RuntimeState() *loadbalancedomain.LocalRuntimeStateStore {
	if s == nil {
		return nil
	}
	return s.runtimeState
}

func resolveRuntimeServicePools(settings config.Settings, options Options) (*pgxpool.Pool, *pgxpool.Pool, *pgxpool.Pool, error) {
	_ = settings
	executionPool := options.ExecutionPool
	telemetryPool := options.TelemetryPool
	feedbackPool := options.FeedbackPool
	if executionPool == nil {
		return nil, nil, nil, fmt.Errorf("runtime execution pool is required")
	}
	if telemetryPool == nil {
		return nil, nil, nil, fmt.Errorf("runtime telemetry pool is required")
	}
	if feedbackPool == nil {
		return nil, nil, nil, fmt.Errorf("runtime feedback pool is required")
	}
	if executionPool == telemetryPool {
		return nil, nil, nil, fmt.Errorf("runtime execution and telemetry pools must be distinct")
	}
	if executionPool == feedbackPool {
		return nil, nil, nil, fmt.Errorf("runtime execution and feedback pools must be distinct")
	}
	if telemetryPool == feedbackPool {
		return nil, nil, nil, fmt.Errorf("runtime telemetry and feedback pools must be distinct")
	}
	return executionPool, telemetryPool, feedbackPool, nil
}

func (s *Service) handleProxy(w http.ResponseWriter, r *http.Request) {
	s.handleStreamingProxy(w, r)
}

const (
	runtimeOperationNotFoundDetail         = "Runtime operation not found"
	runtimeOperationMethodNotAllowedDetail = "Method not allowed for runtime operation"
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
	ctx, requestSpan := startRuntimeSpan(r.Context(), "runtime.request")
	r = r.WithContext(ctx)
	defer requestSpan.End()
	if r.Body != nil {
		defer func() { _ = r.Body.Close() }()
	}
	resolveCtx, resolveSpan := startRuntimeSpan(r.Context(), "runtime.operation.resolve")
	r = r.WithContext(resolveCtx)
	operationMatch, allowedMethods := resolveRuntimeOperationAtIngress(r.Method, r.URL.Path)
	if len(allowedMethods) > 0 {
		runtimeTraceMarkError(resolveSpan, "method_not_allowed")
		resolveSpan.End()
		runtimeTraceMarkError(requestSpan, "method_not_allowed")
		w.Header().Set("Allow", strings.Join(allowedMethods, ", "))
		writeError(w, http.StatusMethodNotAllowed, "", runtimeOperationMethodNotAllowedDetail, nil)
		return
	}
	if operationMatch == nil {
		runtimeTraceMarkError(resolveSpan, "operation_not_found")
		resolveSpan.End()
		runtimeTraceMarkError(requestSpan, "operation_not_found")
		writeError(w, http.StatusNotFound, "", runtimeOperationNotFoundDetail, nil)
		return
	}
	resolveSpan.SetAttributes(runtimeTraceOperationAttributes(operationMatch.Operation)...)
	requestSpan.SetAttributes(runtimeTraceOperationAttributes(operationMatch.Operation)...)
	resolveSpan.End()

	runtimeConfig := s.runtimeProxyConfigSnapshot()
	planningStartedAt := s.nowUTC()
	if canBuildStreamingRequestPlan(operationMatch.Operation) {
		plan, err := s.buildProxyRequestPlan(r, nil, runtimeConfig, *operationMatch)
		if err != nil {
			s.recordRuntimePlanningFailure(r, planningStartedAt, err)
			runtimeTraceMarkError(requestSpan, "request_plan_failed")
			writeDomainError(w, err)
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
		runtimeTraceMarkError(requestSpan, "request_plan_failed")
		writeError(w, http.StatusBadRequest, "", "Invalid request body", nil)
		return
	}
	plan, err := s.buildProxyRequestPlan(r, rawBody, runtimeConfig, *operationMatch)
	if err != nil {
		s.recordRuntimePlanningFailure(r, planningStartedAt, err)
		runtimeTraceMarkError(requestSpan, "request_plan_failed")
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

func (s *Service) buildProxyRequestPlan(r *http.Request, rawBody []byte, runtimeConfig RuntimeProxyConfigSnapshot, operationMatch RuntimeOperationMatch) (requestPlan, error) {
	return s.buildRequestPlan(r.Context(), r, rawBody, runtimeConfig, operationMatch)
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
	executionPlan := plan
	executionBodySource := bodySource
	if preselectedPlan, ok := s.preselectOverflowAffinityPromotionPlan(r, plan); ok {
		executionPlan = preselectedPlan
		executionBodySource = newBufferedRuntimeRequestBodySource(plan.RawRequestBody)
	}
	execution, err := s.executeRequest(r.Context(), r.Method, executionPlan, r.URL.RawQuery, executionBodySource)
	if err != nil {
		runtimeTraceMarkContextError(r.Context(), "runtime_execute_failed")
		writeDomainError(w, err)
		return
	}
	defer func() { _ = execution.Response.Body.Close() }()
	s.writeProxyResponse(w, r, executionPlan, execution, startedAt)
}

func (s *Service) writeProxyResponse(w http.ResponseWriter, r *http.Request, plan requestPlan, execution executionResult, startedAt time.Time) {
	ctx, responseSpan := startRuntimeSpan(r.Context(), "runtime.response.handle", runtimeTracePlanAttributes(plan)...)
	r = r.WithContext(ctx)
	defer responseSpan.End()
	proxyWriter := newRuntimeDeferredCommitWriter(w)
	runtimeTraceSetStatusCode(responseSpan, execution.Response.StatusCode)

	var responseCapture runtimeResponseCapture
	contentType := strings.ToLower(strings.TrimSpace(execution.Response.Header.Get("Content-Type")))
	captureAuditBody := execution.AuditEnabledAtRequest && execution.AuditCaptureBodiesAtRequest
	translationMode := responseTranslationModeForExecution(plan, execution)
	if strings.Contains(contentType, "text/event-stream") {
		if _, ok := streamHooksForProxyResponse(plan.RuntimeOperation, plan.IsStreamingRequest); ok {
			if translationMode == "" || translationMode == TranslationModeNone {
				copyResponseHeaders(proxyWriter.Header(), execution.Response.Header)
				proxyWriter.WriteHeader(execution.Response.StatusCode)
				responseCapture, streamErr := s.codingAgentFormatBridge().ProxyEventStreamAndCaptureCompletedResponse(plan.RuntimeOperation, TranslationModeNone, plan.RequestedModelID, r.Context(), proxyWriter, execution.Response.Body, s.nowUTC, captureAuditBody)
				if streamErr != nil {
					runtimeTraceMarkError(responseSpan, "response_handle_failed")
					slog.Debug("runtime stream proxy ended with classified error", "error", streamErr, "stream_outcome", responseCapture.StreamOutcome)
				}
				runtimeTraceSetStreamOutcome(responseSpan, responseCapture.StreamOutcome)
				proxyWriter.Commit()
				s.recordRuntimeActivity(plan, execution, r, startedAt, responseCapture)
				return
			}

			var translatedStream bytes.Buffer
			responseCapture, streamErr := s.codingAgentFormatBridge().ProxyEventStreamAndCaptureCompletedResponse(plan.RuntimeOperation, translationMode, plan.RequestedModelID, r.Context(), &translatedStream, execution.Response.Body, s.nowUTC, captureAuditBody)
			if streamErr != nil {
				runtimeTraceMarkError(responseSpan, "response_handle_failed")
				slog.Debug("runtime translated stream proxy failed", "error", streamErr, "stream_outcome", responseCapture.StreamOutcome)
				writeTranslatedOpenAIError(w, streamErr, "Failed to translate upstream stream")
				return
			}
			copyTranslatedResponseHeadersWithContentType(proxyWriter.Header(), execution.Response.Header, "text/event-stream")
			proxyWriter.WriteHeader(execution.Response.StatusCode)
			if _, err := proxyWriter.Write(translatedStream.Bytes()); err != nil {
				runtimeTraceMarkError(responseSpan, "response_handle_failed")
				return
			}
			runtimeTraceSetStreamOutcome(responseSpan, responseCapture.StreamOutcome)
			proxyWriter.Commit()
			s.recordRuntimeActivity(plan, execution, r, startedAt, responseCapture)
			return
		}
	}
	if !nonStreamResponseRequiresBufferedInspection(execution.Response.StatusCode, translationMode) {
		copyResponseHeaders(proxyWriter.Header(), execution.Response.Header)
		proxyWriter.WriteHeader(execution.Response.StatusCode)
		passthroughCapture, passthroughErr := proxyNonEventResponseAndCaptureByOperation(plan.RuntimeOperation, translationMode, proxyWriter, execution.Response.Body, contentType, s.nowUTC, captureAuditBody)
		responseCapture = passthroughCapture
		if passthroughErr != nil {
			runtimeTraceMarkError(responseSpan, "response_handle_failed")
			if !proxyWriter.Committed() {
				writeError(w, http.StatusBadGateway, "", "Failed to read upstream response", nil)
				return
			}
		}
		runtimeTraceSetStreamOutcome(responseSpan, responseCapture.StreamOutcome)
		proxyWriter.Commit()
		s.recordRuntimeActivity(plan, execution, r, startedAt, responseCapture)
		return
	}
	sourceRawBody, err := readAndCloseRuntimeResponseBody(execution.Response)
	if err != nil {
		runtimeTraceMarkError(responseSpan, "response_handle_failed")
		writeError(w, http.StatusBadGateway, "", "Failed to read upstream response", nil)
		return
	}
	finalPlan := plan
	finalExecution := execution
	sourceClassification := classifyCLIProxyAPIOverflowResponse(execution.Response.StatusCode, sourceRawBody, translationMode)
	finalRawBody := sourceRawBody
	if promotedPlan, promotedExecution, promotedErr, promoted := s.tryContextOverflowPromotion(r, plan, execution, sourceClassification); promoted {
		if promotedErr != nil {
			var domainErr *domainError
			if errors.As(promotedErr, &domainErr) && domainErr != nil {
				runtimeTraceSetStatusCode(responseSpan, domainErr.StatusCode)
				writeDomainError(w, promotedErr)
				return
			}
			runtimeTraceMarkError(responseSpan, "response_handle_failed")
			writeError(w, http.StatusBadGateway, "", "Failed to replay promoted upstream response", nil)
			return
		}
		finalPlan = mergeContextOverflowPromotedPlan(plan, promotedPlan, execution, promotedExecution, sourceClassification)
		finalExecution = mergeContextOverflowPromotedExecution(execution, promotedExecution)
		finalRawBody, err = readAndCloseRuntimeResponseBody(promotedExecution.Response)
		if err != nil {
			runtimeTraceMarkError(responseSpan, "response_handle_failed")
			writeError(w, http.StatusBadGateway, "", "Failed to read promoted upstream response", nil)
			return
		}
		if affinityMetadata := s.populateOverflowAffinityCacheAfterPromotion(r, plan, execution, promotedPlan, promotedExecution, sourceClassification); affinityMetadata != nil {
			finalPlan.ContextRouting = attachRuntimeContextOverflowAffinityDecision(finalPlan.ContextRouting, affinityMetadata)
		}
		runtimeTraceSetStatusCode(responseSpan, finalExecution.Response.StatusCode)
	}
	responseCapture, err = s.writeBufferedNonStreamResponse(proxyWriter, finalPlan, finalExecution, finalRawBody)
	if err != nil {
		runtimeTraceMarkError(responseSpan, "response_handle_failed")
		var domainErr *domainError
		if errors.As(err, &domainErr) && domainErr != nil {
			writeTranslatedOpenAIError(w, err, "Failed to translate upstream response")
			return
		}
		if !proxyWriter.Committed() {
			writeError(w, http.StatusBadGateway, "", "Failed to read upstream response", nil)
		}
		return
	}
	runtimeTraceSetStreamOutcome(responseSpan, responseCapture.StreamOutcome)
	proxyWriter.Commit()
	s.recordRuntimeActivity(finalPlan, finalExecution, r, startedAt, responseCapture)
}

func responseTranslationModeForExecution(plan requestPlan, execution executionResult) TranslationMode {
	for _, attempt := range plan.orderedTerminalAttempts() {
		if attempt.Connection.ID == execution.Connection.ID {
			return attempt.TranslationMode
		}
	}
	return TranslationModeNone
}

func nonStreamResponseRequiresBufferedInspection(statusCode int, translationMode TranslationMode) bool {
	if translationMode != "" && translationMode != TranslationModeNone {
		return true
	}
	return cliProxyAPIOverflowStatusAllowed(statusCode)
}

func readAndCloseRuntimeResponseBody(response *http.Response) ([]byte, error) {
	if response == nil || response.Body == nil {
		return nil, fmt.Errorf("runtime response body unavailable")
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	return body, nil
}

func shouldPreserveRawTranslatedOverflowResponse(statusCode int, rawBody []byte, translationMode TranslationMode) bool {
	if translationMode == "" || translationMode == TranslationModeNone {
		return false
	}
	if classifyCLIProxyAPIOverflowResponse(statusCode, rawBody, translationMode).Promotable {
		return false
	}
	return classifyCLIProxyAPIOverflowResponse(statusCode, rawBody, TranslationModeNone).Promotable
}

func (s *Service) writeBufferedNonStreamResponse(proxyWriter *runtimeDeferredCommitWriter, plan requestPlan, execution executionResult, rawBody []byte) (runtimeResponseCapture, error) {
	contentType := strings.ToLower(strings.TrimSpace(execution.Response.Header.Get("Content-Type")))
	captureAuditBody := execution.AuditEnabledAtRequest && execution.AuditCaptureBodiesAtRequest
	translationMode := responseTranslationModeForExecution(plan, execution)
	if translationMode == "" || translationMode == TranslationModeNone || shouldPreserveRawTranslatedOverflowResponse(execution.Response.StatusCode, rawBody, translationMode) {
		copyResponseHeaders(proxyWriter.Header(), execution.Response.Header)
		proxyWriter.WriteHeader(execution.Response.StatusCode)
		return proxyNonEventResponseAndCaptureByOperation(plan.RuntimeOperation, TranslationModeNone, proxyWriter, bytes.NewReader(rawBody), contentType, s.nowUTC, captureAuditBody)
	}

	var translatedBody bytes.Buffer
	responseCapture, err := s.codingAgentFormatBridge().ProxyNonEventResponseAndCapture(translationMode, plan.RequestedModelID, &translatedBody, bytes.NewReader(rawBody), s.nowUTC, captureAuditBody)
	if err != nil {
		return runtimeResponseCapture{}, err
	}
	copyTranslatedResponseHeaders(proxyWriter.Header(), execution.Response.Header)
	proxyWriter.WriteHeader(execution.Response.StatusCode)
	if _, err := proxyWriter.Write(translatedBody.Bytes()); err != nil {
		return runtimeResponseCapture{}, err
	}
	return responseCapture, nil
}

func planAllowsContextOverflowPromotion(plan requestPlan) bool {
	if plan.IsStreamingRequest || len(plan.RawRequestBody) == 0 || plan.ContextOverflowPromotionPreselected {
		return false
	}
	switch strings.TrimSpace(plan.RuntimeOperation.Name) {
	case openAIUpstreamOperationChatCompletions, openAIUpstreamOperationResponses:
		return true
	default:
		return false
	}
}

func contextOverflowPromotionPlannedSourceModelID(plan requestPlan) string {
	return contextOverflowPromotionSourceModelID(plan, executionResult{})
}

func (s *Service) preselectOverflowAffinityPromotionPlan(request *http.Request, sourcePlan requestPlan) (requestPlan, bool) {
	if s == nil || s.cache == nil || s.overflowAffinityCache == nil || !planAllowsContextOverflowPromotion(sourcePlan) {
		return requestPlan{}, false
	}
	sourceModelID := contextOverflowPromotionPlannedSourceModelID(sourcePlan)
	if sourceModelID == "" {
		return requestPlan{}, false
	}
	activeProfile, snapshot, generationToken, err := s.cache.loadFreshActiveRuntimePlanWithGenerationToken(request.Context())
	if err != nil {
		return requestPlan{}, false
	}
	return s.preselectOverflowAffinityPromotionPlanForSnapshot(request, sourcePlan, activeProfile.ID, snapshot, generationToken)
}

func (s *Service) preselectOverflowAffinityPromotionPlanForSnapshot(request *http.Request, sourcePlan requestPlan, activeProfileID int, snapshot *planningSnapshot, generationToken string) (requestPlan, bool) {
	if s == nil || s.overflowAffinityCache == nil || !planAllowsContextOverflowPromotion(sourcePlan) || activeProfileID != sourcePlan.ProfileID || snapshot == nil {
		return requestPlan{}, false
	}
	sourceModelID := contextOverflowPromotionPlannedSourceModelID(sourcePlan)
	if sourceModelID == "" {
		return requestPlan{}, false
	}
	sourceModel, found := snapshot.ModelsByID[sourceModelID]
	if !found || sourceModel.ContextOverflowPromotionTargetID == nil {
		return requestPlan{}, false
	}
	promotionTargetModelID := strings.TrimSpace(*sourceModel.ContextOverflowPromotionTargetID)
	if promotionTargetModelID == "" || promotionTargetModelID == sourceModelID {
		return requestPlan{}, false
	}
	if _, found := snapshot.ModelsByID[promotionTargetModelID]; !found {
		return requestPlan{}, false
	}
	contextBucket := overflowAffinityContextBucket(sourcePlan.RequestContextEstimation)
	sourceSelectedTerminalTargetID := sourcePlan.selectedTerminalTargetID()
	key, _, ok := buildOverflowAffinityCacheKey(overflowAffinityCacheKeyInput{
		profileID:                   sourcePlan.ProfileID,
		operationName:               strings.TrimSpace(sourcePlan.RuntimeOperation.Name),
		sourceResolvedModelID:       sourceModelID,
		sourceSelectedTerminalID:    sourceSelectedTerminalTargetID,
		configuredPromotionTargetID: promotionTargetModelID,
		affinityHeaders:             request.Header,
		processLocalSecret:          s.secretEncryptionKey,
		routingGenerationToken:      generationToken,
		contextBucket:               contextBucket,
	})
	if !ok {
		return requestPlan{}, false
	}
	entry, found := s.overflowAffinityCache.get(key)
	if !found {
		return requestPlan{}, false
	}
	if !overflowAffinityCacheEntryMatchesPlan(entry, sourceModelID, sourceSelectedTerminalTargetID, promotionTargetModelID, generationToken, contextBucket) {
		return requestPlan{}, false
	}
	promotedPlan, err := s.buildExplicitTargetRequestPlan(request, sourcePlan.RawRequestBody, s.runtimeProxyConfigSnapshot(), activeProfileID, snapshot, promotionTargetModelID)
	if err != nil {
		return requestPlan{}, false
	}
	if resolvedTargetModelID := strings.TrimSpace(dereferenceString(promotedPlan.ResolvedTargetModelID)); resolvedTargetModelID == "" || resolvedTargetModelID == sourceModelID {
		return requestPlan{}, false
	}
	if promotedSelectedTerminalTargetID := promotedPlan.selectedTerminalTargetID(); promotedSelectedTerminalTargetID != nil && sourceSelectedTerminalTargetID != nil && *promotedSelectedTerminalTargetID == *sourceSelectedTerminalTargetID {
		return requestPlan{}, false
	}
	affinityMetadata := buildRuntimeContextOverflowAffinityDecision(runtimeContextOverflowAffinityStateAccepted, entry, nil)
	return mergeContextOverflowPreselectedPlan(sourcePlan, promotedPlan, affinityMetadata), true
}

func overflowAffinityCacheEntryMatchesPlan(entry overflowAffinityCacheEntry, sourceModelID string, sourceSelectedTerminalTargetID *int, promotionTargetModelID string, generationToken string, contextBucket string) bool {
	return strings.TrimSpace(entry.sourceModelID) == sourceModelID &&
		strings.TrimSpace(entry.promotionTargetID) == promotionTargetModelID &&
		entry.sourceSelectedTerminalTargetID == overflowAffinityTerminalTargetKey(sourceSelectedTerminalTargetID) &&
		entry.generationToken == generationToken &&
		entry.contextBucket == contextBucket
}

func buildRuntimeContextOverflowAffinityDecision(state string, entry overflowAffinityCacheEntry, rejectionReason *string) *runtimeContextOverflowAffinityDecision {
	state = strings.TrimSpace(state)
	switch state {
	case runtimeContextOverflowAffinityStateConsidered,
		runtimeContextOverflowAffinityStateAccepted,
		runtimeContextOverflowAffinityStateRejectedRevalidation,
		runtimeContextOverflowAffinityStatePopulated:
	default:
		return nil
	}
	return &runtimeContextOverflowAffinityDecision{
		State:                  state,
		AffinityHashPrefix:     strings.TrimSpace(entry.affinityHashPrefix),
		ParentHashPrefix:       cloneRuntimeStringPointer(entry.parentHashPrefix),
		ContextBucket:          strings.TrimSpace(entry.contextBucket),
		SourceModelID:          strings.TrimSpace(entry.sourceModelID),
		PromotionTargetModelID: strings.TrimSpace(entry.promotionTargetID),
		RejectionReason:        runtimeContextOverflowAffinityRejectionReasonPointer(rejectionReason),
	}
}

func runtimeContextOverflowAffinityRejectionReasonPointer(rejectionReason *string) *string {
	if rejectionReason == nil {
		return nil
	}
	reason := strings.TrimSpace(*rejectionReason)
	switch reason {
	case runtimeContextOverflowAffinityRejectionMissingAffinity,
		runtimeContextOverflowAffinityRejectionExpired,
		runtimeContextOverflowAffinityRejectionGenerationMismatch,
		runtimeContextOverflowAffinityRejectionTargetChanged,
		runtimeContextOverflowAffinityRejectionTargetInvalid,
		runtimeContextOverflowAffinityRejectionSameTerminal,
		runtimeContextOverflowAffinityRejectionStreaming,
		runtimeContextOverflowAffinityRejectionOperationIneligible:
		return &reason
	default:
		return nil
	}
}

func (s *Service) populateOverflowAffinityCacheAfterPromotion(request *http.Request, sourcePlan requestPlan, sourceExecution executionResult, promotedPlan requestPlan, promotedExecution executionResult, classification cliProxyAPIOverflowClassification) *runtimeContextOverflowAffinityDecision {
	if s == nil || s.cache == nil || s.overflowAffinityCache == nil || !classification.Promotable || !planAllowsContextOverflowPromotion(sourcePlan) || promotedExecution.Response == nil {
		return nil
	}
	sourceModelID := contextOverflowPromotionSourceModelID(sourcePlan, sourceExecution)
	if sourceModelID == "" {
		return nil
	}
	activeProfile, snapshot, generationToken, err := s.cache.loadFreshActiveRuntimePlanWithGenerationToken(request.Context())
	if err != nil || snapshot == nil || activeProfile.ID != sourcePlan.ProfileID {
		return nil
	}
	sourceModel, found := snapshot.ModelsByID[sourceModelID]
	if !found || sourceModel.ContextOverflowPromotionTargetID == nil {
		return nil
	}
	promotionTargetModelID := strings.TrimSpace(*sourceModel.ContextOverflowPromotionTargetID)
	if promotionTargetModelID == "" || promotionTargetModelID == sourceModelID || strings.TrimSpace(promotedPlan.RequestedModelID) != promotionTargetModelID {
		return nil
	}
	if _, found := snapshot.ModelsByID[promotionTargetModelID]; !found {
		return nil
	}
	revalidatedPlan, err := s.buildExplicitTargetRequestPlan(request, sourcePlan.RawRequestBody, s.runtimeProxyConfigSnapshot(), activeProfile.ID, snapshot, promotionTargetModelID)
	if err != nil || strings.TrimSpace(revalidatedPlan.RequestedModelID) != promotionTargetModelID {
		return nil
	}
	promotedResolvedTargetModelID := strings.TrimSpace(dereferenceString(promotedExecution.ResolvedTargetModelID))
	if promotedResolvedTargetModelID == "" {
		promotedResolvedTargetModelID = strings.TrimSpace(dereferenceString(promotedPlan.ResolvedTargetModelID))
	}
	revalidatedResolvedTargetModelID := strings.TrimSpace(dereferenceString(revalidatedPlan.ResolvedTargetModelID))
	if promotedResolvedTargetModelID == "" || revalidatedResolvedTargetModelID == "" || promotedResolvedTargetModelID != revalidatedResolvedTargetModelID {
		return nil
	}
	sourceSelectedTerminalTargetID := sourcePlan.selectedTerminalTargetID()
	if promotedSelectedTerminalTargetID := revalidatedPlan.selectedTerminalTargetID(); promotedSelectedTerminalTargetID != nil && sourceSelectedTerminalTargetID != nil && *promotedSelectedTerminalTargetID == *sourceSelectedTerminalTargetID {
		return nil
	}
	contextBucket := overflowAffinityContextBucket(sourcePlan.RequestContextEstimation)
	key, material, ok := buildOverflowAffinityCacheKey(overflowAffinityCacheKeyInput{
		profileID:                   sourcePlan.ProfileID,
		operationName:               strings.TrimSpace(sourcePlan.RuntimeOperation.Name),
		sourceResolvedModelID:       sourceModelID,
		sourceSelectedTerminalID:    sourceSelectedTerminalTargetID,
		configuredPromotionTargetID: promotionTargetModelID,
		affinityHeaders:             request.Header,
		processLocalSecret:          s.secretEncryptionKey,
		routingGenerationToken:      generationToken,
		contextBucket:               contextBucket,
	})
	if !ok {
		return nil
	}
	entry := buildOverflowAffinityCacheEntry(overflowAffinityCacheEntryInput{
		promotionTargetID:        promotionTargetModelID,
		sourceModelID:            sourceModelID,
		sourceSelectedTerminalID: sourceSelectedTerminalTargetID,
		generationToken:          generationToken,
		contextBucket:            contextBucket,
	}, material)
	s.overflowAffinityCache.put(key, entry)
	return buildRuntimeContextOverflowAffinityDecision(runtimeContextOverflowAffinityStatePopulated, entry, nil)
}

func contextOverflowPromotionSourceModelID(plan requestPlan, execution executionResult) string {
	if plan.ContextRouting != nil && plan.ContextRouting.FacadeSelection != nil && plan.ContextRouting.FacadeSelection.SelectedTargetModelID != nil {
		if selectedTargetModelID := strings.TrimSpace(*plan.ContextRouting.FacadeSelection.SelectedTargetModelID); selectedTargetModelID != "" {
			return selectedTargetModelID
		}
	}
	if execution.ResolvedTargetModelID != nil {
		if resolvedTargetModelID := strings.TrimSpace(*execution.ResolvedTargetModelID); resolvedTargetModelID != "" {
			return resolvedTargetModelID
		}
	}
	return strings.TrimSpace(dereferenceString(plan.ResolvedTargetModelID))
}

func runtimeContextOverflowPromotionAttemptCount(result executionResult) int {
	if len(result.Attempts) > 0 {
		return len(result.Attempts)
	}
	if result.AttemptCount > 0 {
		return result.AttemptCount
	}
	if result.Response != nil {
		return 1
	}
	return 0
}

func runtimeContextOverflowPromotionUsableContextWindowTokens(contextRouting *runtimeContextRoutingDecision) *int {
	if contextRouting == nil {
		return nil
	}
	if contextRouting.SelectedUsableContextWindowTokens != nil {
		return cloneRuntimeIntPointer(contextRouting.SelectedUsableContextWindowTokens)
	}
	return cloneRuntimeIntPointer(contextRouting.UsableContextWindowTokens)
}

func contextOverflowPromotionEstimationMode(plan requestPlan) string {
	if plan.RequestContextEstimation == nil {
		return runtimeContextOverflowPromotionEstimationModePassThrough
	}
	return runtimeContextOverflowPromotionEstimationModeEstimated
}

func buildContextOverflowPromotionDecision(sourcePlan requestPlan, sourceExecution executionResult, promotedPlan requestPlan, promotedExecution executionResult, classification cliProxyAPIOverflowClassification) *runtimeContextOverflowPromotionDecision {
	if !classification.Promotable {
		return nil
	}
	sourceAttemptCount := runtimeContextOverflowPromotionAttemptCount(sourceExecution)
	finalAttemptCount := sourceAttemptCount + runtimeContextOverflowPromotionAttemptCount(promotedExecution)
	promotion := &runtimeContextOverflowPromotionDecision{
		TriggerStatus:      sourceExecution.Response.StatusCode,
		TriggerClassifier:  strings.TrimSpace(classification.Classifier),
		EstimationMode:     contextOverflowPromotionEstimationMode(sourcePlan),
		SourceAttemptCount: sourceAttemptCount,
		FinalAttemptCount:  finalAttemptCount,
		Result:             runtimeContextOverflowPromotionResultPromotedSuccess,
	}
	if code := strings.TrimSpace(classification.ErrorCode); code != "" {
		promotion.TriggerErrorCode = stringPtr(code)
	}
	if sourceResolvedTargetModelID := strings.TrimSpace(contextOverflowPromotionSourceModelID(sourcePlan, sourceExecution)); sourceResolvedTargetModelID != "" {
		promotion.FromResolvedTargetModelID = stringPtr(sourceResolvedTargetModelID)
	}
	if sourceSelectedTerminalTargetID := sourcePlan.selectedTerminalTargetID(); sourceSelectedTerminalTargetID != nil {
		promotion.FromSelectedTerminalTargetID = cloneRuntimeIntPointer(sourceSelectedTerminalTargetID)
	}
	if promotedResolvedTargetModelID := strings.TrimSpace(dereferenceString(promotedExecution.ResolvedTargetModelID)); promotedResolvedTargetModelID == "" {
		if fallbackResolvedTargetModelID := strings.TrimSpace(dereferenceString(promotedPlan.ResolvedTargetModelID)); fallbackResolvedTargetModelID != "" {
			promotion.ToResolvedTargetModelID = stringPtr(fallbackResolvedTargetModelID)
		}
	} else {
		promotion.ToResolvedTargetModelID = stringPtr(promotedResolvedTargetModelID)
	}
	if promotedSelectedTerminalTargetID := promotedPlan.selectedTerminalTargetID(); promotedSelectedTerminalTargetID != nil {
		promotion.ToSelectedTerminalTargetID = cloneRuntimeIntPointer(promotedSelectedTerminalTargetID)
	}
	promotion.FromUsableContextWindowTokens = runtimeContextOverflowPromotionUsableContextWindowTokens(sourcePlan.ContextRouting)
	promotion.ToUsableContextWindowTokens = runtimeContextOverflowPromotionUsableContextWindowTokens(promotedPlan.ContextRouting)
	return promotion
}

func mergeContextOverflowPromotedPlan(sourcePlan requestPlan, promotedPlan requestPlan, sourceExecution executionResult, promotedExecution executionResult, classification cliProxyAPIOverflowClassification) requestPlan {
	merged := promotedPlan
	merged.RequestedModelID = sourcePlan.RequestedModelID
	merged.RequestedVendorID = sourcePlan.RequestedVendorID
	merged.RequestedVendorKey = sourcePlan.RequestedVendorKey
	merged.RequestedVendorName = sourcePlan.RequestedVendorName
	merged.ProfileID = sourcePlan.ProfileID
	merged.APIFamily = sourcePlan.APIFamily
	merged.RuntimeOperation = sourcePlan.RuntimeOperation
	merged.RuntimeOperationPathParams = cloneStringMap(sourcePlan.RuntimeOperationPathParams)
	merged.RawRequestBody = append([]byte(nil), sourcePlan.RawRequestBody...)
	merged.ReportCurrencySnapshot = sourcePlan.ReportCurrencySnapshot
	merged.ClientHeaders = cloneStringMap(sourcePlan.ClientHeaders)
	merged.RequestGenerationParams = sourcePlan.RequestGenerationParamsSnapshot()
	merged.RequestContextEstimation = sourcePlan.RequestContextEstimation
	baseContextRouting := sourcePlan.ContextRouting
	if baseContextRouting == nil {
		baseContextRouting = promotedPlan.ContextRouting
	}
	if baseContextRouting == nil {
		baseContextRouting = &runtimeContextRoutingDecision{Policy: runtimeContextRoutingPolicyName(sourcePlan.Strategy)}
	}
	merged.ContextRouting = attachRuntimeContextOverflowPromotionDecision(baseContextRouting, buildContextOverflowPromotionDecision(sourcePlan, sourceExecution, promotedPlan, promotedExecution, classification))
	merged.SelectedTerminalTargetID = cloneRuntimeIntPointer(sourcePlan.SelectedTerminalTargetID)
	return merged
}

func mergeContextOverflowPreselectedPlan(sourcePlan requestPlan, promotedPlan requestPlan, affinityMetadata *runtimeContextOverflowAffinityDecision) requestPlan {
	merged := promotedPlan
	merged.RequestedModelID = sourcePlan.RequestedModelID
	merged.RequestedVendorID = sourcePlan.RequestedVendorID
	merged.RequestedVendorKey = sourcePlan.RequestedVendorKey
	merged.RequestedVendorName = sourcePlan.RequestedVendorName
	merged.ProfileID = sourcePlan.ProfileID
	merged.APIFamily = sourcePlan.APIFamily
	merged.RuntimeOperation = sourcePlan.RuntimeOperation
	merged.RuntimeOperationPathParams = cloneStringMap(sourcePlan.RuntimeOperationPathParams)
	merged.RawRequestBody = append([]byte(nil), sourcePlan.RawRequestBody...)
	merged.ReportCurrencySnapshot = sourcePlan.ReportCurrencySnapshot
	merged.ClientHeaders = cloneStringMap(sourcePlan.ClientHeaders)
	merged.RequestGenerationParams = sourcePlan.RequestGenerationParamsSnapshot()
	merged.RequestContextEstimation = sourcePlan.RequestContextEstimation
	merged.ContextRouting = sourcePlan.ContextRouting
	if merged.ContextRouting == nil {
		merged.ContextRouting = promotedPlan.ContextRouting
	}
	merged.ContextRouting = attachRuntimeContextOverflowAffinityDecision(merged.ContextRouting, affinityMetadata)
	merged.SelectedTerminalTargetID = cloneRuntimeIntPointer(sourcePlan.SelectedTerminalTargetID)
	merged.ContextOverflowPromotionPreselected = true
	return merged
}

func mergeContextOverflowPromotedExecution(sourceExecution executionResult, promotedExecution executionResult) executionResult {
	merged := promotedExecution
	merged.Attempts = append(append([]executionAttempt(nil), sourceExecution.Attempts...), promotedExecution.Attempts...)
	merged.AttemptCount = sourceExecution.AttemptCount + promotedExecution.AttemptCount
	if len(merged.Attempts) > 0 {
		merged.AttemptCount = len(merged.Attempts)
	}
	return merged
}

func (s *Service) buildContextOverflowPromotionPlan(request *http.Request, sourcePlan requestPlan, execution executionResult) (requestPlan, error) {
	if s == nil || s.cache == nil {
		return requestPlan{}, ErrPublishedRuntimeSnapshotUnavailable
	}
	sourceModelID := contextOverflowPromotionSourceModelID(sourcePlan, execution)
	if sourceModelID == "" {
		return requestPlan{}, fmt.Errorf("context overflow promotion source model unavailable")
	}
	activeProfile, snapshot, err := s.cache.LoadFreshActiveRuntimePlan(request.Context())
	if err != nil {
		return requestPlan{}, err
	}
	if activeProfile.ID != sourcePlan.ProfileID {
		return requestPlan{}, fmt.Errorf("active profile changed before context overflow promotion replay")
	}
	sourceModel, found := snapshot.ModelsByID[sourceModelID]
	if !found {
		return requestPlan{}, fmt.Errorf("context overflow promotion source model %q is unavailable", sourceModelID)
	}
	if sourceModel.ContextOverflowPromotionTargetID == nil {
		return requestPlan{}, fmt.Errorf("context overflow promotion target not configured")
	}
	promotionTargetModelID := strings.TrimSpace(*sourceModel.ContextOverflowPromotionTargetID)
	if promotionTargetModelID == "" || promotionTargetModelID == sourceModelID {
		return requestPlan{}, fmt.Errorf("context overflow promotion target %q is invalid", promotionTargetModelID)
	}
	promotedPlan, err := s.buildExplicitTargetRequestPlan(request, sourcePlan.RawRequestBody, s.runtimeProxyConfigSnapshot(), activeProfile.ID, snapshot, promotionTargetModelID)
	if err != nil {
		return requestPlan{}, err
	}
	if resolvedTargetModelID := strings.TrimSpace(dereferenceString(promotedPlan.ResolvedTargetModelID)); resolvedTargetModelID != "" && resolvedTargetModelID == sourceModelID {
		return requestPlan{}, fmt.Errorf("context overflow promotion target resolved back to source model %q", sourceModelID)
	}
	if promotedSelectedTerminalTargetID := promotedPlan.selectedTerminalTargetID(); promotedSelectedTerminalTargetID != nil && *promotedSelectedTerminalTargetID == execution.Connection.ID {
		return requestPlan{}, fmt.Errorf("context overflow promotion target resolved back to source terminal target %d", execution.Connection.ID)
	}
	return promotedPlan, nil
}

func (s *Service) tryContextOverflowPromotion(request *http.Request, sourcePlan requestPlan, execution executionResult, classification cliProxyAPIOverflowClassification) (requestPlan, executionResult, error, bool) {
	if !classification.Promotable || !planAllowsContextOverflowPromotion(sourcePlan) {
		return requestPlan{}, executionResult{}, nil, false
	}
	promotedPlan, err := s.buildContextOverflowPromotionPlan(request, sourcePlan, execution)
	if err != nil {
		slog.Debug("runtime context overflow promotion skipped", "requested_model_id", sourcePlan.RequestedModelID, "resolved_target_model_id", dereferenceString(execution.ResolvedTargetModelID), "error", err)
		return requestPlan{}, executionResult{}, nil, false
	}
	promotedExecution, err := s.executeRequest(request.Context(), request.Method, promotedPlan, request.URL.RawQuery, newBufferedRuntimeRequestBodySource(sourcePlan.RawRequestBody))
	if err != nil {
		return promotedPlan, executionResult{}, err, true
	}
	return promotedPlan, promotedExecution, nil, true
}

// Downstream bytes become committed on the first header/body write. Once that
// happens, the current attempt is definitive and the runtime must not pivot to
// a retry or hedge winner because the client-visible response has already begun.
type runtimeDeferredCommitWriter struct {
	dst        http.ResponseWriter
	header     http.Header
	statusCode int
	committed  bool
}

func newRuntimeDeferredCommitWriter(dst http.ResponseWriter) *runtimeDeferredCommitWriter {
	return &runtimeDeferredCommitWriter{
		dst:        dst,
		header:     make(http.Header),
		statusCode: http.StatusOK,
	}
}

func (writer *runtimeDeferredCommitWriter) Header() http.Header {
	if writer.committed {
		return writer.dst.Header()
	}
	return writer.header
}

func (writer *runtimeDeferredCommitWriter) WriteHeader(statusCode int) {
	if writer.committed {
		return
	}
	writer.statusCode = statusCode
}

func (writer *runtimeDeferredCommitWriter) Write(payload []byte) (int, error) {
	writer.Commit()
	return writer.dst.Write(payload)
}

func (writer *runtimeDeferredCommitWriter) Flush() {
	writer.Commit()
	if flusher, ok := writer.dst.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (writer *runtimeDeferredCommitWriter) Commit() {
	if writer.committed {
		return
	}
	copyResponseHeaders(writer.dst.Header(), writer.header)
	writer.dst.WriteHeader(writer.statusCode)
	writer.committed = true
}

func (writer *runtimeDeferredCommitWriter) Committed() bool {
	return writer.committed
}

const (
	runtimeStreamOutcomeNotStreaming                 = "not_streaming"
	runtimeStreamOutcomeCompleted                    = "completed"
	runtimeStreamOutcomeProviderIncomplete           = "provider_incomplete"
	runtimeStreamOutcomeClientDisconnected           = "client_disconnected"
	runtimeStreamOutcomeUpstreamReadError            = "upstream_read_error"
	runtimeStreamOutcomeUpstreamEndedWithoutTerminal = "upstream_ended_without_terminal"
	runtimeStreamOutcomeUnknown                      = "unknown"

	runtimeStreamErrorKindClientWriteFailed      = "client_write_failed"
	runtimeStreamErrorKindRequestContextCanceled = "request_context_canceled"
	runtimeStreamErrorKindUpstreamReadFailed     = "upstream_read_failed"
	runtimeStreamErrorKindMissingTerminalEvent   = "missing_terminal_event"
	runtimeStreamErrorDetailMaxLength            = 512
)

type runtimeResponseCapture struct {
	Body                     []byte
	AuditBody                []byte
	Usage                    responseUsage
	UsageRule                runtimeUsageNormalizationRule
	FirstMeaningfulPayloadAt *time.Time
	CompletedAt              *time.Time
	StreamOutcome            string
	StreamErrorKind          *string
	StreamErrorDetail        *string
}

func (capture runtimeResponseCapture) extractedUsage() responseUsage {
	if capture.Usage.hasValues() || capture.Usage.discarded {
		if capture.UsageRule.configured() {
			return capture.Usage.canonicalizedForRuntimeUsage(capture.UsageRule)
		}
		return capture.Usage.normalized()
	}
	return extractResponseUsage(capture.Body, capture.UsageRule).normalized()
}

func proxyNonEventResponseAndCaptureUsage(hooks operationResponseHooks, dst io.Writer, src io.Reader, contentType string, now func() time.Time, captureAuditBody bool) (runtimeResponseCapture, error) {
	if !responseMayContainJSONUsage(contentType) {
		writers := []io.Writer{dst}
		auditBuffer := &bytes.Buffer{}
		if captureAuditBody {
			writers = append(writers, auditBuffer)
		}
		_, err := io.Copy(io.MultiWriter(writers...), src)
		completedAt := now()
		capture := runtimeResponseCapture{CompletedAt: &completedAt, StreamOutcome: runtimeStreamOutcomeNotStreaming, UsageRule: hooks.UsageRule}
		if captureAuditBody {
			capture.AuditBody = append([]byte(nil), auditBuffer.Bytes()...)
		}
		return capture, err
	}
	capture := newStreamedResponseUsageCapture(hooks.UsageRule)
	writers := []io.Writer{dst, capture}
	auditBuffer := &bytes.Buffer{}
	if captureAuditBody {
		writers = append(writers, auditBuffer)
	}
	_, copyErr := io.Copy(io.MultiWriter(writers...), src)
	completedAt := now()
	return capture.runtimeResponseCapture(completedAt, captureAuditBody, auditBuffer.Bytes()), copyErr
}

func responseMayContainJSONUsage(contentType string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(contentType))
	return trimmed == "" || strings.Contains(trimmed, "json")
}

const (
	runtimeUsageObjectCaptureLimit = 8 * 1024
	runtimeJSONKeyCaptureLimit     = 64
)

type streamedResponseUsageCapture struct {
	parser *streamedResponseUsageParser
}

func newStreamedResponseUsageCapture(rule runtimeUsageNormalizationRule) *streamedResponseUsageCapture {
	return &streamedResponseUsageCapture{parser: newStreamedResponseUsageParser(rule)}
}

func (capture *streamedResponseUsageCapture) Write(payload []byte) (int, error) {
	capture.parser.consume(payload)
	return len(payload), nil
}

func (capture *streamedResponseUsageCapture) runtimeResponseCapture(completedAt time.Time, captureAuditBody bool, auditBody []byte) runtimeResponseCapture {
	usage := capture.parser.extractedUsage()
	responseCapture := runtimeResponseCapture{
		Body:          buildUsageBodyFromResponseUsage(usage),
		Usage:         usage,
		UsageRule:     capture.parser.rule,
		CompletedAt:   &completedAt,
		StreamOutcome: runtimeStreamOutcomeNotStreaming,
	}
	if captureAuditBody {
		responseCapture.AuditBody = append([]byte(nil), auditBody...)
	}
	return responseCapture
}

type runtimeJSONUsagePath uint8

const (
	runtimeJSONUsagePathOther runtimeJSONUsagePath = iota
	runtimeJSONUsagePathRoot
	runtimeJSONUsagePathResponse
)

type runtimeJSONFrame struct {
	container    byte
	path         runtimeJSONUsagePath
	expectingKey bool
	pendingKey   string
}

type runtimeJSONUsageObjectCapture struct {
	carrier   runtimeUsageCarrier
	buffer    bytes.Buffer
	depth     int
	inString  bool
	escaped   bool
	oversized bool
}

func newRuntimeJSONUsageObjectCapture(carrier runtimeUsageCarrier) *runtimeJSONUsageObjectCapture {
	capture := &runtimeJSONUsageObjectCapture{carrier: carrier}
	capture.buffer.Grow(256)
	return capture
}

func (capture *runtimeJSONUsageObjectCapture) consumeByte(value byte) bool {
	if !capture.oversized {
		if capture.buffer.Len() < runtimeUsageObjectCaptureLimit {
			_ = capture.buffer.WriteByte(value)
		} else {
			capture.oversized = true
			capture.buffer.Reset()
		}
	}
	if capture.inString {
		if capture.escaped {
			capture.escaped = false
			return false
		}
		switch value {
		case '\\':
			capture.escaped = true
		case '"':
			capture.inString = false
		}
		return false
	}
	switch value {
	case '"':
		capture.inString = true
	case '{':
		capture.depth++
	case '}':
		capture.depth--
	}
	return capture.depth == 0
}

type streamedResponseUsageParser struct {
	rule          runtimeUsageNormalizationRule
	frames        []runtimeJSONFrame
	inString      bool
	escaped       bool
	parsingKey    bool
	keyBytes      []byte
	keyEscaped    bool
	usage         responseUsage
	activeCapture *runtimeJSONUsageObjectCapture
}

func newStreamedResponseUsageParser(rule runtimeUsageNormalizationRule) *streamedResponseUsageParser {
	return &streamedResponseUsageParser{rule: rule}
}

func (parser *streamedResponseUsageParser) consume(payload []byte) {
	for _, value := range payload {
		parser.consumeByte(value)
	}
}

func (parser *streamedResponseUsageParser) consumeByte(value byte) {
	if parser.activeCapture != nil {
		if parser.activeCapture.consumeByte(value) {
			parser.mergeCapturedUsage(parser.activeCapture)
			parser.activeCapture = nil
		}
	}
	if parser.inString {
		parser.consumeStringByte(value)
		return
	}
	if isJSONWhitespace(value) {
		return
	}
	switch value {
	case '"':
		parser.beginString()
	case '{':
		parser.beginObject()
	case '[':
		parser.beginArray()
	case '}':
		parser.endContainer('{')
	case ']':
		parser.endContainer('[')
	case ',':
		parser.handleComma()
	case ':':
		return
	default:
		parser.consumeScalarStart()
	}
}

func (parser *streamedResponseUsageParser) beginString() {
	parser.inString = true
	parser.escaped = false
	parser.parsingKey = false
	frame := parser.currentFrame()
	if frame == nil || frame.container != '{' {
		return
	}
	if frame.expectingKey {
		parser.parsingKey = true
		parser.keyBytes = parser.keyBytes[:0]
		parser.keyEscaped = false
		return
	}
	if frame.pendingKey != "" {
		frame.pendingKey = ""
	}
}

func (parser *streamedResponseUsageParser) consumeStringByte(value byte) {
	if parser.escaped {
		parser.escaped = false
		if parser.parsingKey {
			parser.keyEscaped = true
		}
		return
	}
	if parser.parsingKey && !parser.keyEscaped {
		switch value {
		case '\\':
			parser.keyEscaped = true
		case '"':
		default:
			if len(parser.keyBytes) < runtimeJSONKeyCaptureLimit {
				parser.keyBytes = append(parser.keyBytes, value)
			} else {
				parser.keyEscaped = true
			}
		}
	}
	switch value {
	case '\\':
		parser.escaped = true
	case '"':
		parser.inString = false
		if parser.parsingKey {
			parser.finishKeyString()
		}
	}
}

func (parser *streamedResponseUsageParser) finishKeyString() {
	frame := parser.currentFrame()
	if frame != nil && frame.container == '{' {
		if parser.keyEscaped {
			frame.pendingKey = ""
		} else {
			frame.pendingKey = string(parser.keyBytes)
		}
		frame.expectingKey = false
	}
	parser.parsingKey = false
}

func (parser *streamedResponseUsageParser) beginObject() {
	frame := parser.currentFrame()
	path := runtimeJSONUsagePathOther
	if frame == nil {
		path = runtimeJSONUsagePathRoot
	} else if frame.container == '{' {
		if carrier := runtimeJSONUsageCarrierForKey(frame.path, frame.pendingKey); parser.rule.allowsCarrier(carrier) {
			parser.activeCapture = newRuntimeJSONUsageObjectCapture(carrier)
			_ = parser.activeCapture.consumeByte('{')
		}
		if frame.path == runtimeJSONUsagePathRoot && frame.pendingKey == "response" {
			path = runtimeJSONUsagePathResponse
		}
		if frame.pendingKey != "" {
			frame.pendingKey = ""
		}
	}
	parser.frames = append(parser.frames, runtimeJSONFrame{container: '{', path: path, expectingKey: true})
}

func (parser *streamedResponseUsageParser) beginArray() {
	if frame := parser.currentFrame(); frame != nil && frame.container == '{' && frame.pendingKey != "" {
		frame.pendingKey = ""
	}
	parser.frames = append(parser.frames, runtimeJSONFrame{container: '[', path: runtimeJSONUsagePathOther})
}

func (parser *streamedResponseUsageParser) endContainer(container byte) {
	if len(parser.frames) == 0 {
		return
	}
	if parser.frames[len(parser.frames)-1].container != container {
		return
	}
	parser.frames = parser.frames[:len(parser.frames)-1]
}

func (parser *streamedResponseUsageParser) handleComma() {
	frame := parser.currentFrame()
	if frame == nil || frame.container != '{' {
		return
	}
	frame.expectingKey = true
	frame.pendingKey = ""
}

func (parser *streamedResponseUsageParser) consumeScalarStart() {
	frame := parser.currentFrame()
	if frame == nil || frame.container != '{' || frame.pendingKey == "" {
		return
	}
	frame.pendingKey = ""
}

func (parser *streamedResponseUsageParser) currentFrame() *runtimeJSONFrame {
	if len(parser.frames) == 0 {
		return nil
	}
	return &parser.frames[len(parser.frames)-1]
}

func (parser *streamedResponseUsageParser) mergeCapturedUsage(capture *runtimeJSONUsageObjectCapture) {
	if capture == nil || capture.oversized || capture.buffer.Len() == 0 {
		return
	}
	var payload map[string]any
	if err := json.Unmarshal(capture.buffer.Bytes(), &payload); err != nil {
		return
	}
	parser.usage.mergeRuntimeUsagePayload(parser.rule, capture.carrier, payload)
}

func (parser *streamedResponseUsageParser) extractedUsage() responseUsage {
	return parser.usage.canonicalizedForRuntimeUsage(parser.rule)
}

func runtimeJSONUsageCarrierForKey(path runtimeJSONUsagePath, key string) runtimeUsageCarrier {
	switch path {
	case runtimeJSONUsagePathRoot:
		switch key {
		case "usage":
			return runtimeUsageCarrierRootUsage
		case "usageMetadata":
			return runtimeUsageCarrierRootUsageMetadata
		}
	case runtimeJSONUsagePathResponse:
		if key == "usage" {
			return runtimeUsageCarrierResponseUsage
		}
	}
	return runtimeUsageCarrierNone
}

func isJSONWhitespace(value byte) bool {
	switch value {
	case ' ', '\t', '\n', '\r':
		return true
	default:
		return false
	}
}

func proxyEventStreamAndCaptureCompletedResponse(operation RuntimeOperation, ctx context.Context, dst io.Writer, src io.Reader, now func() time.Time, captureAuditBody bool) (runtimeResponseCapture, error) {
	streamHooks, ok := streamHooksForOperation(operation)
	if !ok {
		return runtimeResponseCapture{}, fmt.Errorf("stream hooks not configured for operation %s", operation.Name)
	}
	reader := bufio.NewReader(src)
	capture := sseCompletedResponseCapture{streamHooks: streamHooks}
	var auditBuffer bytes.Buffer

	captureResult := func(classification sseStreamClassification) runtimeResponseCapture {
		responseCapture := capture.runtimeResponseCapture(classification)
		if captureAuditBody {
			responseCapture.AuditBody = append([]byte(nil), auditBuffer.Bytes()...)
		}
		return responseCapture
	}

	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			capture.consumeLine(line, now())
			written, writeErr := dst.Write(line)
			if captureAuditBody && written > 0 {
				if written > len(line) {
					written = len(line)
				}
				auditBuffer.Write(line[:written])
			}
			if writeErr != nil {
				capture.finishEvent(now())
				return captureResult(classifySSEStreamOutcome(ctx, capture.terminalSignal, nil, writeErr)), writeErr
			}
		}
		if err == nil {
			continue
		}
		capture.finishEvent(now())
		if errors.Is(err, io.EOF) {
			return captureResult(classifySSEStreamOutcome(ctx, capture.terminalSignal, nil, nil)), nil
		}
		return captureResult(classifySSEStreamOutcome(ctx, capture.terminalSignal, err, nil)), err
	}
}

type sseTerminalSignal uint8

const (
	sseTerminalSignalNone sseTerminalSignal = iota
	sseTerminalSignalCompleted
	sseTerminalSignalProviderIncomplete
)

type sseStreamClassification struct {
	outcome string
	kind    *string
	detail  *string
}

type sseCompletedResponseCapture struct {
	streamHooks       operationStreamHooks
	currentEvent      string
	currentDataLines  []string
	completedResponse []byte
	usage             responseUsage
	firstPayloadAt    *time.Time
	completedAt       *time.Time
	terminalSignal    sseTerminalSignal
}

func (capture *sseCompletedResponseCapture) runtimeResponseCapture(classification sseStreamClassification) runtimeResponseCapture {
	outcome := strings.TrimSpace(classification.outcome)
	if outcome == "" {
		outcome = runtimeStreamOutcomeUnknown
	}
	usage := capture.usage.canonicalizedForRuntimeUsage(capture.streamHooks.UsageRule)
	body := capture.completedResponse
	if outcome != runtimeStreamOutcomeCompleted {
		usage = responseUsage{}
		body = nil
	}
	return runtimeResponseCapture{
		Body:                     body,
		Usage:                    usage,
		UsageRule:                capture.streamHooks.UsageRule,
		FirstMeaningfulPayloadAt: capture.firstPayloadAt,
		CompletedAt:              capture.completedAt,
		StreamOutcome:            outcome,
		StreamErrorKind:          classification.kind,
		StreamErrorDetail:        classification.detail,
	}
}

func classifySSEStreamOutcome(ctx context.Context, terminal sseTerminalSignal, upstreamErr error, writeErr error) sseStreamClassification {
	if writeErr != nil {
		return sseStreamClassification{outcome: runtimeStreamOutcomeClientDisconnected, kind: stringPtr(runtimeStreamErrorKindClientWriteFailed), detail: sanitizedStreamErrorDetail(writeErr)}
	}
	if ctx != nil && ctx.Err() != nil {
		return sseStreamClassification{outcome: runtimeStreamOutcomeClientDisconnected, kind: stringPtr(runtimeStreamErrorKindRequestContextCanceled), detail: sanitizedStreamErrorDetail(ctx.Err())}
	}
	if upstreamErr != nil && !errors.Is(upstreamErr, io.EOF) {
		return sseStreamClassification{outcome: runtimeStreamOutcomeUpstreamReadError, kind: stringPtr(runtimeStreamErrorKindUpstreamReadFailed), detail: sanitizedStreamErrorDetail(upstreamErr)}
	}
	switch terminal {
	case sseTerminalSignalProviderIncomplete:
		return sseStreamClassification{outcome: runtimeStreamOutcomeProviderIncomplete}
	case sseTerminalSignalCompleted:
		return sseStreamClassification{outcome: runtimeStreamOutcomeCompleted}
	case sseTerminalSignalNone:
		return sseStreamClassification{outcome: runtimeStreamOutcomeUpstreamEndedWithoutTerminal, kind: stringPtr(runtimeStreamErrorKindMissingTerminalEvent)}
	default:
		return sseStreamClassification{outcome: runtimeStreamOutcomeUnknown}
	}
}

var runtimeStreamErrorAuthorizationBearerPattern = regexp.MustCompile(`(?i)\b(authorization|proxy-authorization)\b\s*[:=]\s*Bearer\s+[A-Za-z0-9._~+/=-]+`)
var runtimeStreamErrorSensitiveFragmentPattern = regexp.MustCompile(`(?i)\b(x-api-key|api[-_ ]?key|api[-_ ]?token|token|secret|password|cookie)\b\s*[:=]\s*("[^"]*"|'[^']*'|\S+)`)
var runtimeStreamErrorBearerPattern = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]+`)
var runtimeStreamErrorJSONObjectPattern = regexp.MustCompile(`\{[^{}]{0,200}\}`)
var runtimeStreamErrorJSONArrayPattern = regexp.MustCompile(`\[[^\[\]]{0,200}\]`)

func sanitizedStreamErrorDetail(err error) *string {
	if err == nil {
		return nil
	}
	detail := strings.Join(strings.Fields(err.Error()), " ")
	if detail == "" {
		return nil
	}
	detail = runtimeStreamErrorAuthorizationBearerPattern.ReplaceAllString(detail, "$1=[REDACTED]")
	detail = runtimeStreamErrorSensitiveFragmentPattern.ReplaceAllString(detail, "$1=[REDACTED]")
	detail = runtimeStreamErrorBearerPattern.ReplaceAllString(detail, "Bearer [REDACTED]")
	detail = runtimeStreamErrorJSONObjectPattern.ReplaceAllString(detail, "[REDACTED]")
	detail = runtimeStreamErrorJSONArrayPattern.ReplaceAllString(detail, "[REDACTED]")
	detail = strings.TrimSpace(strings.Join(strings.Fields(detail), " "))
	if detail == "" {
		return nil
	}
	if len(detail) > runtimeStreamErrorDetailMaxLength {
		detail = detail[:runtimeStreamErrorDetailMaxLength]
	}
	return &detail
}

func (capture *sseCompletedResponseCapture) consumeLine(line []byte, observedAt time.Time) {
	trimmed := strings.TrimRight(string(line), "\r\n")
	if trimmed == "" {
		capture.finishEvent(observedAt)
		return
	}
	if strings.HasPrefix(trimmed, "event:") {
		capture.currentEvent = trimSSEFieldValue(strings.TrimPrefix(trimmed, "event:"))
		return
	}
	if strings.HasPrefix(trimmed, "data:") {
		capture.currentDataLines = append(capture.currentDataLines, trimSSEFieldValue(strings.TrimPrefix(trimmed, "data:")))
	}
}

func (capture *sseCompletedResponseCapture) finishEvent(observedAt time.Time) {
	if len(capture.currentDataLines) > 0 {
		capture.consumePayload([]byte(strings.Join(capture.currentDataLines, "\n")), observedAt)
	}
	capture.currentEvent = ""
	capture.currentDataLines = nil
}

func (capture *sseCompletedResponseCapture) consumePayload(payloadBytes []byte, observedAt time.Time) {
	if strings.TrimSpace(string(payloadBytes)) == "[DONE]" {
		if capture.streamHooks.CompleteOnDoneSentinel {
			completedAt := observedAt
			capture.completedAt = &completedAt
			capture.terminalSignal = sseTerminalSignalCompleted
		}
		return
	}

	var payload map[string]any
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return
	}
	if capture.firstPayloadAt == nil && payloadHasMeaningfulStreamContent(payload) {
		firstPayloadAt := observedAt
		capture.firstPayloadAt = &firstPayloadAt
	}
	if terminalSignal := capture.streamHooks.terminalSignal(capture.currentEvent, payload); terminalSignal != sseTerminalSignalNone {
		completedAt := observedAt
		capture.completedAt = &completedAt
		capture.terminalSignal = terminalSignal
	}
	capture.streamHooks.mergeUsage(&capture.usage, capture.currentEvent, payload)
	if usage := capture.usage.canonicalizedForRuntimeUsage(capture.streamHooks.UsageRule); usage.hasValues() {
		if usageBody := buildUsageBodyFromResponseUsage(usage); len(usageBody) > 0 {
			capture.completedResponse = usageBody
		}
	}
}

func trimSSEFieldValue(value string) string {
	return strings.TrimLeft(value, " ")
}

func payloadHasMeaningfulStreamContent(payload map[string]any) bool {
	return payloadContainsMeaningfulValue(payload)
}

func payloadContainsMeaningfulValue(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, nested := range typed {
			switch key {
			case "usage", "usageMetadata", "type", "id", "model", "role", "index", "stop_reason", "stop_sequence", "finishReason":
				continue
			case "text", "delta", "output_text", "partial_json", "arguments", "reasoning", "thinking":
				if strings.TrimSpace(stringValue(nested)) != "" {
					return true
				}
			}
			if payloadContainsMeaningfulValue(nested) {
				return true
			}
		}
	case []any:
		for _, nested := range typed {
			if payloadContainsMeaningfulValue(nested) {
				return true
			}
		}
	case string:
		return strings.TrimSpace(typed) != ""
	}
	return false
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	default:
		return ""
	}
}

func writeDomainError(w http.ResponseWriter, err error) {
	var runtimeErr *domainError
	if errors.As(err, &runtimeErr) {
		writeError(w, runtimeErr.StatusCode, runtimeErr.ErrorCode, runtimeErr.Detail, runtimeErr.Fields)
		return
	}
	writeError(w, http.StatusInternalServerError, "", "Internal server error", nil)
}

func writeTranslatedOpenAIError(w http.ResponseWriter, err error, fallbackDetail string) {
	var runtimeErr *domainError
	if errors.As(err, &runtimeErr) {
		writeDomainError(w, err)
		return
	}
	detail := strings.TrimSpace(fallbackDetail)
	if detail == "" {
		detail = "Failed to translate upstream response"
	}
	writeError(w, http.StatusBadGateway, "", detail, nil)
}

func writeError(w http.ResponseWriter, statusCode int, errorCode string, detail string, fields map[string]any) {
	payload := map[string]any{"detail": detail}
	if strings.TrimSpace(errorCode) != "" {
		payload["error"] = strings.TrimSpace(errorCode)
	}
	for key, value := range fields {
		if strings.TrimSpace(key) == "" || value == nil {
			continue
		}
		payload[key] = value
	}
	writeJSON(w, statusCode, payload)
}

func writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}
