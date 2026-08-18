package runtime

import (
	"net/http"

	"github.com/coachpo/prism/backend/internal/domain/safediag"
)

func buildRuntimeAuditLogRows(plan requestPlan, request *http.Request, telemetry runtimeTelemetryEnvelopeContext) []auditLogInsert {
	auditLogs := make([]auditLogInsert, 0, len(telemetry.attempts))
	requestBudgetRemaining := ingressAuditRequestBudgetBytes
	for index := range telemetry.attempts {
		attempt := telemetry.attemptContext(index)
		if !attempt.attempt.AuditEnabledAtRequest {
			continue
		}
		auditLogs = append(auditLogs, buildRuntimeAuditLogRow(plan, request, telemetry, attempt, &requestBudgetRemaining))
	}
	return auditLogs
}

func buildRuntimeAuditLogRow(plan requestPlan, request *http.Request, telemetry runtimeTelemetryEnvelopeContext, attempt runtimeTelemetryAttemptContext, requestBudgetRemaining *int64) auditLogInsert {
	requestBody := runtimeCapturedAuditRequestBodyForOperation(plan.RuntimeOperation, attempt.attempt.AuditCaptureBodiesAtRequest, attempt.attempt.RequestBody)
	scrubbedRequestURL, requestURLTruncated := safediag.ScrubRequestURL(runtimeAuditRequestURL(attempt.attempt.RequestURL, request))
	scrubbedBaseURL, baseURLTruncated := safediag.ScrubEndpointBaseURL(attempt.attempt.Connection.Endpoint.BaseURL)
	auditHeaderRules := planBlocklistSensitiveRules(plan)
	requestHeaders := marshalAuditHeaders(attempt.attempt.RequestHeaders, auditHeaderRules)
	responseHeaders := marshalAuditHTTPHeaders(attempt.attempt.ResponseHeaders, auditHeaderRules)
	requestBodyBytes := []byte(nil)
	requestBodyStoredBytes := int64(0)
	requestBodyObserved := int64(0)
	requestBodyTruncated := false
	requestBodyStatus := "not_requested"
	requestBodyLimitReason := "none"
	requestBodyEndState := ""
	requestBodyEncoding := ""
	if requestBody != nil {
		requestBodyBytes = []byte(*requestBody)
		requestBodyObserved = int64(len(requestBodyBytes))
		// Per-body 4 MiB cap then shared 12 MiB ingress budget in immutable
		// launch order (Requests SPEC §5.2 allocation formula).
		perBodyCap := int64(auditBodyCapBytes)
		stored := requestBodyObserved
		if stored > perBodyCap {
			stored = perBodyCap
		}
		if requestBudgetRemaining != nil && stored > *requestBudgetRemaining {
			stored = *requestBudgetRemaining
		}
		if requestBudgetRemaining != nil {
			*requestBudgetRemaining -= stored
		}
		if stored > 0 {
			requestBodyBytes = requestBodyBytes[:stored]
			requestBodyStoredBytes = stored
			requestBodyStatus = "captured"
			requestBodyEndState = "complete"
			requestBodyEncoding = "utf8"
			if stored < requestBodyObserved {
				requestBodyStatus = "truncated"
				requestBodyTruncated = true
				switch {
				case requestBodyObserved > perBodyCap && stored >= perBodyCap:
					requestBodyLimitReason = "body_cap"
				case requestBodyObserved > perBodyCap && stored < perBodyCap:
					requestBodyLimitReason = "both"
				default:
					requestBodyLimitReason = "ingress_budget"
				}
			}
		} else if requestBodyObserved > 0 {
			requestBodyBytes = nil
			requestBodyStatus = "omitted_ingress_budget"
			requestBodyLimitReason = "ingress_budget"
		}
	}
	responseBodyBytes := []byte(nil)
	responseBodyObserved := int64(0)
	responseBodyStoredBytes := int64(0)
	responseBodyTruncated := false
	responseBodyStatus := "not_requested"
	responseBodyLimitReason := "none"
	responseBodyEndState := ""
	responseBodyEncoding := ""
	if attempt.isFinal && attempt.attempt.AuditCaptureBodiesAtRequest {
		if telemetry.capturedResponseBody != nil {
			responseBodyBytes = []byte(*telemetry.capturedResponseBody)
		}
		if telemetry.capturedResponseObserved > 0 || len(responseBodyBytes) > 0 {
			responseBodyObserved = telemetry.capturedResponseObserved
			if responseBodyObserved == 0 {
				responseBodyObserved = int64(len(responseBodyBytes))
			}
			responseBodyStoredBytes = int64(len(responseBodyBytes))
			responseBodyStatus = "captured"
			responseBodyEndState = "complete"
			responseBodyEncoding = "utf8"
			if telemetry.capturedResponseTruncated || (responseBodyStoredBytes > 0 && responseBodyStoredBytes < responseBodyObserved) {
				responseBodyStatus = "truncated"
				responseBodyTruncated = true
				responseBodyLimitReason = "body_cap"
			}
		}
	}
	// The stored request body is the capped/budgeted prefix computed above, not
	// the captured original: request_body_bytes_stored, the truncated flag and
	// the omitted_ingress_budget status all describe those bytes, so writing the
	// full body would contradict its own metadata.
	storedRequestBody := auditBodyString(requestBodyBytes)
	auditLog := auditLogInsert{
		RequestLogAttemptNumber:           attempt.attemptNumber,
		ProfileID:                         plan.ProfileID,
		ModelID:                           plan.RequestedModelID,
		EndpointID:                        attempt.attempt.Connection.Endpoint.ID,
		ConnectionID:                      attempt.attempt.Connection.ID,
		EndpointBaseURL:                   scrubbedBaseURL,
		EndpointBaseURLTruncated:          baseURLTruncated,
		EndpointDescription:               attempt.attempt.Connection.Endpoint.Name,
		RequestMethod:                     request.Method,
		RequestURL:                        scrubbedRequestURL,
		RequestURLTruncated:               requestURLTruncated,
		RequestHeaders:                    requestHeaders,
		RequestBody:                       storedRequestBody,
		RequestBodyStored:                 requestBodyStoredBytes > 0,
		ResponseStatus:                    attempt.attempt.StatusCode,
		ResponseHeaders:                   responseHeaders,
		ResponseBody:                      auditBodyString(responseBodyBytes),
		ResponseBodyStored:                responseBodyStoredBytes > 0,
		IsStream:                          telemetry.isStream,
		DurationMS:                        attempt.responseTimeMS,
		CreatedAt:                         attempt.createdAt,
		AuditEnabledAtRequest:             attempt.attempt.AuditEnabledAtRequest,
		AuditCaptureBodiesAtRequest:       attempt.attempt.AuditCaptureBodiesAtRequest,
		RowKind:                           requestLogRowKindUpstream,
		AttemptNumber:                     intPtr(attempt.attemptNumber),
		AttemptDurationMS:                 nonNegativeIntPointer(attempt.attempt.AttemptDurationMS),
		UpstreamStatusCode:                upstreamStatusPointer(attempt.attempt),
		URLScrubProvenance:                runtimeURLScrubProvenanceRuntime,
		RequestHeadersScrubProvenance:     runtimeURLScrubProvenanceRuntime,
		ResponseHeadersScrubProvenance:    runtimeURLScrubProvenanceRuntime,
		RequestHeadersCaptureStatus:       runtimeAuditHeadersCaptureStatus(requestHeaders),
		ResponseHeadersCaptureStatus:      runtimeAuditHeadersCaptureStatusOptional(responseHeaders),
		RequestHeadersCaptureLimitReason:  "none",
		ResponseHeadersCaptureLimitReason: "none",
		RequestBodyCaptureProvenance:      "runtime_bytes",
		ResponseBodyCaptureProvenance:     "runtime_bytes",
		RequestBodyCaptureStatus:          requestBodyStatus,
		ResponseBodyCaptureStatus:         responseBodyStatus,
		RequestBodyCaptureLimitReason:     requestBodyLimitReason,
		ResponseBodyCaptureLimitReason:    responseBodyLimitReason,
		RequestBodyCaptureEndState:        optionalTrimmedStringPointer(requestBodyEndState),
		ResponseBodyCaptureEndState:       optionalTrimmedStringPointer(responseBodyEndState),
		RequestBodyEncoding:               optionalTrimmedStringPointer(requestBodyEncoding),
		ResponseBodyEncoding:              optionalTrimmedStringPointer(responseBodyEncoding),
		RequestBodyBytesObserved:          int64Ptr(requestBodyObserved),
		RequestBodyBytesStored:            int64Ptr(requestBodyStoredBytes),
		ResponseBodyBytesObserved:         int64Ptr(responseBodyObserved),
		ResponseBodyBytesStored:           int64Ptr(responseBodyStoredBytes),
		RequestBodyTruncated:              requestBodyTruncated,
		ResponseBodyTruncated:             responseBodyTruncated,
	}
	if attempt.isFinal && attempt.attempt.AuditCaptureBodiesAtRequest && responseBodyBytes != nil {
		auditLog.ResponseBody = auditBodyString(responseBodyBytes)
		auditLog.ResponseBodyStored = true
	}
	return auditLog
}

func auditBodyString(bytes []byte) *string {
	if bytes == nil {
		return nil
	}
	resolved := string(bytes)
	return &resolved
}

// upstreamStatusPointer returns the real upstream HTTP status when response
// headers were received; transport/no-response attempts stay null (Requests
// SPEC §5.2: never copy gateway 502 into an attempt HTTP status).
func upstreamStatusPointer(attempt executionAttempt) *int {
	if !attempt.ResponseHeadersReceived {
		return nil
	}
	return intPtr(attempt.StatusCode)
}
