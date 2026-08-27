package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRuntimeLaunchedFailureTelemetrySeparatesOrdinaryTransportFromBudget(t *testing.T) {
	completedAt := time.Date(2026, time.August, 27, 12, 0, 0, 0, time.UTC)
	service := &Service{now: func() time.Time { return completedAt }}
	startedAt := completedAt.Add(-time.Second)
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	selectedID := 101
	plan := c1TelemetryPlan("entry-a", selectedID)

	t.Run("ordinary all transport", func(t *testing.T) {
		attempts := []executionAttempt{
			c1TransportAttempt(1, attemptTriggerInitial, "target-b", 101, 11, completedAt.Add(-600*time.Millisecond)),
			c1TransportAttempt(2, attemptTriggerFailover, "target-c", 202, 22, completedAt.Add(-200*time.Millisecond)),
		}
		runtimeErr := &domainError{
			StatusCode:               http.StatusBadGateway,
			ErrorCode:                "transport_error",
			ResolvedTargetModelID:    stringPtr("target-b"),
			SelectedTerminalTargetID: intPtr(selectedID),
			Detail:                   "all upstream transports failed",
		}
		envelope := service.buildRuntimeBudgetExhaustionTelemetryEnvelope(plan, executionResult{
			AttemptCount: 2,
			Attempts:     attempts,
		}, request, startedAt, runtimeErr)

		if len(envelope.RequestLogs) != 2 {
			t.Fatalf("expected two attempt rows, got %d", len(envelope.RequestLogs))
		}
		assertC1AttemptIdentity(t, envelope.RequestLogs[0], "target-b", 101, 11, selectedID)
		assertC1AttemptIdentity(t, envelope.RequestLogs[1], "target-c", 202, 22, selectedID)
		for index, row := range envelope.RequestLogs {
			if row.UpstreamStatusCode != nil {
				t.Fatalf("attempt %d must not invent an upstream status, got %+v", index+1, row.UpstreamStatusCode)
			}
			if row.AttemptResult == nil || *row.AttemptResult != attemptResultTransportError {
				t.Fatalf("attempt %d must remain transport_error, got %+v", index+1, row.AttemptResult)
			}
			if row.ErrorSource == nil || *row.ErrorSource != errorSourceTransport {
				t.Fatalf("attempt %d must retain transport source, got %+v", index+1, row.ErrorSource)
			}
			if row.IsWinner == nil || *row.IsWinner {
				t.Fatalf("attempt %d must not be a winner, got %+v", index+1, row.IsWinner)
			}
		}
		assertC1NoWinnerUsage(t, envelope.UsageEvent, selectedID, 2, "transport_error")
		assertC1NoWinnerAccounting(t, envelope)
		if !envelope.UsageEvent.FailoverOccurred || envelope.UsageEvent.RoutingEvidenceComplete == nil || !*envelope.UsageEvent.RoutingEvidenceComplete {
			t.Fatalf("expected complete failover evidence, got %+v", envelope.UsageEvent)
		}
	})

	t.Run("attempt budget", func(t *testing.T) {
		attempts := make([]executionAttempt, 0, MaxLaunchedUpstreamAttempts)
		for index := 0; index < MaxLaunchedUpstreamAttempts; index++ {
			trigger := attemptTriggerFailover
			if index == 0 {
				trigger = attemptTriggerInitial
			}
			attempts = append(attempts, c1HTTPFailureAttempt(
				index+1,
				trigger,
				fmt.Sprintf("target-%02d", index+1),
				selectedID+index,
				1_000+index,
				http.StatusTooManyRequests,
				completedAt.Add(-time.Duration(MaxLaunchedUpstreamAttempts-index)*time.Millisecond),
			))
		}
		runtimeErr := &domainError{
			StatusCode:               http.StatusServiceUnavailable,
			ErrorCode:                runtimeAttemptBudgetExhaustedErrorCode,
			ResolvedTargetModelID:    stringPtr("target-01"),
			SelectedTerminalTargetID: intPtr(selectedID),
			Detail:                   "launch budget exhausted",
		}
		envelope := service.buildRuntimeBudgetExhaustionTelemetryEnvelope(plan, executionResult{
			AttemptCount: MaxLaunchedUpstreamAttempts,
			Attempts:     attempts,
		}, request, startedAt, runtimeErr)

		if len(envelope.RequestLogs) != MaxLaunchedUpstreamAttempts {
			t.Fatalf("expected %d attempt rows, got %d", MaxLaunchedUpstreamAttempts, len(envelope.RequestLogs))
		}
		assertC1AttemptIdentity(t, envelope.RequestLogs[0], "target-01", selectedID, 1_000, selectedID)
		assertC1AttemptIdentity(t, envelope.RequestLogs[len(envelope.RequestLogs)-1], "target-64", selectedID+63, 1_063, selectedID)
		assertC1NoWinnerUsage(t, envelope.UsageEvent, selectedID, MaxLaunchedUpstreamAttempts, runtimeAttemptBudgetExhaustedErrorCode)
		assertC1NoWinnerAccounting(t, envelope)
	})
}

