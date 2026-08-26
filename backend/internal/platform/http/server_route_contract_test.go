package platformhttp

import (
	"encoding/json"
	managementaudit "github.com/coachpo/prism/backend/internal/httpapi/management/audit"
	managementauth "github.com/coachpo/prism/backend/internal/httpapi/management/auth"
	managementconfigrules "github.com/coachpo/prism/backend/internal/httpapi/management/configrules"
	managementconnections "github.com/coachpo/prism/backend/internal/httpapi/management/connections"
	managementendpoints "github.com/coachpo/prism/backend/internal/httpapi/management/endpoints"
	managementloadbalance "github.com/coachpo/prism/backend/internal/httpapi/management/loadbalance"
	managementmodels "github.com/coachpo/prism/backend/internal/httpapi/management/models"
	managementsettings "github.com/coachpo/prism/backend/internal/httpapi/management/settings"
	managementstats "github.com/coachpo/prism/backend/internal/httpapi/management/stats"
	"github.com/coachpo/prism/backend/internal/platform/priority"
	"github.com/go-chi/chi/v5"
	"net/http"
	"os"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
)

func TestManagementMutationRouteSpecsDeclareCacheEffect(t *testing.T) {
	t.Parallel()

	seenExplicitNone := false
	seenPlanning := false
	for _, spec := range managementRouteSpecs {
		normalizedMethod := strings.ToUpper(strings.TrimSpace(spec.method))
		if !isManagementMutationMethod(normalizedMethod) {
			continue
		}
		effect := spec.cache
		declared := effect.none || effect.auth || effect.planning || effect.allPlanning || effect.routeWitness
		if !declared {
			t.Fatalf("mutation spec %s %s must declare cache: none or a non-none flag", spec.method, spec.pattern)
		}
		if effect.none {
			seenExplicitNone = true
		}
		if effect.planning {
			seenPlanning = true
		}
	}
	if !seenExplicitNone {
		t.Fatal("registry should include at least one explicitly non-invalidating mutation")
	}
	if !seenPlanning {
		t.Fatal("registry should include at least one planning-invalidating mutation")
	}
}

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
		{name: "general management route has explicit m2 tier", method: http.MethodGet, path: "/api/settings/auth/proxy-keys", want: priority.ManagementTierM2, ok: true},
		{name: "connection batch read uses m2 tier", method: http.MethodPost, path: "/api/models/connections/batch", want: priority.ManagementTierM2, ok: true},
		{name: "audit logs list uses first shed tier", method: http.MethodGet, path: "/api/audit/logs", want: priority.ManagementTierM3, ok: true},
		{name: "removed ghost audit delete job route is not admitted", method: http.MethodPost, path: "/api/audit/logs/delete-jobs", ok: false},
		{name: "maintenance log retention job uses first shed tier", method: http.MethodPost, path: "/api/maintenance/log-retention/jobs", want: priority.ManagementTierM3, ok: true},
		{name: "management jobs list uses first shed tier", method: http.MethodGet, path: "/api/management/jobs", want: priority.ManagementTierM3, ok: true},
		{name: "dashboard stats uses first shed tier", method: http.MethodGet, path: "/api/stats/dashboard", want: priority.ManagementTierM3, ok: true},
		{name: "first shed stats route", method: http.MethodGet, path: "/api/stats/summary", want: priority.ManagementTierM3, ok: true},
		{name: "model export source uses first shed tier", method: http.MethodGet, path: "/api/models/exports/pi/source", want: priority.ManagementTierM3, ok: true},
		{name: "model export render uses first shed tier", method: http.MethodPost, path: "/api/models/exports/opencode/render", want: priority.ManagementTierM3, ok: true},
		{name: "head maps to get", method: http.MethodHead, path: "/api/auth/status", want: priority.ManagementTierM1, ok: true},
		{name: "options bypasses admission", method: http.MethodOptions, path: "/api/models", ok: false},
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

func TestModelExportRouteSpecsAreScopedM3AndPlanningNeutral(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/api/models/exports/pi/source"},
		{method: http.MethodPost, path: "/api/models/exports/opencode/render"},
	}
	for _, testCase := range testCases {
		spec, ok := matchManagementRouteSpec(testCase.method, testCase.path)
		if !ok {
			t.Fatalf("missing model export route spec for %s %s", testCase.method, testCase.path)
		}
		if spec.tier != priority.ManagementTierM3 || !spec.profileScoped {
			t.Fatalf("model export route %s %s = tier %s, profileScoped=%v; want M3/true", testCase.method, testCase.path, spec.tier, spec.profileScoped)
		}
		if spec.cache.auth || spec.cache.planning || spec.cache.allPlanning || spec.cache.routeWitness {
			t.Fatalf("model export route %s %s must not invalidate runtime caches: %+v", testCase.method, testCase.path, spec.cache)
		}
		if isManagementMutationMethod(testCase.method) && !spec.cache.none {
			t.Fatalf("model export render must explicitly declare a none cache effect")
		}
	}
}

