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

const (
	promotionTargetScenarioValid          = "valid"
	promotionTargetScenarioRecursiveValid = "recursive_valid"
)

type modelRouteErrorResponse struct {
	Detail            string                       `json:"detail"`
	RoutingPlanIssues []routingPlanValidationIssue `json:"routing_plan_issues"`
}

func TestModelServiceAcceptsValidPromotionTarget(t *testing.T) {
	ctx, conn, _ := createPromotionTargetTestDatabase(t, "model_service_accepts_valid_promotion_target")
	now := time.Date(2026, time.June, 5, 20, 0, 0, 0, time.UTC)
	source, targetModelID, profileID := seedPromotionTargetScenario(t, ctx, conn, now, promotionTargetScenarioValid)
	source.ContextOverflowPromotionTargetID = stringPtr(targetModelID)

	service := &Service{}
	if err := service.validateContextOverflowPromotionTarget(ctx, conn, profileID, source); err != nil {
		t.Fatalf("expected valid promotion target to pass, got %v", err)
	}
}

func TestModelServiceRejectsUnknownPromotionTarget(t *testing.T) {
	assertServicePromotionTargetValidationFailure(t, promotionTargetValidationCodeUnknown)
}

func TestModelServiceRejectsSelfPromotionTarget(t *testing.T) {
	assertServicePromotionTargetValidationFailure(t, promotionTargetValidationCodeSelf)
}

func TestModelServiceRejectsDisabledPromotionTarget(t *testing.T) {
	assertServicePromotionTargetValidationFailure(t, promotionTargetValidationCodeDisabled)
}

func TestModelServiceRejectsFacadePromotionTarget(t *testing.T) {
	assertServicePromotionTargetValidationFailure(t, promotionTargetValidationCodeFacade)
}

func TestModelServiceRejectsCrossProfilePromotionTarget(t *testing.T) {
	assertServicePromotionTargetValidationFailure(t, promotionTargetValidationCodeCrossProfile)
}

func TestModelServiceRejectsSameTerminalPromotionTarget(t *testing.T) {
	assertServicePromotionTargetValidationFailure(t, promotionTargetValidationCodeSameTerminal)
}

func TestModelServiceRejectsAPIFamilyMismatchPromotionTarget(t *testing.T) {
	assertServicePromotionTargetValidationFailure(t, promotionTargetValidationCodeAPIFamilyMismatch)
}

func TestModelServiceAcceptsRecursivePromotionChainWithoutImmediateLargerWindow(t *testing.T) {
	ctx, conn, _ := createPromotionTargetTestDatabase(t, "model_service_accepts_recursive_promotion_chain")
	now := time.Date(2026, time.June, 5, 20, 5, 0, 0, time.UTC)
	source, targetModelID, profileID := seedPromotionTargetScenario(t, ctx, conn, now, promotionTargetScenarioRecursiveValid)
	source.ContextOverflowPromotionTargetID = stringPtr(targetModelID)

	service := &Service{}
	if err := service.validateContextOverflowPromotionTarget(ctx, conn, profileID, source); err != nil {
		t.Fatalf("expected recursive promotion chain to pass, got %v", err)
	}
}

func TestModelServiceRejectsPromotionCycle(t *testing.T) {
	assertServicePromotionTargetValidationFailure(t, promotionTargetValidationCodeCycle)
}

func TestModelServiceRejectsPromotionMaxDepth(t *testing.T) {
	assertServicePromotionTargetValidationFailure(t, promotionTargetValidationCodeMaxDepth)
}

