package sidecars

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const sidecarConditionUnobservable = "condition_unobservable"

var errSidecarManagementPasswordMissing = errors.New("management password is not configured")

type SidecarSyncResult struct {
	Sidecar               SidecarInstance
	SyncedAt              time.Time
	AuthSnapshotCount     int
	ProviderSnapshotCount int
	Skipped               bool
	SkipReason            string
	ErrorCode             string
	ErrorDetail           string
}

type SidecarSyncSummary struct {
	Checked int
	Synced  int
	Skipped int
	Failed  int
}

type SidecarSyncStatus struct {
	SidecarID             int
	Enabled               bool
	SyncIntervalSeconds   int
	ManagementAuthState   string
	LastSyncAt            *time.Time
	LastSuccessfulSyncAt  *time.Time
	SnapshotStaleAfter    *time.Time
	LastSyncError         *string
	AuthFailurePauseUntil *time.Time
	Stale                 bool
	Due                   bool
	Paused                bool
}

func (s *Service) SyncSidecar(ctx context.Context, sidecarID int) (SidecarSyncResult, error) {
	if s == nil || s.store == nil {
		return SidecarSyncResult{}, fmt.Errorf("sidecar service unavailable")
	}
	instance, found, err := s.store.GetSidecarInstance(ctx, sidecarID)
	if err != nil {
		return SidecarSyncResult{}, err
	}
	if !found {
		return SidecarSyncResult{}, notFoundError("sidecar instance not found")
	}
	if !instance.Enabled {
		result := SidecarSyncResult{Sidecar: instance, SyncedAt: s.nowUTC(), Skipped: true, SkipReason: "sidecar_disabled", ErrorCode: "sidecar_disabled", ErrorDetail: "sidecar is disabled"}
		return result, &domainError{StatusCode: http.StatusConflict, Detail: "sidecar is disabled"}
	}
	return s.syncSidecarInstance(ctx, instance)
}

func (s *Service) SyncDueSidecars(ctx context.Context) (SidecarSyncSummary, error) {
	if s == nil || s.store == nil {
		return SidecarSyncSummary{}, fmt.Errorf("sidecar service unavailable")
	}
	instances, err := s.store.ListSidecarInstances(ctx)
	if err != nil {
		return SidecarSyncSummary{}, err
	}
	summary := SidecarSyncSummary{Checked: len(instances)}
	now := s.nowUTC()
	for _, instance := range instances {
		if !s.sidecarDueForPeriodicSync(instance, now) {
			summary.Skipped++
			continue
		}
		result, syncErr := s.syncSidecarInstance(ctx, instance)
		if result.Skipped {
			summary.Skipped++
			continue
		}
		if syncErr != nil {
			summary.Failed++
			continue
		}
		summary.Synced++
	}
	return summary, nil
}

func (s *Service) syncSidecarInstance(ctx context.Context, instance SidecarInstance) (SidecarSyncResult, error) {
	syncedAt := s.nowUTC()
	result := SidecarSyncResult{Sidecar: instance, SyncedAt: syncedAt}
	password, err := s.decryptManagementPassword(instance.EncryptedManagementPassword)
	if err != nil || strings.TrimSpace(password) == "" {
		if err == nil {
			err = errSidecarManagementPasswordMissing
		}
		return s.finishSyncFailure(ctx, result, instance, syncedAt, err)
	}
	target := CLIProxyTarget{
		BaseURL:               instance.BaseURLCanonical,
		ManagementPassword:    password,
		AllowPrivateNetwork:   instance.AllowPrivateNetwork,
		AllowInsecureHTTP:     instance.AllowInsecureHTTP,
		SkipTLSVerify:         instance.SkipTLSVerify,
		RequestTimeoutSeconds: instance.RequestTimeoutSeconds,
	}
	authInputs, providerBatches, err := s.collectSidecarSnapshots(ctx, instance.ID, syncedAt, target)
	if err != nil {
		return s.finishSyncFailure(ctx, result, instance, syncedAt, err)
	}
	if _, err := s.store.ReplaceAuthSnapshots(ctx, instance.ID, authInputs); err != nil {
		return s.finishSyncFailure(ctx, result, instance, syncedAt, err)
	}
	providerSnapshotCount := 0
	for _, batch := range providerBatches {
		if batch.Replace {
			if _, err := s.store.ReplaceProviderSnapshots(ctx, instance.ID, batch.ProviderKey, batch.Inputs); err != nil {
				return s.finishSyncFailure(ctx, result, instance, syncedAt, err)
			}
			providerSnapshotCount += len(batch.Inputs)
			continue
		}
		for _, input := range batch.Inputs {
			if _, err := s.store.SaveProviderSnapshot(ctx, input); err != nil {
				return s.finishSyncFailure(ctx, result, instance, syncedAt, err)
			}
			providerSnapshotCount++
		}
	}
	updated, err := s.finishSyncSuccess(ctx, instance, syncedAt)
	if err != nil {
		return result, err
	}
	result.Sidecar = updated
	result.AuthSnapshotCount = len(authInputs)
	result.ProviderSnapshotCount = providerSnapshotCount
	return result, nil
}

