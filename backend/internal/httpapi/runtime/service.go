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
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	loadbalancedomain "github.com/coachpo/prism/backend/internal/domain/loadbalance"
	"github.com/coachpo/prism/backend/internal/platform/config"
)

type DashboardUpdatePublisher interface {
	PublishDashboardUpdate(context.Context, int, int) (bool, error)
}

type Options struct {
	Pool             *pgxpool.Pool
	HTTPClient       *http.Client
	Now              func() time.Time
	DashboardUpdates DashboardUpdatePublisher
	Cache            *SharedCache
	RuntimeState     *loadbalancedomain.LocalRuntimeStateStore
	TelemetryOutbox  TelemetryOutboxOptions
}

type Service struct {
	pool                *pgxpool.Pool
	ownsPool            bool
	httpClient          *http.Client
	ownsHTTPClient      bool
	now                 func() time.Time
	bufferingMode       config.RuntimeBufferingMode
	secretEncryptionKey string
	dashboardUpdates    DashboardUpdatePublisher
	cache               *SharedCache
	runtimeState        *loadbalancedomain.LocalRuntimeStateStore
	telemetryOutbox     *runtimeTelemetryOutbox
}

type domainError struct {
	StatusCode int
	Detail     string
}

func (err *domainError) Error() string {
	return err.Detail
}

