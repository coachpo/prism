package stats

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"
)

type DashboardTopologyGraph struct {
	Nodes []DashboardTopologyNode `json:"nodes"`
	Edges []DashboardTopologyEdge `json:"edges"`
	Stats DashboardTopologyStats  `json:"stats"`
}

type DashboardTopologyStats struct {
	ModelCount                  int `json:"model_count"`
	ActiveModelCount            int `json:"active_model_count"`
	DisabledModelCount          int `json:"disabled_model_count"`
	TerminalTargetCount         int `json:"terminal_target_count"`
	ActiveTerminalTargetCount   int `json:"active_terminal_target_count"`
	InactiveTerminalTargetCount int `json:"inactive_terminal_target_count"`
	EndpointCount               int `json:"endpoint_count"`
	EdgeCount                   int `json:"edge_count"`
}

type DashboardTopologyNode struct {
	ID                 string     `json:"id"`
	Kind               string     `json:"kind"`
	ProductKind        *string    `json:"product_kind,omitempty"`
	Label              string     `json:"label"`
	Sublabel           *string    `json:"sublabel,omitempty"`
	Status             string     `json:"status"`
	ModelConfigID      *int       `json:"model_config_id,omitempty"`
	ModelID            *string    `json:"model_id,omitempty"`
	TerminalTargetID   *int       `json:"terminal_target_id,omitempty"`
	ConnectionID       *int       `json:"connection_id,omitempty"`
	EndpointID         *int       `json:"endpoint_id,omitempty"`
	Active             *bool      `json:"active,omitempty"`
	RecentRequestCount *int       `json:"recent_request_count,omitempty"`
	RecentSuccessRate  *float64   `json:"recent_success_rate,omitempty"`
	LastRequestAt      *time.Time `json:"last_request_at,omitempty"`
}

type DashboardTopologyEdge struct {
	ID                  string  `json:"id"`
	Kind                string  `json:"kind"`
	ProductKind         *string `json:"product_kind,omitempty"`
	SourceNodeID        string  `json:"source_node_id"`
	TargetNodeID        string  `json:"target_node_id"`
	Position            *int    `json:"position,omitempty"`
	Enabled             *bool   `json:"enabled,omitempty"`
	SourceModelConfigID *int    `json:"source_model_config_id,omitempty"`
	SourceModelID       *string `json:"source_model_id,omitempty"`
	TargetModelConfigID *int    `json:"target_model_config_id,omitempty"`
	TargetModelID       *string `json:"target_model_id,omitempty"`
	TerminalTargetID    *int    `json:"terminal_target_id,omitempty"`
	ConnectionID        *int    `json:"connection_id,omitempty"`
	EndpointID          *int    `json:"endpoint_id,omitempty"`
}

type dashboardTopologyAccessTarget struct {
	ID                     int
	SourceModelConfigID    int
	TargetType             string
	TargetModelConfigID    *int
	TargetConnectionID     *int
	Position               int
	IsEnabled              bool
	TargetModelID          *string
	TargetModelDisplayName *string
	TargetConnectionName   *string
	TargetConnectionActive *bool
	TargetEndpointID       *int
	TargetEndpointName     *string
	TargetEndpointBaseURL  *string
}

type dashboardTopologyConnectionTelemetry struct {
	RecentRequestCount int
	RecentSuccessCount int
	LastRequestAt      *time.Time
}

