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
	"github.com/coachpo/prism/backend/internal/profiledomain"
)

type planningSnapshot struct {
	ModelsByID             map[string]runtimeModelRecord
	ProxyTargetsBySourceID map[int][]string
	NativeTargetsByModelID map[string]nativePlanningSnapshot
	BlocklistRules         []headerBlocklistRule
	ReportCurrency         runtimeReportCurrencySnapshot
}

type nativePlanningSnapshot struct {
	Model       runtimeModelRecord
	Strategy    *loadbalance.RuntimeStrategy
	Connections []runtimeConnection
}

func (s *Service) loadActiveProfileWithCache(ctx context.Context, tx pgx.Tx) (profiledomain.Profile, error) {
	if s.cache == nil {
		return profiledomain.ResolveActiveProfile(ctx, tx, s.nowUTC)
	}
	return s.cache.loadActiveProfile(s.nowUTC(), func() (profiledomain.Profile, error) {
		return profiledomain.ResolveActiveProfile(ctx, tx, s.nowUTC)
	})
}

func (s *Service) loadPlanningSnapshotWithCache(ctx context.Context, tx pgx.Tx, profileID int) (*planningSnapshot, error) {
	if s.cache == nil {
		return buildPlanningSnapshot(ctx, tx, profileID)
	}
	return s.cache.loadPlanningSnapshot(s.nowUTC(), profileID, func() (*planningSnapshot, error) {
		return buildPlanningSnapshot(ctx, tx, profileID)
	})
}

func buildPlanningSnapshot(ctx context.Context, tx pgx.Tx, profileID int) (*planningSnapshot, error) {
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
				clonedStrategy := cloneRuntimeStrategy(resolved)
				strategy = &clonedStrategy
			}
		}
		nativeTargetsByModelID[modelID] = nativePlanningSnapshot{
			Model:       cloneRuntimeModelRecord(model),
			Strategy:    strategy,
			Connections: cloneRuntimeConnections(connectionsByModelConfigID[model.ID]),
		}
	}

	return &planningSnapshot{
		ModelsByID:             cloneRuntimeModelMap(modelsByID),
		ProxyTargetsBySourceID: cloneProxyTargets(proxyTargetsBySourceID),
		NativeTargetsByModelID: cloneNativePlanningSnapshotMap(nativeTargetsByModelID),
		BlocklistRules:         cloneHeaderBlocklistRules(blocklistRules),
		ReportCurrency:         cloneReportCurrencySnapshot(reportCurrency),
	}, nil
}

func (s *Service) resolveExecutionTargetFromSnapshot(ctx context.Context, tx pgx.Tx, profileID int, snapshot *planningSnapshot, requestedModel runtimeModelRecord, referenceNow time.Time) (runtimeModelRecord, []runtimeConnection, map[int]loadbalance.RuntimeConnectionState, loadbalance.RuntimeStrategy, error) {
	if requestedModel.ModelType != "proxy" {
		return s.loadNativeExecutionTargetFromSnapshot(ctx, tx, profileID, snapshot, requestedModel.ModelID, requestedModel.ModelID, referenceNow)
	}

	targetModelIDs := snapshot.ProxyTargetsBySourceID[requestedModel.ID]
	for _, targetModelID := range targetModelIDs {
		model, connections, runtimeStates, strategy, err := s.loadNativeExecutionTargetFromSnapshot(ctx, tx, profileID, snapshot, targetModelID, requestedModel.ModelID, referenceNow)
		if err == nil {
			return model, connections, runtimeStates, strategy, nil
		}
		var runtimeErr *domainError
		if errors.As(err, &runtimeErr) && runtimeErr.StatusCode == http.StatusServiceUnavailable {
			continue
		}
		return runtimeModelRecord{}, nil, nil, loadbalance.RuntimeStrategy{}, err
	}
	return runtimeModelRecord{}, nil, nil, loadbalance.RuntimeStrategy{}, &domainError{StatusCode: http.StatusServiceUnavailable, Detail: fmt.Sprintf("Proxy model '%s' has no routable targets.", requestedModel.ModelID)}
}

