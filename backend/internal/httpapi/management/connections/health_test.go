package connections

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/coachpo/prism/backend/internal/endpointdomain"
)

const healthTestSecretKey = "health-test-runtime-secret"

type recordedHealthProbeRequest struct {
	Path    string
	Headers http.Header
	Body    map[string]any
}

type healthProbeRecorder struct {
	mu       sync.Mutex
	requests []recordedHealthProbeRequest
	server   *httptest.Server
}

func newHealthProbeRecorder(t *testing.T) *healthProbeRecorder {
	t.Helper()
	recorder := &healthProbeRecorder{}
	recorder.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() { _ = r.Body.Close() }()
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		recorder.mu.Lock()
		recorder.requests = append(recorder.requests, recordedHealthProbeRequest{Path: r.URL.Path, Headers: r.Header.Clone(), Body: body})
		recorder.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(recorder.server.Close)
	return recorder
}

func (r *healthProbeRecorder) snapshotRequests() []recordedHealthProbeRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]recordedHealthProbeRequest, len(r.requests))
	copy(items, r.requests)
	return items
}

func TestProbeConnectionHealthCharacterizesFamilyVariantsAndHeaders(t *testing.T) {
	tests := []struct {
		name            string
		apiFamily       string
		modelID         string
		variant         *string
		customHeaders   map[string]string
		expectedPath    string
		expectedAuthKey string
		expectedAuth    string
		expectedHeaders map[string]string
		absentHeaders   []string
		assertBody      func(*testing.T, map[string]any)
	}{
		{
			name:            "openai responses minimal default",
			apiFamily:       "openai",
			modelID:         "gpt-test",
			expectedPath:    "/probe-root/v1/responses",
			expectedAuthKey: "Authorization",
			expectedAuth:    "Bearer probe-secret",
			expectedHeaders: map[string]string{"X-Allow-Smoke": "still-here"},
			absentHeaders:   []string{"X-Blocked-Secret", "X-Correlation-Id"},
			assertBody: func(t *testing.T, body map[string]any) {
				t.Helper()
				if body["model"] != "gpt-test" || body["max_output_tokens"] != float64(1) || body["reasoning"] != nil || body["input"] == nil {
					t.Fatalf("expected minimal OpenAI responses body, got %+v", body)
				}
			},
		},
		{
			name:            "openai responses reasoning none",
			apiFamily:       "openai",
			modelID:         "gpt-reasoning",
			variant:         connectionStringRef("responses_reasoning_none"),
			expectedPath:    "/probe-root/v1/responses",
			expectedAuthKey: "Authorization",
			expectedAuth:    "Bearer probe-secret",
			assertBody: func(t *testing.T, body map[string]any) {
				t.Helper()
				reasoning := body["reasoning"].(map[string]any)
				if body["model"] != "gpt-reasoning" || reasoning["effort"] != "none" {
					t.Fatalf("expected OpenAI responses reasoning none body, got %+v", body)
				}
			},
		},
		{
			name:            "openai chat completions minimal",
			apiFamily:       "openai",
			modelID:         "gpt-chat",
			variant:         connectionStringRef("chat_completions_minimal"),
			expectedPath:    "/probe-root/v1/chat/completions",
			expectedAuthKey: "Authorization",
			expectedAuth:    "Bearer probe-secret",
			assertBody: func(t *testing.T, body map[string]any) {
				t.Helper()
				if body["model"] != "gpt-chat" || body["max_tokens"] != float64(1) || body["reasoning_effort"] != nil || body["messages"] == nil {
					t.Fatalf("expected minimal OpenAI chat completions body, got %+v", body)
				}
			},
		},
		{
			name:            "openai chat completions reasoning none",
			apiFamily:       "openai",
			modelID:         "gpt-chat-reasoning",
			variant:         connectionStringRef("chat_completions_reasoning_none"),
			expectedPath:    "/probe-root/v1/chat/completions",
			expectedAuthKey: "Authorization",
			expectedAuth:    "Bearer probe-secret",
			assertBody: func(t *testing.T, body map[string]any) {
				t.Helper()
				if body["model"] != "gpt-chat-reasoning" || body["reasoning_effort"] != "none" {
					t.Fatalf("expected OpenAI chat completions reasoning none body, got %+v", body)
				}
			},
		},
		{
			name:            "anthropic messages",
			apiFamily:       "anthropic",
			modelID:         "claude-test",
			customHeaders:   map[string]string{"anthropic-version": "custom-version"},
			expectedPath:    "/probe-root/v1/messages",
			expectedAuthKey: "x-api-key",
			expectedAuth:    "probe-secret",
			expectedHeaders: map[string]string{"anthropic-version": "2023-06-01"},
			assertBody: func(t *testing.T, body map[string]any) {
				t.Helper()
				if body["model"] != "claude-test" || body["max_tokens"] != float64(1) || body["messages"] == nil {
					t.Fatalf("expected Anthropic messages body, got %+v", body)
				}
			},
		},
		{
			name:            "gemini generate content",
			apiFamily:       "gemini",
			modelID:         "gemini-pro",
			expectedPath:    "/probe-root/v1beta/models/gemini-pro:generateContent",
			expectedAuthKey: "Authorization",
			expectedAuth:    "Bearer probe-secret",
			assertBody: func(t *testing.T, body map[string]any) {
				t.Helper()
				generationConfig := body["generationConfig"].(map[string]any)
				if generationConfig["maxOutputTokens"] != float64(1) || body["contents"] == nil {
					t.Fatalf("expected Gemini generateContent body, got %+v", body)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := newHealthProbeRecorder(t)
			encryptedAPIKey, err := endpointdomain.EncryptSecret(" probe-secret ", healthTestSecretKey, func() time.Time { return time.Unix(0, 0).UTC() })
			if err != nil {
				t.Fatalf("encrypt test endpoint api key: %v", err)
			}
			service := &Service{httpClient: recorder.server.Client(), secretEncryptionKey: healthTestSecretKey}
			customHeaders := map[string]string{"Authorization": "Bearer attacker", "X-Allow-Smoke": " still-here ", "X-Blocked-Secret": "blocked", "X-Correlation-ID": "blocked"}
			for key, value := range tt.customHeaders {
				customHeaders[key] = value
			}
			result, err := service.probeConnectionHealth(context.Background(), connectionHealthProbeInput{
				ConnectionID:               7,
				CustomHeaders:              customHeaders,
				Endpoint:                   endpointRecord{BaseURL: recorder.server.URL + "/probe-root/", APIKey: encryptedAPIKey},
				APIFamily:                  tt.apiFamily,
				ModelID:                    tt.modelID,
				OpenAIProbeEndpointVariant: tt.variant,
				HeaderBlocklistRules:       []headerBlocklistRuleRecord{{MatchType: "prefix", Pattern: "x-blocked"}, {MatchType: "exact", Pattern: "x-correlation-id"}},
			})
			if err != nil {
				t.Fatalf("probeConnectionHealth returned error: %v", err)
			}
			if result.HealthStatus != "healthy" || result.Detail != "Connection successful" || result.ResponseTimeMS <= 0 {
				t.Fatalf("expected healthy probe result, got %+v", result)
			}
			requests := recorder.snapshotRequests()
			if len(requests) != 2 {
				t.Fatalf("expected two persisted probe requests, got %+v", requests)
			}
			for index, request := range requests {
				if request.Path != tt.expectedPath {
					t.Fatalf("expected request %d path %q, got %+v", index, tt.expectedPath, request)
				}
				if request.Headers.Get(tt.expectedAuthKey) != tt.expectedAuth {
					t.Fatalf("expected request %d auth header %s=%q, got %+v", index, tt.expectedAuthKey, tt.expectedAuth, request.Headers)
				}
				for key, value := range tt.expectedHeaders {
					if request.Headers.Get(key) != value {
						t.Fatalf("expected request %d header %s=%q, got %+v", index, key, value, request.Headers)
					}
				}
				for _, key := range tt.absentHeaders {
					if request.Headers.Get(key) != "" {
						t.Fatalf("expected request %d header %s to be absent, got %+v", index, key, request.Headers)
					}
				}
				tt.assertBody(t, request.Body)
			}
		})
	}
}

func TestResolveHealthCheckAuthConfigCharacterizesAuthProfileOverrides(t *testing.T) {
	tests := []struct {
		name       string
		apiFamily  string
		authType   *string
		wantHeader string
		wantPrefix string
		wantExtra  map[string]string
		wantError  string
	}{
		{name: "openai default", apiFamily: "openai", wantHeader: "Authorization", wantPrefix: "Bearer ", wantExtra: map[string]string{}},
		{name: "anthropic default", apiFamily: "anthropic", wantHeader: "x-api-key", wantPrefix: "", wantExtra: map[string]string{"anthropic-version": "2023-06-01"}},
		{name: "gemini default", apiFamily: "gemini", wantHeader: "Authorization", wantPrefix: "Bearer ", wantExtra: map[string]string{}},
		{name: "auth type overrides family", apiFamily: "openai", authType: connectionStringRef(" anthropic "), wantHeader: "x-api-key", wantPrefix: "", wantExtra: map[string]string{"anthropic-version": "2023-06-01"}},
		{name: "unsupported auth type", apiFamily: "openai", authType: connectionStringRef("unknown"), wantError: "unsupported auth_type: unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveHealthCheckAuthConfig(tt.authType, tt.apiFamily)
			if tt.wantError != "" {
				if err == nil || err.Error() != tt.wantError {
					t.Fatalf("expected error %q, got %v", tt.wantError, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveHealthCheckAuthConfig returned error: %v", err)
			}
			if got.AuthHeader != tt.wantHeader || got.AuthPrefix != tt.wantPrefix || !reflect.DeepEqual(got.ExtraHeaders, tt.wantExtra) {
				t.Fatalf("unexpected auth config: %+v", got)
			}
		})
	}
}

func TestHealthCheckResponseJSONShapeRemainsConnectionNamed(t *testing.T) {
	checkedAt := time.Date(2026, time.June, 7, 12, 0, 0, 0, time.UTC)
	raw, err := json.Marshal(healthCheckResponse{ConnectionID: 42, HealthStatus: "healthy", CheckedAt: checkedAt, Detail: "Connection successful", ResponseTimeMS: 3})
	if err != nil {
		t.Fatalf("marshal health response: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal health response: %v", err)
	}
	for _, key := range []string{"connection_id", "health_status", "checked_at", "detail", "response_time_ms"} {
		if _, ok := payload[key]; !ok {
			t.Fatalf("expected health response key %q in %+v", key, payload)
		}
	}
	for key := range payload {
		if strings.Contains(key, "terminal") {
			t.Fatalf("expected health response to stay connection-shaped, got key %q in %+v", key, payload)
		}
	}
}

type fakeHealthWritebackExecutor struct {
	rowsAffected int64
	err          error
	query        string
	args         []any
}

func (exec *fakeHealthWritebackExecutor) Exec(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	exec.query = query
	exec.args = append([]any(nil), args...)
	if exec.err != nil {
		return pgconn.CommandTag{}, exec.err
	}
	if exec.rowsAffected == 0 {
		return pgconn.NewCommandTag("UPDATE 0"), nil
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func (exec *fakeHealthWritebackExecutor) Query(context.Context, string, ...any) (pgx.Rows, error) {
	panic("Query is not used by updateConnectionHealthCheckIfUnchanged")
}

func (exec *fakeHealthWritebackExecutor) QueryRow(context.Context, string, ...any) pgx.Row {
	panic("QueryRow is not used by updateConnectionHealthCheckIfUnchanged")
}

func TestUpdateConnectionHealthCheckIfUnchangedUsesOptimisticUpdatedAtToken(t *testing.T) {
	expectedUpdatedAt := time.Date(2026, time.June, 7, 13, 0, 0, 0, time.UTC)
	checkedAt := time.Date(2026, time.June, 7, 13, 1, 0, 0, time.UTC)
	detail := "Connection successful"
	exec := &fakeHealthWritebackExecutor{rowsAffected: 1}
	updated, err := updateConnectionHealthCheckIfUnchanged(context.Background(), exec, 99, expectedUpdatedAt, "healthy", &detail, checkedAt)
	if err != nil {
		t.Fatalf("updateConnectionHealthCheckIfUnchanged returned error: %v", err)
	}
	if !updated {
		t.Fatal("expected rows affected to report health writeback success")
	}
	if !strings.Contains(exec.query, "WHERE id = $1 AND updated_at = $2") {
		t.Fatalf("expected optimistic updated_at predicate in query, got %q", exec.query)
	}
	wantArgs := []any{99, expectedUpdatedAt, "healthy", detail, checkedAt}
	if !reflect.DeepEqual(exec.args, wantArgs) {
		t.Fatalf("expected writeback args %+v, got %+v", wantArgs, exec.args)
	}

	staleExec := &fakeHealthWritebackExecutor{rowsAffected: 0}
	updated, err = updateConnectionHealthCheckIfUnchanged(context.Background(), staleExec, 99, expectedUpdatedAt, "unhealthy", nil, checkedAt)
	if err != nil {
		t.Fatalf("stale optimistic writeback returned error: %v", err)
	}
	if updated {
		t.Fatal("expected stale optimistic writeback to report no rows affected")
	}
	if staleExec.args[3] != nil {
		t.Fatalf("expected nil health_detail to stay nil, got %+v", staleExec.args)
	}
}
