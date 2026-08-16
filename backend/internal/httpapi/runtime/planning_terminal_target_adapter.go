package runtime

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/domain/terminaltarget"
)

func listActiveConnectionsForProfile(ctx context.Context, tx pgx.Tx, profileID int) (map[int]runtimeConnection, error) {
	rows, err := tx.Query(
		ctx,
		`SELECT connections.id, connections.profile_id, connections.api_family, connections.endpoint_id,
			connections.priority, connections.qps_limit, connections.max_in_flight_non_stream, connections.max_in_flight_stream,
			connections.name, connections.auth_type, connections.custom_headers, connections.custom_request_parameters, connections.pricing_template_id,
			connections.openai_text_capability, connections.openai_image_capability,
			pricing_templates.id, pricing_templates.name, pricing_templates.current_revision_id,
			revisions.id, revisions.version, revisions.pricing_unit, revisions.currency_code,
			revisions.reporting_currency_epoch, revisions.input_price, revisions.output_price,
			revisions.cached_input_price, revisions.cache_creation_price, revisions.reasoning_price,
			revisions.effective_at,
			endpoints.id, endpoints.name, endpoints.base_url, endpoints.api_key
		FROM connections
		JOIN endpoints ON endpoints.id = connections.endpoint_id
		LEFT JOIN pricing_templates ON pricing_templates.id = connections.pricing_template_id
		LEFT JOIN pricing_template_revisions AS revisions ON revisions.id = pricing_templates.current_revision_id
		WHERE connections.profile_id = $1 AND connections.is_active = TRUE
		ORDER BY connections.id ASC`,
		profileID,
	)
	if err != nil {
		return nil, fmt.Errorf("query active connections for profile %d: %w", profileID, err)
	}
	defer rows.Close()

	items := make(map[int]runtimeConnection)
	for rows.Next() {
		record, scanErr := scanRuntimeTerminalTargetRecord(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan runtime connection for profile %d: %w", profileID, scanErr)
		}
		item := runtimeConnectionFromTerminalTargetRecord(record)
		items[item.ID] = item
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active connections for profile %d: %w", profileID, err)
	}
	return items, nil
}

func scanRuntimeTerminalTargetRecord(scanner interface{ Scan(...any) error }) (terminaltarget.RuntimeRecord, error) {
	var qpsLimit sql.NullInt32
	var maxInFlightNonStream sql.NullInt32
	var maxInFlightStream sql.NullInt32
	var name sql.NullString
	var authType sql.NullString
	var customHeaders sql.NullString
	var customRequestParameters sql.NullString
	var pricingTemplateID sql.NullInt32
	var openAITextCapability sql.NullString
	var openAIImageCapability sql.NullString
	var templateID sql.NullInt32
	var templateName sql.NullString
	var templateRevisionID sql.NullInt64
	var revisionID sql.NullInt64
	var templatePricingUnit sql.NullString
	var templatePricingCurrencyCode sql.NullString
	var templateInputPrice sql.NullString
	var templateOutputPrice sql.NullString
	var templateCachedInputPrice sql.NullString
	var templateCacheCreationPrice sql.NullString
	var templateReasoningPrice sql.NullString
	var templateVersion sql.NullInt32
	var templateEpoch sql.NullInt32
	var templateEffectiveAt sql.NullTime
	var endpointName sql.NullString
	record := terminaltarget.RuntimeRecord{}
	if err := scanner.Scan(
		&record.ID,
		&record.ProfileID,
		&record.APIFamily,
		&record.EndpointID,
		&record.Priority,
		&qpsLimit,
		&maxInFlightNonStream,
		&maxInFlightStream,
		&name,
		&authType,
		&customHeaders,
		&customRequestParameters,
		&pricingTemplateID,
		&openAITextCapability,
		&openAIImageCapability,
		&templateID,
		&templateName,
		&templateRevisionID,
		&revisionID,
		&templateVersion,
		&templatePricingUnit,
		&templatePricingCurrencyCode,
		&templateEpoch,
		&templateInputPrice,
		&templateOutputPrice,
		&templateCachedInputPrice,
		&templateCacheCreationPrice,
		&templateReasoningPrice,
		&templateEffectiveAt,
		&record.Endpoint.ID,
		&endpointName,
		&record.Endpoint.BaseURL,
		&record.Endpoint.EncryptedAPIKey,
	); err != nil {
		return terminaltarget.RuntimeRecord{}, err
	}
	record.QPSLimit = nullableInt32(qpsLimit)
	record.MaxInFlightNonStream = nullableInt32(maxInFlightNonStream)
	record.MaxInFlightStream = nullableInt32(maxInFlightStream)
	record.Name = nullableString(name)
	record.AuthType = nullableString(authType)
	record.CustomHeaders = parseCustomHeaders(customHeaders)
	customRequestParametersValue, parseErr := parseRuntimeCustomRequestParameters(customRequestParameters)
	if parseErr != nil {
		return terminaltarget.RuntimeRecord{}, fmt.Errorf("invalid custom request parameters for connection %d: %w", record.ID, parseErr)
	}
	record.CustomRequestParameters = customRequestParametersValue
	record.PricingTemplateID = nullableInt32(pricingTemplateID)
	record.OpenAITextCapability = nullableString(openAITextCapability)
	record.OpenAIImageCapability = nullableString(openAIImageCapability)
	record.Endpoint.Name = nullableString(endpointName)
	if templateID.Valid {
		record.PricingTemplate = &terminaltarget.RuntimePricingTemplateSnapshot{
			ID:                  int(templateID.Int32),
			Name:                strings.TrimSpace(templateName.String),
			RevisionID:          revisionID.Int64,
			PricingUnit:         strings.TrimSpace(templatePricingUnit.String),
			PricingCurrencyCode: strings.TrimSpace(templatePricingCurrencyCode.String),
			InputPrice:          strings.TrimSpace(templateInputPrice.String),
			OutputPrice:         strings.TrimSpace(templateOutputPrice.String),
			CachedInputPrice:    strings.TrimSpace(templateCachedInputPrice.String),
			CacheCreationPrice:  strings.TrimSpace(templateCacheCreationPrice.String),
			ReasoningPrice:      strings.TrimSpace(templateReasoningPrice.String),
			Version:             int(templateVersion.Int32),
		}
		if templateEpoch.Valid {
			epoch := int(templateEpoch.Int32)
			record.PricingTemplate.ReportingCurrencyEpoch = &epoch
		}
		if templateEffectiveAt.Valid {
			effectiveAt := templateEffectiveAt.Time.UTC()
			record.PricingTemplate.VersionEffectiveAt = &effectiveAt
		}
	}
	return record, nil
}

