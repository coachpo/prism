package connections

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/endpointdomain"
	"github.com/coachpo/prism/backend/internal/pgxutil"
)

const healthCheckRequestTimeout = 30 * time.Second

type apiFamilyAuthConfig struct {
	AuthHeader   string
	AuthPrefix   string
	ExtraHeaders map[string]string
}

type healthCheckProbeResult struct {
	HealthStatus   string
	Detail         string
	ResponseTimeMS int
}

type connectionHealthProbeReadModel struct {
	ConnectionID               int
	AuthType                   *string
	CustomHeaders              map[string]string
	Endpoint                   endpointRecord
	APIFamily                  string
	ModelID                    string
	OpenAIProbeEndpointVariant *string
	WritebackExpectedUpdatedAt *time.Time
}

type connectionHealthProbeInput struct {
	ConnectionID               int
	AuthType                   *string
	CustomHeaders              map[string]string
	Endpoint                   endpointRecord
	APIFamily                  string
	ModelID                    string
	OpenAIProbeEndpointVariant *string
	HeaderBlocklistRules       []headerBlocklistRuleRecord
	WritebackExpectedUpdatedAt *time.Time
}

var healthCheckAuthConfigs = map[string]apiFamilyAuthConfig{
	"openai": {
		AuthHeader:   "Authorization",
		AuthPrefix:   "Bearer ",
		ExtraHeaders: map[string]string{},
	},
	"anthropic": {
		AuthHeader: "x-api-key",
		AuthPrefix: "",
		ExtraHeaders: map[string]string{
			"anthropic-version": "2023-06-01",
		},
	},
	"gemini": {
		AuthHeader:   "Authorization",
		AuthPrefix:   "Bearer ",
		ExtraHeaders: map[string]string{},
	},
}

func (s *Service) handleConnectionHealthCheck(w http.ResponseWriter, r *http.Request) {
	connectionID, err := routeInt(r, "connection_id")
	if err != nil {
		writeError(w, r, s.allowedOrigins, http.StatusBadRequest, err.Error())
		return
	}
	value, err, _ := s.persistedHealthChecks.Do(fmt.Sprintf("connection:%d", connectionID), func() (any, error) {
		return s.runPersistedConnectionHealthCheck(r.Context(), r, connectionID)
	})
	if err != nil {
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}
	response, ok := value.(healthCheckResponse)
	if !ok {
		writeDomainError(w, r, s.allowedOrigins, fmt.Errorf("unexpected connection health result type"))
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) runPersistedConnectionHealthCheck(ctx context.Context, r *http.Request, connectionID int) (healthCheckResponse, error) {
	probeInput, err := pgxutil.InTxValue(ctx, s.pool, "connection", func(tx pgx.Tx) (connectionHealthProbeInput, error) {
		return s.loadPersistedConnectionHealthProbeInput(ctx, tx, r, connectionID)
	})
	if err != nil {
		return healthCheckResponse{}, err
	}
	if probeInput.WritebackExpectedUpdatedAt == nil {
		return healthCheckResponse{}, fmt.Errorf("persisted health check missing writeback token")
	}
	checkedAt := s.nowUTC()
	result, err := s.probeConnectionHealth(ctx, probeInput)
	if err != nil {
		return healthCheckResponse{}, err
	}
	_, err = pgxutil.InTxValue(ctx, s.pool, "connection", func(tx pgx.Tx) (bool, error) {
		return updateConnectionHealthCheckIfUnchanged(ctx, tx, probeInput.ConnectionID, *probeInput.WritebackExpectedUpdatedAt, result.HealthStatus, stringPtr(result.Detail), checkedAt)
	})
	if err != nil {
		return healthCheckResponse{}, err
	}
	return healthCheckResponse{
		ConnectionID:   probeInput.ConnectionID,
		HealthStatus:   result.HealthStatus,
		CheckedAt:      checkedAt,
		Detail:         result.Detail,
		ResponseTimeMS: result.ResponseTimeMS,
	}, nil
}

func (s *Service) handlePreviewConnectionHealthCheck(w http.ResponseWriter, r *http.Request) {
	modelConfigID, err := routeInt(r, "model_config_id")
	if err != nil {
		writeError(w, r, s.allowedOrigins, http.StatusBadRequest, err.Error())
		return
	}
	var requestBody connectionCreateRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		writeError(w, r, s.allowedOrigins, http.StatusBadRequest, "Invalid request body")
		return
	}

	if requestBody.Priority.Set {
		writeError(w, r, s.allowedOrigins, http.StatusUnprocessableEntity, "priority is not allowed on create")
		return
	}
	probeInput, err := pgxutil.InTxValue(r.Context(), s.pool, "connection", func(tx pgx.Tx) (connectionHealthProbeInput, error) {
		return s.loadPreviewConnectionHealthProbeInput(r.Context(), tx, r, modelConfigID, requestBody)
	})
	if err != nil {
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}
	result, err := s.probeConnectionHealth(r.Context(), probeInput)
	if err != nil {
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}
	writeJSON(w, http.StatusOK, connectionHealthCheckPreviewResponse{
		HealthStatus:   result.HealthStatus,
		CheckedAt:      s.nowUTC(),
		Detail:         result.Detail,
		ResponseTimeMS: result.ResponseTimeMS,
	})
}

