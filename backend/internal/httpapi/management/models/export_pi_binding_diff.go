package models

import (
	"encoding/json"
	"sort"
	"strconv"
)

// piBindingFieldOrder fixes the stable diff order of refresh previews; it
// covers exactly the seven safe pi.dev leaves a binding freezes.
var piBindingFieldOrder = []struct {
	field  string
	render func(piBindingMetadata) *string
}{
	{field: "name", render: func(m piBindingMetadata) *string { return m.Name }},
	{field: "reasoning", render: func(m piBindingMetadata) *string { return renderPiBool(m.Reasoning) }},
	{field: "input", render: func(m piBindingMetadata) *string { return renderPiStringList(m.Input) }},
	{field: "context_window", render: func(m piBindingMetadata) *string { return renderPiInt(m.ContextWindow) }},
	{field: "max_tokens", render: func(m piBindingMetadata) *string { return renderPiInt(m.MaxTokens) }},
	{field: "thinking_level_map", render: func(m piBindingMetadata) *string { return renderPiThinkingLevelMap(m.ThinkingLevelMap) }},
	{field: "compat", render: func(m piBindingMetadata) *string { return renderPiCompat(m.Compat) }},
}

func renderPiBool(value *bool) *string {
	if value == nil {
		return nil
	}
	rendered := strconvFormatBool(*value)
	return &rendered
}

func renderPiInt(value *int64) *string {
	if value == nil {
		return nil
	}
	rendered := strconv.FormatInt(*value, 10)
	return &rendered
}

func renderPiStringList(values []string) *string {
	if values == nil {
		return nil
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		rendered := "<unrepresentable>"
		return &rendered
	}
	rendered := string(encoded)
	return &rendered
}

func renderPiThinkingLevelMap(values map[string]*string) *string {
	if values == nil {
		return nil
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		rendered := "<unrepresentable>"
		return &rendered
	}
	rendered := string(encoded)
	return &rendered
}

func renderPiCompat(values map[string]any) *string {
	if values == nil {
		return nil
	}
	encoded, err := json.Marshal(canonicalizeCompat(values))
	if err != nil {
		rendered := "<unrepresentable>"
		return &rendered
	}
	rendered := string(encoded)
	return &rendered
}

func canonicalizeCompat(values map[string]any) map[string]any {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	ordered := make(map[string]any, len(values))
	for _, key := range keys {
		ordered[key] = values[key]
	}
	return ordered
}

// diffPiBindingSource compares two source metadata projections field by
// field in the stable order used by refresh previews.
func diffPiBindingSource(current, next piBindingMetadata) ([]piBindingFieldChange, bool) {
	changes := make([]piBindingFieldChange, 0)
	for _, descriptor := range piBindingFieldOrder {
		currentValue, nextValue := descriptor.render(current), descriptor.render(next)
		switch {
		case currentValue == nil && nextValue == nil:
			continue
		case currentValue == nil:
			changes = append(changes, piBindingFieldChange{Field: descriptor.field, Current: nil, Next: nextValue, Kind: "added"})
		case nextValue == nil:
			changes = append(changes, piBindingFieldChange{Field: descriptor.field, Current: currentValue, Next: nil, Kind: "removed"})
		case *currentValue != *nextValue:
			changes = append(changes, piBindingFieldChange{Field: descriptor.field, Current: currentValue, Next: nextValue, Kind: "changed"})
		}
	}
	return changes, len(changes) > 0
}

func appendPiDroppedFieldsDiff(changes []piBindingFieldChange, current, next []string) ([]piBindingFieldChange, bool) {
	currentValue := renderPiDroppedFields(current)
	nextValue := renderPiDroppedFields(next)
	if currentValue == nextValue {
		return changes, false
	}
	currentCopy, nextCopy := currentValue, nextValue
	kind := "changed"
	if len(current) == 0 && len(next) > 0 {
		kind = "added"
	} else if len(current) > 0 && len(next) == 0 {
		kind = "removed"
	}
	return append(changes, piBindingFieldChange{
		Field: "dropped_fields", Current: &currentCopy, Next: &nextCopy, Kind: kind,
	}), true
}

func renderPiDroppedFields(values []string) string {
	copyValues := normalizePiDroppedFields(values)
	encoded, err := json.Marshal(copyValues)
	if err != nil {
		return "[]"
	}
	return string(encoded)
}
