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
	DeprioritizedPriority      *int  `json:"deprioritized_priority"`
	PrioritizedPriority        *int  `json:"prioritized_priority"`
	ManualOverridePauseSeconds *int  `json:"manual_override_pause_seconds"`
	ProbeBatchSize             *int  `json:"probe_batch_size"`
	ProbeTimeoutSeconds        *int  `json:"probe_timeout_seconds"`
}

type watchdogPolicyResponse struct {
	ID                         int       `json:"id"`
	SidecarID                  int       `json:"sidecar_id"`
	Enabled                    bool      `json:"enabled"`
	FailureThreshold           int       `json:"failure_threshold"`
	FailureWindowSeconds       int       `json:"failure_window_seconds"`
	FallbackCooldownSeconds    int       `json:"fallback_cooldown_seconds"`
	DeprioritizedPriority      int       `json:"deprioritized_priority"`
	PrioritizedPriority        int       `json:"prioritized_priority"`
	ManualOverridePauseSeconds int       `json:"manual_override_pause_seconds"`
	ProbeBatchSize             int       `json:"probe_batch_size"`
	ProbeTimeoutSeconds        int       `json:"probe_timeout_seconds"`
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
	input := SidecarWatchdogPolicyInput{SidecarID: id, Enabled: current.Enabled, FailureThreshold: current.FailureThreshold, FailureWindowSeconds: current.FailureWindowSeconds, FallbackCooldownSeconds: current.FallbackCooldownSeconds, DeprioritizedPriority: current.DeprioritizedPriority, PrioritizedPriority: current.PrioritizedPriority, ManualOverridePauseSeconds: current.ManualOverridePauseSeconds, ProbeBatchSize: current.ProbeBatchSize, ProbeTimeoutSeconds: current.ProbeTimeoutSeconds}
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
	if requestBody.DeprioritizedPriority != nil {
		if *requestBody.DeprioritizedPriority < 0 {
			writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "deprioritized_priority must be >= 0")
			return
		}
		input.DeprioritizedPriority = *requestBody.DeprioritizedPriority
	}
	if requestBody.PrioritizedPriority != nil {
		if *requestBody.PrioritizedPriority < 0 {
			writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "prioritized_priority must be >= 0")
			return
		}
		input.PrioritizedPriority = *requestBody.PrioritizedPriority
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
	if input.DeprioritizedPriority < 0 || input.PrioritizedPriority < 0 {
		return invalidInputError("watchdog priorities must be non-negative")
	}
	if input.DeprioritizedPriority >= input.PrioritizedPriority {
		return invalidInputError("deprioritized_priority must be less than prioritized_priority")
	}
	if input.ProbeBatchSize < 1 {
		return invalidInputError("probe_batch_size must be >= 1")
	}
	if input.ProbeTimeoutSeconds < 1 {
		return invalidInputError("probe_timeout_seconds must be >= 1")
	}
	return validateWatchdogProbeRuntimePolicy(SidecarWatchdogPolicy{ProbeBatchSize: input.ProbeBatchSize, ProbeTimeoutSeconds: input.ProbeTimeoutSeconds})
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
	return watchdogPolicyResponse{ID: policy.ID, SidecarID: policy.SidecarID, Enabled: policy.Enabled, FailureThreshold: policy.FailureThreshold, FailureWindowSeconds: policy.FailureWindowSeconds, FallbackCooldownSeconds: policy.FallbackCooldownSeconds, DeprioritizedPriority: policy.DeprioritizedPriority, PrioritizedPriority: policy.PrioritizedPriority, ManualOverridePauseSeconds: policy.ManualOverridePauseSeconds, ProbeBatchSize: policy.ProbeBatchSize, ProbeTimeoutSeconds: policy.ProbeTimeoutSeconds, CreatedAt: policy.CreatedAt, UpdatedAt: policy.UpdatedAt}
}

func buildActionRecordResponse(action SidecarWatchdogAction) actionRecordResponse {
	actionType := publicWatchdogActionType(action)
	return actionRecordResponse{ID: action.ID, SidecarID: action.SidecarID, AuthSnapshotID: action.AuthSnapshotID, AuthID: action.AuthID, AuthIndex: action.AuthIndex, Provider: action.Provider, HoldID: action.HoldID, ActionType: actionType, Status: action.Status, Reason: publicWatchdogActionReason(actionType, action), PreviousPriority: action.PreviousPriority, TargetPriority: action.TargetPriority, HoldUntil: action.HoldUntil, ErrorMessage: publicWatchdogActionErrorMessage(actionType, action), CreatedAt: action.CreatedAt, UpdatedAt: action.UpdatedAt, CompletedAt: action.CompletedAt}
}
