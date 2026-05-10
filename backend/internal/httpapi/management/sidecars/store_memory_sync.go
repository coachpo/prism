package sidecars

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
)

func (s *memorySidecarStore) UpdateSidecarSyncMetadata(_ context.Context, input SidecarSyncMetadataInput) (SidecarInstance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if input.SidecarID <= 0 || input.LastSyncAt.IsZero() {
		return SidecarInstance{}, invalidInputError("sidecar_id and last_sync_at are required")
	}
	instance, ok := s.instances[input.SidecarID]
	if !ok || instance.DeletedAt != nil {
		return SidecarInstance{}, notFoundError("sidecar instance not found")
	}
	state := strings.TrimSpace(input.ManagementAuthState)
	if state == "" {
		state = ManagementAuthStateUnknown
	}
	instance.LastSyncAt = cloneTimePtr(&input.LastSyncAt)
	instance.LastSuccessfulSyncAt = cloneTimePtr(input.LastSuccessfulSyncAt)
	instance.SnapshotStaleAfter = cloneTimePtr(input.SnapshotStaleAfter)
	instance.LastSyncError = cloneStringPtr(input.LastSyncError)
	instance.ManagementAuthState = state
	instance.AuthFailurePauseUntil = cloneTimePtr(input.AuthFailurePauseUntil)
	instance.UpdatedAt = input.LastSyncAt.UTC()
	s.instances[input.SidecarID] = instance
	return cloneSidecarInstance(instance), nil
}

func (s *memorySidecarStore) SaveAuthSnapshot(_ context.Context, input SidecarAuthSnapshotInput) (SidecarAuthSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if input.SidecarID <= 0 || strings.TrimSpace(input.AuthID) == "" || strings.TrimSpace(input.Name) == "" {
		return SidecarAuthSnapshot{}, invalidInputError("sidecar_id, auth_id, and name are required")
	}
	if err := validateSidecarSnapshotJSON(input.SnapshotJSON); err != nil {
		return SidecarAuthSnapshot{}, err
	}
	observedAt := input.ObservedAt
	if observedAt.IsZero() {
		observedAt = s.now().UTC()
	}
	snapshots := s.authSnapshots[input.SidecarID]
	if snapshots == nil {
		snapshots = map[string]SidecarAuthSnapshot{}
		s.authSnapshots[input.SidecarID] = snapshots
	}
	now := s.now().UTC()
	snapshot, exists := snapshots[strings.TrimSpace(input.AuthID)]
	if !exists {
		snapshot.ID = s.nextSnapshotID
		s.nextSnapshotID++
		snapshot.SidecarID = input.SidecarID
		snapshot.AuthID = strings.TrimSpace(input.AuthID)
		snapshot.CreatedAt = now
	}
	snapshot.AuthIndex = cloneStringPtr(input.AuthIndex)
	snapshot.Name = strings.TrimSpace(input.Name)
	snapshot.Provider = cloneStringPtr(input.Provider)
	snapshot.Label = cloneStringPtr(input.Label)
	snapshot.Status = cloneStringPtr(input.Status)
	snapshot.StatusMessage = cloneStringPtr(input.StatusMessage)
	snapshot.Disabled = cloneBoolPtr(input.Disabled)
	snapshot.Unavailable = cloneBoolPtr(input.Unavailable)
	snapshot.Priority = cloneIntPtr(input.Priority)
	snapshot.QuotaExceeded = cloneBoolPtr(input.QuotaExceeded)
	snapshot.QuotaReason = cloneStringPtr(input.QuotaReason)
	snapshot.QuotaNextRecoverAt = cloneTimePtr(input.QuotaNextRecoverAt)
	snapshot.NextRetryAfter = cloneTimePtr(input.NextRetryAfter)
	snapshot.SuccessCount = cloneIntPtr(input.SuccessCount)
	snapshot.FailedCount = cloneIntPtr(input.FailedCount)
	snapshot.RecentRequestsJSON = memoryJSON(input.RecentRequestsJSON, "[]")
	snapshot.ModelStatesJSON = memoryJSON(input.ModelStatesJSON, "{}")
	snapshot.SnapshotJSON = memoryJSON(input.SnapshotJSON, "{}")
	snapshot.ObservedAt = observedAt.UTC()
	snapshot.UpdatedAt = now
	snapshots[snapshot.AuthID] = snapshot
	return cloneAuthSnapshot(snapshot), nil
}

