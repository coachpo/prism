package platformhttp

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	pathpkg "path"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/platform/admission"
	"github.com/coachpo/prism/backend/internal/platform/config"
	"github.com/coachpo/prism/backend/internal/platform/priority"
)

const managementAdmissionTimeout = 60 * time.Second

type managementRouteSpec struct {
	name                        string
	method                      string
	pattern                     string
	tier                        priority.ManagementTier
	timeout                     time.Duration
	releaseAdmissionFromHandler bool
	settingsSchemaGuard         bool
}

type hotAdmissionProvider interface {
	AdmissionSnapshot() HotAdmissionSnapshot
	RuntimeProxySnapshot() HotRuntimeProxySnapshot
}

type managementAdmissionController struct {
	controller *admission.Controller
	provider   hotAdmissionProvider
}

type settingsSchemaTransitionReader interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

var managementRouteSpecs = []managementRouteSpec{
	{name: "auth status", method: http.MethodGet, pattern: "/auth/status", tier: priority.ManagementTierM1},
	{name: "auth public bootstrap", method: http.MethodGet, pattern: "/auth/public-bootstrap", tier: priority.ManagementTierM1},
	{name: "auth login", method: http.MethodPost, pattern: "/auth/login", tier: priority.ManagementTierM1},
	{name: "auth logout", method: http.MethodPost, pattern: "/auth/logout", tier: priority.ManagementTierM1},
	{name: "auth refresh", method: http.MethodPost, pattern: "/auth/refresh", tier: priority.ManagementTierM1},
	{name: "auth session", method: http.MethodGet, pattern: "/auth/session", tier: priority.ManagementTierM1},
	{name: "auth public operation status", method: http.MethodGet, pattern: "/auth/operations/{operation_id}/status", tier: priority.ManagementTierM1},
	{name: "auth operation result", method: http.MethodGet, pattern: "/settings/auth/operations/{operation_id}", tier: priority.ManagementTierM2},
	{name: "auth settings read", method: http.MethodGet, pattern: "/settings/auth", tier: priority.ManagementTierM2},
	{name: "auth settings write", method: http.MethodPut, pattern: "/settings/auth", tier: priority.ManagementTierM2},
	{name: "auth proxy keys list", method: http.MethodGet, pattern: "/settings/auth/proxy-keys", tier: priority.ManagementTierM2},
	{name: "auth proxy keys create", method: http.MethodPost, pattern: "/settings/auth/proxy-keys", tier: priority.ManagementTierM2},
	{name: "auth proxy key update", method: http.MethodPatch, pattern: "/settings/auth/proxy-keys/{key_id}", tier: priority.ManagementTierM2},
	{name: "auth proxy key rotate", method: http.MethodPost, pattern: "/settings/auth/proxy-keys/{key_id}/rotate", tier: priority.ManagementTierM2},
	{name: "auth proxy key delete", method: http.MethodDelete, pattern: "/settings/auth/proxy-keys/{key_id}", tier: priority.ManagementTierM2},
	{name: "audit logs list", method: http.MethodGet, pattern: "/audit/logs", tier: priority.ManagementTierM3},
	{name: "audit log read", method: http.MethodGet, pattern: "/audit/logs/{log_id}", tier: priority.ManagementTierM3},
	{name: "audit request body download", method: http.MethodGet, pattern: "/audit/logs/{log_id}/body/request", tier: priority.ManagementTierM3},
	{name: "audit response body download", method: http.MethodGet, pattern: "/audit/logs/{log_id}/body/response", tier: priority.ManagementTierM3},
	{name: "audit delete job create", method: http.MethodPost, pattern: "/audit/logs/delete-jobs", tier: priority.ManagementTierM3},
	{name: "log retention job create", method: http.MethodPost, pattern: "/maintenance/log-retention/jobs", tier: priority.ManagementTierM3, settingsSchemaGuard: true},
	{name: "management jobs list", method: http.MethodGet, pattern: "/management/jobs", tier: priority.ManagementTierM3},
	{name: "management job read", method: http.MethodGet, pattern: "/management/jobs/{job_id}", tier: priority.ManagementTierM3},
	{name: "management job cancel", method: http.MethodPost, pattern: "/management/jobs/{job_id}/cancel", tier: priority.ManagementTierM3, settingsSchemaGuard: true},
	{name: "config header rules list", method: http.MethodGet, pattern: "/config/header-blocklist-rules", tier: priority.ManagementTierM2},
	{name: "config header rule read", method: http.MethodGet, pattern: "/config/header-blocklist-rules/{rule_id}", tier: priority.ManagementTierM2},
	{name: "config header rule create", method: http.MethodPost, pattern: "/config/header-blocklist-rules", tier: priority.ManagementTierM2},
	{name: "config header rule update", method: http.MethodPatch, pattern: "/config/header-blocklist-rules/{rule_id}", tier: priority.ManagementTierM2},
	{name: "config header rule delete", method: http.MethodDelete, pattern: "/config/header-blocklist-rules/{rule_id}", tier: priority.ManagementTierM2},
	{name: "config user-agent rules list", method: http.MethodGet, pattern: "/config/user-agent-client-rules", tier: priority.ManagementTierM2},
	{name: "config user-agent rule read", method: http.MethodGet, pattern: "/config/user-agent-client-rules/{rule_id}", tier: priority.ManagementTierM2},
	{name: "config user-agent rule create", method: http.MethodPost, pattern: "/config/user-agent-client-rules", tier: priority.ManagementTierM2},
	{name: "config user-agent rule update", method: http.MethodPatch, pattern: "/config/user-agent-client-rules/{rule_id}", tier: priority.ManagementTierM2},
	{name: "config user-agent rule delete", method: http.MethodDelete, pattern: "/config/user-agent-client-rules/{rule_id}", tier: priority.ManagementTierM2},
	{name: "model connection batch", method: http.MethodPost, pattern: "/models/connections/batch", tier: priority.ManagementTierM2},
	{name: "model connections list", method: http.MethodGet, pattern: "/models/{model_config_id}/connections", tier: priority.ManagementTierM2},
	{name: "model connection create", method: http.MethodPost, pattern: "/models/{model_config_id}/connections", tier: priority.ManagementTierM2},
	{name: "model connection update", method: http.MethodPatch, pattern: "/models/{model_config_id}/connections/{connection_id}", tier: priority.ManagementTierM2},
	{name: "model connection copy", method: http.MethodPost, pattern: "/models/{model_config_id}/connections/{connection_id}/copies", tier: priority.ManagementTierM2},
	{name: "model connection delete", method: http.MethodDelete, pattern: "/models/{model_config_id}/connections/{connection_id}", tier: priority.ManagementTierM2},
	{name: "model connection copies", method: http.MethodPost, pattern: "/models/{model_config_id}/connections/{connection_id}/copies", tier: priority.ManagementTierM2},
	{name: "legacy model connection put rejection", method: http.MethodPut, pattern: "/models/{model_config_id}/connections/{connection_id}", tier: priority.ManagementTierM2},
	{name: "legacy model connection priority rejection", method: http.MethodPatch, pattern: "/models/{model_config_id}/connections/{connection_id}/priority", tier: priority.ManagementTierM2},
	{name: "connections list", method: http.MethodGet, pattern: "/connections", tier: priority.ManagementTierM2},
	{name: "public connection create rejection", method: http.MethodPost, pattern: "/connections", tier: priority.ManagementTierM2},
	{name: "connection read", method: http.MethodGet, pattern: "/connections/{connection_id}", tier: priority.ManagementTierM2},
	{name: "public connection put rejection", method: http.MethodPut, pattern: "/connections/{connection_id}", tier: priority.ManagementTierM2},
	{name: "public connection patch rejection", method: http.MethodPatch, pattern: "/connections/{connection_id}", tier: priority.ManagementTierM2},
	{name: "public connection delete rejection", method: http.MethodDelete, pattern: "/connections/{connection_id}", tier: priority.ManagementTierM2},
	{name: "connection references list", method: http.MethodGet, pattern: "/connections/{connection_id}/references", tier: priority.ManagementTierM2},
	{name: "pricing templates list", method: http.MethodGet, pattern: "/pricing-templates", tier: priority.ManagementTierM2},
	{name: "pricing template create", method: http.MethodPost, pattern: "/pricing-templates", tier: priority.ManagementTierM2},
	{name: "pricing template import", method: http.MethodPost, pattern: "/pricing-templates/import", tier: priority.ManagementTierM2},
	{name: "pricing template import commit", method: http.MethodPost, pattern: "/pricing-templates/import/commit", tier: priority.ManagementTierM2},
	{name: "pricing template read", method: http.MethodGet, pattern: "/pricing-templates/{template_id}", tier: priority.ManagementTierM2},
	{name: "pricing template update", method: http.MethodPut, pattern: "/pricing-templates/{template_id}", tier: priority.ManagementTierM2},
	{name: "pricing template delete", method: http.MethodDelete, pattern: "/pricing-templates/{template_id}", tier: priority.ManagementTierM2},
	{name: "pricing template revisions", method: http.MethodGet, pattern: "/pricing-templates/{template_id}/revisions", tier: priority.ManagementTierM2},
	{name: "pricing template impact", method: http.MethodGet, pattern: "/pricing-templates/{template_id}/impact", tier: priority.ManagementTierM2},
	{name: "currency migration preview", method: http.MethodPost, pattern: "/settings/costing/currency-migrations/preview", tier: priority.ManagementTierM2},
	{name: "currency migration commit", method: http.MethodPost, pattern: "/settings/costing/currency-migrations/commit", tier: priority.ManagementTierM2},
	{name: "currency migration inventory templates", method: http.MethodGet, pattern: "/settings/costing/pricing-migration-inventories/{inventory_id}/templates", tier: priority.ManagementTierM2},
	{name: "currency migration inventory fx evidence", method: http.MethodGet, pattern: "/settings/costing/pricing-migration-inventories/{inventory_id}/fx-evidence", tier: priority.ManagementTierM2},
	{name: "currency migration draft create", method: http.MethodPost, pattern: "/settings/costing/currency-migration-drafts", tier: priority.ManagementTierM2},
	{name: "currency migration draft read", method: http.MethodGet, pattern: "/settings/costing/currency-migration-drafts/{draft_id}", tier: priority.ManagementTierM2},
	{name: "currency migration draft chunks", method: http.MethodGet, pattern: "/settings/costing/currency-migration-drafts/{draft_id}/chunks", tier: priority.ManagementTierM2},
	{name: "currency migration draft chunk write", method: http.MethodPut, pattern: "/settings/costing/currency-migration-drafts/{draft_id}/chunks/{ordinal}", tier: priority.ManagementTierM2},
	{name: "currency migration draft seal", method: http.MethodPost, pattern: "/settings/costing/currency-migration-drafts/{draft_id}/seal", tier: priority.ManagementTierM2},
	{name: "currency migration draft items", method: http.MethodGet, pattern: "/settings/costing/currency-migration-drafts/{draft_id}/items", tier: priority.ManagementTierM2},
	{name: "currency migration draft preview items", method: http.MethodGet, pattern: "/settings/costing/currency-migration-drafts/{draft_id}/preview-items", tier: priority.ManagementTierM2},
	{name: "pricing template connections list", method: http.MethodGet, pattern: "/pricing-templates/{template_id}/connections", tier: priority.ManagementTierM2},
	{name: "endpoint connections list", method: http.MethodGet, pattern: "/endpoints/connections", tier: priority.ManagementTierM2},
	{name: "endpoint references batch", method: http.MethodPost, pattern: "/endpoints/references/batch", tier: priority.ManagementTierM2},
	{name: "endpoints list", method: http.MethodGet, pattern: "/endpoints", tier: priority.ManagementTierM2},
	{name: "endpoint create", method: http.MethodPost, pattern: "/endpoints", tier: priority.ManagementTierM2},
	{name: "endpoint update", method: http.MethodPut, pattern: "/endpoints/{endpoint_id}", tier: priority.ManagementTierM2},
	{name: "endpoint references batch", method: http.MethodPost, pattern: "/endpoints/references/batch", tier: priority.ManagementTierM2},
	{name: "endpoint references detail", method: http.MethodGet, pattern: "/endpoints/{endpoint_id}/references", tier: priority.ManagementTierM2},
	{name: "endpoint verify", method: http.MethodPost, pattern: "/endpoints/{endpoint_id}/verify", tier: priority.ManagementTierM3},
	{name: "endpoint orphan cleanup", method: http.MethodDelete, pattern: "/endpoints/{endpoint_id}/orphan-connections/{connection_id}", tier: priority.ManagementTierM2},
	{name: "endpoint duplicate", method: http.MethodPost, pattern: "/endpoints/{endpoint_id}/duplicate", tier: priority.ManagementTierM2},
	{name: "endpoint delete", method: http.MethodDelete, pattern: "/endpoints/{endpoint_id}", tier: priority.ManagementTierM2},
	{name: "loadbalance strategies list", method: http.MethodGet, pattern: "/loadbalance/strategies", tier: priority.ManagementTierM2},
	{name: "loadbalance strategy create", method: http.MethodPost, pattern: "/loadbalance/strategies", tier: priority.ManagementTierM2, settingsSchemaGuard: true},
	{name: "loadbalance strategy defaults create", method: http.MethodPost, pattern: "/loadbalance/strategies/defaults", tier: priority.ManagementTierM2, settingsSchemaGuard: true},
	{name: "loadbalance strategy preview", method: http.MethodPost, pattern: "/loadbalance/strategies/preview", tier: priority.ManagementTierM2},
	{name: "loadbalance strategy read", method: http.MethodGet, pattern: "/loadbalance/strategies/{strategy_id}", tier: priority.ManagementTierM2},
	{name: "loadbalance strategy update", method: http.MethodPut, pattern: "/loadbalance/strategies/{strategy_id}", tier: priority.ManagementTierM2},
	{name: "loadbalance strategy set default", method: http.MethodPut, pattern: "/loadbalance/strategies/{strategy_id}/default", tier: priority.ManagementTierM2, settingsSchemaGuard: true},
	{name: "loadbalance strategy models list", method: http.MethodGet, pattern: "/loadbalance/strategies/{strategy_id}/models", tier: priority.ManagementTierM2},
	{name: "loadbalance strategy delete", method: http.MethodDelete, pattern: "/loadbalance/strategies/{strategy_id}", tier: priority.ManagementTierM2, settingsSchemaGuard: true},
	{name: "loadbalance current-state list", method: http.MethodGet, pattern: "/loadbalance/current-state", tier: priority.ManagementTierM3},
	{name: "loadbalance current-state reset", method: http.MethodPost, pattern: "/loadbalance/current-state/{connection_id}/reset", tier: priority.ManagementTierM2},
	{name: "loadbalance incidents list", method: http.MethodGet, pattern: "/loadbalance/incidents", tier: priority.ManagementTierM3},
	{name: "loadbalance events list", method: http.MethodGet, pattern: "/loadbalance/events", tier: priority.ManagementTierM3},
	{name: "loadbalance events query context", method: http.MethodPost, pattern: "/loadbalance/events/query-context", tier: priority.ManagementTierM3},
	{name: "loadbalance event read", method: http.MethodGet, pattern: "/loadbalance/events/{event_id}", tier: priority.ManagementTierM3},
	{name: "models by endpoints", method: http.MethodPost, pattern: "/models/by-endpoints", tier: priority.ManagementTierM2},
	{name: "model targets list", method: http.MethodGet, pattern: "/models/{model_config_id}/targets", tier: priority.ManagementTierM2},
	{name: "model target create", method: http.MethodPost, pattern: "/models/{model_config_id}/targets", tier: priority.ManagementTierM2},
	{name: "model target update", method: http.MethodPut, pattern: "/models/{model_config_id}/targets/{target_id}", tier: priority.ManagementTierM2},
	{name: "model target metadata patch", method: http.MethodPatch, pattern: "/models/{model_config_id}/targets/{target_id}", tier: priority.ManagementTierM2},
	{name: "model target move", method: http.MethodPatch, pattern: "/models/{model_config_id}/targets/{target_id}/position", tier: priority.ManagementTierM2},
	{name: "model target delete", method: http.MethodDelete, pattern: "/models/{model_config_id}/targets/{target_id}", tier: priority.ManagementTierM2},
	{name: "model read", method: http.MethodGet, pattern: "/models/{model_config_id}", tier: priority.ManagementTierM2},
	{name: "model routing diagnostics", method: http.MethodGet, pattern: "/models/{model_config_id}/routing-diagnostics", tier: priority.ManagementTierM2},
	{name: "model create", method: http.MethodPost, pattern: "/models", tier: priority.ManagementTierM2},
	{name: "model update", method: http.MethodPut, pattern: "/models/{model_config_id}", tier: priority.ManagementTierM2},
	{name: "model delete", method: http.MethodDelete, pattern: "/models/{model_config_id}", tier: priority.ManagementTierM2},
	{name: "models by endpoint", method: http.MethodGet, pattern: "/models/by-endpoint/{endpoint_id}", tier: priority.ManagementTierM2},
	{name: "models list", method: http.MethodGet, pattern: "/models", tier: priority.ManagementTierM2},
	{name: "models route-witness resolver", method: http.MethodGet, pattern: "/models/route-witnesses", tier: priority.ManagementTierM2},
	{name: "model routing diagnostics read", method: http.MethodGet, pattern: "/models/{model_config_id}/routing-diagnostics", tier: priority.ManagementTierM2},
	{name: "settings audit read", method: http.MethodGet, pattern: "/settings/audit", tier: priority.ManagementTierM2},
	{name: "settings audit write", method: http.MethodPut, pattern: "/settings/audit", tier: priority.ManagementTierM2, settingsSchemaGuard: true},
	{name: "settings audit storage summary", method: http.MethodGet, pattern: "/settings/audit/storage-summary", tier: priority.ManagementTierM3},
	{name: "settings costing read", method: http.MethodGet, pattern: "/settings/costing", tier: priority.ManagementTierM2},
	{name: "settings costing write", method: http.MethodPut, pattern: "/settings/costing", tier: priority.ManagementTierM2},
	{name: "settings log retention read", method: http.MethodGet, pattern: "/settings/log-retention", tier: priority.ManagementTierM2},
	{name: "settings log retention write", method: http.MethodPut, pattern: "/settings/log-retention", tier: priority.ManagementTierM2, settingsSchemaGuard: true},
	{name: "settings owner drift archive", method: http.MethodPost, pattern: "/settings/log-retention/owner-drift-archive", tier: priority.ManagementTierM2, settingsSchemaGuard: true},
	{name: "retention preflight create", method: http.MethodPost, pattern: "/maintenance/log-retention/preflights", tier: priority.ManagementTierM3, settingsSchemaGuard: true},
	{name: "retention job checkpoints", method: http.MethodGet, pattern: "/management/jobs/{job_id}/checkpoints", tier: priority.ManagementTierM3},
	{name: "retention job partitions", method: http.MethodGet, pattern: "/management/jobs/{job_id}/partitions", tier: priority.ManagementTierM3},
	{name: "stats requests list", method: http.MethodGet, pattern: "/stats/requests", tier: priority.ManagementTierM3},
	{name: "stats requests export", method: http.MethodGet, pattern: "/stats/requests/export", tier: priority.ManagementTierM3},
	{name: "stats cost segments", method: http.MethodGet, pattern: "/stats/cost-segments", tier: priority.ManagementTierM3},
	{name: "stats cost segment symbols", method: http.MethodGet, pattern: "/stats/cost-segments/{segment_key}/symbols", tier: priority.ManagementTierM3},
	{name: "stats request filter options", method: http.MethodGet, pattern: "/stats/request-filter-options/proxy-api-keys", tier: priority.ManagementTierM3},
	{name: "dashboard stats", method: http.MethodGet, pattern: "/stats/dashboard", tier: priority.ManagementTierM3},
	{name: "dashboard recent activity", method: http.MethodGet, pattern: "/stats/dashboard/recent-activity", tier: priority.ManagementTierM3},
	{name: "stats request read", method: http.MethodGet, pattern: "/stats/requests/{request_id}", tier: priority.ManagementTierM3},
	{name: "stats request export", method: http.MethodGet, pattern: "/stats/requests/export", tier: priority.ManagementTierM3},
	{name: "stats summary", method: http.MethodGet, pattern: "/stats/summary", tier: priority.ManagementTierM3},
	{name: "stats model metrics", method: http.MethodPost, pattern: "/stats/models/metrics", tier: priority.ManagementTierM3},
	{name: "stats connection success rates", method: http.MethodGet, pattern: "/stats/connection-success-rates", tier: priority.ManagementTierM3},
	{name: "stats throughput", method: http.MethodGet, pattern: "/stats/throughput", tier: priority.ManagementTierM3},
	{name: "stats spending", method: http.MethodGet, pattern: "/stats/spending", tier: priority.ManagementTierM3},
	{name: "stats usage snapshot", method: http.MethodGet, pattern: "/stats/usage-snapshot", tier: priority.ManagementTierM3},
	{name: "stats query context", method: http.MethodGet, pattern: "/stats/query-context", tier: priority.ManagementTierM3},
	{name: "stats usage summary", method: http.MethodGet, pattern: "/stats/usage-summary", tier: priority.ManagementTierM3},
	{name: "stats usage series", method: http.MethodGet, pattern: "/stats/usage-series", tier: priority.ManagementTierM3},
	{name: "stats usage errors", method: http.MethodGet, pattern: "/stats/usage-errors", tier: priority.ManagementTierM3},
	{name: "stats dashboard now", method: http.MethodGet, pattern: "/stats/dashboard/now", tier: priority.ManagementTierM3},
	{name: "stats observe activity", method: http.MethodGet, pattern: "/stats/observe-activity", tier: priority.ManagementTierM3},
	{name: "stats endpoint model statistics", method: http.MethodGet, pattern: "/stats/endpoints/{endpoint_id}/models", tier: priority.ManagementTierM3},
	{name: "stats endpoint terminal target statistics", method: http.MethodGet, pattern: "/stats/endpoints/{endpoint_id}/terminal-targets", tier: priority.ManagementTierM3},
}

