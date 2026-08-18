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
	ID          int
	ModelID     string
	DisplayName *string
	IsEnabled   bool
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

func loadDashboardSnapshotModels(ctx context.Context, exec queryExecutor, profileID int) ([]dashboardSnapshotModel, error) {
	rows, err := exec.Query(ctx, `SELECT id, model_id, display_name, is_enabled
		FROM model_configs
		WHERE profile_id = $1
		ORDER BY id ASC`, profileID)
	if err != nil {
		return nil, fmt.Errorf("query dashboard models for profile %d: %w", profileID, err)
	}
	defer rows.Close()

	models := make([]dashboardSnapshotModel, 0)
	for rows.Next() {
		var displayName sql.NullString
		model := dashboardSnapshotModel{}
		if err := rows.Scan(&model.ID, &model.ModelID, &displayName, &model.IsEnabled); err != nil {
			return nil, fmt.Errorf("scan dashboard model: %w", err)
		}
		model.ModelID = strings.TrimSpace(model.ModelID)
		model.DisplayName = normalizeOptionalString(nullableString(displayName))
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
	connectionToEdgeKeys := map[int]map[string]struct{}{}
	for _, model := range models {
		if !model.IsEnabled {
			continue
		}
		modelConnections := connectionsByModel[model.ID]
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
				if connectionToEdgeKeys[connection.ID] == nil {
					connectionToEdgeKeys[connection.ID] = map[string]struct{}{}
				}
				connectionToEdgeKeys[connection.ID][edgeKey] = struct{}{}
			}
		}
	}
	if len(edgeMap) == 0 {
		return emptyDashboardRoutingHealthMap(), nil
	}
	if err := applyDashboardRoutingTraffic(ctx, exec, profileID, fromTime, toTime, edgeMap, connectionToEdgeKeys); err != nil {
		return DashboardRoutingHealthMap{}, err
	}
	if err := applyDashboardRoutingSuccessRates(ctx, exec, profileID, fromTime, toTime, edgeMap, connectionToEdgeKeys); err != nil {
		return DashboardRoutingHealthMap{}, err
	}
	return buildDashboardRoutingHealthData(edgeMap), nil
}

