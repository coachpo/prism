package contracttest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/coachpo/prism/backend/internal/platform/config"
	platformhttp "github.com/coachpo/prism/backend/internal/platform/http"
)

func TestHealthVersionSurface(t *testing.T) {
	handler := newShellHandler(t)
	response := exerciseRequest(t, handler, "/health")

	if response.Code != http.StatusOK {
		t.Fatalf("expected /health to return 200, got %d", response.Code)
	}

	var payload struct {
		Status    string `json:"status"`
		Version   string `json:"version"`
		Liveness  string `json:"liveness"`
		Readiness string `json:"readiness"`
		Startup   string `json:"startup"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode /health response: %v", err)
	}

	expectedVersion := readBackendVersion(t)
	if payload.Status != "ok" {
		t.Fatalf("expected /health status ok, got %q", payload.Status)
	}
	if payload.Version == "" {
		t.Fatalf("expected /health version to be non-empty")
	}
	if payload.Version != expectedVersion {
		t.Fatalf("expected /health version %q, got %q", expectedVersion, payload.Version)
	}
	if payload.Liveness != "ok" {
		t.Fatalf("expected /health liveness ok, got %q", payload.Liveness)
	}
	if payload.Readiness != "ready" {
		t.Fatalf("expected /health readiness ready, got %q", payload.Readiness)
	}
	if payload.Startup != "complete" {
		t.Fatalf("expected /health startup complete, got %q", payload.Startup)
	}
}

func TestManagementCORSPreflight(t *testing.T) {
	handler, err := platformhttp.NewHandler(config.Settings{
		Host:               "127.0.0.1",
		Port:               8000,
		AppEnv:             config.EnvironmentDevelopment,
		CORSAllowedOrigins: "http://localhost:15173,http://127.0.0.1:15173",
	})
	if err != nil {
		t.Fatalf("build shell handler with local CORS origins: %v", err)
	}

	request := httptest.NewRequest(http.MethodOptions, "/api/models", nil)
	request.Header.Set("Origin", "http://localhost:15173")
	request.Header.Set("Access-Control-Request-Method", http.MethodGet)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("expected OPTIONS /api/models to return 204, got %d", response.Code)
	}
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:15173" {
		t.Fatalf("expected preflight allow origin header, got %q", got)
	}
	if got := response.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("expected preflight allow credentials header, got %q", got)
	}
	if got := response.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(got, http.MethodGet) {
		t.Fatalf("expected preflight allow methods to include GET, got %q", got)
	}
}

func newShellHandler(t *testing.T) http.Handler {
	t.Helper()

	handler, err := platformhttp.NewHandler(config.Settings{
		Host:   "127.0.0.1",
		Port:   8000,
		AppEnv: config.EnvironmentDevelopment,
	})
	if err != nil {
		t.Fatalf("build shell handler: %v", err)
	}

	return handler
}

func exerciseRequest(t *testing.T, handler http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, path, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func readBackendVersion(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test file path")
	}

	versionPath := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "VERSION"))
	raw, err := os.ReadFile(versionPath)
	if err != nil {
		t.Fatalf("read backend/VERSION: %v", err)
	}

	version := strings.TrimSpace(string(raw))
	if version == "" {
		t.Fatal("expected backend/VERSION to be non-empty")
	}

	return version
}
