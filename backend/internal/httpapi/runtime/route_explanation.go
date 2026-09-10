package runtime

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/coachpo/prism/backend/internal/domain/loadbalance"
	"github.com/coachpo/prism/backend/internal/domain/terminaltarget"
)

const routeExplanationLimit = 256

type RouteExplanationCandidate struct {
	Path             []string `json:"path"`
	Strategy         string   `json:"strategy"`
	ModelConfigID    int      `json:"model_config_id"`
	ModelID          string   `json:"model_id"`
	TerminalTargetID int      `json:"terminal_target_id"`
	PlannerPosition  *int     `json:"planner_position"`
	Schedule         string   `json:"schedule"`
	RuntimeObserved  bool     `json:"runtime_observed"`
	Reason           string   `json:"reason"`
	Capacity         string   `json:"capacity"`
}
type RouteExplanationExclusion struct {
	ModelConfigID int      `json:"model_config_id"`
	Path          []string `json:"path"`
	TargetType    string   `json:"target_type"`
	Reason        string   `json:"reason"`
}
type RouteExplanation struct {
	Exclusions        []RouteExplanationExclusion `json:"exclusions"`
	Generation        string                      `json:"generation"`
	PublishedAt       time.Time                   `json:"published_at"`
	ObservedAt        time.Time                   `json:"observed_at"`
	SampleCompletedAt time.Time                   `json:"sample_completed_at"`
	ModelConfigID     int                         `json:"model_config_id"`
	Operation         string                      `json:"operation"`
	Completeness      string                      `json:"completeness"`
	Boundary          string                      `json:"boundary"`
	PlannerError      string                      `json:"planner_error,omitempty"`
	Candidates        []RouteExplanationCandidate `json:"candidates"`
}

// ExplainRoute observes the already published configuration. It does not
// refresh/invalidate the cache, open a model request, or call live admission.
func (s *Service) ExplainRoute(ctx context.Context, modelID int, operationName string) (RouteExplanation, error) {
	if err := ctx.Err(); err != nil {
		return RouteExplanation{}, err
	}
	published, err := s.cache.requirePublishedSnapshot()
	if err != nil {
		return RouteExplanation{}, err
	}
	snapshot := published.PlanningByProfileID[1]
	if snapshot == nil {
		return RouteExplanation{}, ErrPublishedRuntimeSnapshotUnavailable
	}
	result, err := s.explainRouteSnapshot(ctx, snapshot, modelID, operationName, time.Now().UTC())
	result.Generation = strconv.FormatUint(published.Generation, 10)
	result.PublishedAt = published.PublishedAt
	return result, err
}

