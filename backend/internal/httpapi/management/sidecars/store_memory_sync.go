package sidecars

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"
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

func (s *memorySidecarStore) CreateWatchdogPendingAction(_ context.Context, input SidecarWatchdogPendingActionInput) (SidecarWatchdogPendingAction, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	action, err := s.createWatchdogPendingActionLocked(input)
	if err != nil {
		return SidecarWatchdogPendingAction{}, err
	}
	return cloneWatchdogPendingAction(action), nil
}

func (s *memorySidecarStore) UpdateWatchdogPendingAction(_ context.Context, id int, input SidecarWatchdogPendingActionInput) (SidecarWatchdogPendingAction, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	action, err := s.updateWatchdogPendingActionLocked(id, input)
	if err != nil {
		return SidecarWatchdogPendingAction{}, err
	}
	return cloneWatchdogPendingAction(action), nil
}

func (s *memorySidecarStore) ListWatchdogPendingActions(_ context.Context, sidecarID int) ([]SidecarWatchdogPendingAction, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if sidecarID <= 0 {
		return nil, invalidInputError("sidecar_id is required")
	}
	items := append([]SidecarWatchdogPendingAction(nil), s.pendingActions[sidecarID]...)
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID < items[j].ID
		}
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	for index := range items {
		items[index] = cloneWatchdogPendingAction(items[index])
	}
	return items, nil
}

func (s *memorySidecarStore) ListPendingWatchdogActions(ctx context.Context, sidecarID int) ([]SidecarWatchdogPendingAction, error) {
	return s.ListWatchdogPendingActions(ctx, sidecarID)
}

func (s *memorySidecarStore) ClaimWatchdogPendingActions(_ context.Context, sidecarID int, limit int) ([]SidecarWatchdogPendingAction, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sidecarID <= 0 {
		return nil, invalidInputError("sidecar_id is required")
	}
	items := append([]SidecarWatchdogPendingAction(nil), s.pendingActions[sidecarID]...)
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID < items[j].ID
		}
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	now := s.now().UTC()
	claimed := make([]SidecarWatchdogPendingAction, 0, len(items))
	for _, item := range items {
		for index, action := range s.pendingActions[sidecarID] {
			if action.ID != item.ID {
				continue
			}
			action.AttemptCount++
			action.LastAttemptAt = &now
			action.LastErrorMessage = nil
			action.UpdatedAt = now
			s.pendingActions[sidecarID][index] = action
			claimed = append(claimed, cloneWatchdogPendingAction(action))
			break
		}
	}
	return claimed, nil
}

func (s *memorySidecarStore) DeleteWatchdogPendingAction(_ context.Context, sidecarID int, id int) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sidecarID <= 0 || id <= 0 {
		return false, invalidInputError("sidecar_id and id are required")
	}
	actions := s.pendingActions[sidecarID]
	for index, action := range actions {
		if action.ID != id {
			continue
		}
		s.pendingActions[sidecarID] = append(actions[:index], actions[index+1:]...)
		return true, nil
	}
	return false, nil
}

func (s *memorySidecarStore) DeletePendingWatchdogAction(ctx context.Context, sidecarID int, id int) (bool, error) {
	return s.DeleteWatchdogPendingAction(ctx, sidecarID, id)
}

func (s *memorySidecarStore) DeleteWatchdogPendingActionByHistoryKey(_ context.Context, sidecarID int, createdAt time.Time, id int) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sidecarID <= 0 || id <= 0 || createdAt.IsZero() {
		return false, invalidInputError("sidecar_id, created_at, and id are required")
	}
	actions := s.pendingActions[sidecarID]
	for index, action := range actions {
		if action.ActionHistoryID != id || !action.ActionHistoryCreatedAt.Equal(createdAt.UTC()) {
			continue
		}
		s.pendingActions[sidecarID] = append(actions[:index], actions[index+1:]...)
		return true, nil
	}
	return false, nil
}

func (s *memorySidecarStore) CreateQuotaScanRun(_ context.Context, input SidecarQuotaScanRunInput) (SidecarQuotaScanRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	run, err := s.createQuotaScanRunLocked(input)
	if err != nil {
		return SidecarQuotaScanRun{}, err
	}
	return cloneQuotaScanRun(run), nil
}

func (s *memorySidecarStore) UpdateQuotaScanRun(_ context.Context, id int, input SidecarQuotaScanRunInput) (SidecarQuotaScanRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	run, err := s.updateQuotaScanRunLocked(id, input)
	if err != nil {
		return SidecarQuotaScanRun{}, err
	}
	return cloneQuotaScanRun(run), nil
}

func (s *memorySidecarStore) GetQuotaScanRun(_ context.Context, sidecarID int, id int) (SidecarQuotaScanRun, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if sidecarID <= 0 || id <= 0 {
		return SidecarQuotaScanRun{}, false, invalidInputError("sidecar_id and id are required")
	}
	index := s.findQuotaScanRunIndexLocked(sidecarID, id)
	if index < 0 {
		return SidecarQuotaScanRun{}, false, nil
	}
	return cloneQuotaScanRun(s.quotaScanRuns[sidecarID][index]), true, nil
}

