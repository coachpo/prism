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
)

type Options struct {
	ConfigPath         string
	LoadedRevision     int
	LoadedDocumentETag string
	Manager            config.BootstrapConfigManager
	Writable           func(string) bool
}

type Service struct {
	configPath         string
	loadedRevision     int
	loadedDocumentETag string
	manager            config.BootstrapConfigManager
	writable           func(string) bool
	allowedOrigins     map[string]struct{}
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

	allowedOrigins := map[string]struct{}{}
	for _, origin := range settings.CORSAllowedOriginsList() {
		allowedOrigins[origin] = struct{}{}
	}

	return &Service{
		configPath:         configPath,
		loadedRevision:     options.LoadedRevision,
		loadedDocumentETag: strings.TrimSpace(options.LoadedDocumentETag),
		manager:            options.Manager,
		writable:           writable,
		allowedOrigins:     allowedOrigins,
	}, nil
}

func (s *Service) MountManagementRoutes(api chi.Router) {
	api.Get("/config/bootstrap", s.handleGetBootstrapConfig)
	api.Post("/config/bootstrap/validate", s.handleValidateBootstrapConfig)
	api.Put("/config/bootstrap", s.handlePutBootstrapConfig)
}

func (s *Service) handleGetBootstrapConfig(w http.ResponseWriter, r *http.Request) {
	snapshot, err := s.loadCurrentSnapshot()
	if err != nil {
		writeError(w, r, s.allowedOrigins, http.StatusInternalServerError, "Failed to load bootstrap config")
		return
	}
	writeJSON(w, http.StatusOK, s.responseForSnapshot(snapshot))
}

func (s *Service) handleValidateBootstrapConfig(w http.ResponseWriter, r *http.Request) {
	var requestBody config.BootstrapConfigUpdateRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		writeError(w, r, s.allowedOrigins, http.StatusBadRequest, "Invalid request body")
		return
	}
	if _, err := s.loadCurrentSnapshot(); err != nil {
		writeError(w, r, s.allowedOrigins, http.StatusInternalServerError, "Failed to load bootstrap config")
		return
	}

	prepared, err := s.manager.ValidateBootstrapConfigUpdate(s.configPath, requestBody)
	if err != nil {
		writePrepareError(w, r, s.allowedOrigins, err)
		return
	}
	writeJSON(w, http.StatusOK, s.responseForSnapshot(prepared.Snapshot))
}

func (s *Service) handlePutBootstrapConfig(w http.ResponseWriter, r *http.Request) {
	var requestBody config.BootstrapConfigUpdateRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		writeError(w, r, s.allowedOrigins, http.StatusBadRequest, "Invalid request body")
		return
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	prepared, err := s.manager.PrepareBootstrapConfigUpdate(s.configPath, requestBody)
	if err != nil {
		writePrepareError(w, r, s.allowedOrigins, err)
		return
	}
	snapshot, err := s.manager.WriteBootstrapConfigUpdate(s.configPath, prepared)
	if err != nil {
		writeError(w, r, s.allowedOrigins, http.StatusInternalServerError, "Failed to write bootstrap config")
		return
	}
	writeJSON(w, http.StatusOK, s.responseForSnapshot(snapshot))
}

func (s *Service) loadCurrentSnapshot() (config.BootstrapConfigSnapshot, error) {
	snapshot, _, err := s.manager.LoadBootstrapConfigDocument(s.configPath)
	return snapshot, err
}

func (s *Service) responseForSnapshot(snapshot config.BootstrapConfigSnapshot) config.BootstrapConfigResponse {
	return config.BuildBootstrapConfigResponse(snapshot, s.loadedRevision, s.loadedDocumentETag, s.writable(s.configPath))
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

func writePrepareError(w http.ResponseWriter, r *http.Request, allowedOrigins map[string]struct{}, err error) {
	var conflictErr *config.BootstrapConfigConflictError
	if errors.As(err, &conflictErr) {
		writeError(w, r, allowedOrigins, http.StatusConflict, map[string]any{
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
		writeError(w, r, allowedOrigins, http.StatusUnprocessableEntity, map[string]any{
			"message": secretErr.Error(),
			"field":   secretErr.Field,
			"action":  secretErr.Action,
			"reason":  secretErr.Reason,
		})
		return
	}

	var confirmationErr *config.BootstrapConfigMissingConfirmationsError
	if errors.As(err, &confirmationErr) {
		writeError(w, r, allowedOrigins, http.StatusUnprocessableEntity, map[string]any{
			"message":                confirmationErr.Error(),
			"required_confirmations": confirmationErr.RequiredConfirmations,
		})
		return
	}

	writeError(w, r, allowedOrigins, http.StatusBadRequest, err.Error())
}

func writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, r *http.Request, allowedOrigins map[string]struct{}, statusCode int, detail any) {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin != "" {
		if _, ok := allowedOrigins[origin]; ok {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Vary", "Origin")
		}
	}
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
