package platformhttp

import (
	managementauth "github.com/coachpo/prism/backend/internal/httpapi/management/auth"
	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
	"github.com/coachpo/prism/backend/internal/platform/config"
	platformdb "github.com/coachpo/prism/backend/internal/platform/db"
	"github.com/coachpo/prism/backend/internal/profiledomain"
	"github.com/go-chi/chi/v5"
	"net/http"
	"strings"
	"testing"
)

func TestNewHandlerWithDependenciesMountsBaselineRoutes(t *testing.T) {
	t.Parallel()

	settings := config.Settings{
		Host:                             "127.0.0.1",
		Port:                             8000,
		AppEnv:                           config.EnvironmentDevelopment,
		ManagementDatabasePoolBudget:     config.DatabasePoolBudget{MaxConns: 4},
		ManagementAdmissionControlBudget: config.ManagementAdmissionBudget{M2MaxConcurrent: 2, M3MaxConcurrent: 1},
	}
	handler, err := NewHandlerWithDependencies(settings, Dependencies{
		Version:        "route-assembly-test",
		DatabasePools:  &platformdb.DatabasePools{},
		AuthService:    &managementauth.Service{},
		RuntimeService: &runtimeapi.Service{},
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
		{method: http.MethodPost, path: "/v1/chat/completions"},
		{method: http.MethodPost, path: "/v1/messages"},
		{method: http.MethodPost, path: "/v1beta/models/gemini-pro:generateContent"},
	} {
		assertRouteMounted(t, router, route.method, route.path)
	}
	assertRouteNotMounted(t, router, http.MethodGet, "/metrics")
}

func TestManagementRouteContractClassifiesRuntimeCacheInvalidation(t *testing.T) {
	t.Parallel()

	routeContract := loadManagementRouteContract(t)
	seenAuthInvalidation := false
	seenPlanningInvalidation := false
	seenProfileScopedNonInvalidatingRead := false
	seenProfileScopedNonInvalidatingMutation := false

	for _, row := range routeContract {
		path := sampleManagementRoutePath(row.RoutePattern)
		if row.InvalidatesAuth {
			seenAuthInvalidation = true
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
	if !seenPlanningInvalidation {
		t.Fatal("manifest should include Default-profile planning invalidation rows")
	}
	if !seenProfileScopedNonInvalidatingRead {
		t.Fatal("manifest should include profile-scoped non-invalidating read rows")
	}
	if !seenProfileScopedNonInvalidatingMutation {
		t.Fatal("manifest should include profile-scoped non-invalidating mutation rows")
	}
}
