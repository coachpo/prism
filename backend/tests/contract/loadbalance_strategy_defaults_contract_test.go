package contracttest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	loadbalancedomain "github.com/coachpo/prism/backend/internal/domain/loadbalance"
	managementloadbalance "github.com/coachpo/prism/backend/internal/httpapi/management/loadbalance"
	managementsettings "github.com/coachpo/prism/backend/internal/httpapi/management/settings"
	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
	"github.com/coachpo/prism/backend/internal/platform/config"
	platformhttp "github.com/coachpo/prism/backend/internal/platform/http"
)

// newLoadbalanceContractHarness builds a contract harness that mounts the
// loadbalance management service alongside the base dependencies, sharing one
// process-local runtime state store with a runtime service (so tests can seed
// observations exactly like the s15 harness).
func newLoadbalanceContractHarness(t *testing.T) *contractHarness {
	t.Helper()
	return newContractHarnessFor(t, "loadbalance-contract", contractHarnessOptions{
		DatabaseURL:         "",
		SecretEncryptionKey: "contract-secret",
		Version:             "loadbalance-contract-test",
		SettingsMutator: func(settings *config.Settings) {
			authSettings := contractAuthSettings()
			authSettings.DatabaseURL = settings.DatabaseURL
			*settings = authSettings
		},
		DependenciesBuilder: func(t *testing.T, testContext context.Context, harness *contractHarness, settings config.Settings, pool *pgxpool.Pool) platformhttp.Dependencies {
			t.Helper()
			runtimeCache := runtimeapi.NewSharedCacheWithOptions(runtimeapi.SharedCacheOptions{RefreshPool: pool, SecretEncryptionKey: settings.SecretEncryptionKey})
			if err := runtimeCache.Bootstrap(testContext); err != nil {
				t.Fatalf("bootstrap published runtime snapshot: %v", err)
			}
			runtimeState := loadbalancedomain.NewLocalRuntimeStateStore()
			loadbalanceService, err := managementloadbalance.NewService(settings, managementloadbalance.Options{Pool: pool, RuntimeState: runtimeState})
			if err != nil {
				t.Fatalf("build loadbalance service: %v", err)
			}
			t.Cleanup(loadbalanceService.Close)
			settingsService, err := managementsettings.NewService(settings, managementsettings.Options{Pool: pool})
			if err != nil {
				t.Fatalf("build settings service: %v", err)
			}
			t.Cleanup(settingsService.Close)
			telemetryPool, err := pgxpool.New(testContext, settings.DatabaseURL)
			if err != nil {
				t.Fatalf("create runtime telemetry pool: %v", err)
			}
			t.Cleanup(telemetryPool.Close)
			feedbackPool, err := pgxpool.New(testContext, settings.DatabaseURL)
			if err != nil {
				t.Fatalf("create runtime feedback pool: %v", err)
			}
			t.Cleanup(feedbackPool.Close)
			runtimeService, err := runtimeapi.NewService(settings, runtimeapi.Options{ExecutionPool: pool, TelemetryPool: telemetryPool, FeedbackPool: feedbackPool, Cache: runtimeCache, RuntimeState: runtimeState})
			if err != nil {
				t.Fatalf("build runtime service: %v", err)
			}
			t.Cleanup(runtimeService.Close)
			harness.runtimeCache = runtimeCache
			harness.runtimeService = runtimeService
			return platformhttp.Dependencies{
				LoadbalanceService: loadbalanceService,
				RuntimeCache:       runtimeCache,
				RuntimeState:       runtimeState,
				RuntimeService:     runtimeService,
				SettingsService:    settingsService,
			}
		},
	})
}

type strategyResponseContract struct {
	ID                                 int     `json:"id"`
	Name                               string  `json:"name"`
	LegacyStrategyType                 string  `json:"legacy_strategy_type"`
	IsDefault                          bool    `json:"is_default"`
	FailureStatusCodes                 []int   `json:"failure_status_codes"`
	BanMode                            string  `json:"ban_mode"`
	RetryBaseDelayMS                   int     `json:"retry_base_delay_ms"`
	RetryBackoffMultiplier             float64 `json:"retry_backoff_multiplier"`
	RetryJitterRatio                   float64 `json:"retry_jitter_ratio"`
	RetryMaxDelayMS                    int     `json:"retry_max_delay_ms"`
	CycleRetryAttemptLimit             int     `json:"cycle_retry_attempt_limit"`
	BanCumulativeRetryAttemptThreshold int     `json:"ban_cumulative_retry_attempt_threshold"`
	BanDurationSeconds                 int     `json:"ban_duration_seconds"`
	AttachedModelCount                 int     `json:"attached_model_count"`
	CreatedAt                          string  `json:"created_at"`
	UpdatedAt                          string  `json:"updated_at"`
}

