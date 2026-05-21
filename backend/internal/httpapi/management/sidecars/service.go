package sidecars

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/coachpo/prism/backend/internal/endpointdomain"
	"github.com/coachpo/prism/backend/internal/platform/config"
	platformcors "github.com/coachpo/prism/backend/internal/platform/cors"
)

type persistence interface {
	ListSidecarInstances(context.Context) ([]SidecarInstance, error)
	CreateSidecarInstance(context.Context, SidecarInstanceInput) (SidecarInstance, error)
	GetSidecarInstance(context.Context, int) (SidecarInstance, bool, error)
	UpdateSidecarInstance(context.Context, int, SidecarInstanceInput) (SidecarInstance, error)
	SoftDeleteSidecarInstance(context.Context, int) (bool, error)
	UpdateSidecarSyncMetadata(context.Context, SidecarSyncMetadataInput) (SidecarInstance, error)
	SaveAuthFile(context.Context, SidecarAuthFileInput) (SidecarAuthFile, error)
	ReplaceAuthFiles(context.Context, int, []SidecarAuthFileInput) ([]SidecarAuthFile, error)
	GetAuthFile(context.Context, int, string) (SidecarAuthFile, bool, error)
	ListAuthFiles(context.Context, int) ([]SidecarAuthFile, error)
	SaveProviderSnapshot(context.Context, SidecarProviderSnapshotInput) (SidecarProviderSnapshot, error)
	ReplaceProviderSnapshots(context.Context, int, string, []SidecarProviderSnapshotInput) ([]SidecarProviderSnapshot, error)
	ListProviderSnapshots(context.Context, int) ([]SidecarProviderSnapshot, error)
}

type Options struct {
	CORSOriginProvider platformcors.OriginProvider
	Pool               *pgxpool.Pool
	Store              persistence
	Now                func() time.Time
	CLIProxyClient     *CLIProxyClient
}

type Service struct {
	store               persistence
	now                 func() time.Time
	corsOriginProvider  platformcors.OriginProvider
	secretEncryptionKey string
	cliProxyClient      *CLIProxyClient
}

type domainError struct {
	StatusCode int
	Detail     any
}

func (err *domainError) Error() string {
	return fmt.Sprint(err.Detail)
}

func NewService(settings config.Settings, options Options) (*Service, error) {
	now := options.Now
	if now == nil {
		now = time.Now
	}
	store := options.Store
	if store == nil && options.Pool != nil {
		store = NewStore(StoreOptions{Pool: options.Pool, Now: now, SecretEncryptionKey: settings.SecretEncryptionKey})
	}
	if store == nil {
		store = newMemorySidecarStore(now, settings.SecretEncryptionKey)
	}
	corsOriginProvider := options.CORSOriginProvider
	if corsOriginProvider == nil {
		corsOriginProvider = platformcors.NewStaticOriginProvider(settings.CORSAllowedOriginsList())
	}
	client := options.CLIProxyClient
	if client == nil {
		client = NewCLIProxyClient(nil)
	}
	return &Service{store: store, now: now, corsOriginProvider: corsOriginProvider, secretEncryptionKey: settings.SecretEncryptionKey, cliProxyClient: client}, nil
}

func (s *Service) Close() {}

func (s *Service) nowUTC() time.Time {
	return s.now().UTC()
}

func (s *Service) corsSnapshot() platformcors.Snapshot {
	if s == nil || s.corsOriginProvider == nil {
		return platformcors.Snapshot{}
	}
	return s.corsOriginProvider.CORSSnapshot()
}

func (s *Service) MountManagementRoutes(api chi.Router) {
	api.Get("/sidecars", s.handleListSidecars)
	api.Post("/sidecars", s.handleCreateSidecar)
	api.Get("/sidecars/{sidecar_id}", s.handleGetSidecar)
	api.Patch("/sidecars/{sidecar_id}", s.handleUpdateSidecar)
	api.Delete("/sidecars/{sidecar_id}", s.handleDeleteSidecar)
	api.Post("/sidecars/{sidecar_id}/test-connection", s.handleTestSidecarConnection)
	api.Post("/sidecars/{sidecar_id}/sync", s.handleTriggerSidecarSync)
	api.Get("/sidecars/{sidecar_id}/auth-files", s.handleListSidecarAuthFiles)
	api.Get("/sidecars/{sidecar_id}/auth-files/models", s.handleListSidecarAuthFileModels)
	api.Patch("/sidecars/{sidecar_id}/auth-files/{auth_id}/status", s.handlePatchAuthFileStatus)
	api.Patch("/sidecars/{sidecar_id}/auth-files/{auth_id}/fields", s.handlePatchAuthFileFields)
	api.Delete("/sidecars/{sidecar_id}/auth-files/{auth_id}", s.handleDeleteAuthFile)
	api.Get("/sidecars/{sidecar_id}/providers", s.handleListSidecarProviders)
	api.Get("/sidecars/{sidecar_id}/provider-snapshots", s.handleListProviderSnapshots)
	api.Get("/sidecars/{sidecar_id}/sync-status", s.handleGetSidecarSyncStatus)
}

