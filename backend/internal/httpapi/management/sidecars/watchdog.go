package sidecars

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	WatchdogHoldStatusActive   = "active"
	WatchdogHoldStatusPaused   = "paused"
	WatchdogHoldStatusReleased = "released"

	watchdogActionStatusSucceeded = "succeeded"
	watchdogActionStatusFailed    = "failed"
	watchdogActionStatusSkipped   = "skipped"

	watchdogActionDeprioritize                = "deprioritize"
	watchdogActionRestore                     = "restore"
	watchdogActionSkippedStaleSnapshot        = "watchdog_skipped_stale_snapshot"
	watchdogActionSkippedManagementAuthPause  = "watchdog_skipped_management_auth_pause"
	watchdogActionRestoreSkippedManualPause   = "restore_skipped_manual_pause"
	watchdogActionRestoreSkippedManualChange  = "restore_skipped_manual_override"
	watchdogActionRestoreSkippedMissingAuth   = "restore_skipped_missing_auth"
	watchdogActionRestoreSkippedUnhealthy     = "restore_skipped_unhealthy"
	watchdogActionRestoreSkippedNeedsOperator = "restore_skipped_needs_operator"

	watchdogReasonQuotaExceeded       = "quota_exceeded"
	watchdogReasonQuotaRecoverPending = "quota_recover_pending"
	watchdogReasonUnavailable         = "unavailable"
	watchdogReasonFailureThreshold    = "failure_threshold"
)

type SidecarWatchdogSummary struct {
	Checked    int
	Reconciled int
	Skipped    int
	Failed     int
}

type SidecarWatchdogResult struct {
	SidecarID   int
	Reconciled  bool
	Skipped     bool
	SkipReason  string
	ActionCount int
}

type watchdogCondition struct {
	Triggered    bool
	Reason       string
	FailureCount int
	HoldUntil    time.Time
	Hash         string
}

func (s *Service) ReconcileWatchdogDueSidecars(ctx context.Context) (SidecarWatchdogSummary, error) {
	if s == nil || s.store == nil {
		return SidecarWatchdogSummary{}, fmt.Errorf("sidecar service unavailable")
	}
	instances, err := s.store.ListSidecarInstances(ctx)
	if err != nil {
		return SidecarWatchdogSummary{}, err
	}
	summary := SidecarWatchdogSummary{Checked: len(instances)}
	for _, instance := range instances {
		result, reconcileErr := s.ReconcileSidecarWatchdog(ctx, instance.ID)
		if reconcileErr != nil {
			summary.Failed++
			continue
		}
		if result.Reconciled {
			summary.Reconciled++
			continue
		}
		summary.Skipped++
	}
	return summary, nil
}

