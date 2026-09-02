package models

import (
	"context"
	"database/sql"
	"fmt"
)

func listConnectionCountsByModel(ctx context.Context, exec queryExecutor, profileID int) (map[int]modelConnectionCounts, error) {
	rows, err := exec.Query(ctx, `WITH RECURSIVE terminal_reachability AS (
		SELECT model_access_targets.source_model_config_id AS root_model_config_id,
			model_access_targets.target_model_config_id AS next_model_config_id,
			model_access_targets.target_connection_id AS terminal_connection_id,
			connections.is_active AS terminal_connection_is_active,
			ARRAY[model_access_targets.source_model_config_id] || CASE WHEN model_access_targets.target_model_config_id IS NULL THEN ARRAY[]::integer[] ELSE ARRAY[model_access_targets.target_model_config_id] END AS path
		FROM model_access_targets
		LEFT JOIN connections ON connections.id = model_access_targets.target_connection_id AND connections.profile_id = model_access_targets.profile_id
		WHERE model_access_targets.profile_id = $1
			AND model_access_targets.is_enabled = TRUE
			AND (model_access_targets.target_model_config_id IS NOT NULL OR model_access_targets.target_connection_id IS NOT NULL)
		UNION ALL
		SELECT terminal_reachability.root_model_config_id,
			model_access_targets.target_model_config_id AS next_model_config_id,
			model_access_targets.target_connection_id AS terminal_connection_id,
			connections.is_active AS terminal_connection_is_active,
			terminal_reachability.path || CASE WHEN model_access_targets.target_model_config_id IS NULL THEN ARRAY[]::integer[] ELSE ARRAY[model_access_targets.target_model_config_id] END AS path
		FROM terminal_reachability
		JOIN model_access_targets ON model_access_targets.profile_id = $1 AND model_access_targets.source_model_config_id = terminal_reachability.next_model_config_id
		LEFT JOIN connections ON connections.id = model_access_targets.target_connection_id AND connections.profile_id = model_access_targets.profile_id
		WHERE terminal_reachability.next_model_config_id IS NOT NULL
			AND model_access_targets.is_enabled = TRUE
			AND (model_access_targets.target_connection_id IS NOT NULL OR (model_access_targets.target_model_config_id IS NOT NULL AND NOT model_access_targets.target_model_config_id = ANY(terminal_reachability.path)))
	)
	SELECT root_model_config_id,
		COUNT(DISTINCT terminal_connection_id) AS total_count,
		COUNT(DISTINCT terminal_connection_id) FILTER (WHERE terminal_connection_is_active) AS active_count
	FROM terminal_reachability
	WHERE terminal_connection_id IS NOT NULL
	GROUP BY root_model_config_id`, profileID)
	if err != nil {
		return nil, fmt.Errorf("query reachable model connection counts for profile %d: %w", profileID, err)
	}
	defer rows.Close()

	counts := map[int]modelConnectionCounts{}
	for rows.Next() {
		var modelID int
		var totalCount int
		var activeCount int
		if err := rows.Scan(&modelID, &totalCount, &activeCount); err != nil {
			return nil, fmt.Errorf("scan reachable model connection counts: %w", err)
		}
		counts[modelID] = modelConnectionCounts{Total: totalCount, Active: activeCount}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate reachable model connection counts: %w", err)
	}
	return counts, nil
}

