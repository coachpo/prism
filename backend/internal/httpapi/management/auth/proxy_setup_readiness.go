package auth

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/coachpo/prism/backend/internal/domain/modelrouting"
	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/httpapi/runtime"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	"github.com/jackc/pgx/v5"
)

// ProxySetupReadiness is the aggregate projection returned by
// GET /api/settings/auth/proxy-keys?include=setup_readiness&expected_route_witness_generation={G}.
// Auth enabled: configuration requires an active, server-clock-unexpired key
// (the merged schema has no scope/RPM contract, so a valid key covers every
// route-ready model and application additionally requires a route witness).
// Auth disabled: application is exactly not_required and only optional
// attribution is projected.
type ProxySetupReadiness struct {
	EvaluatedRouteWitnessGeneration   string                         `json:"evaluated_route_witness_generation"`
	ProxyKeyOwnerRevision             string                         `json:"proxy_key_owner_revision"`
	Configuration                     modelrouting.ReadinessAxis     `json:"configuration"`
	Application                       modelrouting.ReadinessAxis     `json:"application"`
	RouteWitnessCount                 int                            `json:"route_witness_count"`
	MatchingWitnessCount              int                            `json:"matching_witness_count"`
	OptionalAttributionWitnessCount   *int                           `json:"optional_attribution_witness_count"`
	RepresentativeMatching            *connectionsMatchingProjection `json:"representative_matching"`
	RepresentativeOptionalAttribution *connectionsMatchingProjection `json:"representative_optional_attribution"`
}

// connectionsMatchingProjection mirrors the shared matching projection shape
// (Pricing/Proxy owners use the same wire).
type connectionsMatchingProjection struct {
	Witness modelrouting.RouteWitnessRef `json:"witness"`
	Model   modelrouting.ModelEntityRef  `json:"model"`
}

