package sidecars

import (
	"net/http"
	"time"
)

type watchdogPolicyUpdateRequest struct {
	Enabled                    *bool `json:"enabled"`
	FailureThreshold           *int  `json:"failure_threshold"`
	FailureWindowSeconds       *int  `json:"failure_window_seconds"`
	FallbackCooldownSeconds    *int  `json:"fallback_cooldown_seconds"`
	UsingPriority              *int  `json:"using_priority"`
	QuotaExceededPriority      *int  `json:"quota_exceeded_priority"`
	ErrorPriority              *int  `json:"error_priority"`
	ManualOverridePauseSeconds *int  `json:"manual_override_pause_seconds"`
	ProbeBatchSize             *int  `json:"probe_batch_size"`
	ProbeTimeoutSeconds        *int  `json:"probe_timeout_seconds"`
	ProbeBatchCooldownSeconds  *int  `json:"probe_batch_cooldown_seconds"`
	ProbeJitterMinMS           *int  `json:"probe_jitter_min_ms"`
	ProbeJitterMaxMS           *int  `json:"probe_jitter_max_ms"`
	CooldownJitterPercent      *int  `json:"cooldown_jitter_percent"`
	QuotaInventoryEnabled      *bool `json:"quota_inventory_enabled"`
	InitialScanEnabled         *bool `json:"initial_scan_enabled"`
	RollingRefreshEnabled      *bool `json:"rolling_refresh_enabled"`
	RollingRefreshAfterSeconds *int  `json:"rolling_refresh_after_seconds"`
}

type watchdogPolicyResponse struct {
	ID                         int       `json:"id"`
	SidecarID                  int       `json:"sidecar_id"`
	Enabled                    bool      `json:"enabled"`
	FailureThreshold           int       `json:"failure_threshold"`
	FailureWindowSeconds       int       `json:"failure_window_seconds"`
	FallbackCooldownSeconds    int       `json:"fallback_cooldown_seconds"`
	UsingPriority              int       `json:"using_priority"`
	QuotaExceededPriority      int       `json:"quota_exceeded_priority"`
	ErrorPriority              int       `json:"error_priority"`
	ManualOverridePauseSeconds int       `json:"manual_override_pause_seconds"`
	ProbeBatchSize             int       `json:"probe_batch_size"`
	ProbeTimeoutSeconds        int       `json:"probe_timeout_seconds"`
	ProbeBatchCooldownSeconds  int       `json:"probe_batch_cooldown_seconds"`
	ProbeJitterMinMS           int       `json:"probe_jitter_min_ms"`
	ProbeJitterMaxMS           int       `json:"probe_jitter_max_ms"`
	CooldownJitterPercent      int       `json:"cooldown_jitter_percent"`
	QuotaInventoryEnabled      bool      `json:"quota_inventory_enabled"`
	InitialScanEnabled         bool      `json:"initial_scan_enabled"`
	RollingRefreshEnabled      bool      `json:"rolling_refresh_enabled"`
	RollingRefreshAfterSeconds int       `json:"rolling_refresh_after_seconds"`
	CreatedAt                  time.Time `json:"created_at"`
	UpdatedAt                  time.Time `json:"updated_at"`
}

type actionHistoryListResponse struct {
	Items []actionRecordResponse `json:"items"`
}

