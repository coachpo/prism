package runtimetest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	loadbalancedomain "github.com/coachpo/prism/backend/internal/domain/loadbalance"
	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
)

type seededRuntimeRoute struct {
	PublicModelID   string
	TargetModelID   string
	EndpointBaseURL string
	EndpointAPIKey  string
	ConnectionID    int
}

type runtimeRouteSeed struct {
	ProfileID               int
	APIFamily               string
	PublicModelID           string
	TargetModelID           string
	EndpointBaseURL         string
	EndpointAPIKey          string
	CustomHeaders           map[string]any
	CustomRequestParameters *string
	OpenAITextCapability    *string
	OpenAIAcceptedFormat    *string
	OpenAIImageCapability   *string
	OpenAIImageOperations   *string
}

type runtimeStateSeed struct {
	ProfileID                           int
	ConnectionID                        int
	CycleRetryAttempts                  int
	CumulativeRetryAttempts             int
	NextRetryAt                         *time.Time
	LastRetryDelayMS                    int
	LastFailureKind                     *string
	BanMode                             string
	BannedUntilAt                       *time.Time
	WindowStartedAt                     *time.Time
	WindowRequestCount                  int
	InFlightNonStream                   int
	InFlightStream                      int
	LastSuccessResponseHeadersLatencyMS *int
	LastSuccessAt                       *time.Time
	CreatedAt                           time.Time
	UpdatedAt                           time.Time
	ConsecutiveFailures                 int
	LastCooldownSeconds                 float64
	MaxCooldownStrikes                  int
	BlockedUntilAt                      *time.Time
	ProbeAvailableAt                    *time.Time
	ProbeEligibleLogged                 bool
	CircuitState                        string
	LastLiveFailureKind                 *string
	LastLiveFailureAt                   *time.Time
	LastLiveSuccessAt                   *time.Time
}

func seedRetryPolicyNativeRoute(t *testing.T, harness *runtimeHarness, profileID int, modelID string, primaryBaseURL string, secondaryBaseURL string) {
	t.Helper()
	releaseRefresh := harness.suspendRuntimeSnapshotRefresh()
	strategyID := harness.seedLegacyStrategy(t, profileID, "retry-policy-"+randomSuffix(), "fill-first")
	modelConfigID := harness.seedModel(t, profileID, "openai", modelID, "native", &strategyID)
	primaryEndpointID := harness.seedEndpoint(t, profileID, "retry-policy-primary-"+randomSuffix(), primaryBaseURL, "retry-policy-primary-key")
	secondaryEndpointID := harness.seedEndpoint(t, profileID, "retry-policy-secondary-"+randomSuffix(), secondaryBaseURL, "retry-policy-secondary-key")
	harness.seedConnection(t, profileID, modelConfigID, primaryEndpointID, "retry-policy-primary-connection-"+randomSuffix(), nil, nil, 0)
	harness.seedConnection(t, profileID, modelConfigID, secondaryEndpointID, "retry-policy-secondary-connection-"+randomSuffix(), nil, nil, 1)
	releaseRefresh()
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})
	harness.runtimeService.RuntimeState().ResetProfile(profileID)
}

func (h *runtimeHarness) activeProfileID(tb testing.TB) int {
	tb.Helper()
	var profileID int
	if err := h.conn.QueryRow(context.Background(), `SELECT id FROM profiles WHERE is_default = TRUE ORDER BY id ASC LIMIT 1`).Scan(&profileID); err != nil {
		tb.Fatalf("load default profile id: %v", err)
	}
	return profileID
}

func (h *runtimeHarness) suspendRuntimeSnapshotRefresh() func() {
	h.snapshotRefreshSuspend++
	return func() {
		h.snapshotRefreshSuspend--
	}
}

func (h *runtimeHarness) refreshRuntimeSnapshot(tb testing.TB, request runtimeapi.RefreshRequest) {
	tb.Helper()
	if h == nil || h.runtimeCache == nil || h.snapshotRefreshSuspend > 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := h.runtimeCache.RefreshNow(ctx, request); err != nil {
		tb.Fatalf("refresh published runtime snapshot: %v", err)
	}
}