// The clamp warning is emitted from buildHotAdmissionSnapshot, which runs on
// both the startup path and every hot config apply; warning here as well
// would log the same line twice per boot.
func newHTTPAdmissionController(settings config.Settings) *admission.Controller {
	managementBudget := settings.ManagementAdmissionBudget()
	return admission.NewController(admission.Limits{
		ManagementM1: managementM1AdmissionBudget(settings, managementBudget),
		ManagementM2: managementBudget.M2MaxConcurrent,
		ManagementM3: managementBudget.M3MaxConcurrent,
	})
}

func warnIfManagementAdmissionClamped(settings config.Settings) {
	configured, effective, clamped := settings.ManagementAdmissionClamp()
	if !clamped {
		return
	}
	slog.Warn(
		"management admission budget clamped by database.pools.management.maxConns; raise maxConns or lower m2MaxConcurrent",
		slog.Int64("configured_m2", configured.M2MaxConcurrent),
		slog.Int64("effective_m2", effective.M2MaxConcurrent),
		slog.Int64("configured_m3", configured.M3MaxConcurrent),
		slog.Int64("effective_m3", effective.M3MaxConcurrent),
		slog.Int("management_max_conns", int(settings.ManagementDatabaseBudget().MaxConns)),
	)
}

func managementM1AdmissionBudget(settings config.Settings, lowerPriorityBudget config.ManagementAdmissionBudget) int64 {
	reserved := int64(settings.ManagementDatabaseBudget().MaxConns) - lowerPriorityBudget.M2MaxConcurrent
	if reserved < 1 {
		return 1
	}
	return reserved
}