func (s *Service) loadPersistedConnectionHealthProbeInput(ctx context.Context, tx pgx.Tx, r *http.Request, connectionID int) (connectionHealthProbeInput, error) {
	profile, err := resolveEffectiveProfile(ctx, tx, r)
	if err != nil {
		return connectionHealthProbeInput{}, err
	}
	current, found, err := loadConnectionRecord(ctx, tx, profile.ID, connectionID, true)
	if err != nil {
		return connectionHealthProbeInput{}, err
	}
	if !found {
		return connectionHealthProbeInput{}, &domainError{StatusCode: http.StatusNotFound, Detail: "Connection not found"}
	}
	endpoint, found, err := loadProfileEndpointRecord(ctx, tx, profile.ID, current.EndpointID)
	if err != nil {
		return connectionHealthProbeInput{}, err
	}
	if !found {
		return connectionHealthProbeInput{}, &domainError{StatusCode: http.StatusBadRequest, Detail: "Connection endpoint is missing"}
	}
	model, found, err := loadModelRecord(ctx, tx, profile.ID, current.ModelConfigID)
	if err != nil {
		return connectionHealthProbeInput{}, err
	}
	if !found {
		return connectionHealthProbeInput{}, &domainError{StatusCode: http.StatusBadRequest, Detail: "Connection model is missing"}
	}
	updatedAt := current.UpdatedAt
	return s.buildConnectionHealthProbeInput(ctx, tx, profile.ID, connectionHealthProbeReadModel{
		ConnectionID:               current.ID,
		AuthType:                   current.AuthType,
		CustomHeaders:              current.CustomHeaders,
		Endpoint:                   endpoint,
		APIFamily:                  model.APIFamily,
		ModelID:                    model.ModelID,
		OpenAIProbeEndpointVariant: current.OpenAIProbeEndpointVariant,
		WritebackExpectedUpdatedAt: &updatedAt,
	})
}

func (s *Service) loadPreviewConnectionHealthProbeInput(ctx context.Context, tx pgx.Tx, r *http.Request, modelConfigID int, requestBody connectionCreateRequest) (connectionHealthProbeInput, error) {
	profile, err := resolveEffectiveProfile(ctx, tx, r)
	if err != nil {
		return connectionHealthProbeInput{}, err
	}
	model, found, err := loadModelRecord(ctx, tx, profile.ID, modelConfigID)
	if err != nil {
		return connectionHealthProbeInput{}, err
	}
	if !found {
		return connectionHealthProbeInput{}, &domainError{StatusCode: http.StatusNotFound, Detail: "Model configuration not found"}
	}
	if requestBody.EndpointID != nil && requestBody.EndpointCreate != nil {
		return connectionHealthProbeInput{}, &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "Exactly one of endpoint_id or endpoint_create is required"}
	}
	if requestBody.EndpointID == nil && requestBody.EndpointCreate == nil {
		return connectionHealthProbeInput{}, &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "Exactly one of endpoint_id or endpoint_create is required"}
	}
	authType, err := validateAuthType(requestBody.AuthType)
	if err != nil {
		return connectionHealthProbeInput{}, err
	}
	if err := validateLimiter("qps_limit", requestBody.QPSLimit); err != nil {
		return connectionHealthProbeInput{}, err
	}
	if err := validateLimiter("max_in_flight_non_stream", requestBody.MaxInFlightNonStream); err != nil {
		return connectionHealthProbeInput{}, err
	}
	if err := validateLimiter("max_in_flight_stream", requestBody.MaxInFlightStream); err != nil {
		return connectionHealthProbeInput{}, err
	}
	openAIProbeVariant, err := resolveOpenAIProbeEndpointVariant(model.APIFamily, requestBody.OpenAIProbeEndpointVariant)
	if err != nil {
		return connectionHealthProbeInput{}, err
	}
	endpoint, err := s.resolvePreviewEndpoint(ctx, tx, profile.ID, requestBody.EndpointID, requestBody.EndpointCreate)
	if err != nil {
		return connectionHealthProbeInput{}, err
	}
	return s.buildConnectionHealthProbeInput(ctx, tx, profile.ID, connectionHealthProbeReadModel{
		AuthType:                   authType,
		CustomHeaders:              requestBody.CustomHeaders,
		Endpoint:                   endpoint,
		APIFamily:                  model.APIFamily,
		ModelID:                    model.ModelID,
		OpenAIProbeEndpointVariant: openAIProbeVariant,
	})
}

