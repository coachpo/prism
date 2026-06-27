package models

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/coachpo/prism/backend/internal/platform/config"
	"github.com/coachpo/prism/backend/internal/platform/migrate"
	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
)

type modelRouteErrorResponse struct {
	Detail            string                       `json:"detail"`
	RoutingPlanIssues []routingPlanValidationIssue `json:"routing_plan_issues"`
}

func TestModelRoutesRejectAccessTargetsInCreateUpdateAndObsoleteTargetFields(t *testing.T) {
	ctx, conn, dsn := createPromotionTargetTestDatabase(t, "model_routes_reject_obsolete_access_target_fields")
	now := time.Date(2026, time.June, 5, 21, 0, 0, 0, time.UTC)
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin seed transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	profileID := insertPromotionProfile(t, ctx, tx, "obsolete-target-fields-profile", now)
	strategyID := insertPromotionStrategy(t, ctx, tx, profileID, "obsolete-target-fields-strategy", now)
	source := insertPromotionModel(t, ctx, tx, profileID, strategyID, now, promotionModelSeed{ModelID: "obsolete-source", APIFamily: "openai", IsEnabled: false})
	target := insertPromotionModel(t, ctx, tx, profileID, strategyID, now, promotionModelSeed{ModelID: "obsolete-target", APIFamily: "openai", IsEnabled: false})
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit seed transaction: %v", err)
	}
	router := newPromotionTargetRouter(t, ctx, dsn, now)

	createResponse := performPromotionTargetModelRequest(t, router, http.MethodPost, "/models", profileID, map[string]any{
		"api_family":              "openai",
		"model_id":                "obsolete-create",
		"loadbalance_strategy_id": source.LoadbalanceStrategyID,
		"access_targets": []map[string]any{{
			"target_type":     "model",
			"target_model_id": target.ModelID,
			"position":        0,
			"weight":          1,
		}},
	})
	assertInvalidRequestBody(t, createResponse)

	updateResponse := performPromotionTargetModelRequest(t, router, http.MethodPut, fmt.Sprintf("/models/%d", source.ID), profileID, map[string]any{
		"access_targets": []map[string]any{{
			"target_type":     "model",
			"target_model_id": target.ModelID,
			"position":        0,
			"target_priority": 0,
		}},
	})
	assertInvalidRequestBody(t, updateResponse)

	targetCreateResponse := performPromotionTargetModelRequest(t, router, http.MethodPost, fmt.Sprintf("/models/%d/targets", source.ID), profileID, map[string]any{
		"target_type":     "model",
		"target_model_id": target.ModelID,
		"position":        1,
		"weight":          1,
	})
	assertObsoleteAccessTargetRouteError(t, targetCreateResponse, "weight")

	targetPatchResponse := performPromotionTargetModelRequest(t, router, http.MethodPatch, fmt.Sprintf("/models/%d/targets/1", source.ID), profileID, map[string]any{
		"target_priority": 0,
	})
	assertObsoleteAccessTargetRouteError(t, targetPatchResponse, "target_priority")
}

func assertInvalidRequestBody(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid request body to fail with 400, got %d: %s", response.Code, response.Body.String())
	}
	var payload modelRouteErrorResponse
	decodePromotionTargetJSON(t, response, &payload)
	if payload.Detail != "Invalid request body" {
		t.Fatalf("expected invalid request body error, got %+v", payload)
	}
}

func assertObsoleteAccessTargetRouteError(t *testing.T, response *httptest.ResponseRecorder, path string) {
	t.Helper()
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected obsolete field request to fail with 400, got %d: %s", response.Code, response.Body.String())
	}
	detail := fmt.Sprintf("%s is obsolete and must be omitted", path)
	var payload modelRouteErrorResponse
	decodePromotionTargetJSON(t, response, &payload)
	if payload.Detail != detail || len(payload.RoutingPlanIssues) != 1 {
		t.Fatalf("unexpected obsolete field payload: %+v", payload)
	}
	issue := payload.RoutingPlanIssues[0]
	if issue.Code != "obsolete_access_target_field" || issue.Path != path || issue.Message != detail {
		t.Fatalf("unexpected obsolete field issue: %+v", issue)
	}
}

