package platformhttp

import (
	"context"
	"net/http"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"

	loadbalancedomain "github.com/coachpo/prism/backend/internal/domain/loadbalance"
	managementauth "github.com/coachpo/prism/backend/internal/httpapi/management/auth"
	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
	"github.com/coachpo/prism/backend/internal/pgxutil"
)

const defaultRuntimeCacheProfileID = 1

type dashboardSnapshotInvalidator interface {
	InvalidateDashboardSnapshot(profileID int)
	InvalidateAllDashboardSnapshots()
}

type runtimeCacheInvalidationMiddleware struct {
	planningCache      *runtimeapi.SharedCache
	authService        *managementauth.Service
	runtimeState       *loadbalancedomain.LocalRuntimeStateStore
	dashboardSnapshots dashboardSnapshotInvalidator
}

type runtimeCacheInvalidationAction struct {
	auth        bool
	planningAll bool
	planningIDs []int
}

type statusCapturingResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func newRuntimeCacheInvalidationMiddleware(planningCache *runtimeapi.SharedCache, authService *managementauth.Service, runtimeState *loadbalancedomain.LocalRuntimeStateStore, dashboardSnapshots dashboardSnapshotInvalidator) *runtimeCacheInvalidationMiddleware {
	if planningCache == nil && authService == nil && runtimeState == nil && dashboardSnapshots == nil {
		return nil
	}
	return &runtimeCacheInvalidationMiddleware{planningCache: planningCache, authService: authService, runtimeState: runtimeState, dashboardSnapshots: dashboardSnapshots}
}

func (m *runtimeCacheInvalidationMiddleware) Middleware(next http.Handler) http.Handler {
	if m == nil || (m.planningCache == nil && m.authService == nil && m.runtimeState == nil && m.dashboardSnapshots == nil) {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		normalizedMethod := strings.ToUpper(strings.TrimSpace(r.Method))
		if !isManagementMutationMethod(normalizedMethod) {
			next.ServeHTTP(w, r)
			return
		}
		action := classifyRuntimeCacheInvalidation(normalizedMethod, r.URL.Path, r.Header)
		if action.hasWork() {
			bumps := runtimeapi.RuntimeGenerationBumpsForRefresh(action.refreshRequest(), normalizedMethod+" "+normalizeManagementRoutePath(r.URL.Path))
			if len(bumps) > 0 {
				ctx := pgxutil.WithBeforeCommitHook(r.Context(), func(ctx context.Context, tx pgx.Tx) error {
					return runtimeapi.AdvanceRuntimeCacheGenerations(ctx, tx, bumps)
				})
				r = r.WithContext(ctx)
			}
		}
		writer := &statusCapturingResponseWriter{ResponseWriter: w}
		next.ServeHTTP(writer, r)
		if !isSuccessfulManagementMutation(normalizedMethod, writer.statusCode) {
			return
		}
		action.apply(m.planningCache, m.runtimeState, m.dashboardSnapshots)
	})
}

func (w *statusCapturingResponseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *statusCapturingResponseWriter) Write(payload []byte) (int, error) {
	if w.statusCode == 0 {
		w.statusCode = http.StatusOK
	}
	return w.ResponseWriter.Write(payload)
}

func isSuccessfulManagementMutation(method string, statusCode int) bool {
	if !isManagementMutationMethod(method) {
		return false
	}
	if statusCode == 0 {
		statusCode = http.StatusOK
	}
	return statusCode >= 200 && statusCode < 400
}

func isManagementMutationMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func classifyRuntimeCacheInvalidation(method string, rawPath string, _ http.Header) runtimeCacheInvalidationAction {
	normalizedMethod := strings.ToUpper(strings.TrimSpace(method))
	segments := managementRouteSegments(normalizeManagementRoutePath(rawPath))
	action := runtimeCacheInvalidationAction{}

	switch {
	case normalizedMethod == http.MethodPut && matchesSegments(segments, "settings", "auth"):
		action.auth = true
	case isRuntimeAuthProxyKeyMutation(normalizedMethod, segments):
		action.auth = true
	case normalizedMethod == http.MethodPut && matchesSegments(segments, "settings", "costing"):
		action.addPlanningProfile(defaultRuntimeCacheProfileID)
	case normalizedMethod == http.MethodPut && matchesSegments(segments, "settings", "audit"):
		action.addPlanningProfile(defaultRuntimeCacheProfileID)
	case isHeaderBlocklistMutation(normalizedMethod, segments):
		action.addPlanningProfile(defaultRuntimeCacheProfileID)
	case isModelPlanningMutation(normalizedMethod, segments):
		action.addPlanningProfile(defaultRuntimeCacheProfileID)
	case isEndpointPlanningMutation(normalizedMethod, segments):
		action.addPlanningProfile(defaultRuntimeCacheProfileID)
	case isConnectionPlanningMutation(normalizedMethod, segments):
		action.addPlanningProfile(defaultRuntimeCacheProfileID)
	case isLoadbalancePlanningMutation(normalizedMethod, segments):
		action.addPlanningProfile(defaultRuntimeCacheProfileID)
	}

	return action
}

func (a runtimeCacheInvalidationAction) apply(planningCache *runtimeapi.SharedCache, runtimeState *loadbalancedomain.LocalRuntimeStateStore, dashboardSnapshots dashboardSnapshotInvalidator) {
	request := a.refreshRequest()
	if planningCache != nil {
		if request.HasWork() {
			planningCache.ScheduleRefresh(request)
		}
	}
	a.invalidateDashboardSnapshots(dashboardSnapshots)
	if runtimeState == nil {
		return
	}
	if a.planningAll {
		runtimeState.ResetAll()
		return
	}
	for _, profileID := range a.planningIDs {
		runtimeState.ResetProfile(profileID)
	}
}

