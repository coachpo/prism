package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	managementsidecars "github.com/coachpo/prism/backend/internal/httpapi/management/sidecars"
	"github.com/coachpo/prism/backend/internal/platform/background"
	"github.com/coachpo/prism/backend/internal/platform/config"
	"github.com/coachpo/prism/backend/internal/platform/logretention"
)

const sidecarIntegrationManagementSecret = "integration-management-secret"

var sidecarIntegrationSecrets = []string{
	sidecarIntegrationManagementSecret,
	"raw-auth-secret",
	"raw-auth-token",
	"raw-provider-secret",
	"raw-provider-token",
	"raw-header-key",
	"raw-openai-secret",
	"raw-openai-token",
	"raw-codex-secret",
	"raw-codex-token",
	"proxy-secret.invalid",
	"mutation-secret",
	"mutation-token",
	"mutation-management-secret",
	"probe-cookie-secret",
	"chatgpt-account-secret",
	"mutation-response-body",
	"mutation-user@example.test",
}

func TestSidecarDBBackedSyncMutationsAndRedaction(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	conn, router := newSidecarIntegrationRouter(t, ctx)
	upstream := newSidecarIntegrationUpstream(t)
	defer upstream.Close()

	createBody, created := sidecarIntegrationRequestJSON(t, router, http.MethodPost, "/sidecars", map[string]any{
		"name":                    "Integration Sidecar",
		"base_url":                upstream.URL(),
		"management_password":     sidecarIntegrationManagementSecret,
		"allow_private_network":   true,
		"allow_insecure_http":     true,
		"sync_interval_seconds":   60,
		"request_timeout_seconds": 5,
	}, http.StatusCreated)
	sidecarIntegrationAssertNoLeaks(t, createBody)
	sidecarID := sidecarIntegrationInt(t, created["id"], "id")
	sidecarIntegrationAssertDBNoLeaks(t, conn, sidecarID)

	syncBody, syncPayload := sidecarIntegrationRequestJSON(t, router, http.MethodPost, "/sidecars/"+strconv.Itoa(sidecarID)+"/sync", nil, http.StatusOK)
	sidecarIntegrationAssertNoLeaks(t, syncBody)
	if sidecarIntegrationInt(t, syncPayload["auth_snapshot_count"], "auth_snapshot_count") != 1 {
		t.Fatalf("expected one auth snapshot after sync, got %+v", syncPayload)
	}
	if sidecarIntegrationInt(t, syncPayload["provider_snapshot_count"], "provider_snapshot_count") != 2 {
		t.Fatalf("expected two provider snapshots after sync, got %+v", syncPayload)
	}
	sidecarIntegrationAssertDBNoLeaks(t, conn, sidecarID)

	for _, path := range []string{
		"/sidecars",
		"/sidecars/" + strconv.Itoa(sidecarID),
		"/sidecars/" + strconv.Itoa(sidecarID) + "/auth-files",
		"/sidecars/" + strconv.Itoa(sidecarID) + "/providers",
		"/sidecars/" + strconv.Itoa(sidecarID) + "/sync-status",
	} {
		body, _ := sidecarIntegrationRequestJSON(t, router, http.MethodGet, path, nil, http.StatusOK)
		sidecarIntegrationAssertNoLeaks(t, body)
	}

	mutationBody, _ := sidecarIntegrationRequestJSON(t, router, http.MethodPatch, "/sidecars/"+strconv.Itoa(sidecarID)+"/auth-files/auth-gemini-primary/fields", map[string]any{
		"priority": 42,
		"headers":  map[string]any{"X-Trace-ID": "trace-123"},
	}, http.StatusOK)
	sidecarIntegrationAssertNoLeaks(t, mutationBody)
	upstream.assertFieldPatch(t, 42)
	assertSidecarIntegrationManualPause(t, conn, sidecarID)

	actionsBody, _ := sidecarIntegrationRequestJSON(t, router, http.MethodGet, "/sidecars/"+strconv.Itoa(sidecarID)+"/actions", nil, http.StatusOK)
	sidecarIntegrationAssertNoLeaks(t, actionsBody)
	if !strings.Contains(actionsBody, "operator_patch") || !strings.Contains(actionsBody, "redacted-by-prism") {
		t.Fatalf("expected redacted operator patch action history, got %s", actionsBody)
	}
	sidecarIntegrationAssertDBNoLeaks(t, conn, sidecarID)
}

func TestSidecarIntegrationProbeLifecycleDeprioritizesRestoresAndExtendsHold(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	now := time.Date(2026, time.May, 11, 10, 0, 0, 0, time.UTC)
	conn, service, router := newSidecarIntegrationServiceRouterWithNow(t, ctx, func() time.Time { return now })
	upstream := newSidecarIntegrationUpstream(t)
	defer upstream.Close()
	upstream.setAuthFiles(sidecarIntegrationCodexAuthFixture("auth-codex-lifecycle", "idx-codex-lifecycle", 10))
	sidecarID := sidecarIntegrationCreateAndSyncSidecar(t, conn, router, upstream, "probe-lifecycle")
	sidecarIntegrationEnableProbePolicy(t, conn, sidecarID, 2, 2)

	resetAt := now.Add(time.Hour)
	upstream.setAPICallResponse("idx-codex-lifecycle", sidecarIntegrationAPICallResponse{StatusCode: http.StatusOK, Body: sidecarIntegrationExhaustedUsageBody(t, resetAt)})
	result, err := service.ReconcileSidecarWatchdog(ctx, sidecarID)
	if err != nil || !result.Reconciled {
		t.Fatalf("expected exhausted probe to deprioritize, result=%+v err=%v", result, err)
	}
	upstream.assertFieldPatchPriorities(t, []int{0})
	upstream.assertAPICalls(t, []string{"idx-codex-lifecycle"})
	sidecarIntegrationSetProbeBatchCompletedAt(t, conn, sidecarID, now)
	sidecarIntegrationAssertActiveHold(t, conn, sidecarID, "auth-codex-lifecycle", 10, 0, resetAt)
	sidecarIntegrationAssertProbeObservation(t, conn, sidecarID, "auth-codex-lifecycle", "probe_succeeded", true, 0)
	sidecarIntegrationAssertPendingActionCount(t, conn, sidecarID, 1)
	sidecarIntegrationAssertDBNoLeaks(t, conn, sidecarID)

	result, err = service.ReconcileSidecarWatchdog(ctx, sidecarID)
	if err != nil || !result.Reconciled || result.Probed != 0 {
		t.Fatalf("expected pending deprioritize repair before due-hold probe, result=%+v err=%v", result, err)
	}
	upstream.assertAPICalls(t, []string{"idx-codex-lifecycle"})
	sidecarIntegrationClearProbeBatchCompletedAt(t, conn, sidecarID)
	sidecarIntegrationAssertPendingActionCount(t, conn, sidecarID, 0)
	sidecarIntegrationAssertDBNoLeaks(t, conn, sidecarID)

	now = resetAt.Add(time.Minute)
	extendedResetAt := now.Add(2 * time.Hour)
	upstream.setAPICallResponse("idx-codex-lifecycle", sidecarIntegrationAPICallResponse{StatusCode: http.StatusOK, Body: sidecarIntegrationExhaustedUsageBody(t, extendedResetAt)})
	result, err = service.ReconcileSidecarWatchdog(ctx, sidecarID)
	if err != nil || result.Reconciled {
		t.Fatalf("expected still-exhausted due hold to extend without priority patch, result=%+v err=%v", result, err)
	}
	upstream.assertFieldPatchPriorities(t, []int{0})
	upstream.assertAPICalls(t, []string{"idx-codex-lifecycle", "idx-codex-lifecycle"})
	sidecarIntegrationAssertActiveHold(t, conn, sidecarID, "auth-codex-lifecycle", 10, 0, extendedResetAt)
	sidecarIntegrationAssertAction(t, conn, sidecarID, "restore_skipped_unhealthy", "skipped")
	sidecarIntegrationAssertDBNoLeaks(t, conn, sidecarID)

	now = extendedResetAt.Add(time.Minute)
	upstream.setAPICallResponse("idx-codex-lifecycle", sidecarIntegrationAPICallResponse{StatusCode: http.StatusOK, Body: sidecarIntegrationHealthyUsageBody()})
	result, err = service.ReconcileSidecarWatchdog(ctx, sidecarID)
	if err != nil || !result.Reconciled {
		t.Fatalf("expected healthy due hold to restore priority, result=%+v err=%v", result, err)
	}
	upstream.assertFieldPatchPriorities(t, []int{0, 10})
	upstream.assertAPICalls(t, []string{"idx-codex-lifecycle", "idx-codex-lifecycle", "idx-codex-lifecycle"})
	sidecarIntegrationAssertReleasedHold(t, conn, sidecarID, "auth-codex-lifecycle")
	sidecarIntegrationAssertAction(t, conn, sidecarID, "restore", "succeeded")
	sidecarIntegrationAssertDBNoLeaks(t, conn, sidecarID)
}

