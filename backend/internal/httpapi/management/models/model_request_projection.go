package models

import (
	"database/sql"
	"encoding/json"
	"strings"

	"github.com/coachpo/prism/backend/internal/domain/terminaltarget"
	"github.com/coachpo/prism/backend/internal/httpapi/management/connections"
)

func accessTargetRequestsFromRecords(records []accessTargetRecord) []modelAccessTargetRequest {
	if len(records) == 0 {
		return []modelAccessTargetRequest{}
	}
	ordered := cloneAccessTargetRecords(records)
	sortAccessTargetRecords(ordered)
	items := make([]modelAccessTargetRequest, 0, len(ordered))
	for _, record := range ordered {
		enabled := record.IsEnabled
		request := modelAccessTargetRequest{TargetType: record.TargetType, Position: record.Position, IsEnabled: &enabled}
		if record.TargetType == "model" && record.TargetModel != nil {
			modelID := record.TargetModel.ModelID
			request.TargetModelID = &modelID
		}
		if record.TargetType == "connection" && record.TargetConnectionID != nil {
			connectionID := *record.TargetConnectionID
			request.ConnectionID = &connectionID
		}
		items = append(items, request)
	}
	return items
}

func stringPtrFromModelRecord(record *modelRecord) *string {
	if record == nil {
		return nil
	}
	return stringPtr(record.ModelID)
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

// routingWindowsFromPayload converts the assembled wire configuration back into
// domain windows for the state projection.
func routingWindowsFromPayload(payload *connections.RoutingSchedulePayload) []terminaltarget.Window {
	if payload == nil || len(payload.Windows) == 0 {
		return nil
	}
	windows := make([]terminaltarget.Window, 0, len(payload.Windows))
	for _, window := range payload.Windows {
		windows = append(windows, terminaltarget.Window{WeekdayMask: window.WeekdayMask, StartMinute: window.StartMinute, EndMinute: window.EndMinute})
	}
	return windows
}

// routingScheduleTimezoneFromSummary reads the timezone off an assembled
// summary. The wire payload is the single source here: the unexported carrier
// field is only populated on the scan path, while composite reads build the
// summary from the payload.
func routingScheduleTimezoneFromSummary(summary *connectionTargetSummary) *string {
	if summary == nil || summary.RoutingSchedule == nil {
		return nil
	}
	timezone := summary.RoutingSchedule.Timezone
	return &timezone
}