func (s *Service) buildConnectionHealthProbeInput(ctx context.Context, tx pgx.Tx, profileID int, readModel connectionHealthProbeReadModel) (connectionHealthProbeInput, error) {
	rules, err := listEnabledHeaderBlocklistRules(ctx, tx, profileID)
	if err != nil {
		return connectionHealthProbeInput{}, err
	}
	return connectionHealthProbeInput{
		ConnectionID:               readModel.ConnectionID,
		AuthType:                   readModel.AuthType,
		CustomHeaders:              normalizeHeaders(readModel.CustomHeaders),
		Endpoint:                   readModel.Endpoint,
		APIFamily:                  readModel.APIFamily,
		ModelID:                    readModel.ModelID,
		OpenAIProbeEndpointVariant: readModel.OpenAIProbeEndpointVariant,
		HeaderBlocklistRules:       rules,
		WritebackExpectedUpdatedAt: readModel.WritebackExpectedUpdatedAt,
	}, nil
}

func (s *Service) resolvePreviewEndpoint(ctx context.Context, tx pgx.Tx, profileID int, endpointID *int, inline *endpointCreateRequest) (endpointRecord, error) {
	if endpointID != nil {
		endpoint, found, err := loadProfileEndpointRecord(ctx, tx, profileID, *endpointID)
		if err != nil {
			return endpointRecord{}, err
		}
		if !found {
			return endpointRecord{}, &domainError{StatusCode: http.StatusNotFound, Detail: "Endpoint not found"}
		}
		return endpoint, nil
	}
	if inline != nil {
		endpointName := strings.TrimSpace(inline.Name)
		if endpointName == "" {
			return endpointRecord{}, &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "endpoint_create.name must not be empty"}
		}
		normalizedURL := endpointdomain.NormalizeBaseURL(inline.BaseURL)
		if warnings := endpointdomain.ValidateBaseURL(normalizedURL); len(warnings) > 0 {
			return endpointRecord{}, &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: strings.Join(warnings, "; ")}
		}

		encryptedAPIKey, err := endpointdomain.EncryptSecret(inline.APIKey, s.secretEncryptionKey, s.now)
		if err != nil {
			return endpointRecord{}, err
		}
		return endpointRecord{
			ID:        0,
			ProfileID: profileID,
			Name:      endpointName,
			BaseURL:   normalizedURL,
			APIKey:    encryptedAPIKey,
			Position:  0,
		}, nil
	}
	return endpointRecord{}, &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "Exactly one of endpoint_id or endpoint_create is required"}
}

func (s *Service) probeConnectionHealth(ctx context.Context, input connectionHealthProbeInput) (healthCheckProbeResult, error) {
	headers, err := s.buildHealthCheckHeaders(input.AuthType, input.APIFamily, input.Endpoint, input.CustomHeaders, input.HeaderBlocklistRules)
	if err != nil {
		return healthCheckProbeResult{}, err
	}
	openAIVariant := defaultOpenAIProbeEndpointVariant
	if input.OpenAIProbeEndpointVariant != nil && strings.TrimSpace(*input.OpenAIProbeEndpointVariant) != "" {
		openAIVariant = strings.TrimSpace(*input.OpenAIProbeEndpointVariant)
	}

	endpointPingPath, endpointPingBody, err := buildHealthCheckRequest(input.APIFamily, input.ModelID, openAIVariant)
	if err != nil {
		return healthCheckProbeResult{}, err
	}
	endpointPingURL, err := buildHealthCheckURL(input.Endpoint.BaseURL, endpointPingPath)
	if err != nil {
		return healthCheckProbeResult{}, err
	}
	endpointPingResult, err := s.executeHealthCheckRequest(ctx, endpointPingURL, headers, endpointPingBody)
	if err != nil {
		return healthCheckProbeResult{}, err
	}
	conversationResult := endpointPingResult
	if endpointPingResult.HealthStatus == "healthy" {
		conversationPath, conversationBody, err := buildHealthCheckRequest(input.APIFamily, input.ModelID, openAIVariant)
		if err != nil {
			return healthCheckProbeResult{}, err
		}
		conversationURL, err := buildHealthCheckURL(input.Endpoint.BaseURL, conversationPath)
		if err != nil {
			return healthCheckProbeResult{}, err
		}
		conversationResult, err = s.executeHealthCheckRequest(ctx, conversationURL, headers, conversationBody)
		if err != nil {
			return healthCheckProbeResult{}, err
		}
	}

	finalStatus := "healthy"
	if endpointPingResult.HealthStatus != "healthy" || conversationResult.HealthStatus != "healthy" {
		finalStatus = "unhealthy"
	}
	detail := conversationResult.Detail
	if endpointPingResult.HealthStatus != "healthy" {
		detail = endpointPingResult.Detail
	}
	responseTimeMS := conversationResult.ResponseTimeMS
	if responseTimeMS == 0 {
		responseTimeMS = endpointPingResult.ResponseTimeMS
	}
	return healthCheckProbeResult{
		HealthStatus:   finalStatus,
		Detail:         detail,
		ResponseTimeMS: responseTimeMS,
	}, nil
}

