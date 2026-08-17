package platformhttp

import (
	"github.com/go-chi/chi/v5"

	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
	"github.com/coachpo/prism/backend/internal/platform/admission"
	"github.com/coachpo/prism/backend/internal/platform/config"
)

func mountRuntimeBranch(router chi.Router, settings config.Settings, deps Dependencies, admissionController *admission.Controller, admissionProvider admissionSnapshotProvider) {
	runtimeAuthService := deps.RuntimeAuthService
	if runtimeAuthService == nil {
		runtimeAuthService = deps.AuthService
	}

	if deps.RuntimeService != nil {
		runtimeHandler := deps.RuntimeService.Handler()
		if runtimeAuthService != nil {
			runtimeHandler = runtimeAuthService.RuntimeMiddleware(runtimeHandler)
		}
		runtimeHandler = proxyAdmissionProviderMiddleware(admissionProvider, admissionController, runtimeHandler)
		runtimeHandler = runtimeapi.RuntimeIngressRequestIDMiddleware(runtimeHandler)
		router.Handle("/v1", runtimeHandler)
		router.Handle("/v1/*", runtimeHandler)
		router.Handle("/v1beta", runtimeHandler)
		router.Handle("/v1beta/*", runtimeHandler)
		return
	}
	if runtimeAuthService != nil {
		runtimeProbeHandler := proxyAdmissionProviderMiddleware(admissionProvider, admissionController, runtimeAuthService.RuntimeMiddleware(runtimeAuthService.RuntimeProbeRouter()))
		runtimeProbeHandler = runtimeapi.RuntimeIngressRequestIDMiddleware(runtimeProbeHandler)
		router.Mount("/v1", runtimeProbeHandler)
		router.Mount("/v1beta", runtimeProbeHandler)
	}
}
