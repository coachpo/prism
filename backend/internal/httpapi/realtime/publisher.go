package realtime

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	statsdomain "github.com/coachpo/prism/backend/internal/domain/stats"
)

type dashboardMetadataRow struct {
	ModelConfigID    *int
	ModelDisplayName *string
	ModelID          *string
	EndpointName     *string
	EndpointBaseURL  *string
}

type requestLogModelMetadata struct {
	ModelID     string
	DisplayName *string
	ModelType   string
}

func (s *Service) PublishDashboardUpdate(ctx context.Context, requestLogID int, profileID int) (bool, error) {
	s.setLatestRequestLogID(profileID, requestLogID)
	if !s.manager.HasSubscribers(profileID, dashboardChannel) {
		return false, nil
	}

	message, err := s.BuildDashboardUpdate(ctx, requestLogID, profileID)
	if err != nil {
		return false, err
	}
	return s.manager.BroadcastToProfile(profileID, dashboardChannel, message) > 0, nil
}

func (s *Service) PublishPendingDashboardUpdate(ctx context.Context, profileID int) (bool, error) {
	requestLogID, ok := s.latestRequestLogID(profileID)
	if !ok {
		return false, nil
	}
	if !s.manager.HasSubscribers(profileID, dashboardChannel) {
		return false, nil
	}
	message, err := s.BuildDashboardUpdate(ctx, requestLogID, profileID)
	if err != nil {
		return false, err
	}
	return s.manager.BroadcastToProfile(profileID, dashboardChannel, message) > 0, nil
}

func (s *Service) BuildDashboardUpdate(ctx context.Context, requestLogID int, profileID int) (DashboardUpdateMessage, error) {
	return withRealtimeTxValue(ctx, s.pool, func(tx pgx.Tx) (DashboardUpdateMessage, error) {
		entry, err := loadRequestLogEntry(ctx, tx, requestLogID, profileID)
		if err != nil {
			return DashboardUpdateMessage{}, err
		}
		windowEnd := entry.CreatedAt.UTC()
		windowStart24H := windowEnd.Add(-24 * time.Hour)
		statsSummary, err := statsdomain.GetStatsSummary(ctx, tx, statsdomain.StatsSummaryParams{ProfileID: entry.ProfileID, FromTime: &windowStart24H, ToTime: &windowEnd})
		if err != nil {
			return DashboardUpdateMessage{}, err
		}
		groupBy := "api_family"
		apiFamilySummary, err := statsdomain.GetStatsSummary(ctx, tx, statsdomain.StatsSummaryParams{ProfileID: entry.ProfileID, FromTime: &windowStart24H, ToTime: &windowEnd, GroupBy: &groupBy})
		if err != nil {
			return DashboardUpdateMessage{}, err
		}
		spendingSummary, err := statsdomain.GetSpending(ctx, tx, statsdomain.SpendingParams{ProfileID: entry.ProfileID, Preset: "last_30_days", ToTime: &windowEnd, GroupBy: "none", Limit: 50, Offset: 0, TopN: 5, ReferenceNow: windowEnd})
		if err != nil {
			return DashboardUpdateMessage{}, err
		}
		throughput, err := statsdomain.GetThroughput(ctx, tx, statsdomain.ThroughputParams{ProfileID: entry.ProfileID, FromTime: &windowStart24H, ToTime: &windowEnd})
		if err != nil {
			return DashboardUpdateMessage{}, err
		}
		routeSnapshot, err := buildDashboardRouteSnapshot(ctx, tx, entry, windowStart24H, windowEnd)
		if err != nil {
			return DashboardUpdateMessage{}, err
		}
		return DashboardUpdateMessage{
			Type:                dashboardUpdateMessageType,
			RequestLog:          entry,
			StatsSummary24H:     statsSummary,
			APIFamilySummary24H: apiFamilySummary,
			SpendingSummary30D:  spendingSummary,
			Throughput24H:       throughput,
			RoutingRoute24H:     routeSnapshot,
		}, nil
	})
}

