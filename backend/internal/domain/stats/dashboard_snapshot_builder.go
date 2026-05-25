package stats

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"
)

type dashboardSnapshotModel struct {
	ID           int
	ModelID      string
	DisplayName  *string
	ModelType    string
	IsEnabled    bool
	StrategyType *string
}

type dashboardSnapshotConnection struct {
	ID              int
	ModelConfigID   int
	EndpointID      int
	EndpointName    *string
	EndpointBaseURL *string
	IsActive        bool
}

type dashboardRoutingEdgeAccumulator struct {
	ModelID                string
	ModelLabel             string
	ModelConfigID          int
	EndpointID             int
	EndpointLabel          string
	ConnectionIDs          map[int]struct{}
	ActiveConnectionCount  int
	TrafficRequestCount24H int
	RequestCount24H        int
	SuccessCount24H        int
	ErrorCount24H          int
}

type dashboardRoutingTotals struct {
	ActiveConnectionCount  int
	TrafficRequestCount24H int
	RequestCount24H        int
	SuccessCount24H        int
	ErrorCount24H          int
	Label                  string
}

type dashboardRoutingModelTotals struct {
	dashboardRoutingTotals
	ModelID string
}

func BuildDashboardSnapshot(ctx context.Context, exec queryExecutor, profileID int, referenceNow time.Time) (DashboardSnapshot, error) {
	aggregate, err := BuildDashboardAggregateSnapshot(ctx, exec, profileID, referenceNow)
	if err != nil {
		return DashboardSnapshot{}, err
	}
	return NewDashboardSnapshot(aggregate, referenceNow), nil
}

func loadDashboardSnapshotModels(ctx context.Context, exec queryExecutor, profileID int) ([]dashboardSnapshotModel, error) {
	rows, err := exec.Query(ctx, `SELECT model_configs.id, model_configs.model_id, model_configs.display_name, model_configs.model_type, model_configs.is_enabled, loadbalance_strategies.strategy_type
		FROM model_configs
		LEFT JOIN loadbalance_strategies ON loadbalance_strategies.id = model_configs.loadbalance_strategy_id AND loadbalance_strategies.profile_id = model_configs.profile_id
		WHERE model_configs.profile_id = $1
		ORDER BY model_configs.id ASC`, profileID)
	if err != nil {
		return nil, fmt.Errorf("query dashboard models for profile %d: %w", profileID, err)
	}
	defer rows.Close()

	models := make([]dashboardSnapshotModel, 0)
	for rows.Next() {
		var displayName sql.NullString
		var strategyType sql.NullString
		model := dashboardSnapshotModel{}
		if err := rows.Scan(&model.ID, &model.ModelID, &displayName, &model.ModelType, &model.IsEnabled, &strategyType); err != nil {
			return nil, fmt.Errorf("scan dashboard model: %w", err)
		}
		model.ModelID = strings.TrimSpace(model.ModelID)
		model.ModelType = strings.TrimSpace(strings.ToLower(model.ModelType))
		model.DisplayName = normalizeOptionalString(nullableString(displayName))
		model.StrategyType = normalizeOptionalString(nullableString(strategyType))
		if model.StrategyType != nil {
			normalized := strings.TrimSpace(strings.ToLower(*model.StrategyType))
			model.StrategyType = &normalized
		}
		if model.ModelID == "" {
			continue
		}
		models = append(models, model)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate dashboard models for profile %d: %w", profileID, err)
	}
	return models, nil
}

func buildDashboardStrategyFamilySummary(models []dashboardSnapshotModel) DashboardStrategyFamilySummary {
	summary := DashboardStrategyFamilySummary{}
	for _, model := range models {
		if model.StrategyType == nil {
			summary.UnassignedCount++
			continue
		}
		if *model.StrategyType == "adaptive" {
			summary.AdaptiveCount++
			continue
		}
		summary.LegacyCount++
	}
	return summary
}

func countDashboardActiveModels(models []dashboardSnapshotModel) int {
	count := 0
	for _, model := range models {
		if model.IsEnabled {
			count++
		}
	}
	return count
}

