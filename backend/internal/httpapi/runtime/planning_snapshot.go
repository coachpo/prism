package runtime

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/domain/loadbalance"
	"github.com/coachpo/prism/backend/internal/endpointdomain"
)

type planningSnapshot struct {
	ModelsByID                   map[string]runtimeModelRecord
	AccessTargetsBySourceModelID map[int][]runtimeAccessTargetRecord
	ConnectionsByID              map[int]runtimeConnection
	StrategiesByModelID          map[int]loadbalance.RuntimeStrategy
	BlocklistRules               []headerBlocklistRule
	ReportCurrency               runtimeReportCurrencySnapshot
}

type runtimeAccessTargetRecord struct {
	ID                        int
	ProfileID                 int
	SourceModelConfigID       int
	TargetType                string
	TargetModelConfigID       *int
	TargetModelID             string
	TargetModelProfileID      int
	TargetModelAPIFamily      string
	TargetModelEnabled        bool
	TargetConnectionID        *int
	TargetConnectionProfileID int
	TargetConnectionAPIFamily string
	Position                  int
	IsEnabled                 bool
	ConnectionEndpointFX      *runtimeEndpointFXSnapshot
}

func buildPlanningSnapshot(ctx context.Context, tx pgx.Tx, profileID int, secretEncryptionKey string) (*planningSnapshot, error) {
	modelsByID, err := listEnabledModelsForProfile(ctx, tx, profileID)
	if err != nil {
		return nil, err
	}
	accessTargetsBySourceModelID, err := listAccessTargetsForProfile(ctx, tx, profileID)
	if err != nil {
		return nil, err
	}
	strategiesByID, err := listRuntimeStrategiesForProfile(ctx, tx, profileID)
	if err != nil {
		return nil, err
	}
	connectionsByID, err := listActiveConnectionsForProfile(ctx, tx, profileID)
	if err != nil {
		return nil, err
	}
	blocklistRules, err := listEnabledHeaderBlocklistRules(ctx, tx, profileID)
	if err != nil {
		return nil, err
	}
	reportCurrency, err := loadRuntimeReportCurrencySnapshot(ctx, tx, profileID)
	if err != nil {
		return nil, err
	}

	for connectionID, connection := range connectionsByID {
		compiled, err := compileRuntimeConnection(connection, connection.APIFamily, secretEncryptionKey)
		if err != nil {
			return nil, fmt.Errorf("compile runtime connection %d for profile %d: %w", connectionID, profileID, err)
		}
		connectionsByID[connectionID] = compiled
	}

	strategiesByModelID := make(map[int]loadbalance.RuntimeStrategy, len(modelsByID))
	for _, model := range modelsByID {
		if model.LoadbalanceStrategyID == nil {
			continue
		}
		if strategy, ok := strategiesByID[*model.LoadbalanceStrategyID]; ok {
			strategiesByModelID[model.ID] = strategy
		}
	}

	return &planningSnapshot{
		ModelsByID:                   modelsByID,
		AccessTargetsBySourceModelID: accessTargetsBySourceModelID,
		ConnectionsByID:              connectionsByID,
		StrategiesByModelID:          strategiesByModelID,
		BlocklistRules:               blocklistRules,
		ReportCurrency:               reportCurrency,
	}, nil
}

func compileRuntimeConnections(source []runtimeConnection, apiFamily string, secretEncryptionKey string) ([]runtimeConnection, error) {
	if len(source) == 0 {
		return nil, nil
	}
	target := make([]runtimeConnection, 0, len(source))
	for _, connection := range source {
		compiled, err := compileRuntimeConnection(connection, apiFamily, secretEncryptionKey)
		if err != nil {
			return nil, err
		}
		target = append(target, compiled)
	}
	return target, nil
}

