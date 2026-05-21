package sidecars

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	neturl "net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

const cliProxyAuthFileDeleteBaselineCommit = "21fad9dbb447a2ab70d51d0ac3e3d032525a6054"

var errOperatorMutationStaleSnapshot = errors.New("stale_snapshot")

var statusMutationAllowedFields = map[string]struct{}{
	"disabled":   {},
	"force_live": {},
}

var fieldsMutationAllowedFields = map[string]struct{}{
	"custom_headers": {},
	"force_live":     {},
	"headers":        {},
	"note":           {},
	"prefix":         {},
	"priority":       {},
	"proxy_url":      {},
}

var deleteMutationAllowedFields = map[string]struct{}{
	"confirm_name": {},
}

var mutationAllowedQueryControls = map[string]struct{}{
	"force_live": {},
}

var allowedMutationHeaderNames = map[string]struct{}{
	"x-correlation-id": {},
	"x-request-id":     {},
	"x-trace-id":       {},
}

type authMutationResponse struct {
	State      string                     `json:"state"`
	Snapshot   *authSnapshotResponse      `json:"snapshot,omitempty"`
	SyncStatus *sidecarSyncStatusResponse `json:"sync_status,omitempty"`
	SyncError  *string                    `json:"sync_error,omitempty"`
}

type operatorMutationTarget struct {
	Instance        SidecarInstance
	Snapshot        SidecarAuthSnapshot
	DeleteSupported bool
}

func (s *Service) handlePatchAuthFileStatus(w http.ResponseWriter, r *http.Request) {
	sidecarID, ok := s.parseSidecarID(w, r)
	if !ok {
		return
	}
	authID, ok := s.parseAuthMutationID(w, r)
	if !ok {
		return
	}
	raw, err := decodeMutationObject(r)
	if err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	forceLive, err := mutationControlBool(raw, r, "force_live")
	if err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	if unknown := mutationUnknownQueryParameters(r, mutationAllowedQueryControls); len(unknown) > 0 {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, fmt.Sprintf("unsupported query parameters: %s", strings.Join(unknown, ", ")))
		return
	}
	if unknown := mutationUnknownFields(raw, statusMutationAllowedFields); len(unknown) > 0 {
		s.rejectOperatorMutation(w, r, fmt.Sprintf("unsupported fields: %s", strings.Join(unknown, ", ")))
		return
	}
	disabled, err := requiredMutationBool(raw, "disabled")
	if err != nil {
		s.rejectOperatorMutation(w, r, err.Error())
		return
	}
	target, err := s.loadOperatorMutationTarget(r.Context(), sidecarID, authID, forceLive)
	if err != nil {
		s.handleOperatorMutationLoadError(w, r, sidecarID, err)
		return
	}
	payload := map[string]any{"name": target.Snapshot.Name, "disabled": disabled}
	_, patchErr := s.patchOperatorAuthFile(r.Context(), target.Instance, "/auth-files/status", payload)
	s.finishOperatorMutation(w, r, target, patchErr)
}

func (s *Service) handlePatchAuthFileFields(w http.ResponseWriter, r *http.Request) {
	sidecarID, ok := s.parseSidecarID(w, r)
	if !ok {
		return
	}
	authID, ok := s.parseAuthMutationID(w, r)
	if !ok {
		return
	}
	raw, err := decodeMutationObject(r)
	if err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	forceLive, err := mutationControlBool(raw, r, "force_live")
	if err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	if unknown := mutationUnknownQueryParameters(r, mutationAllowedQueryControls); len(unknown) > 0 {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, fmt.Sprintf("unsupported query parameters: %s", strings.Join(unknown, ", ")))
		return
	}
	if unknown := mutationUnknownFields(raw, fieldsMutationAllowedFields); len(unknown) > 0 {
		s.rejectOperatorMutation(w, r, fmt.Sprintf("unsupported fields: %s", strings.Join(unknown, ", ")))
		return
	}
	payload, _, err := buildOperatorFieldsPayload(raw, "")
	if err != nil {
		s.rejectOperatorMutation(w, r, err.Error())
		return
	}
	target, err := s.loadOperatorMutationTarget(r.Context(), sidecarID, authID, forceLive)
	if err != nil {
		s.handleOperatorMutationLoadError(w, r, sidecarID, err)
		return
	}
	payload["name"] = target.Snapshot.Name
	_, patchErr := s.patchOperatorAuthFile(r.Context(), target.Instance, "/auth-files/fields", payload)
	s.finishOperatorMutation(w, r, target, patchErr)
}