const updateRouteContractEnv = "PRISM_UPDATE_MANAGEMENT_ROUTE_CONTRACT"

// TestManagementRouteContractMatchesRouteSpecs is the reverse drift guard:
// the manifest rows must be exactly the registry's (pattern, cache-effect)
// groups. Regenerate the JSON with PRISM_UPDATE_MANAGEMENT_ROUTE_CONTRACT=1.
func TestManagementRouteContractMatchesRouteSpecs(t *testing.T) {
	methodsByHTTP := map[string]string{
		http.MethodGet:    "GET",
		http.MethodPost:   "POST",
		http.MethodPut:    "PUT",
		http.MethodPatch:  "PATCH",
		http.MethodDelete: "DELETE",
	}
	groups := map[string]*managementRouteContractRow{}
	for _, spec := range managementRouteSpecs {
		effect := spec.cache
		flags := []string{}
		if effect.auth {
			flags = append(flags, "auth")
		}
		if effect.planning {
			flags = append(flags, "planning")
		}
		if effect.allPlanning {
			flags = append(flags, "allPlanning")
		}
		key := spec.pattern + "|" + strings.Join(flags, ",")
		row, ok := groups[key]
		if !ok {
			row = &managementRouteContractRow{
				RoutePattern:           "/api" + spec.pattern,
				ProfileScoped:          spec.profileScoped,
				InvalidatesAuth:        effect.auth,
				InvalidatesPlanning:    effect.planning,
				InvalidatesAllPlanning: effect.allPlanning,
			}
			groups[key] = row
		}
		row.Methods = append(row.Methods, methodsByHTTP[spec.method])
		row.ProfileScoped = row.ProfileScoped && spec.profileScoped
	}
	rows := make([]managementRouteContractRow, 0, len(groups))
	for _, row := range groups {
		slices.Sort(row.Methods)
		rows = append(rows, *row)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].RoutePattern != rows[j].RoutePattern {
			return rows[i].RoutePattern < rows[j].RoutePattern
		}
		return strings.Join(rows[i].Methods, ",") < strings.Join(rows[j].Methods, ",")
	})

	if os.Getenv(updateRouteContractEnv) != "" {
		payload, err := json.MarshalIndent(rows, "", "  ")
		if err != nil {
			t.Fatalf("marshal regenerated route contract: %v", err)
		}
		if err := os.WriteFile("management_route_contract.json", append(payload, '\n'), 0o644); err != nil {
			t.Fatalf("write regenerated route contract: %v", err)
		}
	}

	raw, err := os.ReadFile("management_route_contract.json")
	if err != nil {
		t.Fatalf("read management route contract manifest: %v", err)
	}
	var current []managementRouteContractRow
	if err := json.Unmarshal(raw, &current); err != nil {
		t.Fatalf("parse management route contract manifest: %v", err)
	}
	if len(current) != len(rows) {
		t.Fatalf("management route contract manifest drifted from registry: got %d rows, want %d; run with %s=1 to regenerate", len(current), len(rows), updateRouteContractEnv)
	}
	for index := range rows {
		got := current[index]
		want := rows[index]
		if got.RoutePattern != want.RoutePattern || got.ProfileScoped != want.ProfileScoped || got.InvalidatesAuth != want.InvalidatesAuth || got.InvalidatesPlanning != want.InvalidatesPlanning || got.InvalidatesAllPlanning != want.InvalidatesAllPlanning || !slices.Equal(got.Methods, want.Methods) {
			t.Fatalf("management route contract manifest drifted at row %d: got %+v, want %+v; run with %s=1 to regenerate", index, got, want, updateRouteContractEnv)
		}
	}
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
		auth:        row.InvalidatesAuth,
		planningAll: row.InvalidatesAllPlanning,
	}
	if row.InvalidatesPlanning {
		action.planningIDs = []int{defaultRuntimeCacheProfileID}
	}
	return action
}

func assertRuntimeCacheInvalidationActionEqual(t *testing.T, method string, path string, got runtimeCacheInvalidationAction, want runtimeCacheInvalidationAction) {
	t.Helper()
	if got.auth != want.auth || got.planningAll != want.planningAll || !reflect.DeepEqual(got.planningIDs, want.planningIDs) {
		t.Fatalf("classifyRuntimeCacheInvalidation(%q, %q) = %+v, want %+v", method, path, got, want)
	}
}
