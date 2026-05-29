package bootstrapconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"

	"github.com/coachpo/prism/backend/internal/platform/config"
	platformcors "github.com/coachpo/prism/backend/internal/platform/cors"
)

type Options struct {
	ConfigPath         string
	LoadedRevision     int
	LoadedDocumentETag string
	CORSOriginProvider platformcors.OriginProvider
	HotApplyRuntime    config.BootstrapConfigHotApplyRuntime
	Manager            config.BootstrapConfigManager
	Writable           func(string) bool
}

type Service struct {
	configPath         string
	loadedRevision     int
	loadedDocumentETag string
	loadedSettings     config.Settings
	liveSettings       config.Settings
	liveMu             sync.RWMutex
	manager            config.BootstrapConfigManager
	writable           func(string) bool
	corsOriginProvider platformcors.OriginProvider
	hotApplyRuntime    config.BootstrapConfigHotApplyRuntime
	writeMu            sync.Mutex
}

func NewService(settings config.Settings, options Options) (*Service, error) {
	configPath := strings.TrimSpace(options.ConfigPath)
	if configPath == "" {
		return nil, fmt.Errorf("bootstrap config path is required")
	}
	if options.LoadedRevision < 0 {
		return nil, fmt.Errorf("loaded bootstrap config revision must be greater than or equal to 0")
	}

	writable := options.Writable
	if writable == nil {
		writable = defaultWritable
	}

	corsOriginProvider := options.CORSOriginProvider
	if corsOriginProvider == nil {
		corsOriginProvider = platformcors.NewStaticOriginProvider(settings.CORSAllowedOriginsList())
	}

	return &Service{
		configPath:         configPath,
		loadedRevision:     options.LoadedRevision,
		loadedDocumentETag: strings.TrimSpace(options.LoadedDocumentETag),
		liveSettings:       settings,
		manager:            options.Manager,
		writable:           writable,
		corsOriginProvider: corsOriginProvider,
		hotApplyRuntime:    options.HotApplyRuntime,
	}, nil
}

func (s *Service) MountManagementRoutes(api chi.Router) {
	api.Get("/config/bootstrap", s.handleGetBootstrapConfig)
	api.Post("/config/bootstrap/validate", s.handleValidateBootstrapConfig)
	api.Put("/config/bootstrap", s.handlePutBootstrapConfig)
}

func (s *Service) corsSnapshot() platformcors.Snapshot {
	if s == nil || s.corsOriginProvider == nil {
		return platformcors.Snapshot{}
	}
	return s.corsOriginProvider.CORSSnapshot()
}

func (s *Service) handleGetBootstrapConfig(w http.ResponseWriter, r *http.Request) {
	corsSnapshot := s.corsSnapshot()
	snapshot, currentSettings, err := s.loadCurrentSnapshot()
	if err != nil {
		writeError(w, r, corsSnapshot, http.StatusInternalServerError, "Failed to load bootstrap config")
		return
	}
	liveSettings := s.currentLiveSettings()
	options, err := currentResponseOptions(liveSettings, currentSettings)
	if err != nil {
		writeError(w, r, corsSnapshot, http.StatusInternalServerError, "Failed to classify bootstrap config effects")
		return
	}
	writeJSON(w, http.StatusOK, s.responseForSnapshot(snapshot, currentSettings, liveSettings, options))
}

func (s *Service) handleValidateBootstrapConfig(w http.ResponseWriter, r *http.Request) {
	corsSnapshot := s.corsSnapshot()
	var requestBody config.BootstrapConfigUpdateRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		writeError(w, r, corsSnapshot, http.StatusBadRequest, "Invalid request body")
		return
	}
	_, currentSettings, err := s.loadCurrentSnapshot()
	if err != nil {
		writeError(w, r, corsSnapshot, http.StatusInternalServerError, "Failed to load bootstrap config")
		return
	}
	liveSettings := s.currentLiveSettings()

	prepared, err := s.manager.ValidateBootstrapConfigUpdate(s.configPath, requestBody)
	if err != nil {
		writePrepareError(w, r, corsSnapshot, err)
		return
	}
	preparedSettings, err := settingsForPreparedUpdate(s.manager, prepared, currentSettings)
	if err != nil {
		writeError(w, r, corsSnapshot, http.StatusInternalServerError, "Failed to load bootstrap config")
		return
	}
	plannedDiff, err := diffBootstrapResponseSettings(liveSettings, preparedSettings)
	if err != nil {
		writePrepareError(w, r, corsSnapshot, err)
		return
	}
	plannedChanges := config.BootstrapConfigPlannedChangesFromDiff(plannedDiff)
	writeJSON(w, http.StatusOK, s.responseForSnapshot(prepared.Snapshot, preparedSettings, liveSettings, config.BootstrapConfigResponseOptions{PlannedChanges: &plannedChanges}))
}