func (s *Service) handleDeleteAuthFile(w http.ResponseWriter, r *http.Request) {
	sidecarID, ok := s.parseSidecarID(w, r)
	if !ok {
		return
	}
	authID, ok := s.parseAuthMutationID(w, r)
	if !ok {
		return
	}
	raw, err := decodeMutationObject(r)
	if err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	if unknown := mutationUnknownQueryParameters(r, nil); len(unknown) > 0 {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, fmt.Sprintf("unsupported query parameters: %s", strings.Join(unknown, ", ")))
		return
	}
	if unknown := mutationUnknownFields(raw, deleteMutationAllowedFields); len(unknown) > 0 {
		s.rejectOperatorMutation(w, r, fmt.Sprintf("unsupported fields: %s", strings.Join(unknown, ", ")))
		return
	}
	confirmName, err := requiredMutationString(raw, "confirm_name")
	if err != nil {
		s.rejectOperatorMutation(w, r, err.Error())
		return
	}
	target, err := s.loadOperatorDeleteTarget(r.Context(), sidecarID, authID, confirmName)
	if err != nil {
		s.handleOperatorMutationLoadError(w, r, sidecarID, err)
		return
	}
	deleteErr := s.deleteOperatorAuthFile(r.Context(), target.Instance, target.Snapshot.Name)
	if deleteErr != nil {
		s.writeOperatorMutationPatchError(w, r, target.Instance, deleteErr)
		return
	}
	writeJSON(w, http.StatusOK, s.operatorDeleteResponseAfterSync(r.Context(), target))
}

func (s *Service) loadOperatorMutationTarget(ctx context.Context, sidecarID int, authID string, forceLive bool) (operatorMutationTarget, error) {
	instance, found, err := s.store.GetSidecarInstance(ctx, sidecarID)
	if err != nil {
		return operatorMutationTarget{}, err
	}
	if !found {
		return operatorMutationTarget{}, notFoundError("sidecar instance not found")
	}
	snapshot, snapshotFound, err := s.store.GetAuthSnapshot(ctx, sidecarID, authID)
	if err != nil {
		return operatorMutationTarget{}, err
	}
	now := s.nowUTC()
	if sidecarSnapshotsStale(instance, now) {
		if !forceLive {
			if !snapshotFound {
				snapshot = SidecarAuthSnapshot{SidecarID: sidecarID, AuthID: authID}
			}
			return operatorMutationTarget{Instance: instance, Snapshot: snapshot}, errOperatorMutationStaleSnapshot
		}
		return s.loadLiveOperatorMutationTarget(ctx, instance, authID, snapshot, snapshotFound, now)
	}
	if snapshotFound {
		return s.loadLiveOperatorMutationTarget(ctx, instance, authID, snapshot, true, now)
	}
	if forceLive {
		return s.loadLiveOperatorMutationTarget(ctx, instance, authID, snapshot, false, now)
	}
	return operatorMutationTarget{}, notFoundError("auth file snapshot not found")
}

func (s *Service) loadLiveOperatorMutationTarget(ctx context.Context, instance SidecarInstance, authID string, stored SidecarAuthSnapshot, hasStored bool, now time.Time) (operatorMutationTarget, error) {
	live, found, err := s.fetchLiveAuthSnapshot(ctx, instance, authID, now)
	if err != nil {
		return operatorMutationTarget{Instance: instance, Snapshot: stored}, err
	}
	if !found {
		return operatorMutationTarget{Instance: instance, Snapshot: stored}, notFoundError("auth file not found in live sidecar state")
	}
	if hasStored && stored.ID > 0 {
		live.ID = stored.ID
		live.CreatedAt = stored.CreatedAt
	}
	return operatorMutationTarget{Instance: instance, Snapshot: live}, nil
}

