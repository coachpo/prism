package models

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/coachpo/prism/backend/internal/domain/pidev"
)

func TestPiCompatDecodingPreservesNumberLiterals(t *testing.T) {
	const raw = `{"chatTemplateArgs":{"large":9007199254740993,"ratio":0.1234567890123456789}}`
	want := map[string]string{
		"large": "9007199254740993",
		"ratio": "0.1234567890123456789",
	}

	assertNumbers := func(t *testing.T, decoded map[string]any) {
		t.Helper()
		args, ok := decoded["chatTemplateArgs"].(map[string]any)
		if !ok {
			t.Fatalf("chatTemplateArgs = %#v, want object", decoded["chatTemplateArgs"])
		}
		for key, literal := range want {
			number, ok := args[key].(json.Number)
			if !ok || number.String() != literal {
				t.Fatalf("chatTemplateArgs.%s = %#v, want json.Number(%q)", key, args[key], literal)
			}
		}
	}

	stored, err := decodeCompatColumn(raw)
	if err != nil {
		t.Fatalf("decode persisted compat: %v", err)
	}
	assertNumbers(t, stored)

	overrideValue, err := parsePiOverrideValue(
		pidev.APIOpenAICompletions,
		"compat",
		json.RawMessage(raw),
	)
	if err != nil {
		t.Fatalf("decode compat override: %v", err)
	}
	override, ok := overrideValue.(map[string]any)
	if !ok {
		t.Fatalf("override = %#v, want object", overrideValue)
	}
	assertNumbers(t, override)
}

func TestNextPiBindingUpdatedAtUsesPostgresPrecision(t *testing.T) {
	previous := time.Date(2026, time.August, 29, 12, 0, 0, 123456000, time.UTC)
	tests := []struct {
		name     string
		previous time.Time
		proposed time.Time
		want     time.Time
	}{
		{
			name:     "later nanosecond in same stored microsecond advances token",
			previous: previous,
			proposed: previous.Add(500 * time.Nanosecond),
			want:     previous.Add(time.Microsecond),
		},
		{
			name:     "later stored microsecond is normalized",
			previous: previous,
			proposed: previous.Add(1500 * time.Nanosecond),
			want:     previous.Add(time.Microsecond),
		},
		{
			name:     "older clock still advances token",
			previous: previous,
			proposed: previous.Add(-time.Second),
			want:     previous.Add(time.Microsecond),
		},
		{
			name:     "new row stores normalized proposal",
			proposed: previous.Add(999 * time.Nanosecond),
			want:     previous,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := nextPiBindingUpdatedAt(test.previous, test.proposed); !got.Equal(test.want) {
				t.Fatalf("nextPiBindingUpdatedAt(%s, %s) = %s, want %s", test.previous, test.proposed, got, test.want)
			}
		})
	}
}