func (s *memorySidecarStore) ListQuotaScanRuns(_ context.Context, sidecarID int) ([]SidecarQuotaScanRun, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if sidecarID <= 0 {
		return nil, invalidInputError("sidecar_id is required")
	}
	items := append([]SidecarQuotaScanRun(nil), s.quotaScanRuns[sidecarID]...)
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	for index := range items {
		items[index] = cloneQuotaScanRun(items[index])
	}
	return items, nil
}

func (s *memorySidecarStore) UpsertAuthQuotaState(_ context.Context, input SidecarAuthQuotaStateInput) (SidecarAuthQuotaState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := s.upsertAuthQuotaStateLocked(input)
	if err != nil {
		return SidecarAuthQuotaState{}, err
	}
	return cloneAuthQuotaState(state), nil
}

func (s *memorySidecarStore) GetAuthQuotaState(_ context.Context, sidecarID int, authID string) (SidecarAuthQuotaState, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	trimmedAuthID := strings.TrimSpace(authID)
	if sidecarID <= 0 || trimmedAuthID == "" {
		return SidecarAuthQuotaState{}, false, invalidInputError("sidecar_id and auth_id are required")
	}
	state, ok := s.quotaStates[sidecarID][trimmedAuthID]
	if !ok {
		return SidecarAuthQuotaState{}, false, nil
	}
	return cloneAuthQuotaState(state), true, nil
}

func (s *memorySidecarStore) ListAuthQuotaStates(_ context.Context, sidecarID int) ([]SidecarAuthQuotaState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if sidecarID <= 0 {
		return nil, invalidInputError("sidecar_id is required")
	}
	states := s.quotaStates[sidecarID]
	items := make([]SidecarAuthQuotaState, 0, len(states))
	for _, state := range states {
		items = append(items, cloneAuthQuotaState(state))
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].AuthID < items[j].AuthID
	})
	return items, nil
}

func (s *memorySidecarStore) CreateWatchdogProbeObservation(_ context.Context, input SidecarWatchdogProbeObservationInput) (SidecarWatchdogProbeObservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	observation, err := s.createWatchdogProbeObservationLocked(input)
	if err != nil {
		return SidecarWatchdogProbeObservation{}, err
	}
	return cloneWatchdogProbeObservation(observation), nil
}

func (s *memorySidecarStore) ListWatchdogProbeObservations(_ context.Context, sidecarID int, limit int) ([]SidecarWatchdogProbeObservation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if sidecarID <= 0 {
		return nil, invalidInputError("sidecar_id is required")
	}
	items := append([]SidecarWatchdogProbeObservation(nil), s.probeObservations[sidecarID]...)
	sort.Slice(items, func(i, j int) bool {
		if items[i].ProbedAt.Equal(items[j].ProbedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].ProbedAt.After(items[j].ProbedAt)
	})
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	for index := range items {
		items[index] = cloneWatchdogProbeObservation(items[index])
	}
	return items, nil
}

func (s *memorySidecarStore) CleanupWatchdogProbeObservations(_ context.Context) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := s.now().UTC().Add(-time.Duration(WatchdogProbeObservationRetentionDays) * 24 * time.Hour)
	var deleted int64
	for sidecarID, observations := range s.probeObservations {
		kept := observations[:0]
		for _, observation := range observations {
			if observation.ProbedAt.Before(cutoff) {
				deleted++
				continue
			}
			kept = append(kept, observation)
		}
		s.probeObservations[sidecarID] = kept
	}
	return deleted, nil
}

func (s *memorySidecarStore) PersistWatchdogProbeDecision(ctx context.Context, decision SidecarWatchdogProbeDecision) (SidecarWatchdogProbeDecisionResult, error) {
	return s.PersistQuotaProbeDecision(ctx, decision)
}