func listEndpointModelRows(ctx context.Context, exec queryExecutor, profileID int, endpointIDs []int) ([]endpointModelConnectionRow, error) {
	if len(endpointIDs) == 0 {
		return []endpointModelConnectionRow{}, nil
	}
	args := []any{profileID, int32ArrayArg(endpointIDs)}
	query := `WITH RECURSIVE terminal_reachability AS (
		SELECT model_access_targets.source_model_config_id AS root_model_config_id,
			model_access_targets.target_model_config_id AS next_model_config_id,
			model_access_targets.target_connection_id AS terminal_connection_id,
			ARRAY[model_access_targets.source_model_config_id] || CASE WHEN model_access_targets.target_model_config_id IS NULL THEN ARRAY[]::integer[] ELSE ARRAY[model_access_targets.target_model_config_id] END AS path
		FROM model_access_targets
		WHERE model_access_targets.profile_id = $1
			AND model_access_targets.is_enabled = TRUE
			AND (model_access_targets.target_model_config_id IS NOT NULL OR model_access_targets.target_connection_id IS NOT NULL)
		UNION ALL
		SELECT terminal_reachability.root_model_config_id,
			model_access_targets.target_model_config_id AS next_model_config_id,
			model_access_targets.target_connection_id AS terminal_connection_id,
			terminal_reachability.path || CASE WHEN model_access_targets.target_model_config_id IS NULL THEN ARRAY[]::integer[] ELSE ARRAY[model_access_targets.target_model_config_id] END AS path
		FROM terminal_reachability
		JOIN model_access_targets ON model_access_targets.profile_id = $1 AND model_access_targets.source_model_config_id = terminal_reachability.next_model_config_id
		WHERE terminal_reachability.next_model_config_id IS NOT NULL
			AND model_access_targets.is_enabled = TRUE
			AND (model_access_targets.target_connection_id IS NOT NULL OR (model_access_targets.target_model_config_id IS NOT NULL AND NOT model_access_targets.target_model_config_id = ANY(terminal_reachability.path)))
	)
	SELECT DISTINCT connections.endpoint_id, connections.id, connections.is_active,
		source_models.id, source_models.profile_id, source_models.api_family, source_models.model_id, source_models.display_name, source_models.loadbalance_strategy_id, source_models.openai_accepted_format, source_models.openai_image_operations, source_models.direct_request_enabled, source_models.is_enabled, source_models.created_at, source_models.updated_at
	FROM terminal_reachability
	JOIN connections ON connections.id = terminal_reachability.terminal_connection_id AND connections.profile_id = $1
	JOIN model_configs AS source_models ON source_models.id = terminal_reachability.root_model_config_id AND source_models.profile_id = $1
	WHERE terminal_reachability.terminal_connection_id IS NOT NULL
		AND connections.endpoint_id = ANY($2)
		AND source_models.is_enabled = TRUE
	ORDER BY connections.endpoint_id ASC, source_models.model_id ASC, source_models.id ASC, connections.id ASC`
	rows, err := exec.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query reachable endpoint model rows for profile %d: %w", profileID, err)
	}
	defer rows.Close()

	items := make([]endpointModelConnectionRow, 0)
	for rows.Next() {
		var row endpointModelConnectionRow
		var displayName sql.NullString
		var loadbalanceStrategyID sql.NullInt32
		var openAIAcceptedFormat sql.NullString
		var openAIImageOperations sql.NullString
		if err := rows.Scan(&row.EndpointID, &row.TerminalConnectionID, &row.ConnectionIsActive, &row.ReachableModelData.ID, &row.ReachableModelData.ProfileID, &row.ReachableModelData.APIFamily, &row.ReachableModelData.ModelID, &displayName, &loadbalanceStrategyID, &openAIAcceptedFormat, &openAIImageOperations, &row.ReachableModelData.DirectRequestEnabled, &row.ReachableModelData.IsEnabled, &row.ReachableModelData.CreatedAt, &row.ReachableModelData.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan reachable endpoint model row: %w", err)
		}
		row.ReachableModelData.DisplayName = nullableStringValue(displayName)
		row.ReachableModelData.LoadbalanceStrategyID = nullableInt32(loadbalanceStrategyID)
		row.ReachableModelData.OpenAIAcceptedFormat = nullableStringValue(openAIAcceptedFormat)
		row.ReachableModelData.OpenAIImageOperations = nullableStringValue(openAIImageOperations)
		row.ReachableModelID = row.ReachableModelData.ID
		items = append(items, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate reachable endpoint model rows: %w", err)
	}
	return items, nil
}
