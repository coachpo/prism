package models

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/coachpo/prism/backend/internal/domain/modelsdev"
)

const maxOverrideStringChars = 500

type overrideFieldSpec struct {
	field    string
	kind     string // string | bool | int | string_list | status | date
	setNull  func(*modelCatalogMetadata)
	setValue func(*modelCatalogMetadata, any)
}

var overrideFieldSpecs = []overrideFieldSpec{
	{field: "name", kind: "string", setNull: func(m *modelCatalogMetadata) { m.Name = nil }, setValue: func(m *modelCatalogMetadata, v any) { m.Name = v.(*string) }},
	{field: "description", kind: "string", setNull: func(m *modelCatalogMetadata) { m.Description = nil }, setValue: func(m *modelCatalogMetadata, v any) { m.Description = v.(*string) }},
	{field: "family", kind: "string", setNull: func(m *modelCatalogMetadata) { m.Family = nil }, setValue: func(m *modelCatalogMetadata, v any) { m.Family = v.(*string) }},
	{field: "release_date", kind: "date", setNull: func(m *modelCatalogMetadata) { m.ReleaseDate = nil }, setValue: func(m *modelCatalogMetadata, v any) { m.ReleaseDate = v.(*string) }},
	{field: "last_updated", kind: "date", setNull: func(m *modelCatalogMetadata) { m.LastUpdated = nil }, setValue: func(m *modelCatalogMetadata, v any) { m.LastUpdated = v.(*string) }},
	{field: "knowledge", kind: "date", setNull: func(m *modelCatalogMetadata) { m.Knowledge = nil }, setValue: func(m *modelCatalogMetadata, v any) { m.Knowledge = v.(*string) }},
	{field: "attachment", kind: "bool", setNull: func(m *modelCatalogMetadata) { m.Attachment = nil }, setValue: func(m *modelCatalogMetadata, v any) { m.Attachment = v.(*bool) }},
	{field: "reasoning", kind: "bool", setNull: func(m *modelCatalogMetadata) { m.Reasoning = nil }, setValue: func(m *modelCatalogMetadata, v any) { m.Reasoning = v.(*bool) }},
	{field: "tool_call", kind: "bool", setNull: func(m *modelCatalogMetadata) { m.ToolCall = nil }, setValue: func(m *modelCatalogMetadata, v any) { m.ToolCall = v.(*bool) }},
	{field: "structured_output", kind: "bool", setNull: func(m *modelCatalogMetadata) { m.StructuredOutput = nil }, setValue: func(m *modelCatalogMetadata, v any) { m.StructuredOutput = v.(*bool) }},
	{field: "temperature", kind: "bool", setNull: func(m *modelCatalogMetadata) { m.Temperature = nil }, setValue: func(m *modelCatalogMetadata, v any) { m.Temperature = v.(*bool) }},
	{field: "modalities_input", kind: "string_list", setNull: func(m *modelCatalogMetadata) { m.ModalitiesInput = nil }, setValue: func(m *modelCatalogMetadata, v any) { m.ModalitiesInput = append([]string(nil), v.([]string)...) }},
	{field: "modalities_output", kind: "string_list", setNull: func(m *modelCatalogMetadata) { m.ModalitiesOutput = nil }, setValue: func(m *modelCatalogMetadata, v any) { m.ModalitiesOutput = append([]string(nil), v.([]string)...) }},
	{field: "limit_context", kind: "int", setNull: func(m *modelCatalogMetadata) { m.LimitContext = nil }, setValue: func(m *modelCatalogMetadata, v any) { m.LimitContext = v.(*int64) }},
	{field: "limit_input", kind: "int", setNull: func(m *modelCatalogMetadata) { m.LimitInput = nil }, setValue: func(m *modelCatalogMetadata, v any) { m.LimitInput = v.(*int64) }},
	{field: "limit_output", kind: "int", setNull: func(m *modelCatalogMetadata) { m.LimitOutput = nil }, setValue: func(m *modelCatalogMetadata, v any) { m.LimitOutput = v.(*int64) }},
	{field: "open_weights", kind: "bool", setNull: func(m *modelCatalogMetadata) { m.OpenWeights = nil }, setValue: func(m *modelCatalogMetadata, v any) { m.OpenWeights = v.(*bool) }},
	{field: "status", kind: "status", setNull: func(m *modelCatalogMetadata) { m.Status = nil }, setValue: func(m *modelCatalogMetadata, v any) { m.Status = v.(*string) }},
}

