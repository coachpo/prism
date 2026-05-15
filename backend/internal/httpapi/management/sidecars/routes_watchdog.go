package sidecars

import (
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
	SweepID            string     `json:"sweep_id"`
	Status             string     `json:"status"`
	PolicyRevisionID   int64      `json:"policy_revision_id"`
	StartedAt          time.Time  `json:"started_at"`
	NextBatchAfter     *time.Time `json:"next_batch_after"`
	RestartRequestedAt *time.Time `json:"restart_requested_at"`
	NextItemIndex      int        `json:"next_item_index"`
	TotalItems         int        `json:"total_items"`
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
	writeJSON(w, http.StatusOK, buildWatchdogPolicyResponse(state))
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
	if requestBody.ExpectedRevisionID == nil || *requestBody.ExpectedRevisionID <= 0 {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "expected_revision_id is required")
		return
	}
	state, err := s.getWatchdogPolicyRevisionState(r.Context(), id)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	base := state.ActiveRevision
	if state.PendingRevision != nil {
		base = state.PendingRevision
	}
	if base == nil {
		writeDomainError(w, r, s.corsSnapshot(), invalidInputError("active watchdog policy revision not found"))
		return
	}
	input := watchdogPolicyRevisionInputFromRevision(*base)
	if err := applyWatchdogPolicyUpdateRequest(&input, requestBody); err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	updated, err := s.savePendingWatchdogPolicyRevision(r.Context(), input, requestBody.ExpectedRevisionID)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	s.recordAction(r.Context(), id, "watchdog-policy.save", "succeeded", nil)
	writeJSON(w, http.StatusOK, buildWatchdogPolicyResponse(updated))
}

func (s *Service) handleApplyWatchdogPolicy(w http.ResponseWriter, r *http.Request) {
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
	state, err := s.applyWatchdogPolicyRevision(r.Context(), id, *requestBody.TargetRevisionID, *requestBody.ExpectedRevisionID)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	s.recordAction(r.Context(), id, "watchdog-policy.apply", "succeeded", nil)
	writeJSON(w, http.StatusOK, buildWatchdogPolicyResponse(state))
}