type actionRecordResponse struct {
	ID               int        `json:"id"`
	SidecarID        int        `json:"sidecar_id"`
	AuthSnapshotID   *int       `json:"auth_snapshot_id,omitempty"`
	AuthID           *string    `json:"auth_id,omitempty"`
	AuthIndex        *string    `json:"auth_index,omitempty"`
	Provider         *string    `json:"provider,omitempty"`
	HoldID           *int       `json:"hold_id,omitempty"`
	ActionType       string     `json:"action_type"`
	Status           string     `json:"status"`
	Reason           *string    `json:"reason,omitempty"`
	PreviousPriority *int       `json:"previous_priority,omitempty"`
	TargetPriority   *int       `json:"target_priority,omitempty"`
	HoldUntil        *time.Time `json:"hold_until,omitempty"`
	ErrorMessage     *string    `json:"error_message,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	CompletedAt      *time.Time `json:"completed_at,omitempty"`
}

func (s *Service) handleGetWatchdogPolicy(w http.ResponseWriter, r *http.Request) {
	id, ok := s.parseSidecarID(w, r)
	if !ok || !s.ensureSidecarExists(w, r, id) {
		return
	}
	policy, err := s.store.GetOrCreateWatchdogPolicy(r.Context(), id)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeJSON(w, http.StatusOK, buildWatchdogPolicyResponse(policy))
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
	current, err := s.store.GetOrCreateWatchdogPolicy(r.Context(), id)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	probeBatchCooldown := current.ProbeBatchCooldownSeconds
	probeJitterMinMS := current.ProbeJitterMinMS
	probeJitterMaxMS := current.ProbeJitterMaxMS
	cooldownJitterPercent := current.CooldownJitterPercent
	quotaInventoryEnabled := current.QuotaInventoryEnabled
	initialScanEnabled := current.InitialScanEnabled
	rollingRefreshEnabled := current.RollingRefreshEnabled
	rollingRefreshAfterSeconds := current.RollingRefreshAfterSeconds
	input := SidecarWatchdogPolicyInput{SidecarID: id, Enabled: current.Enabled, FailureThreshold: current.FailureThreshold, FailureWindowSeconds: current.FailureWindowSeconds, FallbackCooldownSeconds: current.FallbackCooldownSeconds, QuotaExceededPriority: current.QuotaExceededPriority, UsingPriority: current.UsingPriority, ErrorPriority: current.ErrorPriority, ManualOverridePauseSeconds: current.ManualOverridePauseSeconds, ProbeBatchSize: current.ProbeBatchSize, ProbeTimeoutSeconds: current.ProbeTimeoutSeconds, ProbeBatchCooldownSeconds: &probeBatchCooldown, ProbeJitterMinMS: &probeJitterMinMS, ProbeJitterMaxMS: &probeJitterMaxMS, CooldownJitterPercent: &cooldownJitterPercent, QuotaInventoryEnabled: &quotaInventoryEnabled, InitialScanEnabled: &initialScanEnabled, RollingRefreshEnabled: &rollingRefreshEnabled, RollingRefreshAfterSeconds: &rollingRefreshAfterSeconds}
	if requestBody.Enabled != nil {
		input.Enabled = *requestBody.Enabled
	}
	if requestBody.FailureThreshold != nil {
		if *requestBody.FailureThreshold < 1 {
			writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "failure_threshold must be >= 1")
			return
		}
		input.FailureThreshold = *requestBody.FailureThreshold
	}
	if requestBody.FailureWindowSeconds != nil {
		if *requestBody.FailureWindowSeconds < 1 {
			writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "failure_window_seconds must be >= 1")
			return
		}
		input.FailureWindowSeconds = *requestBody.FailureWindowSeconds
	}
	if requestBody.FallbackCooldownSeconds != nil {
		if *requestBody.FallbackCooldownSeconds < 1 {
			writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "fallback_cooldown_seconds must be >= 1")
			return
		}
		input.FallbackCooldownSeconds = *requestBody.FallbackCooldownSeconds
	}
	if requestBody.QuotaExceededPriority != nil {
		if *requestBody.QuotaExceededPriority < 0 {
			writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "quota_exceeded_priority must be >= 0")
			return
		}
		input.QuotaExceededPriority = *requestBody.QuotaExceededPriority
	}
	if requestBody.UsingPriority != nil {
		if *requestBody.UsingPriority < 0 {
			writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "using_priority must be >= 0")
			return
		}
		input.UsingPriority = *requestBody.UsingPriority
	}
	if requestBody.ErrorPriority != nil {
		if *requestBody.ErrorPriority < 0 {
			writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "error_priority must be >= 0")
			return
		}
		input.ErrorPriority = *requestBody.ErrorPriority
	}
	if requestBody.ManualOverridePauseSeconds != nil {
		if *requestBody.ManualOverridePauseSeconds < 1 {
			writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "manual_override_pause_seconds must be >= 1")
			return
		}
		input.ManualOverridePauseSeconds = *requestBody.ManualOverridePauseSeconds
	}
	if requestBody.ProbeBatchSize != nil {
		if *requestBody.ProbeBatchSize < 1 {
			writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "probe_batch_size must be >= 1")
			return
		}
		input.ProbeBatchSize = *requestBody.ProbeBatchSize
	}
	if requestBody.ProbeTimeoutSeconds != nil {
		if *requestBody.ProbeTimeoutSeconds < 1 {
			writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "probe_timeout_seconds must be >= 1")
			return
		}
		input.ProbeTimeoutSeconds = *requestBody.ProbeTimeoutSeconds
	}
	if requestBody.ProbeBatchCooldownSeconds != nil {
		if *requestBody.ProbeBatchCooldownSeconds < 1 {
			writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "probe_batch_cooldown_seconds must be >= 1")
			return
		}
		input.ProbeBatchCooldownSeconds = requestBody.ProbeBatchCooldownSeconds
	}
	if requestBody.ProbeJitterMinMS != nil {
		if *requestBody.ProbeJitterMinMS < 0 {
			writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "probe_jitter_min_ms must be >= 0")
			return
		}
		input.ProbeJitterMinMS = requestBody.ProbeJitterMinMS
	}
	if requestBody.ProbeJitterMaxMS != nil {
		if *requestBody.ProbeJitterMaxMS < 0 {
			writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "probe_jitter_max_ms must be >= 0")
			return
		}
		input.ProbeJitterMaxMS = requestBody.ProbeJitterMaxMS
	}
	if requestBody.CooldownJitterPercent != nil {
		if *requestBody.CooldownJitterPercent < 0 || *requestBody.CooldownJitterPercent > 100 {
			writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "cooldown_jitter_percent must be between 0 and 100")
			return
		}
		input.CooldownJitterPercent = requestBody.CooldownJitterPercent
	}
	if requestBody.QuotaInventoryEnabled != nil {
		input.QuotaInventoryEnabled = requestBody.QuotaInventoryEnabled
	}
	if requestBody.InitialScanEnabled != nil {
		input.InitialScanEnabled = requestBody.InitialScanEnabled
	}
	if requestBody.RollingRefreshEnabled != nil {
		input.RollingRefreshEnabled = requestBody.RollingRefreshEnabled
	}
	if requestBody.RollingRefreshAfterSeconds != nil {
		if *requestBody.RollingRefreshAfterSeconds < 1 {
			writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "rolling_refresh_after_seconds must be >= 1")
			return
		}
		input.RollingRefreshAfterSeconds = requestBody.RollingRefreshAfterSeconds
	}
	if err := validateWatchdogPolicyUpdateInput(input); err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	updated, err := s.store.UpsertWatchdogPolicy(r.Context(), input)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	s.recordAction(r.Context(), id, "watchdog-policy.update", "succeeded", nil)
	writeJSON(w, http.StatusOK, buildWatchdogPolicyResponse(updated))
}

func validateWatchdogPolicyUpdateInput(input SidecarWatchdogPolicyInput) error {
	if input.QuotaExceededPriority < 0 || input.UsingPriority < 0 || input.ErrorPriority < 0 {
		return invalidInputError("watchdog priorities must be non-negative")
	}
	if input.QuotaExceededPriority > input.UsingPriority {
		return invalidInputError("quota_exceeded_priority must be <= using_priority")
	}
	if input.ErrorPriority > input.UsingPriority {
		return invalidInputError("error_priority must be <= using_priority")
	}
	if input.ProbeBatchSize < 1 {
		return invalidInputError("probe_batch_size must be >= 1")
	}
	if input.ProbeTimeoutSeconds < 1 {
		return invalidInputError("probe_timeout_seconds must be >= 1")
	}
	if input.ProbeBatchCooldownSeconds != nil && *input.ProbeBatchCooldownSeconds < 1 {
		return invalidInputError("probe_batch_cooldown_seconds must be >= 1")
	}
	if input.ProbeJitterMinMS != nil && *input.ProbeJitterMinMS < 0 {
		return invalidInputError("probe_jitter_min_ms must be >= 0")
	}
	if input.ProbeJitterMaxMS != nil && *input.ProbeJitterMaxMS < 0 {
		return invalidInputError("probe_jitter_max_ms must be >= 0")
	}
	if input.ProbeJitterMinMS != nil && input.ProbeJitterMaxMS != nil && *input.ProbeJitterMaxMS < *input.ProbeJitterMinMS {
		return invalidInputError("probe_jitter_max_ms must be >= probe_jitter_min_ms")
	}
	if input.CooldownJitterPercent != nil && (*input.CooldownJitterPercent < 0 || *input.CooldownJitterPercent > 100) {
		return invalidInputError("cooldown_jitter_percent must be between 0 and 100")
	}
	if input.RollingRefreshAfterSeconds != nil && *input.RollingRefreshAfterSeconds < 1 {
		return invalidInputError("rolling_refresh_after_seconds must be >= 1")
	}
	return validateWatchdogProbeRuntimePolicy(SidecarWatchdogPolicy{ProbeBatchSize: input.ProbeBatchSize, ProbeTimeoutSeconds: input.ProbeTimeoutSeconds, ProbeJitterMinMS: intValue(input.ProbeJitterMinMS), ProbeJitterMaxMS: intValue(input.ProbeJitterMaxMS)})
}

func intValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func (s *Service) handleListActionHistory(w http.ResponseWriter, r *http.Request) {
	id, ok := s.parseSidecarID(w, r)
	if !ok || !s.ensureSidecarExists(w, r, id) {
		return
	}
	var actions []SidecarWatchdogAction
	if lister, ok := s.store.(actionHistoryPersistence); ok {
		var err error
		actions, err = lister.ListWatchdogActions(r.Context(), id)
		if err != nil {
			writeDomainError(w, r, s.corsSnapshot(), err)
			return
		}
	}
	items := make([]actionRecordResponse, 0, len(actions))
	for _, action := range actions {
		items = append(items, buildActionRecordResponse(action))
	}
	writeJSON(w, http.StatusOK, actionHistoryListResponse{Items: items})
}

func buildWatchdogPolicyResponse(policy SidecarWatchdogPolicy) watchdogPolicyResponse {
	return watchdogPolicyResponse{ID: policy.ID, SidecarID: policy.SidecarID, Enabled: policy.Enabled, FailureThreshold: policy.FailureThreshold, FailureWindowSeconds: policy.FailureWindowSeconds, FallbackCooldownSeconds: policy.FallbackCooldownSeconds, UsingPriority: policy.UsingPriority, QuotaExceededPriority: policy.QuotaExceededPriority, ErrorPriority: policy.ErrorPriority, ManualOverridePauseSeconds: policy.ManualOverridePauseSeconds, ProbeBatchSize: policy.ProbeBatchSize, ProbeTimeoutSeconds: policy.ProbeTimeoutSeconds, ProbeBatchCooldownSeconds: policy.ProbeBatchCooldownSeconds, ProbeJitterMinMS: policy.ProbeJitterMinMS, ProbeJitterMaxMS: policy.ProbeJitterMaxMS, CooldownJitterPercent: policy.CooldownJitterPercent, QuotaInventoryEnabled: policy.QuotaInventoryEnabled, InitialScanEnabled: policy.InitialScanEnabled, RollingRefreshEnabled: policy.RollingRefreshEnabled, RollingRefreshAfterSeconds: policy.RollingRefreshAfterSeconds, CreatedAt: policy.CreatedAt, UpdatedAt: policy.UpdatedAt}
}

func buildActionRecordResponse(action SidecarWatchdogAction) actionRecordResponse {
	actionType := publicWatchdogActionType(action)
	return actionRecordResponse{ID: action.ID, SidecarID: action.SidecarID, AuthSnapshotID: action.AuthSnapshotID, AuthID: action.AuthID, AuthIndex: action.AuthIndex, Provider: action.Provider, HoldID: action.HoldID, ActionType: actionType, Status: action.Status, Reason: publicWatchdogActionReason(actionType, action), PreviousPriority: action.PreviousPriority, TargetPriority: action.TargetPriority, HoldUntil: action.HoldUntil, ErrorMessage: publicWatchdogActionErrorMessage(actionType, action), CreatedAt: action.CreatedAt, UpdatedAt: action.UpdatedAt, CompletedAt: action.CompletedAt}
}