func TestRuntimeZeroLaunchFailureTelemetryKeepsActualIdentityNull(t *testing.T) {
	completedAt := time.Date(2026, time.August, 27, 13, 0, 0, 0, time.UTC)
	service := &Service{now: func() time.Time { return completedAt }}
	startedAt := completedAt.Add(-250 * time.Millisecond)
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	selectedID := 303

	t.Run("planning", func(t *testing.T) {
		runtimeErr := &domainError{
			StatusCode:               http.StatusBadRequest,
			ErrorCode:                "openai_request_translation_unsupported",
			ResolvedTargetModelID:    stringPtr("planned-target"),
			SelectedTerminalTargetID: intPtr(selectedID),
			Detail:                   "planning rejected the target",
		}
		envelope := service.buildRuntimePlanningFailureTelemetryEnvelope(runtimePlanningFailureTelemetry{
			ProfileID:                1,
			RequestedModelID:         "entry-a",
			APIFamily:                "openai",
			RuntimeOperation:         c1RuntimeOperation(),
			RequestPath:              request.URL.Path,
			SelectedTerminalTargetID: intPtr(selectedID),
			ReportCurrencySnapshot:   runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$"},
		}, request, startedAt, runtimeErr)

		assertC1ZeroLaunchEnvelope(t, envelope, requestLogRowKindPlanning, selectedID, runtimeErr.ErrorCode)
	})

	t.Run("admission", func(t *testing.T) {
		plan := c1TelemetryPlan("entry-a", selectedID)
		runtimeErr := &domainError{
			StatusCode:               http.StatusServiceUnavailable,
			ErrorCode:                runtimeAdmissionExhaustedErrorCode,
			ResolvedTargetModelID:    stringPtr("planned-target"),
			SelectedTerminalTargetID: intPtr(selectedID),
			Detail:                   "admission rejected every target",
		}
		envelope := service.buildRuntimeExecutionFailureTelemetryEnvelope(plan, executionResult{}, request, startedAt, runtimeErr)

		assertC1ZeroLaunchEnvelope(t, envelope, requestLogRowKindAdmission, selectedID, runtimeErr.ErrorCode)
	})
}