func createPromotionTargetTestDatabase(t *testing.T, name string) (context.Context, *pgx.Conn, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)

	harness := modelsStoreHarness(t)
	databaseName := fmt.Sprintf("%s_%s", name, modelsRandomSuffix(t))
	dsn := harness.connectionString(databaseName)
	conn := harness.openDatabase(t, ctx, databaseName)

	runner, err := migrate.New(migrate.Options{})
	if err != nil {
		t.Fatalf("build migration runner: %v", err)
	}
	if _, err := runner.Run(ctx, conn); err != nil {
		t.Fatalf("run migrations for %s: %v", databaseName, err)
	}
	t.Cleanup(func() {
		_ = conn.Close(ctx)
	})
	return ctx, conn, dsn
}

func newPromotionTargetRouter(t *testing.T, ctx context.Context, dsn string, now time.Time) http.Handler {
	t.Helper()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("create pgx pool: %v", err)
	}
	t.Cleanup(pool.Close)

	service, err := NewService(config.Settings{}, Options{Pool: pool, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("build model service: %v", err)
	}
	router := chi.NewRouter()
	service.MountManagementRoutes(router)
	return router
}

func performPromotionTargetModelRequest(t *testing.T, handler http.Handler, method string, path string, profileID int, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		rawBody, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		reader = bytes.NewReader(rawBody)
	} else {
		reader = bytes.NewReader(nil)
	}
	request := httptest.NewRequest(method, path, reader)
	request.Header.Set(profiledomain.ProfileIDHeader, fmt.Sprintf("%d", profileID))
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func decodePromotionTargetJSON(t *testing.T, response *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.Unmarshal(response.Body.Bytes(), target); err != nil {
		t.Fatalf("decode JSON response: %v\nbody=%s", err, response.Body.String())
	}
}

func findListedModelByID(t *testing.T, items []modelConfigListResponse, modelConfigID int) modelConfigListResponse {
	t.Helper()
	for _, item := range items {
		if item.ID == modelConfigID {
			return item
		}
	}
	t.Fatalf("expected model %d in listed response, got %+v", modelConfigID, items)
	return modelConfigListResponse{}
}

type promotionModelSeed struct {
	ModelID   string
	APIFamily string
	IsEnabled bool
}

func seedPromotionTerminalModel(t *testing.T, ctx context.Context, tx pgx.Tx, profileID int, strategyID int, now time.Time, seed promotionModelSeed, _ int, _ float64) (modelRecord, int) {
	t.Helper()
	model := insertPromotionModel(t, ctx, tx, profileID, strategyID, now, seed)
	endpointID := insertPromotionEndpoint(t, ctx, tx, profileID, seed.ModelID+"-endpoint", now)
	connectionID := insertPromotionConnection(t, ctx, tx, profileID, endpointID, seed.APIFamily, seed.ModelID+"-connection", now)
	insertPromotionConnectionTarget(t, ctx, tx, profileID, model.ID, connectionID, 0, true, now)
	return model, connectionID
}

func insertPromotionProfile(t *testing.T, ctx context.Context, tx pgx.Tx, name string, now time.Time) int {
	t.Helper()
	return insertPromotionProfileWithActive(t, ctx, tx, name, true, now)
}

func insertPromotionProfileWithActive(t *testing.T, ctx context.Context, tx pgx.Tx, name string, isActive bool, now time.Time) int {
	t.Helper()
	var profileID int
	if err := tx.QueryRow(ctx, `INSERT INTO profiles (name, description, is_active, is_default, is_editable, version, created_at, updated_at) VALUES ($1, $2, $3, FALSE, TRUE, 1, $4, $4) RETURNING id`, name, nil, isActive, now).Scan(&profileID); err != nil {
		t.Fatalf("insert profile %q: %v", name, err)
	}
	return profileID
}

func insertPromotionStrategy(t *testing.T, ctx context.Context, tx pgx.Tx, profileID int, name string, now time.Time) int {
	t.Helper()
	var strategyID int
	if err := tx.QueryRow(ctx, `INSERT INTO loadbalance_strategies (profile_id, name, legacy_strategy_type, failure_status_codes, ban_mode, retry_base_delay_ms, retry_backoff_multiplier, retry_jitter_ratio, retry_max_delay_ms, cycle_retry_attempt_limit, ban_cumulative_retry_attempt_threshold, ban_duration_seconds, created_at, updated_at) VALUES ($1, $2, 'single', ARRAY[429], 'off', 60000, 2.0, 0.2, 900000, 3, 0, 0, $3, $3) RETURNING id`, profileID, name, now).Scan(&strategyID); err != nil {
		t.Fatalf("insert loadbalance strategy %q: %v", name, err)
	}
	return strategyID
}

