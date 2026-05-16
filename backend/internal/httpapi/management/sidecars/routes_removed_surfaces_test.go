package sidecars

import (
	"net/http"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/coachpo/prism/backend/internal/platform/config"
	"github.com/go-chi/chi/v5"
)

func TestSidecarManagementRoutesMatchCurrentSurface(t *testing.T) {
	service, err := NewService(config.Settings{SecretEncryptionKey: "unit-test-secret"}, Options{})
	if err != nil {
		t.Fatalf("new sidecar service: %v", err)
	}
	router := chi.NewRouter()
	service.MountManagementRoutes(router)

	mounted := map[string]map[string]bool{}
	if err := chi.Walk(router, func(method string, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if mounted[route] == nil {
			mounted[route] = map[string]bool{}
		}
		mounted[route][method] = true
		return nil
	}); err != nil {
		t.Fatalf("walk sidecar routes: %v", err)
	}

	failures := []string{}
	for route, methods := range currentSidecarRouteSurface() {
		for _, method := range methods {
			if !mounted[route][method] {
				failures = append(failures, method+" "+route+" missing from mounted sidecar router")
			}
		}
	}
	for route, methods := range mounted {
		for method := range methods {
			if !sidecarRouteMethodAllowed(currentSidecarRouteSurface(), route, method) {
				failures = append(failures, method+" "+route+" unexpectedly mounted in sidecar router")
			}
		}
	}
	if len(failures) > 0 {
		sort.Strings(failures)
		t.Fatalf("sidecar route surface mismatch:\n%s", strings.Join(failures, "\n"))
	}
}

func currentSidecarRouteSurface() map[string][]string {
	return map[string][]string{
		"/sidecars":                                           {http.MethodGet, http.MethodPost},
		"/sidecars/{sidecar_id}":                              {http.MethodGet, http.MethodPatch, http.MethodDelete},
		"/sidecars/{sidecar_id}/test-connection":              {http.MethodPost},
		"/sidecars/{sidecar_id}/sync":                         {http.MethodPost},
		"/sidecars/{sidecar_id}/auth-files":                   {http.MethodGet},
		"/sidecars/{sidecar_id}/auth-files/{auth_id}/status":  {http.MethodPatch},
		"/sidecars/{sidecar_id}/auth-files/{auth_id}/fields":  {http.MethodPatch},
		"/sidecars/{sidecar_id}/auth-snapshots":               {http.MethodGet},
		"/sidecars/{sidecar_id}/auth-snapshots/{snapshot_id}": {http.MethodGet},
		"/sidecars/{sidecar_id}/providers":                    {http.MethodGet},
		"/sidecars/{sidecar_id}/provider-snapshots":           {http.MethodGet},
		"/sidecars/{sidecar_id}/sync-status":                  {http.MethodGet},
	}
}

func sidecarRouteMethodAllowed(surface map[string][]string, route string, method string) bool {
	return slices.Contains(surface[route], method)
}