func applyWatchdogPolicyUpdateRequest(input *SidecarWatchdogPolicyRevisionInput, requestBody watchdogPolicyUpdateRequest) error {
	if requestBody.Enabled != nil {
		input.Enabled = *requestBody.Enabled
	}
	if requestBody.WatchdogSweepIntervalSeconds != nil {
		if *requestBody.WatchdogSweepIntervalSeconds < 1 {
			return invalidInputError("watchdog_sweep_interval_seconds must be >= 1")
		}
		input.WatchdogSweepIntervalSeconds = *requestBody.WatchdogSweepIntervalSeconds
	}
	if requestBody.FailureThreshold != nil {
		if *requestBody.FailureThreshold < 1 {
			return invalidInputError("failure_threshold must be >= 1")
		}
		input.FailureThreshold = *requestBody.FailureThreshold
	}
	if requestBody.FailureWindowSeconds != nil {
		if *requestBody.FailureWindowSeconds < 1 {
			return invalidInputError("failure_window_seconds must be >= 1")
		}
		input.FailureWindowSeconds = *requestBody.FailureWindowSeconds
	}
	if requestBody.FallbackCooldownSeconds != nil {
		if *requestBody.FallbackCooldownSeconds < 1 {
			return invalidInputError("fallback_cooldown_seconds must be >= 1")
		}
		input.FallbackCooldownSeconds = *requestBody.FallbackCooldownSeconds
	}
	if requestBody.UsingPriority != nil {
		if *requestBody.UsingPriority < 0 {
			return invalidInputError("using_priority must be >= 0")
		}
		input.UsingPriority = *requestBody.UsingPriority
	}
	if requestBody.WorkingPriority != nil {
		if *requestBody.WorkingPriority < 1 {
			return invalidInputError("working_priority must be >= 1")
		}
		input.WorkingPriority = *requestBody.WorkingPriority
	}
	if requestBody.QuotaExceededPriority != nil {
		if *requestBody.QuotaExceededPriority < 0 {
			return invalidInputError("quota_exceeded_priority must be >= 0")
		}
		input.QuotaExceededPriority = *requestBody.QuotaExceededPriority
	}
	if requestBody.EmptyQuotaPriority != nil {
		if *requestBody.EmptyQuotaPriority < 1 {
			return invalidInputError("empty_quota_priority must be >= 1")
		}
		input.EmptyQuotaPriority = *requestBody.EmptyQuotaPriority
	}
	if requestBody.InitialPriority != nil {
		if *requestBody.InitialPriority < 1 {
			return invalidInputError("initial_priority must be >= 1")
		}
		input.InitialPriority = *requestBody.InitialPriority
	}
	if requestBody.ErrorPriority != nil {
		if *requestBody.ErrorPriority < 1 {
			return invalidInputError("error_priority must be >= 1")
		}
		input.ErrorPriority = *requestBody.ErrorPriority
	}
	if requestBody.ManualOverridePauseSeconds != nil {
		if *requestBody.ManualOverridePauseSeconds < 1 {
			return invalidInputError("manual_override_pause_seconds must be >= 1")
		}
		input.ManualOverridePauseSeconds = *requestBody.ManualOverridePauseSeconds
	}
	if requestBody.ProbeConcurrency != nil {
		if *requestBody.ProbeConcurrency < 1 {
			return invalidInputError("probe_concurrency must be >= 1")
		}
		input.ProbeConcurrency = *requestBody.ProbeConcurrency
	}
	if requestBody.ProbeTimeoutSeconds != nil {
		if *requestBody.ProbeTimeoutSeconds < 1 {
			return invalidInputError("probe_timeout_seconds must be >= 1")
		}
		input.ProbeTimeoutSeconds = *requestBody.ProbeTimeoutSeconds
	}
	if requestBody.ProbeBatchCooldownSeconds != nil {
		if *requestBody.ProbeBatchCooldownSeconds < 1 {
			return invalidInputError("probe_batch_cooldown_seconds must be >= 1")
		}
		input.ProbeBatchCooldownSeconds = *requestBody.ProbeBatchCooldownSeconds
	}
	if requestBody.ProbeJitterMinMS != nil {
		if *requestBody.ProbeJitterMinMS < 0 {
			return invalidInputError("probe_jitter_min_ms must be >= 0")
		}
		input.ProbeJitterMinMS = *requestBody.ProbeJitterMinMS
	}
	if requestBody.ProbeJitterMaxMS != nil {
		if *requestBody.ProbeJitterMaxMS < 0 {
			return invalidInputError("probe_jitter_max_ms must be >= 0")
		}
		input.ProbeJitterMaxMS = *requestBody.ProbeJitterMaxMS
	}
	if requestBody.CooldownJitterPercent != nil {
		if *requestBody.CooldownJitterPercent < 0 || *requestBody.CooldownJitterPercent > 100 {
			return invalidInputError("cooldown_jitter_percent must be between 0 and 100")
		}
		input.CooldownJitterPercent = *requestBody.CooldownJitterPercent
	}
	if requestBody.QuotaInventoryEnabled != nil {
		input.QuotaInventoryEnabled = *requestBody.QuotaInventoryEnabled
	}
	if requestBody.InitialScanEnabled != nil {
		input.InitialScanEnabled = *requestBody.InitialScanEnabled
	}
	if requestBody.RollingRefreshEnabled != nil {
		input.RollingRefreshEnabled = *requestBody.RollingRefreshEnabled
	}
	if requestBody.RollingRefreshAfterSeconds != nil {
		if *requestBody.RollingRefreshAfterSeconds < 1 {
			return invalidInputError("rolling_refresh_after_seconds must be >= 1")
		}
		input.RollingRefreshAfterSeconds = *requestBody.RollingRefreshAfterSeconds
	}
	return validateWatchdogPolicyRevisionInput(*input)
}

