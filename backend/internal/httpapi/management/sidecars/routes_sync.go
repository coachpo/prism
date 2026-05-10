package sidecars

import (
	"net/http"
	"time"
)

type sidecarSyncResponse struct {
	State                 string                    `json:"state"`
	Sidecar               sidecarInstanceResponse   `json:"sidecar"`
	SyncStatus            sidecarSyncStatusResponse `json:"sync_status"`
	AuthSnapshotCount     int                       `json:"auth_snapshot_count"`
	ProviderSnapshotCount int                       `json:"provider_snapshot_count"`
	ErrorCode             string                    `json:"error_code,omitempty"`
	ErrorDetail           string                    `json:"error_detail,omitempty"`
}

type sidecarSyncStatusResponse struct {
	SidecarID             int        `json:"sidecar_id"`
	Enabled               bool       `json:"enabled"`
	SyncIntervalSeconds   int        `json:"sync_interval_seconds"`
	ManagementAuthState   string     `json:"management_auth_state"`
	LastSyncAt            *time.Time `json:"last_sync_at,omitempty"`
	LastSuccessfulSyncAt  *time.Time `json:"last_successful_sync_at,omitempty"`
	SnapshotStaleAfter    *time.Time `json:"snapshot_stale_after,omitempty"`
	LastSyncError         *string    `json:"last_sync_error,omitempty"`
	AuthFailurePauseUntil *time.Time `json:"auth_failure_pause_until,omitempty"`
	Stale                 bool       `json:"stale"`
	Due                   bool       `json:"due"`
	Paused                bool       `json:"paused"`
}

func (s *Service) handleTriggerSidecarSync(w http.ResponseWriter, r *http.Request) {
	id, ok := s.parseSidecarID(w, r)
	if !ok {
		return
	}
	result, err := s.SyncSidecar(r.Context(), id)
	if err != nil {
		if result.Sidecar.ID == 0 {
			writeDomainError(w, r, s.corsSnapshot(), err)
			return
		}
		reason := "manual sync failed"
		s.recordAction(r.Context(), id, "sync.manual", "failed", &reason)
		writeJSON(w, sidecarSyncHTTPStatus(result), buildSidecarSyncResponse(s, result))
		return
	}
	reason := "manual sync completed"
	s.recordAction(r.Context(), id, "sync.manual", "succeeded", &reason)
	writeJSON(w, http.StatusOK, buildSidecarSyncResponse(s, result))
}

func (s *Service) handleListSidecarAuthFiles(w http.ResponseWriter, r *http.Request) {
	s.handleListAuthSnapshots(w, r)
}

func (s *Service) handleListAuthSnapshots(w http.ResponseWriter, r *http.Request) {
	id, ok := s.parseSidecarID(w, r)
	if !ok || !s.ensureSidecarExists(w, r, id) {
		return
	}
	snapshots, err := s.store.ListAuthSnapshots(r.Context(), id)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	items := make([]authSnapshotResponse, 0, len(snapshots))
	for _, snapshot := range snapshots {
		items = append(items, buildAuthSnapshotResponse(snapshot))
	}
	writeJSON(w, http.StatusOK, authSnapshotListResponse{Items: items})
}

func (s *Service) handleListSidecarProviders(w http.ResponseWriter, r *http.Request) {
	s.handleListProviderSnapshots(w, r)
}

func (s *Service) handleListProviderSnapshots(w http.ResponseWriter, r *http.Request) {
	id, ok := s.parseSidecarID(w, r)
	if !ok || !s.ensureSidecarExists(w, r, id) {
		return
	}
	snapshots, err := s.store.ListProviderSnapshots(r.Context(), id)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	items := make([]providerSnapshotResponse, 0, len(snapshots))
	for _, snapshot := range snapshots {
		items = append(items, buildProviderSnapshotResponse(snapshot))
	}
	writeJSON(w, http.StatusOK, providerSnapshotListResponse{Items: items})
}

func (s *Service) handleGetSidecarSyncStatus(w http.ResponseWriter, r *http.Request) {
	id, ok := s.parseSidecarID(w, r)
	if !ok {
		return
	}
	instance, found, err := s.store.GetSidecarInstance(r.Context(), id)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	if !found {
		writeError(w, r, s.corsSnapshot(), http.StatusNotFound, "sidecar not found")
		return
	}
	writeJSON(w, http.StatusOK, buildSidecarSyncStatusResponse(s.sidecarSyncStatus(instance)))
}

func buildSidecarSyncResponse(s *Service, result SidecarSyncResult) sidecarSyncResponse {
	state := "succeeded"
	if result.Skipped {
		state = "skipped"
	}
	if result.ErrorDetail != "" {
		state = "failed"
	}
	return sidecarSyncResponse{
		State:                 state,
		Sidecar:               buildSidecarInstanceResponse(result.Sidecar),
		SyncStatus:            buildSidecarSyncStatusResponse(s.sidecarSyncStatus(result.Sidecar)),
		AuthSnapshotCount:     result.AuthSnapshotCount,
		ProviderSnapshotCount: result.ProviderSnapshotCount,
		ErrorCode:             result.ErrorCode,
		ErrorDetail:           result.ErrorDetail,
	}
}

func buildSidecarSyncStatusResponse(status SidecarSyncStatus) sidecarSyncStatusResponse {
	return sidecarSyncStatusResponse{
		SidecarID:             status.SidecarID,
		Enabled:               status.Enabled,
		SyncIntervalSeconds:   status.SyncIntervalSeconds,
		ManagementAuthState:   status.ManagementAuthState,
		LastSyncAt:            status.LastSyncAt,
		LastSuccessfulSyncAt:  status.LastSuccessfulSyncAt,
		SnapshotStaleAfter:    status.SnapshotStaleAfter,
		LastSyncError:         status.LastSyncError,
		AuthFailurePauseUntil: status.AuthFailurePauseUntil,
		Stale:                 status.Stale,
		Due:                   status.Due,
		Paused:                status.Paused,
	}
}

func sidecarSyncHTTPStatus(result SidecarSyncResult) int {
	switch result.ErrorCode {
	case "sidecar_disabled":
		return http.StatusConflict
	case string(CLIProxyErrorInvalidManagementAuth):
		return http.StatusFailedDependency
	default:
		return http.StatusBadGateway
	}
}
