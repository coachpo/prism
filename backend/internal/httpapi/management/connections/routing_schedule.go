package connections

import (
	"bytes"
	"encoding/json"
	"net/http"

	"github.com/coachpo/prism/backend/internal/domain/terminaltarget"
)

const routingScheduleFieldName = "routing_schedule"

// routingScheduleReasonMalformed covers a field value that never reaches the
// domain validator: a non-object root, an unknown key, or a wrong member type.
// The domain reason set describes semantically invalid schedules; this one
// describes an undecodable field, so it stays local to the HTTP layer.
const routingScheduleReasonMalformed = "malformed"

// resolveRoutingScheduleCreate applies the create contract: a missing field and
// an explicit null both mean unconfigured; any other value is decoded,
// validated and normalized before a single row is written.
func resolveRoutingScheduleCreate(field RoutingScheduleInput) (*string, []terminaltarget.Window, error) {
	if !field.Set || routingScheduleIsNullLiteral(field.Raw) {
		return nil, nil, nil
	}
	return parseRoutingScheduleField(field.Raw)
}

// resolveRoutingScheduleUpdate applies the PATCH contract: a missing field
// keeps the current configuration, null clears it, and an object replaces it
// wholesale. Windows are never merged — a wire window carries no stable
// identity, so a merge would have to guess which stored row a payload row
// meant, and PATCH is specified as whole-field replacement.
func resolveRoutingScheduleUpdate(currentTimezone *string, currentWindows []terminaltarget.Window, field RoutingScheduleInput) (*string, []terminaltarget.Window, error) {
	if !field.Set {
		return currentTimezone, currentWindows, nil
	}
	if routingScheduleIsNullLiteral(field.Raw) {
		return nil, nil, nil
	}
	return parseRoutingScheduleField(field.Raw)
}

// parseRoutingScheduleField is the only decode point for the field. It builds
// its own decoder with DisallowUnknownFields because the outer decodeJSONBody
// setting does not propagate into a custom UnmarshalJSON: without this, a
// misspelled "weekday_mask" would be silently dropped and the connection would
// be saved with a schedule the operator never wrote.
func parseRoutingScheduleField(raw json.RawMessage) (*string, []terminaltarget.Window, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var payload RoutingSchedulePayload
	if err := decoder.Decode(&payload); err != nil {
		return nil, nil, routingScheduleMalformedDomainError()
	}
	if decoder.More() {
		return nil, nil, routingScheduleMalformedDomainError()
	}
	windows := make([]terminaltarget.Window, 0, len(payload.Windows))
	for _, window := range payload.Windows {
		windows = append(windows, terminaltarget.Window{
			WeekdayMask: window.WeekdayMask,
			StartMinute: window.StartMinute,
			EndMinute:   window.EndMinute,
		})
	}
	if validationErr := terminaltarget.ValidateRoutingSchedule(payload.Timezone, windows); validationErr != nil {
		return nil, nil, routingScheduleValidationDomainError(validationErr)
	}
	// Compile normalizes (sorts and de-duplicates) into a freshly allocated
	// slice, so the stored rows match what routing will later evaluate.
	compiled := terminaltarget.CompileRoutingSchedule(payload.Timezone, windows)
	timezone := compiled.Timezone
	return &timezone, compiled.Windows, nil
}

func routingScheduleMalformedDomainError() error {
	return &DomainError{
		StatusCode: http.StatusUnprocessableEntity,
		Detail:     "Invalid routing schedule",
		Fields: map[string]any{
			"field":  routingScheduleFieldName,
			"path":   routingScheduleFieldName,
			"reason": routingScheduleReasonMalformed,
		},
	}
}

