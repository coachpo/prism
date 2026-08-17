package runtime

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/coachpo/prism/backend/internal/httpapi/requestcontext"
)

func (s *Service) recordRuntimePlanningFailure(request *http.Request, startedAt time.Time, err error) {
	if s == nil || s.runtimeSideEffects == nil {
		return
	}
	var runtimeErr *domainError
	if !errors.As(err, &runtimeErr) || runtimeErr == nil || runtimeErr.PlanningFailure == nil {
		return
	}
	ctx := runtimeRequestContext(request)
	envelope := s.buildRuntimePlanningFailureTelemetryEnvelope(*runtimeErr.PlanningFailure, request, startedAt, runtimeErr)
	envelope.TraceContext = runtimeTraceContextFromContext(ctx)
	intent := RuntimeActivityIntent{Envelope: envelope, TraceContext: envelope.TraceContext}
	if submit := s.runtimeSideEffects.SubmitRuntimeActivityContext(ctx, intent); submit.Status != RuntimeSideEffectAccepted {
		slog.Error("failed to accept runtime planning-failure telemetry intent", "reason", submit.Reason, "profile_id", envelope.UsageEvent.ProfileID, "ingress_request_id", envelope.UsageEvent.IngressRequestID)
	}
}

func (s *Service) recordRuntimeExecutionFailure(plan requestPlan, result executionResult, request *http.Request, startedAt time.Time, err error) {
	if s == nil || s.runtimeSideEffects == nil {
		return
	}
	var runtimeErr *domainError
	if !errors.As(err, &runtimeErr) || runtimeErr == nil {
		return
	}
	// Every terminal gateway error is recorded. An allowlist here used to drop
	// transport_error and request_body_too_large entirely, so the single most
	// common self-hosted failure — an upstream that cannot be reached — produced
	// a 502 for the caller and no row at all in Requests, Dashboard or
	// Analytics, silently inflating the success rate.
	ctx := runtimeRequestContext(request)
	var envelope runtimeTelemetryEnvelope
	if result.AttemptCount > 0 && len(result.Attempts) > 0 {
		// Whenever upstream attempts were actually launched, preserve one row
		// per attempt (trigger, target identity, duration, safe transport
		// detail) plus a finalized usage summary carrying the gateway terminal
		// code (Requests SPEC §4.6). This covers both the 64-launch safety
		// bound and "all launched attempts failed" (transport_error).
		// admission_exhausted never reaches this branch: runtime.go only raises
		// it while launchedAttempts == 0.
		envelope = s.buildRuntimeBudgetExhaustionTelemetryEnvelope(plan, result, request, startedAt, runtimeErr)
	} else {
		envelope = s.buildRuntimeExecutionFailureTelemetryEnvelope(plan, result, request, startedAt, runtimeErr)
	}
	envelope.TraceContext = runtimeTraceContextFromContext(ctx)
	intent := RuntimeActivityIntent{Envelope: envelope, TraceContext: envelope.TraceContext}
	if submit := s.runtimeSideEffects.SubmitRuntimeActivityContext(ctx, intent); submit.Status != RuntimeSideEffectAccepted {
		slog.Error("failed to accept runtime execution-failure telemetry intent", "reason", submit.Reason, "profile_id", envelope.UsageEvent.ProfileID, "ingress_request_id", envelope.UsageEvent.IngressRequestID)
	}
}

func runtimeRequestContext(request *http.Request) context.Context {
	if request != nil && request.Context() != nil {
		return request.Context()
	}
	return context.Background()
}

func (s *Service) buildRuntimeTelemetryEnvelope(plan requestPlan, result executionResult, request *http.Request, startedAt time.Time, responseCapture runtimeResponseCapture) runtimeTelemetryEnvelope {
	telemetry := s.buildRuntimeTelemetryEnvelopeContext(plan, result, request, startedAt, responseCapture)
	requestLogs := buildRuntimeRequestLogRows(plan, request, telemetry)
	auditLogs := buildRuntimeAuditLogRows(plan, request, telemetry)
	usageEvent := buildRuntimeUsageEvent(plan, result, request, telemetry, len(requestLogs))
	return runtimeTelemetryEnvelope{
		RequestLogs:          requestLogs,
		AuditLogs:            auditLogs,
		UsageEvent:           usageEvent,
		AccountingEvent:      buildRuntimeAccountingFinalEvent(usageEvent, requestLogs, telemetry.routeReason, telemetry.usageSource),
		AccountingAttempts:   buildRuntimeAccountingAttemptEvents(requestLogs, telemetry.routeReason, telemetry.usageSource),
		ProxyKeyUsage:        runtimeProxyKeyUsageSignalFromSnapshot(telemetry.proxyKey),
		ProxyKeyAuthEnforced: runtimeProxyKeyAuthEnforcedFromContext(request.Context()),
	}
}

func headerValuePointer(header http.Header, keys ...string) *string {
	for _, key := range keys {
		value := strings.TrimSpace(header.Get(key))
		if value != "" {
			return &value
		}
	}
	return nil
}

func headerMapValuePointer(header map[string]string, key string) *string {
	for headerKey, value := range header {
		if strings.EqualFold(headerKey, key) {
			return trimmedStringPointer(value)
		}
	}
	return nil
}

func proxyKeyIDPointer(proxyKey *requestcontext.RuntimeProxyKeySnapshot) *int {
	if proxyKey == nil {
		return nil
	}
	return &proxyKey.ID
}

func proxyKeyNamePointer(proxyKey *requestcontext.RuntimeProxyKeySnapshot) *string {
	if proxyKey == nil {
		return nil
	}
	return &proxyKey.Name
}
