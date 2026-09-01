package connections

import (
	"net/http"

	"github.com/coachpo/prism/backend/internal/domain/terminaltarget"
)

const upstreamModelIDFieldName = "upstream_model_id"

// validateUpstreamModelIDValue maps the HTTP-neutral Terminal Target value
// contract onto the management API's flat 422 field-error envelope.
func validateUpstreamModelIDValue(field string, value string) (string, error) {
	normalized, validationErr := terminaltarget.NormalizeUpstreamModelID(value)
	if validationErr == nil {
		return normalized, nil
	}
	detail := field + " must not be blank"
	if validationErr.Reason == terminaltarget.UpstreamModelIDReasonTooLong {
		detail = field + " must be at most 200 characters"
	}
	return "", upstreamModelIDValidationError(field, validationErr.Reason, validationErr.Limit, detail)
}

func upstreamModelIDValidationError(field string, reason string, limit int, detail string) error {
	fields := map[string]any{
		"field":  field,
		"path":   field,
		"reason": reason,
	}
	if limit > 0 {
		fields["limit"] = limit
	}
	return &DomainError{StatusCode: http.StatusUnprocessableEntity, Detail: detail, Fields: fields}
}

// OptionalStringFrom builds the package-private presence marker from a
// caller-owned Set/Value pair. It exists for the models composite-create
// passthrough, which decodes the same wire field with its own optionalString.
func OptionalStringFrom(set bool, value *string) optionalString {
	return optionalString{Set: set, Value: value}
}

// resolveUpstreamModelIDCreate resolves the create-time upstream model id.
// Omitting the field writes the owner model's current model_id explicitly to
// the database. An explicit JSON null, a blank value, and an over-length
// value are all 422 field errors before any write happens.
func resolveUpstreamModelIDCreate(ownerModelID string, requested optionalString) (string, error) {
	if !requested.Set {
		return validateUpstreamModelIDValue(upstreamModelIDFieldName, ownerModelID)
	}
	if requested.Value == nil {
		return "", upstreamModelIDValidationError(upstreamModelIDFieldName, "required", 0, "upstream_model_id must not be null; omit the field to default to the owner model_id")
	}
	return validateUpstreamModelIDValue(upstreamModelIDFieldName, *requested.Value)
}

// resolveUpstreamModelIDUpdate resolves the PATCH-time upstream model id.
// Omitting the field preserves the stored value; an explicit null cannot
// clear the identity and is a 422; a provided value replaces it after
// validation.
func resolveUpstreamModelIDUpdate(current *string, requested optionalString) (*string, error) {
	if !requested.Set {
		return current, nil
	}
	if requested.Value == nil {
		return nil, upstreamModelIDValidationError(upstreamModelIDFieldName, "required", 0, "upstream_model_id cannot be cleared")
	}
	normalized, err := validateUpstreamModelIDValue(upstreamModelIDFieldName, *requested.Value)
	if err != nil {
		return nil, err
	}
	return &normalized, nil
}
