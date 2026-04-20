package runtime

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/endpointdomain"
	"github.com/coachpo/prism/backend/internal/profiledomain"
)

var (
	geminiModelRE           = regexp.MustCompile(`^/v1beta/models/([^/:]+)`)
	geminiNativePathRE      = regexp.MustCompile(`^/v1beta/models/[^/:]+(?:[:/].*)?/?$`)
	anthropicMessagesPathRE = regexp.MustCompile(`^/v1/messages(?:/count_tokens)?/?$`)
)

var apiFamilyAuthConfigs = map[string]apiFamilyAuthConfig{
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

var defaultFailoverStatusCodes = []int{403, 422, 429, 500, 502, 503, 504, 529}

var hopByHopHeaders = map[string]struct{}{
	"connection":          {},
	"keep-alive":          {},
	"proxy-authenticate":  {},
	"proxy-authorization": {},
	"te":                  {},
	"trailers":            {},
	"transfer-encoding":   {},
	"upgrade":             {},
	"host":                {},
}

var clientAuthHeaders = map[string]struct{}{
	"authorization":  {},
	"x-api-key":      {},
	"x-goog-api-key": {},
}

type apiFamilyAuthConfig struct {
	AuthHeader   string
	AuthPrefix   string
	ExtraHeaders map[string]string
}

type runtimeModelRecord struct {
	ID                    int
	ProfileID             int
	APIFamily             string
	ModelID               string
	ModelType             string
	VendorID              *int
	VendorKey             *string
	VendorName            *string
	AuditEnabled          bool
	LoadbalanceStrategyID *int
}

type runtimeEndpoint struct {
	ID      int
	Name    *string
	BaseURL string
	APIKey  string
}

type runtimeConnection struct {
	ID            int
	ProfileID     int
	ModelConfigID int
	EndpointID    int
	Priority      int
	Name          *string
	AuthType      *string
	CustomHeaders map[string]any
	Endpoint      runtimeEndpoint
}

type runtimeStrategyRecord struct {
	ID                 int
	Name               string
	StrategyType       string
	LegacyStrategyType *string
	AutoRecoveryRaw    []byte
	RoutingPolicyRaw   []byte
}

type headerBlocklistRule struct {
	MatchType string
	Pattern   string
}

type requestPlan struct {
	RequestedModelID      string
	ResolvedTargetModelID *string
	RequestedVendorID     *int
	RequestedVendorKey    *string
	RequestedVendorName   *string
	ProfileID             int
	APIFamily             string
	AuditEnabledAtRequest bool
	EffectiveRequestPath  string
	RawRequestBody        []byte
	UpstreamBody          []byte
	Connections           []runtimeConnection
	BlocklistRules        []headerBlocklistRule
	ClientHeaders         map[string]string
	FailoverStatusCodes   []int
}

type executionResult struct {
	Response       *http.Response
	Connection     runtimeConnection
	RequestHeaders map[string]string
}

type autoRecoveryDocument struct {
	Mode        string `json:"mode"`
	StatusCodes []int  `json:"status_codes"`
}

type routingPolicyDocument struct {
	CircuitBreaker struct {
		FailureStatusCodes []int `json:"failure_status_codes"`
	} `json:"circuit_breaker"`
}

func (s *Service) buildRequestPlan(ctx context.Context, tx pgx.Tx, request *http.Request, rawBody []byte) (requestPlan, error) {
	requestedModelID, err := resolveModelID(rawBody, request.URL.Path)
	if err != nil {
		return requestPlan{}, &domainError{
			StatusCode: http.StatusBadRequest,
			Detail:     "Cannot determine model for routing. Include 'model' in the request body or use a Gemini-style model path.",
		}
	}

	activeProfile, err := profiledomain.ResolveActiveProfile(ctx, tx, s.nowUTC)
	if err != nil {
		return requestPlan{}, err
	}

	requestedModel, found, err := loadEnabledModelByModelID(ctx, tx, activeProfile.ID, requestedModelID)
	if err != nil {
		return requestPlan{}, err
	}
	if !found {
		return requestPlan{}, &domainError{StatusCode: http.StatusNotFound, Detail: fmt.Sprintf("Model '%s' not configured or disabled", requestedModelID)}
	}

	targetModel, connections, strategy, err := resolveExecutionTarget(ctx, tx, activeProfile.ID, requestedModel)
	if err != nil {
		return requestPlan{}, err
	}
	if err := validatePathCompatibility(targetModel.APIFamily, request.URL.Path); err != nil {
		return requestPlan{}, err
	}

	orderedConnections, err := orderConnections(ctx, tx, activeProfile.ID, targetModel.ID, strategy, connections, s.nowUTC())
	if err != nil {
		return requestPlan{}, err
	}
	if len(orderedConnections) == 0 {
		return requestPlan{}, &domainError{StatusCode: http.StatusServiceUnavailable, Detail: fmt.Sprintf("No active connections available for model '%s'.", requestedModelID)}
	}

	effectiveRequestPath := request.URL.Path
	if pathModelID := extractModelFromPath(request.URL.Path); pathModelID != "" && pathModelID != targetModel.ModelID {
		effectiveRequestPath = rewriteModelInPath(request.URL.Path, pathModelID, targetModel.ModelID)
	}

	upstreamBody := rawBody
	if bodyModelID := extractModelFromBody(rawBody); bodyModelID != "" && bodyModelID != targetModel.ModelID {
		upstreamBody = rewriteModelInBody(rawBody, targetModel.ModelID)
	}

	blocklistRules, err := listEnabledHeaderBlocklistRules(ctx, tx, activeProfile.ID)
	if err != nil {
		return requestPlan{}, err
	}

	return requestPlan{
		RequestedModelID:      requestedModelID,
		ResolvedTargetModelID: stringPointerIfNotEmpty(targetModel.ModelID),
		RequestedVendorID:     requestedModel.VendorID,
		RequestedVendorKey:    requestedModel.VendorKey,
		RequestedVendorName:   requestedModel.VendorName,
		ProfileID:             activeProfile.ID,
		APIFamily:             targetModel.APIFamily,
		AuditEnabledAtRequest: targetModel.AuditEnabled,
		EffectiveRequestPath:  effectiveRequestPath,
		RawRequestBody:        rawBody,
		UpstreamBody:          upstreamBody,
		Connections:           orderedConnections,
		BlocklistRules:        blocklistRules,
		ClientHeaders:         flattenHeaders(request.Header),
		FailoverStatusCodes:   resolveFailoverStatusCodes(strategy),
	}, nil
}

func resolveExecutionTarget(ctx context.Context, tx pgx.Tx, profileID int, requestedModel runtimeModelRecord) (runtimeModelRecord, []runtimeConnection, runtimeStrategyRecord, error) {
	if requestedModel.ModelType != "proxy" {
		return loadNativeExecutionTarget(ctx, tx, profileID, requestedModel.ModelID, requestedModel.ModelID)
	}

	targetModelIDs, err := listProxyTargetModelIDs(ctx, tx, requestedModel.ID)
	if err != nil {
		return runtimeModelRecord{}, nil, runtimeStrategyRecord{}, err
	}
	for _, targetModelID := range targetModelIDs {
		model, connections, strategy, loadErr := loadNativeExecutionTarget(ctx, tx, profileID, targetModelID, requestedModel.ModelID)
		if loadErr == nil {
			return model, connections, strategy, nil
		}
		var runtimeErr *domainError
		if errors.As(loadErr, &runtimeErr) && runtimeErr.StatusCode == http.StatusServiceUnavailable {
			continue
		}
		return runtimeModelRecord{}, nil, runtimeStrategyRecord{}, loadErr
	}
	return runtimeModelRecord{}, nil, runtimeStrategyRecord{}, &domainError{StatusCode: http.StatusServiceUnavailable, Detail: fmt.Sprintf("Proxy model '%s' has no routable targets.", requestedModel.ModelID)}
}

func loadNativeExecutionTarget(ctx context.Context, tx pgx.Tx, profileID int, modelID string, requestedModelID string) (runtimeModelRecord, []runtimeConnection, runtimeStrategyRecord, error) {
	model, found, err := loadEnabledModelByModelID(ctx, tx, profileID, modelID)
	if err != nil {
		return runtimeModelRecord{}, nil, runtimeStrategyRecord{}, err
	}
	if !found {
		return runtimeModelRecord{}, nil, runtimeStrategyRecord{}, &domainError{StatusCode: http.StatusServiceUnavailable, Detail: fmt.Sprintf("No active connections available for model '%s'.", requestedModelID)}
	}
	if model.LoadbalanceStrategyID == nil {
		return runtimeModelRecord{}, nil, runtimeStrategyRecord{}, fmt.Errorf("native model %q is missing loadbalance_strategy", model.ModelID)
	}

	strategy, found, err := loadRuntimeStrategy(ctx, tx, profileID, *model.LoadbalanceStrategyID)
	if err != nil {
		return runtimeModelRecord{}, nil, runtimeStrategyRecord{}, err
	}
	if !found {
		return runtimeModelRecord{}, nil, runtimeStrategyRecord{}, fmt.Errorf("loadbalance strategy %d not found for model %q", *model.LoadbalanceStrategyID, model.ModelID)
	}

	connections, err := listActiveConnectionsForModel(ctx, tx, profileID, model.ID)
	if err != nil {
		return runtimeModelRecord{}, nil, runtimeStrategyRecord{}, err
	}
	if len(connections) == 0 {
		return runtimeModelRecord{}, nil, runtimeStrategyRecord{}, &domainError{StatusCode: http.StatusServiceUnavailable, Detail: fmt.Sprintf("No active connections available for model '%s'.", requestedModelID)}
	}
	return model, connections, strategy, nil
}

func loadEnabledModelByModelID(ctx context.Context, tx pgx.Tx, profileID int, modelID string) (runtimeModelRecord, bool, error) {
	var strategyID sql.NullInt32
	var vendorID sql.NullInt32
	var vendorKey sql.NullString
	var vendorName sql.NullString
	record := runtimeModelRecord{}
	err := tx.QueryRow(
		ctx,
		`SELECT model_configs.id, model_configs.profile_id, model_configs.api_family, model_configs.model_id, model_configs.model_type, vendors.id, vendors.key, vendors.name, COALESCE(vendors.audit_enabled, FALSE), model_configs.loadbalance_strategy_id
		FROM model_configs
		LEFT JOIN vendors ON vendors.id = model_configs.vendor_id
		WHERE model_configs.profile_id = $1 AND model_configs.model_id = $2 AND model_configs.is_enabled = TRUE
		ORDER BY model_configs.id ASC
		LIMIT 1`,
		profileID,
		modelID,
	).Scan(&record.ID, &record.ProfileID, &record.APIFamily, &record.ModelID, &record.ModelType, &vendorID, &vendorKey, &vendorName, &record.AuditEnabled, &strategyID)
	if err == pgx.ErrNoRows {
		return runtimeModelRecord{}, false, nil
	}
	if err != nil {
		return runtimeModelRecord{}, false, fmt.Errorf("load enabled model %q for profile %d: %w", modelID, profileID, err)
	}
	if strategyID.Valid {
		resolved := int(strategyID.Int32)
		record.LoadbalanceStrategyID = &resolved
	}
	record.VendorID = nullableInt32(vendorID)
	record.VendorKey = nullableString(vendorKey)
	record.VendorName = nullableString(vendorName)
	return record, true, nil
}

func listProxyTargetModelIDs(ctx context.Context, tx pgx.Tx, sourceModelConfigID int) ([]string, error) {
	rows, err := tx.Query(
		ctx,
		`SELECT target_models.model_id
		FROM model_proxy_targets
		JOIN model_configs AS target_models ON target_models.id = model_proxy_targets.target_model_config_id
		WHERE model_proxy_targets.source_model_config_id = $1
		ORDER BY model_proxy_targets.position ASC, model_proxy_targets.id ASC`,
		sourceModelConfigID,
	)
	if err != nil {
		return nil, fmt.Errorf("query proxy targets for model %d: %w", sourceModelConfigID, err)
	}
	defer rows.Close()

	items := make([]string, 0)
	for rows.Next() {
		var modelID string
		if err := rows.Scan(&modelID); err != nil {
			return nil, fmt.Errorf("scan proxy target model id: %w", err)
		}
		items = append(items, modelID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate proxy targets for model %d: %w", sourceModelConfigID, err)
	}
	return items, nil
}

func loadRuntimeStrategy(ctx context.Context, tx pgx.Tx, profileID int, strategyID int) (runtimeStrategyRecord, bool, error) {
	var legacyStrategyType sql.NullString
	record := runtimeStrategyRecord{}
	err := tx.QueryRow(
		ctx,
		`SELECT id, name, strategy_type, legacy_strategy_type, auto_recovery, routing_policy
		FROM loadbalance_strategies
		WHERE profile_id = $1 AND id = $2
		LIMIT 1`,
		profileID,
		strategyID,
	).Scan(&record.ID, &record.Name, &record.StrategyType, &legacyStrategyType, &record.AutoRecoveryRaw, &record.RoutingPolicyRaw)
	if err == pgx.ErrNoRows {
		return runtimeStrategyRecord{}, false, nil
	}
	if err != nil {
		return runtimeStrategyRecord{}, false, fmt.Errorf("load runtime strategy %d for profile %d: %w", strategyID, profileID, err)
	}
	if legacyStrategyType.Valid {
		value := legacyStrategyType.String
		record.LegacyStrategyType = &value
	}
	return record, true, nil
}

func listActiveConnectionsForModel(ctx context.Context, tx pgx.Tx, profileID int, modelConfigID int) ([]runtimeConnection, error) {
	rows, err := tx.Query(
		ctx,
		`SELECT connections.id, connections.profile_id, connections.model_config_id, connections.endpoint_id,
			connections.priority, connections.name, connections.auth_type, connections.custom_headers,
			endpoints.id, endpoints.name, endpoints.base_url, endpoints.api_key
		FROM connections
		JOIN endpoints ON endpoints.id = connections.endpoint_id
		WHERE connections.profile_id = $1 AND connections.model_config_id = $2 AND connections.is_active = TRUE
		ORDER BY connections.priority ASC, connections.id ASC`,
		profileID,
		modelConfigID,
	)
	if err != nil {
		return nil, fmt.Errorf("query active connections for model %d: %w", modelConfigID, err)
	}
	defer rows.Close()

	items := make([]runtimeConnection, 0)
	for rows.Next() {
		var name sql.NullString
		var authType sql.NullString
		var customHeaders sql.NullString
		var endpointName sql.NullString
		item := runtimeConnection{}
		if err := rows.Scan(
			&item.ID,
			&item.ProfileID,
			&item.ModelConfigID,
			&item.EndpointID,
			&item.Priority,
			&name,
			&authType,
			&customHeaders,
			&item.Endpoint.ID,
			&endpointName,
			&item.Endpoint.BaseURL,
			&item.Endpoint.APIKey,
		); err != nil {
			return nil, fmt.Errorf("scan runtime connection: %w", err)
		}
		if name.Valid {
			value := name.String
			item.Name = &value
		}
		if authType.Valid {
			value := authType.String
			item.AuthType = &value
		}
		item.Endpoint.Name = nullableString(endpointName)
		item.CustomHeaders = parseCustomHeaders(customHeaders)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active connections for model %d: %w", modelConfigID, err)
	}
	return items, nil
}

func listEnabledHeaderBlocklistRules(ctx context.Context, tx pgx.Tx, profileID int) ([]headerBlocklistRule, error) {
	rows, err := tx.Query(
		ctx,
		`SELECT match_type, pattern
		FROM header_blocklist_rules
		WHERE enabled = TRUE AND (is_system = TRUE OR profile_id = $1)
		ORDER BY is_system DESC, id ASC`,
		profileID,
	)
	if err != nil {
		return nil, fmt.Errorf("query header blocklist rules for profile %d: %w", profileID, err)
	}
	defer rows.Close()

	items := make([]headerBlocklistRule, 0)
	for rows.Next() {
		var item headerBlocklistRule
		if err := rows.Scan(&item.MatchType, &item.Pattern); err != nil {
			return nil, fmt.Errorf("scan header blocklist rule: %w", err)
		}
		item.MatchType = strings.ToLower(strings.TrimSpace(item.MatchType))
		item.Pattern = strings.ToLower(strings.TrimSpace(item.Pattern))
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate header blocklist rules for profile %d: %w", profileID, err)
	}
	return items, nil
}

func orderConnections(ctx context.Context, tx pgx.Tx, profileID int, modelConfigID int, strategy runtimeStrategyRecord, connections []runtimeConnection, nowAt time.Time) ([]runtimeConnection, error) {
	ordered := append([]runtimeConnection(nil), connections...)
	sort.Slice(ordered, func(left int, right int) bool {
		if ordered[left].Priority != ordered[right].Priority {
			return ordered[left].Priority < ordered[right].Priority
		}
		return ordered[left].ID < ordered[right].ID
	})
	if !isRoundRobinStrategy(strategy) || len(ordered) < 2 {
		return ordered, nil
	}
	cursor, err := claimRoundRobinCursorPosition(ctx, tx, profileID, modelConfigID, len(ordered), nowAt)
	if err != nil {
		return nil, err
	}
	if cursor == 0 {
		return ordered, nil
	}
	return append(ordered[cursor:], ordered[:cursor]...), nil
}

func claimRoundRobinCursorPosition(ctx context.Context, tx pgx.Tx, profileID int, modelConfigID int, connectionCount int, nowAt time.Time) (int, error) {
	if connectionCount <= 0 {
		return 0, nil
	}
	if _, err := tx.Exec(
		ctx,
		`INSERT INTO loadbalance_round_robin_state (profile_id, model_config_id, next_cursor, created_at, updated_at)
		VALUES ($1, $2, 0, $3, $3)
		ON CONFLICT (profile_id, model_config_id) DO NOTHING`,
		profileID,
		modelConfigID,
		nowAt,
	); err != nil {
		return 0, fmt.Errorf("insert round-robin state for model %d: %w", modelConfigID, err)
	}
	var rowID int
	var nextCursor int
	if err := tx.QueryRow(
		ctx,
		`SELECT id, next_cursor
		FROM loadbalance_round_robin_state
		WHERE profile_id = $1 AND model_config_id = $2
		FOR UPDATE`,
		profileID,
		modelConfigID,
	).Scan(&rowID, &nextCursor); err != nil {
		return 0, fmt.Errorf("load round-robin state for model %d: %w", modelConfigID, err)
	}
	cursor := nextCursor % connectionCount
	if _, err := tx.Exec(
		ctx,
		`UPDATE loadbalance_round_robin_state SET next_cursor = $2, updated_at = $3 WHERE id = $1`,
		rowID,
		(cursor+1)%connectionCount,
		nowAt,
	); err != nil {
		return 0, fmt.Errorf("update round-robin state for model %d: %w", modelConfigID, err)
	}
	return cursor, nil
}

func isRoundRobinStrategy(strategy runtimeStrategyRecord) bool {
	if strings.ToLower(strings.TrimSpace(strategy.StrategyType)) != "legacy" {
		return false
	}
	if strategy.LegacyStrategyType == nil {
		return false
	}
	return strings.ToLower(strings.TrimSpace(*strategy.LegacyStrategyType)) == "round-robin"
}

func resolveFailoverStatusCodes(strategy runtimeStrategyRecord) []int {
	defaultCodes := append([]int(nil), defaultFailoverStatusCodes...)
	if strings.ToLower(strings.TrimSpace(strategy.StrategyType)) == "adaptive" {
		var policy routingPolicyDocument
		if len(strategy.RoutingPolicyRaw) == 0 || json.Unmarshal(strategy.RoutingPolicyRaw, &policy) != nil || len(policy.CircuitBreaker.FailureStatusCodes) == 0 {
			return defaultCodes
		}
		return append([]int(nil), policy.CircuitBreaker.FailureStatusCodes...)
	}

	var recovery autoRecoveryDocument
	if len(strategy.AutoRecoveryRaw) == 0 || json.Unmarshal(strategy.AutoRecoveryRaw, &recovery) != nil || len(recovery.StatusCodes) == 0 {
		return defaultCodes
	}
	return append([]int(nil), recovery.StatusCodes...)
}

func (s *Service) executeRequest(ctx context.Context, method string, plan requestPlan, requestQuery string) (executionResult, error) {
	attemptedAny := false
	lastError := ""
	for index, connection := range plan.Connections {
		attemptedAny = true
		headers, err := s.buildUpstreamHeaders(connection, plan.APIFamily, plan.ClientHeaders, plan.BlocklistRules)
		if err != nil {
			return executionResult{}, err
		}
		upstreamURL, err := buildUpstreamURL(connection.Endpoint.BaseURL, plan.EffectiveRequestPath, requestQuery)
		if err != nil {
			return executionResult{}, err
		}
		response, err := s.doUpstreamRequest(ctx, method, upstreamURL, headers, plan.UpstreamBody)
		if err != nil {
			lastError = err.Error()
			continue
		}
		if shouldFailover(response.StatusCode, plan.FailoverStatusCodes) && index < len(plan.Connections)-1 {
			lastError = fmt.Sprintf("Upstream returned %d", response.StatusCode)
			_ = response.Body.Close()
			continue
		}
		return executionResult{Response: response, Connection: connection, RequestHeaders: headers}, nil
	}

	if !attemptedAny {
		return executionResult{}, &domainError{StatusCode: http.StatusServiceUnavailable, Detail: fmt.Sprintf("No active connections available for model '%s'.", plan.RequestedModelID)}
	}
	if strings.TrimSpace(lastError) == "" {
		lastError = "Unknown upstream failure"
	}
	return executionResult{}, &domainError{StatusCode: http.StatusBadGateway, Detail: fmt.Sprintf("All connections failed for model '%s'. Last error: %s", plan.RequestedModelID, lastError)}
}

func (s *Service) doUpstreamRequest(ctx context.Context, method string, upstreamURL string, headers map[string]string, rawBody []byte) (*http.Response, error) {
	var bodyReader *bytes.Reader
	if rawBody == nil {
		bodyReader = bytes.NewReader(nil)
	} else {
		bodyReader = bytes.NewReader(rawBody)
	}
	request, err := http.NewRequestWithContext(ctx, method, upstreamURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("build upstream request: %w", err)
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	if _, ok := headers["User-Agent"]; !ok {
		if _, ok := headers["user-agent"]; !ok {
			request.Header["User-Agent"] = []string{""}
		}
	}
	return s.httpClient.Do(request)
}

func (s *Service) buildUpstreamHeaders(connection runtimeConnection, apiFamily string, clientHeaders map[string]string, rules []headerBlocklistRule) (map[string]string, error) {
	config, err := resolveAuthConfig(connection.AuthType, apiFamily)
	if err != nil {
		return nil, err
	}
	apiKey, err := endpointdomain.DecryptSecret(connection.Endpoint.APIKey, s.secretEncryptionKey)
	if err != nil {
		return nil, fmt.Errorf("resolve endpoint api key: %w", err)
	}
	proxyControlledHeaders := map[string]struct{}{strings.ToLower(config.AuthHeader): {}}
	for key := range config.ExtraHeaders {
		proxyControlledHeaders[strings.ToLower(key)] = struct{}{}
	}

	headers := map[string]string{}
	for key, value := range clientHeaders {
		keyLower := strings.ToLower(strings.TrimSpace(key))
		if _, blocked := hopByHopHeaders[keyLower]; blocked {
			continue
		}
		if _, blocked := clientAuthHeaders[keyLower]; blocked {
			continue
		}
		if keyLower == "content-length" || keyLower == "accept-encoding" {
			continue
		}
		if _, blocked := proxyControlledHeaders[keyLower]; blocked {
			continue
		}
		normalizedValue, ok := normalizeHeaderValue(value)
		if !ok {
			continue
		}
		headers[key] = normalizedValue
	}
	headers = sanitizeHeaders(headers, rules)
	headers[config.AuthHeader] = config.AuthPrefix + apiKey
	for key, value := range config.ExtraHeaders {
		headers[key] = value
	}
	for key, rawValue := range connection.CustomHeaders {
		if _, protected := proxyControlledHeaders[strings.ToLower(strings.TrimSpace(key))]; protected {
			continue
		}
		normalizedValue, ok := normalizeHeaderValue(fmt.Sprint(rawValue))
		if !ok {
			continue
		}
		headers[key] = normalizedValue
	}

	sanitized := make(map[string]string, len(headers))
	for key, value := range headers {
		keyLower := strings.ToLower(strings.TrimSpace(key))
		if _, protected := proxyControlledHeaders[keyLower]; protected || !headerIsBlocked(key, rules) {
			sanitized[key] = value
		}
	}
	return sanitized, nil
}

func resolveAuthConfig(authType *string, apiFamily string) (apiFamilyAuthConfig, error) {
	resolvedKey := strings.ToLower(strings.TrimSpace(apiFamily))
	if authType != nil && strings.TrimSpace(*authType) != "" {
		resolvedKey = strings.ToLower(strings.TrimSpace(*authType))
	}
	config, ok := apiFamilyAuthConfigs[resolvedKey]
	if !ok {
		return apiFamilyAuthConfig{}, fmt.Errorf("unsupported auth_type: %s", resolvedKey)
	}
	return config, nil
}

func buildUpstreamURL(baseURL string, requestPath string, requestQuery string) (string, error) {
	parsedURL, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return "", fmt.Errorf("parse upstream base URL: %w", err)
	}
	basePath := strings.TrimRight(parsedURL.Path, "/")
	finalPath := requestPath
	if !strings.HasPrefix(finalPath, "/") {
		finalPath = "/" + finalPath
	}
	parsedURL.Path = basePath + finalPath
	parsedURL.RawPath = parsedURL.EscapedPath()
	if requestQuery != "" {
		if parsedURL.RawQuery != "" {
			parsedURL.RawQuery = parsedURL.RawQuery + "&" + requestQuery
		} else {
			parsedURL.RawQuery = requestQuery
		}
	}
	return parsedURL.String(), nil
}

func flattenHeaders(header http.Header) map[string]string {
	flattened := make(map[string]string, len(header))
	for key, values := range header {
		flattened[key] = strings.Join(values, ", ")
	}
	return flattened
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

func sanitizeHeaders(headers map[string]string, rules []headerBlocklistRule) map[string]string {
	sanitized := make(map[string]string, len(headers))
	for key, value := range headers {
		if headerIsBlocked(key, rules) {
			continue
		}
		sanitized[key] = value
	}
	return sanitized
}

func headerIsBlocked(name string, rules []headerBlocklistRule) bool {
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

func parseCustomHeaders(value sql.NullString) map[string]any {
	if !value.Valid || strings.TrimSpace(value.String) == "" {
		return map[string]any{}
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(value.String), &parsed); err != nil {
		return map[string]any{}
	}
	if parsed == nil {
		return map[string]any{}
	}
	return parsed
}

func resolveModelID(rawBody []byte, requestPath string) (string, error) {
	if modelID := extractModelFromBody(rawBody); modelID != "" {
		return modelID, nil
	}
	if modelID := extractModelFromPath(requestPath); modelID != "" {
		return modelID, nil
	}
	return "", fmt.Errorf("model is required")
}

func extractModelFromBody(rawBody []byte) string {
	if len(rawBody) == 0 {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal(rawBody, &payload); err != nil {
		return ""
	}
	modelID, _ := payload["model"].(string)
	return strings.TrimSpace(modelID)
}

func rewriteModelInBody(rawBody []byte, targetModelID string) []byte {
	if len(rawBody) == 0 {
		return rawBody
	}
	var payload map[string]any
	if err := json.Unmarshal(rawBody, &payload); err != nil {
		return rawBody
	}
	payload["model"] = targetModelID
	rewritten, err := json.Marshal(payload)
	if err != nil {
		return rawBody
	}
	return rewritten
}

func extractModelFromPath(requestPath string) string {
	matches := geminiModelRE.FindStringSubmatch(requestPath)
	if len(matches) < 2 {
		return ""
	}
	return strings.TrimSpace(matches[1])
}

func rewriteModelInPath(requestPath string, originalModel string, targetModel string) string {
	if originalModel == targetModel {
		return requestPath
	}
	return strings.Replace(requestPath, "/models/"+originalModel, "/models/"+targetModel, 1)
}

func validatePathCompatibility(apiFamily string, requestPath string) error {
	pathFamily := "generic"
	switch {
	case geminiNativePathRE.MatchString(requestPath):
		pathFamily = "gemini_native"
	case anthropicMessagesPathRE.MatchString(requestPath):
		pathFamily = "anthropic_messages"
	}
	allowedFamilies := map[string]map[string]struct{}{
		"openai":    {"generic": {}},
		"anthropic": {"anthropic_messages": {}},
		"gemini":    {"gemini_native": {}},
	}
	allowed, ok := allowedFamilies[strings.ToLower(strings.TrimSpace(apiFamily))]
	if !ok {
		return nil
	}
	if _, ok := allowed[pathFamily]; ok {
		return nil
	}
	return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Path '%s' is incompatible with api_family '%s'. Use an api-family-native path.", requestPath, apiFamily)}
}

func shouldFailover(statusCode int, failoverStatusCodes []int) bool {
	for _, candidate := range failoverStatusCodes {
		if statusCode == candidate {
			return true
		}
	}
	return false
}

func copyResponseHeaders(target http.Header, source http.Header) {
	for key, values := range filterResponseHeaders(source) {
		for _, value := range values {
			target.Add(key, value)
		}
	}
}

func filterResponseHeaders(source http.Header) http.Header {
	filtered := make(http.Header)
	for key, values := range source {
		keyLower := strings.ToLower(strings.TrimSpace(key))
		if _, blocked := hopByHopHeaders[keyLower]; blocked {
			continue
		}
		for _, value := range values {
			filtered.Add(key, value)
		}
	}
	return filtered
}

func stringPointerIfNotEmpty(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func nullableInt32(value sql.NullInt32) *int {
	if !value.Valid {
		return nil
	}
	resolved := int(value.Int32)
	return &resolved
}

func nullableString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	resolved := value.String
	return &resolved
}