func TestModelRoutesExposePromotionTargetField(t *testing.T) {
	ctx, conn, dsn := createPromotionTargetTestDatabase(t, "model_routes_expose_promotion_target_field")
	now := time.Date(2026, time.June, 5, 20, 15, 0, 0, time.UTC)
	source, targetModelID, profileID := seedPromotionTargetScenario(t, ctx, conn, now, promotionTargetScenarioValid)
	router := newPromotionTargetRouter(t, ctx, dsn, now)

	updateResponse := performPromotionTargetModelRequest(t, router, http.MethodPut, fmt.Sprintf("/models/%d", source.ID), profileID, map[string]any{
		contextOverflowPromotionTargetField: targetModelID,
	})
	if updateResponse.Code != http.StatusOK {
		t.Fatalf("expected PUT /models/%d to succeed, got %d: %s", source.ID, updateResponse.Code, updateResponse.Body.String())
	}
	var updated modelConfigResponse
	decodePromotionTargetJSON(t, updateResponse, &updated)
	requirePromotionTargetEquals(t, updated.ContextOverflowPromotionTargetID, targetModelID)

	loaded, found, err := loadModelRecord(ctx, conn, profileID, source.ID, false)
	if err != nil {
		t.Fatalf("load updated model: %v", err)
	}
	if !found {
		t.Fatalf("expected source model %d to exist after route update", source.ID)
	}
	requirePromotionTargetEquals(t, loaded.ContextOverflowPromotionTargetID, targetModelID)

	getResponse := performPromotionTargetModelRequest(t, router, http.MethodGet, fmt.Sprintf("/models/%d", source.ID), profileID, nil)
	if getResponse.Code != http.StatusOK {
		t.Fatalf("expected GET /models/%d to succeed, got %d: %s", source.ID, getResponse.Code, getResponse.Body.String())
	}
	var detail modelConfigResponse
	decodePromotionTargetJSON(t, getResponse, &detail)
	requirePromotionTargetEquals(t, detail.ContextOverflowPromotionTargetID, targetModelID)

	listResponse := performPromotionTargetModelRequest(t, router, http.MethodGet, "/models", profileID, nil)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("expected GET /models to succeed, got %d: %s", listResponse.Code, listResponse.Body.String())
	}
	var listed []modelConfigListResponse
	decodePromotionTargetJSON(t, listResponse, &listed)
	listItem := findListedModelByID(t, listed, source.ID)
	requirePromotionTargetEquals(t, listItem.ContextOverflowPromotionTargetID, targetModelID)
}

func TestModelRoutesReturnStablePromotionTargetValidationErrors(t *testing.T) {
	codes := []string{
		promotionTargetValidationCodeUnknown,
		promotionTargetValidationCodeSelf,
		promotionTargetValidationCodeDisabled,
		promotionTargetValidationCodeFacade,
		promotionTargetValidationCodeCrossProfile,
		promotionTargetValidationCodeSameTerminal,
		promotionTargetValidationCodeAPIFamilyMismatch,
		promotionTargetValidationCodeCycle,
		promotionTargetValidationCodeMaxDepth,
	}
	for _, code := range codes {
		t.Run(code, func(t *testing.T) {
			ctx, conn, dsn := createPromotionTargetTestDatabase(t, "model_routes_return_stable_promotion_target_validation_errors_"+code)
			now := time.Date(2026, time.June, 5, 20, 30, 0, 0, time.UTC)
			source, targetModelID, profileID := seedPromotionTargetScenario(t, ctx, conn, now, code)
			router := newPromotionTargetRouter(t, ctx, dsn, now)

			response := performPromotionTargetModelRequest(t, router, http.MethodPut, fmt.Sprintf("/models/%d", source.ID), profileID, map[string]any{
				contextOverflowPromotionTargetField: targetModelID,
			})
			if response.Code != http.StatusBadRequest {
				t.Fatalf("expected PUT /models/%d to fail with 400 for %s, got %d: %s", source.ID, code, response.Code, response.Body.String())
			}
			var payload modelRouteErrorResponse
			decodePromotionTargetJSON(t, response, &payload)
			assertPromotionTargetRouteError(t, payload, code)

			loaded, found, err := loadModelRecord(ctx, conn, profileID, source.ID, false)
			if err != nil {
				t.Fatalf("load source model after failed update: %v", err)
			}
			if !found {
				t.Fatalf("expected source model %d to exist after failed update", source.ID)
			}
			if loaded.ContextOverflowPromotionTargetID != nil {
				t.Fatalf("expected failed update to keep promotion target unset, got %q", *loaded.ContextOverflowPromotionTargetID)
			}
		})
	}
}

