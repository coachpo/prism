package platformhttp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	pathpkg "path"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/semaphore"

	managementaudit "github.com/coachpo/prism/backend/internal/httpapi/management/audit"
	managementauth "github.com/coachpo/prism/backend/internal/httpapi/management/auth"
	managementconfigbundle "github.com/coachpo/prism/backend/internal/httpapi/management/configbundle"
	managementconfigrules "github.com/coachpo/prism/backend/internal/httpapi/management/configrules"
	managementconnections "github.com/coachpo/prism/backend/internal/httpapi/management/connections"
	managementendpoints "github.com/coachpo/prism/backend/internal/httpapi/management/endpoints"
	managementloadbalance "github.com/coachpo/prism/backend/internal/httpapi/management/loadbalance"
	managementmodels "github.com/coachpo/prism/backend/internal/httpapi/management/models"
	managementprofiles "github.com/coachpo/prism/backend/internal/httpapi/management/profiles"
	managementsettings "github.com/coachpo/prism/backend/internal/httpapi/management/settings"
	managementstats "github.com/coachpo/prism/backend/internal/httpapi/management/stats"
	managementvendors "github.com/coachpo/prism/backend/internal/httpapi/management/vendors"
	"github.com/coachpo/prism/backend/internal/httpapi/openapi"
	realtimeapi "github.com/coachpo/prism/backend/internal/httpapi/realtime"
	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
	"github.com/coachpo/prism/backend/internal/platform/config"
	"github.com/coachpo/prism/backend/internal/platform/version"
)

type Dependencies struct {
	Version             string
	OpenAPI             *openapi.Document
	AuditService        *managementaudit.Service
	AuthService         *managementauth.Service
	RuntimeAuthService  *managementauth.Service
	ConfigBundleService *managementconfigbundle.Service
	ConfigRulesService  *managementconfigrules.Service
	ConnectionsService  *managementconnections.Service
	EndpointsService    *managementendpoints.Service
	LoadbalanceService  *managementloadbalance.Service
	ModelsService       *managementmodels.Service
	ProfilesService     *managementprofiles.Service
	RealtimeService     *realtimeapi.Service
	RuntimeService      *runtimeapi.Service
	RuntimeCache        *runtimeapi.SharedCache
	SettingsService     *managementsettings.Service
	StatsService        *managementstats.Service
	VendorsService      *managementvendors.Service
}

type healthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

type managementRouteClass int

const (
	managementRouteClassBypass managementRouteClass = iota
	managementRouteClassM1
	managementRouteClassM2
	managementRouteClassM3
)

type managementRouteRule struct {
	method  string
	pattern string
	class   managementRouteClass
}

