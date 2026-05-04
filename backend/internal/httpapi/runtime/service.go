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
	TelemetryOutbox            TelemetryOutboxOptions
	FeedbackPipeline           RuntimeFeedbackPipelineOptions
	SideEffects                RuntimeSideEffectOptions
	Scheduler                  *background.Scheduler
}

type RuntimeProxyConfigSnapshot struct {
	BufferingMode config.RuntimeBufferingMode
	HTTPClient    *http.Client
}

type RuntimeProxyConfigProvider interface {
	RuntimeProxyConfigSnapshot() RuntimeProxyConfigSnapshot
}

type Service struct {
	executionPool              *pgxpool.Pool
	telemetryPool              *pgxpool.Pool
	feedbackPool               *pgxpool.Pool
	feedbackStore              *runtimeFeedbackStore
	httpClient                 *http.Client
	ownsHTTPClient             bool
	runtimeProxyConfigProvider RuntimeProxyConfigProvider
	staticRuntimeProxyConfig   RuntimeProxyConfigSnapshot
	now                        func() time.Time
	secretEncryptionKey        string
	dashboardUpdates           DashboardUpdatePublisher
	analyticsUpdates           AnalyticsUpdatePublisher
	cache                      *SharedCache
	runtimeState               *loadbalancedomain.LocalRuntimeStateStore
	telemetryOutbox            *runtimeTelemetryOutbox
	feedbackPipeline           *runtimeFeedbackPipeline
	runtimeSideEffects         *RuntimeSideEffectManager
	ownedScheduler             *background.Scheduler
}