func (s *Service) ReconcileSidecarWatchdog(ctx context.Context, sidecarID int) (SidecarWatchdogResult, error) {
	if s == nil || s.store == nil {
		return SidecarWatchdogResult{}, fmt.Errorf("sidecar service unavailable")
	}
	result := SidecarWatchdogResult{SidecarID: sidecarID}
	if !s.tryAcquireWatchdogRun(sidecarID) {
		result.Skipped = true
		result.SkipReason = "watchdog_already_running"
		return result, nil
	}
	defer s.releaseWatchdogRun(sidecarID)

	instance, found, err := s.store.GetSidecarInstance(ctx, sidecarID)
	if err != nil {
		return result, err
	}
	if !found {
		return result, notFoundError("sidecar instance not found")
	}
	if !instance.Enabled {
		result.Skipped = true
		result.SkipReason = "sidecar_disabled"
		return result, nil
	}
	policy, err := s.store.GetOrCreateWatchdogPolicy(ctx, sidecarID)
	if err != nil {
		return result, err
	}
	if !policy.Enabled {
		result.Skipped = true
		result.SkipReason = "watchdog_disabled"
		return result, nil
	}
	now := s.nowUTC()
	if sidecarSyncPaused(instance, now) {
		reason := "management authentication pause is active"
		recorded, err := s.recordWatchdogSkipOnce(ctx, SidecarWatchdogActionInput{SidecarID: sidecarID, ActionType: watchdogActionSkippedManagementAuthPause, Status: watchdogActionStatusSkipped, Reason: &reason}, now)
		if err != nil {
			return result, err
		}
		result.Skipped = true
		result.SkipReason = watchdogActionSkippedManagementAuthPause
		if recorded {
			result.ActionCount++
		}
		return result, nil
	}
	if sidecarSnapshotsStale(instance, now) {
		reason := "sidecar auth snapshots are stale"
		recorded, err := s.recordWatchdogSkipOnce(ctx, SidecarWatchdogActionInput{SidecarID: sidecarID, ActionType: watchdogActionSkippedStaleSnapshot, Status: watchdogActionStatusSkipped, Reason: &reason}, now)
		if err != nil {
			return result, err
		}
		result.Skipped = true
		result.SkipReason = watchdogActionSkippedStaleSnapshot
		if recorded {
			result.ActionCount++
		}
		return result, nil
	}

	holds, err := s.store.ListActiveWatchdogHolds(ctx, sidecarID)
	if err != nil {
		return result, err
	}
	snapshots, err := s.store.ListAuthSnapshots(ctx, sidecarID)
	if err != nil {
		return result, err
	}
	freshSnapshots := make([]SidecarAuthSnapshot, 0, len(snapshots))
	for _, snapshot := range snapshots {
		if watchdogSnapshotFromLatestSync(instance, snapshot) {
			freshSnapshots = append(freshSnapshots, snapshot)
		}
	}
	snapshotByAuth := map[string]SidecarAuthSnapshot{}
	for _, snapshot := range freshSnapshots {
		snapshotByAuth[snapshot.AuthID] = snapshot
	}
	processedHoldAuths := map[string]struct{}{}
	for _, hold := range holds {
		outcome, holdErr := s.reconcileWatchdogHold(ctx, instance, policy, hold, snapshotByAuth[hold.AuthID], now)
		if holdErr != nil {
			return result, holdErr
		}
		if outcome.ActionRecorded {
			result.ActionCount++
		}
		if outcome.Reconciled {
			result.Reconciled = true
		}
		if outcome.Processed {
			processedHoldAuths[hold.AuthID] = struct{}{}
		}
	}
	for _, snapshot := range freshSnapshots {
		if _, processed := processedHoldAuths[snapshot.AuthID]; processed {
			continue
		}
		if !watchdogAuthEnabled(snapshot) {
			continue
		}
		priority := watchdogAuthPriority(snapshot)
		policyTarget := normalizedWatchdogTargetPriority(policy)
		if priority <= policyTarget {
			continue
		}
		condition := evaluateWatchdogCondition(snapshot, policy, now)
		if !condition.Triggered {
			continue
		}
		outcome, deprioritizeErr := s.reconcileWatchdogDeprioritize(ctx, instance, policy, snapshot, condition, now)
		if deprioritizeErr != nil {
			return result, deprioritizeErr
		}
		if outcome.ActionRecorded {
			result.ActionCount++
		}
		if outcome.Reconciled {
			result.Reconciled = true
		}
	}
	if !result.Reconciled && result.ActionCount == 0 {
		result.Skipped = true
		result.SkipReason = "no_watchdog_action_needed"
	}
	return result, nil
}

type watchdogHoldOutcome struct {
	Processed      bool
	Released       bool
	Reconciled     bool
	ActionRecorded bool
}

