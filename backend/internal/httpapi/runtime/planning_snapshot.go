package runtime

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/domain/loadbalance"
	"github.com/coachpo/prism/backend/internal/endpointdomain"
	"github.com/coachpo/prism/backend/internal/profiledomain"
	"github.com/coachpo/prism/backend/internal/providerauth"
)

type planningSnapshot struct {
	ModelsByID map[string]runtimeModelRecord
	// DirectModelsByID is the exact client-ingress allowlist. ModelsByID stays
	// complete so a direct parent can recurse through non-entry Model Targets.
	DirectModelsByID             map[string]runtimeModelRecord
	AccessTargetsBySourceModelID map[int][]runtimeAccessTargetRecord
	TerminalTargetsByID          map[int]runtimeConnection
	StrategiesByModelID          map[int]loadbalance.RuntimeStrategy
	BlocklistRules               []headerBlocklistRule
	ReportCurrency               runtimeReportCurrencySnapshot

	routingPlanOnce sync.Once
	routingPlan     *runtimeRoutingPlan
	routingPlanErr  error
}

func (snapshot *planningSnapshot) compiledRoutingPlan() (*runtimeRoutingPlan, error) {
	if snapshot == nil {
		return nil, invalidRuntimeRoutingPlanError("planning snapshot is nil")
	}
	snapshot.routingPlanOnce.Do(func() {
		snapshot.routingPlan, snapshot.routingPlanErr = compileRuntimeRoutingPlan(snapshot)
		if snapshot.routingPlanErr != nil {
			return
		}
		snapshot.routingPlanErr = validateRuntimeRoutingPlan(snapshot.routingPlan)
	})
	return snapshot.routingPlan, snapshot.routingPlanErr
}

func buildPlanningSnapshot(ctx context.Context, tx pgx.Tx, profileID int, secretEncryptionKey string) (*planningSnapshot, error) {
	modelsByID, directModelsByID, err := listEnabledModelsForProfile(ctx, tx, profileID)
	if err != nil {
		return nil, err
	}
	accessTargetsBySourceModelID, err := listAccessTargetsForProfile(ctx, tx, profileID)
	if err != nil {
		return nil, err
	}
	strategiesByID, err := listRuntimeStrategiesForProfile(ctx, tx, profileID)
	if err != nil {
		return nil, err
	}
	connectionsByID, err := listActiveConnectionsForProfile(ctx, tx, profileID)
	if err != nil {
		return nil, err
	}
	blocklistRules, err := listEnabledHeaderBlocklistRules(ctx, tx, profileID)
	if err != nil {
		return nil, err
	}
	reportCurrency, err := loadRuntimeReportCurrencySnapshot(ctx, tx, profileID)
	if err != nil {
		return nil, err
	}

	for connectionID, connection := range connectionsByID {
		compiled, err := compileRuntimeConnection(connection, connection.APIFamily, secretEncryptionKey)
		if err != nil {
			return nil, fmt.Errorf("compile runtime connection %d for profile %d: %w", connectionID, profileID, err)
		}
		connectionsByID[connectionID] = compiled
	}

	strategiesByModelID := make(map[int]loadbalance.RuntimeStrategy, len(modelsByID))
	for _, model := range modelsByID {
		if model.LoadbalanceStrategyID == nil {
			continue
		}
		if strategy, ok := strategiesByID[*model.LoadbalanceStrategyID]; ok {
			strategiesByModelID[model.ID] = strategy
		}
	}

	snapshot := &planningSnapshot{
		ModelsByID:                   modelsByID,
		DirectModelsByID:             directModelsByID,
		AccessTargetsBySourceModelID: accessTargetsBySourceModelID,
		TerminalTargetsByID:          connectionsByID,
		StrategiesByModelID:          strategiesByModelID,
		BlocklistRules:               blocklistRules,
		ReportCurrency:               reportCurrency,
	}
	return snapshot, nil
}

func compileRuntimeConnection(connection runtimeConnection, apiFamily string, secretEncryptionKey string) (runtimeConnection, error) {
	compiled := connection
	config, err := providerauth.ResolveAuthProfile(connection.AuthType, apiFamily)
	if err != nil {
		return runtimeConnection{}, err
	}
	if strings.TrimSpace(secretEncryptionKey) == "" {
		return compiled, nil
	}
	apiKey, err := endpointdomain.DecryptSecret(connection.EncryptedEndpointAPIKey, secretEncryptionKey)
	if err != nil {
		return runtimeConnection{}, fmt.Errorf("resolve endpoint api key for connection %d: %w", connection.ID, err)
	}
	controlledHeaderNames := config.ControlledHeaderNames()
	extraHeaders := make(map[string]string, len(config.ExtraHeaders))
	for key, value := range config.ExtraHeaders {
		extraHeaders[key] = value
	}
	compiled.UpstreamAuth = &runtimeConnectionUpstreamAuthSnapshot{
		AuthHeader:            config.AuthHeader,
		AuthValue:             config.AuthPrefix + apiKey,
		ExtraHeaders:          extraHeaders,
		ControlledHeaderNames: controlledHeaderNames,
	}
	compiled.EncryptedEndpointAPIKey = ""
	return compiled, nil
}

func listPublishedPlanningProfileIDs(ctx context.Context, tx pgx.Tx) ([]int, error) {
	profile, found, err := profiledomain.LoadNonDeletedProfile(ctx, tx, profiledomain.DefaultProfileID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("%w: default profile %d not found", ErrPublishedRuntimeSnapshotUnavailable, profiledomain.DefaultProfileID)
	}
	return []int{profile.ID}, nil
}
