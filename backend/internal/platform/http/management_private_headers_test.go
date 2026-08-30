package platformhttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	managementmodels "github.com/coachpo/prism/backend/internal/httpapi/management/models"
	"github.com/coachpo/prism/backend/internal/platform/admission"
	"github.com/coachpo/prism/backend/internal/platform/bodylimits"
	"github.com/coachpo/prism/backend/internal/platform/config"
	platformcors "github.com/coachpo/prism/backend/internal/platform/cors"
	"github.com/coachpo/prism/backend/internal/platform/priority"
)

func TestPiPrivateNoStoreRoutePolicyIsExact(t *testing.T) {
	t.Parallel()

	privateRoutes := []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/api/models/exports/pi/source"},
		{method: http.MethodPost, path: "/api/models/exports/pi/render"},
		{method: http.MethodPost, path: "/api/models/7/pi/bind"},
		{method: http.MethodPost, path: "/api/models/7/pi/search"},
		{method: http.MethodPost, path: "/api/models/7/pi/refresh/preview"},
		{method: http.MethodPost, path: "/api/models/7/pi/refresh/commit"},
		{method: http.MethodPut, path: "/api/models/7/pi/override"},
		{method: http.MethodDelete, path: "/api/models/7/pi/override"},
		{method: http.MethodDelete, path: "/api/models/7/pi"},
	}
	for _, route := range privateRoutes {
		route := route
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			t.Parallel()
			response := exercisePrivateNoStoreMiddleware(route.method, route.path)
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
			}
			assertPiPrivateNoStoreHeaders(t, response.Header())
		})
	}

	nonPrivateRoutes := []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/api/models"},
		{method: http.MethodPut, path: "/api/models/7/catalog/override"},
		{method: http.MethodPatch, path: "/api/models/7/pi/override"},
		{method: http.MethodPost, path: "/api/models/exports/pi/resolve"},
		{method: http.MethodPost, path: "/api/models/exports/pi/render/extra"},
	}
	for _, route := range nonPrivateRoutes {
		route := route
		t.Run("unrelated "+route.method+" "+route.path, func(t *testing.T) {
			t.Parallel()
			response := exercisePrivateNoStoreMiddleware(route.method, route.path)
			if got := response.Header().Get("Cache-Control"); got != "" {
				t.Fatalf("unrelated route cache policy = %q, want empty", got)
			}
		})
	}
}

func TestPiPrivateNoStoreHeadersSurviveAdmissionRejection(t *testing.T) {
	t.Parallel()

	controller := &managementAdmissionController{controller: admission.NewController(admission.Limits{
		ManagementM1: 1,
		ManagementM2: 1,
		ManagementM3: 1,
	})}
	_, release, err := controller.controller.Admit(context.Background(), admission.Spec{
		Name: "held M2",
		Metadata: priority.Metadata{
			Priority:       priority.PriorityManagement,
			ManagementTier: priority.ManagementTierM2,
		},
		Timeout: time.Second,
	})
	if err != nil {
		t.Fatalf("pre-acquire M2 lane: %v", err)
	}
	defer release()

	handlerCalled := false
	handler := managementPrivateNoStoreMiddleware(controller.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusNoContent)
	})))
	request := httptest.NewRequest(http.MethodPut, "/api/models/7/pi/override", strings.NewReader(`{}`))
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if handlerCalled {
		t.Fatal("saturated admission should reject before the handler")
	}
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
	assertPiPrivateNoStoreHeaders(t, response.Header())
}

func TestPiPrivateNoStoreHeadersSurviveManagementMiddlewareRejections(t *testing.T) {
	t.Parallel()

	handler, err := NewHandlerWithDependencies(config.Settings{
		Host:                             "127.0.0.1",
		Port:                             8000,
		AppEnv:                           config.EnvironmentDevelopment,
		ManagementDatabasePoolBudget:     config.DatabasePoolBudget{MaxConns: 4},
		ManagementAdmissionControlBudget: config.ManagementAdmissionBudget{M2MaxConcurrent: 2, M3MaxConcurrent: 1},
	}, Dependencies{
		Version:            "pi-private-response-test",
		CORSOriginProvider: platformcors.NewStaticOriginProvider([]string{"http://allowed.local"}),
		ModelsService:      &managementmodels.Service{},
	})
	if err != nil {
		t.Fatalf("create handler: %v", err)
	}

	testCases := []struct {
		name        string
		method      string
		path        string
		body        string
		contentType string
		origin      string
		wantStatus  int
	}{
		{
			name:        "browser guard",
			method:      http.MethodPut,
			path:        "/api/models/7/pi/override",
			body:        `{}`,
			contentType: "application/json",
			origin:      "http://blocked.local",
			wantStatus:  http.StatusForbidden,
		},
		{
			name:        "media type guard",
			method:      http.MethodPost,
			path:        "/api/models/exports/pi/render",
			body:        `{}`,
			contentType: "text/plain",
			wantStatus:  http.StatusUnsupportedMediaType,
		},
		{
			name:        "body limit",
			method:      http.MethodPut,
			path:        "/api/models/7/pi/override",
			body:        strings.Repeat(" ", int(bodylimits.ManagementJSONRequestBodyLimitBytes)+1),
			contentType: "application/json",
			wantStatus:  http.StatusRequestEntityTooLarge,
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(testCase.method, testCase.path, strings.NewReader(testCase.body))
			request.Header.Set("Content-Type", testCase.contentType)
			if testCase.origin != "" {
				request.Header.Set("Origin", testCase.origin)
			}
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			if response.Code != testCase.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, testCase.wantStatus, response.Body.String())
			}
			assertPiPrivateNoStoreHeaders(t, response.Header())
		})
	}

	request := httptest.NewRequest(http.MethodPut, "/api/models/7/catalog/override", strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "text/plain")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("unrelated route status = %d, want %d", response.Code, http.StatusUnsupportedMediaType)
	}
	if got := response.Header().Get("Cache-Control"); got != "" {
		t.Fatalf("unrelated route cache policy = %q, want empty", got)
	}
}

func exercisePrivateNoStoreMiddleware(method string, path string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, nil)
	managementPrivateNoStoreMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})).ServeHTTP(response, request)
	return response
}

func assertPiPrivateNoStoreHeaders(t *testing.T, header http.Header) {
	t.Helper()
	if got := header.Values("Cache-Control"); len(got) != 1 || got[0] != "private, no-store" {
		t.Fatalf("Cache-Control = %q, want one private, no-store value", got)
	}
	for _, want := range []string{"authorization", "cookie", "x-profile-id"} {
		count := 0
		for _, line := range header.Values("Vary") {
			for token := range strings.SplitSeq(line, ",") {
				if strings.EqualFold(strings.TrimSpace(token), want) {
					count++
				}
			}
		}
		if count != 1 {
			t.Fatalf("Vary %q contains %q %d times, want once", header.Values("Vary"), want, count)
		}
	}
}
