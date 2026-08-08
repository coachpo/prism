package terminaltarget

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"sort"
	"strconv"
	"strings"
)

// Custom request parameters are an optional static top-level JSON object
// attached to a Connection (Terminal Target). Prism applies them as a shallow
// top-level overlay on the provider-native upstream request body of every
// actual attempt that selects that Connection.
//
// A nil value, an explicit JSON null, and an empty object all represent "not
// configured". The value is immutable after parsing; callers must not mutate
// the underlying map or byte slices.

const (
	// CustomRequestParametersMaxCompactBytes is the maximum size of the
	// compact UTF-8 JSON encoding of a configured object.
	CustomRequestParametersMaxCompactBytes = 65536
	// CustomRequestParametersMaxDepth is the maximum nesting depth. The root
	// object has depth 1; entering an object or array increases depth by 1.
	CustomRequestParametersMaxDepth = 16
	// CustomRequestParametersMaxMembers is the maximum total number of object
	// members across the whole tree. Array elements are not counted as
	// members.
	CustomRequestParametersMaxMembers = 256
)

// CustomRequestParametersSafeIntegerMin/Max bound the accepted integer
// literals to the ECMAScript safe-integer range (Number.MIN_SAFE_INTEGER ..
// Number.MAX_SAFE_INTEGER).
const (
	CustomRequestParametersSafeIntegerMin = -(1<<53 - 1)
	CustomRequestParametersSafeIntegerMax = 1<<53 - 1
)

// CustomRequestParametersProtectedKeys are the exact, case-sensitive
// top-level keys that a configuration must not contain. Final model selection,
// streaming mode, and the primary client content/system containers of the
// three API families are owned by Prism.
var CustomRequestParametersProtectedKeys = []string{
	"model",
	"models",
	"stream",
	"messages",
	"input",
	"contents",
	"instructions",
	"system",
	"systemInstruction",
}

// Validation reasons exposed through the management API error envelope.
const (
	CustomRequestParametersReasonNotObject        = "not_object"
	CustomRequestParametersReasonDuplicateKey     = "duplicate_key"
	CustomRequestParametersReasonBlankKey         = "blank_key"
	CustomRequestParametersReasonProtectedField   = "protected_field"
	CustomRequestParametersReasonTooLarge         = "too_large"
	CustomRequestParametersReasonTooDeep          = "too_deep"
	CustomRequestParametersReasonTooManyMembers   = "too_many_members"
	CustomRequestParametersReasonNumberOutOfRange = "number_out_of_range"
)

// CustomRequestParametersValidationError describes why a raw JSON value was
// rejected. Path points at the shallowest locatable failure position inside
// the field (for example "custom_request_parameters.provider.order"). Limit
// is only populated for limit-class failures.
type CustomRequestParametersValidationError struct {
	Reason string
	Path   string
	Limit  int
}

func (err *CustomRequestParametersValidationError) Error() string {
	if err == nil {
		return ""
	}
	message := fmt.Sprintf("invalid custom request parameters: %s", err.Reason)
	if err.Path != "" {
		message += " at " + err.Path
	}
	if err.Limit > 0 {
		message += fmt.Sprintf(" (limit %d)", err.Limit)
	}
	return message
}

var errCustomRequestParametersNotObject = errors.New("root value is not a JSON object")

// CustomRequestParameters is a validated, immutable top-level JSON object.
type CustomRequestParameters struct {
	members map[string]json.RawMessage
	encoded []byte
}

// ParseCustomRequestParametersJSON parses and validates a raw JSON value.
// JSON null, an empty object, and blank input all normalize to an empty value
// (IsEmpty() == true). Any other non-object root, malformed JSON, protected
// key, or limit violation returns a *CustomRequestParametersValidationError.
func ParseCustomRequestParametersJSON(raw []byte) (*CustomRequestParameters, *CustomRequestParametersValidationError) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return &CustomRequestParameters{}, nil
	}
	encoded, validationErr := validateAndCanonicalize(trimmed)
	if validationErr != nil {
		return nil, validationErr
	}
	if len(encoded) == 0 {
		return &CustomRequestParameters{}, nil
	}
	var members map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &members); err != nil {
		// Canonical bytes were just produced from validated JSON; this
		// cannot fail unless the value was tampered with.
		return nil, &CustomRequestParametersValidationError{Reason: CustomRequestParametersReasonNotObject, Path: "custom_request_parameters"}
	}
	if len(members) == 0 {
		return &CustomRequestParameters{}, nil
	}
	return &CustomRequestParameters{members: members, encoded: append([]byte(nil), encoded...)}, nil
}

// UnmarshalJSON lets the value participate in management API decoding.
func (value *CustomRequestParameters) UnmarshalJSON(raw []byte) error {
	parsed, validationErr := ParseCustomRequestParametersJSON(raw)
	if validationErr != nil {
		return validationErr
	}
	*value = *parsed
	return nil
}

