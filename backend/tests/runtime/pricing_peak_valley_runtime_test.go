package runtimetest

import (
	"context"
	"crypto/sha256"
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
	templateID := seedPeakValleyRuntimeTemplate(t, harness.conn, profileID, "peak-valley-template-"+suffix, loadRuntimeReportCurrencyCode(t, harness.conn, profileID))
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

func seedPeakValleyRuntimeTemplate(t *testing.T, conn *pgx.Conn, profileID int, name, currency string) int {
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
	if err := tx.QueryRow(context.Background(), `INSERT INTO pricing_template_revisions (template_id, version, pricing_unit, currency_code, reporting_currency_epoch_id, reporting_currency_epoch, currency_attribution, template_kind, pricing_schedule_timezone, pricing_schedule_digest, effective_at, created_at, created_by_kind, created_by_operation_id) SELECT $1, 1, 'PER_1M', $2, settings.current_reporting_currency_epoch_id, epochs.epoch, 'active_epoch', 'peak_valley', 'UTC', $3, $4, $4, 'legacy_backfill', NULL FROM user_settings settings JOIN reporting_currency_epochs epochs ON epochs.id = settings.current_reporting_currency_epoch_id WHERE settings.profile_id = $5 RETURNING id`, templateID, currency, hex.EncodeToString(digest[:]), now, profileID).Scan(&revisionID); err != nil {
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
