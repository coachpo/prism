package platformhttp

import (
	"encoding/json"
	"errors"
	"net/http"
	pathpkg "path"
	"strconv"
	"strings"
	"time"

	"github.com/coachpo/prism/backend/internal/platform/admission"
	"github.com/coachpo/prism/backend/internal/platform/config"
	"github.com/coachpo/prism/backend/internal/platform/priority"
)

const managementAdmissionTimeout = 60 * time.Second

type managementRouteSpec struct {
	name    string
	method  string
	pattern string
	tier    priority.ManagementTier
	timeout time.Duration
}

type hotAdmissionProvider interface {
	AdmissionSnapshot() HotAdmissionSnapshot
	RuntimeProxySnapshot() HotRuntimeProxySnapshot
}

type managementAdmissionController struct {
	controller *admission.Controller
	provider   hotAdmissionProvider
}

var managementRouteSpecs = []managementRouteSpec{
	{name: "auth status", method: http.MethodGet, pattern: "/auth/status", tier: priority.ManagementTierM1},
	{name: "auth public bootstrap", method: http.MethodGet, pattern: "/auth/public-bootstrap", tier: priority.ManagementTierM1},
	{name: "auth login", method: http.MethodPost, pattern: "/auth/login", tier: priority.ManagementTierM1},
	{name: "auth logout", method: http.MethodPost, pattern: "/auth/logout", tier: priority.ManagementTierM1},
	{name: "auth refresh", method: http.MethodPost, pattern: "/auth/refresh", tier: priority.ManagementTierM1},
	{name: "auth session", method: http.MethodGet, pattern: "/auth/session", tier: priority.ManagementTierM1},
	{name: "password reset request", method: http.MethodPost, pattern: "/auth/password-reset/request", tier: priority.ManagementTierM1},
	{name: "password reset confirm", method: http.MethodPost, pattern: "/auth/password-reset/confirm", tier: priority.ManagementTierM1},
	{name: "auth settings read", method: http.MethodGet, pattern: "/settings/auth", tier: priority.ManagementTierM2},
	{name: "auth settings write", method: http.MethodPut, pattern: "/settings/auth", tier: priority.ManagementTierM2},
	{name: "auth email verification request", method: http.MethodPost, pattern: "/settings/auth/email-verification/request", tier: priority.ManagementTierM2},
	{name: "auth email verification confirm", method: http.MethodPost, pattern: "/settings/auth/email-verification/confirm", tier: priority.ManagementTierM2},
	{name: "auth proxy keys list", method: http.MethodGet, pattern: "/settings/auth/proxy-keys", tier: priority.ManagementTierM2},
	{name: "auth proxy keys create", method: http.MethodPost, pattern: "/settings/auth/proxy-keys", tier: priority.ManagementTierM2},
	{name: "auth proxy key update", method: http.MethodPatch, pattern: "/settings/auth/proxy-keys/{key_id}", tier: priority.ManagementTierM2},
	{name: "auth proxy key rotate", method: http.MethodPost, pattern: "/settings/auth/proxy-keys/{key_id}/rotate", tier: priority.ManagementTierM2},
	{name: "auth proxy key delete", method: http.MethodDelete, pattern: "/settings/auth/proxy-keys/{key_id}", tier: priority.ManagementTierM2},
	{name: "audit logs list", method: http.MethodGet, pattern: "/audit/logs", tier: priority.ManagementTierM3},
	{name: "audit log read", method: http.MethodGet, pattern: "/audit/logs/{log_id}", tier: priority.ManagementTierM3},
	{name: "audit delete job create", method: http.MethodPost, pattern: "/audit/logs/delete-jobs", tier: priority.ManagementTierM3},
	{name: "log retention job create", method: http.MethodPost, pattern: "/maintenance/log-retention/jobs", tier: priority.ManagementTierM3},
	{name: "management jobs list", method: http.MethodGet, pattern: "/management/jobs", tier: priority.ManagementTierM3},
	{name: "management job read", method: http.MethodGet, pattern: "/management/jobs/{job_id}", tier: priority.ManagementTierM3},
	{name: "management job cancel", method: http.MethodPost, pattern: "/management/jobs/{job_id}/cancel", tier: priority.ManagementTierM3},
	{name: "bootstrap config read", method: http.MethodGet, pattern: "/config/bootstrap", tier: priority.ManagementTierM2},
	{name: "bootstrap config validate", method: http.MethodPost, pattern: "/config/bootstrap/validate", tier: priority.ManagementTierM2},
	{name: "bootstrap config write", method: http.MethodPut, pattern: "/config/bootstrap", tier: priority.ManagementTierM2},
	{name: "profile config export", method: http.MethodGet, pattern: "/config/profile/export", tier: priority.ManagementTierM3},
	{name: "profile config export with secrets", method: http.MethodPost, pattern: "/config/profile/export/with-secrets", tier: priority.ManagementTierM3},
	{name: "profile config import preview", method: http.MethodPost, pattern: "/config/profile/import/preview", tier: priority.ManagementTierM3},
	{name: "profile config import", method: http.MethodPost, pattern: "/config/profile/import", tier: priority.ManagementTierM3},
	{name: "vendor config export", method: http.MethodGet, pattern: "/config/vendors/export", tier: priority.ManagementTierM3},
	{name: "vendor config import preview", method: http.MethodPost, pattern: "/config/vendors/import/preview", tier: priority.ManagementTierM3},
	{name: "vendor config import", method: http.MethodPost, pattern: "/config/vendors/import", tier: priority.ManagementTierM3},
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
	{name: "model health-check preview", method: http.MethodPost, pattern: "/models/{model_config_id}/connections/health-check-preview", tier: priority.ManagementTierM3},
	{name: "model connection create", method: http.MethodPost, pattern: "/models/{model_config_id}/connections", tier: priority.ManagementTierM2},
	{name: "model connection priority update", method: http.MethodPatch, pattern: "/models/{model_config_id}/connections/{connection_id}/priority", tier: priority.ManagementTierM2},
	{name: "connection update", method: http.MethodPut, pattern: "/connections/{connection_id}", tier: priority.ManagementTierM2},
	{name: "connection pricing template set", method: http.MethodPut, pattern: "/connections/{connection_id}/pricing-template", tier: priority.ManagementTierM2},
	{name: "connection delete", method: http.MethodDelete, pattern: "/connections/{connection_id}", tier: priority.ManagementTierM2},
	{name: "connection health-check", method: http.MethodPost, pattern: "/connections/{connection_id}/health-check", tier: priority.ManagementTierM3},
	{name: "connection owner read", method: http.MethodGet, pattern: "/connections/{connection_id}/owner", tier: priority.ManagementTierM2},
	{name: "pricing templates list", method: http.MethodGet, pattern: "/pricing-templates", tier: priority.ManagementTierM2},
	{name: "pricing template create", method: http.MethodPost, pattern: "/pricing-templates", tier: priority.ManagementTierM2},
	{name: "pricing template read", method: http.MethodGet, pattern: "/pricing-templates/{template_id}", tier: priority.ManagementTierM2},
	{name: "pricing template update", method: http.MethodPut, pattern: "/pricing-templates/{template_id}", tier: priority.ManagementTierM2},
	{name: "pricing template delete", method: http.MethodDelete, pattern: "/pricing-templates/{template_id}", tier: priority.ManagementTierM2},
	{name: "pricing template connections list", method: http.MethodGet, pattern: "/pricing-templates/{template_id}/connections", tier: priority.ManagementTierM2},
	{name: "endpoint connections list", method: http.MethodGet, pattern: "/endpoints/connections", tier: priority.ManagementTierM2},
	{name: "endpoints list", method: http.MethodGet, pattern: "/endpoints", tier: priority.ManagementTierM2},
	{name: "endpoint create", method: http.MethodPost, pattern: "/endpoints", tier: priority.ManagementTierM2},
	{name: "endpoint update", method: http.MethodPut, pattern: "/endpoints/{endpoint_id}", tier: priority.ManagementTierM2},
	{name: "endpoint move", method: http.MethodPatch, pattern: "/endpoints/{endpoint_id}/position", tier: priority.ManagementTierM2},
	{name: "endpoint duplicate", method: http.MethodPost, pattern: "/endpoints/{endpoint_id}/duplicate", tier: priority.ManagementTierM2},
	{name: "endpoint delete", method: http.MethodDelete, pattern: "/endpoints/{endpoint_id}", tier: priority.ManagementTierM2},
	{name: "loadbalance strategies list", method: http.MethodGet, pattern: "/loadbalance/strategies", tier: priority.ManagementTierM2},
	{name: "loadbalance strategy create", method: http.MethodPost, pattern: "/loadbalance/strategies", tier: priority.ManagementTierM2},
	{name: "loadbalance strategy defaults create", method: http.MethodPost, pattern: "/loadbalance/strategies/defaults", tier: priority.ManagementTierM2},
	{name: "loadbalance strategy read", method: http.MethodGet, pattern: "/loadbalance/strategies/{strategy_id}", tier: priority.ManagementTierM2},
	{name: "loadbalance strategy update", method: http.MethodPut, pattern: "/loadbalance/strategies/{strategy_id}", tier: priority.ManagementTierM2},
	{name: "loadbalance strategy delete", method: http.MethodDelete, pattern: "/loadbalance/strategies/{strategy_id}", tier: priority.ManagementTierM2},
	{name: "loadbalance current-state list", method: http.MethodGet, pattern: "/loadbalance/current-state", tier: priority.ManagementTierM3},
	{name: "loadbalance current-state reset", method: http.MethodPost, pattern: "/loadbalance/current-state/{connection_id}/reset", tier: priority.ManagementTierM2},
	{name: "loadbalance events list", method: http.MethodGet, pattern: "/loadbalance/events", tier: priority.ManagementTierM3},
	{name: "loadbalance event read", method: http.MethodGet, pattern: "/loadbalance/events/{event_id}", tier: priority.ManagementTierM3},
	{name: "models by endpoints", method: http.MethodPost, pattern: "/models/by-endpoints", tier: priority.ManagementTierM2},
	{name: "model read", method: http.MethodGet, pattern: "/models/{model_config_id}", tier: priority.ManagementTierM2},
	{name: "model create", method: http.MethodPost, pattern: "/models", tier: priority.ManagementTierM2},
	{name: "model update", method: http.MethodPut, pattern: "/models/{model_config_id}", tier: priority.ManagementTierM2},
	{name: "model delete", method: http.MethodDelete, pattern: "/models/{model_config_id}", tier: priority.ManagementTierM2},
	{name: "models by endpoint", method: http.MethodGet, pattern: "/models/by-endpoint/{endpoint_id}", tier: priority.ManagementTierM2},
	{name: "models list", method: http.MethodGet, pattern: "/models", tier: priority.ManagementTierM2},
	{name: "profiles list", method: http.MethodGet, pattern: "/profiles", tier: priority.ManagementTierM2},
	{name: "profiles active", method: http.MethodGet, pattern: "/profiles/active", tier: priority.ManagementTierM1},
	{name: "profiles bootstrap", method: http.MethodGet, pattern: "/profiles/bootstrap", tier: priority.ManagementTierM1},
	{name: "profile create", method: http.MethodPost, pattern: "/profiles", tier: priority.ManagementTierM2},
	{name: "profile update", method: http.MethodPatch, pattern: "/profiles/{profile_id}", tier: priority.ManagementTierM2},
	{name: "profile activate", method: http.MethodPost, pattern: "/profiles/{profile_id}/activate", tier: priority.ManagementTierM1},
	{name: "profile delete", method: http.MethodDelete, pattern: "/profiles/{profile_id}", tier: priority.ManagementTierM2},
	{name: "realtime websocket", method: http.MethodGet, pattern: "/realtime/ws", tier: priority.ManagementTierM3},
	{name: "settings costing read", method: http.MethodGet, pattern: "/settings/costing", tier: priority.ManagementTierM2},
	{name: "settings costing write", method: http.MethodPut, pattern: "/settings/costing", tier: priority.ManagementTierM2},
	{name: "settings timezone read", method: http.MethodGet, pattern: "/settings/timezone", tier: priority.ManagementTierM2},
	{name: "settings timezone write", method: http.MethodPut, pattern: "/settings/timezone", tier: priority.ManagementTierM2},
	{name: "settings log retention read", method: http.MethodGet, pattern: "/settings/log-retention", tier: priority.ManagementTierM2},
	{name: "settings log retention write", method: http.MethodPut, pattern: "/settings/log-retention", tier: priority.ManagementTierM2},
	{name: "stats requests list", method: http.MethodGet, pattern: "/stats/requests", tier: priority.ManagementTierM3},
	{name: "dashboard stats", method: http.MethodGet, pattern: "/stats/dashboard", tier: priority.ManagementTierM3},
	{name: "stats request read", method: http.MethodGet, pattern: "/stats/requests/{request_id}", tier: priority.ManagementTierM3},
	{name: "stats summary", method: http.MethodGet, pattern: "/stats/summary", tier: priority.ManagementTierM3},
	{name: "stats model metrics", method: http.MethodPost, pattern: "/stats/models/metrics", tier: priority.ManagementTierM3},
	{name: "stats connection success rates", method: http.MethodGet, pattern: "/stats/connection-success-rates", tier: priority.ManagementTierM3},
	{name: "stats throughput", method: http.MethodGet, pattern: "/stats/throughput", tier: priority.ManagementTierM3},
	{name: "stats spending", method: http.MethodGet, pattern: "/stats/spending", tier: priority.ManagementTierM3},
	{name: "stats usage snapshot", method: http.MethodGet, pattern: "/stats/usage-snapshot", tier: priority.ManagementTierM3},
	{name: "stats endpoint model statistics", method: http.MethodGet, pattern: "/stats/endpoints/{endpoint_id}/models", tier: priority.ManagementTierM3},
	{name: "vendors list", method: http.MethodGet, pattern: "/vendors", tier: priority.ManagementTierM2},
	{name: "vendor create", method: http.MethodPost, pattern: "/vendors", tier: priority.ManagementTierM2},
	{name: "vendor read", method: http.MethodGet, pattern: "/vendors/{vendor_id}", tier: priority.ManagementTierM2},
	{name: "vendor models list", method: http.MethodGet, pattern: "/vendors/{vendor_id}/models", tier: priority.ManagementTierM2},
	{name: "vendor update", method: http.MethodPatch, pattern: "/vendors/{vendor_id}", tier: priority.ManagementTierM2},
	{name: "vendor delete", method: http.MethodDelete, pattern: "/vendors/{vendor_id}", tier: priority.ManagementTierM2},
}

func newHTTPAdmissionController(settings config.Settings) *admission.Controller {
	managementBudget := settings.ManagementAdmissionBudget()
	return admission.NewController(admission.Limits{
		ManagementM1: managementM1AdmissionBudget(settings, managementBudget),
		ManagementM2: managementBudget.M2MaxConcurrent,
		ManagementM3: managementBudget.M3MaxConcurrent,
	})
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
		defer release()
		next.ServeHTTP(w, r.WithContext(requestContext))
	})
}

func proxyAdmissionMiddleware(controller *admission.Controller, timeout time.Duration, next http.Handler) http.Handler {
	return proxyAdmissionProviderMiddleware(nil, controller, timeout, next)
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