func (s *Service) decryptManagementPassword(value string) (string, error) {
	return endpointdomain.DecryptSecret(value, s.secretEncryptionKey)
}

type memorySidecarStore struct {
	mu                  sync.RWMutex
	now                 func() time.Time
	secretEncryptionKey string
	nextID              int
	nextSnapshotID      int
	instances           map[int]SidecarInstance
	authFiles           map[int]map[string]SidecarAuthFile
	providerSnapshots   map[int]map[string]SidecarProviderSnapshot
}

func newMemorySidecarStore(now func() time.Time, secretEncryptionKey string) *memorySidecarStore {
	if now == nil {
		now = time.Now
	}
	return &memorySidecarStore{
		now:                 now,
		secretEncryptionKey: secretEncryptionKey,
		nextID:              1,
		nextSnapshotID:      1,
		instances:           map[int]SidecarInstance{},
		authFiles:           map[int]map[string]SidecarAuthFile{},
		providerSnapshots:   map[int]map[string]SidecarProviderSnapshot{},
	}
}

func (s *memorySidecarStore) ListSidecarInstances(context.Context) ([]SidecarInstance, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]SidecarInstance, 0, len(s.instances))
	for _, instance := range s.instances {
		if instance.DeletedAt == nil {
			items = append(items, cloneSidecarInstance(instance))
		}
	}
	return items, nil
}

func (s *memorySidecarStore) CreateSidecarInstance(_ context.Context, input SidecarInstanceInput) (SidecarInstance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	normalized, err := s.normalizeInput(input)
	if err != nil {
		return SidecarInstance{}, err
	}
	now := s.now().UTC()
	id := s.nextID
	s.nextID++
	instance := SidecarInstance{ID: id, Name: normalized.Name, BaseURL: normalized.BaseURL, BaseURLCanonical: normalized.BaseURLCanonical, EncryptedManagementPassword: normalized.ManagementPassword, Enabled: boolValueOr(normalized.Enabled, true), EnvironmentLabel: cloneStringPtr(normalized.EnvironmentLabel), SyncIntervalSeconds: normalized.SyncIntervalSeconds, RequestTimeoutSeconds: normalized.RequestTimeoutSeconds, AllowPrivateNetwork: normalized.AllowPrivateNetwork, AllowInsecureHTTP: normalized.AllowInsecureHTTP, SkipTLSVerify: normalized.SkipTLSVerify, ManagementAuthState: normalized.ManagementAuthState, AuthFailurePauseUntil: cloneTimePtr(normalized.AuthFailurePauseUntil), CreatedAt: now, UpdatedAt: now}
	s.instances[id] = instance
	return cloneSidecarInstance(instance), nil
}

func (s *memorySidecarStore) GetSidecarInstance(_ context.Context, id int) (SidecarInstance, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	instance, ok := s.instances[id]
	if !ok || instance.DeletedAt != nil {
		return SidecarInstance{}, false, nil
	}
	return cloneSidecarInstance(instance), true, nil
}

func (s *memorySidecarStore) UpdateSidecarInstance(_ context.Context, id int, input SidecarInstanceInput) (SidecarInstance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.instances[id]
	if !ok || existing.DeletedAt != nil {
		return SidecarInstance{}, notFoundError("sidecar instance not found")
	}
	normalized, err := s.normalizeInput(input)
	if err != nil {
		return SidecarInstance{}, err
	}
	existing.Name = normalized.Name
	existing.BaseURL = normalized.BaseURL
	existing.BaseURLCanonical = normalized.BaseURLCanonical
	existing.EncryptedManagementPassword = normalized.ManagementPassword
	existing.Enabled = boolValueOr(normalized.Enabled, true)
	existing.EnvironmentLabel = cloneStringPtr(normalized.EnvironmentLabel)
	existing.SyncIntervalSeconds = normalized.SyncIntervalSeconds
	existing.RequestTimeoutSeconds = normalized.RequestTimeoutSeconds
	existing.AllowPrivateNetwork = normalized.AllowPrivateNetwork
	existing.AllowInsecureHTTP = normalized.AllowInsecureHTTP
	existing.SkipTLSVerify = normalized.SkipTLSVerify
	existing.ManagementAuthState = normalized.ManagementAuthState
	existing.AuthFailurePauseUntil = cloneTimePtr(normalized.AuthFailurePauseUntil)
	existing.UpdatedAt = s.now().UTC()
	s.instances[id] = existing
	return cloneSidecarInstance(existing), nil
}