// MarshalJSON emits JSON null for unconfigured/empty values and the canonical
// compact object otherwise.
func (value *CustomRequestParameters) MarshalJSON() ([]byte, error) {
	if value == nil || value.IsEmpty() {
		return []byte("null"), nil
	}
	return append([]byte(nil), value.encoded...), nil
}

// IsEmpty reports whether the value is nil, empty, or canonicalized empty.
func (value *CustomRequestParameters) IsEmpty() bool {
	return value == nil || len(value.members) == 0
}

// TopLevelKeyCount returns the number of top-level members (0 when empty).
func (value *CustomRequestParameters) TopLevelKeyCount() int {
	if value == nil {
		return 0
	}
	return len(value.members)
}

// Clone returns an independent deep copy. The copy shares no mutable storage
// with the source.
func (value *CustomRequestParameters) Clone() *CustomRequestParameters {
	if value == nil {
		return nil
	}
	cloned := &CustomRequestParameters{encoded: append([]byte(nil), value.encoded...)}
	if len(value.members) > 0 {
		cloned.members = make(map[string]json.RawMessage, len(value.members))
		for key, member := range value.members {
			cloned.members[key] = append(json.RawMessage(nil), member...)
		}
	}
	return cloned
}

// RawObject returns the canonical compact encoding of the object, or nil when
// empty. Callers must not mutate the returned slice.
func (value *CustomRequestParameters) RawObject() []byte {
	if value == nil {
		return nil
	}
	return value.encoded
}

// OverlayRequestBody applies this configuration as a deterministic top-level
// shallow overlay on a base JSON object body (the provider-native request
// body after model/path rewrite). Non-conflicting top-level members of the
// base are preserved verbatim; matching top-level keys are replaced entirely;
// nested objects are never recursively merged; configured null values are
// sent as literal JSON null. The returned body is compact JSON with sorted
// top-level keys and is freshly allocated per call.
func (value *CustomRequestParameters) OverlayRequestBody(baseBody []byte) ([]byte, error) {
	if value == nil || value.IsEmpty() {
		return append([]byte(nil), baseBody...), nil
	}
	base := map[string]json.RawMessage{}
	if len(bytes.TrimSpace(baseBody)) > 0 {
		if err := json.Unmarshal(baseBody, &base); err != nil {
			return nil, errCustomRequestParametersNotObject
		}
	}
	if base == nil {
		base = map[string]json.RawMessage{}
	}
	keys := make([]string, 0, len(value.members))
	for key := range value.members {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		base[key] = append(json.RawMessage(nil), value.members[key]...)
	}
	return json.Marshal(base)
}

// OverlayRequestBodyFromRaw parses a raw JSON object string and applies it as
// an overlay on the base body. It is the runtime-facing convenience entry for
// snapshot-held raw values.
func OverlayRequestBodyFromRaw(baseBody []byte, rawObject []byte) ([]byte, error) {
	value, validationErr := ParseCustomRequestParametersJSON(rawObject)
	if validationErr != nil {
		return nil, validationErr
	}
	return value.OverlayRequestBody(baseBody)
}

func validateAndCanonicalize(raw []byte) ([]byte, *CustomRequestParametersValidationError) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()

	token, err := decoder.Token()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, nil
		}
		return nil, &CustomRequestParametersValidationError{Reason: CustomRequestParametersReasonNotObject, Path: "custom_request_parameters"}
	}
	if token == nil {
		return nil, nil
	}
	delim, isDelim := token.(json.Delim)
	if !isDelim || delim != '{' {
		return nil, &CustomRequestParametersValidationError{Reason: CustomRequestParametersReasonNotObject, Path: "custom_request_parameters"}
	}

	walker := &customRequestParametersWalker{
		decoder:   decoder,
		protected: protectedKeySet(),
	}
	if validationErr := walker.walkObject(1, "custom_request_parameters"); validationErr != nil {
		return nil, validationErr
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, &CustomRequestParametersValidationError{Reason: CustomRequestParametersReasonNotObject, Path: "custom_request_parameters"}
	}

	// Canonical compact re-encoding with sorted keys at every level and
	// number literals preserved.
	var payload any
	canonicalDecoder := json.NewDecoder(bytes.NewReader(raw))
	canonicalDecoder.UseNumber()
	if err := canonicalDecoder.Decode(&payload); err != nil {
		return nil, &CustomRequestParametersValidationError{Reason: CustomRequestParametersReasonNotObject, Path: "custom_request_parameters"}
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, &CustomRequestParametersValidationError{Reason: CustomRequestParametersReasonNotObject, Path: "custom_request_parameters"}
	}
	if len(encoded) > CustomRequestParametersMaxCompactBytes {
		return nil, &CustomRequestParametersValidationError{Reason: CustomRequestParametersReasonTooLarge, Path: "custom_request_parameters", Limit: CustomRequestParametersMaxCompactBytes}
	}
	return encoded, nil
}

