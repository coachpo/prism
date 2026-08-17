package platformhttp

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/coachpo/prism/backend/internal/platform/config"
	platformcors "github.com/coachpo/prism/backend/internal/platform/cors"
	"github.com/coachpo/prism/backend/internal/platform/version"
)

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

func NewHandler(settings config.Settings) (http.Handler, error) {
	loadedVersion, err := version.Load()
	if err != nil {
		return nil, err
	}

	return NewHandlerWithDependencies(settings, Dependencies{Version: loadedVersion})
}

func NewHandlerWithDependencies(settings config.Settings, deps Dependencies) (http.Handler, error) {
	deps, err := completeHandlerDependencies(settings, deps)
	if err != nil {
		return nil, err
	}

	router := chi.NewRouter()

	router.Use(middleware.RequestID)
	router.Use(middleware.RealIP)
	router.Use(middleware.Recoverer)
	router.Use(corsMiddleware(deps.CORSOriginProvider))

	admissionController := newHTTPAdmissionController(settings)
	var admissionProvider admissionSnapshotProvider
	if deps.StartupConfigRuntime != nil {
		admissionProvider = deps.StartupConfigRuntime
	}
	router.Group(func(management chi.Router) {
		mountManagementBranch(management, deps, admissionController, admissionProvider)
	})
	router.Group(func(runtime chi.Router) {
		mountRuntimeBranch(runtime, settings, deps, admissionController, admissionProvider)
	})

	return router, nil
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
