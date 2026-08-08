package connections

import (
	"bytes"
	"encoding/json"
	"net/http"

	"github.com/coachpo/prism/backend/internal/domain/terminaltarget"
)

const customRequestParametersFieldName = "custom_request_parameters"

// resolveCustomRequestParametersCreate applies the create contract: missing,
// null, blank, and empty-object inputs all normalize to unconfigured; any
// other valid object is validated and canonicalized; violations return a 422
// with the locatable field error envelope.
func resolveCustomRequestParametersCreate(field optionalCustomRequestParameters) (*terminaltarget.CustomRequestParameters, error) {
	if !field.Set {
		return nil, nil
	}
	return parseCustomRequestParametersField(field.Raw)
}

// resolveCustomRequestParametersUpdate applies the PATCH contract: a missing
// field keeps the current value; null and empty object clear it; a non-empty
// valid object replaces it wholesale; any violation fails the whole PATCH
// atomically.
func resolveCustomRequestParametersUpdate(current *terminaltarget.CustomRequestParameters, field optionalCustomRequestParameters) (*terminaltarget.CustomRequestParameters, error) {
	if !field.Set {
		return current, nil
	}
	return parseCustomRequestParametersField(field.Raw)
}

func parseCustomRequestParametersField(raw json.RawMessage) (*terminaltarget.CustomRequestParameters, error) {
	value, validationErr := terminaltarget.ParseCustomRequestParametersJSON(raw)
	if validationErr != nil {
		return nil, customRequestParametersValidationDomainError(validationErr)
	}
	if value.IsEmpty() {
		return nil, nil
	}
	return value, nil
}

func customRequestParametersValidationDomainError(validationErr *terminaltarget.CustomRequestParametersValidationError) error {
	fields := map[string]any{
		"field":  customRequestParametersFieldName,
		"path":   validationErr.Path,
		"reason": validationErr.Reason,
	}
	if validationErr.Limit > 0 {
		fields["limit"] = validationErr.Limit
	}
	return &domainError{
		StatusCode: http.StatusUnprocessableEntity,
		Detail:     "Invalid custom request parameters",
		Fields:     fields,
	}
}

// customRequestParametersIsNullLiteral reports whether the raw field value is
// the JSON null literal.
func customRequestParametersIsNullLiteral(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}
