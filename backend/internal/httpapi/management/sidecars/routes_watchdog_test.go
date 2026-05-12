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
	_, err := service.store.UpsertWatchdogPolicy(t.Context(), SidecarWatchdogPolicyInput{SidecarID: sidecar.ID, Enabled: true, FailureThreshold: 2, FailureWindowSeconds: 120, FallbackCooldownSeconds: 600, QuotaExceededPriority: 2, UsingPriority: 9, ManualOverridePauseSeconds: 300, ProbeBatchSize: 2, ProbeTimeoutSeconds: 10, ProbeBatchCooldownSeconds: intPtr(45), QuotaInventoryEnabled: boolPtr(false), InitialScanEnabled: boolPtr(false), RollingRefreshEnabled: boolPtr(false), RollingRefreshAfterSeconds: intPtr(7200)})
	if err != nil {
		t.Fatalf("seed watchdog policy: %v", err)
	}
	hiddenBatchAuthID := "hidden-batch-auth"
	_, err = service.store.PersistQuotaProbeDecision(t.Context(), SidecarQuotaPersistDecision{SidecarID: sidecar.ID, Observations: []SidecarWatchdogProbeObservationInput{testProbeObservationInput(sidecar.ID, hiddenBatchAuthID, now)}})
	if err != nil {
		t.Fatalf("seed hidden batch completion: %v", err)
	}

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, watchdogPolicyRoutePath(sidecar.ID), nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("get policy status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if body := recorder.Body.String(); strings.Contains(body, "probe_last_batch_completed_at") || strings.Contains(body, hiddenBatchAuthID) {
		t.Fatalf("policy response leaked hidden fields: %s", body)
	}
	var response watchdogPolicyResponse
	decodeWatchdogRouteResponse(t, recorder, &response)
	if !response.Enabled || response.QuotaExceededPriority != 2 || response.UsingPriority != 9 || response.ErrorPriority != DefaultErrorPriority || response.ProbeBatchSize != 2 || response.ProbeTimeoutSeconds != 10 || response.ProbeBatchCooldownSeconds != 45 || response.QuotaInventoryEnabled || response.InitialScanEnabled || response.RollingRefreshEnabled || response.RollingRefreshAfterSeconds != 7200 {
		t.Fatalf("unexpected policy response: %+v", response)
	}

	patch := `{"quota_exceeded_priority":3,"using_priority":8,"error_priority":3,"probe_batch_size":4,"probe_timeout_seconds":6,"probe_batch_cooldown_seconds":60,"probe_jitter_min_ms":25,"probe_jitter_max_ms":50,"cooldown_jitter_percent":10,"quota_inventory_enabled":true,"initial_scan_enabled":true,"rolling_refresh_enabled":true,"rolling_refresh_after_seconds":5400}`
	patchRecorder := httptest.NewRecorder()
	router.ServeHTTP(patchRecorder, httptest.NewRequest(http.MethodPatch, watchdogPolicyRoutePath(sidecar.ID), strings.NewReader(patch)))
	if patchRecorder.Code != http.StatusOK {
		t.Fatalf("patch policy status = %d body=%s", patchRecorder.Code, patchRecorder.Body.String())
	}
	var updated watchdogPolicyResponse
	decodeWatchdogRouteResponse(t, patchRecorder, &updated)
	if updated.QuotaExceededPriority != 3 || updated.UsingPriority != 8 || updated.ErrorPriority != 3 || updated.ProbeBatchSize != 4 || updated.ProbeTimeoutSeconds != 6 || updated.ProbeBatchCooldownSeconds != 60 || updated.ProbeJitterMinMS != 25 || updated.ProbeJitterMaxMS != 50 || updated.CooldownJitterPercent != 10 || !updated.QuotaInventoryEnabled || !updated.InitialScanEnabled || !updated.RollingRefreshEnabled || updated.RollingRefreshAfterSeconds != 5400 {
		t.Fatalf("unexpected patched policy: %+v", updated)
	}
}