func loadDashboardSnapshotConnections(ctx context.Context, exec queryExecutor, profileID int) ([]dashboardSnapshotConnection, error) {
	rows, err := exec.Query(ctx, `WITH RECURSIVE reachable_targets AS (
		SELECT model_access_targets.source_model_config_id, model_access_targets.target_model_config_id, model_access_targets.target_connection_id, ARRAY[model_access_targets.source_model_config_id] AS path
		FROM model_access_targets
		JOIN model_configs ON model_configs.id = model_access_targets.source_model_config_id AND model_configs.profile_id = model_access_targets.profile_id
		WHERE model_access_targets.profile_id = $1 AND model_access_targets.is_enabled = TRUE AND model_configs.is_enabled = TRUE
		UNION ALL
		SELECT reachable_targets.source_model_config_id, child_targets.target_model_config_id, child_targets.target_connection_id, reachable_targets.path || child_targets.source_model_config_id
		FROM reachable_targets
		JOIN model_access_targets child_targets ON child_targets.profile_id = $1 AND child_targets.source_model_config_id = reachable_targets.target_model_config_id AND child_targets.is_enabled = TRUE
		JOIN model_configs child_models ON child_models.id = child_targets.source_model_config_id AND child_models.profile_id = child_targets.profile_id AND child_models.is_enabled = TRUE
		WHERE reachable_targets.target_model_config_id IS NOT NULL AND NOT child_targets.source_model_config_id = ANY(reachable_targets.path)
	)
		SELECT DISTINCT connections.id, reachable_targets.source_model_config_id, connections.endpoint_id, endpoints.name, endpoints.base_url, connections.is_active
		FROM reachable_targets
		JOIN connections ON connections.id = reachable_targets.target_connection_id AND connections.profile_id = $1
		LEFT JOIN endpoints ON endpoints.id = connections.endpoint_id AND endpoints.profile_id = connections.profile_id
		WHERE reachable_targets.target_connection_id IS NOT NULL
		ORDER BY reachable_targets.source_model_config_id ASC, connections.id ASC`, profileID)
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

func applyDashboardRoutingTraffic(ctx context.Context, exec queryExecutor, profileID int, fromTime time.Time, toTime time.Time, edgeMap map[string]*dashboardRoutingEdgeAccumulator, connectionToEdgeKeys map[int]map[string]struct{}) error {
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
		edge := dashboardRoutingEdgeForTraffic(edgeMap, connectionToEdgeKeys, connectionID, modelID, endpointID)
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

func dashboardRoutingEdgeForTraffic(edgeMap map[string]*dashboardRoutingEdgeAccumulator, connectionToEdgeKeys map[int]map[string]struct{}, connectionID sql.NullInt32, modelID string, endpointID int) *dashboardRoutingEdgeAccumulator {
	if edge := edgeMap[dashboardRoutingEdgeKey(modelID, endpointID)]; edge != nil {
		return edge
	}
	if connectionID.Valid {
		for edgeKey := range connectionToEdgeKeys[int(connectionID.Int32)] {
			if edge := edgeMap[edgeKey]; edge != nil {
				return edge
			}
		}
	}
	return nil
}

func applyDashboardRoutingSuccessRates(ctx context.Context, exec queryExecutor, profileID int, fromTime time.Time, toTime time.Time, edgeMap map[string]*dashboardRoutingEdgeAccumulator, connectionToEdgeKeys map[int]map[string]struct{}) error {
	rows, err := exec.Query(ctx, `SELECT connection_id,
		COUNT(*) AS total_requests,
		COALESCE(SUM(CASE WHEN success_flag THEN 1 ELSE 0 END), 0) AS success_count
		FROM usage_request_events
		WHERE profile_id = $1 AND connection_id IS NOT NULL AND created_at >= $2 AND created_at <= $3
		GROUP BY connection_id`, profileID, fromTime.UTC(), toTime.UTC())
	if err != nil {
		return fmt.Errorf("query dashboard routing success rates for profile %d: %w", profileID, err)
	}
	defer rows.Close()
	for rows.Next() {
		var connectionID int
		var totalRequests int
		var successCount int
		if err := rows.Scan(&connectionID, &totalRequests, &successCount); err != nil {
			return fmt.Errorf("scan dashboard routing success rate: %w", err)
		}
		for edgeKey := range connectionToEdgeKeys[connectionID] {
			edge := edgeMap[edgeKey]
			if edge == nil {
				continue
			}
			edge.RequestCount24H += totalRequests
			edge.SuccessCount24H += successCount
			edge.ErrorCount24H += totalRequests - successCount
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate dashboard routing success rates for profile %d: %w", profileID, err)
	}
	return nil
}

func buildDashboardRoutingHealthData(edgeMap map[string]*dashboardRoutingEdgeAccumulator) DashboardRoutingHealthMap {
	links := make([]DashboardRoutingLink, 0, len(edgeMap))
	for _, edge := range edgeMap {
		links = append(links, DashboardRoutingLink{
			ID:                        dashboardRoutingEdgeKey(edge.ModelID, edge.EndpointID),
			SourceNodeID:              dashboardEndpointNodeID(edge.EndpointID),
			TargetNodeID:              dashboardModelNodeID(edge.ModelConfigID),
			ModelID:                   edge.ModelID,
			ModelLabel:                edge.ModelLabel,
			ModelConfigID:             edge.ModelConfigID,
			EndpointID:                edge.EndpointID,
			EndpointLabel:             edge.EndpointLabel,
			ActiveConnectionCount:     edge.ActiveConnectionCount,
			ActiveTerminalTargetCount: edge.ActiveConnectionCount,
			TrafficRequestCount24H:    edge.TrafficRequestCount24H,
			RequestCount24H:           edge.RequestCount24H,
			SuccessCount24H:           edge.SuccessCount24H,
			ErrorCount24H:             edge.ErrorCount24H,
			SuccessRate24H:            dashboardSuccessRate(edge.SuccessCount24H, edge.RequestCount24H),
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
	activeConnectionTotal := dashboardActiveConnectionTotal(links)
	return DashboardRoutingHealthMap{
		Nodes:                     append(endpointNodes, modelNodes...),
		Links:                     links,
		EndpointCount:             len(endpointNodes),
		ModelCount:                len(modelNodes),
		ActiveConnectionTotal:     activeConnectionTotal,
		ActiveTerminalTargetTotal: activeConnectionTotal,
		TrafficRequestTotal24H:    dashboardTrafficRequestTotal(links),
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
			ID:                        dashboardEndpointNodeID(endpointID),
			Name:                      total.Label,
			Kind:                      "endpoint",
			Label:                     total.Label,
			Sublabel:                  statsStringPtr(fmt.Sprintf("Endpoint %d", endpointID)),
			EndpointID:                &endpointIDValue,
			ModelID:                   nil,
			ModelConfigID:             nil,
			ActiveConnectionCount:     total.ActiveConnectionCount,
			ActiveTerminalTargetCount: total.ActiveConnectionCount,
			TrafficRequestCount24H:    total.TrafficRequestCount24H,
			RequestCount24H:           total.RequestCount24H,
			SuccessCount24H:           total.SuccessCount24H,
			ErrorCount24H:             total.ErrorCount24H,
			SuccessRate24H:            dashboardSuccessRate(total.SuccessCount24H, total.RequestCount24H),
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
			ID:                        dashboardModelNodeID(modelConfigID),
			Name:                      total.Label,
			Kind:                      "model",
			Label:                     total.Label,
			EndpointID:                nil,
			ModelID:                   &modelID,
			ModelConfigID:             &modelConfigIDValue,
			ActiveConnectionCount:     total.ActiveConnectionCount,
			ActiveTerminalTargetCount: total.ActiveConnectionCount,
			TrafficRequestCount24H:    total.TrafficRequestCount24H,
			RequestCount24H:           total.RequestCount24H,
			SuccessCount24H:           total.SuccessCount24H,
			ErrorCount24H:             total.ErrorCount24H,
			SuccessRate24H:            dashboardSuccessRate(total.SuccessCount24H, total.RequestCount24H),
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

// cloneDashboardRoutingHealthMap returns a deep-enough copy for snapshot
// reuse. The value copy carries every scalar field; only the slices need
// fresh backing arrays. Rebuilding the struct field-by-field is what silently
// dropped ActiveTerminalTargetTotal, so never reintroduce a field list here.
func cloneDashboardRoutingHealthMap(value DashboardRoutingHealthMap) DashboardRoutingHealthMap {
	cloned := value
	cloned.Nodes = append([]DashboardRoutingNode{}, value.Nodes...)
	cloned.Links = append([]DashboardRoutingLink{}, value.Links...)
	if cloned.Nodes == nil {
		cloned.Nodes = []DashboardRoutingNode{}
	}
	if cloned.Links == nil {
		cloned.Links = []DashboardRoutingLink{}
	}
	return cloned
}

type DashboardAggregateSnapshot struct {
	ProfileID                 int
	GeneratedAt               time.Time
	SnapshotRevision          string
	SourceWatermark           DashboardSnapshotSourceWatermark
	StatsSummary24H           StatsSummaryResponse
	APIFamilySummary24H       StatsSummaryResponse
	SpendingSummary30D        SpendingReportResponse
	Throughput24H             ThroughputStatsResponse
	UsageSnapshotPreset1      UsageSnapshotResponse
	StreamRequestCount24H     int
	UsageEventRequestCount24H int
	RoutingHealthMap          DashboardRoutingHealthMap
	TotalModelCount           int
	ActiveModelCount          int
}

type DashboardSnapshot struct {
	GeneratedAt       time.Time                        `json:"generated_at"`
	SnapshotRevision  string                           `json:"snapshot_revision"`
	SourceWatermark   DashboardSnapshotSourceWatermark `json:"source_watermark"`
	Coverage24H       DashboardSnapshotCoverage        `json:"coverage_24h"`
	Coverage30D       DashboardSnapshotCoverage        `json:"coverage_30d"`
	Health            DashboardSnapshotHealth          `json:"health"`
	MetricSnapshot    DashboardMetricSnapshot          `json:"metric_snapshot"`
	APIFamilyRows     []StatGroup                      `json:"api_family_rows"`
	TopSpendingModels []SpendingTopModel               `json:"top_spending_models"`
	RoutingHealthMap  DashboardRoutingHealthMap        `json:"routing_health_map"`
}

type DashboardSnapshotSourceWatermark struct {
	LatestUsageEventCreatedAt *time.Time `json:"latest_usage_event_created_at"`
	LatestUsageEventID        *int       `json:"latest_usage_event_id"`
}

type DashboardMetricSnapshot struct {
	ActiveModels           int      `json:"active_models"`
	AverageRPM             float64  `json:"average_rpm"`
	AverageRPMRequestTotal int      `json:"average_rpm_request_total"`
	AvgLatency             *float64 `json:"avg_latency"`
	ErrorRate              *float64 `json:"error_rate"`
	P95Latency             *int     `json:"p95_latency"`
	PricedRequestCount     int      `json:"priced_request_count"`
	StreamShare            float64  `json:"stream_share"`
	SuccessRate            *float64 `json:"success_rate"`
	TotalCost              int64    `json:"total_cost"`
	TotalModels            int      `json:"total_models"`
	TotalRequests          int      `json:"total_requests"`
	UnpricedRequestCount   int      `json:"unpriced_request_count"`
}

type DashboardRoutingHealthMap struct {
	Nodes                     []DashboardRoutingNode `json:"nodes"`
	Links                     []DashboardRoutingLink `json:"links"`
	EndpointCount             int                    `json:"endpointCount"`
	ModelCount                int                    `json:"modelCount"`
	ActiveConnectionTotal     int                    `json:"activeConnectionTotal"`
	ActiveTerminalTargetTotal int                    `json:"activeTerminalTargetTotal"`
	TrafficRequestTotal24H    int                    `json:"trafficRequestTotal24h"`
}

type DashboardRoutingNode struct {
	ID                        string   `json:"id"`
	Name                      string   `json:"name"`
	Kind                      string   `json:"kind"`
	Label                     string   `json:"label"`
	Sublabel                  *string  `json:"sublabel"`
	EndpointID                *int     `json:"endpointId"`
	ModelID                   *string  `json:"modelId"`
	ModelConfigID             *int     `json:"modelConfigId"`
	ActiveConnectionCount     int      `json:"activeConnectionCount"`
	ActiveTerminalTargetCount int      `json:"activeTerminalTargetCount"`
	TrafficRequestCount24H    int      `json:"trafficRequestCount24h"`
	RequestCount24H           int      `json:"requestCount24h"`
	SuccessCount24H           int      `json:"successCount24h"`
	ErrorCount24H             int      `json:"errorCount24h"`
	SuccessRate24H            *float64 `json:"successRate24h"`
}

type DashboardRoutingLink struct {
	ID                        string   `json:"id"`
	SourceNodeID              string   `json:"sourceNodeId"`
	TargetNodeID              string   `json:"targetNodeId"`
	ModelID                   string   `json:"modelId"`
	ModelLabel                string   `json:"modelLabel"`
	ModelConfigID             int      `json:"modelConfigId"`
	EndpointID                int      `json:"endpointId"`
	EndpointLabel             string   `json:"endpointLabel"`
	ActiveConnectionCount     int      `json:"activeConnectionCount"`
	ActiveTerminalTargetCount int      `json:"activeTerminalTargetCount"`
	TrafficRequestCount24H    int      `json:"trafficRequestCount24h"`
	RequestCount24H           int      `json:"requestCount24h"`
	SuccessCount24H           int      `json:"successCount24h"`
	ErrorCount24H             int      `json:"errorCount24h"`
	SuccessRate24H            *float64 `json:"successRate24h"`
}

func cloneDashboardSnapshotSourceWatermark(value DashboardSnapshotSourceWatermark) DashboardSnapshotSourceWatermark {
	clone := DashboardSnapshotSourceWatermark{}
	if value.LatestUsageEventCreatedAt != nil {
		createdAt := value.LatestUsageEventCreatedAt.UTC()
		clone.LatestUsageEventCreatedAt = &createdAt
	}
	if value.LatestUsageEventID != nil {
		latestID := *value.LatestUsageEventID
		clone.LatestUsageEventID = &latestID
	}
	return clone
}

func NewDashboardSnapshot(aggregate DashboardAggregateSnapshot, referenceNow time.Time) DashboardSnapshot {
	generatedAt := aggregate.GeneratedAt.UTC()
	if generatedAt.IsZero() {
		generatedAt = referenceNow.UTC()
	}
	apiFamilyRows := append([]StatGroup{}, aggregate.APIFamilySummary24H.Groups...)
	topSpendingModels := append([]SpendingTopModel{}, aggregate.SpendingSummary30D.TopSpendingModels...)
	return DashboardSnapshot{
		GeneratedAt:       generatedAt,
		SnapshotRevision:  aggregate.SnapshotRevision,
		SourceWatermark:   cloneDashboardSnapshotSourceWatermark(aggregate.SourceWatermark),
		Coverage24H:       DashboardSnapshotCoverage{From: generatedAt.Add(-24 * time.Hour), To: generatedAt},
		Coverage30D:       DashboardSnapshotCoverage{From: generatedAt.Add(-30 * 24 * time.Hour), To: generatedAt},
		Health:            NewDashboardSnapshotHealth(generatedAt, referenceNow),
		MetricSnapshot:    newDashboardMetricSnapshot(aggregate),
		APIFamilyRows:     apiFamilyRows,
		TopSpendingModels: topSpendingModels,
		RoutingHealthMap:  cloneDashboardRoutingHealthMap(aggregate.RoutingHealthMap),
	}
}

func newDashboardMetricSnapshot(aggregate DashboardAggregateSnapshot) DashboardMetricSnapshot {
	streamShare := 0.0
	if aggregate.UsageEventRequestCount24H > 0 {
		streamShare = roundFloat((float64(aggregate.StreamRequestCount24H)/float64(aggregate.UsageEventRequestCount24H))*100, 2)
	}
	snapshot := DashboardMetricSnapshot{
		ActiveModels:           aggregate.ActiveModelCount,
		AverageRPM:             aggregate.Throughput24H.AverageRPM,
		AverageRPMRequestTotal: aggregate.Throughput24H.TotalRequests,
		PricedRequestCount:     aggregate.SpendingSummary30D.Summary.PricedRequestCount,
		StreamShare:            streamShare,
		TotalCost:              aggregate.SpendingSummary30D.Summary.TotalCostMicros,
		TotalModels:            aggregate.TotalModelCount,
		TotalRequests:          aggregate.StatsSummary24H.TotalRequests,
		UnpricedRequestCount:   aggregate.SpendingSummary30D.Summary.UnpricedRequestCount,
	}
	if total := aggregate.StatsSummary24H.TotalRequests; total > 0 {
		successRate := aggregate.StatsSummary24H.SuccessRate
		// Error rate comes straight from the counts instead of 100 minus
		// success rate: the subtraction would turn "no samples" into "100%
		// failed" and would carry the success-rate rounding drift.
		errorRate := roundFloat(float64(aggregate.StatsSummary24H.ErrorCount)/float64(total)*100, 2)
		avgLatency := aggregate.StatsSummary24H.AvgResponseTimeMS
		p95 := aggregate.StatsSummary24H.P95ResponseTimeMS
		snapshot.SuccessRate, snapshot.ErrorRate = &successRate, &errorRate
		snapshot.AvgLatency, snapshot.P95Latency = &avgLatency, &p95
	}
	return snapshot
}

func BuildDashboardAggregateSnapshot(ctx context.Context, exec queryExecutor, profileID int, referenceNow time.Time) (DashboardAggregateSnapshot, error) {
	generatedAt := referenceNow.UTC()
	windowStart24H := generatedAt.Add(-24 * time.Hour)
	apiFamilyGroupBy := "api_family"
	models, err := loadDashboardSnapshotModels(ctx, exec, profileID)
	if err != nil {
		return DashboardAggregateSnapshot{}, err
	}
	statsSummary, err := GetStatsSummary(ctx, exec, StatsSummaryParams{ProfileID: profileID, FromTime: &windowStart24H, ToTime: &generatedAt})
	if err != nil {
		return DashboardAggregateSnapshot{}, err
	}
	apiFamilySummary, err := GetStatsSummary(ctx, exec, StatsSummaryParams{ProfileID: profileID, FromTime: &windowStart24H, ToTime: &generatedAt, GroupBy: &apiFamilyGroupBy})
	if err != nil {
		return DashboardAggregateSnapshot{}, err
	}
	spendingSummary, err := GetSpending(ctx, exec, SpendingParams{ProfileID: profileID, Preset: "last_30_days", ToTime: &generatedAt, GroupBy: "none", Limit: 50, Offset: 0, TopN: 5, ReferenceNow: generatedAt})
	if err != nil {
		return DashboardAggregateSnapshot{}, err
	}
	throughput, err := GetDashboardThroughput(ctx, exec, ThroughputParams{ProfileID: profileID, FromTime: &windowStart24H, ToTime: &generatedAt})
	if err != nil {
		return DashboardAggregateSnapshot{}, err
	}
	usageSnapshot, err := GetUsageSnapshot(ctx, exec, profileID, "1h", generatedAt)
	if err != nil {
		return DashboardAggregateSnapshot{}, err
	}
	streamRequestCount, usageEventRequestCount, err := loadDashboardStreamRequestCounts(ctx, exec, profileID, windowStart24H, generatedAt)
	if err != nil {
		return DashboardAggregateSnapshot{}, err
	}
	sourceWatermark, err := loadDashboardSnapshotSourceWatermark(ctx, exec, profileID, generatedAt)
	if err != nil {
		return DashboardAggregateSnapshot{}, err
	}
	routingHealthMap, err := buildDashboardRoutingHealthMap(ctx, exec, profileID, models, windowStart24H, generatedAt)
	if err != nil {
		return DashboardAggregateSnapshot{}, err
	}
	return DashboardAggregateSnapshot{
		ProfileID:                 profileID,
		GeneratedAt:               generatedAt,
		SnapshotRevision:          newDashboardSnapshotRevision(generatedAt),
		SourceWatermark:           sourceWatermark,
		StatsSummary24H:           statsSummary,
		APIFamilySummary24H:       apiFamilySummary,
		SpendingSummary30D:        spendingSummary,
		Throughput24H:             throughput,
		UsageSnapshotPreset1:      usageSnapshot,
		StreamRequestCount24H:     streamRequestCount,
		UsageEventRequestCount24H: usageEventRequestCount,
		RoutingHealthMap:          routingHealthMap,
		TotalModelCount:           len(models),
		ActiveModelCount:          countDashboardActiveModels(models),
	}, nil
}

func loadDashboardStreamRequestCounts(ctx context.Context, exec queryExecutor, profileID int, fromTime time.Time, toTime time.Time) (int, int, error) {
	var streamCount int64
	var totalCount int64
	if err := exec.QueryRow(ctx, `SELECT
		COUNT(*) FILTER (WHERE COALESCE(NULLIF(stream_outcome, ''), 'not_streaming') <> 'not_streaming'),
		COUNT(*)
		FROM usage_request_events
		WHERE profile_id = $1 AND created_at >= $2 AND created_at < $3`, profileID, fromTime.UTC(), toTime.UTC()).Scan(&streamCount, &totalCount); err != nil {
		return 0, 0, fmt.Errorf("query dashboard usage-event stream request counts for profile %d: %w", profileID, err)
	}
	return int(streamCount), int(totalCount), nil
}

func loadDashboardSnapshotSourceWatermark(ctx context.Context, exec queryExecutor, profileID int, generatedAt time.Time) (DashboardSnapshotSourceWatermark, error) {
	var createdAt sql.NullTime
	var id sql.NullInt64
	if err := exec.QueryRow(ctx, `SELECT
		(SELECT created_at FROM usage_request_events WHERE profile_id = $1 AND created_at <= $2 ORDER BY created_at DESC, id DESC LIMIT 1),
		(SELECT id FROM usage_request_events WHERE profile_id = $1 AND created_at <= $2 ORDER BY created_at DESC, id DESC LIMIT 1)`, profileID, generatedAt.UTC()).Scan(&createdAt, &id); err != nil {
		return DashboardSnapshotSourceWatermark{}, fmt.Errorf("query dashboard source watermark for profile %d: %w", profileID, err)
	}
	watermark := DashboardSnapshotSourceWatermark{}
	if createdAt.Valid {
		latestUsageEventCreatedAt := createdAt.Time.UTC()
		watermark.LatestUsageEventCreatedAt = &latestUsageEventCreatedAt
	}
	if id.Valid {
		latestUsageEventID := int(id.Int64)
		watermark.LatestUsageEventID = &latestUsageEventID
	}
	return watermark, nil
}

func statsStringPtr(value string) *string {
	resolved := value
	return &resolved
}
