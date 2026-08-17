package models

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/coachpo/prism/backend/internal/domain/modelrouting"
	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
	"github.com/jackc/pgx/v5"
)

func (s *Service) deletePrivateConnectionTargetFromMutationItems(ctx context.Context, tx pgx.Tx, profileID int, model modelRecord, targetID int, items []accessTargetMutationItem) (bool, error) {
	index := findAccessTargetMutationIndex(items, targetID)
	if index == -1 {
		return false, &domainError{StatusCode: http.StatusNotFound, Detail: "Model access target not found"}
	}
	item := items[index]
	if !modelrouting.IsTerminalTargetType(item.Request.TargetType) {
		return false, nil
	}
	if item.Request.ConnectionID == nil {
		return true, fmt.Errorf("connection access target %d is missing connection id", targetID)
	}
	if err := ensurePrivateConnectionTargetDeleteAllowed(ctx, tx, profileID, model, targetID); err != nil {
		return true, err
	}
	if err := lockConnectionRow(ctx, tx, profileID, *item.Request.ConnectionID); err != nil {
		return true, err
	}
	if err := deleteModelAccessTargetRow(ctx, tx, targetID); err != nil {
		return true, err
	}
	if err := deleteConnectionRow(ctx, tx, *item.Request.ConnectionID); err != nil {
		return true, err
	}
	if err := compactModelAccessTargetPositions(ctx, tx, profileID, model.ID, s.nowUTC()); err != nil {
		return true, err
	}
	return true, nil
}

func ensurePrivateConnectionTargetDeleteAllowed(ctx context.Context, exec queryExecutor, profileID int, model modelRecord, deletingTargetID int) error {
	if !model.IsEnabled {
		return nil
	}
	enabledCount, err := countEnabledModelAccessTargetsExcluding(ctx, exec, profileID, model.ID, deletingTargetID)
	if err != nil {
		return err
	}
	if enabledCount == 0 {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: "enabled models must include at least one enabled access target"}
	}
	return nil
}

func (s *Service) loadModelTargetMutationState(ctx context.Context, tx pgx.Tx, r *http.Request, modelConfigID int) (profiledomain.Profile, modelRecord, []accessTargetMutationItem, error) {
	profile, err := resolveEffectiveProfile(ctx, tx, r)
	if err != nil {
		return profiledomain.Profile{}, modelRecord{}, nil, err
	}
	model, found, err := loadModelRecord(ctx, tx, profile.ID, modelConfigID, true)
	if err != nil {
		return profiledomain.Profile{}, modelRecord{}, nil, err
	}
	if !found {
		return profiledomain.Profile{}, modelRecord{}, nil, &domainError{StatusCode: http.StatusNotFound, Detail: "Model configuration not found"}
	}
	if err := lockProfileAccessTargetRows(ctx, tx, profile.ID); err != nil {
		return profiledomain.Profile{}, modelRecord{}, nil, err
	}
	items, err := loadAccessTargetMutationItems(ctx, tx, profile.ID, model.ID)
	if err != nil {
		return profiledomain.Profile{}, modelRecord{}, nil, err
	}
	return profile, model, items, nil
}

func (s *Service) replaceModelTargetsFromMutationItems(ctx context.Context, tx pgx.Tx, profileID int, model modelRecord, items []accessTargetMutationItem) ([]modelAccessTargetResponse, error) {
	requests := accessTargetRequestsFromMutationItems(items)
	requests = normalizeAccessTargets(requests)
	if err := validateAccessTargets(requests); err != nil {
		return nil, err
	}
	modelRequests := modelAccessTargetRequestsOnly(requests)
	preservedConnectionTargets := preservedConnectionTargetsFromMutationItems(items)
	if err := validateAccessTargetsForSourceModel(model.ModelID, modelRequests); err != nil {
		return nil, err
	}
	resolvedTargets, err := resolveAccessTargets(ctx, tx, profileID, &model.ID, model.ModelID, model.APIFamily, model.OpenAIAcceptedFormat, model.OpenAIImageOperations, modelRequests)
	if err != nil {
		return nil, err
	}
	if model.IsEnabled && !hasEnabledResolvedOrPreservedAccessTarget(resolvedTargets, preservedConnectionTargets) {
		return nil, routingPlanValidationIssueError("model_no_enabled_targets", "access_targets", "enabled models must include at least one enabled access target")
	}
	if err := ensureAccessTargetGraphAcyclic(ctx, tx, profileID, model.ID, resolvedTargets); err != nil {
		return nil, err
	}
	now := s.nowUTC()
	if err := replaceAccessTargetsPreservingConnections(ctx, tx, profileID, model.ID, resolvedTargets, preservedConnectionTargets, now); err != nil {
		return nil, err
	}
	return loadModelTargetResponses(ctx, tx, profileID, model.ID, now)
}

func (s *Service) updateModelTargetMetadataFromMutationItems(ctx context.Context, tx pgx.Tx, profileID int, model modelRecord, items []accessTargetMutationItem) ([]modelAccessTargetResponse, error) {
	normalizeMutationItemPositions(items)
	requests := accessTargetRequestsFromMutationItems(items)
	if err := validateAccessTargets(requests); err != nil {
		return nil, err
	}
	if model.IsEnabled && !hasEnabledAccessTargetMutationItem(items) {
		return nil, routingPlanValidationIssueError("model_no_enabled_targets", "access_targets", "enabled models must include at least one enabled access target")
	}
	if err := updateAccessTargetMetadata(ctx, tx, profileID, model.ID, items, s.nowUTC()); err != nil {
		return nil, err
	}
	return loadModelTargetResponses(ctx, tx, profileID, model.ID, s.now().UTC())
}

func loadModelTargetResponses(ctx context.Context, exec queryExecutor, profileID int, modelConfigID int, now time.Time) ([]modelAccessTargetResponse, error) {
	accessTargets, err := loadAccessTargetsForModels(ctx, exec, profileID, []int{modelConfigID})
	if err != nil {
		return nil, err
	}
	return accessTargetResponsesFromRecords(accessTargets[modelConfigID], now), nil
}

func loadAccessTargetMutationItems(ctx context.Context, exec queryExecutor, profileID int, modelConfigID int) ([]accessTargetMutationItem, error) {
	accessTargets, err := loadAccessTargetsForModels(ctx, exec, profileID, []int{modelConfigID})
	if err != nil {
		return nil, err
	}
	records := cloneAccessTargetRecords(accessTargets[modelConfigID])
	sortAccessTargetRecords(records)
	items := make([]accessTargetMutationItem, 0, len(records))
	for _, record := range records {
		items = append(items, accessTargetMutationItem{ID: record.ID, Request: accessTargetRequestFromRecord(record)})
	}
	normalizeMutationItemPositions(items)
	return items, nil
}

func preservedConnectionTargetsFromMutationItems(items []accessTargetMutationItem) []preservedConnectionAccessTarget {
	normalizeMutationItemPositions(items)
	preserved := make([]preservedConnectionAccessTarget, 0)
	for _, item := range items {
		if !modelrouting.IsTerminalTargetType(item.Request.TargetType) {
			continue
		}
		enabled := true
		if item.Request.IsEnabled != nil {
			enabled = *item.Request.IsEnabled
		}
		preserved = append(preserved, preservedConnectionAccessTarget{ID: item.ID, Position: item.Request.Position, IsEnabled: enabled, Update: true})
	}
	return preserved
}
