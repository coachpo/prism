package platformhttp

import (
	"github.com/coachpo/prism/backend/internal/platform/admission"
	"github.com/coachpo/prism/backend/internal/platform/config"
	"log/slog"
	"net/http"
)

type admissionSnapshotProvider interface {
	AdmissionSnapshot() StartupAdmissionSnapshot
}

type managementAdmissionController struct {
	controller *admission.Controller
	provider   admissionSnapshotProvider
}

// The clamp warning is emitted from buildStartupAdmissionSnapshot, which runs on
// the startup path; warning here as well would log the same line twice per boot.
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
