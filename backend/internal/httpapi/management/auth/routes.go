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
	"time"

	"github.com/coachpo/prism/backend/internal/httpapi/requestcontext"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	platformcors "github.com/coachpo/prism/backend/internal/platform/cors"
	"github.com/coachpo/prism/backend/internal/platform/email/outbox"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

var publicManagementPaths = map[string]struct{}{
	"/api/auth/status":                 {},
	"/api/auth/public-bootstrap":       {},
	"/api/auth/login":                  {},
	"/api/auth/logout":                 {},
	"/api/auth/refresh":                {},
	"/api/auth/password-reset/request": {},
	"/api/auth/password-reset/confirm": {},
	"/api/realtime/ws":                 {},
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
			recordAuthDecision(r.Context(), authTelemetryBranchManagement, "settings_error")
			writeError(w, r, s.corsSnapshot(), http.StatusInternalServerError, "Failed to load authentication settings")
			return
		}
		if !snapshot.AuthEnabled {
			recordAuthDecision(r.Context(), authTelemetryBranchManagement, "disabled")
			next.ServeHTTP(w, r)
			return
		}
		if isPublicManagementPath(r.URL.Path) {
			recordAuthDecision(r.Context(), authTelemetryBranchManagement, "public")
			next.ServeHTTP(w, r)
			return
		}
		authConfig := s.runtimeAuthConfigSnapshot()
		authSubject, ok := s.authSubjectFromAccessSnapshot(r, authConfig, snapshot)
		if !ok {
			recordAuthDecision(r.Context(), authTelemetryBranchManagement, "unauthenticated")
			writeError(w, r, s.corsSnapshot(), http.StatusUnauthorized, "Authentication required")
			return
		}
		contextWithSubject := context.WithValue(r.Context(), authSubjectContextKey{}, authSubject)
		recordAuthDecision(contextWithSubject, authTelemetryBranchManagement, "authenticated")
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
				recordAuthDecision(r.Context(), authTelemetryBranchRuntime, "snapshot_unavailable")
				writeError(w, r, s.corsSnapshot(), http.StatusServiceUnavailable, "Runtime authentication snapshot is unavailable. Retry later.")
				return
			}
			recordAuthDecision(r.Context(), authTelemetryBranchRuntime, "settings_error")
			writeError(w, r, s.corsSnapshot(), http.StatusInternalServerError, "Failed to load authentication settings")
			return
		}
		if !authSettings.AuthEnabled {
			recordAuthDecision(r.Context(), authTelemetryBranchRuntime, "disabled")
			next.ServeHTTP(w, r)
			return
		}
		rawKey, _ := extractProxyAPIKey(r.Header)
		if rawKey == "" {
			recordAuthDecision(r.Context(), authTelemetryBranchRuntime, "missing_proxy_key")
			writeError(w, r, s.corsSnapshot(), http.StatusUnauthorized, "Proxy API key required")
			return
		}
		proxyKey, err := s.verifyProxyAPIKey(r.Context(), rawKey)
		if err != nil {
			if isPublishedSnapshotUnavailable(err) {
				recordAuthDecision(r.Context(), authTelemetryBranchRuntime, "snapshot_unavailable")
				writeError(w, r, s.corsSnapshot(), http.StatusServiceUnavailable, "Runtime authentication snapshot is unavailable. Retry later.")
				return
			}
			recordAuthDecision(r.Context(), authTelemetryBranchRuntime, "verify_error")
			writeError(w, r, s.corsSnapshot(), http.StatusInternalServerError, "Failed to verify proxy API key")
			return
		}
		if proxyKey == nil {
			recordAuthDecision(r.Context(), authTelemetryBranchRuntime, "invalid_proxy_key")
			writeError(w, r, s.corsSnapshot(), http.StatusUnauthorized, "Invalid proxy API key")
			return
		}
		proxyKeySnapshot := requestcontext.RuntimeProxyKeySnapshot{
			ID:         proxyKey.ID,
			Name:       proxyKey.Name,
			LastUsedAt: s.nowUTC(),
			LastUsedIP: requestIP(r),
		}
		contextWithProxyKey := requestcontext.WithRuntimeProxyKey(r.Context(), proxyKeySnapshot)
		if s.runtimeCache == nil {
			if err := s.enqueueProxyAPIKeyUsage(proxyKey.ID, proxyKeySnapshot.LastUsedAt, proxyKeySnapshot.LastUsedIP); err != nil {
				slog.Error("failed to enqueue proxy API key usage", "error", err, "key_id", proxyKey.ID)
			}
		}
		recordAuthDecision(contextWithProxyKey, authTelemetryBranchRuntime, "authenticated")
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
		writeError(w, r, s.corsSnapshot(), http.StatusInternalServerError, "Failed to load authentication settings")
		return
	}
	writeJSON(w, http.StatusOK, authStatusResponse{AuthEnabled: settingsRow.AuthEnabled})
}