func buildDashboardRoutingHealthMap(ctx context.Context, exec queryExecutor, profileID int, models []dashboardSnapshotModel, fromTime time.Time, toTime time.Time) (DashboardRoutingHealthMap, error) {
	connections, err := loadDashboardSnapshotConnections(ctx, exec, profileID)
	if err != nil {
		return DashboardRoutingHealthMap{}, err
	}
	connectionsByModel := map[int][]dashboardSnapshotConnection{}
	for _, connection := range connections {
		connectionsByModel[connection.ModelConfigID] = append(connectionsByModel[connection.ModelConfigID], connection)
	}

	edgeMap := map[string]*dashboardRoutingEdgeAccumulator{}
	connectionToEdgeKey := map[int]string{}
	for _, model := range models {
		if !model.IsEnabled {
			continue
		}
		modelConnections := connectionsByModel[model.ID]
		if model.ModelType == "proxy" && len(modelConnections) == 0 {
			continue
		}
		for _, connection := range modelConnections {
			edgeKey := dashboardRoutingEdgeKey(model.ModelID, connection.EndpointID)
			edge := edgeMap[edgeKey]
			if edge == nil {
				edge = &dashboardRoutingEdgeAccumulator{
					ModelID:       model.ModelID,
					ModelLabel:    dashboardModelLabel(model),
					ModelConfigID: model.ID,
					EndpointID:    connection.EndpointID,
					EndpointLabel: dashboardEndpointLabel(connection),
					ConnectionIDs: map[int]struct{}{},
				}
				edgeMap[edgeKey] = edge
			}
			if _, exists := edge.ConnectionIDs[connection.ID]; !exists {
				edge.ConnectionIDs[connection.ID] = struct{}{}
				if connection.IsActive {
					edge.ActiveConnectionCount++
				}
			}
			connectionToEdgeKey[connection.ID] = edgeKey
		}
	}
	if len(edgeMap) == 0 {
		return emptyDashboardRoutingHealthMap(), nil
	}
	if err := applyDashboardRoutingTraffic(ctx, exec, profileID, fromTime, toTime, edgeMap, connectionToEdgeKey); err != nil {
		return DashboardRoutingHealthMap{}, err
	}
	if err := applyDashboardRoutingSuccessRates(ctx, exec, profileID, fromTime, toTime, edgeMap, connectionToEdgeKey); err != nil {
		return DashboardRoutingHealthMap{}, err
	}
	return buildDashboardRoutingDiagramData(edgeMap), nil
}

func loadDashboardSnapshotConnections(ctx context.Context, exec queryExecutor, profileID int) ([]dashboardSnapshotConnection, error) {
	rows, err := exec.Query(ctx, `SELECT connections.id, connections.model_config_id, connections.endpoint_id, endpoints.name, endpoints.base_url, connections.is_active
		FROM connections
		LEFT JOIN endpoints ON endpoints.id = connections.endpoint_id AND endpoints.profile_id = connections.profile_id
		WHERE connections.profile_id = $1
		ORDER BY connections.model_config_id ASC, connections.priority ASC, connections.id ASC`, profileID)
	if err != nil {
		return nil, fmt.Errorf("query dashboard connections for profile %d: %w", profileID, err)
	}
	defer rows.Close()

	connections := make([]dashboardSnapshotConnection, 0)
	for rows.Next() {
		var endpointName sql.NullString
		var endpointBaseURL sql.NullString
		connection := dashboardSnapshotConnection{}
		if err := rows.Scan(&connection.ID, &connection.ModelConfigID, &connection.EndpointID, &endpointName, &endpointBaseURL, &connection.IsActive); err != nil {
			return nil, fmt.Errorf("scan dashboard connection: %w", err)
		}
		connection.EndpointName = normalizeOptionalString(nullableString(endpointName))
		connection.EndpointBaseURL = normalizeOptionalString(nullableString(endpointBaseURL))
		connections = append(connections, connection)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate dashboard connections for profile %d: %w", profileID, err)
	}
	return connections, nil
}

