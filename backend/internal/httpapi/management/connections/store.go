package connections

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/coachpo/prism/backend/internal/domain/terminaltarget"
	"github.com/coachpo/prism/backend/internal/endpointdomain"
)

type queryExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type modelRecord struct {
	ID                   int
	ProfileID            int
	ModelID              string
	APIFamily            string
	IsEnabled            bool
	OpenAIAcceptedFormat *string
}

type endpointRecord struct {
	ID        int
	ProfileID int
	Name      string
	BaseURL   string
	APIKey    string
	Position  int
	CreatedAt time.Time
	UpdatedAt time.Time
}

type connectionReferenceRecord struct {
	TargetID      int
	ModelConfigID int
	ModelID       string
	APIFamily     string
	Position      int
	IsEnabled     bool
}

type connectionReferenceModeRecord struct {
	ModelID              string
	OpenAIAcceptedFormat *string
}

type headerBlocklistRuleRecord struct {
	MatchType string
	Pattern   string
}

type pricingTemplateConnectionUsageRecord struct {
	ConnectionID   int
	ConnectionName *string
	ModelConfigID  int
	ModelID        *string
	EndpointID     int
	EndpointName   *string
}

type pricingTemplateResponse struct {
	ID                  int       `json:"id"`
	ProfileID           int       `json:"profile_id"`
	Name                string    `json:"name"`
	Description         *string   `json:"description"`
	PricingUnit         string    `json:"pricing_unit"`
	PricingCurrencyCode string    `json:"pricing_currency_code"`
	InputPrice          string    `json:"input_price"`
	OutputPrice         string    `json:"output_price"`
	CachedInputPrice    string    `json:"cached_input_price"`
	CacheCreationPrice  string    `json:"cache_creation_price"`
	ReasoningPrice      string    `json:"reasoning_price"`
	Version             int       `json:"version"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

const pricingTemplateSelectQuery = `SELECT id, profile_id, name, description, pricing_unit, pricing_currency_code, COALESCE(input_price, '0'), COALESCE(output_price, '0'), COALESCE(cached_input_price, '0'), COALESCE(cache_creation_price, '0'), COALESCE(reasoning_price, '0'), version, created_at, updated_at FROM pricing_templates`

func loadModelRecord(ctx context.Context, exec queryExecutor, profileID int, modelConfigID int, forUpdate bool) (modelRecord, bool, error) {
	query := `SELECT id, profile_id, model_id, api_family, is_enabled, openai_accepted_format FROM model_configs WHERE profile_id = $1 AND id = $2`
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

func ensureModelConfigIDsExist(ctx context.Context, exec queryExecutor, profileID int, modelConfigIDs []int) error {
	if len(modelConfigIDs) == 0 {
		return &domainError{StatusCode: 400, Detail: "model_config_ids must contain at least one model config id"}
	}
	args := []any{profileID, int32ArrayArg(modelConfigIDs)}
	query := `SELECT id FROM model_configs WHERE profile_id = $1 AND id = ANY($2) ORDER BY id ASC`
	rows, err := exec.Query(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("query model ids for profile %d: %w", profileID, err)
	}
	defer rows.Close()

	existing := map[int]struct{}{}
	for rows.Next() {
		var modelConfigID int
		if err := rows.Scan(&modelConfigID); err != nil {
			return fmt.Errorf("scan model id: %w", err)
		}
		existing[modelConfigID] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate model ids for profile %d: %w", profileID, err)
	}
	for _, modelConfigID := range modelConfigIDs {
		if _, ok := existing[modelConfigID]; !ok {
			return &domainError{StatusCode: 404, Detail: "Model configuration not found"}
		}
	}
	return nil
}

func loadProfileEndpointRecord(ctx context.Context, exec queryExecutor, profileID int, endpointID int) (endpointRecord, bool, error) {
	record, err := scanEndpointRecord(exec.QueryRow(ctx, `SELECT id, profile_id, name, base_url, api_key, position, created_at, updated_at FROM endpoints WHERE profile_id = $1 AND id = $2 LIMIT 1`, profileID, endpointID))
	if err == pgx.ErrNoRows {
		return endpointRecord{}, false, nil
	}
	if err != nil {
		return endpointRecord{}, false, fmt.Errorf("load endpoint %d in profile %d: %w", endpointID, profileID, err)
	}
	return record, true, nil
}

func ensureUniqueEndpointName(ctx context.Context, exec queryExecutor, profileID int, endpointName string) error {
	var existingID int
	err := exec.QueryRow(ctx, `SELECT id FROM endpoints WHERE profile_id = $1 AND name = $2 LIMIT 1`, profileID, endpointName).Scan(&existingID)
	if err == nil {
		return &domainError{StatusCode: 409, Detail: fmt.Sprintf("Endpoint name '%s' already exists", endpointName)}
	}
	if err == pgx.ErrNoRows {
		return nil
	}
	return fmt.Errorf("query endpoint name availability for %q: %w", endpointName, err)
}

func ensureUniquePricingTemplateName(ctx context.Context, exec queryExecutor, profileID int, templateName string, excludeID *int) error {
	var existingID int
	err := exec.QueryRow(ctx, `SELECT id FROM pricing_templates WHERE profile_id = $1 AND name = $2 LIMIT 1`, profileID, templateName).Scan(&existingID)
	if err == nil {
		if excludeID != nil && existingID == *excludeID {
			return nil
		}
		return &domainError{StatusCode: 409, Detail: fmt.Sprintf("Pricing template name '%s' already exists", templateName)}
	}
	if err == pgx.ErrNoRows {
		return nil
	}
	return fmt.Errorf("query pricing template name availability for %q: %w", templateName, err)
}

func nextEndpointPosition(ctx context.Context, exec queryExecutor, profileID int) (int, error) {
	var maxPosition sql.NullInt32
	if err := exec.QueryRow(ctx, `SELECT MAX(position) FROM endpoints WHERE profile_id = $1`, profileID).Scan(&maxPosition); err != nil {
		return 0, fmt.Errorf("query next endpoint position for profile %d: %w", profileID, err)
	}
	if !maxPosition.Valid {
		return 0, nil
	}
	return int(maxPosition.Int32) + 1, nil
}

func nextModelAccessTargetPosition(ctx context.Context, exec queryExecutor, profileID int, modelConfigID int) (int, error) {
	var maxPosition sql.NullInt32
	if err := exec.QueryRow(ctx, `SELECT MAX(position) FROM model_access_targets WHERE profile_id = $1 AND source_model_config_id = $2`, profileID, modelConfigID).Scan(&maxPosition); err != nil {
		return 0, fmt.Errorf("query next access target position for model %d: %w", modelConfigID, err)
	}
	if !maxPosition.Valid {
		return 0, nil
	}
	return int(maxPosition.Int32) + 1, nil
}

func insertEndpoint(ctx context.Context, exec queryExecutor, record endpointRecord) (endpointRecord, error) {
	created, err := scanEndpointRecord(exec.QueryRow(ctx, `INSERT INTO endpoints (profile_id, name, base_url, api_key, position, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id, profile_id, name, base_url, api_key, position, created_at, updated_at`, record.ProfileID, record.Name, record.BaseURL, record.APIKey, record.Position, record.CreatedAt, record.UpdatedAt))
	if err != nil {
		return endpointRecord{}, fmt.Errorf("insert inline endpoint %q: %w", record.Name, err)
	}
	return created, nil
}

func listEnabledHeaderBlocklistRules(ctx context.Context, exec queryExecutor, profileID int) ([]headerBlocklistRuleRecord, error) {
	rows, err := exec.Query(ctx, `SELECT match_type, pattern FROM header_blocklist_rules WHERE enabled = TRUE AND (is_system = TRUE OR profile_id = $1) ORDER BY is_system DESC, id ASC`, profileID)
	if err != nil {
		return nil, fmt.Errorf("query header blocklist rules for profile %d: %w", profileID, err)
	}
	defer rows.Close()
	items := make([]headerBlocklistRuleRecord, 0)
	for rows.Next() {
		var item headerBlocklistRuleRecord
		if err := rows.Scan(&item.MatchType, &item.Pattern); err != nil {
			return nil, fmt.Errorf("scan header blocklist rule: %w", err)
		}
		item.MatchType = strings.ToLower(strings.TrimSpace(item.MatchType))
		item.Pattern = strings.ToLower(strings.TrimSpace(item.Pattern))
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate header blocklist rules for profile %d: %w", profileID, err)
	}
	return items, nil
}

func validatePricingTemplateID(ctx context.Context, exec queryExecutor, profileID int, pricingTemplateID *int) (*int, error) {
	if pricingTemplateID == nil {
		return nil, nil
	}
	var existingID int
	err := exec.QueryRow(ctx, `SELECT id FROM pricing_templates WHERE profile_id = $1 AND id = $2 LIMIT 1`, profileID, *pricingTemplateID).Scan(&existingID)
	if err == pgx.ErrNoRows {
		return nil, &domainError{StatusCode: 404, Detail: "Pricing template not found"}
	}
	if err != nil {
		return nil, fmt.Errorf("load pricing template %d for profile %d: %w", *pricingTemplateID, profileID, err)
	}
	resolved := existingID
	return &resolved, nil
}

func loadPricingTemplate(ctx context.Context, exec queryExecutor, profileID int, templateID int, forUpdate bool) (pricingTemplateResponse, bool, error) {
	query := pricingTemplateSelectQuery + ` WHERE profile_id = $1 AND id = $2`
	if forUpdate {
		query += ` FOR UPDATE OF pricing_templates`
	}
	query += ` LIMIT 1`
	item, err := scanPricingTemplateResponse(exec.QueryRow(ctx, query, profileID, templateID))
	if err == pgx.ErrNoRows {
		return pricingTemplateResponse{}, false, nil
	}
	if err != nil {
		return pricingTemplateResponse{}, false, fmt.Errorf("load pricing template %d in profile %d: %w", templateID, profileID, err)
	}
	return item, true, nil
}

func listPricingTemplates(ctx context.Context, exec queryExecutor, profileID int) ([]pricingTemplateResponse, error) {
	rows, err := exec.Query(ctx, pricingTemplateSelectQuery+` WHERE profile_id = $1 ORDER BY updated_at DESC, id DESC`, profileID)
	if err != nil {
		return nil, fmt.Errorf("query pricing templates for profile %d: %w", profileID, err)
	}
	defer rows.Close()
	items := make([]pricingTemplateResponse, 0)
	for rows.Next() {
		item, scanErr := scanPricingTemplateResponse(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pricing templates for profile %d: %w", profileID, err)
	}
	return items, nil
}

func insertPricingTemplate(ctx context.Context, exec queryExecutor, item pricingTemplateResponse) (pricingTemplateResponse, error) {
	created, err := scanPricingTemplateResponse(exec.QueryRow(ctx, `INSERT INTO pricing_templates (profile_id, name, description, pricing_unit, pricing_currency_code, input_price, output_price, cached_input_price, cache_creation_price, reasoning_price, version, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13) RETURNING id, profile_id, name, description, pricing_unit, pricing_currency_code, input_price, output_price, cached_input_price, cache_creation_price, reasoning_price, version, created_at, updated_at`, item.ProfileID, item.Name, nullableString(item.Description), item.PricingUnit, item.PricingCurrencyCode, item.InputPrice, item.OutputPrice, item.CachedInputPrice, item.CacheCreationPrice, item.ReasoningPrice, item.Version, item.CreatedAt, item.UpdatedAt))
	if err != nil {
		return pricingTemplateResponse{}, fmt.Errorf("insert pricing template %q: %w", item.Name, err)
	}
	return created, nil
}

func updatePricingTemplate(ctx context.Context, exec queryExecutor, item pricingTemplateResponse) error {
	if _, err := exec.Exec(ctx, `UPDATE pricing_templates SET name = $2, description = $3, pricing_unit = $4, pricing_currency_code = $5, input_price = $6, output_price = $7, cached_input_price = $8, cache_creation_price = $9, reasoning_price = $10, version = $11, updated_at = $12 WHERE id = $1`, item.ID, item.Name, nullableString(item.Description), item.PricingUnit, item.PricingCurrencyCode, item.InputPrice, item.OutputPrice, item.CachedInputPrice, item.CacheCreationPrice, item.ReasoningPrice, item.Version, item.UpdatedAt); err != nil {
		return fmt.Errorf("update pricing template %d: %w", item.ID, err)
	}
	return nil
}

func deletePricingTemplate(ctx context.Context, exec queryExecutor, templateID int) error {
	if _, err := exec.Exec(ctx, `DELETE FROM pricing_templates WHERE id = $1`, templateID); err != nil {
		return fmt.Errorf("delete pricing template %d: %w", templateID, err)
	}
	return nil
}

func loadConnectionRecord(ctx context.Context, exec queryExecutor, profileID int, connectionID int, forUpdate bool) (connectionResponse, bool, error) {
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
	return item, true, nil
}

func listConnections(ctx context.Context, exec queryExecutor, profileID int) ([]connectionResponse, error) {
	rows, err := exec.Query(ctx, connectionSelectQuery+` WHERE model_access_targets.profile_id = $1 ORDER BY model_access_targets.position ASC, connections.id ASC`, profileID)
	if err != nil {
		return nil, fmt.Errorf("query connections for profile %d: %w", profileID, err)
	}
	defer rows.Close()
	return scanConnectionRows(rows, fmt.Sprintf("iterate connections for profile %d", profileID))
}

func listConnectionsForModel(ctx context.Context, exec queryExecutor, profileID int, modelConfigID int) ([]connectionResponse, error) {
	rows, err := exec.Query(ctx, connectionSelectQuery+` WHERE model_access_targets.profile_id = $1 AND model_access_targets.source_model_config_id = $2 ORDER BY model_access_targets.position ASC, connections.id ASC`, profileID, modelConfigID)
	if err != nil {
		return nil, fmt.Errorf("query connections for model %d: %w", modelConfigID, err)
	}
	defer rows.Close()
	return scanConnectionRows(rows, fmt.Sprintf("iterate connections for model %d", modelConfigID))
}

func loadModelConnectionRecord(ctx context.Context, exec queryExecutor, profileID int, modelConfigID int, connectionID int) (connectionResponse, bool, error) {
	item, err := scanConnectionResponse(exec.QueryRow(ctx, connectionSelectQuery+` WHERE model_access_targets.profile_id = $1 AND model_access_targets.source_model_config_id = $2 AND connections.id = $3 LIMIT 1`, profileID, modelConfigID, connectionID))
	if err == pgx.ErrNoRows {
		return connectionResponse{}, false, nil
	}
	if err != nil {
		return connectionResponse{}, false, fmt.Errorf("load connection %d for model %d: %w", connectionID, modelConfigID, err)
	}
	return item, true, nil
}

func loadConnectionOwnerReference(ctx context.Context, exec queryExecutor, profileID int, modelConfigID int, connectionID int, forUpdate bool) (connectionReferenceRecord, bool, error) {
	query := `SELECT model_access_targets.id, model_configs.id, model_configs.model_id, model_configs.api_family, model_access_targets.position, model_access_targets.is_enabled FROM model_access_targets JOIN model_configs ON model_configs.id = model_access_targets.source_model_config_id WHERE model_access_targets.profile_id = $1 AND model_access_targets.source_model_config_id = $2 AND model_access_targets.target_connection_id = $3`
	if forUpdate {
		query += ` FOR UPDATE OF model_access_targets`
	}
	query += ` LIMIT 1`
	record, err := scanConnectionReferenceRecord(exec.QueryRow(ctx, query, profileID, modelConfigID, connectionID))
	if err == pgx.ErrNoRows {
		return connectionReferenceRecord{}, false, nil
	}
	if err != nil {
		return connectionReferenceRecord{}, false, fmt.Errorf("load owner target for model %d connection %d: %w", modelConfigID, connectionID, err)
	}
	return record, true, nil
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

func listConnectionsByModelIDs(ctx context.Context, exec queryExecutor, profileID int, modelConfigIDs []int) (map[int][]connectionResponse, error) {
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
	defer rows.Close()
	for rows.Next() {
		item, scanErr := scanConnectionResponse(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		if item.ModelConfigID != nil {
			items[*item.ModelConfigID] = append(items[*item.ModelConfigID], item)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate connection batch for profile %d: %w", profileID, err)
	}
	return items, nil
}

func insertTerminalTarget(ctx context.Context, exec queryExecutor, item terminaltarget.Record) (int, error) {
	var terminalTargetID int
	err := exec.QueryRow(ctx, `INSERT INTO connections (profile_id, api_family, endpoint_id, pricing_template_id, qps_limit, max_in_flight_non_stream, max_in_flight_stream, openai_text_capability, is_active, priority, name, auth_type, custom_headers, custom_request_parameters, health_status, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, 'unknown', $15, $16) RETURNING id`, item.ProfileID, item.APIFamily, item.EndpointID, nullableInt(item.PricingTemplateID), nullableInt(item.QPSLimit), nullableInt(item.MaxInFlightNonStream), nullableInt(item.MaxInFlightStream), nullableString(item.OpenAITextCapability), item.IsActive, item.Priority, nullableString(item.Name), nullableString(item.AuthType), nullableJSONString(item.CustomHeaders), nullableCustomRequestParametersArg(item.CustomRequestParameters), item.CreatedAt, item.UpdatedAt).Scan(&terminalTargetID)
	if err != nil {
		return 0, fmt.Errorf("insert terminal target: %w", err)
	}
	return terminalTargetID, nil
}

func insertOwnerTerminalTargetAccess(ctx context.Context, exec queryExecutor, profileID int, modelConfigID int, terminalTargetID int, position int, currentTime time.Time) error {
	if _, err := exec.Exec(ctx, `INSERT INTO model_access_targets (profile_id, source_model_config_id, target_type, target_connection_id, position, is_enabled, created_at, updated_at) VALUES ($1, $2, 'connection', $3, $4, TRUE, $5, $5)`, profileID, modelConfigID, terminalTargetID, position, currentTime); err != nil {
		return fmt.Errorf("insert owner terminal target access for model %d terminal target %d: %w", modelConfigID, terminalTargetID, err)
	}
	return nil
}

func updateTerminalTarget(ctx context.Context, exec queryExecutor, item terminaltarget.Record) error {
	if _, err := exec.Exec(ctx, `UPDATE connections SET api_family = $2, endpoint_id = $3, pricing_template_id = $4, qps_limit = $5, max_in_flight_non_stream = $6, max_in_flight_stream = $7, openai_text_capability = $8, is_active = $9, priority = $10, name = $11, auth_type = $12, custom_headers = $13, custom_request_parameters = $14, updated_at = $15 WHERE id = $1`, item.ID, item.APIFamily, item.EndpointID, nullableInt(item.PricingTemplateID), nullableInt(item.QPSLimit), nullableInt(item.MaxInFlightNonStream), nullableInt(item.MaxInFlightStream), nullableString(item.OpenAITextCapability), item.IsActive, item.Priority, nullableString(item.Name), nullableString(item.AuthType), nullableJSONString(item.CustomHeaders), nullableCustomRequestParametersArg(item.CustomRequestParameters), item.UpdatedAt); err != nil {
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

func deleteModelAccessTargetRow(ctx context.Context, exec queryExecutor, targetID int) error {
	if _, err := exec.Exec(ctx, `DELETE FROM model_access_targets WHERE id = $1`, targetID); err != nil {
		return fmt.Errorf("delete model access target %d: %w", targetID, err)
	}
	return nil
}

func listConnectionReferenceRows(ctx context.Context, exec queryExecutor, profileID int, connectionID int) ([]connectionReferenceRecord, error) {
	rows, err := exec.Query(ctx, `SELECT model_access_targets.id, model_configs.id, model_configs.model_id, model_configs.api_family, model_access_targets.position, model_access_targets.is_enabled FROM model_access_targets JOIN model_configs ON model_configs.id = model_access_targets.source_model_config_id WHERE model_access_targets.profile_id = $1 AND model_access_targets.target_connection_id = $2 ORDER BY model_configs.model_id ASC, model_configs.id ASC, model_access_targets.position ASC`, profileID, connectionID)
	if err != nil {
		return nil, fmt.Errorf("query connection %d references for profile %d: %w", connectionID, profileID, err)
	}
	defer rows.Close()
	items := make([]connectionReferenceRecord, 0)
	for rows.Next() {
		item, scanErr := scanConnectionReferenceRecord(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate connection %d references for profile %d: %w", connectionID, profileID, err)
	}
	return items, nil
}

func listConnectionReferenceModeRows(ctx context.Context, exec queryExecutor, profileID int, connectionID int) ([]connectionReferenceModeRecord, error) {
	rows, err := exec.Query(ctx, `SELECT model_configs.model_id, model_configs.openai_accepted_format FROM model_access_targets JOIN model_configs ON model_configs.id = model_access_targets.source_model_config_id WHERE model_access_targets.profile_id = $1 AND model_access_targets.target_connection_id = $2 ORDER BY model_configs.model_id ASC`, profileID, connectionID)
	if err != nil {
		return nil, fmt.Errorf("query connection %d reference modes for profile %d: %w", connectionID, profileID, err)
	}
	defer rows.Close()
	items := make([]connectionReferenceModeRecord, 0)
	for rows.Next() {
		var item connectionReferenceModeRecord
		var mode sql.NullString
		if err := rows.Scan(&item.ModelID, &mode); err != nil {
			return nil, fmt.Errorf("scan connection %d reference mode: %w", connectionID, err)
		}
		item.OpenAIAcceptedFormat = nullableStringValue(mode)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate connection %d reference modes: %w", connectionID, err)
	}
	return items, nil
}

func listPricingTemplateConnectionUsageRows(ctx context.Context, exec queryExecutor, profileID int, templateID int) ([]pricingTemplateConnectionUsageRecord, error) {
	rows, err := exec.Query(ctx, `SELECT connections.id, connections.name, model_access_targets.source_model_config_id, model_configs.model_id, connections.endpoint_id, endpoints.name FROM model_access_targets JOIN connections ON connections.id = model_access_targets.target_connection_id LEFT JOIN model_configs ON model_configs.id = model_access_targets.source_model_config_id LEFT JOIN endpoints ON endpoints.id = connections.endpoint_id WHERE model_access_targets.profile_id = $1 AND connections.pricing_template_id = $2 ORDER BY connections.id ASC`, profileID, templateID)
	if err != nil {
		return nil, fmt.Errorf("query pricing template %d connection usage for profile %d: %w", templateID, profileID, err)
	}
	defer rows.Close()
	items := make([]pricingTemplateConnectionUsageRecord, 0)
	for rows.Next() {
		item, scanErr := scanPricingTemplateConnectionUsageRecord(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pricing template %d connection usage for profile %d: %w", templateID, profileID, err)
	}
	return items, nil
}

func persistConnectionPriorities(ctx context.Context, exec queryExecutor, items []connectionResponse) error {
	for _, item := range items {
		if item.ModelConfigID == nil {
			continue
		}
		if _, err := exec.Exec(ctx, `UPDATE model_access_targets SET position = $3, updated_at = $4 WHERE source_model_config_id = $1 AND target_connection_id = $2`, *item.ModelConfigID, item.ID, item.Priority, item.UpdatedAt); err != nil {
			return fmt.Errorf("persist connection %d target priority: %w", item.ID, err)
		}
	}
	return nil
}

const connectionSelectQuery = `SELECT connections.id, connections.profile_id, model_access_targets.source_model_config_id, connections.api_family, connections.endpoint_id, endpoints.id, endpoints.profile_id, endpoints.name, endpoints.base_url, endpoints.api_key, endpoints.position, endpoints.created_at, endpoints.updated_at, connections.is_active, model_access_targets.position, connections.name, connections.auth_type, connections.custom_headers, connections.custom_request_parameters, connections.openai_text_capability, connections.pricing_template_id, connections.qps_limit, connections.max_in_flight_non_stream, connections.max_in_flight_stream, pricing_templates.id, pricing_templates.name, pricing_templates.pricing_unit, pricing_templates.pricing_currency_code, pricing_templates.version, connections.created_at, connections.updated_at FROM model_access_targets JOIN connections ON connections.id = model_access_targets.target_connection_id LEFT JOIN endpoints ON endpoints.id = connections.endpoint_id LEFT JOIN pricing_templates ON pricing_templates.id = connections.pricing_template_id`

func scanConnectionRows(rows pgx.Rows, iterateContext string) ([]connectionResponse, error) {
	items := make([]connectionResponse, 0)
	for rows.Next() {
		item, scanErr := scanConnectionResponse(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", iterateContext, err)
	}
	return items, nil
}

func scanModelRecord(scanner interface{ Scan(...any) error }) (modelRecord, error) {
	record := modelRecord{}
	var openAIAcceptedFormat sql.NullString
	if err := scanner.Scan(&record.ID, &record.ProfileID, &record.ModelID, &record.APIFamily, &record.IsEnabled, &openAIAcceptedFormat); err != nil {
		return modelRecord{}, err
	}
	record.OpenAIAcceptedFormat = nullableStringValue(openAIAcceptedFormat)
	return record, nil
}

func scanEndpointRecord(scanner interface{ Scan(...any) error }) (endpointRecord, error) {
	record := endpointRecord{}
	if err := scanner.Scan(&record.ID, &record.ProfileID, &record.Name, &record.BaseURL, &record.APIKey, &record.Position, &record.CreatedAt, &record.UpdatedAt); err != nil {
		return endpointRecord{}, err
	}
	return record, nil
}

func scanConnectionResponse(scanner interface{ Scan(...any) error }) (connectionResponse, error) {
	record, err := scanTerminalTargetRecord(scanner)
	if err != nil {
		return connectionResponse{}, err
	}
	return connectionResponseFromTerminalTargetRecord(record), nil
}

func scanTerminalTargetRecord(scanner interface{ Scan(...any) error }) (terminaltarget.Record, error) {
	var modelConfigID sql.NullInt32
	var joinedEndpointID sql.NullInt32
	var endpointProfileID sql.NullInt32
	var endpointName sql.NullString
	var endpointBaseURL sql.NullString
	var endpointAPIKey sql.NullString
	var endpointPosition sql.NullInt32
	var endpointCreatedAt sql.NullTime
	var endpointUpdatedAt sql.NullTime
	var connectionName sql.NullString
	var authType sql.NullString
	var customHeaders sql.NullString
	var customRequestParameters sql.NullString
	var openAITextCapability sql.NullString
	var pricingTemplateID sql.NullInt32
	var qpsLimit sql.NullInt32
	var maxInFlightNonStream sql.NullInt32
	var maxInFlightStream sql.NullInt32
	var templateID sql.NullInt32
	var templateName sql.NullString
	var templatePricingUnit sql.NullString
	var templatePricingCurrencyCode sql.NullString
	var templateVersion sql.NullInt32
	record := terminaltarget.Record{}
	if err := scanner.Scan(&record.ID, &record.ProfileID, &modelConfigID, &record.APIFamily, &record.EndpointID, &joinedEndpointID, &endpointProfileID, &endpointName, &endpointBaseURL, &endpointAPIKey, &endpointPosition, &endpointCreatedAt, &endpointUpdatedAt, &record.IsActive, &record.Priority, &connectionName, &authType, &customHeaders, &customRequestParameters, &openAITextCapability, &pricingTemplateID, &qpsLimit, &maxInFlightNonStream, &maxInFlightStream, &templateID, &templateName, &templatePricingUnit, &templatePricingCurrencyCode, &templateVersion, &record.CreatedAt, &record.UpdatedAt); err != nil {
		return terminaltarget.Record{}, err
	}
	record.OwnerModelConfigID = nullableInt32(modelConfigID)
	record.Name = nullableStringValue(connectionName)
	record.AuthType = nullableStringValue(authType)
	record.CustomHeaders = parseCustomHeaders(customHeaders)
	record.CustomRequestParameters = parseCustomRequestParameters(customRequestParameters)
	record.OpenAITextCapability = nullableStringValue(openAITextCapability)
	record.PricingTemplateID = nullableInt32(pricingTemplateID)
	record.QPSLimit = nullableInt32(qpsLimit)
	record.MaxInFlightNonStream = nullableInt32(maxInFlightNonStream)
	record.MaxInFlightStream = nullableInt32(maxInFlightStream)
	if joinedEndpointID.Valid {
		record.Endpoint = &terminaltarget.Endpoint{ID: int(joinedEndpointID.Int32), ProfileID: int(endpointProfileID.Int32), Name: endpointName.String, BaseURL: endpointBaseURL.String, APIKey: endpointAPIKey.String, Position: int(endpointPosition.Int32), CreatedAt: endpointCreatedAt.Time.UTC(), UpdatedAt: endpointUpdatedAt.Time.UTC()}
	}
	if templateID.Valid {
		record.PricingTemplate = &terminaltarget.PricingTemplateSummary{ID: int(templateID.Int32), Name: templateName.String, PricingUnit: templatePricingUnit.String, PricingCurrencyCode: templatePricingCurrencyCode.String, Version: int(templateVersion.Int32)}
	}
	return record, nil
}

func terminalTargetRecordFromConnectionResponse(item connectionResponse) terminaltarget.Record {
	record := terminaltarget.Record{
		ID:                       item.ID,
		ProfileID:                item.ProfileID,
		OwnerModelConfigID:       item.ModelConfigID,
		APIFamily:                item.APIFamily,
		EndpointID:               item.EndpointID,
		IsActive:                 item.IsActive,
		Priority:                 item.Priority,
		Name:                     item.Name,
		AuthType:                 item.AuthType,
		CustomHeaders:            item.CustomHeaders,
		CustomRequestParameters:  item.CustomRequestParameters,
		OpenAITextCapability:     item.OpenAITextCapability,
		PricingTemplateID:        item.PricingTemplateID,
		QPSLimit:                 item.QPSLimit,
		MaxInFlightNonStream:     item.MaxInFlightNonStream,
		MaxInFlightStream:        item.MaxInFlightStream,
		CreatedAt:                item.CreatedAt,
		UpdatedAt:                item.UpdatedAt,
	}
	if item.Endpoint != nil {
		record.Endpoint = &terminaltarget.Endpoint{ID: item.Endpoint.ID, ProfileID: item.Endpoint.ProfileID, Name: item.Endpoint.Name, BaseURL: item.Endpoint.BaseURL, Position: item.Endpoint.Position, CreatedAt: item.Endpoint.CreatedAt, UpdatedAt: item.Endpoint.UpdatedAt}
	}
	if item.PricingTemplate != nil {
		record.PricingTemplate = &terminaltarget.PricingTemplateSummary{ID: item.PricingTemplate.ID, Name: item.PricingTemplate.Name, PricingUnit: item.PricingTemplate.PricingUnit, PricingCurrencyCode: item.PricingTemplate.PricingCurrencyCode, Version: item.PricingTemplate.Version}
	}
	return record
}

func connectionResponseFromTerminalTargetRecord(record terminaltarget.Record) connectionResponse {
	item := connectionResponse{
		ID:                       record.ID,
		ProfileID:                record.ProfileID,
		ModelConfigID:            record.OwnerModelConfigID,
		APIFamily:                record.APIFamily,
		EndpointID:               record.EndpointID,
		IsActive:                 record.IsActive,
		Priority:                 record.Priority,
		Name:                     record.Name,
		AuthType:                 record.AuthType,
		CustomHeaders:            record.CustomHeaders,
		CustomRequestParameters:  record.CustomRequestParameters,
		OpenAITextCapability:     record.OpenAITextCapability,
		PricingTemplateID:        record.PricingTemplateID,
		QPSLimit:                 record.QPSLimit,
		MaxInFlightNonStream:     record.MaxInFlightNonStream,
		MaxInFlightStream:        record.MaxInFlightStream,
		CreatedAt:                record.CreatedAt,
		UpdatedAt:                record.UpdatedAt,
	}
	if record.Endpoint != nil {
		item.Endpoint = &endpointResponse{ID: record.Endpoint.ID, ProfileID: record.Endpoint.ProfileID, Name: record.Endpoint.Name, BaseURL: record.Endpoint.BaseURL, HasAPIKey: endpointdomain.HasAPIKey(record.Endpoint.APIKey), MaskedAPIKey: endpointdomain.MaskedAPIKey(record.Endpoint.APIKey), Position: record.Endpoint.Position, CreatedAt: record.Endpoint.CreatedAt, UpdatedAt: record.Endpoint.UpdatedAt}
	}
	if record.PricingTemplate != nil {
		item.PricingTemplate = &connectionPricingTemplateSummary{ID: record.PricingTemplate.ID, Name: record.PricingTemplate.Name, PricingUnit: record.PricingTemplate.PricingUnit, PricingCurrencyCode: record.PricingTemplate.PricingCurrencyCode, Version: record.PricingTemplate.Version}
	}
	return item
}

func scanConnectionReferenceRecord(scanner interface{ Scan(...any) error }) (connectionReferenceRecord, error) {
	record := connectionReferenceRecord{}
	if err := scanner.Scan(&record.TargetID, &record.ModelConfigID, &record.ModelID, &record.APIFamily, &record.Position, &record.IsEnabled); err != nil {
		return connectionReferenceRecord{}, err
	}
	return record, nil
}

func scanPricingTemplateConnectionUsageRecord(scanner interface{ Scan(...any) error }) (pricingTemplateConnectionUsageRecord, error) {
	var connectionName sql.NullString
	var modelID sql.NullString
	var endpointName sql.NullString
	item := pricingTemplateConnectionUsageRecord{}
	if err := scanner.Scan(&item.ConnectionID, &connectionName, &item.ModelConfigID, &modelID, &item.EndpointID, &endpointName); err != nil {
		return pricingTemplateConnectionUsageRecord{}, err
	}
	item.ConnectionName = nullableStringValue(connectionName)
	item.ModelID = nullableStringValue(modelID)
	item.EndpointName = nullableStringValue(endpointName)
	return item, nil
}

func scanPricingTemplateResponse(scanner interface{ Scan(...any) error }) (pricingTemplateResponse, error) {
	var description sql.NullString
	item := pricingTemplateResponse{}
	if err := scanner.Scan(&item.ID, &item.ProfileID, &item.Name, &description, &item.PricingUnit, &item.PricingCurrencyCode, &item.InputPrice, &item.OutputPrice, &item.CachedInputPrice, &item.CacheCreationPrice, &item.ReasoningPrice, &item.Version, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return pricingTemplateResponse{}, err
	}
	item.Description = nullableStringValue(description)
	item.CreatedAt = item.CreatedAt.UTC()
	item.UpdatedAt = item.UpdatedAt.UTC()
	return item, nil
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

func nullableJSONString(value map[string]string) any {
	if len(value) == 0 {
		return nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return string(raw)
}

func nullableCustomRequestParametersArg(value *terminaltarget.CustomRequestParameters) any {
	if value == nil || value.IsEmpty() {
		return nil
	}
	return string(value.RawObject())
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

func int32ArrayArg(values []int) []int32 {
	items := make([]int32, 0, len(values))
	for _, value := range values {
		items = append(items, int32(value))
	}
	return items
}