func (a runtimeCacheInvalidationAction) invalidateDashboardSnapshots(dashboardSnapshots dashboardSnapshotInvalidator) {
	if dashboardSnapshots == nil {
		return
	}
	if a.planningAll {
		dashboardSnapshots.InvalidateAllDashboardSnapshots()
		return
	}
	for _, profileID := range a.planningIDs {
		dashboardSnapshots.InvalidateDashboardSnapshot(profileID)
	}
}

func (a runtimeCacheInvalidationAction) refreshRequest() runtimeapi.RefreshRequest {
	return runtimeapi.RefreshRequest{
		Auth:               a.auth,
		PlanningAll:        a.planningAll,
		PlanningProfileIDs: append([]int(nil), a.planningIDs...),
	}
}

func (a runtimeCacheInvalidationAction) hasWork() bool {
	return a.refreshRequest().HasWork()
}

func (a *runtimeCacheInvalidationAction) addPlanningProfile(profileID int) {
	if profileID <= 0 {
		return
	}
	if slices.Contains(a.planningIDs, profileID) {
		return
	}
	a.planningIDs = append(a.planningIDs, profileID)
}

func matchesSegments(segments []string, pattern ...string) bool {
	if len(segments) != len(pattern) {
		return false
	}
	for idx := range pattern {
		if pattern[idx] == "*" {
			if segments[idx] == "" {
				return false
			}
			continue
		}
		if segments[idx] != pattern[idx] {
			return false
		}
	}
	return true
}

func isRuntimeAuthProxyKeyMutation(method string, segments []string) bool {
	return (method == http.MethodPost && matchesSegments(segments, "settings", "auth", "proxy-keys")) ||
		(method == http.MethodPatch && matchesSegments(segments, "settings", "auth", "proxy-keys", "*")) ||
		(method == http.MethodDelete && matchesSegments(segments, "settings", "auth", "proxy-keys", "*")) ||
		(method == http.MethodPost && matchesSegments(segments, "settings", "auth", "proxy-keys", "*", "rotate"))
}

func isHeaderBlocklistMutation(method string, segments []string) bool {
	return (method == http.MethodPost && matchesSegments(segments, "config", "header-blocklist-rules")) ||
		(method == http.MethodPatch && matchesSegments(segments, "config", "header-blocklist-rules", "*")) ||
		(method == http.MethodDelete && matchesSegments(segments, "config", "header-blocklist-rules", "*"))
}

func isModelPlanningMutation(method string, segments []string) bool {
	return (method == http.MethodPost && matchesSegments(segments, "models")) ||
		(method == http.MethodPut && matchesSegments(segments, "models", "*")) ||
		(method == http.MethodDelete && matchesSegments(segments, "models", "*")) ||
		(method == http.MethodPost && matchesSegments(segments, "models", "*", "targets")) ||
		(method == http.MethodPut && matchesSegments(segments, "models", "*", "targets", "*")) ||
		(method == http.MethodPatch && matchesSegments(segments, "models", "*", "targets", "*")) ||
		(method == http.MethodPatch && matchesSegments(segments, "models", "*", "targets", "*", "position")) ||
		(method == http.MethodDelete && matchesSegments(segments, "models", "*", "targets", "*"))
}

func isEndpointPlanningMutation(method string, segments []string) bool {
	return (method == http.MethodPost && matchesSegments(segments, "endpoints")) ||
		(method == http.MethodPut && matchesSegments(segments, "endpoints", "*")) ||
		(method == http.MethodDelete && matchesSegments(segments, "endpoints", "*")) ||
		(method == http.MethodPost && matchesSegments(segments, "endpoints", "*", "duplicate"))
}

func isConnectionPlanningMutation(method string, segments []string) bool {
	return (method == http.MethodPost && matchesSegments(segments, "models", "*", "connections")) ||
		(method == http.MethodPatch && matchesSegments(segments, "models", "*", "connections", "*")) ||
		(method == http.MethodDelete && matchesSegments(segments, "models", "*", "connections", "*")) ||
		(method == http.MethodPatch && matchesSegments(segments, "models", "*", "connections", "*", "priority")) ||
		(method == http.MethodPut && matchesSegments(segments, "connections", "*")) ||
		(method == http.MethodPut && matchesSegments(segments, "connections", "*", "pricing-template")) ||
		(method == http.MethodDelete && matchesSegments(segments, "connections", "*")) ||
		(method == http.MethodPost && matchesSegments(segments, "pricing-templates")) ||
		(method == http.MethodPost && matchesSegments(segments, "pricing-templates", "import")) ||
		(method == http.MethodPut && matchesSegments(segments, "pricing-templates", "*")) ||
		(method == http.MethodDelete && matchesSegments(segments, "pricing-templates", "*"))
}

func isLoadbalancePlanningMutation(method string, segments []string) bool {
	return (method == http.MethodPost && matchesSegments(segments, "loadbalance", "strategies")) ||
		(method == http.MethodPost && matchesSegments(segments, "loadbalance", "strategies", "defaults")) ||
		(method == http.MethodPut && matchesSegments(segments, "loadbalance", "strategies", "*")) ||
		(method == http.MethodDelete && matchesSegments(segments, "loadbalance", "strategies", "*"))
}
