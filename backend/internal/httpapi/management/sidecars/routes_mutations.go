package sidecars

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

const cliProxyAuthFileDeleteBaselineCommit = "21fad9dbb447a2ab70d51d0ac3e3d032525a6054"
const deletedAuthFileStillPresentDetail = "deleted auth file still present in live sidecar state"

var statusMutationAllowedFields = map[string]struct{}{
	"disabled": {},
}

var fieldsMutationAllowedFields = map[string]struct{}{
	"priority": {},
}

var deleteMutationAllowedFields = map[string]struct{}{
	"confirm_name": {},
}

type authMutationResponse struct {
	State      string                     `json:"state"`
	Snapshot   *authFileResponse          `json:"snapshot,omitempty"`
	SyncStatus *sidecarSyncStatusResponse `json:"sync_status,omitempty"`
	SyncError  *string                    `json:"sync_error,omitempty"`
}

type operatorMutationTarget struct {
	Instance        SidecarInstance
	AuthFile        SidecarAuthFile
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
	if unknown := mutationUnknownQueryParameters(r, nil); len(unknown) > 0 {
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
	target, err := s.loadOperatorMutationTarget(r.Context(), sidecarID, authID)
	if err != nil {
		s.handleOperatorMutationLoadError(w, r, sidecarID, err)
		return
	}
	payload := map[string]any{"name": target.AuthFile.Name, "disabled": disabled}
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
	if unknown := mutationUnknownQueryParameters(r, nil); len(unknown) > 0 {
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
	target, err := s.loadOperatorMutationTarget(r.Context(), sidecarID, authID)
	if err != nil {
		s.handleOperatorMutationLoadError(w, r, sidecarID, err)
		return
	}
	payload["name"] = target.AuthFile.Name
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
	deleteErr := s.deleteOperatorAuthFile(r.Context(), target.Instance, target.AuthFile.Name)
	if deleteErr != nil {
		s.writeOperatorMutationPatchError(w, r, target.Instance, deleteErr)
		return
	}
	response, syncErr := s.operatorDeleteResponseAfterSync(r.Context(), target)
	if syncErr != nil {
		s.writeOperatorMutationPatchError(w, r, target.Instance, syncErr)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) loadOperatorMutationTarget(ctx context.Context, sidecarID int, authID string) (operatorMutationTarget, error) {
	instance, found, err := s.store.GetSidecarInstance(ctx, sidecarID)
	if err != nil {
		return operatorMutationTarget{}, err
	}
	if !found {
		return operatorMutationTarget{}, notFoundError("sidecar instance not found")
	}
	live, found, err := s.fetchLiveAuthFile(ctx, instance, authID, s.nowUTC())
	if err != nil {
		return operatorMutationTarget{Instance: instance}, err
	}
	if !found {
		return operatorMutationTarget{Instance: instance}, notFoundError("auth file not found in live sidecar state")
	}
	return operatorMutationTarget{Instance: instance, AuthFile: live}, nil
}

func (s *Service) loadOperatorDeleteTarget(ctx context.Context, sidecarID int, authID string, confirmName string) (operatorMutationTarget, error) {
	instance, found, err := s.store.GetSidecarInstance(ctx, sidecarID)
	if err != nil {
		return operatorMutationTarget{}, err
	}
	if !found {
		return operatorMutationTarget{}, notFoundError("sidecar instance not found")
	}
	live, found, err := s.fetchLiveDeleteAuthFile(ctx, instance, authID, s.nowUTC())
	if err != nil {
		return operatorMutationTarget{Instance: instance}, err
	}
	if !found {
		return operatorMutationTarget{Instance: instance}, notFoundError("auth file not found in live sidecar state")
	}
	if live.Name != confirmName {
		return operatorMutationTarget{Instance: instance, AuthFile: live}, conflictError("stale_auth_confirmation")
	}
	return operatorMutationTarget{Instance: instance, AuthFile: live, DeleteSupported: true}, nil
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

func (s *Service) fetchLiveAuthFile(ctx context.Context, instance SidecarInstance, authID string, observedAt time.Time) (SidecarAuthFile, bool, error) {
	target, err := s.cliProxyTarget(instance)
	if err != nil {
		return SidecarAuthFile{}, false, err
	}
	authFiles, err := s.fetchSidecarAuthFileRows(ctx, target)
	if err != nil {
		return SidecarAuthFile{}, false, err
	}
	match, found, err := liveAuthFileMatch(instance.ID, observedAt, authFiles, authID)
	if err != nil || !found {
		return SidecarAuthFile{}, found, err
	}
	return match.file, true, nil
}

func (s *Service) fetchLiveDeleteAuthFile(ctx context.Context, instance SidecarInstance, authID string, observedAt time.Time) (SidecarAuthFile, bool, error) {
	target, err := s.cliProxyTarget(instance)
	if err != nil {
		return SidecarAuthFile{}, false, err
	}
	authFiles, capabilityResponse, err := s.fetchSidecarAuthFileRowsWithResponse(ctx, target)
	if err != nil {
		return SidecarAuthFile{}, false, err
	}
	if err := s.validateAuthDeleteCapability(ctx, target, capabilityResponse); err != nil {
		return SidecarAuthFile{}, false, err
	}
	match, found, err := liveAuthFileMatch(instance.ID, observedAt, authFiles, authID)
	if err != nil || !found {
		return SidecarAuthFile{}, found, err
	}
	if err := validateAuthDeleteLiveRow(match.file, match.fields); err != nil {
		return SidecarAuthFile{}, false, err
	}
	return match.file, true, nil
}

type liveAuthFileMatchResult struct {
	fields map[string]any
	file   SidecarAuthFile
}

func liveAuthFileMatch(sidecarID int, observedAt time.Time, authFiles []json.RawMessage, authID string) (liveAuthFileMatchResult, bool, error) {
	authID = strings.TrimSpace(authID)
	if authID == "" {
		return liveAuthFileMatchResult{}, false, unsafeAuthMutationIdentityError("auth_id is required")
	}
	matches := make([]liveAuthFileMatchResult, 0, 1)
	unsafeMatches := 0
	for _, raw := range authFiles {
		fields, err := decodeSidecarSyncObject(raw)
		if err != nil {
			return liveAuthFileMatchResult{}, false, err
		}
		input, err := normalizeSidecarAuthFile(sidecarID, observedAt, raw)
		if err != nil {
			return liveAuthFileMatchResult{}, false, err
		}
		normalized, err := normalizeAuthFileStoreInput(input)
		if err != nil {
			return liveAuthFileMatchResult{}, false, err
		}
		file := authFileFromInput(normalized, observedAt)
		if file.MutationSafe && file.AuthID == authID {
			matches = append(matches, liveAuthFileMatchResult{fields: fields, file: file})
			continue
		}
		if !file.MutationSafe && displayOnlyAuthFileMatchesMutationID(fields, file, authID) {
			unsafeMatches++
		}
	}
	if len(matches) > 1 {
		return liveAuthFileMatchResult{}, false, unsafeAuthMutationIdentityError("multiple live auth rows match auth_id")
	}
	if len(matches) == 1 {
		return matches[0], true, nil
	}
	if unsafeMatches > 0 {
		return liveAuthFileMatchResult{}, false, unsafeAuthMutationIdentityError("auth row has no upstream id")
	}
	return liveAuthFileMatchResult{}, false, nil
}

func displayOnlyAuthFileMatchesMutationID(fields map[string]any, file SidecarAuthFile, authID string) bool {
	if strings.TrimSpace(file.AuthID) != "" {
		return false
	}
	for _, value := range []string{file.Name, trimmedStringValue(fields["auth_id"]), trimmedStringValue(fields["auth_index"]), trimmedStringValue(fields["path"])} {
		if strings.TrimSpace(value) == authID {
			return true
		}
	}
	return false
}

func validateAuthDeleteLiveRow(file SidecarAuthFile, fields map[string]any) error {
	name := strings.TrimSpace(file.Name)
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

func (s *Service) operatorMutationResponseAfterSync(ctx context.Context, target operatorMutationTarget) authMutationResponse {
	current := target.AuthFile
	result, refreshed, found, syncErr := s.refreshAuthFilesAfterMutation(ctx, target.Instance, target.AuthFile.AuthID)
	if syncErr == nil && found {
		current = refreshed
	}
	authFile := buildAuthFileResponse(current)
	response := authMutationResponse{State: "succeeded", Snapshot: &authFile}
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

func (s *Service) refreshAuthFilesAfterMutation(ctx context.Context, instance SidecarInstance, authID string) (SidecarSyncResult, SidecarAuthFile, bool, error) {
	syncedAt := s.nowUTC()
	result := SidecarSyncResult{Sidecar: instance, SyncedAt: syncedAt}
	target, err := s.cliProxyTarget(instance)
	if err != nil {
		failed, failErr := s.finishSyncFailure(ctx, result, instance, syncedAt, err)
		return failed, SidecarAuthFile{}, false, failErr
	}
	authFiles, authFilesResponse, err := s.fetchSidecarAuthFileRowsWithResponse(ctx, target)
	if err != nil {
		failed, failErr := s.finishSyncFailure(ctx, result, instance, syncedAt, err)
		return failed, SidecarAuthFile{}, false, failErr
	}
	deleteSupported := s.authDeleteCapabilityKnownSupported(ctx, target, authFilesResponse)
	authInputs := make([]SidecarAuthFileInput, 0, len(authFiles))
	for _, raw := range authFiles {
		input, err := normalizeSidecarAuthFileWithDeleteSupport(instance.ID, syncedAt, raw, deleteSupported)
		if err != nil {
			failed, failErr := s.finishSyncFailure(ctx, result, instance, syncedAt, err)
			return failed, SidecarAuthFile{}, false, failErr
		}
		authInputs = append(authInputs, input)
	}
	refreshedAuthFiles, err := s.store.ReplaceAuthFiles(ctx, instance.ID, authInputs)
	if err != nil {
		failed, failErr := s.finishSyncFailure(ctx, result, instance, syncedAt, err)
		return failed, SidecarAuthFile{}, false, failErr
	}
	for _, file := range refreshedAuthFiles {
		if file.MutationSafe && file.AuthID == authID {
			updated, err := s.finishSyncSuccess(ctx, instance, syncedAt)
			if err != nil {
				return result, SidecarAuthFile{}, false, err
			}
			result.Sidecar = updated
			return result, file, true, nil
		}
	}
	failed, failErr := s.finishSyncFailure(ctx, result, instance, syncedAt, notFoundError("auth file not found in live sidecar state after mutation"))
	return failed, SidecarAuthFile{}, false, failErr
}

func (s *Service) operatorDeleteResponseAfterSync(ctx context.Context, target operatorMutationTarget) (authMutationResponse, error) {
	result, syncErr := s.refreshAuthFilesAfterDelete(ctx, target.Instance, target.AuthFile.AuthID, target.DeleteSupported)
	response := authMutationResponse{State: "succeeded"}
	if result.Sidecar.ID != 0 {
		syncStatus := buildSidecarSyncStatusResponse(s.sidecarSyncStatus(result.Sidecar))
		response.SyncStatus = &syncStatus
	}
	if syncErr != nil {
		if isDeletedAuthFileStillPresentError(syncErr) {
			return response, syncErr
		}
		response.State = "succeeded_sync_failed"
		authFile := buildAuthFileResponse(target.AuthFile)
		response.Snapshot = &authFile
		detail := sidecarErrorMessage(syncErr)
		response.SyncError = &detail
		return response, nil
	}
	return response, nil
}

func (s *Service) refreshAuthFilesAfterDelete(ctx context.Context, instance SidecarInstance, deletedAuthID string, deleteSupported bool) (SidecarSyncResult, error) {
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
	authInputs := make([]SidecarAuthFileInput, 0, len(authFiles))
	for _, raw := range authFiles {
		input, err := normalizeSidecarAuthFileWithDeleteSupport(instance.ID, syncedAt, raw, deleteSupported)
		if err != nil {
			return s.finishSyncFailure(ctx, result, instance, syncedAt, err)
		}
		if input.AuthID == deletedAuthID {
			return s.finishSyncFailure(ctx, result, instance, syncedAt, conflictError(deletedAuthFileStillPresentDetail))
		}
		authInputs = append(authInputs, input)
	}
	if _, err := s.store.ReplaceAuthFiles(ctx, instance.ID, authInputs); err != nil {
		return s.finishSyncFailure(ctx, result, instance, syncedAt, err)
	}
	updated, err := s.finishSyncSuccess(ctx, instance, syncedAt)
	if err != nil {
		return result, err
	}
	result.Sidecar = updated
	return result, nil
}

func isDeletedAuthFileStillPresentError(err error) bool {
	var storeErr *StoreError
	return errors.As(err, &storeErr) && storeErr.Code == StoreErrorConflict && storeErr.Message == deletedAuthFileStillPresentDetail
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
	if unknown := mutationUnknownFields(raw, fieldsMutationAllowedFields); len(unknown) > 0 {
		return nil, unknown, fmt.Errorf("unsupported fields: %s", strings.Join(unknown, ", "))
	}
	payload := map[string]any{"name": authName}
	fields := make([]string, 0, 1)
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
