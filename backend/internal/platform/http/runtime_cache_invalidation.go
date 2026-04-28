package platformhttp

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	loadbalancedomain "github.com/coachpo/prism/backend/internal/domain/loadbalance"
	managementauth "github.com/coachpo/prism/backend/internal/httpapi/management/auth"
	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
	"github.com/coachpo/prism/backend/internal/profiledomain"
)

type runtimeCacheInvalidationMiddleware struct {
	planningCache *runtimeapi.SharedCache
	authService   *managementauth.Service
	runtimeState  *loadbalancedomain.LocalRuntimeStateStore
}

type runtimeCacheInvalidationAction struct {
	auth          bool
	activeProfile bool
	planningAll   bool
	planningIDs   []int
}

type statusCapturingResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

const immediateRuntimeCacheRefreshTimeout = 30 * time.Second

func newRuntimeCacheInvalidationMiddleware(planningCache *runtimeapi.SharedCache, authService *managementauth.Service, runtimeState *loadbalancedomain.LocalRuntimeStateStore) *runtimeCacheInvalidationMiddleware {
	if planningCache == nil && authService == nil && runtimeState == nil {
		return nil
	}
	return &runtimeCacheInvalidationMiddleware{planningCache: planningCache, authService: authService, runtimeState: runtimeState}
}

