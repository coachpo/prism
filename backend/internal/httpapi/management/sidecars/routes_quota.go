package sidecars

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

type quotaScanCreateRequest struct {
	RequestedBy   *string `json:"requested_by"`
	ReplaceActive *bool   `json:"replace_active"`
}

type quotaStateListResponse struct {
	Items []quotaStateResponse `json:"items"`
}

type quotaStateResponse struct {
	SidecarID        int        `json:"sidecar_id"`
	AuthID           string     `json:"auth_id"`
	AuthName         *string    `json:"auth_name,omitempty"`
	Provider         *string    `json:"provider,omitempty"`
	AuthIndexPresent bool       `json:"auth_index_present"`
	Disabled         bool       `json:"disabled"`
	CurrentPriority  *int       `json:"current_priority,omitempty"`
	QuotaBand        string     `json:"quota_band"`
	ProbeStatus      *string    `json:"probe_status,omitempty"`
	ReasonCode       *string    `json:"reason_code,omitempty"`
	QuotaResetAt     *time.Time `json:"quota_reset_at,omitempty"`
	BlockingWindow   *string    `json:"blocking_window,omitempty"`
	LastSnapshotAt   *time.Time `json:"last_snapshot_at,omitempty"`
	LastProbedAt     *time.Time `json:"last_probed_at,omitempty"`
	LastErrorCode    *string    `json:"last_error_code,omitempty"`
	ActiveHold       bool       `json:"active_hold"`
}

type quotaScanListResponse struct {
	Items []quotaScanResponse `json:"items"`
}