func (s *memorySidecarStore) SoftDeleteSidecarInstance(_ context.Context, id int) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	instance, ok := s.instances[id]
	if !ok || instance.DeletedAt != nil {
		return false, nil
	}
	now := s.now().UTC()
	instance.DeletedAt = &now
	instance.UpdatedAt = now
	s.instances[id] = instance
	return true, nil
}

func (s *memorySidecarStore) GetAuthFile(_ context.Context, sidecarID int, authID string) (SidecarAuthFile, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	files := s.authFiles[sidecarID]
	if files == nil {
		return SidecarAuthFile{}, false, nil
	}
	file, ok := files[strings.TrimSpace(authID)]
	if !ok || !file.MutationSafe {
		return SidecarAuthFile{}, false, nil
	}
	return cloneAuthFile(file), true, nil
}

func (s *memorySidecarStore) normalizeInput(input SidecarInstanceInput) (SidecarInstanceInput, error) {
	if input.SyncIntervalSeconds <= 0 {
		input.SyncIntervalSeconds = DefaultSyncIntervalSeconds
	}
	if input.RequestTimeoutSeconds <= 0 {
		input.RequestTimeoutSeconds = DefaultRequestTimeoutSeconds
	}
	if input.ManagementAuthState == "" {
		input.ManagementAuthState = ManagementAuthStateUnknown
	}
	trimmedPassword := strings.TrimSpace(input.ManagementPassword)
	if strings.HasPrefix(trimmedPassword, encryptedSecretPrefix) {
		if !input.ManagementPasswordIsEncrypted {
			return SidecarInstanceInput{}, invalidInputError("management_password must not use the reserved encrypted secret prefix")
		}
		input.ManagementPassword = trimmedPassword
		return input, nil
	}
	encrypted, err := endpointdomain.EncryptSecret(trimmedPassword, s.secretEncryptionKey, s.now)
	if err != nil {
		return SidecarInstanceInput{}, err
	}
	input.ManagementPassword = encrypted
	return input, nil
}

func cloneSidecarInstance(instance SidecarInstance) SidecarInstance {
	copy := instance
	copy.EnvironmentLabel = cloneStringPtr(instance.EnvironmentLabel)
	copy.LastSyncAt = cloneTimePtr(instance.LastSyncAt)
	copy.LastSuccessfulSyncAt = cloneTimePtr(instance.LastSuccessfulSyncAt)
	copy.SnapshotStaleAfter = cloneTimePtr(instance.SnapshotStaleAfter)
	copy.LastSyncError = cloneStringPtr(instance.LastSyncError)
	copy.AuthFailurePauseUntil = cloneTimePtr(instance.AuthFailurePauseUntil)
	copy.DeletedAt = cloneTimePtr(instance.DeletedAt)
	return copy
}

func cloneAuthFile(file SidecarAuthFile) SidecarAuthFile {
	copy := file
	copy.AuthIndex = cloneStringPtr(file.AuthIndex)
	copy.Provider = cloneStringPtr(file.Provider)
	copy.Label = cloneStringPtr(file.Label)
	copy.Status = cloneStringPtr(file.Status)
	copy.StatusMessage = cloneStringPtr(file.StatusMessage)
	copy.Disabled = cloneBoolPtr(file.Disabled)
	copy.Unavailable = cloneBoolPtr(file.Unavailable)
	copy.Priority = cloneIntPtr(file.Priority)
	copy.QuotaExceeded = cloneBoolPtr(file.QuotaExceeded)
	copy.QuotaReason = cloneStringPtr(file.QuotaReason)
	copy.QuotaNextRecoverAt = cloneTimePtr(file.QuotaNextRecoverAt)
	copy.SuccessCount = cloneIntPtr(file.SuccessCount)
	copy.FailedCount = cloneIntPtr(file.FailedCount)
	copy.RecentRequestsJSON = append([]byte(nil), file.RecentRequestsJSON...)
	copy.ModelStatesJSON = append([]byte(nil), file.ModelStatesJSON...)
	copy.SnapshotJSON = append([]byte(nil), file.SnapshotJSON...)
	return copy
}

func cloneProviderSnapshot(snapshot SidecarProviderSnapshot) SidecarProviderSnapshot {
	copy := snapshot
	copy.Name = cloneStringPtr(snapshot.Name)
	copy.Label = cloneStringPtr(snapshot.Label)
	copy.Status = cloneStringPtr(snapshot.Status)
	copy.Disabled = cloneBoolPtr(snapshot.Disabled)
	copy.SnapshotJSON = append([]byte(nil), snapshot.SnapshotJSON...)
	return copy
}

func cloneStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneBoolPtr(value *bool) *bool {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneIntPtr(value *int) *int {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneInt64Ptr(value *int64) *int64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
