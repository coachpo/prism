package sidecars

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestNormalizeProviderInventoryRedactsSecretsAndWhitelistsMetadata(t *testing.T) {
	payload := map[string]json.RawMessage{
		"gemini-api-key": json.RawMessage(`[{"api-key":"raw-provider-secret","priority":10,"prefix":"team-a/","base-url":"https://generativelanguage.googleapis.com","proxy-url":"http://proxy-secret.invalid","models":[{"name":"gemini-2.5-pro","alias":"gemini-pro","secret":"drop-me"}],"headers":{"Authorization":"Bearer raw-provider-token","X-API-Key":"raw-header-key"},"excluded-models":["gemini-legacy"],"auth-index":"auth_001"}]`),
	}
	inputs, err := normalizeSidecarProviderSnapshots(42, time.Date(2026, time.May, 10, 12, 0, 0, 0, time.UTC), "gemini-api-key", payload)
	if err != nil {
		t.Fatalf("normalize provider snapshot: %v", err)
	}
	if len(inputs) != 1 || inputs[0].ProviderItemKey != "auth_001" {
		t.Fatalf("expected one auth-index keyed provider snapshot, got %+v", inputs)
	}
	snapshot := decodeProviderSnapshotForTest(t, inputs[0].SnapshotJSON)
	if snapshot["secret_present"] != true || snapshot["secret_masked"] != credentialMask {
		t.Fatalf("expected provider secret presence markers, got %+v", snapshot)
	}
	if snapshot["base_url"] != "https://generativelanguage.googleapis.com" || snapshot["proxy_url_present"] != true {
		t.Fatalf("expected safe URL metadata only, got %+v", snapshot)
	}
	if got := stringSliceFromSnapshot(t, snapshot, "header_keys"); !reflect.DeepEqual(got, []string{"Authorization", "X-API-Key"}) {
		t.Fatalf("expected only sorted header key names, got %v", got)
	}
	if got := stringSliceFromSnapshot(t, snapshot, "excluded_models"); !reflect.DeepEqual(got, []string{"gemini-legacy"}) {
		t.Fatalf("expected excluded model names, got %v", got)
	}
	if _, ok := snapshot["api-key"]; ok {
		t.Fatalf("provider snapshot must not expose api-key fields: %+v", snapshot)
	}
	requireProviderSnapshotDoesNotLeak(t, inputs[0].SnapshotJSON, "raw-provider-secret", "raw-provider-token", "raw-header-key", "proxy-secret", "drop-me")
}

func TestNormalizeOpenAICompatibilityEntriesRedactsNestedSecrets(t *testing.T) {
	payload := map[string]json.RawMessage{
		"openai-compatibility": json.RawMessage(`[{"name":"compat","priority":10,"disabled":false,"base-url":"https://compat.example.invalid/v1","api-key-entries":[{"api-key":"raw-openai-secret","proxy-url":"http://proxy-secret.invalid","auth-index":"auth_005","headers":{"Authorization":"Bearer nested-secret"}}],"models":[{"name":"fixture-model","alias":"fixture-alias","api-key":"nested-model-secret"}],"headers":{"X-Fixture":"true"}}]`),
	}
	inputs, err := normalizeSidecarProviderSnapshots(42, time.Now().UTC(), "openai-compatibility", payload)
	if err != nil {
		t.Fatalf("normalize openai compatibility snapshot: %v", err)
	}
	if len(inputs) != 1 || inputs[0].ProviderItemKey != "compat" {
		t.Fatalf("expected one name-keyed compatibility snapshot, got %+v", inputs)
	}
	snapshot := decodeProviderSnapshotForTest(t, inputs[0].SnapshotJSON)
	if snapshot["secret_present"] != true || snapshot["secret_masked"] != credentialMask {
		t.Fatalf("expected aggregate secret markers, got %+v", snapshot)
	}
	entries, ok := snapshot["api_key_entries"].([]any)
	if !ok || len(entries) != 1 {
		t.Fatalf("expected one sanitized api_key_entries item, got %+v", snapshot["api_key_entries"])
	}
	entry, ok := entries[0].(map[string]any)
	if !ok || entry["auth_index"] != "auth_005" || entry["proxy_url_present"] != true || entry["secret_present"] != true || entry["secret_masked"] != credentialMask {
		t.Fatalf("expected sanitized nested entry metadata, got %+v", entries[0])
	}
	if got := stringSliceFromSnapshot(t, entry, "header_keys"); !reflect.DeepEqual(got, []string{"Authorization"}) {
		t.Fatalf("expected nested header key names only, got %v", got)
	}
	if _, ok := entry["api-key"]; ok {
		t.Fatalf("provider entry must not expose api-key fields: %+v", entry)
	}
	requireProviderSnapshotDoesNotLeak(t, inputs[0].SnapshotJSON, "raw-openai-secret", "proxy-secret", "nested-secret", "nested-model-secret")
}