func compileRuntimeConnection(connection runtimeConnection, apiFamily string, secretEncryptionKey string) (runtimeConnection, error) {
	compiled := connection
	config, err := resolveAuthConfig(connection.AuthType, apiFamily)
	if err != nil {
		return runtimeConnection{}, err
	}
	if strings.TrimSpace(secretEncryptionKey) == "" {
		return compiled, nil
	}
	apiKey, err := endpointdomain.DecryptSecret(connection.EncryptedEndpointAPIKey, secretEncryptionKey)
	if err != nil {
		return runtimeConnection{}, fmt.Errorf("resolve endpoint api key for connection %d: %w", connection.ID, err)
	}
	controlledHeaderNames := map[string]struct{}{strings.ToLower(config.AuthHeader): {}}
	extraHeaders := make(map[string]string, len(config.ExtraHeaders))
	for key, value := range config.ExtraHeaders {
		extraHeaders[key] = value
		controlledHeaderNames[strings.ToLower(key)] = struct{}{}
	}
	compiled.UpstreamAuth = &runtimeConnectionUpstreamAuthSnapshot{
		AuthHeader:            config.AuthHeader,
		AuthValue:             config.AuthPrefix + apiKey,
		ExtraHeaders:          extraHeaders,
		ControlledHeaderNames: controlledHeaderNames,
	}
	compiled.EncryptedEndpointAPIKey = ""
	return compiled, nil
}

