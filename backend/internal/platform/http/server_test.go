package platformhttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	managementaudit "github.com/coachpo/prism/backend/internal/httpapi/management/audit"
	managementauth "github.com/coachpo/prism/backend/internal/httpapi/management/auth"
	managementconfigrules "github.com/coachpo/prism/backend/internal/httpapi/management/configrules"
	managementconnections "github.com/coachpo/prism/backend/internal/httpapi/management/connections"
	managementendpoints "github.com/coachpo/prism/backend/internal/httpapi/management/endpoints"
	managementloadbalance "github.com/coachpo/prism/backend/internal/httpapi/management/loadbalance"
	managementmodels "github.com/coachpo/prism/backend/internal/httpapi/management/models"
	managementprofiles "github.com/coachpo/prism/backend/internal/httpapi/management/profiles"
	managementsettings "github.com/coachpo/prism/backend/internal/httpapi/management/settings"
	managementstats "github.com/coachpo/prism/backend/internal/httpapi/management/stats"
	realtimeapi "github.com/coachpo/prism/backend/internal/httpapi/realtime"
	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
	"github.com/coachpo/prism/backend/internal/platform/admission"
	"github.com/coachpo/prism/backend/internal/platform/config"
	platformdb "github.com/coachpo/prism/backend/internal/platform/db"
	"github.com/coachpo/prism/backend/internal/platform/priority"
	"github.com/coachpo/prism/backend/internal/profiledomain"
)

