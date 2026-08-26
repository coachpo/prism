package contracttest

import (
	"context"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	managementconfigrules "github.com/coachpo/prism/backend/internal/httpapi/management/configrules"
	managementconnections "github.com/coachpo/prism/backend/internal/httpapi/management/connections"
	managementloadbalance "github.com/coachpo/prism/backend/internal/httpapi/management/loadbalance"
	managementsettings "github.com/coachpo/prism/backend/internal/httpapi/management/settings"
	"github.com/coachpo/prism/backend/internal/platform/config"
	platformhttp "github.com/coachpo/prism/backend/internal/platform/http"
)

func TestCostingSettings(t *testing.T) {
	harness := newS11ContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)

	initialPayload := requestJSONStatus[map[string]any](t, harness, http.MethodGet, "/api/settings/costing", nil, modelHeader(defaultProfileID), http.StatusOK)
	if initialPayload["report_currency_code"] != "USD" || initialPayload["report_currency_symbol"] != "$" {
		t.Fatalf("expected default costing settings, got %+v", initialPayload)
	}
	if initialPayload["reporting_currency_epoch"] != "1" || initialPayload["pricing_migration_state"] != "ready" {
		t.Fatalf("expected active epoch 1 and ready migration state, got %+v", initialPayload)
	}
	if _, ok := initialPayload["endpoint_fx_mappings"]; ok {
		t.Fatalf("endpoint_fx_mappings must not exist in the steady-state response, got %+v", initialPayload)
	}

	// Legacy currency-code authoring is rejected; currency migration is the
	// only code-change path.
	codeChange := harness.requestJSON(t, harness.client, http.MethodPut, "/api/settings/costing", map[string]any{
		"report_currency_code":   "EUR",
		"report_currency_symbol": "€",
		"timezone_preference":    "Europe/Helsinki",
	}, modelHeader(defaultProfileID))
	assertErrorResponse(t, codeChange, http.StatusUnprocessableEntity, "unknown_field: report_currency_code is not accepted; migrate the reporting currency through the currency migration flow")

	// FX authoring fields are rejected.
	fxAuthoring := harness.requestJSON(t, harness.client, http.MethodPut, "/api/settings/costing", map[string]any{
		"report_currency_symbol": "$",
		"endpoint_fx_mappings":   []map[string]any{{"model_id": "s11-costing-model", "endpoint_id": 1, "fx_rate": "0.92"}},
	}, modelHeader(defaultProfileID))
	assertErrorResponse(t, fxAuthoring, http.StatusUnprocessableEntity, "unknown_field: endpoint_fx_mappings is not accepted; FX authoring was removed")

	// Symbol-only + timezone updates are allowed and keep the code locked.
	updatedPayload, loadedPayload := putThenGetJSON(t, harness, "/api/settings/costing", map[string]any{
		"report_currency_symbol": " US$ ",
		"timezone_preference":    " Europe/Helsinki ",
	}, modelHeader(defaultProfileID))
	if updatedPayload["profile_id"] != float64(defaultProfileID) || updatedPayload["report_currency_code"] != "USD" || updatedPayload["report_currency_symbol"] != "US$" || updatedPayload["timezone_preference"] != "Europe/Helsinki" {
		t.Fatalf("expected updated costing settings payload, got %+v", updatedPayload)
	}
	if updatedPayload["reporting_currency_epoch"] != "1" {
		t.Fatalf("expected symbol-only update to keep epoch 1, got %+v", updatedPayload)
	}
	if loadedPayload["report_currency_code"] != "USD" || loadedPayload["report_currency_symbol"] != "US$" || loadedPayload["timezone_preference"] != "Europe/Helsinki" {
		t.Fatalf("expected costing settings round-trip to persist, got %+v", loadedPayload)
	}

	// The active epoch row carries the same canonical symbol (SPEC 5.3).
	var epochSymbol string
	if err := harness.conn.QueryRow(context.Background(), `SELECT epochs.currency_symbol FROM reporting_currency_epochs AS epochs JOIN user_settings AS settings ON settings.current_reporting_currency_epoch_id = epochs.id WHERE settings.profile_id = $1`, defaultProfileID).Scan(&epochSymbol); err != nil {
		t.Fatalf("load active epoch symbol: %v", err)
	}
	if epochSymbol != "US$" {
		t.Fatalf("expected active epoch symbol to follow the settings symbol, got %q", epochSymbol)
	}

	// Stale CAS is rejected.
	stale := harness.requestJSON(t, harness.client, http.MethodPut, "/api/settings/costing", map[string]any{
		"expected_updated_at":    "2000-01-01T00:00:00Z",
		"report_currency_symbol": "$",
	}, modelHeader(defaultProfileID))
	assertErrorResponse(t, stale, http.StatusConflict, "costing_settings_changed")
}

