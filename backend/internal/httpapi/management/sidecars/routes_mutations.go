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

var errOperatorMutationStaleSnapshot = errors.New("stale_snapshot")

var statusMutationAllowedFields = map[string]struct{}{
	"allow_watchdog": {},
	"disabled":       {},
	"force_live":     {},
}

var fieldsMutationAllowedFields = map[string]struct{}{
	"allow_watchdog": {},
	"custom_headers": {},
	"force_live":     {},
	"headers":        {},
	"note":           {},
	"prefix":         {},
	"priority":       {},
	"proxy_url":      {},
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
	Instance SidecarInstance
	Snapshot SidecarAuthSnapshot
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
		s.recordOperatorMutationRequestFailure(r.Context(), sidecarID, authID, "status", nil, "Invalid request body")
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	forceLive, err := mutationControlBool(raw, r, "force_live")
	if err != nil {
		s.recordOperatorMutationRequestFailure(r.Context(), sidecarID, authID, "status", mutationFieldNames(raw), err.Error())
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	allowWatchdog, err := mutationControlBool(raw, r, "allow_watchdog")
	if err != nil {
		s.recordOperatorMutationRequestFailure(r.Context(), sidecarID, authID, "status", mutationFieldNames(raw), err.Error())
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	if !s.tryAcquireWatchdogRun(sidecarID) {
		s.recordOperatorMutationLockFailure(r.Context(), sidecarID, authID, "status", mutationFieldNames(raw))
		writeError(w, r, s.corsSnapshot(), http.StatusConflict, "sidecar mutation already running")
		return
	}
	defer s.releaseWatchdogRun(sidecarID)
	target, err := s.loadOperatorMutationTarget(r.Context(), sidecarID, authID, forceLive)
	if err != nil {
		s.handleOperatorMutationLoadError(w, r, sidecarID, authID, "status", mutationFieldNames(raw), err)
		return
	}
	if unknown := mutationUnknownFields(raw, statusMutationAllowedFields); len(unknown) > 0 {
		s.rejectOperatorMutation(w, r, target, "status", unknown, fmt.Sprintf("unsupported fields: %s", strings.Join(unknown, ", ")))
		return
	}
	disabled, err := requiredMutationBool(raw, "disabled")
	if err != nil {
		s.rejectOperatorMutation(w, r, target, "status", []string{"disabled"}, err.Error())
		return
	}
	payload := map[string]any{"name": target.Snapshot.Name, "disabled": disabled}
	fields := []string{"disabled"}
	response, patchErr := s.patchOperatorAuthFile(r.Context(), target.Instance, "/auth-files/status", payload)
	s.finishOperatorMutation(w, r, target, "status", fields, response, patchErr, allowWatchdog)
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
		s.recordOperatorMutationRequestFailure(r.Context(), sidecarID, authID, "fields", nil, "Invalid request body")
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	forceLive, err := mutationControlBool(raw, r, "force_live")
	if err != nil {
		s.recordOperatorMutationRequestFailure(r.Context(), sidecarID, authID, "fields", mutationFieldNames(raw), err.Error())
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	allowWatchdog, err := mutationControlBool(raw, r, "allow_watchdog")
	if err != nil {
		s.recordOperatorMutationRequestFailure(r.Context(), sidecarID, authID, "fields", mutationFieldNames(raw), err.Error())
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	if !s.tryAcquireWatchdogRun(sidecarID) {
		s.recordOperatorMutationLockFailure(r.Context(), sidecarID, authID, "fields", mutationFieldNames(raw))
		writeError(w, r, s.corsSnapshot(), http.StatusConflict, "sidecar mutation already running")
		return
	}
	defer s.releaseWatchdogRun(sidecarID)
	target, err := s.loadOperatorMutationTarget(r.Context(), sidecarID, authID, forceLive)
	if err != nil {
		s.handleOperatorMutationLoadError(w, r, sidecarID, authID, "fields", mutationFieldNames(raw), err)
		return
	}
	if unknown := mutationUnknownFields(raw, fieldsMutationAllowedFields); len(unknown) > 0 {
		s.rejectOperatorMutation(w, r, target, "fields", unknown, fmt.Sprintf("unsupported fields: %s", strings.Join(unknown, ", ")))
		return
	}
	payload, fields, err := buildOperatorFieldsPayload(raw, target.Snapshot.Name)
	if err != nil {
		if len(fields) == 0 {
			fields = mutationFieldNames(raw)
		}
		s.rejectOperatorMutation(w, r, target, "fields", fields, err.Error())
		return
	}
	response, patchErr := s.patchOperatorAuthFile(r.Context(), target.Instance, "/auth-files/fields", payload)
	s.finishOperatorMutation(w, r, target, "fields", fields, response, patchErr, allowWatchdog)
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
		return operatorMutationTarget{Instance: instance, Snapshot: snapshot}, nil
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

func (s *Service) finishOperatorMutation(w http.ResponseWriter, r *http.Request, target operatorMutationTarget, route string, fields []string, upstreamResponse map[string]any, patchErr error, allowWatchdog bool) {
	now := s.nowUTC()
	actionCtx := operatorPatchContextFromSnapshot(target.Snapshot, route, fields)
	actionCtx.Response = upstreamResponse
	if patchErr != nil {
		_, _ = s.recordOperatorPatchAction(r.Context(), actionCtx, watchdogActionStatusFailed, patchErr, now)
		s.writeOperatorMutationPatchError(w, r, target.Instance, patchErr)
		return
	}
	hold, err := s.pauseWatchdogForOperatorPatch(r.Context(), target.Snapshot, allowWatchdog, now)
	if err != nil {
		_, _ = s.recordOperatorPatchAction(r.Context(), actionCtx, watchdogActionStatusFailed, err, now)
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	actionCtx = actionCtx.withHold(hold)
	_, _ = s.recordOperatorPatchAction(r.Context(), actionCtx, watchdogActionStatusSucceeded, nil, now)
	writeJSON(w, http.StatusOK, s.operatorMutationResponseAfterSync(r.Context(), target))
}

func (s *Service) rejectOperatorMutation(w http.ResponseWriter, r *http.Request, target operatorMutationTarget, route string, fields []string, detail string) {
	err := &domainError{StatusCode: http.StatusBadRequest, Detail: detail}
	actionCtx := operatorPatchContextFromSnapshot(target.Snapshot, route, fields)
	_, _ = s.recordOperatorPatchAction(r.Context(), actionCtx, watchdogActionStatusFailed, err, s.nowUTC())
	writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, detail)
}

func (s *Service) handleOperatorMutationLoadError(w http.ResponseWriter, r *http.Request, sidecarID int, authID string, route string, fields []string, err error) {
	actionCtx := operatorPatchActionContext{SidecarID: sidecarID, AuthID: authID, Route: route, Fields: fields}
	if snapshot, found, loadErr := s.store.GetAuthSnapshot(r.Context(), sidecarID, authID); loadErr == nil && found {
		actionCtx = operatorPatchContextFromSnapshot(snapshot, route, fields)
	}
	_, _ = s.recordOperatorPatchAction(r.Context(), actionCtx, watchdogActionStatusFailed, err, s.nowUTC())
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

func (s *Service) recordOperatorMutationLockFailure(ctx context.Context, sidecarID int, authID string, route string, fields []string) {
	s.recordOperatorMutationRequestFailure(ctx, sidecarID, authID, route, fields, "sidecar mutation already running")
}

func (s *Service) recordOperatorMutationRequestFailure(ctx context.Context, sidecarID int, authID string, route string, fields []string, detail string) {
	err := &domainError{StatusCode: http.StatusBadRequest, Detail: detail}
	actionCtx := operatorPatchActionContext{SidecarID: sidecarID, AuthID: authID, Route: route, Fields: fields}
	_, _ = s.recordOperatorPatchAction(ctx, actionCtx, watchdogActionStatusFailed, err, s.nowUTC())
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

func (s *Service) pauseWatchdogForOperatorPatch(ctx context.Context, snapshot SidecarAuthSnapshot, allowWatchdog bool, now time.Time) (*SidecarWatchdogHold, error) {
	if allowWatchdog {
		return nil, nil
	}
	policy, err := s.store.GetOrCreateWatchdogPolicy(ctx, snapshot.SidecarID)
	if err != nil {
		return nil, err
	}
	manualPauseUntil := now.Add(time.Duration(normalizedManualPauseSeconds(policy)) * time.Second)
	hold, found, err := s.store.GetActiveWatchdogHold(ctx, snapshot.SidecarID, snapshot.AuthID)
	if err != nil {
		return nil, err
	}
	if found {
		hold.ManualPauseUntil = &manualPauseUntil
		hold.Status = WatchdogHoldStatusPaused
		updated, err := s.store.UpdateWatchdogHold(ctx, hold.ID, watchdogHoldToInput(hold))
		return &updated, err
	}
	hold, err = s.store.CreateWatchdogHold(ctx, SidecarWatchdogHoldInput{SidecarID: snapshot.SidecarID, AuthID: snapshot.AuthID, AuthIndex: cloneStringPtr(snapshot.AuthIndex), Provider: cloneStringPtr(snapshot.Provider), Reason: watchdogReasonManualOperatorPatch, ConditionHash: manualOperatorPatchConditionHash(snapshot.AuthID), PreviousPriority: cloneIntPtr(snapshot.Priority), TargetPriority: normalizedWatchdogTargetPriority(policy), ManualPauseUntil: &manualPauseUntil, Status: WatchdogHoldStatusPaused})
	if err != nil {
		return nil, err
	}
	return &hold, nil
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
		detail := watchdogErrorMessage(syncErr)
		response.SyncError = &detail
	}
	return response
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
			text, err := mutationNullableString(rawValue, name)
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

func mutationNullableString(raw json.RawMessage, name string) (string, error) {
	if string(raw) == "null" {
		return "", nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("%s must be a string", name)
	}
	return value, nil
}

func mutationHeaders(raw map[string]json.RawMessage) (map[string]string, []string, error) {
	headerRaw, headerSet := raw["headers"]
	customRaw, customSet := raw["custom_headers"]
	if headerSet && customSet {
		return nil, []string{"headers", "custom_headers"}, fmt.Errorf("headers and custom_headers cannot both be set")
	}
	if customSet {
		headerRaw = customRaw
		headerSet = true
	}
	if !headerSet {
		return nil, nil, nil
	}
	var rawHeaders map[string]json.RawMessage
	if err := json.Unmarshal(headerRaw, &rawHeaders); err != nil || rawHeaders == nil {
		return nil, []string{"headers"}, fmt.Errorf("headers must be an object")
	}
	headers := make(map[string]string, len(rawHeaders))
	fields := make([]string, 0, len(rawHeaders))
	for key, rawValue := range rawHeaders {
		name := strings.TrimSpace(key)
		fieldName := "headers." + name
		if err := validateMutationHeaderName(name); err != nil {
			return nil, []string{fieldName}, err
		}
		value, err := mutationNullableString(rawValue, fieldName)
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
		return nil
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

func mutationFieldNames(raw map[string]json.RawMessage) []string {
	fields := make([]string, 0, len(raw))
	for key := range raw {
		fields = append(fields, key)
	}
	sort.Strings(fields)
	return fields
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

func manualOperatorPatchConditionHash(authID string) string {
	return "manual_operator_patch:" + strings.TrimSpace(authID)
}
