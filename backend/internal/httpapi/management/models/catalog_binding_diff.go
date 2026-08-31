package models

import (
	"strconv"
	"strings"
)

// catalogFieldOrder fixes the stable diff order of refresh previews.
var catalogFieldOrder = []struct {
	field   string
	get     func(m modelCatalogMetadata) *string
	set     func(*modelCatalogMetadata, *string)
	boolGet func(m modelCatalogMetadata) *bool
	boolSet func(*modelCatalogMetadata, *bool)
	intGet  func(m modelCatalogMetadata) *int64
	intSet  func(*modelCatalogMetadata, *int64)
	listGet func(m modelCatalogMetadata) []string
	listSet func(*modelCatalogMetadata, []string)
}{
	{field: "name", get: func(m modelCatalogMetadata) *string { return m.Name }, set: func(m *modelCatalogMetadata, v *string) { m.Name = v }},
	{field: "description", get: func(m modelCatalogMetadata) *string { return m.Description }, set: func(m *modelCatalogMetadata, v *string) { m.Description = v }},
	{field: "family", get: func(m modelCatalogMetadata) *string { return m.Family }, set: func(m *modelCatalogMetadata, v *string) { m.Family = v }},
	{field: "release_date", get: func(m modelCatalogMetadata) *string { return m.ReleaseDate }, set: func(m *modelCatalogMetadata, v *string) { m.ReleaseDate = v }},
	{field: "last_updated", get: func(m modelCatalogMetadata) *string { return m.LastUpdated }, set: func(m *modelCatalogMetadata, v *string) { m.LastUpdated = v }},
	{field: "knowledge", get: func(m modelCatalogMetadata) *string { return m.Knowledge }, set: func(m *modelCatalogMetadata, v *string) { m.Knowledge = v }},
	{field: "attachment", boolGet: func(m modelCatalogMetadata) *bool { return m.Attachment }, boolSet: func(m *modelCatalogMetadata, v *bool) { m.Attachment = v }},
	{field: "reasoning", boolGet: func(m modelCatalogMetadata) *bool { return m.Reasoning }, boolSet: func(m *modelCatalogMetadata, v *bool) { m.Reasoning = v }},
	{field: "tool_call", boolGet: func(m modelCatalogMetadata) *bool { return m.ToolCall }, boolSet: func(m *modelCatalogMetadata, v *bool) { m.ToolCall = v }},
	{field: "structured_output", boolGet: func(m modelCatalogMetadata) *bool { return m.StructuredOutput }, boolSet: func(m *modelCatalogMetadata, v *bool) { m.StructuredOutput = v }},
	{field: "temperature", boolGet: func(m modelCatalogMetadata) *bool { return m.Temperature }, boolSet: func(m *modelCatalogMetadata, v *bool) { m.Temperature = v }},
	{field: "modalities_input", listGet: func(m modelCatalogMetadata) []string { return m.ModalitiesInput }, listSet: func(m *modelCatalogMetadata, v []string) { m.ModalitiesInput = v }},
	{field: "modalities_output", listGet: func(m modelCatalogMetadata) []string { return m.ModalitiesOutput }, listSet: func(m *modelCatalogMetadata, v []string) { m.ModalitiesOutput = v }},
	{field: "limit_context", intGet: func(m modelCatalogMetadata) *int64 { return m.LimitContext }, intSet: func(m *modelCatalogMetadata, v *int64) { m.LimitContext = v }},
	{field: "limit_input", intGet: func(m modelCatalogMetadata) *int64 { return m.LimitInput }, intSet: func(m *modelCatalogMetadata, v *int64) { m.LimitInput = v }},
	{field: "limit_output", intGet: func(m modelCatalogMetadata) *int64 { return m.LimitOutput }, intSet: func(m *modelCatalogMetadata, v *int64) { m.LimitOutput = v }},
	{field: "open_weights", boolGet: func(m modelCatalogMetadata) *bool { return m.OpenWeights }, boolSet: func(m *modelCatalogMetadata, v *bool) { m.OpenWeights = v }},
	{field: "status", get: func(m modelCatalogMetadata) *string { return m.Status }, set: func(m *modelCatalogMetadata, v *string) { m.Status = v }},
}

// diffCatalogSource compares two metadata projections field by field in the
// stable order. Values render as their canonical strings so booleans, lists,
// and numbers diff uniformly. This is the refresh-preview contract surface:
// preview responses render these rows verbatim.
func diffCatalogSource(current, next modelCatalogMetadata) ([]modelCatalogFieldChange, bool) {
	changes := make([]modelCatalogFieldChange, 0)
	renderString := func(value *string) *string { return value }
	renderBool := func(value *bool) *string {
		if value == nil {
			return nil
		}
		rendered := strconvFormatBool(*value)
		return &rendered
	}
	renderInt := func(value *int64) *string {
		if value == nil {
			return nil
		}
		rendered := strconv.FormatInt(*value, 10)
		return &rendered
	}
	renderList := func(value []string) *string {
		if value == nil {
			return nil
		}
		rendered := "[" + strings.Join(value, ",") + "]"
		return &rendered
	}
	for _, descriptor := range catalogFieldOrder {
		var currentValue, nextValue *string
		switch {
		case descriptor.get != nil:
			currentValue, nextValue = renderString(descriptor.get(current)), renderString(descriptor.get(next))
		case descriptor.boolGet != nil:
			currentValue, nextValue = renderBool(descriptor.boolGet(current)), renderBool(descriptor.boolGet(next))
		case descriptor.intGet != nil:
			currentValue, nextValue = renderInt(descriptor.intGet(current)), renderInt(descriptor.intGet(next))
		case descriptor.listGet != nil:
			currentValue, nextValue = renderList(descriptor.listGet(current)), renderList(descriptor.listGet(next))
		}
		switch {
		case currentValue == nil && nextValue == nil:
			continue
		case currentValue == nil:
			changes = append(changes, modelCatalogFieldChange{Field: descriptor.field, Current: nil, Next: nextValue, Kind: "added"})
		case nextValue == nil:
			changes = append(changes, modelCatalogFieldChange{Field: descriptor.field, Current: currentValue, Next: nil, Kind: "removed"})
		case *currentValue != *nextValue:
			changes = append(changes, modelCatalogFieldChange{Field: descriptor.field, Current: currentValue, Next: nextValue, Kind: "changed"})
		}
	}
	return changes, len(changes) > 0
}

func strconvFormatBool(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
