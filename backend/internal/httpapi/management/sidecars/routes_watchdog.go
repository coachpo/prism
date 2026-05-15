package sidecars

import (
	"context"
	"net/http"
	"time"
)

type watchdogPolicyUpdateRequest struct {
	ExpectedRevisionID           *int64 `json:"expected_revision_id"`
	Enabled                      *bool  `json:"enabled"`
	WatchdogSweepIntervalSeconds *int   `json:"watchdog_sweep_interval_seconds"`
	FailureThreshold             *int   `json:"failure_threshold"`
	FailureWindowSeconds         *int   `json:"failure_window_seconds"`
	FallbackCooldownSeconds      *int   `json:"fallback_cooldown_seconds"`
	UsingPriority                *int   `json:"using_priority"`
	QuotaExceededPriority        *int   `json:"quota_exceeded_priority"`
	WorkingPriority              *int   `json:"working_priority"`
	EmptyQuotaPriority           *int   `json:"empty_quota_priority"`
	InitialPriority              *int   `json:"initial_priority"`
	ErrorPriority                *int   `json:"error_priority"`
	ManualOverridePauseSeconds   *int   `json:"manual_override_pause_seconds"`
	ProbeConcurrency             *int   `json:"probe_concurrency"`
	ProbeTimeoutSeconds          *int   `json:"probe_timeout_seconds"`
	ProbeBatchCooldownSeconds    *int   `json:"probe_batch_cooldown_seconds"`
	ProbeJitterMinMS             *int   `json:"probe_jitter_min_ms"`
	ProbeJitterMaxMS             *int   `json:"probe_jitter_max_ms"`
	CooldownJitterPercent        *int   `json:"cooldown_jitter_percent"`
	QuotaInventoryEnabled        *bool  `json:"quota_inventory_enabled"`
	InitialScanEnabled           *bool  `json:"initial_scan_enabled"`
	RollingRefreshEnabled        *bool  `json:"rolling_refresh_enabled"`
	RollingRefreshAfterSeconds   *int   `json:"rolling_refresh_after_seconds"`
}

type watchdogPolicyApplyRequest struct {
	TargetRevisionID   *int64 `json:"target_revision_id"`
	ExpectedRevisionID *int64 `json:"expected_revision_id"`
}

type watchdogPolicyResponse struct {
	ID                           int                             `json:"id"`
	SidecarID                    int                             `json:"sidecar_id"`
	ActiveRevisionID             *int64                          `json:"active_revision_id"`
	PendingRevisionID            *int64                          `json:"pending_revision_id"`
	HasPendingChanges            bool                            `json:"has_pending_changes"`
	ActiveRevision               *watchdogPolicyRevisionResponse `json:"active_revision"`
	PendingRevision              *watchdogPolicyRevisionResponse `json:"pending_revision"`
	ActiveSweep                  *watchdogActiveSweepResponse    `json:"active_sweep"`
	Enabled                      bool                            `json:"enabled"`
	WatchdogSweepIntervalSeconds int                             `json:"watchdog_sweep_interval_seconds"`
	FailureThreshold             int                             `json:"failure_threshold"`
	FailureWindowSeconds         int                             `json:"failure_window_seconds"`
	FallbackCooldownSeconds      int                             `json:"fallback_cooldown_seconds"`
	UsingPriority                int                             `json:"using_priority"`
	QuotaExceededPriority        int                             `json:"quota_exceeded_priority"`
	WorkingPriority              int                             `json:"working_priority"`
	EmptyQuotaPriority           int                             `json:"empty_quota_priority"`
	InitialPriority              int                             `json:"initial_priority"`
	ErrorPriority                int                             `json:"error_priority"`
	ManualOverridePauseSeconds   int                             `json:"manual_override_pause_seconds"`
	ProbeConcurrency             int                             `json:"probe_concurrency"`
	ProbeTimeoutSeconds          int                             `json:"probe_timeout_seconds"`
	ProbeBatchCooldownSeconds    int                             `json:"probe_batch_cooldown_seconds"`
	ProbeJitterMinMS             int                             `json:"probe_jitter_min_ms"`
	ProbeJitterMaxMS             int                             `json:"probe_jitter_max_ms"`
	CooldownJitterPercent        int                             `json:"cooldown_jitter_percent"`
	QuotaInventoryEnabled        bool                            `json:"quota_inventory_enabled"`
	InitialScanEnabled           bool                            `json:"initial_scan_enabled"`
	RollingRefreshEnabled        bool                            `json:"rolling_refresh_enabled"`
	RollingRefreshAfterSeconds   int                             `json:"rolling_refresh_after_seconds"`
	CreatedAt                    time.Time                       `json:"created_at"`
	UpdatedAt                    time.Time                       `json:"updated_at"`
}

