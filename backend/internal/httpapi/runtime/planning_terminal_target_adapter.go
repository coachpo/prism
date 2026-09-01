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
	records, err := listActiveTerminalTargetRecordsForProfile(ctx, tx, profileID)
	if err != nil {
		return nil, err
	}
	connectionIDs := make([]int, 0, len(records))
	for _, record := range records {
		connectionIDs = append(connectionIDs, record.ID)
	}
	windowsByConnectionID, err := listRoutingWindowsForConnections(ctx, tx, profileID, connectionIDs)
	if err != nil {
		return nil, err
	}
	revisionIDs := make([]int64, 0, len(records))
	for _, record := range records {
		if record.PricingTemplate != nil && record.PricingTemplate.RevisionID > 0 {
			revisionIDs = append(revisionIDs, record.PricingTemplate.RevisionID)
		}
	}
	cardsByRevision, err := listPricingTemplateCardsForRevisions(ctx, tx, revisionIDs)
	if err != nil {
		return nil, err
	}
	pricingWindowsByRevision, err := listPricingTemplateWindowsForRevisions(ctx, tx, revisionIDs)
	if err != nil {
		return nil, err
	}
	items := make(map[int]runtimeConnection, len(records))
	for _, record := range records {
		base := record
		base.RoutingWindows = windowsByConnectionID[base.ID]
		if base.PricingTemplate != nil {
			base.PricingTemplate.Cards = cardsByRevision[base.PricingTemplate.RevisionID]
			base.PricingTemplate.PricingWindows = append([]terminaltarget.Window(nil), pricingWindowsByRevision[base.PricingTemplate.RevisionID]...)
		}
		item := runtimeConnectionFromTerminalTargetRecord(base)
		items[item.ID] = item
	}
	return items, nil
}

// listActiveTerminalTargetRecordsForProfile reads owner-backed active
// connection rows only. Orphan compatibility rows stay out of runtime. It is
// split from listActiveConnectionsForProfile so the routing-window
// batch read runs after rows.Close() has returned: pgx single-connection
// transactions report conn-busy when a second query starts while the first
// result rows are still open, and correctness must not depend on the
// rows-exhaustion auto-close implementation detail.
func listActiveTerminalTargetRecordsForProfile(ctx context.Context, tx pgx.Tx, profileID int) ([]terminaltarget.RuntimeRecord, error) {
	rows, err := tx.Query(
		ctx,
		`SELECT connections.id, connections.profile_id, connections.api_family, connections.endpoint_id,
			connections.priority, connections.qps_limit, connections.max_in_flight_non_stream, connections.max_in_flight_stream,
			connections.name, connections.auth_type, connections.upstream_model_id, connections.custom_headers, connections.custom_request_parameters, connections.pricing_template_id,
			connections.openai_text_capability, connections.openai_image_capability, connections.routing_schedule_timezone,
			pricing_templates.id, pricing_templates.name, pricing_templates.current_revision_id,
			revisions.id, revisions.version, revisions.pricing_unit, revisions.currency_code,
			revisions.reporting_currency_epoch, revisions.template_kind, revisions.tier_input_tokens_above,
			revisions.pricing_schedule_timezone, revisions.pricing_schedule_digest, revisions.effective_at,
			endpoints.id, endpoints.name, endpoints.base_url, endpoints.api_key
		FROM connections
		JOIN model_access_targets AS owner_targets
			ON owner_targets.profile_id = connections.profile_id
			AND owner_targets.target_connection_id = connections.id
		JOIN endpoints ON endpoints.id = connections.endpoint_id
		LEFT JOIN pricing_templates ON pricing_templates.id = connections.pricing_template_id AND pricing_templates.deleted_at IS NULL
		LEFT JOIN pricing_template_revisions AS revisions ON revisions.id = pricing_templates.current_revision_id
		WHERE connections.profile_id = $1 AND connections.is_active = TRUE
		ORDER BY connections.id ASC`,
		profileID,
	)
	if err != nil {
		return nil, fmt.Errorf("query active connections for profile %d: %w", profileID, err)
	}
	defer rows.Close()

	records := make([]terminaltarget.RuntimeRecord, 0)
	for rows.Next() {
		record, scanErr := scanRuntimeTerminalTargetRecord(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan runtime connection for profile %d: %w", profileID, scanErr)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active connections for profile %d: %w", profileID, err)
	}
	return records, nil
}