func (s *Service) loadOperatorDeleteTarget(ctx context.Context, sidecarID int, authID string, confirmName string) (operatorMutationTarget, error) {
	instance, found, err := s.store.GetSidecarInstance(ctx, sidecarID)
	if err != nil {
		return operatorMutationTarget{}, err
	}
	if !found {
		return operatorMutationTarget{}, notFoundError("sidecar instance not found")
	}
	stored, storedFound, err := s.store.GetAuthSnapshot(ctx, sidecarID, authID)
	if err != nil {
		return operatorMutationTarget{}, err
	}
	if !storedFound {
		return operatorMutationTarget{Instance: instance}, notFoundError("auth file snapshot not found")
	}
	live, found, err := s.fetchLiveDeleteAuthSnapshot(ctx, instance, authID, s.nowUTC())
	if err != nil {
		return operatorMutationTarget{Instance: instance, Snapshot: stored}, err
	}
	if !found {
		return operatorMutationTarget{Instance: instance, Snapshot: stored}, notFoundError("auth file not found in live sidecar state")
	}
	if live.Name != confirmName {
		return operatorMutationTarget{Instance: instance, Snapshot: live}, conflictError("stale_auth_confirmation")
	}
	if stored.ID > 0 {
		live.ID = stored.ID
		live.CreatedAt = stored.CreatedAt
	}
	return operatorMutationTarget{Instance: instance, Snapshot: live, DeleteSupported: true}, nil
}

func (s *Service) finishOperatorMutation(w http.ResponseWriter, r *http.Request, target operatorMutationTarget, patchErr error) {
	if patchErr != nil {
		s.writeOperatorMutationPatchError(w, r, target.Instance, patchErr)
		return
	}
	writeJSON(w, http.StatusOK, s.operatorMutationResponseAfterSync(r.Context(), target))
}

func (s *Service) rejectOperatorMutation(w http.ResponseWriter, r *http.Request, detail string) {
	writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, detail)
}

func (s *Service) handleOperatorMutationLoadError(w http.ResponseWriter, r *http.Request, sidecarID int, err error) {
	if errors.Is(err, errOperatorMutationStaleSnapshot) {
		writeError(w, r, s.corsSnapshot(), http.StatusConflict, "stale_snapshot")
		return
	}
	if instance, found, loadErr := s.store.GetSidecarInstance(r.Context(), sidecarID); loadErr == nil && found {
		s.writeOperatorMutationPatchError(w, r, instance, err)
		return
	}
	writeDomainError(w, r, s.corsSnapshot(), err)
}

func (s *Service) patchOperatorAuthFile(ctx context.Context, instance SidecarInstance, path string, payload map[string]any) (map[string]any, error) {
	target, err := s.cliProxyTarget(instance)
	if err != nil {
		return nil, err
	}
	response := map[string]any{}
	_, err = s.cliProxyClient.FetchJSON(ctx, target, http.MethodPatch, path, payload, &response)
	return response, err
}

func (s *Service) deleteOperatorAuthFile(ctx context.Context, instance SidecarInstance, name string) error {
	target, err := s.cliProxyTarget(instance)
	if err != nil {
		return err
	}
	response := map[string]any{}
	_, err = s.cliProxyClient.FetchJSON(ctx, target, http.MethodDelete, "/auth-files", map[string]any{"names": []string{name}}, &response)
	return err
}

func (s *Service) fetchLiveAuthSnapshot(ctx context.Context, instance SidecarInstance, authID string, observedAt time.Time) (SidecarAuthSnapshot, bool, error) {
	target, err := s.cliProxyTarget(instance)
	if err != nil {
		return SidecarAuthSnapshot{}, false, err
	}
	authFiles, err := s.fetchSidecarAuthFileRows(ctx, target)
	if err != nil {
		return SidecarAuthSnapshot{}, false, err
	}
	matches := make([]SidecarAuthSnapshot, 0, 1)
	nameCounts := make(map[string]int, len(authFiles))
	for _, raw := range authFiles {
		input, err := normalizeSidecarAuthSnapshot(instance.ID, observedAt, raw)
		if err != nil {
			return SidecarAuthSnapshot{}, false, err
		}
		snapshot := authSnapshotFromInput(input)
		nameCounts[snapshot.Name]++
		if snapshot.AuthID == authID {
			matches = append(matches, snapshot)
		}
	}
	if len(matches) == 0 {
		return SidecarAuthSnapshot{}, false, nil
	}
	if len(matches) > 1 {
		return SidecarAuthSnapshot{}, false, unsafeAuthMutationIdentityError("multiple live auth rows match auth_id")
	}
	live := matches[0]
	if !authSnapshotHasMutationStableIdentity(live) {
		return SidecarAuthSnapshot{}, false, unsafeAuthMutationIdentityError("auth row identity is name-derived")
	}
	if nameCounts[live.Name] > 1 {
		return SidecarAuthSnapshot{}, false, unsafeAuthMutationIdentityError("duplicate live auth file name")
	}
	return live, true, nil
}