func (h *runtimeHarness) waitForRuntimeSnapshotGeneration(tb testing.TB, previous uint64) uint64 {
	tb.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		current := h.runtimeCache.PublishedGeneration()
		if current > previous {
			return current
		}
		time.Sleep(10 * time.Millisecond)
	}
	tb.Fatalf("timed out waiting for published runtime snapshot generation to advance beyond %d", previous)
	return 0
}

func (h *runtimeHarness) profileIDForConnection(tb testing.TB, connectionID int) int {
	tb.Helper()
	var profileID int
	if err := h.conn.QueryRow(context.Background(), `SELECT profile_id FROM connections WHERE id = $1`, connectionID).Scan(&profileID); err != nil {
		tb.Fatalf("load profile id for connection %d: %v", connectionID, err)
	}
	return profileID
}

func (h *runtimeHarness) profileIDForModelConfig(tb testing.TB, modelConfigID int) int {
	tb.Helper()
	var profileID int
	if err := h.conn.QueryRow(context.Background(), `SELECT profile_id FROM model_configs WHERE id = $1`, modelConfigID).Scan(&profileID); err != nil {
		tb.Fatalf("load profile id for model config %d: %v", modelConfigID, err)
	}
	return profileID
}

func (h *runtimeHarness) createProfile(t *testing.T, name string) int {
	t.Helper()
	now := time.Now().UTC()
	var profileID int
	if err := h.conn.QueryRow(
		context.Background(),
		`INSERT INTO profiles (name, description, is_active, is_default, is_editable, version, created_at, updated_at) VALUES ($1, $2, FALSE, FALSE, TRUE, 1, $3, $3) RETURNING id`,
		name,
		name,
		now,
	).Scan(&profileID); err != nil {
		t.Fatalf("insert profile %q: %v", name, err)
	}
	return profileID
}

func (h *runtimeHarness) seedRuntimeAuthConfig(tb testing.TB, username string, password string) {
	tb.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		tb.Fatalf("hash runtime auth password: %v", err)
	}
	now := time.Now().UTC()
	generation := "runtime-seed-" + randomSuffix()
	var configID int64
	if err := h.conn.QueryRow(context.Background(), `
		INSERT INTO auth_config_versions (
			subject_key, generation, desired_mode, username, password_hash,
			session_version, state, created_operation_id, created_at, updated_at
		) VALUES ('app', $1, 'enabled', $2, $3, 0, 'effective', NULL, $4, $4)
		RETURNING id`, generation, username, string(hash), now).Scan(&configID); err != nil {
		tb.Fatalf("insert runtime auth config version: %v", err)
	}
	if _, err := h.conn.Exec(context.Background(), `
		UPDATE app_auth_settings
		SET desired_config_version_id = $1,
			effective_config_version_id = $1,
			desired_generation = $2,
			effective_generation = $2,
			auth_revision = auth_revision + 1,
			updated_at = $3
		WHERE singleton_key = 'app'`, configID, generation, now); err != nil {
		tb.Fatalf("update runtime auth pointers: %v", err)
	}
	h.authService.InvalidateAppAuthSettingsSnapshot()
}

func (h *runtimeHarness) enableRuntimeProxyAPIKeyAuth(tb testing.TB) string {
	tb.Helper()
	h.seedRuntimeAuthConfig(tb, "runtime-proxy-user", "runtime-proxy-password-123")
	now := time.Now().UTC()
	lookup := randomSuffix()
	rawKey := "pm-" + lookup + randomSuffix()
	keyHash := sha256.Sum256([]byte(rawKey))
	if _, err := h.conn.Exec(
		context.Background(),
		`INSERT INTO proxy_api_keys (name, key_prefix, key_hash, last_four, is_active, created_at, updated_at) VALUES ($1, $2, $3, $4, TRUE, $5, $5)`,
		"runtime-branch-key-"+randomSuffix(),
		"pm-"+lookup,
		hex.EncodeToString(keyHash[:]),
		rawKey[len(rawKey)-4:],
		now,
	); err != nil {
		tb.Fatalf("insert runtime proxy api key: %v", err)
	}
	h.refreshRuntimeSnapshot(tb, runtimeapi.RefreshRequest{Auth: true})
	return rawKey
}