func TestProviderEndpointFailureDoesNotFailSidecarSync(t *testing.T) {
	now := time.Date(2026, time.May, 10, 12, 0, 0, 0, time.UTC)
	failClaude := true
	emptyClaude := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v0/management/claude-api-key" && failClaude {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"error":"provider inventory unavailable"}`))
			return
		}
		if r.URL.Path == "/v0/management/claude-api-key" && emptyClaude {
			writeSyncJSON(w, `{"claude-api-key":[]}`)
			return
		}
		serveSyncFixturePath(t, w, r)
	}))
	defer server.Close()

	service := newSyncTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, server.URL, true, 30)
	result, err := service.SyncSidecar(t.Context(), sidecar.ID)
	if err != nil || result.ErrorDetail != "" || result.ProviderSnapshotCount != 5 {
		t.Fatalf("provider partial failure should not fail sync: result=%+v err=%v", result, err)
	}
	stored, ok, err := service.store.GetSidecarInstance(t.Context(), sidecar.ID)
	if err != nil || !ok || stored.LastSyncError != nil || stored.LastSuccessfulSyncAt == nil || !stored.LastSuccessfulSyncAt.Equal(now) {
		t.Fatalf("partial provider failure should persist successful sync metadata, stored=%+v ok=%v err=%v", stored, ok, err)
	}
	providers, err := service.store.ListProviderSnapshots(t.Context(), sidecar.ID)
	if err != nil || len(providers) != 5 {
		t.Fatalf("expected four provider snapshots plus one provider error snapshot, len=%d err=%v", len(providers), err)
	}
	failure := findProviderSnapshotForTest(t, providers, "claude-api-key", providerInventoryFailureItemKey)
	failureJSON := decodeProviderSnapshotForTest(t, failure.SnapshotJSON)
	if failure.Status == nil || *failure.Status != sidecarConditionUnobservable || failureJSON["condition"] != sidecarConditionUnobservable || failureJSON["partial"] != true {
		t.Fatalf("expected condition_unobservable provider failure snapshot, got snapshot=%+v json=%+v", failure, failureJSON)
	}
	if failureJSON["error_code"] != string(CLIProxyErrorUpstreamStatus) {
		t.Fatalf("expected upstream_status provider error code, got %+v", failureJSON)
	}

	failClaude = false
	now = now.Add(time.Minute)
	result, err = service.SyncSidecar(t.Context(), sidecar.ID)
	if err != nil || result.ProviderSnapshotCount != 5 {
		t.Fatalf("recovered provider endpoint should sync normally: result=%+v err=%v", result, err)
	}
	providers, err = service.store.ListProviderSnapshots(t.Context(), sidecar.ID)
	if err != nil || len(providers) != 5 {
		t.Fatalf("expected recovered provider sync to replace stale error snapshot, len=%d err=%v", len(providers), err)
	}
	for _, provider := range providers {
		if provider.ProviderItemKey == providerInventoryFailureItemKey {
			t.Fatalf("recovered provider sync left stale failure snapshot: %+v", providers)
		}
		requireProviderSnapshotDoesNotLeak(t, provider.SnapshotJSON, "redacted-claude-key")
	}

	emptyClaude = true
	now = now.Add(time.Minute)
	result, err = service.SyncSidecar(t.Context(), sidecar.ID)
	if err != nil || result.ProviderSnapshotCount != 4 {
		t.Fatalf("empty successful provider inventory should clear that provider without failing: result=%+v err=%v", result, err)
	}
	providers, err = service.store.ListProviderSnapshots(t.Context(), sidecar.ID)
	if err != nil || len(providers) != 4 {
		t.Fatalf("expected empty claude inventory to clear claude snapshots, len=%d err=%v", len(providers), err)
	}
	for _, provider := range providers {
		if provider.ProviderKey == "claude-api-key" {
			t.Fatalf("empty successful provider inventory left stale claude snapshot: %+v", providers)
		}
	}
}

func decodeProviderSnapshotForTest(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var snapshot map[string]any
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		t.Fatalf("decode provider snapshot JSON: %v raw=%s", err, raw)
	}
	return snapshot
}

func stringSliceFromSnapshot(t *testing.T, snapshot map[string]any, key string) []string {
	t.Helper()
	raw, ok := snapshot[key].([]any)
	if !ok {
		t.Fatalf("expected %s array in snapshot, got %+v", key, snapshot[key])
	}
	values := make([]string, 0, len(raw))
	for _, value := range raw {
		text, ok := value.(string)
		if !ok {
			t.Fatalf("expected %s string item, got %+v", key, value)
		}
		values = append(values, text)
	}
	return values
}

func requireProviderSnapshotDoesNotLeak(t *testing.T, raw json.RawMessage, forbidden ...string) {
	t.Helper()
	body := string(raw)
	for _, value := range forbidden {
		if strings.Contains(body, value) {
			t.Fatalf("provider snapshot leaked forbidden value %q in %s", value, body)
		}
	}
}

func findProviderSnapshotForTest(t *testing.T, snapshots []SidecarProviderSnapshot, providerKey string, itemKey string) SidecarProviderSnapshot {
	t.Helper()
	for _, snapshot := range snapshots {
		if snapshot.ProviderKey == providerKey && snapshot.ProviderItemKey == itemKey {
			return snapshot
		}
	}
	t.Fatalf("missing provider snapshot %s/%s in %+v", providerKey, itemKey, snapshots)
	return SidecarProviderSnapshot{}
}
