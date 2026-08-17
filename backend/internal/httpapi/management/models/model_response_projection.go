package models

import (
	"time"

	"github.com/coachpo/prism/backend/internal/httpapi/management/connections"
)

func buildModelListResponse(record modelRecord, strategies map[int]strategyRecord, accessTargets map[int][]accessTargetRecord, counts map[int]modelConnectionCounts, health map[string]modelHealthStats, now time.Time) modelConfigListResponse {
	response := modelConfigListResponse{ID: record.ID, ProfileID: record.ProfileID, APIFamily: record.APIFamily, ModelID: record.ModelID, DisplayName: record.DisplayName, LoadbalanceStrategyID: record.LoadbalanceStrategyID, OpenAIAcceptedFormat: record.OpenAIAcceptedFormat, OpenAIImageOperations: record.OpenAIImageOperations, AccessTargets: accessTargetResponsesFromRecords(accessTargets[record.ID], now), IsEnabled: record.IsEnabled, HealthTotalRequests: 0, CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt}
	if record.LoadbalanceStrategyID != nil {
		if strategy, ok := strategies[*record.LoadbalanceStrategyID]; ok {
			response.LoadbalanceStrategy = strategySummaryFromRecord(strategy)
		}
	}
	if count, ok := counts[record.ID]; ok {
		response.ConnectionCount = count.Total
		response.ActiveConnectionCount = count.Active
	}
	if stats, ok := health[record.ModelID]; ok {
		response.HealthSuccessRate = stats.SuccessRate
		response.HealthTotalRequests = stats.TotalRequests
	}
	return response
}

func buildModelDetailResponse(record modelRecord, strategies map[int]strategyRecord, accessTargets map[int][]accessTargetRecord, now time.Time) modelConfigResponse {
	response := modelConfigResponse{ID: record.ID, ProfileID: record.ProfileID, APIFamily: record.APIFamily, ModelID: record.ModelID, DisplayName: record.DisplayName, LoadbalanceStrategyID: record.LoadbalanceStrategyID, OpenAIAcceptedFormat: record.OpenAIAcceptedFormat, OpenAIImageOperations: record.OpenAIImageOperations, AccessTargets: accessTargetResponsesFromRecords(accessTargets[record.ID], now), IsEnabled: record.IsEnabled, CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt}
	if record.LoadbalanceStrategyID != nil {
		if strategy, ok := strategies[*record.LoadbalanceStrategyID]; ok {
			response.LoadbalanceStrategy = strategySummaryFromRecord(strategy)
		}
	}
	return response
}

func strategySummaryFromRecord(record strategyRecord) *loadbalanceStrategySummary {
	return &loadbalanceStrategySummary{ID: record.ID, Name: record.Name, LegacyStrategyType: record.LegacyStrategyType, IsDefault: record.IsDefault, FailureStatusCodes: cloneIntSlice(record.FailureStatusCodes), BanMode: record.BanMode, RetryBaseDelayMS: record.RetryBaseDelayMS, RetryBackoffMultiplier: record.RetryBackoffMultiplier, RetryJitterRatio: record.RetryJitterRatio, RetryMaxDelayMS: record.RetryMaxDelayMS, CycleRetryAttemptLimit: record.CycleRetryAttemptLimit, BanCumulativeRetryAttemptThreshold: record.BanCumulativeRetryAttemptThreshold, BanDurationSeconds: record.BanDurationSeconds}
}

func accessTargetResponsesFromRecords(records []accessTargetRecord, now time.Time) []modelAccessTargetResponse {
	if len(records) == 0 {
		return []modelAccessTargetResponse{}
	}
	ordered := cloneAccessTargetRecords(records)
	sortAccessTargetRecords(ordered)
	items := make([]modelAccessTargetResponse, 0, len(ordered))
	for _, record := range ordered {
		response := modelAccessTargetResponse{ID: record.ID, TargetType: record.TargetType, TargetModelID: stringPtrFromModelRecord(record.TargetModel), ConnectionID: copyIntPtr(record.TargetConnectionID), TerminalTargetID: copyIntPtr(record.TargetConnectionID), Position: record.Position, IsEnabled: record.IsEnabled, CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt}
		if record.TargetModel != nil {
			response.TargetModel = modelTargetSummaryFromRecord(*record.TargetModel)
		}
		if record.Connection != nil {
			connection := *record.Connection
			// The evaluated state is projected here, at the single funnel where
			// both the connection and terminal_target keys are filled from the
			// same struct, so the two keys can never disagree. It reuses the
			// connections package projection rather than a second
			// implementation of window arithmetic.
			connection.RoutingScheduleState = connections.RoutingScheduleStateForConfig(
				connection.routingScheduleTimezone, routingWindowsFromPayload(connection.RoutingSchedule), connection.IsActive, now)
			response.Connection = &connection
			response.TerminalTarget = &connection
		}
		items = append(items, response)
	}
	return items
}

func modelTargetSummaryFromRecord(record modelRecord) *modelTargetSummary {
	return &modelTargetSummary{ID: record.ID, ProfileID: record.ProfileID, APIFamily: record.APIFamily, ModelID: record.ModelID, DisplayName: record.DisplayName, LoadbalanceStrategyID: record.LoadbalanceStrategyID, OpenAIAcceptedFormat: record.OpenAIAcceptedFormat, OpenAIImageOperations: record.OpenAIImageOperations, IsEnabled: record.IsEnabled}
}

func copyIntPtr(value *int) *int {
	if value == nil {
		return nil
	}
	return intPtr(*value)
}

func intPtr(value int) *int {
	resolved := value
	return &resolved
}