func listPublishedPlanningProfileIDs(ctx context.Context, tx pgx.Tx) ([]int, error) {
	rows, err := tx.Query(
		ctx,
		`SELECT id FROM profiles WHERE deleted_at IS NULL ORDER BY id ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("query published planning profile ids: %w", err)
	}
	defer rows.Close()

	profileIDs := make([]int, 0)
	for rows.Next() {
		var profileID int
		if err := rows.Scan(&profileID); err != nil {
			return nil, fmt.Errorf("scan published planning profile id: %w", err)
		}
		profileIDs = append(profileIDs, profileID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate published planning profile ids: %w", err)
	}
	return profileIDs, nil
}

const runtimeAccessResolverMaxDepth = 32

type runtimeAccessResolutionContext struct {
	RequestedModelID   string
	RequestedAPIFamily string
	VisitedModelIDs    map[int]struct{}
	Depth              int
	ReferenceNow       time.Time
}

type runtimeResolvedAccessPlan struct {
	TargetModel      runtimeModelRecord
	Connections      []runtimeConnection
	TerminalAttempts []runtimeTerminalAttempt
	RuntimeStates    map[int]loadbalance.RuntimeConnectionState
	Strategy         loadbalance.RuntimeStrategy
}

type noEligibleTargetsError struct {
	requestedModelID string
}

func (err *noEligibleTargetsError) Error() string {
	return fmt.Sprintf("No eligible targets available for model '%s'.", err.requestedModelID)
}

func (s *Service) resolveExecutionTargetFromSnapshot(profileID int, snapshot *planningSnapshot, requestedModel runtimeModelRecord, referenceNow time.Time) (runtimeResolvedAccessPlan, error) {
	ctx := runtimeAccessResolutionContext{
		RequestedModelID:   requestedModel.ModelID,
		RequestedAPIFamily: requestedModel.APIFamily,
		VisitedModelIDs:    map[int]struct{}{},
		ReferenceNow:       referenceNow,
	}
	resolved, err := s.resolveModelAccessFromSnapshot(profileID, snapshot, requestedModel, ctx)
	if err != nil {
		var noEligible *noEligibleTargetsError
		if errors.As(err, &noEligible) {
			return runtimeResolvedAccessPlan{}, &domainError{StatusCode: http.StatusServiceUnavailable, Detail: noEligible.Error()}
		}
		return runtimeResolvedAccessPlan{}, err
	}
	return resolved, nil
}

func (s *Service) resolveModelAccessFromSnapshot(profileID int, snapshot *planningSnapshot, model runtimeModelRecord, ctx runtimeAccessResolutionContext) (runtimeResolvedAccessPlan, error) {
	if ctx.Depth > runtimeAccessResolverMaxDepth {
		return runtimeResolvedAccessPlan{}, &domainError{StatusCode: http.StatusServiceUnavailable, Detail: fmt.Sprintf("Model access graph exceeded maximum depth of %d while resolving model '%s'.", runtimeAccessResolverMaxDepth, ctx.RequestedModelID)}
	}
	if _, seen := ctx.VisitedModelIDs[model.ID]; seen {
		return runtimeResolvedAccessPlan{}, &domainError{StatusCode: http.StatusServiceUnavailable, Detail: fmt.Sprintf("Model access cycle detected while resolving model '%s'.", ctx.RequestedModelID)}
	}
	strategy, ok := snapshot.StrategiesByModelID[model.ID]
	if !ok {
		return runtimeResolvedAccessPlan{}, fmt.Errorf("model %q is missing loadbalance_strategy", model.ModelID)
	}

	visited := cloneVisitedModelIDs(ctx.VisitedModelIDs)
	visited[model.ID] = struct{}{}
	childContext := ctx
	childContext.VisitedModelIDs = visited
	childContext.Depth++

	orderedTargets := orderRuntimeAccessTargets(profileID, model.ID, strategy, snapshot.AccessTargetsBySourceModelID[model.ID], s.runtimeState)
	resolved := runtimeResolvedAccessPlan{RuntimeStates: map[int]loadbalance.RuntimeConnectionState{}, Strategy: strategy}
	for _, target := range orderedTargets {
		candidate, eligible, err := s.resolveAccessTargetFromSnapshot(profileID, snapshot, model, strategy, target, childContext)
		if err != nil {
			return runtimeResolvedAccessPlan{}, err
		}
		if !eligible {
			continue
		}
		if len(resolved.TerminalAttempts) == 0 {
			resolved.TargetModel = candidate.TargetModel
			resolved.Strategy = candidate.Strategy
		}
		resolved.Connections = append(resolved.Connections, candidate.Connections...)
		resolved.TerminalAttempts = append(resolved.TerminalAttempts, candidate.TerminalAttempts...)
		for connectionID, state := range candidate.RuntimeStates {
			resolved.RuntimeStates[connectionID] = state
		}
	}
	if len(resolved.TerminalAttempts) == 0 || len(resolved.Connections) == 0 {
		return runtimeResolvedAccessPlan{}, &noEligibleTargetsError{requestedModelID: ctx.RequestedModelID}
	}
	return resolved, nil
}

func (s *Service) resolveAccessTargetFromSnapshot(profileID int, snapshot *planningSnapshot, sourceModel runtimeModelRecord, strategy loadbalance.RuntimeStrategy, target runtimeAccessTargetRecord, ctx runtimeAccessResolutionContext) (runtimeResolvedAccessPlan, bool, error) {
	if !target.IsEnabled || target.ProfileID != profileID || target.SourceModelConfigID != sourceModel.ID {
		return runtimeResolvedAccessPlan{}, false, nil
	}
	switch target.TargetType {
	case runtimeAccessTargetTypeConnection:
		return s.resolveConnectionAccessTargetFromSnapshot(profileID, snapshot, sourceModel, strategy, target, ctx.ReferenceNow)
	case runtimeAccessTargetTypeModel:
		return s.resolveModelAccessTargetFromSnapshot(profileID, snapshot, sourceModel, target, ctx)
	default:
		return runtimeResolvedAccessPlan{}, false, nil
	}
}

func (s *Service) resolveConnectionAccessTargetFromSnapshot(profileID int, snapshot *planningSnapshot, sourceModel runtimeModelRecord, strategy loadbalance.RuntimeStrategy, target runtimeAccessTargetRecord, referenceNow time.Time) (runtimeResolvedAccessPlan, bool, error) {
	if target.TargetConnectionID == nil {
		return runtimeResolvedAccessPlan{}, false, nil
	}
	connection, ok := snapshot.ConnectionsByID[*target.TargetConnectionID]
	if !ok {
		return runtimeResolvedAccessPlan{}, false, nil
	}
	if connection.ProfileID != sourceModel.ProfileID || !sameRuntimeAPIFamily(connection.APIFamily, sourceModel.APIFamily) {
		return runtimeResolvedAccessPlan{}, false, nil
	}
	if target.TargetConnectionProfileID != 0 && target.TargetConnectionProfileID != sourceModel.ProfileID {
		return runtimeResolvedAccessPlan{}, false, nil
	}
	if strings.TrimSpace(target.TargetConnectionAPIFamily) != "" && !sameRuntimeAPIFamily(target.TargetConnectionAPIFamily, sourceModel.APIFamily) {
		return runtimeResolvedAccessPlan{}, false, nil
	}

	resolvedConnection := connection
	resolvedConnection.ModelConfigID = sourceModel.ID
	resolvedConnection.Priority = target.Position
	if target.ConnectionEndpointFX != nil {
		fx := *target.ConnectionEndpointFX
		resolvedConnection.EndpointFXSnapshot = &fx
	}

	runtimeStates := s.runtimeState.SnapshotConnectionStates(profileID, runtimeConnectionRefs([]runtimeConnection{resolvedConnection}))
	eligibleConnectionIDs := loadbalance.FilterEligibleConnectionIDs(toConnectionOrderCandidates([]runtimeConnection{resolvedConnection}), runtimeStates, referenceNow)
	if len(eligibleConnectionIDs) == 0 {
		return runtimeResolvedAccessPlan{}, false, nil
	}
	eligibleRuntimeStates := make(map[int]loadbalance.RuntimeConnectionState, len(eligibleConnectionIDs))
	for _, connectionID := range eligibleConnectionIDs {
		if state, ok := runtimeStates[connectionID]; ok {
			eligibleRuntimeStates[connectionID] = state
		}
	}
	return runtimeResolvedAccessPlan{
		TargetModel: sourceModel,
		Connections: []runtimeConnection{resolvedConnection},
		TerminalAttempts: []runtimeTerminalAttempt{{
			TargetModel:               sourceModel,
			Connection:                resolvedConnection,
			Strategy:                  strategy,
			AuditEnabledAtRequest:     sourceModel.AuditEnabled,
			AuditCaptureBodiesRequest: sourceModel.AuditEnabled && sourceModel.AuditCaptureBodies,
		}},
		RuntimeStates: eligibleRuntimeStates,
		Strategy:      strategy,
	}, true, nil
}

func (s *Service) resolveModelAccessTargetFromSnapshot(profileID int, snapshot *planningSnapshot, sourceModel runtimeModelRecord, target runtimeAccessTargetRecord, ctx runtimeAccessResolutionContext) (runtimeResolvedAccessPlan, bool, error) {
	if target.TargetModelConfigID == nil || !target.TargetModelEnabled {
		return runtimeResolvedAccessPlan{}, false, nil
	}
	if target.TargetModelProfileID != sourceModel.ProfileID || !sameRuntimeAPIFamily(target.TargetModelAPIFamily, sourceModel.APIFamily) {
		return runtimeResolvedAccessPlan{}, false, nil
	}
	childModel, ok := snapshot.ModelsByID[target.TargetModelID]
	if !ok || childModel.ID != *target.TargetModelConfigID {
		return runtimeResolvedAccessPlan{}, false, nil
	}
	if childModel.ProfileID != sourceModel.ProfileID || !sameRuntimeAPIFamily(childModel.APIFamily, sourceModel.APIFamily) {
		return runtimeResolvedAccessPlan{}, false, nil
	}
	resolved, err := s.resolveModelAccessFromSnapshot(profileID, snapshot, childModel, ctx)
	if err != nil {
		var noEligible *noEligibleTargetsError
		if errors.As(err, &noEligible) {
			return runtimeResolvedAccessPlan{}, false, nil
		}
		return runtimeResolvedAccessPlan{}, false, err
	}
	return resolved, true, nil
}

func cloneVisitedModelIDs(source map[int]struct{}) map[int]struct{} {
	cloned := make(map[int]struct{}, len(source)+1)
	for modelID := range source {
		cloned[modelID] = struct{}{}
	}
	return cloned
}

func sameRuntimeAPIFamily(left string, right string) bool {
	return strings.EqualFold(strings.TrimSpace(left), strings.TrimSpace(right)) && strings.TrimSpace(left) != "" && strings.TrimSpace(right) != ""
}

func listEnabledModelsForProfile(ctx context.Context, tx pgx.Tx, profileID int) (map[string]runtimeModelRecord, error) {
	rows, err := tx.Query(
		ctx,
		`SELECT model_configs.id, model_configs.profile_id, model_configs.api_family, model_configs.model_id,
			vendors.id, vendors.key, vendors.name,
			COALESCE(vendors.audit_enabled, FALSE), COALESCE(vendors.audit_capture_bodies, FALSE), model_configs.loadbalance_strategy_id
		FROM model_configs
		LEFT JOIN vendors ON vendors.id = model_configs.vendor_id
		WHERE model_configs.profile_id = $1 AND model_configs.is_enabled = TRUE
		ORDER BY model_configs.model_id ASC, model_configs.id ASC`,
		profileID,
	)
	if err != nil {
		return nil, fmt.Errorf("query enabled models for profile %d: %w", profileID, err)
	}
	defer rows.Close()

	items := make(map[string]runtimeModelRecord)
	for rows.Next() {
		var strategyID sql.NullInt32
		var vendorID sql.NullInt32
		var vendorKey sql.NullString
		var vendorName sql.NullString
		item := runtimeModelRecord{}
		if err := rows.Scan(&item.ID, &item.ProfileID, &item.APIFamily, &item.ModelID, &vendorID, &vendorKey, &vendorName, &item.AuditEnabled, &item.AuditCaptureBodies, &strategyID); err != nil {
			return nil, fmt.Errorf("scan enabled model for profile %d: %w", profileID, err)
		}
		if _, exists := items[item.ModelID]; exists {
			continue
		}
		if strategyID.Valid {
			resolved := int(strategyID.Int32)
			item.LoadbalanceStrategyID = &resolved
		}
		item.VendorID = nullableInt32(vendorID)
		item.VendorKey = nullableString(vendorKey)
		item.VendorName = nullableString(vendorName)
		items[item.ModelID] = item
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate enabled models for profile %d: %w", profileID, err)
	}
	return items, nil
}

func listAccessTargetsForProfile(ctx context.Context, tx pgx.Tx, profileID int) (map[int][]runtimeAccessTargetRecord, error) {
	rows, err := tx.Query(
		ctx,
		`SELECT model_access_targets.id, model_access_targets.profile_id, model_access_targets.source_model_config_id,
			model_access_targets.target_type, model_access_targets.target_model_config_id, target_models.model_id,
			target_models.profile_id, target_models.api_family, COALESCE(target_models.is_enabled, FALSE),
			model_access_targets.target_connection_id, connections.profile_id, connections.api_family,
			model_access_targets.position, model_access_targets.is_enabled,
			source_models.model_id, connections.endpoint_id, endpoint_fx_rate_settings.fx_rate::text
		FROM model_access_targets
		JOIN model_configs AS source_models ON source_models.id = model_access_targets.source_model_config_id
		LEFT JOIN model_configs AS target_models ON target_models.id = model_access_targets.target_model_config_id
		LEFT JOIN connections ON connections.id = model_access_targets.target_connection_id
		LEFT JOIN endpoint_fx_rate_settings ON endpoint_fx_rate_settings.profile_id = model_access_targets.profile_id
			AND endpoint_fx_rate_settings.model_id = source_models.model_id
			AND endpoint_fx_rate_settings.endpoint_id = connections.endpoint_id
		WHERE model_access_targets.profile_id = $1
		ORDER BY model_access_targets.source_model_config_id ASC, model_access_targets.position ASC, model_access_targets.id ASC`,
		profileID,
	)
	if err != nil {
		return nil, fmt.Errorf("query access targets for profile %d: %w", profileID, err)
	}
	defer rows.Close()

	items := make(map[int][]runtimeAccessTargetRecord)
	for rows.Next() {
		var targetModelConfigID sql.NullInt32
		var targetModelID sql.NullString
		var targetModelProfileID sql.NullInt32
		var targetModelAPIFamily sql.NullString
		var targetModelEnabled sql.NullBool
		var targetConnectionID sql.NullInt32
		var targetConnectionProfileID sql.NullInt32
		var targetConnectionAPIFamily sql.NullString
		var sourceModelID sql.NullString
		var connectionEndpointID sql.NullInt32
		var endpointFXRate sql.NullString
		item := runtimeAccessTargetRecord{}
		if err := rows.Scan(
			&item.ID,
			&item.ProfileID,
			&item.SourceModelConfigID,
			&item.TargetType,
			&targetModelConfigID,
			&targetModelID,
			&targetModelProfileID,
			&targetModelAPIFamily,
			&targetModelEnabled,
			&targetConnectionID,
			&targetConnectionProfileID,
			&targetConnectionAPIFamily,
			&item.Position,
			&item.IsEnabled,
			&sourceModelID,
			&connectionEndpointID,
			&endpointFXRate,
		); err != nil {
			return nil, fmt.Errorf("scan access target for profile %d: %w", profileID, err)
		}
		item.TargetModelConfigID = nullableInt32(targetModelConfigID)
		item.TargetModelID = strings.TrimSpace(targetModelID.String)
		if targetModelProfileID.Valid {
			item.TargetModelProfileID = int(targetModelProfileID.Int32)
		}
		if targetModelAPIFamily.Valid {
			item.TargetModelAPIFamily = strings.TrimSpace(targetModelAPIFamily.String)
		}
		item.TargetModelEnabled = targetModelEnabled.Valid && targetModelEnabled.Bool
		item.TargetConnectionID = nullableInt32(targetConnectionID)
		if targetConnectionProfileID.Valid {
			item.TargetConnectionProfileID = int(targetConnectionProfileID.Int32)
		}
		if targetConnectionAPIFamily.Valid {
			item.TargetConnectionAPIFamily = strings.TrimSpace(targetConnectionAPIFamily.String)
		}
		if endpointFXRate.Valid && connectionEndpointID.Valid && sourceModelID.Valid {
			item.ConnectionEndpointFX = &runtimeEndpointFXSnapshot{
				ModelID:    strings.TrimSpace(sourceModelID.String),
				EndpointID: int(connectionEndpointID.Int32),
				FXRate:     strings.TrimSpace(endpointFXRate.String),
			}
		}
		items[item.SourceModelConfigID] = append(items[item.SourceModelConfigID], item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate access targets for profile %d: %w", profileID, err)
	}
	return items, nil
}

func listRuntimeStrategiesForProfile(ctx context.Context, tx pgx.Tx, profileID int) (map[int]loadbalance.RuntimeStrategy, error) {
	rows, err := tx.Query(
		ctx,
		`SELECT id, name, legacy_strategy_type, failure_status_codes, ban_mode,
			retry_base_delay_ms, retry_backoff_multiplier, retry_jitter_ratio,
			retry_max_delay_ms, cycle_retry_attempt_limit, ban_cumulative_retry_attempt_threshold, ban_duration_seconds
		FROM loadbalance_strategies
		WHERE profile_id = $1
		ORDER BY id ASC`,
		profileID,
	)
	if err != nil {
		return nil, fmt.Errorf("query runtime strategies for profile %d: %w", profileID, err)
	}
	defer rows.Close()

	items := make(map[int]loadbalance.RuntimeStrategy)
	for rows.Next() {
		var legacyStrategyType string
		var failureStatusCodes []int32
		item := loadbalance.RuntimeStrategy{}
		if err := rows.Scan(
			&item.ID,
			&item.Name,
			&legacyStrategyType,
			&failureStatusCodes,
			&item.BanMode,
			&item.RetryBaseDelayMS,
			&item.RetryBackoffMultiplier,
			&item.RetryJitterRatio,
			&item.RetryMaxDelayMS,
			&item.CycleRetryAttemptLimit,
			&item.BanCumulativeRetryAttemptThreshold,
			&item.BanDurationSeconds,
		); err != nil {
			return nil, fmt.Errorf("scan runtime strategy for profile %d: %w", profileID, err)
		}
		item.LegacyStrategyType = &legacyStrategyType
		item.FailureStatusCodes = intSliceFromInt32(failureStatusCodes)
		items[item.ID] = item
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate runtime strategies for profile %d: %w", profileID, err)
	}
	return items, nil
}

func intSliceFromInt32(values []int32) []int {
	items := make([]int, 0, len(values))
	for _, value := range values {
		items = append(items, int(value))
	}
	return items
}

func listActiveConnectionsForProfile(ctx context.Context, tx pgx.Tx, profileID int) (map[int]runtimeConnection, error) {
	rows, err := tx.Query(
		ctx,
		`SELECT connections.id, connections.profile_id, connections.api_family, connections.endpoint_id,
			connections.priority, connections.qps_limit, connections.max_in_flight_non_stream, connections.max_in_flight_stream,
			connections.name, connections.auth_type, connections.custom_headers, connections.pricing_template_id,
			pricing_templates.id, pricing_templates.pricing_unit, pricing_templates.pricing_currency_code,
			pricing_templates.input_price::text, pricing_templates.output_price::text,
			pricing_templates.cached_input_price::text, pricing_templates.cache_creation_price::text,
			pricing_templates.reasoning_price::text, pricing_templates.version,
			endpoints.id, endpoints.name, endpoints.base_url, endpoints.api_key
		FROM connections
		JOIN endpoints ON endpoints.id = connections.endpoint_id
		LEFT JOIN pricing_templates ON pricing_templates.id = connections.pricing_template_id
		WHERE connections.profile_id = $1 AND connections.is_active = TRUE
		ORDER BY connections.id ASC`,
		profileID,
	)
	if err != nil {
		return nil, fmt.Errorf("query active connections for profile %d: %w", profileID, err)
	}
	defer rows.Close()

	items := make(map[int]runtimeConnection)
	for rows.Next() {
		var qpsLimit sql.NullInt32
		var maxInFlightNonStream sql.NullInt32
		var maxInFlightStream sql.NullInt32
		var name sql.NullString
		var authType sql.NullString
		var customHeaders sql.NullString
		var pricingTemplateID sql.NullInt32
		var templateID sql.NullInt32
		var templatePricingUnit sql.NullString
		var templatePricingCurrencyCode sql.NullString
		var templateInputPrice sql.NullString
		var templateOutputPrice sql.NullString
		var templateCachedInputPrice sql.NullString
		var templateCacheCreationPrice sql.NullString
		var templateReasoningPrice sql.NullString
		var templateVersion sql.NullInt32
		var endpointName sql.NullString
		item := runtimeConnection{}
		if err := rows.Scan(
			&item.ID,
			&item.ProfileID,
			&item.APIFamily,
			&item.EndpointID,
			&item.Priority,
			&qpsLimit,
			&maxInFlightNonStream,
			&maxInFlightStream,
			&name,
			&authType,
			&customHeaders,
			&pricingTemplateID,
			&templateID,
			&templatePricingUnit,
			&templatePricingCurrencyCode,
			&templateInputPrice,
			&templateOutputPrice,
			&templateCachedInputPrice,
			&templateCacheCreationPrice,
			&templateReasoningPrice,
			&templateVersion,
			&item.Endpoint.ID,
			&endpointName,
			&item.Endpoint.BaseURL,
			&item.EncryptedEndpointAPIKey,
		); err != nil {
			return nil, fmt.Errorf("scan runtime connection for profile %d: %w", profileID, err)
		}
		item.QPSLimit = nullableInt32(qpsLimit)
		item.MaxInFlightNonStream = nullableInt32(maxInFlightNonStream)
		item.MaxInFlightStream = nullableInt32(maxInFlightStream)
		item.PricingTemplateID = nullableInt32(pricingTemplateID)
		if name.Valid {
			value := name.String
			item.Name = &value
		}
		if authType.Valid {
			value := authType.String
			item.AuthType = &value
		}
		if templateID.Valid {
			item.PricingTemplateSnapshot = &runtimePricingTemplateSnapshot{
				ID:                  int(templateID.Int32),
				PricingUnit:         strings.TrimSpace(templatePricingUnit.String),
				PricingCurrencyCode: strings.TrimSpace(templatePricingCurrencyCode.String),
				InputPrice:          strings.TrimSpace(templateInputPrice.String),
				OutputPrice:         strings.TrimSpace(templateOutputPrice.String),
				CachedInputPrice:    strings.TrimSpace(templateCachedInputPrice.String),
				CacheCreationPrice:  strings.TrimSpace(templateCacheCreationPrice.String),
				ReasoningPrice:      strings.TrimSpace(templateReasoningPrice.String),
				Version:             int(templateVersion.Int32),
			}
		}
		item.Endpoint.Name = nullableString(endpointName)
		item.CustomHeaders = parseCustomHeaders(customHeaders)
		items[item.ID] = item
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active connections for profile %d: %w", profileID, err)
	}
	return items, nil
}
