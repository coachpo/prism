package runtime_test

import (
	"encoding/json"
	"testing"
)

func mustMarshalBenchmarkJSON(tb testing.TB, value any) []byte {
	tb.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		tb.Fatalf("marshal benchmark JSON: %v", err)
	}
	return raw
}
