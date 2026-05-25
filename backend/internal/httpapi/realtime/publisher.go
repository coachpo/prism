package realtime

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	statsdomain "github.com/coachpo/prism/backend/internal/domain/stats"
	"github.com/coachpo/prism/backend/internal/pgxutil"
)

type requestLogModelMetadata struct {
	ModelID     string
	DisplayName *string
	ModelType   string
}

var errDashboardRequestLogNotFound = errors.New("dashboard request log not found")

func (s *Service) PublishDashboardUpdate(ctx context.Context, requestLogID int, profileID int) (bool, error) {
	s.RecordLatestDashboardRequestLog(profileID, requestLogID)
	if err := s.refreshDashboardAggregateForRequestLog(ctx, requestLogID, profileID); err != nil {
		if errors.Is(err, errDashboardRequestLogNotFound) {
			s.clearLatestDashboardRequestLog(profileID)
			return false, nil
		}
		return false, err
	}
	if !s.HasDashboardSubscribers(profileID) {
		return false, nil
	}

	message, err := s.BuildDashboardUpdate(ctx, requestLogID, profileID)
	if err != nil {
		if errors.Is(err, errDashboardRequestLogNotFound) {
			s.clearLatestDashboardRequestLog(profileID)
			return false, nil
		}
		return false, err
	}
	return s.manager.BroadcastToProfile(profileID, dashboardChannel, message) > 0, nil
}

func (s *Service) PublishPendingDashboardUpdate(ctx context.Context, profileID int) (bool, error) {
	_, delivered, err := s.PublishLatestDashboardUpdate(ctx, profileID)
	return delivered, err
}

func (s *Service) PublishLatestDashboardUpdate(ctx context.Context, profileID int) (int, bool, error) {
	requestLogID, ok := s.latestRequestLogID(profileID)
	if !ok {
		return 0, false, nil
	}
	if err := s.refreshDashboardAggregateForRequestLog(ctx, requestLogID, profileID); err != nil {
		if errors.Is(err, errDashboardRequestLogNotFound) {
			s.clearLatestDashboardRequestLog(profileID)
			return 0, false, nil
		}
		return requestLogID, false, err
	}
	if !s.HasDashboardSubscribers(profileID) {
		return requestLogID, false, nil
	}
	message, err := s.BuildDashboardUpdate(ctx, requestLogID, profileID)
	if err != nil {
		if errors.Is(err, errDashboardRequestLogNotFound) {
			s.clearLatestDashboardRequestLog(profileID)
			return 0, false, nil
		}
		return requestLogID, false, err
	}
	return requestLogID, s.manager.BroadcastToProfile(profileID, dashboardChannel, message) > 0, nil
}

func (s *Service) RecordLatestDashboardRequestLog(profileID int, requestLogID int) {
	s.setLatestRequestLogID(profileID, requestLogID)
}

func (s *Service) InvalidateDashboardSnapshot(profileID int) {
	if s == nil || s.dashboardSnapshots == nil {
		return
	}
	s.dashboardSnapshots.InvalidateProfile(profileID)
}

func (s *Service) HasDashboardSubscribers(profileID int) bool {
	return s.manager.HasSubscribers(profileID, dashboardChannel)
}

func (s *Service) BuildDashboardUpdate(ctx context.Context, requestLogID int, profileID int) (DashboardUpdateMessage, error) {
	return pgxutil.InReadOnlyTxValue(ctx, s.pool, "realtime dashboard", func(tx pgx.Tx) (DashboardUpdateMessage, error) {
		entry, err := loadRequestLogEntry(ctx, tx, requestLogID, profileID)
		if err != nil {
			return DashboardUpdateMessage{}, err
		}
		aggregate, err := s.loadOrBuildDashboardAggregateSnapshot(ctx, tx, entry.ProfileID, entry.CreatedAt)
		if err != nil {
			return DashboardUpdateMessage{}, err
		}
		snapshot := statsdomain.NewDashboardSnapshot(aggregate, s.now().UTC())
		return DashboardUpdateMessage{
			Type:       dashboardUpdateMessageType,
			RequestLog: entry,
			Snapshot:   snapshot,
		}, nil
	})
}