func TestConcurrentLeaseWatchdogIntegrationReconcileSkipsOverlap(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	now := time.Date(2026, time.May, 11, 10, 30, 0, 0, time.UTC)
	conn, pool, service, router := newSidecarIntegrationServiceRouterPoolWithNow(t, ctx, func() time.Time { return now })
	secondService, err := managementsidecars.NewService(config.Settings{SecretEncryptionKey: "sidecar-integration-secret"}, managementsidecars.Options{Pool: pool, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("build second sidecar integration service: %v", err)
	}
	t.Cleanup(secondService.Close)
	upstream := newSidecarIntegrationUpstream(t)
	defer upstream.Close()
	upstream.setAuthFiles(sidecarIntegrationCodexAuthFixture("auth-codex-concurrent", "idx-codex-concurrent", 10))
	resetAt := now.Add(time.Hour)
	upstream.setAPICallResponse("idx-codex-concurrent", sidecarIntegrationAPICallResponse{StatusCode: http.StatusOK, Body: sidecarIntegrationExhaustedUsageBody(t, resetAt), Delay: 500 * time.Millisecond})
	sidecarID := sidecarIntegrationCreateAndSyncSidecar(t, conn, router, upstream, "probe-concurrent")
	sidecarIntegrationEnableProbePolicy(t, conn, sidecarID, 1, 2)

	resultCh := make(chan struct {
		result managementsidecars.SidecarWatchdogResult
		err    error
	}, 1)
	go func() {
		result, err := service.ReconcileSidecarWatchdog(ctx, sidecarID)
		resultCh <- struct {
			result managementsidecars.SidecarWatchdogResult
			err    error
		}{result: result, err: err}
	}()
	upstream.waitForAPICallCount(t, 1)
	duplicate, err := secondService.ReconcileSidecarWatchdog(ctx, sidecarID)
	if err != nil {
		t.Fatalf("duplicate cross-service watchdog reconcile: %v", err)
	}
	if !duplicate.Skipped || duplicate.SkipReason != "watchdog_lease_held" || duplicate.Reconciled || duplicate.ActionCount != 0 {
		t.Fatalf("duplicate cross-service reconcile should skip on DB lease, got %+v", duplicate)
	}

	select {
	case first := <-resultCh:
		if first.err != nil || !first.result.Reconciled || first.result.QuotaHeld != 1 {
			t.Fatalf("first concurrent reconcile failed: result=%+v err=%v", first.result, first.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for first concurrent reconcile")
	}
	upstream.assertAPICalls(t, []string{"idx-codex-concurrent"})
	upstream.assertFieldPatchPriorities(t, []int{0})
	sidecarIntegrationAssertActiveHold(t, conn, sidecarID, "auth-codex-concurrent", 10, 0, resetAt)
	sidecarIntegrationAssertPolicyCursorRemoved(t, conn)
	sidecarIntegrationAssertDBNoLeaks(t, conn, sidecarID)
}

func TestSidecarIntegrationProbeFailuresDoNotPatchPriority(t *testing.T) {
	tests := []struct {
		name           string
		response       sidecarIntegrationAPICallResponse
		timeoutSeconds int
		wantStatus     string
		wantHTTPStatus int
	}{
		{name: "timeout", response: sidecarIntegrationAPICallResponse{StatusCode: http.StatusOK, Body: sidecarIntegrationHealthyUsageBody(), Delay: 1200 * time.Millisecond}, timeoutSeconds: 1, wantStatus: "probe_failed_timeout"},
		{name: "wrapped non 200", response: sidecarIntegrationAPICallResponse{StatusCode: http.StatusTooManyRequests, Body: `{"error":"rate_limited"}`}, timeoutSeconds: 2, wantStatus: "probe_failed_status", wantHTTPStatus: http.StatusTooManyRequests},
		{name: "malformed usage body", response: sidecarIntegrationAPICallResponse{StatusCode: http.StatusOK, Body: `{not-json`}, timeoutSeconds: 2, wantStatus: "probe_failed_parse"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			now := time.Date(2026, time.May, 11, 11, 0, 0, 0, time.UTC)
			conn, service, router := newSidecarIntegrationServiceRouterWithNow(t, ctx, func() time.Time { return now })
			upstream := newSidecarIntegrationUpstream(t)
			defer upstream.Close()
			upstream.setAuthFiles(sidecarIntegrationCodexAuthFixture("auth-codex-failure", "idx-codex-failure", 10))
			upstream.setAPICallResponse("idx-codex-failure", tt.response)
			sidecarID := sidecarIntegrationCreateAndSyncSidecar(t, conn, router, upstream, "probe-failure-"+tt.name)
			sidecarIntegrationEnableProbePolicy(t, conn, sidecarID, 1, tt.timeoutSeconds)

			result, err := service.ReconcileSidecarWatchdog(ctx, sidecarID)
			if err != nil || !result.Skipped || result.SkipReason != "no_watchdog_action_needed" {
				t.Fatalf("expected failed probe to skip without patch, result=%+v err=%v", result, err)
			}
			upstream.assertFieldPatchPriorities(t, nil)
			upstream.assertAPICalls(t, []string{"idx-codex-failure"})
			sidecarIntegrationAssertNoActiveHolds(t, conn, sidecarID)
			sidecarIntegrationAssertProbeObservation(t, conn, sidecarID, "auth-codex-failure", tt.wantStatus, false, tt.wantHTTPStatus)
			sidecarIntegrationAssertDBNoLeaks(t, conn, sidecarID)
		})
	}
}

func TestLeakUnsupportedProviderWatchdogIntegrationSkipDedupe(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	now := time.Date(2026, time.May, 11, 12, 0, 0, 0, time.UTC)
	conn, service, router := newSidecarIntegrationServiceRouterWithNow(t, ctx, func() time.Time { return now })
	upstream := newSidecarIntegrationUpstream(t)
	defer upstream.Close()
	upstream.setAuthFiles(sidecarIntegrationAuthFixtureWith("auth-gemini-unsupported", "auth_unsupported", "gemini", 0))
	sidecarID := sidecarIntegrationCreateAndSyncSidecar(t, conn, router, upstream, "probe-unsupported")
	sidecarIntegrationEnableProbePolicy(t, conn, sidecarID, 1, 2)
	sidecarIntegrationInsertWatchdogHold(t, conn, sidecarID, "auth-gemini-unsupported", "auth_unsupported", "gemini", 10, 0, now.Add(-time.Minute))

	result, err := service.ReconcileSidecarWatchdog(ctx, sidecarID)
	if err != nil || result.Reconciled || result.ActionCount != 1 || result.UnsupportedSkipped != 1 {
		t.Fatalf("expected unsupported provider hold to record a skipped action without reconciling, result=%+v err=%v", result, err)
	}
	repeated, err := service.ReconcileSidecarWatchdog(ctx, sidecarID)
	if err != nil || repeated.Reconciled || repeated.ActionCount != 0 || repeated.UnsupportedSkipped != 1 {
		t.Fatalf("expected repeated unsupported provider hold to skip without action flood, result=%+v err=%v", repeated, err)
	}
	upstream.assertAPICalls(t, nil)
	upstream.assertFieldPatchPriorities(t, nil)
	sidecarIntegrationAssertActionCount(t, conn, sidecarID, "probe_skipped_unsupported_provider", "skipped", 1)
	sidecarIntegrationAssertDBNoLeaks(t, conn, sidecarID)
}

func TestSidecarIntegrationProbeRecoversDueHoldWhenSnapshotsAreStale(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	now := time.Date(2026, time.May, 11, 13, 0, 0, 0, time.UTC)
	conn, service, router := newSidecarIntegrationServiceRouterWithNow(t, ctx, func() time.Time { return now })
	upstream := newSidecarIntegrationUpstream(t)
	defer upstream.Close()
	upstream.setAuthFiles(sidecarIntegrationCodexAuthFixture("auth-codex-stale", "idx-codex-stale", 0))
	upstream.setAPICallResponse("idx-codex-stale", sidecarIntegrationAPICallResponse{StatusCode: http.StatusOK, Body: sidecarIntegrationHealthyUsageBody()})
	sidecarID := sidecarIntegrationCreateAndSyncSidecar(t, conn, router, upstream, "probe-stale")
	sidecarIntegrationEnableProbePolicy(t, conn, sidecarID, 1, 2)
	sidecarIntegrationInsertWatchdogHold(t, conn, sidecarID, "auth-codex-stale", "idx-codex-stale", "codex", 10, 0, now.Add(-time.Minute))
	sidecarIntegrationMarkSnapshotsStale(t, conn, sidecarID, now)

	result, err := service.ReconcileSidecarWatchdog(ctx, sidecarID)
	if err != nil || !result.Reconciled || result.SkipReason == "watchdog_skipped_stale_snapshot" {
		t.Fatalf("expected stale snapshots to still allow due-hold probe recovery, result=%+v err=%v", result, err)
	}
	upstream.assertAPICalls(t, []string{"idx-codex-stale"})
	upstream.assertFieldPatchPriorities(t, []int{10})
	sidecarIntegrationAssertReleasedHold(t, conn, sidecarID, "auth-codex-stale")
	sidecarIntegrationAssertActionMissing(t, conn, sidecarID, "watchdog_skipped_stale_snapshot")
	sidecarIntegrationAssertDBNoLeaks(t, conn, sidecarID)
}

func TestSidecarIntegrationProbeCursorRotation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	now := time.Date(2026, time.May, 11, 14, 0, 0, 0, time.UTC)
	conn, service, router := newSidecarIntegrationServiceRouterWithNow(t, ctx, func() time.Time { return now })
	upstream := newSidecarIntegrationUpstream(t)
	defer upstream.Close()
	upstream.setAuthFiles(
		sidecarIntegrationCodexAuthFixture("auth-codex-a", "idx-codex-a", 10),
		sidecarIntegrationCodexAuthFixture("auth-codex-b", "idx-codex-b", 10),
	)
	sidecarID := sidecarIntegrationCreateAndSyncSidecar(t, conn, router, upstream, "probe-cursor")
	sidecarIntegrationEnableProbePolicy(t, conn, sidecarID, 1, 2)

	if _, err := service.ReconcileSidecarWatchdog(ctx, sidecarID); err != nil {
		t.Fatalf("first cursor reconcile: %v", err)
	}
	sidecarIntegrationSetProbeBatchCompletedAt(t, conn, sidecarID, now)
	now = now.Add(time.Minute)
	if _, err := service.ReconcileSidecarWatchdog(ctx, sidecarID); err != nil {
		t.Fatalf("second cursor reconcile: %v", err)
	}
	upstream.assertAPICalls(t, []string{"idx-codex-a", "idx-codex-b"})
	sidecarIntegrationAssertPolicyCursorRemoved(t, conn)
	sidecarIntegrationAssertDBNoLeaks(t, conn, sidecarID)
}

func TestSidecarIntegrationWatchdogObservationRetention(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	now := time.Date(2026, time.May, 11, 15, 0, 0, 0, time.UTC)
	conn, service, router := newSidecarIntegrationServiceRouterWithNow(t, ctx, func() time.Time { return now })
	upstream := newSidecarIntegrationUpstream(t)
	defer upstream.Close()
	sidecarID := sidecarIntegrationCreateOnlySidecar(t, conn, router, upstream, "probe-retention")
	sidecarIntegrationInsertProbeObservation(t, conn, sidecarID, "auth-old", now.Add(-16*24*time.Hour))
	sidecarIntegrationInsertProbeObservation(t, conn, sidecarID, "auth-new", now.Add(-24*time.Hour))

	scheduler := background.NewScheduler(background.Config{})
	if err := service.RegisterBackgroundWorker(scheduler); err != nil {
		t.Fatalf("register sidecar workers: %v", err)
	}
	if err := scheduler.Start(ctx); err != nil {
		t.Fatalf("start sidecar worker scheduler: %v", err)
	}
	if got := scheduler.Submit(ctx, background.JobRequest{Worker: managementsidecars.SidecarWatchdogWorkerName}); got.Status != background.SubmitAccepted {
		t.Fatalf("submit watchdog worker for retention cleanup: %+v", got)
	}
	if drain := scheduler.Drain(ctx, time.Now().Add(5*time.Second)); drain.TimedOut || drain.Failed != 0 {
		t.Fatalf("watchdog worker retention drain failed: %+v", drain)
	}
	sidecarIntegrationAssertObservationRetained(t, conn, sidecarID, "auth-old", false)
	sidecarIntegrationAssertObservationRetained(t, conn, sidecarID, "auth-new", true)
	sidecarIntegrationAssertDBNoLeaks(t, conn, sidecarID)
}

func TestSidecarIntegrationWatchdogManagementAuthPauseSkipsProbes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	now := time.Date(2026, time.May, 11, 16, 0, 0, 0, time.UTC)
	conn, service, router := newSidecarIntegrationServiceRouterWithNow(t, ctx, func() time.Time { return now })
	upstream := newSidecarIntegrationUpstream(t)
	defer upstream.Close()
	upstream.setAuthFiles(sidecarIntegrationCodexAuthFixture("auth-codex-paused", "idx-codex-paused", 10))
	sidecarID := sidecarIntegrationCreateAndSyncSidecar(t, conn, router, upstream, "probe-pause")
	sidecarIntegrationEnableProbePolicy(t, conn, sidecarID, 1, 2)
	sidecarIntegrationPauseManagementAuth(t, conn, sidecarID, now.Add(time.Hour))

	result, err := service.ReconcileSidecarWatchdog(ctx, sidecarID)
	if err != nil || !result.Skipped || result.SkipReason != "watchdog_skipped_management_auth_pause" {
		t.Fatalf("expected management-auth pause to skip watchdog, result=%+v err=%v", result, err)
	}
	upstream.assertAPICalls(t, nil)
	upstream.assertFieldPatchPriorities(t, nil)
	sidecarIntegrationAssertAction(t, conn, sidecarID, "watchdog_skipped_management_auth_pause", "skipped")
	sidecarIntegrationAssertDBNoLeaks(t, conn, sidecarID)
}

