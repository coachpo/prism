package platformhttp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	loadbalancedomain "github.com/coachpo/prism/backend/internal/domain/loadbalance"
	managementaudit "github.com/coachpo/prism/backend/internal/httpapi/management/audit"
	managementauth "github.com/coachpo/prism/backend/internal/httpapi/management/auth"
	managementbootstrapconfig "github.com/coachpo/prism/backend/internal/httpapi/management/bootstrapconfig"
	managementconfigbundle "github.com/coachpo/prism/backend/internal/httpapi/management/configbundle"
	managementconfigrules "github.com/coachpo/prism/backend/internal/httpapi/management/configrules"
	managementconnections "github.com/coachpo/prism/backend/internal/httpapi/management/connections"
	managementendpoints "github.com/coachpo/prism/backend/internal/httpapi/management/endpoints"
	managementloadbalance "github.com/coachpo/prism/backend/internal/httpapi/management/loadbalance"
	managementmodels "github.com/coachpo/prism/backend/internal/httpapi/management/models"
	managementprofiles "github.com/coachpo/prism/backend/internal/httpapi/management/profiles"
	managementsettings "github.com/coachpo/prism/backend/internal/httpapi/management/settings"
	managementsidecars "github.com/coachpo/prism/backend/internal/httpapi/management/sidecars"
	managementstats "github.com/coachpo/prism/backend/internal/httpapi/management/stats"
	managementvendors "github.com/coachpo/prism/backend/internal/httpapi/management/vendors"
	"github.com/coachpo/prism/backend/internal/httpapi/openapi"
	realtimeapi "github.com/coachpo/prism/backend/internal/httpapi/realtime"
	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
	"github.com/coachpo/prism/backend/internal/platform/admission"
	"github.com/coachpo/prism/backend/internal/platform/config"
	platformcors "github.com/coachpo/prism/backend/internal/platform/cors"
	platformdb "github.com/coachpo/prism/backend/internal/platform/db"
	"github.com/coachpo/prism/backend/internal/platform/version"
)

type Dependencies struct {
	Version                   string
	OpenAPI                   *openapi.Document
	HotBootstrapConfigRuntime *HotBootstrapConfigRuntime
	CORSOriginProvider        platformcors.OriginProvider
	AuditService              *managementaudit.Service
	AuthService               *managementauth.Service
	RuntimeAuthService        *managementauth.Service
	BootstrapConfigService    *managementbootstrapconfig.Service
	ConfigBundleService       *managementconfigbundle.Service
	ConfigRulesService        *managementconfigrules.Service
	ConnectionsService        *managementconnections.Service
	EndpointsService          *managementendpoints.Service
	LoadbalanceService        *managementloadbalance.Service
	ModelsService             *managementmodels.Service
	ProfilesService           *managementprofiles.Service
	RealtimeService           *realtimeapi.Service
	RuntimeService            *runtimeapi.Service
	RuntimeCache              *runtimeapi.SharedCache
	RuntimeState              *loadbalancedomain.LocalRuntimeStateStore
	DatabasePools             *platformdb.DatabasePools
	SettingsService           *managementsettings.Service
	SidecarsService           *managementsidecars.Service
	StatsService              *managementstats.Service
	VendorsService            *managementvendors.Service
}

type ServerOptions struct {
	BootstrapConfig BootstrapConfigOptions
	Dependencies    Dependencies
}

type BootstrapConfigOptions struct {
	ConfigPath         string
	LoadedRevision     int
	LoadedDocumentETag string
}

type healthResponse struct {
	Status    string `json:"status"`
	Version   string `json:"version"`
	Liveness  string `json:"liveness"`
	Readiness string `json:"readiness"`
	Startup   string `json:"startup"`
}

func NewServer(settings config.Settings, options ServerOptions) (*http.Server, error) {
	deps, err := completeDependencies(settings, options)
	if err != nil {
		return nil, err
	}
	handler, err := NewHandlerWithDependencies(settings, deps)
	if err != nil {
		return nil, err
	}
	return &http.Server{
		Addr:              settings.Address(),
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}, nil
}

func completeDependencies(settings config.Settings, options ServerOptions) (Dependencies, error) {
	deps := options.Dependencies
	var err error
	if deps.Version == "" {
		deps.Version, err = version.Load()
		if err != nil {
			return Dependencies{}, err
		}
	}
	if deps.HotBootstrapConfigRuntime == nil {
		deps.HotBootstrapConfigRuntime, err = NewHotBootstrapConfigRuntime(settings)
		if err != nil {
			return Dependencies{}, err
		}
	}
	if deps.CORSOriginProvider == nil && deps.HotBootstrapConfigRuntime != nil {
		deps.CORSOriginProvider = deps.HotBootstrapConfigRuntime
	}
	if deps.BootstrapConfigService == nil && strings.TrimSpace(options.BootstrapConfig.ConfigPath) != "" {
		bootstrapConfigService, bootstrapErr := managementbootstrapconfig.NewService(settings, managementbootstrapconfig.Options{
			ConfigPath:         options.BootstrapConfig.ConfigPath,
			LoadedRevision:     options.BootstrapConfig.LoadedRevision,
			LoadedDocumentETag: options.BootstrapConfig.LoadedDocumentETag,
			CORSOriginProvider: deps.CORSOriginProvider,
			HotApplyRuntime:    deps.HotBootstrapConfigRuntime,
		})
		if bootstrapErr != nil {
			return Dependencies{}, bootstrapErr
		}
		deps.BootstrapConfigService = bootstrapConfigService
	}
	if settings.DocsEnabled() && deps.OpenAPI == nil {
		deps.OpenAPI, err = openapi.Load()
		if err != nil {
			return Dependencies{}, err
		}
	}
	return deps, nil
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
	if deps.RuntimeState == nil && deps.RuntimeService != nil {
		deps.RuntimeState = deps.RuntimeService.RuntimeState()
	}
	if deps.CORSOriginProvider == nil && deps.HotBootstrapConfigRuntime != nil {
		deps.CORSOriginProvider = deps.HotBootstrapConfigRuntime
	}
	if deps.CORSOriginProvider == nil {
		deps.CORSOriginProvider = platformcors.NewStaticOriginProvider(settings.CORSAllowedOriginsList())
	}

	router := chi.NewRouter()

	router.Use(middleware.RequestID)
	router.Use(middleware.RealIP)
	router.Use(middleware.Recoverer)
	router.Use(corsMiddleware(deps.CORSOriginProvider))

	admissionController := newHTTPAdmissionController(settings)
	admissionProvider := deps.HotBootstrapConfigRuntime
	router.Group(func(management chi.Router) {
		mountManagementBranch(management, settings, deps, admissionController, admissionProvider)
	})
	router.Group(func(runtime chi.Router) {
		mountRuntimeBranch(runtime, settings, deps, admissionController, admissionProvider)
	})

	return router, nil
}

