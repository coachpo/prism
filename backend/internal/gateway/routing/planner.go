package routing

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

import gatewaycore "github.com/coachpo/prism/backend/internal/gateway/core"

type Candidate struct {
	UpstreamID     string
	ModelID        string
	EndpointID     *int
	Priority       int
	Weight         int
	Healthy        bool
	CircuitOpen    bool
	QPSLimit       int
	RPMLimit       int
	TPMLimit       int
	IPMLimit       int
	MaxConcurrency int
}

type ReservationRequest struct {
	OperationName      string
	RequestedModelID   string
	EffectiveModelID   string
	TextEndpoint       bool
	ImageEndpoint      bool
	InputTokens        int
	OutputTokens       int
	ImageCount         int
	RequireQPS         bool
	RequireRPM         bool
	RequireTPM         bool
	RequireIPM         bool
	RequireConcurrency bool
}

type Reservation interface {
	Release()
}

type ReservationResult struct {
	Reservation Reservation
	Rejected    bool
	Reason      gatewaycore.RouteReason
}

type ReservationManager interface {
	Reserve(ctx context.Context, candidate Candidate, request ReservationRequest) (ReservationResult, error)
}

type Planner struct {
	Reservations  ReservationManager
	Deterministic bool
	WeightOffset  int
}

type PlanRequest struct {
	OperationName    string
	RequestedModelID string
	EffectiveModelID string
	RouteReason      gatewaycore.RouteReason
	Candidates       []Candidate
	Reservation      ReservationRequest
}

type Plan struct {
	OperationName     string
	RequestedModelID  string
	EffectiveModelID  string
	Selected          Candidate
	Reservation       Reservation
	RouteReason       gatewaycore.RouteReason
	CandidateAttempts []gatewaycore.RouteAttempt
}

func (planner Planner) Select(ctx context.Context, request PlanRequest) (Plan, error) {
	ordered := OrderCandidates(request.Candidates, planner.WeightOffset, planner.Deterministic)
	attempts := make([]gatewaycore.RouteAttempt, 0, len(ordered))
	healthyCandidates := 0
	reservationRejects := 0
	lastReservationReason := gatewaycore.RouteReasonPolicyReject
	reservationRequest := request.Reservation
	reservationRequest.OperationName = strings.TrimSpace(firstNonEmpty(reservationRequest.OperationName, request.OperationName))
	reservationRequest.RequestedModelID = strings.TrimSpace(firstNonEmpty(reservationRequest.RequestedModelID, request.RequestedModelID))
	reservationRequest.EffectiveModelID = strings.TrimSpace(firstNonEmpty(reservationRequest.EffectiveModelID, request.EffectiveModelID))
	reservationRequest, err := NormalizeReservationRequest(reservationRequest)
	if err != nil {
		return Plan{}, err
	}

	for _, candidate := range ordered {
		if strings.TrimSpace(candidate.UpstreamID) == "" {
			continue
		}
		if !candidate.Healthy || candidate.CircuitOpen {
			attempts = append(attempts, routeAttempt(len(attempts)+1, candidate, gatewaycore.RouteReasonCircuitOpenSkip))
			continue
		}
		healthyCandidates++
		result, err := planner.reserve(ctx, candidate, reservationRequest)
		if err != nil {
			return Plan{}, err
		}
		if result.Rejected {
			reservationRejects++
			reason := canonicalReservationReason(result.Reason)
			lastReservationReason = reason
			attempts = append(attempts, routeAttempt(len(attempts)+1, candidate, reason))
			continue
		}
		attempts = append(attempts, routeAttempt(len(attempts)+1, candidate, selectedAttemptRouteReason(request.RouteReason)))
		return Plan{
			OperationName:     reservationRequest.OperationName,
			RequestedModelID:  reservationRequest.RequestedModelID,
			EffectiveModelID:  reservationRequest.EffectiveModelID,
			Selected:          candidate,
			Reservation:       result.Reservation,
			RouteReason:       selectedRouteReason(attempts),
			CandidateAttempts: attempts,
		}, nil
	}

	if healthyCandidates == 0 {
		return Plan{}, gatewaycore.NewGatewayError(gatewaycore.ErrorTypeRouting, string(gatewaycore.RouteReasonNoHealthyUpstream), "No healthy upstream candidates are available", http.StatusServiceUnavailable)
	}
	if reservationRejects > 0 {
		return Plan{}, gatewaycore.NewGatewayError(gatewaycore.ErrorTypeAdmission, "admission_exhausted", fmt.Sprintf("All upstream reservations were rejected with route_reason %q", lastReservationReason), http.StatusServiceUnavailable,
			gatewaycore.FieldError{Field: "route_reason", Code: string(lastReservationReason), Detail: "reservation exhausted candidate set"},
		)
	}
	return Plan{}, gatewaycore.NewGatewayError(gatewaycore.ErrorTypeRouting, string(gatewaycore.RouteReasonNoHealthyUpstream), "No routable upstream candidates are available", http.StatusServiceUnavailable)
}