var managementRouteRules = []managementRouteRule{
	{method: http.MethodGet, pattern: "/auth/status", class: managementRouteClassM1},
	{method: http.MethodGet, pattern: "/auth/public-bootstrap", class: managementRouteClassM1},
	{method: http.MethodPost, pattern: "/auth/login", class: managementRouteClassM1},
	{method: http.MethodPost, pattern: "/auth/logout", class: managementRouteClassM1},
	{method: http.MethodPost, pattern: "/auth/refresh", class: managementRouteClassM1},
	{method: http.MethodGet, pattern: "/auth/session", class: managementRouteClassM1},
	{method: http.MethodPost, pattern: "/auth/password-reset/request", class: managementRouteClassM1},
	{method: http.MethodPost, pattern: "/auth/password-reset/confirm", class: managementRouteClassM1},
	{method: http.MethodGet, pattern: "/profiles/bootstrap", class: managementRouteClassM1},
	{method: http.MethodGet, pattern: "/profiles/active", class: managementRouteClassM1},
	{method: http.MethodPost, pattern: "/profiles/{profile_id}/activate", class: managementRouteClassM1},
	{method: http.MethodGet, pattern: "/realtime/ws", class: managementRouteClassM3},
	{method: http.MethodGet, pattern: "/stats/requests", class: managementRouteClassM3},
	{method: http.MethodDelete, pattern: "/stats/requests", class: managementRouteClassM3},
	{method: http.MethodGet, pattern: "/stats/requests/{request_id}", class: managementRouteClassM3},
	{method: http.MethodGet, pattern: "/stats/summary", class: managementRouteClassM3},
	{method: http.MethodPost, pattern: "/stats/models/metrics", class: managementRouteClassM3},
	{method: http.MethodGet, pattern: "/stats/connection-success-rates", class: managementRouteClassM3},
	{method: http.MethodGet, pattern: "/stats/throughput", class: managementRouteClassM3},
	{method: http.MethodGet, pattern: "/stats/spending", class: managementRouteClassM3},
	{method: http.MethodGet, pattern: "/stats/usage-snapshot", class: managementRouteClassM3},
	{method: http.MethodGet, pattern: "/stats/endpoints/{endpoint_id}/models", class: managementRouteClassM3},
	{method: http.MethodDelete, pattern: "/stats/statistics", class: managementRouteClassM3},
	{method: http.MethodGet, pattern: "/audit/logs", class: managementRouteClassM3},
	{method: http.MethodGet, pattern: "/audit/logs/{log_id}", class: managementRouteClassM3},
	{method: http.MethodDelete, pattern: "/audit/logs", class: managementRouteClassM3},
	{method: http.MethodGet, pattern: "/loadbalance/current-state", class: managementRouteClassM3},
	{method: http.MethodGet, pattern: "/loadbalance/events", class: managementRouteClassM3},
	{method: http.MethodGet, pattern: "/loadbalance/events/{event_id}", class: managementRouteClassM3},
	{method: http.MethodDelete, pattern: "/loadbalance/events", class: managementRouteClassM3},
	{method: http.MethodGet, pattern: "/config/profile/export", class: managementRouteClassM3},
	{method: http.MethodPost, pattern: "/config/profile/import/preview", class: managementRouteClassM3},
	{method: http.MethodPost, pattern: "/config/profile/import", class: managementRouteClassM3},
	{method: http.MethodGet, pattern: "/config/vendors/export", class: managementRouteClassM3},
	{method: http.MethodPost, pattern: "/config/vendors/import/preview", class: managementRouteClassM3},
	{method: http.MethodPost, pattern: "/config/vendors/import", class: managementRouteClassM3},
	{method: http.MethodPost, pattern: "/models/connections/batch", class: managementRouteClassM3},
	{method: http.MethodPost, pattern: "/models/{model_config_id}/connections/health-check-preview", class: managementRouteClassM3},
	{method: http.MethodPost, pattern: "/connections/{connection_id}/health-check", class: managementRouteClassM3},
}

type managementAdmissionController struct {
	m2             *semaphore.Weighted
	m3             *semaphore.Weighted
	overloadStatus int
}

func newManagementAdmissionController(settings config.Settings) *managementAdmissionController {
	budget := settings.ManagementAdmissionBudget()
	return &managementAdmissionController{
		m2:             semaphore.NewWeighted(budget.M2MaxConcurrent),
		m3:             semaphore.NewWeighted(budget.M3MaxConcurrent),
		overloadStatus: http.StatusServiceUnavailable,
	}
}

func (c *managementAdmissionController) Middleware(next http.Handler) http.Handler {
	if c == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch classifyManagementRoute(r.Method, r.URL.Path) {
		case managementRouteClassBypass, managementRouteClassM1:
			next.ServeHTTP(w, r)
			return
		case managementRouteClassM2:
			if !c.m2.TryAcquire(1) {
				c.writeOverload(w)
				return
			}
			defer c.m2.Release(1)
		case managementRouteClassM3:
			if !c.m2.TryAcquire(1) {
				c.writeOverload(w)
				return
			}
			if !c.m3.TryAcquire(1) {
				c.m2.Release(1)
				c.writeOverload(w)
				return
			}
			defer c.m3.Release(1)
			defer c.m2.Release(1)
		}
		next.ServeHTTP(w, r)
	})
}

func (c *managementAdmissionController) writeOverload(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Retry-After", "1")
	w.WriteHeader(c.overloadStatus)
	_ = json.NewEncoder(w).Encode(map[string]string{"detail": "Management route temporarily overloaded. Retry later."})
}

