package contract_test

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

func TestServedDocsSurfaceRemoved(t *testing.T) {
	handler := newShellHandler(t)

	for _, path := range []string{"/docs", "/redoc", "/openapi.json"} {
		response := exerciseRequest(t, handler, path)
		if response.Code != http.StatusNotFound {
			t.Fatalf("expected %s to be absent with 404, got %d", path, response.Code)
		}
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

	request := httptest.NewRequest(http.MethodOptions, "/api/profiles", nil)
	request.Header.Set("Origin", "http://localhost:15173")
	request.Header.Set("Access-Control-Request-Method", http.MethodGet)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("expected OPTIONS /api/profiles to return 204, got %d", response.Code)
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

func TestNormativeDocsParity(t *testing.T) {
	assertFileContains(t, docsPath(t, "API_SPEC.md"), []string{
		"Proxy endpoints (`/v1/*`, `/v1beta/*`) always use the active profile and ignore management scope overrides.",
	})
	assertFileContains(t, docsPath(t, "ARCHITECTURE.md"), []string{
		"Runtime proxy routes (`/v1/*`, `/v1beta/*`) always use active profile and ignore override headers.",
	})
	assertFileContains(t, docsPath(t, "WORKFLOWS.md"), []string{
		"Runtime proxy traffic on `/v1/*` and `/v1beta/*` always uses the active profile, not the selected profile.",
	})
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

func assertFileContains(t *testing.T, path string, needles []string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	content := string(raw)
	for _, needle := range needles {
		if !strings.Contains(content, needle) {
			t.Fatalf("expected %s to contain %q", path, needle)
		}
	}
}

func docsPath(t *testing.T, name string) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test file path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", "docs", name))
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