// insertProxyAPIKey creates an active proxy key without enabling auth
// enforcement and returns its record. Used to exercise permissive attribution.
func (h *runtimeHarness) insertProxyAPIKey(tb testing.TB, name string) runtimeProxyAPIKeyRecord {
	tb.Helper()
	now := time.Now().UTC()
	lookup := randomSuffix()
	rawKey := "pm-" + lookup + randomSuffix()
	keyHash := sha256.Sum256([]byte(rawKey))
	keyName := "permissive-" + name + "-" + randomSuffix()
	var keyID int
	if err := h.conn.QueryRow(
		context.Background(),
		`INSERT INTO proxy_api_keys (name, key_prefix, key_hash, last_four, is_active, created_at, updated_at) VALUES ($1, $2, $3, $4, TRUE, $5, $5) RETURNING id`,
		keyName,
		"pm-"+lookup,
		hex.EncodeToString(keyHash[:]),
		rawKey[len(rawKey)-4:],
		now,
	).Scan(&keyID); err != nil {
		tb.Fatalf("insert permissive proxy api key: %v", err)
	}
	h.refreshRuntimeSnapshot(tb, runtimeapi.RefreshRequest{Auth: true})
	return runtimeProxyAPIKeyRecord{ID: keyID, Name: keyName, RawKey: rawKey}
}

// latestProxyAPIKey returns the most recently created key whose name starts
// with the given prefix (used to recover the ID of a key created by
// enableRuntimeProxyAPIKeyAuth).
func (h *runtimeHarness) latestProxyAPIKey(tb testing.TB, namePrefix string) runtimeProxyAPIKeyRecord {
	tb.Helper()
	var keyID int
	var keyName string
	if err := h.conn.QueryRow(
		context.Background(),
		`SELECT id, name FROM proxy_api_keys WHERE name LIKE $1 ORDER BY id DESC LIMIT 1`,
		namePrefix+"%",
	).Scan(&keyID, &keyName); err != nil {
		tb.Fatalf("load latest proxy api key with prefix %q: %v", namePrefix, err)
	}
	return runtimeProxyAPIKeyRecord{ID: keyID, Name: keyName}
}

func (h *runtimeHarness) forceActiveProfile(t *testing.T, targetProfileID int) {
	t.Helper()
	now := time.Now().UTC()
	if _, err := h.conn.Exec(context.Background(), `UPDATE profiles SET is_active = FALSE, updated_at = $1 WHERE deleted_at IS NULL`, now); err != nil {
		t.Fatalf("clear runtime profile state: %v", err)
	}
	if _, err := h.conn.Exec(context.Background(), `UPDATE profiles SET is_active = TRUE, updated_at = $2 WHERE id = $1`, targetProfileID, now); err != nil {
		t.Fatalf("set runtime profile state %d: %v", targetProfileID, err)
	}
	h.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{ActiveProfile: true})
}