func TestSidecarIntegrationManualQuotaScanLifecycleAndPrivacy(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	now := time.Date(2026, time.May, 11, 17, 0, 0, 0, time.UTC)
	conn, service, router := newSidecarIntegrationServiceRouterWithNow(t, ctx, func() time.Time { return now })
	upstream := newSidecarIntegrationUpstream(t)
	defer upstream.Close()
	upstream.setAuthFiles(
		sidecarIntegrationCodexAuthFixture("auth-a-low", "idx-low", 0),
		sidecarIntegrationCodexAuthFixture("auth-z-high", "idx-high", 0),
	)
	upstream.setAPICallResponse("idx-low", sidecarIntegrationAPICallResponse{StatusCode: http.StatusOK, Body: sidecarIntegrationHealthyUsageBody()})
	upstream.setAPICallResponse("idx-high", sidecarIntegrationAPICallResponse{StatusCode: http.StatusOK, Body: sidecarIntegrationHealthyUsageBody()})
	sidecarID := sidecarIntegrationCreateAndSyncSidecar(t, conn, router, upstream, "manual-quota-scan")
	sidecarIntegrationEnableProbePolicy(t, conn, sidecarID, 1, 2)
	sidecarIntegrationEnableQuotaInventory(t, conn, sidecarID)

	startBody, scan := sidecarIntegrationRequestJSON(t, router, http.MethodPost, "/sidecars/"+strconv.Itoa(sidecarID)+"/quota-scans", map[string]any{"requested_by": "integration-operator"}, http.StatusAccepted)
	sidecarIntegrationAssertNoLeaks(t, startBody)
	sidecarIntegrationAssertQuotaRouteNoInternalLeak(t, startBody, "idx-low", "idx-high")
	if scan["status"] != "queued" || sidecarIntegrationInt(t, scan["planned_count"], "planned_count") != 2 {
		t.Fatalf("expected queued manual quota scan with two planned auths, got %+v", scan)
	}
	scanID := sidecarIntegrationInt(t, scan["id"], "id")
	upstream.assertAPICalls(t, nil)

	result, err := service.ReconcileSidecarWatchdog(ctx, sidecarID)
	if err != nil || result.Probed != 1 {
		t.Fatalf("expected first scan reconcile to probe one auth, result=%+v err=%v", result, err)
	}
	upstream.assertAPICalls(t, []string{"idx-low"})

	currentBody, current := sidecarIntegrationRequestJSON(t, router, http.MethodGet, "/sidecars/"+strconv.Itoa(sidecarID)+"/quota-scans/current", nil, http.StatusOK)
	sidecarIntegrationAssertNoLeaks(t, currentBody)
	sidecarIntegrationAssertQuotaRouteNoInternalLeak(t, currentBody, "idx-low", "idx-high")
	if current["status"] != "running" || sidecarIntegrationInt(t, current["attempted_count"], "attempted_count") != 1 {
		t.Fatalf("expected running scan after first probe, got %+v", current)
	}

	statesBody, states := sidecarIntegrationRequestJSON(t, router, http.MethodGet, "/sidecars/"+strconv.Itoa(sidecarID)+"/quota-states", nil, http.StatusOK)
	sidecarIntegrationAssertNoLeaks(t, statesBody)
	sidecarIntegrationAssertQuotaRouteNoInternalLeak(t, statesBody, "idx-low", "idx-high")
	sidecarIntegrationAssertQuotaState(t, states, "auth-a-low", "using", true, 0, false)

	cancelBody, cancelled := sidecarIntegrationRequestJSON(t, router, http.MethodPost, "/sidecars/"+strconv.Itoa(sidecarID)+"/quota-scans/"+strconv.Itoa(scanID)+"/cancel", nil, http.StatusAccepted)
	sidecarIntegrationAssertNoLeaks(t, cancelBody)
	sidecarIntegrationAssertQuotaRouteNoInternalLeak(t, cancelBody, "idx-low", "idx-high")
	if cancelled["status"] != "cancelled" || cancelled["cancel_requested_at"] == nil || cancelled["completed_at"] == nil {
		t.Fatalf("expected cancelled quota scan lifecycle fields, got %+v", cancelled)
	}
	missingBody, _ := sidecarIntegrationRequestJSON(t, router, http.MethodGet, "/sidecars/"+strconv.Itoa(sidecarID)+"/quota-scans/current", nil, http.StatusNoContent)
	sidecarIntegrationAssertNoLeaks(t, missingBody)
	listBody, _ := sidecarIntegrationRequestJSON(t, router, http.MethodGet, "/sidecars/"+strconv.Itoa(sidecarID)+"/quota-scans", nil, http.StatusOK)
	sidecarIntegrationAssertNoLeaks(t, listBody)
	sidecarIntegrationAssertQuotaRouteNoInternalLeak(t, listBody, "idx-low", "idx-high")
	if !strings.Contains(listBody, "cancelled") {
		t.Fatalf("expected cancelled scan in scan history, got %s", listBody)
	}
	sidecarIntegrationAssertDBNoLeaks(t, conn, sidecarID)
}

