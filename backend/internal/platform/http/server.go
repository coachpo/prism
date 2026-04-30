package platformhttp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	loadbalancedomain "github.com/coachpo/prism/backend/internal/domain/loadbalance"
	statsdomain "github.com/coachpo/prism/backend/internal/domain/stats"
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
	managementstats "github.com/coachpo/prism/backend/internal/httpapi/management/stats"
	managementvendors "github.com/coachpo/prism/backend/internal/httpapi/management/vendors"
	"github.com/coachpo/prism/backend/internal/httpapi/openapi"
	realtimeapi "github.com/coachpo/prism/backend/internal/httpapi/realtime"
	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
	"github.com/coachpo/prism/backend/internal/platform/admission"
	"github.com/coachpo/prism/backend/internal/platform/background"
	"github.com/coachpo/prism/backend/internal/platform/config"
	platformdb "github.com/coachpo/prism/backend/internal/platform/db"
	"github.com/coachpo/prism/backend/internal/platform/email"
	"github.com/coachpo/prism/backend/internal/platform/email/outbox"
	"github.com/coachpo/prism/backend/internal/platform/managementjobs"
	"github.com/coachpo/prism/backend/internal/platform/managementsideeffects"
	"github.com/coachpo/prism/backend/internal/platform/version"
)

type Dependencies struct {
	Version                string
	OpenAPI                *openapi.Document
	AuditService           *managementaudit.Service
	AuthService            *managementauth.Service
	RuntimeAuthService     *managementauth.Service
	BootstrapConfigService *managementbootstrapconfig.Service
	ConfigBundleService    *managementconfigbundle.Service
	ConfigRulesService     *managementconfigrules.Service
	ConnectionsService     *managementconnections.Service
	EndpointsService       *managementendpoints.Service
	LoadbalanceService     *managementloadbalance.Service
	ModelsService          *managementmodels.Service
	ProfilesService        *managementprofiles.Service
	RealtimeService        *realtimeapi.Service
	RuntimeService         *runtimeapi.Service
	RuntimeCache           *runtimeapi.SharedCache
	RuntimeState           *loadbalancedomain.LocalRuntimeStateStore
	DatabasePools          *platformdb.DatabasePools
	SettingsService        *managementsettings.Service
	StatsService           *managementstats.Service
	VendorsService         *managementvendors.Service
}

