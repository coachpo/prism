package sidecars

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

func TestWatchdogPolicyProbeResponse(t *testing.T) {
	now := time.Date(2026, time.May, 11, 18, 0, 0, 0, time.UTC)
	service, router, sidecar := newWatchdogRouteTest(t, now)
	_, err := service.store.UpsertWatchdogPolicy(t.Context(), SidecarWatchdogPolicyInput{SidecarID: sidecar.ID, Enabled: true, FailureThreshold: 2, FailureWindowSeconds: 120, FallbackCooldownSeconds: 600, DeprioritizedPriority: 2, PrioritizedPriority: 9, ManualOverridePauseSeconds: 300, ProbeBatchSize: 2, ProbeTimeoutSeconds: 10})
	if err != nil {
		t.Fatalf("seed watchdog policy: %v", err)
	}
	cursorAuthID := "hidden-cursor-auth"
	_, err = service.store.PersistWatchdogProbeDecision(t.Context(), SidecarWatchdogProbeDecision{SidecarID: sidecar.ID, AdvanceProbeCursor: true, ProbeCursorAuthID: &cursorAuthID})
	if err != nil {
		t.Fatalf("seed hidden probe cursor: %v", err)
	}

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, watchdogPolicyRoutePath(sidecar.ID), nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("get policy status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if body := recorder.Body.String(); strings.Contains(body, "probe_cursor_auth_id") || strings.Contains(body, cursorAuthID) {
		t.Fatalf("policy response leaked hidden cursor: %s", body)
	}
	var response watchdogPolicyResponse
	decodeWatchdogRouteResponse(t, recorder, &response)
	if !response.Enabled || response.DeprioritizedPriority != 2 || response.PrioritizedPriority != 9 || response.ProbeBatchSize != 2 || response.ProbeTimeoutSeconds != 10 {
		t.Fatalf("unexpected policy response: %+v", response)
	}

	patch := `{"deprioritized_priority":3,"prioritized_priority":8,"probe_batch_size":4,"probe_timeout_seconds":6}`
	patchRecorder := httptest.NewRecorder()
	router.ServeHTTP(patchRecorder, httptest.NewRequest(http.MethodPatch, watchdogPolicyRoutePath(sidecar.ID), strings.NewReader(patch)))
	if patchRecorder.Code != http.StatusOK {
		t.Fatalf("patch policy status = %d body=%s", patchRecorder.Code, patchRecorder.Body.String())
	}
	var updated watchdogPolicyResponse
	decodeWatchdogRouteResponse(t, patchRecorder, &updated)
	if updated.DeprioritizedPriority != 3 || updated.PrioritizedPriority != 8 || updated.ProbeBatchSize != 4 || updated.ProbeTimeoutSeconds != 6 {
		t.Fatalf("unexpected patched policy: %+v", updated)
	}
	stored, err := service.store.GetOrCreateWatchdogPolicy(t.Context(), sidecar.ID)
	if err != nil || stored.ProbeCursorAuthID == nil || *stored.ProbeCursorAuthID != cursorAuthID {
		t.Fatalf("hidden cursor should remain internal and preserved, policy=%+v err=%v", stored, err)
	}
}

func TestWatchdogPolicyValidationRejectsProbeBudgetAndPriorities(t *testing.T) {
	maxBudget := watchdogProbeBudgetMaxSeconds()
	oversizedTimeout := strconv.Itoa(maxBudget + 1)
	oversizedBatchTimeout := strconv.Itoa(maxBudget/2 + 1)
	tests := []struct {
		name       string
		body       string
		wantDetail string
	}{
		{name: "negative deprioritized priority", body: `{"deprioritized_priority":-1}`, wantDetail: "deprioritized_priority"},
		{name: "negative prioritized priority", body: `{"prioritized_priority":-1}`, wantDetail: "prioritized_priority"},
		{name: "deprioritized is not below prioritized", body: `{"deprioritized_priority":3,"prioritized_priority":3}`, wantDetail: "deprioritized_priority must be less than prioritized_priority"},
		{name: "zero probe batch", body: `{"probe_batch_size":0}`, wantDetail: "probe_batch_size"},
		{name: "zero probe timeout", body: `{"probe_timeout_seconds":0}`, wantDetail: "probe_timeout_seconds"},
		{name: "timeout exceeds worker budget", body: `{"probe_timeout_seconds":` + oversizedTimeout + `}`, wantDetail: "worker budget"},
		{name: "batch timeout exceeds worker budget", body: `{"probe_batch_size":2,"probe_timeout_seconds":` + oversizedBatchTimeout + `}`, wantDetail: "batch budget"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Date(2026, time.May, 11, 18, 15, 0, 0, time.UTC)
			_, router, sidecar := newWatchdogRouteTest(t, now)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPatch, watchdogPolicyRoutePath(sidecar.ID), strings.NewReader(tt.body)))
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("patch policy status = %d body=%s", recorder.Code, recorder.Body.String())
			}
			if !strings.Contains(recorder.Body.String(), tt.wantDetail) {
				t.Fatalf("validation response %q missing %q", recorder.Body.String(), tt.wantDetail)
			}
		})
	}
}

