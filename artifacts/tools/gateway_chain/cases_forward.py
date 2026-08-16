"""L2/L3 — real forwarding to real upstream models, success and failure.

Every case here spends a real upstream call. Prompts are tiny and max_tokens is
capped so a full run costs a handful of tokens per case.
"""

from __future__ import annotations

from .runner import Case, CaseContext
from .support import (
    as_bool,
    as_int,
    assemble_stream_content,
    dead_model,
    live_model,
    marker,
    wait_for_rows,
)


def _non_streaming_success(context: CaseContext) -> None:
    model = live_model(context)
    token = marker("CHAIN")
    before = context.store.max_id("request_logs")
    response = context.gateway.chat(model, f"Reply with exactly: {token}", max_tokens=32)
    payload = response.json() or {}
    context.record("status", response.status)
    context.record("model_echo", payload.get("model"))
    context.record("usage", payload.get("usage"))

    context.expect_eq("gateway returns the upstream 200", 200, response.status)
    content = ""
    for choice in payload.get("choices") or []:
        content += (choice.get("message") or {}).get("content") or ""
    context.expect(
        "the real model answered with the requested marker",
        token in content,
        expected=f"content contains {token}",
        actual=content[:120],
    )
    usage = payload.get("usage") or {}
    context.expect(
        "upstream reported prompt tokens",
        as_int(usage.get("prompt_tokens")) not in (None, 0),
        expected=">0",
        actual=usage.get("prompt_tokens"),
    )

    rows = wait_for_rows(context, "request_logs", before, expected=1)
    context.expect_eq("exactly one attempt was logged", 1, len(rows))
    if rows:
        row = rows[0]
        context.state["last_success_request_log_id"] = as_int(row.get("id"))
        context.expect_eq("logged operation is openai.chat_completions", "openai.chat_completions", row.get("operation_name"))
        context.expect_eq("logged caller model matches the request", model, row.get("model_id"))
        context.expect_eq("logged upstream status is 200", 200, as_int(row.get("upstream_status_code")))
        context.expect_eq("attempt is marked successful", True, as_bool(row.get("success_flag")))
        context.expect_eq("attempt is marked non-streaming", False, as_bool(row.get("is_stream")))
        context.expect_eq(
            "logged input tokens match what the upstream reported",
            as_int(usage.get("prompt_tokens")),
            as_int(row.get("input_tokens")),
        )
        context.expect_eq(
            "logged output tokens match what the upstream reported",
            as_int(usage.get("completion_tokens")),
            as_int(row.get("output_tokens")),
        )


def _streaming_success(context: CaseContext) -> None:
    model = live_model(context)
    token = marker("STREAM")
    before = context.store.max_id("request_logs")
    response = context.gateway.chat(model, f"Reply with exactly: {token}", max_tokens=32, streaming=True)
    context.record("status", response.status)
    context.record("sse_event_count", len(response.sse_events))

    context.expect_eq("streamed request returns 200", 200, response.status)
    context.expect(
        "response is an event stream",
        "text/event-stream" in (response.header("content-type") or ""),
        expected="text/event-stream",
        actual=response.header("content-type"),
    )
    context.expect(
        "stream terminates with [DONE]",
        any("[DONE]" in event for event in response.sse_events),
        expected="a [DONE] frame",
        actual=response.sse_events[-1][:80] if response.sse_events else "(no frames)",
    )
    assembled = assemble_stream_content(response.sse_events)
    context.record("assembled_content", assembled[:200])
    context.expect(
        "streamed deltas assemble into the requested marker",
        token in assembled,
        expected=f"assembled content contains {token}",
        actual=assembled[:200],
    )

    rows = wait_for_rows(context, "request_logs", before, expected=1)
    context.expect_eq("exactly one attempt was logged", 1, len(rows))
    if rows:
        row = rows[0]
        context.expect_eq("attempt is marked streaming", True, as_bool(row.get("is_stream")))
        context.expect_eq("attempt is marked successful", True, as_bool(row.get("success_flag")))
        context.expect_eq("logged upstream status is 200", 200, as_int(row.get("upstream_status_code")))
        context.expect(
            "streaming outcome is recorded",
            bool(row.get("stream_outcome")),
            expected="non-empty stream_outcome",
            actual=row.get("stream_outcome"),
        )


def _responses_operation(context: CaseContext) -> None:
    """The Responses operation is native-only; it must be routable on a
    dual_native model without any translation."""
    model = live_model(context)
    before = context.store.max_id("request_logs")
    response = context.gateway.responses(model, f"Reply with exactly: {marker('RESP')}")
    context.record("status", response.status)
    context.expect_eq("POST /v1/responses returns 200", 200, response.status)

    rows = wait_for_rows(context, "request_logs", before, expected=1)
    context.expect_eq("exactly one attempt was logged", 1, len(rows))
    if rows:
        context.expect_eq("logged operation is openai.responses", "openai.responses", rows[0].get("operation_name"))


