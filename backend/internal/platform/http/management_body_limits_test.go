package platformhttp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	managementauth "github.com/coachpo/prism/backend/internal/httpapi/management/auth"
	managementbootstrapconfig "github.com/coachpo/prism/backend/internal/httpapi/management/bootstrapconfig"
	managementconfigbundle "github.com/coachpo/prism/backend/internal/httpapi/management/configbundle"
)

func TestManagementBodyLimitMiddlewareRejectsOversizedBodiesWithStableJSON(t *testing.T) {
	handler := newManagementBodyLimitTestHandler()
	tests := []struct {
		name       string
		method     string
		path       string
		limitBytes int64
		body       string
	}{
		{
			name:       "auth login",
			method:     http.MethodPost,
			path:       "/api/auth/login",
			limitBytes: AuthRequestBodyLimitBytes,
			body:       `{"username":"` + strings.Repeat("a", int(AuthRequestBodyLimitBytes)+1),
		},
		{
			name:       "bootstrap config",
			method:     http.MethodPut,
			path:       "/api/config/bootstrap",
			limitBytes: BootstrapRequestBodyLimitBytes,
			body:       `{"values":{"padding":"` + strings.Repeat("a", int(BootstrapRequestBodyLimitBytes)+1),
		},
		{
			name:       "profile import preview",
			method:     http.MethodPost,
			path:       "/api/config/profile/import/preview",
			limitBytes: ConfigBundleRequestBodyLimitBytes,
			body:       `{"version":3,"bundle_kind":"profile_config","padding":"` + strings.Repeat("a", int(ConfigBundleRequestBodyLimitBytes)+1),
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(testCase.method, testCase.path, strings.NewReader(testCase.body))
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			assertRequestBodyTooLargeResponse(t, response, testCase.limitBytes)
		})
	}
}

func TestManagementRequestBodyLimitClassifiesExpectedCaps(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		limitBytes int64
		ok         bool
	}{
		{name: "auth", method: http.MethodPost, path: "/api/auth/login", limitBytes: AuthRequestBodyLimitBytes, ok: true},
		{name: "bootstrap", method: http.MethodPut, path: "/api/config/bootstrap", limitBytes: BootstrapRequestBodyLimitBytes, ok: true},
		{name: "config bundle", method: http.MethodPost, path: "/api/config/vendors/import/preview", limitBytes: ConfigBundleRequestBodyLimitBytes, ok: true},
		{name: "generic management", method: http.MethodPut, path: "/api/settings/costing", limitBytes: ManagementJSONRequestBodyLimitBytes, ok: true},
		{name: "profile export with secrets", method: http.MethodPost, path: "/api/config/profile/export/with-secrets", ok: false},
		{name: "read route", method: http.MethodGet, path: "/api/settings/auth", ok: false},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			limitBytes, ok := managementRequestBodyLimit(testCase.method, testCase.path)
			if ok != testCase.ok {
				t.Fatalf("expected ok=%v, got %v", testCase.ok, ok)
			}
			if limitBytes != testCase.limitBytes {
				t.Fatalf("expected limit %d, got %d", testCase.limitBytes, limitBytes)
			}
		})
	}
}

func newManagementBodyLimitTestHandler() http.Handler {
	managementHandler := NewManagementRouter(
		nil,
		&managementauth.Service{},
		&managementbootstrapconfig.Service{},
		&managementconfigbundle.Service{},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	router := chi.NewRouter()
	router.Mount("/api", managementBodyLimitMiddleware(managementHandler))
	return router
}

func assertRequestBodyTooLargeResponse(t *testing.T, response *httptest.ResponseRecorder, limitBytes int64) {
	t.Helper()
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected HTTP 413, got %d body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		Error      string `json:"error"`
		Message    string `json:"message"`
		LimitBytes int64  `json:"limit_bytes"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode oversized response: %v", err)
	}
	if payload.Error != RequestBodyTooLargeCode {
		t.Fatalf("expected error %q, got %q", RequestBodyTooLargeCode, payload.Error)
	}
	if payload.Message == "" {
		t.Fatal("expected human-readable message")
	}
	if payload.LimitBytes != limitBytes {
		t.Fatalf("expected limit_bytes %d, got %d", limitBytes, payload.LimitBytes)
	}
}
