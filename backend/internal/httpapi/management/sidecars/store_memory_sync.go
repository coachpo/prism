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

func (s *memorySidecarStore) PersistWatchdogProbeDecision(_ context.Context, decision SidecarWatchdogProbeDecision) (SidecarWatchdogProbeDecisionResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if decision.SidecarID <= 0 {
		return SidecarWatchdogProbeDecisionResult{}, invalidInputError("sidecar_id is required")
	}
	if _, ok := s.instances[decision.SidecarID]; !ok {
		return SidecarWatchdogProbeDecisionResult{}, notFoundError("sidecar instance not found")
	}
	normalizedObservations := make([]SidecarWatchdogProbeObservationInput, 0, len(decision.Observations))
	for _, input := range decision.Observations {
		if input.SidecarID == 0 {
			input.SidecarID = decision.SidecarID
		}
		if input.SidecarID != decision.SidecarID {
			return SidecarWatchdogProbeDecisionResult{}, invalidInputError("probe observation sidecar_id must match decision sidecar_id")
		}
		normalized, err := normalizeWatchdogProbeObservationInput(input, s.now().UTC())
		if err != nil {
			return SidecarWatchdogProbeDecisionResult{}, err
		}
		normalizedObservations = append(normalizedObservations, normalized)
	}
	var createHoldInput *SidecarWatchdogHoldInput
	if decision.CreateHold != nil {
		input := *decision.CreateHold
		if input.SidecarID == 0 {
			input.SidecarID = decision.SidecarID
		}
		if err := s.validateWatchdogHoldCreateLocked(input, decision.SidecarID); err != nil {
			return SidecarWatchdogProbeDecisionResult{}, err
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
			return SidecarWatchdogProbeDecisionResult{}, invalidInputError("updated hold sidecar_id must match decision sidecar_id")
		}
		if err := validateWatchdogHoldUpdateInput(decision.UpdateHold.ID, input); err != nil {
			return SidecarWatchdogProbeDecisionResult{}, err
		}
		if !s.watchdogHoldExistsLocked(decision.UpdateHold.ID, decision.SidecarID) {
			return SidecarWatchdogProbeDecisionResult{}, notFoundError("sidecar watchdog hold not found")
		}
		updateHoldInput = &SidecarWatchdogProbeHoldUpdate{ID: decision.UpdateHold.ID, Input: input}
	}
	result := SidecarWatchdogProbeDecisionResult{Observations: make([]SidecarWatchdogProbeObservation, 0, len(normalizedObservations))}
	for _, input := range normalizedObservations {
		observation, err := s.createWatchdogProbeObservationLocked(input)
		if err != nil {
			return SidecarWatchdogProbeDecisionResult{}, err
		}
		result.Observations = append(result.Observations, cloneWatchdogProbeObservation(observation))
	}
	if createHoldInput != nil {
		hold, err := s.createWatchdogHoldLocked(*createHoldInput)
		if err != nil {
			return SidecarWatchdogProbeDecisionResult{}, err
		}
		cloned := cloneWatchdogHold(hold)
		result.CreatedHold = &cloned
	}
	if updateHoldInput != nil {
		hold, err := s.updateWatchdogHoldLocked(updateHoldInput.ID, updateHoldInput.Input)
		if err != nil {
			return SidecarWatchdogProbeDecisionResult{}, err
		}
		cloned := cloneWatchdogHold(hold)
		result.UpdatedHold = &cloned
	}
	if decision.AdvanceProbeCursor {
		policy := s.setWatchdogProbeCursorLocked(decision.SidecarID, decision.ProbeCursorAuthID)
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

func (s *memorySidecarStore) createWatchdogProbeObservationLocked(input SidecarWatchdogProbeObservationInput) (SidecarWatchdogProbeObservation, error) {
	normalized, err := normalizeWatchdogProbeObservationInput(input, s.now().UTC())
	if err != nil {
		return SidecarWatchdogProbeObservation{}, err
	}
	if _, ok := s.instances[normalized.SidecarID]; !ok {
		return SidecarWatchdogProbeObservation{}, notFoundError("sidecar instance not found")
	}
	now := s.now().UTC()
	observation := SidecarWatchdogProbeObservation{ID: s.nextObservationID, SidecarID: normalized.SidecarID, AuthID: normalized.AuthID, AuthIndex: cloneStringPtr(normalized.AuthIndex), Provider: cloneStringPtr(normalized.Provider), ProbedAt: normalized.ProbedAt.UTC(), ProbeStatus: normalized.ProbeStatus, UpstreamStatusCode: cloneIntPtr(normalized.UpstreamStatusCode), QuotaExceeded: normalized.QuotaExceeded, QuotaReason: cloneStringPtr(normalized.QuotaReason), QuotaResetAt: cloneTimePtr(normalized.QuotaResetAt), BlockingWindow: cloneStringPtr(normalized.BlockingWindow), WindowsJSON: memoryJSON(normalized.WindowsJSON, "[]"), ErrorCode: cloneStringPtr(normalized.ErrorCode), CreatedAt: now}
	s.nextObservationID++
	s.probeObservations[normalized.SidecarID] = append(s.probeObservations[normalized.SidecarID], observation)
	return observation, nil
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

func (s *memorySidecarStore) setWatchdogProbeCursorLocked(sidecarID int, cursorAuthID *string) SidecarWatchdogPolicy {
	now := s.now().UTC()
	policy, ok := s.policies[sidecarID]
	if !ok {
		policy = SidecarWatchdogPolicy{ID: s.nextPolicyID, SidecarID: sidecarID, Enabled: false, FailureThreshold: DefaultFailureThreshold, FailureWindowSeconds: DefaultFailureWindowSeconds, FallbackCooldownSeconds: DefaultFallbackCooldownSeconds, DeprioritizedPriority: DefaultDeprioritizedPriority, PrioritizedPriority: DefaultPrioritizedPriority, ManualOverridePauseSeconds: DefaultManualOverridePauseSeconds, ProbeBatchSize: DefaultProbeBatchSize, ProbeTimeoutSeconds: DefaultProbeTimeoutSeconds, CreatedAt: now}
		s.nextPolicyID++
	}
	policy.ProbeCursorAuthID = cloneStringPtr(cursorAuthID)
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