func (s *Service) loadOrBuildDashboardAggregateSnapshot(ctx context.Context, tx pgx.Tx, profileID int, referenceNow time.Time) (statsdomain.DashboardAggregateSnapshot, error) {
	referenceNow = referenceNow.UTC()
	if snapshot, ok := s.dashboardSnapshots.LoadProfile(profileID); ok && !snapshot.GeneratedAt.Before(referenceNow) {
		return snapshot, nil
	}
	snapshot, err := statsdomain.BuildDashboardAggregateSnapshot(ctx, tx, profileID, referenceNow)
	if err != nil {
		return statsdomain.DashboardAggregateSnapshot{}, err
	}
	s.dashboardSnapshots.StoreProfile(snapshot)
	return snapshot, nil
}

func (s *Service) refreshDashboardAggregateForRequestLog(ctx context.Context, requestLogID int, profileID int) error {
	snapshot, err := pgxutil.InReadOnlyTxValue(ctx, s.pool, "realtime dashboard refresh", func(tx pgx.Tx) (statsdomain.DashboardAggregateSnapshot, error) {
		createdAt, timestampErr := loadRequestLogCreatedAt(ctx, tx, requestLogID, profileID)
		if timestampErr != nil {
			return statsdomain.DashboardAggregateSnapshot{}, timestampErr
		}
		return statsdomain.BuildDashboardAggregateSnapshot(ctx, tx, profileID, createdAt)
	})
	if err != nil {
		return err
	}
	s.dashboardSnapshots.StoreProfile(snapshot)
	return nil
}

func loadRequestLogCreatedAt(ctx context.Context, tx pgx.Tx, requestLogID int, profileID int) (time.Time, error) {
	var createdAt time.Time
	err := tx.QueryRow(ctx, `SELECT created_at FROM request_logs WHERE id = $1 AND profile_id = $2 LIMIT 1`, requestLogID, profileID).Scan(&createdAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, fmt.Errorf("%w: request log %d not found for profile %d", errDashboardRequestLogNotFound, requestLogID, profileID)
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("load request log %d timestamp for profile %d: %w", requestLogID, profileID, err)
	}
	return createdAt.UTC(), nil
}

