package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	"github.com/jackc/pgx/v5"
)

func (s *Service) authSubjectFromAccessCookie(request *http.Request, authConfig RuntimeAuthConfigSnapshot, settingsRow appAuthSettingsRow) (authSubject, bool) {
	return s.authSubjectFromAccessSnapshot(request, authConfig, appAuthSettingsSnapshotFromRow(settingsRow))
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

func stringPointer(value string) *string {
	if value == "" {
		return nil
	}
	result := value
	return &result
}
