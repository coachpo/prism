# ADR 0001 — Keep stream-start rejections request-scoped

## Context

A provider can return HTTP 2xx and report an error in the first SSE event. An unsupported reasoning parameter, for example, can affect only the current request. The earlier failover path treated that event as Ban Policy feedback, put the target into a retry window, and diverted later unrelated requests.

## Decision

When Prism can identify a provider error in the first SSE event before committing to that stream and another candidate remains, it closes the rejected stream and continues the same request. It records the next launch as `reroute` and writes no shared routing failure state for the stream-start rejection.

## Consequences

A later request can still select the same target, with the usual admission and Ban Policy checks. Request history can distinguish a reroute from a Ban Policy failover. The behavior is limited to the existing bounded first-event inspection. See the [streaming contract](../runtime-routing.md#33-runtime-request-streaming) for the cases where Prism passes the stream through.

## Evidence

The decision and rationale are recorded in commit `d456757`. The implementation separates [first-event inspection](../../backend/internal/httpapi/runtime/stream_start_inspection.go) from [request execution](../../backend/internal/httpapi/runtime/request_execution_loop.go). The [runtime regression test](../../backend/tests/runtime/stream_start_error_reroute_test.go) covers rerouting and subsequent requests.