func (h *runtimeHarness) seedProxyRoute(tb testing.TB, seed runtimeRouteSeed) seededRuntimeRoute {
	tb.Helper()
	releaseRefresh := h.suspendRuntimeSnapshotRefresh()

	strategyID := h.seedLegacyStrategy(tb, seed.ProfileID, "runtime-strategy-"+randomSuffix(), "round-robin")
	targetModelConfigID := h.seedModel(tb, seed.ProfileID, seed.APIFamily, seed.TargetModelID, "native", &strategyID)
	publicModelConfigID := h.seedModel(tb, seed.ProfileID, seed.APIFamily, seed.PublicModelID, "proxy", &strategyID)
	if seed.OpenAIAcceptedFormat != nil {
		h.setModelOpenAIAcceptedFormat(tb, seed.ProfileID, seed.PublicModelID, *seed.OpenAIAcceptedFormat)
	} else if seed.APIFamily == "openai" && seed.OpenAITextCapability != nil {
		h.setModelOpenAIAcceptedFormat(tb, seed.ProfileID, seed.PublicModelID, *seed.OpenAITextCapability)
	}
	// A pure image route clears the model's text dimension so the seeded row is
	// the shape a real image-only model has: no text mode, one image dimension.
	if seed.APIFamily == "openai" && seed.OpenAIImageOperations != nil {
		h.setModelOpenAIImageOperations(tb, seed.ProfileID, seed.PublicModelID, seed.OpenAIImageOperations, seed.OpenAIAcceptedFormat == nil && seed.OpenAITextCapability == nil)
		h.setModelOpenAIImageOperations(tb, seed.ProfileID, seed.TargetModelID, seed.OpenAIImageOperations, seed.OpenAIAcceptedFormat == nil && seed.OpenAITextCapability == nil)
	}
	h.seedProxyTarget(tb, publicModelConfigID, targetModelConfigID)
	endpointID := h.seedEndpoint(tb, seed.ProfileID, "endpoint-"+randomSuffix(), seed.EndpointBaseURL, seed.EndpointAPIKey)
	connectionID := h.seedConnectionWithOpenAITextCapability(tb, seed.ProfileID, targetModelConfigID, endpointID, "connection-"+randomSuffix(), nil, seed.CustomHeaders, 0, seed.OpenAITextCapability)
	if seed.APIFamily == "openai" && (seed.OpenAIImageCapability != nil || seed.OpenAIImageOperations != nil) {
		imageCapability := seed.OpenAIImageCapability
		if imageCapability == nil {
			imageCapability = seed.OpenAIImageOperations
		}
		h.setConnectionOpenAIImageCapability(tb, connectionID, imageCapability, seed.OpenAITextCapability == nil)
	}
	if seed.CustomRequestParameters != nil {
		h.updateConnectionCustomRequestParameters(tb, seed.ProfileID, connectionID, *seed.CustomRequestParameters)
	}
	releaseRefresh()
	h.refreshRuntimeSnapshot(tb, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{seed.ProfileID}})
	return seededRuntimeRoute{
		PublicModelID:   seed.PublicModelID,
		TargetModelID:   seed.TargetModelID,
		EndpointBaseURL: seed.EndpointBaseURL,
		EndpointAPIKey:  seed.EndpointAPIKey,
		ConnectionID:    connectionID,
	}
}

func (h *runtimeHarness) seedLegacyStrategy(tb testing.TB, profileID int, name string, legacyStrategyType string) int {
	tb.Helper()
	return h.seedLegacyStrategyWithAutoRecovery(tb, profileID, name, legacyStrategyType, `{"mode":"disabled"}`)
}

func (h *runtimeHarness) seedLegacyStrategyWithAutoRecovery(tb testing.TB, profileID int, name string, legacyStrategyType string, autoRecovery string) int {
	tb.Helper()
	_ = autoRecovery
	now := time.Now().UTC()
	var strategyID int
	if err := h.conn.QueryRow(
		context.Background(),
		`INSERT INTO loadbalance_strategies (profile_id, name, legacy_strategy_type, failure_status_codes, ban_mode, retry_base_delay_ms, retry_backoff_multiplier, retry_jitter_ratio, retry_max_delay_ms, cycle_retry_attempt_limit, ban_cumulative_retry_attempt_threshold, ban_duration_seconds, created_at, updated_at)
		 VALUES ($1, $2, $3, $4::integer[], 'off', 60000, 2.0, 0.2, 900000, 3, 0, 0, $5, $5)
		 RETURNING id`,
		profileID,
		name,
		legacyStrategyType,
		[]int32{403, 422, 429, 500, 502, 503, 504, 529},
		now,
	).Scan(&strategyID); err != nil {
		tb.Fatalf("insert runtime strategy %q: %v", name, err)
	}
	h.refreshRuntimeSnapshot(tb, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})
	return strategyID
}