func (s *Service) reconcileWatchdogHold(ctx context.Context, instance SidecarInstance, policy SidecarWatchdogPolicy, hold SidecarWatchdogHold, snapshot SidecarAuthSnapshot, now time.Time) (watchdogHoldOutcome, error) {
	outcome := watchdogHoldOutcome{Processed: true}
	if hold.ManualPauseUntil != nil && now.Before(hold.ManualPauseUntil.UTC()) {
		reason := "manual override pause is active"
		recorded, err := s.recordHoldSkipOnce(ctx, hold, snapshot, watchdogActionRestoreSkippedManualPause, &reason, now)
		if err != nil {
			return outcome, err
		}
		outcome.ActionRecorded = recorded
		return outcome, nil
	}
	if hold.Reason == watchdogReasonManualOperatorPatch {
		hold.Status = WatchdogHoldStatusReleased
		hold.ManualPauseUntil = nil
		hold.ReleasedAt = &now
		if _, err := s.store.UpdateWatchdogHold(ctx, hold.ID, watchdogHoldToInput(hold)); err != nil {
			return outcome, err
		}
		outcome.Processed = false
		outcome.Released = true
		return outcome, nil
	}
	if hold.HoldUntil == nil || now.Before(hold.HoldUntil.UTC()) {
		return outcome, nil
	}
	if hold.PreviousPriority == nil {
		reason := "previous priority is missing; operator must choose restore priority"
		updated, err := s.pauseHoldWithAction(ctx, hold, snapshot, policy, watchdogActionRestoreSkippedNeedsOperator, reason, now)
		if err != nil {
			return outcome, err
		}
		_ = updated
		outcome.ActionRecorded = true
		return outcome, nil
	}
	live, found, err := s.fetchLiveAuthSnapshot(ctx, instance, hold.AuthID, now)
	if err != nil {
		return outcome, err
	}
	if !found {
		reason := "auth no longer exists in fresh preflight read"
		if err := s.recordHoldAction(ctx, hold, snapshot, watchdogActionRestoreSkippedMissingAuth, watchdogActionStatusSkipped, &reason, nil, now); err != nil {
			return outcome, err
		}
		outcome.ActionRecorded = true
		return outcome, nil
	}
	if watchdogAuthPriority(live) != hold.TargetPriority {
		reason := "current priority no longer matches watchdog target priority"
		if _, err := s.pauseHoldWithAction(ctx, hold, live, policy, watchdogActionRestoreSkippedManualChange, reason, now); err != nil {
			return outcome, err
		}
		outcome.ActionRecorded = true
		return outcome, nil
	}
	condition := evaluateWatchdogCondition(live, policy, now)
	if !watchdogAuthEnabled(live) || condition.Triggered {
		reason := "fresh preflight auth is not healthy"
		if condition.Triggered {
			reason = condition.Reason
			hold.Reason = condition.Reason
			hold.ConditionHash = condition.Hash
			hold.HoldUntil = &condition.HoldUntil
			if _, err := s.store.UpdateWatchdogHold(ctx, hold.ID, watchdogHoldToInput(hold)); err != nil {
				return outcome, err
			}
		}
		if err := s.recordHoldAction(ctx, hold, live, watchdogActionRestoreSkippedUnhealthy, watchdogActionStatusSkipped, &reason, nil, now); err != nil {
			return outcome, err
		}
		outcome.ActionRecorded = true
		return outcome, nil
	}
	patchErr := s.patchAuthPriority(ctx, instance, live.Name, *hold.PreviousPriority)
	status := watchdogActionStatusSucceeded
	var errorMessage *string
	if patchErr != nil {
		status = watchdogActionStatusFailed
		message := watchdogErrorMessage(patchErr)
		errorMessage = &message
	}
	action, err := s.createHoldAction(ctx, hold, live, watchdogActionRestore, status, &hold.Reason, errorMessage, now)
	if err != nil {
		return outcome, err
	}
	hold.LastActionID = &action.ID
	if patchErr == nil {
		hold.Status = WatchdogHoldStatusReleased
		hold.ReleasedAt = &now
		outcome.Released = true
		outcome.Reconciled = true
	}
	if _, err := s.store.UpdateWatchdogHold(ctx, hold.ID, watchdogHoldToInput(hold)); err != nil {
		return outcome, err
	}
	outcome.ActionRecorded = true
	return outcome, patchErr
}

func (s *Service) reconcileWatchdogDeprioritize(ctx context.Context, instance SidecarInstance, policy SidecarWatchdogPolicy, snapshot SidecarAuthSnapshot, condition watchdogCondition, now time.Time) (watchdogHoldOutcome, error) {
	outcome := watchdogHoldOutcome{Processed: true}
	hold, err := s.createActiveWatchdogHold(ctx, snapshot, policy, condition)
	if err != nil {
		return outcome, err
	}
	patchErr := s.patchAuthPriority(ctx, instance, snapshot.Name, hold.TargetPriority)
	status := watchdogActionStatusSucceeded
	var errorMessage *string
	if patchErr != nil {
		status = watchdogActionStatusFailed
		message := watchdogErrorMessage(patchErr)
		errorMessage = &message
	}
	reason := condition.Reason
	action, err := s.createHoldAction(ctx, hold, snapshot, watchdogActionDeprioritize, status, &reason, errorMessage, now)
	if err != nil {
		return outcome, err
	}
	hold.Reason = condition.Reason
	hold.ConditionHash = condition.Hash
	hold.HoldUntil = &condition.HoldUntil
	hold.LastActionID = &action.ID
	hold.ManualPauseUntil = nil
	if patchErr != nil {
		hold.Status = WatchdogHoldStatusReleased
		hold.ReleasedAt = &now
	} else {
		hold.Status = WatchdogHoldStatusActive
		hold.ReleasedAt = nil
		outcome.Reconciled = true
	}
	if _, err := s.store.UpdateWatchdogHold(ctx, hold.ID, watchdogHoldToInput(hold)); err != nil {
		return outcome, err
	}
	outcome.ActionRecorded = true
	return outcome, patchErr
}

