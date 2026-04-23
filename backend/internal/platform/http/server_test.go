package platformhttp

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/coachpo/prism/backend/internal/platform/config"
	"github.com/coachpo/prism/backend/internal/profiledomain"
)

func TestClassifyManagementRoute(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		method string
		path   string
		want   managementRouteClass
	}{
		{name: "protected auth route", method: http.MethodGet, path: "/api/auth/status", want: managementRouteClassM1},
		{name: "protected profile activation route", method: http.MethodPost, path: "/api/profiles/42/activate", want: managementRouteClassM1},
		{name: "general management route defaults to m2", method: http.MethodGet, path: "/api/settings/auth/proxy-keys", want: managementRouteClassM2},
		{name: "first shed stats route", method: http.MethodGet, path: "/api/stats/summary", want: managementRouteClassM3},
		{name: "trimmed mounted path still matches", method: http.MethodGet, path: "/realtime/ws", want: managementRouteClassM3},
		{name: "head maps to get", method: http.MethodHead, path: "/api/profiles/active", want: managementRouteClassM1},
		{name: "options bypasses admission", method: http.MethodOptions, path: "/api/profiles", want: managementRouteClassBypass},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := classifyManagementRoute(testCase.method, testCase.path); got != testCase.want {
				t.Fatalf("classifyManagementRoute(%q, %q) = %v, want %v", testCase.method, testCase.path, got, testCase.want)
			}
		})
	}
}

func TestManagementAdmissionControllerFastFailsLowerPriorityRoutes(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		method   string
		path     string
		m2Budget int64
		m3Budget int64
		holdM2   int64
		holdM3   int64
	}{
		{name: "m2 rejects when shared management lane is full", method: http.MethodGet, path: "/api/models", m2Budget: 1, m3Budget: 1, holdM2: 1},
		{name: "m3 rejects when first shed lane is full", method: http.MethodGet, path: "/api/stats/summary", m2Budget: 2, m3Budget: 1, holdM2: 1, holdM3: 1},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			controller := newManagementAdmissionController(config.Settings{ManagementAdmissionControlBudget: config.ManagementAdmissionBudget{M2MaxConcurrent: testCase.m2Budget, M3MaxConcurrent: testCase.m3Budget}})

			for range testCase.holdM2 {
				if !controller.m2.TryAcquire(1) {
					t.Fatal("expected to pre-acquire the shared M2 lane")
				}
				defer controller.m2.Release(1)
			}
			for range testCase.holdM3 {
				if !controller.m3.TryAcquire(1) {
					t.Fatal("expected to pre-acquire the M3 first-shed lane")
				}
				defer controller.m3.Release(1)
			}

			handlerCalled := false
			handler := controller.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				handlerCalled = true
				w.WriteHeader(http.StatusNoContent)
			}))

			request := httptest.NewRequest(testCase.method, testCase.path, nil)
			response := httptest.NewRecorder()
			startedAt := time.Now()
			handler.ServeHTTP(response, request)

			if elapsed := time.Since(startedAt); elapsed > 500*time.Millisecond {
				t.Fatalf("expected fast-fail in <= 500ms, got %s", elapsed)
			}
			if handlerCalled {
				t.Fatal("expected saturated lower-priority route to reject before hitting the handler")
			}
			if response.Code != http.StatusServiceUnavailable {
				t.Fatalf("expected 503 overload response, got %d", response.Code)
			}
			if retryAfter := response.Header().Get("Retry-After"); retryAfter != "1" {
				t.Fatalf("expected Retry-After header to be 1, got %q", retryAfter)
			}
		})
	}
}

func TestManagementAdmissionControllerBypassesProtectedRoutes(t *testing.T) {
	t.Parallel()

	controller := newManagementAdmissionController(config.Settings{ManagementAdmissionControlBudget: config.ManagementAdmissionBudget{M2MaxConcurrent: 1, M3MaxConcurrent: 1}})
	if !controller.m2.TryAcquire(1) {
		t.Fatal("expected to pre-acquire the shared M2 lane")
	}
	defer controller.m2.Release(1)
	if !controller.m3.TryAcquire(1) {
		t.Fatal("expected to pre-acquire the M3 first-shed lane")
	}
	defer controller.m3.Release(1)

	handlerCalled := false
	handler := controller.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusNoContent)
	}))

	request := httptest.NewRequest(http.MethodGet, "/api/profiles/active", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if !handlerCalled {
		t.Fatal("expected protected M1 route to bypass lower-priority admission caps")
	}
	if response.Code != http.StatusNoContent {
		t.Fatalf("expected protected route to reach handler, got %d", response.Code)
	}
}

func TestClassifyRuntimeCacheInvalidation(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		method    string
		path      string
		profileID string
		want      runtimeCacheInvalidationAction
	}{
		{name: "auth settings write invalidates runtime auth", method: http.MethodPut, path: "/api/settings/auth", want: runtimeCacheInvalidationAction{auth: true}},
		{name: "profile activation invalidates active profile", method: http.MethodPost, path: "/api/profiles/7/activate", want: runtimeCacheInvalidationAction{activeProfile: true}},
		{name: "costing write invalidates one planning snapshot", method: http.MethodPut, path: "/api/settings/costing", profileID: "42", want: runtimeCacheInvalidationAction{planningIDs: []int{42}}},
		{name: "vendor write invalidates all planning snapshots", method: http.MethodPatch, path: "/api/vendors/9", want: runtimeCacheInvalidationAction{planningAll: true}},
		{name: "preview route stays read only", method: http.MethodPost, path: "/api/config/profile/import/preview", profileID: "42", want: runtimeCacheInvalidationAction{}},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			header := http.Header{}
			if testCase.profileID != "" {
				header.Set(profiledomain.ProfileIDHeader, testCase.profileID)
			}
			got := classifyRuntimeCacheInvalidation(testCase.method, testCase.path, header)
			if got.auth != testCase.want.auth || got.activeProfile != testCase.want.activeProfile || got.planningAll != testCase.want.planningAll || !reflect.DeepEqual(got.planningIDs, testCase.want.planningIDs) {
				t.Fatalf("classifyRuntimeCacheInvalidation(%q, %q) = %+v, want %+v", testCase.method, testCase.path, got, testCase.want)
			}
		})
	}
}