func (s *Service) handleGetPublicBootstrap(w http.ResponseWriter, r *http.Request) {
	authConfig := s.runtimeAuthConfigSnapshot()
	settingsRow, err := s.loadOrCreateAppAuthSettings(r.Context(), s.pool)
	if err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusInternalServerError, "Failed to load authentication settings")
		return
	}
	if !settingsRow.AuthEnabled {
		s.clearAuthCookies(w, authConfig)
		writeJSON(w, http.StatusOK, sessionResponse{Authenticated: false, AuthEnabled: false, Username: nil})
		return
	}
	if authSubject, ok := s.authSubjectFromAccessCookie(r, authConfig, settingsRow); ok {
		writeJSON(w, http.StatusOK, sessionResponse{Authenticated: true, AuthEnabled: true, Username: stringPointer(authSubject.Username)})
		return
	}
	bundle, err := s.withRefreshCookie(r.Context(), r, authConfig)
	if err != nil {
		var authErr *domainError
		if errors.As(err, &authErr) && authErr.StatusCode == http.StatusUnauthorized {
			s.clearAuthCookies(w, authConfig)
			writeJSON(w, http.StatusOK, sessionResponse{Authenticated: false, AuthEnabled: true, Username: nil})
			return
		}
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	s.setAuthCookies(w, authConfig, bundle.AccessToken, bundle.RefreshToken, bundle.RefreshExpiresAt, bundle.SessionDuration)
	writeJSON(w, http.StatusOK, sessionResponse{Authenticated: true, AuthEnabled: true, Username: nullableString(bundle.SettingsRow.Username)})
}

func (s *Service) handleLogin(w http.ResponseWriter, r *http.Request) {
	authConfig := s.runtimeAuthConfigSnapshot()
	var requestBody loginRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	duration, err := normalizeSessionDuration(requestBody.SessionDuration)
	if err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid session duration")
		return
	}
	bundle, err := pgxutil.InTxValue(r.Context(), s.pool, "auth", func(tx pgx.Tx) (sessionBundle, error) {
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
	s.setAuthCookies(w, authConfig, bundle.AccessToken, bundle.RefreshToken, bundle.RefreshExpiresAt, bundle.SessionDuration)
	writeJSON(w, http.StatusOK, sessionResponse{Authenticated: true, AuthEnabled: bundle.SettingsRow.AuthEnabled, Username: nullableString(bundle.SettingsRow.Username)})
}

func (s *Service) handleLogout(w http.ResponseWriter, r *http.Request) {
	authConfig := s.runtimeAuthConfigSnapshot()
	if err := pgxutil.InTx(r.Context(), s.pool, "auth", func(tx pgx.Tx) error {
		cookie, cookieErr := r.Cookie(authConfig.RefreshCookieName)
		if cookieErr == nil && strings.TrimSpace(cookie.Value) != "" {
			return s.revokeRefreshToken(r.Context(), tx, cookie.Value)
		}
		return nil
	}); err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	settingsRow, err := s.loadOrCreateAppAuthSettings(r.Context(), s.pool)
	if err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusInternalServerError, "Failed to load authentication settings")
		return
	}
	s.clearAuthCookies(w, authConfig)
	writeJSON(w, http.StatusOK, sessionResponse{Authenticated: false, AuthEnabled: settingsRow.AuthEnabled, Username: nil})
}

