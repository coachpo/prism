package runtime

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"

	"github.com/coachpo/prism/backend/internal/domain/safediag"
	gatewaycore "github.com/coachpo/prism/backend/internal/gateway/core"
)

// streamStartInspectionLimit bounds how much of a 2xx event stream the
// executor buffers while waiting for its first event. Provider error events
// are small, so a first event that does not complete within the limit is
// released unchanged.
const streamStartInspectionLimit = int(FailedResponseSampleBytes)

const streamStartErrorFallbackDetail = "upstream stream started with an error event"

// streamStart is the buffered head of an upstream event stream: every byte
// read so far, the first dispatched event when it completed within the
// limit, and the read error that ended buffering.
type streamStart struct {
	prefix   []byte
	event    string
	data     []byte
	complete bool
	readErr  error
}

// readStreamStart buffers an event stream up to its first dispatched event
// using the SSE capture's line rules: lines end at '\n', a trailing '\r' is
// dropped, and a blank line dispatches an event only when it carried data.
// When the stream ends first, its unterminated last line and pending data
// still form the first event.
func readStreamStart(body io.Reader) streamStart {
	var start streamStart
	var dataLines []string
	dispatches := func(line string) bool {
		line = strings.TrimRight(line, "\r")
		switch {
		case line == "" && len(dataLines) > 0:
			return true
		case line == "":
			start.event = ""
		case strings.HasPrefix(line, "event:"):
			start.event = trimSSEFieldValue(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			dataLines = append(dataLines, trimSSEFieldValue(strings.TrimPrefix(line, "data:")))
		}
		return false
	}
	chunk := make([]byte, 4096)
	scanned := 0
	for len(start.prefix) < streamStartInspectionLimit {
		read, err := body.Read(chunk)
		start.prefix = append(start.prefix, chunk[:read]...)
		for lineEnd := bytes.IndexByte(start.prefix[scanned:], '\n'); lineEnd >= 0; lineEnd = bytes.IndexByte(start.prefix[scanned:], '\n') {
			line := string(start.prefix[scanned : scanned+lineEnd])
			scanned += lineEnd + 1
			if dispatches(line) {
				start.data = []byte(strings.Join(dataLines, "\n"))
				start.complete = true
				return start
			}
		}
		if err != nil {
			start.readErr = err
			dispatches(string(start.prefix[scanned:]))
			start.data = []byte(strings.Join(dataLines, "\n"))
			start.complete = len(dataLines) > 0
			return start
		}
	}
	return start
}

// isError reports whether the first event is a provider error: an SSE
// `error` event, or a JSON payload that declares `"type": "error"` or carries
// a top-level `error` member.
func (start streamStart) isError() bool {
	if !start.complete {
		return false
	}
	if start.event == "error" {
		return true
	}
	var payload map[string]any
	if json.Unmarshal(start.data, &payload) != nil {
		return false
	}
	if payloadType, _ := payload["type"].(string); payloadType == "error" {
		return true
	}
	switch errorValue := payload["error"].(type) {
	case map[string]any:
		return true
	case string:
		return strings.TrimSpace(errorValue) != ""
	default:
		return false
	}
}

// diagnostics projects the error event through the shared provider-error
// extractor. A recognized provider code wins over the provider-incomplete
// fallback code, mirroring HTTP failure codes.
func (start streamStart) diagnostics(extraRules []safediag.SensitiveNameRule) attemptFailureDiagnostics {
	extraction := safediag.ExtractProviderErrorEnvelope(start.data, "application/json", extraRules...)
	detail := streamStartErrorFallbackDetail
	if extraction.Recognized && extraction.Detail != "" {
		detail = extraction.Detail
	}
	diagnostic := safeStreamDiagnostic(errorSourceUpstream, failureStageStream, "", runtimeStreamOutcomeProviderIncomplete, detail)
	if code := safediag.AdoptProviderCode(extraction.Code); code != "" {
		diagnostic.Code = code
	}
	diagnostic.Redacted = diagnostic.Redacted || extraction.Redacted
	diagnostic.Truncated = diagnostic.Truncated || extraction.Truncated
	return diagnostic
}

// replay returns a body that yields the buffered head and then the rest of
// the upstream stream, or the read error that ended buffering. Close still
// closes the upstream body so its lease is released.
func (start streamStart) replay(upstream io.ReadCloser) io.ReadCloser {
	rest := io.Reader(upstream)
	if start.readErr != nil {
		rest = streamStartReadError{err: start.readErr}
	}
	return streamStartReplayBody{Reader: io.MultiReader(bytes.NewReader(start.prefix), rest), upstream: upstream}
}

type streamStartReplayBody struct {
	io.Reader
	upstream io.Closer
}

func (body streamStartReplayBody) Close() error {
	return body.upstream.Close()
}

type streamStartReadError struct {
	err error
}

func (reader streamStartReadError) Read([]byte) (int, error) {
	return 0, reader.err
}

// rerouteStreamStartError inspects a 2xx event stream before the executor
// selects it while another candidate remains. A provider error first event is
// treated as an in-band rejection of this request: the attempt is recorded as
// a failed stream attempt and the caller reroutes to the next candidate
// without runtime feedback, like a reroute status, so the target's retry
// window and ban state stay as they were. Otherwise the buffered head is
// replayed so the selected stream reaches the client unchanged.
func (s *Service) rerouteStreamStartError(plan requestPlan, state *requestExecutionState, outcome *executionOutcome) bool {
	if !responseUsesSSEProxy(plan, outcome.Response) {
		return false
	}
	start := readStreamStart(outcome.Response.Body)
	if !start.isError() {
		outcome.Response.Body = start.replay(outcome.Response.Body)
		return false
	}
	_ = outcome.Response.Body.Close()
	diagnostic := start.diagnostics(planBlocklistSensitiveRules(plan))
	inspectedAt := s.nowUTC()
	inspectionMS := durationMilliseconds(inspectedAt.Sub(outcome.Attempt.CompletedAt))
	outcome.Attempt.AttemptResult = attemptResultStreamError
	outcome.Attempt.Diagnostics = &diagnostic
	outcome.Attempt.ResponseTimeMS += inspectionMS
	outcome.Attempt.AttemptDurationMS += inspectionMS
	outcome.Attempt.CompletedAt = inspectedAt
	state.attempts[len(state.attempts)-1] = outcome.Attempt
	state.lastLaunchRerouted = true
	state.lastError = diagnostic.Code
	state.recordRetry(gatewaycore.RouteReasonRerouteHTTP)
	return true
}