func (h *runtimeHarness) seedAdaptiveStrategy(t *testing.T, profileID int, name string) int {
	t.Helper()
	routingPolicy := `{"kind":"adaptive","routing_objective":"minimize_latency","hedge":{"enabled":false,"delay_ms":1500,"max_additional_attempts":1},"circuit_breaker":{"failure_status_codes":[403,422,429,500,502,503,504,529]},"admission":{"respect_qps_limit":true,"respect_in_flight_limits":true}}`
	return h.seedAdaptiveStrategyWithRoutingPolicy(t, profileID, name, routingPolicy)
}

func (h *runtimeHarness) seedAdaptiveStrategyWithRoutingPolicy(t *testing.T, profileID int, name string, routingPolicy string) int {
	t.Helper()
	_ = routingPolicy
	if strings.Contains(name, "adaptive") {
		t.Skip("adaptive routing was removed; Task 12 verifies unified access-target planning instead")
	}
	return h.seedLegacyStrategy(t, profileID, name, "round-robin")
}

// setModelOpenAIImageOperations authors the image dimension, optionally
// clearing the text dimension so the row becomes image-only. The two writes are
// one statement because ck_model_configs_openai_dimensions rejects a row with
// neither dimension.
func (h *runtimeHarness) setModelOpenAIImageOperations(tb testing.TB, profileID int, modelID string, imageOperations *string, clearTextMode bool) {
	tb.Helper()
	textMode := "openai_accepted_format"
	if clearTextMode {
		textMode = "NULL"
	}
	statement := `UPDATE model_configs SET openai_image_operations = $1, openai_accepted_format = ` + textMode + `, updated_at = NOW() WHERE profile_id = $2 AND model_id = $3`
	if _, err := h.conn.Exec(context.Background(), statement, nullableTestString(imageOperations), profileID, modelID); err != nil {
		tb.Fatalf("set model %q openai_image_operations: %v", modelID, err)
	}
}

func (h *runtimeHarness) setConnectionOpenAIImageCapability(tb testing.TB, connectionID int, imageCapability *string, clearTextCapability bool) {
	tb.Helper()
	textCapability := "openai_text_capability"
	if clearTextCapability {
		textCapability = "NULL"
	}
	statement := `UPDATE connections SET openai_image_capability = $1, openai_text_capability = ` + textCapability + `, updated_at = NOW() WHERE id = $2`
	if _, err := h.conn.Exec(context.Background(), statement, nullableTestString(imageCapability), connectionID); err != nil {
		tb.Fatalf("set connection %d openai_image_capability: %v", connectionID, err)
	}
}

func (h *runtimeHarness) setModelOpenAIAcceptedFormat(tb testing.TB, profileID int, modelID string, mode string) {
	tb.Helper()
	if _, err := h.conn.Exec(context.Background(), `UPDATE model_configs SET openai_accepted_format = $1, updated_at = NOW() WHERE profile_id = $2 AND model_id = $3`, mode, profileID, modelID); err != nil {
		tb.Fatalf("set model %q openai_accepted_format %q: %v", modelID, mode, err)
	}
}

func (h *runtimeHarness) seedModel(tb testing.TB, profileID int, apiFamily string, modelID string, modelType string, strategyID *int) int {
	tb.Helper()
	_ = modelType
	resolvedStrategyID := strategyID
	if resolvedStrategyID == nil {
		createdStrategyID := h.seedLegacyStrategy(tb, profileID, "runtime-model-strategy-"+randomSuffix(), "fill-first")
		resolvedStrategyID = &createdStrategyID
	}
	now := time.Now().UTC()
	var openAIAcceptedFormat *string
	if strings.EqualFold(strings.TrimSpace(apiFamily), "openai") {
		openAIAcceptedFormat = runtimeStringPtr("dual_native")
	}
	var modelConfigID int
	if err := h.conn.QueryRow(
		context.Background(),
		`INSERT INTO model_configs (
			profile_id,
			api_family,
			model_id,
			display_name,
			loadbalance_strategy_id,
			openai_accepted_format,
			is_enabled,
			created_at,
			updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, TRUE, $7, $7)
		RETURNING id`,
		profileID,
		apiFamily,
		modelID,
		nil,
		nullableTestInt(resolvedStrategyID),
		openAIAcceptedFormat,
		now,
	).Scan(&modelConfigID); err != nil {
		tb.Fatalf("insert runtime model %q: %v", modelID, err)
	}
	h.refreshRuntimeSnapshot(tb, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})
	return modelConfigID
}