func (s *Service) fetchLiveDeleteAuthSnapshot(ctx context.Context, instance SidecarInstance, authID string, observedAt time.Time) (SidecarAuthSnapshot, bool, error) {
	target, err := s.cliProxyTarget(instance)
	if err != nil {
		return SidecarAuthSnapshot{}, false, err
	}
	authFiles, capabilityResponse, err := s.fetchSidecarAuthFileRowsWithResponse(ctx, target)
	if err != nil {
		return SidecarAuthSnapshot{}, false, err
	}
	if err := s.validateAuthDeleteCapability(ctx, target, capabilityResponse); err != nil {
		return SidecarAuthSnapshot{}, false, err
	}
	type deleteMatch struct {
		fields   map[string]any
		snapshot SidecarAuthSnapshot
	}
	matches := make([]deleteMatch, 0, 1)
	nameCounts := make(map[string]int, len(authFiles))
	for _, raw := range authFiles {
		fields, err := decodeSidecarSyncObject(raw)
		if err != nil {
			return SidecarAuthSnapshot{}, false, err
		}
		input, err := normalizeSidecarAuthSnapshot(instance.ID, observedAt, raw)
		if err != nil {
			return SidecarAuthSnapshot{}, false, err
		}
		snapshot := authSnapshotFromInput(input)
		nameCounts[snapshot.Name]++
		if snapshot.AuthID == authID {
			matches = append(matches, deleteMatch{fields: fields, snapshot: snapshot})
		}
	}
	if len(matches) == 0 {
		return SidecarAuthSnapshot{}, false, nil
	}
	if len(matches) > 1 {
		return SidecarAuthSnapshot{}, false, unsafeAuthMutationIdentityError("multiple live auth rows match auth_id")
	}
	live := matches[0]
	if !authSnapshotHasMutationStableIdentity(live.snapshot) {
		return SidecarAuthSnapshot{}, false, unsafeAuthMutationIdentityError("auth row identity is name-derived")
	}
	if nameCounts[live.snapshot.Name] > 1 {
		return SidecarAuthSnapshot{}, false, unsafeAuthMutationIdentityError("duplicate live auth file name")
	}
	if err := validateAuthDeleteLiveRow(live.snapshot, live.fields); err != nil {
		return SidecarAuthSnapshot{}, false, err
	}
	return live.snapshot, true, nil
}

func validateAuthDeleteLiveRow(snapshot SidecarAuthSnapshot, fields map[string]any) error {
	name := strings.TrimSpace(snapshot.Name)
	if name == "" || strings.ContainsAny(name, `/\\`) {
		return unsafeAuthMutationIdentityError("auth file name is path-like")
	}
	if runtimeOnly, ok := fields["runtime_only"].(bool); ok && runtimeOnly {
		return unsafeAuthMutationIdentityError("runtime-only auth row")
	}
	if source := strings.ToLower(trimmedStringValue(fields["source"])); source != "file" {
		return unsafeAuthMutationIdentityError("auth row is not file-backed")
	}
	if trimmedStringValue(fields["path"]) == "" {
		return unsafeAuthMutationIdentityError("auth row file path is missing")
	}
	return nil
}

func (s *Service) validateAuthDeleteCapability(ctx context.Context, target CLIProxyTarget, response CLIProxyResponse) error {
	if s.authDeleteCapabilityKnownSupported(ctx, target, response) {
		return nil
	}
	return &domainError{StatusCode: http.StatusFailedDependency, Detail: "sidecar auth file delete capability is unsupported or unknown"}
}

func (s *Service) authDeleteCapabilityKnownSupported(ctx context.Context, target CLIProxyTarget, response CLIProxyResponse) bool {
	if authDeleteCapabilitySupported(response) {
		return true
	}
	if !authDeleteCapabilityProbeEligible(response) {
		return false
	}
	supported, err := s.probeAuthDeleteCapability(ctx, target)
	return err == nil && supported
}

