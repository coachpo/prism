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
	"github.com/jackc/pgx/v5/pgxpool"

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
	ConfigBundleService *managementconfigbundle.Service
	ConfigRulesService  *managementconfigrules.Service
	ConnectionsService  *managementconnections.Service
	EndpointsService    *managementendpoints.Service
	LoadbalanceService  *managementloadbalance.Service
	ModelsService       *managementmodels.Service
	ProfilesService     *managementprofiles.Service
	RealtimeService     *realtimeapi.Service
	RuntimeService      *runtimeapi.Service
	SettingsService     *managementsettings.Service
	StatsService        *managementstats.Service
	VendorsService      *managementvendors.Service
}

type healthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
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

	var sharedPool *pgxpool.Pool
	if strings.TrimSpace(settings.DatabaseURL) != "" {
		sharedPool, err = pgxpool.New(context.Background(), settings.DatabaseURL)
		if err != nil {
			return nil, fmt.Errorf("create shared database pool: %w", err)
		}

		authService, authErr := managementauth.NewService(settings, managementauth.Options{Pool: sharedPool})
		if authErr != nil {
			sharedPool.Close()
			return nil, authErr
		}
		profileService, profileErr := managementprofiles.NewService(settings, managementprofiles.Options{Pool: sharedPool})
		if profileErr != nil {
			authService.Close()
			sharedPool.Close()
			return nil, profileErr
		}
		vendorService, vendorErr := managementvendors.NewService(settings, managementvendors.Options{Pool: sharedPool})
		if vendorErr != nil {
			profileService.Close()
			authService.Close()
			sharedPool.Close()
			return nil, vendorErr
		}
		modelsService, modelsErr := managementmodels.NewService(settings, managementmodels.Options{Pool: sharedPool})
		if modelsErr != nil {
			vendorService.Close()
			profileService.Close()
			authService.Close()
			sharedPool.Close()
			return nil, modelsErr
		}
		endpointsService, endpointsErr := managementendpoints.NewService(settings, managementendpoints.Options{Pool: sharedPool})
		if endpointsErr != nil {
			modelsService.Close()
			vendorService.Close()
			profileService.Close()
			authService.Close()
			sharedPool.Close()
			return nil, endpointsErr
		}
		connectionsService, connectionsErr := managementconnections.NewService(settings, managementconnections.Options{Pool: sharedPool})
		if connectionsErr != nil {
			endpointsService.Close()
			modelsService.Close()
			vendorService.Close()
			profileService.Close()
			authService.Close()
			sharedPool.Close()
			return nil, connectionsErr
		}
		settingsService, settingsErr := managementsettings.NewService(settings, managementsettings.Options{Pool: sharedPool})
		if settingsErr != nil {
			connectionsService.Close()
			endpointsService.Close()
			modelsService.Close()
			vendorService.Close()
			profileService.Close()
			authService.Close()
			sharedPool.Close()
			return nil, settingsErr
		}
		loadbalanceService, loadbalanceErr := managementloadbalance.NewService(settings, managementloadbalance.Options{Pool: sharedPool})
		if loadbalanceErr != nil {
			settingsService.Close()
			connectionsService.Close()
			endpointsService.Close()
			modelsService.Close()
			vendorService.Close()
			profileService.Close()
			authService.Close()
			sharedPool.Close()
			return nil, loadbalanceErr
		}
		auditService, auditErr := managementaudit.NewService(settings, managementaudit.Options{Pool: sharedPool})
		if auditErr != nil {
			loadbalanceService.Close()
			settingsService.Close()
			connectionsService.Close()
			endpointsService.Close()
			modelsService.Close()
			vendorService.Close()
			profileService.Close()
			authService.Close()
			sharedPool.Close()
			return nil, auditErr
		}
		statsService, statsErr := managementstats.NewService(settings, managementstats.Options{Pool: sharedPool})
		if statsErr != nil {
			auditService.Close()
			loadbalanceService.Close()
			settingsService.Close()
			connectionsService.Close()
			endpointsService.Close()
			modelsService.Close()
			vendorService.Close()
			profileService.Close()
			authService.Close()
			sharedPool.Close()
			return nil, statsErr
		}
		configRulesService, configRulesErr := managementconfigrules.NewService(settings, managementconfigrules.Options{Pool: sharedPool})
		if configRulesErr != nil {
			statsService.Close()
			auditService.Close()
			loadbalanceService.Close()
			settingsService.Close()
			connectionsService.Close()
			endpointsService.Close()
			modelsService.Close()
			vendorService.Close()
			profileService.Close()
			authService.Close()
			sharedPool.Close()
			return nil, configRulesErr
		}
		configBundleService, configBundleErr := managementconfigbundle.NewService(settings, managementconfigbundle.Options{Pool: sharedPool})
		if configBundleErr != nil {
			configRulesService.Close()
			statsService.Close()
			auditService.Close()
			loadbalanceService.Close()
			settingsService.Close()
			connectionsService.Close()
			endpointsService.Close()
			modelsService.Close()
			vendorService.Close()
			profileService.Close()
			authService.Close()
			sharedPool.Close()
			return nil, configBundleErr
		}
		realtimeService, realtimeErr := realtimeapi.NewService(settings, realtimeapi.Options{Pool: sharedPool, AuthService: authService})
		if realtimeErr != nil {
			configBundleService.Close()
			configRulesService.Close()
			statsService.Close()
			auditService.Close()
			loadbalanceService.Close()
			settingsService.Close()
			connectionsService.Close()
			endpointsService.Close()
			modelsService.Close()
			vendorService.Close()
			profileService.Close()
			authService.Close()
			sharedPool.Close()
			return nil, realtimeErr
		}
		runtimeService, runtimeErr := runtimeapi.NewService(settings, runtimeapi.Options{Pool: sharedPool, DashboardUpdates: realtimeService})
		if runtimeErr != nil {
			realtimeService.Close()
			configBundleService.Close()
			configRulesService.Close()
			statsService.Close()
			auditService.Close()
			loadbalanceService.Close()
			settingsService.Close()
			connectionsService.Close()
			endpointsService.Close()
			modelsService.Close()
			vendorService.Close()
			profileService.Close()
			authService.Close()
			sharedPool.Close()
			return nil, runtimeErr
		}
		deps.AuditService = auditService
		deps.AuthService = authService
		deps.ConfigBundleService = configBundleService
		deps.ConfigRulesService = configRulesService
		deps.ConnectionsService = connectionsService
		deps.EndpointsService = endpointsService
		deps.LoadbalanceService = loadbalanceService
		deps.ModelsService = modelsService
		deps.ProfilesService = profileService
		deps.RealtimeService = realtimeService
		deps.RuntimeService = runtimeService
		deps.SettingsService = settingsService
		deps.StatsService = statsService
		deps.VendorsService = vendorService
	}

	handler, err := NewHandlerWithDependencies(settings, deps)
	if err != nil {
		if deps.AuditService != nil {
			deps.AuditService.Close()
		}
		if deps.AuthService != nil {
			deps.AuthService.Close()
		}
		if deps.ConfigBundleService != nil {
			deps.ConfigBundleService.Close()
		}
		if deps.ConnectionsService != nil {
			deps.ConnectionsService.Close()
		}
		if deps.ConfigRulesService != nil {
			deps.ConfigRulesService.Close()
		}
		if deps.EndpointsService != nil {
			deps.EndpointsService.Close()
		}
		if deps.LoadbalanceService != nil {
			deps.LoadbalanceService.Close()
		}
		if deps.ModelsService != nil {
			deps.ModelsService.Close()
		}
		if deps.ProfilesService != nil {
			deps.ProfilesService.Close()
		}
		if deps.RealtimeService != nil {
			deps.RealtimeService.Close()
		}
		if deps.RuntimeService != nil {
			deps.RuntimeService.Close()
		}
		if deps.SettingsService != nil {
			deps.SettingsService.Close()
		}
		if deps.StatsService != nil {
			deps.StatsService.Close()
		}
		if deps.VendorsService != nil {
			deps.VendorsService.Close()
		}
		if sharedPool != nil {
			sharedPool.Close()
		}
		return nil, err
	}

	server := &http.Server{
		Addr:              settings.Address(),
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}
	if deps.AuditService != nil {
		server.RegisterOnShutdown(func() {
			deps.AuditService.Close()
		})
	}
	if deps.AuthService != nil {
		server.RegisterOnShutdown(func() {
			deps.AuthService.Close()
		})
	}
	if deps.ConfigBundleService != nil {
		server.RegisterOnShutdown(func() {
			deps.ConfigBundleService.Close()
		})
	}
	if deps.ConnectionsService != nil {
		server.RegisterOnShutdown(func() {
			deps.ConnectionsService.Close()
		})
	}
	if deps.ConfigRulesService != nil {
		server.RegisterOnShutdown(func() {
			deps.ConfigRulesService.Close()
		})
	}
	if deps.EndpointsService != nil {
		server.RegisterOnShutdown(func() {
			deps.EndpointsService.Close()
		})
	}
	if deps.LoadbalanceService != nil {
		server.RegisterOnShutdown(func() {
			deps.LoadbalanceService.Close()
		})
	}
	if deps.ModelsService != nil {
		server.RegisterOnShutdown(func() {
			deps.ModelsService.Close()
		})
	}
	if deps.ProfilesService != nil {
		server.RegisterOnShutdown(func() {
			deps.ProfilesService.Close()
		})
	}
	if deps.RealtimeService != nil {
		server.RegisterOnShutdown(func() {
			deps.RealtimeService.Close()
		})
	}
	if deps.RuntimeService != nil {
		server.RegisterOnShutdown(func() {
			deps.RuntimeService.Close()
		})
	}
	if deps.SettingsService != nil {
		server.RegisterOnShutdown(func() {
			deps.SettingsService.Close()
		})
	}
	if deps.StatsService != nil {
		server.RegisterOnShutdown(func() {
			deps.StatsService.Close()
		})
	}
	if deps.VendorsService != nil {
		server.RegisterOnShutdown(func() {
			deps.VendorsService.Close()
		})
	}
	if sharedPool != nil {
		server.RegisterOnShutdown(func() {
			sharedPool.Close()
		})
	}
	return server, nil
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
	if deps.AuthService != nil {
		router.Use(deps.AuthService.Middleware)
	}

	router.Get("/health", healthHandler(deps.Version))
	router.Mount("/api", NewManagementRouter(deps.AuditService, deps.AuthService, deps.ConfigBundleService, deps.ConfigRulesService, deps.ConnectionsService, deps.EndpointsService, deps.LoadbalanceService, deps.ModelsService, deps.ProfilesService, deps.RealtimeService, deps.SettingsService, deps.StatsService, deps.VendorsService))
	if deps.RuntimeService != nil {
		runtimeHandler := deps.RuntimeService.Handler()
		router.Handle("/v1", runtimeHandler)
		router.Handle("/v1/*", runtimeHandler)
		router.Handle("/v1beta", runtimeHandler)
		router.Handle("/v1beta/*", runtimeHandler)
	} else if deps.AuthService != nil {
		router.Mount("/v1", deps.AuthService.RuntimeProbeRouter())
		router.Mount("/v1beta", deps.AuthService.RuntimeProbeRouter())
	}

	if settings.DocsEnabled() {
		router.Get("/openapi.json", deps.OpenAPI.ServeJSON)
		router.Get("/docs", deps.OpenAPI.ServeDocs)
		router.Get("/redoc", deps.OpenAPI.ServeRedoc)
	}

	return router, nil
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