type watchdogPolicyRevisionResponse struct {
	ID                           int64     `json:"id"`
	PolicyID                     int       `json:"policy_id"`
	SidecarID                    int       `json:"sidecar_id"`
	Enabled                      bool      `json:"enabled"`
	WatchdogSweepIntervalSeconds int       `json:"watchdog_sweep_interval_seconds"`
	FailureThreshold             int       `json:"failure_threshold"`
	FailureWindowSeconds         int       `json:"failure_window_seconds"`
	FallbackCooldownSeconds      int       `json:"fallback_cooldown_seconds"`
	UsingPriority                int       `json:"using_priority"`
	QuotaExceededPriority        int       `json:"quota_exceeded_priority"`
	WorkingPriority              int       `json:"working_priority"`
	EmptyQuotaPriority           int       `json:"empty_quota_priority"`
	InitialPriority              int       `json:"initial_priority"`
	ErrorPriority                int       `json:"error_priority"`
	ManualOverridePauseSeconds   int       `json:"manual_override_pause_seconds"`
	ProbeConcurrency             int       `json:"probe_concurrency"`
	ProbeTimeoutSeconds          int       `json:"probe_timeout_seconds"`
	ProbeBatchCooldownSeconds    int       `json:"probe_batch_cooldown_seconds"`
	ProbeJitterMinMS             int       `json:"probe_jitter_min_ms"`
	ProbeJitterMaxMS             int       `json:"probe_jitter_max_ms"`
	CooldownJitterPercent        int       `json:"cooldown_jitter_percent"`
	QuotaInventoryEnabled        bool      `json:"quota_inventory_enabled"`
	InitialScanEnabled           bool      `json:"initial_scan_enabled"`
	RollingRefreshEnabled        bool      `json:"rolling_refresh_enabled"`
	RollingRefreshAfterSeconds   int       `json:"rolling_refresh_after_seconds"`
	CreatedAt                    time.Time `json:"created_at"`
}

type watchdogActiveSweepResponse struct {
	SweepID          string                              `json:"sweep_id"`
	Status           string                              `json:"status"`
	PolicyRevisionID int64                               `json:"policy_revision_id"`
	StartedAt        time.Time                           `json:"started_at"`
	Progress         watchdogActiveSweepProgressResponse `json:"progress"`
}

type watchdogActiveSweepProgressResponse struct {
	TotalItems      int `json:"total_items"`
	PendingItems    int `json:"pending_items"`
	ActiveItems     int `json:"active_items"`
	SucceededItems  int `json:"succeeded_items"`
	FailedItems     int `json:"failed_items"`
	CancelledItems  int `json:"cancelled_items"`
	SupersededItems int `json:"superseded_items"`
	TerminalItems   int `json:"terminal_items"`
}

type actionHistoryListResponse struct {
	Items []actionRecordResponse `json:"items"`
}