func loadRequestLogEntry(ctx context.Context, tx pgx.Tx, requestLogID int, profileID int) (RequestLogEntry, error) {
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT row_to_json(request_logs) FROM request_logs WHERE id = $1 AND profile_id = $2 LIMIT 1`, requestLogID, profileID).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return RequestLogEntry{}, fmt.Errorf("%w: request log %d not found for profile %d", errDashboardRequestLogNotFound, requestLogID, profileID)
	}
	if err != nil {
		return RequestLogEntry{}, fmt.Errorf("load request log %d for profile %d: %w", requestLogID, profileID, err)
	}
	var entry RequestLogEntry
	if err := json.Unmarshal(raw, &entry); err != nil {
		return RequestLogEntry{}, fmt.Errorf("decode request log %d: %w", requestLogID, err)
	}
	entry.CreatedAt = entry.CreatedAt.UTC()
	entry.StreamOutcome = normalizeRequestLogStreamOutcome(entry.StreamOutcome, entry.IsStream, entry.CompletionDurationMS)
	entry.StreamErrorKind = normalizeOptionalString(entry.StreamErrorKind)
	enrichedEntry, err := enrichRequestLogEntry(ctx, tx, entry)
	if err != nil {
		return RequestLogEntry{}, err
	}
	return enrichedEntry, nil
}

func (s *Service) setLatestRequestLogID(profileID int, requestLogID int) {
	s.latestMu.Lock()
	defer s.latestMu.Unlock()
	s.latestRequestLogIDs[profileID] = requestLogID
}

func (s *Service) latestRequestLogID(profileID int) (int, bool) {
	s.latestMu.Lock()
	defer s.latestMu.Unlock()
	requestLogID, ok := s.latestRequestLogIDs[profileID]
	return requestLogID, ok
}

func enrichRequestLogEntry(ctx context.Context, tx pgx.Tx, entry RequestLogEntry) (RequestLogEntry, error) {
	currentModelsByID, err := loadRequestLogModels(ctx, tx, entry.ProfileID)
	if err != nil {
		return RequestLogEntry{}, err
	}
	entry.ModelLabel = resolveRequestLogModelLabel(currentModelsByID, entry.ModelID)
	entry.ResolvedTargetModelLabel = resolveRequestLogResolvedTargetModelLabel(currentModelsByID, entry.ResolvedTargetModelID)
	entry.IsProxyOrigin = resolveRequestLogIsProxyOrigin(currentModelsByID, entry.ModelID, entry.ResolvedTargetModelID)
	return entry, nil
}

func loadRequestLogModels(ctx context.Context, tx pgx.Tx, profileID int) (map[string]requestLogModelMetadata, error) {
	rows, err := tx.Query(ctx, `SELECT model_id, display_name, model_type FROM model_configs WHERE profile_id = $1 ORDER BY id ASC`, profileID)
	if err != nil {
		return nil, fmt.Errorf("query realtime request-log models for profile %d: %w", profileID, err)
	}
	defer rows.Close()
	itemsByID := map[string]requestLogModelMetadata{}
	for rows.Next() {
		var modelID string
		var displayName sql.NullString
		var modelType string
		if err := rows.Scan(&modelID, &displayName, &modelType); err != nil {
			return nil, fmt.Errorf("scan realtime request-log model: %w", err)
		}
		trimmedModelID := strings.TrimSpace(modelID)
		if trimmedModelID == "" {
			continue
		}
		if _, exists := itemsByID[trimmedModelID]; exists {
			continue
		}
		itemsByID[trimmedModelID] = requestLogModelMetadata{
			ModelID:     trimmedModelID,
			DisplayName: normalizeOptionalString(nullableStringPointer(displayName)),
			ModelType:   strings.TrimSpace(strings.ToLower(modelType)),
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate realtime request-log models for profile %d: %w", profileID, err)
	}
	return itemsByID, nil
}

func requestLogModelLabel(model requestLogModelMetadata) string {
	if model.DisplayName != nil && strings.TrimSpace(*model.DisplayName) != "" {
		return strings.TrimSpace(*model.DisplayName)
	}
	return strings.TrimSpace(model.ModelID)
}

func resolveRequestLogModelLabel(currentModelsByID map[string]requestLogModelMetadata, modelID string) string {
	trimmedModelID := strings.TrimSpace(modelID)
	if currentModel, ok := currentModelsByID[trimmedModelID]; ok {
		return requestLogModelLabel(currentModel)
	}
	return trimmedModelID
}

func resolveRequestLogResolvedTargetModelLabel(currentModelsByID map[string]requestLogModelMetadata, resolvedTargetModelID *string) *string {
	resolvedTarget := normalizeOptionalString(resolvedTargetModelID)
	if resolvedTarget == nil {
		return nil
	}
	label := resolveRequestLogModelLabel(currentModelsByID, *resolvedTarget)
	return &label
}

func resolveRequestLogIsProxyOrigin(currentModelsByID map[string]requestLogModelMetadata, modelID string, resolvedTargetModelID *string) bool {
	trimmedModelID := strings.TrimSpace(modelID)
	resolvedTarget := normalizeOptionalString(resolvedTargetModelID)
	if resolvedTarget != nil && *resolvedTarget != trimmedModelID {
		return true
	}
	currentModel, ok := currentModelsByID[trimmedModelID]
	return ok && currentModel.ModelType == "proxy"
}

func normalizeRequestLogStreamOutcome(value string, isStream bool, completionDurationMS *int) string {
	trimmed := strings.TrimSpace(value)
	if trimmed != "" {
		return trimmed
	}
	if !isStream {
		return "not_streaming"
	}
	if completionDurationMS != nil {
		return "completed"
	}
	return "unknown"
}

func normalizeOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func nullableStringPointer(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	resolved := value.String
	return &resolved
}