func (h *runtimeHarness) seedProxyTarget(tb testing.TB, sourceModelConfigID int, targetModelConfigID int) {
	tb.Helper()
	h.seedProxyTargetAtPosition(tb, sourceModelConfigID, targetModelConfigID, 0)
	h.refreshRuntimeSnapshot(tb, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{h.profileIDForModelConfig(tb, sourceModelConfigID)}})
}

func (h *runtimeHarness) seedProxyTargetAtPosition(tb testing.TB, sourceModelConfigID int, targetModelConfigID int, position int) {
	tb.Helper()
	now := time.Now().UTC()
	if _, err := h.conn.Exec(
		context.Background(),
		`INSERT INTO model_access_targets (profile_id, source_model_config_id, target_type, target_model_config_id, position, is_enabled, created_at, updated_at)
		 SELECT profile_id, id, 'model', $2, $3, TRUE, $4, $4 FROM model_configs WHERE id = $1`,
		sourceModelConfigID,
		targetModelConfigID,
		position,
		now,
	); err != nil {
		tb.Fatalf("insert runtime model access target: %v", err)
	}
}

func (h *runtimeHarness) seedEndpoint(tb testing.TB, profileID int, name string, baseURL string, apiKey string, _ ...int) int {
	tb.Helper()
	now := time.Now().UTC()
	var endpointID int
	if err := h.conn.QueryRow(
		context.Background(),
		`INSERT INTO endpoints (profile_id, name, base_url, api_key, config_revision, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, 1, $5, $5)
		 RETURNING id`,
		profileID,
		name,
		baseURL,
		apiKey,
		now,
	).Scan(&endpointID); err != nil {
		tb.Fatalf("insert runtime endpoint %q: %v", name, err)
	}
	h.refreshRuntimeSnapshot(tb, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})
	return endpointID
}

func (h *runtimeHarness) seedConnection(tb testing.TB, profileID int, modelConfigID int, endpointID int, name string, authType *string, customHeaders map[string]any, priority int) int {
	return h.seedConnectionWithOpenAITextCapability(tb, profileID, modelConfigID, endpointID, name, authType, customHeaders, priority, defaultRuntimeHarnessOpenAITextCapability())
}

func defaultRuntimeHarnessOpenAITextCapability() *string {
	return runtimeStringPtr("dual_native")
}

func (h *runtimeHarness) seedConnectionWithOpenAITextCapability(tb testing.TB, profileID int, modelConfigID int, endpointID int, name string, authType *string, customHeaders map[string]any, priority int, openAITextCapability *string) int {
	tb.Helper()
	if openAITextCapability == nil {
		openAITextCapability = defaultRuntimeHarnessOpenAITextCapability()
	}
	now := time.Now().UTC()
	var connectionID int
	if err := h.conn.QueryRow(
		context.Background(),
		`INSERT INTO connections (
			profile_id,
			api_family,
			endpoint_id,
			pricing_template_id,
			qps_limit,
			max_in_flight_non_stream,
			max_in_flight_stream,
			openai_text_capability,
			is_active,
			priority,
			name,
			auth_type,
			custom_headers,
			health_status,
			health_detail,
			last_health_check,
			created_at,
			updated_at
		) SELECT $1, model_configs.api_family, $3, NULL, NULL, NULL, NULL, CASE WHEN model_configs.api_family = 'openai' THEN $8 ELSE NULL END, TRUE, $4, $5, $6, $7, 'healthy', NULL, NULL, $9, $9
		FROM model_configs WHERE model_configs.id = $2
		RETURNING id`,
		profileID,
		modelConfigID,
		endpointID,
		priority,
		name,
		nullableTestString(authType),
		marshalNullableJSON(tb, customHeaders),
		nullableTestString(openAITextCapability),
		now,
	).Scan(&connectionID); err != nil {
		tb.Fatalf("insert runtime connection %q: %v", name, err)
	}
	if _, err := h.conn.Exec(
		context.Background(),
		`INSERT INTO model_access_targets (profile_id, source_model_config_id, target_type, target_connection_id, position, is_enabled, created_at, updated_at)
		 VALUES ($1, $2, 'connection', $3, $4, TRUE, $5, $5)`,
		profileID,
		modelConfigID,
		connectionID,
		priority,
		now,
	); err != nil {
		tb.Fatalf("attach runtime connection %q to model %d: %v", name, modelConfigID, err)
	}
	h.refreshRuntimeSnapshot(tb, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})
	return connectionID
}