def _models_operation(context: CaseContext) -> None:
    """GET /v1/models is served from configuration, so it must list every
    caller-visible model without touching an upstream."""
    response = context.gateway.runtime_raw("GET", "/v1/models", timeout=30.0)
    payload = response.json() or {}
    listed = {entry.get("id") for entry in payload.get("data") or []}
    configured = {
        row["model_id"]
        for row in context.store.rows("select model_id from model_configs", ["model_id"])
        if row["model_id"]
    }
    context.record("listed_count", len(listed))
    context.record("configured_count", len(configured))
    context.expect_eq("GET /v1/models returns 200", 200, response.status)
    context.expect_eq("every configured model is listed", set(), configured - listed)


def _upstream_rejection_relayed(context: CaseContext) -> None:
    """A real upstream refusal must be relayed faithfully and still recorded."""
    model = dead_model(context)
    before = context.store.max_id("request_logs")
    response = context.gateway.chat(model, marker("DEAD"), max_tokens=8, timeout=90.0)
    context.record("status", response.status)
    context.record("body", response.text[:300])
    context.expect(
        "the upstream's own rejection status is relayed",
        response.status in (401, 402, 403, 429),
        expected="a 4xx from the upstream",
        actual=response.status,
    )

    rows = wait_for_rows(context, "request_logs", before, expected=1)
    context.expect("the failed attempt is logged", len(rows) >= 1, expected=">=1", actual=len(rows))
    for row in rows:
        context.expect_eq("attempt is marked unsuccessful", False, as_bool(row.get("success_flag")))
        context.expect_eq("failure is attributed to the upstream", "upstream", row.get("error_source"))
        context.expect_eq("failure stage is the upstream response", "upstream_response", row.get("failure_stage"))
        context.expect(
            "upstream status is recorded on the scoped column",
            as_int(row.get("upstream_status_code")) is not None,
            expected="non-null upstream_status_code",
            actual=row.get("upstream_status_code"),
        )


def _failover_across_targets(context: CaseContext) -> None:
    """A caller model with several peers must try them in order and leave one
    row per attempt, with exactly one winner."""
    model = context.state.get("failover_model")
    if not model:
        context.block("no caller model with more than one access target was discovered")
    before_logs = context.store.max_id("request_logs")
    before_usage = context.store.max_id("usage_request_events")
    response = context.gateway.chat(model, marker("FAILOVER"), max_tokens=8, timeout=180.0)
    context.record("status", response.status)

    rows = wait_for_rows(context, "request_logs", before_logs, expected=2)
    context.record("attempt_rows", len(rows))
    context.expect("more than one target was attempted", len(rows) >= 2, expected=">=2", actual=len(rows))

    attempts = [as_int(row.get("attempt_number")) for row in rows]
    context.record("attempt_numbers", attempts)
    context.expect_eq(
        "attempt numbers are a contiguous 1..N sequence",
        list(range(1, len(rows) + 1)),
        attempts,
    )
    winners = [row for row in rows if as_bool(row.get("is_winner"))]
    context.expect_eq("exactly one attempt is the winner", 1, len(winners))
    targets = [row.get("resolved_target_model_id") for row in rows]
    context.record("resolved_targets", targets)
    context.expect_eq("each attempt resolved a target", 0, sum(1 for target in targets if not target))

    usage = wait_for_rows(context, "usage_request_events", before_usage, expected=1)
    context.expect_eq("one usage event per caller request", 1, len(usage))
    if usage:
        event = usage[0]
        context.expect_eq("usage event counts every attempt", len(rows), as_int(event.get("attempt_count")))
        context.expect_eq(
            "usage event's expected row count matches the attempts written",
            len(rows),
            as_int(event.get("expected_request_log_row_count")),
        )
        context.expect_eq("failover is recorded", True, as_bool(event.get("failover_occurred")))
        context.expect_eq("routing evidence is complete", True, as_bool(event.get("routing_evidence_complete")))


CASES = [
    Case("L2-01", "forward", "non-streaming call reaches a real model and is logged", _non_streaming_success, requires_live_upstream=True),
    Case("L2-02", "forward", "streaming SSE call reaches a real model and is logged", _streaming_success, requires_live_upstream=True),
    Case("L2-03", "forward", "native Responses operation routes and is logged", _responses_operation, requires_live_upstream=True),
    Case("L2-04", "forward", "GET /v1/models lists every configured model", _models_operation),
    Case("L3-01", "forward", "real upstream rejection is relayed and recorded", _upstream_rejection_relayed),
    Case("L3-02", "forward", "failover walks every peer and leaves one row per attempt", _failover_across_targets),
]
