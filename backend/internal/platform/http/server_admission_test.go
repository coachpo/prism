package platformhttp

import (
	"context"
	"github.com/coachpo/prism/backend/internal/platform/admission"
	"github.com/coachpo/prism/backend/internal/platform/priority"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

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
		{name: "m3 rejects when first shed lane is full", method: http.MethodGet, path: "/api/stats/summary", m2Budget: 2, m3Budget: 1, holdM3: 1},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			controller := &managementAdmissionController{controller: admission.NewController(admission.Limits{ManagementM1: 1, ManagementM2: testCase.m2Budget, ManagementM3: testCase.m3Budget})}

			var releases []func()
			defer func() {
				for idx := len(releases) - 1; idx >= 0; idx-- {
					releases[idx]()
				}
			}()
			for range testCase.holdM2 {
				_, release, err := controller.controller.Admit(context.Background(), admission.Spec{Name: "held M2", Metadata: priority.Metadata{Priority: priority.PriorityManagement, ManagementTier: priority.ManagementTierM2}, Timeout: time.Second})
				if err != nil {
					t.Fatalf("expected to pre-acquire the shared M2 lane: %v", err)
				}
				releases = append(releases, release)
			}
			for range testCase.holdM3 {
				_, release, err := controller.controller.Admit(context.Background(), admission.Spec{Name: "held M3", Metadata: priority.Metadata{Priority: priority.PriorityManagement, ManagementTier: priority.ManagementTierM3}, Timeout: time.Second})
				if err != nil {
					t.Fatalf("expected to pre-acquire the M3 first-shed lane: %v", err)
				}
				releases = append(releases, release)
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

func TestConnectionBatchAdmissionBypassesM3Saturation(t *testing.T) {
	t.Parallel()

	controller := &managementAdmissionController{controller: admission.NewController(admission.Limits{ManagementM1: 1, ManagementM2: 2, ManagementM3: 1})}
	_, releaseM3, err := controller.controller.Admit(context.Background(), admission.Spec{Name: "held M3", Metadata: priority.Metadata{Priority: priority.PriorityManagement, ManagementTier: priority.ManagementTierM3}, Timeout: time.Second})
	if err != nil {
		t.Fatalf("expected to pre-acquire the M3 first-shed lane: %v", err)
	}
	defer releaseM3()

	handlerCalled := false
	handler := controller.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		metadata, err := priority.RequireMetadata(r.Context())
		if err != nil {
			t.Fatalf("expected priority metadata in request context: %v", err)
		}
		if metadata.ManagementTier != priority.ManagementTierM2 {
			t.Fatalf("expected connection batch to use M2, got %+v", metadata)
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	request := httptest.NewRequest(http.MethodPost, "/api/models/connections/batch", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if !handlerCalled {
		t.Fatal("expected connection batch read to bypass saturated M3 admission")
	}
	if response.Code != http.StatusNoContent {
		t.Fatalf("expected connection batch read to reach handler, got %d", response.Code)
	}
}

func TestManagementAdmissionControllerKeepsProtectedRoutesIsolated(t *testing.T) {
	t.Parallel()

	controller := &managementAdmissionController{controller: admission.NewController(admission.Limits{ManagementM1: 1, ManagementM2: 2, ManagementM3: 1})}
	_, releaseM3, err := controller.controller.Admit(context.Background(), admission.Spec{Name: "held M3", Metadata: priority.Metadata{Priority: priority.PriorityManagement, ManagementTier: priority.ManagementTierM3}, Timeout: time.Second})
	if err != nil {
		t.Fatalf("expected to pre-acquire the M3 first-shed lane: %v", err)
	}
	defer releaseM3()
	_, releaseM2, err := controller.controller.Admit(context.Background(), admission.Spec{Name: "held M2", Metadata: priority.Metadata{Priority: priority.PriorityManagement, ManagementTier: priority.ManagementTierM2}, Timeout: time.Second})
	if err != nil {
		t.Fatalf("expected to pre-acquire the shared M2 lane: %v", err)
	}
	defer releaseM2()

	handlerCalled := false
	handler := controller.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusNoContent)
	}))

	request := httptest.NewRequest(http.MethodGet, "/api/auth/status", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if !handlerCalled {
		t.Fatal("expected protected M1 route to use capacity isolated from lower-priority admission caps")
	}
	if response.Code != http.StatusNoContent {
		t.Fatalf("expected protected route to reach handler, got %d", response.Code)
	}
}

func TestAdmissionAttachesServerSideWorkloadAndIgnoresPriorityHeaders(t *testing.T) {
	t.Parallel()

	controller := &managementAdmissionController{controller: admission.NewController(admission.Limits{ManagementM1: 1, ManagementM2: 2, ManagementM3: 1})}
	handler := controller.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		metadata, err := priority.RequireMetadata(r.Context())
		if err != nil {
			t.Fatalf("expected priority metadata in request context: %v", err)
		}
		if metadata.Priority != priority.PriorityManagement || metadata.ManagementTier != priority.ManagementTierM3 {
			t.Fatalf("expected server-side M3 management metadata, got %+v", metadata)
		}
		workload, err := admission.RequireWorkload(r.Context())
		if err != nil {
			t.Fatalf("expected admitted workload context: %v", err)
		}
		if workload.Name != "stats summary" {
			t.Fatalf("expected route spec workload name, got %q", workload.Name)
		}
		if _, ok := r.Context().Deadline(); !ok {
			t.Fatal("expected admitted management request to have a deadline")
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	request := httptest.NewRequest(http.MethodGet, "/api/stats/summary", nil)
	request.Header.Set("X-Prism-Priority", "proxy")
	request.Header.Set("X-Management-Tier", "M1")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("expected admitted request to reach handler, got %d", response.Code)
	}
}

func TestProxyAdmissionAttachesServerSideWorkload(t *testing.T) {
	t.Parallel()

	controller := admission.NewController(admission.Limits{Proxy: 1})
	handler := proxyAdmissionProviderMiddleware(nil, controller, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		metadata, err := priority.RequireMetadata(r.Context())
		if err != nil {
			t.Fatalf("expected proxy priority metadata: %v", err)
		}
		if metadata.Priority != priority.PriorityProxy {
			t.Fatalf("expected proxy priority, got %+v", metadata)
		}
		workload, err := admission.RequireWorkload(r.Context())
		if err != nil {
			t.Fatalf("expected proxy workload context: %v", err)
		}
		if workload.Name != "runtime proxy" {
			t.Fatalf("expected runtime proxy workload, got %q", workload.Name)
		}
		if _, ok := r.Context().Deadline(); ok {
			t.Fatal("expected no proxy request deadline after runtime.transport removal")
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	request.Header.Set("X-Prism-Priority", "background")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("expected proxy admission to reach handler, got %d", response.Code)
	}
}