func buildDashboardTopologyGraph(ctx context.Context, exec queryExecutor, profileID int, models []dashboardSnapshotModel, fromTime time.Time, toTime time.Time) (DashboardTopologyGraph, error) {
	accessTargets, err := loadDashboardTopologyAccessTargets(ctx, exec, profileID)
	if err != nil {
		return DashboardTopologyGraph{}, err
	}
	telemetryByConnection, err := loadDashboardTopologyConnectionTelemetry(ctx, exec, profileID, fromTime, toTime)
	if err != nil {
		return DashboardTopologyGraph{}, err
	}

	modelByID := make(map[int]dashboardSnapshotModel, len(models))
	nodesByID := make(map[string]DashboardTopologyNode)
	edges := make([]DashboardTopologyEdge, 0, len(accessTargets)*2)
	for _, model := range models {
		modelByID[model.ID] = model
		node := newDashboardTopologyModelNode(model)
		nodesByID[node.ID] = node
	}
	for _, accessTarget := range accessTargets {
		sourceModel, ok := modelByID[accessTarget.SourceModelConfigID]
		if !ok {
			continue
		}
		switch accessTarget.TargetType {
		case "model":
			targetModelConfigID := intValue(accessTarget.TargetModelConfigID)
			if targetModelConfigID <= 0 {
				continue
			}
			targetModel, found := modelByID[targetModelConfigID]
			if !found {
				targetModel = dashboardSnapshotModel{ID: targetModelConfigID, ModelID: strings.TrimSpace(stringValue(accessTarget.TargetModelID)), DisplayName: normalizeOptionalString(accessTarget.TargetModelDisplayName), IsEnabled: true}
			}
			node := newDashboardTopologyModelNode(targetModel)
			nodesByID[node.ID] = node
			edges = append(edges, newDashboardTopologyModelEdge(sourceModel, accessTarget, node.ID))
		case "connection":
			terminalTargetID := intValue(accessTarget.TargetConnectionID)
			endpointID := intValue(accessTarget.TargetEndpointID)
			if terminalTargetID <= 0 || endpointID <= 0 {
				continue
			}
			telemetry := telemetryByConnection[terminalTargetID]
			terminalNode := newDashboardTopologyTerminalTargetNode(accessTarget, telemetry)
			endpointNode := newDashboardTopologyEndpointNode(accessTarget)
			nodesByID[terminalNode.ID] = terminalNode
			nodesByID[endpointNode.ID] = endpointNode
			edges = append(edges, newDashboardTopologyTerminalRouteEdge(sourceModel, accessTarget, terminalNode.ID))
			edges = append(edges, newDashboardTopologyEndpointBindingEdge(accessTarget, terminalNode.ID, endpointNode.ID))
		}
	}

	nodes := make([]DashboardTopologyNode, 0, len(nodesByID))
	for _, node := range nodesByID {
		nodes = append(nodes, node)
	}
	sort.Slice(nodes, func(i int, j int) bool {
		return dashboardTopologyNodeLess(nodes[i], nodes[j])
	})
	sort.Slice(edges, func(i int, j int) bool {
		return dashboardTopologyEdgeLess(edges[i], edges[j])
	})
	return DashboardTopologyGraph{Nodes: nodes, Edges: edges, Stats: newDashboardTopologyStats(nodes, edges)}, nil
}

func loadDashboardTopologyAccessTargets(ctx context.Context, exec queryExecutor, profileID int) ([]dashboardTopologyAccessTarget, error) {
	rows, err := exec.Query(ctx, `SELECT model_access_targets.id,
		model_access_targets.source_model_config_id,
		model_access_targets.target_type,
		model_access_targets.target_model_config_id,
		model_access_targets.target_connection_id,
		model_access_targets.position,
		model_access_targets.is_enabled,
		target_models.model_id,
		target_models.display_name,
		connections.name,
		connections.is_active,
		endpoints.id,
		endpoints.name,
		endpoints.base_url
		FROM model_access_targets
		LEFT JOIN model_configs AS target_models ON target_models.id = model_access_targets.target_model_config_id AND target_models.profile_id = model_access_targets.profile_id
		LEFT JOIN connections ON connections.id = model_access_targets.target_connection_id AND connections.profile_id = model_access_targets.profile_id
		LEFT JOIN endpoints ON endpoints.id = connections.endpoint_id AND endpoints.profile_id = connections.profile_id
		WHERE model_access_targets.profile_id = $1
		ORDER BY model_access_targets.source_model_config_id ASC, model_access_targets.position ASC, model_access_targets.id ASC`, profileID)
	if err != nil {
		return nil, fmt.Errorf("query dashboard topology access targets for profile %d: %w", profileID, err)
	}
	defer rows.Close()

	items := make([]dashboardTopologyAccessTarget, 0)
	for rows.Next() {
		item := dashboardTopologyAccessTarget{}
		var targetModelConfigID sql.NullInt32
		var targetConnectionID sql.NullInt32
		var targetModelID sql.NullString
		var targetModelDisplayName sql.NullString
		var targetConnectionName sql.NullString
		var targetConnectionActive sql.NullBool
		var targetEndpointID sql.NullInt32
		var targetEndpointName sql.NullString
		var targetEndpointBaseURL sql.NullString
		if err := rows.Scan(&item.ID, &item.SourceModelConfigID, &item.TargetType, &targetModelConfigID, &targetConnectionID, &item.Position, &item.IsEnabled, &targetModelID, &targetModelDisplayName, &targetConnectionName, &targetConnectionActive, &targetEndpointID, &targetEndpointName, &targetEndpointBaseURL); err != nil {
			return nil, fmt.Errorf("scan dashboard topology access target: %w", err)
		}
		item.TargetModelConfigID = nullableInt32(targetModelConfigID)
		item.TargetConnectionID = nullableInt32(targetConnectionID)
		item.TargetModelID = normalizeOptionalString(nullableString(targetModelID))
		item.TargetModelDisplayName = normalizeOptionalString(nullableString(targetModelDisplayName))
		item.TargetConnectionName = normalizeOptionalString(nullableString(targetConnectionName))
		item.TargetConnectionActive = nullableBool(targetConnectionActive)
		item.TargetEndpointID = nullableInt32(targetEndpointID)
		item.TargetEndpointName = normalizeOptionalString(nullableString(targetEndpointName))
		item.TargetEndpointBaseURL = normalizeOptionalString(nullableString(targetEndpointBaseURL))
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate dashboard topology access targets for profile %d: %w", profileID, err)
	}
	return items, nil
}