func authDeleteCapabilitySupported(response CLIProxyResponse) bool {
	return response.Commit == cliProxyAuthFileDeleteBaselineCommit
}

func authDeleteCapabilityProbeEligible(response CLIProxyResponse) bool {
	return strings.TrimSpace(response.Commit) != "" || strings.TrimSpace(response.Version) != "" || strings.TrimSpace(response.BuildDate) != ""
}

func (s *Service) probeAuthDeleteCapability(ctx context.Context, target CLIProxyTarget) (bool, error) {
	var response map[string]any
	_, err := s.cliProxyClient.FetchJSON(ctx, target, http.MethodDelete, "/auth-files", map[string]any{"names": []string{}}, &response)
	if err == nil {
		return false, nil
	}
	var clientErr *CLIProxyClientError
	if errors.As(err, &clientErr) && clientErr.Code == CLIProxyErrorUpstreamStatus && clientErr.StatusCode == http.StatusBadRequest {
		return true, nil
	}
	return false, err
}

func unsafeAuthMutationIdentityError(reason string) error {
	return conflictError("unsafe_auth_identity: " + reason)
}

func authSnapshotHasMutationStableIdentity(snapshot SidecarAuthSnapshot) bool {
	authID := strings.TrimSpace(snapshot.AuthID)
	name := strings.TrimSpace(snapshot.Name)
	if authID == "" || name == "" {
		return false
	}
	if snapshot.AuthIndex != nil && strings.TrimSpace(*snapshot.AuthIndex) != "" {
		return true
	}
	return authID != name
}

func (s *Service) cliProxyTarget(instance SidecarInstance) (CLIProxyTarget, error) {
	password, err := s.decryptManagementPassword(instance.EncryptedManagementPassword)
	if err != nil {
		return CLIProxyTarget{}, err
	}
	if strings.TrimSpace(password) == "" {
		return CLIProxyTarget{}, errSidecarManagementPasswordMissing
	}
	return CLIProxyTarget{BaseURL: instance.BaseURLCanonical, ManagementPassword: password, AllowPrivateNetwork: instance.AllowPrivateNetwork, AllowInsecureHTTP: instance.AllowInsecureHTTP, SkipTLSVerify: instance.SkipTLSVerify, RequestTimeoutSeconds: instance.RequestTimeoutSeconds}, nil
}

func authSnapshotFromInput(input SidecarAuthSnapshotInput) SidecarAuthSnapshot {
	return SidecarAuthSnapshot{SidecarID: input.SidecarID, AuthID: input.AuthID, AuthIndex: cloneStringPtr(input.AuthIndex), Name: input.Name, Provider: cloneStringPtr(input.Provider), Label: cloneStringPtr(input.Label), Status: cloneStringPtr(input.Status), StatusMessage: cloneStringPtr(input.StatusMessage), Disabled: cloneBoolPtr(input.Disabled), Unavailable: cloneBoolPtr(input.Unavailable), Priority: cloneIntPtr(input.Priority), QuotaExceeded: cloneBoolPtr(input.QuotaExceeded), QuotaReason: cloneStringPtr(input.QuotaReason), QuotaNextRecoverAt: cloneTimePtr(input.QuotaNextRecoverAt), SuccessCount: cloneIntPtr(input.SuccessCount), FailedCount: cloneIntPtr(input.FailedCount), RecentRequestsJSON: append([]byte(nil), input.RecentRequestsJSON...), ModelStatesJSON: append([]byte(nil), input.ModelStatesJSON...), SnapshotJSON: append([]byte(nil), input.SnapshotJSON...), ObservedAt: input.ObservedAt}
}

func (s *Service) operatorMutationResponseAfterSync(ctx context.Context, target operatorMutationTarget) authMutationResponse {
	current := target.Snapshot
	result, syncErr := s.SyncSidecar(ctx, target.Instance.ID)
	if syncErr == nil {
		if refreshed, found, err := s.store.GetAuthSnapshot(ctx, target.Instance.ID, target.Snapshot.AuthID); err == nil && found {
			current = refreshed
		}
	}
	snapshot := buildAuthSnapshotResponse(current)
	response := authMutationResponse{State: "succeeded", Snapshot: &snapshot}
	if result.Sidecar.ID != 0 {
		syncStatus := buildSidecarSyncStatusResponse(s.sidecarSyncStatus(result.Sidecar))
		response.SyncStatus = &syncStatus
	}
	if syncErr != nil {
		response.State = "succeeded_sync_failed"
		detail := sidecarErrorMessage(syncErr)
		response.SyncError = &detail
	}
	return response
}

