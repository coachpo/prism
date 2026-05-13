package sidecars

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	WatchdogHoldStatusActive   = "active"
	WatchdogHoldStatusPaused   = "paused"
	WatchdogHoldStatusReleased = "released"

	watchdogActionStatusPending   = "pending"
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

	quotaScanTypeInitial   = "initial"
	quotaScanTypeManual    = "manual"
	quotaScanTypeScheduled = "scheduled"

	quotaScanStatusQueued    = "queued"
	quotaScanStatusRunning   = "running"
	quotaScanStatusCompleted = "completed"
	quotaScanStatusCancelled = "cancelled"
	quotaScanStatusFailed    = "failed"
)

type SidecarWatchdogSummary struct {
	Checked            int
	Reconciled         int
	Skipped            int
	Failed             int
	Probed             int
	QuotaHeld          int
	Restored           int
	ProbeFailed        int
	UnsupportedSkipped int
}

type SidecarWatchdogResult struct {
	SidecarID          int
	Reconciled         bool
	Skipped            bool
	SkipReason         string
	ActionCount        int
	Probed             int
	QuotaHeld          int
	Restored           int
	ProbeFailed        int
	UnsupportedSkipped int
}

type watchdogCondition struct {
	Triggered    bool
	Reason       string
	FailureCount int
	HoldUntil    time.Time
	Hash         string
}

type quotaScanRunPersistence interface {
	CreateQuotaScanRun(context.Context, SidecarQuotaScanRunInput) (SidecarQuotaScanRun, error)
	UpdateQuotaScanRun(context.Context, int, SidecarQuotaScanRunInput) (SidecarQuotaScanRun, error)
	GetQuotaScanRun(context.Context, int, int) (SidecarQuotaScanRun, bool, error)
	ListQuotaScanRuns(context.Context, int) ([]SidecarQuotaScanRun, error)
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
		summary.Probed += result.Probed
		summary.QuotaHeld += result.QuotaHeld
		summary.Restored += result.Restored
		summary.ProbeFailed += result.ProbeFailed
		summary.UnsupportedSkipped += result.UnsupportedSkipped
		if result.Reconciled {
			summary.Reconciled++
			continue
		}
		summary.Skipped++
	}
	return summary, nil
}

func (s *Service) StartManualQuotaScan(ctx context.Context, sidecarID int, requestedBy *string, replaceActive bool) (SidecarQuotaScanRun, error) {
	if s == nil || s.store == nil {
		return SidecarQuotaScanRun{}, fmt.Errorf("sidecar service unavailable")
	}
	scanStore, ok := s.store.(quotaScanRunPersistence)
	if !ok {
		return SidecarQuotaScanRun{}, invalidInputError("sidecar quota scan store is unavailable")
	}
	instance, policy, snapshots, activeHoldAuths, err := s.loadQuotaScanPlanningContext(ctx, sidecarID)
	if err != nil {
		return SidecarQuotaScanRun{}, err
	}
	if !instance.Enabled {
		return SidecarQuotaScanRun{}, invalidInputError("sidecar is disabled")
	}
	if !policy.Enabled || !policy.QuotaInventoryEnabled {
		return SidecarQuotaScanRun{}, invalidInputError("quota inventory is disabled")
	}
	now := s.nowUTC()
	if sidecarSyncPaused(instance, now) {
		return SidecarQuotaScanRun{}, &domainError{StatusCode: http.StatusConflict, Detail: "management authentication pause is active"}
	}
	if sidecarSnapshotsStale(instance, now) {
		return SidecarQuotaScanRun{}, &domainError{StatusCode: http.StatusConflict, Detail: "sidecar auth snapshots are stale"}
	}
	runs, err := scanStore.ListQuotaScanRuns(ctx, sidecarID)
	if err != nil {
		return SidecarQuotaScanRun{}, err
	}
	activeRun, active := activeQuotaScanRun(runs)
	if active && !replaceActive {
		return SidecarQuotaScanRun{}, invalidInputError("active quota scan run already exists for sidecar")
	}
	if active {
		if _, err := cancelQuotaScanRun(ctx, scanStore, activeRun, now); err != nil {
			return SidecarQuotaScanRun{}, err
		}
	}
	plannedCount := len(watchdogQuotaScanProbeCandidates(policy, SidecarQuotaScanRun{ScanType: quotaScanTypeManual}, snapshots, activeHoldAuths, nil))
	return scanStore.CreateQuotaScanRun(ctx, SidecarQuotaScanRunInput{SidecarID: sidecarID, ScanType: quotaScanTypeManual, Status: quotaScanStatusQueued, RequestedBy: cloneStringPtr(requestedBy), PlannedCount: plannedCount})
}

