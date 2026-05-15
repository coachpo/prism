package sidecars

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	platformcors "github.com/coachpo/prism/backend/internal/platform/cors"
)

const credentialMask = "********"

type sidecarCreateRequest struct {
	Name                  string  `json:"name"`
	BaseURL               string  `json:"base_url"`
	ManagementPassword    string  `json:"management_password"`
	Enabled               *bool   `json:"enabled"`
	EnvironmentLabel      *string `json:"environment_label"`
	AllowPrivateNetwork   *bool   `json:"allow_private_network"`
	AllowInsecureHTTP     *bool   `json:"allow_insecure_http"`
	SkipTLSVerify         *bool   `json:"skip_tls_verify"`
	SyncIntervalSeconds   *int    `json:"sync_interval_seconds"`
	RequestTimeoutSeconds *int    `json:"request_timeout_seconds"`
}

type sidecarUpdateRequest struct {
	Name                  optionalString `json:"name"`
	BaseURL               optionalString `json:"base_url"`
	ManagementPassword    optionalString `json:"management_password"`
	Enabled               optionalBool   `json:"enabled"`
	EnvironmentLabel      optionalString `json:"environment_label"`
	AllowPrivateNetwork   optionalBool   `json:"allow_private_network"`
	AllowInsecureHTTP     optionalBool   `json:"allow_insecure_http"`
	SkipTLSVerify         optionalBool   `json:"skip_tls_verify"`
	SyncIntervalSeconds   optionalInt    `json:"sync_interval_seconds"`
	RequestTimeoutSeconds optionalInt    `json:"request_timeout_seconds"`
}

type optionalString struct {
	Set   bool
	Value *string
}

func (value *optionalString) UnmarshalJSON(data []byte) error {
	value.Set = true
	trimmed := bytes.TrimSpace(data)
	if bytes.Equal(trimmed, []byte("null")) {
		value.Value = nil
		return nil
	}
	var parsed string
	if err := json.Unmarshal(trimmed, &parsed); err != nil {
		return err
	}
	value.Value = &parsed
	return nil
}

type optionalBool struct {
	Set   bool
	Value *bool
}

func (value *optionalBool) UnmarshalJSON(data []byte) error {
	value.Set = true
	trimmed := bytes.TrimSpace(data)
	if bytes.Equal(trimmed, []byte("null")) {
		value.Value = nil
		return nil
	}
	var parsed bool
	if err := json.Unmarshal(trimmed, &parsed); err != nil {
		return err
	}
	value.Value = &parsed
	return nil
}

type optionalInt struct {
	Set   bool
	Value *int
}

func (value *optionalInt) UnmarshalJSON(data []byte) error {
	value.Set = true
	trimmed := bytes.TrimSpace(data)
	if bytes.Equal(trimmed, []byte("null")) {
		value.Value = nil
		return nil
	}
	var parsed int
	if err := json.Unmarshal(trimmed, &parsed); err != nil {
		return err
	}
	value.Value = &parsed
	return nil
}

type sidecarListResponse struct {
	Items []sidecarInstanceResponse `json:"items"`
}

type sidecarInstanceResponse struct {
	ID                    int                       `json:"id"`
	Name                  string                    `json:"name"`
	BaseURL               string                    `json:"base_url"`
	BaseURLCanonical      string                    `json:"base_url_canonical"`
	Enabled               bool                      `json:"enabled"`
	EnvironmentLabel      *string                   `json:"environment_label,omitempty"`
	AllowPrivateNetwork   bool                      `json:"allow_private_network"`
	AllowInsecureHTTP     bool                      `json:"allow_insecure_http"`
	SkipTLSVerify         bool                      `json:"skip_tls_verify"`
	SyncIntervalSeconds   int                       `json:"sync_interval_seconds"`
	RequestTimeoutSeconds int                       `json:"request_timeout_seconds"`
	CredentialState       sidecarCredentialResponse `json:"credential_state"`
	ManagementAuthState   string                    `json:"management_auth_state"`
	PauseMetadata         *sidecarPauseResponse     `json:"pause_metadata,omitempty"`
	LastSyncAt            *time.Time                `json:"last_sync_at,omitempty"`
	LastSuccessfulSyncAt  *time.Time                `json:"last_successful_sync_at,omitempty"`
	SnapshotStaleAfter    *time.Time                `json:"snapshot_stale_after,omitempty"`
	LastSyncError         *string                   `json:"last_sync_error,omitempty"`
	CreatedAt             time.Time                 `json:"created_at"`
	UpdatedAt             time.Time                 `json:"updated_at"`
}

