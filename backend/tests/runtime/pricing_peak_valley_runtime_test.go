package runtimetest

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"net/http"
	"testing"
	"time"

	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
	"github.com/jackc/pgx/v5"
)

func TestRuntimePeakValleyRealRequestsPersistSelectedCardEvidence(t *testing.T) {
	currentNow := time.Date(2026, time.August, 10, 10, 0, 0, 0, time.UTC) // Monday, peak window.
	harness := newRuntimeHarnessWithConfig(t, runtimeHarnessConfig{
		RuntimeOptions: runtimeapi.Options{Now: func() time.Time { return currentNow }},
	})
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	upstream := newScriptedUpstream(t, http.StatusOK, map[string]any{
		"id":    "chatcmpl-peak-valley-" + suffix,
		"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15},
	})
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID: profileID, APIFamily: "openai", PublicModelID: "peak-valley-public-" + suffix,
		TargetModelID: "peak-valley-target-" + suffix, EndpointBaseURL: upstream.baseURL("/pricing/peak-valley"), EndpointAPIKey: "peak-valley-key-" + suffix,
	})
	templateID := seedPeakValleyRuntimeTemplate(t, harness.conn, profileID, "peak-valley-template-"+suffix, loadRuntimeReportCurrencyCode(t, harness.conn, profileID), "UTC")
	attachRuntimeConnectionPricingTemplate(t, harness, route.ConnectionID, templateID)

	request := func(content string) {
		response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
			"model": route.PublicModelID, "messages": []map[string]any{{"role": "user", "content": content}},
		}, nil)
		assertStatus(t, response, http.StatusOK)
	}
	request("peak request")
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)
	assertPeakValleyEvidenceRow(t, harness.conn, profileID, "peak", "10", currentNow)

	currentNow = time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC) // half-open end: offpeak.
	request("offpeak request")
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 2, UsageEvents: 2, OutboxRows: 0}, 5*time.Second)
	assertPeakValleyEvidenceRow(t, harness.conn, profileID, "offpeak", "1", currentNow)
}

func TestRuntimePeakValleyInvalidTimezonePersistsUnresolvedEvidence(t *testing.T) {
	currentNow := time.Date(2026, time.August, 10, 10, 0, 0, 0, time.UTC)
	harness := newRuntimeHarnessWithConfig(t, runtimeHarnessConfig{RuntimeOptions: runtimeapi.Options{Now: func() time.Time { return currentNow }}})
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	upstream := newScriptedUpstream(t, http.StatusOK, map[string]any{
		"id":    "chatcmpl-peak-valley-unresolved-" + suffix,
		"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15},
	})
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID: profileID, APIFamily: "openai", PublicModelID: "peak-valley-unresolved-public-" + suffix,
		TargetModelID: "peak-valley-unresolved-target-" + suffix, EndpointBaseURL: upstream.baseURL("/pricing/peak-valley-unresolved"), EndpointAPIKey: "peak-valley-unresolved-fixture-" + suffix,
	})
	templateID := seedPeakValleyRuntimeTemplate(t, harness.conn, profileID, "peak-valley-unresolved-template-"+suffix, loadRuntimeReportCurrencyCode(t, harness.conn, profileID), "Not/AZone")
	attachRuntimeConnectionPricingTemplate(t, harness, route.ConnectionID, templateID)
	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"model": route.PublicModelID, "messages": []map[string]any{{"role": "user", "content": "unresolved schedule"}},
	}, nil)
	assertStatus(t, response, http.StatusOK)
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)

	for _, table := range []string{"request_logs", "usage_request_events"} {
		var status, reason, resolution, kind, state, timezone string
		var role, snapshot sql.NullString
		var decidedAt time.Time
		var localWeekday, localMinute sql.NullInt32
		query := `SELECT pricing_status, unpriced_reason, pricing_resolution_kind, pricing_template_kind, pricing_selection_state, pricing_card_role, pricing_snapshot_input, pricing_schedule_decided_at, pricing_schedule_timezone, pricing_schedule_local_weekday, pricing_schedule_local_minute FROM ` + table + ` WHERE profile_id = $1 ORDER BY created_at DESC, id DESC LIMIT 1`
		if err := harness.conn.QueryRow(context.Background(), query, profileID).Scan(&status, &reason, &resolution, &kind, &state, &role, &snapshot, &decidedAt, &timezone, &localWeekday, &localMinute); err != nil {
			t.Fatalf("load unresolved pricing evidence from %s: %v", table, err)
		}
		if status != "unpriced" || reason != "MISSING_PRICE_DATA" || resolution != "schedule_unresolved" || kind != "peak_valley" || state != "unresolved" || role.Valid || snapshot.Valid || !decidedAt.Equal(currentNow) || timezone != "Not/AZone" || localWeekday.Valid || localMinute.Valid {
			t.Fatalf("unexpected unresolved pricing evidence in %s: %s/%s/%s/%s/%s role=%v snapshot=%v decided=%s timezone=%s local=%v/%v", table, status, reason, resolution, kind, state, role, snapshot, decidedAt, timezone, localWeekday, localMinute)
		}
	}
}