type actionRecordResponse struct {
	ID                    int        `json:"id"`
	SidecarID             int        `json:"sidecar_id"`
	AuthSnapshotID        *int       `json:"auth_snapshot_id,omitempty"`
	AuthID                *string    `json:"auth_id,omitempty"`
	AuthIndex             *string    `json:"auth_index,omitempty"`
	Provider              *string    `json:"provider,omitempty"`
	HoldID                *int       `json:"hold_id,omitempty"`
	ActionType            string     `json:"action_type"`
	Status                string     `json:"status"`
	Reason                *string    `json:"reason,omitempty"`
	PreviousPriority      *int       `json:"previous_priority,omitempty"`
	PreviousPriorityState string     `json:"previous_priority_state"`
	TargetPriority        *int       `json:"target_priority,omitempty"`
	TargetPriorityState   string     `json:"target_priority_state"`
	PriorityState         string     `json:"priority_state"`
	MutationOutcome       string     `json:"mutation_outcome"`
	HoldUntil             *time.Time `json:"hold_until,omitempty"`
	ErrorMessage          *string    `json:"error_message,omitempty"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`
	CompletedAt           *time.Time `json:"completed_at,omitempty"`
}

func (s *Service) handleGetWatchdogPolicy(w http.ResponseWriter, r *http.Request) {
	id, ok := s.parseSidecarID(w, r)
	if !ok || !s.ensureSidecarExists(w, r, id) {
		return
	}
	state, err := s.getWatchdogPolicyRevisionState(r.Context(), id)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	response, err := s.buildWatchdogPolicyResponse(r.Context(), state)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleUpdateWatchdogPolicy(w http.ResponseWriter, r *http.Request) {
	id, ok := s.parseSidecarID(w, r)
	if !ok || !s.ensureSidecarExists(w, r, id) {
		return
	}
	var requestBody watchdogPolicyUpdateRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	updated, err := s.saveWatchdogPolicyRevisionUpdate(r.Context(), id, requestBody)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	response, err := s.buildWatchdogPolicyResponse(r.Context(), updated)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	s.recordAction(r.Context(), id, "watchdog-policy.save", "succeeded", nil)
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleApplyWatchdogPolicy(w http.ResponseWriter, r *http.Request) {
	s.handleApplyWatchdogPolicyWithMode(w, r, SidecarWatchdogPolicyApplyFuture, "watchdog-policy.apply")
}

func (s *Service) handleApplyAndRestartWatchdogPolicy(w http.ResponseWriter, r *http.Request) {
	s.handleApplyWatchdogPolicyWithMode(w, r, SidecarWatchdogPolicyApplyAndRestart, "watchdog-policy.apply-restart")
}

func (s *Service) handleApplyWatchdogPolicyWithMode(w http.ResponseWriter, r *http.Request, mode SidecarWatchdogPolicyApplyMode, actionType string) {
	id, ok := s.parseSidecarID(w, r)
	if !ok || !s.ensureSidecarExists(w, r, id) {
		return
	}
	var requestBody watchdogPolicyApplyRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	if requestBody.TargetRevisionID == nil || *requestBody.TargetRevisionID <= 0 {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "target_revision_id is required")
		return
	}
	if requestBody.ExpectedRevisionID == nil || *requestBody.ExpectedRevisionID <= 0 {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "expected_revision_id is required")
		return
	}
	var state SidecarWatchdogPolicyRevisionState
	var err error
	switch mode {
	case SidecarWatchdogPolicyApplyFuture:
		state, err = s.applyWatchdogPolicyRevision(r.Context(), id, *requestBody.TargetRevisionID, *requestBody.ExpectedRevisionID)
	case SidecarWatchdogPolicyApplyAndRestart:
		state, err = s.applyAndRestartWatchdogPolicyRevision(r.Context(), id, *requestBody.TargetRevisionID, *requestBody.ExpectedRevisionID)
	default:
		err = invalidInputError("unsupported watchdog policy apply mode")
	}
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	response, err := s.buildWatchdogPolicyResponse(r.Context(), state)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	s.recordAction(r.Context(), id, actionType, "succeeded", nil)
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleListActionHistory(w http.ResponseWriter, r *http.Request) {
	id, ok := s.parseSidecarID(w, r)
	if !ok || !s.ensureSidecarExists(w, r, id) {
		return
	}
	policyState, err := s.getWatchdogPolicyRevisionState(r.Context(), id)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	policy := policyState.Policy
	var actions []SidecarWatchdogAction
	if lister, ok := s.store.(actionHistoryPersistence); ok {
		actions, err = lister.ListWatchdogActions(r.Context(), id)
		if err != nil {
			writeDomainError(w, r, s.corsSnapshot(), err)
			return
		}
	}
	items := make([]actionRecordResponse, 0, len(actions))
	for _, action := range actions {
		items = append(items, buildActionRecordResponse(action, policy))
	}
	writeJSON(w, http.StatusOK, actionHistoryListResponse{Items: items})
}

func (s *Service) buildWatchdogPolicyResponse(ctx context.Context, state SidecarWatchdogPolicyRevisionState) (watchdogPolicyResponse, error) {
	policy := state.Policy
	active := buildWatchdogPolicyRevisionResponse(state.ActiveRevision)
	pending := buildWatchdogPolicyRevisionResponse(state.PendingRevision)
	activeSweep, err := s.buildWatchdogActiveSweepResponse(ctx, state.ActiveSweep)
	if err != nil {
		return watchdogPolicyResponse{}, err
	}
	response := watchdogPolicyResponse{ID: policy.ID, SidecarID: policy.SidecarID, ActiveRevisionID: cloneInt64Ptr(policy.ActiveRevisionID), PendingRevisionID: cloneInt64Ptr(policy.PendingRevisionID), HasPendingChanges: state.HasPendingChanges, ActiveRevision: active, PendingRevision: pending, ActiveSweep: activeSweep, CreatedAt: policy.CreatedAt, UpdatedAt: policy.UpdatedAt}
	if active != nil {
		copyWatchdogPolicyRevisionFieldsToResponse(&response, *active)
	} else {
		response.Enabled = policy.Enabled
		response.FailureThreshold = policy.FailureThreshold
		response.FailureWindowSeconds = policy.FailureWindowSeconds
		response.FallbackCooldownSeconds = policy.FallbackCooldownSeconds
		response.UsingPriority = policy.UsingPriority
		response.QuotaExceededPriority = policy.QuotaExceededPriority
		response.WorkingPriority = policy.WorkingPriority
		response.EmptyQuotaPriority = policy.EmptyQuotaPriority
		response.InitialPriority = policy.InitialPriority
		response.ErrorPriority = policy.ErrorPriority
		response.ManualOverridePauseSeconds = policy.ManualOverridePauseSeconds
		response.ProbeConcurrency = policy.ProbeConcurrency
		response.ProbeTimeoutSeconds = policy.ProbeTimeoutSeconds
		response.ProbeBatchCooldownSeconds = policy.ProbeBatchCooldownSeconds
		response.ProbeJitterMinMS = policy.ProbeJitterMinMS
		response.ProbeJitterMaxMS = policy.ProbeJitterMaxMS
		response.CooldownJitterPercent = policy.CooldownJitterPercent
		response.QuotaInventoryEnabled = policy.QuotaInventoryEnabled
		response.InitialScanEnabled = policy.InitialScanEnabled
		response.RollingRefreshEnabled = policy.RollingRefreshEnabled
		response.RollingRefreshAfterSeconds = policy.RollingRefreshAfterSeconds
		response.WatchdogSweepIntervalSeconds = normalizedLegacyWatchdogSweepIntervalSeconds(policy)
	}
	return response, nil
}

func buildWatchdogPolicyRevisionResponse(revision *SidecarWatchdogPolicyRevision) *watchdogPolicyRevisionResponse {
	if revision == nil {
		return nil
	}
	return &watchdogPolicyRevisionResponse{ID: revision.ID, PolicyID: revision.PolicyID, SidecarID: revision.SidecarID, Enabled: revision.Enabled, WatchdogSweepIntervalSeconds: revision.WatchdogSweepIntervalSeconds, FailureThreshold: revision.FailureThreshold, FailureWindowSeconds: revision.FailureWindowSeconds, FallbackCooldownSeconds: revision.FallbackCooldownSeconds, UsingPriority: revision.UsingPriority, QuotaExceededPriority: revision.QuotaExceededPriority, WorkingPriority: revision.WorkingPriority, EmptyQuotaPriority: revision.EmptyQuotaPriority, InitialPriority: revision.InitialPriority, ErrorPriority: revision.ErrorPriority, ManualOverridePauseSeconds: revision.ManualOverridePauseSeconds, ProbeConcurrency: revision.ProbeConcurrency, ProbeTimeoutSeconds: revision.ProbeTimeoutSeconds, ProbeBatchCooldownSeconds: revision.ProbeBatchCooldownSeconds, ProbeJitterMinMS: revision.ProbeJitterMinMS, ProbeJitterMaxMS: revision.ProbeJitterMaxMS, CooldownJitterPercent: revision.CooldownJitterPercent, QuotaInventoryEnabled: revision.QuotaInventoryEnabled, InitialScanEnabled: revision.InitialScanEnabled, RollingRefreshEnabled: revision.RollingRefreshEnabled, RollingRefreshAfterSeconds: revision.RollingRefreshAfterSeconds, CreatedAt: revision.CreatedAt}
}

func copyWatchdogPolicyRevisionFieldsToResponse(response *watchdogPolicyResponse, revision watchdogPolicyRevisionResponse) {
	response.Enabled = revision.Enabled
	response.WatchdogSweepIntervalSeconds = revision.WatchdogSweepIntervalSeconds
	response.FailureThreshold = revision.FailureThreshold
	response.FailureWindowSeconds = revision.FailureWindowSeconds
	response.FallbackCooldownSeconds = revision.FallbackCooldownSeconds
	response.UsingPriority = revision.UsingPriority
	response.QuotaExceededPriority = revision.QuotaExceededPriority
	response.WorkingPriority = revision.WorkingPriority
	response.EmptyQuotaPriority = revision.EmptyQuotaPriority
	response.InitialPriority = revision.InitialPriority
	response.ErrorPriority = revision.ErrorPriority
	response.ManualOverridePauseSeconds = revision.ManualOverridePauseSeconds
	response.ProbeConcurrency = revision.ProbeConcurrency
	response.ProbeTimeoutSeconds = revision.ProbeTimeoutSeconds
	response.ProbeBatchCooldownSeconds = revision.ProbeBatchCooldownSeconds
	response.ProbeJitterMinMS = revision.ProbeJitterMinMS
	response.ProbeJitterMaxMS = revision.ProbeJitterMaxMS
	response.CooldownJitterPercent = revision.CooldownJitterPercent
	response.QuotaInventoryEnabled = revision.QuotaInventoryEnabled
	response.InitialScanEnabled = revision.InitialScanEnabled
	response.RollingRefreshEnabled = revision.RollingRefreshEnabled
	response.RollingRefreshAfterSeconds = revision.RollingRefreshAfterSeconds
}

func (s *Service) buildWatchdogActiveSweepResponse(ctx context.Context, sweep *SidecarWatchdogSweep) (*watchdogActiveSweepResponse, error) {
	if sweep == nil {
		return nil, nil
	}
	progress, err := s.buildWatchdogActiveSweepProgress(ctx, sweep.SweepID)
	if err != nil {
		return nil, err
	}
	return &watchdogActiveSweepResponse{SweepID: sweep.SweepID, Status: sweep.Status, PolicyRevisionID: sweep.PolicyRevisionID, StartedAt: sweep.StartedAt, Progress: progress}, nil
}

func (s *Service) buildWatchdogActiveSweepProgress(ctx context.Context, sweepID string) (watchdogActiveSweepProgressResponse, error) {
	itemStore, ok := s.store.(watchdogSweepItemPersistence)
	if !ok {
		return watchdogActiveSweepProgressResponse{}, nil
	}
	items, err := itemStore.ListWatchdogSweepItems(ctx, sweepID)
	if err != nil {
		return watchdogActiveSweepProgressResponse{}, err
	}
	progress := watchdogActiveSweepProgressResponse{TotalItems: len(items)}
	for _, item := range items {
		switch SidecarWatchdogSweepItemStatus(item.Status) {
		case SidecarWatchdogSweepItemStatusQueued:
			progress.PendingItems++
		case SidecarWatchdogSweepItemStatusLeased:
			progress.ActiveItems++
		case SidecarWatchdogSweepItemStatusSucceeded:
			progress.SucceededItems++
			progress.TerminalItems++
		case SidecarWatchdogSweepItemStatusFailed:
			progress.FailedItems++
			progress.TerminalItems++
		case SidecarWatchdogSweepItemStatusCancelled:
			progress.CancelledItems++
			progress.TerminalItems++
		case SidecarWatchdogSweepItemStatusSuperseded:
			progress.SupersededItems++
			progress.TerminalItems++
		}
	}
	return progress, nil
}

func watchdogPolicyRevisionInputFromRevision(revision SidecarWatchdogPolicyRevision) SidecarWatchdogPolicyRevisionInput {
	return SidecarWatchdogPolicyRevisionInput{SidecarID: revision.SidecarID, Enabled: revision.Enabled, WatchdogSweepIntervalSeconds: revision.WatchdogSweepIntervalSeconds, ProbeConcurrency: revision.ProbeConcurrency, ProbeTimeoutSeconds: revision.ProbeTimeoutSeconds, ProbeBatchCooldownSeconds: revision.ProbeBatchCooldownSeconds, ProbeJitterMinMS: revision.ProbeJitterMinMS, ProbeJitterMaxMS: revision.ProbeJitterMaxMS, CooldownJitterPercent: revision.CooldownJitterPercent, UsingPriority: revision.UsingPriority, QuotaExceededPriority: revision.QuotaExceededPriority, WorkingPriority: revision.WorkingPriority, EmptyQuotaPriority: revision.EmptyQuotaPriority, InitialPriority: revision.InitialPriority, ErrorPriority: revision.ErrorPriority, FailureThreshold: revision.FailureThreshold, FailureWindowSeconds: revision.FailureWindowSeconds, FallbackCooldownSeconds: revision.FallbackCooldownSeconds, ManualOverridePauseSeconds: revision.ManualOverridePauseSeconds, QuotaInventoryEnabled: revision.QuotaInventoryEnabled, InitialScanEnabled: revision.InitialScanEnabled, RollingRefreshEnabled: revision.RollingRefreshEnabled, RollingRefreshAfterSeconds: revision.RollingRefreshAfterSeconds}
}

func buildActionRecordResponse(action SidecarWatchdogAction, policies ...SidecarWatchdogPolicy) actionRecordResponse {
	policy := SidecarWatchdogPolicy{}
	if len(policies) > 0 {
		policy = policies[0]
	}
	actionType := publicWatchdogActionType(action)
	return actionRecordResponse{ID: action.ID, SidecarID: action.SidecarID, AuthSnapshotID: action.AuthSnapshotID, AuthID: action.AuthID, AuthIndex: action.AuthIndex, Provider: action.Provider, HoldID: action.HoldID, ActionType: actionType, Status: action.Status, Reason: publicWatchdogActionReason(actionType, action), PreviousPriority: action.PreviousPriority, PreviousPriorityState: derivePriorityState(policy, action.PreviousPriority), TargetPriority: action.TargetPriority, TargetPriorityState: derivePriorityState(policy, action.TargetPriority), PriorityState: actionHistoryPriorityState(policy, action), MutationOutcome: watchdogActionMutationOutcome(actionType, action), HoldUntil: action.HoldUntil, ErrorMessage: publicWatchdogActionErrorMessage(actionType, action), CreatedAt: action.CreatedAt, UpdatedAt: action.UpdatedAt, CompletedAt: action.CompletedAt}
}

func actionHistoryPriorityState(policy SidecarWatchdogPolicy, action SidecarWatchdogAction) string {
	if action.TargetPriority != nil {
		return derivePriorityState(policy, action.TargetPriority)
	}
	return derivePriorityState(policy, action.PreviousPriority)
}