func (s *Service) operatorDeleteResponseAfterSync(ctx context.Context, target operatorMutationTarget) authMutationResponse {
	result, syncErr := s.refreshAuthSnapshotsAfterDelete(ctx, target.Instance, target.Snapshot.AuthID, target.DeleteSupported)
	response := authMutationResponse{State: "succeeded"}
	if result.Sidecar.ID != 0 {
		syncStatus := buildSidecarSyncStatusResponse(s.sidecarSyncStatus(result.Sidecar))
		response.SyncStatus = &syncStatus
	}
	if syncErr != nil {
		response.State = "succeeded_sync_failed"
		snapshot := buildAuthSnapshotResponse(target.Snapshot)
		response.Snapshot = &snapshot
		detail := sidecarErrorMessage(syncErr)
		response.SyncError = &detail
		return response
	}
	if refreshed, found, err := s.store.GetAuthSnapshot(ctx, target.Instance.ID, target.Snapshot.AuthID); err == nil && found {
		snapshot := buildAuthSnapshotResponse(refreshed)
		response.Snapshot = &snapshot
	}
	return response
}

func (s *Service) refreshAuthSnapshotsAfterDelete(ctx context.Context, instance SidecarInstance, deletedAuthID string, deleteSupported bool) (SidecarSyncResult, error) {
	syncedAt := s.nowUTC()
	result := SidecarSyncResult{Sidecar: instance, SyncedAt: syncedAt}
	target, err := s.cliProxyTarget(instance)
	if err != nil {
		return s.finishSyncFailure(ctx, result, instance, syncedAt, err)
	}
	authFiles, _, err := s.fetchSidecarAuthFileRowsWithResponseAllowEmpty(ctx, target)
	if err != nil {
		return s.finishSyncFailure(ctx, result, instance, syncedAt, err)
	}
	authInputs := make([]SidecarAuthSnapshotInput, 0, len(authFiles))
	for _, raw := range authFiles {
		input, err := normalizeSidecarAuthSnapshotWithDeleteSupport(instance.ID, syncedAt, raw, deleteSupported)
		if err != nil {
			return s.finishSyncFailure(ctx, result, instance, syncedAt, err)
		}
		if input.AuthID == deletedAuthID {
			return s.finishSyncFailure(ctx, result, instance, syncedAt, conflictError("deleted auth file still present in live sidecar state"))
		}
		authInputs = append(authInputs, input)
	}
	replaced, err := s.store.ReplaceAuthSnapshots(ctx, instance.ID, authInputs)
	if err != nil {
		return s.finishSyncFailure(ctx, result, instance, syncedAt, err)
	}
	updated, err := s.finishSyncSuccess(ctx, instance, syncedAt)
	if err != nil {
		return result, err
	}
	result.Sidecar = updated
	result.AuthSnapshotCount = len(replaced)
	return result, nil
}

func sidecarErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	detail, code := redactedSidecarSyncError(err)
	if detail == "" {
		detail = code
	}
	if len(detail) > 500 {
		detail = detail[:500]
	}
	if sidecarTextLooksSensitive(detail) {
		return "redacted-by-prism"
	}
	return detail
}

func (s *Service) writeOperatorMutationPatchError(w http.ResponseWriter, r *http.Request, instance SidecarInstance, err error) {
	var clientErr *CLIProxyClientError
	if errors.As(err, &clientErr) {
		switch clientErr.Code {
		case CLIProxyErrorInvalidManagementAuth:
			s.markInvalidManagementAuth(r.Context(), instance)
			writeError(w, r, s.corsSnapshot(), http.StatusFailedDependency, "sidecar management authentication failed")
		case CLIProxyErrorRequestBuild, CLIProxyErrorUnsupportedPath:
			writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "sidecar mutation request could not be built")
		case CLIProxyErrorManagementDisabled:
			writeError(w, r, s.corsSnapshot(), http.StatusBadGateway, "sidecar management route unavailable")
		default:
			writeError(w, r, s.corsSnapshot(), http.StatusBadGateway, "sidecar mutation failed")
		}
		return
	}
	writeDomainError(w, r, s.corsSnapshot(), err)
}