func (c *managementAdmissionController) Middleware(next http.Handler) http.Handler {
	if c == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		routeSpec, ok := matchManagementRouteSpec(r.Method, r.URL.Path)
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		controller := c.controller
		if c.provider != nil {
			controller = c.provider.AdmissionSnapshot().Controller()
		}
		requestContext, release, err := controller.Admit(r.Context(), routeSpec.AdmissionSpec())
		if err != nil {
			writeAdmissionError(w, err)
			return
		}
		if routeSpec.releaseAdmissionFromHandler {
			requestContext = admission.WithRelease(requestContext, release)
			defer release()
			next.ServeHTTP(w, r.WithContext(requestContext))
			return
		}
		defer release()
		next.ServeHTTP(w, r.WithContext(requestContext))
	})
}

// settingsSchemaGuardMiddleware is deliberately separate from admission so
// the auth middleware can remain the outer owner of protected routes. It
// checks only the exact method/path registry entries marked above.
type settingsSchemaGuardMiddleware struct {
	reader settingsSchemaTransitionReader
}

func (m *settingsSchemaGuardMiddleware) Middleware(next http.Handler) http.Handler {
	if m == nil || m.reader == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		routeSpec, ok := matchManagementRouteSpec(r.Method, r.URL.Path)
		if !ok || !routeSpec.settingsSchemaGuard {
			next.ServeHTTP(w, r)
			return
		}
		phase, exists, err := readSettingsSchemaPhase(r.Context(), m.reader)
		if err != nil {
			// A failed transition read must fail closed before the guarded
			// mutation reaches its handler. The safe response does not expose
			// database details or replay the request.
			writeSettingsSchemaStateUnavailable(w, r)
			return
		}
		if exists && (phase == "quiescing" || phase == "finalizing") {
			writeSettingsSchemaFinalizing(w, r, phase)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func readSettingsSchemaPhase(ctx context.Context, reader settingsSchemaTransitionReader) (string, bool, error) {
	var exists bool
	if err := reader.QueryRow(ctx, `SELECT to_regclass('public.settings_schema_transition') IS NOT NULL`).Scan(&exists); err != nil {
		return "", false, err
	}
	if !exists {
		return "", false, nil
	}
	var phase string
	if err := reader.QueryRow(ctx, `SELECT domain_phase FROM settings_schema_transition WHERE id = 1`).Scan(&phase); err != nil {
		return "", true, err
	}
	return phase, true, nil
}

func writeSettingsSchemaFinalizing(w http.ResponseWriter, r *http.Request, phase string) {
	requestID := middleware.GetReqID(r.Context())
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Retry-After", "3")
	w.WriteHeader(http.StatusServiceUnavailable)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"code":       "settings_schema_finalizing",
		"detail":     "Settings schema is quiescing/finalizing; refetch authoritative state before resubmitting",
		"params":     map[string]any{},
		"details":    map[string]any{"transition_phase": phase, "retry_after_seconds": 3, "retry_scope": "status_check_only", "recovery": "wait_then_refetch_before_resubmit"},
		"request_id": requestID,
	})
}

