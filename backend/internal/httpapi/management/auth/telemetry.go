package auth

import (
	"context"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const (
	authTelemetryBranchManagement = "management"
	authTelemetryBranchRuntime    = "runtime"
	authTelemetryBranchOther      = "other"
)

var (
	authTelemetryMeter = otel.Meter("github.com/coachpo/prism/backend/internal/httpapi/management/auth")

	authTelemetryInitOnce sync.Once
	authTelemetryInitErr  error
	authDecisionCounter   otelmetric.Int64Counter
)

func initAuthTelemetryInstruments() error {
	authTelemetryInitOnce.Do(func() {
		var err error
		authDecisionCounter, err = authTelemetryMeter.Int64Counter(
			"prism.auth.decisions",
			otelmetric.WithDescription("Management and runtime auth decisions by bounded outcome."),
			otelmetric.WithUnit("1"),
		)
		if err != nil {
			authTelemetryInitErr = err
		}
	})
	return authTelemetryInitErr
}

func recordAuthDecision(ctx context.Context, branch string, outcome string) {
	attrs := []attribute.KeyValue{
		attribute.String("branch", normalizedAuthTelemetryBranch(branch)),
		attribute.String("outcome", normalizedAuthTelemetryOutcome(outcome)),
	}
	trace.SpanFromContext(ctx).AddEvent("auth.decision", trace.WithAttributes(attrs...))
	if initAuthTelemetryInstruments() != nil {
		return
	}
	authDecisionCounter.Add(ctx, 1, otelmetric.WithAttributes(attrs...))
}

func normalizedAuthTelemetryBranch(branch string) string {
	switch branch {
	case authTelemetryBranchManagement, authTelemetryBranchRuntime:
		return branch
	default:
		return authTelemetryBranchOther
	}
}

func normalizedAuthTelemetryOutcome(outcome string) string {
	switch outcome {
	case "authenticated", "disabled", "public", "settings_error":
		return outcome
	case "unauthenticated", "snapshot_unavailable", "missing_proxy_key":
		return outcome
	case "verify_error", "invalid_proxy_key":
		return outcome
	default:
		return "other"
	}
}