func (s *Service) buildHealthCheckHeaders(authType *string, apiFamily string, endpoint endpointRecord, customHeaders map[string]string, rules []headerBlocklistRuleRecord) (map[string]string, error) {
	config, err := resolveHealthCheckAuthConfig(authType, apiFamily)
	if err != nil {
		return nil, err
	}

	apiKey, err := endpointdomain.DecryptSecret(endpoint.APIKey, s.secretEncryptionKey)
	if err != nil {
		return nil, fmt.Errorf("resolve endpoint api key for health check: %w", err)
	}
	headers := map[string]string{
		config.AuthHeader: config.AuthPrefix + apiKey,
	}
	for key, value := range config.ExtraHeaders {
		headers[key] = value
	}
	protectedHeaders := map[string]struct{}{strings.ToLower(config.AuthHeader): {}}
	for key := range config.ExtraHeaders {
		protectedHeaders[strings.ToLower(key)] = struct{}{}
	}
	for key, value := range customHeaders {
		if _, protected := protectedHeaders[strings.ToLower(key)]; protected {
			continue
		}
		normalizedValue, ok := normalizeHeaderValue(value)
		if !ok {
			continue
		}
		headers[key] = normalizedValue
	}
	sanitized := make(map[string]string, len(headers))
	for key, value := range headers {
		if _, protected := protectedHeaders[strings.ToLower(key)]; protected || !headerIsBlocked(key, rules) {
			sanitized[key] = value
		}
	}

	return sanitized, nil
}

func (s *Service) executeHealthCheckRequest(ctx context.Context, upstreamURL string, headers map[string]string, body map[string]any) (healthCheckProbeResult, error) {
	rawBody, err := json.Marshal(body)
	if err != nil {
		return healthCheckProbeResult{}, fmt.Errorf("marshal health check request body: %w", err)
	}
	requestContext, cancel := context.WithTimeout(ctx, healthCheckRequestTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestContext, http.MethodPost, upstreamURL, bytes.NewReader(rawBody))
	if err != nil {
		return healthCheckProbeResult{}, fmt.Errorf("build health check request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	startedAt := time.Now()
	response, err := s.httpClient.Do(request)
	if err != nil {
		return mapHealthCheckTransportError(err), nil
	}
	defer func() { _ = response.Body.Close() }()
	responseTimeMS := int(time.Since(startedAt).Milliseconds())
	if responseTimeMS == 0 {
		responseTimeMS = 1
	}
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return healthCheckProbeResult{}, fmt.Errorf("read health check response body: %w", err)
	}

	healthStatus, detail := mapHealthCheckResponse(response.StatusCode, responseBody)
	return healthCheckProbeResult{
		HealthStatus:   healthStatus,
		Detail:         detail,
		ResponseTimeMS: responseTimeMS,
	}, nil
}

func buildHealthCheckRequest(apiFamily string, modelID string, openAIVariant string) (string, map[string]any, error) {
	switch apiFamily {
	case "openai":
		switch openAIVariant {
		case "chat_completions_minimal", "chat_completions_reasoning_none":
			body := map[string]any{
				"model":      modelID,
				"messages":   []map[string]any{{"role": "user", "content": "."}},
				"max_tokens": 1,
			}
			if openAIVariant == "chat_completions_reasoning_none" {
				body["reasoning_effort"] = "none"
			}
			return "/v1/chat/completions", body, nil
		default:
			body := map[string]any{
				"model":             modelID,
				"input":             []map[string]any{{"type": "message", "role": "user", "content": []map[string]any{{"type": "input_text", "text": "."}}}},
				"max_output_tokens": 1,
			}

			if openAIVariant == "responses_reasoning_none" {
				body["reasoning"] = map[string]any{"effort": "none"}
			}
			return "/v1/responses", body, nil
		}
	case "anthropic":
		return "/v1/messages", map[string]any{
			"model":      modelID,
			"max_tokens": 1,
			"messages":   []map[string]any{{"role": "user", "content": "."}},
		}, nil
	case "gemini":
		return fmt.Sprintf("/v1beta/models/%s:generateContent", modelID), map[string]any{
			"contents":         []map[string]any{{"role": "user", "parts": []map[string]any{{"text": "."}}}},
			"generationConfig": map[string]any{"maxOutputTokens": 1},
		}, nil
	default:
		return "", nil, fmt.Errorf("unsupported api_family %q for health check", apiFamily)
	}
}

func buildHealthCheckURL(baseURL string, requestPath string) (string, error) {
	parsedURL, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return "", fmt.Errorf("parse health check base URL: %w", err)
	}
	basePath := strings.TrimRight(parsedURL.Path, "/")
	finalPath := requestPath

	if !strings.HasPrefix(finalPath, "/") {
		finalPath = "/" + finalPath
	}
	parsedURL.Path = basePath + finalPath
	parsedURL.RawPath = parsedURL.EscapedPath()
	return parsedURL.String(), nil
}

