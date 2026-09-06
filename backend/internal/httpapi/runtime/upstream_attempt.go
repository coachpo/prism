package runtime

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/coachpo/prism/backend/internal/domain/loadbalance"
	gatewayrouting "github.com/coachpo/prism/backend/internal/gateway/routing"
)

type executionOutcome struct {
	TerminalAttempt           runtimeTerminalAttempt
	Connection                runtimeConnection
	RequestHeaders            map[string]string
	Response                  *http.Response
	Attempt                   executionAttempt
	Launched                  bool
	Skipped                   bool
	Err                       error
	AdmissionReason           string
	AdmissionState            *loadbalance.RuntimeConnectionState
	UnbannedRecord            *loadbalance.RuntimeConnectionState
	RetryDecision             gatewayrouting.RetryDecision
	FailoverEligible          bool
	Definitive                bool
	SuppressTransportFeedback bool
	FatalError                error
}

// runtimeAttemptLeaseBody keeps a connection's in-flight lease until the
// upstream response body reaches a terminal read or is explicitly closed.
// http.Client.Do returns after response headers, which is too early to release
// a concurrency slot for either streaming or non-streaming responses.
type runtimeAttemptLeaseBody struct {
	io.ReadCloser
	release     func()
	releaseOnce sync.Once
}

func (body *runtimeAttemptLeaseBody) Read(payload []byte) (int, error) {
	written, err := body.ReadCloser.Read(payload)
	if err != nil {
		body.releaseLease()
	}
	return written, err
}

func (body *runtimeAttemptLeaseBody) Close() error {
	err := body.ReadCloser.Close()
	body.releaseLease()
	return err
}

func (body *runtimeAttemptLeaseBody) releaseLease() {
	body.releaseOnce.Do(body.release)
}