func protectedKeySet() map[string]struct{} {
	keys := make(map[string]struct{}, len(CustomRequestParametersProtectedKeys))
	for _, key := range CustomRequestParametersProtectedKeys {
		keys[key] = struct{}{}
	}
	return keys
}

type customRequestParametersWalker struct {
	decoder   *json.Decoder
	protected map[string]struct{}
	members   int
}

func (walker *customRequestParametersWalker) walkObject(depth int, path string) *CustomRequestParametersValidationError {
	if depth > CustomRequestParametersMaxDepth {
		return &CustomRequestParametersValidationError{Reason: CustomRequestParametersReasonTooDeep, Path: path, Limit: CustomRequestParametersMaxDepth}
	}
	seen := make(map[string]struct{})
	for walker.decoder.More() {
		keyToken, err := walker.decoder.Token()
		if err != nil {
			return &CustomRequestParametersValidationError{Reason: CustomRequestParametersReasonNotObject, Path: path}
		}
		key, ok := keyToken.(string)
		if !ok {
			return &CustomRequestParametersValidationError{Reason: CustomRequestParametersReasonNotObject, Path: path}
		}
		walker.members++
		if walker.members > CustomRequestParametersMaxMembers {
			return &CustomRequestParametersValidationError{Reason: CustomRequestParametersReasonTooManyMembers, Path: path, Limit: CustomRequestParametersMaxMembers}
		}
		if _, duplicate := seen[key]; duplicate {
			return &CustomRequestParametersValidationError{Reason: CustomRequestParametersReasonDuplicateKey, Path: path + "." + key}
		}
		seen[key] = struct{}{}
		if strings.TrimSpace(key) == "" {
			return &CustomRequestParametersValidationError{Reason: CustomRequestParametersReasonBlankKey, Path: path}
		}
		if depth == 1 {
			if _, protected := walker.protected[key]; protected {
				return &CustomRequestParametersValidationError{Reason: CustomRequestParametersReasonProtectedField, Path: path + "." + key}
			}
		}
		if validationErr := walker.walkValue(depth+1, path+"."+key); validationErr != nil {
			return validationErr
		}
	}
	if _, err := walker.decoder.Token(); err != nil {
		return &CustomRequestParametersValidationError{Reason: CustomRequestParametersReasonNotObject, Path: path}
	}
	return nil
}

func (walker *customRequestParametersWalker) walkArray(depth int, path string) *CustomRequestParametersValidationError {
	if depth > CustomRequestParametersMaxDepth {
		return &CustomRequestParametersValidationError{Reason: CustomRequestParametersReasonTooDeep, Path: path, Limit: CustomRequestParametersMaxDepth}
	}
	for walker.decoder.More() {
		if validationErr := walker.walkValue(depth+1, path); validationErr != nil {
			return validationErr
		}
	}
	if _, err := walker.decoder.Token(); err != nil {
		return &CustomRequestParametersValidationError{Reason: CustomRequestParametersReasonNotObject, Path: path}
	}
	return nil
}

func (walker *customRequestParametersWalker) walkValue(depth int, path string) *CustomRequestParametersValidationError {
	token, err := walker.decoder.Token()
	if err != nil {
		return &CustomRequestParametersValidationError{Reason: CustomRequestParametersReasonNotObject, Path: path}
	}
	switch typed := token.(type) {
	case json.Delim:
		switch typed {
		case '{':
			return walker.walkObject(depth, path)
		case '[':
			return walker.walkArray(depth, path)
		default:
			return &CustomRequestParametersValidationError{Reason: CustomRequestParametersReasonNotObject, Path: path}
		}
	case json.Number:
		return validateCustomRequestParametersNumber(typed, path)
	default:
		return nil
	}
}

func validateCustomRequestParametersNumber(number json.Number, path string) *CustomRequestParametersValidationError {
	literal := strings.TrimSpace(string(number))
	if literal == "" {
		return &CustomRequestParametersValidationError{Reason: CustomRequestParametersReasonNumberOutOfRange, Path: path}
	}
	if strings.ContainsAny(literal, ".eE") {
		parsed, err := strconv.ParseFloat(literal, 64)
		if err != nil || math.IsInf(parsed, 0) || math.IsNaN(parsed) {
			return &CustomRequestParametersValidationError{Reason: CustomRequestParametersReasonNumberOutOfRange, Path: path}
		}
		return nil
	}
	integer, ok := new(big.Int).SetString(literal, 10)
	if !ok {
		return &CustomRequestParametersValidationError{Reason: CustomRequestParametersReasonNumberOutOfRange, Path: path}
	}
	if integer.Cmp(big.NewInt(CustomRequestParametersSafeIntegerMax)) > 0 ||
		integer.Cmp(big.NewInt(CustomRequestParametersSafeIntegerMin)) < 0 {
		return &CustomRequestParametersValidationError{Reason: CustomRequestParametersReasonNumberOutOfRange, Path: path}
	}
	return nil
}
