package connections

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/domain/terminaltarget"
	"github.com/coachpo/prism/backend/internal/endpointdomain"
)

const connectionSelectQuery = `SELECT connections.id, connections.profile_id, model_access_targets.source_model_config_id, connections.api_family, connections.endpoint_id, endpoints.id, endpoints.profile_id, endpoints.name, endpoints.base_url, endpoints.api_key, endpoints.api_key_fingerprint, endpoints.api_key_updated_at, endpoints.config_revision, endpoints.created_at, endpoints.updated_at, connections.is_active, model_access_targets.position, connections.name, connections.auth_type, connections.custom_headers, connections.custom_request_parameters, connections.openai_text_capability, connections.openai_image_capability, connections.routing_schedule_timezone, connections.pricing_template_id, connections.qps_limit, connections.max_in_flight_non_stream, connections.max_in_flight_stream, pricing_templates.id, pricing_templates.name, revisions.version, revisions.currency_code, revisions.template_kind, connections.created_at, connections.updated_at FROM model_access_targets JOIN connections ON connections.id = model_access_targets.target_connection_id LEFT JOIN endpoints ON endpoints.id = connections.endpoint_id LEFT JOIN pricing_templates ON pricing_templates.id = connections.pricing_template_id LEFT JOIN pricing_template_revisions AS revisions ON revisions.id = pricing_templates.current_revision_id`

const pricingUnitPerMillion = "PER_1M"

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
	var endpointFingerprint sql.NullString
	var endpointKeyUpdatedAt sql.NullTime
	var endpointConfigRevision sql.NullInt64
	var endpointCreatedAt sql.NullTime
	var endpointUpdatedAt sql.NullTime
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
	var templateKind sql.NullString
	var templateVersion sql.NullInt32
	record := terminaltarget.Record{}
	if err := scanner.Scan(&record.ID, &record.ProfileID, &modelConfigID, &record.APIFamily, &record.EndpointID, &joinedEndpointID, &endpointProfileID, &endpointName, &endpointBaseURL, &endpointAPIKey, &endpointFingerprint, &endpointKeyUpdatedAt, &endpointConfigRevision, &endpointCreatedAt, &endpointUpdatedAt, &record.IsActive, &record.Priority, &connectionName, &authType, &customHeaders, &customRequestParameters, &openAITextCapability, &openAIImageCapability, &routingScheduleTimezone, &pricingTemplateID, &qpsLimit, &maxInFlightNonStream, &maxInFlightStream, &templateID, &templateName, &templateVersion, &templateCurrencyCode, &templateKind, &record.CreatedAt, &record.UpdatedAt); err != nil {
		return terminaltarget.Record{}, err
	}
	record.RoutingScheduleTimezone = nullableStringValue(routingScheduleTimezone)
	record.OwnerModelConfigID = nullableInt32(modelConfigID)
	record.Name = nullableStringValue(connectionName)
	record.AuthType = nullableStringValue(authType)
	record.CustomHeaders = parseCustomHeaders(customHeaders)
	record.CustomRequestParameters = parseCustomRequestParameters(customRequestParameters)
	record.OpenAITextCapability = nullableStringValue(openAITextCapability)
	record.OpenAIImageCapability = nullableStringValue(openAIImageCapability)
	record.PricingTemplateID = nullableInt32(pricingTemplateID)
	record.QPSLimit = nullableInt32(qpsLimit)
	record.MaxInFlightNonStream = nullableInt32(maxInFlightNonStream)
	record.MaxInFlightStream = nullableInt32(maxInFlightStream)
	if joinedEndpointID.Valid {
		record.Endpoint = &terminaltarget.Endpoint{
			ID:                int(joinedEndpointID.Int32),
			ProfileID:         int(endpointProfileID.Int32),
			Name:              endpointName.String,
			BaseURL:           endpointBaseURL.String,
			APIKey:            endpointAPIKey.String,
			APIKeyFingerprint: nullableStringValue(endpointFingerprint),
			APIKeyUpdatedAt:   nullableTimeValue(endpointKeyUpdatedAt),
			ConfigRevision:    nullableInt64Value(endpointConfigRevision),
			CreatedAt:         endpointCreatedAt.Time.UTC(),
			UpdatedAt:         endpointUpdatedAt.Time.UTC(),
		}
	}
	if templateID.Valid {
		summary := &terminaltarget.PricingTemplateSummary{ID: int(templateID.Int32), Name: templateName.String, PricingUnit: pricingUnitPerMillion, PricingCurrencyCode: templateCurrencyCode.String, Version: int(templateVersion.Int32)}
		summary.SetTemplateKind(templateKind.String)
		record.PricingTemplate = summary
	}
	return record, nil
}