func TestSidecarIntegrationWatchdogWorkerCooldownSkipsFailedHoldAcrossTicks(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	now := time.Date(2026, time.May, 11, 18, 0, 0, 0, time.UTC)
	conn, service, router := newSidecarIntegrationServiceRouterWithNow(t, ctx, func() time.Time { return now })
	upstream := newSidecarIntegrationUpstream(t)
	defer upstream.Close()
	upstream.setAPICallResponse("idx-cooldown-1", sidecarIntegrationAPICallResponse{StatusCode: http.StatusInternalServerError, Body: `{"error":"upstream"}`})
	upstream.setAPICallResponse("idx-cooldown-2", sidecarIntegrationAPICallResponse{StatusCode: http.StatusInternalServerError, Body: `{"error":"upstream"}`})
	sidecarID := sidecarIntegrationCreateOnlySidecar(t, conn, router, upstream, "cooldown-worker")
	sidecarIntegrationEnableProbePolicy(t, conn, sidecarID, 1, 2)
	sidecarIntegrationInsertWatchdogHold(t, conn, sidecarID, "auth-cooldown-1", "idx-cooldown-1", "codex", 10, 0, now.Add(-2*time.Hour))
	sidecarIntegrationInsertWatchdogHold(t, conn, sidecarID, "auth-cooldown-2", "idx-cooldown-2", "codex", 10, 0, now.Add(-time.Hour))

	sidecarIntegrationRunWatchdogWorker(t, ctx, service, "first failed hold")
	upstream.assertAPICalls(t, []string{"idx-cooldown-1"})
	now = now.Add(time.Second)
	sidecarIntegrationRunWatchdogWorker(t, ctx, service, "second failed hold")
	upstream.assertAPICalls(t, []string{"idx-cooldown-1", "idx-cooldown-2"})
	sidecarIntegrationAssertDBNoLeaks(t, conn, sidecarID)
}

func TestSidecarIntegrationQueueBackedRepairPatchesAndClearsPendingAction(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	now := time.Date(2026, time.May, 11, 19, 0, 0, 0, time.UTC)
	conn, service, router := newSidecarIntegrationServiceRouterWithNow(t, ctx, func() time.Time { return now })
	upstream := newSidecarIntegrationUpstream(t)
	defer upstream.Close()
	upstream.setAuthFiles(sidecarIntegrationCodexAuthFixture("auth-queue-repair", "idx-queue-repair", 10))
	sidecarID := sidecarIntegrationCreateOnlySidecar(t, conn, router, upstream, "queue-repair")
	sidecarIntegrationEnableProbePolicy(t, conn, sidecarID, 1, 2)
	holdUntil := now.Add(2 * time.Hour)
	sidecarIntegrationInsertPendingRepairAction(t, conn, sidecarID, now, "auth-queue-repair", "auth-queue-repair.json", "idx-queue-repair", 10, 0, holdUntil)
	sidecarIntegrationAssertPendingActionCount(t, conn, sidecarID, 1)
	sidecarIntegrationAssertDBNoLeaks(t, conn, sidecarID)

	result, err := service.ReconcileSidecarWatchdog(ctx, sidecarID)
	if err != nil || !result.Reconciled || result.QuotaHeld != 1 {
		t.Fatalf("expected pending queue repair to patch and create hold, result=%+v err=%v", result, err)
	}
	upstream.assertFieldPatchPriorities(t, []int{0})
	sidecarIntegrationAssertPendingActionCount(t, conn, sidecarID, 0)
	sidecarIntegrationAssertActiveHold(t, conn, sidecarID, "auth-queue-repair", 10, 0, holdUntil)
	sidecarIntegrationAssertAction(t, conn, sidecarID, "deprioritize", "succeeded")
	sidecarIntegrationAssertDBNoLeaks(t, conn, sidecarID)
}

func newSidecarIntegrationRouter(t *testing.T, ctx context.Context) (*pgx.Conn, http.Handler) {
	t.Helper()
	conn, _, router := newSidecarIntegrationServiceRouter(t, ctx)
	return conn, router
}

func newSidecarIntegrationServiceRouter(t *testing.T, ctx context.Context) (*pgx.Conn, *managementsidecars.Service, http.Handler) {
	t.Helper()
	return newSidecarIntegrationServiceRouterWithNow(t, ctx, func() time.Time {
		return time.Date(2026, time.May, 10, 12, 0, 0, 0, time.UTC)
	})
}

func newSidecarIntegrationServiceRouterWithNow(t *testing.T, ctx context.Context, now func() time.Time) (*pgx.Conn, *managementsidecars.Service, http.Handler) {
	t.Helper()
	conn, _, service, router := newSidecarIntegrationServiceRouterPoolWithNow(t, ctx, now)
	return conn, service, router
}

func newSidecarIntegrationServiceRouterPoolWithNow(t *testing.T, ctx context.Context, now func() time.Time) (*pgx.Conn, *pgxpool.Pool, *managementsidecars.Service, http.Handler) {
	t.Helper()
	harness := newPostgresHarness(t)
	databaseName := "sidecars_integration_" + randomSuffix(t)
	conn := harness.openDatabase(t, ctx, databaseName)
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	if _, err := newRunner(t).Run(ctx, conn); err != nil {
		t.Fatalf("run migrations for sidecar integration: %v", err)
	}
	pool, err := pgxpool.New(ctx, harness.connectionString(databaseName))
	if err != nil {
		t.Fatalf("open sidecar integration pool: %v", err)
	}
	t.Cleanup(pool.Close)
	retentionStore := logretention.NewStore(logretention.Options{Pool: pool, Now: now})
	if err := retentionStore.EnsurePartitionHorizonForTable(ctx, "sidecar_watchdog_actions"); err != nil {
		t.Fatalf("ensure sidecar action-history partitions: %v", err)
	}
	service, err := managementsidecars.NewService(config.Settings{SecretEncryptionKey: "sidecar-integration-secret"}, managementsidecars.Options{Pool: pool, Now: now})
	if err != nil {
		t.Fatalf("build sidecar integration service: %v", err)
	}
	t.Cleanup(service.Close)
	router := chi.NewRouter()
	service.MountManagementRoutes(router)
	return conn, pool, service, router
}