func decodeMutationObject(r *http.Request) (map[string]json.RawMessage, error) {
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.UseNumber()
	var raw map[string]json.RawMessage
	if err := decoder.Decode(&raw); err != nil {
		return nil, err
	}
	if raw == nil {
		return nil, fmt.Errorf("request body must be a JSON object")
	}
	return raw, nil
}

func mutationControlBool(raw map[string]json.RawMessage, r *http.Request, name string) (bool, error) {
	value := false
	if rawValue, ok := raw[name]; ok {
		parsed, err := optionalMutationBool(rawValue, name)
		if err != nil {
			return false, err
		}
		value = parsed
	}
	if queryValue := strings.TrimSpace(r.URL.Query().Get(name)); queryValue != "" {
		parsed, err := strconv.ParseBool(queryValue)
		if err != nil {
			return false, fmt.Errorf("%s must be a boolean", name)
		}
		value = parsed
	}
	return value, nil
}

func requiredMutationBool(raw map[string]json.RawMessage, name string) (bool, error) {
	rawValue, ok := raw[name]
	if !ok {
		return false, fmt.Errorf("%s is required", name)
	}
	return optionalMutationBool(rawValue, name)
}

func requiredMutationString(raw map[string]json.RawMessage, name string) (string, error) {
	rawValue, ok := raw[name]
	if !ok {
		return "", fmt.Errorf("%s is required", name)
	}
	var value string
	if err := json.Unmarshal(rawValue, &value); err != nil {
		return "", fmt.Errorf("%s must be a string", name)
	}
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%s must not be empty", name)
	}
	return value, nil
}

func optionalMutationBool(raw json.RawMessage, name string) (bool, error) {
	var value *bool
	if err := json.Unmarshal(raw, &value); err != nil || value == nil {
		return false, fmt.Errorf("%s must be a boolean", name)
	}
	return *value, nil
}

func buildOperatorFieldsPayload(raw map[string]json.RawMessage, authName string) (map[string]any, []string, error) {
	payload := map[string]any{"name": authName}
	fields := make([]string, 0, len(raw))
	if rawValue, ok := raw["priority"]; ok {
		priority, err := mutationInt(rawValue, "priority")
		if err != nil {
			return nil, []string{"priority"}, err
		}
		if priority < 0 {
			return nil, []string{"priority"}, fmt.Errorf("priority must be >= 0")
		}
		payload["priority"] = priority
		fields = append(fields, "priority")
	}
	for _, name := range []string{"prefix", "proxy_url", "note"} {
		if rawValue, ok := raw[name]; ok {
			text, err := mutationNonEmptyString(rawValue, name)
			if err != nil {
				return nil, []string{name}, err
			}
			if err := validateMutationStringField(name, text); err != nil {
				return nil, []string{name}, err
			}
			payload[name] = text
			fields = append(fields, name)
		}
	}
	headers, headerFields, err := mutationHeaders(raw)
	if err != nil {
		return nil, headerFields, err
	}
	if len(headerFields) > 0 {
		payload["headers"] = headers
		fields = append(fields, headerFields...)
	}
	if len(fields) == 0 {
		return nil, nil, fmt.Errorf("at least one mutable auth field is required")
	}
	sort.Strings(fields)
	return payload, fields, nil
}

func mutationInt(raw json.RawMessage, name string) (int, error) {
	var value *int
	if err := json.Unmarshal(raw, &value); err != nil || value == nil {
		return 0, fmt.Errorf("%s must be an integer", name)
	}
	return *value, nil
}

func mutationNonEmptyString(raw json.RawMessage, name string) (string, error) {
	if strings.TrimSpace(string(raw)) == "null" {
		return "", fmt.Errorf("%s cannot be null; clearing is not supported, omit it to preserve the current value", name)
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("%s must be a string", name)
	}
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%s must not be empty; clearing is not supported, omit it to preserve the current value", name)
	}
	return value, nil
}

