package runtime

import (
	"testing"
	"time"

	"github.com/coachpo/prism/backend/internal/platform/alerting"
)

func TestRuntimeFeedbackIncidentAlertPayloadsOnlyAlertableEvents(t *testing.T) {
	occurredAt := time.Date(2026, time.June, 7, 15, 30, 0, 0, time.UTC)
	bannedUntil := occurredAt.Add(15 * time.Minute)
	event := runtimeFeedbackEvent{
		ProfileID:    1,
		ConnectionID: 3,
		EndpointID:   4,
		ModelID:      "gpt-alert",
		ObservedAt:   occurredAt,
		CompletedAt:  occurredAt,
	}

	payload, ok := runtimeIncidentAlertPayload(event, "banned", &bannedUntil)
	if !ok {
		t.Fatal("expected banned event to produce alert payload")
	}
	assertIncidentPayload(t, payload, "banned", 3, 4, "gpt-alert", occurredAt, &bannedUntil)

	payload, ok = runtimeIncidentAlertPayload(event, "unbanned", nil)
	if !ok {
		t.Fatal("expected unbanned event to produce alert payload")
	}
	assertIncidentPayload(t, payload, "unbanned", 3, 4, "gpt-alert", occurredAt, nil)

	if _, ok := runtimeIncidentAlertPayload(event, "retry_scheduled", nil); ok {
		t.Fatal("expected retry_scheduled to skip alert payload")
	}
}

func assertIncidentPayload(t *testing.T, payload alerting.IncidentPayload, eventType string, connectionID int, endpointID int, modelID string, occurredAt time.Time, bannedUntil *time.Time) {
	t.Helper()
	if payload.EventType != eventType || payload.ConnectionID != connectionID || payload.EndpointID != endpointID || payload.ModelID != modelID || !payload.OccurredAt.Equal(occurredAt) {
		t.Fatalf("unexpected incident payload: %+v", payload)
	}
	if bannedUntil == nil {
		if payload.BannedUntilAt != nil {
			t.Fatalf("expected nil banned_until_at, got %+v", payload.BannedUntilAt)
		}
		return
	}
	if payload.BannedUntilAt == nil || !payload.BannedUntilAt.Equal(*bannedUntil) {
		t.Fatalf("unexpected banned_until_at: %+v", payload.BannedUntilAt)
	}
}