func sidecarIntegrationRequestJSON(t *testing.T, handler http.Handler, method string, path string, body any, wantStatus int) (string, map[string]any) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal sidecar integration request: %v", err)
		}
		reader = bytes.NewReader(raw)
	}
	request := httptest.NewRequest(method, path, reader)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	responseBody := strings.TrimSpace(recorder.Body.String())
	if recorder.Code != wantStatus {
		t.Fatalf("%s %s status = %d want %d body=%s", method, path, recorder.Code, wantStatus, responseBody)
	}
	payload := map[string]any{}
	if responseBody != "" {
		if err := json.Unmarshal([]byte(responseBody), &payload); err != nil {
			t.Fatalf("decode sidecar integration response %s: %v", responseBody, err)
		}
	}
	return responseBody, payload
}

func sidecarIntegrationAssertDBNoLeaks(t *testing.T, conn *pgx.Conn, sidecarID int) {
	t.Helper()
	var combined string
	if err := conn.QueryRow(context.Background(), `SELECT concat_ws(' ',
		COALESCE((SELECT string_agg(management_password, ' ') FROM sidecar_instances WHERE id = $1), ''),
		COALESCE((SELECT string_agg(snapshot_json::text, ' ') FROM sidecar_auth_snapshots WHERE sidecar_id = $1), ''),
		COALESCE((SELECT string_agg(snapshot_json::text, ' ') FROM sidecar_provider_snapshots WHERE sidecar_id = $1), ''),
		COALESCE((SELECT string_agg(concat_ws(' ', reason, condition_hash), ' ') FROM sidecar_watchdog_holds WHERE sidecar_id = $1), ''),
		COALESCE((SELECT string_agg(concat_ws(' ', auth_id, auth_name, auth_index, provider, action_type, reason, error_message), ' ') FROM sidecar_watchdog_actions WHERE sidecar_id = $1), ''),
		COALESCE((SELECT string_agg(concat_ws(' ', auth_id, auth_name, auth_index, provider, action_type, reason, last_error_message), ' ') FROM sidecar_watchdog_pending_actions WHERE sidecar_id = $1), ''),
		COALESCE((SELECT string_agg(concat_ws(' ', requested_by, cursor_auth_id, status, last_error_code), ' ') FROM sidecar_quota_scan_runs WHERE sidecar_id = $1), ''),
		COALESCE((SELECT string_agg(concat_ws(' ', auth_id, auth_index, auth_name, provider, quota_band, probe_status, reason_code, blocking_window, last_error_code), ' ') FROM sidecar_auth_quota_states WHERE sidecar_id = $1), ''),
		COALESCE((SELECT string_agg(concat_ws(' ', auth_id, auth_index, provider, reason_code, blocking_window, error_code, windows_json::text), ' ') FROM sidecar_quota_probe_observations WHERE sidecar_id = $1), '')
	)`, sidecarID).Scan(&combined); err != nil {
		t.Fatalf("collect sidecar persisted strings: %v", err)
	}
	sidecarIntegrationAssertNoLeaks(t, combined)
	if !strings.Contains(combined, "enc:") {
		t.Fatalf("expected persisted sidecar management password to be encrypted, got %s", combined)
	}
}

func assertSidecarIntegrationManualPause(t *testing.T, conn *pgx.Conn, sidecarID int) {
	t.Helper()
	var count int
	if err := conn.QueryRow(context.Background(), `SELECT count(*) FROM sidecar_watchdog_holds WHERE sidecar_id = $1 AND status = 'paused' AND manual_pause_until IS NOT NULL`, sidecarID).Scan(&count); err != nil {
		t.Fatalf("count manual pause holds: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected one paused manual watchdog hold after operator mutation, got %d", count)
	}
}

func sidecarIntegrationAssertNoLeaks(t *testing.T, value string) {
	t.Helper()
	for _, secret := range sidecarIntegrationSecrets {
		if strings.Contains(value, secret) {
			t.Fatalf("sidecar integration value leaked %q in %s", secret, value)
		}
	}
}

func sidecarIntegrationAssertQuotaRouteNoInternalLeak(t *testing.T, value string, internals ...string) {
	t.Helper()
	for _, marker := range append([]string{`"auth_index":`, `"cursor_auth_id":`, `"last_observation_id":`}, internals...) {
		if strings.Contains(value, marker) {
			t.Fatalf("quota route leaked internal marker %q in %s", marker, value)
		}
	}
}

func sidecarIntegrationAssertQuotaState(t *testing.T, payload map[string]any, authID string, quotaState string, authIndexPresent bool, currentPriority int, activeHold bool) {
	t.Helper()
	items, ok := payload["items"].([]any)
	if !ok {
		t.Fatalf("expected quota-state items array, got %+v", payload)
	}
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok || item["auth_id"] != authID {
			continue
		}
		if item["quota_band"] != quotaState || item["auth_index_present"] != authIndexPresent || item["active_hold"] != activeHold {
			t.Fatalf("unexpected quota state for %s: %+v", authID, item)
		}
		if item["current_priority"] == nil {
			t.Fatalf("expected current_priority for %s, got %+v", authID, item)
		}
		if sidecarIntegrationInt(t, item["current_priority"], "current_priority") != currentPriority {
			t.Fatalf("unexpected current_priority for %s: %+v", authID, item)
		}
		if lastSnapshotAt, ok := item["last_snapshot_at"]; ok && lastSnapshotAt == nil {
			t.Fatalf("expected last_snapshot_at for %s when present, got %+v", authID, item)
		}
		return
	}
	t.Fatalf("quota state for %s not found in %+v", authID, payload)
}

func sidecarIntegrationRunWatchdogWorker(t *testing.T, ctx context.Context, service *managementsidecars.Service, label string) {
	t.Helper()
	scheduler := background.NewScheduler(background.Config{})
	if err := service.RegisterBackgroundWorker(scheduler); err != nil {
		t.Fatalf("register sidecar workers for %s: %v", label, err)
	}
	if err := scheduler.Start(ctx); err != nil {
		t.Fatalf("start sidecar worker scheduler for %s: %v", label, err)
	}
	defer func() { _ = scheduler.Stop(context.Background(), time.Now().Add(time.Second)) }()
	if got := scheduler.Submit(ctx, background.JobRequest{Worker: managementsidecars.SidecarWatchdogWorkerName}); got.Status != background.SubmitAccepted {
		t.Fatalf("submit watchdog worker for %s: %+v", label, got)
	}
	if drain := scheduler.Drain(ctx, time.Now().Add(5*time.Second)); drain.TimedOut || drain.Failed != 0 {
		t.Fatalf("watchdog worker drain failed for %s: %+v", label, drain)
	}
}

func sidecarIntegrationInt(t *testing.T, value any, field string) int {
	t.Helper()
	number, ok := value.(float64)
	if !ok {
		t.Fatalf("expected numeric %s, got %T %v", field, value, value)
	}
	return int(number)
}

func sidecarIntegrationCreateOnlySidecar(t *testing.T, conn *pgx.Conn, router http.Handler, upstream *sidecarIntegrationUpstream, suffix string) int {
	t.Helper()
	body, created := sidecarIntegrationRequestJSON(t, router, http.MethodPost, "/sidecars", map[string]any{
		"name":                    "Integration " + suffix,
		"base_url":                upstream.URL(),
		"management_password":     sidecarIntegrationManagementSecret,
		"allow_private_network":   true,
		"allow_insecure_http":     true,
		"sync_interval_seconds":   60,
		"request_timeout_seconds": 5,
	}, http.StatusCreated)
	sidecarIntegrationAssertNoLeaks(t, body)
	sidecarID := sidecarIntegrationInt(t, created["id"], "id")
	sidecarIntegrationAssertDBNoLeaks(t, conn, sidecarID)
	return sidecarID
}

func sidecarIntegrationCreateAndSyncSidecar(t *testing.T, conn *pgx.Conn, router http.Handler, upstream *sidecarIntegrationUpstream, suffix string) int {
	t.Helper()
	sidecarID := sidecarIntegrationCreateOnlySidecar(t, conn, router, upstream, suffix)
	body, _ := sidecarIntegrationRequestJSON(t, router, http.MethodPost, "/sidecars/"+strconv.Itoa(sidecarID)+"/sync", nil, http.StatusOK)
	sidecarIntegrationAssertNoLeaks(t, body)
	sidecarIntegrationAssertDBNoLeaks(t, conn, sidecarID)
	return sidecarID
}

