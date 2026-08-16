package contracttest

import (
	"net/http"
	"testing"
)

func asMap(t *testing.T, value any) map[string]any {
	t.Helper()
	typed, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", value)
	}
	return typed
}

func jsonInt(t *testing.T, value any) int {
	t.Helper()
	floatValue, ok := value.(float64)
	if !ok {
		t.Fatalf("expected JSON number, got %T", value)
	}
	return int(floatValue)
}

func jsonStringList(t *testing.T, value any) []string {
	t.Helper()
	raw, ok := value.([]any)
	if !ok {
		t.Fatalf("expected JSON array, got %T", value)
	}
	items := make([]string, 0, len(raw))
	for _, item := range raw {
		text, ok := item.(string)
		if !ok {
			t.Fatalf("expected JSON string element, got %T", item)
		}
		items = append(items, text)
	}
	return items
}

func decodeErrorResponseMap(t *testing.T, response *http.Response) map[string]any {
	t.Helper()
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	return payload
}
