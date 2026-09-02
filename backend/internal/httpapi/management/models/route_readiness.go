package models

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/domain/modelrouting"
	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/httpapi/runtime"
	"github.com/coachpo/prism/backend/internal/pgxutil"
)

// modelListReadinessEnvelope wraps the models list with the top-level
// profile route-readiness aggregate when include=route_readiness is set.
type modelListReadinessEnvelope struct {
	Items          []modelConfigListResponse          `json:"items"`
	RouteReadiness modelrouting.ProfileRouteReadiness `json:"route_readiness"`
}

// loadRouteWitnessGeneration ensures the per-profile generation row exists and
// returns its current value. The value only ever advances through the
// invalidation middleware's route-affecting mutation hook.
func loadRouteWitnessGeneration(ctx context.Context, exec queryExecutor, profileID int) (int, error) {
	if _, err := exec.Exec(
		ctx,
		`INSERT INTO route_witness_generations (profile_id, generation, updated_at)
		VALUES ($1, 1, $2)
		ON CONFLICT (profile_id) DO NOTHING`,
		profileID,
		nowUTCForModels(),
	); err != nil {
		return 0, fmt.Errorf("ensure route witness generation for profile %d: %w", profileID, err)
	}
	var generation int
	if err := exec.QueryRow(ctx, `SELECT generation FROM route_witness_generations WHERE profile_id = $1`, profileID).Scan(&generation); err != nil {
		return 0, fmt.Errorf("read route witness generation for profile %d: %w", profileID, err)
	}
	return generation, nil
}

// analyzeProfileRouteReadiness computes the immutable route-witness snapshot
// for the profile and returns the top-level aggregate plus per-model
// summaries. unknown is returned for the whole profile when the analyzer
// cannot produce an authoritative snapshot (never a guessed zero).
func analyzeProfileRouteReadiness(ctx context.Context, tx pgx.Tx, profileID int, records []modelRecord) (modelrouting.ProfileRouteReadiness, map[int]modelrouting.ModelRouteReadinessSummary, error) {
	generation, err := loadRouteWitnessGeneration(ctx, tx, profileID)
	if err != nil {
		return modelrouting.ProfileRouteReadiness{}, nil, err
	}
	graph, err := modelrouting.LoadRouteWitnessGraph(ctx, tx, profileID)
	if err != nil {
		return modelrouting.ProfileRouteReadiness{}, nil, err
	}
	snapshot := modelrouting.AnalyzeRouteWitnessSnapshotWithOperations(graph, generation, runtime.ModelBoundRouteWitnessOperations())
	summaries := map[int]modelrouting.ModelRouteReadinessSummary{}
	for _, record := range records {
		summaries[record.ID] = snapshot.ModelSummary(record.ID)
	}
	return snapshot.ProfileReadiness(), summaries, nil
}

func nowUTCForModels() time.Time { return time.Now().UTC() }

// handleGetRouteWitnesses implements the bounded route-witness resolver
// (Model SPEC §4.4.2): fresh-resolve one selected witness (or the stable
// representative default) within a generation snapshot. A stale generation is
// a typed 409 and an unknown selector a typed 404; the coordinator never
// holds the full witness set.
func (s *Service) handleGetRouteWitnesses(w http.ResponseWriter, r *http.Request) {
	generationRaw := strings.TrimSpace(r.URL.Query().Get("generation"))
	selectedID := strings.TrimSpace(r.URL.Query().Get("selected_id"))
	if generationRaw == "" {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "generation is required")
		return
	}
	if !positiveDecimalString(generationRaw) {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "generation must be a positive decimal string")
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "model", func(tx pgx.Tx) (routeWitnessResolveResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return routeWitnessResolveResponse{}, err
		}
		currentGeneration, err := loadRouteWitnessGeneration(r.Context(), tx, profile.ID)
		if err != nil {
			return routeWitnessResolveResponse{}, err
		}
		if !sameDecimalValue(generationRaw, currentGeneration) {
			return routeWitnessResolveResponse{}, &domainError{
				StatusCode: http.StatusConflict,
				Detail:     "route_witness_generation_changed: route witness generation changed; re-select the readiness snapshot",
			}
		}
		graph, err := modelrouting.LoadRouteWitnessGraph(r.Context(), tx, profile.ID)
		if err != nil {
			return routeWitnessResolveResponse{}, err
		}
		snapshot := modelrouting.AnalyzeRouteWitnessSnapshotWithOperations(graph, currentGeneration, runtime.ModelBoundRouteWitnessOperations())
		var witness *modelrouting.RouteWitnessRef
		if selectedID != "" {
			for _, candidate := range snapshot.DirectWitnesses {
				if candidate.WitnessID == selectedID {
					witness = &candidate
					break
				}
			}
			if witness == nil {
				return routeWitnessResolveResponse{}, &domainError{StatusCode: http.StatusNotFound, Detail: "route_witness_not_found: route witness not found; the routing path may have changed"}
			}
		} else {
			witness = snapshot.RepresentativeWitnessRef()
			if witness == nil {
				return routeWitnessResolveResponse{}, &domainError{StatusCode: http.StatusNotFound, Detail: "route_witness_not_found: no route witness available"}
			}
		}
		witness.Generation = generationRaw
		return routeWitnessResolveResponse{Witness: *witness}, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

type routeWitnessResolveResponse struct {
	Witness modelrouting.RouteWitnessRef `json:"witness"`
}

func positiveDecimalString(value string) bool {
	if value == "" {
		return false
	}
	for _, c := range value {
		if c < '0' || c > '9' {
			return false
		}
	}
	return value != "0"
}

func sameDecimalValue(expected string, actual int) bool {
	return expected == fmt.Sprintf("%d", actual)
}
