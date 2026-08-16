package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/httpapi/requestcontext"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	platformcors "github.com/coachpo/prism/backend/internal/platform/cors"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5"
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

func (s *Service) Middleware(next http.Handler) http.Handler {
	managementHandler := s.ManagementMiddleware(next)
	runtimeHandler := s.RuntimeMiddleware(next)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case requiresManagementAuthHandling(r.URL.Path):
			managementHandler.ServeHTTP(w, r)
		case requiresRuntimeAuthHandling(r.URL.Path):
			runtimeHandler.ServeHTTP(w, r)
		default:
			next.ServeHTTP(w, r)
		}
	})
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

func (s *Service) runtimeMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions || !requiresRuntimeAuthHandling(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		authSettings, err := s.loadRuntimeAuthSettings(r.Context())
		if err != nil {
			if isPublishedSnapshotUnavailable(err) {
				responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusServiceUnavailable, "Runtime authentication snapshot is unavailable. Retry later.")
				return
			}
			responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusInternalServerError, "Failed to load authentication settings")
			return
		}
		authEnforced := authSettings.AuthEnabled

		rawKey, _ := extractProxyAPIKey(r.Header)
		if authEnforced && rawKey == "" {
			responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusUnauthorized, "Proxy API key required")
			return
		}
		if rawKey == "" {
			// Permissive mode with no credential: continue as none.
			attribution := requestcontext.RuntimeProxyKeyAttribution{
				State:        requestcontext.RuntimeProxyKeyNone,
				Snapshot:     nil,
				AuthEnforced: false,
			}
			next.ServeHTTP(w, r.WithContext(requestcontext.WithRuntimeProxyKeyAttribution(r.Context(), attribution)))
			return
		}

		proxyKey, verifyErr := s.verifyProxyAPIKey(r.Context(), rawKey)
		if verifyErr != nil {
			if authEnforced {
				// Once enforcement is known, any verifier/cache/database
				// failure must fail closed with one typed unavailable response.
				// Do not expose whether a particular key lookup failed.
				if isPublishedSnapshotUnavailable(verifyErr) {
					responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusServiceUnavailable, "Runtime authentication snapshot is unavailable. Retry later.")
				} else {
					responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusServiceUnavailable, "Runtime authentication verifier is unavailable. Retry later.")
				}
				return
			}
			// Permissive optional verification failure: fail open for
			// execution, fail closed for identity. The request continues
			// as unknown; lookup details are never disclosed.
			slog.Warn("proxy key optional verification unavailable; attribution unknown", "error", verifyErr)
			attribution := requestcontext.RuntimeProxyKeyAttribution{
				State:        requestcontext.RuntimeProxyKeyUnknown,
				Snapshot:     nil,
				AuthEnforced: false,
			}
			next.ServeHTTP(w, r.WithContext(requestcontext.WithRuntimeProxyKeyAttribution(r.Context(), attribution)))
			return
		}
		if proxyKey == nil {
			if authEnforced {
				responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusUnauthorized, "Invalid proxy API key")
				return
			}
			// Permissive mode with an unrecognized/inactive/expired key:
			// continue as none; the caller is never told why.
			attribution := requestcontext.RuntimeProxyKeyAttribution{
				State:        requestcontext.RuntimeProxyKeyNone,
				Snapshot:     nil,
				AuthEnforced: false,
			}
			next.ServeHTTP(w, r.WithContext(requestcontext.WithRuntimeProxyKeyAttribution(r.Context(), attribution)))
			return
		}

		proxyKeySnapshot := requestcontext.RuntimeProxyKeySnapshot{
			ID:         proxyKey.ID,
			Name:       proxyKey.Name,
			LastUsedAt: s.nowUTC(),
			LastUsedIP: requestIP(r),
		}
		attribution := requestcontext.RuntimeProxyKeyAttribution{
			State:        requestcontext.RuntimeProxyKeyIdentified,
			Snapshot:     &proxyKeySnapshot,
			AuthEnforced: authEnforced,
		}
		contextWithProxyKey := requestcontext.WithRuntimeProxyKeyAttribution(r.Context(), attribution)
		if s.runtimeCache == nil {
			if err := s.enqueueProxyAPIKeyUsage(proxyKey.ID, proxyKeySnapshot.LastUsedAt, proxyKeySnapshot.LastUsedIP); err != nil {
				slog.Error("failed to enqueue proxy API key usage", "error", err, "key_id", proxyKey.ID)
			}
		}
		next.ServeHTTP(w, r.WithContext(contextWithProxyKey))
	})
}