func (s *memorySidecarStore) PersistQuotaProbeDecision(_ context.Context, decision SidecarQuotaPersistDecision) (SidecarQuotaPersistResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if decision.SidecarID <= 0 {
		return SidecarQuotaPersistResult{}, invalidInputError("sidecar_id is required")
	}
	if _, ok := s.instances[decision.SidecarID]; !ok {
		return SidecarQuotaPersistResult{}, notFoundError("sidecar instance not found")
	}
	now := s.now().UTC()
	normalizedObservations := make([]SidecarWatchdogProbeObservationInput, 0, len(decision.Observations))
	for _, input := range decision.Observations {
		if input.SidecarID == 0 {
			input.SidecarID = decision.SidecarID
		}
		if input.SidecarID != decision.SidecarID {
			return SidecarQuotaPersistResult{}, invalidInputError("probe observation sidecar_id must match decision sidecar_id")
		}
		normalized, err := normalizeWatchdogProbeObservationInput(input, now)
		if err != nil {
			return SidecarQuotaPersistResult{}, err
		}
		normalizedObservations = append(normalizedObservations, normalized)
	}
	normalizedQuotaStates := make([]SidecarAuthQuotaStateInput, 0, len(decision.QuotaStates))
	for _, input := range decision.QuotaStates {
		stateInput, err := quotaPersistStateInput(decision.SidecarID, input)
		if err != nil {
			return SidecarQuotaPersistResult{}, err
		}
		if err := s.validateAuthQuotaStateInputLocked(stateInput); err != nil {
			return SidecarQuotaPersistResult{}, err
		}
		normalizedQuotaStates = append(normalizedQuotaStates, stateInput)
	}
	var createHoldInput *SidecarWatchdogHoldInput
	if decision.CreateHold != nil {
		input := *decision.CreateHold
		if input.SidecarID == 0 {
			input.SidecarID = decision.SidecarID
		}
		if err := s.validateWatchdogHoldCreateLocked(input, decision.SidecarID); err != nil {
			return SidecarQuotaPersistResult{}, err
		}
		createHoldInput = &input
	}
	var updateHoldInput *SidecarWatchdogProbeHoldUpdate
	if decision.UpdateHold != nil {
		input := decision.UpdateHold.Input
		if input.SidecarID == 0 {
			input.SidecarID = decision.SidecarID
		}
		if input.SidecarID != decision.SidecarID {
			return SidecarQuotaPersistResult{}, invalidInputError("updated hold sidecar_id must match decision sidecar_id")
		}
		if err := validateWatchdogHoldUpdateInput(decision.UpdateHold.ID, input); err != nil {
			return SidecarQuotaPersistResult{}, err
		}
		if !s.watchdogHoldExistsLocked(decision.UpdateHold.ID, decision.SidecarID) {
			return SidecarQuotaPersistResult{}, notFoundError("sidecar watchdog hold not found")
		}
		updateHoldInput = &SidecarWatchdogProbeHoldUpdate{ID: decision.UpdateHold.ID, Input: input}
	}
	scanRunIndex := -1
	if decision.ScanRunID != nil {
		scanRunIndex = s.findQuotaScanRunIndexLocked(decision.SidecarID, *decision.ScanRunID)
		if scanRunIndex < 0 {
			return SidecarQuotaPersistResult{}, notFoundError("sidecar quota scan run not found")
		}
	}
	result := SidecarQuotaPersistResult{
		Observations: make([]SidecarWatchdogProbeObservation, 0, len(normalizedObservations)),
		QuotaStates:  make([]SidecarAuthQuotaState, 0, len(normalizedObservations)+len(normalizedQuotaStates)),
	}
	for _, input := range normalizedObservations {
		observation, err := s.createWatchdogProbeObservationLocked(input)
		if err != nil {
			return SidecarQuotaPersistResult{}, err
		}
		result.Observations = append(result.Observations, cloneWatchdogProbeObservation(observation))
		state, err := s.upsertAuthQuotaStateLocked(authQuotaStateInputFromProbeObservation(observation))
		if err != nil {
			return SidecarQuotaPersistResult{}, err
		}
		result.QuotaStates = append(result.QuotaStates, cloneAuthQuotaState(state))
	}
	for _, input := range normalizedQuotaStates {
		state, err := s.upsertAuthQuotaStateLocked(input)
		if err != nil {
			return SidecarQuotaPersistResult{}, err
		}
		result.QuotaStates = append(result.QuotaStates, cloneAuthQuotaState(state))
	}
	if scanRunIndex >= 0 && len(result.Observations) > 0 {
		run := s.applyQuotaScanRunObservationBatchLocked(decision.SidecarID, scanRunIndex, result.Observations, decision.CursorAuthID)
		cloned := cloneQuotaScanRun(run)
		result.ScanRun = &cloned
	}
	if createHoldInput != nil {
		hold, err := s.createWatchdogHoldLocked(*createHoldInput)
		if err != nil {
			return SidecarQuotaPersistResult{}, err
		}
		cloned := cloneWatchdogHold(hold)
		result.CreatedHold = &cloned
	}
	if updateHoldInput != nil {
		hold, err := s.updateWatchdogHoldLocked(updateHoldInput.ID, updateHoldInput.Input)
		if err != nil {
			return SidecarQuotaPersistResult{}, err
		}
		cloned := cloneWatchdogHold(hold)
		result.UpdatedHold = &cloned
	}
	if len(result.Observations) > 0 {
		policy := s.setWatchdogProbeBatchCompletionLocked(decision.SidecarID)
		cloned := cloneWatchdogPolicy(policy)
		result.Policy = &cloned
	}
	return result, nil
}