func (s *memorySidecarStore) ReplaceAuthSnapshots(_ context.Context, sidecarID int, inputs []SidecarAuthSnapshotInput) ([]SidecarAuthSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	normalized, err := validateAuthSnapshotReplacementInputs(sidecarID, inputs)
	if err != nil {
		return nil, err
	}
	snapshots := make(map[string]SidecarAuthSnapshot, len(normalized))
	records := make([]SidecarAuthSnapshot, 0, len(normalized))
	now := s.now().UTC()
	for _, input := range normalized {
		observedAt := input.ObservedAt
		if observedAt.IsZero() {
			observedAt = now
		}
		snapshot := SidecarAuthSnapshot{
			ID:                 s.nextSnapshotID,
			SidecarID:          sidecarID,
			AuthID:             input.AuthID,
			AuthIndex:          cloneStringPtr(input.AuthIndex),
			Name:               input.Name,
			Provider:           cloneStringPtr(input.Provider),
			Label:              cloneStringPtr(input.Label),
			Status:             cloneStringPtr(input.Status),
			StatusMessage:      cloneStringPtr(input.StatusMessage),
			Disabled:           cloneBoolPtr(input.Disabled),
			Unavailable:        cloneBoolPtr(input.Unavailable),
			Priority:           cloneIntPtr(input.Priority),
			QuotaExceeded:      cloneBoolPtr(input.QuotaExceeded),
			QuotaReason:        cloneStringPtr(input.QuotaReason),
			QuotaNextRecoverAt: cloneTimePtr(input.QuotaNextRecoverAt),
			NextRetryAfter:     cloneTimePtr(input.NextRetryAfter),
			SuccessCount:       cloneIntPtr(input.SuccessCount),
			FailedCount:        cloneIntPtr(input.FailedCount),
			RecentRequestsJSON: memoryJSON(input.RecentRequestsJSON, "[]"),
			ModelStatesJSON:    memoryJSON(input.ModelStatesJSON, "{}"),
			SnapshotJSON:       memoryJSON(input.SnapshotJSON, "{}"),
			ObservedAt:         observedAt.UTC(),
			CreatedAt:          now,
			UpdatedAt:          now,
		}
		s.nextSnapshotID++
		snapshots[snapshot.AuthID] = snapshot
		records = append(records, cloneAuthSnapshot(snapshot))
	}
	s.authSnapshots[sidecarID] = snapshots
	return records, nil
}

func (s *memorySidecarStore) ListAuthSnapshots(_ context.Context, sidecarID int) ([]SidecarAuthSnapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snapshots := s.authSnapshots[sidecarID]
	items := make([]SidecarAuthSnapshot, 0, len(snapshots))
	for _, snapshot := range snapshots {
		items = append(items, cloneAuthSnapshot(snapshot))
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Name == items[j].Name {
			return items[i].AuthID < items[j].AuthID
		}
		return items[i].Name < items[j].Name
	})
	return items, nil
}

func (s *memorySidecarStore) SaveProviderSnapshot(_ context.Context, input SidecarProviderSnapshotInput) (SidecarProviderSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if input.SidecarID <= 0 || strings.TrimSpace(input.ProviderKey) == "" || strings.TrimSpace(input.ProviderItemKey) == "" {
		return SidecarProviderSnapshot{}, invalidInputError("sidecar_id, provider_key, and provider_item_key are required")
	}
	if err := validateSidecarSnapshotJSON(input.SnapshotJSON); err != nil {
		return SidecarProviderSnapshot{}, err
	}
	observedAt := input.ObservedAt
	if observedAt.IsZero() {
		observedAt = s.now().UTC()
	}
	snapshots := s.providerSnapshots[input.SidecarID]
	if snapshots == nil {
		snapshots = map[string]SidecarProviderSnapshot{}
		s.providerSnapshots[input.SidecarID] = snapshots
	}
	providerKey := strings.TrimSpace(input.ProviderKey)
	itemKey := strings.TrimSpace(input.ProviderItemKey)
	key := memoryProviderSnapshotKey(providerKey, itemKey)
	now := s.now().UTC()
	snapshot, exists := snapshots[key]
	if !exists {
		snapshot.ID = s.nextSnapshotID
		s.nextSnapshotID++
		snapshot.SidecarID = input.SidecarID
		snapshot.ProviderKey = providerKey
		snapshot.ProviderItemKey = itemKey
		snapshot.CreatedAt = now
	}
	snapshot.Name = cloneStringPtr(input.Name)
	snapshot.Label = cloneStringPtr(input.Label)
	snapshot.Status = cloneStringPtr(input.Status)
	snapshot.Disabled = cloneBoolPtr(input.Disabled)
	snapshot.SnapshotJSON = memoryJSON(input.SnapshotJSON, "{}")
	snapshot.ObservedAt = observedAt.UTC()
	snapshot.UpdatedAt = now
	snapshots[key] = snapshot
	return cloneProviderSnapshot(snapshot), nil
}

