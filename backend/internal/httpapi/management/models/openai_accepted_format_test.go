package models

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCreateModelRequiresOpenAIAcceptedFormatForOpenAI(t *testing.T) {
	ctx, conn, dsn := createPromotionTargetTestDatabase(t, "create_requires_openai_accepted_format")
	now := time.Date(2026, time.June, 17, 12, 0, 0, 0, time.UTC)
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin seed transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	profileID := insertPromotionProfile(t, ctx, tx, "create-format-profile", now)
	strategyID := insertPromotionStrategy(t, ctx, tx, profileID, "create-format-strategy", now)
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit seed transaction: %v", err)
	}
	router := newPromotionTargetRouter(t, ctx, dsn, now)

	response := performPromotionTargetModelRequest(t, router, http.MethodPost, "/models", profileID, map[string]any{
		"api_family":                   "openai",
		"model_id":                     "missing-openai-accepted-format",
		"loadbalance_strategy_id":      strategyID,
		"default_output_token_reserve": 4096,
		"max_context_utilization":      0.9,
		"is_enabled":                   false,
	})
	assertModelRouteDetail(t, response, http.StatusBadRequest, "openai_accepted_format is required when api_family is 'openai'")
}

func TestUpdateModelRejectsOpenAIAcceptedFormatForNonOpenAI(t *testing.T) {
	ctx, conn, dsn := createPromotionTargetTestDatabase(t, "update_rejects_openai_accepted_format_non_openai")
	now := time.Date(2026, time.June, 17, 12, 5, 0, 0, time.UTC)
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin seed transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	profileID := insertPromotionProfile(t, ctx, tx, "non-openai-format-profile", now)
	strategyID := insertPromotionStrategy(t, ctx, tx, profileID, "non-openai-format-strategy", now)
	model := insertPromotionModel(t, ctx, tx, profileID, strategyID, now, promotionModelSeed{ModelID: "claude-public", APIFamily: "anthropic", IsEnabled: false})
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit seed transaction: %v", err)
	}
	router := newPromotionTargetRouter(t, ctx, dsn, now)

	response := performPromotionTargetModelRequest(t, router, http.MethodPut, fmt.Sprintf("/models/%d", model.ID), profileID, map[string]any{
		"openai_accepted_format": openAIAcceptedFormatDualNative,
	})
	assertModelRouteDetail(t, response, http.StatusBadRequest, "openai_accepted_format is only allowed when api_family is 'openai'")
}