func TestQuotaRouteResponsesHideInternalFields(t *testing.T) {
	now := time.Date(2026, time.May, 11, 19, 0, 0, 0, time.UTC)
	service, router, sidecar := newWatchdogRouteTest(t, now)
	authIndex := "private-route-auth-index"
	_, err := service.store.SaveAuthSnapshot(t.Context(), SidecarAuthSnapshotInput{SidecarID: sidecar.ID, AuthID: "auth-route-hidden", AuthIndex: &authIndex, Name: "route-hidden.json", Provider: stringPtr("codex"), Priority: intPtr(9), SnapshotJSON: json.RawMessage(`{"id":"auth-route-hidden"}`), ObservedAt: now})
	if err != nil {
		t.Fatalf("seed auth snapshot: %v", err)
	}
	stateStore, ok := service.store.(authQuotaStateStore)
	if !ok {
		t.Fatalf("store does not support quota states")
	}
	_, err = stateStore.UpsertAuthQuotaState(t.Context(), SidecarAuthQuotaStateInput{SidecarID: sidecar.ID, AuthID: "auth-route-hidden", AuthIndex: &authIndex, AuthName: stringPtr("route-hidden.json"), Provider: stringPtr("codex"), SnapshotObservedAt: &now, QuotaBand: "quota_exceeded", ProbeStatus: stringPtr(watchdogProbeStatusSucceeded), LastProbedAt: &now})
	if err != nil {
		t.Fatalf("seed quota state: %v", err)
	}
	privateScanCursor := "private-scan-cursor"
	_, err = service.store.CreateQuotaScanRun(t.Context(), SidecarQuotaScanRunInput{SidecarID: sidecar.ID, ScanType: quotaScanTypeManual, Status: quotaScanStatusRunning, CursorAuthID: &privateScanCursor, PlannedCount: 2, AttemptedCount: 1, StartedAt: &now})
	if err != nil {
		t.Fatalf("seed quota scan: %v", err)
	}

	stateRecorder := httptest.NewRecorder()
	router.ServeHTTP(stateRecorder, httptest.NewRequest(http.MethodGet, "/sidecars/"+strconv.Itoa(sidecar.ID)+"/quota-states", nil))
	if stateRecorder.Code != http.StatusOK {
		t.Fatalf("quota states status = %d body=%s", stateRecorder.Code, stateRecorder.Body.String())
	}
	if body := stateRecorder.Body.String(); strings.Contains(body, `"auth_index":`) || strings.Contains(body, authIndex) || !strings.Contains(body, `"auth_index_present":true`) {
		t.Fatalf("quota state response leaked internal auth index or missed presence flag: %s", body)
	}

	for _, path := range []string{"/sidecars/" + strconv.Itoa(sidecar.ID) + "/quota-scans/current", "/sidecars/" + strconv.Itoa(sidecar.ID) + "/quota-scans"} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("quota scan route %s status = %d body=%s", path, recorder.Code, recorder.Body.String())
		}
		if body := recorder.Body.String(); strings.Contains(body, "cursor_auth_id") || strings.Contains(body, privateScanCursor) {
			t.Fatalf("quota scan response leaked private cursor: %s", body)
		}
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
		{name: "negative quota exceeded priority", body: `{"quota_exceeded_priority":-1}`, wantDetail: "quota_exceeded_priority"},
		{name: "negative using priority", body: `{"using_priority":-1}`, wantDetail: "using_priority"},
		{name: "quota exceeded is above using", body: `{"quota_exceeded_priority":4,"using_priority":3}`, wantDetail: "quota_exceeded_priority must be \\u003c= using_priority"},
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

func TestWatchdogPolicyRejectsInvalidCooldown(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantDetail string
	}{
		{name: "probe batch cooldown", body: `{"probe_batch_cooldown_seconds":0}`, wantDetail: "probe_batch_cooldown_seconds"},
		{name: "rolling refresh after", body: `{"rolling_refresh_after_seconds":0}`, wantDetail: "rolling_refresh_after_seconds"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Date(2026, time.May, 11, 18, 45, 0, 0, time.UTC)
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

func TestQuotaScanRoutesReturnExpectedStatusCodes(t *testing.T) {
	now := time.Date(2026, time.May, 11, 19, 30, 0, 0, time.UTC)
	service, router, sidecar := newWatchdogRouteTest(t, now)
	_, err := service.store.UpsertWatchdogPolicy(t.Context(), SidecarWatchdogPolicyInput{SidecarID: sidecar.ID, Enabled: true, FailureThreshold: DefaultFailureThreshold, FailureWindowSeconds: DefaultFailureWindowSeconds, FallbackCooldownSeconds: DefaultFallbackCooldownSeconds, QuotaExceededPriority: DefaultQuotaExceededPriority, UsingPriority: DefaultUsingPriority, ManualOverridePauseSeconds: DefaultManualOverridePauseSeconds, ProbeBatchSize: DefaultProbeBatchSize, ProbeTimeoutSeconds: DefaultProbeTimeoutSeconds, ProbeBatchCooldownSeconds: intPtr(DefaultProbeBatchCooldownSeconds), QuotaInventoryEnabled: boolPtr(true), InitialScanEnabled: boolPtr(false), RollingRefreshEnabled: boolPtr(false), RollingRefreshAfterSeconds: intPtr(DefaultRollingRefreshAfterSeconds)})
	if err != nil {
		t.Fatalf("seed watchdog policy: %v", err)
	}

	current := httptest.NewRecorder()
	router.ServeHTTP(current, httptest.NewRequest(http.MethodGet, "/sidecars/"+strconv.Itoa(sidecar.ID)+"/quota-scans/current", nil))
	if current.Code != http.StatusNoContent {
		t.Fatalf("expected inactive current scan to return %d, got %d body=%s", http.StatusNoContent, current.Code, current.Body.String())
	}
	if body := current.Body.String(); body != "" {
		t.Fatalf("expected inactive current scan response body to be empty, got %q", body)
	}

	run, err := service.store.CreateQuotaScanRun(t.Context(), SidecarQuotaScanRunInput{SidecarID: sidecar.ID, ScanType: quotaScanTypeManual, Status: quotaScanStatusRunning, PlannedCount: 1, AttemptedCount: 1, StartedAt: &now})
	if err != nil {
		t.Fatalf("seed active quota scan: %v", err)
	}
	cancel := httptest.NewRecorder()
	router.ServeHTTP(cancel, httptest.NewRequest(http.MethodPost, "/sidecars/"+strconv.Itoa(sidecar.ID)+"/quota-scans/"+strconv.Itoa(run.ID)+"/cancel", nil))
	if cancel.Code != http.StatusAccepted {
		t.Fatalf("expected cancel to return %d, got %d body=%s", http.StatusAccepted, cancel.Code, cancel.Body.String())
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
