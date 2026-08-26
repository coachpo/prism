package platformhttp

import (
	"github.com/coachpo/prism/backend/internal/platform/admission"
	"github.com/coachpo/prism/backend/internal/platform/priority"
	"net/http"
)

func proxyAdmissionProviderMiddleware(provider admissionSnapshotProvider, fallbackController *admission.Controller, next http.Handler) http.Handler {
	if provider == nil && fallbackController == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		controller := fallbackController
		if provider != nil {
			controller = provider.AdmissionSnapshot().Controller()
		}
		spec := admission.Spec{
			Name:     "runtime proxy",
			Metadata: priority.Metadata{Priority: priority.PriorityProxy},
			// Timeout is deliberately zero (no deadline): runtime.transport.requestTimeout
			// was removed with the transport config section.
			Timeout: 0,
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
