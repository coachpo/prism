package contract_test

import "testing"

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