type sidecarCredentialResponse struct {
	ManagementPasswordConfigured bool    `json:"management_password_configured"`
	ManagementPasswordMasked     *string `json:"management_password,omitempty"`
}

type sidecarPauseResponse struct {
	Reason      string     `json:"reason"`
	PausedUntil *time.Time `json:"paused_until,omitempty"`
}

type sidecarTestConnectionResponse struct {
	State               string `json:"state"`
	ManagementAuthState string `json:"management_auth_state"`
	StatusCode          int    `json:"status_code"`
}

type authSnapshotListResponse struct {
	Items []authSnapshotResponse `json:"items"`
}

type authSnapshotResponse struct {
	ID                 int             `json:"id"`
	SidecarID          int             `json:"sidecar_id"`
	AuthID             string          `json:"auth_id"`
	AuthIndex          *string         `json:"auth_index,omitempty"`
	Name               string          `json:"name"`
	Provider           *string         `json:"provider,omitempty"`
	Label              *string         `json:"label,omitempty"`
	Status             *string         `json:"status,omitempty"`
	StatusMessage      *string         `json:"status_message,omitempty"`
	Disabled           *bool           `json:"disabled,omitempty"`
	Unavailable        *bool           `json:"unavailable,omitempty"`
	Priority           *int            `json:"priority,omitempty"`
	QuotaExceeded      *bool           `json:"quota_exceeded,omitempty"`
	QuotaReason        *string         `json:"quota_reason,omitempty"`
	QuotaNextRecoverAt *time.Time      `json:"quota_next_recover_at,omitempty"`
	NextRetryAfter     *time.Time      `json:"next_retry_after,omitempty"`
	SuccessCount       *int            `json:"success_count,omitempty"`
	FailedCount        *int            `json:"failed_count,omitempty"`
	RecentRequests     json.RawMessage `json:"recent_requests,omitempty"`
	ModelStates        json.RawMessage `json:"model_states,omitempty"`
	ObservedAt         time.Time       `json:"observed_at"`
	Snapshot           json.RawMessage `json:"snapshot,omitempty"`
}

type providerSnapshotListResponse struct {
	Items []providerSnapshotResponse `json:"items"`
}

type providerSnapshotResponse struct {
	ID              int             `json:"id"`
	SidecarID       int             `json:"sidecar_id"`
	ProviderKey     string          `json:"provider_key"`
	ProviderItemKey string          `json:"provider_item_key"`
	Name            *string         `json:"name,omitempty"`
	Label           *string         `json:"label,omitempty"`
	Status          *string         `json:"status,omitempty"`
	Disabled        *bool           `json:"disabled,omitempty"`
	ObservedAt      time.Time       `json:"observed_at"`
	Snapshot        json.RawMessage `json:"snapshot,omitempty"`
}

func (s *Service) handleListSidecars(w http.ResponseWriter, r *http.Request) {
	instances, err := s.store.ListSidecarInstances(r.Context())
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	items := make([]sidecarInstanceResponse, 0, len(instances))
	for _, instance := range instances {
		items = append(items, buildSidecarInstanceResponse(instance))
	}
	writeJSON(w, http.StatusOK, sidecarListResponse{Items: items})
}

func (s *Service) handleCreateSidecar(w http.ResponseWriter, r *http.Request) {
	var requestBody sidecarCreateRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	input, err := buildCreateInput(requestBody)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	created, err := s.store.CreateSidecarInstance(r.Context(), input)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	s.recordAction(r.Context(), created.ID, "instance.create", "succeeded", nil)
	writeJSON(w, http.StatusCreated, buildSidecarInstanceResponse(created))
}

