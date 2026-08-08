package terminaltarget

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestParseCustomRequestParametersCanonicalization(t *testing.T) {
	tests := []struct {
		name        string
		raw         string
		wantEmpty   bool
		wantCount   int
		wantEncoded string
	}{
		{name: "blank input", raw: "", wantEmpty: true, wantCount: 0},
		{name: "whitespace input", raw: "   \n\t ", wantEmpty: true, wantCount: 0},
		{name: "null literal", raw: "null", wantEmpty: true, wantCount: 0},
		{name: "empty object", raw: "{}", wantEmpty: true, wantCount: 0},
		{name: "object with whitespace", raw: "  {  }  ", wantEmpty: true, wantCount: 0},
		{
			name:        "nested object sorted keys",
			raw:         `{"provider":{"allow_fallbacks":false,"only":["deepinfra/turbo"]},"temperature":0.7}`,
			wantEmpty:   false,
			wantCount:   2,
			wantEncoded: `{"provider":{"allow_fallbacks":false,"only":["deepinfra/turbo"]},"temperature":0.7}`,
		},
		{
			name:        "client formatting canonicalized",
			raw:         "{\n  \"b\": [1, 2],\n  \"a\": {\"z\": true, \"y\": null}\n}",
			wantEmpty:   false,
			wantCount:   2,
			wantEncoded: `{"a":{"y":null,"z":true},"b":[1,2]}`,
		},
		{
			name:        "scalar and string values",
			raw:         `{"s":"text","i":42,"f":1.5,"b":false,"n":null,"arr":["x",{"k":1}]}`,
			wantEmpty:   false,
			wantCount:   6,
			wantEncoded: `{"arr":["x",{"k":1}],"b":false,"f":1.5,"i":42,"n":null,"s":"text"}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value, validationErr := ParseCustomRequestParametersJSON([]byte(test.raw))
			if validationErr != nil {
				t.Fatalf("expected valid parse, got %v", validationErr)
			}
			if value.IsEmpty() != test.wantEmpty {
				t.Fatalf("IsEmpty() = %v, want %v", value.IsEmpty(), test.wantEmpty)
			}
			if value.TopLevelKeyCount() != test.wantCount {
				t.Fatalf("TopLevelKeyCount() = %d, want %d", value.TopLevelKeyCount(), test.wantCount)
			}
			if !test.wantEmpty && string(value.RawObject()) != test.wantEncoded {
				t.Fatalf("encoded = %s, want %s", value.RawObject(), test.wantEncoded)
			}
		})
	}
}

func TestParseCustomRequestParametersValidationReasons(t *testing.T) {
	tests := []struct {
		name       string
		raw        string
		wantReason string
		wantPath   string
		wantLimit  int
	}{
		{name: "array root", raw: `[1,2]`, wantReason: CustomRequestParametersReasonNotObject, wantPath: "custom_request_parameters"},
		{name: "string root", raw: `"hello"`, wantReason: CustomRequestParametersReasonNotObject, wantPath: "custom_request_parameters"},
		{name: "number root", raw: `42`, wantReason: CustomRequestParametersReasonNotObject, wantPath: "custom_request_parameters"},
		{name: "boolean root", raw: `true`, wantReason: CustomRequestParametersReasonNotObject, wantPath: "custom_request_parameters"},
		{name: "malformed json", raw: `{"a": }`, wantReason: CustomRequestParametersReasonNotObject, wantPath: "custom_request_parameters.a"},
		{name: "trailing garbage", raw: `{"a":1} x`, wantReason: CustomRequestParametersReasonNotObject, wantPath: "custom_request_parameters"},
		{name: "duplicate key top level", raw: `{"a":1,"a":2}`, wantReason: CustomRequestParametersReasonDuplicateKey, wantPath: "custom_request_parameters.a"},
		{name: "duplicate key nested", raw: `{"provider":{"only":["a"],"only":["b"]}}`, wantReason: CustomRequestParametersReasonDuplicateKey, wantPath: "custom_request_parameters.provider.only"},
		{name: "blank key", raw: `{"":1}`, wantReason: CustomRequestParametersReasonBlankKey, wantPath: "custom_request_parameters"},
		{name: "whitespace key", raw: `{"  ":1}`, wantReason: CustomRequestParametersReasonBlankKey, wantPath: "custom_request_parameters"},
		{name: "protected model", raw: `{"model":"x"}`, wantReason: CustomRequestParametersReasonProtectedField, wantPath: "custom_request_parameters.model"},
		{name: "protected models", raw: `{"models":["a"]}`, wantReason: CustomRequestParametersReasonProtectedField, wantPath: "custom_request_parameters.models"},
		{name: "protected stream", raw: `{"stream":true}`, wantReason: CustomRequestParametersReasonProtectedField, wantPath: "custom_request_parameters.stream"},
		{name: "protected messages", raw: `{"messages":[]}`, wantReason: CustomRequestParametersReasonProtectedField, wantPath: "custom_request_parameters.messages"},
		{name: "protected input", raw: `{"input":"x"}`, wantReason: CustomRequestParametersReasonProtectedField, wantPath: "custom_request_parameters.input"},
		{name: "protected contents", raw: `{"contents":[]}`, wantReason: CustomRequestParametersReasonProtectedField, wantPath: "custom_request_parameters.contents"},
		{name: "protected instructions", raw: `{"instructions":"x"}`, wantReason: CustomRequestParametersReasonProtectedField, wantPath: "custom_request_parameters.instructions"},
		{name: "protected system", raw: `{"system":"x"}`, wantReason: CustomRequestParametersReasonProtectedField, wantPath: "custom_request_parameters.system"},
		{name: "protected systemInstruction", raw: `{"systemInstruction":"x"}`, wantReason: CustomRequestParametersReasonProtectedField, wantPath: "custom_request_parameters.systemInstruction"},
		{name: "protected nested allowed", raw: `{"provider":{"model":"fine"}}`, wantReason: "", wantPath: ""},
		{name: "case sensitive protected", raw: `{"Model":"x"}`, wantReason: "", wantPath: ""},
		{name: "int below safe range", raw: `{"n":-9007199254740992}`, wantReason: CustomRequestParametersReasonNumberOutOfRange, wantPath: "custom_request_parameters.n"},
		{name: "int above safe range", raw: `{"n":9007199254740992}`, wantReason: CustomRequestParametersReasonNumberOutOfRange, wantPath: "custom_request_parameters.n"},
		{name: "huge integer", raw: `{"n":123456789012345678901234567890}`, wantReason: CustomRequestParametersReasonNumberOutOfRange, wantPath: "custom_request_parameters.n"},
		{name: "infinite exponent", raw: `{"n":1e999}`, wantReason: CustomRequestParametersReasonNumberOutOfRange, wantPath: "custom_request_parameters.n"},
		{name: "negative infinite exponent", raw: `{"n":-1e999}`, wantReason: CustomRequestParametersReasonNumberOutOfRange, wantPath: "custom_request_parameters.n"},
		{name: "min safe integer accepted", raw: `{"n":-9007199254740991}`, wantReason: "", wantPath: ""},
		{name: "max safe integer accepted", raw: `{"n":9007199254740991}`, wantReason: "", wantPath: ""},
		{name: "decimal accepted", raw: `{"n":1.5}`, wantReason: "", wantPath: ""},
		{name: "decimal exponent accepted", raw: `{"n":1.5e-3}`, wantReason: "", wantPath: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value, validationErr := ParseCustomRequestParametersJSON([]byte(test.raw))
			if test.wantReason == "" {
				if validationErr != nil {
					t.Fatalf("expected valid parse for %q, got %v", test.raw, validationErr)
				}
				if value.IsEmpty() {
					t.Fatalf("expected non-empty value for %q", test.raw)
				}
				return
			}
			if validationErr == nil {
				t.Fatalf("expected validation error for %q, got valid value %s", test.raw, value.RawObject())
			}
			if validationErr.Reason != test.wantReason {
				t.Fatalf("reason = %q, want %q (raw %q)", validationErr.Reason, test.wantReason, test.raw)
			}
			if validationErr.Path != test.wantPath {
				t.Fatalf("path = %q, want %q (raw %q)", validationErr.Path, test.wantPath, test.raw)
			}
			if test.wantLimit > 0 && validationErr.Limit != test.wantLimit {
				t.Fatalf("limit = %d, want %d", validationErr.Limit, test.wantLimit)
			}
		})
	}
}

func TestParseCustomRequestParametersLimits(t *testing.T) {
	t.Run("too large compact encoding", func(t *testing.T) {
		raw := `{"k":"` + strings.Repeat("x", CustomRequestParametersMaxCompactBytes) + `"}`
		_, validationErr := ParseCustomRequestParametersJSON([]byte(raw))
		if validationErr == nil || validationErr.Reason != CustomRequestParametersReasonTooLarge {
			t.Fatalf("expected too_large, got %v", validationErr)
		}
		if validationErr.Limit != CustomRequestParametersMaxCompactBytes {
			t.Fatalf("limit = %d, want %d", validationErr.Limit, CustomRequestParametersMaxCompactBytes)
		}
	})
	t.Run("size boundary accepted", func(t *testing.T) {
		raw := `{"k":"` + strings.Repeat("x", CustomRequestParametersMaxCompactBytes-10) + `"}`
		value, validationErr := ParseCustomRequestParametersJSON([]byte(raw))
		if validationErr != nil {
			t.Fatalf("expected size boundary parse to succeed, got %v", validationErr)
		}
		if value.IsEmpty() {
			t.Fatalf("expected non-empty value")
		}
	})
	t.Run("too deep", func(t *testing.T) {
		raw := `{"a":` + strings.Repeat(`{"b":`, 16) + `1` + strings.Repeat(`}`, 16) + `}`
		_, validationErr := ParseCustomRequestParametersJSON([]byte(raw))
		if validationErr == nil || validationErr.Reason != CustomRequestParametersReasonTooDeep {
			t.Fatalf("expected too_deep, got %v", validationErr)
		}
		if validationErr.Limit != CustomRequestParametersMaxDepth {
			t.Fatalf("limit = %d, want %d", validationErr.Limit, CustomRequestParametersMaxDepth)
		}
	})
	t.Run("depth boundary accepted", func(t *testing.T) {
		raw := `{"a":` + strings.Repeat(`{"b":`, 15) + `1` + strings.Repeat(`}`, 15) + `}`
		if _, validationErr := ParseCustomRequestParametersJSON([]byte(raw)); validationErr != nil {
			t.Fatalf("expected depth boundary parse to succeed, got %v", validationErr)
		}
	})
	t.Run("too many members", func(t *testing.T) {
		parts := make([]string, 0, CustomRequestParametersMaxMembers+1)
		for index := 0; index <= CustomRequestParametersMaxMembers; index++ {
			parts = append(parts, fmt.Sprintf("\"k%d\":1", index))
		}
		raw := "{" + strings.Join(parts, ",") + "}"
		_, validationErr := ParseCustomRequestParametersJSON([]byte(raw))
		if validationErr == nil || validationErr.Reason != CustomRequestParametersReasonTooManyMembers {
			t.Fatalf("expected too_many_members, got %v", validationErr)
		}
		if validationErr.Limit != CustomRequestParametersMaxMembers {
			t.Fatalf("limit = %d, want %d", validationErr.Limit, CustomRequestParametersMaxMembers)
		}
	})
	t.Run("member boundary accepted", func(t *testing.T) {
		parts := make([]string, 0, CustomRequestParametersMaxMembers)
		for index := 0; index < CustomRequestParametersMaxMembers; index++ {
			parts = append(parts, fmt.Sprintf("\"k%d\":1", index))
		}
		raw := "{" + strings.Join(parts, ",") + "}"
		if _, validationErr := ParseCustomRequestParametersJSON([]byte(raw)); validationErr != nil {
			t.Fatalf("expected member boundary parse to succeed, got %v", validationErr)
		}
	})
	t.Run("array elements not counted as members", func(t *testing.T) {
		raw := `{"a":[1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25,26,27,28,29,30,31,32,33,34,35,36,37,38,39,40,41,42,43,44,45,46,47,48,49,50,51,52,53,54,55,56,57,58,59,60,61,62,63,64,65,66,67,68,69,70,71,72,73,74,75,76,77,78,79,80,81,82,83,84,85,86,87,88,89,90,91,92,93,94,95,96,97,98,99,100]}`
		if _, validationErr := ParseCustomRequestParametersJSON([]byte(raw)); validationErr != nil {
			t.Fatalf("expected array elements not to count as members, got %v", validationErr)
		}
	})
}

func TestCustomRequestParametersOverlay(t *testing.T) {
	config, validationErr := ParseCustomRequestParametersJSON([]byte(`{"provider":{"only":["deepinfra/turbo"],"allow_fallbacks":false},"temperature":null,"metadata":{"x":1}}`))
	if validationErr != nil {
		t.Fatalf("parse config: %v", validationErr)
	}

	base := []byte(`{"model":"configured-by-prism","provider":{"sort":"price","ignore":["example"]},"temperature":0.7,"messages":[{"role":"user","content":"hi"}]}`)
	merged, err := config.OverlayRequestBody(base)
	if err != nil {
		t.Fatalf("overlay: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(merged, &payload); err != nil {
		t.Fatalf("merged body is not valid JSON: %v", err)
	}
	if payload["model"] != "configured-by-prism" {
		t.Fatalf("model must be preserved, got %v", payload["model"])
	}
	provider, ok := payload["provider"].(map[string]any)
	if !ok {
		t.Fatalf("provider must be a nested object, got %T", payload["provider"])
	}
	if _, hasSort := provider["sort"]; hasSort {
		t.Fatalf("client provider.sort must not survive shallow replacement, got %v", provider)
	}
	if provider["only"] == nil || provider["allow_fallbacks"] != false {
		t.Fatalf("connection provider object must fully replace client value, got %v", provider)
	}
	if value, present := payload["temperature"]; !present || value != nil {
		t.Fatalf("configured null must be sent as literal null, got %v present=%v", value, present)
	}
	messages, ok := payload["messages"].([]any)
	if !ok || len(messages) != 1 {
		t.Fatalf("non-conflicting client field must be preserved, got %v", payload["messages"])
	}
}

func TestCustomRequestParametersOverlayDeterministicAndImmutable(t *testing.T) {
	config, validationErr := ParseCustomRequestParametersJSON([]byte(`{"z":1,"a":{"nested":[true,null,2.5]},"m":"v"}`))
	if validationErr != nil {
		t.Fatalf("parse config: %v", validationErr)
	}
	base := []byte(`{"keep":"client","z":9}`)
	first, err := config.OverlayRequestBody(base)
	if err != nil {
		t.Fatalf("overlay: %v", err)
	}
	for iteration := 0; iteration < 10; iteration++ {
		merged, overlayErr := config.OverlayRequestBody(base)
		if overlayErr != nil {
			t.Fatalf("overlay iteration %d: %v", iteration, overlayErr)
		}
		if string(merged) != string(first) {
			t.Fatalf("overlay output not deterministic: %s vs %s", merged, first)
		}
	}
	// The base and config must not be mutated by repeated overlays.
	if string(base) != `{"keep":"client","z":9}` {
		t.Fatalf("base body was mutated: %s", base)
	}
	if string(config.RawObject()) != `{"a":{"nested":[true,null,2.5]},"m":"v","z":1}` {
		t.Fatalf("config was mutated: %s", config.RawObject())
	}
}

func TestCustomRequestParametersOverlayErrors(t *testing.T) {
	config, validationErr := ParseCustomRequestParametersJSON([]byte(`{"provider":{"only":["x"]}}`))
	if validationErr != nil {
		t.Fatalf("parse config: %v", validationErr)
	}
	if _, err := config.OverlayRequestBody([]byte(`[1,2,3]`)); err == nil {
		t.Fatalf("expected overlay on array base to fail")
	}
	if _, err := config.OverlayRequestBody([]byte(`"string"`)); err == nil {
		t.Fatalf("expected overlay on string base to fail")
	}
	if _, err := config.OverlayRequestBody([]byte(`null`)); err != nil {
		t.Fatalf("expected overlay on null base to be allowed (empty object), got %v", err)
	}
	empty, _ := ParseCustomRequestParametersJSON([]byte(`{}`))
	passthrough, err := empty.OverlayRequestBody([]byte(`{"a":1}`))
	if err != nil || string(passthrough) != `{"a":1}` {
		t.Fatalf("empty config must return the base body unchanged, got %s err %v", passthrough, err)
	}
}

func TestCustomRequestParametersClone(t *testing.T) {
	config, validationErr := ParseCustomRequestParametersJSON([]byte(`{"a":{"b":[1,2,3]},"c":"x"}`))
	if validationErr != nil {
		t.Fatalf("parse config: %v", validationErr)
	}
	cloned := config.Clone()
	if cloned == config {
		t.Fatalf("clone must be a distinct value")
	}
	if string(cloned.RawObject()) != string(config.RawObject()) {
		t.Fatalf("clone must preserve the encoded value, got %s", cloned.RawObject())
	}
	// Mutating the clone's raw object must not affect the source.
	cloneRaw := cloned.RawObject()
	cloneRaw[0] = 'X'
	if string(config.RawObject())[0] == 'X' {
		t.Fatalf("clone shares storage with source")
	}
}

func TestCustomRequestParametersMarshalJSON(t *testing.T) {
	if raw, err := json.Marshal((*CustomRequestParameters)(nil)); err != nil || string(raw) != "null" {
		t.Fatalf("nil marshal = %s, %v", raw, err)
	}
	empty, _ := ParseCustomRequestParametersJSON([]byte(`{}`))
	if raw, err := json.Marshal(empty); err != nil || string(raw) != "null" {
		t.Fatalf("empty marshal = %s, %v", raw, err)
	}
	value, _ := ParseCustomRequestParametersJSON([]byte(`{"provider":{"only":["deepinfra/turbo"]}}`))
	raw, err := json.Marshal(value)
	if err != nil || string(raw) != `{"provider":{"only":["deepinfra/turbo"]}}` {
		t.Fatalf("value marshal = %s, %v", raw, err)
	}
}