type domainError struct {
	StatusCode int
	Detail     string
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

	scheduler := options.Scheduler
	if scheduler == nil {
		scheduler = background.NewScheduler(background.Config{})
	}
	service := &Service{
		executionPool:              executionPool,
		telemetryPool:              telemetryPool,
		feedbackPool:               feedbackPool,
		feedbackStore:              newRuntimeFeedbackStore(feedbackPool),
		httpClient:                 client,
		ownsHTTPClient:             ownsHTTPClient,
		runtimeProxyConfigProvider: options.RuntimeProxyConfigProvider,
		staticRuntimeProxyConfig:   RuntimeProxyConfigSnapshot{BufferingMode: settings.ResolvedRuntimeBufferingMode(), HTTPClient: client},
		now:                        now,
		secretEncryptionKey:        settings.SecretEncryptionKey,
		dashboardUpdates:           options.DashboardUpdates,
		analyticsUpdates:           options.AnalyticsUpdates,
		cache:                      options.Cache,
		runtimeState:               runtimeState,
	}
	telemetryOptions := options.TelemetryOutbox
	telemetryOptions.Scheduler = scheduler
	service.telemetryOutbox = newRuntimeTelemetryOutbox(telemetryPool, service.nowUTC, service.dashboardUpdates, service.analyticsUpdates, telemetryOptions)
	service.feedbackPipeline = newRuntimeFeedbackPipeline(service.feedbackStore, service.runtimeState, options.FeedbackPipeline)
	service.runtimeSideEffects = NewRuntimeSideEffectManager(service.telemetryOutbox, options.SideEffects)
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

func (s *Service) handleStreamingProxy(w http.ResponseWriter, r *http.Request) {
	if r.Body != nil {
		defer func() { _ = r.Body.Close() }()
	}
	runtimeConfig := s.runtimeProxyConfigSnapshot()
	if canBuildStreamingRequestPlan(r) {
		plan, err := s.buildProxyRequestPlan(r, nil, runtimeConfig)
		if err != nil {
			writeDomainError(w, err)
			return
		}
		if canStreamIncomingRequestBody(plan, r) {
			observer := newGeminiGenerationParamsStreamingObserver()
			plan.RequestGenerationSnapshot = observer.Snapshot
			s.handlePlannedProxy(w, r, plan, newStreamingRuntimeRequestBodySource(r.Body, r.ContentLength).withGenerationParamsObserver(observer))
			return
		}
	}
	rawBody, err := readBufferedRequestBody(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	plan, err := s.buildProxyRequestPlan(r, rawBody, runtimeConfig)
	if err != nil {
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

func (s *Service) buildProxyRequestPlan(r *http.Request, rawBody []byte, runtimeConfig RuntimeProxyConfigSnapshot) (requestPlan, error) {
	return s.buildRequestPlan(r.Context(), r, rawBody, runtimeConfig)
}

func canBuildStreamingRequestPlan(r *http.Request) bool {
	if r == nil {
		return false
	}
	return extractModelFromPath(r.URL.Path) != ""
}

func canStreamIncomingRequestBody(plan requestPlan, r *http.Request) bool {
	if r == nil {
		return false
	}
	if extractModelFromPath(r.URL.Path) == "" {
		return false
	}
	if !strings.EqualFold(plan.APIFamily, "gemini") {
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
		writeDomainError(w, err)
		return
	}
	defer func() { _ = execution.Response.Body.Close() }()
	s.writeProxyResponse(w, r, plan, execution, startedAt)
}

func (s *Service) writeProxyResponse(w http.ResponseWriter, r *http.Request, plan requestPlan, execution executionResult, startedAt time.Time) {
	proxyWriter := newRuntimeDeferredCommitWriter(w)
	copyResponseHeaders(proxyWriter.Header(), execution.Response.Header)
	proxyWriter.WriteHeader(execution.Response.StatusCode)

	var responseCapture runtimeResponseCapture
	contentType := strings.ToLower(strings.TrimSpace(execution.Response.Header.Get("Content-Type")))
	captureAuditBody := plan.AuditEnabledAtRequest && plan.AuditCaptureBodiesAtRequest
	if strings.Contains(contentType, "text/event-stream") {
		responseCapture, streamErr := proxyEventStreamAndCaptureCompletedResponse(r.Context(), proxyWriter, execution.Response.Body, s.nowUTC, captureAuditBody)
		if streamErr != nil {
			slog.Debug("runtime stream proxy ended with classified error", "error", streamErr, "stream_outcome", responseCapture.StreamOutcome)
		}
		proxyWriter.Commit()
		s.recordRuntimeActivity(plan, execution, r, startedAt, responseCapture)
		return
	}
	responseCapture, err := proxyNonEventResponseAndCaptureUsage(proxyWriter, execution.Response.Body, contentType, s.nowUTC, captureAuditBody)
	if err != nil {
		if !proxyWriter.Committed() {
			writeError(w, http.StatusBadGateway, "Failed to read upstream response")
		}
		return
	}
	proxyWriter.Commit()
	s.recordRuntimeActivity(plan, execution, r, startedAt, responseCapture)
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
	FirstMeaningfulPayloadAt *time.Time
	CompletedAt              *time.Time
	StreamOutcome            string
	StreamErrorKind          *string
	StreamErrorDetail        *string
}

func (capture runtimeResponseCapture) extractedUsage() responseUsage {
	if capture.Usage.hasValues() {
		return capture.Usage.normalized()
	}
	return extractResponseUsage(capture.Body).normalized()
}

func proxyNonEventResponseAndCaptureUsage(dst io.Writer, src io.Reader, contentType string, now func() time.Time, captureAuditBody bool) (runtimeResponseCapture, error) {
	if !responseMayContainJSONUsage(contentType) {
		writers := []io.Writer{dst}
		auditBuffer := &bytes.Buffer{}
		if captureAuditBody {
			writers = append(writers, auditBuffer)
		}
		_, err := io.Copy(io.MultiWriter(writers...), src)
		completedAt := now()
		capture := runtimeResponseCapture{CompletedAt: &completedAt, StreamOutcome: runtimeStreamOutcomeNotStreaming}
		if captureAuditBody {
			capture.AuditBody = append([]byte(nil), auditBuffer.Bytes()...)
		}
		return capture, err
	}
	capture := newStreamedResponseUsageCapture()
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

func newStreamedResponseUsageCapture() *streamedResponseUsageCapture {
	return &streamedResponseUsageCapture{parser: newStreamedResponseUsageParser()}
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

type runtimeJSONUsageKind uint8

const (
	runtimeJSONUsageKindNone runtimeJSONUsageKind = iota
	runtimeJSONUsageKindStandard
	runtimeJSONUsageKindGemini
)

type runtimeJSONFrame struct {
	container    byte
	path         runtimeJSONUsagePath
	expectingKey bool
	pendingKey   string
}

type runtimeJSONUsageObjectCapture struct {
	kind      runtimeJSONUsageKind
	buffer    bytes.Buffer
	depth     int
	inString  bool
	escaped   bool
	oversized bool
}

func newRuntimeJSONUsageObjectCapture(kind runtimeJSONUsageKind) *runtimeJSONUsageObjectCapture {
	capture := &runtimeJSONUsageObjectCapture{kind: kind}
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
	frames        []runtimeJSONFrame
	inString      bool
	escaped       bool
	parsingKey    bool
	keyBytes      []byte
	keyEscaped    bool
	usage         responseUsage
	activeCapture *runtimeJSONUsageObjectCapture
}

func newStreamedResponseUsageParser() *streamedResponseUsageParser {
	return &streamedResponseUsageParser{}
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
		if kind := runtimeJSONUsageKindForKey(frame.path, frame.pendingKey); kind != runtimeJSONUsageKindNone {
			parser.activeCapture = newRuntimeJSONUsageObjectCapture(kind)
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
	switch capture.kind {
	case runtimeJSONUsageKindStandard:
		parser.usage.mergeStandardUsagePayload(payload)
	case runtimeJSONUsageKindGemini:
		parser.usage.mergeGeminiUsagePayload(payload)
	}
}

func (parser *streamedResponseUsageParser) extractedUsage() responseUsage {
	return parser.usage.normalized()
}

func runtimeJSONUsageKindForKey(path runtimeJSONUsagePath, key string) runtimeJSONUsageKind {
	switch path {
	case runtimeJSONUsagePathRoot:
		switch key {
		case "usage":
			return runtimeJSONUsageKindStandard
		case "usageMetadata":
			return runtimeJSONUsageKindGemini
		}
	case runtimeJSONUsagePathResponse:
		if key == "usage" {
			return runtimeJSONUsageKindStandard
		}
	}
	return runtimeJSONUsageKindNone
}

func isJSONWhitespace(value byte) bool {
	switch value {
	case ' ', '\t', '\n', '\r':
		return true
	default:
		return false
	}
}

func proxyEventStreamAndCaptureCompletedResponse(ctx context.Context, dst io.Writer, src io.Reader, now func() time.Time, captureAuditBody bool) (runtimeResponseCapture, error) {
	reader := bufio.NewReader(src)
	capture := sseCompletedResponseCapture{}
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
	return runtimeResponseCapture{
		Body:                     capture.completedResponse,
		Usage:                    capture.usage,
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
		completedAt := observedAt
		capture.completedAt = &completedAt
		capture.terminalSignal = sseTerminalSignalCompleted
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
	if terminalSignal := payloadTerminalSignal(capture.currentEvent, payload); terminalSignal != sseTerminalSignalNone {
		completedAt := observedAt
		capture.completedAt = &completedAt
		capture.terminalSignal = terminalSignal
	}
	if usagePayload, ok := responseUsagePayload(payload); ok {
		capture.usage.mergeStandardUsagePayload(usagePayload)
	}
	if messagePayload, ok := payload["message"].(map[string]any); ok {
		if usagePayload, ok := messagePayload["usage"].(map[string]any); ok {
			capture.usage.mergeStandardUsagePayload(usagePayload)
		}
	}
	if usageMetadata, ok := payload["usageMetadata"].(map[string]any); ok {
		capture.usage.mergeGeminiUsagePayload(usageMetadata)
	}
	if usageBody := buildUsageBodyFromResponseUsage(capture.usage); len(usageBody) > 0 {
		capture.completedResponse = usageBody
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

func payloadTerminalSignal(event string, payload map[string]any) sseTerminalSignal {
	if event == "response.incomplete" {
		return sseTerminalSignalProviderIncomplete
	}
	if event == "response.completed" || event == "message_stop" {
		return sseTerminalSignalCompleted
	}
	if payloadType, _ := payload["type"].(string); payloadType == "response.incomplete" {
		return sseTerminalSignalProviderIncomplete
	} else if payloadType == "response.completed" || payloadType == "message_stop" {
		return sseTerminalSignalCompleted
	}
	if done, _ := payload["done"].(bool); done {
		return sseTerminalSignalCompleted
	}
	if _, hasGeminiUsage := payload["usageMetadata"].(map[string]any); hasGeminiUsage {
		return sseTerminalSignalCompleted
	}
	return sseTerminalSignalNone
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
		writeError(w, runtimeErr.StatusCode, runtimeErr.Detail)
		return
	}
	writeError(w, http.StatusInternalServerError, "Internal server error")
}

func writeError(w http.ResponseWriter, statusCode int, detail string) {
	writeJSON(w, statusCode, map[string]string{"detail": detail})
}

func writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}