func (s *Service) loadNativeExecutionTargetFromSnapshot(ctx context.Context, tx pgx.Tx, profileID int, snapshot *planningSnapshot, modelID string, requestedModelID string, referenceNow time.Time) (runtimeModelRecord, []runtimeConnection, map[int]loadbalance.RuntimeConnectionState, loadbalance.RuntimeStrategy, error) {
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

	connections := cloneRuntimeConnections(nativeTarget.Connections)
	if len(connections) == 0 {
		return runtimeModelRecord{}, nil, nil, loadbalance.RuntimeStrategy{}, &domainError{StatusCode: http.StatusServiceUnavailable, Detail: fmt.Sprintf("No active connections available for model '%s'.", requestedModelID)}
	}
	strategy := cloneRuntimeStrategy(*nativeTarget.Strategy)
	runtimeStates, err := loadbalance.LoadRuntimeConnectionStates(ctx, tx, profileID, runtimeConnectionIDs(connections))
	if err != nil {
		return runtimeModelRecord{}, nil, nil, loadbalance.RuntimeStrategy{}, err
	}
	for _, connection := range connections {
		state, ok := runtimeStates[connection.ID]
		if !ok || state.IsEligible(referenceNow) {
			continue
		}
		if err := loadbalance.RecordRuntimePlanningSkip(ctx, tx, profileID, connection.ID, state, strategy, referenceNow); err != nil {
			return runtimeModelRecord{}, nil, nil, loadbalance.RuntimeStrategy{}, err
		}
	}
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
	return cloneRuntimeModelRecord(nativeTarget.Model), eligibleConnections, eligibleRuntimeStates, strategy, nil
}