func classifyManagementRoute(method string, rawPath string) managementRouteClass {
	normalizedMethod := strings.ToUpper(strings.TrimSpace(method))
	if normalizedMethod == http.MethodHead {
		normalizedMethod = http.MethodGet
	}
	if normalizedMethod == "" || normalizedMethod == http.MethodOptions {
		return managementRouteClassBypass
	}

	normalizedPath := normalizeManagementRoutePath(rawPath)
	if normalizedPath == "" || normalizedPath == "/" {
		return managementRouteClassBypass
	}

	for _, rule := range managementRouteRules {
		if rule.matches(normalizedMethod, normalizedPath) {
			return rule.class
		}
	}
	return managementRouteClassM2
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

func (r managementRouteRule) matches(method string, path string) bool {
	if r.method != method {
		return false
	}
	patternSegments := managementRouteSegments(normalizeManagementRoutePath(r.pattern))
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

func NewServer(settings config.Settings) (*http.Server, error) {
	loadedVersion, err := version.Load()
	if err != nil {
		return nil, err
	}

	deps := Dependencies{Version: loadedVersion}
	if settings.DocsEnabled() {
		deps.OpenAPI, err = openapi.Load()
		if err != nil {
			return nil, err
		}
	}

	shutdownFns := []func(){}
	registerShutdown := func(closeFn func()) {
		if closeFn != nil {
			shutdownFns = append(shutdownFns, closeFn)
		}
	}
	closeAll := func() {
		for i := len(shutdownFns) - 1; i >= 0; i-- {
			shutdownFns[i]()
		}
	}

	if strings.TrimSpace(settings.DatabaseURL) != "" {
		managementPool, poolErr := newDatabasePool(settings.DatabaseURL, settings.ManagementDatabaseBudget(), "management")
		if poolErr != nil {
			return nil, poolErr
		}
		registerShutdown(managementPool.Close)

		runtimePool, poolErr := newDatabasePool(settings.DatabaseURL, settings.RuntimeDatabaseBudget(), "runtime")
		if poolErr != nil {
			closeAll()
			return nil, poolErr
		}
		registerShutdown(runtimePool.Close)

		runtimePlanningCache := runtimeapi.NewSharedCache(0)
		runtimeAuthCache := managementauth.NewRuntimeCache(0)

		managementAuthService, authErr := managementauth.NewService(settings, managementauth.Options{Pool: managementPool})
		if authErr != nil {
			closeAll()
			return nil, authErr
		}
		registerShutdown(managementAuthService.Close)

		runtimeAuthService, authErr := managementauth.NewService(settings, managementauth.Options{Pool: runtimePool, RuntimeCache: runtimeAuthCache})
		if authErr != nil {
			closeAll()
			return nil, authErr
		}
		registerShutdown(runtimeAuthService.Close)

		profileService, profileErr := managementprofiles.NewService(settings, managementprofiles.Options{Pool: managementPool})
		if profileErr != nil {
			closeAll()
			return nil, profileErr
		}
		registerShutdown(profileService.Close)

		vendorService, vendorErr := managementvendors.NewService(settings, managementvendors.Options{Pool: managementPool})
		if vendorErr != nil {
			closeAll()
			return nil, vendorErr
		}
		registerShutdown(vendorService.Close)

		modelsService, modelsErr := managementmodels.NewService(settings, managementmodels.Options{Pool: managementPool})
		if modelsErr != nil {
			closeAll()
			return nil, modelsErr
		}
		registerShutdown(modelsService.Close)

		endpointsService, endpointsErr := managementendpoints.NewService(settings, managementendpoints.Options{Pool: managementPool})
		if endpointsErr != nil {
			closeAll()
			return nil, endpointsErr
		}
		registerShutdown(endpointsService.Close)

		connectionsService, connectionsErr := managementconnections.NewService(settings, managementconnections.Options{Pool: managementPool})
		if connectionsErr != nil {
			closeAll()
			return nil, connectionsErr
		}
		registerShutdown(connectionsService.Close)

		settingsService, settingsErr := managementsettings.NewService(settings, managementsettings.Options{Pool: managementPool})
		if settingsErr != nil {
			closeAll()
			return nil, settingsErr
		}
		registerShutdown(settingsService.Close)

		loadbalanceService, loadbalanceErr := managementloadbalance.NewService(settings, managementloadbalance.Options{Pool: managementPool})
		if loadbalanceErr != nil {
			closeAll()
			return nil, loadbalanceErr
		}
		registerShutdown(loadbalanceService.Close)

		auditService, auditErr := managementaudit.NewService(settings, managementaudit.Options{Pool: managementPool})
		if auditErr != nil {
			closeAll()
			return nil, auditErr
		}
		registerShutdown(auditService.Close)

		statsService, statsErr := managementstats.NewService(settings, managementstats.Options{Pool: managementPool})
		if statsErr != nil {
			closeAll()
			return nil, statsErr
		}
		registerShutdown(statsService.Close)

		configRulesService, configRulesErr := managementconfigrules.NewService(settings, managementconfigrules.Options{Pool: managementPool})
		if configRulesErr != nil {
			closeAll()
			return nil, configRulesErr
		}
		registerShutdown(configRulesService.Close)

		configBundleService, configBundleErr := managementconfigbundle.NewService(settings, managementconfigbundle.Options{Pool: managementPool})
		if configBundleErr != nil {
			closeAll()
			return nil, configBundleErr
		}
		registerShutdown(configBundleService.Close)

		// Realtime stays on the management pool in Phase 2.2 because it is mounted under
		// /api and currently serves dashboard management traffic rather than the protected
		// runtime hot path.
		realtimeService, realtimeErr := realtimeapi.NewService(settings, realtimeapi.Options{Pool: managementPool, AuthService: managementAuthService})
		if realtimeErr != nil {
			closeAll()
			return nil, realtimeErr
		}
		registerShutdown(realtimeService.Close)

		asyncDashboardPublisher := realtimeapi.NewAsyncDashboardPublisher(realtimeService, realtimeapi.AsyncDashboardPublisherOptions{})
		realtimeService.SetAsyncDashboardPublisher(asyncDashboardPublisher)
		registerShutdown(asyncDashboardPublisher.Close)

		runtimeService, runtimeErr := runtimeapi.NewService(settings, runtimeapi.Options{Pool: runtimePool, DashboardUpdates: asyncDashboardPublisher, Cache: runtimePlanningCache})
		if runtimeErr != nil {
			closeAll()
			return nil, runtimeErr
		}
		registerShutdown(runtimeService.Close)

		deps.AuditService = auditService
		deps.AuthService = managementAuthService
		deps.RuntimeAuthService = runtimeAuthService
		deps.ConfigBundleService = configBundleService
		deps.ConfigRulesService = configRulesService
		deps.ConnectionsService = connectionsService
		deps.EndpointsService = endpointsService
		deps.LoadbalanceService = loadbalanceService
		deps.ModelsService = modelsService
		deps.ProfilesService = profileService
		deps.RealtimeService = realtimeService
		deps.RuntimeService = runtimeService
		deps.RuntimeCache = runtimePlanningCache
		deps.SettingsService = settingsService
		deps.StatsService = statsService
		deps.VendorsService = vendorService
	}

	handler, err := NewHandlerWithDependencies(settings, deps)
	if err != nil {
		closeAll()
		return nil, err
	}

	server := &http.Server{
		Addr:              settings.Address(),
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}
	if len(shutdownFns) > 0 {
		server.RegisterOnShutdown(closeAll)
	}
	return server, nil
}

func newDatabasePool(databaseURL string, budget config.DatabasePoolBudget, lane string) (*pgxpool.Pool, error) {
	parsedConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse %s database pool config: %w", lane, err)
	}
	parsedConfig.MaxConns = budget.MaxConns
	parsedConfig.MinIdleConns = budget.MinIdleConns
	pool, err := pgxpool.NewWithConfig(context.Background(), parsedConfig)
	if err != nil {
		return nil, fmt.Errorf("create %s database pool: %w", lane, err)
	}
	return pool, nil
}

func NewHandler(settings config.Settings) (http.Handler, error) {
	loadedVersion, err := version.Load()
	if err != nil {
		return nil, err
	}

	deps := Dependencies{Version: loadedVersion}
	if settings.DocsEnabled() {
		deps.OpenAPI, err = openapi.Load()
		if err != nil {
			return nil, err
		}
	}

	return NewHandlerWithDependencies(settings, deps)
}

func NewHandlerWithDependencies(settings config.Settings, deps Dependencies) (http.Handler, error) {
	if deps.Version == "" {
		return nil, fmt.Errorf("version is required")
	}
	if settings.DocsEnabled() && deps.OpenAPI == nil {
		return nil, fmt.Errorf("openapi document is required when docs are enabled")
	}

	router := chi.NewRouter()
	allowedOrigins := map[string]struct{}{}
	for _, origin := range settings.CORSAllowedOriginsList() {
		allowedOrigins[origin] = struct{}{}
	}

	router.Use(middleware.RequestID)
	router.Use(middleware.RealIP)
	router.Use(middleware.Recoverer)
	router.Use(corsMiddleware(allowedOrigins))

	router.Group(func(management chi.Router) {
		mountManagementBranch(management, settings, deps)
	})
	router.Group(func(runtime chi.Router) {
		mountRuntimeBranch(runtime, deps)
	})

	return router, nil
}

func mountManagementBranch(router chi.Router, settings config.Settings, deps Dependencies) {
	router.Get("/health", healthHandler(deps.Version))

	managementHandler := NewManagementRouter(deps.AuditService, deps.AuthService, deps.ConfigBundleService, deps.ConfigRulesService, deps.ConnectionsService, deps.EndpointsService, deps.LoadbalanceService, deps.ModelsService, deps.ProfilesService, deps.RealtimeService, deps.SettingsService, deps.StatsService, deps.VendorsService)
	if deps.AuthService != nil {
		managementHandler = deps.AuthService.ManagementMiddleware(managementHandler)
	}
	managementHandler = newManagementAdmissionController(settings).Middleware(managementHandler)
	managementHandler = newRuntimeCacheInvalidationMiddleware(deps.RuntimeCache, deps.RuntimeAuthService).Middleware(managementHandler)
	router.Mount("/api", managementHandler)

	if settings.DocsEnabled() {
		router.Get("/openapi.json", deps.OpenAPI.ServeJSON)
		router.Get("/docs", deps.OpenAPI.ServeDocs)
		router.Get("/redoc", deps.OpenAPI.ServeRedoc)
	}
}

func mountRuntimeBranch(router chi.Router, deps Dependencies) {
	runtimeAuthService := deps.RuntimeAuthService
	if runtimeAuthService == nil {
		runtimeAuthService = deps.AuthService
	}

	if deps.RuntimeService != nil {
		runtimeHandler := deps.RuntimeService.Handler()
		if runtimeAuthService != nil {
			runtimeHandler = runtimeAuthService.RuntimeMiddleware(runtimeHandler)
		}
		router.Handle("/v1", runtimeHandler)
		router.Handle("/v1/*", runtimeHandler)
		router.Handle("/v1beta", runtimeHandler)
		router.Handle("/v1beta/*", runtimeHandler)
		return
	}
	if runtimeAuthService != nil {
		router.Mount("/v1", runtimeAuthService.RuntimeMiddleware(runtimeAuthService.RuntimeProbeRouter()))
		router.Mount("/v1beta", runtimeAuthService.RuntimeMiddleware(runtimeAuthService.RuntimeProbeRouter()))
	}
}

func NewManagementRouter(auditService *managementaudit.Service, authService *managementauth.Service, configBundleService *managementconfigbundle.Service, configRulesService *managementconfigrules.Service, connectionsService *managementconnections.Service, endpointsService *managementendpoints.Service, loadbalanceService *managementloadbalance.Service, modelsService *managementmodels.Service, profilesService *managementprofiles.Service, realtimeService *realtimeapi.Service, settingsService *managementsettings.Service, statsService *managementstats.Service, vendorsService *managementvendors.Service) http.Handler {
	router := chi.NewRouter()
	if auditService != nil {
		auditService.MountManagementRoutes(router)
	}
	if authService != nil {
		authService.MountManagementRoutes(router)
	}
	if configBundleService != nil {
		configBundleService.MountManagementRoutes(router)
	}
	if configRulesService != nil {
		configRulesService.MountManagementRoutes(router)
	}
	if connectionsService != nil {
		connectionsService.MountManagementRoutes(router)
	}
	if endpointsService != nil {
		endpointsService.MountManagementRoutes(router)
	}
	if loadbalanceService != nil {
		loadbalanceService.MountManagementRoutes(router)
	}
	if modelsService != nil {
		modelsService.MountManagementRoutes(router)
	}
	if profilesService != nil {
		profilesService.MountManagementRoutes(router)
	}
	if realtimeService != nil {
		realtimeService.MountManagementRoutes(router)
	}
	if settingsService != nil {
		settingsService.MountManagementRoutes(router)
	}
	if statsService != nil {
		statsService.MountManagementRoutes(router)
	}
	if vendorsService != nil {
		vendorsService.MountManagementRoutes(router)
	}
	return router
}

func corsMiddleware(allowedOrigins map[string]struct{}) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := strings.TrimSpace(r.Header.Get("Origin"))
			if origin != "" {
				if _, ok := allowedOrigins[origin]; ok {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Set("Access-Control-Allow-Credentials", "true")
					w.Header().Set("Vary", "Origin")

					if r.Method == http.MethodOptions && strings.TrimSpace(r.Header.Get("Access-Control-Request-Method")) != "" {
						requestHeaders := strings.TrimSpace(r.Header.Get("Access-Control-Request-Headers"))
						w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,PATCH,DELETE,OPTIONS")
						if requestHeaders != "" {
							w.Header().Set("Access-Control-Allow-Headers", requestHeaders)
						}
						w.WriteHeader(http.StatusNoContent)
						return
					}
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

func healthHandler(version string) http.HandlerFunc {
	response := healthResponse{Status: "ok", Version: version}
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(response)
	}
}