func (s *Service) collectSidecarSnapshots(ctx context.Context, sidecarID int, observedAt time.Time, target CLIProxyTarget) ([]SidecarAuthSnapshotInput, []SidecarProviderSnapshotBatch, error) {
	authFiles, err := s.fetchSidecarAuthFileRows(ctx, target)
	if err != nil {
		return nil, nil, err
	}
	authInputs := make([]SidecarAuthSnapshotInput, 0, len(authFiles))
	for _, raw := range authFiles {
		input, err := normalizeSidecarAuthSnapshot(sidecarID, observedAt, raw)
		if err != nil {
			return nil, nil, err
		}
		authInputs = append(authInputs, input)
	}
	providerBatches := make([]SidecarProviderSnapshotBatch, 0, len(sidecarProviderSyncEndpoints))
	for _, endpoint := range sidecarProviderSyncEndpoints {
		var payload map[string]json.RawMessage
		if _, err := s.cliProxyClient.FetchJSON(ctx, target, http.MethodGet, endpoint.Path, nil, &payload); err != nil {
			providerBatches = append(providerBatches, SidecarProviderSnapshotBatch{ProviderKey: endpoint.ResponseKey, Inputs: []SidecarProviderSnapshotInput{providerInventoryFailureSnapshotInput(sidecarID, observedAt, endpoint.ResponseKey, endpoint.Path, err)}})
			continue
		}
		inputs, err := normalizeSidecarProviderSnapshots(sidecarID, observedAt, endpoint.ResponseKey, payload)
		if err != nil {
			providerBatches = append(providerBatches, SidecarProviderSnapshotBatch{ProviderKey: endpoint.ResponseKey, Inputs: []SidecarProviderSnapshotInput{providerInventoryFailureSnapshotInput(sidecarID, observedAt, endpoint.ResponseKey, endpoint.Path, err)}})
			continue
		}
		providerBatches = append(providerBatches, SidecarProviderSnapshotBatch{ProviderKey: endpoint.ResponseKey, Inputs: inputs, Replace: true})
	}
	return authInputs, providerBatches, nil
}

func (s *Service) finishSyncSuccess(ctx context.Context, instance SidecarInstance, syncedAt time.Time) (SidecarInstance, error) {
	staleAfter := syncedAt.Add(2 * syncIntervalDuration(instance))
	return s.store.UpdateSidecarSyncMetadata(ctx, SidecarSyncMetadataInput{
		SidecarID:             instance.ID,
		LastSyncAt:            syncedAt,
		LastSuccessfulSyncAt:  &syncedAt,
		SnapshotStaleAfter:    &staleAfter,
		ManagementAuthState:   ManagementAuthStateValid,
		AuthFailurePauseUntil: nil,
	})
}