func TestTimezoneSettings(t *testing.T) {
	harness := newS11ContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)

	// Timezone is the Pricing-owned costing surface (Settings SPEC §11.1): the
	// standalone timezone route was removed; timezone_preference shares the
	// costing CAS with report currency.
	payload := requestJSONStatus[map[string]any](t, harness, http.MethodGet, "/api/settings/costing", nil, modelHeader(defaultProfileID), http.StatusOK)
	if payload["timezone_preference"] != nil {
		t.Fatalf("expected default null timezone preference, got %+v", payload)
	}
	updatedAt := asString(t, payload["updated_at"])

	payload = requestJSONStatus[map[string]any](t, harness, http.MethodPut, "/api/settings/costing", map[string]any{"expected_updated_at": updatedAt, "timezone_preference": " America/New_York "}, modelHeader(defaultProfileID), http.StatusOK)
	if payload["timezone_preference"] != "America/New_York" {
		t.Fatalf("expected trimmed timezone preference, got %+v", payload)
	}
	updatedAt = asString(t, payload["updated_at"])

	payload = requestJSONStatus[map[string]any](t, harness, http.MethodPut, "/api/settings/costing", map[string]any{"expected_updated_at": updatedAt, "timezone_preference": "   "}, modelHeader(defaultProfileID), http.StatusOK)
	if payload["timezone_preference"] != nil {
		t.Fatalf("expected blank timezone preference to clear to null, got %+v", payload)
	}
}

func asString(t *testing.T, value any) string {
	t.Helper()
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	t.Fatalf("expected string, got %T %+v", value, value)
	return ""
}

func newS11ContractHarness(t *testing.T) *contractHarness {
	t.Helper()
	return newContractHarnessFor(t, "s11_contract", contractHarnessOptions{
		SecretEncryptionKey: "s11-contract-secret",
		Version:             "s11-contract-test",
		DependenciesBuilder: func(t *testing.T, testContext context.Context, harness *contractHarness, settings config.Settings, pool *pgxpool.Pool) platformhttp.Dependencies {
			t.Helper()
			settingsService, err := managementsettings.NewService(settings, managementsettings.Options{Pool: pool})
			if err != nil {
				t.Fatalf("build settings service: %v", err)
			}
			t.Cleanup(settingsService.Close)
			loadbalanceService, err := managementloadbalance.NewService(settings, managementloadbalance.Options{Pool: pool})
			if err != nil {
				t.Fatalf("build loadbalance service: %v", err)
			}
			t.Cleanup(loadbalanceService.Close)
			configRulesService, err := managementconfigrules.NewService(settings, managementconfigrules.Options{Pool: pool})
			if err != nil {
				t.Fatalf("build config rules service: %v", err)
			}
			t.Cleanup(configRulesService.Close)
			connectionsService, err := managementconnections.NewService(settings, managementconnections.Options{Pool: pool})
			if err != nil {
				t.Fatalf("build connections service: %v", err)
			}
			t.Cleanup(connectionsService.Close)
			return platformhttp.Dependencies{
				SettingsService:    settingsService,
				LoadbalanceService: loadbalanceService,
				ConfigRulesService: configRulesService,
				ConnectionsService: connectionsService,
			}
		},
	})
}

func requestJSONStatus[T any](t *testing.T, harness *contractHarness, method string, path string, body any, headers map[string]string, wantStatus int) T {
	t.Helper()
	response := harness.requestJSON(t, harness.client, method, path, body, headers)
	assertStatus(t, response, wantStatus)
	var payload T
	decodeJSONResponse(t, response, &payload)
	return payload
}

func decodeJSONMap(t *testing.T, response *http.Response) map[string]any {
	t.Helper()
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	return payload
}

func putThenGetJSON(t *testing.T, harness *contractHarness, path string, body map[string]any, headers map[string]string) (map[string]any, map[string]any) {
	t.Helper()
	return requestJSONStatus[map[string]any](t, harness, http.MethodPut, path, body, headers, http.StatusOK), requestJSONStatus[map[string]any](t, harness, http.MethodGet, path, nil, headers, http.StatusOK)
}