// listRoutingWindowsForConnections reads every routing window row of the
// given connections in one query, keyed by connection ID. The empty set
// short-circuits so no SQL is sent for profiles without connections (the
// planning cache default branch rejects unknown queries). The three smallint
// columns scan into Go int: the contract-test fake transaction only supports
// *int for smallint-shaped values.
func listRoutingWindowsForConnections(ctx context.Context, tx pgx.Tx, profileID int, connectionIDs []int) (map[int][]terminaltarget.Window, error) {
	if len(connectionIDs) == 0 {
		return map[int][]terminaltarget.Window{}, nil
	}
	rows, err := tx.Query(ctx,
		`SELECT connection_routing_windows.connection_id,
			connection_routing_windows.weekday_mask,
			connection_routing_windows.start_minute,
			connection_routing_windows.end_minute
		FROM connection_routing_windows
		WHERE connection_routing_windows.profile_id = $1
		  AND connection_routing_windows.connection_id = ANY($2)
		ORDER BY connection_routing_windows.connection_id ASC,
			connection_routing_windows.weekday_mask ASC,
			connection_routing_windows.start_minute ASC,
			connection_routing_windows.end_minute ASC`,
		profileID, int32ArrayArg(connectionIDs))
	if err != nil {
		return nil, fmt.Errorf("query routing windows for profile %d: %w", profileID, err)
	}
	defer rows.Close()

	windowsByConnectionID := map[int][]terminaltarget.Window{}
	for rows.Next() {
		var connectionID, weekdayMask, startMinute, endMinute int
		if err := rows.Scan(&connectionID, &weekdayMask, &startMinute, &endMinute); err != nil {
			return nil, fmt.Errorf("scan routing window for profile %d: %w", profileID, err)
		}
		windowsByConnectionID[connectionID] = append(windowsByConnectionID[connectionID],
			terminaltarget.Window{WeekdayMask: weekdayMask, StartMinute: startMinute, EndMinute: endMinute})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate routing windows for profile %d: %w", profileID, err)
	}
	return windowsByConnectionID, nil
}

// int32ArrayArg converts a Go int slice into the []int32 form expected by
// pgx for a smallint[] ANY parameter. This package keeps its own copy rather
// than sharing the four identical helpers in the management packages: a
// cross-package extraction would touch all four callers and is out of scope
// for the routing-schedule change.
func int32ArrayArg(values []int) []int32 {
	items := make([]int32, 0, len(values))
	for _, value := range values {
		items = append(items, int32(value))
	}
	return items
}