func TestRuntimeSoftDeletedPricingTemplateIsNotPublished(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	upstream := newScriptedUpstream(t, http.StatusOK, map[string]any{
		"id":    "chatcmpl-soft-deleted-pricing-" + suffix,
		"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15},
	})
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID: profileID, APIFamily: "openai", PublicModelID: "soft-deleted-pricing-public-" + suffix,
		TargetModelID: "soft-deleted-pricing-target-" + suffix, EndpointBaseURL: upstream.baseURL("/pricing/soft-deleted"), EndpointAPIKey: "soft-deleted-pricing-fixture-" + suffix,
	})
	templateID := insertRuntimePricingTemplate(t, harness.conn, profileID, "soft-deleted-pricing-template-"+suffix, "", "2", "5", "0", "0", "0")
	attachRuntimeConnectionPricingTemplate(t, harness, route.ConnectionID, templateID)
	if _, err := harness.conn.Exec(context.Background(), `UPDATE pricing_templates SET deleted_at = NOW(), updated_at = NOW() WHERE id = $1`, templateID); err != nil {
		t.Fatalf("soft-delete pricing template fixture: %v", err)
	}
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})
	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"model": route.PublicModelID, "messages": []map[string]any{{"role": "user", "content": "soft deleted template"}},
	}, nil)
	assertStatus(t, response, http.StatusOK)
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)
	var status, reason string
	var kind, role, snapshot sql.NullString
	if err := harness.conn.QueryRow(context.Background(), `SELECT pricing_status, unpriced_reason, pricing_template_kind, pricing_card_role, pricing_snapshot_input FROM usage_request_events WHERE profile_id = $1 ORDER BY created_at DESC, id DESC LIMIT 1`, profileID).Scan(&status, &reason, &kind, &role, &snapshot); err != nil {
		t.Fatalf("load soft-deleted pricing result: %v", err)
	}
	if status != "unpriced" || reason != "PRICING_DISABLED" || kind.Valid || role.Valid || snapshot.Valid {
		t.Fatalf("soft-deleted template leaked into runtime pricing: status=%q reason=%q kind=%v role=%v snapshot=%v", status, reason, kind, role, snapshot)
	}
}

