package models

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/coachpo/prism/backend/internal/domain/modelrouting"
)

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