type ServerOptions struct {
	BootstrapConfig BootstrapConfigOptions
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
	loadedVersion, err := version.Load()
	if err != nil {
		return nil, err
	}

	deps := Dependencies{Version: loadedVersion}
	if strings.TrimSpace(options.BootstrapConfig.ConfigPath) != "" {
		bootstrapConfigService, bootstrapErr := managementbootstrapconfig.NewService(settings, managementbootstrapconfig.Options{
			ConfigPath:         options.BootstrapConfig.ConfigPath,
			LoadedRevision:     options.BootstrapConfig.LoadedRevision,
			LoadedDocumentETag: options.BootstrapConfig.LoadedDocumentETag,
		})
		if bootstrapErr != nil {
			return nil, bootstrapErr
		}
		deps.BootstrapConfigService = bootstrapConfigService
	}
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
		databasePools, poolErr := platformdb.OpenDatabasePools(context.Background(), settings.DatabaseURL, settings.PostgresPoolsBudgetOrDefault())
		if poolErr != nil {
			return nil, poolErr
		}
		registerShutdown(databasePools.Close)
		deps.DatabasePools = databasePools

		managementPool := databasePools.Management.Raw()
		runtimeExecutionPool := databasePools.RuntimeExecution.Raw()
		runtimeTelemetryPool := databasePools.RuntimeTelemetry.Raw()
		runtimeFeedbackPool := databasePools.RuntimeFeedback.Raw()
		realtimePool := databasePools.Realtime.Raw()
		cacheRefreshPool := databasePools.CacheRefresh.Raw()
		backgroundJobsPool := databasePools.BackgroundJobs.Raw()
		backgroundScheduler := background.NewScheduler(background.Config{})
		managementSideEffects := managementsideeffects.NewDispatcher(managementsideeffects.Options{Pool: backgroundJobsPool, Scheduler: backgroundScheduler})
		managementJobs := managementjobs.NewStore(managementjobs.Options{Pool: backgroundJobsPool, Scheduler: backgroundScheduler})

		runtimePlanningCache := runtimeapi.NewSharedCacheWithOptions(runtimeapi.SharedCacheOptions{RefreshPool: cacheRefreshPool, SecretEncryptionKey: settings.SecretEncryptionKey, Scheduler: backgroundScheduler})
		runtimeState := loadbalancedomain.NewLocalRuntimeStateStore()
		runtimeAuthCache := managementauth.NewRuntimeCacheFromShared(runtimePlanningCache)
		if err := runtimePlanningCache.Bootstrap(context.Background()); err != nil {
			closeAll()
			return nil, err
		}

		authMailer, mailerErr := newAuthMailer(settings)
		if mailerErr != nil {
			closeAll()
			return nil, mailerErr
		}
		emailOutbox := outbox.NewStore(outbox.Options{Pool: backgroundJobsPool, Mailer: authMailer, SecretEncryptionKey: settings.SecretEncryptionKey, Scheduler: backgroundScheduler})

		managementAuthService, authErr := managementauth.NewService(settings, managementauth.Options{Pool: managementPool, ProxyKeyUsagePool: backgroundJobsPool, EmailOutbox: emailOutbox, Scheduler: backgroundScheduler})
		if authErr != nil {
			closeAll()
			return nil, authErr
		}
		registerShutdown(managementAuthService.Close)

		runtimeAuthService, authErr := managementauth.NewService(settings, managementauth.Options{Pool: runtimeExecutionPool, RuntimeCache: runtimeAuthCache})
		if authErr != nil {
			closeAll()
			return nil, authErr
		}
		registerShutdown(runtimeAuthService.Close)

		dashboardSnapshots := statsdomain.NewDashboardAggregateStore()

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

		loadbalanceService, loadbalanceErr := managementloadbalance.NewService(settings, managementloadbalance.Options{Pool: managementPool, RuntimeState: runtimeState})
		if loadbalanceErr != nil {
			closeAll()
			return nil, loadbalanceErr
		}
		registerShutdown(loadbalanceService.Close)

		auditService, auditErr := managementaudit.NewService(settings, managementaudit.Options{Pool: managementPool, Jobs: managementJobs})
		if auditErr != nil {
			closeAll()
			return nil, auditErr
		}
		registerShutdown(auditService.Close)

		statsService, statsErr := managementstats.NewService(settings, managementstats.Options{Pool: managementPool, DashboardSnapshots: dashboardSnapshots, SideEffects: managementSideEffects})
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

		realtimeService, realtimeErr := realtimeapi.NewService(settings, realtimeapi.Options{RealtimePool: realtimePool, AuthService: managementAuthService, DashboardSnapshots: dashboardSnapshots})
		if realtimeErr != nil {
			closeAll()
			return nil, realtimeErr
		}
		registerShutdown(realtimeService.Close)

		asyncDashboardPublisher := realtimeapi.NewAsyncDashboardPublisher(realtimeService, realtimeapi.AsyncDashboardPublisherOptions{Scheduler: backgroundScheduler})
		realtimeService.SetAsyncDashboardPublisher(asyncDashboardPublisher)
		registerShutdown(asyncDashboardPublisher.Close)

		runtimeService, runtimeErr := runtimeapi.NewService(settings, runtimeapi.Options{ExecutionPool: runtimeExecutionPool, TelemetryPool: runtimeTelemetryPool, FeedbackPool: runtimeFeedbackPool, DashboardUpdates: asyncDashboardPublisher, Cache: runtimePlanningCache, RuntimeState: runtimeState, Scheduler: backgroundScheduler})
		if runtimeErr != nil {
			closeAll()
			return nil, runtimeErr
		}
		registerShutdown(runtimeService.Close)

		for _, register := range []func(*background.Scheduler) error{
			runtimePlanningCache.RegisterBackgroundWorker,
			managementAuthService.RegisterBackgroundWorkers,
			emailOutbox.RegisterBackgroundWorker,
			managementJobs.RegisterBackgroundWorker,
			managementSideEffects.RegisterBackgroundWorker,
			asyncDashboardPublisher.RegisterBackgroundWorker,
			runtimeService.RegisterBackgroundWorkers,
		} {
			if err := register(backgroundScheduler); err != nil {
				closeAll()
				return nil, err
			}
		}
		if err := backgroundScheduler.Start(context.Background()); err != nil {
			closeAll()
			return nil, err
		}
		registerShutdown(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = backgroundScheduler.Stop(ctx, time.Now().Add(5*time.Second))
		})

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
		deps.RuntimeState = runtimeState
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