func TestRuntimeNormalFailoverAndHedgeTelemetryUseWinnerActualIdentity(t *testing.T) {
	for _, secondTrigger := range []string{attemptTriggerFailover, attemptTriggerHedge} {
		t.Run(secondTrigger, func(t *testing.T) {
			completedAt := time.Date(2026, time.August, 27, 14, 0, 0, 0, time.UTC)
			service := &Service{now: func() time.Time { return completedAt }}
			startedAt := completedAt.Add(-time.Second)
			request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			selectedID := 404
			plan := c1TelemetryPlan("entry-a", selectedID)
			attempts := []executionAttempt{
				c1HTTPFailureAttempt(1, attemptTriggerInitial, "target-b", selectedID, 41, http.StatusServiceUnavailable, completedAt.Add(-700*time.Millisecond)),
				c1SuccessfulAttempt(2, secondTrigger, "target-c", 505, 52, completedAt),
			}
			resolvedTarget := "target-c"
			response := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header)}
			responseCompletedAt := completedAt
			envelope := service.buildRuntimeTelemetryEnvelope(plan, executionResult{
				Response:              response,
				Connection:            attempts[1].Connection,
				ResolvedTargetModelID: &resolvedTarget,
				AttemptCount:          2,
				Attempts:              attempts,
				WinnerOrdinal:         2,
			}, request, startedAt, runtimeResponseCapture{
				CompletedAt:   &responseCompletedAt,
				StreamOutcome: runtimeStreamOutcomeNotStreaming,
			})

			if len(envelope.RequestLogs) != 2 {
				t.Fatalf("expected two attempt rows, got %d", len(envelope.RequestLogs))
			}
			assertC1AttemptIdentity(t, envelope.RequestLogs[0], "target-b", selectedID, 41, selectedID)
			assertC1AttemptIdentity(t, envelope.RequestLogs[1], "target-c", 505, 52, selectedID)
			if envelope.RequestLogs[0].IsWinner == nil || *envelope.RequestLogs[0].IsWinner {
				t.Fatalf("first attempt must be loser, got %+v", envelope.RequestLogs[0].IsWinner)
			}
			if envelope.RequestLogs[1].IsWinner == nil || !*envelope.RequestLogs[1].IsWinner {
				t.Fatalf("second attempt must be winner, got %+v", envelope.RequestLogs[1].IsWinner)
			}

			usage := envelope.UsageEvent
			if usage.ModelID != "entry-a" || usage.ResolvedTargetModelID == nil || *usage.ResolvedTargetModelID != "target-c" {
				t.Fatalf("expected entry-a -> winner target-c, got requested=%q resolved=%+v", usage.ModelID, usage.ResolvedTargetModelID)
			}
			if usage.ConnectionID == nil || *usage.ConnectionID != 505 || usage.EndpointID == nil || *usage.EndpointID != 52 {
				t.Fatalf("expected winner actual identity 505/52, got connection=%+v endpoint=%+v", usage.ConnectionID, usage.EndpointID)
			}
			if usage.SelectedTerminalTargetID == nil || *usage.SelectedTerminalTargetID != selectedID {
				t.Fatalf("expected planning-primary %d, got %+v", selectedID, usage.SelectedTerminalTargetID)
			}
			if usage.FinalAttemptNumber == nil || *usage.FinalAttemptNumber != 2 || usage.FinalAttemptTrigger == nil || *usage.FinalAttemptTrigger != secondTrigger {
				t.Fatalf("expected winner ordinal/trigger 2/%s, got %+v/%+v", secondTrigger, usage.FinalAttemptNumber, usage.FinalAttemptTrigger)
			}
			if secondTrigger == attemptTriggerFailover && !usage.FailoverOccurred {
				t.Fatal("expected failover evidence")
			}
			if secondTrigger == attemptTriggerHedge && !usage.HedgeOccurred {
				t.Fatal("expected hedge evidence")
			}
		})
	}
}

func c1TelemetryPlan(modelID string, selectedID int) requestPlan {
	return requestPlan{
		ReferenceNow:             time.Date(2026, time.August, 27, 11, 0, 0, 0, time.UTC),
		RequestedModelID:         modelID,
		ResolvedTargetModelID:    stringPtr("target-b"),
		ProfileID:                1,
		APIFamily:                "openai",
		RuntimeOperation:         c1RuntimeOperation(),
		SelectedTerminalTargetID: intPtr(selectedID),
		ReportCurrencySnapshot:   runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$"},
	}
}

