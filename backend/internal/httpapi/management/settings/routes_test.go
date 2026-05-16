package settings

import "testing"

func TestRetentionDaysForTableLoadbalanceEvents(t *testing.T) {
	value := 8
	settingsRow := logRetentionSettingsRow{LoadbalanceEventsRetentionDays: &value}

	got := retentionDaysForTable(settingsRow, "loadbalance_events")
	if got == nil || *got != 8 {
		t.Fatalf("expected loadbalance events retention days to resolve to 8, got %+v", got)
	}
}