func sidecarIntegrationEnableProbePolicy(t *testing.T, conn *pgx.Conn, sidecarID int, probeConcurrency int, timeoutSeconds int) {
	t.Helper()
	_, err := conn.Exec(context.Background(), `INSERT INTO sidecar_watchdog_policies (
sidecar_id, enabled, failure_threshold, failure_window_seconds, fallback_cooldown_seconds,
quota_exceeded_priority, using_priority, error_priority, manual_override_pause_seconds, probe_concurrency, probe_timeout_seconds,
quota_inventory_enabled, initial_scan_enabled, rolling_refresh_enabled)
VALUES ($1, true, 3, 3600, 86400, 0, 1, 0, 1800, $2, $3, false, false, true)
ON CONFLICT (sidecar_id) DO UPDATE SET enabled = true, failure_threshold = 3,
failure_window_seconds = 3600, fallback_cooldown_seconds = 86400,
quota_exceeded_priority = 0, using_priority = 1, error_priority = 0, manual_override_pause_seconds = 1800,
probe_concurrency = $2, probe_timeout_seconds = $3,
quota_inventory_enabled = false, initial_scan_enabled = false, rolling_refresh_enabled = true,
updated_at = now()`, sidecarID, probeConcurrency, timeoutSeconds)
	if err != nil {
		t.Fatalf("enable probe policy: %v", err)
	}
}

func sidecarIntegrationEnableQuotaInventory(t *testing.T, conn *pgx.Conn, sidecarID int) {
	t.Helper()
	_, err := conn.Exec(context.Background(), `UPDATE sidecar_watchdog_policies
SET quota_inventory_enabled = true, initial_scan_enabled = true, rolling_refresh_enabled = false, updated_at = now()
WHERE sidecar_id = $1`, sidecarID)
	if err != nil {
		t.Fatalf("enable quota inventory policy: %v", err)
	}
}

func sidecarIntegrationInsertWatchdogHold(t *testing.T, conn *pgx.Conn, sidecarID int, authID string, authIndex string, provider string, previousPriority int, targetPriority int, holdUntil time.Time) {
	t.Helper()
	_, err := conn.Exec(context.Background(), `INSERT INTO sidecar_watchdog_holds (
sidecar_id, auth_id, auth_index, provider, reason, condition_hash, previous_priority,
target_priority, hold_until, status)
VALUES ($1, $2, $3, $4, 'quota_exceeded', $5, $6, $7, $8, 'active')`, sidecarID, authID, authIndex, provider, "integration-hash-"+authID, previousPriority, targetPriority, holdUntil)
	if err != nil {
		t.Fatalf("insert watchdog hold: %v", err)
	}
}

func sidecarIntegrationInsertProbeObservation(t *testing.T, conn *pgx.Conn, sidecarID int, authID string, probedAt time.Time) {
	t.Helper()
	_, err := conn.Exec(context.Background(), `INSERT INTO sidecar_quota_probe_observations (
sidecar_id, auth_id, auth_index, provider, probed_at, probe_status, quota_exceeded, windows_json)
VALUES ($1, $2, $3, 'codex', $4, 'probe_succeeded', false, '[]'::jsonb)`, sidecarID, authID, "idx-"+authID, probedAt)
	if err != nil {
		t.Fatalf("insert probe observation: %v", err)
	}
}

func sidecarIntegrationMarkSnapshotsStale(t *testing.T, conn *pgx.Conn, sidecarID int, now time.Time) {
	t.Helper()
	staleAt := now.Add(-3 * time.Hour)
	_, err := conn.Exec(context.Background(), `UPDATE sidecar_instances
SET last_sync_at = $2, last_successful_sync_at = $2, snapshot_stale_after = $2,
management_auth_state = 'valid', last_sync_error = NULL
WHERE id = $1`, sidecarID, staleAt)
	if err != nil {
		t.Fatalf("mark snapshots stale: %v", err)
	}
}

func sidecarIntegrationPauseManagementAuth(t *testing.T, conn *pgx.Conn, sidecarID int, pausedUntil time.Time) {
	t.Helper()
	_, err := conn.Exec(context.Background(), `UPDATE sidecar_instances
SET auth_failure_pause_until = $2, management_auth_state = 'invalid_management_auth'
WHERE id = $1`, sidecarID, pausedUntil)
	if err != nil {
		t.Fatalf("pause sidecar management auth: %v", err)
	}
}

func sidecarIntegrationInsertPendingRepairAction(t *testing.T, conn *pgx.Conn, sidecarID int, createdAt time.Time, authID string, authName string, authIndex string, previousPriority int, targetPriority int, holdUntil time.Time) {
	t.Helper()
	var actionID int
	if err := conn.QueryRow(context.Background(), `INSERT INTO sidecar_watchdog_actions (
sidecar_id, auth_id, auth_name, auth_index, provider, action_type, reason,
previous_priority, target_priority, hold_until, status, created_at, updated_at)
VALUES ($1, $2, $3, $4, 'codex', 'deprioritize', 'quota_exceeded', $5, $6, $7, 'pending', $8, $8)
RETURNING id`, sidecarID, authID, authName, authIndex, previousPriority, targetPriority, holdUntil, createdAt).Scan(&actionID); err != nil {
		t.Fatalf("insert pending repair action history: %v", err)
	}
	_, err := conn.Exec(context.Background(), `INSERT INTO sidecar_watchdog_pending_actions (
sidecar_id, action_history_created_at, action_history_id, auth_id, auth_name, auth_index,
provider, action_type, reason, previous_priority, target_priority, hold_until)
VALUES ($1, $2, $3, $4, $5, $6, 'codex', 'deprioritize', 'quota_exceeded', $7, $8, $9)`, sidecarID, createdAt, actionID, authID, authName, authIndex, previousPriority, targetPriority, holdUntil)
	if err != nil {
		t.Fatalf("insert pending repair queue row: %v", err)
	}
}

func sidecarIntegrationSetProbeBatchCompletedAt(t *testing.T, conn *pgx.Conn, sidecarID int, completedAt time.Time) {
	t.Helper()
	_, err := conn.Exec(context.Background(), `INSERT INTO sidecar_watchdog_policies (sidecar_id, probe_last_batch_completed_at, probe_next_batch_after)
VALUES ($1, $2, $2)
ON CONFLICT (sidecar_id) DO UPDATE SET probe_last_batch_completed_at = EXCLUDED.probe_last_batch_completed_at, probe_next_batch_after = EXCLUDED.probe_next_batch_after, updated_at = now()`, sidecarID, completedAt.UTC())
	if err != nil {
		t.Fatalf("set probe batch completion: %v", err)
	}
}

func sidecarIntegrationClearProbeBatchCompletedAt(t *testing.T, conn *pgx.Conn, sidecarID int) {
	t.Helper()
	_, err := conn.Exec(context.Background(), `UPDATE sidecar_watchdog_policies SET probe_last_batch_completed_at = NULL, probe_next_batch_after = NULL, updated_at = now() WHERE sidecar_id = $1`, sidecarID)
	if err != nil {
		t.Fatalf("clear probe batch completion: %v", err)
	}
}

func sidecarIntegrationAssertActiveHold(t *testing.T, conn *pgx.Conn, sidecarID int, authID string, previousPriority int, targetPriority int, wantHoldUntil time.Time) {
	t.Helper()
	var status, reason string
	var gotPrevious, gotTarget int
	var holdUntilMatches bool
	if err := conn.QueryRow(context.Background(), `SELECT status, reason, COALESCE(previous_priority, -1), target_priority,
hold_until BETWEEN ($3::timestamptz - interval '1 second') AND ($3::timestamptz + interval '1 second')
FROM sidecar_watchdog_holds WHERE sidecar_id = $1 AND auth_id = $2 ORDER BY id DESC LIMIT 1`, sidecarID, authID, wantHoldUntil).Scan(&status, &reason, &gotPrevious, &gotTarget, &holdUntilMatches); err != nil {
		t.Fatalf("load active hold: %v", err)
	}
	if status != "active" || !strings.HasPrefix(reason, "quota_exceeded") || gotPrevious != previousPriority || gotTarget != targetPriority || !holdUntilMatches {
		t.Fatalf("unexpected active hold status=%s reason=%s previous=%d target=%d holdUntilMatches=%v", status, reason, gotPrevious, gotTarget, holdUntilMatches)
	}
}

