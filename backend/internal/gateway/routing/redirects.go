package routing

import (
	"net/http"
	"strings"

	gatewaycore "github.com/coachpo/prism/backend/internal/gateway/core"
)

type RedirectType string

const (
	RedirectTypeModel    RedirectType = "model"
	RedirectTypeUpstream RedirectType = "upstream"
)

type RedirectRule struct {
	Type          RedirectType
	TargetModelID string
	UpstreamIDs   []string
}

type RedirectDecision struct {
	Type              RedirectType
	RequestedModelID  string
	EffectiveModelID  string
	PinnedUpstreamIDs []string
	RouteReason       gatewaycore.RouteReason
}

func ApplyRedirect(request PlanRequest, rule RedirectRule, candidatesByModel map[string][]Candidate) (PlanRequest, RedirectDecision, error) {
	requestedModelID := strings.TrimSpace(request.RequestedModelID)
	effectiveModelID := strings.TrimSpace(firstNonEmpty(request.EffectiveModelID, requestedModelID))
	switch rule.Type {
	case RedirectTypeModel:
		return applyModelRedirect(request, requestedModelID, strings.TrimSpace(rule.TargetModelID), candidatesByModel)
	case RedirectTypeUpstream:
		return applyUpstreamRedirect(request, requestedModelID, effectiveModelID, rule.UpstreamIDs)
	default:
		return PlanRequest{}, RedirectDecision{}, gatewaycore.NewGatewayError(gatewaycore.ErrorTypeValidation, "redirect_type_invalid", "Redirect type must be model or upstream", http.StatusBadRequest)
	}
}

func applyModelRedirect(request PlanRequest, requestedModelID string, targetModelID string, candidatesByModel map[string][]Candidate) (PlanRequest, RedirectDecision, error) {
	if requestedModelID == "" || targetModelID == "" {
		return PlanRequest{}, RedirectDecision{}, gatewaycore.NewGatewayError(gatewaycore.ErrorTypeValidation, "model_redirect_target_invalid", "Model redirect requires requested and target model ids", http.StatusBadRequest)
	}
	redirected := request
	redirected.RequestedModelID = requestedModelID
	redirected.EffectiveModelID = targetModelID
	redirected.RouteReason = gatewaycore.RouteReasonModelRedirect
	if candidatesByModel != nil {
		redirected.Candidates = cloneCandidates(candidatesByModel[targetModelID])
	}
	return redirected, RedirectDecision{Type: RedirectTypeModel, RequestedModelID: requestedModelID, EffectiveModelID: targetModelID, RouteReason: gatewaycore.RouteReasonModelRedirect}, nil
}

func applyUpstreamRedirect(request PlanRequest, requestedModelID string, effectiveModelID string, upstreamIDs []string) (PlanRequest, RedirectDecision, error) {
	pins := cleanUpstreamIDs(upstreamIDs)
	if requestedModelID == "" || len(pins) == 0 {
		return PlanRequest{}, RedirectDecision{}, gatewaycore.NewGatewayError(gatewaycore.ErrorTypeValidation, "upstream_redirect_target_invalid", "Upstream redirect requires requested model id and at least one upstream id", http.StatusBadRequest)
	}
	redirected := request
	redirected.RequestedModelID = requestedModelID
	redirected.EffectiveModelID = effectiveModelID
	redirected.RouteReason = gatewaycore.RouteReasonUpstreamRedirect
	redirected.Candidates = filterCandidatesByUpstreamIDs(request.Candidates, pins)
	return redirected, RedirectDecision{Type: RedirectTypeUpstream, RequestedModelID: requestedModelID, EffectiveModelID: effectiveModelID, PinnedUpstreamIDs: pins, RouteReason: gatewaycore.RouteReasonUpstreamRedirect}, nil
}

func filterCandidatesByUpstreamIDs(candidates []Candidate, upstreamIDs []string) []Candidate {
	allowed := make(map[string]struct{}, len(upstreamIDs))
	for _, upstreamID := range upstreamIDs {
		allowed[upstreamID] = struct{}{}
	}
	filtered := make([]Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		if _, ok := allowed[strings.TrimSpace(candidate.UpstreamID)]; ok {
			filtered = append(filtered, candidate)
		}
	}
	return filtered
}

func cleanUpstreamIDs(values []string) []string {
	seen := map[string]struct{}{}
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		cleaned = append(cleaned, trimmed)
	}
	return cleaned
}

func cloneCandidates(source []Candidate) []Candidate {
	if len(source) == 0 {
		return nil
	}
	cloned := make([]Candidate, len(source))
	copy(cloned, source)
	return cloned
}