func loadDashboardTopologyConnectionTelemetry(ctx context.Context, exec queryExecutor, profileID int, fromTime time.Time, toTime time.Time) (map[int]dashboardTopologyConnectionTelemetry, error) {
	rows, err := exec.Query(ctx, `SELECT connection_id,
		COUNT(*) FILTER (WHERE created_at >= $2 AND created_at <= $3) AS recent_request_count,
		COALESCE(SUM(CASE WHEN created_at >= $2 AND created_at <= $3 AND success_flag THEN 1 ELSE 0 END), 0) AS recent_success_count,
		MAX(created_at) AS last_request_at
		FROM usage_request_events
		WHERE profile_id = $1 AND connection_id IS NOT NULL AND created_at <= $3
		GROUP BY connection_id`, profileID, fromTime.UTC(), toTime.UTC())
	if err != nil {
		return nil, fmt.Errorf("query dashboard topology telemetry for profile %d: %w", profileID, err)
	}
	defer rows.Close()

	items := make(map[int]dashboardTopologyConnectionTelemetry)
	for rows.Next() {
		var connectionID int
		var recentRequestCount int
		var recentSuccessCount int
		var lastRequestAt sql.NullTime
		if err := rows.Scan(&connectionID, &recentRequestCount, &recentSuccessCount, &lastRequestAt); err != nil {
			return nil, fmt.Errorf("scan dashboard topology telemetry: %w", err)
		}
		items[connectionID] = dashboardTopologyConnectionTelemetry{
			RecentRequestCount: recentRequestCount,
			RecentSuccessCount: recentSuccessCount,
			LastRequestAt:      nullableTime(lastRequestAt),
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate dashboard topology telemetry for profile %d: %w", profileID, err)
	}
	return items, nil
}

func newDashboardTopologyModelNode(model dashboardSnapshotModel) DashboardTopologyNode {
	modelID := strings.TrimSpace(model.ModelID)
	modelConfigID := model.ID
	status := "enabled"
	if !model.IsEnabled {
		status = "disabled"
	}
	node := DashboardTopologyNode{ID: dashboardTopologyModelNodeID(model.ID), Kind: "model", Label: dashboardModelLabel(model), Status: status, ModelConfigID: &modelConfigID, ModelID: &modelID}
	if node.Label != modelID {
		node.Sublabel = &modelID
	}
	return node
}

func newDashboardTopologyTerminalTargetNode(accessTarget dashboardTopologyAccessTarget, telemetry dashboardTopologyConnectionTelemetry) DashboardTopologyNode {
	terminalTargetID := intValue(accessTarget.TargetConnectionID)
	active := true
	if accessTarget.TargetConnectionActive != nil {
		active = *accessTarget.TargetConnectionActive
	}
	status := "active"
	if !active {
		status = "inactive"
	}
	label := dashboardTopologyTerminalTargetLabel(accessTarget.TargetConnectionName, terminalTargetID)
	sublabel := dashboardTopologyTerminalTargetSublabel(accessTarget)
	node := DashboardTopologyNode{ID: dashboardTopologyTerminalTargetNodeID(terminalTargetID), Kind: "connection", ProductKind: statsStringPtr("terminal_target"), Label: label, Status: status, TerminalTargetID: &terminalTargetID, ConnectionID: &terminalTargetID, Active: statsBoolPtr(active), RecentRequestCount: statsIntPtr(telemetry.RecentRequestCount), LastRequestAt: telemetry.LastRequestAt}
	if sublabel != nil {
		node.Sublabel = sublabel
	}
	if telemetry.RecentRequestCount > 0 {
		rate := successRate(telemetry.RecentSuccessCount, telemetry.RecentRequestCount)
		node.RecentSuccessRate = &rate
	}
	return node
}

func newDashboardTopologyEndpointNode(accessTarget dashboardTopologyAccessTarget) DashboardTopologyNode {
	endpointID := intValue(accessTarget.TargetEndpointID)
	label := resolveEndpointLabel(accessTarget.TargetEndpointName, accessTarget.TargetEndpointBaseURL, nil, accessTarget.TargetEndpointID, fmt.Sprintf("Endpoint %d", endpointID))
	return DashboardTopologyNode{ID: dashboardTopologyEndpointNodeID(endpointID), Kind: "endpoint", Label: label, Sublabel: statsStringPtr(fmt.Sprintf("Endpoint %d", endpointID)), Status: "configured", EndpointID: &endpointID}
}

func newDashboardTopologyModelEdge(sourceModel dashboardSnapshotModel, accessTarget dashboardTopologyAccessTarget, targetNodeID string) DashboardTopologyEdge {
	sourceModelConfigID := sourceModel.ID
	sourceModelID := strings.TrimSpace(sourceModel.ModelID)
	targetModelConfigID := accessTarget.TargetModelConfigID
	targetModelID := normalizeOptionalString(accessTarget.TargetModelID)
	return DashboardTopologyEdge{ID: dashboardTopologyAccessTargetEdgeID(accessTarget.ID), Kind: "model_to_model", SourceNodeID: dashboardTopologyModelNodeID(sourceModel.ID), TargetNodeID: targetNodeID, Position: statsIntPtr(accessTarget.Position), Enabled: statsBoolPtr(accessTarget.IsEnabled), SourceModelConfigID: &sourceModelConfigID, SourceModelID: &sourceModelID, TargetModelConfigID: targetModelConfigID, TargetModelID: targetModelID}
}

func newDashboardTopologyTerminalRouteEdge(sourceModel dashboardSnapshotModel, accessTarget dashboardTopologyAccessTarget, targetNodeID string) DashboardTopologyEdge {
	sourceModelConfigID := sourceModel.ID
	sourceModelID := strings.TrimSpace(sourceModel.ModelID)
	terminalTargetID := accessTarget.TargetConnectionID
	return DashboardTopologyEdge{ID: dashboardTopologyAccessTargetEdgeID(accessTarget.ID), Kind: "model_to_connection", ProductKind: statsStringPtr("model_to_terminal_target"), SourceNodeID: dashboardTopologyModelNodeID(sourceModel.ID), TargetNodeID: targetNodeID, Position: statsIntPtr(accessTarget.Position), Enabled: statsBoolPtr(accessTarget.IsEnabled), SourceModelConfigID: &sourceModelConfigID, SourceModelID: &sourceModelID, TerminalTargetID: terminalTargetID, ConnectionID: terminalTargetID}
}

func newDashboardTopologyEndpointBindingEdge(accessTarget dashboardTopologyAccessTarget, sourceNodeID string, targetNodeID string) DashboardTopologyEdge {
	terminalTargetID := accessTarget.TargetConnectionID
	endpointID := accessTarget.TargetEndpointID
	return DashboardTopologyEdge{ID: dashboardTopologyBindingEdgeID(intValue(accessTarget.TargetConnectionID)), Kind: "connection_to_endpoint", ProductKind: statsStringPtr("terminal_target_to_endpoint"), SourceNodeID: sourceNodeID, TargetNodeID: targetNodeID, TerminalTargetID: terminalTargetID, ConnectionID: terminalTargetID, EndpointID: endpointID}
}

func newDashboardTopologyStats(nodes []DashboardTopologyNode, edges []DashboardTopologyEdge) DashboardTopologyStats {
	stats := DashboardTopologyStats{EdgeCount: len(edges)}
	for _, node := range nodes {
		switch node.Kind {
		case "model":
			stats.ModelCount++
			if node.Status == "disabled" {
				stats.DisabledModelCount++
			} else {
				stats.ActiveModelCount++
			}
		case "connection", "terminal_target":
			stats.TerminalTargetCount++
			if node.Active != nil && !*node.Active {
				stats.InactiveTerminalTargetCount++
			} else {
				stats.ActiveTerminalTargetCount++
			}
		case "endpoint":
			stats.EndpointCount++
		}
	}
	return stats
}

func cloneDashboardTopologyGraph(value DashboardTopologyGraph) DashboardTopologyGraph {
	nodes := append([]DashboardTopologyNode{}, value.Nodes...)
	edges := append([]DashboardTopologyEdge{}, value.Edges...)
	if nodes == nil {
		nodes = []DashboardTopologyNode{}
	}
	if edges == nil {
		edges = []DashboardTopologyEdge{}
	}
	return DashboardTopologyGraph{Nodes: nodes, Edges: edges, Stats: value.Stats}
}

func dashboardTopologyModelNodeID(modelConfigID int) string {
	return fmt.Sprintf("model-%d", modelConfigID)
}

func dashboardTopologyTerminalTargetNodeID(terminalTargetID int) string {
	return fmt.Sprintf("terminal-target-%d", terminalTargetID)
}

func dashboardTopologyEndpointNodeID(endpointID int) string {
	return fmt.Sprintf("endpoint-%d", endpointID)
}

func dashboardTopologyAccessTargetEdgeID(accessTargetID int) string {
	return fmt.Sprintf("access-target-%d", accessTargetID)
}

func dashboardTopologyBindingEdgeID(terminalTargetID int) string {
	return fmt.Sprintf("terminal-target-binding-%d", terminalTargetID)
}

func dashboardTopologyTerminalTargetLabel(name *string, terminalTargetID int) string {
	if name != nil && strings.TrimSpace(*name) != "" {
		return strings.TrimSpace(*name)
	}
	return fmt.Sprintf("Terminal target %d", terminalTargetID)
}

func dashboardTopologyTerminalTargetSublabel(accessTarget dashboardTopologyAccessTarget) *string {
	endpointLabel := resolveEndpointLabel(accessTarget.TargetEndpointName, accessTarget.TargetEndpointBaseURL, nil, accessTarget.TargetEndpointID, "")
	if strings.TrimSpace(endpointLabel) == "" {
		return nil
	}
	return statsStringPtr(endpointLabel)
}

func dashboardTopologyNodeLess(left DashboardTopologyNode, right DashboardTopologyNode) bool {
	if left.Kind != right.Kind {
		return dashboardTopologyKindRank(left.Kind) < dashboardTopologyKindRank(right.Kind)
	}
	return dashboardTopologyNodeNumericID(left) < dashboardTopologyNodeNumericID(right)
}

func dashboardTopologyEdgeLess(left DashboardTopologyEdge, right DashboardTopologyEdge) bool {
	if left.Kind != right.Kind {
		return dashboardTopologyEdgeKindRank(left.Kind) < dashboardTopologyEdgeKindRank(right.Kind)
	}
	if left.SourceNodeID != right.SourceNodeID {
		return left.SourceNodeID < right.SourceNodeID
	}
	if left.Position != nil && right.Position != nil && *left.Position != *right.Position {
		return *left.Position < *right.Position
	}
	return left.ID < right.ID
}

func dashboardTopologyKindRank(kind string) int {
	switch kind {
	case "model":
		return 0
	case "connection", "terminal_target":
		return 1
	case "endpoint":
		return 2
	default:
		return 3
	}
}

func dashboardTopologyEdgeKindRank(kind string) int {
	switch kind {
	case "model_to_model":
		return 0
	case "model_to_connection", "model_to_terminal_target":
		return 1
	case "connection_to_endpoint", "terminal_target_to_endpoint":
		return 2
	default:
		return 3
	}
}

func dashboardTopologyNodeNumericID(node DashboardTopologyNode) int {
	if node.ModelConfigID != nil {
		return *node.ModelConfigID
	}
	if node.TerminalTargetID != nil {
		return *node.TerminalTargetID
	}
	if node.EndpointID != nil {
		return *node.EndpointID
	}
	return 0
}

func nullableTime(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	resolved := value.Time.UTC()
	return &resolved
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func statsIntPtr(value int) *int {
	resolved := value
	return &resolved
}