func (s *memorySidecarStore) ListDueWatchdogHolds(_ context.Context, sidecarID int, dueAt time.Time) ([]SidecarWatchdogHold, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if sidecarID <= 0 {
		return nil, invalidInputError("sidecar_id is required")
	}
	if dueAt.IsZero() {
		dueAt = s.now().UTC()
	}
	items := make([]SidecarWatchdogHold, 0)
	for _, hold := range s.holds[sidecarID] {
		if hold.HoldUntil == nil || hold.HoldUntil.After(dueAt) {
			continue
		}
		if hold.Status == WatchdogHoldStatusActive {
			items = append(items, cloneWatchdogHold(hold))
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].HoldUntil.Equal(*items[j].HoldUntil) {
			return items[i].ID < items[j].ID
		}
		return items[i].HoldUntil.Before(*items[j].HoldUntil)
	})
	return items, nil
}

func (s *memorySidecarStore) createWatchdogPendingActionLocked(input SidecarWatchdogPendingActionInput) (SidecarWatchdogPendingAction, error) {
	if err := s.validateWatchdogPendingActionInputLocked(input, 0); err != nil {
		return SidecarWatchdogPendingAction{}, err
	}
	now := s.now().UTC()
	action := SidecarWatchdogPendingAction{
		ID:                     s.nextPendingActionID,
		SidecarID:              input.SidecarID,
		HoldID:                 cloneIntPtr(input.HoldID),
		ActionHistoryCreatedAt: input.ActionHistoryCreatedAt.UTC(),
		ActionHistoryID:        input.ActionHistoryID,
		AuthID:                 cloneStringPtr(input.AuthID),
		AuthName:               cloneStringPtr(input.AuthName),
		AuthIndex:              cloneStringPtr(input.AuthIndex),
		Provider:               cloneStringPtr(input.Provider),
		ActionType:             strings.TrimSpace(input.ActionType),
		Reason:                 cloneStringPtr(input.Reason),
		PreviousPriority:       cloneIntPtr(input.PreviousPriority),
		TargetPriority:         cloneIntPtr(input.TargetPriority),
		HoldUntil:              cloneTimePtr(input.HoldUntil),
		AttemptCount:           input.AttemptCount,
		LastAttemptAt:          cloneTimePtr(input.LastAttemptAt),
		LastErrorMessage:       cloneStringPtr(input.LastErrorMessage),
		CreatedAt:              now,
		UpdatedAt:              now,
	}
	s.nextPendingActionID++
	s.pendingActions[input.SidecarID] = append(s.pendingActions[input.SidecarID], action)
	return action, nil
}

func (s *memorySidecarStore) updateWatchdogPendingActionLocked(id int, input SidecarWatchdogPendingActionInput) (SidecarWatchdogPendingAction, error) {
	if id <= 0 {
		return SidecarWatchdogPendingAction{}, invalidInputError("id is required")
	}
	if err := s.validateWatchdogPendingActionInputLocked(input, id); err != nil {
		return SidecarWatchdogPendingAction{}, err
	}
	for index, action := range s.pendingActions[input.SidecarID] {
		if action.ID != id {
			continue
		}
		action.HoldID = cloneIntPtr(input.HoldID)
		action.ActionHistoryCreatedAt = input.ActionHistoryCreatedAt.UTC()
		action.ActionHistoryID = input.ActionHistoryID
		action.AuthID = cloneStringPtr(input.AuthID)
		action.AuthName = cloneStringPtr(input.AuthName)
		action.AuthIndex = cloneStringPtr(input.AuthIndex)
		action.Provider = cloneStringPtr(input.Provider)
		action.ActionType = strings.TrimSpace(input.ActionType)
		action.Reason = cloneStringPtr(input.Reason)
		action.PreviousPriority = cloneIntPtr(input.PreviousPriority)
		action.TargetPriority = cloneIntPtr(input.TargetPriority)
		action.HoldUntil = cloneTimePtr(input.HoldUntil)
		action.AttemptCount = input.AttemptCount
		action.LastAttemptAt = cloneTimePtr(input.LastAttemptAt)
		action.LastErrorMessage = cloneStringPtr(input.LastErrorMessage)
		action.UpdatedAt = s.now().UTC()
		s.pendingActions[input.SidecarID][index] = action
		return action, nil
	}
	return SidecarWatchdogPendingAction{}, notFoundError("sidecar watchdog pending action not found")
}

func (s *memorySidecarStore) validateWatchdogPendingActionInputLocked(input SidecarWatchdogPendingActionInput, existingID int) error {
	if input.SidecarID <= 0 || input.ActionHistoryCreatedAt.IsZero() || input.ActionHistoryID <= 0 || strings.TrimSpace(input.ActionType) == "" {
		return invalidInputError("sidecar_id, action_history_created_at, action_history_id, and action_type are required")
	}
	if input.AttemptCount < 0 {
		return invalidInputError("attempt_count must be non-negative")
	}
	if _, ok := s.instances[input.SidecarID]; !ok {
		return notFoundError("sidecar instance not found")
	}
	for _, action := range s.pendingActions[input.SidecarID] {
		if action.ID == existingID {
			continue
		}
		if action.ActionHistoryID == input.ActionHistoryID && action.ActionHistoryCreatedAt.Equal(input.ActionHistoryCreatedAt.UTC()) {
			return invalidInputError("pending action already exists for action history row")
		}
	}
	return nil
}

