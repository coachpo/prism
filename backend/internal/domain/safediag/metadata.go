package safediag

import "strings"

// MetadataField is the fixed stable enum of externally controlled metadata
// fields that MUST be scrubbed before entering ordinary telemetry. New
// external fields MUST extend this schema-versioned enum; unknown fields must
// never fall into an unnamed "other".
type MetadataField int

const (
	MetadataFieldCallerRequestID MetadataField = iota
	MetadataFieldProviderCorrelationID
	MetadataFieldCallerUserAgent
	MetadataFieldUpstreamUserAgent
	MetadataFieldProviderRequestID
	MetadataFieldRequestURL
	MetadataFieldEndpointBaseURL
	MetadataFieldRequestedModelLabel
	MetadataFieldResolvedModelLabel
	MetadataFieldOperationName
	MetadataFieldRequestPath
	MetadataFieldTerminalTargetLabel
	MetadataFieldEndpointLabel
)

// MetadataFieldCount is the fixed cardinality of the enum (13).
const MetadataFieldCount = 13

var metadataFieldNames = map[MetadataField]string{
	MetadataFieldCallerRequestID:       "caller_request_id",
	MetadataFieldProviderCorrelationID: "provider_correlation_id",
	MetadataFieldCallerUserAgent:       "caller_user_agent",
	MetadataFieldUpstreamUserAgent:     "upstream_user_agent",
	MetadataFieldProviderRequestID:     "provider_request_id",
	MetadataFieldRequestURL:            "request_url",
	MetadataFieldEndpointBaseURL:       "endpoint_base_url",
	MetadataFieldRequestedModelLabel:   "requested_model_label",
	MetadataFieldResolvedModelLabel:    "resolved_model_label",
	MetadataFieldOperationName:         "operation_name",
	MetadataFieldRequestPath:           "request_path",
	MetadataFieldTerminalTargetLabel:   "terminal_target_label",
	MetadataFieldEndpointLabel:         "endpoint_label",
}

// MetadataFieldName returns the canonical wire name for a field.
func MetadataFieldName(field MetadataField) string {
	return metadataFieldNames[field]
}

// MetadataFieldCaps returns the per-value byte and code-point caps for a
// field per §4.3. Physical column capacity is applied separately by callers
// as the minimum of the two.
func MetadataFieldCaps(field MetadataField) (maxBytes int, maxCodePoints int) {
	switch field {
	case MetadataFieldCallerRequestID, MetadataFieldProviderCorrelationID:
		return MaxCorrelationValueBytes, MaxCorrelationCodePoints
	case MetadataFieldCallerUserAgent, MetadataFieldUpstreamUserAgent:
		return MaxUserAgentBytes, 0
	case MetadataFieldRequestURL:
		return MaxRequestURLBytes, MaxRequestURLCodePoints
	case MetadataFieldEndpointBaseURL:
		return MaxEndpointBaseURLBytes, MaxEndpointBaseURLCodePoints
	case MetadataFieldOperationName:
		return MaxLabelBytes, MaxOperationNameCodePoints
	case MetadataFieldRequestPath:
		return MaxRequestPathBytes, MaxRequestPathCodePoints
	default:
		return MaxLabelBytes, 0
	}
}

// ScrubMetadataValue applies the fixed value scrubber plus the per-field
// caps. The physical column capacity is taken as an additional minimum when
// provided (>0).
func ScrubMetadataValue(field MetadataField, input string, physicalColumnCodePoints int) ScrubResult {
	result := ScrubValue(input, ScrubOptions{})
	// Apply per-field byte cap then code-point cap.
	truncated := false
	value := result.Value
	maxBytes, maxCodePoints := MetadataFieldCaps(field)
	if maxBytes > 0 && len(value) > maxBytes {
		value, truncated = TruncateUTF8(value, maxBytes)
	}
	if maxCodePoints > 0 && utf8RuneCount(value) > maxCodePoints {
		value, _ = TruncateCodePoints(value, maxCodePoints)
		truncated = true
	}
	if physicalColumnCodePoints > 0 && utf8RuneCount(value) > physicalColumnCodePoints {
		value, _ = TruncateCodePoints(value, physicalColumnCodePoints)
		truncated = true
	}
	result.Value = value
	result.Truncated = truncated
	return result
}

// MetadataProvenance holds the redacted/truncated field arrays for a row.
type MetadataProvenance struct {
	Redacted  []MetadataField
	Truncated []MetadataField
}

// Record adds field-level provenance flags.
func (provenance *MetadataProvenance) Record(field MetadataField, redacted bool, truncated bool) {
	if redacted {
		provenance.Redacted = appendProvenanceField(provenance.Redacted, field)
	}
	if truncated {
		provenance.Truncated = appendProvenanceField(provenance.Truncated, field)
	}
}

func appendProvenanceField(fields []MetadataField, field MetadataField) []MetadataField {
	for _, existing := range fields {
		if existing == field {
			return fields
		}
	}
	return append(fields, field)
}

// CanonicalFieldNames converts fields to sorted, deduplicated canonical wire
// names ordered by enum ordinal.
func CanonicalFieldNames(fields []MetadataField) []string {
	names := make([]string, 0, len(fields))
	for ordinal := 0; ordinal < MetadataFieldCount; ordinal++ {
		field := MetadataField(ordinal)
		for _, existing := range fields {
			if existing == field {
				names = append(names, MetadataFieldName(field))
				break
			}
		}
	}
	return names
}

// ParseMetadataFieldName resolves a canonical wire name back to its enum.
func ParseMetadataFieldName(name string) (MetadataField, bool) {
	for ordinal := 0; ordinal < MetadataFieldCount; ordinal++ {
		field := MetadataField(ordinal)
		if strings.EqualFold(metadataFieldNames[field], name) {
			return field, true
		}
	}
	return 0, false
}