func (s *Service) handleRefresh(w http.ResponseWriter, r *http.Request) {
	authConfig := s.runtimeAuthConfigSnapshot()
	bundle, err := s.withRefreshCookie(r.Context(), r, authConfig)
	if err != nil {
		var authErr *domainError
		if errors.As(err, &authErr) && authErr.StatusCode == http.StatusUnauthorized {
			s.clearAuthCookies(w, authConfig)
			writeJSON(w, http.StatusOK, sessionResponse{Authenticated: false, AuthEnabled: true, Username: nil})
			return
		}
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	s.setAuthCookies(w, authConfig, bundle.AccessToken, bundle.RefreshToken, bundle.RefreshExpiresAt, bundle.SessionDuration)
	writeJSON(w, http.StatusOK, sessionResponse{Authenticated: true, AuthEnabled: bundle.SettingsRow.AuthEnabled, Username: nullableString(bundle.SettingsRow.Username)})
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
		writeError(w, r, s.corsSnapshot(), http.StatusUnauthorized, "Authentication required")
		return
	}
	settingsRow, err := s.loadOrCreateAppAuthSettings(r.Context(), s.pool)
	if err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusInternalServerError, "Failed to load authentication settings")
		return
	}
	username := authSubject.Username
	if username == "" {
		username = stringValue(settingsRow.Username)
	}
	writeJSON(w, http.StatusOK, sessionResponse{Authenticated: true, AuthEnabled: settingsRow.AuthEnabled, Username: stringPointer(username)})
}

func (s *Service) handlePasswordResetRequest(w http.ResponseWriter, r *http.Request) {
	authConfig := s.runtimeAuthConfigSnapshot()
	var requestBody passwordResetRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	identifier := strings.TrimSpace(requestBody.UsernameOrEmail)
	result, err := pgxutil.InTxValue(r.Context(), s.pool, "auth", func(tx pgx.Tx) (struct {
		SettingsRow appAuthSettingsRow
		ShouldSend  bool
	}, error) {
		settingsRow, loadErr := s.loadOrCreateAppAuthSettings(r.Context(), tx)
		if loadErr != nil {
			return struct {
				SettingsRow appAuthSettingsRow
				ShouldSend  bool
			}{}, fmt.Errorf("load auth settings: %w", loadErr)
		}
		if !settingsRow.AuthEnabled || !settingsRow.Email.Valid || identifier == "" {
			return struct {
				SettingsRow appAuthSettingsRow
				ShouldSend  bool
			}{SettingsRow: settingsRow}, nil
		}
		allowedIdentifiers := map[string]struct{}{}
		if settingsRow.Username.Valid {
			allowedIdentifiers[settingsRow.Username.String] = struct{}{}
		}
		allowedIdentifiers[settingsRow.Email.String] = struct{}{}
		if _, ok := allowedIdentifiers[identifier]; !ok {
			return struct {
				SettingsRow appAuthSettingsRow
				ShouldSend  bool
			}{SettingsRow: settingsRow}, nil
		}
		otpCode, challengeID, expiresAt, createErr := s.createPasswordResetChallenge(r.Context(), tx, authConfig, settingsRow, requestIP(r))
		if createErr != nil {
			return struct {
				SettingsRow appAuthSettingsRow
				ShouldSend  bool
			}{}, createErr
		}
		if enqueueErr := s.enqueueAuthEmail(r.Context(), tx, outbox.Job{
			Kind:           outbox.KindPasswordReset,
			RecipientEmail: settingsRow.Email.String,
			Template:       outbox.TemplatePasswordReset,
			Secret:         otpCode,
			IdempotencyKey: fmt.Sprintf("password_reset:%d:%d", settingsRow.ID, challengeID),
			Payload: map[string]any{
				"auth_subject_id": settingsRow.ID,
				"challenge_id":    challengeID,
				"expires_at":      expiresAt.UTC().Format(time.RFC3339),
			},
		}); enqueueErr != nil {
			return struct {
				SettingsRow appAuthSettingsRow
				ShouldSend  bool
			}{}, enqueueErr
		}
		return struct {
			SettingsRow appAuthSettingsRow
			ShouldSend  bool
		}{SettingsRow: settingsRow, ShouldSend: true}, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	_ = result
	writeJSON(w, http.StatusOK, successResponse{Success: true})
}

func (s *Service) handlePasswordResetConfirm(w http.ResponseWriter, r *http.Request) {
	authConfig := s.runtimeAuthConfigSnapshot()
	var requestBody passwordResetConfirmRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	if err := pgxutil.InTx(r.Context(), s.pool, "auth", func(tx pgx.Tx) error {
		return s.consumePasswordResetChallenge(r.Context(), tx, strings.TrimSpace(requestBody.OTPCode), requestBody.NewPassword)
	}); err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	s.invalidateAppAuthSettingsSnapshot()
	s.clearAuthCookies(w, authConfig)
	writeJSON(w, http.StatusOK, successResponse{Success: true})
}

func (s *Service) handleGetAuthSettings(w http.ResponseWriter, r *http.Request) {
	settingsRow, err := s.loadOrCreateAppAuthSettings(r.Context(), s.pool)
	if err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusInternalServerError, "Failed to load authentication settings")
		return
	}
	writeJSON(w, http.StatusOK, s.buildAuthSettingsResponse(settingsRow))
}