func requiresManagementAuthHandling(path string) bool {
	return strings.HasPrefix(path, "/api/")
}

func requiresRuntimeAuthHandling(path string) bool {
	return strings.HasPrefix(path, "/v1/") || strings.HasPrefix(path, "/v1beta/")
}

func extractProxyAPIKey(header http.Header) (string, string) {
	authorization := strings.TrimSpace(header.Get("Authorization"))
	if authorization != "" {
		parts := strings.SplitN(authorization, " ", 2)
		if len(parts) == 2 && strings.EqualFold(strings.TrimSpace(parts[0]), "Bearer") && strings.TrimSpace(parts[1]) != "" {
			return strings.TrimSpace(parts[1]), "authorization"
		}
	}
	for _, name := range []string{"X-API-Key", "X-Goog-Api-Key"} {
		value := strings.TrimSpace(header.Get(name))
		if value != "" {
			return value, strings.ToLower(name)
		}
	}
	return "", ""
}

func requestIP(request *http.Request) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(request.RemoteAddr))
	if err == nil {
		return host
	}
	return strings.TrimSpace(request.RemoteAddr)
}

func (s *Service) authSubjectFromAccessCookie(request *http.Request, authConfig RuntimeAuthConfigSnapshot, settingsRow appAuthSettingsRow) (authSubject, bool) {
	return s.authSubjectFromAccessSnapshot(request, authConfig, appAuthSettingsSnapshotFromRow(settingsRow))
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

func (s *Service) handleGetAuthStatus(w http.ResponseWriter, r *http.Request) {
	setNoStoreHeaders(w)
	settingsRow, err := s.loadOrCreateAppAuthSettings(r.Context(), s.pool)
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusInternalServerError, "Failed to load authentication settings")
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, s.buildPublicAuthStatus(settingsRow))
}

// buildPublicAuthStatus renders the tagged PublicAuthStatus union. The only
// legal combinations are enabled+null|disabling_enforced, disabled+null, and
// transition_fail_closed+enabling_fail_closed|rollback_required; every
// branch carries the canonical positive decimal effective_generation.
func (s *Service) buildPublicAuthStatus(row appAuthSettingsRow) authStatusResponse {
	generation := fmt.Sprintf("%d", row.EffectiveAuthGeneration)
	if row.TransitionState.Valid {
		state := row.TransitionState.String
		switch state {
		case "enabling_fail_closed":
			return authStatusResponse{
				State:               "transition_fail_closed",
				TransitionState:     "enabling_fail_closed",
				LoginAvailable:      false,
				EffectiveGeneration: generation,
				RetryAfterSeconds:   s.transitionRetryAfterSeconds(row),
			}
		case "rollback_required":
			return authStatusResponse{
				State:               "transition_fail_closed",
				TransitionState:     "rollback_required",
				LoginAvailable:      false,
				EffectiveGeneration: generation,
				RetryAfterSeconds:   s.transitionRetryAfterSeconds(row),
			}
		case "disabling_enforced":
			return authStatusResponse{
				State:               "enabled",
				TransitionState:     "disabling_enforced",
				LoginAvailable:      true,
				EffectiveGeneration: generation,
			}
		}
	}
	if row.AuthEnabled {
		return authStatusResponse{
			State:               "enabled",
			TransitionState:     nil,
			LoginAvailable:      true,
			EffectiveGeneration: generation,
		}
	}
	return authStatusResponse{
		State:               "disabled",
		TransitionState:     nil,
		LoginAvailable:      false,
		EffectiveGeneration: generation,
	}
}

