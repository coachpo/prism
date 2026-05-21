package sidecars

import (
	"context"
	"errors"
	"net/http"
	neturl "net/url"
	"strings"
	"time"
)

type sidecarSyncResponse struct {
	State                 string                    `json:"state"`
	Sidecar               sidecarInstanceResponse   `json:"sidecar"`
	SyncStatus            sidecarSyncStatusResponse `json:"sync_status"`
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

type authFileModelsResponse struct {
	Models []authFileModelResponse `json:"models"`
}

type authFileModelResponse struct {
	ID          string  `json:"id"`
	DisplayName *string `json:"display_name,omitempty"`
	Type        *string `json:"type,omitempty"`
	OwnedBy     *string `json:"owned_by,omitempty"`
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
		writeJSON(w, sidecarSyncHTTPStatus(result), buildSidecarSyncResponse(s, result))
		return
	}
	writeJSON(w, http.StatusOK, buildSidecarSyncResponse(s, result))
}

func (s *Service) handleListSidecarAuthFiles(w http.ResponseWriter, r *http.Request) {
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
	files, err := s.fetchLiveAuthFilesForDisplay(r.Context(), instance, s.nowUTC())
	if err != nil {
		s.writeAuthFilesError(w, r, instance, err)
		return
	}
	items := make([]authFileResponse, 0, len(files))
	for _, file := range files {
		items = append(items, buildAuthFileResponse(file))
	}
	writeJSON(w, http.StatusOK, authFileListResponse{Items: items})
}

func (s *Service) handleListSidecarAuthFileModels(w http.ResponseWriter, r *http.Request) {
	id, ok := s.parseSidecarID(w, r)
	if !ok {
		return
	}
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "name is required")
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
	models, err := s.fetchSidecarAuthFileModels(r.Context(), instance, name)
	if err != nil {
		s.writeAuthFileModelsError(w, r, instance, err)
		return
	}
	if models.Models == nil {
		models.Models = []authFileModelResponse{}
	}
	writeJSON(w, http.StatusOK, models)
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

func (s *Service) fetchLiveAuthFilesForDisplay(ctx context.Context, instance SidecarInstance, observedAt time.Time) ([]SidecarAuthFile, error) {
	target, err := s.cliProxyTarget(instance)
	if err != nil {
		return nil, err
	}
	authRows, response, err := s.fetchSidecarAuthFileRowsWithResponse(ctx, target)
	if err != nil {
		return nil, err
	}
	deleteSupported := authDeleteCapabilitySupported(response)
	files := make([]SidecarAuthFile, 0, len(authRows))
	for _, raw := range authRows {
		input, err := normalizeSidecarAuthFileWithDeleteSupport(instance.ID, observedAt, raw, deleteSupported)
		if err != nil {
			return nil, err
		}
		normalized, err := normalizeAuthFileStoreInput(input)
		if err != nil {
			return nil, err
		}
		files = append(files, authFileFromInput(normalized, observedAt))
	}
	return files, nil
}

func (s *Service) fetchSidecarAuthFileModels(ctx context.Context, instance SidecarInstance, name string) (authFileModelsResponse, error) {
	target, err := s.cliProxyTarget(instance)
	if err != nil {
		return authFileModelsResponse{}, err
	}
	models := authFileModelsResponse{}
	_, err = s.cliProxyClient.FetchJSONWithQuery(ctx, target, http.MethodGet, "/auth-files/models", neturl.Values{"name": []string{name}}, nil, &models)
	return models, err
}

func (s *Service) writeAuthFilesError(w http.ResponseWriter, r *http.Request, instance SidecarInstance, err error) {
	if errors.Is(err, errSidecarManagementPasswordMissing) {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "management password is not configured")
		return
	}
	var clientErr *CLIProxyClientError
	if errors.As(err, &clientErr) {
		switch clientErr.Code {
		case CLIProxyErrorInvalidManagementAuth:
			s.markInvalidManagementAuth(r.Context(), instance)
			writeError(w, r, s.corsSnapshot(), http.StatusFailedDependency, "sidecar management authentication failed")
		case CLIProxyErrorManagementDisabled:
			writeError(w, r, s.corsSnapshot(), http.StatusNotFound, "auth-files inventory unavailable")
		case CLIProxyErrorRequestBuild, CLIProxyErrorUnsupportedPath:
			writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "sidecar auth-files request could not be built")
		default:
			writeError(w, r, s.corsSnapshot(), http.StatusBadGateway, "sidecar auth-files inventory failed")
		}
		return
	}
	writeDomainError(w, r, s.corsSnapshot(), err)
}

func (s *Service) writeAuthFileModelsError(w http.ResponseWriter, r *http.Request, instance SidecarInstance, err error) {
	if errors.Is(err, errSidecarManagementPasswordMissing) {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "management password is not configured")
		return
	}
	var clientErr *CLIProxyClientError
	if errors.As(err, &clientErr) {
		switch clientErr.Code {
		case CLIProxyErrorInvalidManagementAuth:
			s.markInvalidManagementAuth(r.Context(), instance)
			writeError(w, r, s.corsSnapshot(), http.StatusFailedDependency, "sidecar management authentication failed")
		case CLIProxyErrorManagementDisabled:
			writeError(w, r, s.corsSnapshot(), http.StatusNotFound, "authfile models discovery unsupported")
		case CLIProxyErrorRequestBuild, CLIProxyErrorUnsupportedPath:
			writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "sidecar models request could not be built")
		default:
			writeError(w, r, s.corsSnapshot(), http.StatusBadGateway, "sidecar models discovery failed")
		}
		return
	}
	writeDomainError(w, r, s.corsSnapshot(), err)
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