type quotaScanResponse struct {
	ID                 int        `json:"id"`
	SidecarID          int        `json:"sidecar_id"`
	ScanType           string     `json:"scan_type"`
	Status             string     `json:"status"`
	RequestedBy        *string    `json:"requested_by,omitempty"`
	PlannedCount       int        `json:"planned_count"`
	AttemptedCount     int        `json:"attempted_count"`
	UsingCount         int        `json:"using_count"`
	QuotaExceededCount int        `json:"quota_exceeded_count"`
	ErrorCount         int        `json:"error_count"`
	SkippedCount       int        `json:"skipped_count"`
	CancelRequestedAt  *time.Time `json:"cancel_requested_at,omitempty"`
	StartedAt          *time.Time `json:"started_at,omitempty"`
	CompletedAt        *time.Time `json:"completed_at,omitempty"`
	LastErrorCode      *string    `json:"last_error_code,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

func (s *Service) handleListQuotaStates(w http.ResponseWriter, r *http.Request) {
	id, ok := s.parseSidecarID(w, r)
	if !ok || !s.ensureSidecarExists(w, r, id) {
		return
	}
	states, err := s.store.ListAuthQuotaStates(r.Context(), id)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	snapshots, err := s.store.ListAuthSnapshots(r.Context(), id)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	holds, err := s.store.ListActiveWatchdogHolds(r.Context(), id)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	snapshotByAuth := make(map[string]SidecarAuthSnapshot, len(snapshots))
	for _, snapshot := range snapshots {
		snapshotByAuth[snapshot.AuthID] = snapshot
	}
	activeHoldAuths := watchdogActiveHoldAuthSet(holds)
	items := make([]quotaStateResponse, 0, len(states))
	for _, state := range states {
		snapshot, ok := snapshotByAuth[state.AuthID]
		var snapshotPtr *SidecarAuthSnapshot
		if ok {
			snapshotCopy := snapshot
			snapshotPtr = &snapshotCopy
		}
		items = append(items, buildQuotaStateResponse(id, state, snapshotPtr, hasAuth(activeHoldAuths, state.AuthID)))
	}
	writeJSON(w, http.StatusOK, quotaStateListResponse{Items: items})
}

func (s *Service) handleCreateQuotaScan(w http.ResponseWriter, r *http.Request) {
	id, ok := s.parseSidecarID(w, r)
	if !ok || !s.ensureSidecarExists(w, r, id) {
		return
	}
	var requestBody quotaScanCreateRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	replaceActive := boolValue(requestBody.ReplaceActive, false)
	requestedBy := trimmedStringPtr(requestBody.RequestedBy)
	if !replaceActive {
		runs, err := s.store.ListQuotaScanRuns(r.Context(), id)
		if err != nil {
			writeDomainError(w, r, s.corsSnapshot(), err)
			return
		}
		if _, active := activeQuotaScanRun(runs); active {
			writeError(w, r, s.corsSnapshot(), http.StatusConflict, "active quota scan run already exists for sidecar")
			return
		}
	}
	scanRun, err := s.StartManualQuotaScan(r.Context(), id, requestedBy, replaceActive)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeJSON(w, http.StatusAccepted, buildQuotaScanResponse(scanRun))
}

func (s *Service) handleGetCurrentQuotaScan(w http.ResponseWriter, r *http.Request) {
	id, ok := s.parseSidecarID(w, r)
	if !ok || !s.ensureSidecarExists(w, r, id) {
		return
	}
	runs, err := s.store.ListQuotaScanRuns(r.Context(), id)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	run, active := activeQuotaScanRun(runs)
	if !active {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, buildQuotaScanResponse(run))
}

func (s *Service) handleListQuotaScans(w http.ResponseWriter, r *http.Request) {
	id, ok := s.parseSidecarID(w, r)
	if !ok || !s.ensureSidecarExists(w, r, id) {
		return
	}
	runs, err := s.store.ListQuotaScanRuns(r.Context(), id)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	items := make([]quotaScanResponse, 0, len(runs))
	for _, run := range runs {
		items = append(items, buildQuotaScanResponse(run))
	}
	writeJSON(w, http.StatusOK, quotaScanListResponse{Items: items})
}

func (s *Service) handleCancelQuotaScan(w http.ResponseWriter, r *http.Request) {
	id, ok := s.parseSidecarID(w, r)
	if !ok || !s.ensureSidecarExists(w, r, id) {
		return
	}
	scanRunID, ok := s.parseQuotaScanID(w, r)
	if !ok {
		return
	}
	cancelled, err := s.CancelQuotaScanRun(r.Context(), id, scanRunID)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeJSON(w, http.StatusAccepted, buildQuotaScanResponse(cancelled))
}

func (s *Service) parseQuotaScanID(w http.ResponseWriter, r *http.Request) (int, bool) {
	id, err := strconv.Atoi(strings.TrimSpace(chi.URLParam(r, "scan_id")))
	if err != nil || id <= 0 {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "scan_id must be a positive integer")
		return 0, false
	}
	return id, true
}

func buildQuotaStateResponse(sidecarID int, state SidecarAuthQuotaState, snapshot *SidecarAuthSnapshot, holdActive bool) quotaStateResponse {
	disabled := false
	var priority *int
	if snapshot != nil {
		priority = cloneIntPtr(snapshot.Priority)
		if snapshot.Disabled != nil && *snapshot.Disabled {
			disabled = true
		}
	}
	return quotaStateResponse{
		SidecarID:        sidecarID,
		AuthID:           state.AuthID,
		AuthName:         cloneStringPtr(state.AuthName),
		Provider:         cloneStringPtr(state.Provider),
		AuthIndexPresent: strings.TrimSpace(stringValue(state.AuthIndex)) != "",
		Disabled:         disabled,
		CurrentPriority:  priority,
		QuotaBand:        state.QuotaBand,
		ProbeStatus:      cloneStringPtr(state.ProbeStatus),
		ReasonCode:       cloneStringPtr(state.ReasonCode),
		QuotaResetAt:     cloneTimePtr(state.QuotaResetAt),
		BlockingWindow:   cloneStringPtr(state.BlockingWindow),
		LastSnapshotAt:   cloneTimePtr(state.SnapshotObservedAt),
		LastProbedAt:     cloneTimePtr(state.LastProbedAt),
		LastErrorCode:    cloneStringPtr(state.LastErrorCode),
		ActiveHold:       holdActive,
	}
}

func buildQuotaScanResponse(run SidecarQuotaScanRun) quotaScanResponse {
	return quotaScanResponse{
		ID:                 run.ID,
		SidecarID:          run.SidecarID,
		ScanType:           run.ScanType,
		Status:             run.Status,
		RequestedBy:        cloneStringPtr(run.RequestedBy),
		PlannedCount:       run.PlannedCount,
		AttemptedCount:     run.AttemptedCount,
		UsingCount:         run.UsingCount,
		QuotaExceededCount: run.QuotaExceededCount,
		ErrorCount:         run.ErrorCount,
		SkippedCount:       run.SkippedCount,
		CancelRequestedAt:  cloneTimePtr(run.CancelRequestedAt),
		StartedAt:          cloneTimePtr(run.StartedAt),
		CompletedAt:        cloneTimePtr(run.CompletedAt),
		LastErrorCode:      cloneStringPtr(run.LastErrorCode),
		CreatedAt:          run.CreatedAt,
		UpdatedAt:          run.UpdatedAt,
	}
}

func hasAuth(active map[string]struct{}, authID string) bool {
	_, ok := active[authID]
	return ok
}