func (s *Service) handlePutBootstrapConfig(w http.ResponseWriter, r *http.Request) {
	corsSnapshot := s.corsSnapshot()
	var requestBody config.BootstrapConfigUpdateRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		writeError(w, r, corsSnapshot, http.StatusBadRequest, "Invalid request body")
		return
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	_, currentSettings, err := s.loadCurrentSnapshot()
	if err != nil {
		writeError(w, r, corsSnapshot, http.StatusInternalServerError, "Failed to load bootstrap config")
		return
	}
	liveSettings := s.currentLiveSettings()
	prepared, err := s.manager.PrepareBootstrapConfigUpdate(s.configPath, requestBody)
	if err != nil {
		writePrepareError(w, r, corsSnapshot, err)
		return
	}
	preparedSettings, err := settingsForPreparedUpdate(s.manager, prepared, currentSettings)
	if err != nil {
		writeError(w, r, corsSnapshot, http.StatusInternalServerError, "Failed to load bootstrap config")
		return
	}
	applyDiff, err := diffBootstrapResponseSettings(liveSettings, preparedSettings)
	if err != nil {
		writePrepareError(w, r, corsSnapshot, err)
		return
	}
	applyResult := config.BootstrapConfigApplyResultFromDiff(applyDiff)
	hotApplySettings := s.hotApplySettings(liveSettings, preparedSettings)
	if err := s.validateHotApply(hotApplySettings, applyDiff); err != nil {
		writePrepareError(w, r, corsSnapshot, err)
		return
	}
	snapshot, err := s.manager.WriteBootstrapConfigUpdate(s.configPath, prepared)
	if err != nil {
		writeError(w, r, corsSnapshot, http.StatusInternalServerError, "Failed to write bootstrap config")
		return
	}
	if err := s.publishHotApply(hotApplySettings, applyDiff, &applyResult); err != nil {
		writeApplyFailure(w, s.responseForSnapshot(snapshot, preparedSettings, liveSettings, config.BootstrapConfigResponseOptions{ApplyResult: &applyResult}))
		return
	}
	writeJSON(w, http.StatusOK, s.responseForSnapshot(snapshot, preparedSettings, s.currentLiveSettings(), config.BootstrapConfigResponseOptions{ApplyResult: &applyResult}))
}

func (s *Service) loadCurrentSnapshot() (config.BootstrapConfigSnapshot, config.Settings, error) {
	snapshot, settings, err := s.manager.LoadBootstrapConfigDocument(s.configPath)
	return snapshot, settings, err
}

func (s *Service) responseForSnapshot(snapshot config.BootstrapConfigSnapshot, currentSettings config.Settings, liveSettings config.Settings, options config.BootstrapConfigResponseOptions) config.BootstrapConfigResponse {
	return config.BuildBootstrapConfigResponse(snapshot, currentSettings, liveSettings, s.loadedRevision, s.loadedDocumentETag, s.writable(s.configPath), options)
}

func (s *Service) currentLiveSettings() config.Settings {
	if s == nil {
		return config.Settings{}
	}
	s.liveMu.RLock()
	defer s.liveMu.RUnlock()
	return s.liveSettings
}

func (s *Service) setLiveSettings(settings config.Settings) {
	if s == nil {
		return
	}
	s.liveMu.Lock()
	defer s.liveMu.Unlock()
	s.liveSettings = settings
}

func (s *Service) hotApplySettings(liveSettings config.Settings, requested config.Settings) config.Settings {
	if s == nil {
		return requested
	}
	projected := liveSettings
	projected.CORSAllowedOrigins = requested.CORSAllowedOrigins
	projected.AuthAccessTokenTTLSeconds = requested.AuthAccessTokenTTLSeconds
	projected.AuthRefreshTokenTTLSeconds = requested.AuthRefreshTokenTTLSeconds
	projected.AuthResetCodeTTLSeconds = requested.AuthResetCodeTTLSeconds
	projected.AuthCookieName = requested.AuthCookieName
	projected.AuthRefreshCookieName = requested.AuthRefreshCookieName
	projected.AuthCookieSecure = requested.AuthCookieSecure
	projected.Mail = requested.Mail
	projected.RuntimeTransportConfig = requested.RuntimeTransportConfig
	projected.ManagementAdmissionControlBudget = requested.ManagementAdmissionControlBudget
	// Telemetry providers are startup-owned; telemetry.* edits remain pending until restart.
	return projected
}

func (s *Service) validateHotApply(settings config.Settings, diff config.BootstrapConfigFieldDiff) error {
	if s == nil || s.hotApplyRuntime == nil || len(diff.ChangedHotApplyFields) == 0 {
		return nil
	}
	return s.hotApplyRuntime.Validate(settings)
}