func mountManagementBranch(router chi.Router, settings config.Settings, deps Dependencies, admissionController *admission.Controller, admissionProvider hotAdmissionProvider) {
	router.Get("/health", healthHandler(deps.Version))
	if deps.DatabasePools != nil {
		router.Get("/metrics", platformdb.MetricsHandler(deps.DatabasePools))
	}

	managementHandler := NewManagementRouter(deps.AuditService, deps.AuthService, deps.BootstrapConfigService, deps.ConfigBundleService, deps.ConfigRulesService, deps.ConnectionsService, deps.EndpointsService, deps.LoadbalanceService, deps.ModelsService, deps.ProfilesService, deps.RealtimeService, deps.SettingsService, deps.SidecarsService, deps.StatsService, deps.VendorsService)
	if deps.AuthService != nil {
		managementHandler = deps.AuthService.ManagementMiddleware(managementHandler)
	}
	managementHandler = (&managementAdmissionController{controller: admissionController, provider: admissionProvider}).Middleware(managementHandler)
	managementHandler = newRuntimeCacheInvalidationMiddleware(deps.RuntimeCache, deps.RuntimeAuthService, deps.RuntimeState).Middleware(managementHandler)
	router.Mount("/api", managementHandler)

	if settings.DocsEnabled() {
		router.Get("/openapi.json", deps.OpenAPI.ServeJSON)
		router.Get("/docs", deps.OpenAPI.ServeDocs)
		router.Get("/redoc", deps.OpenAPI.ServeRedoc)
	}
}

func mountRuntimeBranch(router chi.Router, settings config.Settings, deps Dependencies, admissionController *admission.Controller, admissionProvider hotAdmissionProvider) {
	runtimeAuthService := deps.RuntimeAuthService
	if runtimeAuthService == nil {
		runtimeAuthService = deps.AuthService
	}

	if deps.RuntimeService != nil {
		runtimeHandler := deps.RuntimeService.Handler()
		if runtimeAuthService != nil {
			runtimeHandler = runtimeAuthService.RuntimeMiddleware(runtimeHandler)
		}
		runtimeHandler = proxyAdmissionProviderMiddleware(admissionProvider, admissionController, settings.RuntimeTransport().RequestTimeout, runtimeHandler)
		router.Handle("/v1", runtimeHandler)
		router.Handle("/v1/*", runtimeHandler)
		router.Handle("/v1beta", runtimeHandler)
		router.Handle("/v1beta/*", runtimeHandler)
		return
	}
	if runtimeAuthService != nil {
		runtimeProbeHandler := proxyAdmissionProviderMiddleware(admissionProvider, admissionController, settings.RuntimeTransport().RequestTimeout, runtimeAuthService.RuntimeMiddleware(runtimeAuthService.RuntimeProbeRouter()))
		router.Mount("/v1", runtimeProbeHandler)
		router.Mount("/v1beta", runtimeProbeHandler)
	}
}

func NewManagementRouter(auditService *managementaudit.Service, authService *managementauth.Service, bootstrapConfigService *managementbootstrapconfig.Service, configBundleService *managementconfigbundle.Service, configRulesService *managementconfigrules.Service, connectionsService *managementconnections.Service, endpointsService *managementendpoints.Service, loadbalanceService *managementloadbalance.Service, modelsService *managementmodels.Service, profilesService *managementprofiles.Service, realtimeService *realtimeapi.Service, settingsService *managementsettings.Service, sidecarsService *managementsidecars.Service, statsService *managementstats.Service, vendorsService *managementvendors.Service) http.Handler {
	router := chi.NewRouter()
	if auditService != nil {
		auditService.MountManagementRoutes(router)
	}
	if authService != nil {
		authService.MountManagementRoutes(router)
	}
	if bootstrapConfigService != nil {
		bootstrapConfigService.MountManagementRoutes(router)
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
	if sidecarsService != nil {
		sidecarsService.MountManagementRoutes(router)
	}
	if statsService != nil {
		statsService.MountManagementRoutes(router)
	}
	if vendorsService != nil {
		vendorsService.MountManagementRoutes(router)
	}
	return router
}

func corsMiddleware(originProvider platformcors.OriginProvider) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			corsSnapshot := originProvider.CORSSnapshot()
			if platformcors.ApplyAllowOriginHeaders(w, r, corsSnapshot) {
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

			next.ServeHTTP(w, r)
		})
	}
}

func healthHandler(version string) http.HandlerFunc {
	response := healthResponse{
		Status:    "ok",
		Version:   version,
		Liveness:  "ok",
		Readiness: "ready",
		Startup:   "complete",
	}
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(response)
	}
}