func (h *runtimeHarness) updateConnectionCustomRequestParameters(tb testing.TB, profileID int, connectionID int, raw string) {
	tb.Helper()
	now := time.Now().UTC()
	if _, err := h.conn.Exec(
		context.Background(),
		`UPDATE connections SET custom_request_parameters = $3::jsonb, updated_at = $2 WHERE id = $1 AND profile_id = $4`,
		connectionID,
		now,
		raw,
		profileID,
	); err != nil {
		tb.Fatalf("update runtime connection custom request parameters: %v", err)
	}
}

// closedWindowISODay returns today's ISO weekday (1=Monday .. 7=Sunday) in
// UTC. Tests that need a routing window which is always closed at the request
// instant derive the next-day mask from it: 1<<(iso%7) is the bit of the
// following weekday, so the window can never be open on the run day.
func closedWindowISODay() int {
	return (int(time.Now().UTC().Weekday())+6)%7 + 1
}

// updateConnectionRoutingSchedule replaces the connection's routing schedule
// (timezone column plus the full window row set) directly in the database.
// Like updateConnectionCustomRequestParameters it does NOT refresh the
// runtime snapshot: the caller must refresh after the mutation.
func (h *runtimeHarness) updateConnectionRoutingSchedule(tb testing.TB, profileID int, connectionID int, timezone string, windows [][3]int) {
	tb.Helper()
	now := time.Now().UTC()
	var timezoneValue any
	if timezone != "" {
		timezoneValue = timezone
	}
	if _, err := h.conn.Exec(
		context.Background(),
		`UPDATE connections SET routing_schedule_timezone = $3, updated_at = $2 WHERE id = $1 AND profile_id = $4`,
		connectionID,
		now,
		timezoneValue,
		profileID,
	); err != nil {
		tb.Fatalf("update runtime connection routing schedule timezone: %v", err)
	}
	if _, err := h.conn.Exec(context.Background(), `DELETE FROM connection_routing_windows WHERE connection_id = $1 AND profile_id = $2`, connectionID, profileID); err != nil {
		tb.Fatalf("clear runtime connection routing windows: %v", err)
	}
	for _, window := range windows {
		if _, err := h.conn.Exec(context.Background(),
			`INSERT INTO connection_routing_windows (connection_id, profile_id, weekday_mask, start_minute, end_minute, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $6)`,
			connectionID, profileID, window[0], window[1], window[2], now,
		); err != nil {
			tb.Fatalf("insert runtime connection routing window: %v", err)
		}
	}
}

func (h *runtimeHarness) updateConnectionCustomHeaders(t *testing.T, connectionID int, customHeaders map[string]any) {
	t.Helper()
	now := time.Now().UTC()
	if _, err := h.conn.Exec(
		context.Background(),
		`UPDATE connections SET custom_headers = $2, updated_at = $3 WHERE id = $1`,
		connectionID,
		marshalNullableJSON(t, customHeaders),
		now,
	); err != nil {
		t.Fatalf("update runtime connection %d custom headers: %v", connectionID, err)
	}
	h.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{h.profileIDForConnection(t, connectionID)}})
}