func TestListModelsIncludesOpenAIAcceptedFormat(t *testing.T) {
	ctx, conn, dsn := createPromotionTargetTestDatabase(t, "list_includes_openai_accepted_format")
	now := time.Date(2026, time.June, 17, 12, 10, 0, 0, time.UTC)
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin seed transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	profileID := insertPromotionProfile(t, ctx, tx, "list-format-profile", now)
	strategyID := insertPromotionStrategy(t, ctx, tx, profileID, "list-format-strategy", now)
	openAIModel := insertPromotionModel(t, ctx, tx, profileID, strategyID, now, promotionModelSeed{ModelID: "gpt-public", APIFamily: "openai", IsEnabled: false})
	openAITarget := insertPromotionModel(t, ctx, tx, profileID, strategyID, now, promotionModelSeed{ModelID: "gpt-target", APIFamily: "openai", IsEnabled: false})
	insertPromotionModelTarget(t, ctx, tx, profileID, openAIModel.ID, openAITarget.ID, 0, true, now)
	anthropicModel := insertPromotionModel(t, ctx, tx, profileID, strategyID, now, promotionModelSeed{ModelID: "claude-public", APIFamily: "anthropic", IsEnabled: false})
	endpointModel, connectionID := seedPromotionTerminalModel(t, ctx, tx, profileID, strategyID, now, promotionModelSeed{ModelID: "gpt-by-endpoint", APIFamily: "openai", IsEnabled: true}, 16_000, 0.9)
	endpointID := loadOpenAIAcceptedFormatConnectionEndpointID(t, ctx, tx, connectionID)
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit seed transaction: %v", err)
	}
	router := newPromotionTargetRouter(t, ctx, dsn, now)

	listResponse := performPromotionTargetModelRequest(t, router, http.MethodGet, "/models", profileID, nil)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("expected GET /models to succeed, got %d: %s", listResponse.Code, listResponse.Body.String())
	}
	var listed []modelConfigListResponse
	decodePromotionTargetJSON(t, listResponse, &listed)
	openAIItem := findListedModelByID(t, listed, openAIModel.ID)
	if openAIItem.OpenAIAcceptedFormat == nil || *openAIItem.OpenAIAcceptedFormat != openAIAcceptedFormatDualNative {
		t.Fatalf("expected OpenAI list item to expose %q, got %v", openAIAcceptedFormatDualNative, openAIItem.OpenAIAcceptedFormat)
	}
	if len(openAIItem.AccessTargets) != 1 || openAIItem.AccessTargets[0].TargetModel == nil {
		t.Fatalf("expected OpenAI list item to include nested target model, got %+v", openAIItem.AccessTargets)
	}
	nestedFormat := openAIItem.AccessTargets[0].TargetModel.OpenAIAcceptedFormat
	if nestedFormat == nil || *nestedFormat != openAIAcceptedFormatDualNative {
		t.Fatalf("expected nested OpenAI target model to expose %q, got %v", openAIAcceptedFormatDualNative, nestedFormat)
	}
	anthropicItem := findListedModelByID(t, listed, anthropicModel.ID)
	if anthropicItem.OpenAIAcceptedFormat != nil {
		t.Fatalf("expected non-OpenAI list item to omit openai_accepted_format, got %q", *anthropicItem.OpenAIAcceptedFormat)
	}

	getResponse := performPromotionTargetModelRequest(t, router, http.MethodGet, fmt.Sprintf("/models/%d", openAIModel.ID), profileID, nil)
	if getResponse.Code != http.StatusOK {
		t.Fatalf("expected GET /models/%d to succeed, got %d: %s", openAIModel.ID, getResponse.Code, getResponse.Body.String())
	}
	var detail modelConfigResponse
	decodePromotionTargetJSON(t, getResponse, &detail)
	if detail.OpenAIAcceptedFormat == nil || *detail.OpenAIAcceptedFormat != openAIAcceptedFormatDualNative {
		t.Fatalf("expected OpenAI detail to expose %q, got %v", openAIAcceptedFormatDualNative, detail.OpenAIAcceptedFormat)
	}

	endpointResponse := performPromotionTargetModelRequest(t, router, http.MethodGet, fmt.Sprintf("/models/by-endpoint/%d", endpointID), profileID, nil)
	if endpointResponse.Code != http.StatusOK {
		t.Fatalf("expected GET /models/by-endpoint/%d to succeed, got %d: %s", endpointID, endpointResponse.Code, endpointResponse.Body.String())
	}
	var endpointModels []modelConfigListResponse
	decodePromotionTargetJSON(t, endpointResponse, &endpointModels)
	endpointItem := findListedModelByID(t, endpointModels, endpointModel.ID)
	if endpointItem.OpenAIAcceptedFormat == nil || *endpointItem.OpenAIAcceptedFormat != openAIAcceptedFormatDualNative {
		t.Fatalf("expected endpoint OpenAI model to expose %q, got %v", openAIAcceptedFormatDualNative, endpointItem.OpenAIAcceptedFormat)
	}
}

func loadOpenAIAcceptedFormatConnectionEndpointID(t *testing.T, ctx context.Context, tx queryExecutor, connectionID int) int {
	t.Helper()
	var endpointID int
	if err := tx.QueryRow(ctx, `SELECT endpoint_id FROM connections WHERE id = $1`, connectionID).Scan(&endpointID); err != nil {
		t.Fatalf("load endpoint for connection %d: %v", connectionID, err)
	}
	return endpointID
}

func assertModelRouteDetail(t *testing.T, response *httptest.ResponseRecorder, expectedStatus int, expectedDetail string) {
	t.Helper()
	if response.Code != expectedStatus {
		t.Fatalf("expected status %d, got %d: %s", expectedStatus, response.Code, response.Body.String())
	}
	var payload modelRouteErrorResponse
	decodePromotionTargetJSON(t, response, &payload)
	if payload.Detail != expectedDetail {
		t.Fatalf("expected detail %q, got %q", expectedDetail, payload.Detail)
	}
}
