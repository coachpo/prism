package platformhttp

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	managementaudit "github.com/coachpo/prism/backend/internal/httpapi/management/audit"
	managementauth "github.com/coachpo/prism/backend/internal/httpapi/management/auth"
	managementconfigrules "github.com/coachpo/prism/backend/internal/httpapi/management/configrules"
	managementconnections "github.com/coachpo/prism/backend/internal/httpapi/management/connections"
	managementendpoints "github.com/coachpo/prism/backend/internal/httpapi/management/endpoints"
	managementloadbalance "github.com/coachpo/prism/backend/internal/httpapi/management/loadbalance"
	managementmodels "github.com/coachpo/prism/backend/internal/httpapi/management/models"
	managementsettings "github.com/coachpo/prism/backend/internal/httpapi/management/settings"
	managementstats "github.com/coachpo/prism/backend/internal/httpapi/management/stats"
	"github.com/coachpo/prism/backend/internal/platform/admission"
)

type healthResponse struct {
	Status    string `json:"status"`
	Version   string `json:"version"`
	Liveness  string `json:"liveness"`
	Readiness string `json:"readiness"`
	Startup   string `json:"startup"`
}

func mountManagementBranch(router chi.Router, deps Dependencies, admissionController *admission.Controller, admissionProvider hotAdmissionProvider) {
	router.Get("/health", healthHandler(deps.Version))

	managementHandler := NewManagementRouter(deps.AuditService, deps.AuthService, deps.ConfigRulesService, deps.ConnectionsService, deps.EndpointsService, deps.LoadbalanceService, deps.ModelsService, deps.SettingsService, deps.StatsService)
	var schemaTransitionReader settingsSchemaTransitionReader
	if deps.DatabasePools != nil {
		schemaTransitionReader = deps.DatabasePools.Management.Raw()
	}
	// Keep the schema guard inside the auth middleware: an unauthenticated
	// caller must still receive the normal authentication response, while an
	// authenticated guarded mutation is rejected before its handler can create
	// a row, preflight, job or routing default.
	managementHandler = (&settingsSchemaGuardMiddleware{reader: schemaTransitionReader}).Middleware(managementHandler)
	if deps.AuthService != nil {
		managementHandler = deps.AuthService.ManagementMiddleware(managementHandler)
	}
	managementHandler = managementBodyLimitMiddleware(managementHandler)
	managementHandler = (&managementAdmissionController{controller: admissionController, provider: admissionProvider}).Middleware(managementHandler)
	managementHandler = newRuntimeCacheInvalidationMiddleware(deps.RuntimeCache, deps.RuntimeAuthService, deps.RuntimeState, deps.StatsService).Middleware(managementHandler)
	router.Mount("/api", managementHandler)
}

func NewManagementRouter(auditService *managementaudit.Service, authService *managementauth.Service, configRulesService *managementconfigrules.Service, connectionsService *managementconnections.Service, endpointsService *managementendpoints.Service, loadbalanceService *managementloadbalance.Service, modelsService *managementmodels.Service, settingsService *managementsettings.Service, statsService *managementstats.Service) http.Handler {
	router := chi.NewRouter()
	if auditService != nil {
		auditService.MountManagementRoutes(router)
	}
	if authService != nil {
		authService.MountManagementRoutes(router)
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
	if settingsService != nil {
		settingsService.MountManagementRoutes(router)
	}
	if statsService != nil {
		statsService.MountManagementRoutes(router)
	}
	return router
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
