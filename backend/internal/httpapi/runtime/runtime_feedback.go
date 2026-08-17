package runtime

import (
	"context"
	"time"

	"github.com/coachpo/prism/backend/internal/domain/loadbalance"
	"github.com/jackc/pgx/v5/pgxpool"
)

type runtimeFeedbackStore struct {
	pool *pgxpool.Pool
}

func newRuntimeFeedbackStore(pool *pgxpool.Pool) *runtimeFeedbackStore {
	return &runtimeFeedbackStore{pool: pool}
}

func (s *Service) recordRuntimeUnbanned(ctx context.Context, plan requestPlan, connection runtimeConnection, state loadbalance.RuntimeConnectionState, observedAt time.Time) {
	s.enqueueRuntimeFeedback(ctx, plan.RuntimeOperation.Name, runtimeFeedbackEvent{Kind: runtimeFeedbackUnbanned, ProfileID: plan.ProfileID, ConnectionID: connection.ID, ModelConfigID: connection.ModelConfigID, State: state, ObservedAt: observedAt})
}

func (s *Service) recordRuntimeAdmissionRejected(ctx context.Context, plan requestPlan, connection runtimeConnection, state loadbalance.RuntimeConnectionState, observedAt time.Time) {
	s.enqueueRuntimeFeedback(ctx, plan.RuntimeOperation.Name, runtimeFeedbackEvent{Kind: runtimeFeedbackAdmissionRejected, ProfileID: plan.ProfileID, ConnectionID: connection.ID, ModelConfigID: connection.ModelConfigID, State: state, ObservedAt: observedAt})
}

func (s *Service) recordRuntimeSuccess(ctx context.Context, plan requestPlan, connection runtimeConnection, strategy loadbalance.RuntimeStrategy, responseHeadersLatencyMS int, completedAt time.Time) {
	if s == nil || s.runtimeState == nil {
		return
	}
	transition := s.runtimeState.RecordRuntimeSuccess(plan.ProfileID, connection.ModelConfigID, connection.ID, strategy, responseHeadersLatencyMS, completedAt)
	if !transition.RecoveryEventEligible {
		return
	}
	s.enqueueRuntimeFeedback(ctx, plan.RuntimeOperation.Name, runtimeFeedbackEvent{Kind: runtimeFeedbackSuccessRecovery, ProfileID: plan.ProfileID, ConnectionID: connection.ID, ModelConfigID: connection.ModelConfigID, Strategy: strategy, Transition: transition, CompletedAt: completedAt, ResponseTimeMS: responseHeadersLatencyMS})
}

func (s *Service) recordRuntimeFailoverHTTPFailure(ctx context.Context, plan requestPlan, connection runtimeConnection, strategy loadbalance.RuntimeStrategy, completedAt time.Time) {
	if s == nil || s.runtimeState == nil {
		return
	}
	transition := s.runtimeState.RecordRuntimeFailoverHTTPFailure(plan.ProfileID, connection.ModelConfigID, connection.ID, strategy, completedAt)
	s.enqueueRuntimeFeedback(ctx, plan.RuntimeOperation.Name, runtimeFeedbackEvent{Kind: runtimeFeedbackFailoverHTTP, ProfileID: plan.ProfileID, ConnectionID: connection.ID, ModelConfigID: connection.ModelConfigID, Strategy: strategy, Transition: transition, FailureKind: "transient_http", CompletedAt: completedAt})
}

func (s *Service) recordRuntimeTransportFailure(ctx context.Context, plan requestPlan, connection runtimeConnection, strategy loadbalance.RuntimeStrategy, completedAt time.Time) {
	if s == nil || s.runtimeState == nil {
		return
	}
	transition := s.runtimeState.RecordRuntimeTransportFailure(plan.ProfileID, connection.ModelConfigID, connection.ID, strategy, completedAt)
	s.enqueueRuntimeFeedback(ctx, plan.RuntimeOperation.Name, runtimeFeedbackEvent{Kind: runtimeFeedbackTransportFailure, ProfileID: plan.ProfileID, ConnectionID: connection.ID, ModelConfigID: connection.ModelConfigID, Strategy: strategy, Transition: transition, FailureKind: "connect_error", CompletedAt: completedAt})
}

func (s *Service) enqueueRuntimeFeedback(ctx context.Context, operationName string, event runtimeFeedbackEvent) {
	if s == nil {
		return
	}
	event.APIFamily = eventAPIFamily(event.APIFamily, operationName)
	if event.TraceContext.empty() {
		event.TraceContext = runtimeTraceContextFromContext(ctx)
	}
	if s.feedbackPipeline != nil {
		s.feedbackPipeline.TryEnqueueContext(contextFromContext(ctx), event)
	}
}

func contextFromContext(ctx context.Context) context.Context {
	if ctx != nil {
		return ctx
	}
	return context.Background()
}