func (s *Service) CancelQuotaScanRun(ctx context.Context, sidecarID int, scanRunID int) (SidecarQuotaScanRun, error) {
	if s == nil || s.store == nil {
		return SidecarQuotaScanRun{}, fmt.Errorf("sidecar service unavailable")
	}
	scanStore, ok := s.store.(quotaScanRunPersistence)
	if !ok {
		return SidecarQuotaScanRun{}, invalidInputError("sidecar quota scan store is unavailable")
	}
	run, found, err := scanStore.GetQuotaScanRun(ctx, sidecarID, scanRunID)
	if err != nil {
		return SidecarQuotaScanRun{}, err
	}
	if !found {
		return SidecarQuotaScanRun{}, notFoundError("sidecar quota scan run not found")
	}
	if !quotaScanStatusActive(run.Status) {
		return run, nil
	}
	return cancelQuotaScanRun(ctx, scanStore, run, s.nowUTC())
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
	lease, acquired, err := s.tryAcquireWatchdogLease(ctx, sidecarID)
	if err != nil {
		return result, err
	}
	if !acquired {
		result.Skipped = true
		result.SkipReason = "watchdog_lease_held"
		return result, nil
	}
	if lease != nil {
		defer func() {
			releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = lease.Release(releaseCtx)
		}()
	}

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
	if err := validateWatchdogProbeRuntimePolicy(policy); err != nil {
		return result, err
	}
	syncPaused := sidecarSyncPaused(instance, now)

	pendingOutcomes, pendingErr := s.repairPendingWatchdogPatchActions(ctx, instance, policy, now)
	for _, outcome := range pendingOutcomes {
		result.applyHoldOutcome(outcome)
	}
	if pendingErr != nil {
		return result, pendingErr
	}
	if len(pendingOutcomes) > 0 {
		return result, nil
	}

	dueHolds, err := s.store.ListDueWatchdogHolds(ctx, sidecarID, now)
	if err != nil {
		return result, err
	}
	snapshots, err := s.store.ListAuthSnapshots(ctx, sidecarID)
	if err != nil {
		return result, err
	}
	staleSnapshots := sidecarSnapshotsStale(instance, now)
	freshSnapshots := make([]SidecarAuthSnapshot, 0, len(snapshots))
	if !staleSnapshots {
		for _, snapshot := range snapshots {
			if watchdogSnapshotFromLatestSync(instance, snapshot) {
				freshSnapshots = append(freshSnapshots, snapshot)
			}
		}
	}
	snapshotByAuth := make(map[string]SidecarAuthSnapshot, len(snapshots))
	for _, snapshot := range snapshots {
		snapshotByAuth[snapshot.AuthID] = snapshot
	}
	activeHolds, err := s.store.ListActiveWatchdogHolds(ctx, sidecarID)
	if err != nil {
		return result, err
	}

	probeRun := newWatchdogProbeRun(policy, time.Now().UTC())
	probeBatchAllowed := watchdogProbeBatchCooldownElapsed(policy, now) && !syncPaused
	dueOutcome := watchdogProbeBatchOutcome{ProcessedHoldIDs: map[int]struct{}{}, ProcessedAuthIDs: map[string]struct{}{}}
	if probeBatchAllowed || staleSnapshots {
		var dueErr error
		dueOutcome, dueErr = s.reconcileDueWatchdogProbeBatch(ctx, instance, policy, dueHolds, snapshotByAuth, &probeRun, now)
		if dueErr != nil {
			return result, dueErr
		}
		result.applyProbeOutcome(dueOutcome)
	}

	activeHoldAuths := watchdogActiveHoldAuthSet(activeHolds)
	processedHoldAuths := map[string]struct{}{}
	if !syncPaused {
		for _, hold := range activeHolds {
			if _, processed := dueOutcome.ProcessedHoldIDs[hold.ID]; processed {
				processedHoldAuths[hold.AuthID] = struct{}{}
				continue
			}
			if watchdogDueHoldProbeCandidateEligible(hold, now) && !staleSnapshots {
				processedHoldAuths[hold.AuthID] = struct{}{}
				continue
			}
			outcome, holdErr := s.reconcileWatchdogHold(ctx, instance, policy, hold, snapshotByAuth[hold.AuthID], now)
			if holdErr != nil {
				return result, holdErr
			}
			result.applyHoldOutcome(outcome)
			if outcome.Processed {
				processedHoldAuths[hold.AuthID] = struct{}{}
			}
		}
	}

	if probeBatchAllowed && !staleSnapshots {
		quotaStates, err := s.listQuotaStatesByAuth(ctx, sidecarID)
		if err != nil {
			return result, err
		}
		scanRun, hasScanRun, err := s.ensureActiveQuotaScanRun(ctx, sidecarID, policy, freshSnapshots, activeHoldAuths, quotaStates)
		if err != nil {
			return result, err
		}
		if hasScanRun {
			scanOutcome, scanErr := s.reconcileQuotaScanRunBatch(ctx, instance, policy, freshSnapshots, activeHoldAuths, quotaStates, scanRun, &probeRun, now)
			if scanErr != nil {
				return result, scanErr
			}
			result.applyProbeOutcome(scanOutcome)
			for authID := range scanOutcome.ProcessedAuthIDs {
				processedHoldAuths[authID] = struct{}{}
			}
		}

		discoveryOutcome, discoveryErr := s.reconcileDiscoveryWatchdogProbeBatch(ctx, instance, policy, freshSnapshots, activeHoldAuths, quotaStates, &probeRun, now)
		if discoveryErr != nil {
			return result, discoveryErr
		}
		result.applyProbeOutcome(discoveryOutcome)
		for authID := range discoveryOutcome.ProcessedAuthIDs {
			processedHoldAuths[authID] = struct{}{}
		}
	}

	for _, snapshot := range snapshots {
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
		result.applyHoldOutcome(outcome)
	}
	if staleSnapshots && dueOutcome.Attempted > 0 {
		return result, nil
	}
	if !result.Reconciled && result.ActionCount == 0 {
		if syncPaused {
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
		if staleSnapshots {
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
		result.Skipped = true
		result.SkipReason = "no_watchdog_action_needed"
	}
	return result, nil
}

type watchdogHoldOutcome struct {
	Processed          bool
	Released           bool
	Reconciled         bool
	ActionRecorded     bool
	QuotaHeld          bool
	Restored           bool
	ProbeFailed        bool
	UnsupportedSkipped bool
}

type watchdogProbeBatchOutcome struct {
	Attempted          int
	Reconciled         bool
	ActionCount        int
	QuotaHeld          int
	Restored           int
	ProbeFailed        int
	UnsupportedSkipped int
	ProcessedHoldIDs   map[int]struct{}
	ProcessedAuthIDs   map[string]struct{}
}

func (result *SidecarWatchdogResult) applyProbeOutcome(outcome watchdogProbeBatchOutcome) {
	if outcome.Reconciled {
		result.Reconciled = true
	}
	result.ActionCount += outcome.ActionCount
	result.Probed += outcome.Attempted
	result.QuotaHeld += outcome.QuotaHeld
	result.Restored += outcome.Restored
	result.ProbeFailed += outcome.ProbeFailed
	result.UnsupportedSkipped += outcome.UnsupportedSkipped
}

func (result *SidecarWatchdogResult) applyHoldOutcome(outcome watchdogHoldOutcome) {
	if outcome.ActionRecorded {
		result.ActionCount++
	}
	if outcome.Reconciled {
		result.Reconciled = true
	}
	if outcome.QuotaHeld {
		result.QuotaHeld++
	}
	if outcome.Restored {
		result.Restored++
	}
	if outcome.ProbeFailed {
		result.ProbeFailed++
	}
	if outcome.UnsupportedSkipped {
		result.UnsupportedSkipped++
	}
}

func (outcome *watchdogProbeBatchOutcome) applyHoldOutcome(holdOutcome watchdogHoldOutcome) {
	if holdOutcome.ActionRecorded {
		outcome.ActionCount++
	}
	if holdOutcome.Reconciled {
		outcome.Reconciled = true
	}
	if holdOutcome.QuotaHeld {
		outcome.QuotaHeld++
	}
	if holdOutcome.Restored {
		outcome.Restored++
	}
	if holdOutcome.ProbeFailed {
		outcome.ProbeFailed++
	}
	if holdOutcome.UnsupportedSkipped {
		outcome.UnsupportedSkipped++
	}
}

func (s *Service) repairPendingWatchdogPatchActions(ctx context.Context, instance SidecarInstance, policy SidecarWatchdogPolicy, now time.Time) ([]watchdogHoldOutcome, error) {
	pending, err := s.store.ClaimWatchdogPendingActions(ctx, instance.ID, 0)
	if err != nil {
		return nil, err
	}
	if len(pending) == 0 {
		return nil, nil
	}

	outcomes := make([]watchdogHoldOutcome, 0, len(pending))
	for _, pendingAction := range pending {
		action, found, loadErr := s.store.GetWatchdogActionByHistoryKey(ctx, pendingAction.SidecarID, pendingAction.ActionHistoryCreatedAt, pendingAction.ActionHistoryID)
		if loadErr != nil {
			return outcomes, loadErr
		}
		if !found {
			if _, err := s.store.DeleteWatchdogPendingAction(ctx, pendingAction.SidecarID, pendingAction.ID); err != nil {
				return outcomes, err
			}
			continue
		}
		action = mergePendingWatchdogActionPayload(action, pendingAction)
		outcome, repairErr := s.repairPendingWatchdogPatchAction(ctx, instance, policy, action, now)
		outcomes = append(outcomes, outcome)
		if repairErr != nil {
			if err := s.markPendingWatchdogActionAttemptError(ctx, pendingAction, repairErr, now); err != nil {
				return outcomes, err
			}
			return outcomes, repairErr
		}
		if _, err := s.store.DeleteWatchdogPendingAction(ctx, pendingAction.SidecarID, pendingAction.ID); err != nil {
			return outcomes, err
		}
	}
	return outcomes, nil
}

func mergePendingWatchdogActionPayload(action SidecarWatchdogAction, pending SidecarWatchdogPendingAction) SidecarWatchdogAction {
	action.HoldID = cloneIntPtr(pending.HoldID)
	action.AuthID = cloneStringPtr(pending.AuthID)
	action.AuthName = cloneStringPtr(pending.AuthName)
	action.AuthIndex = cloneStringPtr(pending.AuthIndex)
	action.Provider = cloneStringPtr(pending.Provider)
	action.ActionType = pending.ActionType
	action.Reason = cloneStringPtr(pending.Reason)
	action.PreviousPriority = cloneIntPtr(pending.PreviousPriority)
	action.TargetPriority = cloneIntPtr(pending.TargetPriority)
	action.HoldUntil = cloneTimePtr(pending.HoldUntil)
	return action
}

func (s *Service) markPendingWatchdogActionAttemptError(ctx context.Context, pending SidecarWatchdogPendingAction, err error, now time.Time) error {
	message := watchdogErrorMessage(err)
	input := watchdogPendingActionToInput(pending)
	input.LastAttemptAt = &now
	input.LastErrorMessage = &message
	_, updateErr := s.store.UpdateWatchdogPendingAction(ctx, pending.ID, input)
	return updateErr
}

func (s *Service) repairPendingWatchdogPatchAction(ctx context.Context, instance SidecarInstance, policy SidecarWatchdogPolicy, action SidecarWatchdogAction, now time.Time) (watchdogHoldOutcome, error) {
	switch action.ActionType {
	case watchdogActionDeprioritize:
		return s.repairPendingWatchdogDeprioritizeAction(ctx, instance, action, now)
	case watchdogActionRestore:
		return s.repairPendingWatchdogRestoreAction(ctx, instance, policy, action, now)
	default:
		return watchdogHoldOutcome{}, nil
	}
}

func (s *Service) repairPendingWatchdogDeprioritizeAction(ctx context.Context, instance SidecarInstance, action SidecarWatchdogAction, now time.Time) (watchdogHoldOutcome, error) {
	outcome := watchdogHoldOutcome{Processed: true}
	if strings.TrimSpace(stringValue(action.AuthID)) == "" {
		message := "pending deprioritize action is missing auth_id"
		_, err := s.finalizePendingWatchdogAction(ctx, action, watchdogActionStatusFailed, nil, &message, now)
		outcome.ActionRecorded = true
		return outcome, err
	}
	if action.TargetPriority == nil {
		message := "pending deprioritize action is missing target priority"
		_, err := s.finalizePendingWatchdogAction(ctx, action, watchdogActionStatusFailed, nil, &message, now)
		outcome.ActionRecorded = true
		return outcome, err
	}
	if strings.TrimSpace(stringValue(action.Reason)) == "" {
		message := "pending deprioritize action is missing reason"
		_, err := s.finalizePendingWatchdogAction(ctx, action, watchdogActionStatusFailed, nil, &message, now)
		outcome.ActionRecorded = true
		return outcome, err
	}
	selectedName := strings.TrimSpace(stringValue(action.AuthName))
	if selectedName == "" {
		message := "pending deprioritize action is missing auth_name"
		_, err := s.finalizePendingWatchdogAction(ctx, action, watchdogActionStatusFailed, nil, &message, now)
		outcome.ActionRecorded = true
		return outcome, err
	}

	expected := watchdogLiveAuthExpectation{AuthID: strings.TrimSpace(stringValue(action.AuthID)), AuthIndex: strings.TrimSpace(stringValue(action.AuthIndex)), Provider: strings.TrimSpace(stringValue(action.Provider)), Name: selectedName}
	live, found, mismatchReason, err := s.fetchLiveAuthForPriorityPatch(ctx, instance, expected, now)
	if err != nil {
		return outcome, err
	}
	if !found {
		reason := "auth no longer exists in fresh preflight read"
		_, err := s.finalizePendingWatchdogAction(ctx, action, watchdogActionStatusSkipped, &reason, nil, now)
		outcome.ActionRecorded = true
		return outcome, err
	}
	if mismatchReason != nil {
		_, err := s.finalizePendingWatchdogAction(ctx, action, watchdogActionStatusSkipped, mismatchReason, nil, now)
		outcome.ActionRecorded = true
		return outcome, err
	}

	livePriority := watchdogAuthPriority(live)
	if livePriority <= *action.TargetPriority {
		return s.completePendingWatchdogDeprioritizeAction(ctx, action, now)
	}
	if action.PreviousPriority != nil && livePriority != *action.PreviousPriority {
		reason := "current priority no longer matches selected priority"
		_, err := s.finalizePendingWatchdogAction(ctx, action, watchdogActionStatusSkipped, &reason, nil, now)
		outcome.ActionRecorded = true
		return outcome, err
	}

	patchErr := s.patchAuthPriority(ctx, instance, live.Name, *action.TargetPriority)
	if patchErr != nil {
		message := watchdogErrorMessage(patchErr)
		_, err := s.finalizePendingWatchdogAction(ctx, action, watchdogActionStatusFailed, nil, &message, now)
		outcome.ActionRecorded = true
		if err != nil {
			return outcome, err
		}
		return outcome, patchErr
	}
	return s.completePendingWatchdogDeprioritizeAction(ctx, action, now)
}

func (s *Service) completePendingWatchdogDeprioritizeAction(ctx context.Context, action SidecarWatchdogAction, now time.Time) (watchdogHoldOutcome, error) {
	outcome := watchdogHoldOutcome{Processed: true}
	hold, err := s.createActiveWatchdogHoldFromPendingAction(ctx, action)
	if err != nil {
		return outcome, err
	}
	action.HoldID = &hold.ID
	finalized, err := s.finalizePendingWatchdogAction(ctx, action, watchdogActionStatusSucceeded, nil, nil, now)
	if err != nil {
		return outcome, err
	}
	hold.LastActionID = &finalized.ID
	if _, err := s.store.UpdateWatchdogHold(ctx, hold.ID, watchdogHoldToInput(hold)); err != nil {
		return outcome, err
	}
	outcome.ActionRecorded = true
	outcome.Reconciled = true
	outcome.QuotaHeld = true
	return outcome, nil
}

func (s *Service) createActiveWatchdogHoldFromPendingAction(ctx context.Context, action SidecarWatchdogAction) (SidecarWatchdogHold, error) {
	if action.TargetPriority == nil {
		return SidecarWatchdogHold{}, invalidInputError("pending deprioritize action is missing target priority")
	}
	authID := strings.TrimSpace(stringValue(action.AuthID))
	reason := strings.TrimSpace(stringValue(action.Reason))
	if authID == "" || reason == "" {
		return SidecarWatchdogHold{}, invalidInputError("pending deprioritize action is missing auth_id or reason")
	}
	holdUntil := cloneTimePtr(action.HoldUntil)
	if holdUntil != nil {
		utc := holdUntil.UTC()
		holdUntil = &utc
	}
	hold, err := s.store.CreateWatchdogHold(ctx, SidecarWatchdogHoldInput{SidecarID: action.SidecarID, AuthID: authID, AuthIndex: cloneStringPtr(action.AuthIndex), Provider: cloneStringPtr(action.Provider), Reason: reason, ConditionHash: pendingWatchdogActionConditionHash(action), PreviousPriority: cloneIntPtr(action.PreviousPriority), TargetPriority: *action.TargetPriority, HoldUntil: holdUntil, Status: WatchdogHoldStatusActive})
	if err == nil {
		return hold, nil
	}
	if !IsStoreError(err, StoreErrorDuplicateActiveHold) {
		return SidecarWatchdogHold{}, err
	}
	existing, found, loadErr := s.store.GetActiveWatchdogHold(ctx, action.SidecarID, authID)
	if loadErr != nil {
		return SidecarWatchdogHold{}, loadErr
	}
	if !found {
		return SidecarWatchdogHold{}, err
	}
	return existing, nil
}

func pendingWatchdogActionConditionHash(action SidecarWatchdogAction) string {
	holdUntil := ""
	if action.HoldUntil != nil {
		holdUntil = action.HoldUntil.UTC().Format(time.RFC3339Nano)
	}
	input := fmt.Sprintf("pending-action|%d|%d|%s|%s|%s", action.SidecarID, action.ID, strings.TrimSpace(stringValue(action.AuthID)), strings.TrimSpace(stringValue(action.Reason)), holdUntil)
	sum := sha256.Sum256([]byte(input))
	return hex.EncodeToString(sum[:])
}

func (s *Service) repairPendingWatchdogRestoreAction(ctx context.Context, instance SidecarInstance, policy SidecarWatchdogPolicy, action SidecarWatchdogAction, now time.Time) (watchdogHoldOutcome, error) {
	outcome := watchdogHoldOutcome{Processed: true}
	hold, found, err := s.findActiveWatchdogHoldForPendingAction(ctx, action)
	if err != nil {
		return outcome, err
	}
	if !found {
		reason := "watchdog hold is no longer active"
		_, err := s.finalizePendingWatchdogAction(ctx, action, watchdogActionStatusSkipped, &reason, nil, now)
		outcome.ActionRecorded = true
		return outcome, err
	}
	if hold.Status != WatchdogHoldStatusActive {
		reason := "watchdog hold is not active"
		_, err := s.finalizePendingWatchdogAction(ctx, action, watchdogActionStatusSkipped, &reason, nil, now)
		outcome.ActionRecorded = true
		return outcome, err
	}
	restorePriority := cloneIntPtr(action.TargetPriority)
	if restorePriority == nil {
		restorePriority = cloneIntPtr(hold.PreviousPriority)
	}
	if restorePriority == nil {
		reason := "previous priority is missing; operator must choose restore priority"
		return s.pauseHoldWithPendingRestoreAction(ctx, hold, action, policy, reason, now)
	}
	if hold.PreviousPriority == nil {
		hold.PreviousPriority = cloneIntPtr(restorePriority)
	}

	selectedName := strings.TrimSpace(stringValue(action.AuthName))
	if selectedName == "" {
		reason := "pending restore action is missing auth_name"
		_, err := s.finalizePendingWatchdogAction(ctx, action, watchdogActionStatusFailed, nil, &reason, now)
		outcome.ActionRecorded = true
		return outcome, err
	}

	expected := watchdogLiveAuthExpectation{AuthID: hold.AuthID, AuthIndex: strings.TrimSpace(stringValue(hold.AuthIndex)), Provider: strings.TrimSpace(stringValue(hold.Provider)), Name: selectedName}
	if strings.TrimSpace(stringValue(action.AuthIndex)) != "" {
		expected.AuthIndex = strings.TrimSpace(stringValue(action.AuthIndex))
	}
	if strings.TrimSpace(stringValue(action.Provider)) != "" {
		expected.Provider = strings.TrimSpace(stringValue(action.Provider))
	}
	live, liveFound, mismatchReason, err := s.fetchLiveAuthForPriorityPatch(ctx, instance, expected, now)
	if err != nil {
		return outcome, err
	}
	if !liveFound {
		reason := "auth no longer exists in fresh preflight read"
		_, err := s.finalizePendingWatchdogAction(ctx, action, watchdogActionStatusSkipped, &reason, nil, now)
		outcome.ActionRecorded = true
		return outcome, err
	}
	if mismatchReason != nil {
		return s.pauseHoldWithPendingRestoreAction(ctx, hold, action, policy, *mismatchReason, now)
	}

	currentPriority := watchdogAuthPriority(live)
	if currentPriority == *restorePriority {
		return s.completePendingWatchdogRestoreAction(ctx, hold, action, nil, now)
	}
	if currentPriority != hold.TargetPriority {
		reason := "current priority no longer matches watchdog target priority"
		return s.pauseHoldWithPendingRestoreAction(ctx, hold, action, policy, reason, now)
	}
	if !watchdogAuthEnabled(live) {
		reason := "fresh preflight auth is not healthy"
		_, err := s.finalizePendingWatchdogAction(ctx, action, watchdogActionStatusSkipped, &reason, nil, now)
		outcome.ActionRecorded = true
		return outcome, err
	}

	patchErr := s.patchAuthPriority(ctx, instance, live.Name, *restorePriority)
	return s.completePendingWatchdogRestoreAction(ctx, hold, action, patchErr, now)
}

func (s *Service) findActiveWatchdogHoldForPendingAction(ctx context.Context, action SidecarWatchdogAction) (SidecarWatchdogHold, bool, error) {
	holds, err := s.store.ListActiveWatchdogHolds(ctx, action.SidecarID)
	if err != nil {
		return SidecarWatchdogHold{}, false, err
	}
	if action.HoldID != nil {
		for _, hold := range holds {
			if hold.ID == *action.HoldID {
				return hold, true, nil
			}
		}
	}
	authID := strings.TrimSpace(stringValue(action.AuthID))
	if authID == "" {
		return SidecarWatchdogHold{}, false, nil
	}
	for _, hold := range holds {
		if hold.AuthID == authID {
			return hold, true, nil
		}
	}
	return SidecarWatchdogHold{}, false, nil
}

func (s *Service) pauseHoldWithPendingRestoreAction(ctx context.Context, hold SidecarWatchdogHold, action SidecarWatchdogAction, policy SidecarWatchdogPolicy, reason string, now time.Time) (watchdogHoldOutcome, error) {
	outcome := watchdogHoldOutcome{Processed: true}
	finalized, err := s.finalizePendingWatchdogAction(ctx, action, watchdogActionStatusSkipped, &reason, nil, now)
	if err != nil {
		return outcome, err
	}
	manualPauseUntil := now.Add(time.Duration(normalizedManualPauseSeconds(policy)) * time.Second)
	hold.ManualPauseUntil = &manualPauseUntil
	hold.Status = WatchdogHoldStatusPaused
	hold.LastActionID = &finalized.ID
	if _, err := s.store.UpdateWatchdogHold(ctx, hold.ID, watchdogHoldToInput(hold)); err != nil {
		return outcome, err
	}
	outcome.ActionRecorded = true
	return outcome, nil
}

func (s *Service) completePendingWatchdogRestoreAction(ctx context.Context, hold SidecarWatchdogHold, action SidecarWatchdogAction, patchErr error, now time.Time) (watchdogHoldOutcome, error) {
	outcome := watchdogHoldOutcome{Processed: true}
	status := watchdogActionStatusSucceeded
	var errorMessage *string
	if patchErr != nil {
		status = watchdogActionStatusFailed
		message := watchdogErrorMessage(patchErr)
		errorMessage = &message
	}
	finalized, err := s.finalizePendingWatchdogAction(ctx, action, status, nil, errorMessage, now)
	if err != nil {
		return outcome, err
	}
	hold.LastActionID = &finalized.ID
	if patchErr == nil {
		hold.Status = WatchdogHoldStatusReleased
		hold.ReleasedAt = &now
		outcome.Released = true
		outcome.Restored = true
		outcome.Reconciled = true
	}
	if _, err := s.store.UpdateWatchdogHold(ctx, hold.ID, watchdogHoldToInput(hold)); err != nil {
		return outcome, err
	}
	outcome.ActionRecorded = true
	if patchErr != nil {
		return outcome, patchErr
	}
	return outcome, nil
}

func (s *Service) finalizePendingWatchdogAction(ctx context.Context, action SidecarWatchdogAction, status string, reason *string, errorMessage *string, now time.Time) (SidecarWatchdogAction, error) {
	if reason != nil {
		action.Reason = cloneStringPtr(reason)
	}
	return s.finalizeWatchdogAction(ctx, action, status, errorMessage, now)
}

type watchdogProbeRun struct {
	remaining int
	policy    SidecarWatchdogPolicy
	startedAt time.Time
}

func newWatchdogProbeRun(policy SidecarWatchdogPolicy, startedAt time.Time) watchdogProbeRun {
	return watchdogProbeRun{remaining: normalizedProbeConcurrency(policy), policy: policy, startedAt: startedAt}
}

func (run *watchdogProbeRun) nextTimeout(now time.Time) (time.Duration, bool) {
	if run == nil || run.remaining <= 0 {
		return 0, false
	}
	return watchdogEffectiveProbeTimeout(run.policy, run.startedAt, now)
}

func (run *watchdogProbeRun) consume() {
	if run != nil && run.remaining > 0 {
		run.remaining--
	}
}

type watchdogProbeWaveResult struct {
	Candidate      watchdogProbeCandidate
	Classification sidecarWatchdogProbeClassification
	Observation    SidecarWatchdogProbeObservationInput
}

func (s *Service) executeWatchdogProbeWave(ctx context.Context, instance SidecarInstance, policy SidecarWatchdogPolicy, candidates []watchdogProbeCandidate, run *watchdogProbeRun, now time.Time) ([]watchdogProbeWaveResult, error) {
	if len(candidates) == 0 || run == nil || run.remaining <= 0 {
		return nil, nil
	}
	waveSize := len(candidates)
	if run.remaining < waveSize {
		waveSize = run.remaining
	}

	results := make([]watchdogProbeWaveResult, waveSize)
	errs := make([]error, waveSize)
	launched := 0
	var wg sync.WaitGroup
	for i := 0; i < waveSize; i++ {
		timeout, ok := run.nextTimeout(time.Now().UTC())
		if !ok {
			break
		}
		candidate := candidates[i]
		resultIndex := i
		run.consume()
		launched++
		wg.Add(1)
		go func() {
			defer wg.Done()
			classification, observation, err := s.runWatchdogProbe(ctx, instance, policy, candidate, timeout, now)
			if err != nil {
				errs[resultIndex] = err
				return
			}
			results[resultIndex] = watchdogProbeWaveResult{Candidate: candidate, Classification: classification, Observation: observation}
		}()
	}
	wg.Wait()

	for _, err := range errs[:launched] {
		if err != nil {
			return nil, err
		}
	}
	return results[:launched], nil
}

func (s *Service) loadQuotaScanPlanningContext(ctx context.Context, sidecarID int) (SidecarInstance, SidecarWatchdogPolicy, []SidecarAuthSnapshot, map[string]struct{}, error) {
	instance, found, err := s.store.GetSidecarInstance(ctx, sidecarID)
	if err != nil {
		return SidecarInstance{}, SidecarWatchdogPolicy{}, nil, nil, err
	}
	if !found {
		return SidecarInstance{}, SidecarWatchdogPolicy{}, nil, nil, notFoundError("sidecar instance not found")
	}
	policy, err := s.store.GetOrCreateWatchdogPolicy(ctx, sidecarID)
	if err != nil {
		return SidecarInstance{}, SidecarWatchdogPolicy{}, nil, nil, err
	}
	snapshots, err := s.store.ListAuthSnapshots(ctx, sidecarID)
	if err != nil {
		return SidecarInstance{}, SidecarWatchdogPolicy{}, nil, nil, err
	}
	activeHolds, err := s.store.ListActiveWatchdogHolds(ctx, sidecarID)
	if err != nil {
		return SidecarInstance{}, SidecarWatchdogPolicy{}, nil, nil, err
	}
	freshSnapshots := make([]SidecarAuthSnapshot, 0, len(snapshots))
	if !sidecarSnapshotsStale(instance, s.nowUTC()) {
		for _, snapshot := range snapshots {
			if watchdogSnapshotFromLatestSync(instance, snapshot) {
				freshSnapshots = append(freshSnapshots, snapshot)
			}
		}
	}
	return instance, policy, freshSnapshots, watchdogActiveHoldAuthSet(activeHolds), nil
}

func (s *Service) listQuotaStatesByAuth(ctx context.Context, sidecarID int) (map[string]SidecarAuthQuotaState, error) {
	stateStore, ok := s.store.(authQuotaStateStore)
	if !ok {
		return map[string]SidecarAuthQuotaState{}, nil
	}
	states, err := stateStore.ListAuthQuotaStates(ctx, sidecarID)
	if err != nil {
		return nil, err
	}
	return quotaStateByAuth(states), nil
}

func (s *Service) ensureActiveQuotaScanRun(ctx context.Context, sidecarID int, policy SidecarWatchdogPolicy, snapshots []SidecarAuthSnapshot, activeHoldAuths map[string]struct{}, quotaStates map[string]SidecarAuthQuotaState) (SidecarQuotaScanRun, bool, error) {
	if !policy.QuotaInventoryEnabled {
		return SidecarQuotaScanRun{}, false, nil
	}
	scanStore, ok := s.store.(quotaScanRunPersistence)
	if !ok {
		return SidecarQuotaScanRun{}, false, nil
	}
	runs, err := scanStore.ListQuotaScanRuns(ctx, sidecarID)
	if err != nil {
		return SidecarQuotaScanRun{}, false, err
	}
	if activeRun, active := activeQuotaScanRun(runs); active {
		return activeRun, true, nil
	}
	if !policy.InitialScanEnabled || len(quotaStates) == 0 {
		return SidecarQuotaScanRun{}, false, nil
	}
	plannedCount := len(watchdogQuotaScanProbeCandidates(policy, SidecarQuotaScanRun{ScanType: quotaScanTypeInitial}, snapshots, activeHoldAuths, quotaStates))
	if plannedCount == 0 {
		return SidecarQuotaScanRun{}, false, nil
	}
	run, err := scanStore.CreateQuotaScanRun(ctx, SidecarQuotaScanRunInput{SidecarID: sidecarID, ScanType: quotaScanTypeInitial, Status: quotaScanStatusQueued, PlannedCount: plannedCount})
	if err != nil {
		return SidecarQuotaScanRun{}, false, err
	}
	return run, true, nil
}

func (s *Service) reconcileQuotaScanRunBatch(ctx context.Context, instance SidecarInstance, policy SidecarWatchdogPolicy, snapshots []SidecarAuthSnapshot, activeHoldAuths map[string]struct{}, quotaStates map[string]SidecarAuthQuotaState, scanRun SidecarQuotaScanRun, run *watchdogProbeRun, now time.Time) (watchdogProbeBatchOutcome, error) {
	outcome := watchdogProbeBatchOutcome{ProcessedHoldIDs: map[int]struct{}{}, ProcessedAuthIDs: map[string]struct{}{}}
	scanStore, ok := s.store.(quotaScanRunPersistence)
	if !ok || !quotaScanStatusActive(scanRun.Status) {
		return outcome, nil
	}
	if scanRun.CancelRequestedAt != nil || scanRun.Status == quotaScanStatusCancelled {
		_, err := cancelQuotaScanRun(ctx, scanStore, scanRun, now)
		return outcome, err
	}
	if scanRun.Status == quotaScanStatusQueued {
		startedAt := now
		updated := scanRun
		updated.Status = quotaScanStatusRunning
		updated.StartedAt = &startedAt
		var err error
		scanRun, err = scanStore.UpdateQuotaScanRun(ctx, scanRun.ID, quotaScanRunToInput(updated))
		if err != nil {
			return outcome, err
		}
	}
	candidates := watchdogQuotaScanProbeCandidates(policy, scanRun, snapshots, activeHoldAuths, quotaStates)
	if quotaScanRunShouldComplete(scanRun, candidates) {
		_, err := completeQuotaScanRun(ctx, scanStore, scanRun, now)
		return outcome, err
	}
	remainingPlanned := scanRun.PlannedCount - scanRun.AttemptedCount
	if remainingPlanned < 0 {
		remainingPlanned = 0
	}
	waveLimit := len(candidates)
	if waveLimit > remainingPlanned {
		waveLimit = remainingPlanned
	}
	if waveLimit > run.remaining {
		waveLimit = run.remaining
	}
	if waveLimit <= 0 {
		return outcome, nil
	}
	waveCandidates := candidates[:waveLimit]
	results, err := s.executeWatchdogProbeWave(ctx, instance, policy, waveCandidates, run, now)
	if err != nil {
		return outcome, err
	}
	for index, result := range results {
		candidate := waveCandidates[index]
		classification := result.Classification
		cursorAuthID := candidate.AuthID
		decisionResult, err := s.store.PersistWatchdogProbeDecision(ctx, SidecarWatchdogProbeDecision{SidecarID: instance.ID, Observations: []SidecarWatchdogProbeObservationInput{result.Observation}, AdvanceCursor: true, CursorAuthID: &cursorAuthID, ScanRunID: &scanRun.ID})
		if err != nil {
			return outcome, err
		}
		outcome.Attempted++
		if decisionResult.ScanRun != nil {
			scanRun = *decisionResult.ScanRun
		}
		if watchdogProbeClassificationFailed(classification) {
			outcome.ProbeFailed++
		}
		if classification.Status == watchdogProbeStatusSkippedUnsupportedProvider {
			outcome.UnsupportedSkipped++
		}
		outcome.ProcessedAuthIDs[candidate.AuthID] = struct{}{}
		actionOutcome, err := s.applyDiscoveryWatchdogProbeResult(ctx, instance, policy, candidate, classification, now)
		if err != nil {
			return outcome, err
		}
		outcome.applyHoldOutcome(actionOutcome)
	}
	if quotaScanRunShouldComplete(scanRun, watchdogQuotaScanProbeCandidates(policy, scanRun, snapshots, activeHoldAuths, quotaStates)) {
		_, err := completeQuotaScanRun(ctx, scanStore, scanRun, now)
		if err != nil {
			return outcome, err
		}
	}
	return outcome, nil
}

func (s *Service) reconcileDueWatchdogProbeBatch(ctx context.Context, instance SidecarInstance, policy SidecarWatchdogPolicy, holds []SidecarWatchdogHold, snapshotByAuth map[string]SidecarAuthSnapshot, run *watchdogProbeRun, now time.Time) (watchdogProbeBatchOutcome, error) {
	outcome := watchdogProbeBatchOutcome{ProcessedHoldIDs: map[int]struct{}{}, ProcessedAuthIDs: map[string]struct{}{}}
	candidates := make([]watchdogProbeCandidate, 0, len(holds))
	candidateHolds := make([]SidecarWatchdogHold, 0, len(holds))
	for _, hold := range holds {
		candidate, ok := watchdogDueHoldProbeCandidate(hold, now)
		if !ok {
			continue
		}
		candidates = append(candidates, candidate)
		candidateHolds = append(candidateHolds, hold)
	}

	results, err := s.executeWatchdogProbeWave(ctx, instance, policy, candidates, run, now)
	if err != nil {
		return outcome, err
	}
	for index, result := range results {
		hold := candidateHolds[index]
		classification := result.Classification
		outcome.Attempted++
		if watchdogProbeClassificationFailed(classification) {
			outcome.ProbeFailed++
		}
		if classification.Status == watchdogProbeStatusSkippedUnsupportedProvider {
			outcome.UnsupportedSkipped++
		}
		outcome.ProcessedHoldIDs[hold.ID] = struct{}{}
		outcome.ProcessedAuthIDs[hold.AuthID] = struct{}{}

		decision := SidecarWatchdogProbeDecision{SidecarID: instance.ID, Observations: []SidecarWatchdogProbeObservationInput{result.Observation}}
		if update := watchdogHoldUpdateForProbeResult(hold, policy, classification, now); update != nil {
			decision.UpdateHold = &SidecarWatchdogProbeHoldUpdate{ID: hold.ID, Input: *update}
		}
		decisionResult, err := s.store.PersistWatchdogProbeDecision(ctx, decision)
		if err != nil {
			return outcome, err
		}
		actionHold := hold
		if decisionResult.UpdatedHold != nil {
			actionHold = *decisionResult.UpdatedHold
		}
		actionOutcome, err := s.applyDueWatchdogProbeResult(ctx, instance, policy, actionHold, snapshotByAuth[hold.AuthID], classification, now)
		if err != nil {
			return outcome, err
		}
		outcome.applyHoldOutcome(actionOutcome)
	}
	return outcome, nil
}

func (s *Service) reconcileDiscoveryWatchdogProbeBatch(ctx context.Context, instance SidecarInstance, policy SidecarWatchdogPolicy, snapshots []SidecarAuthSnapshot, activeHoldAuths map[string]struct{}, quotaStates map[string]SidecarAuthQuotaState, run *watchdogProbeRun, now time.Time) (watchdogProbeBatchOutcome, error) {
	unsupportedSkipped, unsupportedActions, err := s.recordUnsupportedDiscoveryWatchdogProbeSkips(ctx, instance.ID, policy, snapshots, activeHoldAuths, now)
	if err != nil {
		return watchdogProbeBatchOutcome{}, err
	}
	outcome := watchdogProbeBatchOutcome{UnsupportedSkipped: unsupportedSkipped, ActionCount: unsupportedActions, ProcessedHoldIDs: map[int]struct{}{}, ProcessedAuthIDs: map[string]struct{}{}}
	candidates := watchdogRollingRefreshProbeCandidates(policy, snapshots, activeHoldAuths, quotaStates, now)
	results, err := s.executeWatchdogProbeWave(ctx, instance, policy, candidates, run, now)
	if err != nil {
		return outcome, err
	}
	for index, result := range results {
		candidate := candidates[index]
		classification := result.Classification
		cursorAuthID := candidate.AuthID
		if _, err := s.store.PersistWatchdogProbeDecision(ctx, SidecarWatchdogProbeDecision{SidecarID: instance.ID, Observations: []SidecarWatchdogProbeObservationInput{result.Observation}, AdvanceCursor: true, CursorAuthID: &cursorAuthID}); err != nil {
			return outcome, err
		}
		outcome.Attempted++
		if watchdogProbeClassificationFailed(classification) {
			outcome.ProbeFailed++
		}
		if classification.Status == watchdogProbeStatusSkippedUnsupportedProvider {
			outcome.UnsupportedSkipped++
		}
		outcome.ProcessedAuthIDs[candidate.AuthID] = struct{}{}
		actionOutcome, err := s.applyDiscoveryWatchdogProbeResult(ctx, instance, policy, candidate, classification, now)
		if err != nil {
			return outcome, err
		}
		outcome.applyHoldOutcome(actionOutcome)
	}
	return outcome, nil
}

func (s *Service) runWatchdogProbe(ctx context.Context, instance SidecarInstance, policy SidecarWatchdogPolicy, candidate watchdogProbeCandidate, timeout time.Duration, now time.Time) (sidecarWatchdogProbeClassification, SidecarWatchdogProbeObservationInput, error) {
	spec, ok := buildSidecarWatchdogProbeSpec(candidate.Provider, candidate.AuthIndex)
	if !ok {
		return sidecarWatchdogProbeClassification{}, SidecarWatchdogProbeObservationInput{}, invalidInputError("watchdog probe candidate is not probeable")
	}
	target, err := s.cliProxyTarget(instance)
	if err != nil {
		return sidecarWatchdogProbeClassification{}, SidecarWatchdogProbeObservationInput{}, err
	}
	target.RequestTimeoutSeconds = watchdogProbeRequestTimeoutSeconds(timeout)
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	response, probeErr := s.cliProxyClient.CallSidecarAPI(probeCtx, target, spec.Request)
	classification := classifySidecarWatchdogProbe(candidate.Provider, response, probeErr, now, normalizedFallbackCooldownSeconds(policy))
	observation, err := watchdogProbeObservationInput(instance.ID, candidate, classification, now)
	if err != nil {
		return sidecarWatchdogProbeClassification{}, SidecarWatchdogProbeObservationInput{}, err
	}
	return classification, observation, nil
}

type watchdogLiveAuthExpectation struct {
	AuthID    string
	AuthIndex string
	Provider  string
	Name      string
}

func (s *Service) applyDiscoveryWatchdogProbeResult(ctx context.Context, instance SidecarInstance, policy SidecarWatchdogPolicy, candidate watchdogProbeCandidate, classification sidecarWatchdogProbeClassification, now time.Time) (watchdogHoldOutcome, error) {
	outcome := watchdogHoldOutcome{Processed: true}
	condition := evaluateQuotaProbeObservation(instance.ID, candidate.AuthID, policy, classification, now)
	if !condition.Triggered {
		return outcome, nil
	}
	snapshot := watchdogCandidateSnapshot(candidate)
	expected := watchdogLiveAuthExpectation{AuthID: candidate.AuthID, AuthIndex: candidate.AuthIndex, Provider: candidate.Provider, Name: snapshot.Name}
	live, found, mismatchReason, err := s.fetchLiveAuthForPriorityPatch(ctx, instance, expected, now)
	if err != nil {
		return outcome, err
	}
	reason := condition.Reason
	targetPriority := normalizedWatchdogTargetPriority(policy)
	previousPriority := watchdogCandidatePreviousPriority(candidate, watchdogAuthPriority(live), targetPriority)
	if !found {
		missingReason := "auth no longer exists in fresh preflight read"
		if err := s.recordWatchdogPatchActionWithoutHold(ctx, instance.ID, snapshot, live, watchdogActionDeprioritize, watchdogActionStatusSkipped, &missingReason, previousPriority, &targetPriority, &condition.HoldUntil, nil, now); err != nil {
			return outcome, err
		}
		outcome.ActionRecorded = true
		return outcome, nil
	}
	if mismatchReason != nil {
		if err := s.recordWatchdogPatchActionWithoutHold(ctx, instance.ID, snapshot, live, watchdogActionDeprioritize, watchdogActionStatusSkipped, mismatchReason, previousPriority, &targetPriority, &condition.HoldUntil, nil, now); err != nil {
			return outcome, err
		}
		outcome.ActionRecorded = true
		return outcome, nil
	}
	livePriority := watchdogAuthPriority(live)
	holdInput := watchdogQuotaHoldInputFromProbe(instance.ID, candidate, condition, previousPriority, targetPriority)
	if livePriority <= targetPriority {
		hold, err := s.persistActiveWatchdogProbeHold(ctx, holdInput)
		if err != nil {
			return outcome, err
		}
		if _, err := s.recordHoldActionAndUpdate(ctx, hold, live, watchdogActionDeprioritize, watchdogActionStatusSucceeded, &reason, nil, now); err != nil {
			return outcome, err
		}
		outcome.ActionRecorded = true
		outcome.Reconciled = true
		outcome.QuotaHeld = true
		return outcome, nil
	}
	selectedPriority := watchdogCandidateSelectedPriority(candidate)
	if selectedPriority == nil || livePriority != *selectedPriority {
		manualReason := "current priority no longer matches selected priority"
		if err := s.recordWatchdogPatchActionWithoutHold(ctx, instance.ID, snapshot, live, watchdogActionDeprioritize, watchdogActionStatusSkipped, &manualReason, previousPriority, &targetPriority, &condition.HoldUntil, nil, now); err != nil {
			return outcome, err
		}
		outcome.ActionRecorded = true
		return outcome, nil
	}
	pendingAction, err := s.createPendingPatchActionWithoutHold(ctx, instance.ID, snapshot, live, watchdogActionDeprioritize, &reason, previousPriority, &targetPriority, &condition.HoldUntil)
	if err != nil {
		return outcome, err
	}
	patchErr := s.patchAuthPriority(ctx, instance, live.Name, targetPriority)
	if patchErr != nil {
		message := watchdogErrorMessage(patchErr)
		if _, err := s.finalizeWatchdogAction(ctx, pendingAction, watchdogActionStatusFailed, &message, now); err != nil {
			return outcome, err
		}
		outcome.ActionRecorded = true
		return outcome, patchErr
	}
	hold, err := s.persistActiveWatchdogProbeHold(ctx, holdInput)
	if err != nil {
		return outcome, err
	}
	pendingAction.HoldID = &hold.ID
	action, err := s.finalizeWatchdogAction(ctx, pendingAction, watchdogActionStatusSucceeded, nil, now)
	if err != nil {
		return outcome, err
	}
	hold.LastActionID = &action.ID
	if _, err := s.updateWatchdogHoldViaProbeDecision(ctx, hold.ID, watchdogHoldToInput(hold)); err != nil {
		return outcome, err
	}
	outcome.ActionRecorded = true
	outcome.Reconciled = true
	outcome.QuotaHeld = true
	return outcome, nil
}

func (s *Service) fetchLiveAuthForPriorityPatch(ctx context.Context, instance SidecarInstance, expected watchdogLiveAuthExpectation, now time.Time) (SidecarAuthSnapshot, bool, *string, error) {
	live, found, err := s.fetchLiveAuthSnapshot(ctx, instance, expected.AuthID, now)
	if err != nil || !found {
		return live, found, nil, err
	}
	if expected.AuthIndex != "" && strings.TrimSpace(stringValue(live.AuthIndex)) != expected.AuthIndex {
		reason := "current auth_index no longer matches selected auth"
		return live, true, &reason, nil
	}
	if expected.Provider != "" && normalizedSidecarWatchdogProbeProviderKey(stringValue(live.Provider)) != normalizedSidecarWatchdogProbeProviderKey(expected.Provider) {
		reason := "current provider no longer matches selected auth"
		return live, true, &reason, nil
	}
	if expected.Name != "" && strings.TrimSpace(live.Name) != expected.Name {
		reason := "current auth name no longer matches selected auth"
		return live, true, &reason, nil
	}
	if strings.TrimSpace(live.Name) == "" {
		reason := "current auth name is empty"
		return live, true, &reason, nil
	}
	return live, true, nil, nil
}

func (s *Service) persistActiveWatchdogProbeHold(ctx context.Context, input SidecarWatchdogHoldInput) (SidecarWatchdogHold, error) {
	result, err := s.store.PersistWatchdogProbeDecision(ctx, SidecarWatchdogProbeDecision{SidecarID: input.SidecarID, CreateHold: &input})
	if err == nil {
		if result.CreatedHold == nil {
			return SidecarWatchdogHold{}, fmt.Errorf("sidecar watchdog probe decision did not return created hold")
		}
		return *result.CreatedHold, nil
	}
	if !IsStoreError(err, StoreErrorDuplicateActiveHold) {
		return SidecarWatchdogHold{}, err
	}
	existing, found, loadErr := s.store.GetActiveWatchdogHold(ctx, input.SidecarID, input.AuthID)
	if loadErr != nil {
		return SidecarWatchdogHold{}, loadErr
	}
	if !found {
		return SidecarWatchdogHold{}, err
	}
	merged := mergeWatchdogProbeHoldInput(existing, input)
	return s.updateWatchdogHoldViaProbeDecision(ctx, existing.ID, merged)
}

func mergeWatchdogProbeHoldInput(existing SidecarWatchdogHold, input SidecarWatchdogHoldInput) SidecarWatchdogHoldInput {
	merged := input
	if existing.PreviousPriority != nil {
		merged.PreviousPriority = cloneIntPtr(existing.PreviousPriority)
	}
	if existing.HoldUntil != nil && (merged.HoldUntil == nil || existing.HoldUntil.After(merged.HoldUntil.UTC())) {
		merged.HoldUntil = cloneTimePtr(existing.HoldUntil)
	}
	merged.LastActionID = cloneIntPtr(existing.LastActionID)
	if existing.ManualPauseUntil != nil {
		merged.ManualPauseUntil = cloneTimePtr(existing.ManualPauseUntil)
		merged.Status = existing.Status
	}
	return merged
}

func (s *Service) recordHoldActionAndUpdate(ctx context.Context, hold SidecarWatchdogHold, snapshot SidecarAuthSnapshot, actionType string, status string, reason *string, errorMessage *string, now time.Time) (SidecarWatchdogHold, error) {
	action, err := s.createHoldAction(ctx, hold, snapshot, actionType, status, reason, errorMessage, now)
	if err != nil {
		return hold, err
	}
	hold.LastActionID = &action.ID
	return s.updateWatchdogHoldViaProbeDecision(ctx, hold.ID, watchdogHoldToInput(hold))
}

func (s *Service) updateWatchdogHoldViaProbeDecision(ctx context.Context, id int, input SidecarWatchdogHoldInput) (SidecarWatchdogHold, error) {
	result, err := s.store.PersistWatchdogProbeDecision(ctx, SidecarWatchdogProbeDecision{SidecarID: input.SidecarID, UpdateHold: &SidecarWatchdogProbeHoldUpdate{ID: id, Input: input}})
	if err != nil {
		return SidecarWatchdogHold{}, err
	}
	if result.UpdatedHold == nil {
		return SidecarWatchdogHold{}, fmt.Errorf("sidecar watchdog probe decision did not return updated hold")
	}
	return *result.UpdatedHold, nil
}

func (s *Service) recordWatchdogPatchActionWithoutHold(ctx context.Context, sidecarID int, snapshot SidecarAuthSnapshot, live SidecarAuthSnapshot, actionType string, status string, reason *string, previousPriority *int, targetPriority *int, holdUntil *time.Time, patchErr error, now time.Time) error {
	input := watchdogPatchActionInputWithoutHold(sidecarID, snapshot, live, actionType, status, reason, previousPriority, targetPriority, holdUntil)
	if patchErr != nil {
		message := watchdogErrorMessage(patchErr)
		input.ErrorMessage = &message
	}
	_, err := s.createWatchdogAction(ctx, input, now)
	return err
}

func (s *Service) createPendingPatchActionWithoutHold(ctx context.Context, sidecarID int, snapshot SidecarAuthSnapshot, live SidecarAuthSnapshot, actionType string, reason *string, previousPriority *int, targetPriority *int, holdUntil *time.Time) (SidecarWatchdogAction, error) {
	input := watchdogPatchActionInputWithoutHold(sidecarID, snapshot, live, actionType, watchdogActionStatusPending, reason, previousPriority, targetPriority, holdUntil)
	return s.createPendingWatchdogAction(ctx, input)
}

func watchdogPatchActionInputWithoutHold(sidecarID int, snapshot SidecarAuthSnapshot, live SidecarAuthSnapshot, actionType string, status string, reason *string, previousPriority *int, targetPriority *int, holdUntil *time.Time) SidecarWatchdogActionInput {
	input := SidecarWatchdogActionInput{SidecarID: sidecarID, AuthID: stringPtrFromNonEmpty(firstNonEmpty(live.AuthID, snapshot.AuthID)), AuthName: stringPtrFromNonEmpty(firstNonEmpty(live.Name, snapshot.Name)), AuthIndex: cloneStringPtr(live.AuthIndex), Provider: cloneStringPtr(live.Provider), ActionType: actionType, Reason: reason, PreviousPriority: cloneIntPtr(previousPriority), TargetPriority: cloneIntPtr(targetPriority), HoldUntil: cloneTimePtr(holdUntil), Status: status}
	if input.AuthIndex == nil {
		input.AuthIndex = cloneStringPtr(snapshot.AuthIndex)
	}
	if input.Provider == nil {
		input.Provider = cloneStringPtr(snapshot.Provider)
	}
	if snapshot.ID > 0 {
		input.AuthSnapshotID = &snapshot.ID
	}
	return input
}

func watchdogCandidateSnapshot(candidate watchdogProbeCandidate) SidecarAuthSnapshot {
	if candidate.Snapshot == nil {
		return SidecarAuthSnapshot{AuthID: candidate.AuthID, AuthIndex: stringPtrFromNonEmpty(candidate.AuthIndex), Provider: stringPtrFromNonEmpty(candidate.Provider)}
	}
	return *candidate.Snapshot
}

func watchdogCandidateSelectedPriority(candidate watchdogProbeCandidate) *int {
	if candidate.Snapshot == nil {
		return nil
	}
	return cloneIntPtr(candidate.Snapshot.Priority)
}

func watchdogCandidatePreviousPriority(candidate watchdogProbeCandidate, livePriority int, targetPriority int) *int {
	if selected := watchdogCandidateSelectedPriority(candidate); selected != nil && *selected > targetPriority {
		return selected
	}
	if livePriority > targetPriority {
		priority := livePriority
		return &priority
	}
	return nil
}

func watchdogQuotaHoldInputFromProbe(sidecarID int, candidate watchdogProbeCandidate, condition watchdogCondition, previousPriority *int, targetPriority int) SidecarWatchdogHoldInput {
	holdUntil := condition.HoldUntil.UTC()
	return SidecarWatchdogHoldInput{SidecarID: sidecarID, AuthID: candidate.AuthID, AuthIndex: stringPtrFromNonEmpty(candidate.AuthIndex), Provider: stringPtrFromNonEmpty(candidate.Provider), Reason: condition.Reason, ConditionHash: condition.Hash, PreviousPriority: cloneIntPtr(previousPriority), TargetPriority: targetPriority, HoldUntil: &holdUntil, Status: WatchdogHoldStatusActive}
}

func evaluateQuotaProbeObservation(sidecarID int, authID string, policy SidecarWatchdogPolicy, classification sidecarWatchdogProbeClassification, now time.Time) watchdogCondition {
	if classification.Status != watchdogProbeStatusSucceeded || !classification.QuotaExceeded {
		return watchdogCondition{}
	}
	holdUntil := now.Add(time.Duration(normalizedFallbackCooldownSeconds(policy)) * time.Second)
	if classification.QuotaResetAt != nil && classification.QuotaResetAt.After(now) {
		holdUntil = classification.QuotaResetAt.UTC()
	}
	reason := stringValueOr(classification.QuotaReason, watchdogReasonQuotaExceeded)
	condition := watchdogCondition{Triggered: true, Reason: reason, HoldUntil: holdUntil}
	condition.Hash = watchdogProbeConditionHash(sidecarID, authID, reason, holdUntil, classification)
	return condition
}

func (s *Service) applyDueWatchdogProbeResult(ctx context.Context, instance SidecarInstance, policy SidecarWatchdogPolicy, hold SidecarWatchdogHold, snapshot SidecarAuthSnapshot, classification sidecarWatchdogProbeClassification, now time.Time) (watchdogHoldOutcome, error) {
	outcome := watchdogHoldOutcome{Processed: true}
	if classification.Status == watchdogProbeStatusSucceeded && !classification.QuotaExceeded {
		return s.restoreWatchdogHoldAfterHealthyProbe(ctx, instance, policy, hold, snapshot, now)
	}
	if classification.Status == watchdogProbeStatusSucceeded && classification.QuotaExceeded {
		reason := stringValueOr(classification.QuotaReason, watchdogReasonQuotaExceeded)
		if err := s.recordHoldAction(ctx, hold, snapshot, watchdogActionRestoreSkippedUnhealthy, watchdogActionStatusSkipped, &reason, nil, now); err != nil {
			return outcome, err
		}
		outcome.ActionRecorded = true
		outcome.QuotaHeld = true
		return outcome, nil
	}
	reason := classification.Status
	errorMessage := watchdogProbeFailureMessage(classification)
	if err := s.recordHoldAction(ctx, hold, snapshot, classification.Status, watchdogActionStatusFailed, &reason, errorMessage, now); err != nil {
		return outcome, err
	}
	outcome.ActionRecorded = true
	return outcome, nil
}

func (s *Service) restoreWatchdogHoldAfterHealthyProbe(ctx context.Context, instance SidecarInstance, policy SidecarWatchdogPolicy, hold SidecarWatchdogHold, snapshot SidecarAuthSnapshot, now time.Time) (watchdogHoldOutcome, error) {
	outcome := watchdogHoldOutcome{Processed: true}
	if hold.HoldUntil == nil || now.Before(hold.HoldUntil.UTC()) {
		return outcome, nil
	}
	if hold.PreviousPriority == nil {
		reason := "previous priority is missing; operator must choose restore priority"
		if _, err := s.pauseHoldWithAction(ctx, hold, snapshot, policy, watchdogActionRestoreSkippedNeedsOperator, reason, now); err != nil {
			return outcome, err
		}
		outcome.ActionRecorded = true
		return outcome, nil
	}
	expected := watchdogLiveAuthExpectation{AuthID: hold.AuthID, AuthIndex: strings.TrimSpace(stringValue(hold.AuthIndex)), Provider: strings.TrimSpace(stringValue(hold.Provider))}
	live, found, mismatchReason, err := s.fetchLiveAuthForPriorityPatch(ctx, instance, expected, now)
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
	if mismatchReason != nil {
		if _, err := s.pauseHoldWithAction(ctx, hold, live, policy, watchdogActionRestoreSkippedManualChange, *mismatchReason, now); err != nil {
			return outcome, err
		}
		outcome.ActionRecorded = true
		return outcome, nil
	}
	if strings.TrimSpace(stringValue(live.AuthIndex)) == "" {
		reason := "fresh preflight auth is missing auth_index; operator must choose restore priority"
		if _, err := s.pauseHoldWithAction(ctx, hold, live, policy, watchdogActionRestoreSkippedNeedsOperator, reason, now); err != nil {
			return outcome, err
		}
		outcome.ActionRecorded = true
		return outcome, nil
	}
	currentPriority := watchdogAuthPriority(live)
	if currentPriority == *hold.PreviousPriority {
		action, err := s.createHoldAction(ctx, hold, live, watchdogActionRestore, watchdogActionStatusSucceeded, &hold.Reason, nil, now)
		if err != nil {
			return outcome, err
		}
		hold.LastActionID = &action.ID
		hold.Status = WatchdogHoldStatusReleased
		hold.ReleasedAt = &now
		if _, err := s.updateWatchdogHoldViaProbeDecision(ctx, hold.ID, watchdogHoldToInput(hold)); err != nil {
			return outcome, err
		}
		outcome.ActionRecorded = true
		outcome.Released = true
		outcome.Restored = true
		outcome.Reconciled = true
		return outcome, nil
	}
	if currentPriority != hold.TargetPriority {
		reason := "current priority no longer matches watchdog target priority"
		if _, err := s.pauseHoldWithAction(ctx, hold, live, policy, watchdogActionRestoreSkippedManualChange, reason, now); err != nil {
			return outcome, err
		}
		outcome.ActionRecorded = true
		return outcome, nil
	}
	if !watchdogAuthEnabled(live) {
		reason := "fresh preflight auth is not healthy"
		if err := s.recordHoldAction(ctx, hold, live, watchdogActionRestoreSkippedUnhealthy, watchdogActionStatusSkipped, &reason, nil, now); err != nil {
			return outcome, err
		}
		outcome.ActionRecorded = true
		return outcome, nil
	}
	pendingAction, err := s.createPendingRestoreAction(ctx, hold, live)
	if err != nil {
		return outcome, err
	}
	patchErr := s.patchAuthPriority(ctx, instance, live.Name, *hold.PreviousPriority)
	status := watchdogActionStatusSucceeded
	var errorMessage *string
	if patchErr != nil {
		status = watchdogActionStatusFailed
		message := watchdogErrorMessage(patchErr)
		errorMessage = &message
	}
	action, err := s.finalizeWatchdogAction(ctx, pendingAction, status, errorMessage, now)
	if err != nil {
		return outcome, err
	}
	hold.LastActionID = &action.ID
	if patchErr == nil {
		hold.Status = WatchdogHoldStatusReleased
		hold.ReleasedAt = &now
		outcome.Released = true
		outcome.Restored = true
		outcome.Reconciled = true
	}
	if _, err := s.updateWatchdogHoldViaProbeDecision(ctx, hold.ID, watchdogHoldToInput(hold)); err != nil {
		return outcome, err
	}
	outcome.ActionRecorded = true
	return outcome, patchErr
}

func (s *Service) reconcileQuotaWatchdogHoldWithoutProbe(ctx context.Context, instance SidecarInstance, policy SidecarWatchdogPolicy, hold SidecarWatchdogHold, snapshot SidecarAuthSnapshot, now time.Time) (watchdogHoldOutcome, error) {
	outcome := watchdogHoldOutcome{Processed: true}
	if hold.HoldUntil == nil || now.Before(hold.HoldUntil.UTC()) {
		return outcome, nil
	}
	if hold.PreviousPriority == nil {
		reason := "previous priority is missing; operator must choose restore priority"
		if _, err := s.pauseHoldWithAction(ctx, hold, snapshot, policy, watchdogActionRestoreSkippedNeedsOperator, reason, now); err != nil {
			return outcome, err
		}
		outcome.ActionRecorded = true
		return outcome, nil
	}
	expected := watchdogLiveAuthExpectation{AuthID: hold.AuthID, AuthIndex: strings.TrimSpace(stringValue(hold.AuthIndex)), Provider: strings.TrimSpace(stringValue(hold.Provider))}
	live, found, mismatchReason, err := s.fetchLiveAuthForPriorityPatch(ctx, instance, expected, now)
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
	if mismatchReason != nil {
		if _, err := s.pauseHoldWithAction(ctx, hold, live, policy, watchdogActionRestoreSkippedManualChange, *mismatchReason, now); err != nil {
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
	if strings.TrimSpace(stringValue(hold.AuthIndex)) == "" || strings.TrimSpace(stringValue(live.AuthIndex)) == "" {
		reason := "quota hold is missing auth_index; operator must choose restore priority"
		if _, err := s.pauseHoldWithAction(ctx, hold, live, policy, watchdogActionRestoreSkippedNeedsOperator, reason, now); err != nil {
			return outcome, err
		}
		outcome.ActionRecorded = true
		return outcome, nil
	}
	provider := normalizedSidecarWatchdogProbeProviderKey(stringValue(hold.Provider))
	if !sidecarWatchdogProbeProviderSupported(provider) {
		reason := watchdogProbeStatusSkippedUnsupportedProvider
		recorded, err := s.recordHoldSkipOnce(ctx, hold, live, watchdogProbeStatusSkippedUnsupportedProvider, &reason, now)
		if err != nil {
			return outcome, err
		}
		outcome.ActionRecorded = recorded
		outcome.UnsupportedSkipped = true
	}
	return outcome, nil
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
	if watchdogHoldUsesQuotaProbe(hold) {
		return s.reconcileQuotaWatchdogHoldWithoutProbe(ctx, instance, policy, hold, snapshot, now)
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
	currentPriority := watchdogAuthPriority(live)
	if currentPriority == *hold.PreviousPriority {
		action, err := s.createHoldAction(ctx, hold, live, watchdogActionRestore, watchdogActionStatusSucceeded, &hold.Reason, nil, now)
		if err != nil {
			return outcome, err
		}
		hold.LastActionID = &action.ID
		hold.Status = WatchdogHoldStatusReleased
		hold.ReleasedAt = &now
		if _, err := s.store.UpdateWatchdogHold(ctx, hold.ID, watchdogHoldToInput(hold)); err != nil {
			return outcome, err
		}
		outcome.ActionRecorded = true
		outcome.Released = true
		outcome.Restored = true
		outcome.Reconciled = true
		return outcome, nil
	}
	if currentPriority != hold.TargetPriority {
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
	pendingAction, err := s.createPendingRestoreAction(ctx, hold, live)
	if err != nil {
		return outcome, err
	}
	patchErr := s.patchAuthPriority(ctx, instance, live.Name, *hold.PreviousPriority)
	status := watchdogActionStatusSucceeded
	var errorMessage *string
	if patchErr != nil {
		status = watchdogActionStatusFailed
		message := watchdogErrorMessage(patchErr)
		errorMessage = &message
	}
	action, err := s.finalizeWatchdogAction(ctx, pendingAction, status, errorMessage, now)
	if err != nil {
		return outcome, err
	}
	hold.LastActionID = &action.ID
	if patchErr == nil {
		hold.Status = WatchdogHoldStatusReleased
		hold.ReleasedAt = &now
		outcome.Released = true
		outcome.Restored = true
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
	expected := watchdogLiveAuthExpectation{AuthID: snapshot.AuthID, AuthIndex: strings.TrimSpace(stringValue(snapshot.AuthIndex)), Provider: strings.TrimSpace(stringValue(snapshot.Provider)), Name: snapshot.Name}
	live, found, mismatchReason, err := s.fetchLiveAuthForPriorityPatch(ctx, instance, expected, now)
	if err != nil {
		return outcome, err
	}
	targetPriority := normalizedWatchdogTargetPriority(policy)
	previousPriority := cloneIntPtr(snapshot.Priority)
	reason := condition.Reason
	if !found {
		missingReason := "auth no longer exists in fresh preflight read"
		if err := s.recordWatchdogPatchActionWithoutHold(ctx, instance.ID, snapshot, live, watchdogActionDeprioritize, watchdogActionStatusSkipped, &missingReason, previousPriority, &targetPriority, &condition.HoldUntil, nil, now); err != nil {
			return outcome, err
		}
		outcome.ActionRecorded = true
		return outcome, nil
	}
	if mismatchReason != nil {
		if err := s.recordWatchdogPatchActionWithoutHold(ctx, instance.ID, snapshot, live, watchdogActionDeprioritize, watchdogActionStatusSkipped, mismatchReason, previousPriority, &targetPriority, &condition.HoldUntil, nil, now); err != nil {
			return outcome, err
		}
		outcome.ActionRecorded = true
		return outcome, nil
	}
	if watchdogAuthPriority(live) <= targetPriority {
		hold, err := s.createActiveWatchdogHold(ctx, snapshot, policy, condition)
		if err != nil {
			return outcome, err
		}
		if _, err := s.recordHoldActionAndUpdate(ctx, hold, live, watchdogActionDeprioritize, watchdogActionStatusSucceeded, &reason, nil, now); err != nil {
			return outcome, err
		}
		outcome.ActionRecorded = true
		outcome.Reconciled = true
		return outcome, nil
	}
	if previousPriority != nil && watchdogAuthPriority(live) != *previousPriority {
		manualReason := "current priority no longer matches selected priority"
		if err := s.recordWatchdogPatchActionWithoutHold(ctx, instance.ID, snapshot, live, watchdogActionDeprioritize, watchdogActionStatusSkipped, &manualReason, previousPriority, &targetPriority, &condition.HoldUntil, nil, now); err != nil {
			return outcome, err
		}
		outcome.ActionRecorded = true
		return outcome, nil
	}
	pendingAction, err := s.createPendingPatchActionWithoutHold(ctx, instance.ID, snapshot, live, watchdogActionDeprioritize, &reason, previousPriority, &targetPriority, &condition.HoldUntil)
	if err != nil {
		return outcome, err
	}
	patchErr := s.patchAuthPriority(ctx, instance, live.Name, targetPriority)
	if patchErr != nil {
		message := watchdogErrorMessage(patchErr)
		if _, err := s.finalizeWatchdogAction(ctx, pendingAction, watchdogActionStatusFailed, &message, now); err != nil {
			return outcome, err
		}
		outcome.ActionRecorded = true
		return outcome, patchErr
	}
	hold, err := s.createActiveWatchdogHold(ctx, snapshot, policy, condition)
	if err != nil {
		return outcome, err
	}
	pendingAction.HoldID = &hold.ID
	if _, err := s.finalizeWatchdogAction(ctx, pendingAction, watchdogActionStatusSucceeded, nil, now); err != nil {
		return outcome, err
	}
	outcome.ActionRecorded = true
	outcome.Reconciled = true
	outcome.QuotaHeld = true
	return outcome, nil
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
	failureCount, observed := watchdogRecentFailureCount(snapshot, policy, now)
	if !observed {
		return watchdogCondition{}
	}
	if snapshot.Unavailable != nil && *snapshot.Unavailable {
		reason = watchdogReasonUnavailable
	} else if failureCount >= normalizedFailureThreshold(policy) {
		reason = watchdogReasonFailureThreshold
	}
	condition := watchdogCondition{Triggered: reason != "", Reason: reason, FailureCount: failureCount, HoldUntil: fallbackUntil}
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
	input := watchdogHoldActionInput(hold, snapshot, actionType, status, reason, errorMessage)
	return s.createWatchdogAction(ctx, input, now)
}

func (s *Service) createPendingRestoreAction(ctx context.Context, hold SidecarWatchdogHold, snapshot SidecarAuthSnapshot) (SidecarWatchdogAction, error) {
	input := watchdogHoldActionInput(hold, snapshot, watchdogActionRestore, watchdogActionStatusPending, &hold.Reason, nil)
	if hold.PreviousPriority != nil {
		input.TargetPriority = cloneIntPtr(hold.PreviousPriority)
	}
	return s.createPendingWatchdogAction(ctx, input)
}

func watchdogHoldActionInput(hold SidecarWatchdogHold, snapshot SidecarAuthSnapshot, actionType string, status string, reason *string, errorMessage *string) SidecarWatchdogActionInput {
	input := SidecarWatchdogActionInput{SidecarID: hold.SidecarID, HoldID: &hold.ID, AuthID: &hold.AuthID, AuthName: stringPtrFromNonEmpty(snapshot.Name), AuthIndex: cloneStringPtr(hold.AuthIndex), Provider: cloneStringPtr(hold.Provider), ActionType: actionType, Reason: reason, PreviousPriority: cloneIntPtr(hold.PreviousPriority), TargetPriority: &hold.TargetPriority, HoldUntil: cloneTimePtr(hold.HoldUntil), Status: status, ErrorMessage: errorMessage}
	if snapshot.ID > 0 {
		input.AuthSnapshotID = &snapshot.ID
	}
	return input
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
	input := SidecarWatchdogActionInput{SidecarID: hold.SidecarID, HoldID: &hold.ID, AuthID: &hold.AuthID, AuthIndex: cloneStringPtr(hold.AuthIndex), Provider: cloneStringPtr(hold.Provider), ActionType: actionType, Status: watchdogActionStatusSkipped, Reason: reason}
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
		if !optionalStringEqual(action.Reason, input.Reason) || !optionalStringEqual(action.AuthID, input.AuthID) || !optionalStringEqual(action.Provider, input.Provider) || !optionalIntEqual(action.HoldID, input.HoldID) {
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

func (s *Service) createPendingWatchdogAction(ctx context.Context, input SidecarWatchdogActionInput) (SidecarWatchdogAction, error) {
	input.Status = watchdogActionStatusPending
	input.ErrorMessage = nil
	input.CompletedAt = nil
	action, err := s.store.CreateWatchdogAction(ctx, input)
	if err != nil {
		return SidecarWatchdogAction{}, err
	}
	_, err = s.store.CreateWatchdogPendingAction(ctx, SidecarWatchdogPendingActionInput{
		SidecarID:              action.SidecarID,
		HoldID:                 cloneIntPtr(action.HoldID),
		ActionHistoryCreatedAt: action.CreatedAt,
		ActionHistoryID:        action.ID,
		AuthID:                 cloneStringPtr(action.AuthID),
		AuthName:               cloneStringPtr(action.AuthName),
		AuthIndex:              cloneStringPtr(action.AuthIndex),
		Provider:               cloneStringPtr(action.Provider),
		ActionType:             action.ActionType,
		Reason:                 cloneStringPtr(action.Reason),
		PreviousPriority:       cloneIntPtr(action.PreviousPriority),
		TargetPriority:         cloneIntPtr(action.TargetPriority),
		HoldUntil:              cloneTimePtr(action.HoldUntil),
	})
	if err != nil {
		return SidecarWatchdogAction{}, err
	}
	return action, nil
}

func (s *Service) finalizeWatchdogAction(ctx context.Context, action SidecarWatchdogAction, status string, errorMessage *string, now time.Time) (SidecarWatchdogAction, error) {
	completedAt := now
	action.Status = status
	action.ErrorMessage = cloneStringPtr(errorMessage)
	action.CompletedAt = &completedAt
	return s.store.UpdateWatchdogAction(ctx, action.ID, watchdogActionToInput(action))
}

func watchdogActionToInput(action SidecarWatchdogAction) SidecarWatchdogActionInput {
	return SidecarWatchdogActionInput{SidecarID: action.SidecarID, AuthSnapshotID: cloneIntPtr(action.AuthSnapshotID), HoldID: cloneIntPtr(action.HoldID), AuthID: cloneStringPtr(action.AuthID), AuthName: cloneStringPtr(action.AuthName), AuthIndex: cloneStringPtr(action.AuthIndex), Provider: cloneStringPtr(action.Provider), ActionType: action.ActionType, Reason: cloneStringPtr(action.Reason), PreviousPriority: cloneIntPtr(action.PreviousPriority), TargetPriority: cloneIntPtr(action.TargetPriority), HoldUntil: cloneTimePtr(action.HoldUntil), Status: action.Status, ErrorMessage: cloneStringPtr(action.ErrorMessage), CompletedAt: cloneTimePtr(action.CompletedAt)}
}

func watchdogPendingActionToInput(action SidecarWatchdogPendingAction) SidecarWatchdogPendingActionInput {
	return SidecarWatchdogPendingActionInput{SidecarID: action.SidecarID, HoldID: cloneIntPtr(action.HoldID), ActionHistoryCreatedAt: action.ActionHistoryCreatedAt, ActionHistoryID: action.ActionHistoryID, AuthID: cloneStringPtr(action.AuthID), AuthName: cloneStringPtr(action.AuthName), AuthIndex: cloneStringPtr(action.AuthIndex), Provider: cloneStringPtr(action.Provider), ActionType: action.ActionType, Reason: cloneStringPtr(action.Reason), PreviousPriority: cloneIntPtr(action.PreviousPriority), TargetPriority: cloneIntPtr(action.TargetPriority), HoldUntil: cloneTimePtr(action.HoldUntil), AttemptCount: action.AttemptCount, LastAttemptAt: cloneTimePtr(action.LastAttemptAt), LastErrorMessage: cloneStringPtr(action.LastErrorMessage)}
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

func watchdogAuthQuotaReasonFromSnapshot(snapshot SidecarAuthSnapshot) string {
	if strings.TrimSpace(stringValue(snapshot.AuthIndex)) == "" {
		return "missing_auth_index"
	}
	if !watchdogAuthEnabled(snapshot) {
		return "disabled"
	}
	provider := normalizedSidecarWatchdogProbeProviderKey(stringValue(snapshot.Provider))
	if !sidecarWatchdogProbeProviderSupported(provider) {
		return "unsupported_provider"
	}
	return "unknown"
}

func watchdogAuthQuotaStateInputFromSnapshot(snapshot SidecarAuthSnapshot, observedAt time.Time) SidecarAuthQuotaStateInput {
	observedAt = observedAt.UTC()
	reasonCode := watchdogAuthQuotaReasonFromSnapshot(snapshot)
	return SidecarAuthQuotaStateInput{SidecarID: snapshot.SidecarID, AuthID: snapshot.AuthID, AuthIndex: cloneStringPtr(snapshot.AuthIndex), AuthName: stringPtrFromNonEmpty(snapshot.Name), Provider: cloneStringPtr(snapshot.Provider), SnapshotObservedAt: &observedAt, QuotaBand: quotaBandError, ReasonCode: &reasonCode}
}

func watchdogMissingAuthQuotaStateInput(state SidecarAuthQuotaState, observedAt time.Time) SidecarAuthQuotaStateInput {
	observedAt = observedAt.UTC()
	reasonCode := "missing"
	return SidecarAuthQuotaStateInput{SidecarID: state.SidecarID, AuthID: state.AuthID, AuthIndex: cloneStringPtr(state.AuthIndex), AuthName: cloneStringPtr(state.AuthName), Provider: cloneStringPtr(state.Provider), SnapshotObservedAt: &observedAt, QuotaBand: quotaBandError, ReasonCode: &reasonCode}
}

func watchdogAuthEnabled(snapshot SidecarAuthSnapshot) bool {
	return snapshot.Disabled == nil || !*snapshot.Disabled
}

func watchdogAuthPriority(snapshot SidecarAuthSnapshot) int {
	return intPtrValue(snapshot.Priority)
}

type watchdogProbeCandidate struct {
	AuthID    string
	AuthIndex string
	Provider  string
	Snapshot  *SidecarAuthSnapshot
	Hold      *SidecarWatchdogHold
}

func watchdogDueHoldProbeCandidate(hold SidecarWatchdogHold, now time.Time) (watchdogProbeCandidate, bool) {
	if !watchdogDueHoldProbeCandidateEligible(hold, now) {
		return watchdogProbeCandidate{}, false
	}
	return watchdogProbeCandidate{AuthID: strings.TrimSpace(hold.AuthID), AuthIndex: strings.TrimSpace(stringValue(hold.AuthIndex)), Provider: normalizedSidecarWatchdogProbeProviderKey(stringValue(hold.Provider)), Hold: &hold}, true
}

func watchdogDueHoldProbeCandidateEligible(hold SidecarWatchdogHold, now time.Time) bool {
	if hold.Status != WatchdogHoldStatusActive || !watchdogHoldUsesQuotaProbe(hold) {
		return false
	}
	if hold.HoldUntil == nil || now.Before(hold.HoldUntil.UTC()) {
		return false
	}
	if hold.ManualPauseUntil != nil && now.Before(hold.ManualPauseUntil.UTC()) {
		return false
	}
	provider := normalizedSidecarWatchdogProbeProviderKey(stringValue(hold.Provider))
	if !sidecarWatchdogProbeProviderSupported(provider) {
		return false
	}
	return strings.TrimSpace(stringValue(hold.AuthIndex)) != ""
}

func watchdogHoldUsesQuotaProbe(hold SidecarWatchdogHold) bool {
	reason := strings.TrimSpace(hold.Reason)
	return strings.HasPrefix(reason, watchdogReasonQuotaExceeded) || strings.HasPrefix(reason, watchdogReasonQuotaRecoverPending)
}

func watchdogActiveHoldAuthSet(holds []SidecarWatchdogHold) map[string]struct{} {
	active := map[string]struct{}{}
	for _, hold := range holds {
		if watchdogHoldStatusBlocksActiveDuplicate(hold.Status) {
			active[hold.AuthID] = struct{}{}
		}
	}
	return active
}

func activeQuotaScanRun(runs []SidecarQuotaScanRun) (SidecarQuotaScanRun, bool) {
	for _, run := range runs {
		if quotaScanStatusActive(run.Status) {
			return run, true
		}
	}
	return SidecarQuotaScanRun{}, false
}

func quotaStateByAuth(states []SidecarAuthQuotaState) map[string]SidecarAuthQuotaState {
	byAuth := make(map[string]SidecarAuthQuotaState, len(states))
	for _, state := range states {
		byAuth[state.AuthID] = state
	}
	return byAuth
}

func quotaScanRunToInput(run SidecarQuotaScanRun) SidecarQuotaScanRunInput {
	return SidecarQuotaScanRunInput{SidecarID: run.SidecarID, ScanType: run.ScanType, Status: run.Status, RequestedBy: cloneStringPtr(run.RequestedBy), CursorAuthID: cloneStringPtr(run.CursorAuthID), PlannedCount: run.PlannedCount, AttemptedCount: run.AttemptedCount, UsingCount: run.UsingCount, QuotaExceededCount: run.QuotaExceededCount, ErrorCount: run.ErrorCount, SkippedCount: run.SkippedCount, CancelRequestedAt: cloneTimePtr(run.CancelRequestedAt), StartedAt: cloneTimePtr(run.StartedAt), CompletedAt: cloneTimePtr(run.CompletedAt), LastErrorCode: cloneStringPtr(run.LastErrorCode)}
}

func cancelQuotaScanRun(ctx context.Context, store quotaScanRunPersistence, run SidecarQuotaScanRun, now time.Time) (SidecarQuotaScanRun, error) {
	if !quotaScanStatusActive(run.Status) {
		return run, nil
	}
	cancelled := run
	cancelled.Status = quotaScanStatusCancelled
	if cancelled.CancelRequestedAt == nil {
		cancelRequestedAt := now.UTC()
		cancelled.CancelRequestedAt = &cancelRequestedAt
	}
	completedAt := now.UTC()
	cancelled.CompletedAt = &completedAt
	return store.UpdateQuotaScanRun(ctx, run.ID, quotaScanRunToInput(cancelled))
}

func completeQuotaScanRun(ctx context.Context, store quotaScanRunPersistence, run SidecarQuotaScanRun, now time.Time) (SidecarQuotaScanRun, error) {
	if !quotaScanStatusActive(run.Status) {
		return run, nil
	}
	completed := run
	completed.Status = quotaScanStatusCompleted
	completedAt := now.UTC()
	completed.CompletedAt = &completedAt
	return store.UpdateQuotaScanRun(ctx, run.ID, quotaScanRunToInput(completed))
}

func quotaScanRunShouldComplete(run SidecarQuotaScanRun, candidates []watchdogProbeCandidate) bool {
	if run.PlannedCount <= 0 || run.AttemptedCount >= run.PlannedCount || len(candidates) == 0 {
		return true
	}
	if run.ScanType == quotaScanTypeInitial {
		return false
	}
	return len(candidates) <= run.AttemptedCount
}

func watchdogQuotaScanProbeCandidates(policy SidecarWatchdogPolicy, scanRun SidecarQuotaScanRun, snapshots []SidecarAuthSnapshot, activeHoldAuths map[string]struct{}, quotaStates map[string]SidecarAuthQuotaState) []watchdogProbeCandidate {
	candidates := make([]watchdogProbeCandidate, 0, len(snapshots))
	for _, snapshot := range snapshots {
		if !watchdogQuotaScanProbeEligible(policy, scanRun, snapshot, activeHoldAuths, quotaStates) {
			continue
		}
		provider := normalizedSidecarWatchdogProbeProviderKey(stringValue(snapshot.Provider))
		candidates = append(candidates, watchdogProbeCandidate{AuthID: strings.TrimSpace(snapshot.AuthID), AuthIndex: strings.TrimSpace(stringValue(snapshot.AuthIndex)), Provider: provider, Snapshot: &snapshot})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Provider == candidates[j].Provider {
			return candidates[i].AuthID < candidates[j].AuthID
		}
		return candidates[i].Provider < candidates[j].Provider
	})
	return rotateWatchdogDiscoveryCandidates(candidates, scanRun.CursorAuthID)
}

func watchdogQuotaScanProbeEligible(_ SidecarWatchdogPolicy, scanRun SidecarQuotaScanRun, snapshot SidecarAuthSnapshot, activeHoldAuths map[string]struct{}, quotaStates map[string]SidecarAuthQuotaState) bool {
	if _, held := activeHoldAuths[snapshot.AuthID]; held {
		return false
	}
	if !watchdogAuthEnabled(snapshot) || strings.TrimSpace(stringValue(snapshot.AuthIndex)) == "" {
		return false
	}
	provider := normalizedSidecarWatchdogProbeProviderKey(stringValue(snapshot.Provider))
	if !sidecarWatchdogProbeProviderSupported(provider) {
		return false
	}
	if scanRun.ScanType == quotaScanTypeInitial {
		state, ok := quotaStates[snapshot.AuthID]
		return ok && strings.TrimSpace(state.QuotaBand) == quotaBandError
	}
	return true
}

func watchdogRollingRefreshProbeCandidates(policy SidecarWatchdogPolicy, snapshots []SidecarAuthSnapshot, activeHoldAuths map[string]struct{}, quotaStates map[string]SidecarAuthQuotaState, now time.Time) []watchdogProbeCandidate {
	if !policy.RollingRefreshEnabled {
		return nil
	}
	type rollingRefreshCandidate struct {
		candidate    watchdogProbeCandidate
		priority     int
		lastProbedAt *time.Time
	}
	refreshAfter := time.Duration(normalizedRollingRefreshAfterSeconds(policy)) * time.Second
	items := make([]rollingRefreshCandidate, 0, len(snapshots))
	for _, snapshot := range snapshots {
		if !watchdogDiscoveryProbeEligible(policy, snapshot, activeHoldAuths) {
			continue
		}
		state := quotaStates[snapshot.AuthID]
		if state.LastProbedAt != nil && now.Before(state.LastProbedAt.Add(refreshAfter)) {
			continue
		}
		provider := normalizedSidecarWatchdogProbeProviderKey(stringValue(snapshot.Provider))
		items = append(items, rollingRefreshCandidate{candidate: watchdogProbeCandidate{AuthID: strings.TrimSpace(snapshot.AuthID), AuthIndex: strings.TrimSpace(stringValue(snapshot.AuthIndex)), Provider: provider, Snapshot: &snapshot}, priority: watchdogAuthPriority(snapshot), lastProbedAt: cloneTimePtr(state.LastProbedAt)})
	}
	targetPriority := normalizedPrioritizedPriority(policy)
	sort.SliceStable(items, func(i, j int) bool {
		iPrioritized := items[i].priority >= targetPriority
		jPrioritized := items[j].priority >= targetPriority
		if iPrioritized != jPrioritized {
			return iPrioritized
		}
		if items[i].lastProbedAt == nil || items[j].lastProbedAt == nil {
			return items[i].lastProbedAt == nil && items[j].lastProbedAt != nil
		}
		if !items[i].lastProbedAt.Equal(*items[j].lastProbedAt) {
			return items[i].lastProbedAt.Before(*items[j].lastProbedAt)
		}
		return items[i].candidate.AuthID < items[j].candidate.AuthID
	})
	candidates := make([]watchdogProbeCandidate, 0, len(items))
	for _, item := range items {
		candidates = append(candidates, item.candidate)
	}
	return candidates
}

func normalizedRollingRefreshAfterSeconds(policy SidecarWatchdogPolicy) int {
	if policy.RollingRefreshAfterSeconds > 0 {
		return policy.RollingRefreshAfterSeconds
	}
	return DefaultRollingRefreshAfterSeconds
}

func watchdogDiscoveryProbeEligible(policy SidecarWatchdogPolicy, snapshot SidecarAuthSnapshot, activeHoldAuths map[string]struct{}) bool {
	if !watchdogDiscoveryProbeBaseEligible(policy, snapshot, activeHoldAuths) {
		return false
	}
	provider := normalizedSidecarWatchdogProbeProviderKey(stringValue(snapshot.Provider))
	return sidecarWatchdogProbeProviderSupported(provider)
}

func watchdogDiscoveryProbeBaseEligible(policy SidecarWatchdogPolicy, snapshot SidecarAuthSnapshot, activeHoldAuths map[string]struct{}) bool {
	if _, held := activeHoldAuths[snapshot.AuthID]; held {
		return false
	}
	if !watchdogAuthEnabled(snapshot) || watchdogAuthPriority(snapshot) < normalizedPrioritizedPriority(policy) {
		return false
	}
	return strings.TrimSpace(stringValue(snapshot.AuthIndex)) != ""
}

func (s *Service) recordUnsupportedDiscoveryWatchdogProbeSkips(ctx context.Context, sidecarID int, policy SidecarWatchdogPolicy, snapshots []SidecarAuthSnapshot, activeHoldAuths map[string]struct{}, now time.Time) (int, int, error) {
	unsupportedSkipped, actions := unsupportedDiscoveryWatchdogProbeSkipActions(sidecarID, policy, snapshots, activeHoldAuths)
	actionCount := 0
	for _, action := range actions {
		recorded, err := s.recordWatchdogSkipOnce(ctx, action, now)
		if err != nil {
			return unsupportedSkipped, actionCount, err
		}
		if recorded {
			actionCount++
		}
	}
	return unsupportedSkipped, actionCount, nil
}

func unsupportedDiscoveryWatchdogProbeSkipActions(sidecarID int, policy SidecarWatchdogPolicy, snapshots []SidecarAuthSnapshot, activeHoldAuths map[string]struct{}) (int, []SidecarWatchdogActionInput) {
	unsupportedByProvider := map[string]struct{}{}
	unsupportedSkipped := 0
	for _, snapshot := range snapshots {
		if !watchdogDiscoveryProbeBaseEligible(policy, snapshot, activeHoldAuths) {
			continue
		}
		provider := normalizedSidecarWatchdogProbeProviderKey(stringValue(snapshot.Provider))
		if sidecarWatchdogProbeProviderSupported(provider) {
			continue
		}
		unsupportedSkipped++
		unsupportedByProvider[provider] = struct{}{}
	}
	providers := make([]string, 0, len(unsupportedByProvider))
	for provider := range unsupportedByProvider {
		providers = append(providers, provider)
	}
	sort.Strings(providers)
	reason := watchdogProbeStatusSkippedUnsupportedProvider
	actions := make([]SidecarWatchdogActionInput, 0, len(providers))
	for _, provider := range providers {
		actions = append(actions, SidecarWatchdogActionInput{SidecarID: sidecarID, Provider: stringPtrFromNonEmpty(provider), ActionType: watchdogProbeStatusSkippedUnsupportedProvider, Status: watchdogActionStatusSkipped, Reason: &reason})
	}
	return unsupportedSkipped, actions
}

func watchdogProbeClassificationFailed(classification sidecarWatchdogProbeClassification) bool {
	return strings.HasPrefix(classification.Status, "probe_failed_")
}

func rotateWatchdogDiscoveryCandidates(candidates []watchdogProbeCandidate, cursorAuthID *string) []watchdogProbeCandidate {
	if len(candidates) == 0 || cursorAuthID == nil || strings.TrimSpace(*cursorAuthID) == "" {
		return candidates
	}
	cursor := strings.TrimSpace(*cursorAuthID)
	start := 0
	for index, candidate := range candidates {
		if candidate.AuthID == cursor {
			start = index + 1
			break
		}
	}
	if start == 0 || start >= len(candidates) {
		return candidates
	}
	rotated := make([]watchdogProbeCandidate, 0, len(candidates))
	rotated = append(rotated, candidates[start:]...)
	rotated = append(rotated, candidates[:start]...)
	return rotated
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

func validateWatchdogProbeRuntimePolicy(policy SidecarWatchdogPolicy) error {
	if policy.ProbeConcurrency <= 0 {
		return invalidInputError("probe_concurrency must be >= 1")
	}
	if policy.ProbeConcurrency > MaxProbeConcurrency {
		return invalidInputError(fmt.Sprintf("probe_concurrency must be <= %d", MaxProbeConcurrency))
	}
	timeoutSeconds := normalizedProbeTimeoutSeconds(policy)
	maxBudgetSeconds := watchdogProbeConcurrencyBudgetMaxSeconds()
	if timeoutSeconds > maxBudgetSeconds {
		return invalidInputError("probe_timeout_seconds exceeds watchdog worker budget")
	}
	if policy.ProbeJitterMinMS < 0 || policy.ProbeJitterMaxMS < 0 {
		return invalidInputError("probe jitter bounds must be non-negative")
	}
	if policy.ProbeJitterMaxMS != 0 && policy.ProbeJitterMaxMS < policy.ProbeJitterMinMS {
		return invalidInputError("probe_jitter_max_ms must be >= probe_jitter_min_ms")
	}
	return nil
}

func watchdogEffectiveProbeTimeout(policy SidecarWatchdogPolicy, startedAt time.Time, now time.Time) (time.Duration, bool) {
	if startedAt.IsZero() {
		startedAt = now
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	budgetDeadline := startedAt.Add(sidecarWatchdogWorkerTimeout - sidecarWatchdogWorkerSafetyMargin())
	remaining := budgetDeadline.Sub(now)
	if remaining <= 0 {
		return 0, false
	}
	policyTimeout := time.Duration(normalizedProbeTimeoutSeconds(policy)) * time.Second
	if remaining < policyTimeout {
		return remaining, true
	}
	return policyTimeout, true
}

func watchdogProbeRequestTimeoutSeconds(timeout time.Duration) int {
	if timeout <= 0 {
		return 1
	}
	seconds := int(timeout / time.Second)
	if timeout%time.Second != 0 {
		seconds++
	}
	if seconds < 1 {
		return 1
	}
	return seconds
}

func watchdogProbeObservationInput(sidecarID int, candidate watchdogProbeCandidate, classification sidecarWatchdogProbeClassification, now time.Time) (SidecarWatchdogProbeObservationInput, error) {
	windows := classification.Windows
	if windows == nil {
		windows = []sidecarWatchdogQuotaWindow{}
	}
	windowsJSON, err := json.Marshal(windows)
	if err != nil {
		return SidecarWatchdogProbeObservationInput{}, err
	}
	return SidecarWatchdogProbeObservationInput{SidecarID: sidecarID, AuthID: candidate.AuthID, AuthIndex: stringPtrFromNonEmpty(candidate.AuthIndex), Provider: stringPtrFromNonEmpty(candidate.Provider), ProbedAt: now, ProbeStatus: classification.Status, UpstreamStatusCode: cloneIntPtr(classification.UpstreamStatusCode), QuotaExceeded: classification.QuotaExceeded, ReasonCode: cloneStringPtr(classification.QuotaReason), QuotaResetAt: cloneTimePtr(classification.QuotaResetAt), BlockingWindow: cloneStringPtr(classification.BlockingWindow), WindowsJSON: windowsJSON, ErrorCode: cloneStringPtr(classification.ErrorCode)}, nil
}

func watchdogHoldUpdateForProbeResult(hold SidecarWatchdogHold, policy SidecarWatchdogPolicy, classification sidecarWatchdogProbeClassification, now time.Time) *SidecarWatchdogHoldInput {
	updated := hold
	if classification.Status != watchdogProbeStatusSucceeded {
		nextProbeAfter := now.Add(watchdogDueHoldProbeRetryCooldown(policy))
		updated.HoldUntil = &nextProbeAfter
		input := watchdogHoldToInput(updated)
		return &input
	}
	if !classification.QuotaExceeded {
		return nil
	}
	holdUntil := now.Add(time.Duration(normalizedFallbackCooldownSeconds(policy)) * time.Second)
	if classification.QuotaResetAt != nil && classification.QuotaResetAt.After(now) {
		holdUntil = classification.QuotaResetAt.UTC()
	}
	updated.Reason = stringValueOr(classification.QuotaReason, watchdogReasonQuotaExceeded)
	updated.ConditionHash = watchdogProbeConditionHash(updated.SidecarID, updated.AuthID, updated.Reason, holdUntil, classification)
	updated.HoldUntil = &holdUntil
	input := watchdogHoldToInput(updated)
	return &input
}

func watchdogProbeConditionHash(sidecarID int, authID string, reason string, holdUntil time.Time, classification sidecarWatchdogProbeClassification) string {
	input := fmt.Sprintf("%d|%s|%s|%s|%s|%s", sidecarID, authID, reason, holdUntil.Format(time.RFC3339Nano), stringValue(classification.BlockingWindow), stringValue(classification.ErrorCode))
	sum := sha256.Sum256([]byte(input))
	return hex.EncodeToString(sum[:])
}

func watchdogDueHoldProbeRetryCooldown(policy SidecarWatchdogPolicy) time.Duration {
	cooldown := sidecarWatchdogWorkerInterval
	if cooldown <= 0 {
		return time.Duration(normalizedProbeTimeoutSeconds(policy)) * time.Second
	}
	return cooldown
}

func watchdogProbeFailureMessage(classification sidecarWatchdogProbeClassification) *string {
	if classification.UpstreamStatusCode != nil {
		return stringPtrFromNonEmpty(fmt.Sprintf("%s status=%d", classification.Status, *classification.UpstreamStatusCode))
	}
	if classification.ErrorCode != nil {
		return stringPtrFromNonEmpty(fmt.Sprintf("%s: %s", classification.Status, *classification.ErrorCode))
	}
	return stringPtrFromNonEmpty(classification.Status)
}

func sidecarWatchdogWorkerSafetyMargin() time.Duration {
	return 5 * time.Second
}

func normalizedWatchdogTargetPriority(policy SidecarWatchdogPolicy) int {
	if policy.QuotaExceededPriority < 0 {
		return DefaultQuotaExceededPriority
	}
	return policy.QuotaExceededPriority
}

func normalizedPrioritizedPriority(policy SidecarWatchdogPolicy) int {
	if policy.UsingPriority <= 0 {
		return DefaultUsingPriority
	}
	return policy.UsingPriority
}

func normalizedProbeConcurrency(policy SidecarWatchdogPolicy) int {
	if policy.ProbeConcurrency <= 0 {
		return DefaultProbeConcurrency
	}
	return policy.ProbeConcurrency
}

func normalizedProbeBatchCooldownSeconds(policy SidecarWatchdogPolicy) int {
	if policy.ProbeBatchCooldownSeconds <= 0 {
		return DefaultProbeBatchCooldownSeconds
	}
	return policy.ProbeBatchCooldownSeconds
}

func watchdogProbeBatchCooldownElapsed(policy SidecarWatchdogPolicy, now time.Time) bool {
	if policy.ProbeNextBatchAfter != nil && !policy.ProbeNextBatchAfter.IsZero() {
		return !now.UTC().Before(policy.ProbeNextBatchAfter.UTC())
	}
	if policy.ProbeLastBatchCompletedAt == nil || policy.ProbeLastBatchCompletedAt.IsZero() {
		return true
	}
	cooldownUntil := policy.ProbeLastBatchCompletedAt.UTC().Add(time.Duration(normalizedProbeBatchCooldownSeconds(policy)) * time.Second)
	return !now.UTC().Before(cooldownUntil)
}

func normalizedProbeTimeoutSeconds(policy SidecarWatchdogPolicy) int {
	if policy.ProbeTimeoutSeconds <= 0 {
		return DefaultProbeTimeoutSeconds
	}
	return policy.ProbeTimeoutSeconds
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func stringValueOr(value *string, fallback string) string {
	if strings.TrimSpace(stringValue(value)) == "" {
		return fallback
	}
	return strings.TrimSpace(*value)
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
	if actionHistoryTextLooksSensitive(detail) {
		return "redacted-by-prism"
	}
	return detail
}

type watchdogRunLease interface {
	Release(context.Context) error
}

type watchdogRunLeasePersistence interface {
	TryAcquireWatchdogLease(context.Context, int) (watchdogRunLease, bool, error)
}

func (s *Service) tryAcquireWatchdogLease(ctx context.Context, sidecarID int) (watchdogRunLease, bool, error) {
	locker, ok := s.store.(watchdogRunLeasePersistence)
	if !ok {
		return nil, true, nil
	}
	return locker.TryAcquireWatchdogLease(ctx, sidecarID)
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
