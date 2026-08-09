package contracttest

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	loadbalancedomain "github.com/coachpo/prism/backend/internal/domain/loadbalance"
	managementconnections "github.com/coachpo/prism/backend/internal/httpapi/management/connections"
	managementendpoints "github.com/coachpo/prism/backend/internal/httpapi/management/endpoints"
	managementmodels "github.com/coachpo/prism/backend/internal/httpapi/management/models"
	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
	"github.com/coachpo/prism/backend/internal/platform/config"
	platformhttp "github.com/coachpo/prism/backend/internal/platform/http"
)

// newReverseContractHarness mounts management (models/endpoints/connections)
// and the runtime proxy branch so reverse contracts can be exercised over
// live surfaces.
func newReverseContractHarness(t *testing.T) *contractHarness {
	t.Helper()
	return newContractHarnessFor(t, "reverse_contract", contractHarnessOptions{
		SecretEncryptionKey: "reverse-contract-secret",
		Version:             "reverse-contract-test",
		DependenciesBuilder: func(t *testing.T, testContext context.Context, harness *contractHarness, settings config.Settings, pool *pgxpool.Pool) platformhttp.Dependencies {
			t.Helper()
			runtimeState := loadbalancedomain.NewLocalRuntimeStateStore()
			connectionsService, err := managementconnections.NewService(settings, managementconnections.Options{Pool: pool})
			if err != nil {
				t.Fatalf("build connections service: %v", err)
			}
			t.Cleanup(connectionsService.Close)
			endpointsService, err := managementendpoints.NewService(settings, managementendpoints.Options{Pool: pool})
			if err != nil {
				t.Fatalf("build endpoints service: %v", err)
			}
			t.Cleanup(endpointsService.Close)
			modelsService, err := managementmodels.NewService(settings, managementmodels.Options{Pool: pool, SecretEncryptionKey: settings.SecretEncryptionKey})
			if err != nil {
				t.Fatalf("build models service: %v", err)
			}
			t.Cleanup(modelsService.Close)
			runtimeCache := runtimeapi.NewSharedCacheWithOptions(runtimeapi.SharedCacheOptions{RefreshPool: pool, SecretEncryptionKey: settings.SecretEncryptionKey})
			if err := runtimeCache.Bootstrap(testContext); err != nil {
				t.Fatalf("bootstrap published runtime snapshot: %v", err)
			}
			telemetryPool, err := pgxpool.New(testContext, settings.DatabaseURL)
			if err != nil {
				t.Fatalf("build runtime telemetry pgx pool: %v", err)
			}
			t.Cleanup(telemetryPool.Close)
			feedbackPool, err := pgxpool.New(testContext, settings.DatabaseURL)
			if err != nil {
				t.Fatalf("build runtime feedback pgx pool: %v", err)
			}
			t.Cleanup(feedbackPool.Close)
			runtimeService, err := runtimeapi.NewService(settings, runtimeapi.Options{ExecutionPool: pool, TelemetryPool: telemetryPool, FeedbackPool: feedbackPool, Cache: runtimeCache, RuntimeState: runtimeState})
			if err != nil {
				t.Fatalf("build runtime service: %v", err)
			}
			t.Cleanup(runtimeService.Close)
			harness.runtimeService = runtimeService
			harness.runtimeCache = runtimeCache
			return platformhttp.Dependencies{
				ConnectionsService: connectionsService,
				EndpointsService:   endpointsService,
				ModelsService:      modelsService,
				RuntimeService:     runtimeService,
				RuntimeCache:       runtimeCache,
				RuntimeState:       runtimeState,
			}
		},
	})
}

// TestReverseContractNoExactMatchOrTranslationRemnants pins the absence of
// the superseded OpenAI exact-match contract and the retired translation
// terminology across live runtime surfaces. The three stable planning codes
// are the only OpenAI text planning rejections, and Partial coverage writes
// succeed with structured warnings instead of mismatch hard errors.
func TestReverseContractNoExactMatchOrTranslationRemnants(t *testing.T) {
	harness := newReverseContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Reverse Contract Strategy")

	t.Run("runtime never exposes retired translation codes", func(t *testing.T) {
		modelConfigID := modelInsertModel(t, harness, profileID, modelLoadVendorIDByKey(t, harness, "openai"), "openai", "reverse-contract-runtime-"+randomSuffix(), nil, "native", &strategyID, true)
		endpointID := modelInsertEndpoint(t, harness, profileID, "Reverse Contract Runtime Endpoint", 0)
		modelInsertConnectionWithCapability(t, harness, profileID, modelConfigID, endpointID, "chat_completions_only", 0, true)
		modelID := modelIDForConfig(t, harness, modelConfigID)

		payload := s15JSON[map[string]any](t, harness, profileID, http.MethodPost, "/v1/responses", map[string]any{"model": modelID, "input": "reverse contract"}, http.StatusServiceUnavailable)
		errorBody := fmt.Sprintf("%+v", payload)
		for _, forbidden := range []string{"openai_request_translation_unsupported", "openai_mode_mismatch", "operation_translation_unsupported"} {
			if strings.Contains(errorBody, forbidden) {
				t.Fatalf("runtime error must not expose retired %q, got %+v", forbidden, payload)
			}
		}
		if payload["error"] != "openai_no_compatible_terminal_target" {
			t.Fatalf("expected stable openai_no_compatible_terminal_target code, got %+v", payload)
		}
	})

	t.Run("partial coverage write succeeds with structured warnings", func(t *testing.T) {
		modelConfigID := modelInsertModel(t, harness, profileID, modelLoadVendorIDByKey(t, harness, "openai"), "openai", "reverse-contract-partial-"+randomSuffix(), nil, "native", &strategyID, true)
		endpointID := modelInsertEndpoint(t, harness, profileID, "Reverse Contract Partial Endpoint", 0)
		payload := s15JSON[map[string]any](t, harness, profileID, http.MethodPost, fmt.Sprintf("/api/models/%d/connections", modelConfigID), map[string]any{
			"endpoint_id":            endpointID,
			"openai_text_capability": "chat_completions_only",
		}, http.StatusCreated)
		warnings := payload["configuration_warnings"].([]any)
		if len(warnings) == 0 {
			t.Fatalf("expected partial coverage warning envelope, got %+v", payload)
		}
		if strings.Contains(fmt.Sprintf("%+v", payload), "mismatch") {
			t.Fatalf("partial coverage must not be rejected as a mismatch, got %+v", payload)
		}
	})

	t.Run("management surface never exposes mode-mismatch or translation codes", func(t *testing.T) {
		modelConfigID := modelInsertModel(t, harness, profileID, modelLoadVendorIDByKey(t, harness, "openai"), "openai", "reverse-contract-diagnostics-"+randomSuffix(), nil, "native", &strategyID, true)
		payload := s15GET[map[string]any](t, harness, profileID, fmt.Sprintf("/api/models/%d/routing-diagnostics", modelConfigID), http.StatusOK)
		raw := fmt.Sprintf("%+v", payload)
		for _, forbidden := range []string{"mode_mismatch", "request_translation", "strict_equality"} {
			if strings.Contains(raw, forbidden) {
				t.Fatalf("diagnostics must not expose %q, got %+v", forbidden, payload)
			}
		}
	})
}

func modelIDForConfig(t *testing.T, harness *contractHarness, modelConfigID int) string {
	t.Helper()
	var modelID string
	if err := harness.conn.QueryRow(context.Background(), `SELECT model_id FROM model_configs WHERE id = $1`, modelConfigID).Scan(&modelID); err != nil {
		t.Fatalf("load model id: %v", err)
	}
	return modelID
}
