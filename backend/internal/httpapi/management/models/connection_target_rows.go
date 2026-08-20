package models

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/coachpo/prism/backend/internal/domain/safediag"
	"github.com/coachpo/prism/backend/internal/domain/terminaltarget"
	"github.com/coachpo/prism/backend/internal/httpapi/management/connections"
)

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
	var templateKind sql.NullString
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
		&templateKind,
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
		item.PricingTemplate = &connectionPricingTemplateSummary{ID: int(templateID.Int32), Name: templateName.String, PricingUnit: "PER_1M", PricingCurrencyCode: templateCurrencyCode.String, TemplateKind: templateKind.String, Version: int(templateVersion.Int32)}
	}
	return item, nil
}