func (s *Service) handleGetSidecar(w http.ResponseWriter, r *http.Request) {
	id, ok := s.parseSidecarID(w, r)
	if !ok {
		return
	}
	instance, found, err := s.store.GetSidecarInstance(r.Context(), id)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	if !found {
		writeError(w, r, s.corsSnapshot(), http.StatusNotFound, "sidecar not found")
		return
	}
	writeJSON(w, http.StatusOK, buildSidecarInstanceResponse(instance))
}

func (s *Service) handleUpdateSidecar(w http.ResponseWriter, r *http.Request) {
	id, ok := s.parseSidecarID(w, r)
	if !ok {
		return
	}
	var requestBody sidecarUpdateRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	existing, found, err := s.store.GetSidecarInstance(r.Context(), id)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	if !found {
		writeError(w, r, s.corsSnapshot(), http.StatusNotFound, "sidecar not found")
		return
	}
	input, credentialUpdated, err := buildUpdateInput(existing, requestBody)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	updated, err := s.store.UpdateSidecarInstance(r.Context(), id, input)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	if credentialUpdated {
		s.recordAction(r.Context(), updated.ID, "credential.update", "succeeded", nil)
	} else {
		s.recordAction(r.Context(), updated.ID, "instance.update", "succeeded", nil)
	}
	writeJSON(w, http.StatusOK, buildSidecarInstanceResponse(updated))
}

func (s *Service) handleDeleteSidecar(w http.ResponseWriter, r *http.Request) {
	id, ok := s.parseSidecarID(w, r)
	if !ok {
		return
	}
	deleted, err := s.store.SoftDeleteSidecarInstance(r.Context(), id)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	if !deleted {
		writeError(w, r, s.corsSnapshot(), http.StatusNotFound, "sidecar not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) handleTestSidecarConnection(w http.ResponseWriter, r *http.Request) {
	id, ok := s.parseSidecarID(w, r)
	if !ok {
		return
	}
	instance, found, err := s.store.GetSidecarInstance(r.Context(), id)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	if !found {
		writeError(w, r, s.corsSnapshot(), http.StatusNotFound, "sidecar not found")
		return
	}
	password, err := s.decryptManagementPassword(instance.EncryptedManagementPassword)
	if err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusInternalServerError, "management password could not be read")
		return
	}
	if strings.TrimSpace(password) == "" {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "management password is not configured")
		return
	}
	target := CLIProxyTarget{BaseURL: instance.BaseURLCanonical, ManagementPassword: password, AllowPrivateNetwork: instance.AllowPrivateNetwork, AllowInsecureHTTP: instance.AllowInsecureHTTP, SkipTLSVerify: instance.SkipTLSVerify, RequestTimeoutSeconds: instance.RequestTimeoutSeconds}
	var payload map[string]any
	result, err := s.cliProxyClient.FetchJSON(r.Context(), target, http.MethodGet, "/auth-files", nil, &payload)
	if err != nil {
		s.handleConnectionTestFailure(w, r, instance, err)
		return
	}
	updated := s.markConnectionSuccess(r.Context(), instance)
	s.recordAction(r.Context(), instance.ID, "connection.test", "succeeded", nil)
	writeJSON(w, http.StatusOK, sidecarTestConnectionResponse{State: "succeeded", ManagementAuthState: updated.ManagementAuthState, StatusCode: result.StatusCode})
}

func (s *Service) handleConnectionTestFailure(w http.ResponseWriter, r *http.Request, instance SidecarInstance, err error) {
	var clientErr *CLIProxyClientError
	if errors.As(err, &clientErr) && clientErr.Code == CLIProxyErrorInvalidManagementAuth {
		s.markInvalidManagementAuth(r.Context(), instance)
		reason := "management authentication failed"
		s.recordAction(r.Context(), instance.ID, "connection.test", "failed", &reason)
		writeError(w, r, s.corsSnapshot(), http.StatusFailedDependency, "sidecar management authentication failed; update credentials or run a manual test before automated retries")
		return
	}
	reason := "connection test failed"
	s.recordAction(r.Context(), instance.ID, "connection.test", "failed", &reason)
	writeError(w, r, s.corsSnapshot(), http.StatusBadGateway, "sidecar connection test failed")
}