func (s *Service) transitionRetryAfterSeconds(row appAuthSettingsRow) *int64 {
	if !row.TransitionRetryAfterAt.Valid {
		return nil
	}
	seconds := int64(row.TransitionRetryAfterAt.Time.Sub(s.nowUTC()).Seconds())
	if seconds < 0 {
		seconds = 0
	}
	return &seconds
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

// newOperationID returns a fresh RFC 4122 v4 UUID used as the server fallback
// operation selector for auth transitions. It is a lookup selector, not an
// authorization secret.
func newOperationID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "00000000-0000-4000-8000-000000000000"
	}
	buf[6] = (buf[6] & 0x0f) | 0x40 // version 4
	buf[8] = (buf[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(buf[0:4]),
		hex.EncodeToString(buf[4:6]),
		hex.EncodeToString(buf[6:8]),
		hex.EncodeToString(buf[8:10]),
		hex.EncodeToString(buf[10:16]),
	)
}

func (s *Service) handleGetPublicBootstrap(w http.ResponseWriter, r *http.Request) {
	setNoStoreHeaders(w)
	authConfig := s.runtimeAuthConfigSnapshot()
	settingsRow, err := s.loadOrCreateAppAuthSettings(r.Context(), s.pool)
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusInternalServerError, "Failed to load authentication settings")
		return
	}
	// A persisted fail-closed/rollback transition is a typed 503 for public
	// bootstrap too; it is never reported as disabled or unauthenticated.
	if settingsRow.TransitionState.Valid && settingsRow.TransitionState.String != "disabling_enforced" {
		writeTransitionProblem(w, r, s.corsSnapshot(), settingsRow.TransitionState.String, settingsRow.EffectiveAuthGeneration, s.transitionRetryAfterSeconds(settingsRow))
		return
	}
	if !settingsRow.AuthEnabled {
		s.clearAuthCookies(w, authConfig)
		responseutil.WriteJSON(w, http.StatusOK, sessionResponse{Authenticated: false, AuthEnabled: false, Username: nil})
		return
	}
	if authSubject, ok := s.authSubjectFromAccessCookie(r, authConfig, settingsRow); ok {
		responseutil.WriteJSON(w, http.StatusOK, s.buildAuthenticatedSession(settingsRow, authSubject.Username))
		return
	}
	bundle, err := s.withRefreshCookie(r.Context(), r, authConfig)
	if err != nil {
		var authErr *domainError
		if errors.As(err, &authErr) && authErr.StatusCode == http.StatusUnauthorized {
			s.clearAuthCookies(w, authConfig)
			responseutil.WriteJSON(w, http.StatusOK, sessionResponse{Authenticated: false, AuthEnabled: true, Username: nil})
			return
		}
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	s.setAuthCookies(w, authConfig, bundle.AccessToken, bundle.RefreshToken, bundle.RefreshExpiresAt, bundle.SessionDuration)
	responseutil.WriteJSON(w, http.StatusOK, s.buildAuthenticatedSession(bundle.SettingsRow, stringValue(bundle.SettingsRow.Username)))
}

// buildAuthenticatedSession renders the strict AuthenticatedSession payload
// {authenticated, auth_enabled, username, subject_key} for authenticated
// login/session/refresh/bootstrap responses. subject_key is the server-
// authored canonical account identity; it is never included in public status
// or anonymous/disabled payloads.
func (s *Service) buildAuthenticatedSession(settingsRow appAuthSettingsRow, username string) sessionResponse {
	subjectKey := canonicalSubjectKey(settingsRow.ID)
	return sessionResponse{
		Authenticated: true,
		AuthEnabled:   settingsRow.AuthEnabled,
		Username:      stringPointer(username),
		SubjectKey:    &subjectKey,
	}
}

func canonicalSubjectKey(settingsID int) string {
	return fmt.Sprintf("auth:subject:%d", settingsID)
}