func (s *Service) publishHotApply(settings config.Settings, diff config.BootstrapConfigFieldDiff, result *config.BootstrapConfigApplyResult) error {
	if s == nil || s.hotApplyRuntime == nil || len(diff.ChangedHotApplyFields) == 0 {
		return nil
	}
	retired, err := s.hotApplyRuntime.Publish(settings)
	if err != nil {
		if result != nil {
			result.FailedHotApplyFields = append([]string(nil), diff.ChangedHotApplyFields...)
		}
		return err
	}
	if retired != nil {
		retired.CloseIdleConnections()
	}
	if result != nil {
		result.AppliedNowFields = append([]string(nil), diff.ChangedHotApplyFields...)
		result.PendingHotApplyFields = []string{}
	}
	s.setLiveSettings(settings)
	return nil
}

func settingsForPreparedUpdate(manager config.BootstrapConfigManager, prepared config.BootstrapConfigPreparedUpdate, currentSettings config.Settings) (config.Settings, error) {
	if prepared.Noop {
		return currentSettings, nil
	}
	return manager.Parse(prepared.Payload)
}

func currentResponseOptions(liveSettings config.Settings, currentSettings config.Settings) (config.BootstrapConfigResponseOptions, error) {
	diff, err := diffBootstrapResponseSettings(liveSettings, currentSettings)
	if err != nil {
		return config.BootstrapConfigResponseOptions{}, err
	}
	if !diff.HasChanges() {
		return config.BootstrapConfigResponseOptions{}, nil
	}
	applyResult := config.BootstrapConfigApplyResultFromDiff(diff)
	return config.BootstrapConfigResponseOptions{ApplyResult: &applyResult}, nil
}

func diffBootstrapResponseSettings(liveSettings config.Settings, requestedSettings config.Settings) (config.BootstrapConfigFieldDiff, error) {
	return config.DiffBootstrapConfigSettings(liveSettings, requestedSettings)
}

type bootstrapConfigApplyFailureResponse struct {
	config.BootstrapConfigResponse
	Detail map[string]any `json:"detail"`
}

func writeApplyFailure(w http.ResponseWriter, response config.BootstrapConfigResponse) {
	writeJSON(w, http.StatusInternalServerError, bootstrapConfigApplyFailureResponse{
		BootstrapConfigResponse: response,
		Detail: map[string]any{
			"message":                 "Failed to apply bootstrap config",
			"failed_hot_apply_fields": response.ApplyResult.FailedHotApplyFields,
		},
	})
}

func decodeJSONBody(request *http.Request, target any) error {
	defer func() {
		_ = request.Body.Close()
	}()
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("request body must contain a single JSON object")
		}
		return err
	}
	return nil
}

func writePrepareError(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot, err error) {
	var conflictErr *config.BootstrapConfigConflictError
	if errors.As(err, &conflictErr) {
		writeError(w, r, corsSnapshot, http.StatusConflict, map[string]any{
			"message":           conflictErr.Error(),
			"expected_revision": conflictErr.ExpectedRevision,
			"current_revision":  conflictErr.CurrentRevision,
			"expected_etag":     conflictErr.ExpectedETag,
			"current_etag":      conflictErr.CurrentETag,
		})
		return
	}

	var secretErr *config.BootstrapConfigSecretOperationError
	if errors.As(err, &secretErr) {
		writeError(w, r, corsSnapshot, http.StatusUnprocessableEntity, map[string]any{
			"message": secretErr.Error(),
			"field":   secretErr.Field,
			"action":  secretErr.Action,
			"reason":  secretErr.Reason,
		})
		return
	}

	var confirmationErr *config.BootstrapConfigMissingConfirmationsError
	if errors.As(err, &confirmationErr) {
		writeError(w, r, corsSnapshot, http.StatusUnprocessableEntity, map[string]any{
			"message":                confirmationErr.Error(),
			"required_confirmations": confirmationErr.RequiredConfirmations,
		})
		return
	}

	writeError(w, r, corsSnapshot, http.StatusBadRequest, err.Error())
}

func writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot, statusCode int, detail any) {
	platformcors.ApplyAllowOriginHeaders(w, r, corsSnapshot)
	writeJSON(w, statusCode, map[string]any{"detail": detail})
}

func defaultWritable(path string) bool {
	normalizedPath := strings.TrimSpace(path)
	if normalizedPath == "" {
		return false
	}
	file, err := os.OpenFile(normalizedPath, os.O_WRONLY, 0)
	if err != nil {
		return false
	}
	_ = file.Close()

	parentInfo, err := os.Stat(filepath.Dir(normalizedPath))
	if err != nil || !parentInfo.IsDir() {
		return false
	}
	return parentInfo.Mode().Perm()&0o222 != 0
}