func (s *Service) handleGetAuthSnapshot(w http.ResponseWriter, r *http.Request) {
	id, ok := s.parseSidecarID(w, r)
	if !ok || !s.ensureSidecarExists(w, r, id) {
		return
	}
	snapshot, found, err := s.store.GetAuthSnapshot(r.Context(), id, strings.TrimSpace(chi.URLParam(r, "snapshot_id")))
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	if !found {
		writeError(w, r, s.corsSnapshot(), http.StatusNotFound, "auth snapshot not found")
		return
	}
	writeJSON(w, http.StatusOK, buildAuthSnapshotResponse(snapshot))
}

func buildCreateInput(requestBody sidecarCreateRequest) (SidecarInstanceInput, error) {
	name, err := normalizeSidecarName(requestBody.Name)
	if err != nil {
		return SidecarInstanceInput{}, err
	}
	if strings.TrimSpace(requestBody.ManagementPassword) == "" {
		return SidecarInstanceInput{}, &domainError{StatusCode: http.StatusBadRequest, Detail: "management_password is required"}
	}
	allowPrivate := boolValue(requestBody.AllowPrivateNetwork, false)
	allowInsecure := boolValue(requestBody.AllowInsecureHTTP, false)
	normalizedURL, err := normalizeSidecarBaseURL(requestBody.BaseURL, allowPrivate, allowInsecure)
	if err != nil {
		return SidecarInstanceInput{}, err
	}
	syncInterval, err := positiveSecondsOrDefault(requestBody.SyncIntervalSeconds, DefaultSyncIntervalSeconds, "sync_interval_seconds")
	if err != nil {
		return SidecarInstanceInput{}, err
	}
	requestTimeout, err := positiveSecondsOrDefault(requestBody.RequestTimeoutSeconds, DefaultRequestTimeoutSeconds, "request_timeout_seconds")
	if err != nil {
		return SidecarInstanceInput{}, err
	}
	enabled := boolValue(requestBody.Enabled, true)
	return SidecarInstanceInput{Name: name, BaseURL: normalizedURL, BaseURLCanonical: normalizedURL, ManagementPassword: requestBody.ManagementPassword, Enabled: &enabled, EnvironmentLabel: trimmedStringPtr(requestBody.EnvironmentLabel), SyncIntervalSeconds: syncInterval, RequestTimeoutSeconds: requestTimeout, AllowPrivateNetwork: allowPrivate, AllowInsecureHTTP: allowInsecure, SkipTLSVerify: boolValue(requestBody.SkipTLSVerify, false), ManagementAuthState: ManagementAuthStateUnknown}, nil
}

