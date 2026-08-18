package auth

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/httpapi/requestcontext"
)

var publicManagementPaths = map[string]struct{}{
	"/api/auth/status":           {},
	"/api/auth/public-bootstrap": {},
	"/api/auth/login":            {},
	"/api/auth/logout":           {},
	"/api/auth/refresh":          {},
}

func isPublicManagementPath(path string) bool {
	_, ok := publicManagementPaths[path]
	return ok
}

func (s *Service) managementMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions || !requiresManagementAuthHandling(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		snapshot, err := s.loadAppAuthSettingsSnapshot(r.Context())
		if err != nil {
			responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusInternalServerError, "Failed to load authentication settings")
			return
		}
		// A persisted fail-closed/rollback auth transition blocks ordinary
		// management before any domain handler, with the registered typed
		// 503 problem codes (never a generic 5xx, never auth-disabled). The
		// auth-control settings surface stays reachable as the repair path,
		// and the auth-exempt public surface (status, public-bootstrap,
		// operations status) is never blocked here: each of those routes
		// applies its own transition semantics (tagged union / typed 503).
		if snapshot.TransitionState != "" && snapshot.TransitionState != "disabling_enforced" &&
			!isAuthControlSettingsPath(r.Method, r.URL.Path) &&
			!isPublicManagementPath(r.URL.Path) &&
			!isPublicAuthOperationStatusPath(r.Method, r.URL.Path, r.URL.RawQuery) {
			writeTransitionProblem(w, r, s.corsSnapshot(), snapshot.TransitionState, snapshot.EffectiveAuthGeneration, s.snapshotRetryAfterSeconds(snapshot))
			return
		}
		if !snapshot.AuthEnabled {
			next.ServeHTTP(w, r)
			return
		}
		if isPublicManagementPath(r.URL.Path) || isPublicAuthOperationStatusPath(r.Method, r.URL.Path, r.URL.RawQuery) {
			// Public auth-operation status is a deliberate, bounded exemption
			// (Auth/Session/Landing SPEC §5.1): the operation id is a lookup
			// selector, not authorization, and the fixed ≤1 KiB projection
			// carries no username, mode, operation id, secret or settings. It
			// must stay reachable without a session because ordinary
			// management is blocked during fail-closed transitions.
			next.ServeHTTP(w, r)
			return
		}
		authConfig := s.runtimeAuthConfigSnapshot()
		authSubject, ok := s.authSubjectFromAccessSnapshot(r, authConfig, snapshot)
		if !ok {
			writeAuthProblem(w, r, s.corsSnapshot(), &domainError{StatusCode: http.StatusUnauthorized, Code: ProblemCodeAuthNotAuthenticated, Detail: "Authentication required"})
			return
		}
		contextWithSubject := context.WithValue(r.Context(), authSubjectContextKey{}, authSubject)
		contextWithPrincipal := requestcontext.WithManagementPrincipalSnapshot(contextWithSubject, requestcontext.ManagementPrincipalSnapshot{
			SubjectID:      strconv.Itoa(authSubject.ID),
			TokenVersion:   strconv.Itoa(authSubject.TokenVersion),
			AuthGeneration: strconv.FormatInt(snapshot.EffectiveAuthGeneration, 10),
		})
		next.ServeHTTP(w, r.WithContext(contextWithPrincipal))
	})
}

func requiresManagementAuthHandling(path string) bool {
	return strings.HasPrefix(path, "/api/")
}

// isAuthControlSettingsPath reports whether the request is the auth-control
// settings read/write surface. It stays reachable during a fail-closed
// transition so the operator can repair or confirm the operation; it never
// bypasses session enforcement when the effective mode is enabled.
func isAuthControlSettingsPath(method string, path string) bool {
	if path != "/api/settings/auth" {
		return false
	}
	return method == http.MethodGet || method == http.MethodPut
}

func (s *Service) authSubjectFromAccessSnapshot(request *http.Request, authConfig RuntimeAuthConfigSnapshot, snapshot AppAuthSettingsSnapshot) (authSubject, bool) {
	cookie, err := request.Cookie(authConfig.AccessCookieName)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return authSubject{}, false
	}
	claims, err := parseAccessToken(s.nowUTC(), s.authJWTSecret, cookie.Value)
	if err != nil {
		return authSubject{}, false
	}
	subjectID, err := strconv.Atoi(strings.TrimSpace(claims.Sub))
	if err != nil {
		return authSubject{}, false
	}
	if subjectID != snapshot.ID || claims.TokenVersion != snapshot.TokenVersion {
		return authSubject{}, false
	}
	return authSubject{ID: snapshot.ID, TokenVersion: snapshot.TokenVersion, Username: claims.Username}, true
}

func (s *Service) snapshotRetryAfterSeconds(snapshot AppAuthSettingsSnapshot) *int64 {
	if snapshot.TransitionRetryAfterAt.IsZero() {
		return nil
	}
	seconds := int64(snapshot.TransitionRetryAfterAt.Sub(s.nowUTC()).Seconds())
	if seconds < 0 {
		seconds = 0
	}
	return &seconds
}
