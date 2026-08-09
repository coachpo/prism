package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/httpapi/requestcontext"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	platformcors "github.com/coachpo/prism/backend/internal/platform/cors"
	"github.com/go-chi/chi/v5"
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
		if !snapshot.AuthEnabled {
			next.ServeHTTP(w, r)
			return
		}
		if isPublicManagementPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		authConfig := s.runtimeAuthConfigSnapshot()
		authSubject, ok := s.authSubjectFromAccessSnapshot(r, authConfig, snapshot)
		if !ok {
			responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusUnauthorized, "Authentication required")
			return
		}
		contextWithSubject := context.WithValue(r.Context(), authSubjectContextKey{}, authSubject)
		next.ServeHTTP(w, r.WithContext(contextWithSubject))
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
			if isPublishedSnapshotUnavailable(verifyErr) {
				if authEnforced {
					responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusServiceUnavailable, "Runtime authentication snapshot is unavailable. Retry later.")
					return
				}
				// Permissive optional verification failure: fail open for
				// execution, fail closed for identity. The request continues
				// as unknown; lookup details are never disclosed.
				slog.Warn("proxy key optional verification unavailable; attribution unknown", "key_id", 0)
				attribution := requestcontext.RuntimeProxyKeyAttribution{
					State:        requestcontext.RuntimeProxyKeyUnknown,
					Snapshot:     nil,
					AuthEnforced: false,
				}
				next.ServeHTTP(w, r.WithContext(requestcontext.WithRuntimeProxyKeyAttribution(r.Context(), attribution)))
				return
			}
			responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusInternalServerError, "Failed to verify proxy API key")
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
	settingsRow, err := s.loadOrCreateAppAuthSettings(r.Context(), s.pool)
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusInternalServerError, "Failed to load authentication settings")
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, authStatusResponse{AuthEnabled: settingsRow.AuthEnabled})
}

func (s *Service) handleGetPublicBootstrap(w http.ResponseWriter, r *http.Request) {
	authConfig := s.runtimeAuthConfigSnapshot()
	settingsRow, err := s.loadOrCreateAppAuthSettings(r.Context(), s.pool)
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusInternalServerError, "Failed to load authentication settings")
		return
	}
	if !settingsRow.AuthEnabled {
		s.clearAuthCookies(w, authConfig)
		responseutil.WriteJSON(w, http.StatusOK, sessionResponse{Authenticated: false, AuthEnabled: false, Username: nil})
		return
	}
	if authSubject, ok := s.authSubjectFromAccessCookie(r, authConfig, settingsRow); ok {
		responseutil.WriteJSON(w, http.StatusOK, sessionResponse{Authenticated: true, AuthEnabled: true, Username: stringPointer(authSubject.Username)})
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
	responseutil.WriteJSON(w, http.StatusOK, sessionResponse{Authenticated: true, AuthEnabled: true, Username: nullableString(bundle.SettingsRow.Username)})
}

func (s *Service) handleLogin(w http.ResponseWriter, r *http.Request) {
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
		writeDomainError(w, r, s.corsSnapshot(), result.DomainErr)
		return
	}
	bundle := result.Bundle
	s.setAuthCookies(w, authConfig, bundle.AccessToken, bundle.RefreshToken, bundle.RefreshExpiresAt, bundle.SessionDuration)
	responseutil.WriteJSON(w, http.StatusOK, sessionResponse{Authenticated: true, AuthEnabled: bundle.SettingsRow.AuthEnabled, Username: nullableString(bundle.SettingsRow.Username)})
}

func (s *Service) handleLogout(w http.ResponseWriter, r *http.Request) {
	authConfig := s.runtimeAuthConfigSnapshot()
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
	settingsRow, err := s.loadOrCreateAppAuthSettings(r.Context(), s.pool)
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusInternalServerError, "Failed to load authentication settings")
		return
	}
	s.clearAuthCookies(w, authConfig)
	responseutil.WriteJSON(w, http.StatusOK, sessionResponse{Authenticated: false, AuthEnabled: settingsRow.AuthEnabled, Username: nil})
}

func (s *Service) handleRefresh(w http.ResponseWriter, r *http.Request) {
	authConfig := s.runtimeAuthConfigSnapshot()
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
	responseutil.WriteJSON(w, http.StatusOK, sessionResponse{Authenticated: true, AuthEnabled: bundle.SettingsRow.AuthEnabled, Username: nullableString(bundle.SettingsRow.Username)})
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
	responseutil.WriteJSON(w, http.StatusOK, sessionResponse{Authenticated: true, AuthEnabled: settingsRow.AuthEnabled, Username: stringPointer(username)})
}

func (s *Service) handleGetAuthSettings(w http.ResponseWriter, r *http.Request) {
	settingsRow, err := s.loadOrCreateAppAuthSettings(r.Context(), s.pool)
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusInternalServerError, "Failed to load authentication settings")
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, s.buildAuthSettingsResponse(settingsRow))
}

func (s *Service) handlePutAuthSettings(w http.ResponseWriter, r *http.Request) {
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
	responseutil.WriteJSON(w, http.StatusOK, s.buildAuthSettingsResponse(result.Row))
}

func (s *Service) handleListProxyKeys(w http.ResponseWriter, r *http.Request) {
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
	setNoStoreHeaders(w)
	responseutil.WriteJSON(w, http.StatusCreated, result)
}

func (s *Service) handleUpdateProxyKey(w http.ResponseWriter, r *http.Request) {
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
	setNoStoreHeaders(w)
	responseutil.WriteJSON(w, http.StatusOK, result)
}

func (s *Service) handleDeleteProxyKey(w http.ResponseWriter, r *http.Request) {
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

func writeDomainError(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot, err error) {
	if authErr, ok := errors.AsType[*domainError](err); ok {
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