func buildUpdateInput(existing SidecarInstance, requestBody sidecarUpdateRequest) (SidecarInstanceInput, bool, error) {
	input := instanceToInput(existing)
	credentialUpdated := false
	if requestBody.Name.Set {
		if requestBody.Name.Value == nil {
			return SidecarInstanceInput{}, false, &domainError{StatusCode: http.StatusBadRequest, Detail: "name must not be null"}
		}
		name, err := normalizeSidecarName(*requestBody.Name.Value)
		if err != nil {
			return SidecarInstanceInput{}, false, err
		}
		input.Name = name
	}
	if requestBody.Enabled.Set {
		if requestBody.Enabled.Value == nil {
			return SidecarInstanceInput{}, false, &domainError{StatusCode: http.StatusBadRequest, Detail: "enabled must not be null"}
		}
		input.Enabled = requestBody.Enabled.Value
	}
	if requestBody.EnvironmentLabel.Set {
		input.EnvironmentLabel = trimmedStringPtr(requestBody.EnvironmentLabel.Value)
	}
	if requestBody.AllowPrivateNetwork.Set {
		if requestBody.AllowPrivateNetwork.Value == nil {
			return SidecarInstanceInput{}, false, &domainError{StatusCode: http.StatusBadRequest, Detail: "allow_private_network must not be null"}
		}
		input.AllowPrivateNetwork = *requestBody.AllowPrivateNetwork.Value
	}
	if requestBody.AllowInsecureHTTP.Set {
		if requestBody.AllowInsecureHTTP.Value == nil {
			return SidecarInstanceInput{}, false, &domainError{StatusCode: http.StatusBadRequest, Detail: "allow_insecure_http must not be null"}
		}
		input.AllowInsecureHTTP = *requestBody.AllowInsecureHTTP.Value
	}
	if requestBody.SkipTLSVerify.Set {
		if requestBody.SkipTLSVerify.Value == nil {
			return SidecarInstanceInput{}, false, &domainError{StatusCode: http.StatusBadRequest, Detail: "skip_tls_verify must not be null"}
		}
		input.SkipTLSVerify = *requestBody.SkipTLSVerify.Value
	}
	candidateURL := input.BaseURL
	if requestBody.BaseURL.Set {
		if requestBody.BaseURL.Value == nil {
			return SidecarInstanceInput{}, false, &domainError{StatusCode: http.StatusBadRequest, Detail: "base_url must not be null"}
		}
		candidateURL = *requestBody.BaseURL.Value
	}
	normalizedURL, err := normalizeSidecarBaseURL(candidateURL, input.AllowPrivateNetwork, input.AllowInsecureHTTP)
	if err != nil {
		return SidecarInstanceInput{}, false, err
	}
	input.BaseURL = normalizedURL
	input.BaseURLCanonical = normalizedURL
	if requestBody.SyncIntervalSeconds.Set {
		value, err := optionalPositiveSecondsOrDefault(requestBody.SyncIntervalSeconds.Value, DefaultSyncIntervalSeconds, "sync_interval_seconds")
		if err != nil {
			return SidecarInstanceInput{}, false, err
		}
		input.SyncIntervalSeconds = value
	}
	if requestBody.RequestTimeoutSeconds.Set {
		value, err := optionalPositiveSecondsOrDefault(requestBody.RequestTimeoutSeconds.Value, DefaultRequestTimeoutSeconds, "request_timeout_seconds")
		if err != nil {
			return SidecarInstanceInput{}, false, err
		}
		input.RequestTimeoutSeconds = value
	}
	if requestBody.ManagementPassword.Set {
		if requestBody.ManagementPassword.Value == nil || strings.TrimSpace(*requestBody.ManagementPassword.Value) == "" {
			return SidecarInstanceInput{}, false, &domainError{StatusCode: http.StatusBadRequest, Detail: "management_password must not be empty"}
		}
		input.ManagementPassword = *requestBody.ManagementPassword.Value
		input.ManagementPasswordIsEncrypted = false
		input.ManagementAuthState = ManagementAuthStateUnknown
		input.AuthFailurePauseUntil = nil
		credentialUpdated = true
	}
	return input, credentialUpdated, nil
}

func instanceToInput(instance SidecarInstance) SidecarInstanceInput {
	enabled := instance.Enabled
	return SidecarInstanceInput{Name: instance.Name, BaseURL: instance.BaseURL, BaseURLCanonical: instance.BaseURLCanonical, ManagementPassword: instance.EncryptedManagementPassword, ManagementPasswordIsEncrypted: true, Enabled: &enabled, EnvironmentLabel: cloneStringPtr(instance.EnvironmentLabel), SyncIntervalSeconds: instance.SyncIntervalSeconds, RequestTimeoutSeconds: instance.RequestTimeoutSeconds, AllowPrivateNetwork: instance.AllowPrivateNetwork, AllowInsecureHTTP: instance.AllowInsecureHTTP, SkipTLSVerify: instance.SkipTLSVerify, ManagementAuthState: instance.ManagementAuthState, AuthFailurePauseUntil: cloneTimePtr(instance.AuthFailurePauseUntil)}
}