func resolveHealthCheckAuthConfig(authType *string, apiFamily string) (apiFamilyAuthConfig, error) {
	resolvedKey := strings.ToLower(strings.TrimSpace(apiFamily))
	if authType != nil && strings.TrimSpace(*authType) != "" {
		resolvedKey = strings.ToLower(strings.TrimSpace(*authType))
	}
	config, ok := healthCheckAuthConfigs[resolvedKey]
	if !ok {
		return apiFamilyAuthConfig{}, fmt.Errorf("unsupported auth_type: %s", resolvedKey)
	}
	return config, nil
}

func normalizeHeaderValue(value string) (string, bool) {
	normalized := strings.TrimSpace(value)
	if normalized == "" {
		return "", false
	}
	for _, character := range normalized {
		if character < 0x20 || character == 0x7f {
			return "", false
		}
	}

	return normalized, true
}

func headerIsBlocked(name string, rules []headerBlocklistRuleRecord) bool {
	normalizedName := strings.ToLower(strings.TrimSpace(name))
	for _, rule := range rules {
		switch rule.MatchType {
		case "exact":
			if normalizedName == rule.Pattern {
				return true
			}
		case "prefix":
			if strings.HasPrefix(normalizedName, rule.Pattern) {
				return true
			}
		}
	}
	return false
}

func mapHealthCheckResponse(statusCode int, responseBody []byte) (string, string) {
	upstreamMessage := extractUpstreamErrorMessage(responseBody)
	if statusCode >= 200 && statusCode < 300 {
		return "healthy", "Connection successful"
	}
	if statusCode == http.StatusTooManyRequests {
		return "healthy", "Rate limited (connection works)"
	}
	if statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden {
		detail := fmt.Sprintf("Authentication failed (HTTP %d)", statusCode)
		if upstreamMessage != "" {
			detail += ": " + upstreamMessage
		}

		return "unhealthy", detail
	}
	detail := fmt.Sprintf("HTTP %d", statusCode)
	if upstreamMessage != "" {
		detail += ": " + upstreamMessage
	}
	return "unhealthy", detail
}

func extractUpstreamErrorMessage(responseBody []byte) string {
	if len(responseBody) == 0 {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal(responseBody, &payload); err != nil {
		return ""
	}
	errorValue, ok := payload["error"]
	if !ok {
		return ""
	}
	if errorMap, ok := errorValue.(map[string]any); ok {
		if message, ok := errorMap["message"].(string); ok {
			return message
		}
	}
	if message, ok := errorValue.(string); ok {
		return message
	}
	return ""
}

func mapHealthCheckTransportError(err error) healthCheckProbeResult {

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return healthCheckProbeResult{HealthStatus: "unhealthy", Detail: "Connection timed out"}
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return healthCheckProbeResult{HealthStatus: "unhealthy", Detail: fmt.Sprintf("Connection failed: %v", err)}
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) && strings.Contains(strings.ToLower(urlErr.Err.Error()), "connect") {
		return healthCheckProbeResult{HealthStatus: "unhealthy", Detail: fmt.Sprintf("Connection failed: %v", err)}
	}
	return healthCheckProbeResult{HealthStatus: "unhealthy", Detail: fmt.Sprintf("Error: %v", err)}
}
