package modelexport

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestManualEnhancementAcceptsSafePrimitiveValues(t *testing.T) {
	enhancement := ManualEnhancement{Fields: json.RawMessage(`{"reasoning":false,"options":{"temperature":0},"tags":[],"note":""}`)}
	if err := enhancement.Validate(); err != nil {
		t.Fatalf("safe false/zero/empty values must stay valid and present: %v", err)
	}
}

func TestManualEnhancementRejectsTrailingJSON(t *testing.T) {
	for _, fields := range []json.RawMessage{
		json.RawMessage(`{"name":"safe"}{"reasoning":true}`),
		json.RawMessage(`{"name":"safe"} trailing`),
	} {
		if err := (ManualEnhancement{Fields: fields}).Validate(); err == nil {
			t.Fatalf("trailing JSON/input must fail closed: %s", fields)
		}
	}
}

func TestManualEnhancementRejectsWrongTypesAndUnknownTargetFields(t *testing.T) {
	cases := []struct {
		name     string
		platform Platform
		fields   json.RawMessage
	}{
		{name: "Pi wrong type", platform: PlatformPi, fields: json.RawMessage(`{"reasoning":"yes"}`)},
		{name: "Pi unknown field", platform: PlatformPi, fields: json.RawMessage(`{"mystery":true}`)},
		{name: "OpenCode wrong nested type", platform: PlatformOpenCode, fields: json.RawMessage(`{"limit":{"context":"many","output":1}}`)},
		{name: "OpenCode unknown field", platform: PlatformOpenCode, fields: json.RawMessage(`{"mystery":true}`)},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := (ManualEnhancement{Fields: testCase.fields}).ValidateForPlatform(testCase.platform)
			var invalid *ErrInvalidEnhancement
			if !errors.As(err, &invalid) {
				t.Fatalf("target-schema rejection = %T %v, want ErrInvalidEnhancement", err, err)
			}
		})
	}
}

func TestFullDocumentValidatorsReturnTypedTargetSchemaErrors(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		validate func() error
	}{
		{name: "Pi", validate: func() error {
			return validatePiDocument(map[string]any{"providers": map[string]any{}})
		}},
		{name: "OpenCode", validate: func() error {
			return validateOpenCodeDocument(map[string]any{"$schema": "wrong", "provider": map[string]any{}})
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			err := testCase.validate()
			var schemaError *ErrTargetSchema
			if !errors.As(err, &schemaError) {
				t.Fatalf("full document validation = %T %v, want ErrTargetSchema", err, err)
			}
		})
	}
}

func TestApplyEnhancementFillOverrideLockedSensitive(t *testing.T) {
	object := map[string]any{"id": "model-a", "cost": map[string]any{"input": decimal("1")}}
	enhancement := ManualEnhancement{
		Fields: rawValue(map[string]any{
			"name":   "Custom",
			"id":     "hijack",
			"apiKey": "sk-attack",
		}),
	}
	if err := enhancement.Validate(); err == nil {
		t.Fatalf("sensitive keys must fail validation")
	}
	err := applyEnhancement(object, enhancement, piLockedPaths)
	if err == nil {
		t.Fatalf("locked id must fail closed")
	}
	if _, ok := object["name"]; ok {
		t.Fatalf("nothing may apply after a locked-path failure")
	}

	fill := ManualEnhancement{Fields: rawValue(map[string]any{"name": "Custom"})}
	if err := applyEnhancement(object, fill, piLockedPaths); err != nil {
		t.Fatalf("fill enhancement: %v", err)
	}
	if object["name"] != "Custom" {
		t.Fatalf("missing key must fill, got %v", object["name"])
	}
	if _, ok := object["compat"]; !ok {
		second := ManualEnhancement{Fields: rawValue(map[string]any{"name": "Ignored"})}
		if err := applyEnhancement(object, second, piLockedPaths); err != nil {
			t.Fatalf("second pass: %v", err)
		}
		if object["name"] != "Custom" {
			t.Fatalf("existing keys stay untouched without override_fields, got %v", object["name"])
		}
		override := ManualEnhancement{
			Fields:         rawValue(map[string]any{"name": "Final"}),
			OverrideFields: []string{"name"},
		}
		if err := applyEnhancement(object, override, piLockedPaths); err != nil {
			t.Fatalf("override pass: %v", err)
		}
		if object["name"] != "Final" {
			t.Fatalf("override_fields must replace, got %v", object["name"])
		}
	}
}

func TestCheckLockedPathBlocksSubtrees(t *testing.T) {
	locked := []string{"provider", "options.baseURL"}
	for _, key := range []string{"provider", "provider.npm", "options.baseURL"} {
		if err := checkLockedPath(key, locked); err == nil {
			t.Fatalf("%q must be locked", key)
		}
	}
	if err := checkLockedPath("options", locked); err == nil {
		t.Fatalf("parent of locked subtree must be rejected to protect the child")
	}
	if err := checkLockedPath("limit", locked); err != nil {
		t.Fatalf("unrelated key must pass: %v", err)
	}
}
