package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

func (s *Service) recordRuntimeActivity(plan requestPlan, result executionResult, request *http.Request, startedAt time.Time, responseCapture runtimeResponseCapture) {
	if s == nil || s.runtimeSideEffects == nil {
		return
	}
	ctx := runtimeRequestContext(request)
	envelope := s.buildRuntimeActivityEnvelope(ctx, plan, result, request, startedAt, responseCapture)
	intent := RuntimeActivityIntent{Envelope: envelope, TraceContext: envelope.TraceContext}
	if submit := s.runtimeSideEffects.SubmitRuntimeActivityContext(ctx, intent); submit.Status != RuntimeSideEffectAccepted {
		slog.Error("failed to accept runtime activity telemetry intent", "reason", submit.Reason, "profile_id", envelope.UsageEvent.ProfileID, "ingress_request_id", envelope.UsageEvent.IngressRequestID)
	}
}

func (s *Service) enqueueRuntimeActivityBeforeResponse(plan requestPlan, result executionResult, request *http.Request, startedAt time.Time, responseCapture runtimeResponseCapture) error {
	if s == nil || s.runtimeSideEffects == nil {
		return fmt.Errorf("runtime telemetry outbox unavailable")
	}
	ctx := runtimeRequestContext(request)
	envelope := s.buildRuntimeActivityEnvelope(ctx, plan, result, request, startedAt, responseCapture)
	if err := s.validateRuntimeActivityHandoff(envelope); err != nil {
		return err
	}
	intent := RuntimeActivityIntent{Envelope: envelope, TraceContext: envelope.TraceContext}
	if err := s.runtimeSideEffects.CommitRuntimeActivityBeforeResponse(ctx, intent); err != nil {
		return err
	}
	return nil
}

func (s *Service) enqueueStreamingRuntimeActivityAcceptedBeforeResponse(plan requestPlan, result executionResult, request *http.Request, startedAt time.Time) (int64, error) {
	if s == nil || s.runtimeSideEffects == nil {
		return 0, fmt.Errorf("runtime telemetry outbox unavailable")
	}
	ctx := runtimeRequestContext(request)
	acceptedAt := s.nowUTC()
	envelope := s.buildRuntimeActivityEnvelope(ctx, plan, result, request, startedAt, runtimeResponseCapture{CompletedAt: &acceptedAt, StreamOutcome: runtimeStreamOutcomeUnknown})
	envelope.HandoffPhase = runtimeTelemetryHandoffPhaseStreamAccepted
	if err := s.validateRuntimeActivityHandoff(envelope); err != nil {
		return 0, err
	}
	intent := RuntimeActivityIntent{Envelope: envelope, TraceContext: envelope.TraceContext}
	rowID, err := s.runtimeSideEffects.CommitStreamingRuntimeActivityAcceptedBeforeResponse(ctx, intent)
	if err != nil {
		return 0, err
	}
	return rowID, nil
}

func (s *Service) finalizeStreamingRuntimeActivityBeforeCompletion(acceptedRowID int64, plan requestPlan, result executionResult, request *http.Request, startedAt time.Time, responseCapture runtimeResponseCapture) error {
	if s == nil || s.runtimeSideEffects == nil {
		return fmt.Errorf("runtime telemetry outbox unavailable")
	}
	ctx := runtimeDetachedContext(runtimeRequestContext(request))
	envelope := s.buildRuntimeActivityEnvelope(ctx, plan, result, request, startedAt, responseCapture)
	if err := s.validateRuntimeActivityHandoff(envelope); err != nil {
		return err
	}
	intent := RuntimeActivityIntent{Envelope: envelope, TraceContext: envelope.TraceContext}
	if err := s.runtimeSideEffects.FinalizeStreamingRuntimeActivityBeforeCompletion(ctx, acceptedRowID, intent); err != nil {
		slog.Error("runtime streaming terminal telemetry handoff failed", "error", err, "profile_id", envelope.UsageEvent.ProfileID, "ingress_request_id", envelope.UsageEvent.IngressRequestID)
		return err
	}
	return nil
}

func (s *Service) validateRuntimeActivityHandoff(envelope runtimeTelemetryEnvelope) error {
	intent := RuntimeActivityIntent{Envelope: envelope, TraceContext: envelope.TraceContext}
	if err := intent.validate(); err != nil {
		return err
	}
	return nil
}

func (s *Service) buildRuntimeActivityEnvelope(ctx context.Context, plan requestPlan, result executionResult, request *http.Request, startedAt time.Time, responseCapture runtimeResponseCapture) runtimeTelemetryEnvelope {
	envelope := s.buildRuntimeTelemetryEnvelope(plan, result, request, startedAt, responseCapture)
	envelope.TraceContext = runtimeTraceContextFromContext(ctx)
	return envelope
}
