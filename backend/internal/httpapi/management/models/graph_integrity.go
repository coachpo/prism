package models

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/coachpo/prism/backend/internal/domain/modelrouting"
	"github.com/coachpo/prism/backend/internal/providerauth"
)

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
