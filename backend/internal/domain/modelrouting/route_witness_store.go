package modelrouting

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/coachpo/prism/backend/internal/domain/terminaltarget"

	"github.com/jackc/pgx/v5"
)

// RouteWitnessExecutor is the minimal DB surface the route-witness graph
// loader needs (HTTP-neutral; the management layers pass their tx/exec).
type RouteWitnessExecutor interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

// LoadRouteWitnessGraph loads the static routing graph for one profile in one
// bounded batch (no per-model N+1). Connection targets JOIN endpoints so a
// connection whose endpoint was deleted never appears: an existing Endpoint is
// a hard witness requirement. Deleted/soft-deleted rows are excluded the same
// way the model/endpoint owners exclude them.
func LoadRouteWitnessGraph(ctx context.Context, exec RouteWitnessExecutor, profileID int) (*DiagnosticsGraph, error) {
	graph := &DiagnosticsGraph{
		ModelsByID:                   map[int]DiagnosticsModel{},
		AccessTargetsBySourceModelID: map[int][]DiagnosticsAccessTarget{},
		ConnectionsByID:              map[int]DiagnosticsConnection{},
		StrategiesByModelID:          map[int]DiagnosticsStrategy{},
	}

	modelRows, err := exec.Query(ctx, `SELECT id, profile_id, api_family, model_id, loadbalance_strategy_id, openai_accepted_format, openai_image_operations, is_enabled
		FROM model_configs WHERE profile_id = $1 ORDER BY id ASC`, profileID)
	if err != nil {
		return nil, fmt.Errorf("query route-witness models for profile %d: %w", profileID, err)
	}
	defer modelRows.Close()
	for modelRows.Next() {
		var model DiagnosticsModel
		if err := modelRows.Scan(&model.ConfigID, &model.ProfileID, &model.APIFamily, &model.ModelID, &model.LoadbalanceStrategyID, &model.OpenAIAcceptedFormat, &model.OpenAIImageOperations, &model.IsEnabled); err != nil {
			return nil, fmt.Errorf("scan route-witness model for profile %d: %w", profileID, err)
		}
		graph.ModelsByID[model.ConfigID] = model
	}
	if err := modelRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate route-witness models for profile %d: %w", profileID, err)
	}

	strategyRows, err := exec.Query(ctx, `SELECT id, legacy_strategy_type FROM loadbalance_strategies WHERE profile_id = $1 ORDER BY id ASC`, profileID)
	if err != nil {
		return nil, fmt.Errorf("query route-witness strategies for profile %d: %w", profileID, err)
	}
	defer strategyRows.Close()
	for strategyRows.Next() {
		var strategy DiagnosticsStrategy
		if err := strategyRows.Scan(&strategy.ID, &strategy.Subtype); err != nil {
			return nil, fmt.Errorf("scan route-witness strategy for profile %d: %w", profileID, err)
		}
		graph.StrategiesByModelID[strategy.ID] = strategy
	}
	if err := strategyRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate route-witness strategies for profile %d: %w", profileID, err)
	}

	modelTargetRows, err := exec.Query(ctx, `SELECT mat.id, mat.profile_id, mat.source_model_config_id, mat.target_model_config_id, mat.position, mat.is_enabled,
		tm.id, tm.api_family, tm.model_id, tm.loadbalance_strategy_id, tm.openai_accepted_format, tm.openai_image_operations, tm.is_enabled
		FROM model_access_targets mat
		JOIN model_configs tm ON tm.id = mat.target_model_config_id
		WHERE mat.profile_id = $1 AND mat.target_model_config_id IS NOT NULL
		ORDER BY mat.source_model_config_id ASC, mat.position ASC, mat.id ASC`, profileID)
	if err != nil {
		return nil, fmt.Errorf("query route-witness model targets for profile %d: %w", profileID, err)
	}
	defer modelTargetRows.Close()
	for modelTargetRows.Next() {
		var target DiagnosticsAccessTarget
		var child DiagnosticsModel
		if err := modelTargetRows.Scan(&target.ID, &target.ProfileID, &target.SourceModelConfigID, &target.TargetModelConfigID, &target.Position, &target.IsEnabled,
			&child.ConfigID, &child.APIFamily, &child.ModelID, &child.LoadbalanceStrategyID, &child.OpenAIAcceptedFormat, &child.OpenAIImageOperations, &child.IsEnabled); err != nil {
			return nil, fmt.Errorf("scan route-witness model target for profile %d: %w", profileID, err)
		}
		target.TargetType = TargetTypeModel
		graph.AccessTargetsBySourceModelID[target.SourceModelConfigID] = append(graph.AccessTargetsBySourceModelID[target.SourceModelConfigID], target)
		if _, exists := graph.ModelsByID[child.ConfigID]; !exists {
			child.ProfileID = profileID
			graph.ModelsByID[child.ConfigID] = child
		}
	}
	if err := modelTargetRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate route-witness model targets for profile %d: %w", profileID, err)
	}

	connectionTargetRows, err := exec.Query(ctx, `SELECT mat.id, mat.profile_id, mat.source_model_config_id, mat.target_connection_id, mat.position, mat.is_enabled,
		c.id, c.profile_id, c.api_family, c.endpoint_id, c.is_active, c.openai_text_capability, c.openai_image_capability, c.routing_schedule_timezone
		FROM model_access_targets mat
		JOIN connections c ON c.id = mat.target_connection_id
		JOIN endpoints e ON e.id = c.endpoint_id
		WHERE mat.profile_id = $1 AND mat.target_connection_id IS NOT NULL
		ORDER BY mat.source_model_config_id ASC, mat.position ASC, mat.id ASC`, profileID)
	if err != nil {
		return nil, fmt.Errorf("query route-witness connection targets for profile %d: %w", profileID, err)
	}
	defer connectionTargetRows.Close()
	for connectionTargetRows.Next() {
		var target DiagnosticsAccessTarget
		var connectionID sql.NullInt64
		var connection DiagnosticsConnection
		if err := connectionTargetRows.Scan(&target.ID, &target.ProfileID, &target.SourceModelConfigID, &connectionID, &target.Position, &target.IsEnabled,
			&connection.ID, &connection.ProfileID, &connection.APIFamily, &connection.EndpointID, &connection.IsActive, &connection.OpenAITextCapability, &connection.OpenAIImageCapability, &connection.RoutingScheduleTimezone); err != nil {
			return nil, fmt.Errorf("scan route-witness connection target for profile %d: %w", profileID, err)
		}
		if !connectionID.Valid {
			continue
		}
		target.TargetType = TargetTypeTerminal
		target.TargetConnectionID = &connection.ID
		graph.AccessTargetsBySourceModelID[target.SourceModelConfigID] = append(graph.AccessTargetsBySourceModelID[target.SourceModelConfigID], target)
		if _, exists := graph.ConnectionsByID[connection.ID]; !exists {
			graph.ConnectionsByID[connection.ID] = connection
		}
	}
	if err := connectionTargetRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate route-witness connection targets for profile %d: %w", profileID, err)
	}
	connectionTargetRows.Close()

	// Fifth query: without it the witness graph would carry a timezone and no
	// windows, the schedule projection would read as unrestricted, and the
	// readiness card would keep claiming unconditional readiness. No existing
	// assertion covers that, so the omission would be silent.
	windowRows, err := exec.Query(ctx, `SELECT connection_id, weekday_mask, start_minute, end_minute FROM connection_routing_windows WHERE profile_id = $1 ORDER BY connection_id ASC, weekday_mask ASC, start_minute ASC, end_minute ASC`, profileID)
	if err != nil {
		return nil, fmt.Errorf("query route-witness routing windows for profile %d: %w", profileID, err)
	}
	defer windowRows.Close()
	for windowRows.Next() {
		var connectionID int
		var window terminaltarget.Window
		if err := windowRows.Scan(&connectionID, &window.WeekdayMask, &window.StartMinute, &window.EndMinute); err != nil {
			return nil, fmt.Errorf("scan route-witness routing window for profile %d: %w", profileID, err)
		}
		connection, exists := graph.ConnectionsByID[connectionID]
		if !exists {
			continue
		}
		connection.RoutingWindows = append(connection.RoutingWindows, window)
		graph.ConnectionsByID[connectionID] = connection
	}
	if err := windowRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate route-witness routing windows for profile %d: %w", profileID, err)
	}

	return graph, nil
}
