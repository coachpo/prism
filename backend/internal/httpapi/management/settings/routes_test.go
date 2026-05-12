package settings

import "testing"

func TestRetentionDaysForTableSidecarWatchdogActions(t *testing.T) {
	value := 8
	settingsRow := logRetentionSettingsRow{SidecarActionHistoryRetentionDays: &value}

	got := retentionDaysForTable(settingsRow, "sidecar_watchdog_actions")
	if got == nil || *got != 8 {
		t.Fatalf("expected sidecar watchdog actions retention days to resolve to 8, got %+v", got)
	}
}