func (s *Service) handlePutAuthSettings(w http.ResponseWriter, r *http.Request) {
	authConfig := s.runtimeAuthConfigSnapshot()
	var requestBody authSettingsUpdateRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
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
	writeJSON(w, http.StatusOK, s.buildAuthSettingsResponse(result.Row))
}

func (s *Service) handleEmailVerificationRequest(w http.ResponseWriter, r *http.Request) {
	authConfig := s.runtimeAuthConfigSnapshot()
	var requestBody emailVerificationRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	email, err := validateEmail(requestBody.Email)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	var updatedRow appAuthSettingsRow
	err = pgxutil.InTx(r.Context(), s.pool, "auth", func(tx pgx.Tx) error {
		settingsRow, loadErr := s.loadOrCreateAppAuthSettings(r.Context(), tx)
		if loadErr != nil {
			return fmt.Errorf("load auth settings: %w", loadErr)
		}
		currentRow, otpCode, beginErr := s.beginEmailVerification(r.Context(), tx, authConfig, settingsRow, email)
		if beginErr != nil {
			return beginErr
		}
		if enqueueErr := s.enqueueAuthEmail(r.Context(), tx, outbox.Job{
			Kind:           outbox.KindEmailVerificationOTP,
			RecipientEmail: email,
			Template:       outbox.TemplateEmailVerificationOTP,
			Secret:         otpCode,
			IdempotencyKey: fmt.Sprintf("email_verification_otp:%d:%d", currentRow.ID, currentRow.EmailVerificationExpiresAt.Time.UnixNano()),
			Payload: map[string]any{
				"auth_subject_id": currentRow.ID,
				"expires_at":      currentRow.EmailVerificationExpiresAt.Time.UTC().Format(time.RFC3339),
			},
		}); enqueueErr != nil {
			return enqueueErr
		}
		updatedRow = currentRow
		return nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeJSON(w, http.StatusOK, s.buildEmailVerificationResponse(updatedRow))
}

func (s *Service) enqueueAuthEmail(ctx context.Context, tx pgx.Tx, job outbox.Job) error {
	if s == nil || s.emailOutbox == nil {
		return fmt.Errorf("email outbox unavailable")
	}
	if _, err := s.emailOutbox.EnqueueTx(ctx, tx, job); err != nil {
		return fmt.Errorf("enqueue auth email outbox job: %w", err)
	}
	return nil
}

func (s *Service) handleEmailVerificationConfirm(w http.ResponseWriter, r *http.Request) {
	var requestBody emailVerificationConfirmRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	updatedRow, err := pgxutil.InTxValue(r.Context(), s.pool, "auth", func(tx pgx.Tx) (appAuthSettingsRow, error) {
		settingsRow, loadErr := s.loadOrCreateAppAuthSettings(r.Context(), tx)
		if loadErr != nil {
			return appAuthSettingsRow{}, fmt.Errorf("load auth settings: %w", loadErr)
		}
		return s.confirmEmailVerification(r.Context(), tx, settingsRow, strings.TrimSpace(requestBody.OTPCode))
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeJSON(w, http.StatusOK, s.buildEmailVerificationResponse(updatedRow))
}

func (s *Service) handleListProxyKeys(w http.ResponseWriter, r *http.Request) {
	rows, err := s.listProxyAPIKeys(r.Context(), s.pool)
	if err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusInternalServerError, "Failed to load proxy API keys")
		return
	}
	response := make([]proxyAPIKeyResponse, 0, len(rows))
	for _, row := range rows {
		response = append(response, s.serializeProxyAPIKey(row))
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleCreateProxyKey(w http.ResponseWriter, r *http.Request) {
	var requestBody proxyAPIKeyCreateRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	name, err := validateProxyKeyName(requestBody.Name)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	notes := normalizeNotes(requestBody.Notes)
	result, err := pgxutil.InTxValue(r.Context(), s.pool, "auth", func(tx pgx.Tx) (proxyAPIKeyMutationResponse, error) {
		rawKey, row, createErr := s.createProxyAPIKey(r.Context(), tx, name, notes, requestBody.ExpiresAt, authSubjectIDFromRequest(r))
		if createErr != nil {
			return proxyAPIKeyMutationResponse{}, createErr
		}
		return proxyAPIKeyMutationResponse{Key: rawKey, Item: s.serializeProxyAPIKey(row)}, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (s *Service) handleUpdateProxyKey(w http.ResponseWriter, r *http.Request) {
	keyID, err := routeInt(r, "key_id")
	if err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	var requestBody proxyAPIKeyUpdateRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	name, err := validateProxyKeyName(requestBody.Name)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	notes := normalizeNotes(requestBody.Notes)
	updatedRow, err := pgxutil.InTxValue(r.Context(), s.pool, "auth", func(tx pgx.Tx) (proxyAPIKeyRow, error) {
		return s.updateProxyAPIKey(r.Context(), tx, keyID, name, notes, requestBody.IsActive, requestBody.ExpiresAt)
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeJSON(w, http.StatusOK, s.serializeProxyAPIKey(updatedRow))
}

func (s *Service) handleRotateProxyKey(w http.ResponseWriter, r *http.Request) {
	keyID, err := routeInt(r, "key_id")
	if err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	result, err := pgxutil.InTxValue(r.Context(), s.pool, "auth", func(tx pgx.Tx) (proxyAPIKeyMutationResponse, error) {
		rawKey, row, rotateErr := s.rotateProxyAPIKey(r.Context(), tx, keyID)
		if rotateErr != nil {
			return proxyAPIKeyMutationResponse{}, rotateErr
		}
		return proxyAPIKeyMutationResponse{Key: rawKey, Item: s.serializeProxyAPIKey(row)}, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Service) handleDeleteProxyKey(w http.ResponseWriter, r *http.Request) {
	keyID, err := routeInt(r, "key_id")
	if err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	if err := pgxutil.InTx(r.Context(), s.pool, "auth", func(tx pgx.Tx) error {
		return s.deleteProxyAPIKey(r.Context(), tx, keyID)
	}); err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeJSON(w, http.StatusOK, deletedResponse{Deleted: true})
}

func (s *Service) handleRuntimeProbe(w http.ResponseWriter, r *http.Request) {
	proxyKey, ok := runtimeProxyKeyFromRequest(r)
	if !ok || proxyKey.ID <= 0 {
		recordAuthDecision(r.Context(), authTelemetryBranchRuntime, "missing_proxy_key")
		writeError(w, r, s.corsSnapshot(), http.StatusUnauthorized, "Proxy API key required")
		return
	}
	writeError(w, r, s.corsSnapshot(), http.StatusNotImplemented, "Runtime proxy unavailable without a runtime service")
}

func decodeJSONBody(request *http.Request, target any) error {
	defer func() { _ = request.Body.Close() }()
	decoder := json.NewDecoder(request.Body)
	return decoder.Decode(target)
}

func writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeDomainError(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot, err error) {
	if authErr, ok := errors.AsType[*domainError](err); ok {
		writeError(w, r, corsSnapshot, authErr.StatusCode, authErr.Detail)
		return
	}
	writeError(w, r, corsSnapshot, http.StatusInternalServerError, "Internal server error")
}

func writeError(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot, statusCode int, detail string) {
	platformcors.ApplyAllowOriginHeaders(w, r, corsSnapshot)
	writeJSON(w, statusCode, map[string]string{"detail": detail})
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