func (m *runtimeCacheInvalidationMiddleware) Middleware(next http.Handler) http.Handler {
	if m == nil || (m.planningCache == nil && m.authService == nil && m.runtimeState == nil) {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		normalizedMethod := strings.ToUpper(strings.TrimSpace(r.Method))
		if !isManagementMutationMethod(normalizedMethod) {
			next.ServeHTTP(w, r)
			return
		}
		writer := &statusCapturingResponseWriter{ResponseWriter: w}
		next.ServeHTTP(writer, r)
		if !isSuccessfulManagementMutation(normalizedMethod, writer.statusCode) {
			return
		}
		action := classifyRuntimeCacheInvalidation(normalizedMethod, r.URL.Path, r.Header)
		action.apply(m.planningCache, m.authService, m.runtimeState)
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

func classifyRuntimeCacheInvalidation(method string, rawPath string, header http.Header) runtimeCacheInvalidationAction {
	normalizedMethod := strings.ToUpper(strings.TrimSpace(method))
	segments := managementRouteSegments(normalizeManagementRoutePath(rawPath))
	action := runtimeCacheInvalidationAction{}

	switch {
	case normalizedMethod == http.MethodPut && matchesSegments(segments, "settings", "auth"):
		action.auth = true
	case isRuntimeAuthProxyKeyMutation(normalizedMethod, segments):
		action.auth = true
	case normalizedMethod == http.MethodPost && matchesSegments(segments, "profiles", "*", "activate"):
		action.activeProfile = true
	case normalizedMethod == http.MethodPut && matchesSegments(segments, "settings", "costing"):
		action.addPlanningProfile(profileIDFromHeader(header))
	case normalizedMethod == http.MethodPost && matchesSegments(segments, "config", "profile", "import"):
		action.addPlanningProfile(profileIDFromHeader(header))
	case normalizedMethod == http.MethodPost && matchesSegments(segments, "config", "vendors", "import"):
		action.planningAll = true
	case isHeaderBlocklistMutation(normalizedMethod, segments):
		action.addPlanningProfile(profileIDFromHeader(header))
	case isModelPlanningMutation(normalizedMethod, segments):
		action.addPlanningProfile(profileIDFromHeader(header))
	case isEndpointPlanningMutation(normalizedMethod, segments):
		action.addPlanningProfile(profileIDFromHeader(header))
	case isConnectionPlanningMutation(normalizedMethod, segments):
		action.addPlanningProfile(profileIDFromHeader(header))
	case isLoadbalancePlanningMutation(normalizedMethod, segments):
		action.addPlanningProfile(profileIDFromHeader(header))
	case isVendorPlanningMutation(normalizedMethod, segments):
		action.planningAll = true
	}

	return action
}

func (a runtimeCacheInvalidationAction) apply(planningCache *runtimeapi.SharedCache, authService *managementauth.Service, runtimeState *loadbalancedomain.LocalRuntimeStateStore) {
	request := runtimeapi.RefreshRequest{
		Auth:               a.auth,
		ActiveProfile:      a.activeProfile,
		PlanningAll:        a.planningAll,
		PlanningProfileIDs: append([]int(nil), a.planningIDs...),
	}
	if planningCache != nil {
		if request.HasWork() {
			if request.Auth {
				ctx, cancel := context.WithTimeout(context.Background(), immediateRuntimeCacheRefreshTimeout)
				err := planningCache.RefreshNow(ctx, request)
				cancel()
				if err != nil {
					slog.Error("failed to publish runtime auth snapshot immediately", "error", err, "active_profile", request.ActiveProfile, "planning_all", request.PlanningAll, "planning_profile_ids", request.PlanningProfileIDs)
					planningCache.ScheduleRefresh(request)
				}
			} else {
				planningCache.ScheduleRefresh(request)
			}
		}
	} else if a.auth && authService != nil {
		authService.InvalidateRuntimeCache()
	}
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

func (a *runtimeCacheInvalidationAction) addPlanningProfile(profileID int) {
	if profileID <= 0 {
		return
	}
	for _, existing := range a.planningIDs {
		if existing == profileID {
			return
		}
	}
	a.planningIDs = append(a.planningIDs, profileID)
}

func profileIDFromHeader(header http.Header) int {
	trimmed := strings.TrimSpace(header.Get(profiledomain.ProfileIDHeader))
	if trimmed == "" {
		return 0
	}
	profileID, err := strconv.Atoi(trimmed)
	if err != nil || profileID <= 0 {
		return 0
	}
	return profileID
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
		(method == http.MethodDelete && matchesSegments(segments, "models", "*"))
}

func isEndpointPlanningMutation(method string, segments []string) bool {
	return (method == http.MethodPost && matchesSegments(segments, "endpoints")) ||
		(method == http.MethodPut && matchesSegments(segments, "endpoints", "*")) ||
		(method == http.MethodDelete && matchesSegments(segments, "endpoints", "*")) ||
		(method == http.MethodPost && matchesSegments(segments, "endpoints", "*", "duplicate"))
}

func isConnectionPlanningMutation(method string, segments []string) bool {
	return (method == http.MethodPost && matchesSegments(segments, "models", "*", "connections")) ||
		(method == http.MethodPatch && matchesSegments(segments, "models", "*", "connections", "*", "priority")) ||
		(method == http.MethodPut && matchesSegments(segments, "connections", "*")) ||
		(method == http.MethodPut && matchesSegments(segments, "connections", "*", "pricing-template")) ||
		(method == http.MethodDelete && matchesSegments(segments, "connections", "*")) ||
		(method == http.MethodPost && matchesSegments(segments, "pricing-templates")) ||
		(method == http.MethodPut && matchesSegments(segments, "pricing-templates", "*")) ||
		(method == http.MethodDelete && matchesSegments(segments, "pricing-templates", "*"))
}

func isLoadbalancePlanningMutation(method string, segments []string) bool {
	return (method == http.MethodPost && matchesSegments(segments, "loadbalance", "strategies")) ||
		(method == http.MethodPost && matchesSegments(segments, "loadbalance", "strategies", "defaults")) ||
		(method == http.MethodPut && matchesSegments(segments, "loadbalance", "strategies", "*")) ||
		(method == http.MethodDelete && matchesSegments(segments, "loadbalance", "strategies", "*"))
}

func isVendorPlanningMutation(method string, segments []string) bool {
	return (method == http.MethodPost && matchesSegments(segments, "vendors")) ||
		(method == http.MethodPatch && matchesSegments(segments, "vendors", "*")) ||
		(method == http.MethodDelete && matchesSegments(segments, "vendors", "*"))
}
