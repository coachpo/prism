package runtimetest

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
)

// TestRuntimeTelemetryPoisonRowIsQuarantined pins the behaviour that was
// missing when a single un-insertable usage event stopped all recording for an
// instance: the materializer retried it several times a second forever, its
// attempt counter never moved because the accounting rolled back with the
// failed transaction, and every later row sat behind it.
//
// A permanent database error must retire the row into quarantine so the queue
// keeps moving.
func TestRuntimeTelemetryPoisonRowIsQuarantined(t *testing.T) {
	var poison atomic.Bool
	poison.Store(true)
	harness := newRuntimeHarnessWithConfig(t, runtimeHarnessConfig{
		RuntimeOptions: runtimeapi.Options{
			TelemetryOutbox: runtimeapi.TelemetryOutboxOptions{
				WorkerCount:  1,
				PollInterval: 25 * time.Millisecond,
				Hooks: &runtimeapi.TelemetryOutboxHooks{
					BeforeMaterialize: func(context.Context) error {
						if poison.Load() {
							// SQLSTATE 23514 is a check-constraint violation:
							// waiting can never make this row insertable.
							return &pgconn.PgError{Code: "23514", ConstraintName: "ck_test_poison"}
						}
						return nil
					},
				},
			},
		},
	})
	profileID := harness.activeProfileID(t)
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "poison-public-" + randomSuffix(),
		TargetModelID:   "poison-target-" + randomSuffix(),
		EndpointBaseURL: harness.upstream.baseURL("/poison/quarantine"),
		EndpointAPIKey:  "poison-upstream-key",
	})

	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "poison row"}},
		"model":    route.PublicModelID,
	}, nil)
	assertStatus(t, response, http.StatusOK)

	waitForQuarantinedTelemetry(t, harness, 1, 20*time.Second)

	// With the poison retired, an ordinary request must record normally: this
	// is the head-of-line blocking half of the regression.
	poison.Store(false)
	followUp := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "after the poison"}},
		"model":    route.PublicModelID,
	}, nil)
	assertStatus(t, followUp, http.StatusOK)

	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 20*time.Second)
}

// TestRuntimeTelemetryTransientFailureIsRetriedWithAccounting pins the other
// half: a failure that might resolve on its own must be counted and backed
// off, never retried in a hot loop with a counter stuck at zero.
func TestRuntimeTelemetryTransientFailureIsRetriedWithAccounting(t *testing.T) {
	var failing atomic.Bool
	failing.Store(true)
	harness := newRuntimeHarnessWithConfig(t, runtimeHarnessConfig{
		RuntimeOptions: runtimeapi.Options{
			TelemetryOutbox: runtimeapi.TelemetryOutboxOptions{
				WorkerCount:  1,
				PollInterval: 25 * time.Millisecond,
				Hooks: &runtimeapi.TelemetryOutboxHooks{
					BeforeMaterialize: func(context.Context) error {
						if failing.Load() {
							// SQLSTATE 40001 is a serialization failure, which
							// is exactly the kind of error retrying resolves.
							return &pgconn.PgError{Code: "40001"}
						}
						return nil
					},
				},
			},
		},
	})
	profileID := harness.activeProfileID(t)
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "transient-public-" + randomSuffix(),
		TargetModelID:   "transient-target-" + randomSuffix(),
		EndpointBaseURL: harness.upstream.baseURL("/poison/transient"),
		EndpointAPIKey:  "transient-upstream-key",
	})

	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "transient failure"}},
		"model":    route.PublicModelID,
	}, nil)
	assertStatus(t, response, http.StatusOK)

	waitForOutboxAttemptAccounting(t, harness, 20*time.Second)

	// Once the transient cause clears, the row must still materialize rather
	// than having been discarded.
	failing.Store(false)
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 30*time.Second)
}

func waitForQuarantinedTelemetry(t *testing.T, harness *runtimeHarness, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var got int
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := harness.conn.QueryRow(ctx, `SELECT count(*) FROM runtime_telemetry_quarantine`).Scan(&got)
		cancel()
		if err != nil {
			t.Fatalf("count quarantined telemetry: %v", err)
		}
		if got >= want {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("telemetry quarantine holds %d row(s), want at least %d; an un-insertable row is being retried forever instead of retired", got, want)
}

func waitForOutboxAttemptAccounting(t *testing.T, harness *runtimeHarness, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var attempts int
	var safeCode *string
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := harness.conn.QueryRow(ctx, `
			SELECT core_attempt_count, core_last_safe_error_code
			FROM runtime_telemetry_outbox ORDER BY id LIMIT 1`).Scan(&attempts, &safeCode)
		cancel()
		if err == nil && attempts > 0 {
			if safeCode == nil || *safeCode == "" {
				t.Fatalf("attempt %d was counted without a safe error code", attempts)
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("outbox attempt counter never advanced past 0; failed attempts are not being accounted for, so the row can never be retired")
}

// waitForQuarantinedRows polls until the expected number of rows has been
// retired into quarantine for a profile.
func waitForQuarantinedRows(t *testing.T, conn *pgx.Conn, profileID int, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var got int
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := conn.QueryRow(ctx, `SELECT count(*) FROM runtime_telemetry_quarantine WHERE profile_id = $1`, profileID).Scan(&got)
		cancel()
		if err != nil {
			t.Fatalf("count quarantined telemetry for profile %d: %v", profileID, err)
		}
		if got >= want {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("quarantine holds %d row(s) for profile %d, want at least %d", got, profileID, want)
}