func writeSettingsSchemaStateUnavailable(w http.ResponseWriter, r *http.Request) {
	requestID := middleware.GetReqID(r.Context())
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Retry-After", "3")
	w.WriteHeader(http.StatusServiceUnavailable)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"code":       "internal_error",
		"detail":     "Settings schema transition state is unavailable",
		"params":     map[string]any{},
		"details":    map[string]any{"recovery": "retry", "retry_after_seconds": 3},
		"request_id": requestID,
	})
}

func proxyAdmissionProviderMiddleware(provider hotAdmissionProvider, fallbackController *admission.Controller, fallbackTimeout time.Duration, next http.Handler) http.Handler {
	if provider == nil && fallbackController == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		controller := fallbackController
		timeout := fallbackTimeout
		if provider != nil {
			runtimeProxy := provider.RuntimeProxySnapshot()
			timeout = runtimeProxy.TransportConfig().RequestTimeout
			controller = provider.AdmissionSnapshot().Controller()
		}
		spec := admission.Spec{
			Name:     "runtime proxy",
			Metadata: priority.Metadata{Priority: priority.PriorityProxy},
			Timeout:  timeout,
		}
		requestContext, release, err := controller.Admit(r.Context(), spec)
		if err != nil {
			writeAdmissionError(w, err)
			return
		}
		defer release()
		next.ServeHTTP(w, r.WithContext(requestContext))
	})
}