func (s *Service) finishSyncFailure(ctx context.Context, result SidecarSyncResult, instance SidecarInstance, syncedAt time.Time, err error) (SidecarSyncResult, error) {
	errorDetail, errorCode := redactedSidecarSyncError(err)
	state := instance.ManagementAuthState
	pauseUntil := cloneTimePtr(instance.AuthFailurePauseUntil)
	if state == "" {
		state = ManagementAuthStateUnknown
	}
	if isInvalidManagementAuthError(err) || errors.Is(err, errSidecarManagementPasswordMissing) || strings.TrimSpace(instance.EncryptedManagementPassword) == "" {
		state = ManagementAuthStateInvalid
		until := syncedAt.Add(syncIntervalDuration(instance))
		pauseUntil = &until
	}
	updated, updateErr := s.store.UpdateSidecarSyncMetadata(ctx, SidecarSyncMetadataInput{
		SidecarID:             instance.ID,
		LastSyncAt:            syncedAt,
		LastSuccessfulSyncAt:  cloneTimePtr(instance.LastSuccessfulSyncAt),
		SnapshotStaleAfter:    &syncedAt,
		LastSyncError:         &errorDetail,
		ManagementAuthState:   state,
		AuthFailurePauseUntil: pauseUntil,
	})
	if updateErr != nil {
		return result, updateErr
	}
	result.Sidecar = updated
	result.ErrorCode = errorCode
	result.ErrorDetail = errorDetail
	return result, err
}

func (s *Service) sidecarSyncStatus(instance SidecarInstance) SidecarSyncStatus {
	now := s.nowUTC()
	return SidecarSyncStatus{
		SidecarID:             instance.ID,
		Enabled:               instance.Enabled,
		SyncIntervalSeconds:   normalizedSyncIntervalSeconds(instance),
		ManagementAuthState:   nonEmptyManagementAuthState(instance.ManagementAuthState),
		LastSyncAt:            cloneTimePtr(instance.LastSyncAt),
		LastSuccessfulSyncAt:  cloneTimePtr(instance.LastSuccessfulSyncAt),
		SnapshotStaleAfter:    cloneTimePtr(instance.SnapshotStaleAfter),
		LastSyncError:         cloneStringPtr(instance.LastSyncError),
		AuthFailurePauseUntil: cloneTimePtr(instance.AuthFailurePauseUntil),
		Stale:                 sidecarSnapshotsStale(instance, now),
		Due:                   s.sidecarDueForPeriodicSync(instance, now),
		Paused:                sidecarSyncPaused(instance, now),
	}
}

func (s *Service) sidecarDueForPeriodicSync(instance SidecarInstance, now time.Time) bool {
	if !instance.Enabled || sidecarSyncPaused(instance, now) {
		return false
	}
	if instance.LastSuccessfulSyncAt == nil {
		return true
	}
	return !now.Before(instance.LastSuccessfulSyncAt.Add(syncIntervalDuration(instance)))
}

func sidecarSyncPaused(instance SidecarInstance, now time.Time) bool {
	return instance.AuthFailurePauseUntil != nil && now.Before(instance.AuthFailurePauseUntil.UTC())
}

func sidecarSnapshotsStale(instance SidecarInstance, now time.Time) bool {
	if instance.LastSyncError != nil && strings.TrimSpace(*instance.LastSyncError) != "" {
		return true
	}
	if instance.LastSuccessfulSyncAt == nil {
		return true
	}
	return now.After(instance.LastSuccessfulSyncAt.Add(2 * syncIntervalDuration(instance)))
}

func syncIntervalDuration(instance SidecarInstance) time.Duration {
	return time.Duration(normalizedSyncIntervalSeconds(instance)) * time.Second
}

func normalizedSyncIntervalSeconds(instance SidecarInstance) int {
	if instance.SyncIntervalSeconds <= 0 {
		return DefaultSyncIntervalSeconds
	}
	return instance.SyncIntervalSeconds
}

func nonEmptyManagementAuthState(value string) string {
	state := strings.TrimSpace(value)
	if state == "" {
		return ManagementAuthStateUnknown
	}
	return state
}

func redactedSidecarSyncError(err error) (string, string) {
	if err == nil {
		return "", ""
	}
	var contractErr *sidecarSyncContractError
	if errors.As(err, &contractErr) {
		return err.Error(), "sync_contract"
	}
	var clientErr *CLIProxyClientError
	if errors.As(err, &clientErr) {
		detail := string(clientErr.Code)
		if clientErr.Path != "" {
			detail += " on " + clientErr.Path
		}
		if clientErr.StatusCode != 0 {
			detail += fmt.Sprintf(" status=%d", clientErr.StatusCode)
		}
		return detail, string(clientErr.Code)
	}
	detail := strings.TrimSpace(err.Error())
	if len(detail) > 500 {
		detail = detail[:500]
	}
	return detail, "sync_failed"
}