func mutationHeaders(raw map[string]json.RawMessage) (map[string]string, []string, error) {
	headerRaw, headerSet := raw["headers"]
	customRaw, customSet := raw["custom_headers"]
	fieldRoot := "headers"
	if headerSet && customSet {
		return nil, []string{"headers", "custom_headers"}, fmt.Errorf("headers and custom_headers cannot both be set")
	}
	if customSet {
		headerRaw = customRaw
		headerSet = true
		fieldRoot = "custom_headers"
	}
	if !headerSet {
		return nil, nil, nil
	}
	if strings.TrimSpace(string(headerRaw)) == "null" {
		return nil, []string{fieldRoot}, fmt.Errorf("%s cannot be null; clearing is not supported, omit it to preserve existing headers", fieldRoot)
	}
	var rawHeaders map[string]json.RawMessage
	if err := json.Unmarshal(headerRaw, &rawHeaders); err != nil || rawHeaders == nil {
		return nil, []string{fieldRoot}, fmt.Errorf("%s must be an object", fieldRoot)
	}
	if len(rawHeaders) == 0 {
		return nil, []string{fieldRoot}, fmt.Errorf("%s must include at least one header update; clearing all headers is not supported", fieldRoot)
	}
	headers := make(map[string]string, len(rawHeaders))
	fields := make([]string, 0, len(rawHeaders))
	for key, rawValue := range rawHeaders {
		name := strings.TrimSpace(key)
		fieldName := fieldRoot + "." + name
		if err := validateMutationHeaderName(name); err != nil {
			return nil, []string{fieldName}, err
		}
		value, err := mutationNonEmptyString(rawValue, fieldName)
		if err != nil {
			return nil, []string{fieldName}, err
		}
		headers[name] = value
		fields = append(fields, fieldName)
	}
	sort.Strings(fields)
	return headers, fields, nil
}

func validateMutationHeaderName(name string) error {
	if name == "" {
		return fmt.Errorf("header names must not be empty")
	}
	if _, ok := allowedMutationHeaderNames[strings.ToLower(name)]; !ok {
		return fmt.Errorf("header %s is not allowlisted", name)
	}
	if isSensitiveHeaderName(name) {
		return fmt.Errorf("header %s is not allowed", name)
	}
	for _, r := range name {
		if r <= 32 || r >= 127 || strings.ContainsRune("()<>@,;:\\\"/[]?={}", r) {
			return fmt.Errorf("header %s is not a valid custom header name", name)
		}
	}
	return nil
}

func validateMutationStringField(name string, value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fmt.Errorf("%s must not be empty; clearing is not supported, omit it to preserve the current value", name)
	}
	if mutationValueLooksSensitive(trimmed) {
		return fmt.Errorf("%s must not contain secret-like values", name)
	}
	if name != "proxy_url" {
		return nil
	}
	parsed, err := neturl.Parse(trimmed)
	if err != nil {
		return fmt.Errorf("proxy_url must be a valid URL")
	}
	if parsed.User != nil {
		return fmt.Errorf("proxy_url must not include credentials")
	}
	return nil
}

func mutationValueLooksSensitive(value string) bool {
	normalized := strings.ToLower(value)
	return strings.Contains(normalized, "api_key") ||
		strings.Contains(normalized, "api-key") ||
		strings.Contains(normalized, "apikey") ||
		strings.Contains(normalized, "authorization") ||
		strings.Contains(normalized, "bearer ") ||
		strings.Contains(normalized, "oauth") ||
		strings.Contains(normalized, "password") ||
		strings.Contains(normalized, "secret") ||
		strings.Contains(normalized, "token") ||
		strings.Contains(normalized, "credential")
}

func mutationUnknownFields(raw map[string]json.RawMessage, allowed map[string]struct{}) []string {
	unknown := make([]string, 0)
	for key := range raw {
		if _, ok := allowed[key]; !ok {
			unknown = append(unknown, key)
		}
	}
	sort.Strings(unknown)
	return unknown
}

func mutationUnknownQueryParameters(r *http.Request, allowed map[string]struct{}) []string {
	unknown := make([]string, 0)
	for key := range r.URL.Query() {
		if _, ok := allowed[key]; !ok {
			unknown = append(unknown, key)
		}
	}
	sort.Strings(unknown)
	return unknown
}

func (s *Service) parseAuthMutationID(w http.ResponseWriter, r *http.Request) (string, bool) {
	authID := strings.TrimSpace(chiURLParam(r, "auth_id"))
	if authID == "" {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "auth_id is required")
		return "", false
	}
	return authID, true
}

func chiURLParam(r *http.Request, key string) string {
	return strings.TrimSpace(chi.URLParam(r, key))
}