func (s *Service) markInvalidManagementAuth(ctx context.Context, instance SidecarInstance) SidecarInstance {
	input := instanceToInput(instance)
	pauseUntil := s.nowUTC().Add(time.Duration(instance.SyncIntervalSeconds) * time.Second)
	input.ManagementAuthState = ManagementAuthStateInvalid
	input.AuthFailurePauseUntil = &pauseUntil
	updated, err := s.store.UpdateSidecarInstance(ctx, instance.ID, input)
	if err != nil {
		return instance
	}
	return updated
}

func (s *Service) markConnectionSuccess(ctx context.Context, instance SidecarInstance) SidecarInstance {
	input := instanceToInput(instance)
	input.ManagementAuthState = ManagementAuthStateValid
	input.AuthFailurePauseUntil = nil
	updated, err := s.store.UpdateSidecarInstance(ctx, instance.ID, input)
	if err != nil {
		return instance
	}
	return updated
}

func (s *Service) recordAction(ctx context.Context, sidecarID int, actionType string, status string, reason *string) {
	if s == nil || s.store == nil {
		return
	}
	completedAt := s.nowUTC()
	_, _ = s.store.CreateWatchdogAction(ctx, SidecarWatchdogActionInput{SidecarID: sidecarID, ActionType: actionType, Status: status, Reason: reason, CompletedAt: &completedAt})
}

func (s *Service) ensureSidecarExists(w http.ResponseWriter, r *http.Request, id int) bool {
	_, found, err := s.store.GetSidecarInstance(r.Context(), id)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return false
	}
	if !found {
		writeError(w, r, s.corsSnapshot(), http.StatusNotFound, "sidecar not found")
		return false
	}
	return true
}

func (s *Service) parseSidecarID(w http.ResponseWriter, r *http.Request) (int, bool) {
	id, err := strconv.Atoi(strings.TrimSpace(chi.URLParam(r, "sidecar_id")))
	if err != nil || id <= 0 {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "sidecar_id must be a positive integer")
		return 0, false
	}
	return id, true
}

func normalizeSidecarName(value string) (string, error) {
	name := strings.TrimSpace(value)
	if name == "" {
		return "", &domainError{StatusCode: http.StatusBadRequest, Detail: "name is required"}
	}
	if len(name) > 120 {
		return "", &domainError{StatusCode: http.StatusBadRequest, Detail: "name must be at most 120 characters"}
	}
	return name, nil
}

func normalizeSidecarBaseURL(rawURL string, allowPrivate bool, allowInsecure bool) (string, error) {
	normalized, err := NormalizeCLIProxyBaseURL(rawURL, CLIProxyConnectionPolicy{AllowPrivateNetwork: allowPrivate, AllowInsecureHTTP: allowInsecure})
	if err != nil {
		return "", &domainError{StatusCode: http.StatusBadRequest, Detail: err.Error()}
	}
	return normalized, nil
}

func positiveSecondsOrDefault(value *int, defaultValue int, fieldName string) (int, error) {
	if value == nil {
		return defaultValue, nil
	}
	return optionalPositiveSecondsOrDefault(value, defaultValue, fieldName)
}

func optionalPositiveSecondsOrDefault(value *int, defaultValue int, fieldName string) (int, error) {
	if value == nil {
		return defaultValue, nil
	}
	if *value < 1 {
		return 0, &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("%s must be >= 1", fieldName)}
	}
	return *value, nil
}

func boolValue(value *bool, defaultValue bool) bool {
	if value == nil {
		return defaultValue
	}
	return *value
}

func trimmedStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func buildSidecarInstanceResponse(instance SidecarInstance) sidecarInstanceResponse {
	passwordConfigured := strings.TrimSpace(instance.EncryptedManagementPassword) != ""
	var masked *string
	if passwordConfigured {
		value := credentialMask
		masked = &value
	}
	state := strings.TrimSpace(instance.ManagementAuthState)
	if state == "" {
		state = ManagementAuthStateUnknown
	}
	var pause *sidecarPauseResponse
	if instance.AuthFailurePauseUntil != nil || state == ManagementAuthStateInvalid {
		pause = &sidecarPauseResponse{Reason: ManagementAuthStateInvalid, PausedUntil: instance.AuthFailurePauseUntil}
	}
	return sidecarInstanceResponse{ID: instance.ID, Name: instance.Name, BaseURL: instance.BaseURL, BaseURLCanonical: instance.BaseURLCanonical, Enabled: instance.Enabled, EnvironmentLabel: instance.EnvironmentLabel, AllowPrivateNetwork: instance.AllowPrivateNetwork, AllowInsecureHTTP: instance.AllowInsecureHTTP, SkipTLSVerify: instance.SkipTLSVerify, SyncIntervalSeconds: instance.SyncIntervalSeconds, RequestTimeoutSeconds: instance.RequestTimeoutSeconds, CredentialState: sidecarCredentialResponse{ManagementPasswordConfigured: passwordConfigured, ManagementPasswordMasked: masked}, ManagementAuthState: state, PauseMetadata: pause, LastSyncAt: instance.LastSyncAt, LastSuccessfulSyncAt: instance.LastSuccessfulSyncAt, SnapshotStaleAfter: instance.SnapshotStaleAfter, LastSyncError: instance.LastSyncError, CreatedAt: instance.CreatedAt, UpdatedAt: instance.UpdatedAt}
}

func buildAuthSnapshotResponse(snapshot SidecarAuthSnapshot) authSnapshotResponse {
	return authSnapshotResponse{ID: snapshot.ID, SidecarID: snapshot.SidecarID, AuthID: snapshot.AuthID, AuthIndex: snapshot.AuthIndex, Name: snapshot.Name, Provider: snapshot.Provider, Label: snapshot.Label, Status: snapshot.Status, StatusMessage: snapshot.StatusMessage, Disabled: snapshot.Disabled, Unavailable: snapshot.Unavailable, Priority: snapshot.Priority, QuotaExceeded: snapshot.QuotaExceeded, QuotaReason: snapshot.QuotaReason, QuotaNextRecoverAt: snapshot.QuotaNextRecoverAt, NextRetryAfter: snapshot.NextRetryAfter, SuccessCount: snapshot.SuccessCount, FailedCount: snapshot.FailedCount, RecentRequests: snapshot.RecentRequestsJSON, ModelStates: snapshot.ModelStatesJSON, ObservedAt: snapshot.ObservedAt, Snapshot: snapshot.SnapshotJSON}
}

func buildProviderSnapshotResponse(snapshot SidecarProviderSnapshot) providerSnapshotResponse {
	return providerSnapshotResponse{ID: snapshot.ID, SidecarID: snapshot.SidecarID, ProviderKey: snapshot.ProviderKey, ProviderItemKey: snapshot.ProviderItemKey, Name: snapshot.Name, Label: snapshot.Label, Status: snapshot.Status, Disabled: snapshot.Disabled, ObservedAt: snapshot.ObservedAt, Snapshot: snapshot.SnapshotJSON}
}

func decodeJSONBody(request *http.Request, target any) error {
	defer request.Body.Close()
	decoder := json.NewDecoder(request.Body)
	return decoder.Decode(target)
}

func writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeDomainError(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot, err error) {
	var sidecarErr *domainError
	if errors.As(err, &sidecarErr) {
		writeError(w, r, corsSnapshot, sidecarErr.StatusCode, sidecarErr.Detail)
		return
	}
	var storeErr *StoreError
	if errors.As(err, &storeErr) {
		statusCode := http.StatusInternalServerError
		switch storeErr.Code {
		case StoreErrorInvalidInput:
			statusCode = http.StatusBadRequest
		case StoreErrorNotFound:
			statusCode = http.StatusNotFound
		case StoreErrorDuplicateSidecarName, StoreErrorDuplicateSidecarCanonicalURL, StoreErrorDuplicateActiveHold, StoreErrorConflict:
			statusCode = http.StatusConflict
		}
		writeError(w, r, corsSnapshot, statusCode, storeErr.Error())
		return
	}
	writeError(w, r, corsSnapshot, http.StatusInternalServerError, "Internal server error")
}

func writeError(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot, statusCode int, detail any) {
	platformcors.ApplyAllowOriginHeaders(w, r, corsSnapshot)
	writeJSON(w, statusCode, map[string]any{"detail": detail})
}