func isInvalidManagementAuthError(err error) bool {
	var clientErr *CLIProxyClientError
	return errors.As(err, &clientErr) && clientErr.Code == CLIProxyErrorInvalidManagementAuth
}

type sidecarSyncContractError struct {
	Detail string
}

func (err *sidecarSyncContractError) Error() string {
	if err == nil {
		return ""
	}
	return "sync contract violation: " + err.Detail
}

func newSidecarSyncContractError(detail string) error {
	return &sidecarSyncContractError{Detail: detail}
}

func (s *Service) fetchSidecarAuthFileRows(ctx context.Context, target CLIProxyTarget) ([]json.RawMessage, error) {
	var envelope map[string]json.RawMessage
	if _, err := s.cliProxyClient.FetchJSON(ctx, target, http.MethodGet, "/auth-files", nil, &envelope); err != nil {
		return nil, err
	}
	return decodeSidecarAuthFileRows(envelope)
}

func decodeSidecarAuthFileRows(envelope map[string]json.RawMessage) ([]json.RawMessage, error) {
	filesRaw, ok := envelope["files"]
	if !ok {
		return nil, newSidecarSyncContractError("/auth-files response files must be present")
	}
	trimmed := bytes.TrimSpace(filesRaw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) || trimmed[0] != '[' {
		return nil, newSidecarSyncContractError("/auth-files response files must be an array")
	}
	var files []json.RawMessage
	if err := json.Unmarshal(trimmed, &files); err != nil || files == nil {
		return nil, newSidecarSyncContractError("/auth-files response files must be an array")
	}
	return files, nil
}

func normalizeSidecarAuthSnapshot(sidecarID int, observedAt time.Time, raw json.RawMessage) (SidecarAuthSnapshotInput, error) {
	fields, err := decodeSidecarSyncObject(raw)
	if err != nil {
		return SidecarAuthSnapshotInput{}, err
	}
	authID := firstNonEmptyString(fields, "id", "auth_id", "auth_index", "name")
	name := firstNonEmptyString(fields, "name", "id", "auth_index")
	if authID == "" || name == "" {
		return SidecarAuthSnapshotInput{}, invalidInputError("auth snapshot requires id or name")
	}
	snapshot, err := normalizedAuthSnapshotJSON(fields)
	if err != nil {
		return SidecarAuthSnapshotInput{}, err
	}
	recentRequests, err := syncJSONFieldOrDefault(fields, "recent_requests", "[]")
	if err != nil {
		return SidecarAuthSnapshotInput{}, err
	}
	modelStates, err := syncJSONFieldOrDefault(fields, "model_states", "{}")
	if err != nil {
		return SidecarAuthSnapshotInput{}, err
	}
	quotaExceeded, quotaReason, quotaNextRecoverAt := quotaSnapshotFields(fields)
	return SidecarAuthSnapshotInput{
		SidecarID:          sidecarID,
		AuthID:             authID,
		AuthIndex:          stringPtrFromKeys(fields, "auth_index"),
		Name:               name,
		Provider:           stringPtrFromKeys(fields, "provider", "type"),
		Label:              stringPtrFromKeys(fields, "label"),
		Status:             stringPtrFromKeys(fields, "status"),
		StatusMessage:      stringPtrFromKeys(fields, "status_message"),
		Disabled:           boolPtrFromKey(fields, "disabled"),
		Unavailable:        boolPtrFromKey(fields, "unavailable"),
		Priority:           intPtrFromKey(fields, "priority"),
		QuotaExceeded:      quotaExceeded,
		QuotaReason:        quotaReason,
		QuotaNextRecoverAt: quotaNextRecoverAt,
		NextRetryAfter:     timePtrFromKey(fields, "next_retry_after"),
		SuccessCount:       intPtrFromKey(fields, "success"),
		FailedCount:        intPtrFromKey(fields, "failed"),
		RecentRequestsJSON: recentRequests,
		ModelStatesJSON:    modelStates,
		SnapshotJSON:       snapshot,
		ObservedAt:         observedAt,
	}, nil
}