func NewService(settings config.Settings, options Options) (*Service, error) {
	pool := options.Pool
	ownsPool := false
	if pool == nil {
		if strings.TrimSpace(settings.DatabaseURL) == "" {
			return nil, fmt.Errorf("database URL is required")
		}
		createdPool, err := pgxpool.New(context.Background(), settings.DatabaseURL)
		if err != nil {
			return nil, fmt.Errorf("create runtime database pool: %w", err)
		}
		pool = createdPool
		ownsPool = true
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

	service := &Service{
		pool:                pool,
		ownsPool:            ownsPool,
		httpClient:          client,
		ownsHTTPClient:      ownsHTTPClient,
		now:                 now,
		bufferingMode:       settings.ResolvedRuntimeBufferingMode(),
		secretEncryptionKey: settings.SecretEncryptionKey,
		dashboardUpdates:    options.DashboardUpdates,
		cache:               options.Cache,
		runtimeState:        runtimeState,
	}
	service.telemetryOutbox = newRuntimeTelemetryOutbox(pool, service.nowUTC, service.dashboardUpdates, options.TelemetryOutbox)
	return service, nil
}

func newRuntimeHTTPClient(settings config.Settings) *http.Client {
	transportConfig := settings.RuntimeTransport()
	return &http.Client{
		Timeout: 60 * time.Second,
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

func (s *Service) Close() {
	if s == nil {
		return
	}
	if s.telemetryOutbox != nil {
		result := s.telemetryOutbox.Close()
		if result.TimedOut {
			slog.Warn("runtime telemetry outbox close timed out", "elapsed", result.Elapsed, "pending_rows", result.PendingRows, "inflight", result.Inflight)
		}
	}
	if s.ownsHTTPClient && s.httpClient != nil {
		if closer, ok := s.httpClient.Transport.(interface{ CloseIdleConnections() }); ok {
			closer.CloseIdleConnections()
		}
	}
	if s.ownsPool && s.pool != nil {
		s.pool.Close()
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

func (s *Service) handleProxy(w http.ResponseWriter, r *http.Request) {
	switch s.bufferingMode {
	case config.RuntimeBufferingModeStreaming:
		s.handleStreamingProxy(w, r)
	default:
		s.handleBufferedProxy(w, r)
	}
}

func (s *Service) handleBufferedProxy(w http.ResponseWriter, r *http.Request) {
	if r.Body != nil {
		defer func() { _ = r.Body.Close() }()
	}
	rawBody, err := readBufferedRequestBody(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	plan, err := s.buildProxyRequestPlan(r, rawBody)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	s.handlePlannedProxy(w, r, plan, newBufferedRuntimeRequestBodySource(plan.UpstreamBody))
}

func (s *Service) handleStreamingProxy(w http.ResponseWriter, r *http.Request) {
	if r.Body != nil {
		defer func() { _ = r.Body.Close() }()
	}
	if canBuildStreamingRequestPlan(r) {
		plan, err := s.buildProxyRequestPlan(r, nil)
		if err != nil {
			writeDomainError(w, err)
			return
		}
		if canStreamIncomingRequestBody(plan, r) {
			s.handlePlannedProxy(w, r, plan, newStreamingRuntimeRequestBodySource(r.Body, r.ContentLength))
			return
		}
	}
	rawBody, err := readBufferedRequestBody(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	plan, err := s.buildProxyRequestPlan(r, rawBody)
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

func (s *Service) buildProxyRequestPlan(r *http.Request, rawBody []byte) (requestPlan, error) {
	return s.buildRequestPlan(r.Context(), r, rawBody)
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
	return len(plan.Connections) == 1
}

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
	copyResponseHeaders(w.Header(), execution.Response.Header)
	w.WriteHeader(execution.Response.StatusCode)

	responseCapture := runtimeResponseCapture{}
	contentType := strings.ToLower(strings.TrimSpace(execution.Response.Header.Get("Content-Type")))
	if strings.Contains(contentType, "text/event-stream") {
		responseCapture, _ = proxyEventStreamAndCaptureCompletedResponse(w, execution.Response.Body, s.nowUTC)
		s.recordRuntimeActivity(plan, execution, r, startedAt, responseCapture)
		return
	}
	var err error
	if s.bufferingMode == config.RuntimeBufferingModeStreaming {
		responseCapture, err = proxyNonEventResponseAndCaptureUsage(w, execution.Response.Body, contentType, s.nowUTC)
		if err != nil {
			return
		}
	} else {
		responseCapture.Body, err = io.ReadAll(execution.Response.Body)
		if err != nil {
			writeError(w, http.StatusBadGateway, "Failed to read upstream response")
			return
		}
		responseCapture.Usage = extractResponseUsage(responseCapture.Body)
		_, _ = io.Copy(w, bytes.NewReader(responseCapture.Body))
	}
	s.recordRuntimeActivity(plan, execution, r, startedAt, responseCapture)
}

type runtimeResponseCapture struct {
	Body                     []byte
	Usage                    responseUsage
	FirstMeaningfulPayloadAt *time.Time
	CompletedAt              *time.Time
}

func (capture runtimeResponseCapture) extractedUsage() responseUsage {
	if capture.Usage.hasValues() {
		return capture.Usage.normalized()
	}
	return extractResponseUsage(capture.Body).normalized()
}

func proxyNonEventResponseAndCaptureUsage(dst io.Writer, src io.Reader, contentType string, now func() time.Time) (runtimeResponseCapture, error) {
	if !responseMayContainJSONUsage(contentType) {
		_, err := io.Copy(dst, src)
		completedAt := now()
		return runtimeResponseCapture{CompletedAt: &completedAt}, err
	}
	capture := newStreamedResponseUsageCapture()
	_, copyErr := io.Copy(io.MultiWriter(dst, capture), src)
	completedAt := now()
	return capture.runtimeResponseCapture(completedAt), copyErr
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

func (capture *streamedResponseUsageCapture) runtimeResponseCapture(completedAt time.Time) runtimeResponseCapture {
	usage := capture.parser.extractedUsage()
	return runtimeResponseCapture{
		Body:        buildUsageBodyFromResponseUsage(usage),
		Usage:       usage,
		CompletedAt: &completedAt,
	}
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

func proxyEventStreamAndCaptureCompletedResponse(dst io.Writer, src io.Reader, now func() time.Time) (runtimeResponseCapture, error) {
	reader := bufio.NewReader(src)
	capture := sseCompletedResponseCapture{}

	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			capture.consumeLine(line, now())
			if _, writeErr := dst.Write(line); writeErr != nil {
				capture.finishEvent(now())
				return capture.runtimeResponseCapture(), writeErr
			}
		}
		if err == nil {
			continue
		}
		capture.finishEvent(now())
		if errors.Is(err, io.EOF) {
			return capture.runtimeResponseCapture(), nil
		}
		return capture.runtimeResponseCapture(), err
	}
}

type sseCompletedResponseCapture struct {
	currentEvent      string
	currentDataLines  []string
	completedResponse []byte
	usage             responseUsage
	firstPayloadAt    *time.Time
	completedAt       *time.Time
}

func (capture *sseCompletedResponseCapture) runtimeResponseCapture() runtimeResponseCapture {
	return runtimeResponseCapture{
		Body:                     capture.completedResponse,
		Usage:                    capture.usage,
		FirstMeaningfulPayloadAt: capture.firstPayloadAt,
		CompletedAt:              capture.completedAt,
	}
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
	var payload map[string]any
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return
	}
	if capture.firstPayloadAt == nil && payloadHasMeaningfulStreamContent(payload) {
		firstPayloadAt := observedAt
		capture.firstPayloadAt = &firstPayloadAt
	}
	if payloadSignalsTerminalCompletion(capture.currentEvent, payload) {
		completedAt := observedAt
		capture.completedAt = &completedAt
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

func payloadSignalsTerminalCompletion(event string, payload map[string]any) bool {
	if event == "response.completed" || event == "message_stop" {
		return true
	}
	if payloadType, _ := payload["type"].(string); payloadType == "response.completed" || payloadType == "message_stop" {
		return true
	}
	if done, _ := payload["done"].(bool); done {
		return true
	}
	_, hasGeminiUsage := payload["usageMetadata"].(map[string]any)
	return hasGeminiUsage
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