func sidecarIntegrationAssertReleasedHold(t *testing.T, conn *pgx.Conn, sidecarID int, authID string) {
	t.Helper()
	var count int
	if err := conn.QueryRow(context.Background(), `SELECT count(*) FROM sidecar_watchdog_holds WHERE sidecar_id = $1 AND auth_id = $2 AND status = 'released' AND released_at IS NOT NULL`, sidecarID, authID).Scan(&count); err != nil {
		t.Fatalf("count released hold: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected one released hold for %s, got %d", authID, count)
	}
}

func sidecarIntegrationAssertNoActiveHolds(t *testing.T, conn *pgx.Conn, sidecarID int) {
	t.Helper()
	var count int
	if err := conn.QueryRow(context.Background(), `SELECT count(*) FROM sidecar_watchdog_holds WHERE sidecar_id = $1 AND status = 'active'`, sidecarID).Scan(&count); err != nil {
		t.Fatalf("count active holds: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected no active holds, got %d", count)
	}
}

func sidecarIntegrationAssertProbeObservation(t *testing.T, conn *pgx.Conn, sidecarID int, authID string, status string, quotaExceeded bool, upstreamStatus int) {
	t.Helper()
	var count int
	if err := conn.QueryRow(context.Background(), `SELECT count(*) FROM sidecar_quota_probe_observations
WHERE sidecar_id = $1 AND auth_id = $2 AND probe_status = $3 AND quota_exceeded = $4
AND (($5 = 0 AND upstream_status_code IS NULL) OR upstream_status_code = $5)`, sidecarID, authID, status, quotaExceeded, upstreamStatus).Scan(&count); err != nil {
		t.Fatalf("count probe observations: %v", err)
	}
	if count == 0 {
		t.Fatalf("expected probe observation sidecar=%d auth=%s status=%s quota=%v upstream=%d", sidecarID, authID, status, quotaExceeded, upstreamStatus)
	}
}

func sidecarIntegrationAssertAction(t *testing.T, conn *pgx.Conn, sidecarID int, actionType string, status string) {
	t.Helper()
	var count int
	if err := conn.QueryRow(context.Background(), `SELECT count(*) FROM sidecar_watchdog_actions WHERE sidecar_id = $1 AND action_type = $2 AND status = $3`, sidecarID, actionType, status).Scan(&count); err != nil {
		t.Fatalf("count watchdog actions: %v", err)
	}
	if count == 0 {
		t.Fatalf("expected watchdog action %s status %s for sidecar %d", actionType, status, sidecarID)
	}
}

func sidecarIntegrationAssertActionCount(t *testing.T, conn *pgx.Conn, sidecarID int, actionType string, status string, want int) {
	t.Helper()
	var count int
	if err := conn.QueryRow(context.Background(), `SELECT count(*) FROM sidecar_watchdog_actions WHERE sidecar_id = $1 AND action_type = $2 AND status = $3`, sidecarID, actionType, status).Scan(&count); err != nil {
		t.Fatalf("count watchdog actions: %v", err)
	}
	if count != want {
		t.Fatalf("watchdog action %s status %s count = %d want %d for sidecar %d", actionType, status, count, want, sidecarID)
	}
}

func sidecarIntegrationAssertPendingActionCount(t *testing.T, conn *pgx.Conn, sidecarID int, want int) {
	t.Helper()
	var count int
	if err := conn.QueryRow(context.Background(), `SELECT count(*) FROM sidecar_watchdog_pending_actions WHERE sidecar_id = $1`, sidecarID).Scan(&count); err != nil {
		t.Fatalf("count pending watchdog actions: %v", err)
	}
	if count != want {
		t.Fatalf("pending watchdog action count = %d want %d for sidecar %d", count, want, sidecarID)
	}
}

func sidecarIntegrationAssertActionMissing(t *testing.T, conn *pgx.Conn, sidecarID int, actionType string) {
	t.Helper()
	var count int
	if err := conn.QueryRow(context.Background(), `SELECT count(*) FROM sidecar_watchdog_actions WHERE sidecar_id = $1 AND action_type = $2`, sidecarID, actionType).Scan(&count); err != nil {
		t.Fatalf("count missing watchdog actions: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected no watchdog action %s for sidecar %d, got %d", actionType, sidecarID, count)
	}
}

func sidecarIntegrationAssertPolicyCursorRemoved(t *testing.T, conn *pgx.Conn) {
	t.Helper()
	var exists bool
	if err := conn.QueryRow(context.Background(), `SELECT EXISTS (
		SELECT 1
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name = 'sidecar_watchdog_policies'
		  AND column_name = 'probe_cursor_auth_id'
	)`).Scan(&exists); err != nil {
		t.Fatalf("check probe cursor column absence: %v", err)
	}
	if exists {
		t.Fatalf("expected probe_cursor_auth_id to be removed from sidecar_watchdog_policies")
	}
}

func sidecarIntegrationAssertObservationRetained(t *testing.T, conn *pgx.Conn, sidecarID int, authID string, wantPresent bool) {
	t.Helper()
	var count int
	if err := conn.QueryRow(context.Background(), `SELECT count(*) FROM sidecar_quota_probe_observations WHERE sidecar_id = $1 AND auth_id = $2`, sidecarID, authID).Scan(&count); err != nil {
		t.Fatalf("count retained observations: %v", err)
	}
	if (count > 0) != wantPresent {
		t.Fatalf("observation %s present=%v want %v", authID, count > 0, wantPresent)
	}
}

type sidecarIntegrationAPICall struct {
	AuthIndex string            `json:"authIndex"`
	Method    string            `json:"method"`
	URL       string            `json:"url"`
	Header    map[string]string `json:"header"`
	Data      string            `json:"data"`
}

type sidecarIntegrationAPICallRequest struct {
	AuthIndexSnake string            `json:"auth_index"`
	AuthIndexCamel string            `json:"authIndex"`
	Method         string            `json:"method"`
	URL            string            `json:"url"`
	Header         map[string]string `json:"header"`
	Data           string            `json:"data"`
}

func (r sidecarIntegrationAPICallRequest) capture() sidecarIntegrationAPICall {
	authIndex := r.AuthIndexSnake
	if authIndex == "" {
		authIndex = r.AuthIndexCamel
	}
	return sidecarIntegrationAPICall{AuthIndex: authIndex, Method: r.Method, URL: r.URL, Header: r.Header, Data: r.Data}
}

type sidecarIntegrationAPICallResponse struct {
	StatusCode int
	Body       string
	Delay      time.Duration
}

type sidecarIntegrationUpstream struct {
	server       *httptest.Server
	mu           sync.Mutex
	fieldPatches []map[string]any
	apiCalls     []sidecarIntegrationAPICall
	authFiles    []map[string]any
	apiResponses map[string]sidecarIntegrationAPICallResponse
}

func newSidecarIntegrationUpstream(t *testing.T) *sidecarIntegrationUpstream {
	t.Helper()
	upstream := &sidecarIntegrationUpstream{apiResponses: map[string]sidecarIntegrationAPICallResponse{}}
	upstream.setAuthFiles(sidecarIntegrationAuthFixture())
	upstream.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstream.handle(t, w, r)
	}))
	return upstream
}

func (u *sidecarIntegrationUpstream) URL() string { return u.server.URL }

func (u *sidecarIntegrationUpstream) Close() { u.server.Close() }

func (u *sidecarIntegrationUpstream) setAuthFiles(files ...map[string]any) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.authFiles = make([]map[string]any, 0, len(files))
	for _, file := range files {
		u.authFiles = append(u.authFiles, sidecarIntegrationCloneMap(file))
	}
}

func (u *sidecarIntegrationUpstream) setAPICallResponse(authIndex string, response sidecarIntegrationAPICallResponse) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.apiResponses[authIndex] = response
}