func (s *Service) executeSingleAttempt(ctx context.Context, method string, plan requestPlan, requestQuery string, terminalAttempt runtimeTerminalAttempt, bodySource *runtimeRequestBodySource, lifecycle runtimeAttemptLifecycle) executionOutcome {
	connection := terminalAttempt.Connection
	stripBodyDependentHeaders := connection.CustomRequestParameters != nil && !connection.CustomRequestParameters.IsEmpty()
	headers, err := s.buildUpstreamHeaders(connection, plan.APIFamily, plan.ClientHeaders, plan.BlocklistRules, stripBodyDependentHeaders)
	if err != nil {
		return executionOutcome{TerminalAttempt: terminalAttempt, Connection: connection, FatalError: err}
	}
	upstreamURL, err := buildUpstreamURL(connection.Endpoint.BaseURL, terminalAttempt.EffectiveRequestPath, requestQuery)
	if err != nil {
		return executionOutcome{TerminalAttempt: terminalAttempt, Connection: connection, FatalError: err}
	}

	decision := s.runtimeState.TryBeginConnectionAttempt(loadbalance.RuntimeConnectionAttemptInput{
		ProfileID:     plan.ProfileID,
		ModelConfigID: connection.ModelConfigID,
		ConnectionID:  connection.ID,
		Admission: loadbalance.RuntimeConnectionAdmission{
			QPSLimit:             connection.QPSLimit,
			MaxInFlightNonStream: connection.MaxInFlightNonStream,
			MaxInFlightStream:    connection.MaxInFlightStream,
		},
		Policy:      terminalAttempt.Strategy.AdmissionPolicy(),
		IsStreaming: plan.IsStreamingRequest,
		ObservedAt:  s.nowUTC(),
	})
	if decision.Skipped {
		return executionOutcome{TerminalAttempt: terminalAttempt, Connection: connection, Skipped: true, UnbannedRecord: decision.UnbannedRecord}
	}
	if decision.AdmissionReason != "" {
		return executionOutcome{TerminalAttempt: terminalAttempt, Connection: connection, AdmissionReason: decision.AdmissionReason, AdmissionState: decision.AdmissionState, UnbannedRecord: decision.UnbannedRecord}
	}
	var releaseOnce sync.Once
	releaseAttempt := func() {
		releaseOnce.Do(func() {
			s.runtimeState.FinishConnectionAttempt(decision.Handle, s.nowUTC())
		})
	}

	attemptStartedAt := s.nowUTC()
	attemptBodySource := bodySourceForTerminalAttempt(bodySource, terminalAttempt)
	response, headersLatencyMS, launched, requestErr := s.doUpstreamRequest(ctx, plan.HTTPClient, method, upstreamURL, headers, attemptBodySource)
	if response != nil && response.Body != nil {
		response.Body = &runtimeAttemptLeaseBody{ReadCloser: response.Body, release: releaseAttempt}
	} else {
		releaseAttempt()
	}
	if requestErr != nil && response != nil && response.Body != nil {
		// An errored transport response is never passed through or sampled.
		// Close it here so its lease and transport resources are released.
		_ = response.Body.Close()
	}
	auditHeaders := auditableAttemptHeaders(plan.ClientHeaders, headers)
	outcome := executionOutcome{TerminalAttempt: terminalAttempt, Connection: connection, RequestHeaders: auditHeaders, Response: response, Launched: launched, Err: requestErr, UnbannedRecord: decision.UnbannedRecord}
	if launched {
		attemptCompletedAt := s.nowUTC()
		outcome.Attempt = executionAttempt{
			Connection:                  connection,
			ResolvedTargetModelID:       strings.TrimSpace(terminalAttempt.TargetModel.ModelID),
			RequestURL:                  upstreamURL,
			RequestHeaders:              cloneStringMap(auditHeaders),
			RequestBody:                 append([]byte(nil), terminalAttempt.UpstreamBody...),
			StatusCode:                  http.StatusBadGateway,
			ResponseTimeMS:              durationMilliseconds(attemptCompletedAt.Sub(attemptStartedAt)),
			ResponseHeadersLatencyMS:    headersLatencyMS,
			CompletedAt:                 attemptCompletedAt,
			AuditEnabledAtRequest:       terminalAttempt.AuditEnabledAtRequest,
			AuditCaptureBodiesAtRequest: terminalAttempt.AuditCaptureBodiesRequest,
			UpstreamOperationName:       runtimeUpstreamOperationName(plan.RuntimeOperation, terminalAttempt.TranslationMode),
			UpstreamRequestPath:         dereferenceString(runtimeUpstreamRequestPath(plan.RuntimeOperation, terminalAttempt.TranslationMode, terminalAttempt.EffectiveRequestPath)),
			OperationTranslationMode:    normalizedRuntimeTranslationMode(terminalAttempt.TranslationMode),
			RequestGenerationParams:     terminalAttempt.RequestGenerationParams.clonePointer(),
			LaunchOrdinal:               lifecycle.LaunchOrdinal,
			AttemptTrigger:              lifecycle.AttemptTrigger,
			AttemptDurationMS:           durationMilliseconds(attemptCompletedAt.Sub(attemptStartedAt)),
			UpstreamRequestStarted:      true,
		}
		if response != nil {
			outcome.Attempt.StatusCode = response.StatusCode
			outcome.Attempt.ResponseHeaders = response.Header.Clone()
			outcome.Attempt.ResponseHeadersReceived = true
		}
		if s.isHedgeLoserCancellation(ctx, requestErr) {
			outcome.Attempt.StatusCode = hedgeCanceledAttemptStatusCode
			outcome.Attempt.AttemptResult = attemptResultCancelled
			outcome.SuppressTransportFeedback = true
		}
	}
	if requestErr != nil {
		requestContextErr := ctx.Err()
		outcome.RetryDecision = gatewayrouting.RetryPolicy{FailoverStatusCodes: terminalAttempt.Strategy.FailoverStatusCodes()}.ClassifyTransportError(requestContextErr, requestErr)
		outcome.FailoverEligible = outcome.RetryDecision.Retryable
		outcome.Definitive = !outcome.FailoverEligible
		if requestContextErr != nil {
			outcome.SuppressTransportFeedback = true
			if launched && outcome.Attempt.AttemptResult == "" {
				outcome.Attempt.StatusCode = hedgeCanceledAttemptStatusCode
				outcome.Attempt.AttemptResult = attemptResultCancelled
			}
		}
		if launched && outcome.Attempt.AttemptResult == "" {
			// Bounded, safe transport diagnostic formed at the failure site.
			diagnostic := safeTransportDiagnostic(requestErr)
			outcome.Attempt.Diagnostics = &diagnostic
			outcome.Attempt.AttemptResult = attemptResultTransportError
		}
		return outcome
	}
	outcome.RetryDecision = gatewayrouting.RetryPolicy{FailoverStatusCodes: terminalAttempt.Strategy.FailoverStatusCodes()}.ClassifyHTTPStatus(response.StatusCode)
	outcome.FailoverEligible = outcome.RetryDecision.Retryable
	outcome.Definitive = !outcome.FailoverEligible
	if launched && outcome.FailoverEligible {
		outcome.Attempt.AttemptResult = attemptResultHTTPError
	}
	return outcome
}

