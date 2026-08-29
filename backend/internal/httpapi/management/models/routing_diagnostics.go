package models

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/domain/modelrouting"
	"github.com/coachpo/prism/backend/internal/httpapi/management/connections"
	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/httpapi/runtime"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	"github.com/coachpo/prism/backend/internal/providerauth"
)

// modelBoundOperationsForFamily returns the runtime operation registry's
// model-bound operations for a family. OpenAI returns an empty list here; the
// OpenAI accepted operation set is derived from the model's accepted format
// (the registry includes openai.models which has no model binding and is
// excluded from the compatibility matrix).
func modelBoundOperationsForFamily(apiFamily string) []string {
	operations := make([]string, 0, 4)
	for _, operation := range runtime.RuntimeOperationCatalog() {
		if !strings.EqualFold(operation.APIFamily, apiFamily) {
			continue
		}
		if operation.ModelBindingSource == runtime.RuntimeOperationModelBindingNone {
			continue
		}
		operations = append(operations, operation.Name)
	}
	return operations
}

// acceptedOperationsForModel derives the canonical accepted operation set for
// diagnostics across both OpenAI dimensions; other families use their
// registered model-bound operations from the runtime registry.
//
// An absent text mode contributes no text operation. The old dual_native
// fallback would report a pure image model as accepting Chat Completions and
// Responses, which is exactly the confusion the independent dimensions exist to
// avoid.
func acceptedOperationsForModel(record modelRecord) []string {
	if providerauth.IsOpenAI(record.APIFamily) {
		return modelrouting.OpenAIAcceptedOperationSetForDimensions(record.OpenAIAcceptedFormat, record.OpenAIImageOperations)
	}
	return modelBoundOperationsForFamily(record.APIFamily)
}

// diagnosticsOperationListForModel returns the full operation list analyzed
// for a model. OpenAI responses MUST always carry every registered model-bound
// operation across both dimensions (root-unaccepted rows get accepted=false);
// other families use their registered model-bound operations.
func diagnosticsOperationListForModel(record modelRecord) []string {
	if providerauth.IsOpenAI(record.APIFamily) {
		return modelrouting.OpenAIRegisteredOperationList()
	}
	return modelBoundOperationsForFamily(record.APIFamily)
}

