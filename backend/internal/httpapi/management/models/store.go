package models

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/coachpo/prism/backend/internal/domain/modelrouting"
	"github.com/coachpo/prism/backend/internal/domain/safediag"
	"github.com/coachpo/prism/backend/internal/domain/terminaltarget"
	"github.com/coachpo/prism/backend/internal/httpapi/management/connections"
	"github.com/coachpo/prism/backend/internal/providerauth"
)

type queryExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type modelRecord struct {
	ID                    int
	ProfileID             int
	APIFamily             string
	ModelID               string
	DisplayName           *string
	LoadbalanceStrategyID *int
	OpenAIAcceptedFormat  *string
	OpenAIImageOperations *string
	IsEnabled             bool
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

type accessTargetRecord struct {
	ID                  int
	ProfileID           int
	SourceModelConfigID int
	TargetType          string
	TargetModelConfigID *int
	TargetConnectionID  *int
	Position            int
	IsEnabled           bool
	TargetModel         *modelRecord
	Connection          *connectionTargetSummary
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type resolvedAccessTarget struct {
	TargetType string
	Position   int
	IsEnabled  bool
	Model      *modelRecord
	Connection *connectionTargetSummary
}

type preservedConnectionAccessTarget struct {
	ID        int
	Position  int
	IsEnabled bool
	Update    bool
}

type strategyRecord struct {
	ID                                 int
	Name                               string
	LegacyStrategyType                 string
	IsDefault                          bool
	FailureStatusCodes                 []int
	BanMode                            string
	RetryBaseDelayMS                   int
	RetryBackoffMultiplier             float64
	RetryJitterRatio                   float64
	RetryMaxDelayMS                    int
	CycleRetryAttemptLimit             int
	BanCumulativeRetryAttemptThreshold int
	BanDurationSeconds                 int
}

type modelConnectionCounts struct {
	Total  int
	Active int
}

type modelHealthStats struct {
	SuccessRate   *float64
	TotalRequests int
}

type endpointModelConnectionRow struct {
	EndpointID           int
	TerminalConnectionID int
	ConnectionIsActive   bool
	ReachableModelID     int
	ReachableModelData   modelRecord
}

const modelRecordSelectColumns = `model_configs.id, model_configs.profile_id, model_configs.api_family, model_configs.model_id, model_configs.display_name, model_configs.loadbalance_strategy_id, model_configs.openai_accepted_format, model_configs.openai_image_operations, model_configs.is_enabled, model_configs.created_at, model_configs.updated_at`

func listModelRecords(ctx context.Context, exec queryExecutor, profileID int) ([]modelRecord, error) {
	rows, err := exec.Query(ctx, `SELECT `+modelRecordSelectColumns+` FROM model_configs WHERE profile_id = $1 ORDER BY id ASC`, profileID)
	if err != nil {
		return nil, fmt.Errorf("query models for profile %d: %w", profileID, err)
	}
	defer rows.Close()

	items := make([]modelRecord, 0)
	for rows.Next() {
		record, scanErr := scanModelRecord(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate models for profile %d: %w", profileID, err)
	}
	return items, nil
}

func loadModelRecord(ctx context.Context, exec queryExecutor, profileID int, modelConfigID int, forUpdate bool) (modelRecord, bool, error) {
	query := `SELECT ` + modelRecordSelectColumns + ` FROM model_configs WHERE profile_id = $1 AND id = $2`
	if forUpdate {
		query += ` FOR UPDATE`
	}
	query += ` LIMIT 1`
	record, err := scanModelRecord(exec.QueryRow(ctx, query, profileID, modelConfigID))
	if err == pgx.ErrNoRows {
		return modelRecord{}, false, nil
	}
	if err != nil {
		return modelRecord{}, false, fmt.Errorf("load model %d in profile %d: %w", modelConfigID, profileID, err)
	}
	return record, true, nil
}

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

func listModelHealthStats(ctx context.Context, exec queryExecutor, profileID int) (map[string]modelHealthStats, error) {
	// Model health aggregates retained finalized ingress events (Observe SPEC
	// §3.2 shared classifier; Requests SPEC §6.9): total_requests counts each
	// finalized ingress once; success_count only counts final_result=completed
	// so retry/Hedge/failover never inflate model health.
	rows, err := exec.Query(ctx, `SELECT model_id, COUNT(*) AS total_requests, COALESCE(SUM(CASE WHEN (status_code BETWEEN 200 AND 299 AND stream_outcome IN ('not_streaming','completed')) THEN 1 ELSE 0 END), 0) AS success_count FROM usage_request_events WHERE profile_id = $1 GROUP BY model_id`, profileID)
	if err != nil {
		return nil, fmt.Errorf("query model health stats for profile %d: %w", profileID, err)
	}
	defer rows.Close()

	stats := map[string]modelHealthStats{}
	for rows.Next() {
		var modelID string
		var totalRequests int
		var successCount int
		if err := rows.Scan(&modelID, &totalRequests, &successCount); err != nil {
			return nil, fmt.Errorf("scan model health stats: %w", err)
		}
		var successRate *float64
		if totalRequests > 0 {
			rate := float64(successCount) / float64(totalRequests) * 100
			rate = float64(int(rate*100+0.5)) / 100
			successRate = &rate
		}
		stats[modelID] = modelHealthStats{SuccessRate: successRate, TotalRequests: totalRequests}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate model health stats: %w", err)
	}
	return stats, nil
}

func loadStrategyRecordsByIDs(ctx context.Context, exec queryExecutor, profileID int, strategyIDs []int) (map[int]strategyRecord, error) {
	if len(strategyIDs) == 0 {
		return map[int]strategyRecord{}, nil
	}
	args := []any{profileID, int32ArrayArg(strategyIDs)}
	query := `SELECT id, name, legacy_strategy_type, is_default, failure_status_codes, ban_mode, retry_base_delay_ms, retry_backoff_multiplier, retry_jitter_ratio, retry_max_delay_ms, cycle_retry_attempt_limit, ban_cumulative_retry_attempt_threshold, ban_duration_seconds FROM loadbalance_strategies WHERE profile_id = $1 AND id = ANY($2) ORDER BY id ASC`
	rows, err := exec.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query strategies by id for profile %d: %w", profileID, err)
	}
	defer rows.Close()

	items := map[int]strategyRecord{}
	for rows.Next() {
		record, scanErr := scanStrategyRecord(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items[record.ID] = record
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate strategies by id: %w", err)
	}
	return items, nil
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
		source_models.id, source_models.profile_id, source_models.api_family, source_models.model_id, source_models.display_name, source_models.loadbalance_strategy_id, source_models.openai_accepted_format, source_models.openai_image_operations, source_models.is_enabled, source_models.created_at, source_models.updated_at
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
		if err := rows.Scan(&row.EndpointID, &row.TerminalConnectionID, &row.ConnectionIsActive, &row.ReachableModelData.ID, &row.ReachableModelData.ProfileID, &row.ReachableModelData.APIFamily, &row.ReachableModelData.ModelID, &displayName, &loadbalanceStrategyID, &openAIAcceptedFormat, &openAIImageOperations, &row.ReachableModelData.IsEnabled, &row.ReachableModelData.CreatedAt, &row.ReachableModelData.UpdatedAt); err != nil {
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

func ensureLoadbalanceStrategyExists(ctx context.Context, exec queryExecutor, profileID int, strategyID int) error {
	var existingID int
	err := exec.QueryRow(ctx, `SELECT id FROM loadbalance_strategies WHERE profile_id = $1 AND id = $2 LIMIT 1`, profileID, strategyID).Scan(&existingID)
	if err == pgx.ErrNoRows {
		return &domainError{StatusCode: 400, Detail: "Loadbalance strategy not found"}
	}
	if err != nil {
		return fmt.Errorf("load strategy %d for profile %d: %w", strategyID, profileID, err)
	}
	return nil
}

func ensureModelIDAvailable(ctx context.Context, exec queryExecutor, profileID int, modelID string, excludeID *int) error {
	query := `SELECT id FROM model_configs WHERE profile_id = $1 AND model_id = $2`
	args := []any{profileID, modelID}
	if excludeID != nil {
		query += ` AND id <> $3`
		args = append(args, *excludeID)
	}
	query += ` LIMIT 1`
	var existingID int
	err := exec.QueryRow(ctx, query, args...).Scan(&existingID)
	if err == nil {
		return &domainError{StatusCode: 409, Detail: fmt.Sprintf("Model ID '%s' already exists", modelID)}
	}
	if err == pgx.ErrNoRows {
		return nil
	}
	return fmt.Errorf("query model id availability for %q: %w", modelID, err)
}

func loadAccessTargetsForModels(ctx context.Context, exec queryExecutor, profileID int, modelIDs []int) (map[int][]accessTargetRecord, error) {
	if len(modelIDs) == 0 {
		return map[int][]accessTargetRecord{}, nil
	}
	modelTargets, err := loadModelAccessTargetsForModels(ctx, exec, profileID, modelIDs)
	if err != nil {
		return nil, err
	}
	connectionTargets, err := loadConnectionAccessTargetsForModels(ctx, exec, profileID, modelIDs)
	if err != nil {
		return nil, err
	}
	items := map[int][]accessTargetRecord{}
	for sourceID, targets := range modelTargets {
		items[sourceID] = append(items[sourceID], targets...)
	}
	for sourceID, targets := range connectionTargets {
		items[sourceID] = append(items[sourceID], targets...)
	}
	for sourceID, targets := range items {
		sortAccessTargetRecords(targets)
		items[sourceID] = targets
	}
	return items, nil
}

func loadModelAccessTargetsForModels(ctx context.Context, exec queryExecutor, profileID int, modelIDs []int) (map[int][]accessTargetRecord, error) {
	args := []any{profileID, int32ArrayArg(modelIDs)}
	query := `SELECT model_access_targets.id, model_access_targets.profile_id, model_access_targets.source_model_config_id, model_access_targets.target_model_config_id, model_access_targets.position, model_access_targets.is_enabled, model_access_targets.created_at, model_access_targets.updated_at, target_models.id, target_models.profile_id, target_models.api_family, target_models.model_id, target_models.display_name, target_models.loadbalance_strategy_id, target_models.openai_accepted_format, target_models.openai_image_operations, target_models.is_enabled, target_models.created_at, target_models.updated_at FROM model_access_targets JOIN model_configs AS target_models ON target_models.id = model_access_targets.target_model_config_id WHERE model_access_targets.profile_id = $1 AND model_access_targets.source_model_config_id = ANY($2) AND model_access_targets.target_model_config_id IS NOT NULL ORDER BY model_access_targets.source_model_config_id ASC, model_access_targets.position ASC, model_access_targets.id ASC`
	rows, err := exec.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query model access targets for profile %d: %w", profileID, err)
	}
	defer rows.Close()

	items := map[int][]accessTargetRecord{}
	for rows.Next() {
		var targetModelID int
		record := accessTargetRecord{TargetType: "model"}
		target := modelRecord{}
		var displayName sql.NullString
		var loadbalanceStrategyID sql.NullInt32
		var openAIAcceptedFormat sql.NullString
		var openAIImageOperations sql.NullString
		if err := rows.Scan(&record.ID, &record.ProfileID, &record.SourceModelConfigID, &targetModelID, &record.Position, &record.IsEnabled, &record.CreatedAt, &record.UpdatedAt, &target.ID, &target.ProfileID, &target.APIFamily, &target.ModelID, &displayName, &loadbalanceStrategyID, &openAIAcceptedFormat, &openAIImageOperations, &target.IsEnabled, &target.CreatedAt, &target.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan model access target: %w", err)
		}
		target.DisplayName = nullableStringValue(displayName)
		target.LoadbalanceStrategyID = nullableInt32(loadbalanceStrategyID)
		target.OpenAIAcceptedFormat = nullableStringValue(openAIAcceptedFormat)
		target.OpenAIImageOperations = nullableStringValue(openAIImageOperations)
		record.TargetModelConfigID = intPtr(targetModelID)
		record.TargetModel = &target
		items[record.SourceModelConfigID] = append(items[record.SourceModelConfigID], record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate model access targets: %w", err)
	}
	return items, nil
}

// loadConnectionRoutingWindowsByIDs reads the routing window child rows for a
// set of connections. The parent queries only carry the timezone column, so
// without this second pass every connection would render as configured with
// zero windows. It cannot be folded into the parent JOIN: window rows would
// multiply the access-target rows cartesian-style.
func loadConnectionRoutingWindowsByIDs(ctx context.Context, exec queryExecutor, profileID int, connectionIDs []int) (map[int][]terminaltarget.Window, error) {
	if len(connectionIDs) == 0 {
		return map[int][]terminaltarget.Window{}, nil
	}
	rows, err := exec.Query(ctx, `SELECT connection_id, weekday_mask, start_minute, end_minute FROM connection_routing_windows WHERE profile_id = $1 AND connection_id = ANY($2) ORDER BY connection_id ASC, weekday_mask ASC, start_minute ASC, end_minute ASC`, profileID, int32ArrayArg(connectionIDs))
	if err != nil {
		return nil, fmt.Errorf("query routing windows for profile %d: %w", profileID, err)
	}
	defer rows.Close()
	items := map[int][]terminaltarget.Window{}
	for rows.Next() {
		var connectionID int
		var window terminaltarget.Window
		if err := rows.Scan(&connectionID, &window.WeekdayMask, &window.StartMinute, &window.EndMinute); err != nil {
			return nil, fmt.Errorf("scan routing window: %w", err)
		}
		items[connectionID] = append(items[connectionID], window)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate routing windows for profile %d: %w", profileID, err)
	}
	return items, nil
}

// applyConnectionRoutingSchedule assembles the wire configuration from the
// parent timezone column plus the child window rows. It is clock-free: the
// evaluated state is projected later, at the single response funnel.
func applyConnectionRoutingSchedule(summary *connectionTargetSummary, windows []terminaltarget.Window) {
	if summary.routingScheduleTimezone == nil && len(windows) == 0 {
		return
	}
	payload := &connections.RoutingSchedulePayload{}
	if summary.routingScheduleTimezone != nil {
		payload.Timezone = *summary.routingScheduleTimezone
	}
	for _, window := range windows {
		payload.Windows = append(payload.Windows, connections.RoutingWindowPayload{WeekdayMask: window.WeekdayMask, StartMinute: window.StartMinute, EndMinute: window.EndMinute})
	}
	summary.RoutingSchedule = payload
}

func loadConnectionAccessTargetsForModels(ctx context.Context, exec queryExecutor, profileID int, modelIDs []int) (map[int][]accessTargetRecord, error) {
	args := []any{profileID, int32ArrayArg(modelIDs)}
	query := `SELECT model_access_targets.id, model_access_targets.profile_id, model_access_targets.source_model_config_id, model_access_targets.target_connection_id, model_access_targets.position, model_access_targets.is_enabled, model_access_targets.created_at, model_access_targets.updated_at, connections.id, connections.profile_id, connections.api_family, connections.endpoint_id, endpoints.profile_id, endpoints.name, endpoints.base_url, endpoints.api_key, endpoints.api_key_fingerprint, endpoints.api_key_updated_at, endpoints.config_revision, endpoints.created_at, endpoints.updated_at, connections.is_active, connections.priority, connections.name, connections.auth_type, connections.custom_headers, connections.custom_request_parameters, connections.openai_text_capability, connections.openai_image_capability, connections.routing_schedule_timezone, connections.pricing_template_id, connections.qps_limit, connections.max_in_flight_non_stream, connections.max_in_flight_stream, pricing_templates.id, pricing_templates.name, revisions.version, revisions.currency_code, connections.created_at, connections.updated_at FROM model_access_targets JOIN connections ON connections.id = model_access_targets.target_connection_id JOIN endpoints ON endpoints.id = connections.endpoint_id LEFT JOIN pricing_templates ON pricing_templates.id = connections.pricing_template_id LEFT JOIN pricing_template_revisions AS revisions ON revisions.id = pricing_templates.current_revision_id WHERE model_access_targets.profile_id = $1 AND model_access_targets.source_model_config_id = ANY($2) AND model_access_targets.target_connection_id IS NOT NULL ORDER BY model_access_targets.source_model_config_id ASC, model_access_targets.position ASC, model_access_targets.id ASC`
	rows, err := exec.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query connection access targets for profile %d: %w", profileID, err)
	}
	defer rows.Close()

	items := map[int][]accessTargetRecord{}
	for rows.Next() {
		record, scanErr := scanConnectionAccessTargetRecord(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items[record.SourceModelConfigID] = append(items[record.SourceModelConfigID], record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate connection access targets: %w", err)
	}
	rows.Close()
	connectionIDs := make([]int, 0)
	seen := map[int]struct{}{}
	for _, records := range items {
		for _, record := range records {
			if record.Connection == nil {
				continue
			}
			if _, ok := seen[record.Connection.ID]; ok {
				continue
			}
			seen[record.Connection.ID] = struct{}{}
			connectionIDs = append(connectionIDs, record.Connection.ID)
		}
	}
	windowsByConnection, err := loadConnectionRoutingWindowsByIDs(ctx, exec, profileID, connectionIDs)
	if err != nil {
		return nil, err
	}
	for modelID := range items {
		for index := range items[modelID] {
			if items[modelID][index].Connection == nil {
				continue
			}
			applyConnectionRoutingSchedule(items[modelID][index].Connection, windowsByConnection[items[modelID][index].Connection.ID])
		}
	}
	return items, nil
}

func resolveAccessTargets(ctx context.Context, exec queryExecutor, profileID int, sourceModelConfigID *int, sourceModelID string, apiFamily string, openAIAcceptedFormat *string, openAIImageOperations *string, accessTargets []modelAccessTargetRequest) ([]resolvedAccessTarget, error) {
	authoredTargets := modelrouting.SortAuthoredAccessTargets(modelRoutingTargetsFromRequests(accessTargets))
	modelIDs := make([]string, 0)
	connectionIDs := make([]int, 0)
	for _, target := range authoredTargets {
		switch {
		case modelrouting.IsModelTargetType(target.TargetType) && target.TargetModelID != nil:
			modelIDs = append(modelIDs, strings.TrimSpace(*target.TargetModelID))
		case modelrouting.IsTerminalTargetType(target.TargetType) && target.TerminalTargetID != nil:
			connectionIDs = append(connectionIDs, *target.TerminalTargetID)
		}
	}
	modelsByID, err := loadTargetModelRecordsByModelIDs(ctx, exec, profileID, modelIDs)
	if err != nil {
		return nil, err
	}
	connectionsByID, err := loadConnectionSummariesByIDs(ctx, exec, profileID, connectionIDs)
	if err != nil {
		return nil, err
	}

	resolvedGraphTargets, issues := modelrouting.ResolveAuthoredAccessTargets(authoredTargets, modelrouting.ResolveOptions{
		Source:              modelRoutingSourceNode(sourceModelConfigID, sourceModelID, profileID, apiFamily, openAIAcceptedFormat, openAIImageOperations),
		ModelsByID:          modelRoutingNodesByModelID(modelsByID),
		TerminalTargetsByID: modelRoutingTerminalNodesByConnectionID(connectionsByID),
		IssuePath:           modelRoutingResolveIssuePath,
		IssueDetail:         modelRoutingResolveIssueDetail,
	})
	if err := modelRoutingIssuesError(issues); err != nil {
		return nil, err
	}
	return resolvedAccessTargetsFromModelRouting(resolvedGraphTargets, modelsByID, connectionsByID), nil
}

func modelRoutingSourceNode(sourceModelConfigID *int, sourceModelID string, profileID int, apiFamily string, openAIAcceptedFormat *string, openAIImageOperations *string) modelrouting.ModelNode {
	configID := 0
	if sourceModelConfigID != nil {
		configID = *sourceModelConfigID
	}
	return modelrouting.ModelNode{ConfigID: configID, ProfileID: profileID, ModelID: strings.TrimSpace(sourceModelID), APIFamily: apiFamily, IsEnabled: true, OpenAIAcceptedFormat: openAIAcceptedFormat, OpenAIImageOperations: openAIImageOperations}
}

func modelRoutingNodesByModelID(modelsByID map[string]modelRecord) map[string]modelrouting.ModelNode {
	items := make(map[string]modelrouting.ModelNode, len(modelsByID))
	for modelID, record := range modelsByID {
		items[strings.TrimSpace(modelID)] = modelrouting.ModelNode{ConfigID: record.ID, ProfileID: record.ProfileID, ModelID: record.ModelID, APIFamily: record.APIFamily, IsEnabled: record.IsEnabled, OpenAIAcceptedFormat: record.OpenAIAcceptedFormat, OpenAIImageOperations: record.OpenAIImageOperations}
	}
	return items
}

func modelRoutingTerminalNodesByConnectionID(connectionsByID map[int]connectionTargetSummary) map[int]modelrouting.TerminalTargetNode {
	items := make(map[int]modelrouting.TerminalTargetNode, len(connectionsByID))
	for connectionID, connection := range connectionsByID {
		items[connectionID] = modelrouting.TerminalTargetNode{ID: connection.ID, ProfileID: connection.ProfileID, APIFamily: connection.APIFamily, OpenAITextCapability: connection.OpenAITextCapability, OpenAIImageCapability: connection.OpenAIImageCapability}
	}
	return items
}

func modelRoutingResolveIssuePath(_ string, field string, target modelrouting.AuthoredAccessTarget) string {
	return accessTargetIssuePath(target.Position, field)
}

func modelRoutingResolveIssueDetail(code string, field string, target modelrouting.AuthoredAccessTarget) string {
	switch code {
	case "connection_target_missing_connection":
		if target.TerminalTargetID != nil {
			return fmt.Sprintf("Target connection %d not found", *target.TerminalTargetID)
		}
	case "target_api_family_mismatch":
		if field == "target_model_id" {
			return "Model access targets must use the same api_family as the source model"
		}
		return "Connection access targets must use the same api_family as the source model"
	}
	return ""
}

func resolvedAccessTargetsFromModelRouting(targets []modelrouting.ResolvedAccessTarget, modelsByID map[string]modelRecord, connectionsByID map[int]connectionTargetSummary) []resolvedAccessTarget {
	items := make([]resolvedAccessTarget, 0, len(targets))
	for _, target := range targets {
		switch {
		case modelrouting.IsModelTargetType(target.TargetType) && target.TargetModelID != nil:
			model, ok := modelsByID[strings.TrimSpace(*target.TargetModelID)]
			if !ok {
				continue
			}
			modelCopy := model
			items = append(items, resolvedAccessTarget{TargetType: "model", Position: target.Position, IsEnabled: target.IsEnabled, Model: &modelCopy})
		case modelrouting.IsTerminalTargetType(target.TargetType) && target.TerminalTargetID != nil:
			connection, ok := connectionsByID[*target.TerminalTargetID]
			if !ok {
				continue
			}
			connectionCopy := connection
			items = append(items, resolvedAccessTarget{TargetType: "connection", Position: target.Position, IsEnabled: target.IsEnabled, Connection: &connectionCopy})
		}
	}
	return items
}

func loadTargetModelRecordsByModelIDs(ctx context.Context, exec queryExecutor, profileID int, modelIDs []string) (map[string]modelRecord, error) {
	if len(modelIDs) == 0 {
		return map[string]modelRecord{}, nil
	}
	query := `SELECT ` + modelRecordSelectColumns + ` FROM model_configs WHERE profile_id = $1 AND model_id = ANY($2) ORDER BY id ASC`
	rows, err := exec.Query(ctx, query, profileID, modelIDs)
	if err != nil {
		return nil, fmt.Errorf("query target models for profile %d: %w", profileID, err)
	}
	defer rows.Close()
	items := map[string]modelRecord{}
	for rows.Next() {
		record, scanErr := scanModelRecord(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items[record.ModelID] = record
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate target models: %w", err)
	}
	return items, nil
}

func loadConnectionSummariesByIDs(ctx context.Context, exec queryExecutor, profileID int, connectionIDs []int) (map[int]connectionTargetSummary, error) {
	if len(connectionIDs) == 0 {
		return map[int]connectionTargetSummary{}, nil
	}
	args := []any{profileID, int32ArrayArg(connectionIDs)}
	query := `SELECT connections.id, connections.profile_id, connections.api_family, connections.endpoint_id, endpoints.profile_id, endpoints.name, endpoints.base_url, endpoints.api_key, endpoints.api_key_fingerprint, endpoints.api_key_updated_at, endpoints.config_revision, endpoints.created_at, endpoints.updated_at, connections.is_active, connections.priority, connections.name, connections.auth_type, connections.custom_headers, connections.custom_request_parameters, connections.openai_text_capability, connections.openai_image_capability, connections.routing_schedule_timezone, connections.pricing_template_id, connections.qps_limit, connections.max_in_flight_non_stream, connections.max_in_flight_stream, pricing_templates.id, pricing_templates.name, revisions.version, revisions.currency_code, connections.created_at, connections.updated_at FROM connections JOIN endpoints ON endpoints.id = connections.endpoint_id LEFT JOIN pricing_templates ON pricing_templates.id = connections.pricing_template_id LEFT JOIN pricing_template_revisions AS revisions ON revisions.id = pricing_templates.current_revision_id WHERE connections.profile_id = $1 AND connections.id = ANY($2) ORDER BY connections.id ASC`
	rows, err := exec.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query target connections for profile %d: %w", profileID, err)
	}
	defer rows.Close()
	items := map[int]connectionTargetSummary{}
	for rows.Next() {
		connection, scanErr := scanConnectionTargetSummary(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items[connection.ID] = connection
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate target connections: %w", err)
	}
	rows.Close()
	windowsByConnection, err := loadConnectionRoutingWindowsByIDs(ctx, exec, profileID, connectionIDs)
	if err != nil {
		return nil, err
	}
	for id := range items {
		summary := items[id]
		applyConnectionRoutingSchedule(&summary, windowsByConnection[id])
		items[id] = summary
	}
	return items, nil
}

func replaceAccessTargets(ctx context.Context, tx pgx.Tx, sourceProfileID int, sourceModelConfigID int, targets []resolvedAccessTarget, currentTime time.Time) error {
	return replaceAccessTargetsPreservingConnections(ctx, tx, sourceProfileID, sourceModelConfigID, targets, nil, currentTime)
}

func replaceAccessTargetsPreservingConnections(ctx context.Context, tx pgx.Tx, sourceProfileID int, sourceModelConfigID int, targets []resolvedAccessTarget, preservedConnectionTargets []preservedConnectionAccessTarget, currentTime time.Time) error {
	if _, err := tx.Exec(ctx, `DELETE FROM model_access_targets WHERE source_model_config_id = $1 AND target_model_config_id IS NOT NULL`, sourceModelConfigID); err != nil {
		return fmt.Errorf("delete access targets for model %d: %w", sourceModelConfigID, err)
	}
	for _, target := range sortPreservedConnectionAccessTargetsByPosition(preservedConnectionTargets) {
		if !target.Update {
			continue
		}
		if _, err := tx.Exec(ctx, `UPDATE model_access_targets SET position = $3, is_enabled = $4, updated_at = $5 WHERE id = $1 AND source_model_config_id = $2 AND target_connection_id IS NOT NULL`, target.ID, sourceModelConfigID, target.Position, target.IsEnabled, currentTime); err != nil {
			return fmt.Errorf("update preserved connection access target %d for model %d: %w", target.ID, sourceModelConfigID, err)
		}
	}
	for _, target := range sortResolvedAccessTargetsByPosition(targets) {
		if target.TargetType == "model" {
			if target.Model == nil {
				return fmt.Errorf("replace access targets for model %d: missing model target", sourceModelConfigID)
			}
			if _, err := tx.Exec(ctx, `INSERT INTO model_access_targets (profile_id, source_model_config_id, target_type, target_model_config_id, position, is_enabled, created_at, updated_at) VALUES ($1, $2, 'model', $3, $4, $5, $6, $6)`, sourceProfileID, sourceModelConfigID, target.Model.ID, target.Position, target.IsEnabled, currentTime); err != nil {
				return mapAccessTargetWriteError(err, sourceModelConfigID)
			}
			continue
		}
		if target.Connection == nil {
			return fmt.Errorf("replace access targets for model %d: missing connection target", sourceModelConfigID)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO model_access_targets (profile_id, source_model_config_id, target_type, target_connection_id, position, is_enabled, created_at, updated_at) VALUES ($1, $2, 'connection', $3, $4, $5, $6, $6)`, sourceProfileID, sourceModelConfigID, target.Connection.ID, target.Position, target.IsEnabled, currentTime); err != nil {
			return mapAccessTargetWriteError(err, sourceModelConfigID)
		}
	}
	return nil
}

func mapAccessTargetWriteError(err error, sourceModelConfigID int) error {
	if isUniqueViolation(err, "uq_model_access_targets_source_target_model") || isUniqueViolation(err, "uq_model_access_targets_source_target_connection") {
		return &domainError{StatusCode: 400, Detail: "access_targets must contain unique target references"}
	}
	if isUniqueViolation(err, "uq_model_access_targets_source_position") {
		return &domainError{StatusCode: 400, Detail: "access_targets must contain unique position values"}
	}
	return fmt.Errorf("insert access target for model %d: %w", sourceModelConfigID, err)
}

func lockProfileAccessTargetRows(ctx context.Context, tx pgx.Tx, profileID int) error {
	rows, err := tx.Query(ctx, `SELECT id FROM model_access_targets WHERE profile_id = $1 FOR UPDATE`, profileID)
	if err != nil {
		return fmt.Errorf("lock access targets for profile %d: %w", profileID, err)
	}
	defer rows.Close()
	for rows.Next() {
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate access target locks: %w", err)
	}
	return nil
}

// managementGraphMaxDepth mirrors the runtime planner's resolver depth limit
// so graph mutations reject over-deep graphs before they can be persisted.
const managementGraphMaxDepth = 32

func ensureAccessTargetGraphAcyclic(ctx context.Context, exec queryExecutor, profileID int, sourceModelConfigID int, targets []resolvedAccessTarget) error {
	targetIDs := modelTargetIDsFromResolved(targets)
	if len(targetIDs) == 0 {
		return nil
	}
	var hasCycle bool
	var maxDepth int
	err := exec.QueryRow(ctx, `WITH RECURSIVE graph AS (SELECT source_model_config_id, target_model_config_id FROM model_access_targets WHERE profile_id = $1 AND target_model_config_id IS NOT NULL AND source_model_config_id <> $2 UNION ALL SELECT $2::integer AS source_model_config_id, proposed.target_model_config_id FROM unnest($3::integer[]) AS proposed(target_model_config_id)), walk(node_id, path, depth) AS (SELECT graph.target_model_config_id, ARRAY[$2::integer, graph.target_model_config_id], 1 FROM graph WHERE graph.source_model_config_id = $2 UNION ALL SELECT graph.target_model_config_id, walk.path || graph.target_model_config_id, walk.depth + 1 FROM walk JOIN graph ON graph.source_model_config_id = walk.node_id WHERE graph.target_model_config_id = $2 OR NOT graph.target_model_config_id = ANY(walk.path)) SELECT EXISTS(SELECT 1 FROM walk WHERE node_id = $2), COALESCE(MAX(depth), 0) FROM walk`, profileID, sourceModelConfigID, int32ArrayArg(targetIDs)).Scan(&hasCycle, &maxDepth)
	if err != nil {
		return fmt.Errorf("check access target cycles for model %d: %w", sourceModelConfigID, err)
	}
	if hasCycle {
		return routingPlanValidationIssueError("model_graph_cycle", "access_targets", "access_targets cannot introduce a model target cycle")
	}
	if maxDepth > managementGraphMaxDepth {
		return routingPlanValidationIssueError("model_graph_max_depth", "access_targets", fmt.Sprintf("access_targets cannot exceed the maximum model graph depth of %d", managementGraphMaxDepth))
	}
	return nil
}

func listAccessTargetReferrers(ctx context.Context, exec queryExecutor, profileID int, targetModelConfigID int, excludeID *int) ([]modelRecord, error) {
	query := `SELECT DISTINCT ` + modelRecordSelectColumns + ` FROM model_configs JOIN model_access_targets ON model_access_targets.source_model_config_id = model_configs.id WHERE model_configs.profile_id = $1 AND model_access_targets.target_model_config_id = $2`
	args := []any{profileID, targetModelConfigID}
	if excludeID != nil {
		query += ` AND model_configs.id <> $3`
		args = append(args, *excludeID)
	}
	query += ` ORDER BY model_configs.model_id ASC, model_configs.id ASC`
	rows, err := exec.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query access target referrers for model %d: %w", targetModelConfigID, err)
	}
	defer rows.Close()
	items := make([]modelRecord, 0)
	for rows.Next() {
		record, scanErr := scanModelRecord(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate access target referrers for model %d: %w", targetModelConfigID, err)
	}
	return items, nil
}

// ensureOpenAIAcceptedFormatChangeAllowed prevents a model mode change from
// leaving any existing direct, inbound, or disabled relation in a different
// strict OpenAI mode. Disabled relations remain configuration references and
// therefore are not exempt from the equality contract.
func ensureOpenAIAcceptedFormatChangeAllowed(ctx context.Context, exec queryExecutor, profileID int, modelConfigID int, accessTargets []accessTargetRecord, nextMode *string) error {
	for _, target := range accessTargets {
		if modelrouting.IsTerminalTargetType(target.TargetType) && target.Connection != nil {
			if !providerauth.OpenAITextModesMatch(nextMode, target.Connection.OpenAITextCapability) {
				return &domainError{StatusCode: http.StatusConflict, Detail: "Cannot change openai_accepted_format while connection access targets exist with a different openai_text_capability"}
			}
		}
		if modelrouting.IsModelTargetType(target.TargetType) && target.TargetModel != nil {
			if !providerauth.OpenAITextModesMatch(nextMode, target.TargetModel.OpenAIAcceptedFormat) {
				return &domainError{StatusCode: http.StatusConflict, Detail: "Cannot change openai_accepted_format while model access targets exist with a different openai_accepted_format"}
			}
		}
	}
	referrers, err := listAccessTargetReferrers(ctx, exec, profileID, modelConfigID, nil)
	if err != nil {
		return err
	}
	conflicting := make([]string, 0, len(referrers))
	for _, referrer := range referrers {
		if !providerauth.OpenAITextModesMatch(nextMode, referrer.OpenAIAcceptedFormat) {
			conflicting = append(conflicting, referrer.ModelID)
		}
	}
	if len(conflicting) > 0 {
		return &domainError{StatusCode: http.StatusConflict, Detail: fmt.Sprintf("Cannot change openai_accepted_format: models [%s] target this model", strings.Join(conflicting, ", "))}
	}
	return nil
}

func deleteSourceAccessTargets(ctx context.Context, tx pgx.Tx, sourceModelConfigID int) error {
	if _, err := tx.Exec(ctx, `DELETE FROM model_access_targets WHERE source_model_config_id = $1`, sourceModelConfigID); err != nil {
		return fmt.Errorf("delete source access targets for model %d: %w", sourceModelConfigID, err)
	}
	return nil
}

func deleteSourceAccessTargetsAndOwnedConnections(ctx context.Context, tx pgx.Tx, profileID int, sourceModelConfigID int) error {
	rows, err := tx.Query(ctx, `SELECT target_connection_id FROM model_access_targets WHERE profile_id = $1 AND source_model_config_id = $2 AND target_connection_id IS NOT NULL ORDER BY target_connection_id ASC FOR UPDATE`, profileID, sourceModelConfigID)
	if err != nil {
		return fmt.Errorf("query owned connections for model %d: %w", sourceModelConfigID, err)
	}
	connectionIDs := make([]int, 0)
	for rows.Next() {
		var connectionID int
		if err := rows.Scan(&connectionID); err != nil {
			rows.Close()
			return fmt.Errorf("scan owned connection for model %d: %w", sourceModelConfigID, err)
		}
		connectionIDs = append(connectionIDs, connectionID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate owned connections for model %d: %w", sourceModelConfigID, err)
	}
	rows.Close()
	if err := deleteSourceAccessTargets(ctx, tx, sourceModelConfigID); err != nil {
		return err
	}
	for _, connectionID := range connectionIDs {
		if err := deleteConnectionRowForProfile(ctx, tx, profileID, connectionID); err != nil {
			return err
		}
	}
	return nil
}

func deleteConnectionRowForProfile(ctx context.Context, exec queryExecutor, profileID int, connectionID int) error {
	if _, err := exec.Exec(ctx, `DELETE FROM connections WHERE profile_id = $1 AND id = $2`, profileID, connectionID); err != nil {
		return fmt.Errorf("delete connection %d for profile %d: %w", connectionID, profileID, err)
	}
	return nil
}

func countEnabledModelAccessTargetsExcluding(ctx context.Context, exec queryExecutor, profileID int, modelConfigID int, excludeTargetID int) (int, error) {
	var count int
	if err := exec.QueryRow(ctx, `SELECT COUNT(*) FROM model_access_targets WHERE profile_id = $1 AND source_model_config_id = $2 AND is_enabled = TRUE AND id <> $3`, profileID, modelConfigID, excludeTargetID).Scan(&count); err != nil {
		return 0, fmt.Errorf("count enabled access targets for model %d: %w", modelConfigID, err)
	}
	return count, nil
}

func compactModelAccessTargetPositions(ctx context.Context, exec queryExecutor, profileID int, modelConfigID int, currentTime time.Time) error {
	_, err := exec.Exec(ctx, `UPDATE model_access_targets AS target SET position = ordered.new_position, updated_at = $3 FROM (SELECT id, (ROW_NUMBER() OVER (ORDER BY position ASC, id ASC) - 1)::integer AS new_position FROM model_access_targets WHERE profile_id = $1 AND source_model_config_id = $2) AS ordered WHERE target.id = ordered.id AND target.position <> ordered.new_position`, profileID, modelConfigID, currentTime)
	if err != nil {
		return fmt.Errorf("compact access target positions for model %d: %w", modelConfigID, err)
	}
	return nil
}

func updateAccessTargetMetadata(ctx context.Context, exec queryExecutor, profileID int, modelConfigID int, items []accessTargetMutationItem, currentTime time.Time) error {
	for _, item := range items {
		enabled := true
		if item.Request.IsEnabled != nil {
			enabled = *item.Request.IsEnabled
		}
		commandTag, err := exec.Exec(ctx, `UPDATE model_access_targets SET position = $4, is_enabled = $5, updated_at = $6 WHERE profile_id = $1 AND source_model_config_id = $2 AND id = $3`, profileID, modelConfigID, item.ID, item.Request.Position, enabled, currentTime)
		if err != nil {
			return fmt.Errorf("update model access target %d metadata: %w", item.ID, err)
		}
		if commandTag.RowsAffected() == 0 {
			return &domainError{StatusCode: 404, Detail: "Model access target not found"}
		}
	}
	return nil
}

func lockConnectionRow(ctx context.Context, tx pgx.Tx, profileID int, connectionID int) error {
	var existingID int
	err := tx.QueryRow(ctx, `SELECT id FROM connections WHERE profile_id = $1 AND id = $2 FOR UPDATE`, profileID, connectionID).Scan(&existingID)
	if err == pgx.ErrNoRows {
		return &domainError{StatusCode: 404, Detail: "Connection not found"}
	}
	if err != nil {
		return fmt.Errorf("lock connection %d for profile %d: %w", connectionID, profileID, err)
	}
	return nil
}

func deleteModelAccessTargetRow(ctx context.Context, exec queryExecutor, targetID int) error {
	if _, err := exec.Exec(ctx, `DELETE FROM model_access_targets WHERE id = $1`, targetID); err != nil {
		return fmt.Errorf("delete model access target %d: %w", targetID, err)
	}
	return nil
}

func deleteConnectionRow(ctx context.Context, exec queryExecutor, connectionID int) error {
	if _, err := exec.Exec(ctx, `DELETE FROM connections WHERE id = $1`, connectionID); err != nil {
		return fmt.Errorf("delete connection %d: %w", connectionID, err)
	}
	return nil
}

func insertModel(ctx context.Context, tx pgx.Tx, record modelRecord) (modelRecord, error) {
	var createdID int
	if err := tx.QueryRow(ctx, `INSERT INTO model_configs (profile_id, api_family, model_id, display_name, loadbalance_strategy_id, openai_accepted_format, openai_image_operations, is_enabled, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING id`, record.ProfileID, record.APIFamily, record.ModelID, nullableString(record.DisplayName), nullableInt(record.LoadbalanceStrategyID), nullableString(record.OpenAIAcceptedFormat), nullableString(record.OpenAIImageOperations), record.IsEnabled, record.CreatedAt, record.UpdatedAt).Scan(&createdID); err != nil {
		if isUniqueViolation(err, "uq_model_configs_profile_model_id") {
			return modelRecord{}, &domainError{StatusCode: 409, Detail: fmt.Sprintf("Model ID '%s' already exists", record.ModelID)}
		}
		return modelRecord{}, fmt.Errorf("insert model %q: %w", record.ModelID, err)
	}
	record.ID = createdID
	return record, nil
}

func updateModel(ctx context.Context, tx pgx.Tx, record modelRecord) (modelRecord, error) {
	if _, err := tx.Exec(ctx, `UPDATE model_configs SET api_family = $2, model_id = $3, display_name = $4, loadbalance_strategy_id = $5, openai_accepted_format = $6, openai_image_operations = $7, is_enabled = $8, updated_at = $9 WHERE id = $1`, record.ID, record.APIFamily, record.ModelID, nullableString(record.DisplayName), nullableInt(record.LoadbalanceStrategyID), nullableString(record.OpenAIAcceptedFormat), nullableString(record.OpenAIImageOperations), record.IsEnabled, record.UpdatedAt); err != nil {
		if isUniqueViolation(err, "uq_model_configs_profile_model_id") {
			return modelRecord{}, &domainError{StatusCode: 409, Detail: fmt.Sprintf("Model ID '%s' already exists", record.ModelID)}
		}
		return modelRecord{}, fmt.Errorf("update model %d: %w", record.ID, err)
	}
	return record, nil
}

func deleteModel(ctx context.Context, tx pgx.Tx, modelConfigID int) error {
	if _, err := tx.Exec(ctx, `DELETE FROM model_configs WHERE id = $1`, modelConfigID); err != nil {
		return fmt.Errorf("delete model %d: %w", modelConfigID, err)
	}
	return nil
}

func scanModelRecord(scanner interface{ Scan(...any) error }) (modelRecord, error) {
	var displayName sql.NullString
	var loadbalanceStrategyID sql.NullInt32
	var openAIAcceptedFormat sql.NullString
	var openAIImageOperations sql.NullString
	record := modelRecord{}
	if err := scanner.Scan(&record.ID, &record.ProfileID, &record.APIFamily, &record.ModelID, &displayName, &loadbalanceStrategyID, &openAIAcceptedFormat, &openAIImageOperations, &record.IsEnabled, &record.CreatedAt, &record.UpdatedAt); err != nil {
		return modelRecord{}, err
	}
	record.DisplayName = nullableStringValue(displayName)
	record.LoadbalanceStrategyID = nullableInt32(loadbalanceStrategyID)
	record.OpenAIAcceptedFormat = nullableStringValue(openAIAcceptedFormat)
	record.OpenAIImageOperations = nullableStringValue(openAIImageOperations)
	return record, nil
}

func scanStrategyRecord(scanner interface{ Scan(...any) error }) (strategyRecord, error) {
	var failureStatusCodes []int32
	record := strategyRecord{}
	if err := scanner.Scan(
		&record.ID,
		&record.Name,
		&record.LegacyStrategyType,
		&record.IsDefault,
		&failureStatusCodes,
		&record.BanMode,
		&record.RetryBaseDelayMS,
		&record.RetryBackoffMultiplier,
		&record.RetryJitterRatio,
		&record.RetryMaxDelayMS,
		&record.CycleRetryAttemptLimit,
		&record.BanCumulativeRetryAttemptThreshold,
		&record.BanDurationSeconds,
	); err != nil {
		return strategyRecord{}, err
	}
	record.FailureStatusCodes = intSliceFromInt32(failureStatusCodes)
	return record, nil
}

func scanConnectionAccessTargetRecord(scanner interface{ Scan(...any) error }) (accessTargetRecord, error) {
	var connectionID int
	record := accessTargetRecord{TargetType: "connection"}
	connection, err := scanConnectionTargetSummaryWithPrefix(scanner, []any{&record.ID, &record.ProfileID, &record.SourceModelConfigID, &connectionID, &record.Position, &record.IsEnabled, &record.CreatedAt, &record.UpdatedAt})
	if err != nil {
		return accessTargetRecord{}, fmt.Errorf("scan connection access target: %w", err)
	}
	record.TargetConnectionID = intPtr(connectionID)
	record.Connection = &connection
	return record, nil
}

func scanConnectionTargetSummary(scanner interface{ Scan(...any) error }) (connectionTargetSummary, error) {
	return scanConnectionTargetSummaryWithPrefix(scanner, nil)
}

func scanConnectionTargetSummaryWithPrefix(scanner interface{ Scan(...any) error }, prefix []any) (connectionTargetSummary, error) {
	var endpointAPIKey sql.NullString
	var endpointFingerprint *string
	var endpointKeyUpdatedAt *time.Time
	var connectionName sql.NullString
	var authType sql.NullString
	var customHeaders sql.NullString
	var customRequestParameters sql.NullString
	var openAITextCapability sql.NullString
	var openAIImageCapability sql.NullString
	var routingScheduleTimezone sql.NullString
	var pricingTemplateID sql.NullInt32
	var qpsLimit sql.NullInt32
	var maxInFlightNonStream sql.NullInt32
	var maxInFlightStream sql.NullInt32
	var templateID sql.NullInt32
	var templateName sql.NullString
	var templateCurrencyCode sql.NullString
	var templateVersion sql.NullInt32
	item := connectionTargetSummary{}
	endpoint := endpointResponse{}
	dest := append(prefix,
		&item.ID,
		&item.ProfileID,
		&item.APIFamily,
		&item.EndpointID,
		&endpoint.ProfileID,
		&endpoint.Name,
		&endpoint.BaseURL,
		&endpointAPIKey,
		&endpointFingerprint,
		&endpointKeyUpdatedAt,
		&endpoint.ConfigRevision,
		&endpoint.CreatedAt,
		&endpoint.UpdatedAt,
		&item.IsActive,
		&item.Priority,
		&connectionName,
		&authType,
		&customHeaders,
		&customRequestParameters,
		&openAITextCapability,
		&openAIImageCapability,
		&routingScheduleTimezone,
		&pricingTemplateID,
		&qpsLimit,
		&maxInFlightNonStream,
		&maxInFlightStream,
		&templateID,
		&templateName,
		&templateVersion,
		&templateCurrencyCode,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if err := scanner.Scan(dest...); err != nil {
		return connectionTargetSummary{}, err
	}
	endpoint.ID = item.EndpointID
	endpoint.HasAPIKey = strings.TrimSpace(endpointAPIKey.String) != ""
	endpoint.APIKeyFingerprint = endpointFingerprint
	endpoint.APIKeyUpdatedAt = endpointKeyUpdatedAt
	item.Endpoint = &endpoint
	item.Name = nullableStringValue(connectionName)
	item.AuthType = nullableStringValue(authType)
	item.CustomHeaders, item.CustomHeadersRedacted = safediag.RedactSensitiveHeaderValues(parseCustomHeaders(customHeaders))
	item.CustomRequestParameters = parseCustomRequestParameters(customRequestParameters)
	item.OpenAITextCapability = nullableStringValue(openAITextCapability)
	item.OpenAIImageCapability = nullableStringValue(openAIImageCapability)
	item.routingScheduleTimezone = nullableStringValue(routingScheduleTimezone)
	item.PricingTemplateID = nullableInt32(pricingTemplateID)
	item.QPSLimit = nullableInt32(qpsLimit)
	item.MaxInFlightNonStream = nullableInt32(maxInFlightNonStream)
	item.MaxInFlightStream = nullableInt32(maxInFlightStream)
	if templateID.Valid {
		item.PricingTemplate = &connectionPricingTemplateSummary{ID: int(templateID.Int32), Name: templateName.String, PricingUnit: "PER_1M", PricingCurrencyCode: templateCurrencyCode.String, Version: int(templateVersion.Int32)}
	}
	return item, nil
}

func buildModelListResponse(record modelRecord, strategies map[int]strategyRecord, accessTargets map[int][]accessTargetRecord, counts map[int]modelConnectionCounts, health map[string]modelHealthStats, now time.Time) modelConfigListResponse {
	response := modelConfigListResponse{ID: record.ID, ProfileID: record.ProfileID, APIFamily: record.APIFamily, ModelID: record.ModelID, DisplayName: record.DisplayName, LoadbalanceStrategyID: record.LoadbalanceStrategyID, OpenAIAcceptedFormat: record.OpenAIAcceptedFormat, OpenAIImageOperations: record.OpenAIImageOperations, AccessTargets: accessTargetResponsesFromRecords(accessTargets[record.ID], now), IsEnabled: record.IsEnabled, HealthTotalRequests: 0, CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt}
	if record.LoadbalanceStrategyID != nil {
		if strategy, ok := strategies[*record.LoadbalanceStrategyID]; ok {
			response.LoadbalanceStrategy = strategySummaryFromRecord(strategy)
		}
	}
	if count, ok := counts[record.ID]; ok {
		response.ConnectionCount = count.Total
		response.ActiveConnectionCount = count.Active
	}
	if stats, ok := health[record.ModelID]; ok {
		response.HealthSuccessRate = stats.SuccessRate
		response.HealthTotalRequests = stats.TotalRequests
	}
	return response
}

func buildModelDetailResponse(record modelRecord, strategies map[int]strategyRecord, accessTargets map[int][]accessTargetRecord, now time.Time) modelConfigResponse {
	response := modelConfigResponse{ID: record.ID, ProfileID: record.ProfileID, APIFamily: record.APIFamily, ModelID: record.ModelID, DisplayName: record.DisplayName, LoadbalanceStrategyID: record.LoadbalanceStrategyID, OpenAIAcceptedFormat: record.OpenAIAcceptedFormat, OpenAIImageOperations: record.OpenAIImageOperations, AccessTargets: accessTargetResponsesFromRecords(accessTargets[record.ID], now), IsEnabled: record.IsEnabled, CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt}
	if record.LoadbalanceStrategyID != nil {
		if strategy, ok := strategies[*record.LoadbalanceStrategyID]; ok {
			response.LoadbalanceStrategy = strategySummaryFromRecord(strategy)
		}
	}
	return response
}

func strategySummaryFromRecord(record strategyRecord) *loadbalanceStrategySummary {
	return &loadbalanceStrategySummary{ID: record.ID, Name: record.Name, LegacyStrategyType: record.LegacyStrategyType, IsDefault: record.IsDefault, FailureStatusCodes: cloneIntSlice(record.FailureStatusCodes), BanMode: record.BanMode, RetryBaseDelayMS: record.RetryBaseDelayMS, RetryBackoffMultiplier: record.RetryBackoffMultiplier, RetryJitterRatio: record.RetryJitterRatio, RetryMaxDelayMS: record.RetryMaxDelayMS, CycleRetryAttemptLimit: record.CycleRetryAttemptLimit, BanCumulativeRetryAttemptThreshold: record.BanCumulativeRetryAttemptThreshold, BanDurationSeconds: record.BanDurationSeconds}
}

func accessTargetResponsesFromRecords(records []accessTargetRecord, now time.Time) []modelAccessTargetResponse {
	if len(records) == 0 {
		return []modelAccessTargetResponse{}
	}
	ordered := cloneAccessTargetRecords(records)
	sortAccessTargetRecords(ordered)
	items := make([]modelAccessTargetResponse, 0, len(ordered))
	for _, record := range ordered {
		response := modelAccessTargetResponse{ID: record.ID, TargetType: record.TargetType, TargetModelID: stringPtrFromModelRecord(record.TargetModel), ConnectionID: copyIntPtr(record.TargetConnectionID), TerminalTargetID: copyIntPtr(record.TargetConnectionID), Position: record.Position, IsEnabled: record.IsEnabled, CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt}
		if record.TargetModel != nil {
			response.TargetModel = modelTargetSummaryFromRecord(*record.TargetModel)
		}
		if record.Connection != nil {
			connection := *record.Connection
			// The evaluated state is projected here, at the single funnel where
			// both the connection and terminal_target keys are filled from the
			// same struct, so the two keys can never disagree. It reuses the
			// connections package projection rather than a second
			// implementation of window arithmetic.
			connection.RoutingScheduleState = connections.RoutingScheduleStateForConfig(
				connection.routingScheduleTimezone, routingWindowsFromPayload(connection.RoutingSchedule), connection.IsActive, now)
			response.Connection = &connection
			response.TerminalTarget = &connection
		}
		items = append(items, response)
	}
	return items
}

func modelTargetSummaryFromRecord(record modelRecord) *modelTargetSummary {
	return &modelTargetSummary{ID: record.ID, ProfileID: record.ProfileID, APIFamily: record.APIFamily, ModelID: record.ModelID, DisplayName: record.DisplayName, LoadbalanceStrategyID: record.LoadbalanceStrategyID, OpenAIAcceptedFormat: record.OpenAIAcceptedFormat, OpenAIImageOperations: record.OpenAIImageOperations, IsEnabled: record.IsEnabled}
}

func accessTargetRequestsFromRecords(records []accessTargetRecord) []modelAccessTargetRequest {
	if len(records) == 0 {
		return []modelAccessTargetRequest{}
	}
	ordered := cloneAccessTargetRecords(records)
	sortAccessTargetRecords(ordered)
	items := make([]modelAccessTargetRequest, 0, len(ordered))
	for _, record := range ordered {
		enabled := record.IsEnabled
		request := modelAccessTargetRequest{TargetType: record.TargetType, Position: record.Position, IsEnabled: &enabled}
		if record.TargetType == "model" && record.TargetModel != nil {
			modelID := record.TargetModel.ModelID
			request.TargetModelID = &modelID
		}
		if record.TargetType == "connection" && record.TargetConnectionID != nil {
			connectionID := *record.TargetConnectionID
			request.ConnectionID = &connectionID
		}
		items = append(items, request)
	}
	return items
}

func hasEnabledResolvedAccessTarget(targets []resolvedAccessTarget) bool {
	for _, target := range targets {
		if target.IsEnabled {
			return true
		}
	}
	return false
}

func modelTargetIDsFromResolved(targets []resolvedAccessTarget) []int {
	seen := map[int]struct{}{}
	items := make([]int, 0)
	for _, target := range targets {
		if target.TargetType != "model" || target.Model == nil {
			continue
		}
		if _, ok := seen[target.Model.ID]; ok {
			continue
		}
		seen[target.Model.ID] = struct{}{}
		items = append(items, target.Model.ID)
	}
	sort.Ints(items)
	return items
}

func cloneAccessTargetRecords(values []accessTargetRecord) []accessTargetRecord {
	cloned := make([]accessTargetRecord, len(values))
	copy(cloned, values)
	return cloned
}

func sortAccessTargetRecords(values []accessTargetRecord) {
	sort.Slice(values, func(left int, right int) bool {
		if values[left].Position == values[right].Position {
			return values[left].ID < values[right].ID
		}
		return values[left].Position < values[right].Position
	})
}

func sortResolvedAccessTargetsByPosition(values []resolvedAccessTarget) []resolvedAccessTarget {
	ordered := make([]resolvedAccessTarget, len(values))
	copy(ordered, values)
	sort.Slice(ordered, func(left int, right int) bool {
		return ordered[left].Position < ordered[right].Position
	})
	return ordered
}

func sortPreservedConnectionAccessTargetsByPosition(values []preservedConnectionAccessTarget) []preservedConnectionAccessTarget {
	ordered := make([]preservedConnectionAccessTarget, len(values))
	copy(ordered, values)
	sort.Slice(ordered, func(left int, right int) bool {
		if ordered[left].Position == ordered[right].Position {
			return ordered[left].ID < ordered[right].ID
		}
		return ordered[left].Position < ordered[right].Position
	})
	return ordered
}

func accessTargetInputKey(value modelAccessTargetRequest) string {
	if value.TargetType == "model" && value.TargetModelID != nil {
		return "model:" + strings.TrimSpace(*value.TargetModelID)
	}
	if value.TargetType == "connection" && value.ConnectionID != nil {
		return fmt.Sprintf("connection:%d", *value.ConnectionID)
	}
	return value.TargetType
}

func stringPtrFromModelRecord(record *modelRecord) *string {
	if record == nil {
		return nil
	}
	return stringPtr(record.ModelID)
}

func copyIntPtr(value *int) *int {
	if value == nil {
		return nil
	}
	return intPtr(*value)
}

func intPtr(value int) *int {
	resolved := value
	return &resolved
}

func cloneIntSlice(values []int) []int {
	if len(values) == 0 {
		return []int{}
	}
	cloned := make([]int, len(values))
	copy(cloned, values)
	return cloned
}

func int32ArrayArg(values []int) []int32 {
	items := make([]int32, 0, len(values))
	for _, value := range values {
		items = append(items, int32(value))
	}
	return items
}

func intSliceFromInt32(values []int32) []int {
	items := make([]int, 0, len(values))
	for _, value := range values {
		items = append(items, int(value))
	}
	return items
}

func nullableString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableInt32(value sql.NullInt32) *int {
	if !value.Valid {
		return nil
	}
	resolved := int(value.Int32)
	return &resolved
}

func nullableFloat64Value(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableStringValue(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	resolved := value.String
	return &resolved
}

func parseCustomHeaders(value sql.NullString) map[string]string {
	if !value.Valid || strings.TrimSpace(value.String) == "" {
		return nil
	}
	parsed := map[string]string{}
	if err := json.Unmarshal([]byte(value.String), &parsed); err != nil {
		return nil
	}
	return parsed
}

// parseCustomRequestParameters parses the JSONB column text into the shared
// validated value. Management reads normalize invalid persisted data to
// unconfigured; the runtime planning snapshot independently fails closed on
// invalid persisted data before publishing.
func parseCustomRequestParameters(value sql.NullString) *terminaltarget.CustomRequestParameters {
	if !value.Valid || strings.TrimSpace(value.String) == "" {
		return nil
	}
	parsed, validationErr := terminaltarget.ParseCustomRequestParametersJSON([]byte(value.String))
	if validationErr != nil || parsed.IsEmpty() {
		return nil
	}
	return parsed
}

func sortModelRecordsByID(records []modelRecord) {
	sort.Slice(records, func(left int, right int) bool {
		return records[left].ID < records[right].ID
	})
}

func setModelEnabled(ctx context.Context, exec queryExecutor, profileID int, modelConfigID int, enabled bool, currentTime time.Time) error {
	_, err := exec.Exec(ctx, `UPDATE model_configs SET is_enabled = $3, updated_at = $4 WHERE profile_id = $1 AND id = $2`, profileID, modelConfigID, enabled, currentTime)
	if err != nil {
		return fmt.Errorf("set model %d enabled=%t: %w", modelConfigID, enabled, err)
	}
	return nil
}

// routingWindowsFromPayload converts the assembled wire configuration back into
// domain windows for the state projection.
func routingWindowsFromPayload(payload *connections.RoutingSchedulePayload) []terminaltarget.Window {
	if payload == nil || len(payload.Windows) == 0 {
		return nil
	}
	windows := make([]terminaltarget.Window, 0, len(payload.Windows))
	for _, window := range payload.Windows {
		windows = append(windows, terminaltarget.Window{WeekdayMask: window.WeekdayMask, StartMinute: window.StartMinute, EndMinute: window.EndMinute})
	}
	return windows
}

// routingScheduleTimezoneFromSummary reads the timezone off an assembled
// summary. The wire payload is the single source here: the unexported carrier
// field is only populated on the scan path, while composite reads build the
// summary from the payload.
func routingScheduleTimezoneFromSummary(summary *connectionTargetSummary) *string {
	if summary == nil || summary.RoutingSchedule == nil {
		return nil
	}
	timezone := summary.RoutingSchedule.Timezone
	return &timezone
}