func normalizedAuthSnapshotJSON(fields map[string]any) (json.RawMessage, error) {
	snapshot := map[string]any{}
	for _, key := range []string{"id", "auth_index", "name", "type", "provider", "label", "status", "status_message", "disabled", "unavailable", "priority", "success", "failed", "quota", "next_retry_after", "recent_requests", "model_states", "note"} {
		if value, ok := fields[key]; ok {
			snapshot[key] = sanitizeSidecarSnapshotValue(key, value)
		}
	}
	missing := missingSyncFields(fields, "quota", "model_states", "recent_requests")
	if len(missing) > 0 {
		snapshot["condition"] = sidecarConditionUnobservable
		snapshot["unobservable_fields"] = missing
	}
	return marshalSidecarSnapshotJSON(snapshot)
}

func quotaSnapshotFields(fields map[string]any) (*bool, *string, *time.Time) {
	quota, ok := mapValue(fields["quota"])
	if !ok {
		return nil, nil, nil
	}
	return boolPtrFromKey(quota, "exceeded"), stringPtrFromKeys(quota, "reason"), timePtrFromKey(quota, "next_recover_at")
}

func decodeSidecarSyncObject(raw json.RawMessage) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var fields map[string]any
	if err := decoder.Decode(&fields); err != nil {
		return nil, invalidInputError("sidecar snapshot entry must be a JSON object")
	}
	if fields == nil {
		return nil, invalidInputError("sidecar snapshot entry must be a JSON object")
	}
	return fields, nil
}

func syncJSONFieldOrDefault(fields map[string]any, key string, fallback string) (json.RawMessage, error) {
	value, ok := fields[key]
	if !ok || value == nil {
		return json.RawMessage(fallback), nil
	}
	return marshalSidecarSnapshotJSON(sanitizeSidecarSnapshotValue(key, value))
}

func marshalSidecarSnapshotJSON(value any) (json.RawMessage, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, invalidInputError("sidecar snapshot JSON could not be normalized")
	}
	return json.RawMessage(payload), nil
}

func sanitizeSidecarSnapshotObject(fields map[string]any) map[string]any {
	copy := make(map[string]any, len(fields))
	for key, value := range fields {
		copy[key] = sanitizeSidecarSnapshotValue(key, value)
	}
	return copy
}

func sanitizeSidecarSnapshotValue(key string, value any) any {
	if isSensitiveSnapshotKey(key) {
		if text, ok := value.(string); ok && isAllowedRedactedSecretValue(text) {
			return text
		}
		return "redacted-by-prism"
	}
	switch typed := value.(type) {
	case map[string]any:
		return sanitizeSidecarSnapshotObject(typed)
	case []any:
		items := make([]any, 0, len(typed))
		for _, item := range typed {
			items = append(items, sanitizeSidecarSnapshotValue("", item))
		}
		return items
	default:
		return value
	}
}

func missingSyncFields(fields map[string]any, keys ...string) []string {
	missing := make([]string, 0)
	for _, key := range keys {
		if value, ok := fields[key]; !ok || value == nil {
			missing = append(missing, key)
		}
	}
	return missing
}

func firstNonEmptyString(fields map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := trimmedStringValue(fields[key]); value != "" {
			return value
		}
	}
	return ""
}

func stringPtrFromKeys(fields map[string]any, keys ...string) *string {
	for _, key := range keys {
		if value := trimmedStringValue(fields[key]); value != "" {
			return &value
		}
	}
	return nil
}

func boolPtrFromKey(fields map[string]any, key string) *bool {
	value, ok := fields[key]
	if !ok || value == nil {
		return nil
	}
	if typed, ok := value.(bool); ok {
		copy := typed
		return &copy
	}
	return nil
}

func intPtrFromKey(fields map[string]any, key string) *int {
	value, ok := intFromValue(fields[key])
	if !ok {
		return nil
	}
	return &value
}

func timePtrFromKey(fields map[string]any, key string) *time.Time {
	value := trimmedStringValue(fields[key])
	if value == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil
	}
	parsed = parsed.UTC()
	return &parsed
}

func trimmedStringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return strings.TrimSpace(typed.String())
	default:
		return ""
	}
}

func intFromValue(value any) (int, bool) {
	switch typed := value.(type) {
	case json.Number:
		parsed, err := typed.Int64()
		if err == nil {
			return int(parsed), true
		}
	case float64:
		return int(typed), true
	case int:
		return typed, true
	}
	return 0, false
}

func mapValue(value any) (map[string]any, bool) {
	typed, ok := value.(map[string]any)
	return typed, ok
}