func decodeOverrideFields(body []byte) (map[string]any, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, newCatalogDomainError(http.StatusBadRequest, "Invalid request body", nil)
	}
	if len(raw) == 0 {
		return nil, newCatalogDomainError(http.StatusUnprocessableEntity, "override payload must carry at least one field", map[string]any{"field": "body"})
	}
	values := make(map[string]any, len(raw))
	for key, valueRaw := range raw {
		var spec *overrideFieldSpec
		for index := range overrideFieldSpecs {
			if overrideFieldSpecs[index].field == key {
				spec = &overrideFieldSpecs[index]
				break
			}
		}
		if spec == nil {
			return nil, newCatalogDomainError(http.StatusUnprocessableEntity, fmt.Sprintf("unknown override field %q", key), map[string]any{"field": key})
		}
		if string(valueRaw) == "null" {
			values[key] = nil
			continue
		}
		parsed, err := parseOverrideValue(spec.kind, valueRaw)
		if err != nil {
			return nil, err
		}
		values[key] = parsed
	}
	return values, nil
}

func parseOverrideValue(kind string, raw json.RawMessage) (any, error) {
	fieldViolation := func(reason, message string) error {
		return newCatalogDomainError(http.StatusUnprocessableEntity, message, map[string]any{"field": kind, "reason": reason})
	}
	switch kind {
	case "string", "date":
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, fieldViolation("invalid_type", "must be a string or null")
		}
		if len(value) > maxOverrideStringChars {
			return nil, fieldViolation("too_long", fmt.Sprintf("must not exceed %d characters", maxOverrideStringChars))
		}
		if kind == "date" && strings.TrimSpace(value) != "" && !isLooseCatalogDate(value) {
			return nil, fieldViolation("invalid_date", "must look like YYYY-MM or YYYY-MM-DD")
		}
		return &value, nil
	case "status":
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, fieldViolation("invalid_type", "must be alpha, beta, deprecated, or null")
		}
		switch value {
		case modelsdev.StatusAlpha, modelsdev.StatusBeta, modelsdev.StatusDeprecated:
			return &value, nil
		default:
			return nil, fieldViolation("invalid_enum", "must be alpha, beta, deprecated, or null")
		}
	case "bool":
		var value bool
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, fieldViolation("invalid_type", "must be a boolean or null")
		}
		return &value, nil
	case "int":
		var value int64
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, fieldViolation("invalid_type", "must be a non-negative integer or null")
		}
		if value < 0 {
			return nil, fieldViolation("invalid_range", "must be a non-negative integer or null")
		}
		return &value, nil
	case "string_list":
		var value []string
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, fieldViolation("invalid_type", "must be an array of strings or null")
		}
		for _, item := range value {
			if len(item) > maxOverrideStringChars {
				return nil, fieldViolation("too_long", fmt.Sprintf("list entries must not exceed %d characters", maxOverrideStringChars))
			}
		}
		return value, nil
	}
	return nil, fieldViolation("invalid_type", "unsupported override type")
}

func isLooseCatalogDate(value string) bool {
	trimmed := strings.TrimSpace(value)
	parts := strings.Split(trimmed, "-")
	if len(parts) != 2 && len(parts) != 3 {
		return false
	}
	if len(parts[0]) != 4 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		for _, r := range part {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	if len(parts) >= 2 && len(parts[1]) != 2 {
		return false
	}
	if len(parts) == 3 && len(parts[2]) != 2 {
		return false
	}
	return true
}
