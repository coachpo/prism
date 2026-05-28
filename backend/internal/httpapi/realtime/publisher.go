package realtime

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	statsdomain "github.com/coachpo/prism/backend/internal/domain/stats"
	"github.com/coachpo/prism/backend/internal/pgxutil"
)

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
	detail, err := statsdomain.GetRequestLogDetail(ctx, tx, profileID, requestLogID)
	if err != nil {
		return RequestLogEntry{}, fmt.Errorf("load request log %d for profile %d: %w", requestLogID, profileID, err)
	}
	if detail == nil {
		return RequestLogEntry{}, fmt.Errorf("%w: request log %d not found for profile %d", errDashboardRequestLogNotFound, requestLogID, profileID)
	}
	return requestLogEntryFromDetail(*detail), nil
}

func requestLogEntryFromDetail(detail statsdomain.RequestLogDetailResponse) RequestLogEntry {
	return RequestLogEntry{
		ID:                                detail.Summary.ID,
		ProfileID:                         detail.Routing.ProfileID,
		ModelID:                           detail.Summary.ModelID,
		ModelLabel:                        detail.Summary.ModelLabel,
		ResolvedTargetModelID:             detail.Summary.ResolvedTargetModelID,
		ResolvedTargetModelLabel:          detail.Summary.ResolvedTargetModelLabel,
		APIFamily:                         detail.Summary.APIFamily,
		VendorID:                          detail.Summary.VendorID,
		VendorKey:                         detail.Summary.VendorKey,
		VendorName:                        detail.Summary.VendorName,
		EndpointID:                        detail.Routing.EndpointID,
		ConnectionID:                      detail.Routing.ConnectionID,
		ProxyAPIKeyID:                     detail.Request.ProxyAPIKeyID,
		ProxyAPIKeyNameSnapshot:           detail.Request.ProxyAPIKeyNameSnapshot,
		IngressRequestID:                  detail.Request.IngressRequestID,
		AttemptNumber:                     detail.Request.AttemptNumber,
		ProviderCorrelationID:             detail.Request.ProviderCorrelationID,
		EndpointBaseURL:                   detail.Routing.EndpointBaseURL,
		StatusCode:                        detail.Summary.StatusCode,
		ResponseTimeMS:                    detail.Summary.ResponseTimeMS,
		TTFTMS:                            detail.Summary.TTFTMS,
		CompletionDurationMS:              detail.Summary.CompletionDurationMS,
		IsStream:                          detail.Summary.IsStream,
		StreamOutcome:                     detail.Summary.StreamOutcome,
		StreamErrorKind:                   detail.Summary.StreamErrorKind,
		InputTokens:                       detail.Usage.InputTokens,
		OutputTokens:                      detail.Usage.OutputTokens,
		TotalTokens:                       detail.Usage.TotalTokens,
		SuccessFlag:                       detail.Usage.SuccessFlag,
		BillableFlag:                      detail.Usage.BillableFlag,
		PricedFlag:                        detail.Usage.PricedFlag,
		UnpricedReason:                    detail.Usage.UnpricedReason,
		CacheReadInputTokens:              detail.Usage.CacheReadInputTokens,
		CacheCreationInputTokens:          detail.Usage.CacheCreationInputTokens,
		ReasoningTokens:                   detail.Usage.ReasoningTokens,
		InputCostMicros:                   detail.Costing.InputCostMicros,
		OutputCostMicros:                  detail.Costing.OutputCostMicros,
		CacheReadInputCostMicros:          detail.Costing.CacheReadInputCostMicros,
		CacheCreationInputCostMicros:      detail.Costing.CacheCreationInputCostMicros,
		ReasoningCostMicros:               detail.Costing.ReasoningCostMicros,
		TotalCostOriginalMicros:           detail.Costing.TotalCostOriginalMicros,
		TotalCostUserCurrencyMicros:       detail.Costing.TotalCostUserCurrencyMicros,
		CurrencyCodeOriginal:              detail.Costing.CurrencyCodeOriginal,
		ReportCurrencyCode:                detail.Costing.ReportCurrencyCode,
		ReportCurrencySymbol:              detail.Costing.ReportCurrencySymbol,
		FXRateUsed:                        detail.Costing.FXRateUsed,
		FXRateSource:                      detail.Costing.FXRateSource,
		PricingSnapshotUnit:               detail.Pricing.PricingSnapshotUnit,
		PricingSnapshotInput:              detail.Pricing.PricingSnapshotInput,
		PricingSnapshotOutput:             detail.Pricing.PricingSnapshotOutput,
		PricingSnapshotCacheReadInput:     detail.Pricing.PricingSnapshotCacheReadInput,
		PricingSnapshotCacheCreationInput: detail.Pricing.PricingSnapshotCacheCreationInput,
		PricingSnapshotReasoning:          detail.Pricing.PricingSnapshotReasoning,
		PricingConfigVersionUsed:          detail.Pricing.PricingConfigVersionUsed,
		RequestPath:                       detail.Request.RequestPath,
		ErrorDetail:                       detail.Request.ErrorDetail,
		EndpointDescription:               detail.Routing.EndpointDescription,
		CreatedAt:                         detail.Summary.CreatedAt.UTC(),
	}
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
