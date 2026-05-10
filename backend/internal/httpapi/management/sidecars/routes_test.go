package sidecars

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/coachpo/prism/backend/internal/platform/config"
)

func TestSidecarRoutesMaskAndEncryptManagementPassword(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v0/management/auth-files" {
			t.Fatalf("unexpected upstream path %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer upstream.Close()

	service, err := NewService(config.Settings{SecretEncryptionKey: "unit-test-secret"}, Options{})
	if err != nil {
		t.Fatalf("new sidecar service: %v", err)
	}
	router := chi.NewRouter()
	service.MountManagementRoutes(router)

	createBody := `{"name":"Local CLIProxyAPI","base_url":"` + upstream.URL + `","management_password":"top-secret","allow_private_network":true,"allow_insecure_http":true}`
	createRecorder := httptest.NewRecorder()
	router.ServeHTTP(createRecorder, httptest.NewRequest(http.MethodPost, "/sidecars", strings.NewReader(createBody)))
	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", createRecorder.Code, createRecorder.Body.String())
	}
	if strings.Contains(createRecorder.Body.String(), "top-secret") || strings.Contains(createRecorder.Body.String(), "ManagementPasswordCiphertext") {
		t.Fatalf("create response leaked credential material: %s", createRecorder.Body.String())
	}
	var created sidecarInstanceResponse
	if err := json.Unmarshal(createRecorder.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if !created.CredentialState.ManagementPasswordConfigured || created.CredentialState.ManagementPasswordMasked == nil || *created.CredentialState.ManagementPasswordMasked != credentialMask {
		t.Fatalf("expected masked credential state, got %+v", created.CredentialState)
	}
	stored, ok, err := service.store.GetSidecarInstance(t.Context(), created.ID)
	if err != nil || !ok {
		t.Fatalf("load stored sidecar ok=%v err=%v", ok, err)
	}
	if stored.EncryptedManagementPassword == "" || stored.EncryptedManagementPassword == "top-secret" || !strings.HasPrefix(stored.EncryptedManagementPassword, "enc:") {
		t.Fatalf("expected encrypted stored password, got %q", stored.EncryptedManagementPassword)
	}

	testRecorder := httptest.NewRecorder()
	router.ServeHTTP(testRecorder, httptest.NewRequest(http.MethodPost, "/sidecars/"+strconv.Itoa(created.ID)+"/test-connection", nil))
	if testRecorder.Code != http.StatusFailedDependency {
		t.Fatalf("test connection status = %d body=%s", testRecorder.Code, testRecorder.Body.String())
	}
	if strings.Contains(testRecorder.Body.String(), "top-secret") {
		t.Fatalf("test connection response leaked credential material: %s", testRecorder.Body.String())
	}
	stored, ok, err = service.store.GetSidecarInstance(t.Context(), created.ID)
	if err != nil || !ok {
		t.Fatalf("reload stored sidecar ok=%v err=%v", ok, err)
	}
	if stored.ManagementAuthState != ManagementAuthStateInvalid || stored.AuthFailurePauseUntil == nil {
		t.Fatalf("expected invalid-management-auth pause metadata, got state=%q pause_until=%v", stored.ManagementAuthState, stored.AuthFailurePauseUntil)
	}
}

func TestSidecarRouteSkeletonsMount(t *testing.T) {
	t.Parallel()
	service, err := NewService(config.Settings{SecretEncryptionKey: "unit-test-secret"}, Options{})
	if err != nil {
		t.Fatalf("new sidecar service: %v", err)
	}
	router := chi.NewRouter()
	service.MountManagementRoutes(router)

	for _, route := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/sidecars"},
		{http.MethodPost, "/sidecars"},
		{http.MethodGet, "/sidecars/sc_1"},
		{http.MethodPatch, "/sidecars/sc_1"},
		{http.MethodDelete, "/sidecars/sc_1"},
		{http.MethodPost, "/sidecars/sc_1/test-connection"},
		{http.MethodPost, "/sidecars/sc_1/sync"},
		{http.MethodGet, "/sidecars/sc_1/auth-files"},
		{http.MethodPatch, "/sidecars/sc_1/auth-files/auth_001/status"},
		{http.MethodPatch, "/sidecars/sc_1/auth-files/auth_001/fields"},
		{http.MethodGet, "/sidecars/sc_1/auth-snapshots"},
		{http.MethodGet, "/sidecars/sc_1/auth-snapshots/snap_1"},
		{http.MethodGet, "/sidecars/sc_1/providers"},
		{http.MethodGet, "/sidecars/sc_1/provider-snapshots"},
		{http.MethodGet, "/sidecars/sc_1/sync-status"},
		{http.MethodGet, "/sidecars/sc_1/watchdog-policy"},
		{http.MethodPut, "/sidecars/sc_1/watchdog-policy"},
		{http.MethodGet, "/sidecars/sc_1/actions"},
	} {
		routeContext := chi.NewRouteContext()
		if !router.Match(routeContext, route.method, route.path) {
			t.Fatalf("expected route %s %s to be mounted", route.method, route.path)
		}
	}
}