func (s *Service) handleLogin(w http.ResponseWriter, r *http.Request) {
	setNoStoreHeaders(w)
	authConfig := s.runtimeAuthConfigSnapshot()
	var requestBody loginRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	duration, err := normalizeSessionDuration(requestBody.SessionDuration)
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid session duration")
		return
	}
	result, err := pgxutil.InTxValue(r.Context(), s.pool, "auth", func(tx pgx.Tx) (loginAuthenticationResult, error) {
		// Login reads live settings so a persisted fail-closed transition is
		// a registered typed 503, never a credentials error or disabled mode.
		settingsRow, loadErr := s.loadOrCreateAppAuthSettings(r.Context(), tx)
		if loadErr != nil {
			return loginAuthenticationResult{}, loadErr
		}
		if settingsRow.TransitionState.Valid && settingsRow.TransitionState.String != "disabling_enforced" {
			return loginAuthenticationResult{DomainErr: &domainError{
				StatusCode: http.StatusServiceUnavailable,
				Code:       transitionProblemCodeFor(settingsRow.TransitionState.String),
				Detail:     transitionProblemDetailFor(settingsRow.TransitionState.String),
				Details:    transitionProblemDetailsFor(settingsRow.TransitionState.String, settingsRow.EffectiveAuthGeneration, s.transitionRetryAfterSeconds(settingsRow)),
			}}, nil
		}
		return s.authenticateUser(
			r.Context(),
			tx,
			authConfig,
			strings.TrimSpace(requestBody.Username),
			requestBody.Password,
			duration,
			r.UserAgent(),
			requestIP(r),
		)
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	if result.DomainErr != nil {
		writeAuthProblem(w, r, s.corsSnapshot(), result.DomainErr)
		return
	}
	bundle := result.Bundle
	s.setAuthCookies(w, authConfig, bundle.AccessToken, bundle.RefreshToken, bundle.RefreshExpiresAt, bundle.SessionDuration)
	responseutil.WriteJSON(w, http.StatusOK, s.buildAuthenticatedSession(bundle.SettingsRow, stringValue(bundle.SettingsRow.Username)))
}

func (s *Service) handleLogout(w http.ResponseWriter, r *http.Request) {
	setNoStoreHeaders(w)
	authConfig := s.runtimeAuthConfigSnapshot()
	settingsRow, err := s.loadOrCreateAppAuthSettings(r.Context(), s.pool)
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusInternalServerError, "Failed to load authentication settings")
		return
	}
	// A persisted fail-closed transition also blocks logout with the typed
	// 503 so the operator cannot race a half-published mode change.
	if settingsRow.TransitionState.Valid && settingsRow.TransitionState.String != "disabling_enforced" {
		writeTransitionProblem(w, r, s.corsSnapshot(), settingsRow.TransitionState.String, settingsRow.EffectiveAuthGeneration, s.transitionRetryAfterSeconds(settingsRow))
		return
	}
	if !settingsRow.AuthEnabled {
		// Auth-mode race: auth is already disabled, so there is no session to
		// revoke; the registered typed 400 lets the client re-bootstrap to the
		// open-access explainer instead of pretending a session ended.
		writeAuthProblem(w, r, s.corsSnapshot(), &domainError{StatusCode: http.StatusBadRequest, Code: ProblemCodeAuthNotEnabled, Detail: "本实例未启用身份验证"})
		return
	}
	if err := pgxutil.InTx(r.Context(), s.pool, "auth", func(tx pgx.Tx) error {
		cookie, cookieErr := r.Cookie(authConfig.RefreshCookieName)
		if cookieErr == nil && strings.TrimSpace(cookie.Value) != "" {
			_, revokeErr := s.revokeRefreshToken(r.Context(), tx, cookie.Value)
			if revokeErr != nil {
				return revokeErr
			}
		}
		return nil
	}); err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	// Idempotent strict 204: valid, missing, expired and revoked cookies all
	// revoke nothing new, always send the canonical clear-cookie headers and
	// never go through the management pre-handler.
	s.clearAuthCookies(w, authConfig)
	w.Header().Set("Cache-Control", "private, no-store")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) handleRefresh(w http.ResponseWriter, r *http.Request) {
	setNoStoreHeaders(w)
	authConfig := s.runtimeAuthConfigSnapshot()
	settingsRow, err := s.loadOrCreateAppAuthSettings(r.Context(), s.pool)
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusInternalServerError, "Failed to load authentication settings")
		return
	}
	// Refresh reads live auth access truth: a persisted fail-closed
	// transition is the registered typed 503 (never false,false or a generic
	// 5xx), and auth-disabled is a live 200 {auth_enabled:false,
	// authenticated:false}, never a 200 {true,false} stale-mode answer.
	if settingsRow.TransitionState.Valid && settingsRow.TransitionState.String != "disabling_enforced" {
		writeTransitionProblem(w, r, s.corsSnapshot(), settingsRow.TransitionState.String, settingsRow.EffectiveAuthGeneration, s.transitionRetryAfterSeconds(settingsRow))
		return
	}
	if !settingsRow.AuthEnabled {
		s.clearAuthCookies(w, authConfig)
		responseutil.WriteJSON(w, http.StatusOK, sessionResponse{Authenticated: false, AuthEnabled: false, Username: nil})
		return
	}
	bundle, err := s.withRefreshCookie(r.Context(), r, authConfig)
	if err != nil {
		var authErr *domainError
		if errors.As(err, &authErr) && authErr.StatusCode == http.StatusUnauthorized {
			s.clearAuthCookies(w, authConfig)
			responseutil.WriteJSON(w, http.StatusOK, sessionResponse{Authenticated: false, AuthEnabled: true, Username: nil})
			return
		}
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	s.setAuthCookies(w, authConfig, bundle.AccessToken, bundle.RefreshToken, bundle.RefreshExpiresAt, bundle.SessionDuration)
	responseutil.WriteJSON(w, http.StatusOK, s.buildAuthenticatedSession(bundle.SettingsRow, stringValue(bundle.SettingsRow.Username)))
}