func OrderCandidates(candidates []Candidate, weightOffset int, deterministic bool) []Candidate {
	eligible := make([]Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate.UpstreamID) != "" {
			eligible = append(eligible, candidate)
		}
	}
	if len(eligible) == 0 {
		return nil
	}
	sort.SliceStable(eligible, func(left int, right int) bool {
		if eligible[left].Priority != eligible[right].Priority {
			return eligible[left].Priority < eligible[right].Priority
		}
		return strings.TrimSpace(eligible[left].UpstreamID) < strings.TrimSpace(eligible[right].UpstreamID)
	})
	ordered := make([]Candidate, 0, len(eligible))
	for start := 0; start < len(eligible); {
		end := start + 1
		for end < len(eligible) && eligible[end].Priority == eligible[start].Priority {
			end++
		}
		ordered = append(ordered, rotateWeightedTier(eligible[start:end], weightOffset, deterministic)...)
		start = end
	}
	return ordered
}

func rotateWeightedTier(tier []Candidate, weightOffset int, deterministic bool) []Candidate {
	ordered := append([]Candidate(nil), tier...)
	if deterministic || len(ordered) < 2 {
		return ordered
	}
	totalWeight := 0
	for _, candidate := range ordered {
		totalWeight += effectiveWeight(candidate.Weight)
	}
	if totalWeight <= 0 {
		return ordered
	}
	offset := weightOffset % totalWeight
	if offset < 0 {
		offset += totalWeight
	}
	start := 0
	cumulative := 0
	for index, candidate := range ordered {
		cumulative += effectiveWeight(candidate.Weight)
		if offset < cumulative {
			start = index
			break
		}
	}
	if start == 0 {
		return ordered
	}
	return append(ordered[start:], ordered[:start]...)
}

func NormalizeReservationRequest(request ReservationRequest) (ReservationRequest, error) {
	request.OperationName = strings.TrimSpace(request.OperationName)
	request.RequestedModelID = strings.TrimSpace(request.RequestedModelID)
	request.EffectiveModelID = strings.TrimSpace(request.EffectiveModelID)
	if request.TextEndpoint && request.ImageEndpoint {
		return ReservationRequest{}, gatewaycore.NewGatewayError(gatewaycore.ErrorTypeValidation, "ambiguous_endpoint_type", "Reservation request cannot be both text and image endpoint", http.StatusBadRequest)
	}
	if !request.TextEndpoint && !request.ImageEndpoint {
		request.TextEndpoint, request.ImageEndpoint = inferReservationEndpointShape(request.OperationName)
	}
	request.RequireQPS = true
	request.RequireConcurrency = true
	if request.TextEndpoint {
		request.RequireRPM = true
		request.RequireTPM = true
	}
	if request.ImageEndpoint {
		request.RequireIPM = true
	}
	return request, nil
}

func inferReservationEndpointShape(operationName string) (bool, bool) {
	normalized := strings.ToLower(strings.TrimSpace(operationName))
	if strings.Contains(normalized, "image") {
		return false, true
	}
	switch normalized {
	case "openai.chat_completions", "openai.responses", "anthropic.messages", "anthropic.count_tokens", "gemini.generate_content", "gemini.stream_generate_content", "gemini.count_tokens":
		return true, false
	default:
		return false, false
	}
}