func validateWatchdogPolicyRevisionInput(input SidecarWatchdogPolicyRevisionInput) error {
	if input.WatchdogSweepIntervalSeconds < 1 {
		return invalidInputError("watchdog_sweep_interval_seconds must be >= 1")
	}
	if input.QuotaExceededPriority < 0 || input.UsingPriority < 0 {
		return invalidInputError("legacy watchdog priorities must be non-negative")
	}
	if input.QuotaExceededPriority > input.UsingPriority {
		return invalidInputError("quota_exceeded_priority must be <= using_priority")
	}
	if input.WorkingPriority < 1 || input.EmptyQuotaPriority < 1 || input.InitialPriority < 1 || input.ErrorPriority < 1 {
		return invalidInputError("watchdog priority bands must be >= 1")
	}
	if input.WorkingPriority < input.EmptyQuotaPriority || input.EmptyQuotaPriority < input.InitialPriority || input.InitialPriority < input.ErrorPriority {
		return invalidInputError("watchdog priority bands must satisfy working_priority >= empty_quota_priority >= initial_priority >= error_priority")
	}
	if input.ProbeConcurrency < 1 {
		return invalidInputError("probe_concurrency must be >= 1")
	}
	if input.ProbeTimeoutSeconds < 1 {
		return invalidInputError("probe_timeout_seconds must be >= 1")
	}
	if input.ProbeBatchCooldownSeconds < 1 {
		return invalidInputError("probe_batch_cooldown_seconds must be >= 1")
	}
	if input.ProbeJitterMinMS < 0 {
		return invalidInputError("probe_jitter_min_ms must be >= 0")
	}
	if input.ProbeJitterMaxMS < 0 {
		return invalidInputError("probe_jitter_max_ms must be >= 0")
	}
	if input.ProbeJitterMaxMS < input.ProbeJitterMinMS {
		return invalidInputError("probe_jitter_max_ms must be >= probe_jitter_min_ms")
	}
	if input.CooldownJitterPercent < 0 || input.CooldownJitterPercent > 100 {
		return invalidInputError("cooldown_jitter_percent must be between 0 and 100")
	}
	if input.RollingRefreshAfterSeconds < 1 {
		return invalidInputError("rolling_refresh_after_seconds must be >= 1")
	}
	return validateWatchdogProbeRuntimePolicy(SidecarWatchdogPolicy{ProbeConcurrency: input.ProbeConcurrency, ProbeTimeoutSeconds: input.ProbeTimeoutSeconds, ProbeJitterMinMS: input.ProbeJitterMinMS, ProbeJitterMaxMS: input.ProbeJitterMaxMS})
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

func buildWatchdogPolicyResponse(state SidecarWatchdogPolicyRevisionState) watchdogPolicyResponse {
	policy := state.Policy
	active := buildWatchdogPolicyRevisionResponse(state.ActiveRevision)
	pending := buildWatchdogPolicyRevisionResponse(state.PendingRevision)
	response := watchdogPolicyResponse{ID: policy.ID, SidecarID: policy.SidecarID, ActiveRevisionID: cloneInt64Ptr(policy.ActiveRevisionID), PendingRevisionID: cloneInt64Ptr(policy.PendingRevisionID), HasPendingChanges: state.HasPendingChanges, ActiveRevision: active, PendingRevision: pending, ActiveSweep: buildWatchdogActiveSweepResponse(state.ActiveSweep, policy), CreatedAt: policy.CreatedAt, UpdatedAt: policy.UpdatedAt}
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
	return response
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

func buildWatchdogActiveSweepResponse(sweep *SidecarWatchdogSweep, policy SidecarWatchdogPolicy) *watchdogActiveSweepResponse {
	if sweep == nil {
		return nil
	}
	totalItems := 0
	if items, err := decodeWatchdogSweepSnapshot(sweep.SnapshotJSON); err == nil {
		totalItems = len(items)
	}
	var restartRequestedAt *time.Time
	if policy.ActiveRevisionID != nil && *policy.ActiveRevisionID != sweep.PolicyRevisionID {
		restartedAt := policy.UpdatedAt
		restartRequestedAt = &restartedAt
	}
	return &watchdogActiveSweepResponse{SweepID: sweep.SweepID, Status: sweep.Status, PolicyRevisionID: sweep.PolicyRevisionID, StartedAt: sweep.StartedAt, NextBatchAfter: cloneTimePtr(sweep.NextBatchAfter), RestartRequestedAt: restartRequestedAt, NextItemIndex: sweep.NextItemIndex, TotalItems: totalItems}
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