func (s *Service) withRefreshCookie(ctx context.Context, request *http.Request, authConfig RuntimeAuthConfigSnapshot) (sessionBundle, error) {
	cookie, err := request.Cookie(authConfig.RefreshCookieName)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return sessionBundle{}, &domainError{StatusCode: http.StatusUnauthorized, Detail: "Invalid refresh token"}
	}
	return pgxutil.InTxValue(ctx, s.pool, "auth", func(tx pgx.Tx) (sessionBundle, error) {
		return s.rotateRefreshToken(ctx, tx, authConfig, cookie.Value, request.UserAgent(), requestIP(request))
	})
}

func (s *Service) handleGetSession(w http.ResponseWriter, r *http.Request) {
	setNoStoreHeaders(w)
	authSubject, ok := authSubjectFromRequest(r)
	if !ok {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusUnauthorized, "Authentication required")
		return
	}
	settingsRow, err := s.loadOrCreateAppAuthSettings(r.Context(), s.pool)
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusInternalServerError, "Failed to load authentication settings")
		return
	}
	username := authSubject.Username
	if username == "" {
		username = stringValue(settingsRow.Username)
	}
	responseutil.WriteJSON(w, http.StatusOK, s.buildAuthenticatedSession(settingsRow, username))
}

func (s *Service) handleGetAuthSettings(w http.ResponseWriter, r *http.Request) {
	setNoStoreHeaders(w)
	settingsRow, err := s.loadOrCreateAppAuthSettings(r.Context(), s.pool)
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusInternalServerError, "Failed to load authentication settings")
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, s.buildAuthSettingsResponse(settingsRow))
}

