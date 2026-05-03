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
	ModelsByID             map[string]runtimeModelRecord
	ProxyTargetsBySourceID map[int][]runtimeProxyTargetRecord
	NativeTargetsByModelID map[string]nativePlanningSnapshot
	BlocklistRules         []headerBlocklistRule
	ReportCurrency         runtimeReportCurrencySnapshot
}

type runtimeProxyTargetRecord struct {
	SourceModelConfigID int
	TargetModelID       string
	ID                  int
	Position            int
	Weight              int
	TargetPriority      int
}

type nativePlanningSnapshot struct {
	Model       runtimeModelRecord
	Strategy    *loadbalance.RuntimeStrategy
	Connections []runtimeConnection
}

func buildPlanningSnapshot(ctx context.Context, tx pgx.Tx, profileID int, secretEncryptionKey string) (*planningSnapshot, error) {
	modelsByID, err := listEnabledModelsForProfile(ctx, tx, profileID)
	if err != nil {
		return nil, err
	}
	proxyTargetsBySourceID, err := listProxyTargetModelIDsForProfile(ctx, tx, profileID)
	if err != nil {
		return nil, err
	}
	strategiesByID, err := listRuntimeStrategiesForProfile(ctx, tx, profileID)
	if err != nil {
		return nil, err
	}
	connectionsByModelConfigID, err := listActiveConnectionsForProfile(ctx, tx, profileID)
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

	nativeTargetsByModelID := make(map[string]nativePlanningSnapshot, len(modelsByID))
	for modelID, model := range modelsByID {
		if model.ModelType == "proxy" {
			continue
		}
		var strategy *loadbalance.RuntimeStrategy
		if model.LoadbalanceStrategyID != nil {
			if resolved, ok := strategiesByID[*model.LoadbalanceStrategyID]; ok {
				resolvedStrategy := resolved
				strategy = &resolvedStrategy
			}
		}
		compiledConnections, err := compileRuntimeConnections(connectionsByModelConfigID[model.ID], model.APIFamily, secretEncryptionKey)
		if err != nil {
			return nil, fmt.Errorf("compile runtime connections for profile %d model %q: %w", profileID, model.ModelID, err)
		}
		nativeTargetsByModelID[modelID] = nativePlanningSnapshot{
			Model:       model,
			Strategy:    strategy,
			Connections: compiledConnections,
		}
	}

	return &planningSnapshot{
		ModelsByID:             modelsByID,
		ProxyTargetsBySourceID: proxyTargetsBySourceID,
		NativeTargetsByModelID: nativeTargetsByModelID,
		BlocklistRules:         blocklistRules,
		ReportCurrency:         reportCurrency,
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

func (s *Service) resolveExecutionTargetFromSnapshot(profileID int, snapshot *planningSnapshot, requestedModel runtimeModelRecord, referenceNow time.Time) (runtimeModelRecord, []runtimeConnection, map[int]loadbalance.RuntimeConnectionState, loadbalance.RuntimeStrategy, error) {
	if requestedModel.ModelType != "proxy" {
		return s.loadNativeExecutionTargetFromSnapshot(profileID, snapshot, requestedModel.ModelID, requestedModel.ModelID, referenceNow)
	}
	return s.selectProxyExecutionTargetFromSnapshot(profileID, snapshot, requestedModel, referenceNow)
}

type proxyResolvedTargetCandidate struct {
	Record        runtimeProxyTargetRecord
	Model         runtimeModelRecord
	Connections   []runtimeConnection
	RuntimeStates map[int]loadbalance.RuntimeConnectionState
	Strategy      loadbalance.RuntimeStrategy
}

func (s *Service) selectProxyExecutionTargetFromSnapshot(profileID int, snapshot *planningSnapshot, requestedModel runtimeModelRecord, referenceNow time.Time) (runtimeModelRecord, []runtimeConnection, map[int]loadbalance.RuntimeConnectionState, loadbalance.RuntimeStrategy, error) {
	strategy := normalizedRuntimeProxySelectionStrategy(requestedModel.ProxySelectionStrategy)
	if !isSupportedRuntimeProxySelectionStrategy(strategy) {
		return runtimeModelRecord{}, nil, nil, loadbalance.RuntimeStrategy{}, fmt.Errorf("proxy model %q has invalid proxy_selection_strategy %q", requestedModel.ModelID, requestedModel.ProxySelectionStrategy)
	}
	if strategy == proxySelectionStrategyWeightedStatic {
		return s.selectWeightedProxyExecutionTargetFromSnapshot(profileID, snapshot, requestedModel, referenceNow)
	}
	return s.selectOrderedProxyExecutionTargetFromSnapshot(profileID, snapshot, requestedModel, strategy, referenceNow)
}

func (s *Service) selectOrderedProxyExecutionTargetFromSnapshot(profileID int, snapshot *planningSnapshot, requestedModel runtimeModelRecord, strategy string, referenceNow time.Time) (runtimeModelRecord, []runtimeConnection, map[int]loadbalance.RuntimeConnectionState, loadbalance.RuntimeStrategy, error) {
	targetRecords := orderRuntimeProxyTargetCandidates(strategy, snapshot.ProxyTargetsBySourceID[requestedModel.ID])
	for _, targetRecord := range targetRecords {
		candidate, routable, err := s.resolveProxyTargetCandidateFromSnapshot(profileID, snapshot, requestedModel, targetRecord, referenceNow)
		if err != nil {
			return runtimeModelRecord{}, nil, nil, loadbalance.RuntimeStrategy{}, err
		}
		if routable {
			return candidate.Model, candidate.Connections, candidate.RuntimeStates, candidate.Strategy, nil
		}
	}
	return runtimeModelRecord{}, nil, nil, loadbalance.RuntimeStrategy{}, proxyNoRoutableTargetsError(requestedModel)
}

func (s *Service) selectWeightedProxyExecutionTargetFromSnapshot(profileID int, snapshot *planningSnapshot, requestedModel runtimeModelRecord, referenceNow time.Time) (runtimeModelRecord, []runtimeConnection, map[int]loadbalance.RuntimeConnectionState, loadbalance.RuntimeStrategy, error) {
	targetRecords := orderRuntimeProxyTargetCandidates(proxySelectionStrategyWeightedStatic, snapshot.ProxyTargetsBySourceID[requestedModel.ID])
	candidates := make([]proxyResolvedTargetCandidate, 0, len(targetRecords))
	totalWeight := 0
	for _, targetRecord := range targetRecords {
		candidate, routable, err := s.resolveProxyTargetCandidateFromSnapshot(profileID, snapshot, requestedModel, targetRecord, referenceNow)
		if err != nil {
			return runtimeModelRecord{}, nil, nil, loadbalance.RuntimeStrategy{}, err
		}
		if !routable {
			continue
		}
		candidates = append(candidates, candidate)
		totalWeight += targetRecord.Weight
	}
	if len(candidates) == 0 {
		return runtimeModelRecord{}, nil, nil, loadbalance.RuntimeStrategy{}, proxyNoRoutableTargetsError(requestedModel)
	}

	cursorSlot := s.runtimeState.ClaimProxyWeightedCursor(profileID, requestedModel.ID, totalWeight)
	cumulativeWeight := 0
	for _, candidate := range candidates {
		cumulativeWeight += candidate.Record.Weight
		if cursorSlot < cumulativeWeight {
			return candidate.Model, candidate.Connections, candidate.RuntimeStates, candidate.Strategy, nil
		}
	}
	selected := candidates[len(candidates)-1]
	return selected.Model, selected.Connections, selected.RuntimeStates, selected.Strategy, nil
}

func (s *Service) resolveProxyTargetCandidateFromSnapshot(profileID int, snapshot *planningSnapshot, requestedModel runtimeModelRecord, targetRecord runtimeProxyTargetRecord, referenceNow time.Time) (proxyResolvedTargetCandidate, bool, error) {
	if !proxyTargetMatchesRequestedModel(snapshot, requestedModel, targetRecord.TargetModelID) {
		return proxyResolvedTargetCandidate{}, false, nil
	}
	model, connections, runtimeStates, strategy, err := s.loadNativeExecutionTargetFromSnapshot(profileID, snapshot, targetRecord.TargetModelID, requestedModel.ModelID, referenceNow)
	if err == nil {
		return proxyResolvedTargetCandidate{Record: targetRecord, Model: model, Connections: connections, RuntimeStates: runtimeStates, Strategy: strategy}, true, nil
	}
	var runtimeErr *domainError
	if errors.As(err, &runtimeErr) && runtimeErr.StatusCode == http.StatusServiceUnavailable {
		return proxyResolvedTargetCandidate{}, false, nil
	}
	return proxyResolvedTargetCandidate{}, false, err
}

func proxyNoRoutableTargetsError(requestedModel runtimeModelRecord) error {
	return &domainError{StatusCode: http.StatusServiceUnavailable, Detail: fmt.Sprintf("Proxy model '%s' has no routable targets.", requestedModel.ModelID)}
}

func proxyTargetMatchesRequestedModel(snapshot *planningSnapshot, requestedModel runtimeModelRecord, targetModelID string) bool {
	targetModel, ok := snapshot.ModelsByID[targetModelID]
	if !ok {
		return false
	}
	if targetModel.ModelType != "native" {
		return false
	}
	return targetModel.APIFamily == requestedModel.APIFamily
}

func (s *Service) loadNativeExecutionTargetFromSnapshot(profileID int, snapshot *planningSnapshot, modelID string, requestedModelID string, referenceNow time.Time) (runtimeModelRecord, []runtimeConnection, map[int]loadbalance.RuntimeConnectionState, loadbalance.RuntimeStrategy, error) {
	nativeTarget, ok := snapshot.NativeTargetsByModelID[modelID]
	if !ok {
		return runtimeModelRecord{}, nil, nil, loadbalance.RuntimeStrategy{}, &domainError{StatusCode: http.StatusServiceUnavailable, Detail: fmt.Sprintf("No active connections available for model '%s'.", requestedModelID)}
	}
	if nativeTarget.Model.LoadbalanceStrategyID == nil {
		return runtimeModelRecord{}, nil, nil, loadbalance.RuntimeStrategy{}, fmt.Errorf("native model %q is missing loadbalance_strategy", nativeTarget.Model.ModelID)
	}
	if nativeTarget.Strategy == nil {
		return runtimeModelRecord{}, nil, nil, loadbalance.RuntimeStrategy{}, fmt.Errorf("loadbalance strategy %d not found for model %q", *nativeTarget.Model.LoadbalanceStrategyID, nativeTarget.Model.ModelID)
	}

	connections := nativeTarget.Connections
	if len(connections) == 0 {
		return runtimeModelRecord{}, nil, nil, loadbalance.RuntimeStrategy{}, &domainError{StatusCode: http.StatusServiceUnavailable, Detail: fmt.Sprintf("No active connections available for model '%s'.", requestedModelID)}
	}
	strategy := *nativeTarget.Strategy
	runtimeStates := s.runtimeState.SnapshotConnectionStates(profileID, runtimeConnectionRefs(connections))
	eligibleConnectionIDs := loadbalance.FilterEligibleConnectionIDs(toConnectionOrderCandidates(connections), runtimeStates, referenceNow)
	eligibleConnections := orderConnectionsByID(connections, eligibleConnectionIDs)
	if len(eligibleConnections) == 0 {
		return runtimeModelRecord{}, nil, nil, loadbalance.RuntimeStrategy{}, &domainError{StatusCode: http.StatusServiceUnavailable, Detail: fmt.Sprintf("No active connections available for model '%s'.", requestedModelID)}
	}
	eligibleRuntimeStates := make(map[int]loadbalance.RuntimeConnectionState, len(eligibleConnections))
	for _, connectionID := range eligibleConnectionIDs {
		if state, ok := runtimeStates[connectionID]; ok {
			eligibleRuntimeStates[connectionID] = state
		}
	}
	return nativeTarget.Model, eligibleConnections, eligibleRuntimeStates, strategy, nil
}

func listEnabledModelsForProfile(ctx context.Context, tx pgx.Tx, profileID int) (map[string]runtimeModelRecord, error) {
	rows, err := tx.Query(
		ctx,
		`SELECT model_configs.id, model_configs.profile_id, model_configs.api_family, model_configs.model_id, model_configs.model_type,
			model_configs.proxy_selection_strategy, vendors.id, vendors.key, vendors.name,
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
		var proxySelectionStrategy sql.NullString
		var vendorID sql.NullInt32
		var vendorKey sql.NullString
		var vendorName sql.NullString
		item := runtimeModelRecord{}
		if err := rows.Scan(&item.ID, &item.ProfileID, &item.APIFamily, &item.ModelID, &item.ModelType, &proxySelectionStrategy, &vendorID, &vendorKey, &vendorName, &item.AuditEnabled, &item.AuditCaptureBodies, &strategyID); err != nil {
			return nil, fmt.Errorf("scan enabled model for profile %d: %w", profileID, err)
		}
		if _, exists := items[item.ModelID]; exists {
			continue
		}
		if proxySelectionStrategy.Valid {
			item.ProxySelectionStrategy = proxySelectionStrategy.String
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

func listProxyTargetModelIDsForProfile(ctx context.Context, tx pgx.Tx, profileID int) (map[int][]runtimeProxyTargetRecord, error) {
	rows, err := tx.Query(
		ctx,
		`SELECT model_proxy_targets.source_model_config_id, target_models.model_id,
			model_proxy_targets.id, model_proxy_targets.position, model_proxy_targets.weight, model_proxy_targets.target_priority
		FROM model_proxy_targets
		JOIN model_configs AS source_models ON source_models.id = model_proxy_targets.source_model_config_id
		JOIN model_configs AS target_models ON target_models.id = model_proxy_targets.target_model_config_id
		WHERE source_models.profile_id = $1
		ORDER BY model_proxy_targets.source_model_config_id ASC, model_proxy_targets.position ASC, model_proxy_targets.id ASC`,
		profileID,
	)
	if err != nil {
		return nil, fmt.Errorf("query proxy targets for profile %d: %w", profileID, err)
	}
	defer rows.Close()

	items := make(map[int][]runtimeProxyTargetRecord)
	for rows.Next() {
		item := runtimeProxyTargetRecord{}
		if err := rows.Scan(&item.SourceModelConfigID, &item.TargetModelID, &item.ID, &item.Position, &item.Weight, &item.TargetPriority); err != nil {
			return nil, fmt.Errorf("scan proxy target for profile %d: %w", profileID, err)
		}
		items[item.SourceModelConfigID] = append(items[item.SourceModelConfigID], item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate proxy targets for profile %d: %w", profileID, err)
	}
	return items, nil
}

func listRuntimeStrategiesForProfile(ctx context.Context, tx pgx.Tx, profileID int) (map[int]loadbalance.RuntimeStrategy, error) {
	rows, err := tx.Query(
		ctx,
		`SELECT id, name, strategy_type, legacy_strategy_type, auto_recovery, routing_policy
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
		var legacyStrategyType sql.NullString
		item := loadbalance.RuntimeStrategy{}
		if err := rows.Scan(&item.ID, &item.Name, &item.StrategyType, &legacyStrategyType, &item.AutoRecoveryRaw, &item.RoutingPolicyRaw); err != nil {
			return nil, fmt.Errorf("scan runtime strategy for profile %d: %w", profileID, err)
		}
		if legacyStrategyType.Valid {
			value := legacyStrategyType.String
			item.LegacyStrategyType = &value
		}
		items[item.ID] = item
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate runtime strategies for profile %d: %w", profileID, err)
	}
	return items, nil
}

func listActiveConnectionsForProfile(ctx context.Context, tx pgx.Tx, profileID int) (map[int][]runtimeConnection, error) {
	rows, err := tx.Query(
		ctx,
		`SELECT connections.id, connections.profile_id, connections.model_config_id, connections.endpoint_id,
			connections.priority, connections.qps_limit, connections.max_in_flight_non_stream, connections.max_in_flight_stream,
			connections.name, connections.auth_type, connections.custom_headers, connections.pricing_template_id,
			pricing_templates.id, pricing_templates.pricing_unit, pricing_templates.pricing_currency_code,
			pricing_templates.input_price::text, pricing_templates.output_price::text,
			pricing_templates.cached_input_price::text, pricing_templates.cache_creation_price::text,
			pricing_templates.reasoning_price::text, pricing_templates.version,
			model_configs.model_id, endpoint_fx_rate_settings.fx_rate::text,
			endpoints.id, endpoints.name, endpoints.base_url, endpoints.api_key
		FROM connections
		JOIN endpoints ON endpoints.id = connections.endpoint_id
		JOIN model_configs ON model_configs.id = connections.model_config_id
		LEFT JOIN pricing_templates ON pricing_templates.id = connections.pricing_template_id
		LEFT JOIN endpoint_fx_rate_settings ON endpoint_fx_rate_settings.profile_id = connections.profile_id
			AND endpoint_fx_rate_settings.model_id = model_configs.model_id
			AND endpoint_fx_rate_settings.endpoint_id = connections.endpoint_id
		WHERE connections.profile_id = $1 AND connections.is_active = TRUE
		ORDER BY connections.model_config_id ASC, connections.priority ASC, connections.id ASC`,
		profileID,
	)
	if err != nil {
		return nil, fmt.Errorf("query active connections for profile %d: %w", profileID, err)
	}
	defer rows.Close()

	items := make(map[int][]runtimeConnection)
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
		var pricingModelID string
		var endpointFXRate sql.NullString
		var endpointName sql.NullString
		item := runtimeConnection{}
		if err := rows.Scan(
			&item.ID,
			&item.ProfileID,
			&item.ModelConfigID,
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
			&pricingModelID,
			&endpointFXRate,
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
				CachedInputPrice:    nullableString(templateCachedInputPrice),
				CacheCreationPrice:  nullableString(templateCacheCreationPrice),
				ReasoningPrice:      nullableString(templateReasoningPrice),
				Version:             int(templateVersion.Int32),
			}
		}
		if endpointFXRate.Valid {
			item.EndpointFXSnapshot = &runtimeEndpointFXSnapshot{
				ModelID:    strings.TrimSpace(pricingModelID),
				EndpointID: item.EndpointID,
				FXRate:     strings.TrimSpace(endpointFXRate.String),
			}
		}
		item.Endpoint.Name = nullableString(endpointName)
		item.CustomHeaders = parseCustomHeaders(customHeaders)
		items[item.ModelConfigID] = append(items[item.ModelConfigID], item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active connections for profile %d: %w", profileID, err)
	}
	return items, nil
}