func c1RuntimeOperation() RuntimeOperation {
	return RuntimeOperation{
		Name:               "openai.chat_completions",
		Method:             http.MethodPost,
		APIFamily:          "openai",
		PathTemplate:       "/v1/chat/completions",
		ModelBindingSource: RuntimeOperationModelBindingBody,
		HookCollectionID:   "openai.chat_completions",
	}
}

func c1Connection(connectionID, endpointID int) runtimeConnection {
	endpointName := fmt.Sprintf("Endpoint %d", endpointID)
	return runtimeConnection{
		ID:        connectionID,
		ProfileID: 1,
		APIFamily: "openai",
		Endpoint: runtimeEndpoint{
			ID:      endpointID,
			Name:    &endpointName,
			BaseURL: fmt.Sprintf("https://endpoint-%d.invalid", endpointID),
		},
	}
}

func c1TransportAttempt(ordinal int, trigger, target string, connectionID, endpointID int, completedAt time.Time) executionAttempt {
	return executionAttempt{
		Connection:              c1Connection(connectionID, endpointID),
		ResolvedTargetModelID:   target,
		StatusCode:              http.StatusBadGateway,
		ResponseTimeMS:          17,
		CompletedAt:             completedAt,
		LaunchOrdinal:           ordinal,
		AttemptTrigger:          trigger,
		AttemptResult:           attemptResultTransportError,
		AttemptDurationMS:       17,
		UpstreamRequestStarted:  true,
		ResponseHeadersReceived: false,
		Diagnostics: &attemptFailureDiagnostics{
			Source: errorSourceTransport,
			Stage:  failureStageUpstreamConnect,
			Code:   "transport_error",
			Detail: "upstream transport failed",
		},
	}
}

func c1HTTPFailureAttempt(ordinal int, trigger, target string, connectionID, endpointID, statusCode int, completedAt time.Time) executionAttempt {
	return executionAttempt{
		Connection:              c1Connection(connectionID, endpointID),
		ResolvedTargetModelID:   target,
		ResponseHeaders:         make(http.Header),
		StatusCode:              statusCode,
		ResponseTimeMS:          23,
		CompletedAt:             completedAt,
		LaunchOrdinal:           ordinal,
		AttemptTrigger:          trigger,
		AttemptResult:           attemptResultHTTPError,
		AttemptDurationMS:       23,
		UpstreamRequestStarted:  true,
		ResponseHeadersReceived: true,
	}
}

func c1SuccessfulAttempt(ordinal int, trigger, target string, connectionID, endpointID int, completedAt time.Time) executionAttempt {
	return executionAttempt{
		Connection:              c1Connection(connectionID, endpointID),
		ResolvedTargetModelID:   target,
		ResponseHeaders:         make(http.Header),
		StatusCode:              http.StatusOK,
		ResponseTimeMS:          29,
		CompletedAt:             completedAt,
		LaunchOrdinal:           ordinal,
		AttemptTrigger:          trigger,
		AttemptDurationMS:       29,
		UpstreamRequestStarted:  true,
		ResponseHeadersReceived: true,
	}
}

func assertC1AttemptIdentity(t *testing.T, row requestLogInsert, target string, connectionID, endpointID, selectedID int) {
	t.Helper()
	if row.ResolvedTargetModelID == nil || *row.ResolvedTargetModelID != target {
		t.Fatalf("expected attempt target %q, got %+v", target, row.ResolvedTargetModelID)
	}
	if row.ConnectionID == nil || *row.ConnectionID != connectionID || row.EndpointID == nil || *row.EndpointID != endpointID {
		t.Fatalf("expected attempt actual identity %d/%d, got connection=%+v endpoint=%+v", connectionID, endpointID, row.ConnectionID, row.EndpointID)
	}
	if row.SelectedTerminalTargetID == nil || *row.SelectedTerminalTargetID != selectedID {
		t.Fatalf("expected planning-primary %d, got %+v", selectedID, row.SelectedTerminalTargetID)
	}
}