// loadRoutingDiagnosticsGraph loads the full authored graph for a profile in a
// bounded number of queries (models + strategies + access targets), regardless
// of the number of models.
func loadRoutingDiagnosticsGraph(ctx context.Context, tx pgx.Tx, profileID int, records []modelRecord) (*modelrouting.DiagnosticsGraph, error) {
	graph := &modelrouting.DiagnosticsGraph{
		ModelsByID:                   map[int]modelrouting.DiagnosticsModel{},
		AccessTargetsBySourceModelID: map[int][]modelrouting.DiagnosticsAccessTarget{},
		ConnectionsByID:              map[int]modelrouting.DiagnosticsConnection{},
		StrategiesByModelID:          map[int]modelrouting.DiagnosticsStrategy{},
	}
	modelIDs := uniqueModelIDs(records)
	for _, record := range records {
		graph.ModelsByID[record.ID] = modelrouting.DiagnosticsModel{
			ConfigID:              record.ID,
			ProfileID:             record.ProfileID,
			ModelID:               record.ModelID,
			APIFamily:             record.APIFamily,
			IsEnabled:             record.IsEnabled,
			OpenAIAcceptedFormat:  cloneStringPointer(record.OpenAIAcceptedFormat),
			OpenAIImageOperations: cloneStringPointer(record.OpenAIImageOperations),
			LoadbalanceStrategyID: cloneIntPointer(record.LoadbalanceStrategyID),
		}
	}
	strategyRows, err := tx.Query(ctx, `SELECT id, legacy_strategy_type FROM loadbalance_strategies WHERE profile_id = $1 ORDER BY id ASC`, profileID)
	if err != nil {
		return nil, fmt.Errorf("query diagnostics strategies for profile %d: %w", profileID, err)
	}
	defer strategyRows.Close()
	for strategyRows.Next() {
		var strategy modelrouting.DiagnosticsStrategy
		if err := strategyRows.Scan(&strategy.ID, &strategy.Subtype); err != nil {
			return nil, fmt.Errorf("scan diagnostics strategy for profile %d: %w", profileID, err)
		}
		graph.StrategiesByModelID[strategy.ID] = strategy
	}
	if err := strategyRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate diagnostics strategies for profile %d: %w", profileID, err)
	}

	accessTargets, err := loadAccessTargetsForModels(ctx, tx, profileID, modelIDs)
	if err != nil {
		return nil, err
	}
	for sourceModelConfigID, targets := range accessTargets {
		for _, target := range targets {
			graph.AccessTargetsBySourceModelID[sourceModelConfigID] = append(graph.AccessTargetsBySourceModelID[sourceModelConfigID], modelrouting.DiagnosticsAccessTarget{
				ID:                  target.ID,
				ProfileID:           target.ProfileID,
				SourceModelConfigID: target.SourceModelConfigID,
				TargetType:          target.TargetType,
				TargetModelConfigID: cloneIntPointer(target.TargetModelConfigID),
				TargetConnectionID:  cloneIntPointer(target.TargetConnectionID),
				Position:            target.Position,
				IsEnabled:           target.IsEnabled,
			})
			if target.Connection == nil {
				continue
			}
			if _, exists := graph.ConnectionsByID[target.Connection.ID]; exists {
				continue
			}
			graph.ConnectionsByID[target.Connection.ID] = modelrouting.DiagnosticsConnection{
				ID:                      target.Connection.ID,
				ProfileID:               target.Connection.ProfileID,
				APIFamily:               target.Connection.APIFamily,
				EndpointID:              target.Connection.EndpointID,
				IsActive:                target.Connection.IsActive,
				OpenAITextCapability:    cloneStringPointer(target.Connection.OpenAITextCapability),
				OpenAIImageCapability:   cloneStringPointer(target.Connection.OpenAIImageCapability),
				RoutingScheduleTimezone: routingScheduleTimezoneFromSummary(target.Connection),
				RoutingWindows:          routingWindowsFromPayload(target.Connection.RoutingSchedule),
			}
		}
	}
	return graph, nil
}

