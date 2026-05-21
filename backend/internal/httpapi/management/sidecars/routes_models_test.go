package sidecars

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

func TestAuthFileModelsDiscoveryReadOnlyPassthrough(t *testing.T) {
	now := time.Date(2026, time.May, 21, 12, 0, 0, 0, time.UTC)
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodGet || r.URL.Path != "/v0/management/auth-files/models" {
			t.Fatalf("models discovery must call only the read-only models route, got %s %s", r.Method, r.URL.Path)
		}
		if r.URL.Query().Get("name") != "primary-oauth.json" {
			t.Fatalf("models discovery forwarded wrong name query: %q", r.URL.RawQuery)
		}
		if r.Header.Get("X-Management-Key") != "sync-secret" {
			t.Fatalf("expected management auth header, got %q", r.Header.Get("X-Management-Key"))
		}
		writeJSON(w, http.StatusOK, authFileModelsResponse{Models: []authFileModelResponse{{
			ID:          "gemini-1.5-pro",
			DisplayName: stringPtr("Gemini 1.5 Pro"),
			Type:        stringPtr("chat"),
			OwnedBy:     stringPtr("google"),
		}}})
	}))
	defer upstream.Close()
	service := newSyncTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL, true, 3600)
	router := chi.NewRouter()
	service.MountManagementRoutes(router)

	response := requestAuthFileModels(t, router, sidecar.ID, "primary-oauth.json", http.StatusOK)
	if calls != 1 {
		t.Fatalf("expected exactly one upstream models call, got %d", calls)
	}
	if len(response.Models) != 1 || response.Models[0].ID != "gemini-1.5-pro" || response.Models[0].DisplayName == nil || *response.Models[0].DisplayName != "Gemini 1.5 Pro" {
		t.Fatalf("unexpected models response: %+v", response)
	}
}
func TestAuthFileModelsDiscoveryUnsupportedMaps404(t *testing.T) {
	now := time.Date(2026, time.May, 21, 12, 0, 0, 0, time.UTC)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v0/management/auth-files/models" {
			t.Fatalf("unsupported models flow must not fall back to %s", r.URL.Path)
		}
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "management API disabled"})
	}))
	defer upstream.Close()
	service := newSyncTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL, true, 3600)
	router := chi.NewRouter()
	service.MountManagementRoutes(router)

	requestAuthFileModels(t, router, sidecar.ID, "primary-oauth.json", http.StatusNotFound)
}

func TestAuthFileModelsDiscoveryEmptySupported(t *testing.T) {
	now := time.Date(2026, time.May, 21, 12, 0, 0, 0, time.UTC)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v0/management/auth-files/models" {
			t.Fatalf("empty supported models flow must not fall back to %s", r.URL.Path)
		}
		writeJSON(w, http.StatusOK, authFileModelsResponse{Models: []authFileModelResponse{}})
	}))
	defer upstream.Close()
	service := newSyncTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL, true, 3600)
	router := chi.NewRouter()
	service.MountManagementRoutes(router)

	response := requestAuthFileModels(t, router, sidecar.ID, "empty-oauth.json", http.StatusOK)
	if response.Models == nil || len(response.Models) != 0 {
		t.Fatalf("expected supported empty models list, got %+v", response)
	}
}

func requestAuthFileModels(t *testing.T, router http.Handler, sidecarID int, name string, status int) authFileModelsResponse {
	t.Helper()
	path := "/sidecars/" + strconv.Itoa(sidecarID) + "/auth-files/models?name=" + name
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != status {
		t.Fatalf("expected status %d, got %d body=%s", status, rr.Code, rr.Body.String())
	}
	var response authFileModelsResponse
	if rr.Code == http.StatusOK {
		if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
			t.Fatalf("decode models response: %v", err)
		}
	}
	return response
}