func (s *Service) createActiveWatchdogHold(ctx context.Context, snapshot SidecarAuthSnapshot, policy SidecarWatchdogPolicy, condition watchdogCondition) (SidecarWatchdogHold, error) {
	previousPriority := cloneIntPtr(snapshot.Priority)
	hold, err := s.store.CreateWatchdogHold(ctx, SidecarWatchdogHoldInput{SidecarID: snapshot.SidecarID, AuthID: snapshot.AuthID, AuthIndex: cloneStringPtr(snapshot.AuthIndex), Provider: cloneStringPtr(snapshot.Provider), Reason: condition.Reason, ConditionHash: condition.Hash, PreviousPriority: previousPriority, TargetPriority: normalizedWatchdogTargetPriority(policy), HoldUntil: &condition.HoldUntil, Status: WatchdogHoldStatusActive})
	if err == nil {
		return hold, nil
	}
	if !IsStoreError(err, StoreErrorDuplicateActiveHold) {
		return SidecarWatchdogHold{}, err
	}
	existing, found, loadErr := s.store.GetActiveWatchdogHold(ctx, snapshot.SidecarID, snapshot.AuthID)
	if loadErr != nil {
		return SidecarWatchdogHold{}, loadErr
	}
	if !found {
		return SidecarWatchdogHold{}, err
	}
	return existing, nil
}

func (s *Service) patchAuthPriority(ctx context.Context, instance SidecarInstance, authName string, priority int) error {
	target, err := s.cliProxyTarget(instance)
	if err != nil {
		return err
	}
	payload := map[string]any{"name": authName, "priority": priority}
	var response map[string]any
	_, err = s.cliProxyClient.FetchJSON(ctx, target, http.MethodPatch, "/auth-files/fields", payload, &response)
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
	for _, raw := range authFiles {
		input, err := normalizeSidecarAuthSnapshot(instance.ID, observedAt, raw)
		if err != nil {
			return SidecarAuthSnapshot{}, false, err
		}
		if input.AuthID == authID {
			return authSnapshotFromInput(input), true, nil
		}
	}
	return SidecarAuthSnapshot{}, false, nil
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

func evaluateWatchdogCondition(snapshot SidecarAuthSnapshot, policy SidecarWatchdogPolicy, now time.Time) watchdogCondition {
	fallbackUntil := now.Add(time.Duration(normalizedFallbackCooldownSeconds(policy)) * time.Second)
	reason := ""
	holdUntil := fallbackUntil
	failureCount, observed := watchdogRecentFailureCount(snapshot, policy, now)
	if !observed {
		return watchdogCondition{}
	}
	if snapshot.QuotaExceeded != nil && *snapshot.QuotaExceeded {
		reason = watchdogReasonQuotaExceeded
		if snapshot.QuotaNextRecoverAt != nil && snapshot.QuotaNextRecoverAt.After(now) {
			holdUntil = snapshot.QuotaNextRecoverAt.UTC()
		}
	} else if snapshot.QuotaNextRecoverAt != nil && snapshot.QuotaNextRecoverAt.After(now) {
		reason = watchdogReasonQuotaRecoverPending
		holdUntil = snapshot.QuotaNextRecoverAt.UTC()
	} else if snapshot.Unavailable != nil && *snapshot.Unavailable {
		reason = watchdogReasonUnavailable
	} else if failureCount >= normalizedFailureThreshold(policy) {
		reason = watchdogReasonFailureThreshold
	}
	condition := watchdogCondition{Triggered: reason != "", Reason: reason, FailureCount: failureCount, HoldUntil: holdUntil}
	if condition.Triggered {
		condition.Hash = watchdogConditionHash(snapshot, condition)
	}
	return condition
}

func watchdogRecentFailureCount(snapshot SidecarAuthSnapshot, policy SidecarWatchdogPolicy, now time.Time) (int, bool) {
	trimmed := strings.TrimSpace(string(snapshot.RecentRequestsJSON))
	if trimmed == "" || trimmed == "null" {
		return 0, false
	}
	var rows []map[string]any
	if err := json.Unmarshal(snapshot.RecentRequestsJSON, &rows); err != nil || len(rows) == 0 {
		return 0, false
	}
	cutoff := now.Add(-time.Duration(normalizedFailureWindowSeconds(policy)) * time.Second)
	count := 0
	for _, row := range rows {
		if !watchdogRecentRequestInWindow(row, cutoff) {
			continue
		}
		count += intFromAny(row["failure_count"])
		count += intFromAny(row["failed"])
		count += intFromAny(row["failures"])
	}
	return count, true
}

func watchdogRecentRequestInWindow(row map[string]any, cutoff time.Time) bool {
	for _, key := range []string{"window_end", "window_start", "time"} {
		value, ok := row[key].(string)
		if !ok || strings.TrimSpace(value) == "" {
			continue
		}
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			return true
		}
		return !parsed.Before(cutoff)
	}
	return true
}