func (s *Service) explainRouteSnapshot(ctx context.Context, snapshot *planningSnapshot, modelID int, operationName string, now time.Time) (RouteExplanation, error) {
	result := RouteExplanation{ObservedAt: now, ModelConfigID: modelID, Operation: operationName, Completeness: "sampled", Boundary: "published_configuration_and_independently_sampled_runtime; admission_not_reserved; next_request_not_guaranteed", Candidates: []RouteExplanationCandidate{}, Exclusions: []RouteExplanationExclusion{}}
	var requested runtimeModelRecord
	for _, model := range snapshot.ModelsByID {
		if model.ID == modelID {
			requested = model
			break
		}
	}
	if requested.ID == 0 {
		return result, fmt.Errorf("model not enabled in published configuration")
	}
	var operation RuntimeOperation
	for _, item := range RuntimeOperationCatalog() {
		if item.Name == operationName && item.ModelBindingSource != RuntimeOperationModelBindingNone {
			operation = item
			break
		}
	}
	if operation.Name == "" {
		return result, fmt.Errorf("unknown native operation")
	}
	if err := validateOperationAPIFamily(operation, requested); err != nil {
		return result, err
	}
	plan, err := snapshot.compiledRoutingPlan()
	if err != nil {
		return result, err
	}
	// Bound expanded paths, not only stored edges: shared subgraphs can expand exponentially.
	count := 0
	var check func(int, int) error
	check = func(id, depth int) error {
		if depth > runtimeAccessResolverMaxDepth {
			return fmt.Errorf("route explanation depth exceeds %d", runtimeAccessResolverMaxDepth)
		}
		for _, target := range snapshot.AccessTargetsBySourceModelID[id] {
			count++
			if count > routeExplanationLimit {
				return fmt.Errorf("route explanation exceeds %d expanded targets", routeExplanationLimit)
			}
			if target.TargetModelConfigID != nil {
				if err := check(*target.TargetModelConfigID, depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := check(modelID, 0); err != nil {
		return result, err
	}
	sampled, err := s.runtimeState.CloneForObservation(1, 4096)
	if err != nil {
		return result, err
	}
	observer := &Service{runtimeState: sampled}
	resolved, planningErr := observer.resolveExecutionTargetFromRoutingPlanWithOptions(1, plan, requested, operation, now)
	if planningErr != nil {
		result.PlannerError = planningErr.Error()
	}
	positions := map[string][]int{}
	for index, attempt := range resolved.TerminalAttempts {
		key := routeExplanationPathKey(attempt.ModelPath, attempt.Connection.ID)
		positions[key] = append(positions[key], index+1)
	}
	var visit func(int, []string)
	visit = func(id int, path []string) {
		name := ""
		for _, model := range snapshot.ModelsByID {
			if model.ID == id {
				name = model.ModelID
				break
			}
		}
		path = append(append([]string{}, path...), name)
		for _, target := range snapshot.AccessTargetsBySourceModelID[id] {
			exclude := func(reason string) {
				result.Exclusions = append(result.Exclusions, RouteExplanationExclusion{ModelConfigID: id, Path: append(append([]string{}, path...), target.TargetModelID), TargetType: target.TargetType, Reason: reason})
			}
			if !target.IsEnabled {
				exclude("target_disabled")
				continue
			}
			if target.TargetModelConfigID != nil {
				if _, exists := plan.ModelsByConfigID[*target.TargetModelConfigID]; !exists {
					exclude("target_unavailable")
					continue
				}
				visit(*target.TargetModelConfigID, path)
				continue
			}
			if target.TargetConnectionID == nil {
				continue
			}
			connection, ok := snapshot.TerminalTargetsByID[*target.TargetConnectionID]
			if !ok {
				exclude("target_unavailable")
				continue
			}
			row := RouteExplanationCandidate{Path: path, Strategy: normalizedRuntimeLegacyStrategyType(snapshot.StrategiesByModelID[id]), ModelConfigID: id, TerminalTargetID: connection.ID, Schedule: routeScheduleLabel(connection.RoutingSchedule.DecideAt(now)), Reason: "excluded_by_configuration_or_strategy", Capacity: "not_observed"}
			for _, model := range snapshot.ModelsByID {
				if model.ID == id {
					row.ModelID = model.ModelID
				}
			}
			states := sampled.SnapshotConnectionStates(1, []loadbalance.RuntimeConnectionRef{{ConnectionID: connection.ID, ModelConfigID: id}})
			state, observed := states[connection.ID]
			row.RuntimeObserved = observed
			if !observed {
				result.Completeness = "partial"
			}
			key := routeExplanationPathKey(path, connection.ID)
			if ordered := positions[key]; len(ordered) > 0 {
				position := ordered[0]
				positions[key] = ordered[1:]
				row.PlannerPosition = &position
				row.Reason = "planner_candidate"
			} else if observed && len(loadbalance.FilterEligibleConnectionIDs([]loadbalance.ConnectionOrderCandidate{{ID: connection.ID}}, states, now)) == 0 {
				row.Reason = "banned"
			} else if connection.RoutingSchedule.DecideAt(now) == terminaltarget.RoutingScheduleClosed {
				row.Reason = "schedule_closed"
			}
			if observed {
				row.Capacity = "sampled_not_reserved"
				admission := loadbalance.RuntimeConnectionAdmission{QPSLimit: connection.QPSLimit, MaxInFlightNonStream: connection.MaxInFlightNonStream, MaxInFlightStream: connection.MaxInFlightStream}
				strategy := snapshot.StrategiesByModelID[id]
				nonStream := loadbalance.AdmissionRejectionReason(state, admission, strategy.AdmissionPolicy(), false, now)
				stream := loadbalance.AdmissionRejectionReason(state, admission, strategy.AdmissionPolicy(), true, now)
				if nonStream != "" || stream != "" {
					row.Capacity = "sampled_limit_reached"
				}
			}
			result.Candidates = append(result.Candidates, row)
		}
	}
	visit(modelID, nil)
	sort.SliceStable(result.Candidates, func(i, j int) bool {
		a, b := result.Candidates[i].PlannerPosition, result.Candidates[j].PlannerPosition
		if a != nil && b != nil {
			return *a < *b
		}
		return a != nil && b == nil
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	result.SampleCompletedAt = time.Now().UTC()
	return result, nil
}

func routeScheduleLabel(decision terminaltarget.RoutingScheduleDecision) string {
	switch decision {
	case terminaltarget.RoutingScheduleUnrestricted:
		return "unrestricted"
	case terminaltarget.RoutingScheduleOpen:
		return "open"
	case terminaltarget.RoutingScheduleClosed:
		return "closed"
	default:
		return "unresolved"
	}
}

func routeExplanationPathKey(path []string, id int) string {
	return strings.Join(path, "\x00") + "\x00" + strconv.Itoa(id)
}