func (s managementRouteSpec) AdmissionSpec() admission.Spec {
	timeout := s.timeout
	if timeout == 0 {
		timeout = managementAdmissionTimeout
	}
	return admission.Spec{
		Name: s.name,
		Metadata: priority.Metadata{
			Priority:       priority.PriorityManagement,
			ManagementTier: s.tier,
		},
		Timeout: timeout,
	}
}

func matchManagementRouteSpec(method string, rawPath string) (managementRouteSpec, bool) {
	normalizedMethod := strings.ToUpper(strings.TrimSpace(method))
	if normalizedMethod == http.MethodHead {
		normalizedMethod = http.MethodGet
	}
	if normalizedMethod == "" || normalizedMethod == http.MethodOptions {
		return managementRouteSpec{}, false
	}

	normalizedPath := normalizeManagementRoutePath(rawPath)
	if normalizedPath == "" || normalizedPath == "/" {
		return managementRouteSpec{}, false
	}

	for _, spec := range managementRouteSpecs {
		if spec.matches(normalizedMethod, normalizedPath) {
			return spec, true
		}
	}
	return managementRouteSpec{}, false
}

func normalizeManagementRoutePath(rawPath string) string {
	trimmed := strings.TrimSpace(rawPath)
	if trimmed == "" {
		return ""
	}
	cleaned := pathpkg.Clean("/" + strings.TrimPrefix(trimmed, "/"))
	if cleaned == "/api" {
		return "/"
	}
	if strings.HasPrefix(cleaned, "/api/") {
		return strings.TrimPrefix(cleaned, "/api")
	}
	return cleaned
}