func watchdogConditionHash(snapshot SidecarAuthSnapshot, condition watchdogCondition) string {
	input := fmt.Sprintf("%d|%s|%s|%s|%d|%s", snapshot.SidecarID, snapshot.AuthID, condition.Reason, condition.HoldUntil.Format(time.RFC3339Nano), condition.FailureCount, strings.TrimSpace(string(snapshot.SnapshotJSON)))
	sum := sha256.Sum256([]byte(input))
	return hex.EncodeToString(sum[:])
}

func (s *Service) pauseHoldWithAction(ctx context.Context, hold SidecarWatchdogHold, snapshot SidecarAuthSnapshot, policy SidecarWatchdogPolicy, actionType string, reason string, now time.Time) (SidecarWatchdogHold, error) {
	manualPauseUntil := now.Add(time.Duration(normalizedManualPauseSeconds(policy)) * time.Second)
	action, err := s.createHoldAction(ctx, hold, snapshot, actionType, watchdogActionStatusSkipped, &reason, nil, now)
	if err != nil {
		return SidecarWatchdogHold{}, err
	}
	hold.ManualPauseUntil = &manualPauseUntil
	hold.Status = WatchdogHoldStatusPaused
	hold.LastActionID = &action.ID
	return s.store.UpdateWatchdogHold(ctx, hold.ID, watchdogHoldToInput(hold))
}

func (s *Service) createHoldAction(ctx context.Context, hold SidecarWatchdogHold, snapshot SidecarAuthSnapshot, actionType string, status string, reason *string, errorMessage *string, now time.Time) (SidecarWatchdogAction, error) {
	input := SidecarWatchdogActionInput{SidecarID: hold.SidecarID, HoldID: &hold.ID, AuthID: &hold.AuthID, AuthIndex: cloneStringPtr(hold.AuthIndex), Provider: cloneStringPtr(hold.Provider), ActionType: actionType, Reason: reason, PreviousPriority: cloneIntPtr(hold.PreviousPriority), TargetPriority: &hold.TargetPriority, HoldUntil: cloneTimePtr(hold.HoldUntil), Status: status, ErrorMessage: errorMessage}
	if snapshot.ID > 0 {
		input.AuthSnapshotID = &snapshot.ID
	}
	return s.createWatchdogAction(ctx, input, now)
}

func (s *Service) recordHoldAction(ctx context.Context, hold SidecarWatchdogHold, snapshot SidecarAuthSnapshot, actionType string, status string, reason *string, errorMessage *string, now time.Time) error {
	_, err := s.createHoldAction(ctx, hold, snapshot, actionType, status, reason, errorMessage, now)
	return err
}

func (s *Service) recordWatchdogAction(ctx context.Context, input SidecarWatchdogActionInput, now time.Time) error {
	_, err := s.createWatchdogAction(ctx, input, now)
	return err
}

