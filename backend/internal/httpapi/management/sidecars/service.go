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
	SaveAuthSnapshot(context.Context, SidecarAuthSnapshotInput) (SidecarAuthSnapshot, error)
	GetAuthSnapshot(context.Context, int, string) (SidecarAuthSnapshot, bool, error)
	ListAuthSnapshots(context.Context, int) ([]SidecarAuthSnapshot, error)
	SaveProviderSnapshot(context.Context, SidecarProviderSnapshotInput) (SidecarProviderSnapshot, error)
	ReplaceProviderSnapshots(context.Context, int, string, []SidecarProviderSnapshotInput) ([]SidecarProviderSnapshot, error)
	ListProviderSnapshots(context.Context, int) ([]SidecarProviderSnapshot, error)
	GetOrCreateWatchdogPolicy(context.Context, int) (SidecarWatchdogPolicy, error)
	UpsertWatchdogPolicy(context.Context, SidecarWatchdogPolicyInput) (SidecarWatchdogPolicy, error)
	CreateWatchdogHold(context.Context, SidecarWatchdogHoldInput) (SidecarWatchdogHold, error)
	GetActiveWatchdogHold(context.Context, int, string) (SidecarWatchdogHold, bool, error)
	ListActiveWatchdogHolds(context.Context, int) ([]SidecarWatchdogHold, error)
	UpdateWatchdogHold(context.Context, int, SidecarWatchdogHoldInput) (SidecarWatchdogHold, error)
	CreateWatchdogAction(context.Context, SidecarWatchdogActionInput) (SidecarWatchdogAction, error)
}

type actionHistoryPersistence interface {
	ListWatchdogActions(context.Context, int) ([]SidecarWatchdogAction, error)
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
	watchdogMu          sync.Mutex
	watchdogLocks       map[int]struct{}
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
	return &Service{store: store, now: now, corsOriginProvider: corsOriginProvider, secretEncryptionKey: settings.SecretEncryptionKey, cliProxyClient: client, watchdogLocks: map[int]struct{}{}}, nil
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
	api.Patch("/sidecars/{sidecar_id}/auth-files/{auth_id}/status", s.handlePatchAuthFileStatus)
	api.Patch("/sidecars/{sidecar_id}/auth-files/{auth_id}/fields", s.handlePatchAuthFileFields)
	api.Get("/sidecars/{sidecar_id}/auth-snapshots", s.handleListAuthSnapshots)
	api.Get("/sidecars/{sidecar_id}/auth-snapshots/{snapshot_id}", s.handleGetAuthSnapshot)
	api.Get("/sidecars/{sidecar_id}/providers", s.handleListSidecarProviders)
	api.Get("/sidecars/{sidecar_id}/provider-snapshots", s.handleListProviderSnapshots)
	api.Get("/sidecars/{sidecar_id}/sync-status", s.handleGetSidecarSyncStatus)
	api.Get("/sidecars/{sidecar_id}/watchdog-policy", s.handleGetWatchdogPolicy)
	api.Put("/sidecars/{sidecar_id}/watchdog-policy", s.handleUpdateWatchdogPolicy)
	api.Get("/sidecars/{sidecar_id}/actions", s.handleListActionHistory)
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
	nextPolicyID        int
	nextHoldID          int
	nextActionID        int
	instances           map[int]SidecarInstance
	authSnapshots       map[int]map[string]SidecarAuthSnapshot
	providerSnapshots   map[int]map[string]SidecarProviderSnapshot
	policies            map[int]SidecarWatchdogPolicy
	holds               map[int][]SidecarWatchdogHold
	actions             map[int][]SidecarWatchdogAction
}

