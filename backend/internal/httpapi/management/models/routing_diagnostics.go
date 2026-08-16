package models

import (
	"context"
	"encoding/json"
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
				ID:                    target.Connection.ID,
				ProfileID:             target.Connection.ProfileID,
				APIFamily:             target.Connection.APIFamily,
				IsActive:              target.Connection.IsActive,
				OpenAITextCapability:  cloneStringPointer(target.Connection.OpenAITextCapability),
				OpenAIImageCapability: cloneStringPointer(target.Connection.OpenAIImageCapability),
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

type routingDiagnosticsPreviewRequest struct {
	OpenAIAcceptedFormat  *string `json:"openai_accepted_format"`
	OpenAIImageOperations *string `json:"openai_image_operations"`
	LoadbalanceStrategyID *int    `json:"loadbalance_strategy_id"`
	IsEnabled             *bool   `json:"is_enabled"`
}

// routingDiagnosticsPreviewResponse wraps the diagnostics result with the
// mode-change guard computed for the proposed values. The embedded result
// flattens into the same response shape as GET diagnostics.
type routingDiagnosticsPreviewResponse struct {
	modelrouting.DiagnosticsResult
	ModeChangeGuard modeChangeGuard `json:"mode_change_guard"`
}

type modeChangeGuard struct {
	Allowed                      bool   `json:"allowed"`
	BlockingOwnerConnectionCount int    `json:"blocking_owner_connection_count"`
	RequiredAction               string `json:"required_action"`
}

const modeChangeRequiredAction = "delete_relationships_then_change_mode_then_recreate"

func (s *Service) handlePreviewRoutingDiagnostics(w http.ResponseWriter, r *http.Request) {
	modelConfigID, err := routeInt(r, "model_config_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	var requestBody routingDiagnosticsPreviewRequest
	if err := decodeStrictJSONBody(r, &requestBody); err != nil {
		writeDecodeError(w, r, s.corsSnapshot(), err)
		return
	}
	if requestBody.OpenAIAcceptedFormat == nil && requestBody.OpenAIImageOperations == nil && requestBody.LoadbalanceStrategyID == nil && requestBody.IsEnabled == nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "at least one of openai_accepted_format, openai_image_operations, loadbalance_strategy_id, or is_enabled is required")
		return
	}
	// Preview validates only the dimensions the caller actually proposed; the
	// joint "at least one dimension" rule belongs to the persisted mutation,
	// because a preview may change one dimension while the other stays as
	// authored on the record.
	if requestBody.OpenAIAcceptedFormat != nil || requestBody.OpenAIImageOperations != nil {
		if err := validateProposedOpenAIDimensions(requestBody.OpenAIAcceptedFormat, requestBody.OpenAIImageOperations); err != nil {
			writeDomainError(w, r, s.corsSnapshot(), err)
			return
		}
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "model", func(tx pgx.Tx) (previewDiagnosticsOutcome, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return previewDiagnosticsOutcome{}, err
		}
		records, err := listModelRecords(r.Context(), tx, profile.ID)
		if err != nil {
			return previewDiagnosticsOutcome{}, err
		}
		record, found := findModelRecordByID(records, modelConfigID)
		if !found {
			return previewDiagnosticsOutcome{}, &domainError{StatusCode: http.StatusNotFound, Detail: "Model configuration not found"}
		}
		if requestBody.LoadbalanceStrategyID != nil {
			if err := ensureLoadbalanceStrategyExists(r.Context(), tx, profile.ID, *requestBody.LoadbalanceStrategyID); err != nil {
				return previewDiagnosticsOutcome{}, err
			}
		}
		graph, err := loadRoutingDiagnosticsGraph(r.Context(), tx, profile.ID, records)
		if err != nil {
			return previewDiagnosticsOutcome{}, err
		}
		proposed := record
		if requestBody.OpenAIAcceptedFormat != nil {
			proposed.OpenAIAcceptedFormat = cloneStringPointer(requestBody.OpenAIAcceptedFormat)
		}
		if requestBody.OpenAIImageOperations != nil {
			proposed.OpenAIImageOperations = cloneStringPointer(requestBody.OpenAIImageOperations)
		}
		if requestBody.LoadbalanceStrategyID != nil {
			proposed.LoadbalanceStrategyID = cloneIntPointer(requestBody.LoadbalanceStrategyID)
		}
		if requestBody.IsEnabled != nil {
			proposed.IsEnabled = *requestBody.IsEnabled
		}
		graph.ModelsByID[modelConfigID] = modelrouting.DiagnosticsModel{
			ConfigID:              proposed.ID,
			ProfileID:             proposed.ProfileID,
			ModelID:               proposed.ModelID,
			APIFamily:             proposed.APIFamily,
			IsEnabled:             proposed.IsEnabled,
			OpenAIAcceptedFormat:  cloneStringPointer(proposed.OpenAIAcceptedFormat),
			OpenAIImageOperations: cloneStringPointer(proposed.OpenAIImageOperations),
			LoadbalanceStrategyID: cloneIntPointer(proposed.LoadbalanceStrategyID),
		}
		result := modelrouting.Analyze(graph, modelConfigID, diagnosticsOperationListForModel(proposed))
		return previewDiagnosticsOutcome{
			Result: result,
			Guard:  computeModeChangeGuard(graph, modelConfigID, proposed.OpenAIAcceptedFormat, record.OpenAIAcceptedFormat),
		}, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, routingDiagnosticsPreviewResponse{
		DiagnosticsResult: response.Result,
		ModeChangeGuard:   response.Guard,
	})
}