func (s *Service) recordWatchdogSkipOnce(ctx context.Context, input SidecarWatchdogActionInput, now time.Time) (bool, error) {
	input.Status = watchdogActionStatusSkipped
	if s.hasMatchingSkippedAction(ctx, input) {
		return false, nil
	}
	return true, s.recordWatchdogAction(ctx, input, now)
}

func (s *Service) recordHoldSkipOnce(ctx context.Context, hold SidecarWatchdogHold, snapshot SidecarAuthSnapshot, actionType string, reason *string, now time.Time) (bool, error) {
	input := SidecarWatchdogActionInput{SidecarID: hold.SidecarID, HoldID: &hold.ID, AuthID: &hold.AuthID, ActionType: actionType, Status: watchdogActionStatusSkipped, Reason: reason}
	if s.hasMatchingSkippedAction(ctx, input) {
		return false, nil
	}
	return true, s.recordHoldAction(ctx, hold, snapshot, actionType, watchdogActionStatusSkipped, reason, nil, now)
}

func (s *Service) hasMatchingSkippedAction(ctx context.Context, input SidecarWatchdogActionInput) bool {
	lister, ok := s.store.(actionHistoryPersistence)
	if !ok {
		return false
	}
	actions, err := lister.ListWatchdogActions(ctx, input.SidecarID)
	if err != nil {
		return false
	}
	for _, action := range actions {
		if action.Status != watchdogActionStatusSkipped || action.ActionType != input.ActionType {
			continue
		}
		if !optionalStringEqual(action.Reason, input.Reason) || !optionalStringEqual(action.AuthID, input.AuthID) || !optionalIntEqual(action.HoldID, input.HoldID) {
			continue
		}
		return true
	}
	return false
}

func (s *Service) createWatchdogAction(ctx context.Context, input SidecarWatchdogActionInput, now time.Time) (SidecarWatchdogAction, error) {
	completedAt := now
	input.CompletedAt = &completedAt
	return s.store.CreateWatchdogAction(ctx, input)
}

func watchdogHoldToInput(hold SidecarWatchdogHold) SidecarWatchdogHoldInput {
	return SidecarWatchdogHoldInput{SidecarID: hold.SidecarID, AuthID: hold.AuthID, AuthIndex: cloneStringPtr(hold.AuthIndex), Provider: cloneStringPtr(hold.Provider), Reason: hold.Reason, ConditionHash: hold.ConditionHash, PreviousPriority: cloneIntPtr(hold.PreviousPriority), TargetPriority: hold.TargetPriority, HoldUntil: cloneTimePtr(hold.HoldUntil), ManualPauseUntil: cloneTimePtr(hold.ManualPauseUntil), Status: hold.Status, LastActionID: cloneIntPtr(hold.LastActionID), ReleasedAt: cloneTimePtr(hold.ReleasedAt)}
}

func authSnapshotFromInput(input SidecarAuthSnapshotInput) SidecarAuthSnapshot {
	return SidecarAuthSnapshot{SidecarID: input.SidecarID, AuthID: input.AuthID, AuthIndex: cloneStringPtr(input.AuthIndex), Name: input.Name, Provider: cloneStringPtr(input.Provider), Label: cloneStringPtr(input.Label), Status: cloneStringPtr(input.Status), StatusMessage: cloneStringPtr(input.StatusMessage), Disabled: cloneBoolPtr(input.Disabled), Unavailable: cloneBoolPtr(input.Unavailable), Priority: cloneIntPtr(input.Priority), QuotaExceeded: cloneBoolPtr(input.QuotaExceeded), QuotaReason: cloneStringPtr(input.QuotaReason), QuotaNextRecoverAt: cloneTimePtr(input.QuotaNextRecoverAt), NextRetryAfter: cloneTimePtr(input.NextRetryAfter), SuccessCount: cloneIntPtr(input.SuccessCount), FailedCount: cloneIntPtr(input.FailedCount), RecentRequestsJSON: append([]byte(nil), input.RecentRequestsJSON...), ModelStatesJSON: append([]byte(nil), input.ModelStatesJSON...), SnapshotJSON: append([]byte(nil), input.SnapshotJSON...), ObservedAt: input.ObservedAt}
}