func TestManagementRouteSpecClassification(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		method string
		path   string
		want   priority.ManagementTier
		ok     bool
	}{
		{name: "protected auth route", method: http.MethodGet, path: "/api/auth/status", want: priority.ManagementTierM1, ok: true},
		{name: "protected profile activation route", method: http.MethodPost, path: "/api/profiles/42/activate", want: priority.ManagementTierM1, ok: true},
		{name: "general management route has explicit m2 tier", method: http.MethodGet, path: "/api/settings/auth/proxy-keys", want: priority.ManagementTierM2, ok: true},
		{name: "connection batch read uses m2 tier", method: http.MethodPost, path: "/api/models/connections/batch", want: priority.ManagementTierM2, ok: true},
		{name: "first shed stats route", method: http.MethodGet, path: "/api/stats/summary", want: priority.ManagementTierM3, ok: true},
		{name: "trimmed mounted path still matches", method: http.MethodGet, path: "/realtime/ws", want: priority.ManagementTierM3, ok: true},
		{name: "head maps to get", method: http.MethodHead, path: "/api/profiles/active", want: priority.ManagementTierM1, ok: true},
		{name: "options bypasses admission", method: http.MethodOptions, path: "/api/profiles", ok: false},
		{name: "unknown management path stays unadmitted for router 404", method: http.MethodGet, path: "/api/not-mounted", ok: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, ok := matchManagementRouteSpec(testCase.method, testCase.path)
			if ok != testCase.ok {
				t.Fatalf("matchManagementRouteSpec(%q, %q) ok = %v, want %v", testCase.method, testCase.path, ok, testCase.ok)
			}
			if ok && got.tier != testCase.want {
				t.Fatalf("matchManagementRouteSpec(%q, %q) tier = %v, want %v", testCase.method, testCase.path, got.tier, testCase.want)
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

	request := httptest.NewRequest(http.MethodGet, "/api/profiles/active", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if !handlerCalled {
		t.Fatal("expected protected M1 route to use capacity isolated from lower-priority admission caps")
	}
	if response.Code != http.StatusNoContent {
		t.Fatalf("expected protected route to reach handler, got %d", response.Code)
	}
}

func TestManagementAdmissionUsesPublishedHotLimitsWithoutBlockingInflightRelease(t *testing.T) {
	settings := config.Settings{
		ManagementDatabasePoolBudget:     config.DatabasePoolBudget{MaxConns: 3},
		ManagementAdmissionControlBudget: config.ManagementAdmissionBudget{M2MaxConcurrent: 1, M3MaxConcurrent: 1},
	}
	runtime, err := NewHotBootstrapConfigRuntime(settings)
	if err != nil {
		t.Fatalf("create hot bootstrap runtime: %v", err)
	}
	controller := &managementAdmissionController{controller: newHTTPAdmissionController(settings), provider: runtime}
	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	handler := controller.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Test-Request") == "first" {
			close(firstEntered)
			<-releaseFirst
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	firstDone := make(chan int, 1)
	go func() {
		request := httptest.NewRequest(http.MethodGet, "/api/models", nil)
		request.Header.Set("X-Test-Request", "first")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		firstDone <- response.Code
	}()
	<-firstEntered

	updated := settings
	updated.ManagementAdmissionControlBudget = config.ManagementAdmissionBudget{M2MaxConcurrent: 2, M3MaxConcurrent: 1}
	retired, err := runtime.Publish(updated)
	if err != nil {
		t.Fatalf("publish updated admission limits: %v", err)
	}
	retired.CloseIdleConnections()

	secondRequest := httptest.NewRequest(http.MethodGet, "/api/models", nil)
	secondResponse := httptest.NewRecorder()
	handler.ServeHTTP(secondResponse, secondRequest)
	if secondResponse.Code != http.StatusNoContent {
		t.Fatalf("expected request under published admission limits to proceed, got %d", secondResponse.Code)
	}

	close(releaseFirst)
	if firstCode := <-firstDone; firstCode != http.StatusNoContent {
		t.Fatalf("expected in-flight request to release through old controller, got %d", firstCode)
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
	handler := proxyAdmissionProviderMiddleware(nil, controller, time.Minute, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		if _, ok := r.Context().Deadline(); !ok {
			t.Fatal("expected proxy request deadline")
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

func TestNewHandlerWithDependenciesMountsBaselineRoutes(t *testing.T) {
	t.Parallel()

	settings := config.Settings{
		Host:                             "127.0.0.1",
		Port:                             8000,
		AppEnv:                           config.EnvironmentDevelopment,
		RuntimeTransportConfig:           config.RuntimeTransportConfig{RequestTimeout: time.Second},
		ManagementDatabasePoolBudget:     config.DatabasePoolBudget{MaxConns: 4},
		ManagementAdmissionControlBudget: config.ManagementAdmissionBudget{M2MaxConcurrent: 2, M3MaxConcurrent: 1},
	}
	handler, err := NewHandlerWithDependencies(settings, Dependencies{
		Version:         "route-assembly-test",
		DatabasePools:   &platformdb.DatabasePools{},
		AuthService:     &managementauth.Service{},
		ProfilesService: &managementprofiles.Service{},
		RealtimeService: &realtimeapi.Service{},
		RuntimeService:  &runtimeapi.Service{},
	})
	if err != nil {
		t.Fatalf("create handler: %v", err)
	}
	router, ok := handler.(*chi.Mux)
	if !ok {
		t.Fatalf("expected handler to be chi mux, got %T", handler)
	}

	for _, route := range []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/health"},
		{method: http.MethodGet, path: "/api/auth/status"},
		{method: http.MethodGet, path: "/api/profiles/active"},
		{method: http.MethodGet, path: "/api/realtime/ws"},
		{method: http.MethodPost, path: "/v1/chat/completions"},
		{method: http.MethodPost, path: "/v1/messages"},
		{method: http.MethodPost, path: "/v1beta/models/gemini-pro:generateContent"},
	} {
		assertRouteMounted(t, router, route.method, route.path)
	}
	assertRouteNotMounted(t, router, http.MethodGet, "/metrics")
}

func assertRouteMounted(t *testing.T, router *chi.Mux, method string, path string) {
	t.Helper()
	routeContext := chi.NewRouteContext()
	if !router.Match(routeContext, method, path) {
		t.Fatalf("expected route %s %s to be mounted", method, path)
	}
}

func assertRouteNotMounted(t *testing.T, router *chi.Mux, method string, path string) {
	t.Helper()
	routeContext := chi.NewRouteContext()
	if router.Match(routeContext, method, path) {
		t.Fatalf("expected route %s %s to be unmapped", method, path)
	}
}

func TestManagementRouteSpecsCoverMountedRoutes(t *testing.T) {
	t.Parallel()

	managementRouter, ok := NewManagementRouter(
		&managementaudit.Service{},
		&managementauth.Service{},
		&managementconfigrules.Service{},
		&managementconnections.Service{},
		&managementendpoints.Service{},
		&managementloadbalance.Service{},
		&managementmodels.Service{},
		&managementprofiles.Service{},
		&realtimeapi.Service{},
		&managementsettings.Service{},
		&managementstats.Service{},
	).(*chi.Mux)
	if !ok {
		t.Fatal("expected management router to be a chi mux")
	}

	specs := make(map[string]struct{}, len(managementRouteSpecs))
	for _, routeSpec := range managementRouteSpecs {
		if routeSpec.tier == "" {
			t.Fatalf("route spec %q has no tier", routeSpec.name)
		}
		specs[routeKey(routeSpec.method, routeSpec.pattern)] = struct{}{}
	}

	walkErr := chi.Walk(managementRouter, func(method string, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if method == http.MethodHead || method == http.MethodOptions {
			return nil
		}
		key := routeKey(method, route)
		if _, ok := specs[key]; !ok {
			t.Fatalf("mounted management route %s %s has no admission route spec", method, route)
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk management routes: %v", walkErr)
	}
}

func routeKey(method string, route string) string {
	return strings.ToUpper(method) + " " + normalizeManagementRoutePath(route)
}

type managementRouteContractRow struct {
	RoutePattern             string   `json:"route_pattern"`
	Methods                  []string `json:"methods"`
	ProfileScoped            bool     `json:"profile_scoped"`
	InvalidatesAuth          bool     `json:"invalidates_auth"`
	InvalidatesActiveProfile bool     `json:"invalidates_active_profile"`
	InvalidatesPlanning      bool     `json:"invalidates_planning"`
	InvalidatesAllPlanning   bool     `json:"invalidates_all_planning"`
}

var routeContractPlaceholderPattern = regexp.MustCompile(`\{[^/]+\}`)

func loadManagementRouteContract(t *testing.T) []managementRouteContractRow {
	t.Helper()

	payload, err := os.ReadFile("management_route_contract.json")
	if err != nil {
		t.Fatalf("read management route contract manifest: %v", err)
	}
	var rows []managementRouteContractRow
	if err := json.Unmarshal(payload, &rows); err != nil {
		t.Fatalf("parse management route contract manifest: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("management route contract manifest is empty")
	}
	return rows
}

func sampleManagementRoutePath(routePattern string) string {
	return routeContractPlaceholderPattern.ReplaceAllString(routePattern, "7")
}

func expectedRuntimeCacheInvalidationAction(row managementRouteContractRow, method string) runtimeCacheInvalidationAction {
	if !isManagementMutationMethod(method) {
		return runtimeCacheInvalidationAction{}
	}
	action := runtimeCacheInvalidationAction{
		auth:          row.InvalidatesAuth,
		activeProfile: row.InvalidatesActiveProfile,
		planningAll:   row.InvalidatesAllPlanning,
	}
	if row.InvalidatesPlanning {
		action.planningIDs = []int{42}
	}
	return action
}

func assertRuntimeCacheInvalidationActionEqual(t *testing.T, method string, path string, got runtimeCacheInvalidationAction, want runtimeCacheInvalidationAction) {
	t.Helper()
	if got.auth != want.auth || got.activeProfile != want.activeProfile || got.planningAll != want.planningAll || !reflect.DeepEqual(got.planningIDs, want.planningIDs) {
		t.Fatalf("classifyRuntimeCacheInvalidation(%q, %q) = %+v, want %+v", method, path, got, want)
	}
}

func TestManagementRouteContractClassifiesRuntimeCacheInvalidation(t *testing.T) {
	t.Parallel()

	routeContract := loadManagementRouteContract(t)
	seenAuthInvalidation := false
	seenActiveProfileInvalidation := false
	seenPlanningInvalidation := false
	seenProfileScopedNonInvalidatingRead := false
	seenProfileScopedNonInvalidatingMutation := false

	for _, row := range routeContract {
		path := sampleManagementRoutePath(row.RoutePattern)
		if row.InvalidatesAuth {
			seenAuthInvalidation = true
		}
		if row.InvalidatesActiveProfile {
			seenActiveProfileInvalidation = true
		}
		if row.InvalidatesPlanning {
			seenPlanningInvalidation = true
		}
		if row.ProfileScoped && !row.InvalidatesAuth && !row.InvalidatesActiveProfile && !row.InvalidatesPlanning && !row.InvalidatesAllPlanning {
			for _, method := range row.Methods {
				normalizedMethod := strings.ToUpper(method)
				if normalizedMethod == http.MethodGet {
					seenProfileScopedNonInvalidatingRead = true
				}
				if isManagementMutationMethod(normalizedMethod) {
					seenProfileScopedNonInvalidatingMutation = true
				}
			}
		}

		for _, method := range row.Methods {
			method := strings.ToUpper(method)
			t.Run(method+" "+row.RoutePattern, func(t *testing.T) {
				t.Parallel()

				header := http.Header{}
				if row.ProfileScoped {
					header.Set(profiledomain.ProfileIDHeader, "42")
				}
				got := classifyRuntimeCacheInvalidation(method, path, header)
				want := expectedRuntimeCacheInvalidationAction(row, method)
				assertRuntimeCacheInvalidationActionEqual(t, method, path, got, want)
			})
		}
	}

	if !seenAuthInvalidation {
		t.Fatal("manifest should include runtime auth invalidation rows")
	}
	if !seenActiveProfileInvalidation {
		t.Fatal("manifest should include active-profile invalidation rows")
	}
	if !seenPlanningInvalidation {
		t.Fatal("manifest should include selected-profile planning invalidation rows")
	}
	if !seenProfileScopedNonInvalidatingRead {
		t.Fatal("manifest should include profile-scoped non-invalidating read rows")
	}
	if !seenProfileScopedNonInvalidatingMutation {
		t.Fatal("manifest should include profile-scoped non-invalidating mutation rows")
	}
}

func TestCORSMiddlewareUsesPublishedRuntimeOrigins(t *testing.T) {
	settings := config.Settings{
		AppEnv:             config.EnvironmentProduction,
		CORSAllowedOrigins: "https://old.example.test",
	}
	runtime, err := NewHotBootstrapConfigRuntime(settings)
	if err != nil {
		t.Fatalf("create hot bootstrap runtime: %v", err)
	}
	handler, err := NewHandlerWithDependencies(settings, Dependencies{Version: "cors-runtime-test", HotBootstrapConfigRuntime: runtime})
	if err != nil {
		t.Fatalf("create handler: %v", err)
	}

	assertCORSOrigin(t, handler, "https://old.example.test", "https://old.example.test")

	updated := settings
	updated.CORSAllowedOrigins = "https://new.example.test"
	retired, err := runtime.Publish(updated)
	if err != nil {
		t.Fatalf("publish CORS runtime update: %v", err)
	}
	retired.CloseIdleConnections()

	assertCORSOrigin(t, handler, "https://new.example.test", "https://new.example.test")
	assertCORSOrigin(t, handler, "https://old.example.test", "")
}

func assertCORSOrigin(t *testing.T, handler http.Handler, origin string, want string) {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	request.Header.Set("Origin", origin)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected health response status 200, got %d", response.Code)
	}
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != want {
		t.Fatalf("Access-Control-Allow-Origin for %q = %q, want %q", origin, got, want)
	}
}
