package platformhttp

import (
	"context"
	"encoding/json"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5"
	"net/http"
)

type settingsSchemaTransitionReader interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// settingsSchemaGuardMiddleware is deliberately separate from admission so
// the auth middleware can remain the outer owner of protected routes. It
// checks only the exact method/path registry entries marked above.
type settingsSchemaGuardMiddleware struct {
	reader settingsSchemaTransitionReader
}

func (m *settingsSchemaGuardMiddleware) Middleware(next http.Handler) http.Handler {
	if m == nil || m.reader == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		routeSpec, ok := matchManagementRouteSpec(r.Method, r.URL.Path)
		if !ok || !routeSpec.settingsSchemaGuard {
			next.ServeHTTP(w, r)
			return
		}
		phase, exists, err := readSettingsSchemaPhase(r.Context(), m.reader)
		if err != nil {
			// A failed transition read must fail closed before the guarded
			// mutation reaches its handler. The safe response does not expose
			// database details or replay the request.
			writeSettingsSchemaStateUnavailable(w, r)
			return
		}
		if exists && (phase == "quiescing" || phase == "finalizing") {
			writeSettingsSchemaFinalizing(w, r, phase)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func readSettingsSchemaPhase(ctx context.Context, reader settingsSchemaTransitionReader) (string, bool, error) {
	var exists bool
	if err := reader.QueryRow(ctx, `SELECT to_regclass('public.settings_schema_transition') IS NOT NULL`).Scan(&exists); err != nil {
		return "", false, err
	}
	if !exists {
		return "", false, nil
	}
	var phase string
	if err := reader.QueryRow(ctx, `SELECT domain_phase FROM settings_schema_transition WHERE id = 1`).Scan(&phase); err != nil {
		return "", true, err
	}
	return phase, true, nil
}

func writeSettingsSchemaFinalizing(w http.ResponseWriter, r *http.Request, phase string) {
	requestID := middleware.GetReqID(r.Context())
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Retry-After", "3")
	w.WriteHeader(http.StatusServiceUnavailable)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"code":       "settings_schema_finalizing",
		"detail":     "Settings schema is quiescing/finalizing; refetch authoritative state before resubmitting",
		"params":     map[string]any{},
		"details":    map[string]any{"transition_phase": phase, "retry_after_seconds": 3, "retry_scope": "status_check_only", "recovery": "wait_then_refetch_before_resubmit"},
		"request_id": requestID,
	})
}

func writeSettingsSchemaStateUnavailable(w http.ResponseWriter, r *http.Request) {
	requestID := middleware.GetReqID(r.Context())
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Retry-After", "3")
	w.WriteHeader(http.StatusServiceUnavailable)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"code":       "internal_error",
		"detail":     "Settings schema transition state is unavailable",
		"params":     map[string]any{},
		"details":    map[string]any{"recovery": "retry", "retry_after_seconds": 3},
		"request_id": requestID,
	})
}