func watchdogSnapshotFromLatestSync(instance SidecarInstance, snapshot SidecarAuthSnapshot) bool {
	if instance.LastSuccessfulSyncAt == nil {
		return false
	}
	return !snapshot.ObservedAt.Before(instance.LastSuccessfulSyncAt.UTC())
}

func watchdogAuthEnabled(snapshot SidecarAuthSnapshot) bool {
	return snapshot.Disabled == nil || !*snapshot.Disabled
}

func watchdogAuthPriority(snapshot SidecarAuthSnapshot) int {
	return intPtrValue(snapshot.Priority)
}

func intPtrValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func optionalStringEqual(left *string, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func optionalIntEqual(left *int, right *int) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func intFromAny(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	case json.Number:
		parsed, err := typed.Int64()
		if err == nil {
			return int(parsed)
		}
	}
	return 0
}

func normalizedWatchdogTargetPriority(policy SidecarWatchdogPolicy) int {
	if policy.DeprioritizedPriority < 0 {
		return DefaultDeprioritizedPriority
	}
	return policy.DeprioritizedPriority
}

func normalizedFailureThreshold(policy SidecarWatchdogPolicy) int {
	if policy.FailureThreshold <= 0 {
		return DefaultFailureThreshold
	}
	return policy.FailureThreshold
}

func normalizedFailureWindowSeconds(policy SidecarWatchdogPolicy) int {
	if policy.FailureWindowSeconds <= 0 {
		return DefaultFailureWindowSeconds
	}
	return policy.FailureWindowSeconds
}

func normalizedFallbackCooldownSeconds(policy SidecarWatchdogPolicy) int {
	if policy.FallbackCooldownSeconds <= 0 {
		return DefaultFallbackCooldownSeconds
	}
	return policy.FallbackCooldownSeconds
}

func normalizedManualPauseSeconds(policy SidecarWatchdogPolicy) int {
	if policy.ManualOverridePauseSeconds <= 0 {
		return DefaultManualOverridePauseSeconds
	}
	return policy.ManualOverridePauseSeconds
}

func watchdogErrorMessage(err error) string {
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
	return detail
}

func (s *Service) tryAcquireWatchdogRun(sidecarID int) bool {
	s.watchdogMu.Lock()
	defer s.watchdogMu.Unlock()
	if s.watchdogLocks == nil {
		s.watchdogLocks = map[int]struct{}{}
	}
	if _, exists := s.watchdogLocks[sidecarID]; exists {
		return false
	}
	s.watchdogLocks[sidecarID] = struct{}{}
	return true
}

func (s *Service) releaseWatchdogRun(sidecarID int) {
	s.watchdogMu.Lock()
	defer s.watchdogMu.Unlock()
	delete(s.watchdogLocks, sidecarID)
}

func (s *memorySidecarStore) CreateWatchdogHold(_ context.Context, input SidecarWatchdogHoldInput) (SidecarWatchdogHold, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if input.SidecarID <= 0 || strings.TrimSpace(input.AuthID) == "" || strings.TrimSpace(input.Reason) == "" || strings.TrimSpace(input.ConditionHash) == "" || strings.TrimSpace(input.Status) == "" {
		return SidecarWatchdogHold{}, invalidInputError("sidecar_id, auth_id, reason, condition_hash, and status are required")
	}
	if _, ok := s.instances[input.SidecarID]; !ok {
		return SidecarWatchdogHold{}, notFoundError("sidecar instance not found")
	}
	authID := strings.TrimSpace(input.AuthID)
	status := strings.TrimSpace(input.Status)
	if status == WatchdogHoldStatusActive || status == WatchdogHoldStatusPaused {
		for _, hold := range s.holds[input.SidecarID] {
			if hold.AuthID == authID && (hold.Status == WatchdogHoldStatusActive || hold.Status == WatchdogHoldStatusPaused) {
				return SidecarWatchdogHold{}, &StoreError{Code: StoreErrorDuplicateActiveHold, Message: "active sidecar watchdog hold already exists"}
			}
		}
	}
	now := s.now().UTC()
	hold := SidecarWatchdogHold{ID: s.nextHoldID, SidecarID: input.SidecarID, AuthID: authID, AuthIndex: cloneStringPtr(input.AuthIndex), Provider: cloneStringPtr(input.Provider), Reason: strings.TrimSpace(input.Reason), ConditionHash: strings.TrimSpace(input.ConditionHash), PreviousPriority: cloneIntPtr(input.PreviousPriority), TargetPriority: input.TargetPriority, HoldUntil: cloneTimePtr(input.HoldUntil), ManualPauseUntil: cloneTimePtr(input.ManualPauseUntil), Status: strings.TrimSpace(input.Status), LastActionID: cloneIntPtr(input.LastActionID), CreatedAt: now, UpdatedAt: now, ReleasedAt: cloneTimePtr(input.ReleasedAt)}
	s.nextHoldID++
	s.holds[input.SidecarID] = append(s.holds[input.SidecarID], hold)
	return cloneWatchdogHold(hold), nil
}

