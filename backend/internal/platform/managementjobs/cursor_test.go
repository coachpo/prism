package managementjobs

import (
	"strings"
	"testing"
	"time"
)

func TestJobsCursorRoundTripAndTamperRejection(t *testing.T) {
	store := &Store{cursorKey: deriveCursorSigningKey("cursor-test-secret", "test")}
	payload := jobsCursorPayload{
		Version:    2,
		Origin:     "automatic",
		States:     []string{"queued", "running"},
		Limit:      25,
		Sort:       jobsCursorSort,
		UpperAt:    "2026-08-11T10:00:00Z",
		UpperID:    "job_upper",
		PositionAt: "2026-08-11T09:00:00Z",
		PositionID: "job_position",
	}
	encoded := store.encodeJobsCursor(payload)
	decoded, ok := store.decodeJobsCursor(encoded)
	if !ok {
		t.Fatal("expected signed cursor to decode")
	}
	if decoded.Origin != payload.Origin || decoded.Limit != payload.Limit || decoded.UpperID != payload.UpperID || decoded.PositionID != payload.PositionID || !sameJobStates(decoded.States, payload.States) {
		t.Fatalf("decoded cursor changed its bound: got %+v", decoded)
	}

	tampered := encoded[:len(encoded)-1] + "A"
	if _, ok := store.decodeJobsCursor(tampered); ok {
		t.Fatal("expected tampered cursor to be rejected")
	}
	otherStore := &Store{cursorKey: deriveCursorSigningKey("different-secret", "test")}
	if _, ok := otherStore.decodeJobsCursor(encoded); ok {
		t.Fatal("expected cursor signed by another store to be rejected")
	}
}

func TestJobsCursorHelperUsesOpaqueSignedFormat(t *testing.T) {
	at := time.Date(2026, 8, 11, 10, 0, 0, 0, time.UTC)
	encoded := EncodeJobsCursor(at, "job_1")
	if strings.Contains(encoded, "job_1") || strings.Contains(encoded, at.Format(time.RFC3339)) {
		t.Fatalf("cursor leaked raw keyset fields: %q", encoded)
	}
	decodedAt, decodedID, ok := DecodeJobsCursor(encoded)
	if !ok || !decodedAt.Equal(at) || decodedID != "job_1" {
		t.Fatalf("helper cursor did not round-trip: %s %s %v", decodedAt, decodedID, ok)
	}
}