func (u *sidecarIntegrationUpstream) handle(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	if got := r.Header.Get("X-Management-Key"); got != sidecarIntegrationManagementSecret {
		t.Errorf("expected management key header %q, got %q", sidecarIntegrationManagementSecret, got)
	}
	switch r.URL.Path {
	case "/v0/management/api-call":
		u.handleAPICall(t, w, r)
	case "/v0/management/auth-files":
		u.handleAuthFiles(w)
	case "/v0/management/gemini-api-key":
		sidecarIntegrationWriteJSON(w, map[string]any{"gemini-api-key": []any{sidecarIntegrationGeminiProviderFixture()}})
	case "/v0/management/openai-compatibility":
		sidecarIntegrationWriteJSON(w, map[string]any{"openai-compatibility": []any{sidecarIntegrationOpenAIProviderFixture()}})
	case "/v0/management/claude-api-key":
		sidecarIntegrationWriteJSON(w, map[string]any{"claude-api-key": []any{}})
	case "/v0/management/codex-api-key":
		sidecarIntegrationWriteJSON(w, map[string]any{"codex-api-key": []any{}})
	case "/v0/management/vertex-api-key":
		sidecarIntegrationWriteJSON(w, map[string]any{"vertex-api-key": []any{}})
	case "/v0/management/auth-files/fields":
		u.recordFieldPatch(t, r)
		sidecarIntegrationWriteJSON(w, map[string]any{"ok": true, "api_key": "mutation-secret", "management_password": "mutation-management-secret", "headers": map[string]any{"Authorization": "Bearer mutation-token"}, "body": "mutation-response-body", "account_id": "chatgpt-account-secret", "email": "mutation-user@example.test"})
	default:
		t.Errorf("unexpected sidecar integration upstream path %s", r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}
}

func (u *sidecarIntegrationUpstream) handleAuthFiles(w http.ResponseWriter) {
	u.mu.Lock()
	files := make([]any, 0, len(u.authFiles))
	for _, file := range u.authFiles {
		files = append(files, sidecarIntegrationCloneMap(file))
	}
	u.mu.Unlock()
	sidecarIntegrationWriteJSON(w, map[string]any{"files": files})
}

func (u *sidecarIntegrationUpstream) handleAPICall(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	if r.Method != http.MethodPost {
		t.Errorf("expected POST /api-call, got %s", r.Method)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var request sidecarIntegrationAPICallRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		t.Fatalf("decode api-call payload: %v", err)
	}
	call := request.capture()
	rawCall, err := json.Marshal(call)
	if err != nil {
		t.Fatalf("marshal api-call capture: %v", err)
	}
	sidecarIntegrationAssertNoLeaks(t, string(rawCall))
	u.mu.Lock()
	u.apiCalls = append(u.apiCalls, call)
	response, ok := u.apiResponses[call.AuthIndex]
	u.mu.Unlock()
	if !ok {
		response = sidecarIntegrationAPICallResponse{StatusCode: http.StatusOK, Body: sidecarIntegrationHealthyUsageBody()}
	}
	if response.Delay > 0 {
		time.Sleep(response.Delay)
	}
	sidecarIntegrationWriteJSON(w, map[string]any{"status_code": response.StatusCode, "header": map[string][]string{}, "body": response.Body})
}

func (u *sidecarIntegrationUpstream) recordFieldPatch(t *testing.T, r *http.Request) {
	t.Helper()
	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		t.Fatalf("decode field patch payload: %v", err)
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	u.fieldPatches = append(u.fieldPatches, payload)
	name, _ := payload["name"].(string)
	if name == "" || payload["priority"] == nil {
		return
	}
	priority := sidecarIntegrationInt(t, payload["priority"], "priority")
	for _, file := range u.authFiles {
		if file["name"] == name {
			file["priority"] = priority
			return
		}
	}
}

func (u *sidecarIntegrationUpstream) assertFieldPatch(t *testing.T, wantPriority int) {
	t.Helper()
	u.assertFieldPatchPriorities(t, []int{wantPriority})
}

func (u *sidecarIntegrationUpstream) assertFieldPatchPriorities(t *testing.T, want []int) {
	t.Helper()
	u.mu.Lock()
	defer u.mu.Unlock()
	if len(u.fieldPatches) != len(want) {
		t.Fatalf("field patch priorities count = %d want %d patches=%+v", len(u.fieldPatches), len(want), u.fieldPatches)
	}
	for i, wantPriority := range want {
		if got := sidecarIntegrationInt(t, u.fieldPatches[i]["priority"], "priority"); got != wantPriority {
			t.Fatalf("field patch[%d] priority = %d want %d patches=%+v", i, got, wantPriority, u.fieldPatches)
		}
	}
}

func (u *sidecarIntegrationUpstream) assertAPICalls(t *testing.T, wantAuthIndexes []string) {
	t.Helper()
	u.mu.Lock()
	defer u.mu.Unlock()
	if len(u.apiCalls) != len(wantAuthIndexes) {
		t.Fatalf("api-call count = %d want %d calls=%+v", len(u.apiCalls), len(wantAuthIndexes), u.apiCalls)
	}
	for i, want := range wantAuthIndexes {
		call := u.apiCalls[i]
		if call.AuthIndex != want {
			t.Fatalf("api-call[%d] authIndex = %q want %q calls=%+v", i, call.AuthIndex, want, u.apiCalls)
		}
		if call.Method != http.MethodGet || call.URL != "https://chatgpt.com/backend-api/wham/usage" {
			t.Fatalf("api-call[%d] request target = %s %s", i, call.Method, call.URL)
		}
		if call.Header["Authorization"] != "Bearer $TOKEN$" || call.Header["Content-Type"] != "application/json" || call.Header["User-Agent"] == "" {
			t.Fatalf("api-call[%d] did not use safe bearer-token placeholder headers: %+v", i, call.Header)
		}
		for key := range call.Header {
			if strings.EqualFold(key, "Cookie") || strings.EqualFold(key, "Chatgpt-Account-Id") {
				t.Fatalf("api-call[%d] leaked forbidden identity header %q", i, key)
			}
		}
	}
}

func (u *sidecarIntegrationUpstream) waitForAPICallCount(t *testing.T, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		u.mu.Lock()
		got := len(u.apiCalls)
		u.mu.Unlock()
		if got >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	u.mu.Lock()
	calls := append([]sidecarIntegrationAPICall(nil), u.apiCalls...)
	u.mu.Unlock()
	t.Fatalf("timed out waiting for %d api calls, got %+v", want, calls)
}

func sidecarIntegrationWriteJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

func sidecarIntegrationCloneMap(value map[string]any) map[string]any {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	var cloned map[string]any
	if err := json.Unmarshal(raw, &cloned); err != nil {
		panic(err)
	}
	return cloned
}

func sidecarIntegrationAuthFixture() map[string]any {
	return sidecarIntegrationAuthFixtureWith("auth-gemini-primary", "auth_001", "gemini", 10)
}

func sidecarIntegrationAuthFixtureWith(authID string, authIndex string, provider string, priority int) map[string]any {
	return map[string]any{
		"id": authID, "auth_index": authIndex, "name": authID + ".json", "provider": provider, "label": authID, "status": "active", "disabled": false, "priority": priority,
		"quota": map[string]any{"exceeded": false, "reason": "", "next_recover_at": nil}, "recent_requests": []any{map[string]any{"success": 4, "failed": 0}}, "model_states": map[string]any{"default": map[string]any{"status": "active", "api_key": "raw-auth-token"}},
		"api_key": "raw-auth-secret",
	}
}

func sidecarIntegrationCodexAuthFixture(authID string, authIndex string, priority int) map[string]any {
	fixture := sidecarIntegrationAuthFixtureWith(authID, authIndex, "codex", priority)
	fixture["api_key"] = "raw-codex-secret"
	fixture["model_states"] = map[string]any{"codex": map[string]any{"status": "active", "api_key": "raw-codex-token"}}
	fixture["headers"] = map[string]any{"Cookie": "probe-cookie-secret", "Chatgpt-Account-Id": "chatgpt-account-secret"}
	return fixture
}

func sidecarIntegrationGeminiProviderFixture() map[string]any {
	return map[string]any{"api-key": "raw-provider-secret", "priority": 10, "prefix": "team-a/", "auth-index": "auth_001", "proxy-url": "http://proxy-secret.invalid", "headers": map[string]any{"Authorization": "Bearer raw-provider-token", "X-API-Key": "raw-header-key"}}
}

func sidecarIntegrationOpenAIProviderFixture() map[string]any {
	return map[string]any{"name": "compat", "priority": 5, "api-key-entries": []any{map[string]any{"api-key": "raw-openai-secret", "auth-index": "auth_openai", "headers": map[string]any{"Authorization": "Bearer raw-openai-token"}}}}
}

func sidecarIntegrationHealthyUsageBody() string {
	return `{"rate_limit":{"allowed":true,"primary_window":{"used_percent":5,"limit_window_seconds":18000}}}`
}

func sidecarIntegrationExhaustedUsageBody(t *testing.T, resetAt time.Time) string {
	t.Helper()
	payload := map[string]any{"rate_limit": map[string]any{"allowed": false, "primary_window": map[string]any{"limit_reached": true, "limit_window_seconds": 18000, "reset_at": resetAt.Unix()}}}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal exhausted usage body: %v", err)
	}
	return string(raw)
}