func applyDashboardRoutingTraffic(ctx context.Context, exec queryExecutor, profileID int, fromTime time.Time, toTime time.Time, edgeMap map[string]*dashboardRoutingEdgeAccumulator, connectionToEdgeKey map[int]string) error {
	rows, err := exec.Query(ctx, `SELECT connection_id, model_id, endpoint_id, COUNT(*)
		FROM usage_request_events
		WHERE profile_id = $1 AND endpoint_id IS NOT NULL AND success_flag = TRUE AND created_at >= $2 AND created_at <= $3
		GROUP BY connection_id, model_id, endpoint_id`, profileID, fromTime.UTC(), toTime.UTC())
	if err != nil {
		return fmt.Errorf("query dashboard routing traffic for profile %d: %w", profileID, err)
	}
	defer rows.Close()
	for rows.Next() {
		var connectionID sql.NullInt32
		var modelID string
		var endpointID int
		var requestCount int
		if err := rows.Scan(&connectionID, &modelID, &endpointID, &requestCount); err != nil {
			return fmt.Errorf("scan dashboard routing traffic: %w", err)
		}
		edge := dashboardRoutingEdgeForTraffic(edgeMap, connectionToEdgeKey, connectionID, modelID, endpointID)
		if edge == nil {
			continue
		}
		if requestCount > 0 {
			edge.TrafficRequestCount24H += requestCount
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate dashboard routing traffic for profile %d: %w", profileID, err)
	}
	return nil
}

func dashboardRoutingEdgeForTraffic(edgeMap map[string]*dashboardRoutingEdgeAccumulator, connectionToEdgeKey map[int]string, connectionID sql.NullInt32, modelID string, endpointID int) *dashboardRoutingEdgeAccumulator {
	if connectionID.Valid {
		if edgeKey, ok := connectionToEdgeKey[int(connectionID.Int32)]; ok {
			if edge := edgeMap[edgeKey]; edge != nil {
				return edge
			}
		}
	}
	return edgeMap[dashboardRoutingEdgeKey(modelID, endpointID)]
}

func applyDashboardRoutingSuccessRates(ctx context.Context, exec queryExecutor, profileID int, fromTime time.Time, toTime time.Time, edgeMap map[string]*dashboardRoutingEdgeAccumulator, connectionToEdgeKey map[int]string) error {
	rates, err := GetConnectionSuccessRates(ctx, exec, ConnectionSuccessRateParams{ProfileID: profileID, FromTime: &fromTime, ToTime: &toTime})
	if err != nil {
		return err
	}
	for _, rate := range rates {
		edgeKey, ok := connectionToEdgeKey[rate.ConnectionID]
		if !ok {
			continue
		}
		edge := edgeMap[edgeKey]
		if edge == nil {
			continue
		}
		edge.RequestCount24H += rate.TotalRequests
		edge.SuccessCount24H += rate.SuccessCount
		edge.ErrorCount24H += rate.ErrorCount
	}
	return nil
}

func buildDashboardRoutingDiagramData(edgeMap map[string]*dashboardRoutingEdgeAccumulator) DashboardRoutingHealthMap {
	links := make([]DashboardRoutingLink, 0, len(edgeMap))
	for _, edge := range edgeMap {
		links = append(links, DashboardRoutingLink{
			ID:                     dashboardRoutingEdgeKey(edge.ModelID, edge.EndpointID),
			SourceNodeID:           dashboardEndpointNodeID(edge.EndpointID),
			TargetNodeID:           dashboardModelNodeID(edge.ModelConfigID),
			ModelID:                edge.ModelID,
			ModelLabel:             edge.ModelLabel,
			ModelConfigID:          edge.ModelConfigID,
			EndpointID:             edge.EndpointID,
			EndpointLabel:          edge.EndpointLabel,
			ActiveConnectionCount:  edge.ActiveConnectionCount,
			TrafficRequestCount24H: edge.TrafficRequestCount24H,
			RequestCount24H:        edge.RequestCount24H,
			SuccessCount24H:        edge.SuccessCount24H,
			ErrorCount24H:          edge.ErrorCount24H,
			SuccessRate24H:         dashboardSuccessRate(edge.SuccessCount24H, edge.RequestCount24H),
		})
	}
	sort.Slice(links, func(i int, j int) bool {
		return dashboardRoutingLinkLess(links[i], links[j])
	})
	return buildDashboardRoutingDataFromLinks(links)
}

func buildDashboardRoutingDataFromLinks(links []DashboardRoutingLink) DashboardRoutingHealthMap {
	endpointTotals := map[int]dashboardRoutingTotals{}
	modelTotals := map[int]dashboardRoutingModelTotals{}
	for _, link := range links {
		endpointTotal := endpointTotals[link.EndpointID]
		if endpointTotal.Label == "" {
			endpointTotal.Label = link.EndpointLabel
		}
		endpointTotal.ActiveConnectionCount += link.ActiveConnectionCount
		endpointTotal.TrafficRequestCount24H += link.TrafficRequestCount24H
		endpointTotal.RequestCount24H += link.RequestCount24H
		endpointTotal.SuccessCount24H += link.SuccessCount24H
		endpointTotal.ErrorCount24H += link.ErrorCount24H
		endpointTotals[link.EndpointID] = endpointTotal

		modelTotal := modelTotals[link.ModelConfigID]
		if modelTotal.Label == "" {
			modelTotal.Label = link.ModelLabel
			modelTotal.ModelID = link.ModelID
		}
		modelTotal.ActiveConnectionCount += link.ActiveConnectionCount
		modelTotal.TrafficRequestCount24H += link.TrafficRequestCount24H
		modelTotal.RequestCount24H += link.RequestCount24H
		modelTotal.SuccessCount24H += link.SuccessCount24H
		modelTotal.ErrorCount24H += link.ErrorCount24H
		modelTotals[link.ModelConfigID] = modelTotal
	}

	endpointNodes := buildDashboardEndpointNodes(endpointTotals)
	modelNodes := buildDashboardModelNodes(modelTotals)
	return DashboardRoutingHealthMap{
		Nodes:                  append(endpointNodes, modelNodes...),
		Links:                  links,
		EndpointCount:          len(endpointNodes),
		ModelCount:             len(modelNodes),
		ActiveConnectionTotal:  dashboardActiveConnectionTotal(links),
		TrafficRequestTotal24H: dashboardTrafficRequestTotal(links),
	}
}

func buildDashboardEndpointNodes(totals map[int]dashboardRoutingTotals) []DashboardRoutingNode {
	endpointIDs := make([]int, 0, len(totals))
	for endpointID := range totals {
		endpointIDs = append(endpointIDs, endpointID)
	}
	sort.Slice(endpointIDs, func(i int, j int) bool {
		return dashboardRoutingTotalLess(endpointIDs[i], totals[endpointIDs[i]], endpointIDs[j], totals[endpointIDs[j]])
	})

	nodes := make([]DashboardRoutingNode, 0, len(endpointIDs))
	for _, endpointID := range endpointIDs {
		total := totals[endpointID]
		endpointIDValue := endpointID
		nodes = append(nodes, DashboardRoutingNode{
			ID:                     dashboardEndpointNodeID(endpointID),
			Name:                   total.Label,
			Kind:                   "endpoint",
			Label:                  total.Label,
			Sublabel:               statsStringPtr(fmt.Sprintf("Endpoint %d", endpointID)),
			EndpointID:             &endpointIDValue,
			ModelID:                nil,
			ModelConfigID:          nil,
			ActiveConnectionCount:  total.ActiveConnectionCount,
			TrafficRequestCount24H: total.TrafficRequestCount24H,
			RequestCount24H:        total.RequestCount24H,
			SuccessCount24H:        total.SuccessCount24H,
			ErrorCount24H:          total.ErrorCount24H,
			SuccessRate24H:         dashboardSuccessRate(total.SuccessCount24H, total.RequestCount24H),
		})
	}
	return nodes
}

func buildDashboardModelNodes(totals map[int]dashboardRoutingModelTotals) []DashboardRoutingNode {
	modelConfigIDs := make([]int, 0, len(totals))
	for modelConfigID := range totals {
		modelConfigIDs = append(modelConfigIDs, modelConfigID)
	}
	sort.Slice(modelConfigIDs, func(i int, j int) bool {
		left := totals[modelConfigIDs[i]].dashboardRoutingTotals
		right := totals[modelConfigIDs[j]].dashboardRoutingTotals
		return dashboardRoutingTotalLess(modelConfigIDs[i], left, modelConfigIDs[j], right)
	})

	nodes := make([]DashboardRoutingNode, 0, len(modelConfigIDs))
	for _, modelConfigID := range modelConfigIDs {
		total := totals[modelConfigID]
		modelID := total.ModelID
		modelConfigIDValue := modelConfigID
		node := DashboardRoutingNode{
			ID:                     dashboardModelNodeID(modelConfigID),
			Name:                   total.Label,
			Kind:                   "model",
			Label:                  total.Label,
			EndpointID:             nil,
			ModelID:                &modelID,
			ModelConfigID:          &modelConfigIDValue,
			ActiveConnectionCount:  total.ActiveConnectionCount,
			TrafficRequestCount24H: total.TrafficRequestCount24H,
			RequestCount24H:        total.RequestCount24H,
			SuccessCount24H:        total.SuccessCount24H,
			ErrorCount24H:          total.ErrorCount24H,
			SuccessRate24H:         dashboardSuccessRate(total.SuccessCount24H, total.RequestCount24H),
		}
		if total.Label != modelID {
			node.Sublabel = &modelID
		}
		nodes = append(nodes, node)
	}
	return nodes
}

func dashboardRoutingLinkLess(left DashboardRoutingLink, right DashboardRoutingLink) bool {
	if left.ActiveConnectionCount != right.ActiveConnectionCount {
		return left.ActiveConnectionCount > right.ActiveConnectionCount
	}
	if left.TrafficRequestCount24H != right.TrafficRequestCount24H {
		return left.TrafficRequestCount24H > right.TrafficRequestCount24H
	}
	if left.EndpointLabel != right.EndpointLabel {
		return left.EndpointLabel < right.EndpointLabel
	}
	if left.ModelLabel != right.ModelLabel {
		return left.ModelLabel < right.ModelLabel
	}
	return left.ID < right.ID
}

func dashboardRoutingTotalLess(leftID int, left dashboardRoutingTotals, rightID int, right dashboardRoutingTotals) bool {
	if left.ActiveConnectionCount != right.ActiveConnectionCount {
		return left.ActiveConnectionCount > right.ActiveConnectionCount
	}
	if left.Label != right.Label {
		return left.Label < right.Label
	}
	return leftID < rightID
}

func dashboardModelLabel(model dashboardSnapshotModel) string {
	if model.DisplayName != nil && strings.TrimSpace(*model.DisplayName) != "" {
		return strings.TrimSpace(*model.DisplayName)
	}
	return strings.TrimSpace(model.ModelID)
}

func dashboardEndpointLabel(connection dashboardSnapshotConnection) string {
	return resolveEndpointLabel(connection.EndpointName, connection.EndpointBaseURL, nil, &connection.EndpointID, fmt.Sprintf("Endpoint %d", connection.EndpointID))
}

func dashboardSuccessRate(successCount int, requestCount int) *float64 {
	if requestCount <= 0 {
		return nil
	}
	rate := successRate(successCount, requestCount)
	return &rate
}

func dashboardRoutingEdgeKey(modelID string, endpointID int) string {
	return fmt.Sprintf("%s#%d", strings.TrimSpace(modelID), endpointID)
}

func dashboardEndpointNodeID(endpointID int) string {
	return fmt.Sprintf("endpoint-%d", endpointID)
}

func dashboardModelNodeID(modelConfigID int) string {
	return fmt.Sprintf("model-%d", modelConfigID)
}

func dashboardActiveConnectionTotal(links []DashboardRoutingLink) int {
	total := 0
	for _, link := range links {
		total += link.ActiveConnectionCount
	}
	return total
}

func dashboardTrafficRequestTotal(links []DashboardRoutingLink) int {
	total := 0
	for _, link := range links {
		total += link.TrafficRequestCount24H
	}
	return total
}

func emptyDashboardRoutingHealthMap() DashboardRoutingHealthMap {
	return DashboardRoutingHealthMap{
		Nodes: []DashboardRoutingNode{},
		Links: []DashboardRoutingLink{},
	}
}

func cloneDashboardRoutingHealthMap(value DashboardRoutingHealthMap) DashboardRoutingHealthMap {
	nodes := append([]DashboardRoutingNode{}, value.Nodes...)
	links := append([]DashboardRoutingLink{}, value.Links...)
	if nodes == nil {
		nodes = []DashboardRoutingNode{}
	}
	if links == nil {
		links = []DashboardRoutingLink{}
	}
	return DashboardRoutingHealthMap{
		Nodes:                  nodes,
		Links:                  links,
		EndpointCount:          value.EndpointCount,
		ModelCount:             value.ModelCount,
		ActiveConnectionTotal:  value.ActiveConnectionTotal,
		TrafficRequestTotal24H: value.TrafficRequestTotal24H,
	}
}