func insertPromotionModel(t *testing.T, ctx context.Context, tx pgx.Tx, profileID int, strategyID int, now time.Time, seed promotionModelSeed) modelRecord {
	t.Helper()
	record := modelRecord{
		ProfileID:             profileID,
		APIFamily:             seed.APIFamily,
		ModelID:               seed.ModelID,
		DisplayName:           stringPtr(seed.ModelID),
		LoadbalanceStrategyID: intPtr(strategyID),
		IsEnabled:             seed.IsEnabled,
		CreatedAt:             now,
		UpdatedAt:             now,
	}
	if seed.APIFamily == "openai" {
		record.OpenAIAcceptedFormat = stringPtr(openAIAcceptedFormatDualNative)
	}
	return mustInsertModelRecord(t, ctx, tx, record)
}

func insertPromotionEndpoint(t *testing.T, ctx context.Context, tx pgx.Tx, profileID int, name string, now time.Time) int {
	t.Helper()
	var endpointID int
	if err := tx.QueryRow(ctx, `INSERT INTO endpoints (profile_id, name, base_url, api_key, position, created_at, updated_at) VALUES ($1, $2, $3, $4, 0, $5, $5) RETURNING id`, profileID, name, fmt.Sprintf("https://%s.example", name), "test-api-key", now).Scan(&endpointID); err != nil {
		t.Fatalf("insert endpoint %q: %v", name, err)
	}
	return endpointID
}

func insertPromotionConnection(t *testing.T, ctx context.Context, tx pgx.Tx, profileID int, endpointID int, apiFamily string, name string, now time.Time) int {
	t.Helper()
	var connectionID int
	var openAITextCapability any
	if apiFamily == "openai" {
		openAITextCapability = "responses_only"
	}
	if err := tx.QueryRow(ctx, `INSERT INTO connections (profile_id, api_family, endpoint_id, pricing_template_id, qps_limit, max_in_flight_non_stream, max_in_flight_stream, openai_probe_endpoint_variant, openai_text_capability, is_active, priority, name, auth_type, custom_headers, health_status, health_detail, last_health_check, created_at, updated_at) VALUES ($1, $2, $3, NULL, NULL, NULL, NULL, NULL, $6, TRUE, 0, $4, NULL, NULL, 'healthy', NULL, NULL, $5, $5) RETURNING id`, profileID, apiFamily, endpointID, name, now, openAITextCapability).Scan(&connectionID); err != nil {
		t.Fatalf("insert connection %q: %v", name, err)
	}
	return connectionID
}

func insertPromotionModelTarget(t *testing.T, ctx context.Context, tx pgx.Tx, profileID int, sourceModelConfigID int, targetModelConfigID int, position int, isEnabled bool, now time.Time) {
	t.Helper()
	if _, err := tx.Exec(ctx, `INSERT INTO model_access_targets (profile_id, source_model_config_id, target_type, target_model_config_id, position, is_enabled, created_at, updated_at) VALUES ($1, $2, 'model', $3, $4, $5, $6, $6)`, profileID, sourceModelConfigID, targetModelConfigID, position, isEnabled, now); err != nil {
		t.Fatalf("insert model target source=%d target=%d: %v", sourceModelConfigID, targetModelConfigID, err)
	}
}

func insertPromotionConnectionTarget(t *testing.T, ctx context.Context, tx pgx.Tx, profileID int, sourceModelConfigID int, connectionID int, position int, isEnabled bool, now time.Time) {
	t.Helper()
	if _, err := tx.Exec(ctx, `INSERT INTO model_access_targets (profile_id, source_model_config_id, target_type, target_connection_id, position, is_enabled, created_at, updated_at) VALUES ($1, $2, 'connection', $3, $4, $5, $6, $6)`, profileID, sourceModelConfigID, connectionID, position, isEnabled, now); err != nil {
		t.Fatalf("insert connection target model=%d connection=%d: %v", sourceModelConfigID, connectionID, err)
	}
}