func (s *Service) handlePutAuthSettings(w http.ResponseWriter, r *http.Request) {
	setNoStoreHeaders(w)
	authConfig := s.runtimeAuthConfigSnapshot()
	var requestBody authSettingsUpdateRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	result, err := pgxutil.InTxValue(r.Context(), s.pool, "auth", func(tx pgx.Tx) (authSettingsMutationResult, error) {
		settingsRow, loadErr := s.loadOrCreateAppAuthSettings(r.Context(), tx)
		if loadErr != nil {
			return authSettingsMutationResult{}, fmt.Errorf("load auth settings: %w", loadErr)
		}
		return s.updateAuthSettings(r.Context(), tx, settingsRow, requestBody)
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	s.invalidateAppAuthSettingsSnapshot()
	if result.SessionInvalidated {
		s.clearAuthCookies(w, authConfig)
	}
	// Publish proof: the just-written effective mode/generation must
	// round-trip through a fresh DB read (the same source the management
	// middleware consumes) before it is reported as effective. This check is
	// available in every service shape and never probes the runtime cache,
	// which the management service does not own; runtime adoption follows the
	// invalidation middleware's generation bump, and a genuinely unavailable
	// runtime snapshot is already fail-closed by the runtime middleware's
	// typed 503. Persisted transition states stay real (crash-interrupted or
	// explicitly seeded operations), never entered from a transient refresh
	// race. A failed round-trip is a real persistence failure: the write is
	// reverted and a durable rollback_required transition is persisted with
	// the initiating browser's operation id (or a server fallback).
	if err := s.validateAuthSettingsPublished(r.Context(), result.Row); err != nil {
		slog.Warn("auth settings publish proof failed", "error", err)
		s.enterAuthRollbackRequired(w, r, result, requestBody.OperationID, authConfig)
		writeTransitionProblem(w, r, s.corsSnapshot(), "rollback_required", result.Previous.EffectiveAuthGeneration, nil)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, s.buildAuthSettingsResponse(result.Row))
}

// validateAuthSettingsPublished proves the just-written settings row
// round-trips through a fresh DB read with the expected effective mode and
// generation.
func (s *Service) validateAuthSettingsPublished(ctx context.Context, written appAuthSettingsRow) error {
	row, err := s.loadOrCreateAppAuthSettings(ctx, s.pool)
	if err != nil {
		return fmt.Errorf("publish auth settings: %w", err)
	}
	if row.AuthEnabled != written.AuthEnabled || row.EffectiveAuthGeneration != written.EffectiveAuthGeneration {
		return fmt.Errorf("publish auth settings: written mode/generation did not round-trip")
	}
	return nil
}

// enterAuthRollbackRequired persists the durable rollback_required transition
// after reverting the just-written effective mode to the previous value. The
// operation id is the browser intent that started this write (or a server
// fallback); a lost response never creates a second mutation.
func (s *Service) enterAuthRollbackRequired(w http.ResponseWriter, r *http.Request, result authSettingsMutationResult, operationID string, authConfig RuntimeAuthConfigSnapshot) {
	if operationID == "" {
		operationID = newOperationID()
	}
	retryAfterAt := s.nowUTC().Add(15 * time.Minute)
	previous := result.Previous
	if _, err := pgxutil.InTxValue(r.Context(), s.pool, "auth", func(tx pgx.Tx) (appAuthSettingsRow, error) {
		row, loadErr := s.loadOrCreateAppAuthSettings(r.Context(), tx)
		if loadErr != nil {
			return appAuthSettingsRow{}, loadErr
		}
		if _, updateErr := tx.Exec(
			r.Context(),
			`UPDATE app_auth_settings
			SET auth_enabled = $2, effective_auth_generation = $3, updated_at = $4
			WHERE id = $1`,
			row.ID,
			previous.AuthEnabled,
			previous.EffectiveAuthGeneration,
			s.nowUTC(),
		); updateErr != nil {
			return appAuthSettingsRow{}, updateErr
		}
		return s.setAuthTransition(r.Context(), tx, row.ID, "rollback_required", operationID, retryAfterAt, 0)
	}); err != nil {
		slog.Error("persist auth rollback transition", "error", err)
	}
	s.invalidateAppAuthSettingsSnapshot()
	s.clearAuthCookies(w, authConfig)
}

func (s *Service) handleListProxyKeys(w http.ResponseWriter, r *http.Request) {
	setNoStoreHeaders(w)
	for _, value := range r.URL.Query()["include"] {
		if strings.TrimSpace(value) == "setup_readiness" {
			expectedGeneration := strings.TrimSpace(r.URL.Query().Get("expected_route_witness_generation"))
			if expectedGeneration == "" {
				responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "expected_route_witness_generation is required with include=setup_readiness")
				return
			}
			s.handleListProxyKeysWithSetupReadiness(w, r, expectedGeneration)
			return
		}
	}
	rows, err := s.listProxyAPIKeys(r.Context(), s.pool)
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusInternalServerError, "Failed to load proxy API keys")
		return
	}
	capacity, err := countProxyKeyCapacity(r.Context(), s.pool, s.nowUTC())
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusInternalServerError, "Failed to load proxy API key capacity")
		return
	}
	response := proxyAPIKeyListResponse{Items: make([]proxyAPIKeyResponse, 0, len(rows)), Capacity: capacity}
	for _, row := range rows {
		response.Items = append(response.Items, s.serializeProxyAPIKey(row))
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) handleCreateProxyKey(w http.ResponseWriter, r *http.Request) {
	setNoStoreHeaders(w)
	var requestBody proxyAPIKeyCreateRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	name, err := validateProxyKeyName(requestBody.Name)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	notes := normalizeNotes(requestBody.Notes)
	result, err := pgxutil.InTxValue(r.Context(), s.pool, "auth", func(tx pgx.Tx) (proxyAPIKeyMutationResponse, error) {
		rawKey, row, capacity, createErr := s.createProxyAPIKey(r.Context(), tx, name, notes, requestBody.ExpiresAt, authSubjectIDFromRequest(r))
		if createErr != nil {
			return proxyAPIKeyMutationResponse{}, createErr
		}
		return proxyAPIKeyMutationResponse{Key: rawKey, Item: s.serializeProxyAPIKey(row), Capacity: capacity}, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusCreated, result)
}

func (s *Service) handleUpdateProxyKey(w http.ResponseWriter, r *http.Request) {
	setNoStoreHeaders(w)
	keyID, err := routeInt(r, "key_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	var requestBody proxyAPIKeyUpdateRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	name, err := validateProxyKeyName(requestBody.Name)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	notes := normalizeNotes(requestBody.Notes)
	type updateResult struct {
		row      proxyAPIKeyRow
		capacity proxyKeyCapacitySnapshot
	}
	result, err := pgxutil.InTxValue(r.Context(), s.pool, "auth", func(tx pgx.Tx) (updateResult, error) {
		row, capacity, updateErr := s.updateProxyAPIKey(r.Context(), tx, keyID, name, notes, requestBody.IsActive, requestBody.ExpiresAt)
		if updateErr != nil {
			return updateResult{}, updateErr
		}
		return updateResult{row: row, capacity: capacity}, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, proxyAPIKeyUpdateResponse{Item: s.serializeProxyAPIKey(result.row), Capacity: result.capacity})
}

func (s *Service) handleRotateProxyKey(w http.ResponseWriter, r *http.Request) {
	setNoStoreHeaders(w)
	keyID, err := routeInt(r, "key_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	result, err := pgxutil.InTxValue(r.Context(), s.pool, "auth", func(tx pgx.Tx) (proxyAPIKeyMutationResponse, error) {
		rawKey, row, capacity, rotateErr := s.rotateProxyAPIKey(r.Context(), tx, keyID)
		if rotateErr != nil {
			return proxyAPIKeyMutationResponse{}, rotateErr
		}
		return proxyAPIKeyMutationResponse{Key: rawKey, Item: s.serializeProxyAPIKey(row), Capacity: capacity}, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, result)
}

func (s *Service) handleDeleteProxyKey(w http.ResponseWriter, r *http.Request) {
	setNoStoreHeaders(w)
	keyID, err := routeInt(r, "key_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	capacity, err := pgxutil.InTxValue(r.Context(), s.pool, "auth", func(tx pgx.Tx) (proxyKeyCapacitySnapshot, error) {
		return s.deleteProxyAPIKey(r.Context(), tx, keyID)
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, deletedResponse{DeletedID: keyID, Capacity: capacity})
}

func (s *Service) handleRuntimeProbe(w http.ResponseWriter, r *http.Request) {
	proxyKey, ok := runtimeProxyKeyFromRequest(r)
	if !ok || proxyKey.ID <= 0 {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusUnauthorized, "Proxy API key required")
		return
	}
	responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusNotImplemented, "Runtime proxy unavailable without a runtime service")
}

func decodeJSONBody(request *http.Request, target any) error {
	defer func() { _ = request.Body.Close() }()
	decoder := json.NewDecoder(request.Body)
	return decoder.Decode(target)
}

func decodeStrictJSONBody(request *http.Request, target any) error {
	defer func() { _ = request.Body.Close() }()
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("request body must contain one JSON value")
		}
		return err
	}
	return nil
}