func (planner Planner) reserve(ctx context.Context, candidate Candidate, request ReservationRequest) (ReservationResult, error) {
	if planner.Reservations == nil {
		return ReservationResult{}, nil
	}
	return planner.Reservations.Reserve(ctx, candidate, request)
}

func routeAttempt(number int, candidate Candidate, reason gatewaycore.RouteReason) gatewaycore.RouteAttempt {
	return gatewaycore.RouteAttempt{AttemptNumber: number, UpstreamID: strings.TrimSpace(candidate.UpstreamID), EndpointID: cloneInt(candidate.EndpointID), Reason: canonicalRouteReason(reason)}
}

func selectedRouteReason(attempts []gatewaycore.RouteAttempt) gatewaycore.RouteReason {
	redirectReason := gatewaycore.RouteReasonDirectMatch
	for _, attempt := range attempts {
		switch attempt.Reason {
		case gatewaycore.RouteReasonQPSOverflow, gatewaycore.RouteReasonRPMOverflow, gatewaycore.RouteReasonTPMOverflow, gatewaycore.RouteReasonIPMOverflow, gatewaycore.RouteReasonConcurrencyOverflow, gatewaycore.RouteReasonRetry429, gatewaycore.RouteReasonRetry5xx, gatewaycore.RouteReasonRetryHTTP, gatewaycore.RouteReasonRetryConnectTimeout, gatewaycore.RouteReasonRetryTransport:
			return attempt.Reason
		case gatewaycore.RouteReasonModelRedirect, gatewaycore.RouteReasonUpstreamRedirect:
			if redirectReason == gatewaycore.RouteReasonDirectMatch {
				redirectReason = attempt.Reason
			}
		}
	}
	return redirectReason
}

func selectedAttemptRouteReason(reason gatewaycore.RouteReason) gatewaycore.RouteReason {
	switch canonicalRouteReason(reason) {
	case gatewaycore.RouteReasonModelRedirect, gatewaycore.RouteReasonUpstreamRedirect:
		return reason
	default:
		return gatewaycore.RouteReasonDirectMatch
	}
}

func canonicalReservationReason(reason gatewaycore.RouteReason) gatewaycore.RouteReason {
	canonical := canonicalRouteReason(reason)
	switch canonical {
	case gatewaycore.RouteReasonDirectMatch, gatewaycore.RouteReasonQPSOverflow, gatewaycore.RouteReasonRPMOverflow, gatewaycore.RouteReasonTPMOverflow, gatewaycore.RouteReasonIPMOverflow, gatewaycore.RouteReasonConcurrencyOverflow, gatewaycore.RouteReasonCircuitOpenSkip, gatewaycore.RouteReasonNoHealthyUpstream, gatewaycore.RouteReasonPolicyReject:
		return canonical
	default:
		return gatewaycore.RouteReasonPolicyReject
	}
}

func canonicalRouteReason(reason gatewaycore.RouteReason) gatewaycore.RouteReason {
	switch reason {
	case gatewaycore.RouteReasonDirectMatch, gatewaycore.RouteReasonModelRedirect, gatewaycore.RouteReasonUpstreamRedirect, gatewaycore.RouteReasonQPSOverflow, gatewaycore.RouteReasonRPMOverflow, gatewaycore.RouteReasonTPMOverflow, gatewaycore.RouteReasonIPMOverflow, gatewaycore.RouteReasonConcurrencyOverflow, gatewaycore.RouteReasonRetry429, gatewaycore.RouteReasonRetry5xx, gatewaycore.RouteReasonRetryHTTP, gatewaycore.RouteReasonRetryConnectTimeout, gatewaycore.RouteReasonRetryTransport, gatewaycore.RouteReasonCircuitOpenSkip, gatewaycore.RouteReasonNoHealthyUpstream, gatewaycore.RouteReasonPolicyReject:
		return reason
	default:
		return gatewaycore.RouteReasonPolicyReject
	}
}

func effectiveWeight(weight int) int {
	if weight <= 0 {
		return 1
	}
	return weight
}

func firstNonEmpty(left string, right string) string {
	if strings.TrimSpace(left) != "" {
		return left
	}
	return right
}

func cloneInt(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
