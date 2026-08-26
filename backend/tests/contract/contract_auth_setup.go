package contracttest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
)

type refreshTokenSnapshot struct {
	ID            int
	RotatedFromID *int
	RevokedAt     *time.Time
	LastUsedAt    *time.Time
}

type loginThrottleSnapshot struct {
	SubjectKey    string
	RemoteAddress string
	FailureCount  int
	LockedUntil   *time.Time
}

type loginAttemptResult struct {
	Status int
	Body   string
	Err    error
}

type appAuthSettingsRecord struct {
	ID           int
	AuthEnabled  bool
	Username     *string
	Email        *string
	PendingEmail *string
	PasswordHash *string
	EmailBoundAt *time.Time
	TokenVersion int
}

type proxyKeySnapshot struct {
	ID            int
	Name          string
	KeyPrefix     string
	IsActive      bool
	ExpiresAt     *time.Time
	LastUsedAt    *time.Time
	LastUsedIP    *string
	CreatedByID   *int
	Notes         *string
	RotatedAt     *time.Time
	RotationCount int
	CreatedAt     time.Time
}

func performLoginAttempt(harness *contractHarness, client *http.Client, username string, password string) loginAttemptResult {
	payload, err := json.Marshal(map[string]any{"username": username, "password": password, "session_duration": "7_days"})
	if err != nil {
		return loginAttemptResult{Err: fmt.Errorf("marshal login attempt: %w", err)}
	}
	request, err := http.NewRequest(http.MethodPost, harness.url+"/api/auth/login", bytes.NewReader(payload))
	if err != nil {
		return loginAttemptResult{Err: fmt.Errorf("build login attempt: %w", err)}
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return loginAttemptResult{Err: fmt.Errorf("perform login attempt: %w", err)}
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return loginAttemptResult{Err: fmt.Errorf("read login attempt response: %w", err)}
	}
	return loginAttemptResult{Status: response.StatusCode, Body: strings.TrimSpace(string(body))}
}

func putAuthSettings(t *testing.T, harness *contractHarness, desiredEnabled bool, accountChange map[string]any, acknowledgements map[string]any) *http.Response {
	t.Helper()
	getResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/settings/auth", nil, nil)
	assertStatus(t, getResponse, http.StatusOK)
	var parsed struct {
		Revision string `json:"revision"`
		AuthMode struct {
			Effective string `json:"effective"`
		} `json:"auth_mode"`
		ProxyKeyReadiness struct {
			ReadinessGeneration string `json:"readiness_generation"`
		} `json:"proxy_key_readiness"`
	}
	if err := json.Unmarshal([]byte(readResponseBody(t, getResponse)), &parsed); err != nil {
		t.Fatalf("decode auth settings response: %v", err)
	}
	body := map[string]any{
		"operation_id":         "contract-" + fmt.Sprintf("%d", time.Now().UnixNano()),
		"expected_revision":    parsed.Revision,
		"desired_auth_enabled": desiredEnabled,
		"account_change":       accountChange,
	}
	if desiredEnabled && parsed.AuthMode.Effective != "enabled" {
		body["expected_proxy_key_readiness_generation"] = parsed.ProxyKeyReadiness.ReadinessGeneration
	}
	if acknowledgements != nil {
		body["acknowledgements"] = acknowledgements
	}
	return harness.requestJSON(t, harness.client, http.MethodPut, "/api/settings/auth", body, nil)
}

func enableVerifiedAuth(t *testing.T, harness *contractHarness, username string, password string, _ string) {
	t.Helper()
	// New auth contract (Settings SPEC §8.2): staged immutable config version
	// + acknowledgements; enabling without activation-safe keys requires the
	// zero-key acknowledgement.
	getResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/settings/auth", nil, nil)
	assertStatus(t, getResponse, http.StatusOK)
	getPayload := readResponseBody(t, getResponse)
	var parsed struct {
		Revision          string `json:"revision"`
		ProxyKeyReadiness struct {
			ReadinessGeneration string `json:"readiness_generation"`
		} `json:"proxy_key_readiness"`
	}
	if err := json.Unmarshal([]byte(getPayload), &parsed); err != nil {
		t.Fatalf("decode auth settings response: %v", err)
	}
	revision := parsed.Revision
	readinessGeneration := parsed.ProxyKeyReadiness.ReadinessGeneration
	body := map[string]any{
		"operation_id":      "contract-enable-auth",
		"expected_revision": revision,
		"expected_proxy_key_readiness_generation": readinessGeneration,
		"desired_auth_enabled":                    true,
		"account_change": map[string]any{
			"kind":         "update",
			"username":     username,
			"new_password": password,
		},
		"acknowledgements": map[string]any{
			"enable_without_active_proxy_keys": true,
		},
	}
	enableResponse := harness.requestJSON(t, harness.client, http.MethodPut, "/api/settings/auth", body, nil)
	if enableResponse.StatusCode != http.StatusOK {
		t.Logf("enable PUT status=%d body=%s", enableResponse.StatusCode, readResponseBody(t, enableResponse))
	}
	assertStatus(t, enableResponse, http.StatusOK)
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{Auth: true})
}

func loginWithVerifiedAuth(t *testing.T, harness *contractHarness, username string, password string, email string) {
	t.Helper()
	seedVerifiedAuthSettings(t, harness, username, password, email)
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{Auth: true})
	loginResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/auth/login", map[string]any{"username": username, "password": password, "session_duration": "7_days"}, nil)
	assertStatus(t, loginResponse, http.StatusOK)
}

func seedVerifiedAuthSettings(t *testing.T, harness *contractHarness, username string, password string, _ string) {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password for test seed: %v", err)
	}
	now := time.Now().UTC()
	// The finalizer removed the in-place credential columns; seeding uses the
	// immutable auth_config_versions pointer (Settings SPEC §14.1 item 11).
	var configID int64
	if err := harness.conn.QueryRow(
		context.Background(),
		`INSERT INTO auth_config_versions (
			subject_key, generation, desired_mode, username, password_hash,
			session_version, state, created_operation_id, created_at, updated_at
		) VALUES ('app', 'contract-seed', 'enabled', $1, $2, 0, 'effective', NULL, $3, $3)
		RETURNING id`,
		username,
		string(hash),
		now,
	).Scan(&configID); err != nil {
		t.Fatalf("insert seeded auth config version: %v", err)
	}
	if _, err := harness.conn.Exec(
		context.Background(),
		`UPDATE app_auth_settings
		SET desired_config_version_id = $1,
			effective_config_version_id = $1,
			desired_generation = 'contract-seed',
			effective_generation = 'contract-seed',
			auth_revision = auth_revision + 1,
			updated_at = $2
		WHERE singleton_key = 'app'`,
		configID,
		now,
	); err != nil {
		t.Fatalf("seed verified auth settings pointer: %v", err)
	}
}