func (s *Service) handleGetRoutingDiagnostics(w http.ResponseWriter, r *http.Request) {
	modelConfigID, err := routeInt(r, "model_config_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "model", func(tx pgx.Tx) (modelrouting.DiagnosticsResult, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return modelrouting.DiagnosticsResult{}, err
		}
		records, err := listModelRecords(r.Context(), tx, profile.ID)
		if err != nil {
			return modelrouting.DiagnosticsResult{}, err
		}
		record, found := findModelRecordByID(records, modelConfigID)
		if !found {
			return modelrouting.DiagnosticsResult{}, &domainError{StatusCode: http.StatusNotFound, Detail: "Model configuration not found"}
		}
		graph, err := loadRoutingDiagnosticsGraph(r.Context(), tx, profile.ID, records)
		if err != nil {
			return modelrouting.DiagnosticsResult{}, err
		}
		return modelrouting.Analyze(graph, modelConfigID, diagnosticsOperationListForModel(record)), nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func findModelRecordByID(records []modelRecord, modelConfigID int) (modelRecord, bool) {
	for _, record := range records {
		if record.ID == modelConfigID {
			return record, true
		}
	}
	return modelRecord{}, false
}

// attachRoutingSummaries computes the compact routing summary for every model
// in one bounded batch (single graph load + in-memory analyses) and attaches
// it to the list response. It never issues per-model queries.
func attachRoutingSummaries(records []modelRecord, accessTargets map[int][]accessTargetRecord, strategies map[int]strategyRecord, summaries map[int]modelrouting.RoutingSummary) error {
	graph := &modelrouting.DiagnosticsGraph{
		ModelsByID:                   map[int]modelrouting.DiagnosticsModel{},
		AccessTargetsBySourceModelID: map[int][]modelrouting.DiagnosticsAccessTarget{},
		ConnectionsByID:              map[int]modelrouting.DiagnosticsConnection{},
		StrategiesByModelID:          map[int]modelrouting.DiagnosticsStrategy{},
	}
	for _, record := range records {
		graph.ModelsByID[record.ID] = modelrouting.DiagnosticsModel{
			ConfigID:              record.ID,
			ProfileID:             record.ProfileID,
			ModelID:               record.ModelID,
			APIFamily:             record.APIFamily,
			IsEnabled:             record.IsEnabled,
			OpenAIAcceptedFormat:  cloneStringPointer(record.OpenAIAcceptedFormat),
			OpenAIImageOperations: cloneStringPointer(record.OpenAIImageOperations),
			LoadbalanceStrategyID: cloneIntPointer(record.LoadbalanceStrategyID),
		}
	}
	for _, strategy := range strategies {
		graph.StrategiesByModelID[strategy.ID] = modelrouting.DiagnosticsStrategy{ID: strategy.ID, Subtype: strategy.LegacyStrategyType}
	}
	for sourceModelConfigID, targets := range accessTargets {
		for _, target := range targets {
			graph.AccessTargetsBySourceModelID[sourceModelConfigID] = append(graph.AccessTargetsBySourceModelID[sourceModelConfigID], modelrouting.DiagnosticsAccessTarget{
				ID:                  target.ID,
				ProfileID:           target.ProfileID,
				SourceModelConfigID: target.SourceModelConfigID,
				TargetType:          target.TargetType,
				TargetModelConfigID: cloneIntPointer(target.TargetModelConfigID),
				TargetConnectionID:  cloneIntPointer(target.TargetConnectionID),
				Position:            target.Position,
				IsEnabled:           target.IsEnabled,
			})
			if target.Connection == nil {
				continue
			}
			if _, exists := graph.ConnectionsByID[target.Connection.ID]; exists {
				continue
			}
			graph.ConnectionsByID[target.Connection.ID] = modelrouting.DiagnosticsConnection{
				ID:                      target.Connection.ID,
				ProfileID:               target.Connection.ProfileID,
				APIFamily:               target.Connection.APIFamily,
				EndpointID:              target.Connection.EndpointID,
				IsActive:                target.Connection.IsActive,
				OpenAITextCapability:    cloneStringPointer(target.Connection.OpenAITextCapability),
				OpenAIImageCapability:   cloneStringPointer(target.Connection.OpenAIImageCapability),
				RoutingScheduleTimezone: routingScheduleTimezoneFromSummary(target.Connection),
				RoutingWindows:          routingWindowsFromPayload(target.Connection.RoutingSchedule),
			}
		}
	}
	for _, record := range records {
		result := modelrouting.Analyze(graph, record.ID, diagnosticsOperationListForModel(record))
		summaries[record.ID] = modelrouting.BuildRoutingSummary(graph, graph.ModelsByID[record.ID], result)
	}
	return nil
}

func cloneStringPointer(value *string) *string {
	if value == nil {
		return nil
	}
	resolved := *value
	return &resolved
}

func cloneIntPointer(value *int) *int {
	if value == nil {
		return nil
	}
	resolved := *value
	return &resolved
}

// modelMutationWarnings runs the authoritative analyzer over the committed
// graph after a routing-relevant mutation and returns its warnings.
func modelMutationWarnings(ctx context.Context, tx pgx.Tx, profileID int, modelConfigID int) ([]modelrouting.ConfigurationWarning, error) {
	records, err := listModelRecords(ctx, tx, profileID)
	if err != nil {
		return nil, err
	}
	record, found := findModelRecordByID(records, modelConfigID)
	if !found {
		return nil, &domainError{StatusCode: http.StatusNotFound, Detail: "Model configuration not found"}
	}
	graph, err := loadRoutingDiagnosticsGraph(ctx, tx, profileID, records)
	if err != nil {
		return nil, err
	}
	result := modelrouting.Analyze(graph, modelConfigID, diagnosticsOperationListForModel(record))
	return result.ConfigurationWarnings, nil
}

// translateConnectionWriterError converts the connections package's shared
// writer DomainError into this package's domainError so the management layer
// preserves status and detail instead of falling through to 500.
func translateConnectionWriterError(err error) error {
	var connectionErr *connections.DomainError
	if errors.As(err, &connectionErr) && connectionErr != nil {
		detail, ok := connectionErr.Detail.(string)
		if !ok {
			detail = connectionErr.Error()
		}
		return &domainError{StatusCode: connectionErr.StatusCode, Detail: detail}
	}
	return err
}