func loadRequestLogEntry(ctx context.Context, tx pgx.Tx, requestLogID int, profileID int) (RequestLogEntry, error) {
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT row_to_json(request_logs) FROM request_logs WHERE id = $1 AND profile_id = $2 LIMIT 1`, requestLogID, profileID).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return RequestLogEntry{}, fmt.Errorf("request log %d not found for profile %d", requestLogID, profileID)
	}
	if err != nil {
		return RequestLogEntry{}, fmt.Errorf("load request log %d for profile %d: %w", requestLogID, profileID, err)
	}
	var entry RequestLogEntry
	if err := json.Unmarshal(raw, &entry); err != nil {
		return RequestLogEntry{}, fmt.Errorf("decode request log %d: %w", requestLogID, err)
	}
	entry.CreatedAt = entry.CreatedAt.UTC()
	enrichedEntry, err := enrichRequestLogEntry(ctx, tx, entry)
	if err != nil {
		return RequestLogEntry{}, err
	}
	return enrichedEntry, nil
}

func buildDashboardRouteSnapshot(ctx context.Context, tx pgx.Tx, entry RequestLogEntry, fromTime time.Time, toTime time.Time) (*DashboardRouteSnapshot, error) {
	if entry.EndpointID == nil {
		return nil, nil
	}

	var totalRequests int
	var successCount int
	var trafficCount int
	err := tx.QueryRow(
		ctx,
		`SELECT COUNT(*), COALESCE(SUM(CASE WHEN status_code BETWEEN 200 AND 299 THEN 1 ELSE 0 END), 0), COALESCE(SUM(CASE WHEN success_flag = TRUE THEN 1 ELSE 0 END), 0) FROM request_logs WHERE profile_id = $1 AND model_id = $2 AND endpoint_id = $3 AND created_at >= $4 AND created_at <= $5`,
		entry.ProfileID,
		entry.ModelID,
		*entry.EndpointID,
		fromTime,
		toTime,
	).Scan(&totalRequests, &successCount, &trafficCount)
	if err != nil {
		return nil, fmt.Errorf("aggregate dashboard route snapshot for request log %d: %w", entry.ID, err)
	}

	metadata, err := loadDashboardMetadata(ctx, tx, entry)
	if err != nil {
		return nil, err
	}
	modelLabel := entry.ModelID
	endpointLabel := firstNonEmpty(nullableStringValue(entry.EndpointDescription), nullableStringValue(entry.EndpointBaseURL), fmt.Sprintf("Endpoint %d", *entry.EndpointID))
	if metadata != nil {
		modelLabel = firstNonEmpty(nullableStringValue(metadata.ModelDisplayName), nullableStringValue(metadata.ModelID), modelLabel)
		endpointLabel = firstNonEmpty(nullableStringValue(metadata.EndpointName), nullableStringValue(metadata.EndpointBaseURL), endpointLabel)
	}

	activeConnectionCount := 0
	if metadata != nil && metadata.ModelConfigID != nil {
		if err := tx.QueryRow(
			ctx,
			`SELECT COUNT(*) FROM connections WHERE profile_id = $1 AND model_config_id = $2 AND endpoint_id = $3 AND is_active = TRUE`,
			entry.ProfileID,
			*metadata.ModelConfigID,
			*entry.EndpointID,
		).Scan(&activeConnectionCount); err != nil {
			return nil, fmt.Errorf("count active connections for request log %d: %w", entry.ID, err)
		}
	}

	errorCount := totalRequests - successCount
	var successRate *float64
	if totalRequests > 0 {
		rate := roundTo(successCount, totalRequests, 2)
		successRate = &rate
	}
	return &DashboardRouteSnapshot{
		ModelID:               entry.ModelID,
		ModelConfigID:         metadataModelConfigID(metadata),
		ModelLabel:            modelLabel,
		EndpointID:            *entry.EndpointID,
		EndpointLabel:         endpointLabel,
		ActiveConnectionCount: activeConnectionCount,
		TrafficRequestCount24: trafficCount,
		RequestCount24:        totalRequests,
		SuccessCount24:        successCount,
		ErrorCount24:          errorCount,
		SuccessRate24:         successRate,
	}, nil
}

func loadDashboardMetadata(ctx context.Context, tx pgx.Tx, entry RequestLogEntry) (*dashboardMetadataRow, error) {
	if entry.ConnectionID != nil {
		metadata, found, err := queryDashboardMetadataByConnection(ctx, tx, *entry.ConnectionID, entry.ProfileID)
		if err != nil {
			return nil, err
		}
		if found {
			return metadata, nil
		}
	}
	metadata, found, err := queryDashboardMetadataFallback(ctx, tx, entry.ProfileID, *entry.EndpointID, entry.ModelID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	return metadata, nil
}

func queryDashboardMetadataByConnection(ctx context.Context, tx pgx.Tx, connectionID int, profileID int) (*dashboardMetadataRow, bool, error) {
	return queryDashboardMetadata(
		ctx,
		tx,
		`SELECT connections.model_config_id, model_configs.display_name, model_configs.model_id, endpoints.name, endpoints.base_url FROM connections JOIN model_configs ON model_configs.id = connections.model_config_id JOIN endpoints ON endpoints.id = connections.endpoint_id WHERE connections.id = $1 AND connections.profile_id = $2 LIMIT 1`,
		connectionID,
		profileID,
	)
}

func queryDashboardMetadataFallback(ctx context.Context, tx pgx.Tx, profileID int, endpointID int, modelID string) (*dashboardMetadataRow, bool, error) {
	return queryDashboardMetadata(
		ctx,
		tx,
		`SELECT model_configs.id, model_configs.display_name, model_configs.model_id, endpoints.name, endpoints.base_url FROM connections JOIN model_configs ON model_configs.id = connections.model_config_id JOIN endpoints ON endpoints.id = connections.endpoint_id WHERE connections.profile_id = $1 AND connections.endpoint_id = $2 AND model_configs.model_id = $3 ORDER BY connections.is_active DESC, connections.priority ASC LIMIT 1`,
		profileID,
		endpointID,
		modelID,
	)
}

func queryDashboardMetadata(ctx context.Context, tx pgx.Tx, query string, args ...any) (*dashboardMetadataRow, bool, error) {
	var modelConfigID sql.NullInt32
	var displayName sql.NullString
	var modelID sql.NullString
	var endpointName sql.NullString
	var endpointBaseURL sql.NullString
	err := tx.QueryRow(ctx, query, args...).Scan(&modelConfigID, &displayName, &modelID, &endpointName, &endpointBaseURL)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("query dashboard metadata: %w", err)
	}
	return &dashboardMetadataRow{
		ModelConfigID:    nullableInt32(modelConfigID),
		ModelDisplayName: nullableStringPointer(displayName),
		ModelID:          nullableStringPointer(modelID),
		EndpointName:     nullableStringPointer(endpointName),
		EndpointBaseURL:  nullableStringPointer(endpointBaseURL),
	}, true, nil
}

func metadataModelConfigID(metadata *dashboardMetadataRow) *int {
	if metadata == nil {
		return nil
	}
	return metadata.ModelConfigID
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

func withRealtimeTxValue[T any](ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) (T, error)) (T, error) {
	var zero T
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return zero, fmt.Errorf("begin realtime transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()
	value, err := fn(tx)
	if err != nil {
		return zero, err
	}
	if err := tx.Commit(ctx); err != nil {
		return zero, fmt.Errorf("commit realtime transaction: %w", err)
	}
	return value, nil
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func nullableStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func nullableStringPointer(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	resolved := value.String
	return &resolved
}

func roundTo(numerator int, denominator int, places int) float64 {
	if denominator == 0 {
		return 0
	}
	factor := math.Pow10(places)
	value := (float64(numerator) / float64(denominator)) * 100
	return math.Round(value*factor) / factor
}

func nullableInt32(value sql.NullInt32) *int {
	if !value.Valid {
		return nil
	}
	resolved := int(value.Int32)
	return &resolved
}