func TestModelRoutesRejectObsoleteAccessTargetFields(t *testing.T) {
	ctx, conn, dsn := createPromotionTargetTestDatabase(t, "model_routes_reject_obsolete_access_target_fields")
	now := time.Date(2026, time.June, 5, 21, 0, 0, 0, time.UTC)
	source, targetModelID, profileID := seedPromotionTargetScenario(t, ctx, conn, now, promotionTargetScenarioValid)
	router := newPromotionTargetRouter(t, ctx, dsn, now)

	createResponse := performPromotionTargetModelRequest(t, router, http.MethodPost, "/models", profileID, map[string]any{
		"api_family":              "openai",
		"model_id":                "obsolete-create",
		"loadbalance_strategy_id": source.LoadbalanceStrategyID,
		"access_targets": []map[string]any{{
			"target_type":     "model",
			"target_model_id": targetModelID,
			"position":        0,
			"weight":          1,
		}},
	})
	assertObsoleteAccessTargetRouteError(t, createResponse, "access_targets[0].weight")

	updateResponse := performPromotionTargetModelRequest(t, router, http.MethodPut, fmt.Sprintf("/models/%d", source.ID), profileID, map[string]any{
		"access_targets": []map[string]any{{
			"target_type":     "model",
			"target_model_id": targetModelID,
			"position":        0,
			"target_priority": 0,
		}},
	})
	assertObsoleteAccessTargetRouteError(t, updateResponse, "access_targets[0].target_priority")

	targetCreateResponse := performPromotionTargetModelRequest(t, router, http.MethodPost, fmt.Sprintf("/models/%d/targets", source.ID), profileID, map[string]any{
		"target_type":     "model",
		"target_model_id": targetModelID,
		"position":        1,
		"weight":          1,
	})
	assertObsoleteAccessTargetRouteError(t, targetCreateResponse, "weight")

	targetPatchResponse := performPromotionTargetModelRequest(t, router, http.MethodPatch, fmt.Sprintf("/models/%d/targets/1", source.ID), profileID, map[string]any{
		"target_priority": 0,
	})
	assertObsoleteAccessTargetRouteError(t, targetPatchResponse, "target_priority")
}

func assertServicePromotionTargetValidationFailure(t *testing.T, expectedCode string) {
	t.Helper()
	ctx, conn, _ := createPromotionTargetTestDatabase(t, "model_service_validation_failure_"+expectedCode)
	now := time.Date(2026, time.June, 5, 20, 45, 0, 0, time.UTC)
	source, targetModelID, profileID := seedPromotionTargetScenario(t, ctx, conn, now, expectedCode)
	source.ContextOverflowPromotionTargetID = stringPtr(targetModelID)

	service := &Service{}
	err := service.validateContextOverflowPromotionTarget(ctx, conn, profileID, source)
	requirePromotionTargetDomainError(t, err, expectedCode)
}

func requirePromotionTargetDomainError(t *testing.T, err error, expectedCode string) {
	t.Helper()
	detail := expectedPromotionTargetDetail(expectedCode)
	domainErr := requireModelDomainError(t, err, http.StatusBadRequest, detail)
	issues, ok := domainErr.Fields["routing_plan_issues"].([]routingPlanValidationIssue)
	if !ok {
		t.Fatalf("expected routing_plan_issues payload, got %+v", domainErr.Fields)
	}
	if len(issues) != 1 {
		t.Fatalf("expected one routing_plan_issue, got %+v", issues)
	}
	issue := issues[0]
	if issue.Code != expectedCode || issue.Path != contextOverflowPromotionTargetField || issue.Message != detail {
		t.Fatalf("unexpected promotion-target routing_plan_issue: %+v", issue)
	}
}