func scanRuntimeTerminalTargetRecord(scanner interface{ Scan(...any) error }) (terminaltarget.RuntimeRecord, error) {
	var qpsLimit sql.NullInt32
	var maxInFlightNonStream sql.NullInt32
	var maxInFlightStream sql.NullInt32
	var name sql.NullString
	var authType sql.NullString
	var upstreamModelID sql.NullString
	var customHeaders sql.NullString
	var customRequestParameters sql.NullString
	var pricingTemplateID sql.NullInt32
	var openAITextCapability sql.NullString
	var openAIImageCapability sql.NullString
	var routingScheduleTimezone sql.NullString
	var templateID sql.NullInt32
	var templateName sql.NullString
	var templateRevisionID sql.NullInt64
	var revisionID sql.NullInt64
	var templatePricingUnit sql.NullString
	var templatePricingCurrencyCode sql.NullString
	var templateKind sql.NullString
	var templateTierInputTokensAbove sql.NullInt32
	var templateScheduleTimezone sql.NullString
	var templateScheduleDigest sql.NullString
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
		&upstreamModelID,
		&customHeaders,
		&customRequestParameters,
		&pricingTemplateID,
		&openAITextCapability,
		&openAIImageCapability,
		&routingScheduleTimezone,
		&templateID,
		&templateName,
		&templateRevisionID,
		&revisionID,
		&templateVersion,
		&templatePricingUnit,
		&templatePricingCurrencyCode,
		&templateEpoch,
		&templateKind,
		&templateTierInputTokensAbove,
		&templateScheduleTimezone,
		&templateScheduleDigest,
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
	if !upstreamModelID.Valid || strings.TrimSpace(upstreamModelID.String) == "" {
		return terminaltarget.RuntimeRecord{}, fmt.Errorf("active owned connection %d is missing upstream_model_id", record.ID)
	}
	normalizedUpstreamModelID := strings.TrimSpace(upstreamModelID.String)
	record.UpstreamModelID = &normalizedUpstreamModelID
	record.CustomHeaders = parseCustomHeaders(customHeaders)
	customRequestParametersValue, parseErr := parseRuntimeCustomRequestParameters(customRequestParameters)
	if parseErr != nil {
		return terminaltarget.RuntimeRecord{}, fmt.Errorf("invalid custom request parameters for connection %d: %w", record.ID, parseErr)
	}
	record.CustomRequestParameters = customRequestParametersValue
	record.PricingTemplateID = nullableInt32(pricingTemplateID)
	record.OpenAITextCapability = nullableString(openAITextCapability)
	record.OpenAIImageCapability = nullableString(openAIImageCapability)
	record.RoutingScheduleTimezone = nullableString(routingScheduleTimezone)
	record.Endpoint.Name = nullableString(endpointName)
	if templateID.Valid {
		record.PricingTemplate = &terminaltarget.RuntimePricingTemplateSnapshot{
			ID:                      int(templateID.Int32),
			Name:                    strings.TrimSpace(templateName.String),
			RevisionID:              revisionID.Int64,
			PricingUnit:             strings.TrimSpace(templatePricingUnit.String),
			PricingCurrencyCode:     strings.TrimSpace(templatePricingCurrencyCode.String),
			TemplateKind:            strings.TrimSpace(templateKind.String),
			TierInputTokensAbove:    nullableInt32(templateTierInputTokensAbove),
			PricingScheduleTimezone: nullableString(templateScheduleTimezone),
			PricingScheduleDigest:   strings.TrimSpace(templateScheduleDigest.String),
			Version:                 int(templateVersion.Int32),
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
	// The routing schedule compiles here and nowhere else. compileRuntimeConnection
	// is wrong for two reasons: it returns early when the deployment has no
	// secret encryption key configured, which would silently skip the whole
	// schedule; and it can return errors, which buildPlanningSnapshot turns
	// into a whole-snapshot failure. This function has no error return, so an
	// unparseable timezone can only degrade to Unresolved on this single
	// connection (fail-closed to the connection, never to the profile).
	routingScheduleTimezone := ""
	if record.RoutingScheduleTimezone != nil {
		routingScheduleTimezone = strings.TrimSpace(*record.RoutingScheduleTimezone)
	}
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
		UpstreamModelID:         cloneRuntimeStringPointer(record.UpstreamModelID),
		EncryptedEndpointAPIKey: record.Endpoint.EncryptedAPIKey,
		CustomHeaders:           record.CustomHeaders,
		CustomRequestParameters: record.CustomRequestParameters,
		PricingTemplateID:       record.PricingTemplateID,
		OpenAITextCapability:    record.OpenAITextCapability,
		OpenAIImageCapability:   record.OpenAIImageCapability,
		RoutingSchedule:         terminaltarget.CompileRoutingSchedule(routingScheduleTimezone, record.RoutingWindows),
		Endpoint: runtimeEndpoint{
			ID:      record.Endpoint.ID,
			Name:    record.Endpoint.Name,
			BaseURL: record.Endpoint.BaseURL,
		},
	}
	if record.PricingTemplate != nil {
		cards := make(map[string]runtimePricingCard, len(record.PricingTemplate.Cards))
		for role, card := range record.PricingTemplate.Cards {
			cards[role] = card
		}
		timezone := dereferenceString(record.PricingTemplate.PricingScheduleTimezone)
		pricingSchedule := terminaltarget.CompilePricingSchedule(timezone, record.PricingTemplate.PricingWindows)
		pricingScheduleDigest := strings.TrimSpace(record.PricingTemplate.PricingScheduleDigest)
		pricingScheduleDigestValid := true
		if strings.TrimSpace(record.PricingTemplate.TemplateKind) == "peak_valley" {
			// The child-table rows and their digest are compiled once into the
			// immutable planning snapshot. Requests only read this boolean; a
			// truncated child read therefore remains unresolved without hashing
			// the same window set on every pricing attempt.
			pricingScheduleDigestValid = len(record.PricingTemplate.PricingWindows) > 0 && pricingScheduleDigest != "" && terminaltarget.PricingWindowsDigest(record.PricingTemplate.PricingWindows) == pricingScheduleDigest
		}
		item.PricingTemplateSnapshot = &runtimePricingTemplateSnapshot{
			ID:                         record.PricingTemplate.ID,
			Name:                       record.PricingTemplate.Name,
			RevisionID:                 record.PricingTemplate.RevisionID,
			PricingUnit:                record.PricingTemplate.PricingUnit,
			PricingCurrencyCode:        record.PricingTemplate.PricingCurrencyCode,
			TemplateKind:               record.PricingTemplate.TemplateKind,
			Cards:                      cards,
			TierInputTokensAbove:       cloneRuntimeIntPointer(record.PricingTemplate.TierInputTokensAbove),
			PricingSchedule:            pricingSchedule,
			PricingScheduleDigest:      pricingScheduleDigest,
			PricingScheduleDigestValid: pricingScheduleDigestValid,
			Version:                    record.PricingTemplate.Version,
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