// parseRuntimeCustomRequestParameters parses the JSONB column text with the
// shared validator and fails closed: invalid persisted configuration rejects
// the whole snapshot generation (cold start fails, hot refresh keeps the
// last-good snapshot) instead of silently forwarding requests without the
// configured overlay. The error carries only the connection ID and the
// validation reason/path, never the configuration value.
func parseRuntimeCustomRequestParameters(value sql.NullString) (*terminaltarget.CustomRequestParameters, error) {
	if !value.Valid || strings.TrimSpace(value.String) == "" {
		return nil, nil
	}
	parsed, validationErr := terminaltarget.ParseCustomRequestParametersJSON([]byte(value.String))
	if validationErr != nil {
		return nil, validationErr
	}
	if parsed.IsEmpty() {
		return nil, nil
	}
	return parsed, nil
}

func runtimeConnectionFromTerminalTargetRecord(record terminaltarget.RuntimeRecord) runtimeConnection {
	item := runtimeConnection{
		ID:                      record.ID,
		ProfileID:               record.ProfileID,
		APIFamily:               record.APIFamily,
		EndpointID:              record.EndpointID,
		Priority:                record.Priority,
		QPSLimit:                record.QPSLimit,
		MaxInFlightNonStream:    record.MaxInFlightNonStream,
		MaxInFlightStream:       record.MaxInFlightStream,
		Name:                    record.Name,
		AuthType:                record.AuthType,
		EncryptedEndpointAPIKey: record.Endpoint.EncryptedAPIKey,
		CustomHeaders:           record.CustomHeaders,
		CustomRequestParameters: record.CustomRequestParameters,
		PricingTemplateID:       record.PricingTemplateID,
		OpenAITextCapability:    record.OpenAITextCapability,
		OpenAIImageCapability:   record.OpenAIImageCapability,
		Endpoint: runtimeEndpoint{
			ID:      record.Endpoint.ID,
			Name:    record.Endpoint.Name,
			BaseURL: record.Endpoint.BaseURL,
		},
	}
	if record.PricingTemplate != nil {
		item.PricingTemplateSnapshot = &runtimePricingTemplateSnapshot{
			ID:                  record.PricingTemplate.ID,
			Name:                record.PricingTemplate.Name,
			RevisionID:          record.PricingTemplate.RevisionID,
			PricingUnit:         record.PricingTemplate.PricingUnit,
			PricingCurrencyCode: record.PricingTemplate.PricingCurrencyCode,
			InputPrice:          record.PricingTemplate.InputPrice,
			OutputPrice:         record.PricingTemplate.OutputPrice,
			CachedInputPrice:    record.PricingTemplate.CachedInputPrice,
			CacheCreationPrice:  record.PricingTemplate.CacheCreationPrice,
			ReasoningPrice:      record.PricingTemplate.ReasoningPrice,
			Version:             record.PricingTemplate.Version,
		}
		if record.PricingTemplate.ReportingCurrencyEpoch != nil {
			epoch := *record.PricingTemplate.ReportingCurrencyEpoch
			item.PricingTemplateSnapshot.ReportingCurrencyEpoch = &epoch
		}
		if record.PricingTemplate.VersionEffectiveAt != nil {
			effectiveAt := record.PricingTemplate.VersionEffectiveAt.UTC()
			item.PricingTemplateSnapshot.VersionEffectiveAt = &effectiveAt
		}
	}
	return item
}