type strategyDefaultsResponseContract struct {
	Created           []map[string]any `json:"created"`
	Existing          []map[string]any `json:"existing"`
	DefaultStrategyID *int             `json:"default_strategy_id"`
	DefaultChanged    bool             `json:"default_changed"`
	Complete          bool             `json:"complete"`
}

func strategyContractFromResponse(t *testing.T, response *http.Response) strategyResponseContract {
	t.Helper()
	var item strategyResponseContract
	decodeContractResponse(t, response, &item)
	return item
}

func decodeContractResponse(t *testing.T, response *http.Response, target any) {
	t.Helper()
	defer response.Body.Close()
	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		t.Fatalf("decode contract response: %v", err)
	}
}

func readProfilePlanningGeneration(t *testing.T, harness *contractHarness, profileID int) int64 {
	t.Helper()
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO runtime_cache_generations (domain, scope_type, scope_id, version, reason) VALUES ('runtime_planning', 'profile', $1, 0, 'lazy_init') ON CONFLICT (domain, scope_type, scope_id) DO NOTHING`, fmt.Sprintf("%d", profileID)); err != nil {
		t.Fatalf("ensure profile planning generation: %v", err)
	}
	var generation int64
	if err := harness.conn.QueryRow(context.Background(), `SELECT version FROM runtime_cache_generations WHERE domain = 'runtime_planning' AND scope_type = 'profile' AND scope_id = $1`, fmt.Sprintf("%d", profileID)).Scan(&generation); err != nil {
		t.Fatalf("read profile planning generation: %v", err)
	}
	return generation
}

func loadStrategyListContract(t *testing.T, harness *contractHarness, profileID int) []strategyResponseContract {
	t.Helper()
	response := harness.requestJSON(t, harness.client, http.MethodGet, "/api/loadbalance/strategies", nil, modelHeader(profileID))
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected strategy list 200, got %d", response.StatusCode)
	}
	var payload []strategyResponseContract
	decodeContractResponse(t, response, &payload)
	return payload
}

func TestStrategyExplicitDefaultContract(t *testing.T) {
	harness := newLoadbalanceContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)

	items := loadStrategyListContract(t, harness, profileID)
	if len(items) != 3 {
		t.Fatalf("expected startup to seed 3 canonical strategies, got %d", len(items))
	}
	// Stable presentation order: is_default DESC, id ASC; the default is the
	// canonical fill-first row regardless of creation order.
	if !items[0].IsDefault {
		t.Fatalf("expected first list item to be the default, got %+v", items[0])
	}
	if items[0].Name != "Default fill-first routing" || items[0].LegacyStrategyType != "fill-first" {
		t.Fatalf("expected canonical fill-first as default, got %+v", items[0])
	}
	defaultCount := 0
	for _, item := range items {
		if item.IsDefault {
			defaultCount++
		}
	}
	if defaultCount != 1 {
		t.Fatalf("expected exactly one default, got %d", defaultCount)
	}
	defaultStrategyID := items[0].ID

	// Ordinary create must not accept is_default in the body.
	rejected := harness.requestJSON(t, harness.client, http.MethodPost, "/api/loadbalance/strategies", map[string]any{
		"name": "Hacky Default", "legacy_strategy_type": "single", "is_default": true,
	}, modelHeader(profileID))
	if rejected.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected create with is_default to be rejected with 400, got %d", rejected.StatusCode)
	}

	// Create a non-default strategy; the default must not move.
	created := harness.requestJSON(t, harness.client, http.MethodPost, "/api/loadbalance/strategies", map[string]any{
		"name": "Custom A", "legacy_strategy_type": "round-robin",
	}, modelHeader(profileID))
	if created.StatusCode != http.StatusCreated {
		t.Fatalf("expected create 201, got %d", created.StatusCode)
	}
	custom := strategyContractFromResponse(t, created)
	if custom.IsDefault {
		t.Fatalf("expected ordinary create to be non-default, got %+v", custom)
	}

	// Editing/renaming a non-default strategy must not change the default ID
	// and must not flip is_default.
	updated := harness.requestJSON(t, harness.client, http.MethodPut, fmt.Sprintf("/api/loadbalance/strategies/%d", custom.ID), map[string]any{
		"name": "Custom A renamed", "legacy_strategy_type": "round-robin",
	}, modelHeader(profileID))
	if updated.StatusCode != http.StatusOK {
		t.Fatalf("expected update 200, got %d", updated.StatusCode)
	}
	afterUpdate := strategyContractFromResponse(t, updated)
	if afterUpdate.IsDefault || afterUpdate.Name != "Custom A renamed" {
		t.Fatalf("expected rename to keep non-default, got %+v", afterUpdate)
	}
	items = loadStrategyListContract(t, harness, profileID)
	if items[0].ID != defaultStrategyID || !items[0].IsDefault {
		t.Fatalf("expected default ID to stay stable after rename, got %+v", items[0])
	}

	// Set-default CAS: stale expected value conflicts with a map detail that
	// carries the current default id for UI recovery.
	stale := harness.requestJSON(t, harness.client, http.MethodPut, fmt.Sprintf("/api/loadbalance/strategies/%d/default", custom.ID), map[string]any{
		"expected_default_strategy_id": 9999,
	}, modelHeader(profileID))
	if stale.StatusCode != http.StatusConflict {
		t.Fatalf("expected stale CAS 409, got %d", stale.StatusCode)
	}
	var stalePayload struct {
		Code   string `json:"code"`
		Detail struct {
			Message                  string `json:"message"`
			CurrentDefaultStrategyID *int   `json:"current_default_strategy_id"`
		} `json:"detail"`
	}
	decodeContractResponse(t, stale, &stalePayload)
	if stalePayload.Code != "default_strategy_changed" || stalePayload.Detail.Message != "Default loadbalance strategy changed since the request was prepared; reload and confirm again" || stalePayload.Detail.CurrentDefaultStrategyID == nil || *stalePayload.Detail.CurrentDefaultStrategyID != defaultStrategyID {
		t.Fatalf("unexpected stale CAS payload %+v", stalePayload)
	}

	// Set-default does not bump the profile planning generation: capture the
	// generation before the atomic switch and compare after all default
	// mutations (including the idempotent replay).
	generation := readProfilePlanningGeneration(t, harness, profileID)

	// Correct CAS: atomic switch, previous default reported.
	switched := harness.requestJSON(t, harness.client, http.MethodPut, fmt.Sprintf("/api/loadbalance/strategies/%d/default", custom.ID), map[string]any{
		"expected_default_strategy_id": defaultStrategyID,
	}, modelHeader(profileID))
	if switched.StatusCode != http.StatusOK {
		t.Fatalf("expected set-default 200, got %d", switched.StatusCode)
	}
	var switchedPayload struct {
		DefaultStrategyID         int  `json:"default_strategy_id"`
		PreviousDefaultStrategyID *int `json:"previous_default_strategy_id"`
		Changed                   bool `json:"changed"`
	}
	decodeContractResponse(t, switched, &switchedPayload)
	if !switchedPayload.Changed || switchedPayload.DefaultStrategyID != custom.ID || switchedPayload.PreviousDefaultStrategyID == nil || *switchedPayload.PreviousDefaultStrategyID != defaultStrategyID {
		t.Fatalf("unexpected set-default payload %+v", switchedPayload)
	}

	// Replay with a stale expected value is an idempotent 200 no-op.
	replayed := harness.requestJSON(t, harness.client, http.MethodPut, fmt.Sprintf("/api/loadbalance/strategies/%d/default", custom.ID), map[string]any{
		"expected_default_strategy_id": defaultStrategyID,
	}, modelHeader(profileID))
	var replayedPayload struct {
		DefaultStrategyID int  `json:"default_strategy_id"`
		Changed           bool `json:"changed"`
	}
	decodeContractResponse(t, replayed, &replayedPayload)
	if replayed.StatusCode != http.StatusOK || replayedPayload.Changed || replayedPayload.DefaultStrategyID != custom.ID {
		t.Fatalf("expected idempotent no-op replay, got %d %+v", replayed.StatusCode, replayedPayload)
	}

	// Switch back to fill-first with expected=null semantics when a default
	// exists: 409 (null only means "no default expected").
	nullExpected := harness.requestJSON(t, harness.client, http.MethodPut, fmt.Sprintf("/api/loadbalance/strategies/%d/default", defaultStrategyID), map[string]any{
		"expected_default_strategy_id": nil,
	}, modelHeader(profileID))
	if nullExpected.StatusCode != http.StatusConflict {
		t.Fatalf("expected null CAS with existing default to be 409, got %d", nullExpected.StatusCode)
	}

	back := harness.requestJSON(t, harness.client, http.MethodPut, fmt.Sprintf("/api/loadbalance/strategies/%d/default", defaultStrategyID), map[string]any{
		"expected_default_strategy_id": custom.ID,
	}, modelHeader(profileID))
	if back.StatusCode != http.StatusOK {
		t.Fatalf("expected switch back 200, got %d", back.StatusCode)
	}

	// Missing expected key is a typed 400.
	missingExpected := harness.requestJSONRaw(t, harness.client, http.MethodPut, fmt.Sprintf("/api/loadbalance/strategies/%d/default", custom.ID), `{}`, modelHeader(profileID))
	if missingExpected.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected missing CAS key to be 400, got %d", missingExpected.StatusCode)
	}

	// A strategy that does not exist in the effective (pinned default) profile
	// is a 404; the profile scope is enforced by ResolveEffectiveProfile.
	notFound := harness.requestJSON(t, harness.client, http.MethodPut, "/api/loadbalance/strategies/999999/default", map[string]any{
		"expected_default_strategy_id": nil,
	}, modelHeader(profileID))
	if notFound.StatusCode != http.StatusNotFound {
		t.Fatalf("expected unknown strategy set-default 404, got %d", notFound.StatusCode)
	}

	// The default strategy cannot be deleted; the operator must switch first.
	deletedDefault := harness.requestJSON(t, harness.client, http.MethodDelete, fmt.Sprintf("/api/loadbalance/strategies/%d", defaultStrategyID), nil, modelHeader(profileID))
	if deletedDefault.StatusCode != http.StatusConflict {
		t.Fatalf("expected default delete 409, got %d", deletedDefault.StatusCode)
	}
	var deletePayload struct {
		Code   string `json:"code"`
		Detail struct {
			Message           string `json:"message"`
			DefaultStrategyID *int   `json:"default_strategy_id"`
		} `json:"detail"`
	}
	decodeContractResponse(t, deletedDefault, &deletePayload)
	if deletePayload.Code != "default_strategy_replacement_required" || deletePayload.Detail.Message != "Cannot delete the default loadbalance strategy; set another strategy as the new model default first (or create built-in strategies when none remain)" || deletePayload.Detail.DefaultStrategyID == nil || *deletePayload.Detail.DefaultStrategyID != defaultStrategyID {
		t.Fatalf("unexpected default delete payload %+v", deletePayload)
	}

	// All default mutations together left the planning generation untouched.
	if got := readProfilePlanningGeneration(t, harness, profileID); got != generation {
		t.Fatalf("expected set-default to leave planning generation untouched, got %d -> %d", generation, got)
	}

	// Restore the default flag on the canonical row for a clean final state.
	if _, err := harness.conn.Exec(context.Background(), `UPDATE loadbalance_strategies SET is_default = FALSE WHERE id = $1`, custom.ID); err != nil {
		t.Fatalf("cleanup default flag: %v", err)
	}
	if _, err := harness.conn.Exec(context.Background(), `UPDATE loadbalance_strategies SET is_default = TRUE WHERE id = $1`, defaultStrategyID); err != nil {
		t.Fatalf("restore default flag: %v", err)
	}
}

func TestStrategyDefaultsActionContract(t *testing.T) {
	harness := newLoadbalanceContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)

	// Fresh startup state: all three canonical rows exist; the action is a
	// complete no-op that reports existing rows and does not touch the default.
	items := loadStrategyListContract(t, harness, profileID)
	if len(items) != 3 {
		t.Fatalf("expected 3 canonical strategies, got %d", len(items))
	}
	defaultID := items[0].ID

	action := harness.requestJSON(t, harness.client, http.MethodPost, "/api/loadbalance/strategies/defaults", map[string]any{}, modelHeader(profileID))
	if action.StatusCode != http.StatusOK {
		t.Fatalf("expected defaults action 200, got %d", action.StatusCode)
	}
	var payload strategyDefaultsResponseContract
	decodeContractResponse(t, action, &payload)
	if !payload.Complete || len(payload.Existing) != 3 || len(payload.Created) != 0 {
		t.Fatalf("expected complete all-existing result, got %+v", payload)
	}
	if payload.DefaultStrategyID == nil || *payload.DefaultStrategyID != defaultID || payload.DefaultChanged {
		t.Fatalf("expected unchanged default in complete action, got %+v", payload)
	}

	// Deleting a canonical row makes the action create it again; the default
	// (fill-first) stays untouched.
	var singleID int
	for _, item := range items {
		if item.Name == "Default single routing" {
			singleID = item.ID
		}
	}
	deleted := harness.requestJSON(t, harness.client, http.MethodDelete, fmt.Sprintf("/api/loadbalance/strategies/%d", singleID), nil, modelHeader(profileID))
	if deleted.StatusCode != http.StatusOK {
		t.Fatalf("expected delete 200, got %d", deleted.StatusCode)
	}
	action = harness.requestJSON(t, harness.client, http.MethodPost, "/api/loadbalance/strategies/defaults", map[string]any{}, modelHeader(profileID))
	decodeContractResponse(t, action, &payload)
	if !payload.Complete || len(payload.Created) != 1 || len(payload.Existing) != 2 || payload.Created[0]["canonical_name"] != "Default single routing" {
		t.Fatalf("expected one created single row, got %+v", payload)
	}
	if payload.DefaultChanged || payload.DefaultStrategyID == nil || *payload.DefaultStrategyID != defaultID {
		t.Fatalf("expected default unchanged after repair, got %+v", payload)
	}
	// Reload the list: the repaired canonical single row has a new id.
	singleID = 0
	for _, item := range loadStrategyListContract(t, harness, profileID) {
		if item.Name == "Default single routing" {
			singleID = item.ID
		}
	}
	if singleID == 0 {
		t.Fatalf("expected repaired canonical single row")
	}

	// A name conflict blocks the whole action with 409 and all conflict names.
	var roundRobinID int
	for _, item := range loadStrategyListContract(t, harness, profileID) {
		if item.Name == "Default round-robin routing" {
			roundRobinID = item.ID
		}
	}
	renamedAway := harness.requestJSON(t, harness.client, http.MethodPut, fmt.Sprintf("/api/loadbalance/strategies/%d", singleID), map[string]any{
		"name": "Custom single", "legacy_strategy_type": "single",
	}, modelHeader(profileID))
	if renamedAway.StatusCode != http.StatusOK {
		t.Fatalf("expected rename canonical single away 200, got %d", renamedAway.StatusCode)
	}
	conflict := harness.requestJSON(t, harness.client, http.MethodPut, fmt.Sprintf("/api/loadbalance/strategies/%d", roundRobinID), map[string]any{
		"name": "Default single routing", "legacy_strategy_type": "round-robin",
	}, modelHeader(profileID))
	if conflict.StatusCode != http.StatusOK {
		t.Fatalf("expected rename into canonical name 200, got %d", conflict.StatusCode)
	}
	action = harness.requestJSON(t, harness.client, http.MethodPost, "/api/loadbalance/strategies/defaults", map[string]any{}, modelHeader(profileID))
	if action.StatusCode != http.StatusConflict {
		t.Fatalf("expected canonical conflict 409, got %d", action.StatusCode)
	}
	var conflictPayload struct {
		Code   string `json:"code"`
		Detail struct {
			Message          string   `json:"message"`
			ConflictingNames []string `json:"conflicting_names"`
		} `json:"detail"`
	}
	decodeContractResponse(t, action, &conflictPayload)
	if conflictPayload.Code != "canonical_strategy_conflict" || conflictPayload.Detail.Message != "Canonical loadbalance strategy default name conflict" || len(conflictPayload.Detail.ConflictingNames) != 1 || conflictPayload.Detail.ConflictingNames[0] != "Default single routing" {
		t.Fatalf("unexpected conflict payload %+v", conflictPayload)
	}

	// The conflict must not have partially created anything.
	if items := loadStrategyListContract(t, harness, profileID); len(items) != 3 {
		t.Fatalf("expected conflict to leave strategy count at 3, got %d", len(items))
	}
}

func TestStrategyImpactListContract(t *testing.T) {
	harness := newLoadbalanceContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)

	items := loadStrategyListContract(t, harness, profileID)
	fillFirst := items[0]

	// Seed five directly-attached models via SQL (direct attachment truth).
	for index := 0; index < 5; index++ {
		if _, err := harness.conn.Exec(context.Background(), `INSERT INTO model_configs (profile_id, api_family, model_id, display_name, loadbalance_strategy_id, openai_accepted_format, is_enabled, created_at, updated_at) VALUES ($1, 'openai', $2, $3, $4, 'dual_native', TRUE, now(), now())`,
			profileID, fmt.Sprintf("impact-model-%d", index), fmt.Sprintf("Impact Model %d", index), fillFirst.ID); err != nil {
			t.Fatalf("seed attached model %d: %v", index, err)
		}
	}

	response := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/loadbalance/strategies/%d/models?limit=2", fillFirst.ID), nil, modelHeader(profileID))
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected impact list 200, got %d", response.StatusCode)
	}
	var page struct {
		StrategyID         int `json:"strategy_id"`
		AttachedModelCount int `json:"attached_model_count"`
		Items              []struct {
			ModelConfigID int    `json:"model_config_id"`
			ModelID       string `json:"model_id"`
			DisplayName   string `json:"display_name"`
			IsEnabled     bool   `json:"is_enabled"`
		} `json:"items"`
		HasMore    bool    `json:"has_more"`
		NextCursor *string `json:"next_cursor"`
	}
	decodeContractResponse(t, response, &page)
	if page.AttachedModelCount != 5 || len(page.Items) != 2 || !page.HasMore || page.NextCursor == nil {
		t.Fatalf("unexpected first impact page %+v", page)
	}
	// Keyset order: lower(display_name), id. Display names are
	// "Impact Model 0..4" so model 0 sorts first.
	if page.Items[0].ModelID != "impact-model-0" || page.Items[1].ModelID != "impact-model-1" {
		t.Fatalf("unexpected keyset order %+v", page.Items)
	}

	second := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/loadbalance/strategies/%d/models?limit=2&cursor=%s", fillFirst.ID, *page.NextCursor), nil, modelHeader(profileID))
	secondCursor := *page.NextCursor
	decodeContractResponse(t, second, &page)
	if len(page.Items) != 2 || !page.HasMore || page.Items[0].ModelID != "impact-model-2" {
		t.Fatalf("unexpected second impact page %+v", page)
	}

	third := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/loadbalance/strategies/%d/models?limit=2&cursor=%s", fillFirst.ID, *page.NextCursor), nil, modelHeader(profileID))
	decodeContractResponse(t, third, &page)
	if len(page.Items) != 1 || page.HasMore || page.NextCursor != nil || page.Items[0].ModelID != "impact-model-4" {
		t.Fatalf("unexpected final impact page %+v", page)
	}

	// Cursor scope mismatch: different limit is a typed 422.
	mismatch := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/loadbalance/strategies/%d/models?limit=3&cursor=%s", fillFirst.ID, secondCursor), nil, modelHeader(profileID))
	if mismatch.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("expected cursor scope mismatch 422, got %d", mismatch.StatusCode)
	}
	var mismatchPayload struct {
		Code string `json:"code"`
	}
	decodeContractResponse(t, mismatch, &mismatchPayload)
	if mismatchPayload.Code != "cursor_scope_mismatch" {
		t.Fatalf("expected cursor_scope_mismatch code, got %+v", mismatchPayload)
	}

	// A stale cursor (planning generation bumped by a payload edit) is a 409.
	edited := harness.requestJSON(t, harness.client, http.MethodPut, fmt.Sprintf("/api/loadbalance/strategies/%d", fillFirst.ID), map[string]any{
		"name": "Default fill-first routing", "legacy_strategy_type": "fill-first", "retry_base_delay_ms": 61000,
	}, modelHeader(profileID))
	if edited.StatusCode != http.StatusOK {
		t.Fatalf("expected payload edit 200, got %d", edited.StatusCode)
	}
	stale := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/loadbalance/strategies/%d/models?limit=2&cursor=%s", fillFirst.ID, secondCursor), nil, modelHeader(profileID))
	if stale.StatusCode != http.StatusConflict {
		t.Fatalf("expected stale impact cursor 409, got %d", stale.StatusCode)
	}
	var stalePayload struct {
		Code string `json:"code"`
	}
	decodeContractResponse(t, stale, &stalePayload)
	if stalePayload.Code != "impact_cursor_stale" {
		t.Fatalf("expected impact_cursor_stale code, got %+v", stalePayload)
	}

	// Count parity with the list DTO and deleted/disabled rows: disable one
	// model; direct attachment truth still counts it.
	if _, err := harness.conn.Exec(context.Background(), `UPDATE model_configs SET is_enabled = FALSE WHERE model_id = 'impact-model-3'`); err != nil {
		t.Fatalf("disable attached model: %v", err)
	}
	response = harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/loadbalance/strategies/%d/models?limit=10", fillFirst.ID), nil, modelHeader(profileID))
	decodeContractResponse(t, response, &page)
	if page.AttachedModelCount != 5 || len(page.Items) != 5 {
		t.Fatalf("expected disabled model to remain directly attached, got %+v", page)
	}
}

func TestStrategySetDefaultConcurrentCAS(t *testing.T) {
	harness := newLoadbalanceContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	items := loadStrategyListContract(t, harness, profileID)
	defaultStrategyID := items[0].ID
	targetID := items[2].ID

	// Scenario 1: two concurrent set-default requests to the SAME target with
	// the same expected value. The profile lock serializes them; the winner
	// commits (changed=true) and the second request hits the idempotent no-op
	// (200, changed=false) because the target already is the default — the
	// partial unique index is never the normal control flow.
	sendTo := func(strategyID int, expected int) (int, bool, *int) {
		response := harness.requestJSON(t, harness.client, http.MethodPut, fmt.Sprintf("/api/loadbalance/strategies/%d/default", strategyID), map[string]any{
			"expected_default_strategy_id": expected,
		}, modelHeader(profileID))
		if response.StatusCode == http.StatusOK {
			var payload struct {
				DefaultStrategyID int  `json:"default_strategy_id"`
				Changed           bool `json:"changed"`
			}
			decodeContractResponse(t, response, &payload)
			return response.StatusCode, payload.Changed, &payload.DefaultStrategyID
		}
		if response.StatusCode == http.StatusConflict {
			var payload struct {
				Detail struct {
					Message                  string `json:"message"`
					CurrentDefaultStrategyID *int   `json:"current_default_strategy_id"`
				} `json:"detail"`
			}
			decodeContractResponse(t, response, &payload)
			return response.StatusCode, false, payload.Detail.CurrentDefaultStrategyID
		}
		t.Fatalf("unexpected concurrent set-default status %d", response.StatusCode)
		return 0, false, nil
	}

	type outcome struct {
		status    int
		changed   bool
		defaultID *int
	}
	results := make(chan outcome, 2)
	for range 2 {
		go func() {
			status, changed, defaultID := sendTo(targetID, defaultStrategyID)
			results <- outcome{status: status, changed: changed, defaultID: defaultID}
		}()
	}
	first := <-results
	second := <-results

	changedCount := 0
	for _, result := range []outcome{first, second} {
		if result.status != http.StatusOK || result.defaultID == nil || *result.defaultID != targetID {
			t.Fatalf("unexpected same-target concurrent outcome %+v", result)
		}
		if result.changed {
			changedCount++
		}
	}
	if changedCount != 1 {
		t.Fatalf("expected exactly one changed=true in same-target race, got %d (%+v, %+v)", changedCount, first, second)
	}

	// Scenario 2: concurrent set-default to DIFFERENT targets. Reset the
	// default to fill-first first, then race single vs round-robin. The winner
	// commits; the loser sees a changed default and returns 409 with the new
	// default id.
	if _, err := harness.conn.Exec(context.Background(), `UPDATE loadbalance_strategies SET is_default = FALSE WHERE profile_id = $1`, profileID); err != nil {
		t.Fatalf("clear default for scenario 2: %v", err)
	}
	if _, err := harness.conn.Exec(context.Background(), `UPDATE loadbalance_strategies SET is_default = TRUE WHERE profile_id = $1 AND name = 'Default fill-first routing'`, profileID); err != nil {
		t.Fatalf("reset default for scenario 2: %v", err)
	}
	singleID := items[1].ID
	results = make(chan outcome, 2)
	for _, strategyID := range []int{singleID, targetID} {
		strategyID := strategyID
		go func() {
			status, changed, defaultID := sendTo(strategyID, defaultStrategyID)
			results <- outcome{status: status, changed: changed, defaultID: defaultID}
		}()
	}
	first = <-results
	second = <-results

	successes := 0
	conflicts := 0
	for _, result := range []outcome{first, second} {
		switch result.status {
		case http.StatusOK:
			successes++
			if !result.changed || result.defaultID == nil {
				t.Fatalf("unexpected different-target winner %+v", result)
			}
		case http.StatusConflict:
			conflicts++
			if result.defaultID == nil {
				t.Fatalf("expected conflict to report the new default, got %+v", result)
			}
		default:
			t.Fatalf("unexpected different-target status %d", result.status)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("expected exactly one success and one conflict, got successes=%d conflicts=%d", successes, conflicts)
	}

	// The final default is the winner's target, with exactly one default row.
	var defaultCount int
	if err := harness.conn.QueryRow(context.Background(), `SELECT COUNT(*) FROM loadbalance_strategies WHERE profile_id = $1 AND is_default = TRUE`, profileID).Scan(&defaultCount); err != nil {
		t.Fatalf("count defaults: %v", err)
	}
	if defaultCount != 1 {
		t.Fatalf("expected exactly one default after concurrent set-default, got %d", defaultCount)
	}
	items = loadStrategyListContract(t, harness, profileID)
	if !items[0].IsDefault || (items[0].ID != singleID && items[0].ID != targetID) {
		t.Fatalf("expected final default to be one of the racers, got %+v", items[0])
	}
}

func TestStrategyPreviewContract(t *testing.T) {
	harness := newLoadbalanceContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)

	before := loadStrategyListContract(t, harness, profileID)
	generation := readProfilePlanningGeneration(t, harness, profileID)

	// Balanced draft (name omitted is allowed) matches the golden steps.
	response := harness.requestJSON(t, harness.client, http.MethodPost, "/api/loadbalance/strategies/preview", map[string]any{
		"legacy_strategy_type":                   "fill-first",
		"failure_status_codes":                   []int{403, 422, 429, 500, 502, 503, 504, 529},
		"ban_mode":                               "off",
		"retry_base_delay_ms":                    60000,
		"retry_backoff_multiplier":               2.0,
		"retry_jitter_ratio":                     0.2,
		"retry_max_delay_ms":                     900000,
		"cycle_retry_attempt_limit":              3,
		"ban_cumulative_retry_attempt_threshold": 0,
		"ban_duration_seconds":                   0,
	}, modelHeader(profileID))
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected preview 200, got %d", response.StatusCode)
	}
	var preview struct {
		NormalizedPolicy map[string]any `json:"normalized_policy"`
		Steps            []struct {
			FailureOrdinal         int         `json:"failure_ordinal"`
			CycleRetryAttempt      int         `json:"cycle_retry_attempt"`
			CumulativeRetryAttempt int         `json:"cumulative_retry_attempt"`
			NominalDelayMS         int         `json:"nominal_delay_ms"`
			JitterMinDelayMS       int         `json:"jitter_min_delay_ms"`
			JitterMaxDelayMS       int         `json:"jitter_max_delay_ms"`
			CycleExhausted         bool        `json:"cycle_exhausted"`
			BanTransition          interface{} `json:"ban_transition"`
		} `json:"steps"`
		ShownStepCount              int    `json:"shown_step_count"`
		HasMore                     bool   `json:"has_more"`
		TerminationReason           string `json:"termination_reason"`
		CycleExhaustionAfterAttempt int    `json:"cycle_exhaustion_after_attempt"`
		BanProjection               struct {
			Mode                            string `json:"mode"`
			CumulativeRetryAttemptThreshold int    `json:"cumulative_retry_attempt_threshold"`
			TransitionAtCumulativeFailure   *int   `json:"transition_at_cumulative_failure"`
			DurationSeconds                 int    `json:"duration_seconds"`
		} `json:"ban_projection"`
	}
	decodeContractResponse(t, response, &preview)
	if len(preview.Steps) != 3 || preview.ShownStepCount != 3 || preview.HasMore || preview.TerminationReason != "cycle_exhausted" || preview.CycleExhaustionAfterAttempt != 3 {
		t.Fatalf("unexpected balanced preview %+v", preview)
	}
	wantDelays := []int{60000, 120000, 240000}
	for index, step := range preview.Steps {
		if step.FailureOrdinal != index+1 || step.NominalDelayMS != wantDelays[index] {
			t.Fatalf("unexpected step %d: %+v", index, step)
		}
		if step.JitterMinDelayMS != wantDelays[index]*4/5 || step.JitterMaxDelayMS != wantDelays[index]*6/5 {
			t.Fatalf("unexpected jitter bounds at step %d: %+v", index, step)
		}
		if step.CycleExhausted != (index == 2) || step.BanTransition != nil {
			t.Fatalf("unexpected step flags %+v", step)
		}
	}
	if preview.BanProjection.Mode != "off" || preview.BanProjection.TransitionAtCumulativeFailure != nil {
		t.Fatalf("unexpected ban projection %+v", preview.BanProjection)
	}
	if _, ok := preview.NormalizedPolicy["name"]; ok {
		t.Fatalf("expected normalized policy without a name for an omitted name, got %+v", preview.NormalizedPolicy)
	}

	// Preview is side-effect free: no strategy rows created, no generation bump.
	if after := loadStrategyListContract(t, harness, profileID); len(after) != len(before) {
		t.Fatalf("expected preview to create no rows, got %d -> %d", len(before), len(after))
	}
	if got := readProfilePlanningGeneration(t, harness, profileID); got != generation {
		t.Fatalf("expected preview to leave planning generation untouched, got %d -> %d", generation, got)
	}

	// Invalid drafts reuse the authoritative validation.
	invalid := harness.requestJSON(t, harness.client, http.MethodPost, "/api/loadbalance/strategies/preview", map[string]any{
		"legacy_strategy_type": "fill-first", "retry_base_delay_ms": -5,
	}, modelHeader(profileID))
	assertErrorResponse(t, invalid, http.StatusBadRequest, "retry_base_delay_ms must be between 0 and 86400000")
}