func seedPeakValleyRuntimeTemplate(t *testing.T, conn *pgx.Conn, profileID int, name, currency, timezone string) int {
	t.Helper()
	tx, err := conn.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin peak-valley fixture transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	now := time.Now().UTC()
	windowBytes := []byte("1,600,720")
	digest := sha256.Sum256(windowBytes)
	var templateID, revisionID int
	if err := tx.QueryRow(context.Background(), `INSERT INTO pricing_templates (profile_id, name, description, created_at, updated_at, current_revision_id, deleted_at) VALUES ($1, $2, NULL, $3, $3, NULL, NULL) RETURNING id`, profileID, name, now).Scan(&templateID); err != nil {
		t.Fatalf("insert peak-valley template: %v", err)
	}
	if err := tx.QueryRow(context.Background(), `INSERT INTO pricing_template_revisions (template_id, version, pricing_unit, currency_code, reporting_currency_epoch_id, reporting_currency_epoch, currency_attribution, template_kind, pricing_schedule_timezone, pricing_schedule_digest, effective_at, created_at, created_by_kind, created_by_operation_id) SELECT $1, 1, 'PER_1M', $2, settings.current_reporting_currency_epoch_id, epochs.epoch, 'active_epoch', 'peak_valley', $3, $4, $5, $5, 'legacy_backfill', NULL FROM user_settings settings JOIN reporting_currency_epochs epochs ON epochs.id = settings.current_reporting_currency_epoch_id WHERE settings.profile_id = $6 RETURNING id`, templateID, currency, timezone, hex.EncodeToString(digest[:]), now, profileID).Scan(&revisionID); err != nil {
		t.Fatalf("insert peak-valley revision: %v", err)
	}
	for _, card := range []struct{ role, input, output string }{{"peak", "10", "20"}, {"offpeak", "1", "2"}} {
		if _, err := tx.Exec(context.Background(), `INSERT INTO pricing_template_cards (revision_id, template_kind, card_role, input_price, output_price, cached_input_price, cache_creation_price, reasoning_price) VALUES ($1, 'peak_valley', $2, $3, $4, '0', '0', '0')`, revisionID, card.role, card.input, card.output); err != nil {
			t.Fatalf("insert %s card: %v", card.role, err)
		}
	}
	if _, err := tx.Exec(context.Background(), `INSERT INTO pricing_template_windows (revision_id, weekday_mask, start_minute, end_minute, created_at) VALUES ($1, 1, 600, 720, $2)`, revisionID, now); err != nil {
		t.Fatalf("insert peak window: %v", err)
	}
	if _, err := tx.Exec(context.Background(), `UPDATE pricing_templates SET current_revision_id = $1, updated_at = $2 WHERE id = $3`, revisionID, now, templateID); err != nil {
		t.Fatalf("attach peak-valley revision: %v", err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatalf("commit peak-valley fixture: %v", err)
	}
	return templateID
}

func assertPeakValleyEvidenceRow(t *testing.T, conn *pgx.Conn, profileID int, wantRole, wantInput string, wantAt time.Time) {
	t.Helper()
	var kind, state, role, input, timezone, digest string
	var decidedAt, createdAt time.Time
	var localWeekday, localMinute int
	if err := conn.QueryRow(context.Background(), `SELECT pricing_template_kind, pricing_selection_state, pricing_card_role, pricing_snapshot_input, pricing_schedule_decided_at, pricing_schedule_timezone, pricing_schedule_local_weekday, pricing_schedule_local_minute, pricing_schedule_digest, created_at FROM request_logs WHERE profile_id = $1 ORDER BY created_at DESC, id DESC LIMIT 1`, profileID).Scan(&kind, &state, &role, &input, &decidedAt, &timezone, &localWeekday, &localMinute, &digest, &createdAt); err != nil {
		t.Fatalf("load peak-valley evidence: %v", err)
	}
	if kind != "peak_valley" || state != "selected" || role != wantRole || input != wantInput || !decidedAt.UTC().Equal(wantAt.UTC()) || timezone != "UTC" || localWeekday != 1 || localMinute != wantAt.Hour()*60+wantAt.Minute() || digest != "d0988c249c83375fda16cecdff70de05bdf8af51528c0797665dc33f62d422cc" {
		t.Fatalf("unexpected peak-valley evidence kind=%q state=%q role=%q input=%q decided_at=%s local=%d/%d", kind, state, role, input, decidedAt.UTC().Format(time.RFC3339), localWeekday, localMinute)
	}
	var usageKind, usageState, usageRole, usageTimezone, usageDigest string
	var usageDecidedAt time.Time
	var usageWeekday, usageMinute int
	if err := conn.QueryRow(context.Background(), `SELECT pricing_template_kind, pricing_selection_state, pricing_card_role, pricing_schedule_decided_at, pricing_schedule_timezone, pricing_schedule_local_weekday, pricing_schedule_local_minute, pricing_schedule_digest FROM usage_request_events WHERE profile_id = $1 ORDER BY created_at DESC, id DESC LIMIT 1`, profileID).Scan(&usageKind, &usageState, &usageRole, &usageDecidedAt, &usageTimezone, &usageWeekday, &usageMinute, &usageDigest); err != nil {
		t.Fatalf("load usage peak-valley evidence: %v", err)
	}
	if usageKind != kind || usageState != state || usageRole != role || !usageDecidedAt.UTC().Equal(decidedAt.UTC()) || usageTimezone != timezone || usageWeekday != localWeekday || usageMinute != localMinute || usageDigest != digest {
		t.Fatalf("telemetry evidence asymmetry request=%s/%s/%s/%s/%s usage=%s/%s/%s/%s/%s", kind, state, role, digest, timezone, usageKind, usageState, usageRole, usageDigest, usageTimezone)
	}
	var totalCost int64
	if err := conn.QueryRow(context.Background(), `SELECT total_cost_user_currency_micros FROM usage_request_events WHERE profile_id = $1 ORDER BY created_at DESC, id DESC LIMIT 1`, profileID).Scan(&totalCost); err != nil {
		t.Fatalf("load usage peak-valley cost: %v", err)
	}
	t.Logf("SQL evidence kind=%s state=%s role=%s input_snapshot=%s total_cost_user_currency_micros=%d created_at=%s pricing_schedule_decided_at=%s timezone=%s local_weekday=%d local_minute=%d digest=%s", kind, state, role, input, totalCost, createdAt.UTC().Format(time.RFC3339), decidedAt.UTC().Format(time.RFC3339), timezone, localWeekday, localMinute, digest)
}