func writeDomainError(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot, err error) {
	setNoStoreHeaders(w)
	if authErr, ok := errors.AsType[*domainError](err); ok {
		if authErr.Code != "" && (strings.HasPrefix(authErr.Code, "auth_") || authErr.Code == "invalid_operator_account" || authErr.Code == "operation_id_conflict") {
			platformcors.ApplyAllowOriginHeaders(w, r, corsSnapshot)
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			details := authErr.Fields["details"]
			if details == nil {
				details = map[string]any{}
			}
			w.WriteHeader(authErr.StatusCode)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code":       authErr.Code,
				"detail":     authErr.Detail,
				"params":     map[string]any{},
				"details":    details,
				"request_id": middleware.GetReqID(r.Context()),
			})
			return
		}
		responseutil.WriteErrorFields(w, r, corsSnapshot, authErr.StatusCode, authErr.Detail, domainErrorFields(authErr))
		return
	}
	slog.Error("auth handler internal error", "error", err)
	responseutil.WriteError(w, r, corsSnapshot, http.StatusInternalServerError, "Internal server error")
}

func domainErrorFields(err *domainError) map[string]any {
	fields := make(map[string]any, len(err.Fields)+1)
	if err.Code != "" {
		fields["code"] = err.Code
	}
	for key, value := range err.Fields {
		fields[key] = value
	}
	return fields
}