func (s *memorySidecarStore) GetActiveWatchdogHold(_ context.Context, sidecarID int, authID string) (SidecarWatchdogHold, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	trimmedAuthID := strings.TrimSpace(authID)
	for index := len(s.holds[sidecarID]) - 1; index >= 0; index-- {
		hold := s.holds[sidecarID][index]
		if hold.AuthID == trimmedAuthID && (hold.Status == WatchdogHoldStatusActive || hold.Status == WatchdogHoldStatusPaused) {
			return cloneWatchdogHold(hold), true, nil
		}
	}
	return SidecarWatchdogHold{}, false, nil
}

func (s *memorySidecarStore) ListActiveWatchdogHolds(_ context.Context, sidecarID int) ([]SidecarWatchdogHold, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]SidecarWatchdogHold, 0)
	for _, hold := range s.holds[sidecarID] {
		if hold.Status == WatchdogHoldStatusActive || hold.Status == WatchdogHoldStatusPaused {
			items = append(items, cloneWatchdogHold(hold))
		}
	}
	return items, nil
}

func (s *memorySidecarStore) UpdateWatchdogHold(_ context.Context, id int, input SidecarWatchdogHoldInput) (SidecarWatchdogHold, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id <= 0 || input.SidecarID <= 0 || strings.TrimSpace(input.AuthID) == "" || strings.TrimSpace(input.Reason) == "" || strings.TrimSpace(input.ConditionHash) == "" || strings.TrimSpace(input.Status) == "" {
		return SidecarWatchdogHold{}, invalidInputError("id, sidecar_id, auth_id, reason, condition_hash, and status are required")
	}
	for index, hold := range s.holds[input.SidecarID] {
		if hold.ID != id {
			continue
		}
		hold.AuthID = strings.TrimSpace(input.AuthID)
		hold.AuthIndex = cloneStringPtr(input.AuthIndex)
		hold.Provider = cloneStringPtr(input.Provider)
		hold.Reason = strings.TrimSpace(input.Reason)
		hold.ConditionHash = strings.TrimSpace(input.ConditionHash)
		hold.PreviousPriority = cloneIntPtr(input.PreviousPriority)
		hold.TargetPriority = input.TargetPriority
		hold.HoldUntil = cloneTimePtr(input.HoldUntil)
		hold.ManualPauseUntil = cloneTimePtr(input.ManualPauseUntil)
		hold.Status = strings.TrimSpace(input.Status)
		hold.LastActionID = cloneIntPtr(input.LastActionID)
		hold.ReleasedAt = cloneTimePtr(input.ReleasedAt)
		hold.UpdatedAt = s.now().UTC()
		s.holds[input.SidecarID][index] = hold
		return cloneWatchdogHold(hold), nil
	}
	return SidecarWatchdogHold{}, notFoundError("sidecar watchdog hold not found")
}

func cloneWatchdogHold(hold SidecarWatchdogHold) SidecarWatchdogHold {
	copy := hold
	copy.AuthIndex = cloneStringPtr(hold.AuthIndex)
	copy.Provider = cloneStringPtr(hold.Provider)
	copy.PreviousPriority = cloneIntPtr(hold.PreviousPriority)
	copy.HoldUntil = cloneTimePtr(hold.HoldUntil)
	copy.ManualPauseUntil = cloneTimePtr(hold.ManualPauseUntil)
	copy.LastActionID = cloneIntPtr(hold.LastActionID)
	copy.ReleasedAt = cloneTimePtr(hold.ReleasedAt)
	return copy
}