// routingScheduleValidationDomainError maps a domain validation failure onto
// the locatable field-error envelope. The status split follows
// settings.normalizeTimezonePreference for the two reasons it already covers
// (over-length is a 400, an unknown name is a 422) and adds one tier that has
// no precedent in this repository: a missing zoneinfo database is a server
// capability gap, not caller input, so reporting it as 422 would send the
// operator hunting for a typo in a name that is actually correct.
func routingScheduleValidationDomainError(validationErr *terminaltarget.RoutingScheduleValidationError) error {
	statusCode := http.StatusUnprocessableEntity
	switch validationErr.Reason {
	case terminaltarget.RoutingScheduleReasonTimezoneTooLong:
		statusCode = http.StatusBadRequest
	case terminaltarget.RoutingScheduleReasonTimezoneDatabaseUnavailable:
		statusCode = http.StatusServiceUnavailable
	}
	fields := map[string]any{
		"field":  routingScheduleFieldName,
		"path":   validationErr.Path,
		"reason": validationErr.Reason,
	}
	if validationErr.Index >= 0 {
		fields["index"] = validationErr.Index
	}
	if validationErr.Limit > 0 {
		fields["limit"] = validationErr.Limit
	}
	return &DomainError{
		StatusCode: statusCode,
		Detail:     "Invalid routing schedule",
		Fields:     fields,
	}
}

// routingSchedulePayloadFromRecord renders stored configuration onto the wire.
// It returns nil for an unconfigured connection so the JSON field is null
// rather than an empty object that would read as "configured with no windows".
//
// The window slice stays nil when there is nothing to render, matching
// routingWindowsFromPayload in the other direction: the record/response
// round-trip is asserted with DeepEqual, which treats a nil slice and an empty
// slice as different. A schedule that passed validation always has at least one
// window, so the empty case is unreachable through the API.
func routingSchedulePayloadFromRecord(timezone *string, windows []terminaltarget.Window) *RoutingSchedulePayload {
	if timezone == nil && len(windows) == 0 {
		return nil
	}
	payload := &RoutingSchedulePayload{}
	if timezone != nil {
		payload.Timezone = *timezone
	}
	if len(windows) == 0 {
		return payload
	}
	payload.Windows = make([]RoutingWindowPayload, 0, len(windows))
	for _, window := range windows {
		payload.Windows = append(payload.Windows, RoutingWindowPayload{
			WeekdayMask: window.WeekdayMask,
			StartMinute: window.StartMinute,
			EndMinute:   window.EndMinute,
		})
	}
	return payload
}

func routingWindowPayloadsFromWindows(windows []terminaltarget.Window) []RoutingWindowPayload {
	if len(windows) == 0 {
		return nil
	}
	payloads := make([]RoutingWindowPayload, 0, len(windows))
	for _, window := range windows {
		payloads = append(payloads, RoutingWindowPayload{
			WeekdayMask: window.WeekdayMask,
			StartMinute: window.StartMinute,
			EndMinute:   window.EndMinute,
		})
	}
	return payloads
}

// routingWindowsFromPayload is the inverse of routingSchedulePayloadFromRecord.
func routingWindowsFromPayload(payload *RoutingSchedulePayload) []terminaltarget.Window {
	if payload == nil || len(payload.Windows) == 0 {
		return nil
	}
	windows := make([]terminaltarget.Window, 0, len(payload.Windows))
	for _, window := range payload.Windows {
		windows = append(windows, terminaltarget.Window{
			WeekdayMask: window.WeekdayMask,
			StartMinute: window.StartMinute,
			EndMinute:   window.EndMinute,
		})
	}
	return windows
}

// routingScheduleConfigFromResponse unpacks the wire payload back into the
// storage-shaped pair, so update paths can feed the current configuration into
// the PATCH resolver and the window writer.
func routingScheduleConfigFromResponse(item connectionResponse) (*string, []terminaltarget.Window) {
	if item.RoutingSchedule == nil {
		return nil, nil
	}
	timezone := item.RoutingSchedule.Timezone
	return &timezone, routingWindowsFromPayload(item.RoutingSchedule)
}

func routingScheduleIsNullLiteral(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}