type previewDiagnosticsOutcome struct {
	Result modelrouting.DiagnosticsResult
	Guard  modeChangeGuard
}

// computeModeChangeGuard reports whether a proposed openai_accepted_format
// change would orphan owner connections whose capability cannot serve the
// proposed mode. It is advisory and read-only: the preview never persists or
// deletes anything, and the blocking count covers every connection attached
// to the root model through its terminal access targets. currentFormat is the
// committed model format before the proposal.
func computeModeChangeGuard(graph *modelrouting.DiagnosticsGraph, modelConfigID int, proposedFormat *string, currentFormat *string) modeChangeGuard {
	guard := modeChangeGuard{Allowed: true, RequiredAction: modeChangeRequiredAction}
	if proposedFormat == nil {
		return guard
	}
	if currentFormat != nil && *currentFormat == *proposedFormat {
		return guard
	}
	if _, ok := graph.ModelsByID[modelConfigID]; !ok {
		return guard
	}
	blocking := 0
	for _, target := range graph.AccessTargetsBySourceModelID[modelConfigID] {
		if target.TargetConnectionID == nil {
			continue
		}
		connection, ok := graph.ConnectionsByID[*target.TargetConnectionID]
		if !ok || connection.OpenAITextCapability == nil {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(*connection.OpenAITextCapability), strings.TrimSpace(*proposedFormat)) {
			blocking++
		}
	}
	guard.BlockingOwnerConnectionCount = blocking
	guard.Allowed = blocking == 0
	return guard
}

func decodeStrictJSONBody(request *http.Request, target any) error {
	defer func() { _ = request.Body.Close() }()
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
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
				ID:                    target.Connection.ID,
				ProfileID:             target.Connection.ProfileID,
				APIFamily:             target.Connection.APIFamily,
				IsActive:              target.Connection.IsActive,
				OpenAITextCapability:  cloneStringPointer(target.Connection.OpenAITextCapability),
				OpenAIImageCapability: cloneStringPointer(target.Connection.OpenAIImageCapability),
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

// validateProposedOpenAIDimensions checks the individual values a diagnostics
// preview proposes. Unlike the mutation validator it does not require at least
// one dimension, because a preview only overlays the dimensions it names.
func validateProposedOpenAIDimensions(acceptedFormat *string, imageOperations *string) error {
	if acceptedFormat != nil && !isValidOpenAIAcceptedFormat(strings.TrimSpace(*acceptedFormat)) {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: "openai_accepted_format must be one of 'responses_only', 'chat_completions_only', or 'dual_native'"}
	}
	if imageOperations != nil && !providerauth.IsSupportedOpenAIImageCapability(*imageOperations) {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: "openai_image_operations must be one of 'generations', 'edits', or 'generations_and_edits'"}
	}
	return nil
}