func terminalTargetRecordFromConnectionResponse(item connectionResponse) terminaltarget.Record {
	record := terminaltarget.Record{
		ID:                      item.ID,
		ProfileID:               item.ProfileID,
		OwnerModelConfigID:      item.ModelConfigID,
		APIFamily:               item.APIFamily,
		EndpointID:              item.EndpointID,
		IsActive:                item.IsActive,
		Priority:                item.Priority,
		Name:                    item.Name,
		AuthType:                item.AuthType,
		CustomHeaders:           item.CustomHeaders,
		CustomRequestParameters: item.CustomRequestParameters,
		OpenAITextCapability:    item.OpenAITextCapability,
		OpenAIImageCapability:   item.OpenAIImageCapability,
		PricingTemplateID:       item.PricingTemplateID,
		QPSLimit:                item.QPSLimit,
		MaxInFlightNonStream:    item.MaxInFlightNonStream,
		MaxInFlightStream:       item.MaxInFlightStream,
		CreatedAt:               item.CreatedAt,
		UpdatedAt:               item.UpdatedAt,
	}
	if item.Endpoint != nil {
		record.Endpoint = &terminaltarget.Endpoint{
			ID:                item.Endpoint.ID,
			ProfileID:         item.Endpoint.ProfileID,
			Name:              item.Endpoint.Name,
			BaseURL:           item.Endpoint.BaseURL,
			APIKeyFingerprint: item.Endpoint.APIKeyFingerprint,
			APIKeyUpdatedAt:   item.Endpoint.APIKeyUpdatedAt,
			ConfigRevision:    item.Endpoint.ConfigRevision,
			CreatedAt:         item.Endpoint.CreatedAt,
			UpdatedAt:         item.Endpoint.UpdatedAt,
		}
	}
	if item.PricingTemplate != nil {
		record.PricingTemplate = &terminaltarget.PricingTemplateSummary{ID: item.PricingTemplate.ID, Name: item.PricingTemplate.Name, PricingUnit: item.PricingTemplate.PricingUnit, PricingCurrencyCode: item.PricingTemplate.PricingCurrencyCode, TemplateKind: item.PricingTemplate.TemplateKind, Version: item.PricingTemplate.Version}
	}
	// RoutingScheduleState is deliberately not carried across: it is a clock
	// derived projection computed at response assembly, not stored state.
	if item.RoutingSchedule != nil {
		timezone := item.RoutingSchedule.Timezone
		record.RoutingScheduleTimezone = &timezone
		record.RoutingWindows = routingWindowsFromPayload(item.RoutingSchedule)
	}
	return record
}

func connectionResponseFromTerminalTargetRecord(record terminaltarget.Record) connectionResponse {
	item := connectionResponse{
		ID:                      record.ID,
		ProfileID:               record.ProfileID,
		ModelConfigID:           record.OwnerModelConfigID,
		APIFamily:               record.APIFamily,
		EndpointID:              record.EndpointID,
		IsActive:                record.IsActive,
		Priority:                record.Priority,
		Name:                    record.Name,
		AuthType:                record.AuthType,
		CustomHeaders:           record.CustomHeaders,
		CustomRequestParameters: record.CustomRequestParameters,
		OpenAITextCapability:    record.OpenAITextCapability,
		OpenAIImageCapability:   record.OpenAIImageCapability,
		PricingTemplateID:       record.PricingTemplateID,
		QPSLimit:                record.QPSLimit,
		MaxInFlightNonStream:    record.MaxInFlightNonStream,
		MaxInFlightStream:       record.MaxInFlightStream,
		CreatedAt:               record.CreatedAt,
		UpdatedAt:               record.UpdatedAt,
	}
	if record.Endpoint != nil {
		item.Endpoint = &endpointResponse{ID: record.Endpoint.ID, ProfileID: record.Endpoint.ProfileID, Name: record.Endpoint.Name, BaseURL: record.Endpoint.BaseURL, HasAPIKey: endpointdomain.HasAPIKey(record.Endpoint.APIKey), APIKeyFingerprint: record.Endpoint.APIKeyFingerprint, APIKeyUpdatedAt: record.Endpoint.APIKeyUpdatedAt, ConfigRevision: record.Endpoint.ConfigRevision, CreatedAt: record.Endpoint.CreatedAt, UpdatedAt: record.Endpoint.UpdatedAt}
	}
	if record.PricingTemplate != nil {
		item.PricingTemplate = &connectionPricingTemplateSummary{ID: record.PricingTemplate.ID, Name: record.PricingTemplate.Name, PricingUnit: record.PricingTemplate.PricingUnit, PricingCurrencyCode: record.PricingTemplate.PricingCurrencyCode, TemplateKind: record.PricingTemplate.TemplateKind, Version: record.PricingTemplate.Version}
	}
	item.RoutingSchedule = routingSchedulePayloadFromRecord(record.RoutingScheduleTimezone, record.RoutingWindows)
	return item
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