func bodySourceForTerminalAttempt(bodySource *runtimeRequestBodySource, terminalAttempt runtimeTerminalAttempt) *runtimeRequestBodySource {
	if bodySource != nil && bodySource.useStreamingBody {
		return bodySource
	}
	return newBufferedRuntimeRequestBodySource(terminalAttempt.UpstreamBody)
}

func (s *Service) doUpstreamRequest(ctx context.Context, client *http.Client, method string, upstreamURL string, headers map[string]string, bodySource *runtimeRequestBodySource) (*http.Response, int, bool, error) {
	if client == nil {
		client = s.httpClient
	}
	if client == nil {
		return nil, 0, false, fmt.Errorf("runtime HTTP client unavailable")
	}
	requestBody, contentLength, err := bodySource.Open()
	if err != nil {
		return nil, 0, false, fmt.Errorf("open upstream request body: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, method, upstreamURL, requestBody)
	if err != nil {
		if requestBody != nil {
			_ = requestBody.Close()
		}
		return nil, 0, false, fmt.Errorf("build upstream request: %w", err)
	}
	request.ContentLength = contentLength
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	// Header.Set has already canonicalized whatever casing the client or the
	// connection's custom headers used, so this reads the effective value
	// rather than guessing at map keys. With nothing set, send an empty
	// User-Agent instead of letting net/http substitute its Go-http-client
	// default, which would name the gateway's implementation outright.
	if request.Header.Get("User-Agent") == "" {
		request.Header["User-Agent"] = []string{""}
	}
	headersReceivedAt := s.nowUTC()
	response, err := client.Do(request)
	headersLatencyMS := durationMilliseconds(s.nowUTC().Sub(headersReceivedAt))
	return response, headersLatencyMS, true, err
}

func buildUpstreamURL(baseURL string, requestPath string, requestQuery string) (string, error) {
	parsedURL, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return "", fmt.Errorf("parse upstream base URL: %w", err)
	}
	basePath := strings.TrimRight(parsedURL.Path, "/")
	finalPath := requestPath
	if !strings.HasPrefix(finalPath, "/") {
		finalPath = "/" + finalPath
	}
	parsedURL.Path = basePath + finalPath
	parsedURL.RawPath = parsedURL.EscapedPath()
	if requestQuery != "" {
		if parsedURL.RawQuery != "" {
			parsedURL.RawQuery = parsedURL.RawQuery + "&" + requestQuery
		} else {
			parsedURL.RawQuery = requestQuery
		}
	}
	return parsedURL.String(), nil
}