func (s *memorySidecarStore) createQuotaScanRunLocked(input SidecarQuotaScanRunInput) (SidecarQuotaScanRun, error) {
	if err := s.validateQuotaScanRunInputLocked(input, 0); err != nil {
		return SidecarQuotaScanRun{}, err
	}
	now := s.now().UTC()
	run := SidecarQuotaScanRun{
		ID:                 s.nextScanRunID,
		SidecarID:          input.SidecarID,
		ScanType:           strings.TrimSpace(input.ScanType),
		Status:             strings.TrimSpace(input.Status),
		RequestedBy:        cloneStringPtr(input.RequestedBy),
		CursorAuthID:       cloneStringPtr(input.CursorAuthID),
		PlannedCount:       input.PlannedCount,
		AttemptedCount:     input.AttemptedCount,
		UsingCount:         input.UsingCount,
		QuotaExceededCount: input.QuotaExceededCount,
		ErrorCount:         input.ErrorCount,
		SkippedCount:       input.SkippedCount,
		CancelRequestedAt:  cloneTimePtr(input.CancelRequestedAt),
		StartedAt:          cloneTimePtr(input.StartedAt),
		CompletedAt:        cloneTimePtr(input.CompletedAt),
		LastErrorCode:      cloneStringPtr(input.LastErrorCode),
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	s.nextScanRunID++
	s.quotaScanRuns[input.SidecarID] = append(s.quotaScanRuns[input.SidecarID], run)
	return run, nil
}

func (s *memorySidecarStore) updateQuotaScanRunLocked(id int, input SidecarQuotaScanRunInput) (SidecarQuotaScanRun, error) {
	if id <= 0 {
		return SidecarQuotaScanRun{}, invalidInputError("id is required")
	}
	if err := s.validateQuotaScanRunInputLocked(input, id); err != nil {
		return SidecarQuotaScanRun{}, err
	}
	index := s.findQuotaScanRunIndexLocked(input.SidecarID, id)
	if index < 0 {
		return SidecarQuotaScanRun{}, notFoundError("sidecar quota scan run not found")
	}
	run := s.quotaScanRuns[input.SidecarID][index]
	run.ScanType = strings.TrimSpace(input.ScanType)
	run.Status = strings.TrimSpace(input.Status)
	run.RequestedBy = cloneStringPtr(input.RequestedBy)
	run.CursorAuthID = cloneStringPtr(input.CursorAuthID)
	run.PlannedCount = input.PlannedCount
	run.AttemptedCount = input.AttemptedCount
	run.UsingCount = input.UsingCount
	run.QuotaExceededCount = input.QuotaExceededCount
	run.ErrorCount = input.ErrorCount
	run.SkippedCount = input.SkippedCount
	run.CancelRequestedAt = cloneTimePtr(input.CancelRequestedAt)
	run.StartedAt = cloneTimePtr(input.StartedAt)
	run.CompletedAt = cloneTimePtr(input.CompletedAt)
	run.LastErrorCode = cloneStringPtr(input.LastErrorCode)
	run.UpdatedAt = s.now().UTC()
	s.quotaScanRuns[input.SidecarID][index] = run
	return run, nil
}

func (s *memorySidecarStore) validateQuotaScanRunInputLocked(input SidecarQuotaScanRunInput, existingID int) error {
	if input.SidecarID <= 0 || strings.TrimSpace(input.ScanType) == "" || strings.TrimSpace(input.Status) == "" {
		return invalidInputError("sidecar_id, scan_type, and status are required")
	}
	if _, ok := s.instances[input.SidecarID]; !ok {
		return notFoundError("sidecar instance not found")
	}
	if !validQuotaScanType(input.ScanType) {
		return invalidInputError("scan_type is not supported")
	}
	if !validQuotaScanStatus(input.Status) {
		return invalidInputError("scan status is not supported")
	}
	if input.PlannedCount < 0 || input.AttemptedCount < 0 || input.UsingCount < 0 || input.QuotaExceededCount < 0 || input.ErrorCount < 0 || input.SkippedCount < 0 {
		return invalidInputError("scan counters must be non-negative")
	}
	if quotaScanStatusActive(input.Status) {
		for _, run := range s.quotaScanRuns[input.SidecarID] {
			if run.ID != existingID && quotaScanStatusActive(run.Status) {
				return invalidInputError("active quota scan run already exists for sidecar")
			}
		}
	}
	return nil
}

func (s *memorySidecarStore) findQuotaScanRunIndexLocked(sidecarID int, id int) int {
	for index, run := range s.quotaScanRuns[sidecarID] {
		if run.ID == id {
			return index
		}
	}
	return -1
}

func (s *memorySidecarStore) applyQuotaScanRunObservationBatchLocked(sidecarID int, index int, observations []SidecarWatchdogProbeObservation, cursorAuthID *string) SidecarQuotaScanRun {
	run := s.quotaScanRuns[sidecarID][index]
	for _, observation := range observations {
		run.AttemptedCount++
		switch quotaBandFromProbeObservation(observation) {
		case quotaBandUsing:
			run.UsingCount++
		case quotaBandQuotaExceeded:
			run.QuotaExceededCount++
		case quotaBandError:
			run.ErrorCount++
		default:
			run.SkippedCount++
		}
	}
	if cursorAuthID != nil {
		run.CursorAuthID = cloneStringPtr(cursorAuthID)
	}
	run.UpdatedAt = s.now().UTC()
	s.quotaScanRuns[sidecarID][index] = run
	return run
}

func validQuotaScanType(value string) bool {
	switch strings.TrimSpace(value) {
	case "initial", "manual", "scheduled":
		return true
	default:
		return false
	}
}

func validQuotaScanStatus(value string) bool {
	switch strings.TrimSpace(value) {
	case "queued", "running", "completed", "cancelled", "failed":
		return true
	default:
		return false
	}
}

func quotaScanStatusActive(value string) bool {
	switch strings.TrimSpace(value) {
	case "queued", "running":
		return true
	default:
		return false
	}
}

func (s *memorySidecarStore) createWatchdogProbeObservationLocked(input SidecarWatchdogProbeObservationInput) (SidecarWatchdogProbeObservation, error) {
	normalized, err := normalizeWatchdogProbeObservationInput(input, s.now().UTC())
	if err != nil {
		return SidecarWatchdogProbeObservation{}, err
	}
	if _, ok := s.instances[normalized.SidecarID]; !ok {
		return SidecarWatchdogProbeObservation{}, notFoundError("sidecar instance not found")
	}
	now := s.now().UTC()
	observation := SidecarWatchdogProbeObservation{ID: s.nextObservationID, SidecarID: normalized.SidecarID, AuthID: normalized.AuthID, AuthIndex: cloneStringPtr(normalized.AuthIndex), Provider: cloneStringPtr(normalized.Provider), ProbedAt: normalized.ProbedAt.UTC(), ProbeStatus: normalized.ProbeStatus, UpstreamStatusCode: cloneIntPtr(normalized.UpstreamStatusCode), QuotaBand: normalized.QuotaBand, QuotaExceeded: normalized.QuotaExceeded, ReasonCode: cloneStringPtr(normalized.ReasonCode), QuotaResetAt: cloneTimePtr(normalized.QuotaResetAt), BlockingWindow: cloneStringPtr(normalized.BlockingWindow), WindowsJSON: memoryJSON(normalized.WindowsJSON, "[]"), ErrorCode: cloneStringPtr(normalized.ErrorCode), CreatedAt: now}
	s.nextObservationID++
	s.probeObservations[normalized.SidecarID] = append(s.probeObservations[normalized.SidecarID], observation)
	return observation, nil
}

func (s *memorySidecarStore) upsertAuthQuotaStateLocked(input SidecarAuthQuotaStateInput) (SidecarAuthQuotaState, error) {
	if err := s.validateAuthQuotaStateInputLocked(input); err != nil {
		return SidecarAuthQuotaState{}, err
	}
	now := s.now().UTC()
	states := s.quotaStates[input.SidecarID]
	if states == nil {
		states = map[string]SidecarAuthQuotaState{}
		s.quotaStates[input.SidecarID] = states
	}
	authID := strings.TrimSpace(input.AuthID)
	state, exists := states[authID]
	if !exists {
		state = SidecarAuthQuotaState{SidecarID: input.SidecarID, AuthID: authID, CreatedAt: now}
	}
	if input.AuthIndex != nil {
		state.AuthIndex = cloneStringPtr(input.AuthIndex)
	}
	if input.AuthName != nil {
		state.AuthName = cloneStringPtr(input.AuthName)
	}
	if input.Provider != nil {
		state.Provider = cloneStringPtr(input.Provider)
	}
	if input.SnapshotObservedAt != nil {
		state.SnapshotObservedAt = cloneTimePtr(input.SnapshotObservedAt)
	}
	state.QuotaBand = strings.TrimSpace(input.QuotaBand)
	state.ProbeStatus = cloneStringPtr(input.ProbeStatus)
	state.QuotaExceeded = state.QuotaBand == quotaBandQuotaExceeded || input.QuotaExceeded
	state.ReasonCode = cloneStringPtr(input.ReasonCode)
	state.QuotaResetAt = cloneTimePtr(input.QuotaResetAt)
	state.BlockingWindow = cloneStringPtr(input.BlockingWindow)
	state.LastObservationID = cloneIntPtr(input.LastObservationID)
	state.LastProbedAt = cloneTimePtr(input.LastProbedAt)
	state.LastErrorCode = cloneStringPtr(input.LastErrorCode)
	state.UpdatedAt = now
	states[authID] = state
	return state, nil
}

func (s *memorySidecarStore) validateAuthQuotaStateInputLocked(input SidecarAuthQuotaStateInput) error {
	if input.SidecarID <= 0 || strings.TrimSpace(input.AuthID) == "" || strings.TrimSpace(input.QuotaBand) == "" {
		return invalidInputError("sidecar_id, auth_id, and quota_band are required")
	}
	if _, ok := s.instances[input.SidecarID]; !ok {
		return notFoundError("sidecar instance not found")
	}
	if !validAuthQuotaBand(input.QuotaBand) {
		return invalidInputError("quota band is not supported")
	}
	return nil
}

func authQuotaStateInputFromProbeObservation(observation SidecarWatchdogProbeObservation) SidecarAuthQuotaStateInput {
	lastObservationID := observation.ID
	lastProbedAt := observation.ProbedAt
	quotaBand := quotaBandFromProbeObservation(observation)
	return SidecarAuthQuotaStateInput{
		SidecarID:         observation.SidecarID,
		AuthID:            observation.AuthID,
		AuthIndex:         cloneStringPtr(observation.AuthIndex),
		Provider:          cloneStringPtr(observation.Provider),
		QuotaBand:         quotaBand,
		ProbeStatus:       stringPtrFromNonEmpty(observation.ProbeStatus),
		QuotaExceeded:     quotaBand == quotaBandQuotaExceeded || observation.QuotaExceeded,
		ReasonCode:        cloneStringPtr(observation.ReasonCode),
		QuotaResetAt:      cloneTimePtr(observation.QuotaResetAt),
		BlockingWindow:    cloneStringPtr(observation.BlockingWindow),
		LastObservationID: &lastObservationID,
		LastProbedAt:      &lastProbedAt,
		LastErrorCode:     cloneStringPtr(observation.ErrorCode),
	}
}

func quotaBandFromProbeObservation(observation SidecarWatchdogProbeObservation) string {
	if validAuthQuotaBand(observation.QuotaBand) {
		return strings.TrimSpace(observation.QuotaBand)
	}
	return quotaBandFromProbeStatus(observation.ProbeStatus, observation.QuotaExceeded)
}

func quotaBandFromProbeStatus(probeStatus string, quotaExceeded bool) string {
	if probeStatus == watchdogProbeStatusSucceeded {
		if quotaExceeded {
			return quotaBandQuotaExceeded
		}
		return quotaBandUsing
	}
	return quotaBandError
}

func validAuthQuotaBand(value string) bool {
	switch strings.TrimSpace(value) {
	case quotaBandUsing, quotaBandQuotaExceeded, quotaBandError:
		return true
	default:
		return false
	}
}

func (s *memorySidecarStore) validateWatchdogHoldCreateLocked(input SidecarWatchdogHoldInput, decisionSidecarID int) error {
	if input.SidecarID != decisionSidecarID {
		return invalidInputError("created hold sidecar_id must match decision sidecar_id")
	}
	if err := validateWatchdogHoldCreateInput(input); err != nil {
		return err
	}
	if _, ok := s.instances[input.SidecarID]; !ok {
		return notFoundError("sidecar instance not found")
	}
	if !watchdogHoldStatusBlocksActiveDuplicate(input.Status) {
		return nil
	}
	authID := strings.TrimSpace(input.AuthID)
	for _, hold := range s.holds[input.SidecarID] {
		if hold.AuthID == authID && watchdogHoldStatusBlocksActiveDuplicate(hold.Status) {
			return &StoreError{Code: StoreErrorDuplicateActiveHold, Message: "active sidecar watchdog hold already exists"}
		}
	}
	return nil
}

func (s *memorySidecarStore) createWatchdogHoldLocked(input SidecarWatchdogHoldInput) (SidecarWatchdogHold, error) {
	if err := s.validateWatchdogHoldCreateLocked(input, input.SidecarID); err != nil {
		return SidecarWatchdogHold{}, err
	}
	now := s.now().UTC()
	hold := SidecarWatchdogHold{ID: s.nextHoldID, SidecarID: input.SidecarID, AuthID: strings.TrimSpace(input.AuthID), AuthIndex: cloneStringPtr(input.AuthIndex), Provider: cloneStringPtr(input.Provider), Reason: strings.TrimSpace(input.Reason), ConditionHash: strings.TrimSpace(input.ConditionHash), PreviousPriority: cloneIntPtr(input.PreviousPriority), TargetPriority: input.TargetPriority, HoldUntil: cloneTimePtr(input.HoldUntil), ManualPauseUntil: cloneTimePtr(input.ManualPauseUntil), Status: strings.TrimSpace(input.Status), LastActionID: cloneIntPtr(input.LastActionID), CreatedAt: now, UpdatedAt: now, ReleasedAt: cloneTimePtr(input.ReleasedAt)}
	s.nextHoldID++
	s.holds[input.SidecarID] = append(s.holds[input.SidecarID], hold)
	return hold, nil
}

func (s *memorySidecarStore) watchdogHoldExistsLocked(id int, sidecarID int) bool {
	for _, hold := range s.holds[sidecarID] {
		if hold.ID == id {
			return true
		}
	}
	return false
}

func (s *memorySidecarStore) updateWatchdogHoldLocked(id int, input SidecarWatchdogHoldInput) (SidecarWatchdogHold, error) {
	if err := validateWatchdogHoldUpdateInput(id, input); err != nil {
		return SidecarWatchdogHold{}, err
	}
	for index, hold := range s.holds[input.SidecarID] {
		if hold.ID != id {
			continue
		}
		hold.AuthID = strings.TrimSpace(input.AuthID)
		hold.AuthIndex = cloneStringPtr(input.AuthIndex)
		hold.Provider = cloneStringPtr(input.Provider)
		hold.Reason = strings.TrimSpace(input.Reason)
		hold.ConditionHash = strings.TrimSpace(input.ConditionHash)
		hold.PreviousPriority = cloneIntPtr(input.PreviousPriority)
		hold.TargetPriority = input.TargetPriority
		hold.HoldUntil = cloneTimePtr(input.HoldUntil)
		hold.ManualPauseUntil = cloneTimePtr(input.ManualPauseUntil)
		hold.Status = strings.TrimSpace(input.Status)
		hold.LastActionID = cloneIntPtr(input.LastActionID)
		hold.ReleasedAt = cloneTimePtr(input.ReleasedAt)
		hold.UpdatedAt = s.now().UTC()
		s.holds[input.SidecarID][index] = hold
		return hold, nil
	}
	return SidecarWatchdogHold{}, notFoundError("sidecar watchdog hold not found")
}

func (s *memorySidecarStore) setWatchdogProbeBatchCompletionLocked(sidecarID int) SidecarWatchdogPolicy {
	now := s.now().UTC()
	policy, ok := s.policies[sidecarID]
	if !ok {
		policy = SidecarWatchdogPolicy{ID: s.nextPolicyID, SidecarID: sidecarID, Enabled: false, FailureThreshold: DefaultFailureThreshold, FailureWindowSeconds: DefaultFailureWindowSeconds, FallbackCooldownSeconds: DefaultFallbackCooldownSeconds, QuotaExceededPriority: DefaultQuotaExceededPriority, UsingPriority: DefaultUsingPriority, WorkingPriority: DefaultWorkingPriority, EmptyQuotaPriority: DefaultEmptyQuotaPriority, InitialPriority: DefaultInitialPriority, ErrorPriority: DefaultErrorPriority, ManualOverridePauseSeconds: DefaultManualOverridePauseSeconds, ProbeConcurrency: DefaultProbeConcurrency, ProbeTimeoutSeconds: DefaultProbeTimeoutSeconds, ProbeBatchCooldownSeconds: DefaultProbeBatchCooldownSeconds, ProbeJitterMinMS: DefaultProbeJitterMinMS, ProbeJitterMaxMS: DefaultProbeJitterMaxMS, CooldownJitterPercent: DefaultCooldownJitterPercent, QuotaInventoryEnabled: true, InitialScanEnabled: true, RollingRefreshEnabled: true, RollingRefreshAfterSeconds: DefaultRollingRefreshAfterSeconds, CreatedAt: now}
		s.nextPolicyID++
	}
	nextBatchAfter := now.Add(time.Duration(normalizedProbeBatchCooldownSeconds(policy)) * time.Second)
	policy.ProbeLastBatchCompletedAt = &now
	policy.ProbeNextBatchAfter = &nextBatchAfter
	policy.UpdatedAt = now
	s.policies[sidecarID] = policy
	return policy
}

func validateWatchdogHoldCreateInput(input SidecarWatchdogHoldInput) error {
	if input.SidecarID <= 0 || strings.TrimSpace(input.AuthID) == "" || strings.TrimSpace(input.Reason) == "" || strings.TrimSpace(input.ConditionHash) == "" || strings.TrimSpace(input.Status) == "" {
		return invalidInputError("sidecar_id, auth_id, reason, condition_hash, and status are required")
	}
	return nil
}

func validateWatchdogHoldUpdateInput(id int, input SidecarWatchdogHoldInput) error {
	if id <= 0 || input.SidecarID <= 0 || strings.TrimSpace(input.AuthID) == "" || strings.TrimSpace(input.Reason) == "" || strings.TrimSpace(input.ConditionHash) == "" || strings.TrimSpace(input.Status) == "" {
		return invalidInputError("id, sidecar_id, auth_id, reason, condition_hash, and status are required")
	}
	return nil
}

func watchdogHoldStatusBlocksActiveDuplicate(status string) bool {
	trimmed := strings.TrimSpace(status)
	return trimmed == WatchdogHoldStatusActive || trimmed == WatchdogHoldStatusPaused
}
