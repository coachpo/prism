package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	"github.com/jackc/pgx/v5"
)

func requestIP(request *http.Request) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(request.RemoteAddr))
	if err == nil {
		return host
	}
	return strings.TrimSpace(request.RemoteAddr)
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
		return responseutil.SanitizeDecodeError(err)
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

func setNoStoreHeaders(w http.ResponseWriter) {
	// Create/rotate responses carry the one-time raw key: they must not be
	// cached by a reverse proxy, service worker or browser.
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Pragma", "no-cache")
}
