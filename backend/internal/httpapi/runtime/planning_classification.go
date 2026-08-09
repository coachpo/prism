package runtime

import (
	"net/http"
	"strings"

	"github.com/coachpo/prism/backend/internal/domain/modelrouting"
	"github.com/coachpo/prism/backend/internal/providerauth"
)

// classifyOpenAIPlanningRejection maps a failed OpenAI text resolution to one
// of the three stable planning codes:
//
//   - openai_no_compatible_terminal_target: the authored graph reachable from
//     the root model contains no capability-compatible Terminal leaf (ignoring
//     enable/active/strategy truncation);
//   - openai_no_eligible_terminal_target: compatible leaves exist, but every
//     one is statically ineligible (disabled row, inactive connection or
//     strategy truncation);
//   - ordinary 503 (dynamic): a statically eligible compatible route existed
//     but was dynamically unavailable (Ban/retry/admission).
//
// The classification is runtime-reachable-graph scoped: model targets only
// follow enabled same-profile/same-family models, matching what the runtime
// can actually resolve.
func (s *Service) classifyOpenAIPlanningRejection(profileID int, routingPlan *runtimeRoutingPlan, model runtimeModelRecord, operation RuntimeOperation, observation *runtimePlanningObservation) error {
	if observation != nil && observation.CompatibleStaticRouteSeen {
		return &domainError{StatusCode: http.StatusServiceUnavailable, Detail: noEligibleTargetsErrorDetail(model.ModelID)}
	}
	if staticGraphHasCompatibleTerminalLeaf(profileID, routingPlan, model, operation, map[int]struct{}{}) {
		return openAINoEligibleTerminalTargetDomainError()
	}
	return openAINoCompatibleTerminalTargetDomainError()
}

// staticGraphHasCompatibleTerminalLeaf walks the authored access-target graph
// reachable from model and reports whether any Terminal connection is
// capability-compatible with the operation. It ignores access-target row
// enablement, connection activation and strategy truncation, and never reads
// Ban/retry/admission/in-flight state or round-robin cursors.
func staticGraphHasCompatibleTerminalLeaf(profileID int, routingPlan *runtimeRoutingPlan, model runtimeModelRecord, operation RuntimeOperation, visited map[int]struct{}) bool {
	if _, seen := visited[model.ID]; seen {
		return false
	}
	authored := routingPlan.authoredTargetsForModel(model.ID)
	if len(authored) == 0 {
		return false
	}
	nextVisited := cloneVisitedModelIDs(visited)
	nextVisited[model.ID] = struct{}{}
	for _, target := range authored {
		if target.ProfileID != profileID || target.SourceModelConfigID != model.ID {
			continue
		}
		switch target.TargetType {
		case runtimeAccessTargetTypeModel:
			childModel, ok, err := routingPlan.modelForAccessTarget(model, target)
			if err != nil || !ok {
				continue
			}
			if staticGraphHasCompatibleTerminalLeaf(profileID, routingPlan, childModel, operation, nextVisited) {
				return true
			}
		case runtimeAccessTargetTypeConnection:
			if target.TargetConnectionID == nil {
				continue
			}
			if target.TargetConnectionProfileID != 0 && target.TargetConnectionProfileID != profileID {
				continue
			}
			if strings.TrimSpace(target.TargetConnectionAPIFamily) != "" && !modelrouting.SameAPIFamily(target.TargetConnectionAPIFamily, model.APIFamily) {
				continue
			}
			if target.ConnectionOpenAITextCapability != nil && providerauth.OpenAITextModesMatch(model.OpenAIAcceptedFormat, target.ConnectionOpenAITextCapability) && providerauth.OpenAITextCapabilitySupportsNativeOperation(*target.ConnectionOpenAITextCapability, operation.Name) {
				return true
			}
		}
	}
	return false
}

func noEligibleTargetsErrorDetail(requestedModelID string) string {
	return "No eligible targets available for model '" + strings.TrimSpace(requestedModelID) + "'."
}