func TestRedactWatchdogActionHistoryProbeUnsupportedAndQuotaHoldResponse(t *testing.T) {
	now := time.Date(2026, time.May, 11, 18, 30, 0, 0, time.UTC)
	service, router, sidecar := newWatchdogRouteTest(t, now)
	rawReason := `{"body":"secret-body","headers":{"Authorization":"Bearer raw-token"},"email":"person@example.com"}`
	rawError := `usage body parse failed near {"account_id":"acct_123","body":"secret-body","token":"raw-token"}`
	createWatchdogRouteAction(t, service, SidecarWatchdogActionInput{SidecarID: sidecar.ID, AuthID: stringPtrFromNonEmpty("auth-probe"), AuthIndex: stringPtrFromNonEmpty("idx-probe"), Provider: stringPtrFromNonEmpty("codex"), ActionType: watchdogProbeStatusFailedParse, Status: watchdogActionStatusFailed, Reason: &rawReason, ErrorMessage: &rawError})
	createWatchdogRouteAction(t, service, SidecarWatchdogActionInput{SidecarID: sidecar.ID, AuthID: stringPtrFromNonEmpty("auth-unsupported"), AuthIndex: stringPtrFromNonEmpty("idx-unsupported"), Provider: stringPtrFromNonEmpty("gemini"), ActionType: watchdogProbeStatusSkippedUnsupportedProvider, Status: watchdogActionStatusSkipped, Reason: stringPtrFromNonEmpty("unsupported provider body should not leak")})
	createWatchdogRouteAction(t, service, SidecarWatchdogActionInput{SidecarID: sidecar.ID, AuthID: stringPtrFromNonEmpty("auth-success"), AuthIndex: stringPtrFromNonEmpty("idx-success"), Provider: stringPtrFromNonEmpty("codex"), ActionType: watchdogProbeStatusSucceeded, Status: watchdogActionStatusSucceeded, Reason: stringPtrFromNonEmpty("success raw body ignored")})
	holdID := 42
	holdUntil := now.Add(5 * time.Hour)
	createWatchdogRouteAction(t, service, SidecarWatchdogActionInput{SidecarID: sidecar.ID, HoldID: &holdID, AuthID: stringPtrFromNonEmpty("auth-held"), AuthIndex: stringPtrFromNonEmpty("idx-held"), Provider: stringPtrFromNonEmpty("codex"), ActionType: watchdogActionRestoreSkippedUnhealthy, Status: watchdogActionStatusSkipped, Reason: stringPtrFromNonEmpty("quota_exceeded:five_hour"), HoldUntil: &holdUntil})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/sidecars/"+strconv.Itoa(sidecar.ID)+"/actions", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("actions status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	for _, forbidden := range []string{"secret-body", "Bearer raw-token", "person@example.com", "acct_123", "raw-token", "\"body\"", "Authorization"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("action history leaked %q in %s", forbidden, body)
		}
	}
	var response actionHistoryListResponse
	decodeWatchdogRouteResponse(t, recorder, &response)
	itemsByType := map[string]actionRecordResponse{}
	for _, item := range response.Items {
		itemsByType[item.ActionType] = item
	}
	failed := itemsByType[watchdogProbeStatusFailedParse]
	if failed.Reason == nil || *failed.Reason != watchdogProbeStatusFailedParse || failed.ErrorMessage == nil || *failed.ErrorMessage != watchdogProbeStatusFailedParse {
		t.Fatalf("probe parse failure should be generic and distinct, got %+v", failed)
	}
	if _, ok := itemsByType[watchdogProbeStatusSkippedUnsupportedProvider]; !ok {
		t.Fatalf("missing unsupported-provider probe action in %+v", response.Items)
	}
	if _, ok := itemsByType[watchdogProbeStatusSucceeded]; !ok {
		t.Fatalf("missing probe success action in %+v", response.Items)
	}
	quota := itemsByType[watchdogActionQuotaHoldExtended]
	if quota.Reason == nil || *quota.Reason != "quota_exceeded:five_hour" || quota.HoldUntil == nil || !quota.HoldUntil.Equal(holdUntil) {
		t.Fatalf("quota hold extension not shaped distinctly: %+v", quota)
	}
}

func newWatchdogRouteTest(t *testing.T, now time.Time) (*Service, http.Handler, SidecarInstance) {
	t.Helper()
	service := newSyncTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, "http://127.0.0.1:18080", true, 900)
	router := chi.NewRouter()
	service.MountManagementRoutes(router)
	return service, router, sidecar
}

func watchdogPolicyRoutePath(sidecarID int) string {
	return "/sidecars/" + strconv.Itoa(sidecarID) + "/watchdog-policy"
}

func decodeWatchdogRouteResponse(t *testing.T, recorder *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.Unmarshal(recorder.Body.Bytes(), target); err != nil {
		t.Fatalf("decode route response: %v body=%s", err, recorder.Body.String())
	}
}

func createWatchdogRouteAction(t *testing.T, service *Service, input SidecarWatchdogActionInput) SidecarWatchdogAction {
	t.Helper()
	action, err := service.store.CreateWatchdogAction(t.Context(), input)
	if err != nil {
		t.Fatalf("create watchdog action: %v", err)
	}
	return action
}