func (s *memorySidecarStore) ReplaceProviderSnapshots(_ context.Context, sidecarID int, providerKey string, inputs []SidecarProviderSnapshotInput) ([]SidecarProviderSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	providerKey = strings.TrimSpace(providerKey)
	if sidecarID <= 0 || providerKey == "" {
		return nil, invalidInputError("sidecar_id and provider_key are required")
	}
	for _, input := range inputs {
		if input.SidecarID != sidecarID || strings.TrimSpace(input.ProviderKey) != providerKey || strings.TrimSpace(input.ProviderItemKey) == "" {
			return nil, invalidInputError("provider replacement input does not match provider batch")
		}
		if err := validateSidecarSnapshotJSON(input.SnapshotJSON); err != nil {
			return nil, err
		}
	}
	snapshots := s.providerSnapshots[sidecarID]
	if snapshots == nil {
		snapshots = map[string]SidecarProviderSnapshot{}
		s.providerSnapshots[sidecarID] = snapshots
	}
	for key, snapshot := range snapshots {
		if snapshot.ProviderKey == providerKey {
			delete(snapshots, key)
		}
	}
	records := make([]SidecarProviderSnapshot, 0, len(inputs))
	now := s.now().UTC()
	for _, input := range inputs {
		observedAt := input.ObservedAt
		if observedAt.IsZero() {
			observedAt = now
		}
		itemKey := strings.TrimSpace(input.ProviderItemKey)
		snapshot := SidecarProviderSnapshot{
			ID:              s.nextSnapshotID,
			SidecarID:       sidecarID,
			ProviderKey:     providerKey,
			ProviderItemKey: itemKey,
			Name:            cloneStringPtr(input.Name),
			Label:           cloneStringPtr(input.Label),
			Status:          cloneStringPtr(input.Status),
			Disabled:        cloneBoolPtr(input.Disabled),
			SnapshotJSON:    memoryJSON(input.SnapshotJSON, "{}"),
			ObservedAt:      observedAt.UTC(),
			CreatedAt:       now,
			UpdatedAt:       now,
		}
		s.nextSnapshotID++
		snapshots[memoryProviderSnapshotKey(providerKey, itemKey)] = snapshot
		records = append(records, cloneProviderSnapshot(snapshot))
	}
	return records, nil
}

func (s *memorySidecarStore) ListProviderSnapshots(_ context.Context, sidecarID int) ([]SidecarProviderSnapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snapshots := s.providerSnapshots[sidecarID]
	items := make([]SidecarProviderSnapshot, 0, len(snapshots))
	for _, snapshot := range snapshots {
		items = append(items, cloneProviderSnapshot(snapshot))
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].ProviderKey == items[j].ProviderKey {
			return items[i].ProviderItemKey < items[j].ProviderItemKey
		}
		return items[i].ProviderKey < items[j].ProviderKey
	})
	return items, nil
}

func memoryProviderSnapshotKey(providerKey string, itemKey string) string {
	return providerKey + "\x00" + itemKey
}

func memoryJSON(value json.RawMessage, fallback string) json.RawMessage {
	trimmed := strings.TrimSpace(string(value))
	if trimmed == "" {
		trimmed = fallback
	}
	return json.RawMessage(trimmed)
}
