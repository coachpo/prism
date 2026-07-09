package settings

import (
	"strings"
	"testing"
)

func TestRetentionDaysForTableLoadbalanceEvents(t *testing.T) {
	value := 8
	settingsRow := logRetentionSettingsRow{LoadbalanceEventsRetentionDays: &value}

	got := retentionDaysForTable(settingsRow, "loadbalance_events")
	if got == nil || *got != 8 {
		t.Fatalf("expected loadbalance events retention days to resolve to 8, got %+v", got)
	}
}

func TestAuditSettingsDefaultsStableFamilyOrder(t *testing.T) {
	response := buildAuditSettingsResponse(7, []auditSettingsRow{{APIFamily: "gemini", AuditEnabled: true}})

	if response.ProfileID != 7 || len(response.Settings) != 3 {
		t.Fatalf("expected three audit settings for profile 7, got %+v", response)
	}
	wantFamilies := []string{"openai", "anthropic", "gemini"}
	for index, family := range wantFamilies {
		setting := response.Settings[index]
		if setting.APIFamily != family {
			t.Fatalf("expected family order %v, got %+v", wantFamilies, response.Settings)
		}
		if family != "gemini" && (setting.AuditEnabled || setting.AuditCaptureBodies) {
			t.Fatalf("expected missing family %s to default false/false, got %+v", family, setting)
		}
	}
	if !response.Settings[2].AuditEnabled || response.Settings[2].AuditCaptureBodies {
		t.Fatalf("expected existing gemini row to preserve booleans, got %+v", response.Settings[2])
	}
}

func TestAuditSettingsValidationNormalizesAndOrdersFamilies(t *testing.T) {
	request := auditSettingsUpdateRequest{Settings: []auditSetting{
		{APIFamily: " Gemini ", AuditEnabled: true, AuditCaptureBodies: true},
		{APIFamily: "openai", AuditEnabled: false},
		{APIFamily: "ANTHROPIC", AuditEnabled: true},
	}}

	if err := normalizeAndValidateAuditSettingsRequest(&request); err != nil {
		t.Fatalf("expected valid audit settings, got %v", err)
	}
	wantFamilies := []string{"openai", "anthropic", "gemini"}
	for index, family := range wantFamilies {
		if request.Settings[index].APIFamily != family {
			t.Fatalf("expected canonical family order %v, got %+v", wantFamilies, request.Settings)
		}
	}
}

func TestAuditSettingsValidationRejectsInvalidPayloads(t *testing.T) {
	testCases := []struct {
		name    string
		request auditSettingsUpdateRequest
		detail  string
	}{
		{
			name: "unknown family",
			request: auditSettingsUpdateRequest{Settings: []auditSetting{
				{APIFamily: "openai"},
				{APIFamily: "anthropic"},
				{APIFamily: "mistral"},
			}},
			detail: "not supported",
		},
		{
			name: "duplicate family",
			request: auditSettingsUpdateRequest{Settings: []auditSetting{
				{APIFamily: "openai"},
				{APIFamily: "openai"},
				{APIFamily: "gemini"},
			}},
			detail: "Duplicate audit setting for api_family=openai",
		},
		{
			name: "missing family",
			request: auditSettingsUpdateRequest{Settings: []auditSetting{
				{APIFamily: "openai"},
				{APIFamily: "anthropic"},
			}},
			detail: "settings must include exactly openai, anthropic, and gemini",
		},
		{
			name: "capture requires enabled",
			request: auditSettingsUpdateRequest{Settings: []auditSetting{
				{APIFamily: "openai", AuditCaptureBodies: true},
				{APIFamily: "anthropic"},
				{APIFamily: "gemini"},
			}},
			detail: "audit_capture_bodies requires audit_enabled",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			err := normalizeAndValidateAuditSettingsRequest(&testCase.request)
			if err == nil || !strings.Contains(err.Error(), testCase.detail) {
				t.Fatalf("expected error containing %q, got %v", testCase.detail, err)
			}
		})
	}
}