func assertPromotionTargetRouteError(t *testing.T, payload modelRouteErrorResponse, expectedCode string) {
	t.Helper()
	detail := expectedPromotionTargetDetail(expectedCode)
	if payload.Detail != detail {
		t.Fatalf("expected detail %q, got %q", detail, payload.Detail)
	}
	if len(payload.RoutingPlanIssues) != 1 {
		t.Fatalf("expected one routing_plan_issue, got %+v", payload.RoutingPlanIssues)
	}
	issue := payload.RoutingPlanIssues[0]
	if issue.Code != expectedCode || issue.Path != contextOverflowPromotionTargetField || issue.Message != detail {
		t.Fatalf("unexpected route routing_plan_issue: %+v", issue)
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

func expectedPromotionTargetDetail(code string) string {
	switch code {
	case promotionTargetValidationCodeUnknown:
		return "context_overflow_promotion_target_id must reference an existing model"
	case promotionTargetValidationCodeSelf:
		return "context_overflow_promotion_target_id cannot reference the source model"
	case promotionTargetValidationCodeDisabled:
		return "context_overflow_promotion_target_id must reference an enabled model"
	case promotionTargetValidationCodeFacade:
		return "context_overflow_promotion_target_id must reference a non-facade model"
	case promotionTargetValidationCodeCrossProfile:
		return "context_overflow_promotion_target_id must reference a model in the selected profile"
	case promotionTargetValidationCodeSameTerminal:
		return "context_overflow_promotion_target_id must not resolve to the same terminal target as the source model"
	case promotionTargetValidationCodeAPIFamilyMismatch:
		return "context_overflow_promotion_target_id must reference a model with the same api_family"
	case promotionTargetValidationCodeCycle:
		return "context_overflow_promotion_target_id must not introduce a promotion target cycle"
	case promotionTargetValidationCodeMaxDepth:
		return "context_overflow_promotion_target_id promotion chain cannot exceed depth 3"
	default:
		panic(fmt.Sprintf("unexpected promotion target validation code %q", code))
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
	ModelID       string
	APIFamily     string
	IsEnabled     bool
	FacadeEnabled bool
}

func seedPromotionTargetScenario(t *testing.T, ctx context.Context, conn *pgx.Conn, now time.Time, scenarioCode string) (modelRecord, string, int) {
	t.Helper()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin scenario transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	profileID := insertPromotionProfile(t, ctx, tx, "profile-"+scenarioCode, now)
	strategyID := insertPromotionStrategy(t, ctx, tx, profileID, "strategy-"+scenarioCode, now)

	switch scenarioCode {
	case promotionTargetScenarioValid:
		source, _ := seedPromotionTerminalModel(t, ctx, tx, profileID, strategyID, now, promotionModelSeed{ModelID: "source-small", APIFamily: "openai", IsEnabled: true}, 8_000, 1.0)
		target, _ := seedPromotionTerminalModel(t, ctx, tx, profileID, strategyID, now, promotionModelSeed{ModelID: "target-large", APIFamily: "openai", IsEnabled: true}, 16_000, 1.0)
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("commit valid scenario: %v", err)
		}
		return source, target.ModelID, profileID
	case promotionTargetScenarioRecursiveValid:
		source, _ := seedPromotionTerminalModel(t, ctx, tx, profileID, strategyID, now, promotionModelSeed{ModelID: "source-recursive", APIFamily: "openai", IsEnabled: true}, 16_000, 1.0)
		intermediate, _ := seedPromotionTerminalModel(t, ctx, tx, profileID, strategyID, now, promotionModelSeed{ModelID: "target-recursive-small", APIFamily: "openai", IsEnabled: true}, 8_000, 1.0)
		terminal, _ := seedPromotionTerminalModel(t, ctx, tx, profileID, strategyID, now, promotionModelSeed{ModelID: "target-recursive-terminal", APIFamily: "openai", IsEnabled: true}, 32_000, 1.0)
		setPromotionTargetID(t, ctx, tx, intermediate.ID, terminal.ModelID)
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("commit recursive valid scenario: %v", err)
		}
		return source, intermediate.ModelID, profileID
	case promotionTargetValidationCodeUnknown:
		source, _ := seedPromotionTerminalModel(t, ctx, tx, profileID, strategyID, now, promotionModelSeed{ModelID: "source-openai", APIFamily: "openai", IsEnabled: true}, 8_000, 1.0)
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("commit unknown-target scenario: %v", err)
		}
		return source, "missing-promotion-target", profileID
	case promotionTargetValidationCodeSelf:
		source, _ := seedPromotionTerminalModel(t, ctx, tx, profileID, strategyID, now, promotionModelSeed{ModelID: "self-openai", APIFamily: "openai", IsEnabled: true}, 8_000, 1.0)
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("commit self-target scenario: %v", err)
		}
		return source, source.ModelID, profileID
	case promotionTargetValidationCodeDisabled:
		source, _ := seedPromotionTerminalModel(t, ctx, tx, profileID, strategyID, now, promotionModelSeed{ModelID: "source-disabled-check", APIFamily: "openai", IsEnabled: true}, 8_000, 1.0)
		target := insertPromotionModel(t, ctx, tx, profileID, strategyID, now, promotionModelSeed{ModelID: "target-disabled", APIFamily: "openai", IsEnabled: false})
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("commit disabled-target scenario: %v", err)
		}
		return source, target.ModelID, profileID
	case promotionTargetValidationCodeFacade:
		source, _ := seedPromotionTerminalModel(t, ctx, tx, profileID, strategyID, now, promotionModelSeed{ModelID: "source-facade-check", APIFamily: "openai", IsEnabled: true}, 8_000, 1.0)
		target := insertPromotionModel(t, ctx, tx, profileID, strategyID, now, promotionModelSeed{ModelID: "target-facade", APIFamily: "openai", IsEnabled: true, FacadeEnabled: true})
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("commit facade-target scenario: %v", err)
		}
		return source, target.ModelID, profileID
	case promotionTargetValidationCodeCrossProfile:
		source, _ := seedPromotionTerminalModel(t, ctx, tx, profileID, strategyID, now, promotionModelSeed{ModelID: "source-cross-profile", APIFamily: "openai", IsEnabled: true}, 8_000, 1.0)
		otherProfileID := insertPromotionProfileWithActive(t, ctx, tx, "profile-other", false, now)
		otherStrategyID := insertPromotionStrategy(t, ctx, tx, otherProfileID, "strategy-other", now)
		target, _ := seedPromotionTerminalModel(t, ctx, tx, otherProfileID, otherStrategyID, now, promotionModelSeed{ModelID: "target-cross-profile", APIFamily: "openai", IsEnabled: true}, 16_000, 1.0)
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("commit cross-profile scenario: %v", err)
		}
		return source, target.ModelID, profileID
	case promotionTargetValidationCodeSameTerminal:
		target, _ := seedPromotionTerminalModel(t, ctx, tx, profileID, strategyID, now, promotionModelSeed{ModelID: "target-same-terminal", APIFamily: "openai", IsEnabled: true}, 8_000, 1.0)
		source := insertPromotionModel(t, ctx, tx, profileID, strategyID, now, promotionModelSeed{ModelID: "source-same-terminal", APIFamily: "openai", IsEnabled: true})
		insertPromotionModelTarget(t, ctx, tx, profileID, source.ID, target.ID, 0, true, now)
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("commit same-terminal scenario: %v", err)
		}
		return source, target.ModelID, profileID
	case promotionTargetValidationCodeAPIFamilyMismatch:
		source, _ := seedPromotionTerminalModel(t, ctx, tx, profileID, strategyID, now, promotionModelSeed{ModelID: "source-openai-mismatch", APIFamily: "openai", IsEnabled: true}, 8_000, 1.0)
		target, _ := seedPromotionTerminalModel(t, ctx, tx, profileID, strategyID, now, promotionModelSeed{ModelID: "target-anthropic", APIFamily: "anthropic", IsEnabled: true}, 16_000, 1.0)
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("commit api-family mismatch scenario: %v", err)
		}
		return source, target.ModelID, profileID
	case promotionTargetValidationCodeCycle:
		source, _ := seedPromotionTerminalModel(t, ctx, tx, profileID, strategyID, now, promotionModelSeed{ModelID: "source-cycle", APIFamily: "openai", IsEnabled: true}, 8_000, 1.0)
		target, _ := seedPromotionTerminalModel(t, ctx, tx, profileID, strategyID, now, promotionModelSeed{ModelID: "target-cycle", APIFamily: "openai", IsEnabled: true}, 16_000, 1.0)
		setPromotionTargetID(t, ctx, tx, target.ID, source.ModelID)
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("commit promotion cycle scenario: %v", err)
		}
		return source, target.ModelID, profileID
	case promotionTargetValidationCodeMaxDepth:
		source, _ := seedPromotionTerminalModel(t, ctx, tx, profileID, strategyID, now, promotionModelSeed{ModelID: "source-depth", APIFamily: "openai", IsEnabled: true}, 8_000, 1.0)
		first, _ := seedPromotionTerminalModel(t, ctx, tx, profileID, strategyID, now, promotionModelSeed{ModelID: "target-depth-1", APIFamily: "openai", IsEnabled: true}, 16_000, 1.0)
		second, _ := seedPromotionTerminalModel(t, ctx, tx, profileID, strategyID, now, promotionModelSeed{ModelID: "target-depth-2", APIFamily: "openai", IsEnabled: true}, 24_000, 1.0)
		third, _ := seedPromotionTerminalModel(t, ctx, tx, profileID, strategyID, now, promotionModelSeed{ModelID: "target-depth-3", APIFamily: "openai", IsEnabled: true}, 32_000, 1.0)
		fourth, _ := seedPromotionTerminalModel(t, ctx, tx, profileID, strategyID, now, promotionModelSeed{ModelID: "target-depth-4", APIFamily: "openai", IsEnabled: true}, 40_000, 1.0)
		setPromotionTargetID(t, ctx, tx, first.ID, second.ModelID)
		setPromotionTargetID(t, ctx, tx, second.ID, third.ModelID)
		setPromotionTargetID(t, ctx, tx, third.ID, fourth.ModelID)
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("commit promotion max-depth scenario: %v", err)
		}
		return source, first.ModelID, profileID
	default:
		t.Fatalf("unexpected promotion target scenario %q", scenarioCode)
		return modelRecord{}, "", 0
	}
}