func newAuthMailer(settings config.Settings) (managementauth.Mailer, error) {
	mailer, _, err := email.NewMailer(settings.Mail)
	if err != nil {
		return nil, fmt.Errorf("create auth mailer: %w", err)
	}
	return mailer, nil
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

	router := chi.NewRouter()
	allowedOrigins := map[string]struct{}{}
	for _, origin := range settings.CORSAllowedOriginsList() {
		allowedOrigins[origin] = struct{}{}
	}

	router.Use(middleware.RequestID)
	router.Use(middleware.RealIP)
	router.Use(middleware.Recoverer)
	router.Use(corsMiddleware(allowedOrigins))

	admissionController := newHTTPAdmissionController(settings)
	router.Group(func(management chi.Router) {
		mountManagementBranch(management, settings, deps, admissionController)
	})
	router.Group(func(runtime chi.Router) {
		mountRuntimeBranch(runtime, settings, deps, admissionController)
	})

	return router, nil
}

func mountManagementBranch(router chi.Router, settings config.Settings, deps Dependencies, admissionController *admission.Controller) {
	router.Get("/health", healthHandler(deps.Version))
	if deps.DatabasePools != nil {
		router.Get("/metrics", platformdb.MetricsHandler(deps.DatabasePools))
	}

	managementHandler := NewManagementRouter(deps.AuditService, deps.AuthService, deps.BootstrapConfigService, deps.ConfigBundleService, deps.ConfigRulesService, deps.ConnectionsService, deps.EndpointsService, deps.LoadbalanceService, deps.ModelsService, deps.ProfilesService, deps.RealtimeService, deps.SettingsService, deps.StatsService, deps.VendorsService)
	if deps.AuthService != nil {
		managementHandler = deps.AuthService.ManagementMiddleware(managementHandler)
	}
	managementHandler = (&managementAdmissionController{controller: admissionController}).Middleware(managementHandler)
	managementHandler = newRuntimeCacheInvalidationMiddleware(deps.RuntimeCache, deps.RuntimeAuthService, deps.RuntimeState).Middleware(managementHandler)
	router.Mount("/api", managementHandler)

	if settings.DocsEnabled() {
		router.Get("/openapi.json", deps.OpenAPI.ServeJSON)
		router.Get("/docs", deps.OpenAPI.ServeDocs)
		router.Get("/redoc", deps.OpenAPI.ServeRedoc)
	}
}

func mountRuntimeBranch(router chi.Router, settings config.Settings, deps Dependencies, admissionController *admission.Controller) {
	runtimeAuthService := deps.RuntimeAuthService
	if runtimeAuthService == nil {
		runtimeAuthService = deps.AuthService
	}

	if deps.RuntimeService != nil {
		runtimeHandler := deps.RuntimeService.Handler()
		if runtimeAuthService != nil {
			runtimeHandler = runtimeAuthService.RuntimeMiddleware(runtimeHandler)
		}
		runtimeHandler = proxyAdmissionMiddleware(admissionController, settings.RuntimeTransport().RequestTimeout, runtimeHandler)
		router.Handle("/v1", runtimeHandler)
		router.Handle("/v1/*", runtimeHandler)
		router.Handle("/v1beta", runtimeHandler)
		router.Handle("/v1beta/*", runtimeHandler)
		return
	}
	if runtimeAuthService != nil {
		runtimeProbeHandler := proxyAdmissionMiddleware(admissionController, settings.RuntimeTransport().RequestTimeout, runtimeAuthService.RuntimeMiddleware(runtimeAuthService.RuntimeProbeRouter()))
		router.Mount("/v1", runtimeProbeHandler)
		router.Mount("/v1beta", runtimeProbeHandler)
	}
}

func NewManagementRouter(auditService *managementaudit.Service, authService *managementauth.Service, bootstrapConfigService *managementbootstrapconfig.Service, configBundleService *managementconfigbundle.Service, configRulesService *managementconfigrules.Service, connectionsService *managementconnections.Service, endpointsService *managementendpoints.Service, loadbalanceService *managementloadbalance.Service, modelsService *managementmodels.Service, profilesService *managementprofiles.Service, realtimeService *realtimeapi.Service, settingsService *managementsettings.Service, statsService *managementstats.Service, vendorsService *managementvendors.Service) http.Handler {
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
