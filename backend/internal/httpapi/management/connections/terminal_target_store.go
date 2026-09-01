package connections

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/coachpo/prism/backend/internal/domain/terminaltarget"
	"github.com/jackc/pgx/v5"
)

func loadConnectionRecord(ctx context.Context, exec queryExecutor, profileID int, connectionID int, forUpdate bool, now time.Time) (connectionResponse, bool, error) {
	query := connectionSelectQuery + ` WHERE model_access_targets.profile_id = $1 AND connections.id = $2`
	if forUpdate {
		query += ` FOR UPDATE OF connections`
	}
	query += ` LIMIT 1`
	item, err := scanConnectionResponse(exec.QueryRow(ctx, query, profileID, connectionID))
	if err == pgx.ErrNoRows {
		return connectionResponse{}, false, nil
	}
	if err != nil {
		return connectionResponse{}, false, fmt.Errorf("load connection %d in profile %d: %w", connectionID, profileID, err)
	}
	items := []connectionResponse{item}
	if err := attachConnectionRoutingWindows(ctx, exec, profileID, items, now); err != nil {
		return connectionResponse{}, false, err
	}
	return items[0], true, nil
}

func listConnections(ctx context.Context, exec queryExecutor, profileID int, now time.Time) ([]connectionResponse, error) {
	rows, err := exec.Query(ctx, connectionSelectQuery+` WHERE model_access_targets.profile_id = $1 ORDER BY model_access_targets.position ASC, connections.id ASC`, profileID)
	if err != nil {
		return nil, fmt.Errorf("query connections for profile %d: %w", profileID, err)
	}
	items, err := scanConnectionRows(rows, fmt.Sprintf("iterate connections for profile %d", profileID))
	rows.Close()
	if err != nil {
		return nil, err
	}
	if err := attachConnectionRoutingWindows(ctx, exec, profileID, items, now); err != nil {
		return nil, err
	}
	return items, nil
}

func listConnectionsForModel(ctx context.Context, exec queryExecutor, profileID int, modelConfigID int, now time.Time) ([]connectionResponse, error) {
	rows, err := exec.Query(ctx, connectionSelectQuery+` WHERE model_access_targets.profile_id = $1 AND model_access_targets.source_model_config_id = $2 ORDER BY model_access_targets.position ASC, connections.id ASC`, profileID, modelConfigID)
	if err != nil {
		return nil, fmt.Errorf("query connections for model %d: %w", modelConfigID, err)
	}
	items, err := scanConnectionRows(rows, fmt.Sprintf("iterate connections for model %d", modelConfigID))
	rows.Close()
	if err != nil {
		return nil, err
	}
	if err := attachConnectionRoutingWindows(ctx, exec, profileID, items, now); err != nil {
		return nil, err
	}
	return items, nil
}

func loadModelConnectionRecord(ctx context.Context, exec queryExecutor, profileID int, modelConfigID int, connectionID int, now time.Time) (connectionResponse, bool, error) {
	item, err := scanConnectionResponse(exec.QueryRow(ctx, connectionSelectQuery+` WHERE model_access_targets.profile_id = $1 AND model_access_targets.source_model_config_id = $2 AND connections.id = $3 LIMIT 1`, profileID, modelConfigID, connectionID))
	if err == pgx.ErrNoRows {
		return connectionResponse{}, false, nil
	}
	if err != nil {
		return connectionResponse{}, false, fmt.Errorf("load connection %d for model %d: %w", connectionID, modelConfigID, err)
	}
	items := []connectionResponse{item}
	if err := attachConnectionRoutingWindows(ctx, exec, profileID, items, now); err != nil {
		return connectionResponse{}, false, err
	}
	return items[0], true, nil
}

// loadConnectionCustomHeadersRaw returns the stored custom header map without
// any wire masking, for write paths that must resolve redaction sentinels or
// clone values verbatim.
func loadConnectionCustomHeadersRaw(ctx context.Context, exec queryExecutor, profileID int, connectionID int) (map[string]string, error) {
	var raw sql.NullString
	if err := exec.QueryRow(ctx, `SELECT custom_headers FROM connections WHERE profile_id = $1 AND id = $2`, profileID, connectionID).Scan(&raw); err != nil {
		return nil, fmt.Errorf("load connection %d custom headers: %w", connectionID, err)
	}
	return parseCustomHeaders(raw), nil
}