func assertC1NoWinnerUsage(t *testing.T, usage usageEventInsert, selectedID, attemptCount int, finalErrorCode string) {
	t.Helper()
	if usage.ResolvedTargetModelID != nil || usage.ConnectionID != nil || usage.EndpointID != nil {
		t.Fatalf("no-winner usage must keep actual identity null, got resolved=%+v connection=%+v endpoint=%+v", usage.ResolvedTargetModelID, usage.ConnectionID, usage.EndpointID)
	}
	if usage.SelectedTerminalTargetID == nil || *usage.SelectedTerminalTargetID != selectedID {
		t.Fatalf("expected planning-primary %d, got %+v", selectedID, usage.SelectedTerminalTargetID)
	}
	if usage.AttemptCount != attemptCount {
		t.Fatalf("expected attempt_count=%d, got %d", attemptCount, usage.AttemptCount)
	}
	if usage.ExpectedRequestLogRowCount == nil || *usage.ExpectedRequestLogRowCount != attemptCount {
		t.Fatalf("expected request-log row count=%d, got %+v", attemptCount, usage.ExpectedRequestLogRowCount)
	}
	if usage.FinalErrorCode == nil || *usage.FinalErrorCode != finalErrorCode {
		t.Fatalf("expected final error %q, got %+v", finalErrorCode, usage.FinalErrorCode)
	}
	if usage.FinalAttemptNumber != nil || usage.FinalAttemptTrigger != nil || usage.FinalTargetEntryTrigger != nil {
		t.Fatalf("no-winner usage must keep final attempt fields null, got number=%+v trigger=%+v entry=%+v", usage.FinalAttemptNumber, usage.FinalAttemptTrigger, usage.FinalTargetEntryTrigger)
	}
}

func assertC1NoWinnerAccounting(t *testing.T, envelope runtimeTelemetryEnvelope) {
	t.Helper()
	for index, event := range envelope.AccountingAttempts {
		if event.Final {
			t.Fatalf("no-winner accounting attempt %d must not claim final", index+1)
		}
	}
}

func assertC1ZeroLaunchEnvelope(t *testing.T, envelope runtimeTelemetryEnvelope, rowKind string, selectedID int, finalErrorCode string) {
	t.Helper()
	if len(envelope.RequestLogs) != 1 {
		t.Fatalf("expected one diagnostic row, got %d", len(envelope.RequestLogs))
	}
	if len(envelope.AccountingAttempts) != 0 {
		t.Fatalf("zero-launch envelope must not create attempt accounting events, got %d", len(envelope.AccountingAttempts))
	}
	row := envelope.RequestLogs[0]
	if row.RowKind != rowKind || row.ResolvedTargetModelID != nil || row.ConnectionID != nil || row.EndpointID != nil {
		t.Fatalf("expected identity-free %s diagnostic, got %+v", rowKind, row)
	}
	usage := envelope.UsageEvent
	if usage.AttemptCount != 0 || usage.ResolvedTargetModelID != nil || usage.ConnectionID != nil || usage.EndpointID != nil {
		t.Fatalf("zero-launch usage must keep count 0 and actual identity null, got %+v", usage)
	}
	if usage.SelectedTerminalTargetID == nil || *usage.SelectedTerminalTargetID != selectedID {
		t.Fatalf("expected planning-primary %d, got %+v", selectedID, usage.SelectedTerminalTargetID)
	}
	if usage.ExpectedRequestLogRowCount == nil || *usage.ExpectedRequestLogRowCount != 1 {
		t.Fatalf("expected one diagnostic request-log row, got %+v", usage.ExpectedRequestLogRowCount)
	}
	if usage.FinalErrorCode == nil || *usage.FinalErrorCode != finalErrorCode {
		t.Fatalf("expected final error %q, got %+v", finalErrorCode, usage.FinalErrorCode)
	}
	if usage.FinalAttemptNumber != nil || usage.FinalAttemptTrigger != nil || usage.FinalTargetEntryTrigger != nil {
		t.Fatalf("zero-launch usage must keep final attempt fields null, got %+v", usage)
	}
}