func (s managementRouteSpec) matches(method string, path string) bool {
	if s.method != method {
		return false
	}
	patternSegments := managementRouteSegments(normalizeManagementRoutePath(s.pattern))
	pathSegments := managementRouteSegments(path)
	if len(patternSegments) != len(pathSegments) {
		return false
	}
	for idx := range patternSegments {
		segment := patternSegments[idx]
		if strings.HasPrefix(segment, "{") && strings.HasSuffix(segment, "}") {
			if pathSegments[idx] == "" {
				return false
			}
			continue
		}
		if segment != pathSegments[idx] {
			return false
		}
	}
	return true
}

func managementRouteSegments(path string) []string {
	trimmed := strings.TrimPrefix(strings.TrimSpace(path), "/")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "/")
}

func writeAdmissionError(w http.ResponseWriter, err error) {
	if overload, ok := errors.AsType[*admission.OverloadError](err); ok {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if overload.RetryAfter > 0 {
			w.Header().Set("Retry-After", retryAfterHeaderValue(overload.RetryAfter))
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{"detail": "Management route temporarily overloaded. Retry later."})
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusInternalServerError)
	_ = json.NewEncoder(w).Encode(map[string]string{"detail": "Admission failed"})
}

func retryAfterHeaderValue(duration time.Duration) string {
	seconds := int(duration.Round(time.Second) / time.Second)
	seconds = max(seconds, 1)
	return strconv.Itoa(seconds)
}