func listEnabledModelsForProfile(ctx context.Context, tx pgx.Tx, profileID int) (map[string]runtimeModelRecord, error) {
	rows, err := tx.Query(
		ctx,
		`SELECT model_configs.id, model_configs.profile_id, model_configs.api_family, model_configs.model_id, model_configs.model_type,
			vendors.id, vendors.key, vendors.name, COALESCE(vendors.audit_enabled, FALSE), model_configs.loadbalance_strategy_id
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
		if err := rows.Scan(&item.ID, &item.ProfileID, &item.APIFamily, &item.ModelID, &item.ModelType, &vendorID, &vendorKey, &vendorName, &item.AuditEnabled, &strategyID); err != nil {
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

func listProxyTargetModelIDsForProfile(ctx context.Context, tx pgx.Tx, profileID int) (map[int][]string, error) {
	rows, err := tx.Query(
		ctx,
		`SELECT model_proxy_targets.source_model_config_id, target_models.model_id
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

	items := make(map[int][]string)
	for rows.Next() {
		var sourceModelConfigID int
		var targetModelID string
		if err := rows.Scan(&sourceModelConfigID, &targetModelID); err != nil {
			return nil, fmt.Errorf("scan proxy target for profile %d: %w", profileID, err)
		}
		items[sourceModelConfigID] = append(items[sourceModelConfigID], targetModelID)
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
			&item.Endpoint.APIKey,
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

func clonePlanningSnapshot(snapshot planningSnapshot) planningSnapshot {
	cloned := planningSnapshot{
		ModelsByID:             cloneRuntimeModelMap(snapshot.ModelsByID),
		ProxyTargetsBySourceID: cloneProxyTargets(snapshot.ProxyTargetsBySourceID),
		NativeTargetsByModelID: cloneNativePlanningSnapshotMap(snapshot.NativeTargetsByModelID),
		BlocklistRules:         cloneHeaderBlocklistRules(snapshot.BlocklistRules),
		ReportCurrency:         cloneReportCurrencySnapshot(snapshot.ReportCurrency),
	}
	return cloned
}

func cloneRuntimeModelMap(source map[string]runtimeModelRecord) map[string]runtimeModelRecord {
	if len(source) == 0 {
		return nil
	}
	target := make(map[string]runtimeModelRecord, len(source))
	for key, value := range source {
		target[key] = cloneRuntimeModelRecord(value)
	}
	return target
}

func cloneRuntimeModelRecord(model runtimeModelRecord) runtimeModelRecord {
	cloned := model
	cloned.VendorID = cloneOptionalInt(model.VendorID)
	cloned.VendorKey = cloneOptionalString(model.VendorKey)
	cloned.VendorName = cloneOptionalString(model.VendorName)
	cloned.LoadbalanceStrategyID = cloneOptionalInt(model.LoadbalanceStrategyID)
	return cloned
}

func cloneNativePlanningSnapshotMap(source map[string]nativePlanningSnapshot) map[string]nativePlanningSnapshot {
	if len(source) == 0 {
		return nil
	}
	target := make(map[string]nativePlanningSnapshot, len(source))
	for key, value := range source {
		cloned := nativePlanningSnapshot{
			Model:       cloneRuntimeModelRecord(value.Model),
			Connections: cloneRuntimeConnections(value.Connections),
		}
		if value.Strategy != nil {
			strategy := cloneRuntimeStrategy(*value.Strategy)
			cloned.Strategy = &strategy
		}
		target[key] = cloned
	}
	return target
}

func cloneRuntimeStrategy(strategy loadbalance.RuntimeStrategy) loadbalance.RuntimeStrategy {
	cloned := strategy
	cloned.LegacyStrategyType = cloneOptionalString(strategy.LegacyStrategyType)
	cloned.AutoRecoveryRaw = append([]byte(nil), strategy.AutoRecoveryRaw...)
	cloned.RoutingPolicyRaw = append([]byte(nil), strategy.RoutingPolicyRaw...)
	return cloned
}

func cloneRuntimeConnections(source []runtimeConnection) []runtimeConnection {
	if len(source) == 0 {
		return nil
	}
	target := make([]runtimeConnection, 0, len(source))
	for _, connection := range source {
		target = append(target, cloneRuntimeConnection(connection))
	}
	return target
}

func cloneRuntimeConnection(connection runtimeConnection) runtimeConnection {
	cloned := connection
	cloned.QPSLimit = cloneOptionalInt(connection.QPSLimit)
	cloned.MaxInFlightNonStream = cloneOptionalInt(connection.MaxInFlightNonStream)
	cloned.MaxInFlightStream = cloneOptionalInt(connection.MaxInFlightStream)
	cloned.Name = cloneOptionalString(connection.Name)
	cloned.AuthType = cloneOptionalString(connection.AuthType)
	cloned.CustomHeaders = cloneJSONMap(connection.CustomHeaders)
	cloned.PricingTemplateID = cloneOptionalInt(connection.PricingTemplateID)
	if connection.PricingTemplateSnapshot != nil {
		snapshot := clonePricingTemplateSnapshot(*connection.PricingTemplateSnapshot)
		cloned.PricingTemplateSnapshot = &snapshot
	}
	if connection.EndpointFXSnapshot != nil {
		snapshot := cloneEndpointFXSnapshot(*connection.EndpointFXSnapshot)
		cloned.EndpointFXSnapshot = &snapshot
	}
	cloned.Endpoint = cloneRuntimeEndpoint(connection.Endpoint)
	return cloned
}

func cloneRuntimeEndpoint(endpoint runtimeEndpoint) runtimeEndpoint {
	cloned := endpoint
	cloned.Name = cloneOptionalString(endpoint.Name)
	return cloned
}

func clonePricingTemplateSnapshot(snapshot runtimePricingTemplateSnapshot) runtimePricingTemplateSnapshot {
	cloned := snapshot
	cloned.CachedInputPrice = cloneOptionalString(snapshot.CachedInputPrice)
	cloned.CacheCreationPrice = cloneOptionalString(snapshot.CacheCreationPrice)
	cloned.ReasoningPrice = cloneOptionalString(snapshot.ReasoningPrice)
	return cloned
}

func cloneEndpointFXSnapshot(snapshot runtimeEndpointFXSnapshot) runtimeEndpointFXSnapshot {
	return snapshot
}

func cloneHeaderBlocklistRules(source []headerBlocklistRule) []headerBlocklistRule {
	if len(source) == 0 {
		return nil
	}
	return append([]headerBlocklistRule(nil), source...)
}

func cloneReportCurrencySnapshot(snapshot runtimeReportCurrencySnapshot) runtimeReportCurrencySnapshot {
	return snapshot
}

func cloneProxyTargets(source map[int][]string) map[int][]string {
	if len(source) == 0 {
		return nil
	}
	target := make(map[int][]string, len(source))
	for key, values := range source {
		target[key] = append([]string(nil), values...)
	}
	return target
}

func cloneOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneOptionalInt(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneJSONMap(source map[string]any) map[string]any {
	if len(source) == 0 {
		return nil
	}
	target := make(map[string]any, len(source))
	for key, value := range source {
		target[key] = cloneJSONValue(value)
	}
	return target
}

func cloneJSONValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneJSONMap(typed)
	case []any:
		cloned := make([]any, 0, len(typed))
		for _, item := range typed {
			cloned = append(cloned, cloneJSONValue(item))
		}
		return cloned
	default:
		return typed
	}
}
