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

	"github.com/coachpo/prism/backend/internal/endpointdomain"
)

type queryExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type modelRecord struct {
	ID        int
	ProfileID int
	ModelID   string
	ModelType string
	APIFamily string
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

type connectionOwnerRecord struct {
	ConnectionID    int
	ModelConfigID   int
	ModelID         *string
	ConnectionName  *string
	EndpointID      *int
	EndpointName    *string
	EndpointBaseURL *string
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

func loadModelRecord(ctx context.Context, exec queryExecutor, profileID int, modelConfigID int) (modelRecord, bool, error) {
	record, err := scanModelRecord(exec.QueryRow(ctx, `SELECT id, profile_id, model_id, model_type, api_family FROM model_configs WHERE profile_id = $1 AND id = $2 LIMIT 1`, profileID, modelConfigID))
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
	args := []any{profileID}
	for _, modelConfigID := range modelConfigIDs {
		args = append(args, modelConfigID)
	}
	query := fmt.Sprintf(`SELECT id FROM model_configs WHERE profile_id = $1 AND id IN (%s) ORDER BY id ASC`, placeholders(2, len(modelConfigIDs)))
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

func loadConnectionRecord(ctx context.Context, exec queryExecutor, profileID int, connectionID int, forUpdate bool) (connectionResponse, bool, error) {
	query := connectionSelectQuery + ` WHERE connections.profile_id = $1 AND connections.id = $2`
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

func listConnectionsForModel(ctx context.Context, exec queryExecutor, profileID int, modelConfigID int) ([]connectionResponse, error) {
	rows, err := exec.Query(ctx, connectionSelectQuery+` WHERE connections.profile_id = $1 AND connections.model_config_id = $2 ORDER BY connections.priority ASC, connections.id ASC`, profileID, modelConfigID)
	if err != nil {
		return nil, fmt.Errorf("query connections for model %d: %w", modelConfigID, err)
	}
	defer rows.Close()
	return scanConnectionRows(rows, fmt.Sprintf("iterate connections for model %d", modelConfigID))
}

func listConnectionsByModelIDs(ctx context.Context, exec queryExecutor, profileID int, modelConfigIDs []int) (map[int][]connectionResponse, error) {
	items := make(map[int][]connectionResponse, len(modelConfigIDs))
	if len(modelConfigIDs) == 0 {
		return items, nil
	}
	args := []any{profileID}
	for _, modelConfigID := range modelConfigIDs {
		args = append(args, modelConfigID)
	}
	query := fmt.Sprintf(`%s WHERE connections.profile_id = $1 AND connections.model_config_id IN (%s) ORDER BY connections.model_config_id ASC, connections.priority ASC, connections.id ASC`, connectionSelectQuery, placeholders(2, len(modelConfigIDs)))
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
		items[item.ModelConfigID] = append(items[item.ModelConfigID], item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate connection batch for profile %d: %w", profileID, err)
	}
	return items, nil
}

func insertConnection(ctx context.Context, exec queryExecutor, item connectionResponse) (int, error) {
	var connectionID int
	err := exec.QueryRow(ctx, `INSERT INTO connections (profile_id, model_config_id, endpoint_id, pricing_template_id, qps_limit, max_in_flight_non_stream, max_in_flight_stream, openai_probe_endpoint_variant, is_active, priority, name, auth_type, custom_headers, health_status, health_detail, last_health_check, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18) RETURNING id`, item.ProfileID, item.ModelConfigID, item.EndpointID, nullableInt(item.PricingTemplateID), nullableInt(item.QPSLimit), nullableInt(item.MaxInFlightNonStream), nullableInt(item.MaxInFlightStream), nullableString(item.OpenAIProbeEndpointVariant), item.IsActive, item.Priority, nullableString(item.Name), nullableString(item.AuthType), nullableJSONString(item.CustomHeaders), item.HealthStatus, nullableString(item.HealthDetail), nullableTimeValue(item.LastHealthCheck), item.CreatedAt, item.UpdatedAt).Scan(&connectionID)
	if err != nil {
		return 0, fmt.Errorf("insert connection for model %d: %w", item.ModelConfigID, err)
	}
	return connectionID, nil
}

func updateConnectionRow(ctx context.Context, exec queryExecutor, item connectionResponse) error {
	if _, err := exec.Exec(ctx, `UPDATE connections SET endpoint_id = $2, pricing_template_id = $3, qps_limit = $4, max_in_flight_non_stream = $5, max_in_flight_stream = $6, openai_probe_endpoint_variant = $7, is_active = $8, priority = $9, name = $10, auth_type = $11, custom_headers = $12, health_status = $13, health_detail = $14, last_health_check = $15, updated_at = $16 WHERE id = $1`, item.ID, item.EndpointID, nullableInt(item.PricingTemplateID), nullableInt(item.QPSLimit), nullableInt(item.MaxInFlightNonStream), nullableInt(item.MaxInFlightStream), nullableString(item.OpenAIProbeEndpointVariant), item.IsActive, item.Priority, nullableString(item.Name), nullableString(item.AuthType), nullableJSONString(item.CustomHeaders), item.HealthStatus, nullableString(item.HealthDetail), nullableTimeValue(item.LastHealthCheck), item.UpdatedAt); err != nil {
		return fmt.Errorf("update connection %d: %w", item.ID, err)
	}
	return nil
}

func updateConnectionHealthCheck(ctx context.Context, exec queryExecutor, connectionID int, healthStatus string, healthDetail *string, lastHealthCheck time.Time) error {
	if _, err := exec.Exec(ctx, `UPDATE connections SET health_status = $2, health_detail = $3, last_health_check = $4 WHERE id = $1`, connectionID, healthStatus, nullableString(healthDetail), lastHealthCheck); err != nil {
		return fmt.Errorf("update connection %d health check: %w", connectionID, err)
	}
	return nil
}

func deleteConnectionRow(ctx context.Context, exec queryExecutor, connectionID int) error {
	if _, err := exec.Exec(ctx, `DELETE FROM connections WHERE id = $1`, connectionID); err != nil {
		return fmt.Errorf("delete connection %d: %w", connectionID, err)
	}
	return nil
}

func clearConnectionRuntimeState(ctx context.Context, exec queryExecutor, profileID int, connectionID int) error {
	if _, err := exec.Exec(ctx, `DELETE FROM routing_connection_runtime_state WHERE profile_id = $1 AND connection_id = $2`, profileID, connectionID); err != nil {
		return fmt.Errorf("clear runtime state for connection %d: %w", connectionID, err)
	}
	return nil
}

func clearRoundRobinStateForModel(ctx context.Context, exec queryExecutor, profileID int, modelConfigID int) error {
	if _, err := exec.Exec(ctx, `DELETE FROM loadbalance_round_robin_state WHERE profile_id = $1 AND model_config_id = $2`, profileID, modelConfigID); err != nil {
		return fmt.Errorf("clear round robin state for model %d: %w", modelConfigID, err)
	}
	return nil
}

func loadConnectionOwner(ctx context.Context, exec queryExecutor, profileID int, connectionID int) (connectionOwnerRecord, bool, error) {
	record, err := scanConnectionOwnerRecord(exec.QueryRow(ctx, `SELECT connections.id, connections.model_config_id, model_configs.model_id, connections.name, endpoints.id, endpoints.name, endpoints.base_url FROM connections LEFT JOIN model_configs ON model_configs.id = connections.model_config_id LEFT JOIN endpoints ON endpoints.id = connections.endpoint_id WHERE connections.profile_id = $1 AND connections.id = $2 LIMIT 1`, profileID, connectionID))
	if err == pgx.ErrNoRows {
		return connectionOwnerRecord{}, false, nil
	}
	if err != nil {
		return connectionOwnerRecord{}, false, fmt.Errorf("load connection owner for %d in profile %d: %w", connectionID, profileID, err)
	}
	return record, true, nil
}

func listPricingTemplateConnectionUsageRows(ctx context.Context, exec queryExecutor, profileID int, templateID int) ([]pricingTemplateConnectionUsageRecord, error) {
	rows, err := exec.Query(ctx, `SELECT connections.id, connections.name, connections.model_config_id, model_configs.model_id, connections.endpoint_id, endpoints.name FROM connections LEFT JOIN model_configs ON model_configs.id = connections.model_config_id LEFT JOIN endpoints ON endpoints.id = connections.endpoint_id WHERE connections.profile_id = $1 AND connections.pricing_template_id = $2 ORDER BY connections.id ASC`, profileID, templateID)
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
		if _, err := exec.Exec(ctx, `UPDATE connections SET priority = $2, updated_at = $3 WHERE id = $1`, item.ID, item.Priority, item.UpdatedAt); err != nil {
			return fmt.Errorf("persist connection %d priority: %w", item.ID, err)
		}
	}
	return nil
}

const connectionSelectQuery = `SELECT connections.id, connections.profile_id, connections.model_config_id, connections.endpoint_id, endpoints.id, endpoints.profile_id, endpoints.name, endpoints.base_url, endpoints.api_key, endpoints.position, endpoints.created_at, endpoints.updated_at, connections.is_active, connections.priority, connections.name, connections.auth_type, connections.custom_headers, connections.openai_probe_endpoint_variant, connections.pricing_template_id, connections.qps_limit, connections.max_in_flight_non_stream, connections.max_in_flight_stream, pricing_templates.id, pricing_templates.name, pricing_templates.pricing_unit, pricing_templates.pricing_currency_code, pricing_templates.version, connections.health_status, connections.health_detail, connections.last_health_check, connections.created_at, connections.updated_at FROM connections LEFT JOIN endpoints ON endpoints.id = connections.endpoint_id LEFT JOIN pricing_templates ON pricing_templates.id = connections.pricing_template_id`

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
	if err := scanner.Scan(&record.ID, &record.ProfileID, &record.ModelID, &record.ModelType, &record.APIFamily); err != nil {
		return modelRecord{}, err
	}
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
	if err := scanner.Scan(&item.ID, &item.ProfileID, &item.ModelConfigID, &item.EndpointID, &joinedEndpointID, &endpointProfileID, &endpointName, &endpointBaseURL, &endpointAPIKey, &endpointPosition, &endpointCreatedAt, &endpointUpdatedAt, &item.IsActive, &item.Priority, &connectionName, &authType, &customHeaders, &openAIProbeEndpointVariant, &pricingTemplateID, &qpsLimit, &maxInFlightNonStream, &maxInFlightStream, &templateID, &templateName, &templatePricingUnit, &templatePricingCurrencyCode, &templateVersion, &item.HealthStatus, &healthDetail, &lastHealthCheck, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return connectionResponse{}, err
	}
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
	if joinedEndpointID.Valid {
		item.Endpoint = &endpointResponse{ID: int(joinedEndpointID.Int32), ProfileID: int(endpointProfileID.Int32), Name: endpointName.String, BaseURL: endpointBaseURL.String, HasAPIKey: endpointdomain.HasAPIKey(endpointAPIKey.String), MaskedAPIKey: endpointdomain.MaskedAPIKey(endpointAPIKey.String), Position: int(endpointPosition.Int32), CreatedAt: endpointCreatedAt.Time.UTC(), UpdatedAt: endpointUpdatedAt.Time.UTC()}
	}
	if templateID.Valid {
		item.PricingTemplate = &connectionPricingTemplateSummary{ID: int(templateID.Int32), Name: templateName.String, PricingUnit: templatePricingUnit.String, PricingCurrencyCode: templatePricingCurrencyCode.String, Version: int(templateVersion.Int32)}
	}
	return item, nil
}

func scanConnectionOwnerRecord(scanner interface{ Scan(...any) error }) (connectionOwnerRecord, error) {
	var modelID sql.NullString
	var connectionName sql.NullString
	var endpointID sql.NullInt32
	var endpointName sql.NullString
	var endpointBaseURL sql.NullString
	record := connectionOwnerRecord{}
	if err := scanner.Scan(&record.ConnectionID, &record.ModelConfigID, &modelID, &connectionName, &endpointID, &endpointName, &endpointBaseURL); err != nil {
		return connectionOwnerRecord{}, err
	}
	record.ModelID = nullableStringValue(modelID)
	record.ConnectionName = nullableStringValue(connectionName)
	record.EndpointID = nullableInt32(endpointID)
	record.EndpointName = nullableStringValue(endpointName)
	record.EndpointBaseURL = nullableStringValue(endpointBaseURL)
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

func nullableTimeValue(value *time.Time) any {
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

func placeholders(start int, count int) string {
	parts := make([]string, count)
	for index := 0; index < count; index++ {
		parts[index] = fmt.Sprintf("$%d", start+index)
	}
	return strings.Join(parts, ", ")
}