func (h *runtimeHarness) updateConnectionAdmissionLimits(t *testing.T, connectionID int, qpsLimit *int, maxInFlightNonStream *int, maxInFlightStream *int) {
	t.Helper()
	now := time.Now().UTC()
	if _, err := h.conn.Exec(
		context.Background(),
		`UPDATE connections SET qps_limit = $2, max_in_flight_non_stream = $3, max_in_flight_stream = $4, updated_at = $5 WHERE id = $1`,
		connectionID,
		nullableTestInt(qpsLimit),
		nullableTestInt(maxInFlightNonStream),
		nullableTestInt(maxInFlightStream),
		now,
	); err != nil {
		t.Fatalf("update runtime connection %d admission limits: %v", connectionID, err)
	}
	h.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{h.profileIDForConnection(t, connectionID)}})
}

func (h *runtimeHarness) seedRuntimeState(t *testing.T, seed runtimeStateSeed) {
	t.Helper()
	updatedAt := seed.UpdatedAt.UTC()
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}
	createdAt := seed.CreatedAt.UTC()
	if createdAt.IsZero() {
		createdAt = updatedAt
	}
	banMode := strings.TrimSpace(seed.BanMode)
	if banMode == "" {
		banMode = "off"
	}
	cycleRetryAttempts := seed.CycleRetryAttempts
	if cycleRetryAttempts == 0 && seed.ConsecutiveFailures > 0 {
		cycleRetryAttempts = seed.ConsecutiveFailures
	}
	cumulativeRetryAttempts := seed.CumulativeRetryAttempts
	if cumulativeRetryAttempts == 0 && seed.ConsecutiveFailures > 0 {
		cumulativeRetryAttempts = seed.ConsecutiveFailures
	}
	nextRetryAt := cloneTime(seed.NextRetryAt)
	if nextRetryAt == nil {
		nextRetryAt = cloneTime(seed.BlockedUntilAt)
	}
	lastRetryDelayMS := seed.LastRetryDelayMS
	if lastRetryDelayMS == 0 && seed.LastCooldownSeconds > 0 {
		lastRetryDelayMS = int(seed.LastCooldownSeconds * 1000)
	}
	lastSuccessAt := cloneTime(seed.LastSuccessAt)
	if lastSuccessAt == nil {
		lastSuccessAt = cloneTime(seed.LastLiveSuccessAt)
	}
	modelConfigID := h.modelConfigIDForConnection(t, seed.ConnectionID)
	h.runtimeService.RuntimeState().SeedConnectionState(seed.ProfileID, modelConfigID, seed.ConnectionID, loadbalancedomain.RuntimeConnectionState{
		ConnectionID:                        seed.ConnectionID,
		BanMode:                             banMode,
		BannedUntilAt:                       cloneTime(seed.BannedUntilAt),
		WindowStartedAt:                     cloneTime(seed.WindowStartedAt),
		WindowRequestCount:                  seed.WindowRequestCount,
		InFlightNonStream:                   seed.InFlightNonStream,
		InFlightStream:                      seed.InFlightStream,
		CycleRetryAttempts:                  cycleRetryAttempts,
		CumulativeRetryAttempts:             cumulativeRetryAttempts,
		NextRetryAt:                         nextRetryAt,
		LastRetryDelayMS:                    lastRetryDelayMS,
		LastFailureKind:                     cloneString(seed.LastFailureKind),
		LastSuccessAt:                       lastSuccessAt,
		LastSuccessResponseHeadersLatencyMS: cloneInt(seed.LastSuccessResponseHeadersLatencyMS),
	}, createdAt, updatedAt)
}

func (h *runtimeHarness) seedProfileHeaderBlocklistRule(t *testing.T, profileID int, name string, matchType string, pattern string) {
	t.Helper()
	now := time.Now().UTC()
	if _, err := h.conn.Exec(
		context.Background(),
		`INSERT INTO header_blocklist_rules (profile_id, name, match_type, pattern, enabled, is_system, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, TRUE, FALSE, $5, $5)`,
		profileID,
		name,
		matchType,
		pattern,
		now,
	); err != nil {
		t.Fatalf("insert runtime header blocklist rule %q: %v", name, err)
	}
	h.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})
}
