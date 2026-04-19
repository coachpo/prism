package models

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/coachpo/prism/backend/internal/vendordomain"
)

type queryExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type modelRecord struct {
	ID                    int
	ProfileID             int
	VendorID              *int
	APIFamily             string
	ModelID               string
	DisplayName           *string
	ModelType             string
	LoadbalanceStrategyID *int
	IsEnabled             bool
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

type vendorRecord struct {
	ID                 int
	Key                string
	Name               string
	Description        *string
	IconKey            *string
	AuditEnabled       bool
	AuditCaptureBodies bool
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type strategyRecord struct {
	ID                 int
	Name               string
	StrategyType       string
	LegacyStrategyType *string
	AutoRecovery       any
	RoutingPolicy      any
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
	EndpointID          int
	ConnectionIsActive  bool
	ConnectionModelID   int
	ConnectionModelData modelRecord
}

func listModelRecords(ctx context.Context, exec queryExecutor, profileID int) ([]modelRecord, error) {
	rows, err := exec.Query(ctx, `SELECT id, profile_id, vendor_id, api_family, model_id, display_name, model_type, loadbalance_strategy_id, is_enabled, created_at, updated_at FROM model_configs WHERE profile_id = $1 ORDER BY id ASC`, profileID)
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
	query := `SELECT id, profile_id, vendor_id, api_family, model_id, display_name, model_type, loadbalance_strategy_id, is_enabled, created_at, updated_at FROM model_configs WHERE profile_id = $1 AND id = $2`
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

func loadModelRecordsByIDs(ctx context.Context, exec queryExecutor, profileID int, modelIDs []int) ([]modelRecord, error) {
	if len(modelIDs) == 0 {
		return []modelRecord{}, nil
	}
	args := []any{profileID}
	for _, modelID := range modelIDs {
		args = append(args, modelID)
	}
	query := fmt.Sprintf(`SELECT id, profile_id, vendor_id, api_family, model_id, display_name, model_type, loadbalance_strategy_id, is_enabled, created_at, updated_at FROM model_configs WHERE profile_id = $1 AND id IN (%s) ORDER BY id ASC`, placeholders(2, len(modelIDs)))
	rows, err := exec.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query models by id for profile %d: %w", profileID, err)
	}
	defer rows.Close()

	items := make([]modelRecord, 0, len(modelIDs))
	for rows.Next() {
		record, scanErr := scanModelRecord(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate models by id for profile %d: %w", profileID, err)
	}
	return items, nil
}

func listConnectionCountsByModel(ctx context.Context, exec queryExecutor, profileID int) (map[int]modelConnectionCounts, error) {
	rows, err := exec.Query(ctx, `SELECT model_config_id, COUNT(*) AS total_count, COALESCE(SUM(CASE WHEN is_active THEN 1 ELSE 0 END), 0) AS active_count FROM connections WHERE profile_id = $1 GROUP BY model_config_id`, profileID)
	if err != nil {
		return nil, fmt.Errorf("query model connection counts for profile %d: %w", profileID, err)
	}
	defer rows.Close()

	counts := map[int]modelConnectionCounts{}
	for rows.Next() {
		var modelID int
		var totalCount int
		var activeCount int
		if err := rows.Scan(&modelID, &totalCount, &activeCount); err != nil {
			return nil, fmt.Errorf("scan model connection counts: %w", err)
		}
		counts[modelID] = modelConnectionCounts{Total: totalCount, Active: activeCount}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate model connection counts: %w", err)
	}
	return counts, nil
}

func listModelHealthStats(ctx context.Context, exec queryExecutor, profileID int) (map[string]modelHealthStats, error) {
	rows, err := exec.Query(ctx, `SELECT model_id, COUNT(*) AS total_requests, COALESCE(SUM(CASE WHEN status_code BETWEEN 200 AND 299 THEN 1 ELSE 0 END), 0) AS success_count FROM request_logs WHERE profile_id = $1 GROUP BY model_id`, profileID)
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

func loadVendorRecordsByIDs(ctx context.Context, exec queryExecutor, vendorIDs []int) (map[int]vendorRecord, error) {
	if len(vendorIDs) == 0 {
		return map[int]vendorRecord{}, nil
	}
	args := make([]any, 0, len(vendorIDs))
	for _, vendorID := range vendorIDs {
		args = append(args, vendorID)
	}
	query := fmt.Sprintf(`SELECT id, key, name, description, icon_key, audit_enabled, audit_capture_bodies, created_at, updated_at FROM vendors WHERE id IN (%s) ORDER BY id ASC`, placeholders(1, len(vendorIDs)))
	rows, err := exec.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query vendors by id: %w", err)
	}
	defer rows.Close()

	items := map[int]vendorRecord{}
	for rows.Next() {
		record, scanErr := scanVendorRecord(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items[record.ID] = record
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate vendors by id: %w", err)
	}
	return items, nil
}

func loadStrategyRecordsByIDs(ctx context.Context, exec queryExecutor, profileID int, strategyIDs []int) (map[int]strategyRecord, error) {
	if len(strategyIDs) == 0 {
		return map[int]strategyRecord{}, nil
	}
	args := []any{profileID}
	for _, strategyID := range strategyIDs {
		args = append(args, strategyID)
	}
	query := fmt.Sprintf(`SELECT id, name, strategy_type, legacy_strategy_type, auto_recovery, routing_policy FROM loadbalance_strategies WHERE profile_id = $1 AND id IN (%s) ORDER BY id ASC`, placeholders(2, len(strategyIDs)))
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

func loadProxyTargetsForModels(ctx context.Context, exec queryExecutor, modelIDs []int) (map[int][]proxyTargetReference, error) {
	if len(modelIDs) == 0 {
		return map[int][]proxyTargetReference{}, nil
	}
	args := make([]any, 0, len(modelIDs))
	for _, modelID := range modelIDs {
		args = append(args, modelID)
	}
	query := fmt.Sprintf(`SELECT model_proxy_targets.source_model_config_id, model_proxy_targets.position, target_models.model_id FROM model_proxy_targets JOIN model_configs AS target_models ON target_models.id = model_proxy_targets.target_model_config_id WHERE model_proxy_targets.source_model_config_id IN (%s) ORDER BY model_proxy_targets.source_model_config_id ASC, model_proxy_targets.position ASC, model_proxy_targets.id ASC`, placeholders(1, len(modelIDs)))
	rows, err := exec.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query proxy targets by source model: %w", err)
	}
	defer rows.Close()

	items := map[int][]proxyTargetReference{}
	for rows.Next() {
		var sourceModelID int
		var position int
		var targetModelID string
		if err := rows.Scan(&sourceModelID, &position, &targetModelID); err != nil {
			return nil, fmt.Errorf("scan proxy target: %w", err)
		}
		items[sourceModelID] = append(items[sourceModelID], proxyTargetReference{TargetModelID: targetModelID, Position: position})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate proxy targets: %w", err)
	}
	return items, nil
}

func loadConnectionsForModel(ctx context.Context, exec queryExecutor, profileID int, modelConfigID int) ([]connectionResponse, error) {
	rows, err := exec.Query(ctx, `SELECT connections.id, connections.profile_id, connections.model_config_id, connections.endpoint_id, endpoints.profile_id, endpoints.name, endpoints.base_url, endpoints.api_key, endpoints.position, endpoints.created_at, endpoints.updated_at, connections.is_active, connections.priority, connections.name, connections.auth_type, connections.custom_headers, connections.openai_probe_endpoint_variant, connections.pricing_template_id, connections.qps_limit, connections.max_in_flight_non_stream, connections.max_in_flight_stream, pricing_templates.id, pricing_templates.name, pricing_templates.pricing_unit, pricing_templates.pricing_currency_code, pricing_templates.version, connections.health_status, connections.health_detail, connections.last_health_check, connections.created_at, connections.updated_at FROM connections JOIN endpoints ON endpoints.id = connections.endpoint_id LEFT JOIN pricing_templates ON pricing_templates.id = connections.pricing_template_id WHERE connections.profile_id = $1 AND connections.model_config_id = $2 ORDER BY connections.priority ASC, connections.id ASC`, profileID, modelConfigID)
	if err != nil {
		return nil, fmt.Errorf("query connections for model %d: %w", modelConfigID, err)
	}
	defer rows.Close()

	items := make([]connectionResponse, 0)
	for rows.Next() {
		item, scanErr := scanConnectionResponse(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate connections for model %d: %w", modelConfigID, err)
	}
	return items, nil
}

func listEndpointModelRows(ctx context.Context, exec queryExecutor, profileID int, endpointIDs []int) ([]endpointModelConnectionRow, error) {
	if len(endpointIDs) == 0 {
		return []endpointModelConnectionRow{}, nil
	}
	args := []any{profileID}
	for _, endpointID := range endpointIDs {
		args = append(args, endpointID)
	}
	query := fmt.Sprintf(`SELECT connections.endpoint_id, connections.is_active, model_configs.id, model_configs.profile_id, model_configs.vendor_id, model_configs.api_family, model_configs.model_id, model_configs.display_name, model_configs.model_type, model_configs.loadbalance_strategy_id, model_configs.is_enabled, model_configs.created_at, model_configs.updated_at FROM connections JOIN model_configs ON model_configs.id = connections.model_config_id WHERE connections.profile_id = $1 AND connections.endpoint_id IN (%s) ORDER BY connections.endpoint_id ASC, model_configs.id ASC, connections.priority ASC, connections.id ASC`, placeholders(2, len(endpointIDs)))
	rows, err := exec.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query endpoint model rows for profile %d: %w", profileID, err)
	}
	defer rows.Close()

	items := make([]endpointModelConnectionRow, 0)
	for rows.Next() {
		var row endpointModelConnectionRow
		if err := rows.Scan(&row.EndpointID, &row.ConnectionIsActive, &row.ConnectionModelData.ID, &row.ConnectionModelData.ProfileID, &row.ConnectionModelData.VendorID, &row.ConnectionModelData.APIFamily, &row.ConnectionModelData.ModelID, &row.ConnectionModelData.DisplayName, &row.ConnectionModelData.ModelType, &row.ConnectionModelData.LoadbalanceStrategyID, &row.ConnectionModelData.IsEnabled, &row.ConnectionModelData.CreatedAt, &row.ConnectionModelData.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan endpoint model row: %w", err)
		}
		row.ConnectionModelID = row.ConnectionModelData.ID
		items = append(items, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate endpoint model rows: %w", err)
	}
	return items, nil
}

func ensureVendorExists(ctx context.Context, exec queryExecutor, vendorID int) error {
	var existingID int
	err := exec.QueryRow(ctx, `SELECT id FROM vendors WHERE id = $1 LIMIT 1`, vendorID).Scan(&existingID)
	if err == pgx.ErrNoRows {
		return &domainError{StatusCode: 400, Detail: "Vendor not found"}
	}
	if err != nil {
		return fmt.Errorf("load vendor %d: %w", vendorID, err)
	}
	return nil
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

func listProxyReferrers(ctx context.Context, exec queryExecutor, profileID int, modelID string, excludeID *int) ([]modelRecord, error) {
	targetIDQuery := `SELECT id FROM model_configs WHERE profile_id = $1 AND model_id = $2`
	args := []any{profileID, modelID}
	query := `SELECT DISTINCT model_configs.id, model_configs.profile_id, model_configs.vendor_id, model_configs.api_family, model_configs.model_id, model_configs.display_name, model_configs.model_type, model_configs.loadbalance_strategy_id, model_configs.is_enabled, model_configs.created_at, model_configs.updated_at FROM model_configs JOIN model_proxy_targets ON model_proxy_targets.source_model_config_id = model_configs.id WHERE model_configs.profile_id = $1 AND model_proxy_targets.target_model_config_id = (` + targetIDQuery + `)`
	if excludeID != nil {
		query += ` AND model_configs.id <> $3`
		args = append(args, *excludeID)
	}
	query += ` ORDER BY model_configs.model_id ASC, model_configs.id ASC`
	rows, err := exec.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query proxy referrers for %q: %w", modelID, err)
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
		return nil, fmt.Errorf("iterate proxy referrers for %q: %w", modelID, err)
	}
	return items, nil
}

func resolveProxyTargets(ctx context.Context, exec queryExecutor, profileID int, modelType string, proxyTargets []proxyTargetReference, apiFamily string, excludeModelID *string) ([]modelRecord, error) {
	if modelType == "native" {
		if len(proxyTargets) > 0 {
			return nil, &domainError{StatusCode: 400, Detail: "proxy_targets must be empty for native models"}
		}
		return []modelRecord{}, nil
	}
	if len(proxyTargets) == 0 {
		return []modelRecord{}, nil
	}
	if excludeModelID != nil {
		for _, proxyTarget := range proxyTargets {
			if proxyTarget.TargetModelID == *excludeModelID {
				return nil, &domainError{StatusCode: 400, Detail: "Proxy model cannot target itself"}
			}
		}
	}
	args := []any{profileID}
	targetModelIDs := make([]string, 0, len(proxyTargets))
	for _, proxyTarget := range proxyTargets {
		targetModelIDs = append(targetModelIDs, proxyTarget.TargetModelID)
		args = append(args, proxyTarget.TargetModelID)
	}
	query := fmt.Sprintf(`SELECT id, profile_id, vendor_id, api_family, model_id, display_name, model_type, loadbalance_strategy_id, is_enabled, created_at, updated_at FROM model_configs WHERE profile_id = $1 AND model_id IN (%s) ORDER BY id ASC`, placeholders(2, len(targetModelIDs)))
	rows, err := exec.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query proxy targets for profile %d: %w", profileID, err)
	}
	defer rows.Close()

	targetsByModelID := map[string]modelRecord{}
	for rows.Next() {
		record, scanErr := scanModelRecord(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		targetsByModelID[record.ModelID] = record
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate proxy targets: %w", err)
	}

	orderedTargets := make([]modelRecord, 0, len(proxyTargets))
	for _, proxyTarget := range proxyTargets {
		target, ok := targetsByModelID[proxyTarget.TargetModelID]
		if !ok {
			return nil, &domainError{StatusCode: 400, Detail: fmt.Sprintf("Target model '%s' not found", proxyTarget.TargetModelID)}
		}
		if target.ModelType != "native" {
			return nil, &domainError{StatusCode: 400, Detail: fmt.Sprintf("Target model '%s' is not a native model (chained proxies not allowed)", proxyTarget.TargetModelID)}
		}
		if target.APIFamily != apiFamily {
			return nil, &domainError{StatusCode: 400, Detail: "Proxy targets must use the same api_family as the proxy model"}
		}
		orderedTargets = append(orderedTargets, target)
	}
	return orderedTargets, nil
}

func replaceProxyTargets(ctx context.Context, tx pgx.Tx, sourceModelConfigID int, targetModels []modelRecord, proxyTargets []proxyTargetReference) error {
	if _, err := tx.Exec(ctx, `DELETE FROM model_proxy_targets WHERE source_model_config_id = $1`, sourceModelConfigID); err != nil {
		return fmt.Errorf("delete proxy targets for model %d: %w", sourceModelConfigID, err)
	}
	for index, proxyTarget := range proxyTargets {
		if _, err := tx.Exec(ctx, `INSERT INTO model_proxy_targets (source_model_config_id, target_model_config_id, position) VALUES ($1, $2, $3)`, sourceModelConfigID, targetModels[index].ID, proxyTarget.Position); err != nil {
			if isUniqueViolation(err, "uq_model_proxy_targets_source_target") {
				return &domainError{StatusCode: 400, Detail: "proxy_targets must contain unique target_model_id values"}
			}
			if isUniqueViolation(err, "uq_model_proxy_targets_source_position") {
				return &domainError{StatusCode: 400, Detail: "proxy_targets must contain unique position values"}
			}
			return fmt.Errorf("insert proxy target for model %d: %w", sourceModelConfigID, err)
		}
	}
	return nil
}

func insertModel(ctx context.Context, tx pgx.Tx, record modelRecord) (modelRecord, error) {
	created, err := scanModelRecord(tx.QueryRow(ctx, `INSERT INTO model_configs (profile_id, vendor_id, api_family, model_id, display_name, model_type, loadbalance_strategy_id, is_enabled, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING id, profile_id, vendor_id, api_family, model_id, display_name, model_type, loadbalance_strategy_id, is_enabled, created_at, updated_at`, record.ProfileID, nullableInt(record.VendorID), record.APIFamily, record.ModelID, nullableString(record.DisplayName), record.ModelType, nullableInt(record.LoadbalanceStrategyID), record.IsEnabled, record.CreatedAt, record.UpdatedAt))
	if err != nil {
		if isUniqueViolation(err, "uq_model_configs_profile_model_id") {
			return modelRecord{}, &domainError{StatusCode: 409, Detail: fmt.Sprintf("Model ID '%s' already exists", record.ModelID)}
		}
		return modelRecord{}, fmt.Errorf("insert model %q: %w", record.ModelID, err)
	}
	return created, nil
}

func updateModel(ctx context.Context, tx pgx.Tx, record modelRecord) (modelRecord, error) {
	updated, err := scanModelRecord(tx.QueryRow(ctx, `UPDATE model_configs SET vendor_id = $2, api_family = $3, model_id = $4, display_name = $5, model_type = $6, loadbalance_strategy_id = $7, is_enabled = $8, updated_at = $9 WHERE id = $1 RETURNING id, profile_id, vendor_id, api_family, model_id, display_name, model_type, loadbalance_strategy_id, is_enabled, created_at, updated_at`, record.ID, nullableInt(record.VendorID), record.APIFamily, record.ModelID, nullableString(record.DisplayName), record.ModelType, nullableInt(record.LoadbalanceStrategyID), record.IsEnabled, record.UpdatedAt))
	if err != nil {
		if isUniqueViolation(err, "uq_model_configs_profile_model_id") {
			return modelRecord{}, &domainError{StatusCode: 409, Detail: fmt.Sprintf("Model ID '%s' already exists", record.ModelID)}
		}
		return modelRecord{}, fmt.Errorf("update model %d: %w", record.ID, err)
	}
	return updated, nil
}

func deleteModel(ctx context.Context, tx pgx.Tx, modelConfigID int) error {
	if _, err := tx.Exec(ctx, `DELETE FROM model_configs WHERE id = $1`, modelConfigID); err != nil {
		return fmt.Errorf("delete model %d: %w", modelConfigID, err)
	}
	return nil
}

func syncRenamedModelReferences(ctx context.Context, tx pgx.Tx, profileID int, originalModelID string, newModelID string) error {
	if _, err := tx.Exec(ctx, `UPDATE endpoint_fx_rate_settings SET model_id = $3 WHERE profile_id = $1 AND model_id = $2`, profileID, originalModelID, newModelID); err != nil {
		return fmt.Errorf("sync renamed model references from %q to %q: %w", originalModelID, newModelID, err)
	}
	return nil
}

func scanModelRecord(scanner interface{ Scan(...any) error }) (modelRecord, error) {
	var vendorID sql.NullInt32
	var displayName sql.NullString
	var loadbalanceStrategyID sql.NullInt32
	record := modelRecord{}
	if err := scanner.Scan(&record.ID, &record.ProfileID, &vendorID, &record.APIFamily, &record.ModelID, &displayName, &record.ModelType, &loadbalanceStrategyID, &record.IsEnabled, &record.CreatedAt, &record.UpdatedAt); err != nil {
		return modelRecord{}, err
	}
	record.VendorID = nullableInt32(vendorID)
	record.DisplayName = nullableStringValue(displayName)
	record.LoadbalanceStrategyID = nullableInt32(loadbalanceStrategyID)
	return record, nil
}

func scanVendorRecord(scanner interface{ Scan(...any) error }) (vendorRecord, error) {
	var description sql.NullString
	var iconKey sql.NullString
	record := vendorRecord{}
	if err := scanner.Scan(&record.ID, &record.Key, &record.Name, &description, &iconKey, &record.AuditEnabled, &record.AuditCaptureBodies, &record.CreatedAt, &record.UpdatedAt); err != nil {
		return vendorRecord{}, err
	}
	record.Description = nullableStringValue(description)
	record.IconKey = nullableStringValue(iconKey)
	return record, nil
}

func scanStrategyRecord(scanner interface{ Scan(...any) error }) (strategyRecord, error) {
	var legacyStrategyType sql.NullString
	var autoRecoveryRaw []byte
	var routingPolicyRaw []byte
	record := strategyRecord{}
	if err := scanner.Scan(&record.ID, &record.Name, &record.StrategyType, &legacyStrategyType, &autoRecoveryRaw, &routingPolicyRaw); err != nil {
		return strategyRecord{}, err
	}
	record.LegacyStrategyType = nullableStringValue(legacyStrategyType)
	if len(autoRecoveryRaw) > 0 {
		if err := json.Unmarshal(autoRecoveryRaw, &record.AutoRecovery); err != nil {
			return strategyRecord{}, fmt.Errorf("decode strategy auto_recovery: %w", err)
		}
	}
	if len(routingPolicyRaw) > 0 {
		if err := json.Unmarshal(routingPolicyRaw, &record.RoutingPolicy); err != nil {
			return strategyRecord{}, fmt.Errorf("decode strategy routing_policy: %w", err)
		}
	}
	return record, nil
}

func scanConnectionResponse(scanner interface{ Scan(...any) error }) (connectionResponse, error) {
	var endpointAPIKey sql.NullString
	var connectionName sql.NullString
	var authType sql.NullString
	var customHeaders sql.NullString
	var openAIProbeEndpointVariant sql.NullString
	var pricingTemplateID sql.NullInt32
	var qpsLimit sql.NullInt32
	var maxInFlightNonStream sql.NullInt32
	var maxInFlightStream sql.NullInt32
	var templateID sql.NullInt32
	var templateName sql.NullString
	var templatePricingUnit sql.NullString
	var templatePricingCurrencyCode sql.NullString
	var templateVersion sql.NullInt32
	var healthDetail sql.NullString
	var lastHealthCheck sql.NullTime
	item := connectionResponse{}
	endpoint := endpointResponse{}
	if err := scanner.Scan(&item.ID, &item.ProfileID, &item.ModelConfigID, &item.EndpointID, &endpoint.ProfileID, &endpoint.Name, &endpoint.BaseURL, &endpointAPIKey, &endpoint.Position, &endpoint.CreatedAt, &endpoint.UpdatedAt, &item.IsActive, &item.Priority, &connectionName, &authType, &customHeaders, &openAIProbeEndpointVariant, &pricingTemplateID, &qpsLimit, &maxInFlightNonStream, &maxInFlightStream, &templateID, &templateName, &templatePricingUnit, &templatePricingCurrencyCode, &templateVersion, &item.HealthStatus, &healthDetail, &lastHealthCheck, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return connectionResponse{}, err
	}
	endpoint.ID = item.EndpointID
	endpoint.HasAPIKey = strings.TrimSpace(endpointAPIKey.String) != ""
	endpoint.MaskedAPIKey = maskedAPIKey(endpointAPIKey)
	item.Endpoint = &endpoint
	item.Name = nullableStringValue(connectionName)
	item.AuthType = nullableStringValue(authType)
	item.CustomHeaders = parseCustomHeaders(customHeaders)
	item.OpenAIProbeEndpointVariant = nullableStringValue(openAIProbeEndpointVariant)
	item.PricingTemplateID = nullableInt32(pricingTemplateID)
	item.QPSLimit = nullableInt32(qpsLimit)
	item.MaxInFlightNonStream = nullableInt32(maxInFlightNonStream)
	item.MaxInFlightStream = nullableInt32(maxInFlightStream)
	item.HealthDetail = nullableStringValue(healthDetail)
	item.LastHealthCheck = nullableTime(lastHealthCheck)
	if templateID.Valid {
		item.PricingTemplate = &connectionPricingTemplateSummary{ID: int(templateID.Int32), Name: templateName.String, PricingUnit: templatePricingUnit.String, PricingCurrencyCode: templatePricingCurrencyCode.String, Version: int(templateVersion.Int32)}
	}
	return item, nil
}

func buildModelListResponse(record modelRecord, vendors map[int]vendorRecord, strategies map[int]strategyRecord, proxyTargets map[int][]proxyTargetReference, counts map[int]modelConnectionCounts, health map[string]modelHealthStats) modelConfigListResponse {
	response := modelConfigListResponse{ID: record.ID, ProfileID: record.ProfileID, VendorID: record.VendorID, APIFamily: record.APIFamily, ModelID: record.ModelID, DisplayName: record.DisplayName, ModelType: record.ModelType, ProxyTargets: cloneProxyTargets(proxyTargets[record.ID]), LoadbalanceStrategyID: record.LoadbalanceStrategyID, IsEnabled: record.IsEnabled, HealthTotalRequests: 0, CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt}
	if record.VendorID != nil {
		if vendor, ok := vendors[*record.VendorID]; ok {
			response.Vendor = vendorResponseFromRecord(vendor)
		}
	}
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

func buildModelDetailResponse(record modelRecord, vendors map[int]vendorRecord, strategies map[int]strategyRecord, proxyTargets map[int][]proxyTargetReference, connections []connectionResponse) modelConfigResponse {
	response := modelConfigResponse{ID: record.ID, ProfileID: record.ProfileID, VendorID: record.VendorID, APIFamily: record.APIFamily, ModelID: record.ModelID, DisplayName: record.DisplayName, ModelType: record.ModelType, ProxyTargets: cloneProxyTargets(proxyTargets[record.ID]), LoadbalanceStrategyID: record.LoadbalanceStrategyID, IsEnabled: record.IsEnabled, Connections: connections, CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt}
	if response.Connections == nil {
		response.Connections = []connectionResponse{}
	}
	if record.VendorID != nil {
		if vendor, ok := vendors[*record.VendorID]; ok {
			response.Vendor = vendorResponseFromRecord(vendor)
		}
	}
	if record.LoadbalanceStrategyID != nil {
		if strategy, ok := strategies[*record.LoadbalanceStrategyID]; ok {
			response.LoadbalanceStrategy = strategySummaryFromRecord(strategy)
		}
	}
	return response
}

func vendorResponseFromRecord(record vendorRecord) *vendorResponse {
	return &vendorResponse{ID: record.ID, Key: record.Key, Name: record.Name, Description: record.Description, IconKey: record.IconKey, IsReadonly: vendordomain.IsReadonlyVendorKey(record.Key), AuditEnabled: record.AuditEnabled, AuditCaptureBodies: record.AuditCaptureBodies, CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt}
}

func strategySummaryFromRecord(record strategyRecord) *loadbalanceStrategySummary {
	return &loadbalanceStrategySummary{ID: record.ID, Name: record.Name, StrategyType: record.StrategyType, LegacyStrategyType: record.LegacyStrategyType, AutoRecovery: record.AutoRecovery, RoutingPolicy: record.RoutingPolicy}
}

func cloneProxyTargets(values []proxyTargetReference) []proxyTargetReference {
	if len(values) == 0 {
		return []proxyTargetReference{}
	}
	cloned := make([]proxyTargetReference, len(values))
	copy(cloned, values)
	return cloned
}

func placeholders(start int, count int) string {
	parts := make([]string, count)
	for index := 0; index < count; index++ {
		parts[index] = fmt.Sprintf("$%d", start+index)
	}
	return strings.Join(parts, ", ")
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

func nullableStringValue(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	resolved := value.String
	return &resolved
}

func nullableTime(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	resolved := value.Time.UTC()
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

func maskedAPIKey(value sql.NullString) *string {
	if !value.Valid || strings.TrimSpace(value.String) == "" {
		return nil
	}
	masked := "********"
	return &masked
}

func sortModelRecordsByID(records []modelRecord) {
	sort.Slice(records, func(left int, right int) bool {
		return records[left].ID < records[right].ID
	})
}