func seedPromotionTerminalModel(t *testing.T, ctx context.Context, tx pgx.Tx, profileID int, strategyID int, now time.Time, seed promotionModelSeed, contextWindowTokens int, maxContextUtilization float64) (modelRecord, int) {
	t.Helper()
	model := insertPromotionModel(t, ctx, tx, profileID, strategyID, now, seed)
	endpointID := insertPromotionEndpoint(t, ctx, tx, profileID, seed.ModelID+"-endpoint", now)
	connectionID := insertPromotionConnection(t, ctx, tx, profileID, endpointID, seed.APIFamily, seed.ModelID+"-connection", contextWindowTokens, maxContextUtilization, now)
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
	selectionPolicy := (*string)(nil)
	fallbackPolicy := (*string)(nil)
	if seed.FacadeEnabled {
		selectionPolicy = stringPtr(facadeSelectionPolicyOrderedEligibleContext)
		fallbackPolicy = stringPtr(facadeFallbackPolicySkipIneligibleTargets)
	}
	record := modelRecord{
		ProfileID:                 profileID,
		APIFamily:                 seed.APIFamily,
		ModelID:                   seed.ModelID,
		DisplayName:               stringPtr(seed.ModelID),
		LoadbalanceStrategyID:     intPtr(strategyID),
		DefaultOutputTokenReserve: 4096,
		MaxContextUtilization:     0.9,
		FacadeEnabled:             seed.FacadeEnabled,
		FacadeSelectionPolicy:     selectionPolicy,
		FacadeFallbackPolicy:      fallbackPolicy,
		IsEnabled:                 seed.IsEnabled,
		CreatedAt:                 now,
		UpdatedAt:                 now,
	}
	if seed.APIFamily == "openai" {
		record.OpenAIAcceptedFormat = stringPtr(openAIAcceptedFormatDualNative)
	}
	return mustInsertModelRecord(t, ctx, tx, record)
}