func insertTerminalTarget(ctx context.Context, exec queryExecutor, item terminaltarget.Record) (int, error) {
	var terminalTargetID int
	err := exec.QueryRow(ctx, `INSERT INTO connections (profile_id, api_family, endpoint_id, upstream_model_id, pricing_template_id, qps_limit, max_in_flight_non_stream, max_in_flight_stream, openai_text_capability, openai_image_capability, is_active, priority, name, auth_type, custom_headers, custom_request_parameters, routing_schedule_timezone, health_status, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, 0, $12, $13, $14, $15, $18, 'unknown', $16, $17) RETURNING id`, item.ProfileID, item.APIFamily, item.EndpointID, nullableString(item.UpstreamModelID), nullableInt(item.PricingTemplateID), nullableInt(item.QPSLimit), nullableInt(item.MaxInFlightNonStream), nullableInt(item.MaxInFlightStream), nullableString(item.OpenAITextCapability), nullableString(item.OpenAIImageCapability), item.IsActive, nullableString(item.Name), nullableString(item.AuthType), nullableJSONString(item.CustomHeaders), nullableCustomRequestParametersArg(item.CustomRequestParameters), item.CreatedAt, item.UpdatedAt, nullableString(item.RoutingScheduleTimezone)).Scan(&terminalTargetID)
	if err != nil {
		return 0, fmt.Errorf("insert terminal target: %w", err)
	}
	return terminalTargetID, nil
}

func updateTerminalTarget(ctx context.Context, exec queryExecutor, item terminaltarget.Record) error {
	if _, err := exec.Exec(ctx, `UPDATE connections SET api_family = $2, endpoint_id = $3, upstream_model_id = $4, pricing_template_id = $5, qps_limit = $6, max_in_flight_non_stream = $7, max_in_flight_stream = $8, openai_text_capability = $9, openai_image_capability = $10, is_active = $11, name = $12, auth_type = $13, custom_headers = $14, custom_request_parameters = $15, updated_at = $16, routing_schedule_timezone = $17 WHERE id = $1`, item.ID, item.APIFamily, item.EndpointID, nullableString(item.UpstreamModelID), nullableInt(item.PricingTemplateID), nullableInt(item.QPSLimit), nullableInt(item.MaxInFlightNonStream), nullableInt(item.MaxInFlightStream), nullableString(item.OpenAITextCapability), nullableString(item.OpenAIImageCapability), item.IsActive, nullableString(item.Name), nullableString(item.AuthType), nullableJSONString(item.CustomHeaders), nullableCustomRequestParametersArg(item.CustomRequestParameters), item.UpdatedAt, nullableString(item.RoutingScheduleTimezone)); err != nil {
		return fmt.Errorf("update terminal target %d: %w", item.ID, err)
	}
	return nil
}

func deleteTerminalTarget(ctx context.Context, exec queryExecutor, terminalTargetID int) error {
	if _, err := exec.Exec(ctx, `DELETE FROM connections WHERE id = $1`, terminalTargetID); err != nil {
		return fmt.Errorf("delete terminal target %d: %w", terminalTargetID, err)
	}
	return nil
}

func listConnectionsByModelIDs(ctx context.Context, exec queryExecutor, profileID int, modelConfigIDs []int, now time.Time) (map[int][]connectionResponse, error) {
	items := make(map[int][]connectionResponse, len(modelConfigIDs))
	if len(modelConfigIDs) == 0 {
		return items, nil
	}
	args := []any{profileID, int32ArrayArg(modelConfigIDs)}
	query := connectionSelectQuery + ` WHERE model_access_targets.profile_id = $1 AND model_access_targets.source_model_config_id = ANY($2) ORDER BY model_access_targets.source_model_config_id ASC, model_access_targets.position ASC, connections.id ASC`
	rows, err := exec.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query connection batch for profile %d: %w", profileID, err)
	}
	// Rows are drained into a flat slice and closed before the window batch
	// read: the connection is busy while rows are open, so issuing the second
	// query inside the loop would fail.
	scanned := make([]connectionResponse, 0, len(modelConfigIDs))
	for rows.Next() {
		item, scanErr := scanConnectionResponse(rows)
		if scanErr != nil {
			rows.Close()
			return nil, scanErr
		}
		scanned = append(scanned, item)
	}
	iterateErr := rows.Err()
	rows.Close()
	if iterateErr != nil {
		return nil, fmt.Errorf("iterate connection batch for profile %d: %w", profileID, iterateErr)
	}
	if err := attachConnectionRoutingWindows(ctx, exec, profileID, scanned, now); err != nil {
		return nil, err
	}
	for _, item := range scanned {
		if item.ModelConfigID != nil {
			items[*item.ModelConfigID] = append(items[*item.ModelConfigID], item)
		}
	}
	return items, nil
}