// writeAuthProblem writes a registered auth problem through the shared flat
// management envelope {code, detail, params, details, request_id} and sets
// the same-source Retry-After header for auth_login_locked. The wire params
// are always the registered exact empty object. Errors without a registered
// auth code keep the legacy writer until their owner converges them.
func writeAuthProblem(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot, err *domainError) {
	if err == nil {
		return
	}
	if _, ok := lookupAuthProblemEntry(err.Code); !ok {
		writeDomainError(w, r, corsSnapshot, err)
		return
	}
	params := authProblemParams(err.Code)
	var details any
	if err.Details != nil {
		details = err.Details
	}
	if err.Code == ProblemCodeAuthLoginLocked {
		if locked, ok := details.(AuthLoginLockedDetails); ok {
			w.Header().Set("Retry-After", fmt.Sprintf("%d", locked.RetryAfterSeconds))
		}
	}
	responseutil.WriteProblem(w, r, corsSnapshot, err.StatusCode, err.Code, err.Detail, params, details)
}

func transitionProblemCodeFor(state string) string {
	if state == "rollback_required" {
		return ProblemCodeAuthTransitionRecoveryNeeded
	}
	return ProblemCodeAuthTransitionInProgress
}

func transitionProblemDetailFor(state string) string {
	if state == "rollback_required" {
		return "正在恢复上一份有效配置"
	}
	return "正在安全启用，管理访问暂不可用"
}

// writeTransitionProblem writes the registered typed 503 for a persisted
// fail-closed auth transition, with the same-source optional Retry-After.
func writeTransitionProblem(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot, state string, generation int64, retryAfter *int64) {
	code := transitionProblemCodeFor(state)
	details := transitionProblemDetailsFor(state, generation, retryAfter)
	if retryAfter != nil {
		w.Header().Set("Retry-After", fmt.Sprintf("%d", *retryAfter))
	}
	responseutil.WriteProblem(w, r, corsSnapshot, http.StatusServiceUnavailable, code, transitionProblemDetailFor(state), authProblemParams(code), details)
}

func setNoStoreHeaders(w http.ResponseWriter) {
	// Create/rotate responses carry the one-time raw key: they must not be
	// cached by a reverse proxy, service worker or browser.
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Pragma", "no-cache")
}

func routeInt(request *http.Request, name string) (int, error) {
	value := strings.TrimSpace(chi.URLParam(request, name))
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("invalid %s", name)
	}
	return parsed, nil
}

func stringPointer(value string) *string {
	if value == "" {
		return nil
	}
	result := value
	return &result
}