func setPromotionTargetID(t *testing.T, ctx context.Context, tx pgx.Tx, modelConfigID int, targetModelID string) {
	t.Helper()
	if _, err := tx.Exec(ctx, `UPDATE model_configs SET context_overflow_promotion_target_id = $2 WHERE id = $1`, modelConfigID, targetModelID); err != nil {
		t.Fatalf("set promotion target for model %d: %v", modelConfigID, err)
	}
}

func insertPromotionEndpoint(t *testing.T, ctx context.Context, tx pgx.Tx, profileID int, name string, now time.Time) int {
	t.Helper()
	var endpointID int
	if err := tx.QueryRow(ctx, `INSERT INTO endpoints (profile_id, name, base_url, api_key, position, created_at, updated_at) VALUES ($1, $2, $3, $4, 0, $5, $5) RETURNING id`, profileID, name, fmt.Sprintf("https://%s.example", name), "test-api-key", now).Scan(&endpointID); err != nil {
		t.Fatalf("insert endpoint %q: %v", name, err)
	}
	return endpointID
}

func insertPromotionConnection(t *testing.T, ctx context.Context, tx pgx.Tx, profileID int, endpointID int, apiFamily string, name string, contextWindowTokens int, maxContextUtilization float64, now time.Time) int {
	t.Helper()
	var connectionID int
	var openAITextCapability any
	if apiFamily == "openai" {
		openAITextCapability = "responses_only"
	}
	if err := tx.QueryRow(ctx, `INSERT INTO connections (profile_id, api_family, endpoint_id, context_window_tokens, default_output_token_reserve, max_context_utilization, pricing_template_id, qps_limit, max_in_flight_non_stream, max_in_flight_stream, openai_probe_endpoint_variant, openai_text_capability, is_active, priority, name, auth_type, custom_headers, health_status, health_detail, last_health_check, created_at, updated_at) VALUES ($1, $2, $3, $4, 4096, $5, NULL, NULL, NULL, NULL, NULL, $8, TRUE, 0, $6, NULL, NULL, 'healthy', NULL, NULL, $7, $7) RETURNING id`, profileID, apiFamily, endpointID, contextWindowTokens, maxContextUtilization, name, now, openAITextCapability).Scan(&connectionID); err != nil {
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