// handleListProxyKeysWithSetupReadiness builds the setup readiness projection
// on the proxy-keys list. It is read-only: the expected generation binds one
// immutable analyzer snapshot; a mismatch is a typed 409.
func (s *Service) handleListProxyKeysWithSetupReadiness(w http.ResponseWriter, r *http.Request, expectedGeneration string) {
	setNoStoreHeaders(w)
	requestContext := r.Context()
	response, err := pgxutil.InTxValue(requestContext, s.pool, "auth", func(tx pgx.Tx) (ProxySetupReadiness, error) {
		currentGeneration, err := loadRouteWitnessGenerationForAuth(requestContext, tx, s.nowUTC())
		if err != nil {
			return ProxySetupReadiness{}, err
		}
		if !sameDecimalValue(expectedGeneration, currentGeneration) {
			return ProxySetupReadiness{}, &domainError{
				StatusCode: http.StatusConflict,
				Detail:     "route_witness_generation_changed: route witness generation changed; re-select the readiness snapshot",
			}
		}
		settingsRow, err := s.loadOrCreateAppAuthSettings(requestContext, tx)
		if err != nil {
			return ProxySetupReadiness{}, fmt.Errorf("load auth settings: %w", err)
		}
		// Reuse the Proxy owner's counted readiness snapshot.  This keeps the
		// setup projection on the same server-clock safety horizon as the Auth
		// enable fence; a second live-key scan here could report a key as usable
		// after it had crossed the 30-second activation boundary.
		keyReadiness, err := s.captureProxyKeyReadiness(requestContext, tx)
		if err != nil {
			// Do not turn an owner-read failure into a fabricated not-ready
			// projection (or zero counts). The setup surface has no safe
			// unavailable sub-union, so keep the typed service-unavailable
			// boundary used by Auth activation.
			return ProxySetupReadiness{}, &domainError{
				StatusCode: http.StatusServiceUnavailable,
				Code:       "proxy_key_readiness_unavailable",
				Detail:     "proxy key readiness is temporarily unavailable; retry later",
				Fields: map[string]any{"details": map[string]any{
					"recovery":            "retry",
					"retry_after_seconds": 5,
				}},
			}
		}
		// The readiness snapshot is the Proxy owner's single counted clock and
		// generation. Do not take a second COUNT/MAX(updated_at) snapshot here:
		// that would let setup readiness disagree with the activation fence at
		// the expiry horizon.
		revision := keyReadiness.Generation
		graph, err := modelrouting.LoadRouteWitnessGraph(requestContext, tx, profileIDForAuthRead(r))
		if err != nil {
			return ProxySetupReadiness{}, err
		}
		snapshot := modelrouting.AnalyzeRouteWitnessSnapshotWithOperations(graph, currentGeneration, runtime.ModelBoundRouteWitnessOperations())

		effectiveKeyExists := keyReadiness.SafeActive > 0
		configuration := modelrouting.ReadinessAxis{State: "not_ready", ReasonCodes: []string{"no_effective_key"}}
		if effectiveKeyExists {
			configuration = modelrouting.ReadinessAxis{State: "ready", ReasonCodes: []string{}}
		}
		witnessCount := snapshot.DirectRouteWitnessCount
		application := modelrouting.ReadinessAxis{State: "not_ready", ReasonCodes: []string{"no_route_witness"}}
		matchingCount := 0
		if witnessCount > 0 && effectiveKeyExists {
			application = modelrouting.ReadinessAxis{State: "ready", ReasonCodes: []string{}}
			matchingCount = witnessCount
		}
		if !settingsRow.AuthEnabled {
			application = modelrouting.ReadinessAxis{State: "not_required", ReasonCodes: []string{}}
			// Optional attribution: an effective key may attribute, but scope
			// and RPM never gate runtime access when auth is disabled.
			optionalCount := 0
			if effectiveKeyExists {
				optionalCount = witnessCount
			}
			optional := &optionalCount
			optionalProjection := &connectionsMatchingProjection{}
			if optionalCount > 0 {
				modelRef, refErr := loadModelEntityRefForAuth(requestContext, tx, snapshot)
				if refErr != nil {
					return ProxySetupReadiness{}, refErr
				}
				witness := snapshot.RepresentativeWitnessRef()
				if witness != nil {
					optionalProjection = &connectionsMatchingProjection{Witness: *witness, Model: *modelRef}
				} else {
					optionalProjection = nil
				}
			} else {
				optionalProjection = nil
			}
			return ProxySetupReadiness{
				EvaluatedRouteWitnessGeneration:   fmt.Sprintf("%d", currentGeneration),
				ProxyKeyOwnerRevision:             revision,
				Configuration:                     configuration,
				Application:                       application,
				RouteWitnessCount:                 witnessCount,
				MatchingWitnessCount:              0,
				OptionalAttributionWitnessCount:   optional,
				RepresentativeOptionalAttribution: optionalProjection,
			}, nil
		}
		var matchingProjection *connectionsMatchingProjection
		if matchingCount > 0 {
			modelRef, refErr := loadModelEntityRefForAuth(requestContext, tx, snapshot)
			if refErr != nil {
				return ProxySetupReadiness{}, refErr
			}
			witness := snapshot.RepresentativeWitnessRef()
			if witness != nil {
				matchingProjection = &connectionsMatchingProjection{Witness: *witness, Model: *modelRef}
			}
		}
		return ProxySetupReadiness{
			EvaluatedRouteWitnessGeneration: fmt.Sprintf("%d", currentGeneration),
			ProxyKeyOwnerRevision:           revision,
			Configuration:                   configuration,
			Application:                     application,
			RouteWitnessCount:               witnessCount,
			MatchingWitnessCount:            matchingCount,
			RepresentativeMatching:          matchingProjection,
		}, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func loadRouteWitnessGenerationForAuth(ctx context.Context, exec queryExecutor, now time.Time) (int, error) {
	if _, err := exec.Exec(
		ctx,
		`INSERT INTO route_witness_generations (profile_id, generation, updated_at)
		VALUES ($1, 1, $2) ON CONFLICT (profile_id) DO NOTHING`,
		1,
		now,
	); err != nil {
		return 0, fmt.Errorf("ensure route witness generation: %w", err)
	}
	var generation int
	if err := exec.QueryRow(ctx, `SELECT generation FROM route_witness_generations WHERE profile_id = $1`, 1).Scan(&generation); err != nil {
		return 0, fmt.Errorf("read route witness generation: %w", err)
	}
	return generation, nil
}

func sameDecimalValue(expected string, actual int) bool {
	return expected == fmt.Sprintf("%d", actual)
}

func profileIDForAuthRead(r *http.Request) int {
	// ponytail: profile pinned to Default(1) for management reads.
	return 1
}

// loadModelEntityRefForAuth resolves the canonical Model ref for the
// snapshot's representative witness.
func loadModelEntityRefForAuth(ctx context.Context, exec queryExecutor, snapshot modelrouting.RouteWitnessSnapshot) (*modelrouting.ModelEntityRef, error) {
	witness := snapshot.RepresentativeWitnessRef()
	if witness == nil {
		return nil, nil
	}
	var displayName *string
	var modelID string
	if err := exec.QueryRow(ctx, `SELECT model_id, display_name FROM model_configs WHERE id = $1`, witness.ModelConfigID).Scan(&modelID, &displayName); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("load model ref for witness %s: %w", witness.WitnessID, err)
	}
	deleted := false
	name := modelID
	nameSource := "current"
	if displayName != nil && strings.TrimSpace(*displayName) != "" {
		name = strings.TrimSpace(*displayName)
	}
	return &modelrouting.ModelEntityRef{
		Kind:          "model",
		ModelConfigID: witness.ModelConfigID,
		ModelID:       modelID,
		Name:          name,
		NameSource:    nameSource,
		Deleted:       &deleted,
	}, nil
}
