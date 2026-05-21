package sidecars

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

const (
	liveAuthID    = "auth-gemini-primary"
	liveAuthIndex = "auth_001"
	liveAuthName  = "gemini-primary.json"
)

type liveAuthFixture struct {
	Priority           int
	Provider           string
	Disabled           bool
	Unavailable        bool
	QuotaExceeded      bool
	QuotaNextRecoverAt *time.Time
	FailureCount       int
}

func seedLiveAuthFile(t *testing.T, service *Service, sidecarID int, observedAt time.Time, auth liveAuthFixture) {
	t.Helper()
	markLiveAuthFilesMetadataFresh(t, service, sidecarID, observedAt)
	recentRequests, err := json.Marshal([]map[string]any{{"window_start": observedAt.Add(-time.Minute).Format(time.RFC3339), "window_end": observedAt.Format(time.RFC3339), "failure_count": auth.FailureCount}})
	if err != nil {
		t.Fatalf("marshal recent requests: %v", err)
	}
	authJSON, err := json.Marshal(liveAuthPayload(auth))
	if err != nil {
		t.Fatalf("marshal auth file: %v", err)
	}
	priority := liveAuthPriority(auth.Priority)
	_, err = service.store.SaveAuthFile(t.Context(), SidecarAuthFileInput{SidecarID: sidecarID, AuthID: liveAuthID, AuthIndex: stringPtr(liveAuthIndex), Name: liveAuthName, Provider: stringPtr("gemini"), Label: stringPtr("Gemini primary"), Status: stringPtr("active"), Disabled: boolPtr(auth.Disabled), Unavailable: boolPtr(auth.Unavailable), Priority: intPtr(priority), QuotaExceeded: boolPtr(auth.QuotaExceeded), QuotaNextRecoverAt: cloneTimePtr(auth.QuotaNextRecoverAt), FailedCount: intPtr(auth.FailureCount), RecentRequestsJSON: recentRequests, ModelStatesJSON: json.RawMessage(`{}`), SnapshotJSON: authJSON, ObservedAt: observedAt})
	if err != nil {
		t.Fatalf("save live auth file fixture: %v", err)
	}
}

func markLiveAuthFilesMetadataFresh(t *testing.T, service *Service, sidecarID int, now time.Time) {
	t.Helper()
	staleAfter := now.Add(2 * time.Hour)
	_, err := service.store.UpdateSidecarSyncMetadata(t.Context(), SidecarSyncMetadataInput{SidecarID: sidecarID, LastSyncAt: now, LastSuccessfulSyncAt: &now, SnapshotStaleAfter: &staleAfter, ManagementAuthState: ManagementAuthStateValid})
	if err != nil {
		t.Fatalf("mark live auth-file metadata fresh: %v", err)
	}
}

func liveAuthPriority(priority int) int {
	return priority
}

func liveAuthPayload(auth liveAuthFixture) map[string]any {
	provider := auth.Provider
	if provider == "" {
		provider = "gemini"
	}
	payload := map[string]any{"id": liveAuthID, "auth_index": liveAuthIndex, "name": liveAuthName, "provider": provider, "label": "Gemini primary", "status": "active", "disabled": auth.Disabled, "unavailable": auth.Unavailable, "runtime_only": false, "source": "file", "path": "/mock/cliproxy/auth/" + liveAuthName, "priority": auth.Priority, "failed": auth.FailureCount, "recent_requests": []any{map[string]any{"window_start": "2026-05-10T11:59:00Z", "window_end": "2026-05-10T12:00:00Z", "failure_count": auth.FailureCount}}}
	if auth.QuotaExceeded || auth.QuotaNextRecoverAt != nil {
		quota := map[string]any{"exceeded": auth.QuotaExceeded, "reason": "rate_limit"}
		if auth.QuotaNextRecoverAt != nil {
			quota["next_recover_at"] = auth.QuotaNextRecoverAt.Format(time.RFC3339)
		}
		payload["quota"] = quota
	}
	return payload
}

func writeAuthFixtureJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}