func newMemorySidecarStore(now func() time.Time, secretEncryptionKey string) *memorySidecarStore {
	if now == nil {
		now = time.Now
	}
	return &memorySidecarStore{now: now, secretEncryptionKey: secretEncryptionKey, nextID: 1, nextSnapshotID: 1, nextPolicyID: 1, nextHoldID: 1, nextActionID: 1, instances: map[int]SidecarInstance{}, authSnapshots: map[int]map[string]SidecarAuthSnapshot{}, providerSnapshots: map[int]map[string]SidecarProviderSnapshot{}, policies: map[int]SidecarWatchdogPolicy{}, holds: map[int][]SidecarWatchdogHold{}, actions: map[int][]SidecarWatchdogAction{}}
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

func (s *memorySidecarStore) GetAuthSnapshot(_ context.Context, sidecarID int, authID string) (SidecarAuthSnapshot, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snapshots := s.authSnapshots[sidecarID]
	if snapshots == nil {
		return SidecarAuthSnapshot{}, false, nil
	}
	snapshot, ok := snapshots[authID]
	if !ok {
		return SidecarAuthSnapshot{}, false, nil
	}
	return cloneAuthSnapshot(snapshot), true, nil
}

func (s *memorySidecarStore) GetOrCreateWatchdogPolicy(_ context.Context, sidecarID int) (SidecarWatchdogPolicy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.instances[sidecarID]; !ok {
		return SidecarWatchdogPolicy{}, notFoundError("sidecar instance not found")
	}
	if policy, ok := s.policies[sidecarID]; ok {
		return policy, nil
	}
	now := s.now().UTC()
	policy := SidecarWatchdogPolicy{ID: s.nextPolicyID, SidecarID: sidecarID, Enabled: false, FailureThreshold: DefaultFailureThreshold, FailureWindowSeconds: DefaultFailureWindowSeconds, FallbackCooldownSeconds: DefaultFallbackCooldownSeconds, DeprioritizedPriority: DefaultDeprioritizedPriority, ManualOverridePauseSeconds: DefaultManualOverridePauseSeconds, CreatedAt: now, UpdatedAt: now}
	s.nextPolicyID++
	s.policies[sidecarID] = policy
	return policy, nil
}

func (s *memorySidecarStore) UpsertWatchdogPolicy(_ context.Context, input SidecarWatchdogPolicyInput) (SidecarWatchdogPolicy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.instances[input.SidecarID]; !ok {
		return SidecarWatchdogPolicy{}, notFoundError("sidecar instance not found")
	}
	now := s.now().UTC()
	policy, ok := s.policies[input.SidecarID]
	if !ok {
		policy = SidecarWatchdogPolicy{ID: s.nextPolicyID, SidecarID: input.SidecarID, CreatedAt: now}
		s.nextPolicyID++
	}
	normalized := normalizePolicyInput(input)
	policy.Enabled = normalized.Enabled
	policy.FailureThreshold = normalized.FailureThreshold
	policy.FailureWindowSeconds = normalized.FailureWindowSeconds
	policy.FallbackCooldownSeconds = normalized.FallbackCooldownSeconds
	policy.DeprioritizedPriority = normalized.DeprioritizedPriority
	policy.ManualOverridePauseSeconds = normalized.ManualOverridePauseSeconds
	policy.UpdatedAt = now
	s.policies[input.SidecarID] = policy
	return policy, nil
}

func (s *memorySidecarStore) CreateWatchdogAction(_ context.Context, input SidecarWatchdogActionInput) (SidecarWatchdogAction, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	action := SidecarWatchdogAction{ID: s.nextActionID, SidecarID: input.SidecarID, AuthSnapshotID: cloneIntPtr(input.AuthSnapshotID), HoldID: cloneIntPtr(input.HoldID), AuthID: cloneStringPtr(input.AuthID), AuthIndex: cloneStringPtr(input.AuthIndex), Provider: cloneStringPtr(input.Provider), ActionType: input.ActionType, Reason: cloneStringPtr(input.Reason), PreviousPriority: cloneIntPtr(input.PreviousPriority), TargetPriority: cloneIntPtr(input.TargetPriority), HoldUntil: cloneTimePtr(input.HoldUntil), Status: input.Status, ErrorMessage: cloneStringPtr(input.ErrorMessage), CreatedAt: now, UpdatedAt: now, CompletedAt: cloneTimePtr(input.CompletedAt)}
	s.nextActionID++
	s.actions[input.SidecarID] = append(s.actions[input.SidecarID], action)
	return action, nil
}

func (s *memorySidecarStore) ListWatchdogActions(_ context.Context, sidecarID int) ([]SidecarWatchdogAction, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := append([]SidecarWatchdogAction(nil), s.actions[sidecarID]...)
	for index := range items {
		items[index] = cloneWatchdogAction(items[index])
	}
	return items, nil
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

func cloneAuthSnapshot(snapshot SidecarAuthSnapshot) SidecarAuthSnapshot {
	copy := snapshot
	copy.AuthIndex = cloneStringPtr(snapshot.AuthIndex)
	copy.Provider = cloneStringPtr(snapshot.Provider)
	copy.Label = cloneStringPtr(snapshot.Label)
	copy.Status = cloneStringPtr(snapshot.Status)
	copy.StatusMessage = cloneStringPtr(snapshot.StatusMessage)
	copy.Disabled = cloneBoolPtr(snapshot.Disabled)
	copy.Unavailable = cloneBoolPtr(snapshot.Unavailable)
	copy.Priority = cloneIntPtr(snapshot.Priority)
	copy.QuotaExceeded = cloneBoolPtr(snapshot.QuotaExceeded)
	copy.QuotaReason = cloneStringPtr(snapshot.QuotaReason)
	copy.QuotaNextRecoverAt = cloneTimePtr(snapshot.QuotaNextRecoverAt)
	copy.NextRetryAfter = cloneTimePtr(snapshot.NextRetryAfter)
	copy.SuccessCount = cloneIntPtr(snapshot.SuccessCount)
	copy.FailedCount = cloneIntPtr(snapshot.FailedCount)
	copy.RecentRequestsJSON = append([]byte(nil), snapshot.RecentRequestsJSON...)
	copy.ModelStatesJSON = append([]byte(nil), snapshot.ModelStatesJSON...)
	copy.SnapshotJSON = append([]byte(nil), snapshot.SnapshotJSON...)
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

func cloneWatchdogAction(action SidecarWatchdogAction) SidecarWatchdogAction {
	copy := action
	copy.AuthSnapshotID = cloneIntPtr(action.AuthSnapshotID)
	copy.HoldID = cloneIntPtr(action.HoldID)
	copy.AuthID = cloneStringPtr(action.AuthID)
	copy.AuthIndex = cloneStringPtr(action.AuthIndex)
	copy.Provider = cloneStringPtr(action.Provider)
	copy.Reason = cloneStringPtr(action.Reason)
	copy.PreviousPriority = cloneIntPtr(action.PreviousPriority)
	copy.TargetPriority = cloneIntPtr(action.TargetPriority)
	copy.HoldUntil = cloneTimePtr(action.HoldUntil)
	copy.ErrorMessage = cloneStringPtr(action.ErrorMessage)
	copy.CompletedAt = cloneTimePtr(action.CompletedAt)
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

func cloneTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
